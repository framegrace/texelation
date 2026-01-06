#!/usr/bin/env bash
set -euo pipefail

texelui_cmd=(./bin/texelui)
if [[ ! -x "${texelui_cmd[0]}" ]]; then
  echo "Missing ${texelui_cmd[0]}. Run: make build-apps" >&2
  exit 1
fi

session=$("${texelui_cmd[@]}" open --spec - <<'JSON'
{
  "title": "Find Files",
  "layout": { "type": "form", "gap": 1, "padding": 1, "label_width": 14 },
  "widgets": [
    { "id": "root", "type": "input", "label": "Root", "value": "." },
    { "id": "pattern", "type": "input", "label": "Pattern", "value": "*.go" },
    { "id": "max_depth", "type": "number", "label": "Max Depth", "value": 3 },
    { "id": "follow", "type": "checkbox", "label": "Follow symlinks", "value": false },
    { "id": "run", "type": "button", "label": "Run" },
    { "id": "exit", "type": "button", "label": "Exit" },
    { "id": "status", "type": "label", "text": "" },
    { "id": "log", "type": "textarea", "label": "Output", "height": 12, "readonly": true }
  ]
}
JSON
)

export TEXELUI_SESSION="$session"
trap '"${texelui_cmd[@]}" close >/dev/null 2>&1 || true' EXIT

while true; do
  event=$("${texelui_cmd[@]}" wait --events click:run,click:exit)
  case "$event" in
    click:exit) break ;;
    click:run)
      eval "$("${texelui_cmd[@]}" get --ids root,pattern,max_depth,follow --format sh)"

      "${texelui_cmd[@]}" set --id status --text "Running..."
      cmd=(find "$root")
      [ -n "$max_depth" ] && cmd+=(-maxdepth "$max_depth")
      [ "$follow" = "true" ] && cmd+=(-L)
      cmd+=(-name "$pattern")

      if "${texelui_cmd[@]}" run --stdout log --stderr log --clear log -- "${cmd[@]}"; then
        "${texelui_cmd[@]}" set --id status --text "Done."
      else
        rc=$?
        "${texelui_cmd[@]}" set --id status --text "Failed (rc=$rc)."
      fi
      ;;
  esac
done
