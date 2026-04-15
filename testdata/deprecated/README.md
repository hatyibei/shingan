# testdata/deprecated

Sample WorkflowGraph JSON files for the `deprecated_model` rule.

| File | Expected findings |
|------|-------------------|
| `shutdown_models.json` | Critical×3 (gpt-3.5-turbo-0613, claude-2, gemini-pro) |
| `deprecated_models.json` | Warning×1 (gpt-4-32k) |
| `active_models.json` | 0 findings (gpt-4o, claude-3-5-sonnet) |

## Usage

```bash
./shingan analyze --format json --input testdata/deprecated/shutdown_models.json
./shingan analyze --format markdown --input testdata/deprecated/deprecated_models.json
./shingan analyze --format json --input testdata/deprecated/active_models.json
```
