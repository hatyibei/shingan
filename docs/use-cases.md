> 🌐 Language: **English** | [日本語](./use-cases.ja.md)

# Shingan Use Cases

A collection of scenarios showing how Shingan's 20 rules + 7 entry points (CLI / LSP / MCP / API / GitHub Action / Runner / Web middleware) are used in practice.

---

## 1. SaaS agent platform — quality gate at save time

**Scenario**: A user (non-engineer) clicks Save on a workflow in a GUI workflow editor (n8n / Dify / a proprietary SaaS).

**Integration**:
```
[User: clicks Save button]
  ↓
[Editor backend] → POST https://shingan.internal/analyze { format: "json", content: {...} }
  ↓
[Shingan API] runs 20 rules concurrently (typical workflow: 30 nodes → 0.2ms)
  ↓
[response] { findings: [...], summary: {...} }
  ↓
[Editor] If Critical, blocks save and shows a warning in the UI
```

**Incidents prevented**:
- `loop_guard`: Infinite loop with no MaxIter set → tens of thousands of yen in Gemini billing
- `error_handler_checker`: No fallback when browser operation fails → GUI automation halts midway
- `secret_exposure_scanner`: API key hard-coded in a prompt → leaked into logs

**Cost**: < 0.5ms per analysis; even at 10,000 runs per day server cost is < $0.01/month

---

## 2. CI/CD Pull Request guard

**Scenario**: A development team manages agent definitions in a repository as Go code (ADK-Go) or JSON.

**GitHub Actions**:
```yaml
- uses: hatyibei/shingan@v0.5.0
  with:
    format: adk-go
    input: ./agents/
    fail-on: critical
    output: sarif
    output-file: shingan.sarif

- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: shingan.sarif
```

**Effect**:
- Shingan warnings are shown inline on the PR's Files changed tab
- When Critical is detected, Branch Protection blocks merging
- Developers can run `shingan analyze` **locally** to fix issues in advance

**Plausible real-world examples**:
- `deprecated_model`: A team writing an Agent with `gpt-4-0314` (already shut down) caught before PR review
- `cost_estimation`: A configuration using gpt-4o inside a loop is called out at PR time with "wouldn't mini be enough?"

---

## 3. Pre-execution guard for the agent runtime

**Scenario**: Use Shingan-runner as a middleware before agent execution.

```bash
./shingan-runner --sample infinite_loop_unbounded --dry-run
# → Shingan detects Critical → execution refused
# → zero Gemini calls, zero cost incurred
```

**Application**: Run API middleware inside the **ADK Web UI** (`cmd/shingan-web`)
- User clicks "Submit" in the browser → Run API → Shingan middleware → if clean, forward to Gemini; if not, 403

This is the "last line of defense" between Shingan and Gemini execution.

---

## 4. Security audit batch

**Scenario**: Hundreds of agents are running inside an enterprise. Periodically (monthly) re-analyze all of them to surface deprecation and security issues.

```bash
# cron at 02:00 on the 1st of every month
for dir in /agents/*/; do
  shingan analyze --format adk-go --input "$dir" --output sarif --output-file "reports/$(basename $dir).sarif"
done

# Aggregate SARIF and notify
cat reports/*.sarif | jq '.runs[0].results[] | select(.level=="error")' | slack-notify #security
```

**What gets detected**:
- `deprecated_model`: a list of Agents still using shut-down models
- `secret_exposure_scanner`: hard-coded secrets unearthed
- `pii_leak_scanner`: RAG paths that could lead to GDPR violations

---

## 5. Education and training

**Scenario**: Teaching internal engineers "best practices for AI agent design".

```bash
# A sample with bugs intentionally introduced
shingan-gen --pattern buggy --seed 42 > exercise.json

# Trainees manually review exercise.json to find errors
# Then run Shingan to check answers
shingan analyze --format json --input exercise.json --output markdown
```

**Use**: New-hire training, workshops; `testdata/generated/buggy-seed42.json` can be used as-is as a documented bad example.

---

## 6. Embedding into other systems

### LangGraph (Python) — GA
Use `infrastructure/parser/langgraph.go` + `scripts/export_langgraph_server.py` to extract `StateGraph`. Requires `pip install langgraph`.

### ADK-Go (Google) — GA
Native analysis with `go/parser` + `go/types`. The Tool category is inferred from the generics of `functiontool.New[TArgs, TResults]`.

### Generic JSON workflow / proprietary GUI editor — GA
Normalize any workflow JSON into the `domain.WorkflowGraph` format and the 20 rules work as-is. See the IR section in `docs/rule-authoring.md`.

### Support for n8n / CrewAI / Mastra (planned for v0.7+)
Just add a new parser in `infrastructure/parser/<framework>.go`. Thanks to the Onion Architecture, the domain / application layers stay unchanged.

---

## 7. Data-driven quality improvement

**Scenario**: Track the types of detected Findings over time.

```bash
# Save findings to a DB
shingan analyze --format json --input . --output json | jq '.findings[]' | psql -c "INSERT INTO findings ..."

# Visualize in Grafana: monthly Critical count, top-5 frequently fired rules, etc.
```

**Effect**: You can quantitatively measure how a development team is "habituating workflow quality".

---

## 8. Developing Shingan itself (self-dogfood)

Express Shingan's own pipeline as a WorkflowGraph (`testdata/meta/shingan_pipeline.json`) and analyze it with Shingan. v0.1 produced 5 false positives, all resolved in v0.2 by separating NodeType + 2-hop tracking.

**Lesson**: To continuously drive down a rule's false-positive rate, ongoing self-application — "dogfooding" — is highly effective.

---

## Recommended flags by use case

| Use case | Recommended command |
|---|---|
| CI PR guard | `--output sarif --output-file out.sarif` |
| Production middleware | Run `shingan-web`, inject middleware |
| Pre-execution guard | `shingan-runner --sample NAME` |
| Audit batch | `--output json` + jq processing |
| Human review | `--output markdown` (table view) |
| Show high-confidence only | `--min-confidence 0.9` |
