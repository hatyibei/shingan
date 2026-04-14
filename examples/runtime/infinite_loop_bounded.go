// Package runtime contains ADK-Go agent definitions used as Shingan runtime demo targets.
//
// Expected Findings for infinite_loop_bounded.go:
//   - none (MaxIterations=3 is set, safe execution)
package runtime

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
)

// BuildInfiniteLoopBounded constructs a LoopAgent with MaxIterations=3.
// Shingan finds no Critical issues — this agent is safe to execute.
// The loop runs a counter agent that increments a value and stops at 3.
func BuildInfiniteLoopBounded() (agent.Agent, error) {
	counterAgent, err := llmagent.New(llmagent.Config{
		Name: "counter_agent",
		Instruction: `You are a simple counter. Each time you are called, respond with the next integer
starting from 1. If you have already responded with 3 or more, escalate by including the word "DONE"
in your response. Keep your response short (one line).`,
	})
	if err != nil {
		return nil, err
	}

	return loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "bounded_loop",
			SubAgents: []agent.Agent{counterAgent},
		},
		MaxIterations: 3, // Shingan detects this as safe (max_iterations is set)
	})
}
