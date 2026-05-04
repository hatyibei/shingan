# testdata/model_mismatch

Sample WorkflowGraph JSON files for the `model_card_mismatch` rule.

| File | Expected findings |
|------|-------------------|
| `wrong.json` | Critical×3 (gpt-4o on Anthropic url; claude-3-5-sonnet with provider=openai; gemini-1.5-pro on Anthropic url) |
| `correct.json` | 0 findings — gpt-4o on OpenAI, claude-3-5-sonnet with provider=anthropic, gemini-1.5-pro on Vertex AI |

## Usage

```bash
./shingan analyze --format json --input testdata/model_mismatch/wrong.json
./shingan analyze --format markdown --input testdata/model_mismatch/correct.json
```
