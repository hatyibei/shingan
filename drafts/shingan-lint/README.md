# shingan-lint PR / Issue Draft Templates

`shingan-lint` (npm wrapper + CLI) への外部コントリビューション用ドラフト集。

これらは `hatyibei/shingan` (本リポジトリ) ではなく、`shingan-lint` を配布している側のリポジトリへ送ることを想定したテンプレートです。

## 推奨送付順

初手は摩擦の小さい docs / エラーメッセージ改善から。

| # | 種別 | タイトル | ファイル |
|---|------|---------|---------|
| 0 | Issue | npx wrapper exits silently when binary download fails | `issues/00-npm-wrapper-silent-exit.md` |
| 1 | PR (docs) | document manual binary cache setup | `prs/01-docs-npm-manual-cache.md` |
| 2 | PR (fix) | print actionable error when binary is missing | `prs/02-fix-npm-actionable-error.md` |
| 3 | PR (docs) | add JSON workflow input examples | `prs/03-docs-cli-json-examples.md` |
| 4 | PR (feat) | add `--version` flag and `version` command | `prs/04-feat-cli-version.md` |
| 5 | PR (feat) | add `doctor` diagnostic command | `prs/05-feat-cli-doctor.md` |

ここまでが「最初に出すと効く5本」。

## 残りのテンプレート

### npm wrapper / 配布

| # | タイトル | ファイル |
|---|---------|---------|
| 6 | feat(npm): retry binary download at runtime | `prs/06-feat-npm-runtime-download.md` |
| 7 | feat(npm): verify release artifact checksums | `prs/07-feat-npm-checksum.md` |
| 8 | feat(npm): document proxy env vars | `prs/08-feat-npm-proxy-envs.md` |
| 9 | feat(cli): `--print-binary-path` | `prs/09-feat-cli-print-binary-path.md` |

### CLI

| # | タイトル | ファイル |
|---|---------|---------|
| 10 | feat(cli): `init` command for JSON template | `prs/10-feat-cli-init.md` |
| 11 | feat(cli): `validate` command | `prs/11-feat-cli-validate.md` |
| 12 | feat(cli): `--fail-on` severity threshold | `prs/12-feat-cli-fail-on.md` |
| 13 | fix(cli): document and standardize exit codes | `prs/13-fix-cli-exit-codes.md` |
| 14 | feat(cli): directory input for JSON workflows | `prs/14-feat-cli-directory-input-json.md` |
| 15 | fix(cli): clarify directory format error | `prs/15-fix-cli-directory-error-message.md` |
| 16 | feat(cli): `--strict-schema` | `prs/16-feat-cli-strict-schema.md` |
| 17 | feat(cli): `--list-formats` | `prs/17-feat-cli-list-formats.md` |
| 18 | feat(cli): `explain --finding` extension | `prs/18-feat-cli-explain-finding.md` |

### Output / Schema

| # | タイトル | ファイル |
|---|---------|---------|
| 19 | feat(json): publish JSON Schema | `prs/19-feat-json-schema.md` |
| 20 | feat(output): secret redaction in findings | `prs/20-feat-output-redact-secrets.md` |
| 21 | feat(output): include rule documentation URIs | `prs/21-feat-output-rule-doc-links.md` |

### Docs / Examples

| # | タイトル | ファイル |
|---|---------|---------|
| 22 | docs(ci): GitHub Actions examples | `prs/22-docs-ci-github-actions.md` |
| 23 | docs(policy): `.shingan.yaml` examples | `prs/23-docs-policy-yaml-examples.md` |
| 24 | docs(rules): before/after for built-in rules | `prs/24-docs-rules-before-after.md` |
| 25 | docs(langgraph): document import requirement | `prs/25-docs-langgraph-requirements.md` |
| 26 | docs(examples): sample workflows directory | `prs/26-docs-examples-sample-workflows.md` |

### 大物 (Issue先行推奨)

| # | タイトル | ファイル |
|---|---------|---------|
| 27 | feat(langgraph): static discovery without import | `prs/27-feat-langgraph-static.md` |

### Positioning / マーケティング

| # | タイトル | ファイル |
|---|---------|---------|
| 28 | docs(positioning): pre-deployment static analysis tagline | `prs/28-docs-positioning.md` |

## トーンと進め方

- 初手は Issue で「この方向で良いか」確認してからPR
- PRは小さく、既存挙動を壊さない
- Test Plan を本文に含める
- 「設計が悪い」など強い言い方は避ける
- 大量整形差分は混ぜない
