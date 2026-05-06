> 🌐 Language: **English** (Japanese translation welcome — see [issue tracker](https://github.com/hatyibei/shingan/issues))

# SARIF Output — GitHub Code Scanning Integration

Shingan supports SARIF v2.1.0 output via `--output sarif`, enabling native integration with GitHub Code Scanning for inline PR annotations.

## Severity Mapping

| Shingan Severity | SARIF Level |
|-----------------|-------------|
| `Critical`      | `error`     |
| `Warning`       | `warning`   |
| `Info`          | `note`      |

## Sample Output

```json
{
  "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
  "version": "2.1.0",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "Shingan",
          "version": "0.1.0",
          "informationUri": "https://github.com/hatyibei/shingan",
          "rules": [
            {
              "id": "cycle_detection",
              "name": "cycle_detection",
              "shortDescription": { "text": "cycle_detection" },
              "fullDescription": { "text": "cycle_detection" },
              "defaultConfiguration": { "level": "error" }
            }
          ]
        }
      },
      "results": [
        {
          "ruleId": "cycle_detection",
          "level": "error",
          "message": { "text": "cycle detected at non-Control node \"loop_node\"" },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {
                  "uri": "workflow://nodes/loop_node"
                }
              }
            }
          ]
        }
      ]
    }
  ]
}
```

Note: Shingan's Workflow Graph does not carry source-file line information. Artifact URIs use the synthetic scheme `workflow://nodes/<nodeID>` (or `workflow://graph` for graph-level findings). GitHub Code Scanning will display these as file-less annotations in the Security tab.

## GitHub Actions Integration

```yaml
name: Shingan Workflow Analysis

on:
  pull_request:
    paths:
      - '**.go'

jobs:
  analyze:
    runs-on: ubuntu-latest
    permissions:
      security-events: write   # required for upload-sarif
      contents: read

    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: stable

      - name: Install Shingan
        run: go install github.com/hatyibei/shingan/cmd/shingan@latest

      - name: Run Shingan (SARIF output)
        run: |
          shingan analyze \
            --format adk-go \
            --input . \
            --output sarif \
            --output-file shingan-results.sarif
        continue-on-error: true   # let upload run even when findings exist

      - name: Upload SARIF to GitHub Code Scanning
        uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: shingan-results.sarif
          category: shingan
```

## PR Inline Warnings

After the workflow runs, GitHub Code Scanning displays findings in two places:

1. **Security tab** (`repo > Security > Code scanning`) — full list of all findings across branches.
2. **Pull request "Files changed" tab** — inline annotations on the diff. Because Shingan reports workflow-node URIs rather than source lines, annotations appear as PR-level comments rather than line-level inline comments.

To promote findings to blocking PR checks, configure a branch protection rule requiring the `shingan` Code Scanning category to have zero `error`-level results before merging.
