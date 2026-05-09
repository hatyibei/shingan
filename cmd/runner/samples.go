// Package main provides the shingan-runner CLI.
package main

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/genai"

	"google.golang.org/adk/model"
	adkgemini "google.golang.org/adk/model/gemini"
)

const (
	// vertexProject is the GCP project for Vertex AI.
	vertexProject = "axial-mercury-486503-j5"
	// vertexLocation is the GCP region.
	vertexLocation = "us-central1"
	// geminiModel is the Gemini model used for all demo agents.
	// gemini-2.0-flash-001 is available on Vertex AI us-central1 and suitable for PoC.
	geminiModel = "gemini-2.0-flash-001"
)

// buildSimpleAgent creates a minimal LlmAgent backed by Vertex AI Gemini.
// Corresponds to examples/runtime/simple_agent.go.
func buildSimpleAgent(ctx context.Context) (agent.Agent, error) {
	m, err := newGeminiModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("create model: %w", err)
	}
	return llmagent.New(llmagent.Config{
		Name:        "hello_agent",
		Model:       m,
		Instruction: "Say hello in Japanese. Respond with a single short greeting only.",
	})
}

// buildInfiniteLoopBounded creates a LoopAgent with MaxIterations=3.
// Corresponds to examples/runtime/infinite_loop_bounded.go.
// If maxIter > 0 the provided value overrides the default of 3.
func buildInfiniteLoopBounded(ctx context.Context, maxIter uint) (agent.Agent, error) {
	if maxIter == 0 {
		maxIter = 3
	}
	m, err := newGeminiModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("create model: %w", err)
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
			Name:      "bounded_loop",
			SubAgents: []agent.Agent{counterAgent},
		},
		MaxIterations: maxIter,
	})
}

// newGeminiModel creates a Gemini model using Vertex AI backend with ADC.
func newGeminiModel(ctx context.Context) (model.LLM, error) {
	return adkgemini.NewModel(ctx, geminiModel, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  vertexProject,
		Location: vertexLocation,
	})
}
