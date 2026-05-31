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
// Supported formats: "json", "adk-go", "samurai", "langgraph", "n8n",
// "crewai", "langgraph-js", "pydantic-graph", "llamaindex", "autogen",
// "mastra". Returns an error for unknown format names.
//
// Note: the LangGraph, CrewAI, LangGraph.js, pydantic-graph, llamaindex,
// autogen and mastra parsers each own a subprocess (Python for
// langgraph/crewai/pydantic-graph/llamaindex/autogen, Node for
// langgraph-js/mastra).
// Callers that handle many graphs in one session should keep a single parser
// instance and reuse it; the factory itself does not memoise instances
// (matching the existing stateless design for json/adk-go/samurai/n8n).
// Failure to spawn the Python worker (Python missing, framework not
// installed, etc.) yields a descriptive error that callers can surface to
// the user.
func (f *ParserFactory) Create(format string) (application.WorkflowParser, error) {
	switch format {
	case "json":
		return parser.NewJSONParser(), nil
	case "adk-go":
		return parser.NewADKGoParser(), nil
	case "samurai":
		return parser.NewSamuraiParser(), nil
	case "langgraph":
		p, err := parser.NewLangGraphParser()
		if err != nil {
			return nil, fmt.Errorf("create langgraph parser: %w", err)
		}
		return p, nil
	case "n8n":
		return parser.NewN8nParser(), nil
	case "crewai":
		p, err := parser.NewCrewAIParser()
		if err != nil {
			return nil, fmt.Errorf("create crewai parser: %w", err)
		}
		return p, nil
	case "langgraph-js":
		p, err := parser.NewLangGraphJSParser()
		if err != nil {
			return nil, fmt.Errorf("create langgraph-js parser: %w", err)
		}
		return p, nil
	case "pydantic-graph":
		p, err := parser.NewPydanticGraphParser()
		if err != nil {
			return nil, fmt.Errorf("create pydantic-graph parser: %w", err)
		}
		return p, nil
	case "llamaindex":
		p, err := parser.NewLlamaIndexParser()
		if err != nil {
			return nil, fmt.Errorf("create llamaindex parser: %w", err)
		}
		return p, nil
	case "autogen":
		p, err := parser.NewAutoGenParser()
		if err != nil {
			return nil, fmt.Errorf("create autogen parser: %w", err)
		}
		return p, nil
	case "mastra":
		p, err := parser.NewMastraParser()
		if err != nil {
			return nil, fmt.Errorf("create mastra parser: %w", err)
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown parser format %q: supported formats are \"json\", \"adk-go\", \"samurai\", \"langgraph\", \"n8n\", \"crewai\", \"langgraph-js\", \"pydantic-graph\", \"llamaindex\", \"autogen\", \"mastra\"", format)
	}
}
