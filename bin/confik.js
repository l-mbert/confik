#!/usr/bin/env node
"use strict";

const path = require("path");
const { spawn } = require("child_process");

const platform = process.platform;
const arch = process.arch;

const map = new Map([
  ["darwin-arm64", { pkg: "@confik/darwin-arm64", bin: "confik" }],
  ["darwin-x64", { pkg: "@confik/darwin-x64", bin: "confik" }],
  ["linux-x64", { pkg: "@confik/linux-x64", bin: "confik" }],
  ["win32-x64", { pkg: "@confik/win32-x64", bin: "confik.exe" }],
]);

const key = `${platform}-${arch}`;
const target = map.get(key);

if (!target) {
  console.error(`confik: unsupported platform ${platform}-${arch}`);
  process.exit(1);
}

let pkgDir;
try {
  pkgDir = path.dirname(require.resolve(`${target.pkg}/package.json`));
} catch (err) {
  console.error(`confik: missing optional dependency ${target.pkg}.`);
  console.error("confik: try reinstalling or check optionalDependencies.");
  process.exit(1);
}

const binPath = path.join(pkgDir, target.bin);
const args = process.argv.slice(2);

// Use spawn with stdio: inherit to allow the child to interact with TTY
const child = spawn(binPath, args, { stdio: "inherit" });

// Handle child process exit
child.on("close", (code, signal) => {
  if (signal) {
    // If child was killed by a signal, exit with 1 (or mimic signal if possible)
    process.exit(1);
  } else {
    process.exit(code);
  }
});

// Forward signals to child?
// In TTY 'inherit' mode, Ctrl+C sends SIGINT to the process group, so both get it.
// We want Node to NOT exit immediately so child can finish cleanup.
// So we attach dummy listeners to keep Node alive/ignore the default exit behavior.
// The child will handle the signal and exit, triggering 'close'.

const signals = ["SIGINT", "SIGTERM", "SIGHUP"];
signals.forEach((sig) => {
  process.on(sig, () => {
    // Do nothing in the wrapper, let the child handle it.
    // If the child is already dead or doesn't exit, we might get stuck?
    // But confik is designed to exit on these signals.
  });
});

child.on("error", (err) => {
  console.error(`confik: failed to spawn binary: ${err.message}`);
  process.exit(1);
});
