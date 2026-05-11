package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/hatyibei/shingan/domain"
)

// ADKGoOption configures an ADKGoParser.
type ADKGoOption func(*ADKGoParser)

// WithoutTypes disables the go/types second-pass analysis.
// Useful for fast testing where type information is not needed.
func WithoutTypes() ADKGoOption {
	return func(p *ADKGoParser) { p.enableTypes = false }
}

// ADKGoParser parses WorkflowGraph from ADK-Go source code using Go AST analysis.
// When enableTypes is true (default), a go/types second-pass is attempted via
// ParseFile to resolve generic type arguments of functiontool.New[TArgs, TResults].
type ADKGoParser struct {
	enableTypes bool
}

// NewADKGoParser returns a ready-to-use ADKGoParser.
// By default the go/types second-pass is enabled; pass WithoutTypes() to disable it.
func NewADKGoParser(opts ...ADKGoOption) *ADKGoParser {
	p := &ADKGoParser{enableTypes: true}
	for _, o := range opts {
		o(p)
	}
	return p
}

// SupportedFormat implements application.WorkflowParser.
func (p *ADKGoParser) SupportedFormat() string {
	return "adk-go"
}

// Parse analyzes ADK-Go source bytes and constructs a WorkflowGraph.
// It recognises SequentialAgent, LoopAgent, ParallelAgent, and LlmAgent composite literals.
//
// The synthetic filename "input.go" is used in token positions because no
// real path is available at this entry point. Callers that have a real
// path on disk should use ParseFile, which preserves it in Pos.File so
// downstream features (--since, LSP, code actions) can attribute findings
// back to the originating source file.
func (p *ADKGoParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	return p.parseWithFilename(input, "input.go")
}

// parseWithFilename is the internal entry point used by both Parse and
// ParseFile. It threads the source filename through to the FileSet so
// each Node.Pos.File reflects the actual file rather than the literal
// "input.go" placeholder. Per Codex iter2 P1: ParseFile previously fell
// through to Parse and lost the real path, breaking --since/LSP path
// matching for multi-file ADK-Go inputs.
func (p *ADKGoParser) parseWithFilename(input []byte, filename string) (*domain.WorkflowGraph, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, input, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("adk-go parser: parse Go source: %w", err)
	}

	b := &adkgoBuilder{
		fset:             fset,
		file:             file,
		nodes:            make(map[string]*domain.Node),
		counter:          0,
		varDecls:         make(map[string]*ast.CompositeLit),
		varCallArgs:      make(map[string]*ast.CompositeLit),
		varAgentIDs:      make(map[string]string),
		varFuncToolNames: make(map[string]string),
	}

	// Pre-scan package-level var declarations so identifier references can be resolved.
	b.collectVarDecls()

	// Find entry candidates: top-level vars/assignments containing orchestrator agents.
	entryCandidates := b.findEntryCandidates()

	if len(entryCandidates) == 0 {
		// No orchestrator (Sequential/Loop/Parallel) — but the function-walk
		// inside findEntryCandidates may still have produced standalone
		// LlmAgent nodes (factory pattern: `func NewX() agent.Agent { x, _ :=
		// llmagent.New(...) }`, common in google/adk-samples). Promote the
		// first such node to the graph entry instead of dropping everything.
		// Without this, single-agent factory files would silently parse as
		// empty graphs (dogfood: financial-advisor/agents/data_analyst.go
		// returned 0 findings even though the parser correctly extracted
		// the LLM agent).
		if len(b.nodes) > 0 {
			entryCandidates = b.firstStandaloneAgentID()
		}
		if len(entryCandidates) == 0 {
			// Two sub-cases:
			//   (a) Nothing parsed at all → return an empty graph (legacy).
			//   (b) Multiple ambiguous roots (Codex round-2 P6) →
			//       preserve the nodes so the catalog reflects what's
			//       in the file, but leave EntryNodeID empty so the
			//       reachability rule doesn't manufacture
			//       unreachable_node FPs against an arbitrarily-chosen
			//       leaf.
			if len(b.nodes) == 0 {
				return &domain.WorkflowGraph{
					Nodes: make(map[string]*domain.Node),
					Edges: []domain.Edge{},
				}, nil
			}
			return &domain.WorkflowGraph{
				Nodes: b.nodes,
				Edges: b.edges,
				// EntryNodeID intentionally empty.
			}, nil
		}
	}

	// Check for //shingan:entry annotation to override the default first-entry selection.
	entryNodeID := b.findShingaEntryAnnotation(entryCandidates)
	if entryNodeID == "" {
		entryNodeID = entryCandidates[0]
	}

	graph := &domain.WorkflowGraph{
		Nodes:       b.nodes,
		Edges:       b.edges,
		EntryNodeID: entryNodeID,
	}
	return graph, nil
}

// adkgoBuilder holds parsing state while walking the AST.
type adkgoBuilder struct {
	fset             *token.FileSet
	file             *ast.File
	nodes            map[string]*domain.Node
	edges            []domain.Edge
	counter          int
	varDecls         map[string]*ast.CompositeLit // package-level var name -> composite literal (bare struct)
	varCallArgs      map[string]*ast.CompositeLit // var name -> Config literal passed to pkgname.New(cfg)
	varAgentIDs      map[string]string            // var name -> already-assigned nodeID (real-API style)
	varFuncToolNames map[string]string            // var name -> tool name from functiontool.New(Config{Name:...}, ...)
}

// sourcePos converts a token.Pos into a domain.SourcePos using the builder's FileSet.
// Returns a zero SourcePos for token.NoPos so IsZero remains a reliable predicate.
func (b *adkgoBuilder) sourcePos(pos token.Pos) domain.SourcePos {
	if !pos.IsValid() {
		return domain.SourcePos{}
	}
	p := b.fset.Position(pos)
	return domain.SourcePos{
		File: p.Filename,
		Line: p.Line,
		Col:  p.Column,
	}
}

// collectVarDecls pre-scans the file for package-level var declarations of agent composite literals.
// It handles both bare struct literals (`var x = &SequentialAgent{...}`) and
// real ADK-Go SDK constructor calls (`var x, _ = loopagent.New(loopagent.Config{...})`).
func (b *adkgoBuilder) collectVarDecls() {
	for _, decl := range b.file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				val := vs.Values[i]
				if cl := extractCompositeLit(val); cl != nil {
					b.varDecls[name.Name] = cl
					continue
				}
				// Detect functiontool.New(Config{Name: "..."}, handler) at package level.
				if toolName := extractFuncToolName(val); toolName != "" {
					b.varFuncToolNames[name.Name] = toolName
					continue
				}
				// Also handle pkgname.New(Config{...}) at package level.
				if cfg := extractNewCallConfig(val); cfg != nil {
					b.varCallArgs[name.Name] = cfg
				}
			}
		}
	}
}

// collectFuncVarDecls scans a function body for short variable declarations of
// agent constructor calls: `x, _ := loopagent.New(loopagent.Config{...})`.
// Must be called before processAgentLit so that sub-agent ident references can
// be resolved.
func (b *adkgoBuilder) collectFuncVarDecls(body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		stmt, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// Only short-variable declarations (:=).
		if stmt.Tok != token.DEFINE {
			return true
		}
		for i, lhs := range stmt.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok || ident.Name == "_" {
				continue
			}
			if i >= len(stmt.Rhs) {
				continue
			}
			// Detect functiontool.New(...) in function body.
			if toolName := extractFuncToolName(stmt.Rhs[i]); toolName != "" {
				b.varFuncToolNames[ident.Name] = toolName
				continue
			}
			if cfg := extractNewCallConfig(stmt.Rhs[i]); cfg != nil {
				b.varCallArgs[ident.Name] = cfg
			}
		}
		return true
	})
}

// extractNewCallConfig extracts the first CompositeLit argument from a
// pkgname.New(Config{...}) call expression, which is the pattern used by
// google.golang.org/adk workflow and LLM agent constructors.
func extractNewCallConfig(expr ast.Expr) *ast.CompositeLit {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	// Function must be a SelectorExpr (pkgname.New or pkgname.New[...]).
	fun := call.Fun
	// Handle generic instantiation: pkgname.New[T, R](...)
	if idx, ok2 := fun.(*ast.IndexListExpr); ok2 {
		fun = idx.X
	} else if idx, ok2 := fun.(*ast.IndexExpr); ok2 {
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return nil
	}
	// Ensure we have at least one argument that is a CompositeLit (or &CompositeLit).
	if len(call.Args) == 0 {
		return nil
	}
	return extractCompositeLit(call.Args[0])
}

// extractFuncToolName detects a functiontool.New(Config{Name: "..."}, handler) call
// and extracts the tool name from the Config's Name field.
// Returns "" if the expression is not this pattern.
func extractFuncToolName(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	// Unwrap generic index expressions (functiontool.New[T, R](...)).
	fun := call.Fun
	if idx, ok2 := fun.(*ast.IndexListExpr); ok2 {
		fun = idx.X
	} else if idx, ok2 := fun.(*ast.IndexExpr); ok2 {
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return ""
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || strings.ToLower(pkgIdent.Name) != "functiontool" {
		return ""
	}
	// First argument must be a CompositeLit (functiontool.Config{Name: "..."}).
	if len(call.Args) == 0 {
		return ""
	}
	cfgLit := extractCompositeLit(call.Args[0])
	if cfgLit == nil {
		return ""
	}
	fields := extractKeyedFields(cfgLit)
	return stringFieldValue(fields, "Name")
}

// firstStandaloneAgentID returns a 1-element slice with the most
// plausible graph entry — used as a fallback when the file declares no
// orchestrator (Sequential/Loop/Parallel). Selection preference:
//
//  1. LLM nodes with zero incoming edges (the root agent that wraps
//     others as tools, e.g. `financial_coordinator` in
//     google/adk-samples/financial-advisor).
//  2. Any node with zero incoming edges (handles cases where the
//     root happens to be a Tool/Loop/Sequence).
//  3. Alphabetically-first node id (legacy fallback for graphs with
//     no clear root — every node has an incoming edge, typically a
//     pure cycle).
//
// Without preference #1 the parser picked `data_analyst` over
// `financial_coordinator` purely on alphabetic order, which then
// made the other four agents wrongly report as `unreachable_node`
// from the perspective of an entry that's actually a leaf.
func (b *adkgoBuilder) firstStandaloneAgentID() []string {
	if len(b.nodes) == 0 {
		return nil
	}
	inDegree := make(map[string]int, len(b.nodes))
	for id := range b.nodes {
		inDegree[id] = 0
	}
	for _, e := range b.edges {
		inDegree[e.To]++
	}
	var rootLLMs, rootAny []string
	for id, n := range b.nodes {
		if inDegree[id] == 0 {
			rootAny = append(rootAny, id)
			if n != nil && n.Type == domain.NodeTypeLLM {
				rootLLMs = append(rootLLMs, id)
			}
		}
	}
	// Codex round-2 P6: only auto-promote when the root is
	// unambiguous. A file with two unrelated standalone factories
	// (NewA / NewB, no edges between them) has TWO zero-indegree
	// LLMs — picking one alphabetically would manufacture an
	// `unreachable_node` finding on the other. Returning nil here
	// leaves EntryNodeID empty, which signals "no clear root" to
	// downstream rules (unreachable_node skips its check in that
	// case).
	if len(rootLLMs) == 1 {
		return rootLLMs[:1]
	}
	if len(rootLLMs) > 1 {
		return nil
	}
	if len(rootAny) == 1 {
		return rootAny[:1]
	}
	if len(rootAny) > 1 {
		return nil
	}
	// All nodes have incoming edges — graph is a closed cycle.
	// Pick alphabetically for deterministic output (the cycle itself
	// is what callers care about, not the entry).
	var pickFrom []string
	for id := range b.nodes {
		pickFrom = append(pickFrom, id)
	}
	sort.Strings(pickFrom)
	if len(pickFrom) == 0 {
		return nil
	}
	return pickFrom[:1]
}

// findEntryCandidates walks the AST to find all top-level orchestrator agents
// (SequentialAgent, LoopAgent, ParallelAgent) and processes them into the graph.
// Returns a list of node IDs in order of discovery.
// Handles both bare struct literals and the real ADK-Go SDK constructor pattern.
func (b *adkgoBuilder) findEntryCandidates() []string {
	var candidates []string

	// Walk all top-level declarations.
	for _, decl := range b.file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// Package-level var declarations.
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for idx, nameIdent := range vs.Names {
					if idx >= len(vs.Values) {
						continue
					}
					val := vs.Values[idx]

					// Bare struct literal: var x = &SequentialAgent{...}
					if cl := extractCompositeLit(val); cl != nil {
						typeName := compositeLitTypeName(cl)
						if isOrchestratorType(typeName) {
							nodeID := b.processAgentLit(cl, nil)
							if nodeID != "" {
								candidates = append(candidates, nodeID)
							}
						}
						continue
					}

					// Real ADK-Go SDK call: var x, _ = loopagent.New(loopagent.Config{...})
					if cfg, ok2 := b.varCallArgs[nameIdent.Name]; ok2 {
						agentType := resolveConfigAgentType(val)
						if isOrchestratorType(agentType) {
							nodeID := b.processRealAPIConfig(cfg, agentType, val)
							if nodeID != "" {
								b.varAgentIDs[nameIdent.Name] = nodeID
								candidates = append(candidates, nodeID)
							}
						}
					}
				}
			}

		case *ast.FuncDecl:
			// Functions: look for local assignments.
			if d.Body == nil {
				continue
			}
			// First pass: collect all variable declarations in this function body
			// so that sub-agent ident references resolve correctly.
			b.collectFuncVarDecls(d.Body)

			// Second pass: process LlmAgent vars so they're in varAgentIDs when
			// orchestrators reference them as sub-agents.
			b.processFuncLlmAgents(d.Body)

			// Third pass: find orchestrators.
			ast.Inspect(d.Body, func(n ast.Node) bool {
				stmt, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range stmt.Rhs {
					// Bare struct literal path.
					if cl := extractCompositeLit(rhs); cl != nil {
						typeName := compositeLitTypeName(cl)
						if isOrchestratorType(typeName) {
							nodeID := b.processAgentLit(cl, nil)
							if nodeID != "" {
								candidates = append(candidates, nodeID)
							}
						}
						continue
					}
					// Real ADK-Go SDK call path.
					if cfg := extractNewCallConfig(rhs); cfg != nil {
						agentType := resolveConfigAgentType(rhs)
						if isOrchestratorType(agentType) {
							nodeID := b.processRealAPIConfig(cfg, agentType, rhs)
							if nodeID != "" {
								// Record variable name if lhs is a single ident.
								if i < len(stmt.Lhs) {
									if lhsIdent, ok2 := stmt.Lhs[i].(*ast.Ident); ok2 && lhsIdent.Name != "_" {
										b.varAgentIDs[lhsIdent.Name] = nodeID
									}
								}
								candidates = append(candidates, nodeID)
							}
						}
					}
				}
				return true
			})
		}
	}

	return candidates
}

// processFuncLlmAgents walks a function body and processes LlmAgent constructor
// calls so their nodeIDs are recorded in varAgentIDs before orchestrators try
// to resolve sub-agent references.
func (b *adkgoBuilder) processFuncLlmAgents(body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		stmt, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range stmt.Rhs {
			cfg := extractNewCallConfig(rhs)
			if cfg == nil {
				continue
			}
			agentType := resolveConfigAgentType(rhs)
			if agentType != "LlmAgent" {
				continue
			}
			nodeID := b.processRealAPIConfig(cfg, agentType, rhs)
			if nodeID != "" && i < len(stmt.Lhs) {
				if lhsIdent, ok2 := stmt.Lhs[i].(*ast.Ident); ok2 && lhsIdent.Name != "_" {
					b.varAgentIDs[lhsIdent.Name] = nodeID
				}
			}
		}
		return true
	})
}

// resolveConfigAgentType determines the semantic agent type from a
// pkgname.New(pkgname.Config{...}) call expression by inspecting the package
// qualifier of the function being called.
// Returns "SequentialAgent", "LoopAgent", "ParallelAgent", "LlmAgent", or "".
func resolveConfigAgentType(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	fun := call.Fun
	// Strip generic index expressions.
	if idx, ok2 := fun.(*ast.IndexListExpr); ok2 {
		fun = idx.X
	} else if idx, ok2 := fun.(*ast.IndexExpr); ok2 {
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return ""
	}
	// X is the package identifier (e.g. "loopagent", "sequentialagent", "llmagent").
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return agentTypeFromPackage(pkgIdent.Name)
}

// agentTypeFromPackage maps ADK-Go package names to Shingan agent type names.
func agentTypeFromPackage(pkg string) string {
	switch strings.ToLower(pkg) {
	case "loopagent":
		return "LoopAgent"
	case "sequentialagent":
		return "SequentialAgent"
	case "parallelagent":
		return "ParallelAgent"
	case "llmagent":
		return "LlmAgent"
	}
	return ""
}

// processRealAPIConfig processes the Config composite literal of an ADK-Go SDK
// New() call and builds graph nodes accordingly.
// For workflow agents, Name/SubAgents live in AgentConfig.
// MaxIterations lives directly in loopagent.Config.
func (b *adkgoBuilder) processRealAPIConfig(cfg *ast.CompositeLit, agentType string, callExpr ast.Expr) string {
	topFields := extractKeyedFields(cfg)

	// Resolve Name and SubAgents: for workflow agents they are inside AgentConfig.
	var name string
	var subAgentFields map[string]ast.Expr
	if agentConfigExpr, ok := topFields["AgentConfig"]; ok {
		// Workflow agent style: sequentialagent.Config{AgentConfig: agent.Config{...}}
		if acCL := extractCompositeLit(agentConfigExpr); acCL != nil {
			acFields := extractKeyedFields(acCL)
			name = stringFieldValue(acFields, "Name")
			subAgentFields = acFields
		}
	} else {
		// LlmAgent style: all fields at top level.
		name = stringFieldValue(topFields, "Name")
		subAgentFields = topFields
	}

	nodeID := b.resolveNodeID(name)

	// Anchor the source position at the Config literal; fall back to the
	// enclosing call expression so we still record a location when the Config
	// composite is unusual.
	pos := b.sourcePos(cfg.Pos())
	if pos.IsZero() && callExpr != nil {
		pos = b.sourcePos(callExpr.Pos())
	}

	switch agentType {
	case "LlmAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLLM,
			Config: make(map[string]any),
			Pos:    pos,
		}
		if instr := stringFieldValue(topFields, "Instruction"); instr != "" {
			node.Config["instruction"] = instr
		}
		b.nodes[nodeID] = node
		b.processToolsRealAPI(topFields, nodeID)

	case "SequentialAgent":
		// NodeTypeSequence — not Loop. SequentialAgent runs its
		// sub-agents in fixed order once, so max_iterations is not a
		// meaningful concept and loop_guard must NOT fire here.
		// Dogfood: google/adk-samples llm-auditor (2026-05-11).
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeSequence,
			Config: make(map[string]any),
			Pos:    pos,
		}
		b.nodes[nodeID] = node
		if subAgentFields != nil {
			b.processSubAgentsSequentialReal(subAgentFields, nodeID)
		}

	case "LoopAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLoop,
			Config: make(map[string]any),
			Pos:    pos,
		}
		// MaxIterations is in loopagent.Config directly (not inside AgentConfig).
		if maxIter := intFieldValue(topFields, "MaxIterations"); maxIter != nil {
			node.Config["max_iterations"] = *maxIter
		}
		b.nodes[nodeID] = node
		if subAgentFields != nil {
			b.processSubAgentsLoopReal(subAgentFields, nodeID)
		}

	case "ParallelAgent":
		// NodeTypeParallel — distinct from Loop. ParallelAgent fans
		// out to its sub-agents concurrently; max_iterations is not
		// applicable, but cost / max_parallel_branches rules want
		// to fire here, hence a dedicated type rather than reuse of
		// NodeTypeControl.
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeParallel,
			Config: make(map[string]any),
			Pos:    pos,
		}
		b.nodes[nodeID] = node
		if subAgentFields != nil {
			b.processSubAgentsParallelReal(subAgentFields, nodeID)
		}

	default:
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLLM,
			Config: make(map[string]any),
			Pos:    pos,
		}
		b.nodes[nodeID] = node
	}

	return nodeID
}

// processSubAgentsSequentialReal processes SubAgents for a SequentialAgent built with the real ADK API.
func (b *adkgoBuilder) processSubAgentsSequentialReal(fields map[string]ast.Expr, parentID string) {
	subAgents := b.extractRealSubAgents(fields)
	var prevID string
	for _, subID := range subAgents {
		if subID == "" {
			continue
		}
		if prevID == "" {
			b.edges = append(b.edges, domain.Edge{From: parentID, To: subID})
		} else {
			b.edges = append(b.edges, domain.Edge{From: prevID, To: subID})
		}
		prevID = subID
	}
}

// processSubAgentsLoopReal processes SubAgents for a LoopAgent built with the real ADK API.
func (b *adkgoBuilder) processSubAgentsLoopReal(fields map[string]ast.Expr, parentID string) {
	subAgents := b.extractRealSubAgents(fields)
	var firstID, prevID string
	for _, subID := range subAgents {
		if subID == "" {
			continue
		}
		if prevID == "" {
			firstID = subID
			b.edges = append(b.edges, domain.Edge{From: parentID, To: subID})
		} else {
			b.edges = append(b.edges, domain.Edge{From: prevID, To: subID})
		}
		prevID = subID
	}
	if prevID != "" && firstID != "" && prevID != firstID {
		b.edges = append(b.edges, domain.Edge{From: prevID, To: firstID, Condition: "loop_back"})
	} else if prevID != "" && prevID == firstID {
		b.edges = append(b.edges, domain.Edge{From: prevID, To: firstID, Condition: "loop_back"})
	}
}

// processSubAgentsParallelReal processes SubAgents for a ParallelAgent built with the real ADK API.
func (b *adkgoBuilder) processSubAgentsParallelReal(fields map[string]ast.Expr, parentID string) {
	subAgents := b.extractRealSubAgents(fields)
	for _, subID := range subAgents {
		if subID == "" {
			continue
		}
		b.edges = append(b.edges, domain.Edge{From: parentID, To: subID, Condition: "parallel_branch"})
	}
}

// extractRealSubAgents resolves the SubAgents slice in an agent.Config field.
// Each element is an identifier referring to a variable whose nodeID is in varAgentIDs.
func (b *adkgoBuilder) extractRealSubAgents(fields map[string]ast.Expr) []string {
	expr, ok := fields["SubAgents"]
	if !ok {
		return nil
	}
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var result []string
	for _, elt := range cl.Elts {
		if ident, ok2 := elt.(*ast.Ident); ok2 {
			if nodeID, found := b.varAgentIDs[ident.Name]; found {
				result = append(result, nodeID)
			} else {
				// Unknown identifier — create a placeholder.
				nodeID := toSnakeCase(ident.Name)
				if _, exists := b.nodes[nodeID]; !exists {
					b.nodes[nodeID] = &domain.Node{
						ID:     nodeID,
						Name:   ident.Name,
						Type:   domain.NodeTypeLLM,
						Config: make(map[string]any),
						Pos:    b.sourcePos(ident.Pos()),
					}
				}
				result = append(result, nodeID)
			}
		}
	}
	return result
}

// processToolsRealAPI handles the Tools field in llmagent.Config (real SDK style).
func (b *adkgoBuilder) processToolsRealAPI(fields map[string]ast.Expr, ownerID string) {
	toolsExpr, ok := fields["Tools"]
	if !ok {
		return
	}
	cl, ok := toolsExpr.(*ast.CompositeLit)
	if !ok {
		return
	}
	for _, elt := range cl.Elts {
		toolID := b.processToolElement(elt)
		if toolID == "" {
			continue
		}
		b.edges = append(b.edges, domain.Edge{From: ownerID, To: toolID})
	}
}

// findShingaEntryAnnotation looks for a //shingan:entry comment and returns the
// node ID of the nearest candidate, if found.
func (b *adkgoBuilder) findShingaEntryAnnotation(candidates []string) string {
	for _, cg := range b.file.Comments {
		for _, c := range cg.List {
			if strings.Contains(c.Text, "shingan:entry") && len(candidates) > 0 {
				return candidates[0]
			}
		}
	}
	return ""
}

// processAgentLit processes an agent composite literal, adds it to the graph,
// and returns the node ID.  parent is the node ID of the containing agent (for edge creation).
func (b *adkgoBuilder) processAgentLit(cl *ast.CompositeLit, parent *string) string {
	typeName := compositeLitTypeName(cl)
	fields := extractKeyedFields(cl)

	name := stringFieldValue(fields, "Name")
	nodeID := b.resolveNodeID(name)

	// The composite literal token is the declared position of the agent.
	pos := b.sourcePos(cl.Pos())

	switch typeName {
	case "LlmAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLLM,
			Config: make(map[string]any),
			Pos:    pos,
		}
		if model := stringFieldValue(fields, "Model"); model != "" {
			node.Config["model"] = model
		}
		if instr := stringFieldValue(fields, "Instruction"); instr != "" {
			node.Config["instruction"] = instr
		}
		b.nodes[nodeID] = node

		// Process Tools field.
		b.processTools(fields, nodeID)

	case "SequentialAgent":
		// See ADK-Go SequentialAgent case in the SDK-style path above
		// for the rationale on NodeTypeSequence vs NodeTypeControl.
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeSequence,
			Config: make(map[string]any),
			Pos:    pos,
		}
		b.nodes[nodeID] = node
		b.processSubAgentsSequential(fields, nodeID)

	case "LoopAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLoop,
			Config: make(map[string]any),
			Pos:    pos,
		}
		if maxIter := intFieldValue(fields, "MaxIterations"); maxIter != nil {
			node.Config["max_iterations"] = *maxIter
		}
		b.nodes[nodeID] = node
		b.processSubAgentsLoop(fields, nodeID)

	case "ParallelAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeParallel,
			Config: make(map[string]any),
			Pos:    pos,
		}
		b.nodes[nodeID] = node
		b.processSubAgentsParallel(fields, nodeID)

	default:
		// Unknown type — create a generic node.
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLLM,
			Config: make(map[string]any),
			Pos:    pos,
		}
		b.nodes[nodeID] = node
	}

	return nodeID
}

// processSubAgentsSequential processes SubAgents for a SequentialAgent,
// creating sequential edges between consecutive sub-agents.
func (b *adkgoBuilder) processSubAgentsSequential(fields map[string]ast.Expr, parentID string) {
	subAgents := extractSubAgents(fields)
	var prevID string
	for _, sub := range subAgents {
		subID := b.processSubAgent(sub)
		if subID == "" {
			continue
		}
		if prevID == "" {
			// First sub-agent: edge from parent to first sub-agent.
			b.edges = append(b.edges, domain.Edge{From: parentID, To: subID})
		} else {
			b.edges = append(b.edges, domain.Edge{From: prevID, To: subID})
		}
		prevID = subID
	}
}

// processSubAgentsLoop processes SubAgents for a LoopAgent,
// creating sequential edges and a loopback edge from last to first sub-agent.
func (b *adkgoBuilder) processSubAgentsLoop(fields map[string]ast.Expr, parentID string) {
	subAgents := extractSubAgents(fields)
	var firstID, prevID string
	for _, sub := range subAgents {
		subID := b.processSubAgent(sub)
		if subID == "" {
			continue
		}
		if prevID == "" {
			firstID = subID
			b.edges = append(b.edges, domain.Edge{From: parentID, To: subID})
		} else {
			b.edges = append(b.edges, domain.Edge{From: prevID, To: subID})
		}
		prevID = subID
	}
	// Loopback: last → first.
	if prevID != "" && firstID != "" && prevID != firstID {
		b.edges = append(b.edges, domain.Edge{From: prevID, To: firstID, Condition: "loop_back"})
	} else if prevID != "" && prevID == firstID {
		// Single sub-agent: self-loop.
		b.edges = append(b.edges, domain.Edge{From: prevID, To: firstID, Condition: "loop_back"})
	}
}

// processSubAgentsParallel processes SubAgents for a ParallelAgent,
// creating parallel branch edges from parent to all sub-agents.
func (b *adkgoBuilder) processSubAgentsParallel(fields map[string]ast.Expr, parentID string) {
	subAgents := extractSubAgents(fields)
	for _, sub := range subAgents {
		subID := b.processSubAgent(sub)
		if subID == "" {
			continue
		}
		b.edges = append(b.edges, domain.Edge{From: parentID, To: subID, Condition: "parallel_branch"})
	}
}

// processSubAgent processes a single sub-agent expression and returns its node ID.
func (b *adkgoBuilder) processSubAgent(expr ast.Expr) string {
	// Composite literal (inline agent definition).
	if cl := extractCompositeLit(expr); cl != nil {
		return b.processAgentLit(cl, nil)
	}
	// Identifier reference to a package-level var.
	if ident, ok := expr.(*ast.Ident); ok {
		// Check real-API varAgentIDs first (already-processed nodes).
		if nodeID, found := b.varAgentIDs[ident.Name]; found {
			return nodeID
		}
		// Check bare struct literal varDecls.
		if cl, found := b.varDecls[ident.Name]; found {
			return b.processAgentLit(cl, nil)
		}
		// Unknown identifier — create a placeholder node.
		nodeID := toSnakeCase(ident.Name)
		if _, exists := b.nodes[nodeID]; !exists {
			b.nodes[nodeID] = &domain.Node{
				ID:     nodeID,
				Name:   ident.Name,
				Type:   domain.NodeTypeLLM,
				Config: make(map[string]any),
				Pos:    b.sourcePos(ident.Pos()),
			}
		}
		return nodeID
	}
	return ""
}

// processTools handles the Tools field, creating NodeTypeTool nodes and edges.
func (b *adkgoBuilder) processTools(fields map[string]ast.Expr, ownerID string) {
	toolsExpr, ok := fields["Tools"]
	if !ok {
		return
	}
	cl, ok := toolsExpr.(*ast.CompositeLit)
	if !ok {
		return
	}
	for _, elt := range cl.Elts {
		toolID := b.processToolElement(elt)
		if toolID == "" {
			continue
		}
		b.edges = append(b.edges, domain.Edge{From: ownerID, To: toolID})
	}
}

// processToolElement extracts a single tool reference and creates a tool node.
// It resolves identifier names against varFuncToolNames (functiontool.New results)
// to obtain the tool's declared Name from its Config, falling back to the variable name.
func (b *adkgoBuilder) processToolElement(expr ast.Expr) string {
	// Try to get the identifier name first (for varFuncToolNames lookup).
	if ident, ok := expr.(*ast.Ident); ok {
		// If this var was created via functiontool.New(Config{Name: "..."}, handler),
		// use the declared tool name from the Config for better accuracy.
		if toolName, found := b.varFuncToolNames[ident.Name]; found {
			nodeID := toSnakeCase(toolName)
			if _, exists := b.nodes[nodeID]; !exists {
				b.nodes[nodeID] = &domain.Node{
					ID:     nodeID,
					Name:   toolName,
					Type:   domain.NodeTypeTool,
					Config: map[string]any{"category": inferToolCategory(toolName)},
					Pos:    b.sourcePos(ident.Pos()),
				}
			}
			return nodeID
		}
	}

	// `<pkg>tool.New(arg, ...)` constructor calls — unwrap to the first
	// positional argument so the tool name reflects what's being wrapped
	// rather than the constructor verb "New". Triggered by
	// google/adk-samples financial-advisor (2026-05-11):
	//   `agenttool.New(dataAnalyst, nil)` previously surfaced as a tool
	//   named "new" because extractIdentOrSelectorName walked through
	//   the CallExpr to its Fun selector and returned "New".
	if call, ok := expr.(*ast.CallExpr); ok && isToolConstructorCall(call) && len(call.Args) > 0 {
		// `functiontool.New(Config{Name: "..."}, handler)` — inline
		// form (not assigned to a var first). Pull the declared name
		// straight from the Config literal so the tool node carries
		// the user-intended identifier, not the wrapped variable's
		// name. Symmetric with the var-assigned path resolved via
		// varFuncToolNames above.
		if toolName := extractFuncToolName(call); toolName != "" {
			nodeID := toSnakeCase(toolName)
			if _, exists := b.nodes[nodeID]; !exists {
				b.nodes[nodeID] = &domain.Node{
					ID:     nodeID,
					Name:   toolName,
					Type:   domain.NodeTypeTool,
					Config: map[string]any{"category": inferToolCategory(toolName)},
					Pos:    b.sourcePos(call.Pos()),
				}
			}
			return nodeID
		}
		// Other tool constructors (agenttool.New, ...): the first arg
		// is the wrapped resource, so use its identifier as the tool
		// reference.
		return b.processToolElement(call.Args[0])
	}

	name := extractIdentOrSelectorName(expr)
	if name == "" {
		return ""
	}
	nodeID := toSnakeCase(name)
	if _, exists := b.nodes[nodeID]; !exists {
		b.nodes[nodeID] = &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeTool,
			Config: map[string]any{"category": inferToolCategory(name)},
			Pos:    b.sourcePos(expr.Pos()),
		}
	}
	return nodeID
}

// resolveNodeID generates a unique node ID from a name, or generates node_<n> if name is empty.
func (b *adkgoBuilder) resolveNodeID(name string) string {
	if name == "" {
		b.counter++
		return fmt.Sprintf("node_%d", b.counter)
	}
	id := toSnakeCase(name)
	// Ensure uniqueness.
	if _, exists := b.nodes[id]; exists {
		b.counter++
		return fmt.Sprintf("%s_%d", id, b.counter)
	}
	return id
}

// renameNode renames a node and updates all edges that reference the old ID.
func (b *adkgoBuilder) renameNode(oldID, newID string) {
	node, ok := b.nodes[oldID]
	if !ok {
		return
	}
	if _, exists := b.nodes[newID]; exists {
		// Target name already occupied — don't rename.
		return
	}
	node.ID = newID
	delete(b.nodes, oldID)
	b.nodes[newID] = node

	for i := range b.edges {
		if b.edges[i].From == oldID {
			b.edges[i].From = newID
		}
		if b.edges[i].To == oldID {
			b.edges[i].To = newID
		}
	}
}

// ─── AST helpers ────────────────────────────────────────────────────────────

// extractCompositeLit unwraps an expression to its CompositeLit, if any.
// Handles &T{...} (UnaryExpr) and T{...} (direct).
func extractCompositeLit(expr ast.Expr) *ast.CompositeLit {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		return e
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			if cl, ok := e.X.(*ast.CompositeLit); ok {
				return cl
			}
		}
	}
	return nil
}

// compositeLitTypeName returns the bare type name of a composite literal
// (e.g. "SequentialAgent" from adk.SequentialAgent{} or SequentialAgent{}).
func compositeLitTypeName(cl *ast.CompositeLit) string {
	if cl.Type == nil {
		return ""
	}
	switch t := cl.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		return compositeLitTypeName(&ast.CompositeLit{Type: t.X})
	}
	return ""
}

// isOrchestratorType returns true for agent types that are entry-point candidates.
func isOrchestratorType(name string) bool {
	switch name {
	case "SequentialAgent", "LoopAgent", "ParallelAgent":
		return true
	}
	return false
}

// extractKeyedFields returns a map of field name → value expression
// for a composite literal with keyed fields.
func extractKeyedFields(cl *ast.CompositeLit) map[string]ast.Expr {
	m := make(map[string]ast.Expr)
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		m[key.Name] = kv.Value
	}
	return m
}

// extractSubAgents returns the list of expressions in the SubAgents slice field.
func extractSubAgents(fields map[string]ast.Expr) []ast.Expr {
	expr, ok := fields["SubAgents"]
	if !ok {
		return nil
	}
	cl, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	return cl.Elts
}

// stringFieldValue extracts a string literal value for a keyed field.
// Returns "" if field is missing, not a string literal, or an identifier (we return the ident name).
func stringFieldValue(fields map[string]ast.Expr, key string) string {
	expr, ok := fields[key]
	if !ok {
		return ""
	}
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			// Strip surrounding quotes.
			s := e.Value
			if len(s) >= 2 && s[0] == '"' {
				return s[1 : len(s)-1]
			}
			if len(s) >= 2 && s[0] == '`' {
				return s[1 : len(s)-1]
			}
			return s
		}
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// intFieldValue extracts an integer literal value for a keyed field.
// Returns nil if field is missing or not an integer literal.
func intFieldValue(fields map[string]ast.Expr, key string) *int {
	expr, ok := fields[key]
	if !ok {
		return nil
	}
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return nil
	}
	var n int
	for _, ch := range lit.Value {
		if ch < '0' || ch > '9' {
			return nil
		}
		n = n*10 + int(ch-'0')
	}
	return &n
}

// isToolConstructorCall reports whether call is `<pkg>tool.New(...)`
// — the conventional ADK-Go pattern for wrapping a resource as a
// Tool (agenttool.New, functiontool.New, etc.). Used by
// processToolElement so inline tool-constructor calls in a Tools
// slice unwrap to the wrapped argument rather than emitting "New"
// as the tool name.
//
// Match rule: Fun is a SelectorExpr where the Sel name is "New" and
// the package identifier ends with "tool" (case-insensitive). Catches
// the four established ADK-Go constructors (functiontool, agenttool,
// langchaintool, mcptool) plus future "*tool" packages without
// hard-coding each.
func isToolConstructorCall(call *ast.CallExpr) bool {
	// Unwrap generic instantiation FIRST: for
	// `functiontool.New[TArgs, TResults](...)`, Go's AST stores
	// call.Fun as an *ast.IndexListExpr (or *ast.IndexExpr for a
	// single type arg) WRAPPING the SelectorExpr, not the other way
	// around. Asserting *ast.SelectorExpr on the raw call.Fun would
	// fail for every generic form and silently drop the tool from
	// the graph. Mirrors the unwrap order in extractFuncToolName.
	fun := call.Fun
	if idx, ok := fun.(*ast.IndexListExpr); ok {
		fun = idx.X
	} else if idx, ok := fun.(*ast.IndexExpr); ok {
		fun = idx.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "New" {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return strings.HasSuffix(strings.ToLower(pkgIdent.Name), "tool")
}

// extractIdentOrSelectorName extracts an identifier or selector name from an expression.
func extractIdentOrSelectorName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.CallExpr:
		return extractIdentOrSelectorName(e.Fun)
	case *ast.UnaryExpr:
		return extractIdentOrSelectorName(e.X)
	}
	return ""
}

// ─── String helpers ──────────────────────────────────────────────────────────

// toSnakeCase converts a camelCase or PascalCase string to snake_case.
func toSnakeCase(s string) string {
	if s == "" {
		return ""
	}
	var out strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(r + 32) // toLower
		} else if r == ' ' || r == '-' {
			out.WriteByte('_')
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// inferToolCategory guesses the category of a tool from its identifier name.
// Keyword priority: browser > mcp > code > api (default).
func inferToolCategory(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "browser") ||
		strings.Contains(lower, "click") ||
		strings.Contains(lower, "scrape") ||
		strings.Contains(lower, "selenium") ||
		strings.Contains(lower, "puppeteer"):
		return "browser"
	case strings.Contains(lower, "mcp"):
		return "mcp"
	case strings.Contains(lower, "code") ||
		strings.Contains(lower, "exec") ||
		strings.Contains(lower, "shell") ||
		strings.Contains(lower, "eval"):
		return "code"
	case strings.Contains(lower, "fetch") ||
		strings.Contains(lower, "api") ||
		strings.Contains(lower, "http") ||
		strings.Contains(lower, "rest"):
		return "api"
	default:
		return "api"
	}
}

// ─── go/types second-pass ───────────────────────────────────────────────────

// ParseFile parses a single .go file by path.
// If enableTypes is true, a go/types second-pass is attempted first to enrich
// tool category inference using the TArgs type of functiontool.New[TArgs, TResults].
// On any error from the types pass, it falls back to reading the file bytes and
// calling Parse (the AST-only path).
func (p *ADKGoParser) ParseFile(path string) (*domain.WorkflowGraph, error) {
	if p.enableTypes {
		graph, err := p.parseWithTypes(path)
		if err == nil {
			return graph, nil
		}
		// Fallback: types pass failed (missing go.sum, network, etc.) — use AST-only.
	}

	data, err := readFileBytes(path)
	if err != nil {
		return nil, fmt.Errorf("adk-go parser: read %q: %w", path, err)
	}
	// Thread the real path through to the FileSet so Pos.File matches the
	// caller's input. Codex iter2 P1: hardcoding "input.go" here broke
	// --since path matching and LSP file attribution for multi-file inputs.
	return p.parseWithFilename(data, path)
}

// readFileBytes reads a file's contents; separated so it can be swapped in tests.
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// parseWithTypes loads a Go file using go/packages (with type information) and
// performs the AST-only parse, then enriches tool nodes using types.Info.Instances
// to resolve the TArgs type argument of functiontool.New[TArgs, TResults] calls.
// Returns an error if packages.Load fails or produces errors that prevent analysis.
func (p *ADKGoParser) parseWithTypes(path string) (*domain.WorkflowGraph, error) {
	// Resolve to absolute path so packages.Load can locate go.mod.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %q: %w", path, err)
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedDeps,
		Dir:  filepath.Dir(absPath),
		Fset: token.NewFileSet(),
	}

	pkgs, err := packages.Load(cfg, "file="+absPath)
	if err != nil {
		return nil, fmt.Errorf("go/packages load: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("go/packages: no packages loaded for %q", path)
	}

	// Collect any load errors; treat them as fallback triggers.
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return nil, fmt.Errorf("go/packages errors: %v", pkg.Errors[0])
	}

	// Read the source bytes from disk for the AST-only pass.
	fileBytes, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read file for AST pass: %w", err)
	}

	graph, err := p.Parse(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("AST pass: %w", err)
	}

	// Second pass: enrich tool nodes using go/types instance information.
	if pkg.TypesInfo != nil {
		enrichToolsFromTypeInfo(graph, pkg)
	}

	return graph, nil
}

// enrichToolsFromTypeInfo walks pkg.TypesInfo.Instances looking for
// functiontool.New[TArgs, TResults] instantiations.
// For each instantiation, TArgs struct field names are used to re-infer the
// tool category with higher confidence (field names like "Query", "URL", etc.
// are stronger signals than the tool name alone).
func enrichToolsFromTypeInfo(graph *domain.WorkflowGraph, pkg *packages.Package) {
	if pkg.TypesInfo == nil {
		return
	}

	for ident, inst := range pkg.TypesInfo.Instances {
		// We only care about "New" (functiontool.New).
		if ident.Name != "New" {
			continue
		}
		// Verify it is the functiontool package.
		obj := pkg.TypesInfo.Uses[ident]
		if obj == nil {
			continue
		}
		pkgPath := ""
		if fn, ok := obj.(*types.Func); ok {
			if fn.Pkg() != nil {
				pkgPath = fn.Pkg().Path()
			}
		}
		if !strings.HasSuffix(pkgPath, "functiontool") {
			continue
		}

		// inst.TypeArgs contains [TArgs, TResults].
		if inst.TypeArgs == nil || inst.TypeArgs.Len() < 1 {
			continue
		}

		// TArgs is the first type argument.
		tArgs := inst.TypeArgs.At(0)
		category := categoryFromType(tArgs)
		if category == "" {
			continue
		}

		// Apply the enriched category to matching tool nodes.
		// We identify tools created near this instantiation by checking all
		// tool nodes whose existing category is the default ("api") and whose
		// name/fields match the TArgs signals.
		applyEnrichedCategory(graph, tArgs, category)
	}
}

// categoryFromType infers a tool category from a Go type.
// For struct types, it inspects field names; for named types, it uses the type name.
func categoryFromType(t types.Type) string {
	// Dereference pointers.
	for {
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		} else {
			break
		}
	}

	// Collect names to inspect: type name + struct field names.
	var names []string

	switch tt := t.(type) {
	case *types.Named:
		names = append(names, tt.Obj().Name())
		if st, ok := tt.Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				names = append(names, st.Field(i).Name())
			}
		}
	case *types.Struct:
		for i := 0; i < tt.NumFields(); i++ {
			names = append(names, tt.Field(i).Name())
		}
	}

	return inferToolCategoryFromNames(names)
}

// inferToolCategoryFromNames applies category heuristics over a list of names
// (type name + field names) combined into a single lower-case string.
func inferToolCategoryFromNames(names []string) string {
	combined := strings.ToLower(strings.Join(names, " "))
	switch {
	case strings.Contains(combined, "browser") ||
		strings.Contains(combined, "click") ||
		strings.Contains(combined, "scrape") ||
		strings.Contains(combined, "url") ||
		strings.Contains(combined, "selenium") ||
		strings.Contains(combined, "puppeteer"):
		return "browser"
	case strings.Contains(combined, "mcp"):
		return "mcp"
	case strings.Contains(combined, "code") ||
		strings.Contains(combined, "exec") ||
		strings.Contains(combined, "shell") ||
		strings.Contains(combined, "eval"):
		return "code"
	case strings.Contains(combined, "fetch") ||
		strings.Contains(combined, "api") ||
		strings.Contains(combined, "http") ||
		strings.Contains(combined, "rest"):
		return "api"
	default:
		return ""
	}
}

// applyEnrichedCategory updates the "category" Config field of tool nodes
// that match the given TArgs type. Matching is done by comparing the
// category inferred from TArgs against the struct's name-based hints.
// Only nodes whose current category is the fallback "api" are updated
// (to avoid overriding more specific categories already set from tool name).
func applyEnrichedCategory(graph *domain.WorkflowGraph, tArgs types.Type, category string) {
	// Derive a hint name from the TArgs type (e.g. "browserArgs" → "browser").
	typeName := ""
	if named, ok := tArgs.(*types.Named); ok {
		typeName = strings.ToLower(named.Obj().Name())
	}
	if typeName == "" {
		return
	}

	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeTool {
			continue
		}
		existing, _ := node.Config["category"].(string)
		// Only enrich nodes where the type name is a substring of the tool node ID
		// or vice-versa, ensuring we target the right tool.
		nodeIDLower := strings.ToLower(node.ID)
		if strings.Contains(typeName, nodeIDLower) ||
			strings.Contains(nodeIDLower, strings.TrimSuffix(typeName, "args")) {
			// Update if the types pass gives a more specific (non-api) category,
			// or if the current category is the default.
			if existing == "api" || existing == "" {
				node.Config["category"] = category
			}
		}
	}
}
