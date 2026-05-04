# model_card_mismatch

`model_card_mismatch` flags LLM nodes whose declared `model` name disagrees
with the configured `base_url` or `provider`. A `gpt-*` model wired to
`api.anthropic.com` is guaranteed to fail at runtime — this rule catches it
before deployment.

| Tier | Severity | Confidence | Reason |
|------|----------|------------|--------|
| Local (ADR-007) | Critical / Info | 0.4 or 1.0 | `exact_static_match` or `heuristic_pattern` |

## Provider table (built-in)

| Model prefix | Provider | Allowed endpoints |
|--------------|----------|-------------------|
| `gpt-*`, `o1-*`, `text-davinci*`, `text-embedding*` | OpenAI | `api.openai.com`, `*.openai.azure.com` (Azure OpenAI) |
| `claude-*`, `claude*` | Anthropic | `api.anthropic.com` |
| `gemini-*`, `gemini*`, `text-bison*`, `chat-bison*` | Google | `generativelanguage.googleapis.com`, `aiplatform.googleapis.com` (Vertex AI) |

`Config["provider"]` is matched case-insensitively against a list of common
aliases (e.g. `"vertex"`, `"google-vertex"`, `"vertexai"`, `"azure"`,
`"azure-openai"`).

## Detection logic

1. The node's `Config["model"]` is matched against the prefix table.
2. If the prefix is **known**:
   - `Config["provider"]` (if set) is normalised and compared with the
     expected provider.
   - Otherwise `Config["base_url"]` (if set) is matched against the
     allowed-endpoint substring list.
   - When `provider` matches, a custom `base_url` is accepted (legitimate
     proxy / self-hosted compatible endpoint scenario).
   - When neither matches the expected provider, a **Critical** finding is
     emitted with Confidence 1.0 and `exact_static_match`.
3. If the prefix is **unknown** but `Config["provider"]` is set, an **Info**
   finding is emitted (Confidence 0.4, `heuristic_pattern`) so reviewers can
   decide whether to extend Shingan's table.
4. Otherwise the rule is silent.

## Examples

```json
{
  "type": "llm",
  "config": {
    "model": "gpt-4o",
    "base_url": "https://api.anthropic.com/v1"
  }
}
```
→ Critical (1.0, `exact_static_match`) — gpt-* on Anthropic.

```json
{
  "type": "llm",
  "config": {
    "model": "claude-3-5-sonnet",
    "provider": "openai"
  }
}
```
→ Critical (1.0, `exact_static_match`).

```json
{
  "type": "llm",
  "config": {
    "model": "gemini-1.5-pro",
    "base_url": "https://us-central1-aiplatform.googleapis.com/v1"
  }
}
```
→ no finding — Vertex AI is whitelisted as Google.

```json
{
  "type": "llm",
  "config": {
    "model": "gpt-4o",
    "provider": "openai",
    "base_url": "https://my-internal-proxy.corp/v1"
  }
}
```
→ no finding — provider matches the model prefix, custom base_url is treated
as a legitimate proxy.

```json
{
  "type": "llm",
  "config": {
    "model": "mistral-large",
    "provider": "mistral"
  }
}
```
→ Info (0.4, `heuristic_pattern`) — knowledge gap; reviewer is asked to
verify whether the table needs to be extended.

## Suggestion

> Model `<model>` belongs to provider `<expected>` but `base_url`/`provider`
> is set to `<actual>`. Either update the model name or the endpoint to match.

## See also

- [ADR-007](../shingan-adr.md#adr-007) — Local / Path / Global rule tiers
- [ADR-008](../shingan-adr.md#adr-008) — Confidence × ConfidenceReason
- [`testdata/model_mismatch/`](../testdata/model_mismatch/) — sample fixtures
