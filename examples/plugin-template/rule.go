// Package examplerule is a runnable Shingan plugin example that
// flags any LangGraph workflow whose graph name begins with the
// banned prefix "TODO_". The rule is intentionally trivial — the
// purpose of this file is to demonstrate the plugin author surface
// (domain.AnalysisRule + plugin.MustRegister), not to ship a useful
// detector.
//
// To use this plugin in your own shingan binary, see
// examples/plugin-template/cmd/shingan-with-plugins/main.go and the
// README in this directory.
package examplerule

import (
	"strings"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/plugin"
)

// BannedPrefix is the graph-name prefix the rule treats as a marker
// that a workflow hasn't been finalised. Production graphs should
// rename away from "TODO_" before merge.
const BannedPrefix = "TODO_"

// Rule satisfies domain.AnalysisRule with one Finding per node whose
// id begins with BannedPrefix.
//
// In a real plugin you would add struct fields for configuration
// (loaded at init() time from environment / .shingan.yaml extras /
// constructor args) and a Severity field to expose via Meta().
type Rule struct{}

// Name returns the rule's unique identifier. Plugin rules MUST start
// with `plugin.ExperimentalPrefix` until Shingan v1.0.
func (Rule) Name() string { return plugin.ExperimentalPrefix + "todo_node_marker" }

// Analyze emits a Finding for every node whose ID begins with BannedPrefix.
// Severity is forced to Warning at the Finding level so the rule's
// behaviour is independent of the Manifest's default Severity.
func (Rule) Analyze(g *domain.WorkflowGraph) []domain.Finding {
	if g == nil {
		return nil
	}
	out := []domain.Finding{}
	for _, node := range g.Nodes {
		if node == nil {
			continue
		}
		if !strings.HasPrefix(node.ID, BannedPrefix) {
			continue
		}
		out = append(out, domain.Finding{
			RuleName:         Rule{}.Name(),
			NodeID:           node.ID,
			Severity:         domain.Warning,
			Confidence:       1.0,
			ConfidenceReason: domain.ReasonExactStaticMatch,
			Message: "node ID begins with `" + BannedPrefix +
				"`: rename before merge to production",
			Suggestion: "Rename the node to something descriptive; the " +
				"`" + BannedPrefix + "` prefix marks unfinished work.",
		})
	}
	return out
}

func init() {
	plugin.MustRegister(Rule{}, plugin.Manifest{
		Severity:          domain.Warning,
		Frameworks:        []string{"all"},
		Tags:              []string{"hygiene", "example"},
		DocsURL:           "https://github.com/hatyibei/shingan/tree/main/examples/plugin-template",
		MinShinganVersion: "0.9.0",
	})
}
