// Package real contains ADK-Go SDK–native agent definitions used as
// Shingan static-analysis fixtures.
//
// Expected Findings for missing_handler.go:
//   - error_handler_checker: Warning — the planner LlmAgent has Tools but no
//     error-handler path (no fallback sub-agent on tool failure).
package real

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type browserArgs struct{ Query string }
type browserResult struct{ Content string }

// browserSearch is a stub tool representing browser automation.
// In production this would call a real browser; here it is a no-op stub.
var browserSearch, _ = functiontool.New(
	functiontool.Config{Name: "browser_search", Description: "Search the web via browser automation."},
	functiontool.Func[browserArgs, browserResult](func(_ tool.Context, args browserArgs) (browserResult, error) {
		return browserResult{Content: "stub: " + args.Query}, nil
	}),
)

// BuildMissingHandler constructs a SequentialAgent whose planner uses an
// external tool (browser_search) but provides no error-handler fallback.
// Shingan should flag this as error_handler_checker Warning.
func BuildMissingHandler() {
	planner, _ := llmagent.New(llmagent.Config{
		Name:        "planner",
		Instruction: "Plan the scraping steps.",
		Tools:       []tool.Tool{browserSearch},
	})
	summarizer, _ := llmagent.New(llmagent.Config{
		Name:        "summarizer",
		Instruction: "Summarize the scraped content.",
	})
	_, _ = sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:      "web_scraper",
			SubAgents: []agent.Agent{planner, summarizer},
		},
	})
}
