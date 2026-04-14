package main

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

// agentSourceMap maps ADK app names to the source files that Shingan analyzes.
// The keys must match the agent Name fields registered in agent.NewMultiLoader.
var agentSourceMap = map[string]string{
	"infinite_loop_unbounded": "examples/runtime/infinite_loop_unbounded.go",
	"infinite_loop_bounded":   "examples/runtime/infinite_loop_bounded.go",
	"simple_hello":            "examples/runtime/simple_agent.go",
}

// buildDemoLoader constructs the three demo agents and returns a MultiLoader
// together with the source-file map used by the middleware for static analysis.
//
// infinite_loop_unbounded is built without a model because Shingan will block
// all run requests for it before they reach the ADK runtime.
// The other two agents are backed by Vertex AI Gemini.
func buildDemoLoader(ctx context.Context) (agent.Loader, map[string]string, error) {
	// unbounded — no model needed; Shingan blocks all runs
	unbounded, err := buildInfiniteLoopUnbounded()
	if err != nil {
		return nil, nil, fmt.Errorf("build infinite_loop_unbounded: %w", err)
	}

	// bounded — real Gemini model, Shingan passes
	bounded, err := buildInfiniteLoopBounded(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("build infinite_loop_bounded: %w", err)
	}

	// simple_hello — real Gemini model, Shingan passes
	simple, err := buildSimpleHello(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("build simple_hello: %w", err)
	}

	loader, err := agent.NewMultiLoader(unbounded, bounded, simple)
	if err != nil {
		return nil, nil, fmt.Errorf("create multi loader: %w", err)
	}

	return loader, agentSourceMap, nil
}

// buildInfiniteLoopUnbounded creates a LoopAgent without MaxIterations.
// This is intentionally unsafe — Shingan's guard will block execution.
func buildInfiniteLoopUnbounded() (agent.Agent, error) {
	classifierAgent, err := llmagent.New(llmagent.Config{
		Name:        "classifier",
		Instruction: "Classify the input. Respond with a category name.",
		// No model: Shingan guard blocks before ADK tries to call it.
	})
	if err != nil {
		return nil, fmt.Errorf("create classifier agent: %w", err)
	}

	return loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "infinite_loop_unbounded",
			SubAgents: []agent.Agent{classifierAgent},
		},
		// MaxIterations intentionally absent — this is the Critical bug Shingan detects.
	})
}

// buildInfiniteLoopBounded creates a LoopAgent with MaxIterations=3.
// Shingan finds no Critical issues — safe to execute.
func buildInfiniteLoopBounded(ctx context.Context) (agent.Agent, error) {
	m, err := newVertexGemini(ctx)
	if err != nil {
		return nil, err
	}

	counterAgent, err := llmagent.New(llmagent.Config{
		Name:  "counter_agent",
		Model: m,
		Instruction: `You are a simple counter. Each time you are called, respond with the next integer
starting from 1. If you have already responded with 3 or more, escalate by including the word "DONE"
in your response. Keep your response short (one line).`,
	})
	if err != nil {
		return nil, fmt.Errorf("create counter agent: %w", err)
	}

	return loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:      "infinite_loop_bounded",
			SubAgents: []agent.Agent{counterAgent},
		},
		MaxIterations: 3,
	})
}

// buildSimpleHello creates a minimal LlmAgent that greets in Japanese.
// Shingan finds no issues — safe to execute.
func buildSimpleHello(ctx context.Context) (agent.Agent, error) {
	m, err := newVertexGemini(ctx)
	if err != nil {
		return nil, err
	}

	return llmagent.New(llmagent.Config{
		Name:        "simple_hello",
		Model:       m,
		Instruction: "Say hello in Japanese. Respond with a single short greeting only.",
	})
}

// newVertexGemini creates a Gemini model backed by Vertex AI using ADC.
func newVertexGemini(ctx context.Context) (model.LLM, error) {
	proj := projectID
	if v := getEnvOrDefault("GOOGLE_CLOUD_PROJECT", ""); v != "" {
		proj = v
	}
	loc := location
	if v := getEnvOrDefault("GOOGLE_CLOUD_LOCATION", ""); v != "" {
		loc = v
	}

	m, err := adkgemini.NewModel(ctx, geminiModel, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  proj,
		Location: loc,
	})
	if err != nil {
		return nil, fmt.Errorf("create Vertex AI Gemini model: %w", err)
	}
	return m, nil
}

// getEnvOrDefault returns the environment variable value or a fallback.
func getEnvOrDefault(key, fallback string) string {
	if v, ok := lookupEnv(key); ok {
		return v
	}
	return fallback
}
