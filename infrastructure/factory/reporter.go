package factory

import (
	"fmt"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/infrastructure/reporter"
)

// ReporterFactory creates ReportFormatter instances by format name.
type ReporterFactory struct{}

// NewReporterFactory returns a new ReporterFactory.
func NewReporterFactory() *ReporterFactory {
	return &ReporterFactory{}
}

// Create returns a ReportFormatter for the given format name.
// Supported formats: "json", "markdown", "sarif".
// Returns an error for unknown format names.
func (f *ReporterFactory) Create(format string) (application.ReportFormatter, error) {
	switch format {
	case "json":
		return reporter.NewJSONReporter(), nil
	case "markdown":
		return reporter.NewMarkdownReporter(), nil
	case "sarif":
		return reporter.NewSARIFReporter(), nil
	default:
		return nil, fmt.Errorf("unknown reporter format %q: supported formats are \"json\", \"markdown\", \"sarif\"", format)
	}
}

// CreateSARIFWithMetadata returns a SARIF reporter enriched with the
// rule metadata catalog. The CLI calls this (rather than Create) when
// emitting SARIF so the resulting reportingDescriptor entries carry
// stability flags, helpUri, descriptions, and category/framework
// tags. Other formats don't currently consume the manifest catalog,
// so Create stays simple.
func (f *ReporterFactory) CreateSARIFWithMetadata(metadata map[string]reporter.RuleMetadata) application.ReportFormatter {
	return reporter.NewSARIFReporter().WithRuleMetadata(metadata)
}
