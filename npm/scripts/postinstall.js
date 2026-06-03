#!/usr/bin/env node
// shingan-lint postinstall: downloads the platform-specific Go binary
// from the matching GitHub Release tag, verifies it via the
// checksums.txt sha256 entry, extracts it from the goreleaser tarball,
// and installs it under ~/.cache/shingan-lint/v<version>/.
//
// Integrity is FAIL-CLOSED (#29): the install aborts (non-zero exit) on a
// checksum mismatch, a missing/unreachable checksums.txt, OR the archive
// being absent from checksums.txt. There is no "download but skip
// verification" path.
//
// The one intentional, explicit escape hatch is SHINGAN_SKIP_POSTINSTALL=1
// — it skips the download entirely (for air-gapped/CI-mirror builds where
// the binary is provided externally). It is opt-in and never the default.

'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const crypto = require('crypto');
// NOTE: `tar` is require()d lazily inside extractTar(), not at module load.
// Extraction only happens AFTER the fail-closed checksum gate passes, so a
// verification failure never reaches the archive-handling code — and the
// regression test can require() this module without the `tar` dep present.

const PACKAGE_VERSION = require('../package.json').version;

function platformTag() {
  const p = process.platform;
  const a = process.arch;
  if (p === 'darwin' && a === 'arm64') return { os: 'darwin', arch: 'arm64', ext: 'tar.gz', exe: 'shingan' };
  if (p === 'darwin' && a === 'x64') return { os: 'darwin', arch: 'amd64', ext: 'tar.gz', exe: 'shingan' };
  if (p === 'linux' && a === 'arm64') return { os: 'linux', arch: 'arm64', ext: 'tar.gz', exe: 'shingan' };
  if (p === 'linux' && a === 'x64') return { os: 'linux', arch: 'amd64', ext: 'tar.gz', exe: 'shingan' };
  if (p === 'win32' && a === 'arm64') return { os: 'windows', arch: 'arm64', ext: 'zip', exe: 'shingan.exe' };
  if (p === 'win32' && a === 'x64') return { os: 'windows', arch: 'amd64', ext: 'zip', exe: 'shingan.exe' };
  throw new Error(`shingan-lint: unsupported platform/arch: ${p}/${a}`);
}

function cacheDir() {
  const base = process.env.SHINGAN_CACHE_DIR ||
    process.env.XDG_CACHE_HOME ||
    path.join(os.homedir(), '.cache');
  return path.join(base, 'shingan-lint', `v${PACKAGE_VERSION}`);
}

function archiveName(tag) {
  return `shingan_${PACKAGE_VERSION}_${tag.os}_${tag.arch}.${tag.ext}`;
}

function releaseURL(tag) {
  const base = process.env.SHINGAN_DOWNLOAD_BASE ||
    `https://github.com/hatyibei/shingan/releases/download/v${PACKAGE_VERSION}`;
  return `${base}/${archiveName(tag)}`;
}

function checksumURL() {
  const base = process.env.SHINGAN_DOWNLOAD_BASE ||
    `https://github.com/hatyibei/shingan/releases/download/v${PACKAGE_VERSION}`;
  return `${base}/checksums.txt`;
}

async function fetchBuffer(url) {
  // Node 18+ has global fetch. Follow redirects (GitHub returns 302 to S3).
  const res = await fetch(url, { redirect: 'follow' });
  if (!res.ok) {
    throw new Error(`download ${url} failed: HTTP ${res.status}`);
  }
  const ab = await res.arrayBuffer();
  return Buffer.from(ab);
}

async function fetchText(url) {
  const buf = await fetchBuffer(url);
  return buf.toString('utf8');
}

function sha256(buf) {
  return crypto.createHash('sha256').update(buf).digest('hex');
}

function findExpectedHash(checksumsText, archiveName) {
  // checksums.txt format: "<hash>  <filename>" per line
  for (const line of checksumsText.split('\n')) {
    const parts = line.trim().split(/\s+/);
    if (parts.length === 2 && parts[1] === archiveName) {
      return parts[0];
    }
  }
  return null;
}

// verifyChecksum is the fail-CLOSED integrity gate (#29). It throws on
// *any* condition that prevents proving the downloaded archive is
// authentic — a sha256 mismatch, or the archive being absent from
// checksums.txt — so the only way past it is a genuine, matching hash.
// Earlier this logic warned-and-continued (fail-OPEN): a tampered
// binary, or a stripped/empty checksums.txt, would silently install.
// Pure + synchronous so it is unit-testable without any network.
function verifyChecksum(archiveBuf, archive, checksumsText) {
  const expected = findExpectedHash(checksumsText, archive);
  if (!expected) {
    throw new Error(
      `${archive} not found in checksums.txt — cannot verify integrity. ` +
        `Refusing to install an unverifiable binary. ` +
        `If the release is genuinely missing checksums and you accept the risk, ` +
        `set SHINGAN_SKIP_POSTINSTALL=1 and install the binary yourself.`
    );
  }
  const actual = sha256(archiveBuf);
  if (actual !== expected) {
    throw new Error(
      `sha256 mismatch for ${archive}: expected ${expected}, got ${actual}. ` +
        `The downloaded archive does NOT match the published checksum — ` +
        `aborting install (possible tampering or a corrupted download).`
    );
  }
  return expected;
}

async function extractTar(buf, dest, tag) {
  // Write to temp file so `tar` can stream it back.
  const tmpFile = path.join(dest, `_archive.${tag.ext}`);
  fs.writeFileSync(tmpFile, buf);
  try {
    if (tag.ext === 'tar.gz') {
      const tar = require('tar');
      await tar.x({ file: tmpFile, cwd: dest, strict: true });
    } else if (tag.ext === 'zip') {
      // Minimal zip extraction without an extra dependency: shell out.
      const { execFileSync } = require('child_process');
      execFileSync('powershell.exe', [
        '-NoProfile',
        '-Command',
        `Expand-Archive -Path "${tmpFile}" -DestinationPath "${dest}" -Force`,
      ], { stdio: 'inherit' });
    } else {
      throw new Error(`unknown archive ext: ${tag.ext}`);
    }
  } finally {
    fs.unlinkSync(tmpFile);
  }
}

async function main() {
  if (process.env.SHINGAN_SKIP_POSTINSTALL === '1') {
    console.log('shingan-lint: SHINGAN_SKIP_POSTINSTALL=1 set, skipping binary download.');
    return;
  }

  let tag;
  try {
    tag = platformTag();
  } catch (e) {
    console.error(`shingan-lint: ${e.message}`);
    console.error(`Supported: darwin/arm64, darwin/x64, linux/arm64, linux/x64, win32/arm64, win32/x64.`);
    console.error(`If your platform is unsupported, install via: go install github.com/hatyibei/shingan/cmd/shingan@v${PACKAGE_VERSION}`);
    process.exit(0); // exit 0 so CI installs don't break — user gets actionable error on first invoke
  }

  const dest = cacheDir();
  const binPath = path.join(dest, tag.exe);
  if (fs.existsSync(binPath)) {
    console.log(`shingan-lint: binary already cached at ${binPath}`);
    return;
  }

  fs.mkdirSync(dest, { recursive: true });

  const archive = archiveName(tag);
  const url = releaseURL(tag);
  console.log(`shingan-lint: downloading ${archive} from ${url}`);

  let archiveBuf;
  try {
    archiveBuf = await fetchBuffer(url);
  } catch (e) {
    console.error(`shingan-lint: download failed: ${e.message}`);
    console.error(`If the release was just published, GitHub may need a few seconds to propagate.`);
    process.exit(1);
  }

  // Fail-CLOSED integrity check (#29). The checksums.txt file MUST be
  // reachable and MUST contain a matching sha256 for this archive, or we
  // abort the install. A checksum mismatch, a missing/unreachable
  // checksums.txt, or the archive being absent from it all raise here and
  // propagate to main().catch → exit(1). The documented escape hatch for
  // air-gapped/dev environments is SHINGAN_SKIP_POSTINSTALL=1 (handled at
  // the top of main()), which skips the download entirely — there is no
  // "download but skip verification" mode by design.
  let checksumsText;
  try {
    checksumsText = await fetchText(checksumURL());
  } catch (e) {
    throw new Error(
      `unable to fetch checksums.txt for integrity verification: ${e.message}. ` +
        `Refusing to install an unverified binary. ` +
        `If the release was just published, GitHub may need a few seconds to ` +
        `propagate — retry shortly. For air-gapped installs set ` +
        `SHINGAN_SKIP_POSTINSTALL=1 and provide the binary yourself.`
    );
  }
  const expected = verifyChecksum(archiveBuf, archive, checksumsText);
  console.log(`shingan-lint: sha256 verified (${expected.slice(0, 12)}…)`);

  await extractTar(archiveBuf, dest, tag);

  if (!fs.existsSync(binPath)) {
    console.error(`shingan-lint: binary ${tag.exe} missing after extraction in ${dest}.`);
    console.error(`Archive may have a different layout than expected. Files extracted:`);
    for (const f of fs.readdirSync(dest)) console.error(`  ${f}`);
    process.exit(1);
  }

  // Ensure executable bit (no-op on Windows).
  try { fs.chmodSync(binPath, 0o755); } catch (_) {}

  console.log(`shingan-lint: installed ${tag.exe} → ${binPath}`);
}

// Export the pure helpers so the regression test (test/postinstall.test.js)
// can exercise the fail-closed checksum gate without any network.
module.exports = { verifyChecksum, findExpectedHash, sha256, main };

// Only auto-run the installer when invoked directly (`node postinstall.js`,
// i.e. as npm's postinstall hook). When `require()`d by the test, do nothing.
if (require.main === module) {
  main().catch((err) => {
    console.error(`shingan-lint: postinstall failed: ${err.stack || err.message}`);
    process.exit(1);
  });
}
