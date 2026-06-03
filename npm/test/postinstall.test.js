#!/usr/bin/env node
// Regression test for #29: the postinstall checksum gate must FAIL-CLOSED.
//
// Two layers:
//   1. Unit — verifyChecksum() throws on mismatch / missing-entry and only
//      returns on an exact match (no network).
//   2. End-to-end — run the real postinstall.js against a local HTTP fixture
//      (via SHINGAN_DOWNLOAD_BASE) serving a tampered archive, and assert the
//      process exits NON-ZERO and never installs the binary.
//
// Uses only the Node standard library (node:test) — no extra dev deps, and
// test/ is excluded from the published package via .npmignore.

'use strict';

const test = require('node:test');
const assert = require('node:assert');
const crypto = require('crypto');
const http = require('http');
const os = require('os');
const fs = require('fs');
const path = require('path');
const { execFile } = require('child_process');

const pi = require('../scripts/postinstall.js');

const sha256 = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

// ---------------------------------------------------------------------------
// 1. Unit: verifyChecksum is fail-closed.
// ---------------------------------------------------------------------------

test('verifyChecksum returns the expected hash on an exact match', () => {
  const buf = Buffer.from('good archive bytes');
  const name = 'shingan_0.9.0_linux_amd64.tar.gz';
  const checksums = `${sha256(buf)}  ${name}\n`;
  assert.strictEqual(pi.verifyChecksum(buf, name, checksums), sha256(buf));
});

test('verifyChecksum throws on a sha256 mismatch (tampered archive)', () => {
  const buf = Buffer.from('tampered archive bytes');
  const name = 'shingan_0.9.0_linux_amd64.tar.gz';
  const wrongHash = sha256(Buffer.from('the original archive'));
  const checksums = `${wrongHash}  ${name}\n`;
  assert.throws(() => pi.verifyChecksum(buf, name, checksums), /sha256 mismatch/);
});

test('verifyChecksum throws when the archive is absent from checksums.txt', () => {
  const buf = Buffer.from('some archive');
  const name = 'shingan_0.9.0_linux_amd64.tar.gz';
  // checksums.txt lists a *different* file only — ours is unverifiable.
  const checksums = `${sha256(buf)}  shingan_0.9.0_darwin_arm64.tar.gz\n`;
  assert.throws(() => pi.verifyChecksum(buf, name, checksums), /not found in checksums\.txt/);
});

test('verifyChecksum throws on an empty checksums.txt', () => {
  const buf = Buffer.from('some archive');
  const name = 'shingan_0.9.0_linux_amd64.tar.gz';
  assert.throws(() => pi.verifyChecksum(buf, name, ''), /not found in checksums\.txt/);
});

// ---------------------------------------------------------------------------
// 2. End-to-end: a tampered download aborts the install with a non-zero exit.
// ---------------------------------------------------------------------------

function archiveNameFor() {
  const pkg = require('../package.json');
  const p = process.platform;
  const a = process.arch;
  const osName = p === 'darwin' ? 'darwin' : p === 'win32' ? 'windows' : 'linux';
  const arch = a === 'arm64' ? 'arm64' : 'amd64';
  const ext = p === 'win32' ? 'zip' : 'tar.gz';
  return `shingan_${pkg.version}_${osName}_${arch}.${ext}`;
}

// Serves: a valid archive body, but a checksums.txt whose hash does NOT match
// → the install must reject it (mismatch) rather than proceed.
function startTamperFixture(archive) {
  const body = Buffer.from('this is not really a tarball, but the hash is what matters');
  const wrongHash = sha256(Buffer.from('a totally different file'));
  const checksums = `${wrongHash}  ${archive}\n`;
  const server = http.createServer((req, res) => {
    if (req.url.endsWith('/checksums.txt')) {
      res.writeHead(200, { 'content-type': 'text/plain' });
      res.end(checksums);
    } else if (req.url.endsWith(`/${archive}`)) {
      res.writeHead(200, { 'content-type': 'application/octet-stream' });
      res.end(body);
    } else {
      res.writeHead(404);
      res.end('not found');
    }
  });
  return server;
}

test('postinstall aborts (non-zero exit) on a checksum mismatch and installs nothing', (t, done) => {
  const archive = archiveNameFor();
  const server = startTamperFixture(archive);
  server.listen(0, '127.0.0.1', () => {
    const { port } = server.address();
    const base = `http://127.0.0.1:${port}/download`;
    const cacheDir = fs.mkdtempSync(path.join(os.tmpdir(), 'shingan-postinstall-test-'));

    const env = {
      ...process.env,
      SHINGAN_DOWNLOAD_BASE: base,
      SHINGAN_CACHE_DIR: cacheDir,
      // Make sure the global skip hatch is not accidentally set in the env.
      SHINGAN_SKIP_POSTINSTALL: '',
    };

    execFile(
      process.execPath,
      [path.join(__dirname, '..', 'scripts', 'postinstall.js')],
      { env, timeout: 30000 },
      (err, stdout, stderr) => {
        server.close();
        try {
          // Must have exited non-zero (fail-closed).
          assert.ok(err, 'expected postinstall to exit non-zero on checksum mismatch');
          assert.notStrictEqual(err.code, 0, 'exit code must be non-zero');
          // Must surface an actionable mismatch error.
          assert.match(stderr + stdout, /sha256 mismatch/i);
          // Must NOT have installed any binary.
          const installed = fs.existsSync(cacheDir)
            ? fs.readdirSync(cacheDir).filter((f) => f.startsWith('shingan'))
            : [];
          assert.deepStrictEqual(
            installed.filter((f) => f === 'shingan' || f === 'shingan.exe'),
            [],
            'no binary should be installed when the checksum fails'
          );
          done();
        } catch (e) {
          done(e);
        } finally {
          fs.rmSync(cacheDir, { recursive: true, force: true });
        }
      }
    );
  });
});
