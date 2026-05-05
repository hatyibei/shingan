> 🌐 Language: **English** | [日本語](./secret-detection.ja.md)

# Secret Detection — `secret_exposure_scanner`

> v0.3 feature (delivered ahead of schedule)

## Overview

`secret_exposure_scanner` is a rule that statically detects secrets — API keys,
tokens, and private keys — embedded in the `Node.Config` field of a workflow graph.

Hard-coded secrets in LLM prompts or tool arguments can leak through logs, debug output,
or the LLM's context window.

---

## Detection Patterns

| Pattern name | Regex | Severity |
|---|---|---|
| `aws_access_key` | `AKIA[0-9A-Z]{16}` | **Critical** |
| `private_key_pem` | `-----BEGIN (RSA )?PRIVATE KEY-----` | **Critical** |
| `anthropic_api_key` | `sk-ant-[A-Za-z0-9_-]{20,}` | **Critical** |
| `openai_api_key` | `sk-[A-Za-z0-9]{20,}` | **Critical** |
| `github_token` | `gh[pousr]_[A-Za-z0-9]{36,}` | Warning |
| `slack_token` | `xox[bpars]-[A-Za-z0-9-]{10,}` | Warning |
| `jwt` | `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}` | Info |
| `generic_secret` | `(?i)(password\|secret\|api_key\|apikey\|token)\s*[:=]\s*['"]?[A-Za-z0-9_-]{20,}` | Info |

**Note:** `anthropic_api_key` is checked before `openai_api_key` (since `sk-ant-` also matches `sk-`).

---

## Severity Decision

| Severity | Target | Response priority |
|---|---|---|
| **Critical** | AWS / GCP / OpenAI / Anthropic keys, private keys | Immediate response required |
| **Warning** | GitHub Token / Slack Bot Token | Same-day response recommended |
| **Info** | JWT / generic `password=XXX` patterns | Plan a remediation |

---

## Config Fields Scanned

The scanner walks **every Config value** recursively, including:

- `string` values — direct pattern match
- `map[string]any` values — keys are nested as `parent.child` and scanned
- `[]any` values — scanned with `parent[0]`-style indexed paths

Common detection targets:
- `Config["prompt"]` / `Config["prompt_template"]` / `Config["instruction"]`
- `Config["api_key"]` / `Config["headers"]` (e.g. `Authorization: Bearer sk-...`)
- Array-shaped prompt lists

---

## Exclusion Logic (False-Positive Suppression)

Values containing the following patterns are excluded as **safe references**:

| Pattern | Example |
|---|---|
| Shell environment variable | `$API_KEY`, `${OPENAI_KEY}` |
| Template placeholder | `{{secret}}`, `{{ env.TOKEN }}` |
| Node.js environment variable reference | `process.env.API_KEY` |
| Go environment variable reference | `os.Getenv("API_KEY")` |

**However**, when a placeholder-bearing string still contains a secret in the remaining text, the scanner reports it.
Example: `"sk-abc123... ${SUFFIX}"` — after stripping the placeholder, `sk-abc123...` remains, so the secret is flagged.

---

## Safe Pattern Example

```json
{
  "id": "llm_node",
  "config": {
    "api_key": "${OPENAI_API_KEY}",
    "prompt": "Authenticate using {{api_token}}",
    "headers": {
      "Authorization": "Bearer process.env.API_KEY"
    }
  }
}
```

---

## Unsafe Pattern Example

```json
{
  "id": "llm_node",
  "config": {
    "api_key": "sk-abcdefghijklmnopqrstuvwxyz123456",
    "prompt": "Use AKIAIOSFODNN7EXAMPLE for AWS",
    "headers": {
      "Authorization": "Bearer sk-ant-api01-abcdefghijklmnopqrstuvwxyz"
    }
  }
}
```

---

## Verification Commands

```bash
# Graph containing hard-coded keys -> Critical findings
shingan analyze --format json --input testdata/secrets/exposed.json --output markdown

# Environment-variable references only -> 0 findings
shingan analyze --format json --input testdata/secrets/safe.json --output markdown

# Generate with shingan-gen and pipe through analysis
shingan-gen --pattern secret-exposure | shingan analyze --format json --input /dev/stdin --output markdown
```

---

## v0.4 Plan: Shannon Entropy Scanner

In addition to pattern matching, a high-precision scanner using **Shannon entropy** is planned to detect unknown secret patterns.

- Flag random strings whose entropy exceeds the > 3.5 threshold
- Auto-discriminate the baseline (unsalted hash vs. random key)
- Context-aware scoring (key name, surrounding text) to reduce false positives
