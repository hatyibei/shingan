# 野良リポジトリ検証ハンドオフ（Claude Code 向け）

> 作成: 2026-05-30 / 対象: ワークフロー型 AI エージェントの野良 OSS リポジトリへの shingan 精度検証
> 前提: PR #18（`eval_missing` トークン判定修正, Issue #17）はマージ済み。本検証は修正後バイナリで実施。

## 0. これは何

リモート環境で「ワークフロー型 AI エージェントの野良リポジトリをできるだけ多く発見 → `shingan analyze` で検証 → 真のバグ/誤検知をトリアージ」を実施した結果の引き継ぎ。GitHub 検索 API で発見した **28 リポジトリ（langgraph / crewai、すべて実際に clone & 解析成功）** を `--min-confidence=0.7` でスイープした。

**第三者リポジトリへの issue/PR 自動投稿はしていない**（ツールスコープ外 + 責任ある運用。`docs/benchmarks.md` 方針＝「誤検知を排除した本物のバグだけ・個別に・人間レビュー後に提出」）。Claude Code 側でトリアージを完了させ、本物のバグだけ提出文面を起こす想定。

## 1. やってほしいこと（TODO）

1. **最優先トリアージ — 唯一の Critical**: `hereandnowai/master-langgraph-workflows...` の `cycle_detection @ generate (LLM, 100%)`。
   self-RAG / corrective-RAG 系の `generate ⇄ grade` リトライループの典型形。**条件付き exit エッジの有無**を確認し、
   - 本当に exit が無く無限ループ → **真バグ (TP)**。再現手順付きで提出文面を作成。
   - parser が `END` / 条件分岐 exit を取りこぼしている → **shingan 側の FP**。`langgraph` parser shim の改善 issue を `hatyibei/shingan` に起票（既存 dogfood 修正と同型）。
2. **大量 Warning リポジトリの妥当性チェック**: `techindicium/MultiAgent-CrewAI` (W=32), `RN0311/CrewAISecurityAgent` (W=21), `Numb94/crewai-astock` (W=10)。
   `error_handler_checker` / `unreachable_node` が支配的。crewai は「agents.py ファクトリ型」で空グラフ→FP が出やすい既知パターン（v0.8.7 で一部修正済み）。新たな FP パターンが無いか確認し、あれば回帰フィクスチャ化。
3. **提出判断**: TP のうち「インパクトが弱い」ものは起票見送り（`docs/benchmarks.md` の運用どおり）。提出するものだけ、対象リポジトリごとに issue 下書きを用意（本文に再現コマンドを必ず含める）。

## 2. スイープ結果サマリ（28 repos, min-confidence=0.7, 修正後バイナリ）

| Repo | FW | ★ | Total | **Crit** | Warn | rules |
|---|---|---|---|---|---|---|
| hereandnowai/master-langgraph-workflows-...-by-hereandnow-ai | langgraph | 36 | 17 | **1** | 16 | cycle×6, error_handler×9, unreachable×2 |
| zzzlip/langraph-AI-Accompany-Agent | langgraph | 20 | 7 | 0 | 7 | unreachable×7 |
| yx-fan/multi-agent-orchestration-framework | langgraph | 27 | 1 | 0 | 1 | unreachable×1 |
| emanueleielo/langgraph-think-tool | langgraph | 12 | 1 | 0 | 1 | cycle×1 |
| GenseeAI/cognify | langgraph | 277 | 0 | 0 | 0 | — |
| redhat-community-ai-tools/UnifAI | langgraph | 37 | 0 | 0 | 0 | — |
| 0verL1nk/PaperSage | langgraph | 36 | 0 | 0 | 0 | — |
| Programmergyt/ai-career-copilot | langgraph | 18 | 0 | 0 | 0 | — |
| zen-apps/ai-fitness-planner | langgraph | 13 | 0 | 0 | 0 | — |
| gapilongo/SOC | langgraph | 11 | 0 | 0 | 0 | — |
| techindicium/MultiAgent-CrewAI | crewai | 11 | 32 | 0 | 32 | circular_dep×3, cycle×3, error_handler×11, unreachable×15 |
| RN0311/CrewAISecurityAgent | crewai | 14 | 21 | 0 | 21 | error_handler×9, unreachable×12 |
| Numb94/crewai-astock | crewai | 40 | 10 | 0 | 10 | error_handler×10 |
| arham2211/crypto-bot-analyzer | crewai | 18 | 8 | 0 | 8 | error_handler×8 |
| praj2408/Smart-Marketing-Assistant-Crew-AI | crewai | 36 | 7 | 0 | 7 | error_handler×7 |
| shaadclt/Doctor-Assist-crewAI | crewai | 15 | 6 | 0 | 6 | error_handler×2, unreachable×4 |
| LikithMeruvu/Python-coding-Agent | crewai | 22 | 6 | 0 | 6 | error_handler×4, unreachable×2 |
| mesutdmn/Autonomous-Multi-Agent-...-Essay-Writer | crewai | 42 | 5 | 0 | 5 | error_handler×2, unreachable×3 |
| hanantabak2/crewai_ai_business_consultant_on_streamlit | crewai | 18 | 5 | 0 | 5 | error_handler×2, unreachable×3 |
| tonykipkemboi/crewai-gmail-automation | crewai | 189 | 4 | 0 | 4 | error_handler×4 |
| NoManNayeem/Langchain_CrewAI_Gemini-AI_Agents | crewai | 14 | 3 | 0 | 3 | error_handler×1, unreachable×2 |
| hectorpine/multiple-model-crew | crewai | 11 | 2 | 0 | 2 | error_handler×2 |
| strnad/CrewAI-Studio | crewai | 1281 | 1 | 0 | 1 | error_handler×1 |
| botextractai/ai-crewai-multi-agent | crewai | 38 | 1 | 0 | 1 | error_handler×1 |
| alexnodeland/crewlit | crewai | 26 | 1 | 0 | 1 | error_handler×1 |
| tonykipkemboi/crewai-streamlit-demo | crewai | 73 | 1 | 0 | 1 | unreachable×1 |
| startino/aitino | crewai | 90 | 0 | 0 | 0 | — |
| luandev/ComfyUI-CrewAI | crewai | 63 | 0 | 0 | 0 | — |

**所見**
- 真の Critical 候補は **1 件のみ**（hereandnowai の cycle_detection）。
- `eval_missing` の誤検知は **全 28 repo で再発ゼロ** → PR #18 のトークン判定修正が dogfood corpus の外でも有効と実地確認。
- 残りはすべて Warning（`error_handler_checker` 中心、次いで `unreachable_node` / `cycle_detection`）。

## 3. 再現コマンド（CLI / Claude Code）

### 環境準備（重要・ここで一番ハマる）
langgraph / crewai パーサーは **Python 実体が必須**。`import langgraph` / `import crewai` できないと、レポートが **空（0 件）で返るのに正常に見える** 罠がある。

```bash
# venv 推奨（システム PyYAML と衝突するため）
python3 -m venv .venv && source .venv/bin/activate
pip install langgraph crewai

# shingan は venv の python3 を PATH 経由で使う（CLI に python-bin 上書き env は無い）
export PATH="$PWD/.venv/bin:$PATH"
python3 -c "import langgraph, crewai; print('parser deps OK')"

# バイナリ（PR #18 マージ後の main を使うこと）
make build-all        # -> /tmp/shingan
```
- adk-go は Go ネイティブで追加依存なし。

### 単発解析
```bash
git clone --depth=1 https://github.com/hereandnowai/master-langgraph-workflows-in-python-20-real-world-agent-projects-by-hereandnow-ai /tmp/r
/tmp/shingan analyze --format=langgraph --input=/tmp/r --output=markdown --min-confidence=0.7
# 期待: Total 17 / Critical 1 (cycle_detection @ "generate") / eval_missing は 0 件
```

### コーパス全体（任意・再スイープ）
発見クエリは GitHub 検索 API（未認証 ~10 req/min）。framework core lib / tutorial / template は除外済み。worklist は star 降順で 28 件にキャップ。

```bash
# fw <TAB> full_name の worklist を作り、各行で:
#   git clone --depth=1 $clone $dir
#   shingan analyze --format=$fw --input=$dir --output=markdown --min-confidence=0.7
# 集計行 "| Total | Critical | Warning | Info |" の次の2行目を拾う
```

## 4. 検証対象 28 リポジトリ（clone URL）

```
# langgraph
https://github.com/hereandnowai/master-langgraph-workflows-in-python-20-real-world-agent-projects-by-hereandnow-ai.git
https://github.com/zzzlip/langraph-AI-Accompany-Agent.git
https://github.com/yx-fan/multi-agent-orchestration-framework.git
https://github.com/emanueleielo/langgraph-think-tool.git
https://github.com/GenseeAI/cognify.git
https://github.com/redhat-community-ai-tools/UnifAI.git
https://github.com/0verL1nk/PaperSage.git
https://github.com/Programmergyt/ai-career-copilot.git
https://github.com/zen-apps/ai-fitness-planner.git
https://github.com/gapilongo/SOC.git
# crewai
https://github.com/techindicium/MultiAgent-CrewAI.git
https://github.com/RN0311/CrewAISecurityAgent.git
https://github.com/Numb94/crewai-astock.git
https://github.com/arham2211/crypto-bot-analyzer.git
https://github.com/praj2408/Smart-Marketing-Assistant-Crew-AI.git
https://github.com/shaadclt/Doctor-Assist-crewAI.git
https://github.com/LikithMeruvu/Python-coding-Agent.git
https://github.com/mesutdmn/Autonomous-Multi-Agent-Systems-with-CrewAI-Essay-Writer.git
https://github.com/hanantabak2/crewai_ai_business_consultant_on_streamlit.git
https://github.com/tonykipkemboi/crewai-gmail-automation.git
https://github.com/NoManNayeem/Langchain_CrewAI_Gemini-AI_Agents.git
https://github.com/hectorpine/multiple-model-crew.git
https://github.com/strnad/CrewAI-Studio.git
https://github.com/botextractai/ai-crewai-multi-agent.git
https://github.com/alexnodeland/crewlit.git
https://github.com/tonykipkemboi/crewai-streamlit-demo.git
https://github.com/startino/aitino.git
https://github.com/luandev/ComfyUI-CrewAI.git
```

## 5. 提出ルール（厳守）

- 第三者リポジトリへの issue/PR は **TP（誤検知でないと確認できたもの）だけ**。1 リポジトリ 1 件、人間レビュー後に手動投稿。
- issue 本文には必ず **再現コマンド**（上記 §3 の単発解析）と **shingan のバージョン/コミット** を記載。
- インパクトが弱い TP は起票見送り（spam 回避、shingan のブランド保護）。
- shingan 側の FP を見つけた場合は逆に `hatyibei/shingan` に回帰フィクスチャ付きで起票/PR（dogfood 駆動の運用どおり）。

---
*このコンテナは揮発するため、レポート実体 `/tmp/wild-sweep/*.report.md` と `/tmp/wild-sweep/SUMMARY.tsv` は環境破棄で消える。再現は §3 のコマンドで。*
