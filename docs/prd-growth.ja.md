# Shingan 成長戦略 PRD — npm DL・ユーザー数・GitHub Stars 拡大

```
ステータス: Draft v1.0 (2026-06-10)
作成者:    hatyibei
関連 ADR:  ADR-019 (成長・配布チャネル戦略)
対象期間:  2026-06 〜 2027-06 (v0.9.x → v1.0 → post-1.0)
```

> 本 PRD は「何を・なぜ・どの順で」やるかの実行計画を扱う。その骨格となる
> チャネル選定・テレメトリ・サイト戦略の意思決定は
> [ADR-019](../shingan-adr.md#adr-019) を参照。

---

## 1. 背景と課題

### 現状

- `shingan-lint` v0.9.1 を npm に公開済み(2026-06-04)。Beta、v1.0 は 2026 年後半目標。
- メンテナ 1 名、約 4 週間で 50 コミットの開発速度。
- プロダクト表面は既に広い: 6 フレームワーク対応(LangGraph / CrewAI / n8n /
  ADK-Go / JSON / Samurai)+ PoC 6 パーサー、20+ ルール、LSP / MCP / VS Code
  拡張、GitHub Action + SARIF、Plugin SDK (`experimental:`)、diff / baseline /
  severity-policy による段階導入パス。
- 品質の証拠も既にある: 実 OSS リポジトリ(gpt-researcher 24K★ 等)への
  dogfood 実績([case-studies](./case-studies/))、Critical 偽陽性ゼロの
  追跡記録([benchmarks](./benchmarks.ja.md))。

### 課題

**プロダクトの深さに対して、認知と「最初の価値到達までの摩擦」が
ボトルネックになっている。** 機能不足ではなく、見つけてもらえない・
試す導線が npm/GitHub README 以外にない・採用後の定着を測る手段がない、
が制約。

### なぜ今か

- OWASP Agentic AI Top 10 (2025) 公開直後で「エージェントセキュリティ」
  カテゴリの認知が立ち上がり中。静的解析ニッチは実質無競合
  ([comparison](./comparison.ja.md) 参照: FlowLint は n8n 限定、
  LangSmith/Langfuse はランタイム専用)。
- カテゴリ確立期に「このカテゴリのデフォルトツール」の座を取れるかが
  12 ヶ月で決まる。後発の Semgrep 等がエージェントルールを追加する前に
  先行者ポジションを固める必要がある。

---

## 2. ターゲットペルソナ

| | P1: LangGraph / CrewAI 個人開発者 | P2: プラットフォーム / AppSec チーム | P3: OSS エージェント FW メンテナ・テンプレート作者 |
|---|---|---|---|
| 課題 | エージェントが無限ループ・コスト爆発しないか不安。レビュー観点が分からない | 組織内に増殖するエージェントワークフローの統制。監査対応 (OWASP / SOC 2) | 自分のリポジトリの品質シグナルが欲しい。example が壊れていないか CI で守りたい |
| 入口 | `npx --yes shingan-lint demo`(30 秒・セットアップ不要)→ IDE (LSP / MCP) | SARIF + GitHub Code Scanning、OWASP マッピング表、公開 FP ベンチマーク | GitHub Action + バッジ |
| 成功状態 | 5 分以内に自分のワークフローで finding が出て、IDE か CI に組み込む | informational CI (`continue-on-error`) を組織標準として展開 | 上流リポジトリに Shingan CI が入りバッジが付く(= アドボカシーチャネル化) |
| 主 KPI | npm 週次 DL、Stars | Action 利用リポジトリ数 | 上流採用リポジトリ数 |

ADR-011 で主戦場を LangGraph と定めた通り、**P1 が最優先**。P3 は獲得人数は
少ないが 1 件あたりの波及(その OSS の利用者全員への露出)が最大で、
dogfood 済み case-studies をそのまま転用できる。P2 は v0.10(PR bot /
org dashboard)と公開 FP ベンチマークが揃うまで本格攻略しない(ADR-019
選択肢 C 却下理由)。

---

## 3. 現状ファネル分析(Discover → Try → Adopt → Retain → Advocate)

| 段階 | 現有資産 | ギャップ |
|---|---|---|
| **Discover** | README(日英)、badges、[comparison](./comparison.ja.md)、OWASP マッピング表、dogfood 実績テーブル | デモ GIF / 動画なし。shingan.dev 未稼働。GitHub Marketplace / VS Code Marketplace 未掲載。技術記事ゼロ。awesome リスト未掲載 |
| **Try** | `npx --yes shingan-lint demo`(ゼロインストール・30 秒)、Docker、go install、pre-commit hook(2026-06-10 追加) | Homebrew なし。`demo` の存在自体が README を読まないと分からない |
| **Adopt** | GitHub Action + SARIF + sticky PR コメント、`.shingan.yaml`、`--baseline` / `--since` 段階導入 | セットアップが手作業(`shingan init` がない)。推奨が informational CI 止まりで「習慣化」の強制力が弱い |
| **Retain** | LSP / MCP の日常導線、リリース速度 | 定着を測る手段ゼロ(テレメトリなし)。v0.10 PR bot が最大のリテンション機能 |
| **Advocate** | case-studies 3 本、Critical FP ゼロ記録 | 「scanned with Shingan」バッジなし。上流 OSS への CI 採用 PR 未実施。testimonial なし。コミュニティルールゼロ |

**最弱リンクは Discover。** Try 以降の体験(npx demo → Action → LSP)は
既に競争力があるため、施策は「露出の獲得」と「露出 → demo 実行の転換率」に
重点配分する。

---

## 4. North Star メトリクスと KPI ツリー

### North Star Metric (NSM)

**「週次で Shingan が解析したワークフロー数」**。ただしテレメトリ非導入
(ADR-019 決定 2)のため直接計測不能。次の 2 つを代理指標とする:

1. **npm 週次ダウンロード数**(`shingan-lint`)
2. **GitHub Action 利用リポジトリ数**(自リポジトリ除く)

### KPI ツリー

```
NSM: 週次解析ワークフロー数 (代理: npm週次DL + Action利用リポジトリ数)
├── 獲得 (Acquisition)
│   ├── GitHub Stars / README トラフィック (Insights)
│   ├── 記事・ベンチマーク経由の流入 (referrer)
│   └── Marketplace 掲載面 (GitHub Action / VS Code / MCP レジストリ)
├── 活性化 (Activation)
│   ├── README → `npx demo` 実行への転換 (DL 数の非CIスパイクで近似)
│   └── `shingan init` 完了 (v0.10〜)
├── 定着 (Retention) — 代理指標のみ
│   ├── Action 利用リポジトリのバージョン追従ラグ
│   ├── npm DL の週次反復パターン (CI 定期実行の証跡)
│   └── issue / discussion のリピーター率
└── 紹介 (Referral)
    ├── 上流 OSS リポジトリへの CI 採用数 + バッジ掲出数
    └── コミュニティ plugin / rule 数 (ADR-010)
```

---

## 5. KPI 目標値(3 / 6 / 12 ヶ月)

単独メンテナの OSS として現実的な水準で設定する。Base = 計画施策のみで
到達すべき値、Stretch = HN / 上流採用などの非線形イベントが当たった場合。

| KPI | 現在 (2026-06, 推定) | 3ヶ月 (2026-09) Base / Stretch | 6ヶ月 (2026-12, v1.0 期) Base / Stretch | 12ヶ月 (2027-06) Base / Stretch | 計測元 |
|---|---|---|---|---|---|
| GitHub Stars | < 100 | 250 / 400 | 600 / 1,000 | 1,500 / 3,000 | GitHub API / Insights |
| npm 週次 DL | < 200 | 500 / 800 | 1,500 / 2,500 | 4,000 / 8,000 | api.npmjs.org |
| Action 利用リポジトリ (自分以外) | ~0 | 10 / 20 | 40 / 80 | 150 / 300 | GitHub code search |
| VS Code 拡張インストール数 | 未公開 | 公開 + 100 | 500 | 2,000 | VS Code Marketplace |
| コミュニティ plugin / rule 数 | 0 | 1 | 3 | 10 | リポジトリ / registry 調査 |
| 外部コントリビュータ (累計) | ~0 | 3 | 8 | 20 | GitHub contributors |

> **注意**: npm DL は CI の再インストールで水増しされる(キャッシュなしの
> Action 実行は毎回 DL にカウント)。絶対値ではなく**トレンドと比率**
> (DL / Action 利用リポジトリ数)で読むこと。Stars は虚栄指標になりうる —
> NSM はあくまで「解析されたワークフロー数」であり、Stars はその先行指標
> としてのみ扱う。

---

## 6. 施策一覧(優先度付き)

凡例 — 工数: S (〜半日) / M (〜3日) / L (1週間〜)。インパクト: 高 / 中 / 低。
**原則: S 工数 × 高インパクトから着手し、メンテナ 1 名の持続可能性を守る**
(リスク §9-1)。

### 6.1 配布・オンボーディング改善(npm DL 直結)

| # | 施策 | 工数 | インパクト | ファネル | 備考 |
|---|---|---|---|---|---|
| 1 | README 冒頭にデモ GIF / asciinema(`npx demo` の 30 秒) | S | 高 | Discover→Try | 露出→実行の転換率を直接押し上げる最安の一手 |
| 2 | GitHub Marketplace に Action 掲載 | S | 高 | Discover | `action.yml` は完成済み。README の v1.0 予定を前倒し (ADR-019) |
| 3 | Homebrew tap (`hatyibei/homebrew-shingan`) | S | 高 | Try | GoReleaser `brews:` の設定追加のみ。Go/CLI ユーザーの標準導線 |
| 4 | pre-commit hook (`.pre-commit-hooks.yaml`) | S | 中 | Adopt | Python (LangGraph / CrewAI) ユーザーの標準習慣に同乗。**実装済み (2026-06-10)** — [docs/pre-commit.md](./pre-commit.md) |
| 5 | VS Code Marketplace に拡張公開 | M | 中 | Discover→Retain | `extensions/vscode-shingan/` 既存。v0.10 タイミング (ADR-019) |
| - | winget / scoop / apt | - | 低 | - | 見送り (ADR-019)。需要シグナルが出るまで追加しない |

### 6.2 コミュニティ・認知施策(Stars 直結)

| # | 施策 | 工数 | インパクト | ファネル | 備考 |
|---|---|---|---|---|---|
| 1 | 公開 FP ベンチマークレポート(OSS 100+ ワークフロー)を旗艦コンテンツとして公開 | M | 高 | Discover/Advocate | v0.9 ロードマップ既定項目。P2 攻略と上流 PR (6.2-3) の信頼基盤を兼ねる |
| 2 | リリース毎に技術記事 1 本(Zenn + dev.to)。v0.10 PR bot 公開時に Show HN / r/LangChain 投稿 | M | 高 | Discover | 大型機能と同時でないと HN は跳ねない。v0.10 を「打ち上げの瞬間」に設計 (§7) |
| 3 | dogfood 済み OSS 2〜3 リポジトリへ Shingan CI 追加の上流 PR +「scanned with Shingan」バッジ | M | 高 | Advocate | case-studies を配布チャネルに転換。FP ベンチマーク公開後・min-confidence 高設定で実施(評判リスク §9-2) |
| 4 | good-first-issue 整備 + CONTRIBUTING からの導線強化 | S | 中 | Advocate | 外部コントリビュータ KPI に直結 |
| 5 | awesome-langgraph / awesome-llm-security 等への掲載 PR | S | 中 | Discover | 1 回きりの S 工数で恒久的な被リンク |

### 6.3 プロダクト機能(v0.10+ ロードマップ、リテンション直結)

| # | 施策 | 工数 | インパクト | ファネル | 備考 |
|---|---|---|---|---|---|
| 1 | PR bot(変更ノードへのインラインコメント) | L | 高 | Retain | v0.10 既定。informational → blocking 昇格の前提。打ち上げコンテンツ (6.2-2) と同期 |
| 2 | `shingan init` — `.shingan.yaml` + GitHub workflow 雛形をワンコマンド生成 | M | 高 | Adopt | 「README を読んで手作業」を排除。activation の主仕掛け |
| 3 | baseline / severity-policy の blocking CI 昇格パス完成 | M | 高 | Retain | README が自認する operational gap の解消。v1.0 ゲート |
| 4 | org dashboard(コスト / PII / cycle メトリクスの時系列) | L | 中 | Retain (P2) | v0.10+ 維持。P2 本格攻略の前提だが先行投資しない (ADR-019 選択肢 C 却下) |

### 6.4 エコシステム連携

| # | 施策 | 工数 | インパクト | ファネル | 備考 |
|---|---|---|---|---|---|
| 1 | plugin-template の推進 + コミュニティルール最初の 3 件を伴走支援 | M | 中 | Advocate | ADR-010 の `experimental:` 戦略の実地展開。ESLint 型エコシステムの種 |
| 2 | MCP レジストリ(Claude / Cursor 系ディレクトリ)へ shingan-mcp 掲載 | S | 中 | Discover | エージェント開発者の新導線。掲載のみで維持コストほぼゼロ |
| 3 | LangGraph templates / n8n community への相互掲載 | M | 中 | Discover | FW 公式エコシステム面への露出 |
| 4 | PoC 6 パーサー(langgraph-js / Mastra / OpenAI Agents / AutoGen / LlamaIndex / pydantic-graph)の GA 昇格を需要シグナル駆動に | - | 中 | Adopt | issue 数・記事反応・DL referrer で順序決定。先回りで全部 GA 化しない |

---

## 7. マイルストーン整合(v0.9 → v1.0)

既存ロードマップ(README)に成長施策を重ねる。**機能リリースと認知施策を
同じ瞬間に束ねる**のが原則(単独のマーケ活動はしない)。

| 時期 | プロダクト(既定ロードマップ) | 成長施策(本 PRD) |
|---|---|---|
| **v0.9.x**(現在〜2026 Q3) | Mastra parser、30+ ルール、Plugin SDK preview | 配布クイックウィン全部(6.1-1〜4)、FP ベンチマーク公開(6.2-1)、awesome 掲載(6.2-5)、MCP レジストリ(6.4-2) |
| **v0.10**(2026 Q4) | PR bot、org dashboard 着手 | **メイン打ち上げ**: Show HN + 記事(6.2-2)、上流 OSS PR(6.2-3)、VS Code Marketplace(6.1-5)、`shingan init`(6.3-2) |
| **v1.0**(2026 後半) | Plugin SDK GA、5+ FW × 25+ ルール、安定性保証 | 「production-ready」宣言記事、blocking CI 昇格ガイド(6.3-3)、コミュニティルール 3 件(6.4-1) |
| **post-1.0**(2027) | - | P2(AppSec)本格攻略、テレメトリ再検討(ADR-019 決定 2 の見直し条件) |

---

## 8. 計測プラン

| 指標 | 取得方法 | 頻度 |
|---|---|---|
| npm 週次 DL | `https://api.npmjs.org/downloads/point/last-week/shingan-lint` | 週次 |
| Stars / トラフィック / referrer | GitHub Insights(**14 日しか遡れない**ため月次スナップショット必須) | 月次 |
| Action 利用リポジトリ | GitHub code search: `uses: hatyibei/shingan` (`path:.github/workflows`) | 月次 |
| GHCR pull 数 | GHCR パッケージページ | 月次 |
| VS Code 拡張インストール | Marketplace 管理画面 | 月次(公開後) |

スナップショットは **tracking issue に追記**して記録する(リポジトリへの
コミットはしない — 計測データでコード履歴を汚さない)。

**テレメトリは導入しない**(v0.10 まで。ADR-019 決定 2)。導入を再検討する
場合も opt-in(`SHINGAN_TELEMETRY=1` または `.shingan.yaml` 明示)・匿名・
version / rule-count のみ・ソースデータ送信なし、を不変条件とする。
セキュリティリンターにとって信頼が中核資産であり、postinstall バイナリ
ダウンロードで既に信頼コストを支払わせている以上、追加の信頼支出は
正当化できない。

---

## 9. リスク

| # | リスク | 影響 | 緩和策 |
|---|---|---|---|
| 1 | 単独メンテナのバーンアウト | 全施策停止 | S 工数優先の順序付け(§6 凡例)。スコープ凍結: §6 にない施策は次版 PRD まで追加しない |
| 2 | 上流 OSS への CI 採用 PR で FP を出し評判毀損 | Advocate チャネル焼失 | FP ベンチマーク公開(6.2-1)を**必ず先行**。min-confidence 高 + informational モードで提案 |
| 3 | 競合参入(Semgrep 等がエージェントルール追加) | カテゴリ先行者利益の喪失 | 12 ヶ月のカテゴリ確立ウィンドウ内に Marketplace 面と上流採用を確保(乗り換えコスト形成) |
| 4 | npm postinstall でのバイナリ DL への不信 | Try 段階の離脱 | 既存の SHA-256 検証 + `--provenance` 署名を README / docs で明示。Homebrew / Docker の代替導線を案内 |
| 5 | Stars 偏重による指標ゲーミング | NSM と乖離した意思決定 | §4 の KPI ツリーで Stars を先行指標に格下げ。月次レビューは NSM 代理指標を主語にする |

---

## 10. 参照

- [ADR-019: 成長・配布チャネル戦略](../shingan-adr.md#adr-019) — 本 PRD の意思決定面
- [comparison.ja.md](./comparison.ja.md) — 競合ポジショニング
- [benchmarks.ja.md](./benchmarks.ja.md) — dogfood / FP 実績
- [case-studies/](./case-studies/) — 上流 PR(6.2-3)の対象候補と証拠
- [README.ja.md](../README.ja.md) — 既定ロードマップ・operational gap の自己評価
- [ADR-010 / ADR-011 / ADR-018](../shingan-adr.md) — Plugin SDK・主戦場 LangGraph・feedback store 境界
