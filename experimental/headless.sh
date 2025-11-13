#!/usr/bin/env bash
set -Eeuo pipefail

WIDTH=1280
HEIGHT=720
FRAMERATE=30

HDIR="$(mktemp -d)"; chmod 700 "$HDIR"
LOGBASE="/tmp/wl-headless-$$"
SWAY_LOG="$LOGBASE.sway.log"
REC_LOG="$LOGBASE.rec.log"
GL_LOG="$LOGBASE.glmark.log"

CLEANED=0; CHILD_PIDS=()
cleanup() {
  ((CLEANED==0)) || return 0; CLEANED=1; set +e
  for pid in "${CHILD_PIDS[@]:-}"; do kill -TERM "$pid" 2>/dev/null || true; done
  sleep 0.2
  for pid in "${CHILD_PIDS[@]:-}"; do kill -KILL "$pid" 2>/dev/null || true; done
  rm -rf "$HDIR"
}
trap cleanup EXIT INT TERM HUP

export WLR_BACKENDS=headless
export WLR_LIBINPUT_NO_DEVICES=1
export WLR_HEADLESS_OUTPUTS=1
# If EGL flakes in headless, uncomment:
# export WLR_RENDERER=pixman
# export WLR_NO_HARDWARE_CURSORS=1

# 1) start sway (silent; logs to file)
XDG_RUNTIME_DIR="$HDIR" sway -d --unsupported-gpu >"$SWAY_LOG" 2>&1 &
CHILD_PIDS+=("$!")
sleep 0.4

# 2) sockets
WL_SOCK="$(basename "$(ls "$HDIR"/wayland-* | head -n1)")"
IPC_SOCK="$(ls "$HDIR"/sway-ipc.*.sock | head -n1 || true)"
[[ -n "${WL_SOCK:-}" && -n "${IPC_SOCK:-}" ]] || exit 1

swayc() { XDG_RUNTIME_DIR="$HDIR" swaymsg -s "$IPC_SOCK" "$@"; }

# 3) output + mode (best effort)
OUT_NAME="$(swayc -t get_outputs | jq -r '.[] | select(.active==true) | .name' | head -n1)"
[[ -n "$OUT_NAME" ]] || exit 1
if ! swayc output "$OUT_NAME" resolution ${WIDTH}x${HEIGHT} position 0,0 enable >/dev/null 2>&1; then
  ACT_W=$(swayc -t get_outputs | jq -r ".[] | select(.name==\"$OUT_NAME\") | .current_mode.width")
  ACT_H=$(swayc -t get_outputs | jq -r ".[] | select(.name==\"$OUT_NAME\") | .current_mode.height")
  [[ "$ACT_W" != "null" && "$ACT_H" != "null" ]] && WIDTH="$ACT_W" HEIGHT="$ACT_H"
fi

# 4) client (glmark2) — run forever; silence to log
env -u DISPLAY XDG_RUNTIME_DIR="$HDIR" WAYLAND_DISPLAY="$WL_SOCK" \
  glmark2-wayland --run-forever -s ${WIDTH}x${HEIGHT} >"$GL_LOG" 2>&1 &
CHILD_PIDS+=("$!")

# 5) recorder -> kitty_stream
# Use rawvideo to stdout ("-") to avoid any overwrite prompts.
# recorder -> kitty (raw BGR0 frames to stdout fd)
REC_ARGS=(
  -o "$OUT_NAME"
  --no-dmabuf
  --codec rawvideo
  --pixel-format bgr0
  --framerate "$FRAMERATE"
)

# add muxer flag if your build has it
if wf-recorder --help 2>/dev/null | grep -q -- '--muxer'; then
  REC_ARGS+=( --muxer rawvideo )
fi

# add --overwrite if supported (avoids “Output file exists. Overwrite? Y/n:”)
if wf-recorder --help 2>/dev/null | grep -q -- '--overwrite'; then
  REC_ARGS+=( --overwrite )
fi

env -u DISPLAY XDG_RUNTIME_DIR="$HDIR" WAYLAND_DISPLAY="$WL_SOCK" \
  wf-recorder "${REC_ARGS[@]}" --file=/proc/self/fd/1 2>"$REC_LOG" \
| ./kitty_stream -w "$WIDTH" -h "$HEIGHT" -pixfmt bgrx 
