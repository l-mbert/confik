"use strict";

const fs = require("fs");
const path = require("path");

const rootDir = path.join(__dirname, "..");
const rootPkgPath = path.join(rootDir, "package.json");
const rootPkg = JSON.parse(fs.readFileSync(rootPkgPath, "utf8"));
const version = rootPkg.version;

const platformPackages = [
  { dir: "confik-darwin-arm64", name: "@confik/darwin-arm64" },
  { dir: "confik-darwin-x64", name: "@confik/darwin-x64" },
  { dir: "confik-linux-x64", name: "@confik/linux-x64" },
  { dir: "confik-win32-x64", name: "@confik/win32-x64" },
];

const checkOnly = process.argv.includes("--check");
const problems = [];
let rootChanged = false;

function writeJson(filePath, payload) {
  fs.writeFileSync(filePath, JSON.stringify(payload, null, 2) + "\n");
}

function ensureOptionalDeps() {
  if (!rootPkg.optionalDependencies || typeof rootPkg.optionalDependencies !== "object") {
    rootPkg.optionalDependencies = {};
    rootChanged = true;
  }

  for (const pkg of platformPackages) {
    const current = rootPkg.optionalDependencies[pkg.name];
    if (current !== version) {
      if (checkOnly) {
        problems.push(`optionalDependencies.${pkg.name} = ${current || "<missing>"}`);
      } else {
        rootPkg.optionalDependencies[pkg.name] = version;
        rootChanged = true;
      }
    }
  }
}

function syncPlatformPackages() {
  for (const pkg of platformPackages) {
    const pkgPath = path.join(rootDir, "packages", pkg.dir, "package.json");
    const data = fs.readFileSync(pkgPath, "utf8");
    const payload = JSON.parse(data);

    if (payload.name !== pkg.name) {
      if (checkOnly) {
        problems.push(`${path.relative(rootDir, pkgPath)} name = ${payload.name || "<missing>"}`);
      } else {
        payload.name = pkg.name;
      }
    }

    if (payload.version !== version) {
      if (checkOnly) {
        problems.push(`${path.relative(rootDir, pkgPath)} version = ${payload.version || "<missing>"}`);
      } else {
        payload.version = version;
      }
    }

    if (!checkOnly) {
      writeJson(pkgPath, payload);
    }
  }
}

ensureOptionalDeps();
syncPlatformPackages();

if (checkOnly) {
  if (problems.length > 0) {
    console.error("confik: version mismatch detected:");
    for (const problem of problems) {
      console.error(`- ${problem}`);
    }
    process.exit(1);
  }
  process.exit(0);
}

if (rootChanged) {
  writeJson(rootPkgPath, rootPkg);
}
