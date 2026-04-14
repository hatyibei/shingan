// Package real contains ADK-Go SDK–native agent definitions used as
// Shingan static-analysis fixtures.
//
// Expected Findings for unreachable.go:
//   - unreachable_node: Warning — orphan_analyzer is defined but never added
//     to the orchestrator's SubAgents, so it is unreachable from the entry node.
package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
)

// BuildUnreachable constructs a SequentialAgent orchestrator that intentionally
// omits orphanAnalyzer from its SubAgents list.
// Shingan should flag orphan_analyzer as unreachable_node Warning.
func BuildUnreachable() {
	inputProcessor, _ := llmagent.New(llmagent.Config{
		Name:        "input_processor",
		Instruction: "Process the raw input.",
	})
	outputFormatter, _ := llmagent.New(llmagent.Config{
		Name:        "output_formatter",
		Instruction: "Format the final output.",
	})

	// orphanAnalyzer is created but deliberately excluded from SubAgents.
	// This represents a dead node — defined but never reached.
	orphanAnalyzer, _ := llmagent.New(llmagent.Config{
		Name:        "orphan_analyzer",
		Instruction: "This node is never reached from the orchestrator.",
	})
	_ = orphanAnalyzer // suppress "declared and not used"

	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "orchestrator",
			SubAgents: []agent.Agent{inputProcessor, outputFormatter},
			// orphanAnalyzer is intentionally absent here
		},
	})
}
