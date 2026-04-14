// Package runtime contains ADK-Go agent definitions used as Shingan runtime demo targets.
//
// These files serve two roles:
//   1. Static analysis targets — Shingan's ADKGoParser reads them as source.
//   2. Reference implementations — the patterns here are mirrored in cmd/runner/samples.go
//      for actual execution against Vertex AI Gemini.
//
// Expected Findings for simple_agent.go:
//   - none (clean agent, no structural issues)
package runtime

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
)

// BuildSimpleAgent constructs a minimal LlmAgent.
// No Model field is set here — this file is the static analysis target.
// The actual runtime builder in cmd/runner/samples.go provides a real model.
func BuildSimpleAgent() (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        "hello_agent",
		Instruction: "Say hello in Japanese. Respond with a single short greeting only.",
	})
}
