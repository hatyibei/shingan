//go:build shingan_fixture

// Package agents contains intentionally buggy ADK-Go agent definitions for use
// as Shingan static-analysis fixtures.
//
// Expected Findings for unreachable.go:
//   - unreachable_node: Warning — orphan_analyzer is defined but not reachable
//     from the entry node (it is not listed in orchestrator's SubAgents)
package agents

// orchestrator is the entry SequentialAgent. orphanAnalyzer is NOT in SubAgents.
var orchestrator = &SequentialAgent{
	Name: "orchestrator",
	SubAgents: []Agent{
		&LlmAgent{
			Name:        "input_processor",
			Model:       "gpt-4o-mini",
			Instruction: "Process the raw input.",
		},
		&LlmAgent{
			Name:        "output_formatter",
			Model:       "gpt-4o-mini",
			Instruction: "Format the final output.",
		},
	},
}

// orphanAnalyzer is defined but never connected to the orchestrator — unreachable node.
var orphanAnalyzer = &LlmAgent{
	Name:        "orphan_analyzer",
	Model:       "gpt-4o",
	Instruction: "This node is never reached from the orchestrator.",
}
