# testdata/temperature

Sample WorkflowGraph JSON files for the `temperature_misuse` rule.

| File | Expected findings |
|------|-------------------|
| `misuse.json` | Warning×3 (json_extractor: structured_output+temp; label_classifier: classification temp>0.3; code_writer: code_generation temp>0) |
| `ok.json` | 0 findings — first two nodes use temperature=0; creative_writing task is exempt regardless of temperature |

## Usage

```bash
./shingan analyze --format json --input testdata/temperature/misuse.json
./shingan analyze --format markdown --input testdata/temperature/ok.json
```
