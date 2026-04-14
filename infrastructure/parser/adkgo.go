package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// ADKGoParser parses WorkflowGraph from ADK-Go source code using Go AST analysis.
// Only standard library packages (go/parser, go/ast, go/token) are used.
type ADKGoParser struct{}

// NewADKGoParser returns a ready-to-use ADKGoParser.
func NewADKGoParser() *ADKGoParser {
	return &ADKGoParser{}
}

// SupportedFormat implements application.WorkflowParser.
func (p *ADKGoParser) SupportedFormat() string {
	return "adk-go"
}

// Parse analyzes ADK-Go source bytes and constructs a WorkflowGraph.
// It recognises SequentialAgent, LoopAgent, ParallelAgent, and LlmAgent composite literals.
func (p *ADKGoParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "input.go", input, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("adk-go parser: parse Go source: %w", err)
	}

	b := &adkgoBuilder{
		fset:     fset,
		file:     file,
		nodes:    make(map[string]*domain.Node),
		counter:  0,
		varDecls: make(map[string]*ast.CompositeLit),
	}

	// Pre-scan package-level var declarations so identifier references can be resolved.
	b.collectVarDecls()

	// Find entry candidates: top-level vars/assignments containing orchestrator agents.
	entryCandidates := b.findEntryCandidates()

	if len(entryCandidates) == 0 {
		// No recognized agent found — return empty but valid graph.
		return &domain.WorkflowGraph{
			Nodes: make(map[string]*domain.Node),
			Edges: []domain.Edge{},
		}, nil
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
	fset     *token.FileSet
	file     *ast.File
	nodes    map[string]*domain.Node
	edges    []domain.Edge
	counter  int
	varDecls map[string]*ast.CompositeLit // package-level var name -> composite literal
}

// collectVarDecls pre-scans the file for package-level var declarations of agent composite literals.
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
				if i < len(vs.Values) {
					if cl := extractCompositeLit(vs.Values[i]); cl != nil {
						b.varDecls[name.Name] = cl
					}
				}
			}
		}
	}
}

// findEntryCandidates walks the AST to find all top-level orchestrator agents
// (SequentialAgent, LoopAgent, ParallelAgent) and processes them into the graph.
// Returns a list of node IDs in order of discovery.
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
				for idx := range vs.Names {
					if idx >= len(vs.Values) {
						continue
					}
					cl := extractCompositeLit(vs.Values[idx])
					if cl == nil {
						continue
					}
					typeName := compositeLitTypeName(cl)
					if isOrchestratorType(typeName) {
						nodeID := b.processAgentLit(cl, nil)
						if nodeID != "" {
							candidates = append(candidates, nodeID)
						}
					}
				}
			}

		case *ast.FuncDecl:
			// Functions: look for local assignments like `myAgent := &SequentialAgent{...}`.
			if d.Body == nil {
				continue
			}
			ast.Inspect(d.Body, func(n ast.Node) bool {
				switch stmt := n.(type) {
				case *ast.AssignStmt:
					for _, rhs := range stmt.Rhs {
						cl := extractCompositeLit(rhs)
						if cl == nil {
							continue
						}
						typeName := compositeLitTypeName(cl)
						if !isOrchestratorType(typeName) {
							continue
						}
						nodeID := b.processAgentLit(cl, nil)
						if nodeID != "" {
							candidates = append(candidates, nodeID)
						}
					}
				}
				return true
			})
		}
	}

	return candidates
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

	switch typeName {
	case "LlmAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeLLM,
			Config: make(map[string]any),
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
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeControl,
			Config: make(map[string]any),
		}
		b.nodes[nodeID] = node
		b.processSubAgentsSequential(fields, nodeID)

	case "LoopAgent":
		node := &domain.Node{
			ID:     nodeID,
			Name:   name,
			Type:   domain.NodeTypeControl,
			Config: make(map[string]any),
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
			Type:   domain.NodeTypeControl,
			Config: make(map[string]any),
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
func (b *adkgoBuilder) processToolElement(expr ast.Expr) string {
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
// Known keywords: browser, code, api (default).
func inferToolCategory(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "browser"):
		return "browser"
	case strings.Contains(lower, "code") || strings.Contains(lower, "exec"):
		return "code"
	default:
		return "api"
	}
}
