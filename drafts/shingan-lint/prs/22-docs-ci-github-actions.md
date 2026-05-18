# PR: docs(ci): add GitHub Actions examples

**Title:** `docs(ci): add GitHub Actions examples`

**Branch suggestion:** `docs/ci-github-actions`

---

## Summary

Drop-in workflow examples for the most common CI surface (GitHub Actions). Lowers adoption friction noticeably.

## README addition (draft)

````markdown
### GitHub Actions

Minimal lint job:

```yaml
name: shingan
on: [pull_request]
jobs:
  shingan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: npx --yes --package shingan-lint shingan analyze --format json --input workflows
```

Upload to GitHub Code Scanning via SARIF:

```yaml
      - run: npx --yes --package shingan-lint shingan analyze \
               --format json --input workflows \
               --output sarif --output-file shingan.sarif
      - uses: github/codeql-action/upload-sarif@v3
        with: { sarif_file: shingan.sarif }
```

Restricted environments (private mirror):

```yaml
      - run: npx --yes --package shingan-lint shingan analyze --input workflows
        env:
          SHINGAN_DOWNLOAD_BASE: https://artifacts.internal.example.com/shingan
```
````

## Test Plan

- [x] Commands run successfully end-to-end on a clean Actions runner.
- [x] SARIF upload accepts the produced file.
- [x] No code changes.
