package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFormat_File(t *testing.T) {
	cases := []struct {
		name, ext, body, want string
	}{
		{"n8n_export", ".json", `{"name":"x","nodes":[{"type":"n8n-nodes-base.set","name":"S"}],"connections":{}}`, "n8n"},
		{"n8n_langchain_node", ".json", `{"nodes":[{"type":"@n8n/n8n-nodes-langchain.agent"}]}`, "n8n"},
		{"shingan_json", ".json", `{"entry_node_id":"a","nodes":{"a":{"type":"LLM"}}}`, "json"},
		{"langgraph_py", ".py", "from langgraph.graph import StateGraph\n", "langgraph"},
		{"crewai_py", ".py", "from crewai import Agent, Crew\n", "crewai"},
		{"pydantic_graph_py", ".py", "from pydantic_graph import Graph, BaseNode\n", "pydantic-graph"},
		{"llamaindex_py", ".py", "from llama_index.core.workflow import Workflow\n", "llamaindex"},
		{"autogen_py", ".py", "from autogen import GroupChat\n", "autogen"},
		{"langgraphjs_ts", ".ts", `import { StateGraph } from "@langchain/langgraph";`, "langgraph-js"},
		{"mastra_ts", ".ts", `import { Mastra } from "@mastra/core";`, "mastra"},
		{"openai_agents_ts", ".ts", `import { Agent } from "@openai/agents";`, "openai-agents"},
		{"adk_go", ".go", "package main\nimport \"google.golang.org/adk/agents\"\n", "adk-go"},
		{"unknown_md", ".md", "# readme", ""},
		{"plain_go", ".go", "package main\nfunc main() {}\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "f"+c.ext)
			if err := os.WriteFile(p, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := detectFormat(p); got != c.want {
				t.Errorf("detectFormat(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

// n8n's `connections` block can sit past the 16KB sniff window in a large
// export; the node-type signature near the top must still resolve it to n8n.
func TestDetectFormat_LargeN8nExport(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.json")
	var b []byte
	b = append(b, []byte(`{"nodes":[{"type":"n8n-nodes-base.httpRequest","name":"H"}`)...)
	for i := 0; i < 4000; i++ {
		b = append(b, []byte(`,{"type":"n8n-nodes-base.set","name":"pad","parameters":{"k":"vvvvvvvvvv"}}`)...)
	}
	b = append(b, []byte(`],"connections":{}}`)...)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectFormat(p); got != "n8n" {
		t.Errorf("large n8n export detected as %q, want n8n", got)
	}
}

// A directory mixing a few config .json with framework files must resolve to
// the framework, not the generic json fallback.
func TestDetectFormat_DirPrefersFramework(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"entry_node_id":"a"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "graph.py"), []byte("from langgraph.graph import StateGraph"), 0o600)
	os.WriteFile(filepath.Join(dir, "agent.py"), []byte("from langgraph.graph import StateGraph"), 0o600)
	if got := detectFormat(dir); got != "langgraph" {
		t.Errorf("dir detected as %q, want langgraph", got)
	}
}

func TestDetectFormat_Missing(t *testing.T) {
	if got := detectFormat(filepath.Join(t.TempDir(), "nope.py")); got != "" {
		t.Errorf("missing path = %q, want empty", got)
	}
}
