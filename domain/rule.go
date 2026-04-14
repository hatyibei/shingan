package domain

// AnalysisRule defines the interface that every static analysis rule must implement.
//
// The interface is defined here in the domain layer (the consumer), not in the
// infrastructure layer (the implementor), following the Dependency Inversion
// Principle of Onion Architecture.
//
// Rules are stateless: Analyze receives the graph as a read-only input and
// returns its findings without side effects. This property allows the
// AnalysisOrchestrator in the application layer to run rules concurrently
// using goroutines.
type AnalysisRule interface {
	// Name returns the unique identifier for this rule (e.g. "cycle_detection").
	Name() string

	// Analyze inspects the given WorkflowGraph and returns all findings.
	// An empty (or nil) slice means no issues were detected.
	Analyze(graph *WorkflowGraph) []Finding
}
