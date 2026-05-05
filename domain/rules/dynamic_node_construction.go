package rules

import (
	"fmt"
	"regexp"

	"github.com/hatyibei/shingan/domain"
)

// DynamicNodeConstruction flags Node.Config values that contain dynamic-code
// construction patterns such as `eval(`, `exec(`, `Function(`, `compile(`,
// `__import__(`, `getattr(`, `setattr(`. These constructs let workflow
// authors generate logic at runtime, which defeats static analysis and almost
// always opens an RCE attack surface.
//
// Tier: Local (ADR-007) — recursive scan of a single node's Config fits the
// 1-walk dispatcher (OnAny). ConfidenceReason varies per pattern hit:
//   - Critical (eval / exec / Function): exact_static_match, confidence 0.95
//   - Warning  (compile / __import__):  exact_static_match, confidence 0.85
//   - Info     (getattr / setattr):     heuristic_pattern,  confidence 0.6
//
// Detection strategy mirrors secret_exposure_scanner:
//  1. Walk a curated subset of Config keys ({body, fn, handler, callback,
//     code, factory, builder}) recursively; other keys (e.g. `description`)
//     are skipped to keep noise low.
//  2. Strip env-var / template placeholders (${VAR}, {{handler}}) before
//     scanning so a value that's purely a placeholder reference doesn't
//     trip the rule. Mixed values (`eval(${PAYLOAD})`) still fire because
//     `eval(` survives placeholder removal.
//  3. Match against a ranked list of patterns; the highest-severity match
//     wins (Critical > Warning > Info) per (node, key) pair so a single
//     value like `getattr(obj, 'cmd')(eval(payload))` collapses into one
//     Critical finding.
//
// The sibling Path rule eval_missing covers the *structural* attack surface
// (LLM → code-execution Tool reachability); this rule covers the
// *string-literal* attack surface inside Config values themselves.
type DynamicNodeConstruction struct{}

// NewDynamicNodeConstruction returns a ready-to-use DynamicNodeConstruction.
func NewDynamicNodeConstruction() *DynamicNodeConstruction {
	return &DynamicNodeConstruction{}
}

// Name returns the unique rule identifier.
func (d *DynamicNodeConstruction) Name() string {
	return "dynamic_node_construction"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (d *DynamicNodeConstruction) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     d.Name(),
		Severity: domain.Critical,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. Dynamic code construction can hide on
// any node type (Tool nodes are most common but Output / Human / Loop nodes
// also occasionally carry handler bodies), so we register OnAny rather than
// narrowing to a specific NodeType.
func (d *DynamicNodeConstruction) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnAny: func(c *domain.RuleContext, n *domain.Node) {
			scanNodeForDynamicConstruction(c, n)
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (d *DynamicNodeConstruction) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	ctx := domain.NewRuleContext(graph, d.Name())
	for _, node := range graph.Nodes {
		scanNodeForDynamicConstruction(ctx, node)
	}
	return ctx.Findings()
}

// dynamicScanKeys is the set of Config keys that are scanned for dynamic-code
// construction patterns. Other keys (description, model, prompt_template, …)
// are skipped to keep the false-positive rate low. The list errs on the side
// of being narrow; extending it is a one-line patch.
var dynamicScanKeys = map[string]bool{
	"body":     true,
	"fn":       true,
	"handler":  true,
	"callback": true,
	"code":     true,
	"factory":  true,
	"builder":  true,
}

// dynamicPattern associates a category name with a compiled regex and a
// (severity, confidence, reason) tuple.
type dynamicPattern struct {
	name       string
	pattern    *regexp.Regexp
	severity   domain.Severity
	confidence float64
	reason     domain.ConfidenceReason
}

// dynamicPatterns is the ordered list of detected categories. Highest
// severity comes first because the per-key scan keeps the strongest match.
//
// `\s*` between the function name and the opening paren tolerates
// whitespace-permissive code styles ("eval ( payload )").
var dynamicPatterns = []dynamicPattern{
	{
		name:       "eval_call",
		pattern:    regexp.MustCompile(`\beval\s*\(`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "exec_call",
		pattern:    regexp.MustCompile(`\bexec\s*\(`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		// Word-boundary matches both the bare constructor `Function(` and
		// the `new Function(` JS form. The capital F prevents matching
		// generic identifiers like `lambdaFunction(`.
		name:       "function_constructor",
		pattern:    regexp.MustCompile(`\bFunction\s*\(`),
		severity:   domain.Critical,
		confidence: 0.95,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "compile_call",
		pattern:    regexp.MustCompile(`\bcompile\s*\(`),
		severity:   domain.Warning,
		confidence: 0.85,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "dunder_import",
		pattern:    regexp.MustCompile(`__import__\s*\(`),
		severity:   domain.Warning,
		confidence: 0.85,
		reason:     domain.ReasonExactStaticMatch,
	},
	{
		name:       "getattr_call",
		pattern:    regexp.MustCompile(`\bgetattr\s*\(`),
		severity:   domain.Info,
		confidence: 0.6,
		reason:     domain.ReasonHeuristicPattern,
	},
	{
		name:       "setattr_call",
		pattern:    regexp.MustCompile(`\bsetattr\s*\(`),
		severity:   domain.Info,
		confidence: 0.6,
		reason:     domain.ReasonHeuristicPattern,
	},
}

// scanNodeForDynamicConstruction is the per-node entry point shared by both
// the visitor listener and the legacy Analyze fallback.
func scanNodeForDynamicConstruction(ctx *domain.RuleContext, node *domain.Node) {
	if node == nil {
		return
	}
	for key, val := range node.Config {
		// Only scan a curated subset of keys; the recursive descent below
		// re-anchors the scan against the same key, so nested map fields
		// inherit the parent's "is this a scanned key?" decision.
		if !dynamicScanKeys[key] {
			continue
		}
		scanDynamicValue(ctx, node, key, val)
	}
}

// scanDynamicValue dispatches to the appropriate handler based on the
// runtime type of val and emits at most one Finding per top-level (node,
// key) pair.
func scanDynamicValue(ctx *domain.RuleContext, node *domain.Node, key string, val any) {
	hits := collectDynamicHits(val)
	if len(hits) == 0 {
		return
	}
	// Pick the highest-severity hit so a value containing both eval and
	// getattr collapses into a single Critical finding.
	best := hits[0]
	for _, h := range hits[1:] {
		if h.severity > best.severity {
			best = h
		}
	}

	ctx.Report(domain.Finding{
		RuleName: "dynamic_node_construction",
		Severity: best.severity,
		NodeID:   node.ID,
		Message: fmt.Sprintf(
			"node %q config[%q] contains a dynamic-code-construction pattern (%s)",
			node.ID, key, best.name,
		),
		Suggestion:       "Dynamic code construction detected (`eval`/`exec`/`Function`). Static analysis cannot reason about generated code paths. Refactor to explicit dispatch or use a sandboxed evaluator with allowlist.",
		Confidence:       best.confidence,
		ConfidenceReason: best.reason,
	})
}

// dynamicHit is a single pattern match that survived placeholder stripping.
type dynamicHit struct {
	name       string
	severity   domain.Severity
	confidence float64
	reason     domain.ConfidenceReason
}

// collectDynamicHits walks val recursively, applying the pattern set to
// every leaf string. Placeholder-only strings are skipped (mirrors
// scanString's logic in secret_exposure_scanner via hasActualSecret).
func collectDynamicHits(val any) []dynamicHit {
	switch v := val.(type) {
	case string:
		return collectStringHits(v)
	case map[string]any:
		var all []dynamicHit
		for _, sub := range v {
			all = append(all, collectDynamicHits(sub)...)
		}
		return all
	case []any:
		var all []dynamicHit
		for _, item := range v {
			all = append(all, collectDynamicHits(item)...)
		}
		return all
	}
	return nil
}

// collectStringHits applies every pattern to s, returning each category that
// matches. Placeholder-only strings (purely ${VAR} / {{var}} references) are
// skipped to avoid flagging legitimate runtime injection of trusted code.
//
// Mixed strings (`eval(${PAYLOAD})`) still fire because `eval(` survives
// placeholder removal — the same trick secret_exposure uses for
// "sk-...${SUFFIX}".
func collectStringHits(s string) []dynamicHit {
	if s == "" {
		return nil
	}
	// Placeholder-only? Use the existing secret_exposure helper: if removing
	// placeholders leaves a string with no actual pattern, we treat the
	// value as a safe runtime reference and bail.
	if placeholderPattern.MatchString(s) && !hasActualDynamicPattern(s) {
		return nil
	}
	var hits []dynamicHit
	for _, p := range dynamicPatterns {
		if p.pattern.MatchString(s) {
			hits = append(hits, dynamicHit{
				name:       p.name,
				severity:   p.severity,
				confidence: p.confidence,
				reason:     p.reason,
			})
		}
	}
	return hits
}

// hasActualDynamicPattern reports whether s contains a dynamic-code pattern
// even after every placeholder reference has been stripped. Mirrors the
// secret_exposure_scanner.hasActualSecret helper.
func hasActualDynamicPattern(s string) bool {
	stripped := placeholderPattern.ReplaceAllString(s, "")
	for _, p := range dynamicPatterns {
		if p.pattern.MatchString(stripped) {
			return true
		}
	}
	return false
}

func init() {
	registerBuiltin(NewDynamicNodeConstruction())
}
