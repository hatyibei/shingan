> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# Feedback Store (`shingan feedback`)

`shingan feedback` records durable **true-positive / false-positive** labels for
findings, so triage decisions accrue over time against a stable finding
identity. It is **capture-only**: recording a label never changes
`shingan analyze` output. The data is intended to feed a future confidence
calibration layer once enough labels exist to justify it (see
[ADR-018](../shingan-adr.md#adr-018-ml-based-dynamic-confidence--capture-only-feedback-store学習層は-defer)).
Nothing reads these labels back into the analysis pipeline today.

## Why labels stay valid over time

Each record is keyed by the **same fingerprint baselines use**:
`rule + node_id + source_file + message_digest`. Confidence is deliberately
**not** part of that key, so a label you record now stays attached to "the same
finding" even as a rule's static confidence is tuned later — exactly the
property a future calibration layer needs.

## Storage format (JSONL)

Records are appended to a JSON Lines file (one compact JSON object per line),
so the store appends cheaply and grows over time. Each line is a
`FeedbackRecord`:

```json
{"fingerprint":{"rule":"loop_guard","node_id":"agent_a","source_file":"wf.py","message_digest":"0a1b2c3d4e5f6071"},"label":"fp","source":"cli","timestamp":"2026-06-04T09:30:00Z"}
```

| Field | Meaning |
|---|---|
| `fingerprint` | The stable finding identity (`rule`, `node_id`, optional `source_file`, `message_digest`). |
| `label` | `tp` (true positive) or `fp` (false positive). |
| `source` | Provenance of the label: `cli`, `sarif`, or `api`. |
| `timestamp` | When the label was recorded (UTC, RFC 3339). |

## Usage

### Append a single label

Identify the finding by its raw fields — the message digest is derived for you
via the same `domain.Fingerprint` baselines use:

```
shingan feedback --store labels.jsonl \
    --rule loop_guard --node agent_a --message "max_iterations not set" \
    --label fp
```

Or supply an explicit message digest (for findings whose digest is a rule
message-template ID — see the limitation below):

```
shingan feedback --store labels.jsonl \
    --rule loop_guard --node n1 --digest loop_guard.max_iterations_missing \
    --label tp
```

### Ingest a batch

Pass a JSON array **or** a JSONL file of `{finding|fingerprint, label}` items.
The `finding` shape mirrors `shingan analyze --output json`, so you can triage
analyze output directly and feed it back:

```
shingan feedback --store labels.jsonl --ingest triaged.json
```

```json
[
  {"finding": {"rule": "loop_guard", "node_id": "a", "message": "max_iterations not set"}, "label": "fp"},
  {"fingerprint": {"rule": "r3", "node_id": "n3", "message_digest": "abc123"}, "label": "tp", "source": "sarif"}
]
```

## Flags

| Flag | Purpose |
|---|---|
| `--store` (required) | Path to the JSONL store to append to / load from. |
| `--ingest` | Path to a JSON array / JSONL file of `{finding\|fingerprint, label}`. |
| `--rule`, `--node`, `--source-file` | Finding identity fields (single-record mode). |
| `--message` | Finding message; its digest is derived via `domain.Fingerprint`. |
| `--digest` | Explicit message digest, instead of `--message`. |
| `--label` | `tp` or `fp`. |
| `--source` | Label provenance: `cli` (default), `sarif`, or `api`. |

## Limitation: template-ID findings from analyze JSON

`shingan analyze --output json` emits a finding's `message` but **not** its
`message_template_id`. For ordinary rules the fingerprint reconstructed from the
JSON `message` matches the live finding exactly. But rules that implement
`RuleWithMessageTemplate` use the template ID as their `message_digest`; that ID
is absent from analyze output, so a fingerprint rebuilt from the JSON `message`
would hash the message instead and **not** match the live finding's key.

For such findings, feed back via an explicit `fingerprint` object (carrying the
real `message_digest`) or `--digest`. This is intentional: adding
`message_template_id` to the analyze JSON output would change analyze's output
bytes, which this increment guarantees not to do.
