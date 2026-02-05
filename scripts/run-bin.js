#!/usr/bin/env node
"use strict";

const path = require("path");
const { spawnSync } = require("child_process");

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
  console.error("confik: try reinstalling (npm install) or check optionalDependencies settings.");
  process.exit(1);
}

const binPath = path.join(pkgDir, target.bin);
const result = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`confik: failed to run native binary (${result.error.message})`);
  process.exit(1);
}

process.exit(result.status ?? 1);
