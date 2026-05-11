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

// RuleMetadata is the SARIF-facing slice of a rule manifest. It is
// optional input to SARIFReporter — when supplied, the reporter emits
// `reportingDescriptor` entries with `helpUri`, descriptions, and a
// rich `properties.tags` set including the rule's stability flag,
// declared frameworks, and category tags. GitHub Code Scanning uses
// `properties.tags` for filtering, and we lean on the same machinery
// to let users distinguish built-in rules from plugin rules
// (stability=experimental).
//
// Defined here (rather than in domain or application) because it is
// pure SARIF-emission metadata — the source-of-truth lives in
// application.RuleManifest; the CLI converts to this struct before
// instantiating the reporter so the Onion direction
// (infrastructure ← cli) stays correct.
type RuleMetadata struct {
	Description string
	Stability   string   // "stable" | "experimental"
	Tags        []string // domain category tags ("security", "cost", …)
	Frameworks  []string // ("langgraph", "all", …)
	DocsURL     string   // becomes reportingDescriptor.helpUri
}

// SARIFReporter implements application.ReportFormatter for SARIF v2.1.0 output,
// compatible with GitHub Code Scanning.
type SARIFReporter struct {
	// metadata is keyed by rule name. nil = legacy output (no
	// descriptions, no tags, no helpUri) so the public constructor
	// stays backwards compatible.
	metadata map[string]RuleMetadata
}

// NewSARIFReporter returns a new SARIFReporter without per-rule
// metadata. Use WithRuleMetadata to attach a manifest catalog so the
// emitted `reportingDescriptor` entries carry stability flags,
// helpUri, tags, and descriptions.
func NewSARIFReporter() *SARIFReporter {
	return &SARIFReporter{}
}

// WithRuleMetadata attaches the metadata catalog used to enrich the
// SARIF `reportingDescriptor` entries. Pass nil to clear. Returns the
// receiver so the call chains in factory wiring.
func (r *SARIFReporter) WithRuleMetadata(m map[string]RuleMetadata) *SARIFReporter {
	r.metadata = m
	return r
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

// sarifPrecision converts a confidence value to a SARIF precision string.
// high: >=0.9, medium: 0.6–0.9, low: <0.6
func sarifPrecision(confidence float64) string {
	switch {
	case confidence >= 0.9:
		return "high"
	case confidence >= 0.6:
		return "medium"
	default:
		return "low"
	}
}

// ---- SARIF JSON structures ----

type sarifRoot struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	ShortDescription sarifMessage           `json:"shortDescription"`
	FullDescription  sarifMessage           `json:"fullDescription"`
	HelpURI          string                 `json:"helpUri,omitempty"`
	DefaultConfig    sarifDefaultConfig     `json:"defaultConfiguration"`
	Properties       map[string]interface{} `json:"properties,omitempty"`
}

type sarifDefaultConfig struct {
	Level string `json:"level"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID     string                 `json:"ruleId"`
	Level      string                 `json:"level"`
	Message    sarifMessage           `json:"message"`
	Locations  []sarifLocation        `json:"locations"`
	Properties map[string]interface{} `json:"properties,omitempty"`
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
	// Build ordered unique rules, preserving first-seen severity and confidence per rule.
	type ruleKey struct {
		name       string
		severity   domain.Severity
		confidence float64
	}
	seen := make(map[string]bool)
	var orderedRules []ruleKey

	for _, f := range findings {
		if !seen[f.RuleName] {
			seen[f.RuleName] = true
			orderedRules = append(orderedRules, ruleKey{
				name:       f.RuleName,
				severity:   f.Severity,
				confidence: f.Confidence,
			})
		}
	}

	rules := make([]sarifRule, 0, len(orderedRules))
	for _, rk := range orderedRules {
		// Default — emitted when metadata is nil so legacy behaviour
		// (rule ID as both descriptions, no helpUri, no tags) is
		// preserved.
		shortDesc := rk.name
		fullDesc := rk.name
		helpURI := ""
		props := map[string]interface{}{
			"precision": sarifPrecision(rk.confidence),
		}

		// Plugin namespace separation lives here: stability flag,
		// declared frameworks, and category tags ride along as
		// SARIF `properties.tags`. GitHub Code Scanning surfaces this
		// array as filter chips so security teams can scope to
		// "shingan-stable + tag:security" without writing a query.
		if md, ok := r.metadata[rk.name]; ok {
			if md.Description != "" {
				shortDesc = md.Description
				fullDesc = md.Description
			}
			helpURI = md.DocsURL

			tags := make([]string, 0, 2+len(md.Tags)+len(md.Frameworks))
			tags = append(tags, "shingan-rule")
			if md.Stability != "" {
				tags = append(tags, "stability:"+md.Stability)
			}
			for _, t := range md.Tags {
				tags = append(tags, "category:"+t)
			}
			for _, fw := range md.Frameworks {
				tags = append(tags, "framework:"+fw)
			}
			props["tags"] = tags
			if md.Stability != "" {
				props["stability"] = md.Stability
			}
			if len(md.Frameworks) > 0 {
				props["frameworks"] = md.Frameworks
			}
		}

		rules = append(rules, sarifRule{
			ID:               rk.name,
			Name:             rk.name,
			ShortDescription: sarifMessage{Text: shortDesc},
			FullDescription:  sarifMessage{Text: fullDesc},
			HelpURI:          helpURI,
			DefaultConfig:    sarifDefaultConfig{Level: sarifLevel(rk.severity)},
			Properties:       props,
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
			Properties: map[string]interface{}{
				"confidence": f.Confidence,
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
