# Contributing to confik

Thanks for helping make `confik` faster and more reliable. This guide keeps contributions focused and easy to review.

## Quick Start

1. Fork and clone the repo.
2. Install Go (1.20+ recommended).
3. Run tests:

```bash
go test ./...
```

## Project Structure

- `main.go`: CLI implementation
- `registry.json`: Registry of config files that **do not** need to be staged
- `packages/`: Platform npm packages (one per OS/arch)
- `scripts/`: Build and install helpers

## Registry Policy (Patterns)

We want the registry to reflect **widely‑used tools** that already support `.config/` (or equivalent). To keep this list sane:

- We **do not** add patterns for tools that are not widely adopted.
- A tool should have **at least 100 GitHub stars** before we consider adding its config to `registry.json`.
- If a tool is popular but still doesn’t support `.config/`, it should not be in the registry (because it must be staged).

If you want a pattern that doesn’t meet the threshold, use `registryOverride` in your local `.config/confik.json` instead.

## Code Style

- Keep functions small and focused.
- Avoid heavy dependencies. startup speed matters.
- Prefer pure functions for helpers (easier to test).

## Tests

Please include tests for behavior changes or bug fixes:

```bash
go test ./...
```

## Versioning and Release

Platform package versions are kept **in the repo** to match the root `package.json`. Before releasing:

```bash
pnpm run sync-versions
```

Commit the resulting changes. CI will fail if versions drift.

## Release Notes

If your change impacts behavior or install flow, add a short note in your PR description so we can include it in release notes.
