# Security Policy

## サポートバージョン

| Version | Supported |
|---------|-----------|
| 0.1.x   | ✓ |
| < 0.1   | ✗ |

## 脆弱性報告

Shingan自体に脆弱性を発見した場合、**GitHub Issueでは報告しないでください**。以下の方法で非公開に連絡してください:

1. GitHub Security Advisory (推奨): https://github.com/hatyibei/shingan/security/advisories/new
2. メール: hathibei7@gmail.com （件名に `[Shingan Security]` を含める）

報告には以下を含めてください:
- 脆弱性の種類 (例: RCE, path traversal, injection)
- 再現手順
- 影響範囲 (どのバイナリ/機能に影響するか)
- 可能なら修正提案

## 対応SLA

- 24時間以内に受領確認
- 7日以内に初回トリアージ
- Critical: 14日以内に修正・リリース
- High/Medium: 30日以内
- Low: 次期マイナーリリース

## 既知のセキュリティ事項

### Vertex AI ADC認証

`shingan-runner` / `shingan-web` は Google Cloud Application Default Credentials (ADC) を使用します。

- 認証情報は `~/.config/gcloud/application_default_credentials.json` に保存される
- バイナリ自体は認証情報を保持しない
- Docker実行時は `-v ~/.config/gcloud:/home/nonroot/.config/gcloud:ro` でマウントする想定

### shingan-web の middleware

- `POST /api/run` / `POST /api/run_sse` を解析ブロックの対象
- 認証機構は **現状なし** (localhost想定)
- プロダクション展開時は前段に認証プロキシを配置してください

### 静的解析の限界

Shinganは**静的解析**ツールで、実行時の非決定的挙動 (LLMのハルシネーション、ネットワーク故障) は検出できません。ランタイム観測 (LangSmith等) との併用が前提です。

## Responsible Disclosure

報告者には修正リリースのクレジット (CHANGELOG + Security Advisory) を付与します。希望しない場合は報告時にその旨を明記してください。
