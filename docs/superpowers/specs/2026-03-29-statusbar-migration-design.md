# Status Bar Migration to TexelUI Widget System

**Issue**: #135
**Date**: 2026-03-29
**Status**: Approved

## Overview

Migrate `apps/statusbar/` from a custom cell-buffer renderer to a `adapter.UIApp` using the texelui widget system. Split the monolithic `StatePayload` event into fine-grained events. Add editable workspace names, per-workspace accent colors, and toast messages. Register via the app registry for pluggability.

## Architecture

### Widget Composition

The status bar is an `adapter.UIApp` with `UIManager` managing two widgets:

| Widget | Position | Height | Purpose |
|--------|----------|--------|---------|
| `TabBar` (texelui) | Row 0 | 1 | Workspace tabs with powerline separators, edit mode |
| `BlendInfoLine` (custom) | Row 1 | 1 | Gradient blend + overlaid mode/title/fps/clock + toasts |

Total status pane height: **2 rows** (fixed, never changes).

**TabBar** (row 0):
- texelui `primitives.TabBar` with `Style.NoBlendRow = true`
- Tab items from `[]WorkspaceInfo` — labels are workspace names
- Per-workspace accent color on active tab
- Active tab bg matches desktop bg for seamless blending
- Keyboard nav: arrow keys, 1-9 jump, Enter to edit, double-click to edit

**BlendInfoLine** (row 1):
- Custom widget in the status bar app (not in texelui — desktop-specific)
- 3-stop OKLCH gradient: `activeAccentColor → activeAccentColor → contentBG`
- Left side: mode icon + active pane title
- Right side: fps counter + clock (HH:MM:SS)
- Text fg computed for contrast against gradient at each position
- **Toast mode**: temporarily replaces normal content with a message. Gradient shifts to severity color (red=error, green=success, yellow=warning, blue=info). Auto-dismisses after configurable duration (default 3s). Returns to normal blend line after dismiss.

### Layout

`UIApp.SetOnResize` positions widgets:
- TabBar at (0, 0), width = viewport width, height = 1
- BlendInfoLine at (0, 1), width = viewport width, height = 1

The desktop calls `AddStatusPane(statusbar, SideTop, 2)` — changed from 1 to 2.

## Event System Refactor

Replace `EventStateUpdate`/`StatePayload` with fine-grained events:

| Event | Frequency | Payload |
|-------|-----------|---------|
| `EventWorkspacesChanged` | Rare | `[]WorkspaceInfo` (ID, Name, Color), `ActiveID int` |
| `EventWorkspaceSwitched` | Low | `ActiveID int` |
| `EventModeChanged` | Low | `InControlMode bool`, `SubMode rune` |
| `EventActivePaneChanged` | Low | `ActiveTitle string` |
| `EventPerformanceUpdate` | High (~60fps) | `LastPublishDuration time.Duration` |
| `EventToast` | Rare | `Message string`, `Severity int`, `Duration time.Duration` |

The desktop engine broadcasts each independently. `EventPerformanceUpdate` fires from the publish loop; workspace events fire only on actual state changes.

`EventStateUpdate` and `StatePayload` are removed. Any other listeners migrate to the new event types.

### Event Routing in Status Bar

```
EventWorkspacesChanged  → TabBar (rebuild tabs)
EventWorkspaceSwitched  → TabBar (SetActive) + BlendInfoLine (accent color)
EventModeChanged        → BlendInfoLine (mode icon)
EventActivePaneChanged  → BlendInfoLine (title text)
EventPerformanceUpdate  → BlendInfoLine (fps counter)
EventToast              → BlendInfoLine (toast mode)
```

Clock is an internal ticker (1s interval), not a desktop event.

## Workspace Colors & Names

### WorkspaceInfo

```go
type WorkspaceInfo struct {
    ID    int
    Name  string      // user-editable, defaults to "default" (ws 1) or "" (others)
    Color tcell.Color // auto-assigned from palette
}
```

### Color Palette

8 visually distinct accent colors defined in theme semantic colors (`workspace.accent.1` through `workspace.accent.8`). New workspaces get the next color in sequence, wrapping around. Colors are stored on the `Workspace` struct.

### Names

- Workspace 1 is named `"default"`
- Workspace 2+ start with an empty name, which triggers the TabBar to enter edit mode immediately on creation
- If the user presses Escape or confirms an empty name, the workspace number is used as fallback

### Creation Flow

1. User presses Ctrl-2 (or Ctrl-N)
2. Desktop creates workspace, assigns next palette color, broadcasts `EventWorkspacesChanged`
3. Status bar receives event, detects new workspace (ID > 1)
4. Status bar calls `TabBar.EditTab(newTabIndex)` to enter edit mode
5. User types name, presses Enter
6. `OnRename` callback fires → status bar calls `DesktopEngine.RenameWorkspace(id, name)`
7. Desktop updates workspace name, broadcasts `EventWorkspacesChanged`

### Persistence

Names and colors persist via the existing `SnapshotStore` workspace metadata serialization.

## TabBar Edit Mode (texelui change)

New capabilities added to `primitives.TabBar`:

### API

- `EditTab(index int)` — programmatically enters edit mode on a tab
- `CancelEdit()` — cancels any active edit
- `OnRename func(index int, newName string)` — callback on Enter (confirm)
- `OnEditCancel func(index int)` — callback on Escape (cancel)

### Behavior

- Double-click on a tab label enters edit mode
- Input is pre-filled with current label, text selected
- Enter confirms, Escape cancels (reverts to previous label)
- Clicking outside the editing tab confirms the edit
- Tab key confirms the edit (not focus advance — Tab key is for tab switching)
- The editing tab remains visually active (same styling as selected tab)

### Rendering

The edit input replaces just the label area within the tab. Powerline separators stay in place. The input widget inherits the tab's bg/fg colors.

## Communication Back to Desktop

The status bar needs to trigger actions on the desktop. A callback interface defined in the `texel` package (to avoid circular imports) is injected after creation:

```go
// In texel/desktop_status.go
type StatusBarActions interface {
    SwitchToWorkspace(id int)
    RenameWorkspace(id int, name string)
}
```

- `TabBar.OnChange` → calls `actions.SwitchToWorkspace(id)`
- `TabBar.OnRename` → calls `actions.RenameWorkspace(id, name)`

## Pluggable Registry

### Contract

Any app implementing `core.App` + `Listener` can serve as a status bar. The desktop engine is agnostic — it calls `AddStatusPane` with whatever app the registry provides.

### Registration

- Default status bar registers as `"statusbar"` (category `"system"`) in the built-in registry
- Server harness creates it via `registry.CreateApp("statusbar", nil)` instead of `statusbar.New()`
- Alternative implementations can register with the same name via external app manifests

### StatusBarActions Injection

The `StatusBarActions` interface is passed after creation via a setup method (e.g., `SetActions(StatusBarActions)`), since the registry's `CreateApp` returns a generic `App`. The server harness calls this after creation and before `AddStatusPane`.

## Files Changed

### texelui (new/modified)

| File | Change |
|------|--------|
| `primitives/tabbar.go` | Add `EditTab()`, `CancelEdit()`, `OnRename`, `OnEditCancel`, double-click edit, inline input rendering |

### texelation (new/modified)

| File | Change |
|------|--------|
| `texel/dispatcher.go` | Add new event types, remove `EventStateUpdate`/`StatePayload` |
| `texel/desktop_engine_core.go` | Broadcast fine-grained events instead of `broadcastStateUpdate()` |
| `texel/workspace.go` | Add `Name`, `Color` fields to Workspace struct |
| `apps/statusbar/statusbar.go` | Full rewrite: `adapter.UIApp` with TabBar + BlendInfoLine |
| `apps/statusbar/blend_info_line.go` | New: custom widget for row 1 (gradient + text + toasts) |
| `apps/statusbar/statusbar_test.go` | Rewrite tests for new architecture |
| `cmd/texel-server/main.go` | Change `AddStatusPane` size from 1 to 2, use registry, inject `StatusBarActions` |
| `registry/builtins.go` | Update statusbar registration if needed |
| `internal/runtime/server/desktop_publisher.go` | Update any `StatePayload` references |
| `internal/runtime/server/snapshot_store.go` | Persist workspace names and colors |

### Migration of existing listeners

Any code subscribing to `EventStateUpdate` must migrate to the relevant fine-grained events. Search for `EventStateUpdate` and `StatePayload` references to identify all call sites.
