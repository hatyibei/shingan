// Package runtime contains ADK-Go agent definitions used as Shingan runtime demo targets.
//
// Expected Findings for infinite_loop_unbounded.go:
//   - cycle_detection: Critical — LoopAgent with MaxIterations == 0 (absent)
//     produces an unbounded loop; Shingan detects the missing max_iterations guard.
//
// SAFETY NOTE: This file exists ONLY as a static analysis target.
// cmd/runner/main.go will refuse to execute this agent after Shingan detects
// the Critical finding. The guard prevents actual infinite execution.
package runtime

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
)

// BuildInfiniteLoopUnbounded constructs a LoopAgent WITHOUT MaxIterations set.
// This intentionally introduces the cycle_detection Critical bug.
// Shingan will flag this as Critical — the runner will refuse to execute it.
func BuildInfiniteLoopUnbounded() (agent.Agent, error) {
	classifierAgent, err := llmagent.New(llmagent.Config{
		Name:        "classifier",
		Instruction: "Classify the input. Respond with a category name.",
	})
	if err != nil {
		return nil, err
	}

	// MaxIterations is intentionally absent — this is the bug Shingan detects.
	// Note: use assignment so Shingan's AST parser recognises the LoopAgent node.
	unboundedLoop, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "unbounded_loop",
			SubAgents: []agent.Agent{classifierAgent},
		},
		// MaxIterations: 0 (default) → infinite loop → Shingan Critical
	})
	if err != nil {
		return nil, err
	}
	return unboundedLoop, nil
}
