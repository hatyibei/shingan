// Regression fixture for the v0.9.1 ADK-Go FP fix: SequentialAgent
// must NOT fire loop_guard.
//
// Mirrors the pattern from google/adk-samples/go/agents/llm-auditor
// (auditor/auditor.go) which previously surfaced a Critical
// false-positive `loop_guard` finding because the parser mapped
// SequentialAgent to NodeTypeControl and the rule treated Control as
// Loop. After v0.9.1 the parser uses NodeTypeSequence; loop_guard
// only fires on NodeTypeLoop / NodeTypeControl, so this fixture
// should produce zero findings under loop_guard.
//
// Build tag prevents `go build ./...` from trying to compile against
// the real ADK-Go SDK (we only need parser-level analysis here).
//go:build ignore

package auditor

import (
	"google.golang.org/adk-go/agent"
	"google.golang.org/adk-go/agent/sequentialagent"
)

func NewAuditor(criticAgent, reviserAgent agent.Agent) agent.Agent {
	rootAgent, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "llm_auditor",
			Description: "Evaluates LLM-generated answers.",
			SubAgents: []agent.Agent{
				criticAgent,
				reviserAgent,
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return rootAgent
}
