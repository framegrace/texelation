#!/bin/bash

# This script will log all sequences sent to the terminal when running codex
# We'll use script to capture the raw output

LOGFILE="/tmp/codex_sequences.log"

echo "Starting codex with sequence logging to $LOGFILE"
echo "Press Ctrl+D to exit and view the log"

# Run codex through script to capture all output
script -q -c "codex" "$LOGFILE"

echo ""
echo "=== Raw sequences captured ==="
cat "$LOGFILE" | od -A x -t x1z -v | head -100
echo ""
echo "Full log saved to: $LOGFILE"
