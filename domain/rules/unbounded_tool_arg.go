package rules

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// UnboundedToolArgChecker flags Tool nodes whose argument schema contains
// fields without an upper bound (string `maxLength`, array `maxItems`,
// number `maximum`). Tool inputs without bounds let an LLM-controlled or
// attacker-controlled caller send a monster payload that blows up token
// usage, hits provider rate limits, or causes the tool runtime to OOM.
//
// Tier: Local (ADR-007) — decision per node, fits the 1-walk dispatcher.
//
// Detection scope: only the Tool node's `args_schema` / `parameters` /
// `input_schema` Config keys are inspected (not generic Config values, to
// avoid duplication with secret_exposure_scanner / temperature_misuse).
// Inside that schema we recurse through JSON-schema `properties` and
// `items` so nested object/array fields are reached.
//
// Severity table (per offending field):
//
//	string × maxLength missing               → Warning, 0.7, heuristic_pattern
//	string × maxLength > 100_000             → Info,    0.5, heuristic_pattern
//	array  × maxItems missing                → Warning, 0.7, heuristic_pattern
//	number × maximum missing                 → Info,    0.4, heuristic_pattern
//
// Findings are capped at maxFindingsPerNode so a 50-field schema doesn't
// drown the report.
type UnboundedToolArgChecker struct{}

// NewUnboundedToolArgChecker returns a ready-to-use checker.
func NewUnboundedToolArgChecker() *UnboundedToolArgChecker {
	return &UnboundedToolArgChecker{}
}

// Name returns the unique rule identifier.
func (u *UnboundedToolArgChecker) Name() string {
	return "unbounded_tool_arg"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (u *UnboundedToolArgChecker) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     u.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. Only Tool nodes are inspected.
func (u *UnboundedToolArgChecker) Listener(ctx *domain.RuleContext) domain.Listener {
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeTool: func(c *domain.RuleContext, n *domain.Node) {
				for _, f := range evaluateUnboundedToolArg(n) {
					c.Report(f)
				}
			},
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (u *UnboundedToolArgChecker) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	var findings []domain.Finding
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeTool {
			continue
		}
		findings = append(findings, evaluateUnboundedToolArg(node)...)
	}
	return findings
}

// schemaConfigKeys is the set of Config keys this rule considers a JSON
// schema describing tool arguments. Any one of them being a map[string]any
// triggers the recursive scan.
var schemaConfigKeys = []string{"args_schema", "parameters", "input_schema"}

// maxStringLengthThreshold is the threshold above which a declared
// maxLength is treated as "effectively unbounded" (Info severity). Set to
// 100K characters — well beyond any reasonable tool argument and into the
// "attacker-controlled blow-up" territory.
const maxStringLengthThreshold = 100_000

// maxFindingsPerNode caps the number of unbounded fields reported on a
// single Tool node so a 50-field schema does not drown the report.
const maxFindingsPerNode = 5

// evaluateUnboundedToolArg returns one Finding per offending field on n.
// Schema-less Tool nodes (no args_schema / parameters / input_schema) are
// silently skipped — they are out of scope for this rule.
func evaluateUnboundedToolArg(n *domain.Node) []domain.Finding {
	if n == nil || n.Config == nil {
		return nil
	}
	var findings []domain.Finding
	for _, key := range schemaConfigKeys {
		raw, ok := n.Config[key]
		if !ok {
			continue
		}
		schema, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		walkSchema(n, key, schema, &findings)
		if len(findings) >= maxFindingsPerNode {
			findings = findings[:maxFindingsPerNode]
			return findings
		}
	}
	return findings
}

// walkSchema descends into a JSON-schema-shaped map and emits findings for
// each unbounded primitive field. The path argument records the dotted
// JSON-pointer-ish path used in messages.
//
// Per Codex iter11 P2: tools that take a single primitive or top-level
// array argument (`args_schema: {"type":"string"}` /
// `{"type":"array","items":...}`) used to be silently missed because we
// only classified entries under `properties`. We now classify the root
// schema as well so primitive / array roots fire.
func walkSchema(n *domain.Node, path string, schema map[string]any, findings *[]domain.Finding) {
	if schema == nil {
		return
	}

	// Classify the root schema itself when it's a primitive (string /
	// number) or an array. Object roots are handled via the per-property
	// loop below; classifying the object root would double-count.
	if t, _ := schema["type"].(string); t != "" && t != "object" {
		classifyField(n, path, schema, findings)
		if len(*findings) >= maxFindingsPerNode {
			return
		}
	}

	// Recurse into properties (object schema).
	if props, ok := schema["properties"].(map[string]any); ok {
		for fieldName, fieldRaw := range props {
			fieldSchema, ok := fieldRaw.(map[string]any)
			if !ok {
				continue
			}
			fieldPath := path + "." + fieldName
			classifyField(n, fieldPath, fieldSchema, findings)
			if len(*findings) >= maxFindingsPerNode {
				return
			}
			// Recurse — nested object / array of objects.
			walkSchema(n, fieldPath, fieldSchema, findings)
		}
	}

	// Recurse into items (array schema).
	if items, ok := schema["items"].(map[string]any); ok {
		walkSchema(n, path+".items", items, findings)
	}
}

// classifyField emits at most one Finding for a single JSON-schema field
// node. The schema map represents the field itself (e.g. `{"type": "string",
// "maxLength": 1000}`).
func classifyField(n *domain.Node, fieldPath string, schema map[string]any, findings *[]domain.Finding) {
	if len(*findings) >= maxFindingsPerNode {
		return
	}
	t, _ := schema["type"].(string)
	switch t {
	case "string":
		if !hasNumericKey(schema, "maxLength") {
			*findings = append(*findings, domain.Finding{
				RuleName:         "unbounded_tool_arg",
				Severity:         domain.Warning,
				NodeID:           n.ID,
				Message:          fmt.Sprintf("Tool node %q schema field %q (string) has no maxLength — caller can blow up token usage with arbitrary input", n.ID, fieldPath),
				Suggestion:       fmt.Sprintf("Add `maxLength` to schema field %q (e.g. 4000) so attacker-controlled or LLM-generated payloads cannot trigger token / API failures.", fieldPath),
				Confidence:       0.7,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			})
			return
		}
		if v, ok := numericKey(schema, "maxLength"); ok && v > maxStringLengthThreshold {
			*findings = append(*findings, domain.Finding{
				RuleName:         "unbounded_tool_arg",
				Severity:         domain.Info,
				NodeID:           n.ID,
				Message:          fmt.Sprintf("Tool node %q schema field %q (string) has maxLength=%.0f which exceeds the %d-character soft limit — effectively unbounded", n.ID, fieldPath, v, maxStringLengthThreshold),
				Suggestion:       fmt.Sprintf("Reduce `maxLength` on schema field %q to a realistic upper bound (e.g. 4000-32000 characters).", fieldPath),
				Confidence:       0.5,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			})
		}
	case "array":
		if !hasNumericKey(schema, "maxItems") {
			*findings = append(*findings, domain.Finding{
				RuleName:         "unbounded_tool_arg",
				Severity:         domain.Warning,
				NodeID:           n.ID,
				Message:          fmt.Sprintf("Tool node %q schema field %q (array) has no maxItems — caller can submit an unbounded list", n.ID, fieldPath),
				Suggestion:       fmt.Sprintf("Add `maxItems` to schema field %q so the tool cannot be hammered with unboundedly large arrays.", fieldPath),
				Confidence:       0.7,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			})
		}
	case "number", "integer":
		if !hasNumericKey(schema, "maximum") {
			*findings = append(*findings, domain.Finding{
				RuleName:         "unbounded_tool_arg",
				Severity:         domain.Info,
				NodeID:           n.ID,
				Message:          fmt.Sprintf("Tool node %q schema field %q (%s) has no maximum — consider bounding numeric inputs", n.ID, fieldPath, t),
				Suggestion:       fmt.Sprintf("Add `maximum` to schema field %q to reject unrealistically large numeric arguments.", fieldPath),
				Confidence:       0.4,
				ConfidenceReason: domain.ReasonHeuristicPattern,
			})
		}
	}
}

// hasNumericKey reports whether key is present on schema and decodes to a
// number (float64 / int / int64). Non-numeric values (string / bool / nil)
// are treated as missing so a typo never fakes a bound.
func hasNumericKey(schema map[string]any, key string) bool {
	_, ok := numericKey(schema, key)
	return ok
}

// numericKey returns the numeric value of schema[key] (float64 / int /
// int64) and whether it was present and numeric.
func numericKey(schema map[string]any, key string) (float64, bool) {
	v, ok := schema[key]
	if !ok {
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

func init() {
	registerBuiltin(NewUnboundedToolArgChecker())
}
