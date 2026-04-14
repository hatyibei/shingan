# testdata/samurai — SamuraiAI テストデータ

## 注意: このスキーマは想定版

このディレクトリ内の JSON ファイルは、**SamuraiAI 社内スキーマの想定版**である。

- 実際の SamuraiAI ワークフロー JSON スキーマは**非公開**
- ADR Appendix B の SamuraiAI ↔ ADK-Go ノードマッピング表をもとに推定して作成
- 入社後に公式スキーマを確認し、`infrastructure/parser/samurai.go` の構造体定義を差し替える

## full.json — 意図的に混入したバグ

Shingan の解析ルールが発火することを確認するため、以下のバグを意図的に含む:

| バグ | 箇所 | 期待する Finding |
|---|---|---|
| `max_iterations` 未設定のループ | `node_loop` (type: loop) | CycleDetector: Critical — LoopGuard が発火 |
| `node_browser` 直後に条件分岐なし | `node_browser → node_connector` | ErrorHandlerChecker: Critical |
| `node_connector` / `node_api` / `node_code` 後にエラーハンドリング欠落 | 各 Tool ノード | ErrorHandlerChecker: Warning/Info |
| `node_auto_judge` が `node_condition` からのループバックで重複呼出 | loop→auto_judge→condition→loop | RedundantLLMDetector の検討対象 |

## ファイル一覧

| ファイル | 説明 |
|---|---|
| `full.json` | Appendix B 全14ノード型を含むフルサンプル（意図的バグあり） |

## 差し替え手順

実スキーマが判明したとき:

1. `infrastructure/parser/samurai.go` 内の `SamuraiWorkflow`, `SamuraiNode`, `SamuraiEdge` 構造体を実スキーマに合わせて更新
2. `mapSamuraiNodeType()` のマッピング表を実ノード名に合わせて更新
3. このディレクトリのテストデータを実 JSON サンプルに置き換え
4. `go test ./infrastructure/parser/...` を実行して全テスト GREEN を確認

`domain/` 層・`application/` 層の変更は不要。
