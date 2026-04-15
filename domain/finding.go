package domain

// Severity represents the importance of a Finding.
// Higher numeric values indicate higher severity, enabling natural sort order
// (Critical > Warning > Info).
type Severity int

const (
	// Info indicates a suggestion or improvement opportunity.
	Info Severity = iota // 0
	// Warning indicates a condition that may cause failures under some circumstances.
	Warning // 1
	// Critical indicates a condition that will cause workflow failure or poses
	// a serious risk (e.g., infinite loop with no exit condition).
	Critical // 2
)

// String returns the human-readable label for the Severity.
func (s Severity) String() string {
	switch s {
	case Critical:
		return "critical"
	case Warning:
		return "warning"
	case Info:
		return "info"
	default:
		return "unknown"
	}
}

// Finding represents a single issue detected by an AnalysisRule.
type Finding struct {
	// RuleName is the identifier of the rule that produced this finding.
	RuleName string
	// Severity is the importance level of the finding.
	Severity Severity
	// NodeID is the ID of the node where the issue was detected.
	// Empty string means the finding applies to the whole graph.
	NodeID string
	// Message is a concise description of the detected issue.
	Message string
	// Suggestion is an actionable recommendation for fixing the issue.
	Suggestion string
	// Confidence is the rule's certainty that this is a true positive (0.0–1.0).
	// 1.0 = deterministic detection (e.g. DFS back-edge), <0.5 = heuristic.
	// The orchestrator normalises 0.0 to 1.0 for backward compatibility.
	Confidence float64
}
