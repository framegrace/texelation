# Status Bar Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the status bar from a hand-rolled cell-buffer renderer to a texelui widget-based `adapter.UIApp`, with fine-grained events, editable workspace names, per-workspace accent colors, and toast messages.

**Architecture:** The status bar becomes an `adapter.UIApp` with two widgets: a texelui `TabBar` (row 0, workspace tabs with inline edit) and a custom `BlendInfoLine` (row 1, gradient + mode/title/fps/clock + toasts). The monolithic `EventStateUpdate`/`StatePayload` is replaced with fine-grained events. The status bar registers via the app registry.

**Tech Stack:** Go 1.24.3, texelui (widgets, adapter, color/gradient, theme), tcell/v2

**Spec:** `docs/superpowers/specs/2026-03-29-statusbar-migration-design.md`

---

## File Structure

### texelui changes

| File | Responsibility |
|------|---------------|
| `primitives/tabbar.go` (modify) | Add `EditTab()`, `CancelEdit()`, `OnRename`, `OnEditCancel`, inline Input widget, double-click edit |
| `primitives/tabbar_test.go` (modify) | Tests for edit mode |

### texelation changes

| File | Responsibility |
|------|---------------|
| `texel/dispatcher.go` (modify) | New event types + payload structs, remove `EventStateUpdate`/`StatePayload` |
| `texel/workspace.go` (modify) | Add `Name`, `Color` fields to `Workspace` |
| `texel/desktop_engine_core.go` (modify) | Replace `broadcastStateUpdate()` with fine-grained broadcast methods |
| `texel/desktop_engine_control_mode.go` (modify) | Call new broadcast methods |
| `texel/workspace_navigation.go` (modify) | Call new broadcast methods |
| `texel/workspace_layout.go` (modify) | Call new broadcast methods |
| `texel/workspace.go` (modify) | Call new broadcast methods |
| `texel/pane.go` (modify) | Call new broadcast methods |
| `texel/snapshot_restore.go` (modify) | Call new broadcast methods |
| `texel/desktop_overlays.go` (modify) | Call new broadcast methods (if needed) |
| `apps/statusbar/statusbar.go` (rewrite) | Widget-based UIApp with TabBar + BlendInfoLine |
| `apps/statusbar/blend_info_line.go` (create) | Custom widget: gradient + overlaid info text + toast mode |
| `apps/statusbar/blend_info_line_test.go` (create) | Tests for BlendInfoLine |
| `apps/statusbar/statusbar_test.go` (rewrite) | Tests for new status bar |
| `internal/runtime/server/connection_sync.go` (modify) | Handle fine-grained events for protocol forwarding |
| `internal/runtime/server/connection.go` (modify) | Update initial state push on handshake |
| `protocol/messages.go` (modify) | Update `StateUpdate` struct with workspace names/colors |
| `cmd/texel-server/main.go` (modify) | Registry-based creation, size 2, inject `StatusBarActions` |

---

## Task 1: Add Workspace Name and Color to Workspace Struct

**Files:**
- Modify: `texelui/theme/semantics.go` (add workspace accent colors)
- Modify: `texelation/texel/workspace.go` (add Name, Color fields)
- Modify: `texelation/texel/workspace.go` (update newWorkspace)

### Steps

- [ ] **Step 1: Add workspace accent semantic colors to texelui**

In `texelui/theme/semantics.go`, add 8 workspace accent colors to the defaults map. Find the existing accent entries and add after them:

```go
// Workspace accents (auto-assigned palette)
"workspace.accent.1": "@blue",
"workspace.accent.2": "@green",
"workspace.accent.3": "@mauve",
"workspace.accent.4": "@peach",
"workspace.accent.5": "@pink",
"workspace.accent.6": "@teal",
"workspace.accent.7": "@yellow",
"workspace.accent.8": "@lavender",
```

- [ ] **Step 2: Add Name and Color fields to Workspace**

In `texelation/texel/workspace.go`, add fields to the `Workspace` struct (after the `id` field, around line 64):

```go
type Workspace struct {
	id                  int
	Name                string      // user-editable, defaults to "default" (ws 1) or number string
	Color               tcell.Color // auto-assigned accent color from theme palette
	x, y, width, height int
	// ... rest unchanged
}
```

- [ ] **Step 3: Add workspace color palette helper**

In `texelation/texel/workspace.go`, add a function to resolve workspace accent colors:

```go
var workspaceAccentKeys = [8]string{
	"workspace.accent.1", "workspace.accent.2", "workspace.accent.3", "workspace.accent.4",
	"workspace.accent.5", "workspace.accent.6", "workspace.accent.7", "workspace.accent.8",
}

// WorkspaceAccentColor returns the accent color for a workspace based on its
// position in creation order. Colors cycle through 8 theme-defined accents.
func WorkspaceAccentColor(index int) tcell.Color {
	key := workspaceAccentKeys[index%len(workspaceAccentKeys)]
	return theme.ResolveColorName(theme.Get().GetSemanticColor(key))
}
```

Add the `theme` import: `"github.com/framegrace/texelui/theme"`.

- [ ] **Step 4: Update newWorkspace to set Name and Color**

In `texelation/texel/workspace.go`, modify the `newWorkspace` function. Find where `id` is assigned and add Name/Color initialization:

```go
func newWorkspace(id int, desktop *DesktopEngine) *Workspace {
	name := fmt.Sprintf("%d", id)
	if id == 1 {
		name = "default"
	}
	w := &Workspace{
		id:              id,
		Name:            name,
		Color:           WorkspaceAccentColor(id - 1),
		// ... rest of existing fields
	}
	// ... rest unchanged
}
```

Add `"fmt"` to imports if not already present.

- [ ] **Step 5: Add RenameWorkspace to DesktopEngine**

In `texelation/texel/desktop_engine_core.go`, add a method for workspace renaming:

```go
// RenameWorkspace changes the display name of a workspace.
func (d *DesktopEngine) RenameWorkspace(id int, name string) {
	ws, ok := d.workspaces[id]
	if !ok {
		return
	}
	ws.Name = name
}
```

This will later broadcast `EventWorkspacesChanged` once that event type exists (Task 2).

- [ ] **Step 6: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All existing tests pass (new fields have zero values or defaults, no breakage).

Also run texelui tests: `cd /home/marc/projects/texel/texelui && go test ./...`

- [ ] **Step 7: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add texel/workspace.go texel/desktop_engine_core.go
cd /home/marc/projects/texel/texelui && git add theme/semantics.go
```

Commit texelui: `git commit -m "Add workspace accent semantic colors"`
Commit texelation: `git commit -m "Add Name and Color fields to Workspace struct"`

---

## Task 2: Define Fine-Grained Event Types

**Files:**
- Modify: `texelation/texel/dispatcher.go`

### Steps

- [ ] **Step 1: Add new event types and payload structs**

In `texelation/texel/dispatcher.go`, add the new event types to the `const` block (after `EventAppAttached`):

```go
const (
	// Control Events
	EventControlOn EventType = iota
	EventControlOff
	// Pane Events
	EventPaneActiveChanged
	EventPaneClosed
	// Workspace/Global Events
	EventStateUpdate // DEPRECATED: will be removed after migration
	EventTreeChanged
	EventThemeChanged
	EventAppAttached
	// Fine-grained status events
	EventWorkspacesChanged
	EventWorkspaceSwitched
	EventModeChanged
	EventActivePaneChanged
	EventPerformanceUpdate
	EventToast
)
```

Add the new payload structs after the existing `StatePayload` (keep `StatePayload` for now — it will be removed in Task 5):

```go
// WorkspaceInfo describes a single workspace for status display.
type WorkspaceInfo struct {
	ID    int
	Name  string
	Color tcell.Color
}

// WorkspacesChangedPayload is sent when workspaces are created, destroyed, renamed, or recolored.
type WorkspacesChangedPayload struct {
	Workspaces      []WorkspaceInfo
	ActiveID        int
}

// WorkspaceSwitchedPayload is sent when the active workspace changes.
type WorkspaceSwitchedPayload struct {
	ActiveID int
}

// ModeChangedPayload is sent when control/input mode toggles.
type ModeChangedPayload struct {
	InControlMode bool
	SubMode       rune
}

// ActivePaneChangedPayload is sent when the focused pane changes.
type ActivePaneChangedPayload struct {
	ActiveTitle string
}

// PerformanceUpdatePayload carries publish-loop timing for FPS display.
type PerformanceUpdatePayload struct {
	LastPublishDuration time.Duration
}

// ToastSeverity indicates the visual style of a toast message.
type ToastSeverity int

const (
	ToastInfo ToastSeverity = iota
	ToastSuccess
	ToastWarning
	ToastError
)

// ToastPayload is sent when a transient message should be displayed.
type ToastPayload struct {
	Message  string
	Severity ToastSeverity
	Duration time.Duration
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass. New types are additive — nothing references them yet.

- [ ] **Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add texel/dispatcher.go
git commit -m "Add fine-grained event types for status bar migration"
```

---

## Task 3: Add Fine-Grained Broadcast Methods to DesktopEngine

**Files:**
- Modify: `texelation/texel/desktop_engine_core.go`

### Steps

- [ ] **Step 1: Add helper to build WorkspacesChangedPayload**

In `texelation/texel/desktop_engine_core.go`, add after the existing `currentStatePayload` method:

```go
// workspacesChangedPayload builds a WorkspacesChangedPayload from current state.
func (d *DesktopEngine) workspacesChangedPayload() WorkspacesChangedPayload {
	infos := make([]WorkspaceInfo, 0, len(d.workspaces))
	for _, ws := range d.workspaces {
		infos = append(infos, WorkspaceInfo{
			ID:    ws.id,
			Name:  ws.Name,
			Color: ws.Color,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	activeID := 0
	if d.activeWorkspace != nil {
		activeID = d.activeWorkspace.id
	}
	return WorkspacesChangedPayload{Workspaces: infos, ActiveID: activeID}
}
```

- [ ] **Step 2: Add individual broadcast methods**

In the same file, add these methods:

```go
func (d *DesktopEngine) broadcastWorkspacesChanged() {
	d.dispatcher.Broadcast(Event{
		Type:    EventWorkspacesChanged,
		Payload: d.workspacesChangedPayload(),
	})
}

func (d *DesktopEngine) broadcastWorkspaceSwitched() {
	activeID := 0
	if d.activeWorkspace != nil {
		activeID = d.activeWorkspace.id
	}
	d.dispatcher.Broadcast(Event{
		Type:    EventWorkspaceSwitched,
		Payload: WorkspaceSwitchedPayload{ActiveID: activeID},
	})
}

func (d *DesktopEngine) broadcastModeChanged() {
	d.dispatcher.Broadcast(Event{
		Type:    EventModeChanged,
		Payload: ModeChangedPayload{InControlMode: d.inControlMode, SubMode: d.subControlMode},
	})
}

func (d *DesktopEngine) broadcastActivePaneChanged() {
	var title string
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
		title = d.zoomedPane.Pane.getTitle()
	} else if d.activeWorkspace != nil {
		title = d.activeWorkspace.tree.ActiveTitle()
	}
	d.dispatcher.Broadcast(Event{
		Type:    EventActivePaneChanged,
		Payload: ActivePaneChangedPayload{ActiveTitle: title},
	})
}

func (d *DesktopEngine) broadcastPerformanceUpdate() {
	d.dispatcher.Broadcast(Event{
		Type:    EventPerformanceUpdate,
		Payload: PerformanceUpdatePayload{LastPublishDuration: time.Duration(d.lastPublishNanos.Load())},
	})
}

// BroadcastToast sends a transient toast message to all listeners.
func (d *DesktopEngine) BroadcastToast(message string, severity ToastSeverity, duration time.Duration) {
	d.dispatcher.Broadcast(Event{
		Type:    EventToast,
		Payload: ToastPayload{Message: message, Severity: severity, Duration: duration},
	})
}
```

- [ ] **Step 3: Wire broadcastStateUpdate to also emit fine-grained events**

Modify the existing `broadcastStateUpdate()` to ALSO broadcast fine-grained events (dual-emit during transition). Add these lines at the end of `broadcastStateUpdate()`, after the existing `d.dispatcher.Broadcast(Event{Type: EventStateUpdate, ...})` call:

```go
// Dual-emit fine-grained events during migration.
d.broadcastWorkspacesChanged()
d.broadcastWorkspaceSwitched()
d.broadcastModeChanged()
d.broadcastActivePaneChanged()
d.broadcastPerformanceUpdate()
```

This is intentionally noisy — it means every `broadcastStateUpdate()` call also fires the fine-grained events. This lets the new status bar work immediately while the old code paths still exist.

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass. The new broadcasts are additive — no listener handles them yet.

- [ ] **Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add texel/desktop_engine_core.go
git commit -m "Add fine-grained broadcast methods for status bar events"
```

---

## Task 4: Add TabBar Edit Mode to texelui

**Files:**
- Modify: `texelui/primitives/tabbar.go`
- Modify: `texelui/primitives/tabbar_test.go`

### Steps

- [ ] **Step 1: Write failing tests for edit mode**

In `texelui/primitives/tabbar_test.go`, add these tests:

```go
func TestTabBar_EditTab_EnterConfirms(t *testing.T) {
	tabs := []TabItem{{Label: "One"}, {Label: "Two"}, {Label: "Three"}}
	tb := NewTabBar(0, 0, 40, tabs)

	var renamed bool
	var renameIdx int
	var renameLabel string
	tb.OnRename = func(idx int, newName string) {
		renamed = true
		renameIdx = idx
		renameLabel = newName
	}

	tb.EditTab(1) // Edit "Two"
	if !tb.IsEditing() {
		t.Fatal("expected editing state")
	}

	// Type "NewName" by sending key events
	for _, ch := range "NewName" {
		tb.HandleKey(tcell.NewEventKey(tcell.KeyRune, ch, tcell.ModNone))
	}
	// Press Enter to confirm
	tb.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if !renamed {
		t.Fatal("OnRename not called")
	}
	if renameIdx != 1 {
		t.Errorf("expected index 1, got %d", renameIdx)
	}
	if renameLabel != "NewName" {
		t.Errorf("expected 'NewName', got %q", renameLabel)
	}
	if tb.IsEditing() {
		t.Error("should not be editing after confirm")
	}
	if tb.Tabs[1].Label != "NewName" {
		t.Errorf("tab label not updated, got %q", tb.Tabs[1].Label)
	}
}

func TestTabBar_EditTab_EscapeCancels(t *testing.T) {
	tabs := []TabItem{{Label: "One"}, {Label: "Two"}}
	tb := NewTabBar(0, 0, 40, tabs)

	var cancelled bool
	tb.OnEditCancel = func(idx int) { cancelled = true }
	tb.OnRename = func(idx int, newName string) { t.Error("OnRename should not be called on cancel") }

	tb.EditTab(0)
	tb.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'X', tcell.ModNone))
	tb.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if !cancelled {
		t.Fatal("OnEditCancel not called")
	}
	if tb.IsEditing() {
		t.Error("should not be editing after cancel")
	}
	if tb.Tabs[0].Label != "One" {
		t.Errorf("label should revert to 'One', got %q", tb.Tabs[0].Label)
	}
}

func TestTabBar_EditTab_EmptyConfirmsOriginal(t *testing.T) {
	tabs := []TabItem{{Label: "Keep"}}
	tb := NewTabBar(0, 0, 40, tabs)

	var renameLabel string
	tb.OnRename = func(idx int, newName string) { renameLabel = newName }

	tb.EditTab(0)
	// Select all and delete (Ctrl+A then delete is not available, just clear manually)
	// The input should be pre-filled with "Keep" and selected.
	// Home then Shift-End then Delete to clear:
	tb.HandleKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone))
	for range len("Keep") {
		tb.HandleKey(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))
	}
	tb.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	// Empty string should still fire OnRename with empty — caller decides fallback
	if renameLabel != "" {
		t.Errorf("expected empty rename, got %q", renameLabel)
	}
}

func TestTabBar_CancelEdit(t *testing.T) {
	tabs := []TabItem{{Label: "Test"}}
	tb := NewTabBar(0, 0, 40, tabs)
	tb.EditTab(0)
	if !tb.IsEditing() {
		t.Fatal("expected editing")
	}
	tb.CancelEdit()
	if tb.IsEditing() {
		t.Error("should not be editing after CancelEdit")
	}
}

func TestTabBar_EditTab_OutOfRange(t *testing.T) {
	tabs := []TabItem{{Label: "Only"}}
	tb := NewTabBar(0, 0, 40, tabs)
	tb.EditTab(5) // out of range
	if tb.IsEditing() {
		t.Error("should not enter edit for out-of-range index")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelui && go test ./primitives/ -run "TestTabBar_Edit|TestTabBar_Cancel" -v`
Expected: FAIL — `EditTab`, `IsEditing`, `OnRename`, `OnEditCancel`, `CancelEdit` are undefined.

- [ ] **Step 3: Implement edit mode on TabBar**

In `texelui/primitives/tabbar.go`, add the edit mode fields to the `TabBar` struct:

```go
type TabBar struct {
	core.BaseWidget
	Tabs            []TabItem
	ActiveIdx       int
	OnChange        func(int)
	OnRename        func(index int, newName string)  // NEW
	OnEditCancel    func(index int)                   // NEW
	Style           TabBarStyle
	ShowFocusMarker bool
	OnFocusExit     func(forward bool)

	hoverIdx    int
	inv         func(core.Rect)
	editIdx     int    // NEW: index being edited, -1 if not editing
	editInput   *widgets.Input // NEW: inline input widget
	editOriginal string // NEW: original label before edit
}
```

Add `"github.com/framegrace/texelui/widgets"` to imports.

Update `NewTabBar` to initialize `editIdx: -1`:

```go
func NewTabBar(x, y, w int, tabs []TabItem) *TabBar {
	tb := &TabBar{
		Tabs:            tabs,
		ShowFocusMarker: true,
		hoverIdx:        -1,
		editIdx:         -1,
	}
	tb.SetPosition(x, y)
	tb.Resize(w, tb.TabBarHeight())
	tb.SetFocusable(true)
	return tb
}
```

Add the edit mode methods:

```go
// IsEditing returns true if a tab label is being edited.
func (tb *TabBar) IsEditing() bool {
	return tb.editIdx >= 0
}

// EditTab enters inline edit mode on the tab at the given index.
func (tb *TabBar) EditTab(index int) {
	if index < 0 || index >= len(tb.Tabs) {
		return
	}
	tb.editIdx = index
	tb.editOriginal = tb.Tabs[index].Label
	tb.editInput = widgets.NewInput()
	tb.editInput.Text = tb.Tabs[index].Label
	tb.editInput.CaretPos = len([]rune(tb.editInput.Text))
	tb.editInput.OnSubmit = func(text string) {
		tb.confirmEdit(text)
	}
	tb.editInput.Focus()
	tb.invalidate()
}

// CancelEdit cancels any active edit, reverting to the original label.
func (tb *TabBar) CancelEdit() {
	if tb.editIdx < 0 {
		return
	}
	idx := tb.editIdx
	tb.Tabs[idx].Label = tb.editOriginal
	tb.editIdx = -1
	tb.editInput = nil
	tb.editOriginal = ""
	if tb.OnEditCancel != nil {
		tb.OnEditCancel(idx)
	}
	tb.invalidate()
}

func (tb *TabBar) confirmEdit(text string) {
	if tb.editIdx < 0 {
		return
	}
	idx := tb.editIdx
	tb.Tabs[idx].Label = text
	tb.editIdx = -1
	tb.editInput = nil
	tb.editOriginal = ""
	if tb.OnRename != nil {
		tb.OnRename(idx, text)
	}
	tb.invalidate()
}

func (tb *TabBar) invalidate() {
	if tb.inv != nil {
		_, _ = tb.Position()
		w, h := tb.Size()
		x, y := tb.Position()
		tb.inv(core.Rect{X: x, Y: y, W: w, H: h})
	}
}
```

- [ ] **Step 4: Route keys through edit input when editing**

In the `HandleKey` method of TabBar, add edit mode handling at the top (before existing key handling):

```go
func (tb *TabBar) HandleKey(ev *tcell.EventKey) bool {
	// Edit mode: route to inline input
	if tb.IsEditing() {
		switch ev.Key() {
		case tcell.KeyEscape:
			tb.CancelEdit()
			return true
		case tcell.KeyTab:
			// Tab confirms edit (not focus advance)
			tb.confirmEdit(tb.editInput.Text)
			return true
		default:
			return tb.editInput.HandleKey(ev)
		}
	}
	// ... existing key handling unchanged
```

- [ ] **Step 5: Add double-click edit support**

In the `HandleMouse` method, detect double-click on a tab label. Find the existing click handling and add double-click detection:

```go
func (tb *TabBar) HandleMouse(ev *tcell.EventMouse) bool {
	// ... existing mouse handling
	// After the existing click-to-select logic, add:
	if ev.Buttons() == tcell.Button1 {
		mx, _ := ev.Position()
		x, _ := tb.Position()
		localX := mx - x
		clickedIdx := tb.tabAtX(localX)

		if clickedIdx >= 0 {
			// If clicking outside editing tab, confirm edit
			if tb.IsEditing() && clickedIdx != tb.editIdx {
				tb.confirmEdit(tb.editInput.Text)
			}
			// Double-click detection: if clicking already-active tab, enter edit
			if clickedIdx == tb.ActiveIdx && !tb.IsEditing() {
				tb.EditTab(clickedIdx)
				return true
			}
		}
	}
	// ... rest of existing handling
```

Note: This is a simplified double-click (clicking the already-active tab). A proper double-click timer can be added later if needed.

- [ ] **Step 6: Render inline input when editing**

In the `Draw` method, when rendering the tab at `editIdx`, draw the Input widget instead of the label text. Find the label rendering section inside the tab drawing loop and wrap it:

```go
// Inside the Draw method's tab rendering loop, where the label is drawn:
if tb.IsEditing() && i == tb.editIdx {
	// Position and size the input to fit the tab label area
	labelX := tabStartX + 1 // after left separator
	labelW := tabWidth - 2  // minus separators
	if labelW < 1 {
		labelW = 1
	}
	tb.editInput.SetPosition(labelX, y)
	tb.editInput.Resize(labelW, 1)
	tb.editInput.Draw(painter)
} else {
	// ... existing label rendering
}
```

The exact integration point depends on the Draw method's structure — the implementing agent should find where tab labels are drawn and wrap with this conditional.

- [ ] **Step 7: Run tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./primitives/ -run "TestTabBar_Edit|TestTabBar_Cancel" -v`
Expected: All 5 new tests pass.

Run full suite: `cd /home/marc/projects/texel/texelui && go test ./...`
Expected: All tests pass (no regressions).

- [ ] **Step 8: Commit**

```bash
cd /home/marc/projects/texel/texelui && git add primitives/tabbar.go primitives/tabbar_test.go
git commit -m "Add inline edit mode to TabBar widget"
```

---

## Task 5: Migrate Call Sites from broadcastStateUpdate to Fine-Grained Broadcasts

**Files:**
- Modify: `texelation/texel/desktop_engine_core.go`
- Modify: `texelation/texel/desktop_engine_control_mode.go`
- Modify: `texelation/texel/workspace_navigation.go`
- Modify: `texelation/texel/workspace_layout.go`
- Modify: `texelation/texel/workspace.go`
- Modify: `texelation/texel/pane.go`
- Modify: `texelation/texel/snapshot_restore.go`
- Modify: `texelation/texel/dispatcher.go`

This task replaces each `broadcastStateUpdate()` call with the specific fine-grained event(s) that the call site actually needs. After all sites are migrated, `broadcastStateUpdate()`, `shouldBroadcastState()`, `storeLastState()`, `currentStatePayload()`, `StatePayload`, and `EventStateUpdate` are removed.

### Steps

- [ ] **Step 1: Audit all call sites**

Each `broadcastStateUpdate()` call site and what it actually changes:

| File:Line | Context | Needed Events |
|-----------|---------|---------------|
| `desktop_engine_core.go:315` | After `CloseWorkspace` | `EventWorkspacesChanged` + `EventWorkspaceSwitched` |
| `desktop_engine_core.go:395` | After `toggleZoom` | `EventActivePaneChanged` |
| `desktop_engine_core.go:471` | Inside `SetRefreshHandler` (called on every publish) | `EventPerformanceUpdate` |
| `desktop_engine_core.go:619` | After `SwitchToWorkspace` | `EventWorkspaceSwitched` + `EventActivePaneChanged` |
| `desktop_engine_control_mode.go:40` | `enterControlMode` | `EventModeChanged` |
| `desktop_engine_control_mode.go:64` | `exitControlMode` | `EventModeChanged` |
| `desktop_engine_control_mode.go:112` | `toggleSubControlMode` | `EventModeChanged` |
| `workspace_navigation.go:61` | After `NewWorkspace` | `EventWorkspacesChanged` + `EventWorkspaceSwitched` |
| `workspace_navigation.go:110` | After focus change in split | `EventActivePaneChanged` |
| `workspace_navigation.go:265` | After pane close | `EventActivePaneChanged` + `EventWorkspacesChanged` (pane count may change tabs) |
| `pane.go:429` | After app title changes | `EventActivePaneChanged` |
| `snapshot_restore.go:123` | After snapshot restore | `EventWorkspacesChanged` + `EventWorkspaceSwitched` + `EventModeChanged` + `EventActivePaneChanged` |
| `workspace.go:186` | After adding pane | `EventActivePaneChanged` |
| `workspace.go:315` | After split pane | `EventActivePaneChanged` |
| `workspace_layout.go:55` | After resize pane borders | (nothing status-relevant) |
| `workspace_layout.go:186` | After pane focus during resize | `EventActivePaneChanged` |

- [ ] **Step 2: Replace call sites in desktop_engine_control_mode.go**

Replace all 3 `d.broadcastStateUpdate()` calls with `d.broadcastModeChanged()`:

In `enterControlMode` (~line 40):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastModeChanged()
```

In `exitControlMode` (~line 64):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastModeChanged()
```

In `toggleSubControlMode` (~line 112):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastModeChanged()
```

- [ ] **Step 3: Replace call sites in workspace_navigation.go**

Line 61 (after `NewWorkspace`):
```go
// Replace: w.desktop.broadcastStateUpdate()
w.desktop.broadcastWorkspacesChanged()
w.desktop.broadcastWorkspaceSwitched()
```

Line 110 (after focus change):
```go
// Replace: w.desktop.broadcastStateUpdate()
w.desktop.broadcastActivePaneChanged()
```

Line 265 (after pane close):
```go
// Replace: w.desktop.broadcastStateUpdate()
w.desktop.broadcastActivePaneChanged()
```

- [ ] **Step 4: Replace call sites in desktop_engine_core.go**

Line 315 (after `CloseWorkspace`):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastWorkspacesChanged()
d.broadcastWorkspaceSwitched()
```

Line 395 (after `toggleZoom`):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastActivePaneChanged()
```

Line 471 (inside `SetRefreshHandler`):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastPerformanceUpdate()
```

Line 619 (after `SwitchToWorkspace`):
```go
// Replace: d.broadcastStateUpdate()
d.broadcastWorkspaceSwitched()
d.broadcastActivePaneChanged()
```

- [ ] **Step 5: Replace call sites in pane.go, workspace.go, workspace_layout.go, snapshot_restore.go**

`pane.go:429`:
```go
// Replace: p.screen.desktop.broadcastStateUpdate()
p.screen.desktop.broadcastActivePaneChanged()
```

`workspace.go:186`:
```go
// Replace: w.desktop.broadcastStateUpdate()
w.desktop.broadcastActivePaneChanged()
```

`workspace.go:315`:
```go
// Replace: w.desktop.broadcastStateUpdate()
w.desktop.broadcastActivePaneChanged()
```

`workspace_layout.go:55`:
```go
// Remove: w.desktop.broadcastStateUpdate()
// Resize border movement doesn't change any status-visible state.
```

`workspace_layout.go:186`:
```go
// Replace: w.desktop.broadcastStateUpdate()
w.desktop.broadcastActivePaneChanged()
```

`snapshot_restore.go:123`:
```go
// Replace: d.broadcastStateUpdate()
d.broadcastWorkspacesChanged()
d.broadcastWorkspaceSwitched()
d.broadcastModeChanged()
d.broadcastActivePaneChanged()
```

- [ ] **Step 6: Update RenameWorkspace to broadcast**

In `desktop_engine_core.go`, update the `RenameWorkspace` method added in Task 1:

```go
func (d *DesktopEngine) RenameWorkspace(id int, name string) {
	ws, ok := d.workspaces[id]
	if !ok {
		return
	}
	ws.Name = name
	d.broadcastWorkspacesChanged()
}
```

- [ ] **Step 7: Remove dual-emit from broadcastStateUpdate**

In `desktop_engine_core.go`, remove the 5 fine-grained broadcast calls added in Task 3 Step 3 from `broadcastStateUpdate()`. The method should now only contain the legacy `EventStateUpdate` broadcast (kept for `connection_sync.go` until Task 7).

- [ ] **Step 8: Update connection_sync.go to handle fine-grained events**

In `internal/runtime/server/connection_sync.go`, update `OnEvent` to handle both old and new events. The protocol `StateUpdate` message still needs to be sent to clients, so we build it from the fine-grained events:

Add fields to track accumulated state on the `connection` struct (find definition in `connection.go`):

```go
// In the connection struct, add:
lastWorkspaces  WorkspacesChangedPayload // cached for building StateUpdate
lastMode        ModeChangedPayload
lastActiveTitle string
```

Wait — this gets complex. A simpler approach: keep `CurrentStatePayload()` and `sendStateUpdate()` working for now. The protocol migration is a separate concern. Just add the new event types to `OnEvent` as no-ops (the protocol still uses `EventStateUpdate`):

```go
func (c *connection) OnEvent(event texel.Event) {
	switch event.Type {
	case texel.EventStateUpdate:
		payload, ok := event.Payload.(texel.StatePayload)
		if !ok {
			return
		}
		c.sendStateUpdate(payload)
	case texel.EventTreeChanged:
		c.sendTreeSnapshot()
	case texel.EventWorkspacesChanged, texel.EventWorkspaceSwitched,
		texel.EventModeChanged, texel.EventActivePaneChanged,
		texel.EventPerformanceUpdate, texel.EventToast:
		// Fine-grained events handled by status bar directly.
		// Protocol still uses StateUpdate for client sync.
	}
}
```

Since `broadcastStateUpdate()` is still called from `SetRefreshHandler` for the `EventPerformanceUpdate` path... wait, we just removed it. We need to keep sending `EventStateUpdate` for the protocol. Add a separate call in `SetRefreshHandler`:

In `desktop_engine_core.go`, modify `SetRefreshHandler`:

```go
func (d *DesktopEngine) SetRefreshHandler(handler func()) {
	d.refreshMu.Lock()
	d.refreshHandler = func() {
		d.broadcastPerformanceUpdate()
		d.broadcastStateUpdate() // Keep for protocol clients
		if handler != nil {
			handler()
		}
	}
	d.refreshMu.Unlock()
}
```

Actually, the cleanest approach: keep `broadcastStateUpdate()` specifically for the protocol path. It only fires from `SetRefreshHandler` now. All other call sites use fine-grained events. The status bar only listens to fine-grained events. The protocol connection only listens to `EventStateUpdate`.

So the final state of `SetRefreshHandler`:
```go
func (d *DesktopEngine) SetRefreshHandler(handler func()) {
	d.refreshMu.Lock()
	d.refreshHandler = func() {
		d.broadcastPerformanceUpdate()
		d.broadcastStateUpdate()
		if handler != nil {
			handler()
		}
	}
	d.refreshMu.Unlock()
}
```

And remove `broadcastStateUpdate()` from everywhere else (Steps 2-5 already did this).

- [ ] **Step 9: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass. The status bar test will still work because `OnEvent` still handles `EventStateUpdate` (until Task 8 rewrites it).

- [ ] **Step 10: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add texel/desktop_engine_core.go texel/desktop_engine_control_mode.go texel/workspace_navigation.go texel/workspace_layout.go texel/workspace.go texel/pane.go texel/snapshot_restore.go internal/runtime/server/connection_sync.go
git commit -m "Migrate broadcastStateUpdate call sites to fine-grained events"
```

---

## Task 6: Implement BlendInfoLine Widget

**Files:**
- Create: `texelation/apps/statusbar/blend_info_line.go`
- Create: `texelation/apps/statusbar/blend_info_line_test.go`

### Steps

- [ ] **Step 1: Write failing tests for BlendInfoLine**

Create `texelation/apps/statusbar/blend_info_line_test.go`:

```go
package statusbar

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestBlendInfoLine_Render(t *testing.T) {
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(60, 1)
	bil.SetAccentColor(tcell.ColorBlue)
	bil.SetMode(false, 0)
	bil.SetTitle("texelterm")
	bil.SetFPS(42, 60)
	bil.SetClock("14:32:07")

	// Verify it doesn't panic and produces output
	p := newTestPainter(60, 1)
	bil.Draw(p)

	// Check that mode icon + title appear on the left
	line := p.Row(0)
	if len(line) == 0 {
		t.Fatal("expected non-empty render")
	}
}

func TestBlendInfoLine_ToastMode(t *testing.T) {
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(60, 1)
	bil.SetAccentColor(tcell.ColorBlue)

	bil.ShowToast("Saved!", ToastSuccess, 3*time.Second)
	if !bil.isToastActive() {
		t.Fatal("expected toast to be active")
	}

	bil.DismissToast()
	if bil.isToastActive() {
		t.Error("expected toast to be dismissed")
	}
}

func TestBlendInfoLine_ToastReplacesContent(t *testing.T) {
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(40, 1)
	bil.SetAccentColor(tcell.ColorBlue)
	bil.SetTitle("texelterm")

	bil.ShowToast("Error!", ToastError, 3*time.Second)

	p := newTestPainter(40, 1)
	bil.Draw(p)

	line := p.RowString(0)
	if !containsString(line, "Error!") {
		t.Errorf("expected toast text in output, got %q", line)
	}
}
```

The `newTestPainter` and `containsString` helpers will be created alongside the implementation. The implementing agent should create a minimal test painter that captures SetCell calls for verification — or use `core.NewPainter` with a test buffer if that's the standard pattern in texelui tests.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/statusbar/ -run "TestBlendInfoLine" -v`
Expected: FAIL — `NewBlendInfoLine` not defined.

- [ ] **Step 3: Implement BlendInfoLine**

Create `texelation/apps/statusbar/blend_info_line.go`:

```go
package statusbar

import (
	"fmt"
	"sync"
	"time"

	"github.com/framegrace/texelui/color"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/texel"
)

// BlendInfoLine renders a gradient blend row with overlaid status text.
// In normal mode: left = mode icon + title, right = fps + clock.
// In toast mode: replaces all content with a message in severity-colored gradient.
type BlendInfoLine struct {
	core.BaseWidget
	mu sync.RWMutex

	accentColor tcell.Color
	contentBG   tcell.Color

	// Normal mode state
	inControlMode bool
	subMode       rune
	title         string
	fpsActual     float64
	fpsTheoretical float64
	clock         string

	// Toast state
	toastMessage  string
	toastSeverity texel.ToastSeverity
	toastExpiry   time.Time
	toastActive   bool

	inv func(core.Rect)
}

// NewBlendInfoLine creates a new blend info line widget.
func NewBlendInfoLine() *BlendInfoLine {
	bil := &BlendInfoLine{
		contentBG: theme.ResolveColorName(theme.Get().GetSemanticColor("bg.base")),
	}
	return bil
}

func (bil *BlendInfoLine) SetInvalidator(fn func(core.Rect)) {
	bil.inv = fn
}

func (bil *BlendInfoLine) SetAccentColor(c tcell.Color) {
	bil.mu.Lock()
	bil.accentColor = c
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) SetMode(controlMode bool, subMode rune) {
	bil.mu.Lock()
	bil.inControlMode = controlMode
	bil.subMode = subMode
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) SetTitle(title string) {
	bil.mu.Lock()
	bil.title = title
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) SetFPS(actual, theoretical float64) {
	bil.mu.Lock()
	bil.fpsActual = actual
	bil.fpsTheoretical = theoretical
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) SetClock(t string) {
	bil.mu.Lock()
	bil.clock = t
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) ShowToast(message string, severity texel.ToastSeverity, duration time.Duration) {
	bil.mu.Lock()
	bil.toastMessage = message
	bil.toastSeverity = severity
	bil.toastExpiry = time.Now().Add(duration)
	bil.toastActive = true
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) DismissToast() {
	bil.mu.Lock()
	bil.toastActive = false
	bil.mu.Unlock()
	bil.invalidate()
}

func (bil *BlendInfoLine) isToastActive() bool {
	bil.mu.RLock()
	defer bil.mu.RUnlock()
	if bil.toastActive && time.Now().After(bil.toastExpiry) {
		return false
	}
	return bil.toastActive
}

func (bil *BlendInfoLine) Draw(painter *core.Painter) {
	bil.mu.RLock()
	defer bil.mu.RUnlock()

	w, _ := bil.Size()
	x, y := bil.Position()
	if w <= 0 {
		return
	}

	// Check if toast expired
	if bil.toastActive && time.Now().After(bil.toastExpiry) {
		bil.toastActive = false
	}

	if bil.toastActive {
		bil.drawToast(painter, x, y, w)
	} else {
		bil.drawNormal(painter, x, y, w)
	}
}

func (bil *BlendInfoLine) drawNormal(painter *core.Painter, x, y, w int) {
	// Build gradient: accent → accent → contentBG
	accent := bil.accentColor
	if accent == 0 {
		accent = theme.ResolveColorName(theme.Get().GetSemanticColor("accent"))
	}

	bg := color.Linear(0,
		color.Stop(0, accent),
		color.Stop(0.3, accent),
		color.Stop(1, bil.contentBG),
	).WithLocal().Build()

	// Fill background with gradient
	for col := 0; col < w; col++ {
		style := color.DynamicStyle{BG: bg, FG: color.Solid(tcell.ColorWhite)}
		painter.SetDynamicCell(x+col, y, ' ', style)
	}

	// Left: mode icon + title
	modeIcon := " \uf11c " // keyboard icon (INPUT mode)
	if bil.inControlMode {
		modeIcon = " \uf085 " // ctrl icon
	}
	left := modeIcon + bil.title

	// Right: fps + clock
	var right string
	if bil.fpsTheoretical > 0 {
		right = fmt.Sprintf("%.0f/%.0f fps  %s ", bil.fpsActual, bil.fpsTheoretical, bil.clock)
	} else if bil.fpsActual > 0 {
		right = fmt.Sprintf("%.0f fps  %s ", bil.fpsActual, bil.clock)
	} else {
		right = bil.clock + " "
	}

	// Draw left text (dark on bright gradient)
	fgLeft := color.Solid(tcell.NewRGBColor(30, 30, 46)) // dark text for contrast
	for i, ch := range left {
		if x+i >= x+w {
			break
		}
		style := color.DynamicStyle{BG: bg, FG: fgLeft}
		painter.SetDynamicCell(x+i, y, ch, style)
	}

	// Draw right text (muted on dark gradient end)
	fgRight := color.Solid(theme.ResolveColorName(theme.Get().GetSemanticColor("text.muted")))
	rightStart := w - len([]rune(right))
	if rightStart < len([]rune(left)) {
		rightStart = len([]rune(left))
	}
	for i, ch := range right {
		col := rightStart + i
		if col >= w {
			break
		}
		style := color.DynamicStyle{BG: bg, FG: fgRight}
		painter.SetDynamicCell(x+col, y, ch, style)
	}
}

func (bil *BlendInfoLine) drawToast(painter *core.Painter, x, y, w int) {
	// Severity-based accent color
	var toastColor tcell.Color
	switch bil.toastSeverity {
	case texel.ToastSuccess:
		toastColor = theme.ResolveColorName(theme.Get().GetSemanticColor("action.success"))
	case texel.ToastWarning:
		toastColor = theme.ResolveColorName(theme.Get().GetSemanticColor("action.warning"))
	case texel.ToastError:
		toastColor = theme.ResolveColorName(theme.Get().GetSemanticColor("action.danger"))
	default:
		toastColor = theme.ResolveColorName(theme.Get().GetSemanticColor("accent"))
	}

	bg := color.Linear(0,
		color.Stop(0, toastColor),
		color.Stop(0.5, toastColor),
		color.Stop(1, bil.contentBG),
	).WithLocal().Build()

	// Fill with gradient
	for col := 0; col < w; col++ {
		style := color.DynamicStyle{BG: bg, FG: color.Solid(tcell.ColorWhite)}
		painter.SetDynamicCell(x+col, y, ' ', style)
	}

	// Draw message centered-left with contrast
	msg := " " + bil.toastMessage + " "
	fg := color.Solid(tcell.NewRGBColor(30, 30, 46))
	for i, ch := range msg {
		if x+i >= x+w {
			break
		}
		style := color.DynamicStyle{BG: bg, FG: fg}
		painter.SetDynamicCell(x+i, y, ch, style)
	}
}

func (bil *BlendInfoLine) invalidate() {
	if bil.inv != nil {
		x, y := bil.Position()
		w, h := bil.Size()
		bil.inv(core.Rect{X: x, Y: y, W: w, H: h})
	}
}
```

- [ ] **Step 4: Add test helpers**

The test file needs a test painter. Check how other texelui tests create painters — likely by creating a cell buffer and wrapping it. The implementing agent should look at existing test patterns in `texelui/primitives/tabbar_test.go` or `texelui/widgets/` for examples and follow the same pattern.

- [ ] **Step 5: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/statusbar/ -run "TestBlendInfoLine" -v`
Expected: All 3 tests pass.

- [ ] **Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add apps/statusbar/blend_info_line.go apps/statusbar/blend_info_line_test.go
git commit -m "Add BlendInfoLine widget for status bar gradient row"
```

---

## Task 7: Rewrite Status Bar as UIApp

**Files:**
- Rewrite: `texelation/apps/statusbar/statusbar.go`
- Rewrite: `texelation/apps/statusbar/statusbar_test.go`

### Steps

- [ ] **Step 1: Write failing tests for the new status bar**

Rewrite `texelation/apps/statusbar/statusbar_test.go`:

```go
package statusbar

import (
	"testing"
	"time"

	"github.com/framegrace/texelation/texel"
	"github.com/gdamore/tcell/v2"
)

func TestStatusBar_ReceivesWorkspacesChanged(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	sb.OnEvent(texel.Event{
		Type: texel.EventWorkspacesChanged,
		Payload: texel.WorkspacesChangedPayload{
			Workspaces: []texel.WorkspaceInfo{
				{ID: 1, Name: "default", Color: tcell.ColorBlue},
				{ID: 2, Name: "dev", Color: tcell.ColorGreen},
			},
			ActiveID: 1,
		},
	})

	buf := sb.Render()
	if len(buf) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(buf))
	}
	if len(buf[0]) != 80 {
		t.Fatalf("expected 80 cols, got %d", len(buf[0]))
	}
	// Verify non-empty render
	hasContent := false
	for _, cell := range buf[0] {
		if cell.Rune != 0 && cell.Rune != ' ' {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("row 0 has no visible content")
	}
}

func TestStatusBar_ReceivesModeChanged(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	sb.OnEvent(texel.Event{
		Type:    texel.EventModeChanged,
		Payload: texel.ModeChangedPayload{InControlMode: true, SubMode: 'A'},
	})

	// Should not panic, blend line updates
	buf := sb.Render()
	if len(buf) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(buf))
	}
}

func TestStatusBar_ReceivesToast(t *testing.T) {
	sb := New()
	sb.Resize(80, 2)

	sb.OnEvent(texel.Event{
		Type: texel.EventToast,
		Payload: texel.ToastPayload{
			Message:  "Settings saved",
			Severity: texel.ToastSuccess,
			Duration: 3 * time.Second,
		},
	})

	// Blend line should show toast
	buf := sb.Render()
	if len(buf) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(buf))
	}
}

func TestStatusBar_Lifecycle(t *testing.T) {
	sb := New()
	ch := make(chan bool, 1)
	sb.SetRefreshNotifier(ch)

	go func() { _ = sb.Run() }()
	time.Sleep(50 * time.Millisecond)
	sb.Stop()
}

func TestStatusBar_Title(t *testing.T) {
	sb := New()
	if sb.GetTitle() != "Status Bar" {
		t.Errorf("unexpected title: %s", sb.GetTitle())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/statusbar/ -run "TestStatusBar_" -v`
Expected: FAIL or partial pass — the current statusbar.go doesn't handle fine-grained events.

- [ ] **Step 3: Rewrite statusbar.go**

Replace the contents of `texelation/apps/statusbar/statusbar.go` with:

```go
package statusbar

import (
	"fmt"
	"sync"
	"time"

	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/primitives"
	"github.com/gdamore/tcell/v2"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/texel"
)

// StatusBarActions is the callback interface for desktop commands.
type StatusBarActions interface {
	SwitchToWorkspace(id int)
	RenameWorkspace(id int, name string)
}

// StatusBarApp is a widget-based status bar using texelui's adapter.UIApp.
type StatusBarApp struct {
	app       *adapter.UIApp
	ui        *core.UIManager
	tabBar    *primitives.TabBar
	blendLine *BlendInfoLine

	mu          sync.RWMutex
	workspaces  []texel.WorkspaceInfo
	activeID    int
	actions     StatusBarActions
	stopClock   chan struct{}

	// FPS smoothing
	lastRenderTime time.Time
	smoothFPS      float64
	smoothTheoFPS  float64
}

// New creates a new widget-based status bar.
func New() *StatusBarApp {
	ui := core.NewUIManager()
	ui.DisableStatusBar()

	tabBar := primitives.NewTabBar(0, 0, 80, []primitives.TabItem{{Label: "default"}})
	tabBar.Style.NoBlendRow = true

	blendLine := NewBlendInfoLine()

	ui.AddWidget(tabBar)
	ui.AddWidget(blendLine)

	app := adapter.NewUIApp("Status Bar", ui)
	app.DisableStatusBar()

	sb := &StatusBarApp{
		app:       app,
		ui:        ui,
		tabBar:    tabBar,
		blendLine: blendLine,
		stopClock: make(chan struct{}),
	}

	// Wire tab callbacks
	tabBar.OnChange = func(idx int) {
		sb.mu.RLock()
		actions := sb.actions
		var wsID int
		if idx >= 0 && idx < len(sb.workspaces) {
			wsID = sb.workspaces[idx].ID
		}
		sb.mu.RUnlock()
		if actions != nil && wsID > 0 {
			actions.SwitchToWorkspace(wsID)
		}
	}

	tabBar.OnRename = func(idx int, newName string) {
		sb.mu.RLock()
		actions := sb.actions
		var wsID int
		if idx >= 0 && idx < len(sb.workspaces) {
			wsID = sb.workspaces[idx].ID
		}
		sb.mu.RUnlock()
		if actions != nil && wsID > 0 {
			if newName == "" {
				newName = fmt.Sprintf("%d", wsID)
			}
			actions.RenameWorkspace(wsID, newName)
		}
	}

	app.SetOnResize(func(w, h int) {
		tabBar.SetPosition(0, 0)
		tabBar.Resize(w, 1)
		blendLine.SetPosition(0, 1)
		blendLine.Resize(w, 1)
	})

	return sb
}

// SetActions injects the desktop callback interface.
func (sb *StatusBarApp) SetActions(actions StatusBarActions) {
	sb.mu.Lock()
	sb.actions = actions
	sb.mu.Unlock()
}

// --- core.App interface (delegated to adapter.UIApp) ---

func (sb *StatusBarApp) Run() error {
	// Start clock ticker
	go sb.clockLoop()
	return sb.app.Run()
}

func (sb *StatusBarApp) Stop() {
	close(sb.stopClock)
	sb.app.Stop()
}

func (sb *StatusBarApp) Resize(cols, rows int) { sb.app.Resize(cols, rows) }
func (sb *StatusBarApp) GetTitle() string       { return sb.app.GetTitle() }
func (sb *StatusBarApp) HandleKey(ev *tcell.EventKey) { sb.app.HandleKey(ev) }

func (sb *StatusBarApp) SetRefreshNotifier(ch chan<- bool) {
	sb.app.SetRefreshNotifier(ch)
}

func (sb *StatusBarApp) Render() [][]texelcore.Cell {
	return sb.app.Render()
}

// --- texel.Listener interface ---

func (sb *StatusBarApp) OnEvent(event texel.Event) {
	switch event.Type {
	case texel.EventWorkspacesChanged:
		if p, ok := event.Payload.(texel.WorkspacesChangedPayload); ok {
			sb.handleWorkspacesChanged(p)
		}
	case texel.EventWorkspaceSwitched:
		if p, ok := event.Payload.(texel.WorkspaceSwitchedPayload); ok {
			sb.handleWorkspaceSwitched(p)
		}
	case texel.EventModeChanged:
		if p, ok := event.Payload.(texel.ModeChangedPayload); ok {
			sb.blendLine.SetMode(p.InControlMode, p.SubMode)
		}
	case texel.EventActivePaneChanged:
		if p, ok := event.Payload.(texel.ActivePaneChangedPayload); ok {
			sb.blendLine.SetTitle(p.ActiveTitle)
		}
	case texel.EventPerformanceUpdate:
		if p, ok := event.Payload.(texel.PerformanceUpdatePayload); ok {
			sb.updateFPS(p.LastPublishDuration)
		}
	case texel.EventToast:
		if p, ok := event.Payload.(texel.ToastPayload); ok {
			sb.blendLine.ShowToast(p.Message, p.Severity, p.Duration)
		}
	}
}

func (sb *StatusBarApp) handleWorkspacesChanged(p texel.WorkspacesChangedPayload) {
	sb.mu.Lock()
	prevCount := len(sb.workspaces)
	sb.workspaces = p.Workspaces
	sb.activeID = p.ActiveID
	sb.mu.Unlock()

	// Rebuild tab items
	tabs := make([]primitives.TabItem, len(p.Workspaces))
	activeIdx := 0
	for i, ws := range p.Workspaces {
		tabs[i] = primitives.TabItem{Label: ws.Name, ID: fmt.Sprintf("%d", ws.ID)}
		if ws.ID == p.ActiveID {
			activeIdx = i
		}
	}
	sb.tabBar.Tabs = tabs
	sb.tabBar.SetActive(activeIdx)

	// Update blend line accent from active workspace
	for _, ws := range p.Workspaces {
		if ws.ID == p.ActiveID {
			sb.blendLine.SetAccentColor(ws.Color)
			break
		}
	}

	// If a new workspace was added (count increased) and it's not ws 1, enter edit mode
	if len(p.Workspaces) > prevCount && len(p.Workspaces) > 1 {
		newIdx := len(p.Workspaces) - 1
		if p.Workspaces[newIdx].ID > 1 {
			sb.tabBar.EditTab(newIdx)
		}
	}

	sb.refresh()
}

func (sb *StatusBarApp) handleWorkspaceSwitched(p texel.WorkspaceSwitchedPayload) {
	sb.mu.Lock()
	sb.activeID = p.ActiveID
	workspaces := sb.workspaces
	sb.mu.Unlock()

	for i, ws := range workspaces {
		if ws.ID == p.ActiveID {
			sb.tabBar.SetActive(i)
			sb.blendLine.SetAccentColor(ws.Color)
			break
		}
	}
	sb.refresh()
}

func (sb *StatusBarApp) updateFPS(publishDuration time.Duration) {
	now := time.Now()
	sb.mu.Lock()
	if !sb.lastRenderTime.IsZero() {
		elapsed := now.Sub(sb.lastRenderTime).Seconds()
		if elapsed > 0 {
			instantFPS := 1.0 / elapsed
			const alpha = 0.1
			sb.smoothFPS = sb.smoothFPS*(1-alpha) + instantFPS*alpha
		}
	}
	if publishDuration > 0 {
		theoFPS := 1.0 / publishDuration.Seconds()
		const alpha = 0.1
		sb.smoothTheoFPS = sb.smoothTheoFPS*(1-alpha) + theoFPS*alpha
	}
	sb.lastRenderTime = now
	fps := sb.smoothFPS
	theoFPS := sb.smoothTheoFPS
	sb.mu.Unlock()

	sb.blendLine.SetFPS(fps, theoFPS)
}

func (sb *StatusBarApp) clockLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sb.stopClock:
			return
		case t := <-ticker.C:
			sb.blendLine.SetClock(t.Format("15:04:05"))
			sb.refresh()
		}
	}
}

func (sb *StatusBarApp) refresh() {
	ch := sb.app.RefreshChan()
	if ch != nil {
		select {
		case ch <- true:
		default:
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/statusbar/ -v`
Expected: All tests pass.

- [ ] **Step 5: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add apps/statusbar/statusbar.go apps/statusbar/statusbar_test.go
git commit -m "Rewrite status bar as widget-based UIApp with fine-grained events"
```

---

## Task 8: Wire Status Bar via Registry and Update Server Harness

**Files:**
- Create: `texelation/apps/statusbar/init.go`
- Modify: `texelation/cmd/texel-server/main.go`

### Steps

- [ ] **Step 1: Create registry provider for status bar**

Create `texelation/apps/statusbar/init.go`:

```go
package statusbar

import (
	"github.com/framegrace/texelation/registry"
)

func init() {
	registry.RegisterBuiltInProvider(func(reg *registry.Registry) {
		reg.RegisterBuiltIn(registry.Manifest{
			Name:        "statusbar",
			DisplayName: "Status Bar",
			Description: "Workspace tabs, mode indicator, and system info",
			Version:     "2.0.0",
			Type:        registry.AppTypeBuiltIn,
			Icon:        "bar",
			Category:    "system",
		}, func(config map[string]interface{}) interface{} {
			return New()
		})
	})
}
```

- [ ] **Step 2: Update server harness**

In `texelation/cmd/texel-server/main.go`, replace the direct `statusbar.New()` call with registry-based creation and inject `StatusBarActions`.

Replace lines 164-165:
```go
// Old:
// status := statusbar.New()
// desktop.AddStatusPane(status, texel.SideTop, 1)

// New:
statusApp := desktop.Registry().CreateApp("statusbar", nil)
if sb, ok := statusApp.(*statusbar.StatusBarApp); ok {
	sb.SetActions(desktop)
}
desktop.AddStatusPane(statusApp.(texel.App), texel.SideTop, 2)
```

This requires that `DesktopEngine` implements `StatusBarActions`. Add this check — `DesktopEngine` already has `SwitchToWorkspace(id int)`. It needs `RenameWorkspace(id int, name string)` which was added in Task 1.

- [ ] **Step 3: Verify DesktopEngine satisfies StatusBarActions**

In `texelation/texel/desktop_engine_core.go`, add a compile-time check (near the top of the file, after imports):

```go
var _ statusbar.StatusBarActions = (*DesktopEngine)(nil)
```

Wait — this creates a circular import (`texel` → `statusbar` → `texel`). Instead, define the `StatusBarActions` interface in the `texel` package:

Move the interface from `apps/statusbar/statusbar.go` to `texel/desktop_status.go`:

```go
// StatusBarActions is the callback interface for status bar → desktop commands.
type StatusBarActions interface {
	SwitchToWorkspace(id int)
	RenameWorkspace(id int, name string)
}
```

And in `apps/statusbar/statusbar.go`, change the type reference:
```go
actions texel.StatusBarActions
```

Update `SetActions` signature:
```go
func (sb *StatusBarApp) SetActions(actions texel.StatusBarActions) {
```

Then in `texel/desktop_engine_core.go`:
```go
// compile-time check
var _ StatusBarActions = (*DesktopEngine)(nil)
```

And in `cmd/texel-server/main.go`:
```go
if sb, ok := statusApp.(*statusbar.StatusBarApp); ok {
	sb.SetActions(desktop)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass.

- [ ] **Step 5: Build and verify**

Run: `cd /home/marc/projects/texel/texelation && make build`
Expected: Clean build.

- [ ] **Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add apps/statusbar/init.go apps/statusbar/statusbar.go texel/desktop_status.go texel/desktop_engine_core.go cmd/texel-server/main.go
git commit -m "Wire status bar via app registry with StatusBarActions injection"
```

---

## Task 9: Update Protocol StateUpdate for Workspace Names and Colors

**Files:**
- Modify: `texelation/protocol/messages.go`
- Modify: `texelation/internal/runtime/server/connection_sync.go`
- Modify: `texelation/internal/runtime/server/connection.go`

### Steps

- [ ] **Step 1: Add workspace info to protocol StateUpdate**

In `texelation/protocol/messages.go`, update the `StateUpdate` struct:

```go
type StateUpdate struct {
	WorkspaceID   int32
	AllWorkspaces []int32
	InControlMode bool
	SubMode       rune
	ActiveTitle   string
	DesktopBgRGB  uint32
	Zoomed        bool
	ZoomedPaneID  [16]byte
	// New fields for workspace metadata
	WorkspaceNames  []string // parallel to AllWorkspaces
	WorkspaceColors []uint32 // parallel to AllWorkspaces, RGB packed
}
```

Update the `EncodeStateUpdate` and `DecodeStateUpdate` functions to serialize the new fields. The implementing agent should follow the existing encoding pattern (length-prefixed arrays with binary.Write).

- [ ] **Step 2: Update sendStateUpdate to populate new fields**

In `connection_sync.go`, modify `sendStateUpdate` to include workspace names and colors from the desktop:

```go
func (c *connection) sendStateUpdate(state texel.StatePayload) {
	// ... existing code ...

	// Populate workspace metadata from desktop
	sink, ok := c.sink.(*DesktopSink)
	var names []string
	var colors []uint32
	if ok && sink.Desktop() != nil {
		wsPayload := sink.Desktop().WorkspacesInfo()
		names = make([]string, len(wsPayload))
		colors = make([]uint32, len(wsPayload))
		for i, ws := range wsPayload {
			names[i] = ws.Name
			r, g, b := ws.Color.RGB()
			colors[i] = colorToRGB(r, g, b)
		}
	}

	update := protocol.StateUpdate{
		// ... existing fields ...
		WorkspaceNames:  names,
		WorkspaceColors: colors,
	}
	// ... rest unchanged
}
```

Add a `WorkspacesInfo()` method to `DesktopEngine` that returns `[]WorkspaceInfo`:

```go
// In texel/desktop_engine_core.go:
func (d *DesktopEngine) WorkspacesInfo() []WorkspaceInfo {
	return d.workspacesChangedPayload().Workspaces
}
```

- [ ] **Step 3: Update initial state push on handshake**

In `connection.go`, around line 68 where `CurrentStatePayload()` is called, ensure the initial push also includes workspace metadata. Since `sendStateUpdate` now fetches it from the desktop, this should work automatically.

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add protocol/messages.go internal/runtime/server/connection_sync.go internal/runtime/server/connection.go texel/desktop_engine_core.go
git commit -m "Add workspace names and colors to protocol StateUpdate"
```

---

## Task 10: Persist Workspace Names and Colors in Snapshots

**Files:**
- Modify: `texelation/internal/runtime/server/snapshot_store.go`
- Modify: `texelation/texel/snapshot.go` (if workspace metadata is captured there)

### Steps

- [ ] **Step 1: Add workspace metadata to StoredSnapshot**

In `snapshot_store.go`, add workspace metadata to the snapshot format:

```go
type WorkspaceMetadata struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color uint32 `json:"color"` // RGB packed
}

type StoredSnapshot struct {
	// ... existing fields ...
	WorkspaceMetadata []WorkspaceMetadata `json:"workspace_metadata,omitempty"`
}
```

- [ ] **Step 2: Populate metadata on save**

In the snapshot save path, populate `WorkspaceMetadata` from the desktop's workspace info. The implementing agent should find the `Save` method and add the metadata collection before serialization.

- [ ] **Step 3: Restore metadata on load**

In the snapshot restore path, apply saved names and colors to workspaces after they are created. The implementing agent should find the restore method and add name/color assignment after workspace creation.

- [ ] **Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add internal/runtime/server/snapshot_store.go texel/snapshot.go
git commit -m "Persist workspace names and colors in snapshots"
```

---

## Task 11: Clean Up Legacy EventStateUpdate

**Files:**
- Modify: `texelation/texel/dispatcher.go`
- Modify: `texelation/texel/desktop_engine_core.go`

### Steps

- [ ] **Step 1: Verify no code depends on EventStateUpdate except protocol path**

Run: `grep -rn "EventStateUpdate\|StatePayload\|broadcastStateUpdate\|currentStatePayload\|CurrentStatePayload\|shouldBroadcastState\|storeLastState" --include="*.go" texelation/`

The only remaining references should be:
- `dispatcher.go` (definitions)
- `desktop_engine_core.go` (`broadcastStateUpdate` called from `SetRefreshHandler`, `currentStatePayload`, `CurrentStatePayload`)
- `connection_sync.go` (protocol forwarding)
- `connection.go` (initial handshake)

If any other files still reference these, fix them first.

- [ ] **Step 2: Keep EventStateUpdate and StatePayload for protocol use**

Since the protocol path (`connection_sync.go`) still uses `StatePayload` and `EventStateUpdate`, we cannot fully remove them yet. Mark them as deprecated with a comment:

```go
// Deprecated: EventStateUpdate is used only for protocol client sync.
// New code should use EventWorkspacesChanged, EventWorkspaceSwitched, etc.
EventStateUpdate
```

```go
// Deprecated: StatePayload is used only for protocol client sync.
// New code should use fine-grained payloads.
type StatePayload struct {
```

- [ ] **Step 3: Remove unused deduplication fields if possible**

If `shouldBroadcastState`/`storeLastState` are only called from `broadcastStateUpdate`, and `broadcastStateUpdate` is only called from `SetRefreshHandler`, the deduplication is still useful (avoids spamming protocol updates when nothing changed).

Keep them in place. The full removal can happen when the protocol migrates to fine-grained messages (out of scope).

- [ ] **Step 4: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: All tests pass.

- [ ] **Step 5: Build all binaries**

Run: `cd /home/marc/projects/texel/texelation && make build`
Expected: Clean build.

- [ ] **Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelation && git add texel/dispatcher.go texel/desktop_engine_core.go
git commit -m "Mark EventStateUpdate/StatePayload as deprecated (protocol-only)"
```

---

## Task Summary

| Task | Description | Depends On |
|------|-------------|------------|
| 1 | Add Name/Color to Workspace struct + palette | — |
| 2 | Define fine-grained event types | — |
| 3 | Add fine-grained broadcast methods | 1, 2 |
| 4 | Add TabBar edit mode (texelui) | — |
| 5 | Migrate all broadcastStateUpdate call sites | 3 |
| 6 | Implement BlendInfoLine widget | 2 |
| 7 | Rewrite status bar as UIApp | 4, 5, 6 |
| 8 | Wire via registry + StatusBarActions | 7 |
| 9 | Update protocol StateUpdate | 1, 5 |
| 10 | Persist workspace metadata in snapshots | 1, 8 |
| 11 | Clean up legacy EventStateUpdate | 5, 9 |

Tasks 1, 2, and 4 are independent and can be done in parallel.
Tasks 3, 6 depend on 1+2.
Task 5 depends on 3.
Task 7 is the big integration task depending on 4, 5, 6.
Tasks 8-11 are sequential finalization.
