package factory

import (
	"fmt"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// ParserFactory creates WorkflowParser instances by format name.
type ParserFactory struct{}

// NewParserFactory returns a ready-to-use ParserFactory.
func NewParserFactory() *ParserFactory {
	return &ParserFactory{}
}

// Create returns a WorkflowParser for the given format name.
// Supported formats: "json", "adk-go".
// Returns an error for unknown format names.
func (f *ParserFactory) Create(format string) (application.WorkflowParser, error) {
	switch format {
	case "json":
		return parser.NewJSONParser(), nil
	case "adk-go":
		return parser.NewADKGoParser(), nil
	default:
		return nil, fmt.Errorf("unknown parser format %q: supported formats are \"json\", \"adk-go\"", format)
	}
}
