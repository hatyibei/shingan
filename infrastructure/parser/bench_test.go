package parser_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// generateADKGoSource generates valid ADK-Go source bytes containing a
// SequentialAgent with n LlmAgent sub-agents. This produces source that
// the ADKGoParser can parse, with roughly n*200 bytes per agent.
func generateADKGoSource(n int) []byte {
	var sb strings.Builder
	sb.WriteString("package agents\n\n")
	sb.WriteString("var workflow = &SequentialAgent{\n")
	sb.WriteString(`	Name: "benchmark_workflow",` + "\n")
	sb.WriteString("	SubAgents: []Agent{\n")
	for i := 0; i < n; i++ {
		sb.WriteString(fmt.Sprintf(
			"\t\t&LlmAgent{Name: \"agent_%04d\", Model: \"gpt-4o-mini\", Instruction: \"Step %d: process the input and produce output.\"},\n",
			i, i,
		))
	}
	sb.WriteString("	},\n}\n")
	return []byte(sb.String())
}

// generateJSONSource generates valid Shingan JSON bytes for a workflow with n nodes
// arranged in a linear chain.
func generateJSONSource(n int) []byte {
	type jsonNode struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type jsonEdge struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	type jsonGraph struct {
		Nodes       []jsonNode `json:"nodes"`
		Edges       []jsonEdge `json:"edges"`
		EntryNodeID string     `json:"entry_node_id"`
	}

	nodes := make([]jsonNode, n)
	edges := make([]jsonEdge, 0, n-1)
	types := []string{"llm", "tool", "control", "human", "output"}

	for i := 0; i < n; i++ {
		nodes[i] = jsonNode{
			ID:   fmt.Sprintf("node_%04d", i),
			Name: fmt.Sprintf("node_%04d", i),
			Type: types[i%len(types)],
		}
		if i > 0 {
			edges = append(edges, jsonEdge{
				From: fmt.Sprintf("node_%04d", i-1),
				To:   fmt.Sprintf("node_%04d", i),
			})
		}
	}

	g := jsonGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "node_0000",
	}
	data, err := json.Marshal(g)
	if err != nil {
		panic(fmt.Sprintf("generateJSONSource: marshal error: %v", err))
	}
	return data
}

// generateJSONFromDomainGraph converts a domain.WorkflowGraph to Shingan JSON bytes.
// This is used when you need a realistic graph (not just a chain).
func generateJSONFromDomainGraph(g *domain.WorkflowGraph) []byte {
	type jsonNode struct {
		ID     string         `json:"id"`
		Name   string         `json:"name"`
		Type   domain.NodeType `json:"type"`
		Config map[string]any `json:"config,omitempty"`
	}
	type jsonEdge struct {
		From      string `json:"from"`
		To        string `json:"to"`
		Condition string `json:"condition,omitempty"`
	}
	type jsonGraph struct {
		Nodes       []jsonNode `json:"nodes"`
		Edges       []jsonEdge `json:"edges"`
		EntryNodeID string     `json:"entry_node_id"`
	}

	nodes := make([]jsonNode, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, jsonNode{
			ID:     n.ID,
			Name:   n.Name,
			Type:   n.Type,
			Config: n.Config,
		})
	}
	edges := make([]jsonEdge, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = jsonEdge{From: e.From, To: e.To, Condition: e.Condition}
	}

	out, err := json.Marshal(jsonGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: g.EntryNodeID,
	})
	if err != nil {
		panic(fmt.Sprintf("generateJSONFromDomainGraph: marshal error: %v", err))
	}
	return out
}

// --- ADKGo Parser benchmarks ---

func BenchmarkADKGoParser_N10(b *testing.B) {
	src := generateADKGoSource(10)
	p := parser.NewADKGoParser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(src)
	}
}

func BenchmarkADKGoParser_N100(b *testing.B) {
	src := generateADKGoSource(100)
	p := parser.NewADKGoParser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(src)
	}
}

func BenchmarkADKGoParser_N1000(b *testing.B) {
	src := generateADKGoSource(1000)
	p := parser.NewADKGoParser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(src)
	}
}

// --- JSON Parser benchmarks ---

func BenchmarkJSONParser_N10(b *testing.B) {
	src := generateJSONSource(10)
	p := parser.NewJSONParser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(src)
	}
}

func BenchmarkJSONParser_N100(b *testing.B) {
	src := generateJSONSource(100)
	p := parser.NewJSONParser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(src)
	}
}

func BenchmarkJSONParser_N1000(b *testing.B) {
	src := generateJSONSource(1000)
	p := parser.NewJSONParser()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(src)
	}
}
