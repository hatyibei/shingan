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

// ConfidenceReason categorises why a Finding carries the Confidence value it
// does. It accompanies the numeric Confidence so that downstream consumers
// (LSP hover, SARIF properties, Markdown reporter) can show actionable
// explanations rather than a bare number (ADR-008).
type ConfidenceReason string

const (
	// ReasonExactStaticMatch is used for findings produced by deterministic
	// static checks (e.g. DFS back-edge cycle detection, exact string match
	// against a deprecated model list). Recommended Confidence: 1.0.
	ReasonExactStaticMatch ConfidenceReason = "exact_static_match"

	// ReasonOverApproximatedDynamic is used when the analyser had to make a
	// conservative assumption to handle dynamic constructs (e.g. LangGraph
	// conditional_edges where the return type is untyped str). Recommended
	// Confidence: 0.5.
	ReasonOverApproximatedDynamic ConfidenceReason = "over_approximated_dynamic"

	// ReasonParserFallback is used when the parser could not fully resolve
	// a node's metadata and the rule had to operate on partial data.
	// Recommended Confidence: 0.4.
	ReasonParserFallback ConfidenceReason = "parser_fallback"

	// ReasonExperimentalRule is used by experimental rules whose detection
	// logic is not yet validated against a wide corpus. Recommended
	// Confidence: 0.6.
	ReasonExperimentalRule ConfidenceReason = "experimental_rule"

	// ReasonHeuristicPattern is used when the rule relies on naming or
	// shape heuristics rather than precise semantics (e.g. PII-hint name
	// matching, prompt-template duplicate detection). Recommended
	// Confidence: 0.3-0.7.
	ReasonHeuristicPattern ConfidenceReason = "heuristic_pattern"
)

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
	// ConfidenceReason categorises why Confidence has the value it does.
	// Refactored rules MUST populate this to make filter decisions
	// (`--min-confidence`) interpretable. Pre-refactor rules that have not
	// been migrated yet may leave it empty; the JSON/SARIF reporters omit
	// the field via `omitempty` semantics on the producing layer.
	ConfidenceReason ConfidenceReason
	// SourceFile is the absolute or repo-relative path of the file the
	// finding originates from. Populated by the multi-file directory
	// pipeline (ADR-012) so per-file independent graphs can attribute
	// findings to their originating file even when Node.Pos is empty
	// (e.g. JSON parser without position info). Single-file inputs leave
	// it empty; consumers should fall back to Node.Pos.File when needed.
	SourceFile string
}
