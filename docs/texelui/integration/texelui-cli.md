# TexelUI CLI (Bash)

This example opens a form, waits for Run/Exit, runs a command with the form
values, streams output to a text area, and updates a status label. The loop
keeps running until Exit is pressed.

```bash
#!/usr/bin/env bash
set -euo pipefail

session=$(texelui open --spec - <<'JSON'
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

while true; do
  event=$(texelui wait --events click:run,click:exit)
  case "$event" in
    click:exit) break ;;
    click:run)
      eval "$(texelui get --ids root,pattern,max_depth,follow --format sh)"

      texelui set --id status --text "Running..."
      cmd=(find "$root")
      [ -n "$max_depth" ] && cmd+=(-maxdepth "$max_depth")
      [ "$follow" = "true" ] && cmd+=(-L)
      cmd+=(-name "$pattern")

      if texelui run --stdout log --stderr log --clear log -- "${cmd[@]}"; then
        texelui set --id status --text "Done."
      else
        rc=$?
        texelui set --id status --text "Failed (rc=$rc)."
      fi
      ;;
  esac
done

texelui close
```

Notes:
- `texelui` auto-starts its background server and uses `TEXELUI_SESSION`.
- When running from this repo, you can use `go run ./cmd/texelui` in place of `texelui`.
