# confik

`confik` is a small CLI that temporarily stages files from `.config/` into the project root while a command runs, then cleans them up afterward. The published npm package ships native Go binaries, with a tiny Node.js shim that dispatches to the correct platform binary.

## Install

```bash
npm install @confik/cli
# or
yarn add @confik/cli
# or
pnpm add @confik/cli
# or
bun add @confik/cli
```

## Usage

```bash
confik yarn dev
confik -- vite build
confik --dry-run npm run test
confik --clean
```

## Behavior

- Looks for `.config/` in the current working directory.
- Copies eligible files into the project root before running your command.
- Never overwrites existing root files (they are skipped).
- Removes staged files on exit (including `SIGINT`, `SIGTERM`, `SIGHUP`).
- Adds a temporary block to `.git/info/exclude` so staged files are not accidentally committed.
- Uses a lock file in `.config/` to serialize concurrent runs in the same directory.

## Config

Create `.config/confik.json`:

```json
{
  "$schema": "https://raw.githubusercontent.com/l-mbert/confik/refs/heads/main/confik.schema.json",
  "exclude": ["**/*.local", "private/**"],
  "registry": true,
  "registryOverride": ["vite.config.ts"],
  "gitignore": true
}
```

- `exclude`: glob patterns (relative to `.config/`) to skip.
- `registry`: enable the built-in registry skip list.
- `registryOverride`: force-copy patterns that would otherwise be skipped by the registry.
- `gitignore`: enable temporary `.git/info/exclude` handling (default `true`).

## Registry

The built-in registry lives in `registry.json` and contains filenames that are considered safe to leave in `.config/` without copying. You can disable it with `--no-registry` or override with `registryOverride`.

## Cleanup

If the process crashes, you can run:

```bash
confik --clean
```

This removes any leftover staged files from `.config/.confik-manifest.json` and clears any `confik` blocks in `.git/info/exclude`.

## Build (local dev)

```bash
go build -o dist/confik
```

## Build platform packages (for publishing)

```bash
pnpm run build:binaries
```

This produces native binaries in `packages/` for `darwin/arm64`, `darwin/x64`, `linux/x64`, and `win32/x64`.
