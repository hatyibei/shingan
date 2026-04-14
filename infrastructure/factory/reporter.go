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
// Supported formats: "json", "markdown".
// Returns an error for unknown format names.
func (f *ReporterFactory) Create(format string) (application.ReportFormatter, error) {
	switch format {
	case "json":
		return reporter.NewJSONReporter(), nil
	case "markdown":
		return reporter.NewMarkdownReporter(), nil
	default:
		return nil, fmt.Errorf("unknown reporter format %q: supported formats are \"json\", \"markdown\"", format)
	}
}
