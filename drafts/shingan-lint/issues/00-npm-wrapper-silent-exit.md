# Issue: npx wrapper exits with no actionable message when binary download fails

**Title:** `npx wrapper exits silently when postinstall cannot download platform binary`

**Labels (suggested):** `bug`, `installer`, `dx`

---

Hi, thanks for building Shingan. I tried `shingan-lint` via npx in a restricted environment and hit a first-run issue that was hard to diagnose.

## Environment

- OS: Linux
- Node: 18.20.8
- npm: 10.8.2
- Package: `shingan-lint@0.8.7`

## Reproduction

```bash
npx --yes --package shingan-lint shingan --help
npx --yes --package shingan-lint shingan analyze --input .
```

## Actual Behavior

The commands exit with code 1 and no actionable output when the postinstall binary download fails (for example because GitHub Releases is unreachable from the network).

## Expected Behavior

The wrapper should print a clear diagnostic when the platform binary is missing, including:

- the expected cache path
- platform / architecture
- relevant env vars: `SHINGAN_CACHE_DIR`, `SHINGAN_DOWNLOAD_BASE`, `SHINGAN_SKIP_POSTINSTALL`
- a manual install hint (URL pattern + tar extraction example)

## Proposal

I'd be happy to open a small PR that:

1. Detects "binary missing / not executable" before spawning.
2. Prints an actionable error message with the info above.
3. Adds a short Troubleshooting section to the README.

Does that direction sound OK? If you'd prefer a different approach (e.g. retrying the download at runtime instead of just reporting), I'm happy to follow that.
