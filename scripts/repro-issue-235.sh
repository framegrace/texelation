#!/usr/bin/env bash
# Helper to capture zoom-debug logs for issue #235 repros.
#
# Usage:
#   scripts/repro-issue-235.sh s1   # symptom 1: htop overlay over zoomed shell
#   scripts/repro-issue-235.sh s2   # symptom 2: zoomed alt-screen quadrant
#   scripts/repro-issue-235.sh s3   # symptom 3: server-emit wedge
#   scripts/repro-issue-235.sh s4   # symptom 4: help dialog behind zoom
#
# Steps performed:
#   1. Stops any running texelation daemon (so the env vars take effect).
#   2. Removes the staging log so we don't carry old lines forward.
#   3. Prints the repro recipe for the chosen symptom.
#   4. Starts texelation with TEXELATION_DEBUG_ZOOM and
#      TEXELATION_DEBUG_ZOOM_FILE set.
#   5. After you exit texelation, copies the log into
#      docs/superpowers/captures/2026-05-06-issue-235/<symptom>.log
#      with a header block (date, commit, what-you-saw prompt).

set -euo pipefail

SYMPTOM="${1:-}"
case "$SYMPTOM" in
  s1|s2|s3|s4) ;;
  *)
    echo "usage: $0 {s1|s2|s3|s4}" >&2
    echo >&2
    echo "  s1  htop overlay over zoomed shell" >&2
    echo "  s2  zoomed alt-screen renders top-left quadrant only" >&2
    echo "  s3  server stops emitting after N zoom toggles" >&2
    echo "  s4  Ctrl+a help dialog hidden behind zoom" >&2
    exit 2
    ;;
esac

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BIN=./bin/texelation
if [[ ! -x "$BIN" ]]; then
  echo "$BIN not found; run \`make build\` first" >&2
  exit 1
fi

CAPTURES_DIR="docs/superpowers/captures/2026-05-06-issue-235"
STAGING_LOG="/tmp/zoomdebug-${SYMPTOM}-staging.log"
FINAL_LOG="${CAPTURES_DIR}/${SYMPTOM}.log"

mkdir -p "$CAPTURES_DIR"

cat <<EOF

=== issue #235 repro: ${SYMPTOM} ===

EOF

case "$SYMPTOM" in
  s1)
    cat <<'EOF'
SETUP: 2-pane horizontal split. Run htop in the LEFT pane. Focus
       the RIGHT pane (a normal shell).

ACTION: Zoom the shell pane (Ctrl+a z). Wait 5-10 seconds without
        typing. Watch for htop content appearing over the zoomed
        shell. Press one key in the shell to clear the overlay.
        Wait again, repeat 2-3 cycles.

CAPTURE WINDOW: From the zoom keystroke through 3 overlay/clear
        cycles. Then press Ctrl+a q to exit texelation.
EOF
    ;;
  s2)
    cat <<'EOF'
SETUP: 2-pane horizontal split. Run htop in EITHER pane.

ACTION: Focus the htop pane. Zoom it (Ctrl+a z). Observe the
        screen — does htop content fill the whole zoom area, or
        only a top-left rectangle? Unzoom (Ctrl+a z).

CAPTURE WINDOW: One zoom + one unzoom is enough. Then press
        Ctrl+a q to exit texelation.
EOF
    ;;
  s3)
    cat <<'EOF'
SETUP: 2-pane horizontal split. Run htop in either pane.

ACTION: Toggle zoom (Ctrl+a z), wait ~1 second, toggle again.
        Repeat 5-10 times. Watch for the screen no longer
        responding to zoom toggles. Note the cycle count when
        the wedge first appears (write it down).

CAPTURE WINDOW: From the first toggle through the wedge. Then
        press Ctrl+a q to exit (or texelation --stop in another
        terminal if it's wedged).

NOTE: This symptom did not reproduce on the reset baseline in
      the previous capture run. If you cannot trigger it within
      ~15 toggles, write "did not reproduce" in the header and
      move on.
EOF
    ;;
  s4)
    cat <<'EOF'
SETUP: Any pane.

ACTION: Zoom it (Ctrl+a z). Press Ctrl+a (enter control mode) -
        the help dialog modal should appear. Observe whether it
        renders ON TOP (correct, expected on the reset baseline)
        or BEHIND the zoomed pane (the original bug). Press Esc
        to dismiss the modal.

CAPTURE WINDOW: One zoom + one Ctrl+a help open + Esc. Then
        press Ctrl+a q to exit texelation.

NOTE: Expected outcome on this baseline is "dialog renders on
      top". If that's what you see, write that in the header -
      it confirms the dropped commit ec76646 was the cause.
EOF
    ;;
esac

echo
echo "Press Enter when you're ready to start the capture, or Ctrl+C to abort."
read -r

# Stop any running daemon so the new env vars take effect.
echo "Stopping any existing daemon..."
"$BIN" --stop 2>/dev/null || true
# Brief settle so the lock file releases.
sleep 1

# Remove stale staging log (don't touch the captures dir).
rm -f "$STAGING_LOG"

echo "Starting texelation with TEXELATION_DEBUG_ZOOM=1 ..."
echo "    log file: $STAGING_LOG"
echo "Reproduce the symptom, then exit texelation (Ctrl+a q)."
echo

# Run texelation in the foreground; both client and server write to the
# same log file with [zoom-debug client] / [zoom-debug server] prefixes.
TEXELATION_DEBUG_ZOOM=1 \
TEXELATION_DEBUG_ZOOM_FILE="$STAGING_LOG" \
  "$BIN"

echo
echo "texelation exited."

if [[ ! -s "$STAGING_LOG" ]]; then
  echo "WARNING: $STAGING_LOG is empty. Did the daemon inherit the env vars?" >&2
  echo "Check: cat /proc/\$(cat ~/.texelation/texelation.pid)/environ | tr '\\0' '\\n' | grep ZOOM" >&2
  exit 1
fi

# Capture commit hash and basic counts for the header.
COMMIT=$(git rev-parse HEAD)
DATE=$(date -Is)
CLIENT_LINES=$(grep -c "\[zoom-debug client\]" "$STAGING_LOG" || true)
SERVER_LINES=$(grep -c "\[zoom-debug server\]" "$STAGING_LOG" || true)
TOTAL_LINES=$(wc -l < "$STAGING_LOG")

echo
echo "Captured $TOTAL_LINES lines (client=$CLIENT_LINES, server=$SERVER_LINES)."
echo
echo "Now describe what you observed (single line; press Enter when done):"
read -r OBSERVATION

EXTRA=""
if [[ "$SYMPTOM" == "s3" ]]; then
  echo "Cycle count when wedge appeared (or 'did not reproduce'):"
  read -r CYCLE
  EXTRA="Cycle: ${CYCLE}"
fi

# Build the final log with a header block.
{
  echo "# Issue #235 capture - ${SYMPTOM}"
  echo "Date: ${DATE}"
  echo "Commit: ${COMMIT}"
  echo "Lines: total=${TOTAL_LINES} client=${CLIENT_LINES} server=${SERVER_LINES}"
  if [[ -n "$EXTRA" ]]; then
    echo "${EXTRA}"
  fi
  echo "Observed: ${OBSERVATION}"
  echo "---"
  cat "$STAGING_LOG"
} > "$FINAL_LOG"

echo
echo "Saved capture to $FINAL_LOG"
echo
echo "Next: review the log, then if you want it on the branch:"
echo "  git add -f $FINAL_LOG"
echo "  git commit -m 'Capture issue #235 ${SYMPTOM} run'"
