# PR: docs(npm): document manual binary cache setup

**Title:** `docs(npm): document manual binary cache setup for restricted environments`

**Branch suggestion:** `docs/manual-cache-setup`

---

## Summary

Document how to use `shingan-lint` in environments where the postinstall script cannot reach GitHub Releases (corporate networks, air-gapped CI, restricted sandboxes).

The wrapper already supports `SHINGAN_CACHE_DIR`, `SHINGAN_DOWNLOAD_BASE`, and `SHINGAN_SKIP_POSTINSTALL`, but these are not surfaced in the README, so users hit a wall on first run.

## Changes

- Add a "Restricted environments" subsection to the README.
- Document the cache directory layout per platform.
- Show how to pre-populate the cache and point the wrapper at it.
- Show how to use an internal mirror via `SHINGAN_DOWNLOAD_BASE`.
- Show how to skip postinstall and provide the binary manually.

## README addition (draft)

````markdown
### Restricted environments

If your environment cannot reach `https://github.com/hatyibei/shingan/releases`
during install (corporate networks, air-gapped CI, etc.), you have three options.

#### Option A — Pre-populate the cache

```bash
mkdir -p ~/.cache/shingan-lint/v0.8.7
curl -L https://github.com/hatyibei/shingan/releases/download/v0.8.7/shingan_0.8.7_linux_amd64.tar.gz \
  | tar -xz -C ~/.cache/shingan-lint/v0.8.7
```

Default cache locations:

| Platform | Path |
|----------|------|
| Linux    | `${XDG_CACHE_HOME:-$HOME/.cache}/shingan-lint/<version>` |
| macOS    | `$HOME/Library/Caches/shingan-lint/<version>` |
| Windows  | `%LOCALAPPDATA%\shingan-lint\<version>` |

Override with `SHINGAN_CACHE_DIR`:

```bash
SHINGAN_CACHE_DIR=/opt/shingan-cache npx --package shingan-lint shingan --help
```

#### Option B — Internal mirror

```bash
SHINGAN_DOWNLOAD_BASE=https://artifacts.internal.example.com/shingan \
  npm install shingan-lint
```

The wrapper appends `/<version>/<asset>` to that base.

#### Option C — Skip postinstall

```bash
SHINGAN_SKIP_POSTINSTALL=1 npm install shingan-lint
```

Then place the binary at the expected cache path manually.
````

## Test Plan

- [x] Verified README renders correctly on GitHub.
- [x] Verified the documented commands match what the wrapper actually reads.
- [x] No code changes; existing tests unaffected.
