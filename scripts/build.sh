#!/usr/bin/env bash
# Builds frontend and backend into a single binary.
#   EWIKI_BUILD_OUT   output path (default: bin/ewiki)
#   EWIKI_VERSION     version string (default: git describe)
set -euo pipefail
cd "$(dirname "$0")/.."
OUT="${EWIKI_BUILD_OUT:-bin/ewiki}"
VERSION="${EWIKI_VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

echo "==> generating plugin imports"
go run ./tools/genplugins

echo "==> building frontend"
(
  cd web
  if [ ! -d node_modules/esbuild ]; then
    if [ -f package-lock.json ]; then npm ci --no-audit --no-fund; else npm install --no-audit --no-fund; fi
  fi
  node build.mjs
)

echo "==> building backend ($VERSION)"
mkdir -p "$(dirname "$OUT")"
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/mtsonline/enterprisewiki/internal/version.Version=${VERSION} -X github.com/mtsonline/enterprisewiki/internal/version.Commit=${COMMIT}" \
  -o "$OUT" ./cmd/ewiki
echo "==> done: $OUT"
