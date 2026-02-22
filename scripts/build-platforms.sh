#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$REPO_ROOT/npm-platforms"
VERSION=$(node -p "require('$REPO_ROOT/package.json').version")
COMMIT=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "Building agent-chat v${VERSION} (${COMMIT})"

# ── 1. Bundle the client UI (embedded into the Go binary) ────────────────
echo "→ Bundling client UI…"
make -C "$REPO_ROOT" bundle-client

# ── 2. Cross-compile Go binary for each platform ─────────────────────────
TARGETS=(
  "linux   amd64  linux-x64"
  "linux   arm64  linux-arm64"
  "darwin  amd64  darwin-x64"
  "darwin  arm64  darwin-arm64"
  "windows amd64  win32-x64"
  "windows arm64  win32-arm64"
)

rm -rf "$OUT_DIR"

for target in "${TARGETS[@]}"; do
  read -r goos goarch pkg_suffix <<< "$target"

  pkg_dir="$OUT_DIR/$pkg_suffix"
  bin_dir="$pkg_dir/bin"
  mkdir -p "$bin_dir"

  bin_name="agent-chat"
  if [ "$goos" = "windows" ]; then
    bin_name="agent-chat.exe"
  fi

  echo "→ Compiling ${goos}/${goarch}…"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -C "$REPO_ROOT" -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o "$bin_dir/$bin_name" .

  # Map goarch to npm cpu field
  case "$goarch" in
    amd64) npm_cpu="x64" ;;
    arm64) npm_cpu="arm64" ;;
  esac

  # Map goos to npm os field
  case "$goos" in
    linux)   npm_os="linux" ;;
    darwin)  npm_os="darwin" ;;
    windows) npm_os="win32" ;;
  esac

  cat > "$pkg_dir/package.json" <<PKGJSON
{
  "name": "@choonkeat/agent-chat-${pkg_suffix}",
  "version": "${VERSION}",
  "description": "agent-chat binary for ${goos}/${goarch}",
  "license": "MIT",
  "os": ["${npm_os}"],
  "cpu": ["${npm_cpu}"],
  "files": ["bin/"]
}
PKGJSON

  echo "  ✓ npm-platforms/${pkg_suffix} ($(du -h "$bin_dir/$bin_name" | cut -f1))"
done

echo ""
echo "Done. Platform packages are in ${OUT_DIR}/"
ls -1 "$OUT_DIR"
