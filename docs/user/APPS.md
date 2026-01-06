# Texelation Apps & Usage

This guide summarizes the built-in apps, how to open them, and the key behaviours users should expect. See the top-level `README.md` for global control-mode shortcuts and how to start the server/client pair.

## Accessing Apps

- **Launcher (Ctrl+A, then `l`)**: Enter control mode with `Ctrl+A`, press `l` to open the launcher overlay. Use Up/Down to navigate, `Enter` to launch the selected app in the active pane, `Esc` to close.
- **Help (F1 or Ctrl+A, then `h`)**: Opens the help overlay; `Esc` closes it.
- New panes default to the terminal unless configured otherwise; you can replace the active pane’s app via the launcher.

## App Catalog

### Launcher
- Floating overlay listing available built-in and registry apps.
- Navigation: Up/Down to move selection, `Enter` to launch in the active pane, `Esc` to close.
- Opens via `Ctrl+A` → `l` (control mode). Uses the registry, so wrapper apps placed under `~/.config/texelation/apps/` also appear.

### TexelTerm (Terminal)
- Default app in new panes; full terminal emulator rendered via tcell.
- Supports scrollback with mouse wheel, Shift+wheel (page), Alt+wheel (fine), and keyboard scroll shortcuts (Alt+PgUp/PgDn, Alt+Up/Down).
- Mouse selection and copy-friendly highlighting that respects theme colours.
- Bracketed paste and BEL-driven flash effect are enabled; resize updates are immediate.

### Status Bar
- Lives at the top of the workspace and shows workspace tabs, control-mode status, and the active pane title, with an embedded clock.
- Uses Powerline/Nerd Font separators for the tab look; falls back gracefully if the font lacks glyphs.
- Turns red while control mode is active.

### Clock
- Simple digital clock app (also embedded inside the status bar). Can be launched from the launcher if you want a standalone clock pane.

### Help
- Static help overlay with common shortcuts. Open with F1 or `Ctrl+A` → `h`; close with `Esc`.

### Flicker (Demo)
- Visual pipeline demo that alternates coloured backgrounds to validate rendering and card composition. Accessible from the launcher.

### TexelUI Demos
- TexelUI demos and CLI now live in the TexelUI repo (`github.com/framegrace/texelui`).
