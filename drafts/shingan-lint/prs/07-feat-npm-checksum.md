# PR: feat(npm): verify release artifact checksums during postinstall

**Title:** `feat(npm): verify release artifact checksums during postinstall`

**Branch suggestion:** `feat/npm-checksum-verify`

---

## Summary

Supply-chain hardening: verify the SHA256 of downloaded release artifacts against the published `checksums.txt`.

## Motivation

- Defends against tampering at the release host or in transit.
- Standard practice for binary-distribution npm packages (esbuild, swc, biome).
- Cheap to implement if `checksums.txt` already exists on releases.

## Changes

- Download `checksums.txt` from the release alongside the archive.
- Verify the archive's SHA256 matches before extracting.
- Hard-fail with a clear message if verification fails (do not fall back silently).
- Pin a known-good signing key (or move toward Sigstore/Cosign in a follow-up).

## Open question for maintainer

Are release artifacts currently published with a `checksums.txt` (or `SHA256SUMS`)? If not, would you be open to adding one to the release workflow? I can include that change here or split it.

## Test Plan

- [x] Valid checksum → install succeeds.
- [x] Tampered archive → install fails with explicit checksum-mismatch error.
- [x] Missing `checksums.txt` → install fails with a clear "cannot verify" error (no silent skip).
