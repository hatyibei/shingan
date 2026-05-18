# PR: feat(npm): document and honor proxy env vars explicitly

**Title:** `feat(npm): document and honor HTTPS_PROXY / HTTP_PROXY / NO_PROXY`

**Branch suggestion:** `feat/npm-proxy-support`

---

## Summary

Corporate networks frequently require an HTTP(S) proxy. Make the postinstall download respect standard proxy env vars and document the behavior.

## Changes

- Postinstall uses `HTTPS_PROXY`, `HTTP_PROXY`, and `NO_PROXY` if set.
- README "Restricted environments" section gains a Proxy subsection.
- If a proxy var is present but the download fails, include that in the actionable error from #02.

## README addition

```markdown
#### Proxy

The wrapper honors the standard `HTTPS_PROXY`, `HTTP_PROXY`, and `NO_PROXY` env vars
during both postinstall and the runtime download fallback.

```bash
HTTPS_PROXY=http://proxy.internal:8080 \
NO_PROXY=.internal,localhost \
  npm install shingan-lint
```
```

## Test Plan

- [x] Direct connection still works when no proxy vars are set.
- [x] `HTTPS_PROXY` is honored.
- [x] `NO_PROXY` correctly excludes matching hosts.
