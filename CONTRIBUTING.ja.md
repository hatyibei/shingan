> 🌐 Language: [English](./CONTRIBUTING.md) | **日本語**

# Contributing to Shingan

ShinganはAIエージェントワークフロー静的解析ツールです。新ルール追加、パーサー拡張、パフォーマンス改善など、ContributionはWelcomeです。

## 開発環境

- Go 1.25以上
- make
- (optional) Vertex AI/Gemini 動作確認用にGCPプロジェクトとADC

```bash
git clone https://github.com/hatyibei/shingan.git
cd shingan
go mod tidy
go test -race ./...
go build -o shingan ./cmd/shingan
```

## アーキテクチャ原則

**Onion Architecture** 厳守。層構造は以下のみ許容:

```
cmd/  → infrastructure/  → application/  → domain/
```

- `domain/` は**外部依存ゼロ**（標準ライブラリのみ）
- `application/` は `domain/` のみimport可
- `infrastructure/` が `application/` で定義された interface の具象実装
- 逆方向の依存を作ると即座にリジェクト

詳細は [docs/architecture.md](./docs/architecture.md)。

## 新ルール追加の手順

詳しい手順 + tier 振り分け + ConfidenceReason 選び方は [docs/rule-authoring.md](./docs/rule-authoring.md) を参照。要約:

1. README の解析ルール表で既出でないか確認
2. Issue 起票 (`enhancement`, `new-rule` ラベル)
3. `domain/rules/<rule_id>.go` に `LocalRule` / `PathRule` / `GlobalRule` interface 実装 (ADR-007)
4. `domain/rules/<rule_id>_test.go` に最低 5 ケース (positive / negative / edge / Reason stamp / Meta)
5. `init()` で `registerBuiltin(NewYourRule())` を呼ぶ — factory 編集不要
6. `domain/testutil/generate.go` に generator + `cmd/shingan-gen/main.go` に pattern 追加
7. `docs/rules/<rule-id>.md` 新規 + README ルール表更新
8. `cmd/shingan-mcp/explain.go` に説明追加 (parity 維持)
9. `make lint && go test -race ./...` グリーン

## PRを出す前に

```bash
go vet ./...
go test -race ./...
go test -race -tags=e2e ./...   # CLI/API/Runner E2E
go build ./cmd/...               # 全バイナリビルド確認
```

## コミットメッセージ

Conventional Commits推奨:
- `feat:` 新機能
- `fix:` バグ修正
- `docs:` ドキュメントのみ
- `test:` テスト追加
- `refactor:` 挙動を変えないリファクタ
- `ci:` CI/ビルド変更

## ライセンス

Contributionは [MIT License](./LICENSE) の下で提供されます。

## 行動規範

相互尊重、建設的議論。技術的な指摘は遠慮なく、人格攻撃はNG。
