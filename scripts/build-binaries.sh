#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

build_target() {
  local goos="$1"
  local goarch="$2"
  local pkg="$3"
  local outfile="$4"

  echo "Building ${pkg} (${goos}/${goarch})"
  (cd "${ROOT_DIR}" && GOOS="${goos}" GOARCH="${goarch}" go build -o "${ROOT_DIR}/packages/${pkg}/${outfile}")
}

build_target "darwin" "arm64" "confik-darwin-arm64" "confik"
build_target "darwin" "amd64" "confik-darwin-x64" "confik"
build_target "linux" "amd64" "confik-linux-x64" "confik"
build_target "windows" "amd64" "confik-win32-x64" "confik.exe"
