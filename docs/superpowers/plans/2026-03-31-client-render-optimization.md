# Client Render Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce client CPU usage to near-zero at idle by only re-compositing and re-rendering cells that actually changed.

**Architecture:** Add dirty tracking at pane and row level in `BufferCache`. Keep a persistent workspace buffer on `clientState`. On each render, skip unchanged panes, only composite dirty rows, and diff against the previous buffer to minimize `SetContent` calls. Fall back to full render for geometry changes and workspace effects.

**Tech Stack:** Go 1.24.3, tcell/v2, texelation client runtime

**Spec:** `docs/superpowers/specs/2026-03-31-client-render-optimization-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `client/buffercache.go` (modify) | Add `dirty`, `dirtyRows`, `hasAnimatedCells` to `PaneState`; set in `ApplyDelta`/`ApplySnapshot`; add `ClearDirty()` |
| `client/buffercache_test.go` (modify) | Tests for dirty tracking |
| `internal/runtime/client/client_state.go` (modify) | Add `prevBuffer`, `fullRenderNeeded` |
| `internal/runtime/client/renderer.go` (modify) | Incremental `render`, `incrementalComposite`, `diffAndShow` |
| `internal/runtime/client/renderer_test.go` (modify) | Tests for incremental rendering |
| `internal/runtime/client/protocol_handler.go` (modify) | Set `fullRenderNeeded` on tree snapshot |
| `internal/effects/manager.go` (modify) | Add `HasActiveWorkspaceEffects()` |

---

## Task 1: Add Dirty Tracking to PaneState

**Files:**
- Modify: `/home/marc/projects/texel/texelation/client/buffercache.go`
- Modify: `/home/marc/projects/texel/texelation/client/buffercache_test.go`

### Steps

- [ ] **Step 1: Add dirty fields to PaneState**

In `/home/marc/projects/texel/texelation/client/buffercache.go`, add to the `PaneState` struct:

```go
type PaneState struct {
	ID               [16]byte
	Revision         uint32
	UpdatedAt        time.Time
	rowsMu           sync.RWMutex
	rows             map[int][]Cell
	Title            string
	Rect             clientRect
	Active           bool
	Resizing         bool
	ZOrder           int
	HandlesSelection bool

	// Dirty tracking for incremental rendering.
	Dirty         bool         // true when pane has new content since last render
	DirtyRows     map[int]bool // nil = all rows dirty; non-nil = only listed rows
	HasAnimated   bool         // true if any cell has animated DynFG/DynBG
}
```

- [ ] **Step 2: Add ClearDirty method**

```go
// ClearDirty resets the dirty flags after rendering.
func (p *PaneState) ClearDirty() {
	p.Dirty = false
	p.DirtyRows = nil
}
```

- [ ] **Step 3: Set dirty flags in ApplyDelta**

In `ApplyDelta`, after the row loop (after `pane.rowsMu.Unlock()`), add:

```go
	pane.rowsMu.Unlock()

	// Mark pane and specific rows as dirty for incremental rendering.
	pane.Dirty = true
	if pane.DirtyRows == nil && len(delta.Rows) < int(pane.Rect.Height) {
		pane.DirtyRows = make(map[int]bool, len(delta.Rows))
	}
	if pane.DirtyRows != nil {
		for _, rowDelta := range delta.Rows {
			pane.DirtyRows[int(rowDelta.Row)] = true
		}
	}

	// Check if any cell in the delta has animated dynamic colors.
	for _, entry := range delta.Styles {
		if entry.AttrFlags&protocol.AttrHasDynamic != 0 {
			if protocolDescIsAnimated(entry.DynFG) || protocolDescIsAnimated(entry.DynBG) {
				pane.HasAnimated = true
				break
			}
		}
	}

	pane.Revision = delta.Revision
```

Add the `protocolDescIsAnimated` helper to `buffercache.go` (same logic as in renderer.go — keep it DRY by moving it here and exporting, or duplicate since it's 10 lines):

```go
func protocolDescIsAnimated(d protocol.DynColorDesc) bool {
	if d.Type >= 2 && d.Type <= 3 {
		return true
	}
	for _, s := range d.Stops {
		if s.Color.Type >= 2 && s.Color.Type <= 3 {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Set dirty flags in ApplySnapshot**

In `ApplySnapshot`, after setting `pane.Rect`, add:

```go
		pane.Rect = clientRect{X: int(paneSnap.X), Y: int(paneSnap.Y), Width: int(paneSnap.Width), Height: int(paneSnap.Height)}
		pane.Dirty = true
		pane.DirtyRows = nil // nil = all rows dirty
```

- [ ] **Step 5: Write tests**

Append to `/home/marc/projects/texel/texelation/client/buffercache_test.go` (create if it doesn't exist):

```go
func TestPaneState_DirtyOnDelta(t *testing.T) {
	cache := NewBufferCache()
	paneID := [16]byte{1}
	cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: paneID, X: 0, Y: 0, Width: 80, Height: 24,
		}},
	})

	pane := cache.PaneByID(paneID)
	// Snapshot marks pane dirty with all rows
	if !pane.Dirty {
		t.Error("expected pane dirty after snapshot")
	}
	if pane.DirtyRows != nil {
		t.Error("expected DirtyRows nil (all dirty) after snapshot")
	}

	pane.ClearDirty()
	if pane.Dirty {
		t.Error("expected pane clean after ClearDirty")
	}

	// Apply delta — marks specific rows dirty
	cache.ApplyDelta(protocol.BufferDelta{
		PaneID:   paneID,
		Revision: 1,
		Styles:   []protocol.StyleEntry{{FgModel: protocol.ColorModelRGB, FgValue: 0xFF0000}},
		Rows: []protocol.RowDelta{
			{Row: 5, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}},
			{Row: 10, Spans: []protocol.CellSpan{{StartCol: 0, Text: "world", StyleIndex: 0}}},
		},
	})

	if !pane.Dirty {
		t.Error("expected pane dirty after delta")
	}
	if pane.DirtyRows == nil {
		t.Error("expected specific DirtyRows after delta")
	}
	if !pane.DirtyRows[5] || !pane.DirtyRows[10] {
		t.Error("expected rows 5 and 10 dirty")
	}
	if pane.DirtyRows[0] {
		t.Error("row 0 should not be dirty")
	}
}
```

- [ ] **Step 6: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./client/ -v -run TestPaneState_Dirty`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add client/buffercache.go client/buffercache_test.go
git commit -m "Add pane and row dirty tracking to PaneState"
```

---

## Task 2: Add Persistent Buffer and fullRenderNeeded to clientState

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/client_state.go`
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/protocol_handler.go`
- Modify: `/home/marc/projects/texel/texelation/internal/effects/manager.go`

### Steps

- [ ] **Step 1: Add prevBuffer and fullRenderNeeded to clientState**

In `/home/marc/projects/texel/texelation/internal/runtime/client/client_state.go`, add to `clientState`:

```go
	// Incremental rendering state
	prevBuffer      [][]client.Cell
	fullRenderNeeded bool
```

- [ ] **Step 2: Set fullRenderNeeded on tree snapshot**

In `/home/marc/projects/texel/texelation/internal/runtime/client/protocol_handler.go`, in the `MsgTreeSnapshot` case, after `cache.ApplySnapshot(snap)`:

```go
	case protocol.MsgTreeSnapshot:
		snap, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			log.Printf("decode snapshot failed: %v", err)
			return false
		}
		cache.ApplySnapshot(snap)
		state.fullRenderNeeded = true
		if state.effects != nil {
			state.effects.ResetPaneStates(cache.SortedPanes())
		}
		return true
```

- [ ] **Step 3: Add HasActiveWorkspaceEffects to effects Manager**

In `/home/marc/projects/texel/texelation/internal/effects/manager.go`, add:

```go
// HasActiveWorkspaceEffects returns true if any workspace effect is currently active.
func (m *Manager) HasActiveWorkspaceEffects() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, eff := range m.workspaceEffects {
		if eff.Active() {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run build**

Run: `cd /home/marc/projects/texel/texelation && go build ./...`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/client_state.go internal/runtime/client/protocol_handler.go internal/effects/manager.go
git commit -m "Add prevBuffer, fullRenderNeeded, HasActiveWorkspaceEffects"
```

---

## Task 3: Implement Incremental Render Path

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/renderer.go`

### Steps

- [ ] **Step 1: Add helper to allocate or resize the persistent buffer**

Add at the top of renderer.go (after imports):

```go
// ensurePrevBuffer allocates or resizes the persistent workspace buffer.
// Returns true if the buffer was reallocated (size changed).
func ensurePrevBuffer(state *clientState, width, height int) bool {
	if len(state.prevBuffer) == height && (height == 0 || len(state.prevBuffer[0]) == width) {
		return false
	}
	state.prevBuffer = make([][]client.Cell, height)
	for y := 0; y < height; y++ {
		row := make([]client.Cell, width)
		for x := range row {
			row[x] = client.Cell{Ch: ' ', Style: state.defaultStyle}
		}
		state.prevBuffer[y] = row
	}
	return true
}
```

- [ ] **Step 2: Add diff-based screen output function**

```go
// diffAndShow writes only changed cells to the tcell screen.
func diffAndShow(screen tcell.Screen, current, previous [][]client.Cell, defaultStyle tcell.Style) {
	for y, row := range current {
		if y >= len(previous) {
			break
		}
		prevRow := previous[y]
		for x, cell := range row {
			if x >= len(prevRow) {
				break
			}
			prev := prevRow[x]
			if cell.Ch == prev.Ch && cell.Style == prev.Style {
				continue
			}
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			style := cell.Style
			if style == (tcell.Style{}) {
				style = defaultStyle
			}
			screen.SetContent(x, y, ch, nil, style)
		}
	}
	screen.Show()
}
```

- [ ] **Step 3: Add incrementalComposite function**

This composites only dirty panes and dirty rows into the existing `prevBuffer`:

```go
// incrementalComposite updates only dirty panes/rows in the persistent buffer.
// Returns true if any animated cells exist (need continuous rendering).
func incrementalComposite(state *clientState, screenW, screenH int) bool {
	hasDynamic := false
	animTime := float32(time.Since(state.animStart).Seconds())
	panes := state.cache.SortedPanes()

	for _, pane := range panes {
		// Animated panes must re-composite every frame even without new deltas.
		if !pane.Dirty && !pane.HasAnimated {
			continue
		}

		x := pane.Rect.X
		y := pane.Rect.Y
		w := pane.Rect.Width
		h := pane.Rect.Height
		if w <= 0 || h <= 0 {
			continue
		}

		for rowIdx := 0; rowIdx < h; rowIdx++ {
			// Skip unchanged rows (unless all rows are dirty or pane has animation).
			if pane.DirtyRows != nil && !pane.DirtyRows[rowIdx] && !pane.HasAnimated {
				continue
			}

			targetY := y + rowIdx
			if targetY < 0 || targetY >= screenH {
				continue
			}

			source := pane.RowCells(rowIdx)
			for col := 0; col < w; col++ {
				targetX := x + col
				if targetX < 0 || targetX >= screenW {
					continue
				}

				var cell client.Cell
				if col < len(source) {
					cell = source[col]
				}
				if cell.Ch == 0 {
					cell.Ch = ' '
				}
				if cell.Style == (tcell.Style{}) {
					cell.Style = state.defaultStyle
				}

				style := cell.Style

				// Resolve dynamic colors client-side
				if cell.DynBG.Type >= 2 || cell.DynFG.Type >= 2 {
					ctx := color.ColorContext{
						X: col, Y: rowIdx,
						W: w, H: h,
						PX: x, PY: y,
						PW: w, PH: h,
						SX: targetX, SY: targetY,
						SW: screenW, SH: screenH,
						T: animTime,
					}
					fg, bg, attrs := style.Decompose()
					if cell.DynBG.Type >= 2 {
						bg = color.FromDesc(protocolDescToColor(cell.DynBG)).Resolve(ctx)
					}
					if cell.DynFG.Type >= 2 {
						fg = color.FromDesc(protocolDescToColor(cell.DynFG)).Resolve(ctx)
					}
					style = tcell.StyleDefault.Foreground(fg).Background(bg).Attributes(attrs)
					if protocolDescIsAnimated(cell.DynBG) || protocolDescIsAnimated(cell.DynFG) {
						hasDynamic = true
					}
				}

				state.prevBuffer[targetY][targetX] = client.Cell{Ch: cell.Ch, Style: style}
			}
		}

		// Apply pane effects if any are active
		if state.effects != nil {
			// Build a sub-buffer view for the pane's screen region.
			paneView := make([][]client.Cell, h)
			for r := 0; r < h; r++ {
				ty := y + r
				if ty >= 0 && ty < screenH && x >= 0 && x+w <= screenW {
					paneView[r] = state.prevBuffer[ty][x : x+w]
				} else {
					paneView[r] = make([]client.Cell, w) // out of bounds — dummy
				}
			}
			state.effects.ApplyPaneEffects(pane, paneView)
		}

		pane.ClearDirty()
	}

	return hasDynamic
}
```

- [ ] **Step 4: Rename existing render to fullRender**

Rename the current `render` function to `fullRender`:

```go
func fullRender(state *clientState, screen tcell.Screen) {
	// ... existing render body unchanged ...
}
```

- [ ] **Step 5: Write new render function that dispatches between full and incremental**

```go
func render(state *clientState, screen tcell.Screen) {
	width, height := screen.Size()

	// Check if we need a full render.
	resized := ensurePrevBuffer(state, width, height)
	needsFull := state.fullRenderNeeded || resized || state.prevBuffer == nil
	if !needsFull && state.effects != nil && state.effects.HasActiveWorkspaceEffects() {
		needsFull = true
	}

	if needsFull {
		fullRender(state, screen)
		state.fullRenderNeeded = false
		// Copy the full render result into prevBuffer for future diffs.
		// fullRender writes directly to screen, so we need to capture its output.
		// Instead, modify fullRender to also write to prevBuffer.
		return
	}

	// Incremental path: update effects timing.
	if state.effects != nil {
		state.effects.Update(time.Now())
	}

	// Take a snapshot of prevBuffer for diffing.
	oldBuffer := make([][]client.Cell, height)
	for y := 0; y < height; y++ {
		oldBuffer[y] = make([]client.Cell, width)
		copy(oldBuffer[y], state.prevBuffer[y])
	}

	hasDynamic := incrementalComposite(state, width, height)
	diffAndShow(screen, state.prevBuffer, oldBuffer, state.defaultStyle)
	state.dynAnimating = hasDynamic
}
```

- [ ] **Step 6: Update fullRender to write to prevBuffer**

At the end of `fullRender`, after `showWorkspaceBuffer` and before `screen.Show()`, copy the workspace buffer to `prevBuffer`:

```go
	showWorkspaceBuffer(screen, workspaceBuffer, state.defaultStyle)
	screen.Show()

	// Store rendered result for future incremental diffs.
	ensurePrevBuffer(state, width, height)
	for y := 0; y < height && y < len(workspaceBuffer); y++ {
		copy(state.prevBuffer[y], workspaceBuffer[y])
	}

	// Clear all pane dirty flags after full render.
	for _, pane := range state.cache.SortedPanes() {
		pane.ClearDirty()
	}

	state.dynAnimating = hasDynamic
```

Note: `hasDynamic` is the existing variable from `compositeInto`'s return value already in `fullRender`. Make sure `state.dynAnimating` is set here AND at the end of fullRender (remove the existing assignment at the bottom that was in the old `render`).

- [ ] **Step 7: Remove screen.Clear() reference in incremental path**

The incremental path (`render` → `incrementalComposite` → `diffAndShow`) never calls `screen.Clear()`. Only `fullRender` calls it. Verify this is the case — no change needed if `screen.Clear()` is only in `fullRender`.

- [ ] **Step 8: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/client/ -v`
Expected: all existing tests pass. (The new incremental path is exercised when `fullRenderNeeded` is false.)

- [ ] **Step 9: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/client/renderer.go
git commit -m "Incremental render: skip unchanged panes, diff-based screen output"
```

---

## Task 4: Handle Resize and Input Events

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/input_handler.go`

### Steps

- [ ] **Step 1: Set fullRenderNeeded on resize**

In `/home/marc/projects/texel/texelation/internal/runtime/client/input_handler.go`, find the resize event handling. Search for `EventResize`:

```go
	case *tcell.EventResize:
```

Add `state.fullRenderNeeded = true` before the render call. If there's no explicit resize case, check if `render(state, screen)` is called after resize events — the `screen.Size()` change in the next `render` call will trigger buffer reallocation via `ensurePrevBuffer`, which returns `true` and triggers `needsFull`. So resize is already handled by `ensurePrevBuffer`. But setting the flag explicitly is safer:

If there IS a resize handler:
```go
	case *tcell.EventResize:
		state.fullRenderNeeded = true
		// ... existing resize handling ...
```

If there ISN'T a resize handler (resize is handled by tcell internally and the next render picks up the new size), no change needed — `ensurePrevBuffer` handles it.

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelation && make test`
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/client/input_handler.go
git commit -m "Set fullRenderNeeded on screen resize"
```

---

## Task 5: Integration Test and Manual Verification

**Files:**
- Modify: `/home/marc/projects/texel/texelation/internal/runtime/client/renderer_test.go`

### Steps

- [ ] **Step 1: Write test for incremental render skipping clean panes**

Append to `/home/marc/projects/texel/texelation/internal/runtime/client/renderer_test.go`:

```go
func TestIncrementalComposite_SkipsCleanPanes(t *testing.T) {
	paneID := [16]byte{1}
	cache := client.NewBufferCache()
	cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: paneID, X: 0, Y: 0, Width: 10, Height: 2,
		}},
	})
	cache.ApplyDelta(protocol.BufferDelta{
		PaneID:   paneID,
		Revision: 1,
		Styles:   []protocol.StyleEntry{{FgModel: protocol.ColorModelRGB, FgValue: 0xFF0000}},
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}}},
	})

	state := &clientState{
		cache:        cache,
		defaultStyle: tcell.StyleDefault,
		animStart:    time.Now(),
	}

	// First: full render to populate prevBuffer.
	ensurePrevBuffer(state, 10, 2)
	state.fullRenderNeeded = false

	// Pane is dirty from delta — incremental composite should update it.
	pane := cache.PaneByID(paneID)
	if !pane.Dirty {
		t.Fatal("pane should be dirty after delta")
	}

	hasDyn := incrementalComposite(state, 10, 2)
	if hasDyn {
		t.Error("no animated cells, should not report hasDynamic")
	}

	// After composite, pane should be clean.
	if pane.Dirty {
		t.Error("pane should be clean after incrementalComposite")
	}

	// Verify cell was written to prevBuffer.
	if state.prevBuffer[0][0].Ch != 'h' {
		t.Errorf("expected 'h' at (0,0), got '%c'", state.prevBuffer[0][0].Ch)
	}

	// Now apply no new delta — pane stays clean.
	hasDyn = incrementalComposite(state, 10, 2)
	// prevBuffer should still have the old content (not cleared).
	if state.prevBuffer[0][0].Ch != 'h' {
		t.Errorf("clean pane should preserve prevBuffer content, got '%c'", state.prevBuffer[0][0].Ch)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./internal/runtime/client/ -v -run TestIncremental`
Expected: PASS.

- [ ] **Step 3: Run full test suite and build**

Run: `cd /home/marc/projects/texel/texelation && make test && make build`
Expected: all pass, build clean.

- [ ] **Step 4: Manual verification**

Test with `./bin/texelation`:
1. **Idle**: CPU should be near 0% with no input or animation
2. **Typing in terminal**: CPU spikes only while typing, drops when idle
3. **Tab nav mode**: pulse animation runs at 60fps, other panes are not re-composited
4. **Workspace switch**: one full render on switch, then incremental
5. **Resize**: one full render, then incremental

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/renderer_test.go
git commit -m "Add integration test for incremental compositing"
```

---

## Task Summary

| Task | Description | Depends On |
|------|-------------|------------|
| 1 | Dirty tracking on PaneState | — |
| 2 | prevBuffer, fullRenderNeeded, HasActiveWorkspaceEffects | — |
| 3 | Incremental render path (core implementation) | 1, 2 |
| 4 | Handle resize events | 3 |
| 5 | Integration test + manual verification | 3, 4 |
