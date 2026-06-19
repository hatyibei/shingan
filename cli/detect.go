package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// detectFormat infers the analyze --format for a file or directory from its
// extension plus lightweight content sniffing (import statements / structural
// JSON keys). It returns "" when nothing recognisable is found, so the caller
// can fail with a clear, actionable message instead of mis-parsing.
//
// This is the keystone for `--format auto`, and therefore for an editor / agent
// hook that fires on an arbitrary saved file: the hook cannot know the framework
// up front, so the analyzer has to. Sniffing reads only the head of each file.
func detectFormat(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return detectFormatDir(path)
	}
	return detectFormatFile(path)
}

func detectFormatFile(path string) string {
	body := strings.ToLower(readHead(path, 16*1024))
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		return detectPython(body)
	case ".ts", ".mts", ".cts", ".tsx", ".js", ".mjs", ".cjs", ".jsx":
		return detectTypeScript(body)
	case ".go":
		// adk-go and samurai both live in .go; only adk-go has a reliable
		// content signature. samurai is left to an explicit --format.
		if containsAnyStr(body, "google.golang.org/adk", "loopagent", "sequentialagent", "adk.") {
			return "adk-go"
		}
		return ""
	case ".json":
		// n8n exports carry node type strings ("n8n-nodes-base.*" / "@n8n/*")
		// near the top of the `nodes` array — reliable inside the sniff window
		// even when the trailing `connections` block sits past it. shingan's own
		// WorkflowGraph JSON is the generic default.
		if containsAnyStr(body, "n8n-nodes-", "@n8n/") {
			return "n8n"
		}
		if strings.Contains(body, "\"nodes\"") && strings.Contains(body, "\"connections\"") {
			return "n8n"
		}
		return "json"
	}
	return ""
}

func detectPython(body string) string {
	switch {
	case strings.Contains(body, "pydantic_graph"):
		return "pydantic-graph"
	case strings.Contains(body, "crewai"):
		return "crewai"
	case containsAnyStr(body, "llama_index", "llamaindex", "llama-index"):
		return "llamaindex"
	case strings.Contains(body, "autogen"):
		return "autogen"
	case containsAnyStr(body, "langgraph", "stategraph"):
		return "langgraph"
	}
	return ""
}

func detectTypeScript(body string) string {
	switch {
	case strings.Contains(body, "@mastra"):
		return "mastra"
	case strings.Contains(body, "@openai/agents"):
		return "openai-agents"
	case containsAnyStr(body, "@langchain/langgraph", "stategraph"):
		return "langgraph-js"
	}
	return ""
}

// detectFormatDir walks dir and returns the most common framework format. A
// generic "json" hit is only used when nothing framework-specific is found, so
// a project that mixes a few config .json files with LangGraph .py still
// resolves to langgraph.
func detectFormatDir(dir string) string {
	counts := map[string]int{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if containsAnyStr(p, "/node_modules/", "/.git/", "/dist/", "/build/") ||
			strings.HasSuffix(p, ".test.ts") || strings.HasSuffix(p, ".test.py") {
			return nil
		}
		if f := detectFormatFile(p); f != "" {
			counts[f]++
		}
		return nil
	})

	best, bestN := "", 0
	for f, n := range counts {
		if f == "json" { // weak fallback — never wins over a framework hit
			continue
		}
		if n > bestN {
			best, bestN = f, n
		}
	}
	if best != "" {
		return best
	}
	if counts["json"] > 0 {
		return "json"
	}
	return ""
}

func readHead(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, n)
	r, _ := f.Read(buf)
	return string(buf[:r])
}

func containsAnyStr(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
