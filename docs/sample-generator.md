> 🌐 Language: **English** | [日本語](./sample-generator.ja.md)

# shingan-gen — Sample Workflow Generator

`shingan-gen` is a CLI tool that generates random or intentionally-patterned WorkflowGraph JSON, so developers can try Shingan's static analysis right away.

## Install / Build

```bash
go build -o shingan-gen ./cmd/shingan-gen
# or
make gen-cli
```

## Usage

```bash
shingan-gen --pattern <name> --size <N> --seed <S> --output <path>
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--pattern` | `random` | Generation pattern (see below) |
| `--size` | `10` | Number of nodes (applies to random/clean/unreachable/cycle) |
| `--seed` | `42` | Random seed (fix for reproducibility) |
| `--output` | stdout | Output file path (`-` or omitted = stdout) |

## Pattern List

### `random` — Random Graph

A large graph with intentional bugs. Compatible with `GenerateRandomGraph`.

```bash
shingan-gen --pattern random --size 50 --seed 42 > random.json
shingan analyze --format json --input random.json --output markdown
```

**Expected Findings**: Multiple Critical/Warning (cycles, loop_guard, unreachable, pii_leak, etc.)

---

### `clean` — Issue-Free Graph

A structurally correct graph that passes all 7 rules.
Use when implementing a new rule to confirm it produces no false positives.

```bash
shingan-gen --pattern clean --size 20 --seed 42 > clean.json
shingan analyze --format json --input clean.json --output markdown
# Expected: 0 findings
```

**Expected Findings**: None (0 items)

---

### `buggy` — Trigger All 7 Rules

A graph designed so every analysis rule returns at least one Finding.

```bash
shingan-gen --pattern buggy --seed 42 > buggy.json
shingan analyze --format json --input buggy.json --output markdown
# Expected: all 7 rules fire
```

**Expected Findings**:

| Rule | Severity | Reason |
|------|----------|--------|
| `cycle_detection` | Critical | Loop node with no max_iterations forms a cycle |
| `loop_guard` | Critical | LoopAgent without max_iterations configured |
| `unreachable_node` | Warning | dangling_node (LLM) is unreachable from the entry |
| `error_handler_checker` | Warning | api_tool has only unconditional edges (no error handling) |
| `cost_estimation` | Warning | gpt-4o node sits inside an unbounded loop |
| `redundant_llm_call` | Warning | Two LLM nodes share the same (model, prompt_template) |
| `pii_leak_scanner` | Warning | Path from RAG tool → external API has no Human gate |

---

### `infinite-loop` — LoopGuard Trigger Pattern

A graph where a LoopAgent without `max_iterations` forms a cycle.

```bash
shingan-gen --pattern infinite-loop --seed 42 > infinite-loop.json
shingan analyze --format json --input infinite-loop.json --output markdown
```

**Expected Findings**:
- `loop_guard`: Critical — `unbounded_loop` has no max_iterations
- `cycle_detection`: Critical — Loop node forms a cycle but max_iterations is unset

---

### `unreachable` — unreachable_node Trigger Pattern

A graph that contains isolated nodes unreachable from the entry node.

```bash
shingan-gen --pattern unreachable --size 15 --seed 42 > unreachable.json
shingan analyze --format json --input unreachable.json --output markdown
```

**Expected Findings**:
- `unreachable_node`: Warning — `dangling_llm` (LLM type) is unreachable
- `unreachable_node`: Warning — `dangling_tool` (Tool type) is unreachable

---

### `pii-leak` — PIILeakScanner Trigger Pattern

A graph with a path from a RAG tool to an external API (no Human gate).

```bash
shingan-gen --pattern pii-leak --seed 42 > pii-leak.json
shingan analyze --format json --input pii-leak.json --output markdown
```

**Expected Findings**:
- `pii_leak_scanner`: Warning — `user_data_rag` → `external_api` with no Human gate
- `error_handler_checker`: Warning — Tool node missing conditional edges

---

### `cycle` — Pure Cycle Graph

A raw cycle that is not protected by a Loop/LoopAgent node (graph definition error).

```bash
shingan-gen --pattern cycle --size 4 --seed 42 > cycle.json
shingan analyze --format json --input cycle.json --output markdown
```

**Expected Findings**:
- `cycle_detection`: Critical — Non-Loop nodes form a cycle without a parent Loop node

---

## Use With Pipes

```bash
# Pass a buggy graph directly to shingan analyze
shingan-gen --pattern buggy | shingan analyze --format json --input /dev/stdin --output markdown

# Validate a clean graph
shingan-gen --pattern clean --size 50 | shingan analyze --format json --input /dev/stdin --output json | jq '.findings | length'
# Expected output: 0
```

## Makefile Targets

```bash
# Build shingan-gen
make gen-cli

# Generate any pattern (stdout)
make sample-buggy
make sample-clean
make sample-pii-leak
# etc.
```

## Educational Use

### Workflow When Implementing a New Rule

1. Use `shingan-gen --pattern clean` to confirm the new rule produces no false positives
2. Add a pattern for the new rule in `domain/testutil/generate.go`
3. Add expected samples to `testdata/generated/`

### As Test Fixtures

Each pattern file can be used as input for `shingan analyze` E2E tests:

```go
testdata := "../../testdata/generated/buggy-seed42.json"
findings := runAnalysis(t, testdata)
if !hasCriticalFinding(findings, "cycle_detection") {
    t.Error("expected cycle_detection Critical")
}
```

## JSON Format

The JSON emitted by `shingan-gen` is compatible with `shingan analyze --format json`.
Nodes are emitted as an array (sorted by ID):

```json
{
  "nodes": [
    {"id": "entry", "name": "entry", "type": "llm", "config": {"model": "gpt-4o-mini"}}
  ],
  "edges": [
    {"from": "entry", "to": "output"}
  ],
  "entry_node_id": "entry"
}
```
