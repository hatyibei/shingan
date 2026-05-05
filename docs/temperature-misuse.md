> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# temperature_misuse

`temperature_misuse` flags LLM nodes that combine an explicit `temperature > 0`
with a deterministic-task signal (structured output, extraction, classification,
code generation). High temperature makes schema-bound or label-bound output
unstable across runs and inflates eval variance.

| Tier | Severity | Confidence | Reason |
|------|----------|------------|--------|
| Local (ADR-007) | Warning / Info | 0.5–0.9 | `exact_static_match` or `heuristic_pattern` |

## Detection priority

The rule fires on the **first matching signal** below; later signals are not
re-evaluated for the same node.

1. **`structured_output == true` or `response_format == "json_object"` with
   `temperature > 0`** → Warning, Confidence 0.9, `exact_static_match`.
   Schema-binding is a hard deterministic signal — we know the user expects a
   parseable response.
2. **`task == "classification"` with `temperature > 0.3`** → Warning,
   Confidence 0.7, `heuristic_pattern`. Class probabilities become unstable
   above ~0.3.
3. **`task == "code_generation"` with `temperature > 0`** → Warning,
   Confidence 0.7, `heuristic_pattern`. Even small temperature drift produces
   different compilable outputs across runs.
4. **`task` in `{extraction, structured_output}` with `temperature > 0`** →
   Info, Confidence 0.5, `heuristic_pattern`. Field values may differ across
   runs; tighten only when the task explicitly requires stability.

If `Config["task"]` is missing the rule falls back to keyword matching on
`node.Name` (`extract`, `classif`, `code_gen` / `codegen`). Anything outside
those categories is silently ignored.

## What it does NOT flag

- `temperature == 0` (correct deterministic config) regardless of task.
- `temperature` key absent — the provider default may already be deterministic;
  flagging the absence would generate false positives.
- Non-LLM nodes — only `NodeTypeLLM` is inspected.
- Tasks signalling creative intent (`creative_writing`, `summarisation`,
  `chat`, etc.) — high temperature is desirable here.

## Suggestion

> Set `temperature` to 0 for deterministic tasks. High temperature produces
> output variability that defeats structured extraction/classification.

## Examples

```json
{
  "type": "llm",
  "config": {
    "model": "gpt-4o-mini",
    "structured_output": true,
    "temperature": 0.7
  }
}
```
→ Warning (0.9, `exact_static_match`).

```json
{
  "type": "llm",
  "name": "extract_invoice_fields",
  "config": {
    "model": "gpt-4o-mini",
    "temperature": 0.4
  }
}
```
→ Info (0.5, `heuristic_pattern`) via name heuristic.

## See also

- [ADR-007](../shingan-adr.md#adr-007) — Local / Path / Global rule tiers
- [ADR-008](../shingan-adr.md#adr-008) — Confidence × ConfidenceReason
- [`testdata/temperature/`](../testdata/temperature/) — sample fixtures
