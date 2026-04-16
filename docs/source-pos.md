# SourcePos — Node 位置情報 (Phase 2 Foundation)

> Phase 2 の LSP サーバ・VS Code 拡張・CodeAction はすべて
> `domain.Node.Pos` に載った位置情報を読み取って動く。本ドキュメントは
> その契約・意図・今後の拡張ポイントをまとめる。

## 設計意図

Phase 1 までの `domain.Finding` は `NodeID` しか位置情報を持たず、
IDE 上の該当行ハイライトや "Go to Finding" 相当の操作ができなかった。
Phase 2 で次の UX を実現するには、Parser が各 `Node` にソース位置を
付与する必要がある:

- LSP `textDocument/publishDiagnostics` の `range`
- LSP `textDocument/codeAction` の編集対象レンジ
- VS Code Code Lens (`▶ Analyze` / `⚡ Quick Fix all`) の注入位置
- SARIF レポートの `locations[].physicalLocation`

これらはすべて「`Node` が書かれた行・列」を知らないと出せない。
位置情報は `Finding` ではなく **`Node` 側** に載せる — 同じノードに
複数の Finding が立っても、位置は一度で済む。

## 型定義

`domain/graph.go`:

```go
type SourcePos struct {
    File string `json:"file,omitempty"` // パーサ定義。埋め込み入力では空を許容
    Line int    `json:"line,omitempty"` // 1-based
    Col  int    `json:"col,omitempty"`  // 1-based
}

func (p SourcePos) IsZero() bool {
    return p.File == "" && p.Line == 0 && p.Col == 0
}

type Node struct {
    ID     string         `json:"id"`
    Name   string         `json:"name"`
    Type   NodeType       `json:"type"`
    Config map[string]any `json:"config,omitempty"`
    Pos    SourcePos      `json:"pos,omitempty"` // NEW in 0.6.0
}
```

### `IsZero()` 規則

- **原則**: Parser が位置を確定できない場合は Pos をゼロ値のままにする
- **消費者の責務**: `SourcePos.IsZero()` で position 情報の有無を判定し、
  ゼロであれば range 情報を省く / ファイル全体を対象にする等フォールバック
- ゼロ判定は「3 フィールドすべて空」で行う。`File=""` かつ `Line=0` かつ
  `Col=0` のときのみ "未設定" 扱い

## Parser ごとの埋め方

### ADK-Go Parser (`infrastructure/parser/adkgo.go`)

AST ベース。既に `token.FileSet` を保持しているので `cl.Pos()` や
`ident.Pos()` を `b.sourcePos(pos)` ヘルパー経由で `SourcePos` に変換:

```go
func (b *adkgoBuilder) sourcePos(pos token.Pos) domain.SourcePos {
    if !pos.IsValid() {
        return domain.SourcePos{}
    }
    p := b.fset.Position(pos)
    return domain.SourcePos{File: p.Filename, Line: p.Line, Col: p.Column}
}
```

埋め込み箇所:

- `processAgentLit` — bare struct literal (`&LlmAgent{...}` 等) は `cl.Pos()`
- `processRealAPIConfig` — real SDK (`loopagent.New(loopagent.Config{...})`)
  は `cfg.Pos()` で Config 複合リテラルを指す (フォールバックで `callExpr.Pos()`)
- `processToolElement` — tool ノードの識別子 / 式の `Pos()`
- `extractRealSubAgents`, `processSubAgent` — 未解決 ident の placeholder
  ノードは `ident.Pos()`

`Parse([]byte)` ではファイル名に `"input.go"` がハードコードされるため、
テストはその値を検証する。`ParseFile(path)` は types pass がサクセスした
場合のみ実ファイルパスが Filename に載る。

### JSON Parser (`infrastructure/parser/json.go`)

コード変更なし。`domain.Node` の `Pos` フィールドに `json:"pos,omitempty"`
タグが付いているので:

- 入力 JSON に `"pos": {"file": ..., "line": ..., "col": ...}` があれば自動デコード
- `"pos"` フィールドがなければ `SourcePos{}` (IsZero)
- 既存 v0.5.0 以前の testdata は未修正で動く (backward compatible)

### SamuraiAI Parser (`infrastructure/parser/samurai.go`)

想定スキーマ段階なので、入力 JSON に位置情報がある前提にしない。
`SamuraiNode` に optional な `Pos *domain.SourcePos \`json:"pos,omitempty"\``
を持たせ、入力に含まれていれば `domain.Node.Pos` にコピー。実際の
SamuraiAI スキーマ確定後に map して上書きする。

## 今後の拡張

- `domain.Finding` に `Range` を追加 (`StartPos`, `EndPos`) — 現状の
  `Pos` は単点情報、LSP は range が欲しいのでノード宣言の始端・終端を
  取る必要がある。Phase 2-F の CodeAction 本格実装で追加予定
- Python (LangGraph) Parser — `python -m shingan_export <file>` が
  返す JSON に `pos` フィールドを埋めてもらう契約でそのまま動く
- LSP サーバ — `SourcePos.IsZero()` で `publishDiagnostics` の range を
  「ファイル先頭 0 列」にフォールバックする。ゼロ値だった場合でも診断
  自体は返す(「位置不明だが発火した」は情報として有用)

## backward compatibility

- 既存 JSON testdata は `pos` フィールドを持たない — 全テストが
  green のまま (`TestJSONParser_NoPosField_BackwardCompat` が gating)
- 既存の consumer (Reporter / Orchestrator) は `Pos` を無視して動作 —
  `SourcePos` はあくまで optional な追加情報
