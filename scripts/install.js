"use strict";

const fs = require("fs");
const path = require("path");

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
  console.error("confik: try reinstalling or check npm/yarn optionalDependencies settings.");
  process.exit(1);
}

const source = path.join(pkgDir, target.bin);
const binDir = path.join(__dirname, "..", "bin");
const dest = path.join(binDir, platform === "win32" ? "confik.exe" : "confik");

fs.mkdirSync(binDir, { recursive: true });

try {
  fs.copyFileSync(source, dest);
  fs.chmodSync(dest, 0o755);
  if (platform === "win32") {
    const altDest = path.join(binDir, "confik");
    fs.copyFileSync(source, altDest);
  }
} catch (err) {
  console.error(`confik: failed to install binary: ${err.message}`);
  process.exit(1);
}
