#!/usr/bin/env bash
# Build the 10 platforms that MiniWiki's CI has successfully cross-compiled.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "Usage: $0 VERSION OUTPUT_DIRECTORY (for example: v0.24.3-beta build/release)" >&2
  exit 2
fi

version="$1"
out_dir="$2"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
  echo "Invalid release version: $version" >&2
  exit 2
fi

mkdir -p "$out_dir"
platforms=(
  linux-amd64 linux-arm64 windows-amd64
  darwin-amd64 darwin-arm64
  freebsd-amd64 freebsd-arm64
  openbsd-amd64 openbsd-arm64
  netbsd-amd64
)

for platform in "${platforms[@]}"; do
  os="${platform%-*}"
  arch="${platform##*-}"
  suffix=""
  if [[ "$os" == windows ]]; then suffix=".exe"; fi
  echo "Building $platform..."
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build \
    -mod=readonly -trimpath \
    -ldflags="-s -w -X main.version=${version#v}" \
    -o "$out_dir/miniwiki-$platform$suffix" \
    ./cmd/wiki
done
echo "Built ${#platforms[@]} executables for $version."
