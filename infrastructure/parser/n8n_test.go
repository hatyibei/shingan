package parser_test

import (
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// TestN8nParser_SupportedFormat ensures the parser advertises itself under "n8n".
func TestN8nParser_SupportedFormat(t *testing.T) {
	p := parser.NewN8nParser()
	if got := p.SupportedFormat(); got != "n8n" {
		t.Errorf("SupportedFormat() = %q, want \"n8n\"", got)
	}
}

// TestN8nParser_SimpleChain tests a 3-node linear chain.
// Webhook → ChatGPT → HTTP Request.
// TestN8nParser_SkipsStickyNotes verifies that `n8n-nodes-base.stickyNote`
// nodes — visual annotation widgets in the n8n UI, never executed —
// are dropped from the WorkflowGraph entirely. They are NOT part of
// the workflow but the JSON export carries them indistinguishably from
// real nodes; without this skip every sticky note triggers an
// `unreachable_node` warning.
//
// Dogfood: Zie619/n8n-workflows community sweep — most workflows
// carry 4-12 sticky notes; the random 10-file sample showed each
// would have produced 4-13 unreachable_node FPs without this fix.
func TestN8nParser_SkipsStickyNotes(t *testing.T) {
	input := []byte(`{
		"name": "Workflow with sticky notes",
		"nodes": [
			{"id": "n1", "name": "Webhook",     "type": "n8n-nodes-base.webhook",    "parameters": {}, "position": [0, 0]},
			{"id": "n2", "name": "Process",     "type": "n8n-nodes-base.code",       "parameters": {}, "position": [200, 0]},
			{"id": "n3", "name": "Sticky Note", "type": "n8n-nodes-base.stickyNote", "parameters": {}, "position": [-100, -100]},
			{"id": "n4", "name": "Sticky Note1","type": "n8n-nodes-base.stickyNote", "parameters": {}, "position": [400, 100]}
		],
		"connections": {
			"Webhook": {
				"main": [[{"node": "Process", "type": "main", "index": 0}]]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}
	if _, ok := graph.Nodes["Sticky Note"]; ok {
		t.Errorf("expected Sticky Note to be dropped from WorkflowGraph; nodes=%v", nodeIDList(graph))
	}
	if _, ok := graph.Nodes["Sticky Note1"]; ok {
		t.Errorf("expected Sticky Note1 to be dropped from WorkflowGraph; nodes=%v", nodeIDList(graph))
	}
	if len(graph.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2 (Webhook + Process); nodes=%v", len(graph.Nodes), nodeIDList(graph))
	}
}

func TestN8nParser_SimpleChain(t *testing.T) {
	input := []byte(`{
		"name": "Simple Chain",
		"nodes": [
			{"id": "n1", "name": "Webhook",      "type": "n8n-nodes-base.webhook",     "parameters": {}, "position": [0, 0]},
			{"id": "n2", "name": "ChatGPT",      "type": "n8n-nodes-base.openAi",      "parameters": {"model": "gpt-4o"}, "position": [200, 0]},
			{"id": "n3", "name": "HTTP Request", "type": "n8n-nodes-base.httpRequest", "parameters": {"url": "https://example.com"}, "position": [400, 0]}
		],
		"connections": {
			"Webhook": {
				"main": [[{"node": "ChatGPT", "type": "main", "index": 0}]]
			},
			"ChatGPT": {
				"main": [[{"node": "HTTP Request", "type": "main", "index": 0}]]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(graph.Nodes))
	}
	if len(graph.Edges) != 2 {
		t.Errorf("len(Edges) = %d, want 2", len(graph.Edges))
	}

	// Webhook should be detected as the entry node (trigger / no incoming).
	if graph.EntryNodeID != "Webhook" {
		t.Errorf("EntryNodeID = %q, want \"Webhook\"", graph.EntryNodeID)
	}

	// Check NodeType mappings.
	checkNodeType(t, graph, "Webhook", domain.NodeTypeTool)
	checkNodeType(t, graph, "ChatGPT", domain.NodeTypeLLM)
	checkNodeType(t, graph, "HTTP Request", domain.NodeTypeTool)
}

// TestN8nParser_OpenAiNodeType validates n8n-nodes-base.openAi maps to NodeTypeLLM.
func TestN8nParser_OpenAiNodeType(t *testing.T) {
	input := []byte(`{
		"name": "OpenAI Test",
		"nodes": [
			{"id": "1", "name": "OpenAI",   "type": "n8n-nodes-base.openAi",   "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "Anthropic","type": "n8n-nodes-base.anthropic","parameters": {}, "position": [0, 0]},
			{"id": "3", "name": "Gemini",   "type": "n8n-nodes-base.gemini",   "parameters": {}, "position": [0, 0]},
			{"id": "4", "name": "ChatGPT",  "type": "@n8n/n8n-nodes-langchain.lmChatOpenAi", "parameters": {}, "position": [0, 0]}
		],
		"connections": {}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	checkNodeType(t, graph, "OpenAI", domain.NodeTypeLLM)
	checkNodeType(t, graph, "Anthropic", domain.NodeTypeLLM)
	checkNodeType(t, graph, "Gemini", domain.NodeTypeLLM)
	checkNodeType(t, graph, "ChatGPT", domain.NodeTypeLLM)
}

// TestN8nParser_HttpRequestTool validates httpRequest maps to NodeTypeTool with category=api.
func TestN8nParser_HttpRequestTool(t *testing.T) {
	input := []byte(`{
		"name": "HTTP Test",
		"nodes": [
			{"id": "1", "name": "HTTP Request", "type": "n8n-nodes-base.httpRequest", "parameters": {"url": "https://api.example.com"}, "position": [0, 0]}
		],
		"connections": {}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	checkNodeType(t, graph, "HTTP Request", domain.NodeTypeTool)
	checkConfig(t, graph, "HTTP Request", "category", "api")
}

// TestN8nParser_IfCondition validates if maps to NodeTypeCondition with two
// branch edges labeled "true"/"false" from main[0]/main[1] respectively.
func TestN8nParser_IfCondition(t *testing.T) {
	input := []byte(`{
		"name": "If Test",
		"nodes": [
			{"id": "1", "name": "Trigger", "type": "n8n-nodes-base.manualTrigger", "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "If",      "type": "n8n-nodes-base.if",     "parameters": {}, "position": [0, 0]},
			{"id": "3", "name": "OnTrue",  "type": "n8n-nodes-base.set",    "parameters": {}, "position": [0, 0]},
			{"id": "4", "name": "OnFalse", "type": "n8n-nodes-base.set",    "parameters": {}, "position": [0, 0]}
		],
		"connections": {
			"Trigger": {
				"main": [[{"node": "If", "type": "main", "index": 0}]]
			},
			"If": {
				"main": [
					[{"node": "OnTrue",  "type": "main", "index": 0}],
					[{"node": "OnFalse", "type": "main", "index": 0}]
				]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	checkNodeType(t, graph, "If", domain.NodeTypeCondition)

	// We expect 1 trigger→if edge plus 2 if→branch edges with conditions.
	if len(graph.Edges) != 3 {
		t.Errorf("len(Edges) = %d, want 3", len(graph.Edges))
	}

	var trueEdge, falseEdge *domain.Edge
	for i := range graph.Edges {
		e := &graph.Edges[i]
		if e.From == "If" && e.To == "OnTrue" {
			trueEdge = e
		}
		if e.From == "If" && e.To == "OnFalse" {
			falseEdge = e
		}
	}
	if trueEdge == nil {
		t.Fatal("missing edge If → OnTrue")
	}
	if falseEdge == nil {
		t.Fatal("missing edge If → OnFalse")
	}
	if trueEdge.Condition != "true" {
		t.Errorf("If→OnTrue Condition = %q, want \"true\"", trueEdge.Condition)
	}
	if falseEdge.Condition != "false" {
		t.Errorf("If→OnFalse Condition = %q, want \"false\"", falseEdge.Condition)
	}
}

// TestN8nParser_Branching validates that a single source can fan out to multiple
// destinations through one output port.
func TestN8nParser_Branching(t *testing.T) {
	input := []byte(`{
		"name": "Branching",
		"nodes": [
			{"id": "1", "name": "Source",  "type": "n8n-nodes-base.manualTrigger", "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "Branch1", "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]},
			{"id": "3", "name": "Branch2", "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]},
			{"id": "4", "name": "Branch3", "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]}
		],
		"connections": {
			"Source": {
				"main": [[
					{"node": "Branch1", "type": "main", "index": 0},
					{"node": "Branch2", "type": "main", "index": 0},
					{"node": "Branch3", "type": "main", "index": 0}
				]]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if len(graph.Edges) != 3 {
		t.Errorf("len(Edges) = %d, want 3", len(graph.Edges))
	}
	count := 0
	for _, e := range graph.Edges {
		if e.From == "Source" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("outgoing edges from Source = %d, want 3", count)
	}
}

// TestN8nParser_EmptyConnections validates that nodes without connections are
// preserved (so unreachable_node can fire on detached subgraphs).
func TestN8nParser_EmptyConnections(t *testing.T) {
	input := []byte(`{
		"name": "Empty Connections",
		"nodes": [
			{"id": "1", "name": "Trigger",  "type": "n8n-nodes-base.manualTrigger", "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "Connected","type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]},
			{"id": "3", "name": "Orphan",   "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]}
		],
		"connections": {
			"Trigger": {
				"main": [[{"node": "Connected", "type": "main", "index": 0}]]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(graph.Nodes))
	}
	if len(graph.Edges) != 1 {
		t.Errorf("len(Edges) = %d, want 1", len(graph.Edges))
	}

	// Trigger is the entry; Orphan is detached so unreachable_node should fire downstream.
	if graph.EntryNodeID != "Trigger" {
		t.Errorf("EntryNodeID = %q, want \"Trigger\"", graph.EntryNodeID)
	}
	if _, ok := graph.Nodes["Orphan"]; !ok {
		t.Error("Orphan node should be retained in graph for unreachable_node to fire")
	}
}

// TestN8nParser_InvalidJSON validates parse error on malformed input.
func TestN8nParser_InvalidJSON(t *testing.T) {
	p := parser.NewN8nParser()
	_, err := p.Parse([]byte(`{invalid json`))
	if err == nil {
		t.Fatal("Parse() expected error on invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "n8n parser") {
		t.Errorf("error message should mention n8n parser, got: %v", err)
	}
}

// TestN8nParser_AIAgentNode validates @n8n/n8n-nodes-langchain.* node types
// (agent / tool / chain / model) are mapped sensibly.
func TestN8nParser_AIAgentNode(t *testing.T) {
	input := []byte(`{
		"name": "AI Agent",
		"nodes": [
			{"id": "1", "name": "Agent",        "type": "@n8n/n8n-nodes-langchain.agent",       "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "ChatModel",    "type": "@n8n/n8n-nodes-langchain.lmChatOpenAi","parameters": {}, "position": [0, 0]},
			{"id": "3", "name": "PromptChain",  "type": "@n8n/n8n-nodes-langchain.chainLlm",    "parameters": {}, "position": [0, 0]}
		],
		"connections": {}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	checkNodeType(t, graph, "Agent", domain.NodeTypeLLM)
	checkNodeType(t, graph, "ChatModel", domain.NodeTypeLLM)
	checkNodeType(t, graph, "PromptChain", domain.NodeTypeLLM)
}

// TestN8nParser_NodeIDFromName validates that a node's "name" field becomes
// its Node.ID in the resulting WorkflowGraph (n8n connections key by name).
func TestN8nParser_NodeIDFromName(t *testing.T) {
	input := []byte(`{
		"name": "ID Test",
		"nodes": [
			{"id": "uuid-1234", "name": "MyCustomName", "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]}
		],
		"connections": {}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if _, ok := graph.Nodes["MyCustomName"]; !ok {
		t.Errorf("expected Node.ID to be name (\"MyCustomName\"), got nodes: %v", nodeIDs(graph))
	}
	if _, found := graph.Nodes["uuid-1234"]; found {
		t.Error("Node.ID should be name, not n8n internal id")
	}
}

// TestN8nParser_CodeNode validates code/function nodes map to NodeTypeTool with
// category=code_execution so eval_missing rule can fire when reachable from LLM.
func TestN8nParser_CodeNode(t *testing.T) {
	input := []byte(`{
		"name": "Code",
		"nodes": [
			{"id": "1", "name": "Code",        "type": "n8n-nodes-base.code",           "parameters": {"jsCode": "return items"}, "position": [0, 0]},
			{"id": "2", "name": "Function",    "type": "n8n-nodes-base.function",       "parameters": {}, "position": [0, 0]},
			{"id": "3", "name": "ExecCommand", "type": "n8n-nodes-base.executeCommand", "parameters": {}, "position": [0, 0]}
		],
		"connections": {}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	for _, name := range []string{"Code", "Function", "ExecCommand"} {
		checkNodeType(t, graph, name, domain.NodeTypeTool)
		checkConfig(t, graph, name, "category", "code_execution")
	}
}

// TestN8nParser_DisabledNodeSkipped validates disabled nodes are not added to graph.
func TestN8nParser_DisabledNodeSkipped(t *testing.T) {
	input := []byte(`{
		"name": "Disabled",
		"nodes": [
			{"id": "1", "name": "Active",   "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "Disabled", "type": "n8n-nodes-base.set", "parameters": {}, "position": [0, 0], "disabled": true}
		],
		"connections": {
			"Active": {
				"main": [[{"node": "Disabled", "type": "main", "index": 0}]]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if _, found := graph.Nodes["Disabled"]; found {
		t.Error("disabled node should be skipped from graph")
	}
	if _, found := graph.Nodes["Active"]; !found {
		t.Error("active node should be retained")
	}
	// Edge to disabled node should be dropped.
	for _, e := range graph.Edges {
		if e.To == "Disabled" {
			t.Errorf("edge to disabled node should be dropped, got %v", e)
		}
	}
}

// TestN8nParser_TriggerEntryDetection validates that webhook/trigger nodes are
// preferred as entry node even if they appear later in the array.
func TestN8nParser_TriggerEntryDetection(t *testing.T) {
	input := []byte(`{
		"name": "Trigger Entry",
		"nodes": [
			{"id": "1", "name": "Process", "type": "n8n-nodes-base.set",     "parameters": {}, "position": [0, 0]},
			{"id": "2", "name": "Webhook", "type": "n8n-nodes-base.webhook", "parameters": {}, "position": [0, 0]}
		],
		"connections": {
			"Webhook": {
				"main": [[{"node": "Process", "type": "main", "index": 0}]]
			}
		}
	}`)

	p := parser.NewN8nParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if graph.EntryNodeID != "Webhook" {
		t.Errorf("EntryNodeID = %q, want \"Webhook\" (trigger should win over array order)", graph.EntryNodeID)
	}
}

// nodeIDs returns the ID list for diagnostic messages.
func nodeIDs(g *domain.WorkflowGraph) []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	return ids
}
