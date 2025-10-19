#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
CLIENT_PKG="./client/cmd/texel-client"
SERVER_PKG="./cmd/texel-server"

TARGETS=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<<"$target"
  out_dir="$DIST_DIR/${GOOS}-${GOARCH}"
  mkdir -p "$out_dir"

  server_name="texel-server"
  client_name="texel-client"
  if [[ "$GOOS" == "windows" ]]; then
    server_name+=".exe"
    client_name+=".exe"
  fi

  echo "building $GOOS/$GOARCH"
  env CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -o "$out_dir/$server_name" "$SERVER_PKG"
  env CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" \
    go build -trimpath -o "$out_dir/$client_name" "$CLIENT_PKG"

done

echo "binaries written to $DIST_DIR"
