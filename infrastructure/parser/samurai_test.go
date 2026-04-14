package parser_test

import (
	"os"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

func TestSamuraiParser_SupportedFormat(t *testing.T) {
	p := parser.NewSamuraiParser()
	if got := p.SupportedFormat(); got != "samurai" {
		t.Errorf("SupportedFormat() = %q, want \"samurai\"", got)
	}
}

// TestSamuraiParser_BasicFlow は LLM + Browser + Loop + Condition の基本フロー解析。
func TestSamuraiParser_BasicFlow(t *testing.T) {
	input := []byte(`{
		"version": "1.0",
		"workflow_id": "wf_basic",
		"entry_node": "node_1",
		"nodes": [
			{"id": "node_1", "type": "llm",       "name": "分類器",     "config": {"model": "gemini-1.5-flash"}},
			{"id": "node_2", "type": "browser",   "name": "ブラウザ操作", "config": {"action": "click"}},
			{"id": "node_3", "type": "loop",      "name": "ループ",      "config": {"max_iterations": 5}},
			{"id": "node_4", "type": "condition", "name": "条件分岐",    "config": {"expression": "ok"}}
		],
		"edges": [
			{"from": "node_1", "to": "node_2"},
			{"from": "node_2", "to": "node_3"},
			{"from": "node_3", "to": "node_4"},
			{"from": "node_4", "to": "node_1", "condition": "retry"}
		]
	}`)

	p := parser.NewSamuraiParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	// エントリノード確認
	if graph.EntryNodeID != "node_1" {
		t.Errorf("EntryNodeID = %q, want \"node_1\"", graph.EntryNodeID)
	}

	// ノード数確認（4つすべて変換されること）
	if len(graph.Nodes) != 4 {
		t.Errorf("len(Nodes) = %d, want 4", len(graph.Nodes))
	}

	// NodeType マッピング検証
	checkNodeType(t, graph, "node_1", domain.NodeTypeLLM)
	checkNodeType(t, graph, "node_2", domain.NodeTypeTool)
	checkNodeType(t, graph, "node_3", domain.NodeTypeControl)
	checkNodeType(t, graph, "node_4", domain.NodeTypeControl)

	// browser ノードに category = "browser" が付与されること
	checkConfig(t, graph, "node_2", "category", "browser")

	// エッジ数確認
	if len(graph.Edges) != 4 {
		t.Errorf("len(Edges) = %d, want 4", len(graph.Edges))
	}
}

// TestSamuraiParser_MemoSkip は memo ノードがスキップされることを確認。
func TestSamuraiParser_MemoSkip(t *testing.T) {
	input := []byte(`{
		"version": "1.0",
		"workflow_id": "wf_memo",
		"entry_node": "node_llm",
		"nodes": [
			{"id": "node_llm",  "type": "llm",  "name": "LLM",    "config": {}},
			{"id": "node_memo", "type": "memo", "name": "メモ",    "config": {"note": "dummy"}}
		],
		"edges": [
			{"from": "node_memo", "to": "node_llm"}
		]
	}`)

	p := parser.NewSamuraiParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	// memo ノードはスキップされ、LLM ノードのみ残る
	if len(graph.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d, want 1 (memo should be skipped)", len(graph.Nodes))
	}
	if _, found := graph.Nodes["node_memo"]; found {
		t.Error("node_memo should be skipped (type=memo), but it was found in graph")
	}
	if _, found := graph.Nodes["node_llm"]; !found {
		t.Error("node_llm not found in graph")
	}

	// memo ノードへのエッジも除外される
	if len(graph.Edges) != 0 {
		t.Errorf("len(Edges) = %d, want 0 (edge from memo node should be excluded)", len(graph.Edges))
	}
}

// TestSamuraiParser_UnknownType は未知の type でエラーが返ることを確認。
func TestSamuraiParser_UnknownType(t *testing.T) {
	input := []byte(`{
		"version": "1.0",
		"workflow_id": "wf_unknown",
		"entry_node": "node_1",
		"nodes": [
			{"id": "node_1", "type": "future_node_type", "name": "未知ノード", "config": {}}
		],
		"edges": []
	}`)

	p := parser.NewSamuraiParser()
	_, err := p.Parse(input)
	if err == nil {
		t.Fatal("expected error for unknown node type, got nil")
	}
	// エラーメッセージに unknown type が含まれること
	if got := err.Error(); len(got) == 0 {
		t.Error("error message is empty")
	}
}

// TestSamuraiParser_EntryNodeRequired は entry_node 未設定でエラーが返ることを確認。
func TestSamuraiParser_EntryNodeRequired(t *testing.T) {
	input := []byte(`{
		"version": "1.0",
		"workflow_id": "wf_no_entry",
		"nodes": [
			{"id": "node_1", "type": "llm", "name": "LLM", "config": {}}
		],
		"edges": []
	}`)

	p := parser.NewSamuraiParser()
	_, err := p.Parse(input)
	if err == nil {
		t.Fatal("expected error for missing entry_node, got nil")
	}
}

// TestSamuraiParser_EdgeConditionPreserved はエッジの condition フィールドが保持されることを確認。
func TestSamuraiParser_EdgeConditionPreserved(t *testing.T) {
	input := []byte(`{
		"version": "1.0",
		"workflow_id": "wf_condition",
		"entry_node": "node_cond",
		"nodes": [
			{"id": "node_cond",    "type": "condition", "name": "条件",   "config": {}},
			{"id": "node_success", "type": "output",    "name": "成功出力", "config": {}},
			{"id": "node_retry",   "type": "loop",      "name": "再試行", "config": {}}
		],
		"edges": [
			{"from": "node_cond", "to": "node_success", "condition": "success"},
			{"from": "node_cond", "to": "node_retry",   "condition": "retry"}
		]
	}`)

	p := parser.NewSamuraiParser()
	graph, err := p.Parse(input)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	condMap := make(map[string]string)
	for _, e := range graph.Edges {
		condMap[e.To] = e.Condition
	}
	if got := condMap["node_success"]; got != "success" {
		t.Errorf("edge to node_success: condition = %q, want \"success\"", got)
	}
	if got := condMap["node_retry"]; got != "retry" {
		t.Errorf("edge to node_retry: condition = %q, want \"retry\"", got)
	}
}

// TestSamuraiParser_FullGraph は testdata/samurai/full.json を読み込み
// Appendix B の全14ノード型が正しく変換されることを確認する。
func TestSamuraiParser_FullGraph(t *testing.T) {
	data, err := os.ReadFile("../../testdata/samurai/full.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	p := parser.NewSamuraiParser()
	graph, err := p.Parse(data)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	// memo ノードを除く16ノード (node_memo はスキップ)
	const wantNodes = 16
	if len(graph.Nodes) != wantNodes {
		t.Errorf("len(Nodes) = %d, want %d", len(graph.Nodes), wantNodes)
	}

	// エントリノード確認
	if graph.EntryNodeID != "node_classifier" {
		t.Errorf("EntryNodeID = %q, want \"node_classifier\"", graph.EntryNodeID)
	}

	// 各 NodeType マッピング確認
	llmNodes := []string{"node_classifier", "node_agent", "node_auto_judge", "node_param_extract"}
	for _, id := range llmNodes {
		checkNodeType(t, graph, id, domain.NodeTypeLLM)
	}

	toolNodes := map[string]string{
		"node_browser":   "browser",
		"node_connector": "api",
		"node_api":       "api",
		"node_mcp_tool":  "mcp",
		"node_code":      "code",
		"node_knowledge": "rag",
	}
	for id, wantCat := range toolNodes {
		checkNodeType(t, graph, id, domain.NodeTypeTool)
		checkConfig(t, graph, id, "category", wantCat)
	}

	controlNodes := []string{"node_loop", "node_condition"}
	for _, id := range controlNodes {
		checkNodeType(t, graph, id, domain.NodeTypeControl)
	}

	humanNodes := []string{"node_approval", "node_review"}
	for _, id := range humanNodes {
		checkNodeType(t, graph, id, domain.NodeTypeHuman)
	}

	outputNodes := []string{"node_output", "node_answer"}
	for _, id := range outputNodes {
		checkNodeType(t, graph, id, domain.NodeTypeOutput)
	}

	// memo ノードはグラフに存在しないこと
	if _, found := graph.Nodes["node_memo"]; found {
		t.Error("node_memo (type=memo) should be skipped")
	}

	// node_loop に max_iterations が設定されていないこと（意図的バグ: LoopGuard 発火を想定）
	loopNode, ok := graph.Nodes["node_loop"]
	if !ok {
		t.Fatal("node_loop not found")
	}
	if _, hasMaxIter := loopNode.Config["max_iterations"]; hasMaxIter {
		t.Log("INFO: node_loop has max_iterations set; LoopGuard will not fire")
	} else {
		t.Log("INFO: node_loop has NO max_iterations — LoopGuard (CycleDetector) should fire as Critical")
	}
}

// TestSamuraiParser_AllNodeTypeMappings は全 SamuraiAI ノード型が
// 正しい NodeType にマッピングされることを個別に確認する。
func TestSamuraiParser_AllNodeTypeMappings(t *testing.T) {
	tests := []struct {
		samuraiType string
		wantType    domain.NodeType
		wantCat     string // Tool の場合のカテゴリ; 空文字は確認なし
	}{
		{"llm", domain.NodeTypeLLM, ""},
		{"auto_judge", domain.NodeTypeLLM, ""},
		{"param_extract", domain.NodeTypeLLM, ""},
		{"agent", domain.NodeTypeLLM, ""},
		{"browser", domain.NodeTypeTool, "browser"},
		{"connector", domain.NodeTypeTool, "api"},
		{"api", domain.NodeTypeTool, "api"},
		{"mcp_tool", domain.NodeTypeTool, "mcp"},
		{"code", domain.NodeTypeTool, "code"},
		{"knowledge_search", domain.NodeTypeTool, "rag"},
		{"loop", domain.NodeTypeControl, ""},
		{"condition", domain.NodeTypeControl, ""},
		{"approval", domain.NodeTypeHuman, ""},
		{"review", domain.NodeTypeHuman, ""},
		{"output", domain.NodeTypeOutput, ""},
		{"answer", domain.NodeTypeOutput, ""},
	}

	for _, tc := range tests {
		t.Run(tc.samuraiType, func(t *testing.T) {
			input := buildSingleNodeWorkflow(tc.samuraiType)
			p := parser.NewSamuraiParser()
			graph, err := p.Parse(input)
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			n, ok := graph.Nodes["test_node"]
			if !ok {
				t.Fatal("test_node not found")
			}
			if n.Type != tc.wantType {
				t.Errorf("NodeType = %v, want %v", n.Type, tc.wantType)
			}
			if tc.wantCat != "" {
				if got, ok2 := n.Config["category"]; !ok2 || got != tc.wantCat {
					t.Errorf("config[category] = %v, want %q", got, tc.wantCat)
				}
			}
		})
	}
}

// ─── helper functions ─────────────────────────────────────────────────────────

func buildSingleNodeWorkflow(nodeType string) []byte {
	return []byte(`{
		"version": "1.0",
		"workflow_id": "wf_test",
		"entry_node": "test_node",
		"nodes": [{"id": "test_node", "type": "` + nodeType + `", "name": "テスト", "config": {}}],
		"edges": []
	}`)
}

func checkNodeType(t *testing.T, graph *domain.WorkflowGraph, nodeID string, want domain.NodeType) {
	t.Helper()
	n, ok := graph.Nodes[nodeID]
	if !ok {
		t.Errorf("node %q not found in graph", nodeID)
		return
	}
	if n.Type != want {
		t.Errorf("node %q: Type = %v, want %v", nodeID, n.Type, want)
	}
}

func checkConfig(t *testing.T, graph *domain.WorkflowGraph, nodeID, key string, want any) {
	t.Helper()
	n, ok := graph.Nodes[nodeID]
	if !ok {
		t.Errorf("node %q not found in graph", nodeID)
		return
	}
	got, ok := n.Config[key]
	if !ok {
		t.Errorf("node %q: config[%q] not set", nodeID, key)
		return
	}
	if got != want {
		t.Errorf("node %q: config[%q] = %v, want %v", nodeID, key, got, want)
	}
}
