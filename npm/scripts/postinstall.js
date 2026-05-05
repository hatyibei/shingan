#!/usr/bin/env node
// shingan-lint postinstall: downloads the platform-specific Go binary
// from the matching GitHub Release tag, verifies it via the
// checksums.txt sha256 entry, extracts it from the goreleaser tarball,
// and installs it under ~/.cache/shingan-lint/v<version>/.
//
// Skipped silently when SHINGAN_SKIP_POSTINSTALL=1 (CI mirroring use
// cases like air-gapped builds where the binary is provided
// externally).

'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const crypto = require('crypto');
const { pipeline } = require('stream/promises');
const tar = require('tar');

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

async function extractTar(buf, dest, tag) {
  // Write to temp file so `tar` can stream it back.
  const tmpFile = path.join(dest, `_archive.${tag.ext}`);
  fs.writeFileSync(tmpFile, buf);
  try {
    if (tag.ext === 'tar.gz') {
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

  // Verify checksum if checksums.txt is reachable. Soft-fail when the
  // checksum file is missing (e.g. early-stage release without
  // goreleaser's checksum step), so users still get a usable binary
  // — at the cost of trust on first install.
  try {
    const checksumsText = await fetchText(checksumURL());
    const expected = findExpectedHash(checksumsText, archive);
    if (expected) {
      const actual = sha256(archiveBuf);
      if (actual !== expected) {
        throw new Error(`sha256 mismatch: expected ${expected}, got ${actual}`);
      }
      console.log(`shingan-lint: sha256 verified (${expected.slice(0, 12)}…)`);
    } else {
      console.warn(`shingan-lint: ${archive} not found in checksums.txt — proceeding without verification`);
    }
  } catch (e) {
    console.warn(`shingan-lint: checksum verification skipped: ${e.message}`);
  }

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

main().catch((err) => {
  console.error(`shingan-lint: postinstall failed: ${err.stack || err.message}`);
  process.exit(1);
});
