#!/usr/bin/env node
// shingan CLI shim. Spawns the platform-specific Go binary downloaded
// by postinstall.js into ~/.cache/shingan-lint/v<version>/<binary>.
// Forwards every argv element + stdio + exit code, so `npx shingan
// analyze --input ./testdata` behaves identically to running the
// native binary directly.

'use strict';

const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

const PACKAGE_VERSION = require('../package.json').version;

function platformTag() {
  const p = process.platform;
  const a = process.arch;
  if (p === 'darwin' && a === 'arm64') return { os: 'darwin', arch: 'arm64', exe: 'shingan' };
  if (p === 'darwin' && a === 'x64') return { os: 'darwin', arch: 'amd64', exe: 'shingan' };
  if (p === 'linux' && a === 'arm64') return { os: 'linux', arch: 'arm64', exe: 'shingan' };
  if (p === 'linux' && a === 'x64') return { os: 'linux', arch: 'amd64', exe: 'shingan' };
  if (p === 'win32' && a === 'arm64') return { os: 'windows', arch: 'arm64', exe: 'shingan.exe' };
  if (p === 'win32' && a === 'x64') return { os: 'windows', arch: 'amd64', exe: 'shingan.exe' };
  throw new Error(`shingan-lint: unsupported platform/arch combination: ${p}/${a}`);
}

function cacheDir() {
  const base = process.env.SHINGAN_CACHE_DIR ||
    process.env.XDG_CACHE_HOME ||
    path.join(os.homedir(), '.cache');
  return path.join(base, 'shingan-lint', `v${PACKAGE_VERSION}`);
}

function binaryPath() {
  const t = platformTag();
  return path.join(cacheDir(), t.exe);
}

function main() {
  const bin = binaryPath();
  if (!fs.existsSync(bin)) {
    console.error(
      `shingan-lint: binary not found at ${bin}\n` +
      `Run \`npm rebuild shingan-lint\` (or \`pnpm rebuild shingan-lint\`) to re-trigger postinstall, ` +
      `or download manually from https://github.com/hatyibei/shingan/releases/tag/v${PACKAGE_VERSION}`
    );
    process.exit(1);
  }
  const child = spawn(bin, process.argv.slice(2), { stdio: 'inherit' });
  child.on('error', (err) => {
    console.error(`shingan-lint: failed to exec ${bin}: ${err.message}`);
    process.exit(1);
  });
  child.on('exit', (code, signal) => {
    if (signal) {
      // Mirror the signal: re-raise so shells see e.g. 130 for SIGINT.
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code === null ? 1 : code);
  });
}

main();
