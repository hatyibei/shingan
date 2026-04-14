# testdata/generated

This directory contains pre-generated WorkflowGraph JSON files produced by `shingan-gen`.
Each file is deterministically generated from a fixed seed and can be used as reference
fixtures for testing, benchmarking, and rule development.

## Files

| File | Pattern | Size | Seed | Expected Findings |
|------|---------|------|------|-------------------|
| `random-size100-seed42.json` | random | 100 nodes | 42 | Multiple Critical + Warning (intentional bugs) |
| `clean-size10-seed42.json` | clean | 10 nodes | 42 | **0 findings** |
| `buggy-seed42.json` | buggy | ~10 nodes | 42 | 7 rules fire (see below) |
| `infinite-loop-seed42.json` | infinite-loop | 3 nodes | 42 | loop_guard Critical, cycle_detection Critical |
| `unreachable-size20-seed42.json` | unreachable | 20 nodes | 42 | unreachable_node Warning (dangling LLM + Tool) |
| `pii-leak-seed42.json` | pii-leak | 4 nodes | 42 | pii_leak_scanner Warning, error_handler_checker Warning |
| `cycle-seed42.json` | cycle | 5 nodes | 42 | cycle_detection Critical |

## Expected Findings per Pattern

### buggy-seed42.json — All 7 Rules Fire

| Rule | Severity | Description |
|------|----------|-------------|
| `cycle_detection` | Critical | Loop node in cycle without max_iterations |
| `loop_guard` | Critical | LoopAgent missing max_iterations |
| `unreachable_node` | Warning | dangling_node (LLM) not reachable from entry |
| `error_handler_checker` | Warning | api_tool, buggy_rag, buggy_api_sink have no conditional edges |
| `cost_estimation` | Warning | gpt-4o LLM nodes inside an unbounded loop |
| `redundant_llm_call` | Warning | expensive_llm and duplicate_llm share model + prompt_template |
| `pii_leak_scanner` | Warning | RAG tool → external API without Human approval gate |

### infinite-loop-seed42.json

| Rule | Severity | Description |
|------|----------|-------------|
| `loop_guard` | Critical | unbounded_loop has no max_iterations |
| `cycle_detection` | Critical | Loop node in cycle, no max_iterations |

### unreachable-size20-seed42.json

| Rule | Severity | Description |
|------|----------|-------------|
| `unreachable_node` | Warning | dangling_llm (LLM) is unreachable |
| `unreachable_node` | Warning | dangling_tool (Tool) is unreachable |

### pii-leak-seed42.json

| Rule | Severity | Description |
|------|----------|-------------|
| `pii_leak_scanner` | Warning | rag_tool → api_sink without Human gate |
| `error_handler_checker` | Warning | Tool nodes without conditional outgoing edges |

### cycle-seed42.json

| Rule | Severity | Description |
|------|----------|-------------|
| `cycle_detection` | Critical | Raw LLM cycle with no parent Loop node |

### clean-size10-seed42.json

No findings expected. Use as a regression baseline to confirm that a workflow
passes all 7 rules.

## Regenerating

```bash
# Build the generator
go build -o /tmp/shingan-gen ./cmd/shingan-gen

# Regenerate all samples
/tmp/shingan-gen --pattern random --size 100 --seed 42 --output testdata/generated/random-size100-seed42.json
/tmp/shingan-gen --pattern clean  --size 10  --seed 42 --output testdata/generated/clean-size10-seed42.json
/tmp/shingan-gen --pattern buggy            --seed 42 --output testdata/generated/buggy-seed42.json
/tmp/shingan-gen --pattern infinite-loop    --seed 42 --output testdata/generated/infinite-loop-seed42.json
/tmp/shingan-gen --pattern unreachable --size 20 --seed 42 --output testdata/generated/unreachable-size20-seed42.json
/tmp/shingan-gen --pattern pii-leak         --seed 42 --output testdata/generated/pii-leak-seed42.json
/tmp/shingan-gen --pattern cycle  --size 5  --seed 42 --output testdata/generated/cycle-seed42.json
```

## Analyzing

```bash
# Build shingan
go build -o /tmp/shingan ./cmd/shingan

# Analyze any sample
/tmp/shingan analyze --format json --input testdata/generated/buggy-seed42.json --output markdown
/tmp/shingan analyze --format json --input testdata/generated/clean-size10-seed42.json --output markdown
```
