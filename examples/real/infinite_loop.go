// Package real contains ADK-Go SDK–native agent definitions used as
// Shingan static-analysis fixtures.  Unlike testdata/agents/*.go these files
// import the real google.golang.org/adk SDK and compile without any build tag.
//
// Expected Findings for infinite_loop.go:
//   - cycle_detection: Critical — loopagent.Config with MaxIterations == 0 (absent)
//     produces an unbounded loop; Shingan detects the missing max_iterations guard.
package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
)

// BuildInfiniteLoop constructs a LoopAgent without MaxIterations set,
// which creates an unbounded loop that runs until a sub-agent escalates.
// Shingan should flag this as cycle_detection Critical.
func BuildInfiniteLoop() {
	classifier, _ := llmagent.New(llmagent.Config{
		Name:        "classifier",
		Instruction: "Classify the input document.",
	})
	validator, _ := llmagent.New(llmagent.Config{
		Name:        "validator",
		Instruction: "Validate the classification result.",
	})

	// MaxIterations is intentionally absent — this is the bug Shingan detects.
	_, _ = loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "retry_loop",
			SubAgents: []agent.Agent{classifier, validator},
		},
		// MaxIterations: 0  (default) → infinite loop
	})
}
