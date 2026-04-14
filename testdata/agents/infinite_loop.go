//go:build shingan_fixture

// Package agents contains intentionally buggy ADK-Go agent definitions for use
// as Shingan static-analysis fixtures.
//
// Expected Findings for infinite_loop.go:
//   - cycle_detection: Critical — LoopAgent has a loopback edge with no MaxIterations guard
//   - (MaxIterations is not set, so Config["max_iterations"] is absent)
package agents

// retryAgent is a LoopAgent without MaxIterations — potential infinite loop.
//
//shingan:entry
var retryAgent = &LoopAgent{
	Name: "retry_loop",
	SubAgents: []Agent{
		&LlmAgent{
			Name:        "classifier",
			Model:       "gpt-4o",
			Instruction: "Classify the input.",
		},
		&LlmAgent{
			Name:        "validator",
			Model:       "gpt-4o-mini",
			Instruction: "Validate the classification result.",
		},
	},
}
