package reporter

import (
	"encoding/json"

	"github.com/hatyibei/shingan/domain"
)

const (
	sarifSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion = "2.1.0"
	toolName     = "Shingan"
	toolVersion  = "0.1.0"
	toolInfoURI  = "https://github.com/hatyibei/shingan"
)

// SARIFReporter implements application.ReportFormatter for SARIF v2.1.0 output,
// compatible with GitHub Code Scanning.
type SARIFReporter struct{}

// NewSARIFReporter returns a new SARIFReporter.
func NewSARIFReporter() *SARIFReporter {
	return &SARIFReporter{}
}

// ContentType returns the MIME type for SARIF output.
func (r *SARIFReporter) ContentType() string {
	return "application/sarif+json"
}

// sarifLevel converts a domain.Severity to the SARIF level string.
func sarifLevel(s domain.Severity) string {
	switch s {
	case domain.Critical:
		return "error"
	case domain.Warning:
		return "warning"
	default:
		return "note"
	}
}

// ---- SARIF JSON structures ----

type sarifRoot struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool    `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string       `json:"name"`
	Version        string       `json:"version"`
	InformationURI string       `json:"informationUri"`
	Rules          []sarifRule  `json:"rules"`
}

type sarifRule struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	ShortDescription sarifMessage        `json:"shortDescription"`
	FullDescription  sarifMessage        `json:"fullDescription"`
	DefaultConfig    sarifDefaultConfig  `json:"defaultConfiguration"`
}

type sarifDefaultConfig struct {
	Level string `json:"level"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string            `json:"ruleId"`
	Level     string            `json:"level"`
	Message   sarifMessage      `json:"message"`
	Locations []sarifLocation   `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// Format serializes findings into SARIF v2.1.0 JSON bytes.
// Rules are deduplicated by RuleName; each Finding becomes one result entry.
// Artifact URIs use the synthetic scheme "workflow://nodes/<nodeID>"
// since Shingan's Workflow Graph does not carry source file locations.
func (r *SARIFReporter) Format(findings []domain.Finding) ([]byte, error) {
	// Build ordered unique rules, preserving first-seen severity per rule.
	type ruleKey struct {
		name     string
		severity domain.Severity
	}
	seen := make(map[string]bool)
	var orderedRules []ruleKey

	for _, f := range findings {
		if !seen[f.RuleName] {
			seen[f.RuleName] = true
			orderedRules = append(orderedRules, ruleKey{name: f.RuleName, severity: f.Severity})
		}
	}

	rules := make([]sarifRule, 0, len(orderedRules))
	for _, rk := range orderedRules {
		rules = append(rules, sarifRule{
			ID:               rk.name,
			Name:             rk.name,
			ShortDescription: sarifMessage{Text: rk.name},
			FullDescription:  sarifMessage{Text: rk.name},
			DefaultConfig:    sarifDefaultConfig{Level: sarifLevel(rk.severity)},
		})
	}

	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		nodeURI := "workflow://nodes/" + f.NodeID
		if f.NodeID == "" {
			nodeURI = "workflow://graph"
		}

		results = append(results, sarifResult{
			RuleID:  f.RuleName,
			Level:   sarifLevel(f.Severity),
			Message: sarifMessage{Text: f.Message},
			Locations: []sarifLocation{
				{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifactLocation{URI: nodeURI},
					},
				},
			},
		})
	}

	doc := sarifRoot{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           toolName,
						Version:        toolVersion,
						InformationURI: toolInfoURI,
						Rules:          rules,
					},
				},
				Results: results,
			},
		},
	}

	return json.MarshalIndent(doc, "", "  ")
}
