package rules

import (
	"fmt"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// TemperatureMisuseChecker flags LLM nodes that combine an explicit
// `temperature > 0` with a deterministic task signature (structured output,
// extraction, classification, code generation).
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
//
// Detection priority (highest-confidence first):
//  1. Config["structured_output"] == true OR Config["response_format"] ==
//     "json_object" → Warning, confidence 0.9, ReasonExactStaticMatch.
//     Reason: the schema-binding flag itself is a hard deterministic signal.
//  2. Config["task"] (or node.Name keyword) classifies the node:
//     - "classification" with temp > 0.3 → Warning, 0.7, heuristic_pattern
//     - "code_generation" with temp > 0  → Warning, 0.7, heuristic_pattern
//     - "extraction" / "structured_output" task with temp > 0 → Info, 0.5,
//     heuristic_pattern (already covered by signal #1 if the config flag is
//     present; this case fires only when the user expressed intent through
//     `task` alone).
//
// A node with no deterministic signal (creative_writing / unspecified) is
// silently skipped to keep the false-positive rate low.
type TemperatureMisuseChecker struct{}

// NewTemperatureMisuseChecker returns a ready-to-use checker.
func NewTemperatureMisuseChecker() *TemperatureMisuseChecker {
	return &TemperatureMisuseChecker{}
}

// Name returns the unique rule identifier.
func (t *TemperatureMisuseChecker) Name() string {
	return "temperature_misuse"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (t *TemperatureMisuseChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     t.Name(),
		Severity: domain.Warning,
		Fixable:  true,
	}
}

// Listener implements domain.LocalRule. Only LLM nodes are inspected.
func (t *TemperatureMisuseChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLLM: func(c *domain.RuleContext, n *domain.Node) {
				if f, ok := evaluateTemperatureMisuse(n); ok {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (t *TemperatureMisuseChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}
		if f, ok := evaluateTemperatureMisuse(node); ok {
			findings = append(findings, f)
		}
	}
	return findings
}

// evaluateTemperatureMisuse encodes the priority table documented on the
// checker. It returns the Finding plus ok=true when the node should be
// reported, or ok=false otherwise.
func evaluateTemperatureMisuse(node *domain.Node) (domain.Finding, bool) {
	temp, hasTemp := floatConfig(node, "temperature")
	if !hasTemp {
		return domain.Finding{}, false
	}

	// Signal #1: explicit deterministic flags — strongest static evidence.
	if temp > 0 {
		structured, _ := boolConfig(node, "structured_output")
		responseFormat := stringConfig(node, "response_format")
		if structured || responseFormat == "json_object" {
			detail := "structured_output"
			if !structured && responseFormat == "json_object" {
				detail = "response_format=json_object"
			}
			return domain.Finding{
				RuleName: "temperature_misuse",
				Severity: domain.Warning,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"node %q sets temperature=%.2f while %s is enabled — schema-bound output requires a deterministic decode.",
					node.ID, temp, detail,
				),
				Suggestion:       "Set `temperature` to 0 for deterministic tasks. High temperature produces output variability that defeats structured extraction/classification.",
				Confidence:       0.9,
				ConfidenceReason: domain.ReasonExactStaticMatch,
			}, true
		}
	}

	// Signal #2: task / name heuristics.
	task := taskCategory(node)
	switch task {
	case "classification":
		if temp > 0.3 {
			return domain.Finding{
				RuleName: "temperature_misuse",
				Severity: domain.Warning,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"node %q is a classification task but uses temperature=%.2f (>0.3) — class probabilities become unstable across runs.",
					node.ID, temp,
				),
				Suggestion:       "Set `temperature` to 0 for deterministic tasks. High temperature produces output variability that defeats structured extraction/classification.",
				Confidence:       0.7,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			}, true
		}
	case "code_generation":
		if temp > 0 {
			return domain.Finding{
				RuleName: "temperature_misuse",
				Severity: domain.Warning,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"node %q is a code_generation task but uses temperature=%.2f — non-zero temperature causes drift across compilable variants.",
					node.ID, temp,
				),
				Suggestion:       "Set `temperature` to 0 for deterministic tasks. High temperature produces output variability that defeats structured extraction/classification.",
				Confidence:       0.7,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			}, true
		}
	case "extraction", "structured_output":
		if temp > 0 {
			return domain.Finding{
				RuleName: "temperature_misuse",
				Severity: domain.Info,
				NodeID:   node.ID,
				Message: fmt.Sprintf(
					"node %q is an extraction/structured_output task but uses temperature=%.2f — extracted fields may differ between runs.",
					node.ID, temp,
				),
				Suggestion:       "Set `temperature` to 0 for deterministic tasks. High temperature produces output variability that defeats structured extraction/classification.",
				Confidence:       0.5,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			}, true
		}
	}

	return domain.Finding{}, false
}

// taskCategory determines the deterministic-task category of a node by
// looking at Config["task"] first and then falling back to node.Name keyword
// scanning. The returned value is one of:
//
//	"extraction" / "classification" / "structured_output" / "code_generation" / ""
//
// The empty string means "no deterministic signal" and the rule should not
// fire.
func taskCategory(node *domain.Node) string {
	if t := strings.ToLower(stringConfig(node, "task")); t != "" {
		switch t {
		case "extraction", "extract":
			return "extraction"
		case "classification", "classify":
			return "classification"
		case "structured_output", "structured-output", "json", "json_output":
			return "structured_output"
		case "code_generation", "code-gen", "codegen":
			return "code_generation"
		}
		// Unknown task value — fall through to name heuristic.
	}

	name := strings.ToLower(node.Name)
	switch {
	case strings.Contains(name, "extract"):
		return "extraction"
	case strings.Contains(name, "classif"): // classify, classification, classifier
		return "classification"
	case strings.Contains(name, "code_gen"), strings.Contains(name, "codegen"):
		return "code_generation"
	}
	return ""
}

// floatConfig returns Config[key] as a float64 alongside a presence flag.
// Both float64 and int (JSON-encoded numbers may decode as either) are
// accepted; non-numeric values are rejected so a typo never trips the rule.
func floatConfig(node *domain.Node, key string) (float64, bool) {
	if node == nil || node.Config == nil {
		return 0, false
	}
	v, ok := node.Config[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// boolConfig returns Config[key] as a bool alongside a presence flag.
// Returns (false, false) if the key is missing or the value is not a bool.
func boolConfig(node *domain.Node, key string) (bool, bool) {
	if node == nil || node.Config == nil {
		return false, false
	}
	v, ok := node.Config[key]
	if !ok || v == nil {
		return false, false
	}
	b, ok := v.(bool)
	if !ok {
		return false, false
	}
	return b, true
}

func init() {
	registerBuiltin(NewTemperatureMisuseChecker())
}
