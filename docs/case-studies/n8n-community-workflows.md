> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Case study: `Zie619/n8n-workflows` community corpus

## Repo

[github.com/Zie619/n8n-workflows](https://github.com/Zie619/n8n-workflows) — a community-curated collection of real n8n workflow exports (Notion / Postmark / Telegram / Email / Webhook / langchain-AI-Agent variants).

## Setup

```bash
npm install -g shingan-lint@latest
git clone https://github.com/Zie619/n8n-workflows /tmp/n8n-real
```

n8n is the **only Shingan-supported framework that needs no Python or Node bridge** — analysis runs purely in Go from the JSON export. Zero runtime dependencies on the user side.

## Findings sweep (Shingan v0.8.2)

| Workflow | Nodes | Triggers | Findings | Notable |
|---|---|---|---|---|
| `0141_Notion_Webhook_Create_Webhook.json` | 13 | 1 | 12 (11 Warning + 1 Info) | A Notion webhook that exports nodes without `connections` — degenerate workflow, all 12 nodes correctly flagged unreachable |
| `0272_Notion_GoogleDrive_Create_Triggered.json` | small | 1 | 2 Warnings | clean structure, missing-error-handler suggestions |
| `0057-Activecampaign-Create-Triggered.json` | small | 1 | 1 Warning | one missing-error-handler |

> **All three are "real-world community exports" — no curation.** 100% of the findings on the first workflow are high-confidence; the second and third surface the kind of error-handling-gap pattern that CI gating catches before production.

## Bugs in Shingan that this case study fixed

The n8n parser shipped in v0.7 worked against our own testdata but produced **mass false positives on community workflows**. Each fix below was driven directly by this dogfood corpus and shipped in v0.8.2:

### 1. UUID-keyed connections (P0 — silent breakage on every modern export)

Modern n8n exports key the `connections` map by node UUID, not by node name. The parser only matched by name → almost every modern workflow looked nearly disconnected → `unreachable_node` produced 30+ false positives.

```diff
+ uuidToID := make(map[string]string, len(wf.Nodes))      // new in v0.8.2
+ // ...
+ resolveRef := func(ref string) string {
+   if id, ok := nameToID[ref]; ok { return id }
+   if id, ok := uuidToID[ref]; ok { return id }
+   return ""
+ }
```

[Commit `9b5be95`](https://github.com/hatyibei/shingan/commit/9b5be95)

### 2. Multi-trigger virtual root

Workflows with >1 `category="trigger"` node (Telegram + Webhook + Schedule + Respond-to-Webhook) only had one entry picked, so other triggers' sub-flows reported as unreachable. Now Shingan synthesises an internal `__n8n_multi_trigger_root__` node connected to all triggers when ≥2 exist.

```diff
+ triggerIDs := allTriggerNodeIDs(nodes, nodeOrder, edges)
+ if len(triggerIDs) >= 2 {
+   nodes[virtualRoot] = &domain.Node{...}
+   for _, tid := range triggerIDs {
+     edges = append(edges, domain.Edge{From: virtualRoot, To: tid})
+   }
+   entryID = virtualRoot
+ }
```

### 3. `ai_*` port edges (langchain AI Agent integration)

n8n's langchain AI Agent nodes route sub-resources (LLM, tools, memory, output parsers) via `ai_languageModel` / `ai_tool` / `ai_memory` / `ai_outputParser` ports — not `main`. These were skipped as "decoration", causing langchain-heavy workflows to show 30+ unreachable_node FPs.

Now emitted as edges with `Condition=portName` so reachability rules count them while `error_handler_checker` correctly treats them as runtime-managed sub-resources (non-empty Condition = conditional branch, not missing-fallback signal).

## Take

n8n parser is **the most production-ready surface in Shingan today** — pure Go, no Python/Node bridge, works on community exports, and v0.8.2 addresses every false-positive class we saw in this dogfood pass. Real community workflows produce real, actionable findings.

## How to add Shingan to your n8n setup

```bash
# Export the workflow from n8n
n8n export:workflow --id=42 --output=workflow.json

# Run shingan
npx shingan-lint@latest analyze --format n8n --input workflow.json --output markdown
```

Or in CI on every workflow file under `n8n-exports/`:

```yaml
- name: Lint n8n workflows
  run: |
    for f in n8n-exports/*.json; do
      npx shingan-lint@latest analyze --format n8n --input "$f" --output sarif \
        --output-file "${f%.json}.sarif" || true
    done
- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: ./n8n-exports/
```
