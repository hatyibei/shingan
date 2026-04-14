# ADK Web UI 統合 — 技術設計と面接トーク台本

## 概要

`cmd/shingan-web` は Google ADK の Web UI (`localhost:8080/ui/`) を起動し、
Run API (`/api/run`, `/api/run_sse`) に Shingan の静的解析ガード middleware を注入する。
SamuraiAI のような GUI ワークフローエディタ上で "実行前ガード" がどう見えるかを視覚的にデモする。

---

## アーキテクチャ

```
Browser
  │
  ├── GET  /ui/**           → ADK Web UI (Angular SPA, embedded)
  │
  └── POST /api/run         → shinganGuardMiddleware
      POST /api/run_sse          │
                                 ├── [Critical findings?] → 403 + JSON
                                 │     { "error": "shingan_guard",
                                 │       "agent": "infinite_loop_unbounded",
                                 │       "findings": [...] }
                                 │
                                 └── [Clean] → ADK REST API → Vertex AI Gemini
```

---

## middleware 注入ポイント

ADK の Web launcher は `web.BuildBaseRouter()` でベースの gorilla/mux ルーターを生成し、
`Sublauncher.SetupSubrouters(router, config)` でルートを追加する。

`router.Use(middleware)` は **それ以降に追加されるルートすべてに適用** される。
そのため、次の順序で組み立てることで ADK API の前に Shingan を割り込ませる:

```go
router := web.BuildBaseRouter()
router.Use(shinganGuardMiddleware(sourceMap))  // ← Shingan を先に登録

apiSL.SetupSubrouters(router, config)          // ← ADK REST API を後から追加
webuiSL.SetupSubrouters(router, config)        // ← Web UI を後から追加
```

---

## Run API パス (実測)

ADK REST API の runtime ルーターは以下を登録する:

| Method | Path | 用途 |
|---|---|---|
| POST | `/api/run` | 非ストリーミング実行 |
| POST | `/api/run_sse` | SSE ストリーミング実行 |

Agent 名は URL パスではなくリクエスト **ボディ** の `appName` フィールドに含まれる:

```json
{
  "appName": "infinite_loop_unbounded",
  "userId": "user1",
  "sessionId": "session1",
  "newMessage": { "role": "user", "parts": [{"text": "hello"}] }
}
```

middleware でボディを読んだ後は必ず `io.NopCloser(bytes.NewReader(...))` で復元する。

---

## Agent 名とソースファイルのマッピング

静的解析対象ファイルは `agentSourceMap` でハードコードする (v0.1):

```go
var agentSourceMap = map[string]string{
    "infinite_loop_unbounded": "examples/runtime/infinite_loop_unbounded.go",
    "infinite_loop_bounded":   "examples/runtime/infinite_loop_bounded.go",
    "simple_hello":            "examples/runtime/simple_agent.go",
}
```

パスは `go.mod` を起点に解決する (`findProjectRoot()`)。

---

## 403 レスポンス形式

```json
{
  "error": "shingan_guard",
  "agent": "infinite_loop_unbounded",
  "findings": [
    {
      "rule": "loop_guard",
      "severity": "critical",
      "nodeId": "unbounded_loop",
      "message": "LoopAgent \"unbounded_loop\" has no MaxIterations configured — potential infinite loop",
      "suggestion": "Set MaxIterations to a bounded value (recommended: 3-10 for testing, 50-100 for production)"
    }
  ]
}
```

---

## 面接でのトーク台本 (30-60秒)

> 「SamuraiAI は GUI ワークフローエディタですよね。
> このデモでは Google 公式の ADK Web UI を SamuraiAI に見立てて、
> Shingan をミドルウェアとして Run API の手前に挟んでいます。
>
> 画面左の `infinite_loop_unbounded` を選んでメッセージを送ると、
> Shingan が実行前にソースを AST 解析して MaxIterations の欠落を検出し、
> 403 でブロックします。Gemini には一切リクエストが届きません。
>
> 隣の `infinite_loop_bounded` は MaxIterations=3 があるので解析がパスし、
> Vertex AI から実際の応答が返ってきます。
>
> 企業向けエージェントは一度動くと副作用が不可逆なので、
> 『実行前に機械的に止める』というレイヤーの価値を視覚的に示せます。」

---

## 起動手順

```bash
# 1. ビルド
cd /path/to/shingan
go build -o shingan-web ./cmd/shingan-web

# 2. 起動
bash scripts/web-demo.sh

# 3. ブラウザで http://localhost:8080 を開く
```

### curl での動作確認

```bash
# セッション作成
SESSION=$(curl -s -X POST http://localhost:8080/api/apps/infinite_loop_unbounded/users/u1/sessions \
  -H 'Content-Type: application/json' -d '{}' | jq -r '.id')

# infinite_loop_unbounded → 403 期待
curl -s -X POST http://localhost:8080/api/run \
  -H 'Content-Type: application/json' \
  -d "{\"appName\":\"infinite_loop_unbounded\",\"userId\":\"u1\",\"sessionId\":\"$SESSION\",\"newMessage\":{\"role\":\"user\",\"parts\":[{\"text\":\"hello\"}]}}" \
  | jq .

# infinite_loop_bounded → 200 期待  
SESSION2=$(curl -s -X POST http://localhost:8080/api/apps/infinite_loop_bounded/users/u1/sessions \
  -H 'Content-Type: application/json' -d '{}' | jq -r '.id')
curl -s -X POST http://localhost:8080/api/run \
  -H 'Content-Type: application/json' \
  -d "{\"appName\":\"infinite_loop_bounded\",\"userId\":\"u1\",\"sessionId\":\"$SESSION2\",\"newMessage\":{\"role\":\"user\",\"parts\":[{\"text\":\"hello\"}]}}" \
  | jq '.[] | .content.parts[].text'
```

---

## 制約と今後の課題

| 制約 | 対処方針 |
|---|---|
| Agent 名とソースファイルの対応がハードコード | v0.2: Agent 登録時にメタデータとして渡す動的マップ |
| SSE ストリーミング (`/api/run_sse`) も同じ middleware でガード | 現実装でも同パスにマッチするため問題なし |
| ADK Web UI のエラー表示はデフォルトのアラートダイアログ | デモとして十分; v0.2 でカスタム表示検討 |
| `infinite_loop_unbounded` のモデル未設定 | Shingan がブロックするため Gemini に届かない。正常動作 |

---

## ファイル構成

```
cmd/shingan-web/
├── main.go        # エントリポイント; router組立・server起動
├── agents.go      # 3種のデモAgent構築; Vertex AI Geminiモデル初期化
├── middleware.go  # shinganGuardMiddleware; 403ブロック処理
├── analyzer.go    # Shinganコア呼び出し; sourceMap解決
├── main_test.go   # middleware ユニットテスト (7ケース)
└── README.md      # 起動手順・デモ手順

scripts/
└── web-demo.sh    # ワンコマンド起動スクリプト (chmod 755)

docs/
└── adk-webui-integration.md  # 本ドキュメント
```

---

## v0.2.0 解析精度向上: go/types セカンドパス

v0.2.0 で `infrastructure/parser/adkgo.go` に `go/types` ベースのセカンドパスを追加した。
`ADKGoParser.ParseFile(path)` 経由で呼ぶと、`packages.Load` で型情報付きロードを行い、
`functiontool.New[TArgs, TResults](...)` の TArgs 型引数を `types.Info.Instances` から取得する。

TArgs の struct 名・フィールド名（例: `browserArgs{Query string}` → "browser"）を使って
既存の名前ベースのカテゴリ推定を補強するため、middleware での Tool 種別判定がより正確になる。
`packages.Load` が失敗した場合（ネットワーク非接続、go.sum 不足など）は自動的に AST-only にフォールバックする。
