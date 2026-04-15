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

// Analyze scans all LLM nodes for duplicate (model, prompt_template) pairs and
// reports each group of duplicates as a Warning finding on every affected node.
func (r *RedundantLLMDetector) Analyze(graph *domain.WorkflowGraph) []domain.Finding {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	// Group node IDs by their (model, prompt_template) key.
	// Nodes without prompt_template are skipped.
	groups := make(map[llmKey][]string)

	for _, node := range graph.Nodes {
		if node.Type != domain.NodeTypeLLM {
			continue
		}

		pt := stringConfig(node, "prompt_template")
		if pt == "" {
			// No prompt_template — skip as per spec.
			continue
		}

		model := stringConfig(node, "model")
		key := llmKey{model: model, promptTemplate: pt}
		groups[key] = append(groups[key], node.ID)
	}

	var findings []domain.Finding

	for _, nodeIDs := range groups {
		if len(nodeIDs) < 2 {
			continue
		}

		// Sort IDs for deterministic output.
		sort.Strings(nodeIDs)
		idList := strings.Join(nodeIDs, ", ")

		// Emit one Finding per affected node.
		for _, id := range nodeIDs {
			findings = append(findings, domain.Finding{
				RuleName: r.Name(),
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
				Confidence: 0.9,
			})
		}
	}

	return findings
}
