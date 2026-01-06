#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"/.. && pwd)"
BASELINE_DIR="$ROOT_DIR/docs/baseline"
SCREENS_DIR="$BASELINE_DIR/screens"

mkdir -p "$BASELINE_DIR" "$SCREENS_DIR"

# Capture Go environment
GOCACHE="$ROOT_DIR/.cache" go env > "$BASELINE_DIR/go-env.txt"

# Capture module list
GOCACHE="$ROOT_DIR/.cache" go list ./... > "$BASELINE_DIR/module-list.txt"

# Optionally import screenshot passed as argument
if [[ $# -gt 0 ]]; then
    for shot in "$@"; do
        if [[ -f "$shot" ]]; then
            dest="$SCREENS_DIR/$(basename "$shot")"
            cp "$shot" "$dest"
            echo "Stored screenshot $dest"
        else
            echo "Warning: screenshot $shot not found" >&2
        fi
    done
fi

echo "Baseline capture complete."
