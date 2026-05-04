package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hatyibei/shingan/domain"
)

// llmKey is the deduplication key for a LLM node: (model, prompt_template).
type llmKey struct {
	model          string
	promptTemplate string
}

// RedundantLLMDetector identifies LLM nodes that share the same model and
// prompt_template, indicating that their results could be cached or the
// duplicate calls removed.
//
// Tier: Local (ADR-007) — uses OnNode to bucket LLM nodes during the walk
// and OnGraph to emit findings once all duplicates are known.
// ConfidenceReason: ReasonExactStaticMatch (deterministic key comparison).
//
// Severity rules:
//   - 2 or more nodes with the same (model, prompt_template) → Warning per group
//   - LLM nodes without prompt_template set are skipped entirely
type RedundantLLMDetector struct{}

// NewRedundantLLMDetector returns a ready-to-use RedundantLLMDetector.
func NewRedundantLLMDetector() *RedundantLLMDetector {
	return &RedundantLLMDetector{}
}

// Name returns the unique rule identifier.
func (r *RedundantLLMDetector) Name() string {
	return "redundant_llm_call"
}

// Meta returns the rule metadata used by the tier-aware orchestrator.
func (r *RedundantLLMDetector) Meta() domain.RuleMeta {
	return domain.RuleMeta{
		Name:     r.Name(),
		Severity: domain.Warning,
		Fixable:  false,
	}
}

// Listener implements domain.LocalRule. It builds a per-rule grouping map
// during node visits and finalises duplicates in OnGraph.
func (r *RedundantLLMDetector) Listener(ctx *domain.RuleContext) domain.Listener {
	groups := make(map[llmKey][]string)
	return domain.Listener{
		OnNode: map[domain.NodeType]domain.NodeHandler{
			domain.NodeTypeLLM: func(_ *domain.RuleContext, n *domain.Node) {
				pt := stringConfig(n, "prompt_template")
				if pt == "" {
					return
				}
				key := llmKey{
					model:          stringConfig(n, "model"),
					promptTemplate: pt,
				}
				groups[key] = append(groups[key], n.ID)
			},
		},
		OnGraph: func(c *domain.RuleContext, _ *domain.WorkflowGraph) {
			emitRedundantFindings(c, groups)
		},
	}
}

// Analyze keeps the legacy AnalysisRule contract alive.
func (r *RedundantLLMDetector) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}
	groups := make(map[llmKey][]string)
	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}
		pt := stringConfig(node, "prompt_template")
		if pt == "" {
			continue
		}
		key := llmKey{
			model:          stringConfig(node, "model"),
			promptTemplate: pt,
		}
		groups[key] = append(groups[key], node.ID)
	}

	ctx := domain.NewRuleContext(graph, r.Name())
	emitRedundantFindings(ctx, groups)
	return ctx.Findings()
}

// emitRedundantFindings consumes the bucketed group map and reports a Finding
// per node in every group with size >= 2.
func emitRedundantFindings(ctx *domain.RuleContext, groups map[llmKey][]string) {
	for _, nodeIDs := range groups {
		if len(nodeIDs) < 2 {
			continue
		}
		sort.Strings(nodeIDs)
		idList := strings.Join(nodeIDs, ", ")
		for _, id := range nodeIDs {
			ctx.Report(domain.Finding{
				RuleName: "redundant_llm_call",
				Severity: domain.Warning,
				NodeID:   id,
				Message: fmt.Sprintf(
					"LLM node %q has the same model and prompt_template as nodes: %s",
					id, idList,
				),
				Suggestion: fmt.Sprintf(
					"ノード %s は同じprompt_templateとmodelで実行されています。結果のキャッシュ or 重複排除を検討してください",
					idList,
				),
				Confidence:       0.9,
				ConfidenceReason: domain.ReasonExactStaticMatch,
			})
		}
	}
}

func init() {
	registerBuiltin(NewRedundantLLMDetector())
}
