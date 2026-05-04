// Package parser provides WorkflowParser implementations for different input formats.
//
// このファイルは SamuraiAI ワークフロー JSON パーサーを実装する。
//
// ⚠️  スキーマ注意事項:
//   - このスキーマは【想定版】である。実際の SamuraiAI 社内スキーマは非公開のため、
//     ADR Appendix B のマッピングに基づく推定で定義している。
//   - 入社後に公式スキーマを取得し、SamuraiWorkflow / SamuraiNode 構造体を
//     差し替えるだけで対応が完了する予定。
//   - このパーサーの存在自体が、Onion Architecture による「アダプター追加だけで
//     新フレームワーク対応可能」の実証である。domain/ 層は一行も変更していない。
package parser

import (
	"encoding/json"
	"fmt"

	"github.com/hatyibei/shingan/domain"
)

// ─── SamuraiAI JSON schema structs (想定版) ──────────────────────────────────
//
// 実スキーマ差し替え時はここだけ変更する。domain 層・application 層は無変更。

// SamuraiWorkflow は SamuraiAI ワークフロー JSON のルート構造体。
type SamuraiWorkflow struct {
	Version    string         `json:"version"`
	WorkflowID string         `json:"workflow_id"`
	EntryNode  string         `json:"entry_node"`
	Nodes      []SamuraiNode  `json:"nodes"`
	Edges      []SamuraiEdge  `json:"edges"`
}

// SamuraiNode は SamuraiAI の単一ノード定義。
type SamuraiNode struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"` // "llm", "browser", "loop", "condition", etc.
	Name   string            `json:"name"`
	Config map[string]any    `json:"config"`        // フレームワーク固有の設定
	Pos    *domain.SourcePos `json:"pos,omitempty"` // optional source position (future-schema compat)
}

// SamuraiEdge は SamuraiAI のエッジ定義。
type SamuraiEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Condition string `json:"condition,omitempty"` // 空文字 = 無条件
}

// ─── SamuraiParser ────────────────────────────────────────────────────────────

// SamuraiParser は SamuraiAI JSON 形式の WorkflowParser 実装。
// SupportedFormat() = "samurai"
type SamuraiParser struct{}

// NewSamuraiParser は初期化済みの SamuraiParser を返す。
func NewSamuraiParser() *SamuraiParser {
	return &SamuraiParser{}
}

// SupportedFormat implements application.WorkflowParser.
func (p *SamuraiParser) SupportedFormat() string {
	return "samurai"
}

// Parse は SamuraiAI JSON バイト列を受け取り domain.WorkflowGraph に変換して返す。
//
// 変換規則 (ADR Appendix B に基づく):
//   - "llm" / "auto_judge" / "param_extract" / "agent" → NodeTypeLLM
//   - "browser" / "connector" / "api" / "mcp_tool" / "code" / "knowledge_search" → NodeTypeTool
//   - "loop"      → NodeTypeLoop      (max_iterations 必須)
//   - "condition" → NodeTypeCondition (max_iterations 不要)
//   - "approval" / "review" → NodeTypeHuman
//   - "output" / "answer" → NodeTypeOutput
//   - "memo" → スキップ（実行時無視ノード）
//   - その他 → エラー返却（握りつぶし禁止）
func (p *SamuraiParser) Parse(input []byte) (*domain.WorkflowGraph, error) {
	var sw SamuraiWorkflow
	if err := json.Unmarshal(input, &sw); err != nil {
		return nil, fmt.Errorf("samurai parser: unmarshal: %w", err)
	}

	if sw.EntryNode == "" {
		return nil, fmt.Errorf("samurai parser: entry_node is required but not set")
	}

	// ノード変換
	nodes := make(map[string]*domain.Node, len(sw.Nodes))
	for _, sn := range sw.Nodes {
		// "memo" はスキップ（設計時の付箋、実行時無視）
		if sn.Type == "memo" {
			continue
		}

		nodeType, category, err := mapSamuraiNodeType(sn.Type)
		if err != nil {
			return nil, fmt.Errorf("samurai parser: node %q: %w", sn.ID, err)
		}

		cfg := make(map[string]any, len(sn.Config)+1)
		for k, v := range sn.Config {
			cfg[k] = v
		}
		// Tool ノードにはカテゴリを付加（browser / api / mcp / code / rag）
		if category != "" {
			cfg["category"] = category
		}

		node := &domain.Node{
			ID:     sn.ID,
			Name:   sn.Name,
			Type:   nodeType,
			Config: cfg,
		}
		if sn.Pos != nil {
			node.Pos = *sn.Pos
		}
		nodes[sn.ID] = node
	}

	// エッジ変換（memo ノードへの/からのエッジは自動除外）
	edges := make([]domain.Edge, 0, len(sw.Edges))
	for _, se := range sw.Edges {
		// 変換後のノードマップにない ID はスキップ（memo ノードへの接続等）
		if _, fromOK := nodes[se.From]; !fromOK {
			continue
		}
		if _, toOK := nodes[se.To]; !toOK {
			continue
		}
		edges = append(edges, domain.Edge{
			From:      se.From,
			To:        se.To,
			Condition: se.Condition,
		})
	}

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: sw.EntryNode,
	}, nil
}

// mapSamuraiNodeType は SamuraiAI ノード type 文字列を
// domain.NodeType と (Tool の場合は) カテゴリ文字列にマッピングする。
//
// 対応表 (ADR Appendix B):
//
//	SamuraiAI type         → NodeType          category
//	──────────────────────────────────────────────────────
//	llm                    → NodeTypeLLM        ""
//	auto_judge             → NodeTypeLLM        ""   (Intent 分類)
//	param_extract          → NodeTypeLLM        ""   (構造化データ抽出)
//	agent                  → NodeTypeLLM        ""   (自律エージェント)
//	browser                → NodeTypeTool       "browser"
//	connector / api        → NodeTypeTool       "api"
//	mcp_tool               → NodeTypeTool       "mcp"
//	code                   → NodeTypeTool       "code"
//	knowledge_search       → NodeTypeTool       "rag"
//	loop                   → NodeTypeLoop       ""
//	condition              → NodeTypeCondition  ""
//	approval / review      → NodeTypeHuman      ""
//	output / answer        → NodeTypeOutput     ""
//	memo                   → (caller でスキップ)
func mapSamuraiNodeType(t string) (domain.NodeType, string, error) {
	switch t {
	// LLM 系
	case "llm", "auto_judge", "param_extract", "agent":
		return domain.NodeTypeLLM, "", nil

	// Tool 系（外部 I/O）
	case "browser":
		return domain.NodeTypeTool, "browser", nil
	case "connector", "api":
		return domain.NodeTypeTool, "api", nil
	case "mcp_tool":
		return domain.NodeTypeTool, "mcp", nil
	case "code":
		return domain.NodeTypeTool, "code", nil
	case "knowledge_search":
		return domain.NodeTypeTool, "rag", nil

	// 制御系
	case "loop":
		return domain.NodeTypeLoop, "", nil
	case "condition":
		return domain.NodeTypeCondition, "", nil

	// Human-in-the-loop
	case "approval", "review":
		return domain.NodeTypeHuman, "", nil

	// 出力系
	case "output", "answer":
		return domain.NodeTypeOutput, "", nil

	default:
		return 0, "", fmt.Errorf("unknown SamuraiAI node type %q: update mapSamuraiNodeType when real schema is available", t)
	}
}
