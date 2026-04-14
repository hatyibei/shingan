package application

import "github.com/hatyibei/shingan/domain"

// WorkflowParser converts raw input bytes into a WorkflowGraph.
// The interface is defined here in the application layer (the consumer) and
// implementations live in the infrastructure layer (Dependency Inversion).
type WorkflowParser interface {
	// Parse converts raw input bytes into a WorkflowGraph.
	// Returns an error if the input cannot be parsed or produces an invalid graph.
	Parse(input []byte) (*domain.WorkflowGraph, error)

	// SupportedFormat returns the canonical format name this parser handles
	// (e.g. "json", "adk-go").
	SupportedFormat() string
}
