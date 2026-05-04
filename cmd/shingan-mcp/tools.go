package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

// toolDeps bundles the collaborators every tool handler needs. Passing one
// value around keeps handlers pure (no package-level globals) and makes them
// trivial to unit-test (see main_test.go).
type toolDeps struct {
	analyzerFactory *factory.AnalyzerFactory
	parserFactory   *factory.ParserFactory
	orchestrator    *application.AnalysisOrchestrator
}

// ---- Tool argument / result schemas -----------------------------------------

// AnalyzeGraphArgs is the input schema of shingan_analyze_graph.
type AnalyzeGraphArgs struct {
	GraphJSON string `json:"graph_json" jsonschema:"WorkflowGraph serialized as JSON (nodes as array, edges, entry_node_id)"`
}

// AnalyzeFileArgs is the input schema of shingan_analyze_file.
type AnalyzeFileArgs struct {
	Path      string `json:"path" jsonschema:"absolute or working-directory-relative path to the workflow file or adk-go directory"`
	Framework string `json:"framework" jsonschema:"one of: json, adk-go, samurai"`
}

// ExplainRuleArgs is the input schema of shingan_explain_rule.
type ExplainRuleArgs struct {
	RuleName string `json:"rule_name" jsonschema:"one of the 11 built-in rule names (e.g. loop_guard, cycle_detection)"`
}

// SuggestModelArgs is the input schema of shingan_suggest_model.
type SuggestModelArgs struct {
	NodeDescription    string `json:"node_description" jsonschema:"natural-language description of what the node does"`
	InputTokenEstimate int    `json:"input_token_estimate" jsonschema:"average expected input token count per call"`
}

// FindingDTO is the wire form of a finding. Mirrors domain.Finding but
// serialises Severity as its lowercase string ("critical" / "warning" / "info")
// so downstream JSON consumers don't have to learn the Go enum values.
//
// SourceFile is populated when the finding comes from a directory analysis
// (ADR-012): per-file independent graphs let the MCP client know which
// file in a multi-file workflow the issue belongs to. Empty for single-
// file inputs.
type FindingDTO struct {
	RuleName   string  `json:"rule_name"`
	Severity   string  `json:"severity"`
	NodeID     string  `json:"node_id,omitempty"`
	Message    string  `json:"message"`
	Suggestion string  `json:"suggestion,omitempty"`
	Confidence float64 `json:"confidence"`
	SourceFile string  `json:"source_file,omitempty"`
}

// FindingList is the canonical output wrapper for both analyze tools.
type FindingList struct {
	Findings []FindingDTO `json:"findings"`
	Count    int          `json:"count"`
}

// ModelRecommendation is the output of shingan_suggest_model.
type ModelRecommendation struct {
	Model                   string  `json:"model"`
	Rationale               string  `json:"rationale"`
	EstimatedCostPerCallUSD float64 `json:"estimated_cost_per_call_usd"`
}

// RuleExplanation is the structured output of shingan_explain_rule.
// We wrap the explanation text in an object (rather than returning a bare
// string) because the MCP SDK requires tool output schemas to be
// JSON-schema type "object".
type RuleExplanation struct {
	RuleName    string `json:"rule_name"`
	Explanation string `json:"explanation"`
}

// ---- Registration ------------------------------------------------------------

// registerTools attaches all four Shingan tools to the given MCP server.
// Extracted so main_test.go can exercise the same wiring in-memory.
func registerTools(server *mcp.Server, deps *toolDeps) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "shingan_analyze_graph",
		Description: "Run all 11 Shingan analysis rules against an in-memory " +
			"WorkflowGraph JSON. Returns findings sorted by severity desc, " +
			"confidence desc, rule name asc.",
	}, deps.analyzeGraph)

	mcp.AddTool(server, &mcp.Tool{
		Name: "shingan_analyze_file",
		Description: "Parse a workflow file (or adk-go directory) from disk " +
			"and run all 11 Shingan analysis rules. Supported frameworks: " +
			"json, adk-go, samurai.",
	}, deps.analyzeFile)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "shingan_explain_rule",
		Description: "Return a detailed explanation of one of the 11 built-in Shingan rules.",
	}, deps.explainRule)

	mcp.AddTool(server, &mcp.Tool{
		Name: "shingan_suggest_model",
		Description: "Heuristic LLM model recommendation given a node " +
			"description and expected input token count.",
	}, deps.suggestModel)
}

// ---- Tool handlers -----------------------------------------------------------

// recoverHandler turns a panic inside a tool handler into a clean MCP error
// response so a single misbehaving rule cannot bring down the entire server
// (the client would otherwise see EOF on stdio with no diagnostic).
//
// Usage:
//
//	defer recoverHandler("analyzeGraph", &res)
//
// On recover it overwrites `*res` with an errorResult that includes the
// originating tool name and panic value. The structured-output return value
// is left as the caller's zero value, which is what MCP clients expect when
// IsError=true.
func recoverHandler(tool string, res **mcp.CallToolResult) {
	if r := recover(); r != nil {
		*res = errorResult(fmt.Sprintf("internal error in %s: %v", tool, r))
	}
}

// analyzeGraph implements shingan_analyze_graph.
func (d *toolDeps) analyzeGraph(
	_ context.Context,
	_ *mcp.CallToolRequest,
	args AnalyzeGraphArgs,
) (res *mcp.CallToolResult, _ FindingList, _ error) {
	defer recoverHandler("shingan_analyze_graph", &res)

	if strings.TrimSpace(args.GraphJSON) == "" {
		return errorResult("graph_json is required"), FindingList{}, nil
	}

	var graph domain.WorkflowGraph
	if err := json.Unmarshal([]byte(args.GraphJSON), &graph); err != nil {
		return errorResult(fmt.Sprintf("parse graph_json: %v", err)), FindingList{}, nil
	}

	return d.runAnalysis(&graph)
}

// analyzeFile implements shingan_analyze_file.
func (d *toolDeps) analyzeFile(
	_ context.Context,
	_ *mcp.CallToolRequest,
	args AnalyzeFileArgs,
) (res *mcp.CallToolResult, _ FindingList, _ error) {
	defer recoverHandler("shingan_analyze_file", &res)

	if args.Path == "" {
		return errorResult("path is required"), FindingList{}, nil
	}
	framework := args.Framework
	if framework == "" {
		framework = "json"
	}

	parser, err := d.parserFactory.Create(framework)
	if err != nil {
		return errorResult(err.Error()), FindingList{}, nil
	}

	inputs, err := loadGraphsAsMulti(args.Path, framework, parser)
	if err != nil {
		return errorResult(fmt.Sprintf("load graph: %v", err)), FindingList{}, nil
	}

	return d.runAnalysisMulti(inputs)
}

// explainRule implements shingan_explain_rule.
func (d *toolDeps) explainRule(
	_ context.Context,
	_ *mcp.CallToolRequest,
	args ExplainRuleArgs,
) (res *mcp.CallToolResult, _ RuleExplanation, _ error) {
	defer recoverHandler("shingan_explain_rule", &res)

	text, ok := ruleExplanations[args.RuleName]
	if !ok {
		return errorResult(fmt.Sprintf(
			"unknown rule %q; try one of: %s",
			args.RuleName, strings.Join(knownRuleNames(), ", "),
		)), RuleExplanation{}, nil
	}
	out := RuleExplanation{RuleName: args.RuleName, Explanation: text}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, out, nil
}

// suggestModel implements shingan_suggest_model.
func (d *toolDeps) suggestModel(
	_ context.Context,
	_ *mcp.CallToolRequest,
	args SuggestModelArgs,
) (res *mcp.CallToolResult, _ ModelRecommendation, _ error) {
	defer recoverHandler("shingan_suggest_model", &res)

	rec := recommendModel(args.NodeDescription, args.InputTokenEstimate)
	body, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("marshal recommendation: %v", err)), ModelRecommendation{}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, rec, nil
}

// ---- Helpers -----------------------------------------------------------------

// runAnalysis is a thin wrapper that runs runAnalysisMulti on a single
// in-memory graph (shingan_analyze_graph tool path, no source file).
func (d *toolDeps) runAnalysis(graph *domain.WorkflowGraph) (*mcp.CallToolResult, FindingList, error) {
	return d.runAnalysisMulti([]application.GraphWithSource{{Graph: graph}})
}

// runAnalysisMulti drives the orchestrator over a slice of (graph,
// sourceFile) pairs and maps the domain findings into the wire DTO. Per
// ADR-012, directory inputs (shingan_analyze_file with a folder path)
// produce one element per file so independent agent definitions are no
// longer merged into a single graph.
func (d *toolDeps) runAnalysisMulti(inputs []application.GraphWithSource) (*mcp.CallToolResult, FindingList, error) {
	rules := d.analyzerFactory.CreateAll()
	findings := d.orchestrator.AnalyzeMulti(inputs, rules)

	out := FindingList{
		Findings: make([]FindingDTO, 0, len(findings)),
		Count:    len(findings),
	}
	for _, f := range findings {
		out.Findings = append(out.Findings, FindingDTO{
			RuleName:   f.RuleName,
			Severity:   f.Severity.String(),
			NodeID:     f.NodeID,
			Message:    f.Message,
			Suggestion: f.Suggestion,
			Confidence: f.Confidence,
			SourceFile: f.SourceFile,
		})
	}

	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("marshal findings: %v", err)), out, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, out, nil
}

// loadGraphsAsMulti reads a workflow from disk and returns one
// (graph, sourceFile) pair per parsed file. Per ADR-012, directory
// inputs (currently only adk-go) produce per-file independent graphs
// rather than a single merged graph, so independent agent definitions
// in different files don't collide and trigger spurious findings.
//
// Single-file inputs return a one-element slice. Errors propagate;
// per-file parse failures inside a directory are skipped silently
// (mirrors CLI resilience).
func loadGraphsAsMulti(path, framework string, p application.WorkflowParser) ([]application.GraphWithSource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	if !info.IsDir() {
		// Prefer ParseFile when the parser implements it: ADK-Go's
		// ParseFile threads the real path through go/types for the
		// second-pass type analysis (functiontool.New[TArgs] generic
		// inference), and LangGraph's ParseFile uses sys.path resolution
		// (Codex iter4 P2).
		var g *domain.WorkflowGraph
		if fp, ok := p.(interface {
			ParseFile(string) (*domain.WorkflowGraph, error)
		}); ok {
			g, err = fp.ParseFile(path)
		} else {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil, fmt.Errorf("read file %q: %w", path, rerr)
			}
			g, err = p.Parse(data)
		}
		if err != nil {
			return nil, err
		}
		return []application.GraphWithSource{{Graph: g, SourceFile: path}}, nil
	}

	if framework != "adk-go" {
		return nil, fmt.Errorf(
			"directory input is only supported for framework=\"adk-go\" (got %q)",
			framework,
		)
	}

	var inputs []application.GraphWithSource
	walkErr := filepath.Walk(path, func(p2 string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() || filepath.Ext(p2) != ".go" {
			return nil
		}
		// Use ParseFile when supported so per-file Pos.File matches the
		// real path (Codex iter4 P2 + ADR-012).
		var g *domain.WorkflowGraph
		var perr error
		if fp, ok := p.(interface {
			ParseFile(string) (*domain.WorkflowGraph, error)
		}); ok {
			g, perr = fp.ParseFile(p2)
		} else {
			data, rerr := os.ReadFile(p2)
			if rerr != nil {
				return nil // skip unreadable files; mirrors CLI behaviour
			}
			g, perr = p.Parse(data)
		}
		if perr != nil {
			return nil // skip unparseable files
		}
		inputs = append(inputs, application.GraphWithSource{Graph: g, SourceFile: p2})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %q: %w", path, walkErr)
	}
	return inputs, nil
}

// errorResult wraps a plain-text error into an MCP CallToolResult that flags
// IsError=true. The structured-output field is zero-valued in this path; the
// client reads the error from .Content[0].Text.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// recommendModel is a deliberately simple heuristic. It beats silence, but is
// not a cost oracle — see docs/mcp-server.md for the decision tree.
func recommendModel(description string, inputTokens int) ModelRecommendation {
	d := strings.ToLower(description)

	// Keyword sets. Kept as literals (not regex) because we want cheap, very
	// obvious classifications — anything fancier belongs in the Shingan rule
	// engine, not in a free-form suggestion tool.
	cheapHints := []string{"分類", "classification", "extract", "抽出", "label", "tag", "summarize short"}
	heavyHints := []string{"reasoning", "推論", "complex", "multi-step", "math proof", "architect"}

	has := func(hints []string) bool {
		for _, h := range hints {
			if strings.Contains(d, h) {
				return true
			}
		}
		return false
	}

	switch {
	case has(heavyHints):
		// Heavy reasoning: recommend a frontier model.
		return ModelRecommendation{
			Model: "claude-3-5-sonnet",
			Rationale: "Description contains reasoning/complex keywords. " +
				"Frontier-tier reasoning is worth the cost delta vs. mini models.",
			EstimatedCostPerCallUSD: estimateCostUSD(3.0, 15.0, inputTokens),
		}
	case has(cheapHints), inputTokens > 0 && inputTokens < 1000:
		// Classification/extraction or short prompts — mini tier is plenty.
		return ModelRecommendation{
			Model: "gpt-4o-mini",
			Rationale: "Short / classification-style workload. A mini-tier model " +
				"typically matches quality within 2-3% at ~20x lower cost.",
			EstimatedCostPerCallUSD: estimateCostUSD(0.15, 0.60, inputTokens),
		}
	default:
		return ModelRecommendation{
			Model: "gpt-4o",
			Rationale: "No strong cheap/heavy signal in the description; " +
				"balanced default. Re-evaluate after observing cost + quality in production.",
			EstimatedCostPerCallUSD: estimateCostUSD(2.5, 10.0, inputTokens),
		}
	}
}

// Cost-estimation tunables. Pulled out as named constants so the heuristic
// is self-documenting and easy to tweak without hunting through arithmetic.
const (
	// defaultInputTokens is assumed when the caller passes a non-positive
	// input_token_estimate — a "modest chat turn" baseline.
	defaultInputTokens = 500
	// outputTokenFloor is the lower bound for the inferred output length.
	// Real LLM responses rarely fall below ~50 tokens once headers + JSON
	// scaffolding are included.
	outputTokenFloor = 50
	// outputToInputRatio approximates response size as ~10% of input. Tool
	// workloads (extraction, classification) tend to be input-heavy.
	outputToInputRatio = 10
	// costRoundingFactor truncates the float to 6 decimal places for stable
	// JSON output (avoids spurious diffs from float jitter).
	costRoundingFactor = 1_000_000
	// tokensPerMillion converts $/1M-token prices to per-token cost.
	tokensPerMillion = 1_000_000.0
)

// estimateCostUSD returns a rough per-call cost using the supplied
// input/output $/1M-token prices and the caller-provided input token
// estimate. See the package-level constants above for the assumptions.
func estimateCostUSD(inputPricePerMillion, outputPricePerMillion float64, inputTokens int) float64 {
	if inputTokens <= 0 {
		inputTokens = defaultInputTokens
	}
	outputTokens := inputTokens / outputToInputRatio
	if outputTokens < outputTokenFloor {
		outputTokens = outputTokenFloor
	}
	cost := (float64(inputTokens)/tokensPerMillion)*inputPricePerMillion +
		(float64(outputTokens)/tokensPerMillion)*outputPricePerMillion
	return float64(int(cost*costRoundingFactor)) / costRoundingFactor
}
