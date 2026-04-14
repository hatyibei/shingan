//go:build shingan_fixture

// Package agents contains intentionally buggy ADK-Go agent definitions for use
// as Shingan static-analysis fixtures.
//
// Expected Findings for missing_handler.go:
//   - error_handler_checker: Warning — browser_tool (NodeTypeTool) has no outgoing
//     error-handler edge (no fallback path on failure)
package agents

// webScraper is a SequentialAgent that uses a browser tool without error handling.
var webScraper = &SequentialAgent{
	Name: "web_scraper",
	SubAgents: []Agent{
		&LlmAgent{
			Name:        "planner",
			Model:       "gpt-4o",
			Instruction: "Plan the scraping steps.",
			Tools: []Tool{
				browserTool,
				apiTool,
			},
		},
		&LlmAgent{
			Name:        "summarizer",
			Model:       "gpt-4o-mini",
			Instruction: "Summarize the scraped content.",
		},
	},
}

// browserTool provides browser automation capabilities.
var browserTool = &BrowserTool{
	Name: "browser_tool",
}

// apiTool provides external API access.
var apiTool = &APITool{
	Name: "api_tool",
}
