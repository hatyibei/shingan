package reporter

import (
	"encoding/json"

	"github.com/hatyibei/shingan/domain"
)

// JSONReporter implements application.ReportFormatter for machine-readable JSON output.
type JSONReporter struct{}

// NewJSONReporter returns a new JSONReporter.
func NewJSONReporter() *JSONReporter {
	return &JSONReporter{}
}

// ContentType returns the MIME type for JSON output.
func (r *JSONReporter) ContentType() string {
	return "application/json"
}

// jsonFinding is the JSON-serializable representation of a domain.Finding.
// SourceFile (ADR-012) is omitted when empty for backward compatibility:
// single-file inputs emit reports without the field, directory inputs
// stamp every finding with its originating path.
type jsonFinding struct {
	Rule             string  `json:"rule"`
	Severity         string  `json:"severity"`
	NodeID           string  `json:"node_id"`
	Message          string  `json:"message"`
	Suggestion       string  `json:"suggestion"`
	Confidence       float64 `json:"confidence"`
	ConfidenceReason string  `json:"confidence_reason,omitempty"`
	SourceFile       string  `json:"source_file,omitempty"`
}

// jsonSummary holds aggregate counts per severity.
type jsonSummary struct {
	Total               int `json:"total"`
	Critical            int `json:"critical"`
	Warning             int `json:"warning"`
	Info                int `json:"info"`
	HighConfidenceCount int `json:"high_confidence_count"`
}

// jsonReport is the top-level JSON structure.
type jsonReport struct {
	Findings []jsonFinding `json:"findings"`
	Summary  jsonSummary   `json:"summary"`
}

// Format serializes findings into JSON bytes.
// Severity values are output as human-readable strings ("info"/"warning"/"critical").
func (r *JSONReporter) Format(findings []domain.Finding) ([]byte, error) {
	jf := make([]jsonFinding, 0, len(findings))
	summary := jsonSummary{Total: len(findings)}

	for _, f := range findings {
		jf = append(jf, jsonFinding{
			Rule:             f.RuleName,
			Severity:         f.Severity.String(),
			NodeID:           f.NodeID,
			Message:          f.Message,
			Suggestion:       f.Suggestion,
			Confidence:       f.Confidence,
			ConfidenceReason: string(f.ConfidenceReason),
			SourceFile:       f.SourceFile,
		})
		switch f.Severity {
		case domain.Critical:
			summary.Critical++
		case domain.Warning:
			summary.Warning++
		case domain.Info:
			summary.Info++
		}
		if f.Confidence >= 0.9 {
			summary.HighConfidenceCount++
		}
	}

	report := jsonReport{
		Findings: jf,
		Summary:  summary,
	}

	return json.Marshal(report)
}
