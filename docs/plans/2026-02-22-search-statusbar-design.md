# Search in Status Bar Design

## Problem

The search overlay (HistoryNavigator) is a 2-line card at the bottom of the terminal that overlaps terminal content. Moving it into the status bar integrates it cleanly into the existing UI chrome.

## Design

### Status Bar Modes

**Normal mode** (default):
```
│ key hints from focused widget          hover help / timed messages │
```

**Search mode** (Ctrl+G or search toggle click):
```
│ 🔍 [search input......] ◀Prev Next▶ 1/42   Tab:Next S-Tab:Prev Esc:Close │
```

- Left zone: search icon + input (flexible, min ~15 chars) + prev/next buttons + counter (fixed width)
- Right zone: search keymap hints; high-priority messages (errors/warnings) temporarily override

### Toggle Button

New search toggle in the top-right overlay (alongside TFM, TUI, WRP, ALT):
- Icon: nf-md-magnify (󰍉) or similar
- True toggle: click opens search when off, closes when on
- Active state tracks HistoryNavigator visibility

### Rendering

When search opens:
1. Status bar left widgets → search widgets (icon, input, prev/next, counter)
2. Status bar right zone → persistent search hints
3. HistoryNavigator stops rendering its own 2-line overlay

When search closes:
1. Status bar reverts to normal mode
2. Search toggle goes inactive

### Changes by File

**`toggle_overlay.go`**: Add search toggle button, wire open/close.

**`history_navigator.go`**:
- Remove row 2 (keymap hints) → hints move to status bar
- Stop rendering 2-line overlay (Render becomes no-op)
- Expose search widgets for status bar to host
- Keep all search logic, scroll animation, highlighting

**`term.go`**:
- Search open: set status bar left widgets + right hint text
- Search close: clear left widgets + hint text
- Ctrl+G toggles (open if closed, close if open)

**`texelui/widgets/statusbar.go`**:
- Add persistent right-zone text (for search hints)
- Coexists with timed messages (messages override temporarily)
