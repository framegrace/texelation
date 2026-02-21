# Texelterm Status Bar & Transformer Toggle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a status bar to texelterm showing mode indicators and make Ctrl+T fully disable/enable the transformer pipeline (not just overlay visibility).

**Architecture:** Bridge the existing `texelui/widgets/StatusBar` widget into TexelTerm's render pipeline via `core.Painter`. Extend StatusBar with `StyledSegment` for per-token styling on the left side. Add runtime enable/disable to the transformer `Pipeline`. Store the pipeline reference in TexelTerm for Ctrl+T toggling.

**Tech Stack:** Go, texelui (core.Painter, widgets.StatusBar), tcell

---

### Task 1: Add `StyledSegment` and `SetLeftSegments` to StatusBar widget

**Files:**
- Modify: `texelui/widgets/statusbar.go:38-54` (struct fields), `:300-405` (Draw method)
- Test: `texelui/widgets/statusbar_test.go`

**Step 1: Write the failing test**

Add to `texelui/widgets/statusbar_test.go`:

```go
func TestStatusBarStyledSegments(t *testing.T) {
	sb := NewStatusBar()
	sb.SetPosition(0, 0)
	sb.Resize(40, 2)

	bright := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	dim := tcell.StyleDefault.Foreground(tcell.ColorGray).Background(tcell.ColorBlack)

	sb.SetLeftSegments([]StyledSegment{
		{Text: "TFM", Style: bright},
		{Text: " ", Style: dim},
		{Text: "INS", Style: dim},
	})

	// Draw into a buffer
	buf := make([][]core.Cell, 2)
	for y := range buf {
		buf[y] = make([]core.Cell, 40)
	}
	p := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 2})
	sb.Draw(p)

	// Row 1 (content row) should have "TFM INS" starting at x=1
	got := string([]rune{buf[1][1].Ch, buf[1][2].Ch, buf[1][3].Ch})
	if got != "TFM" {
		t.Errorf("expected 'TFM' at x=1, got %q", got)
	}

	// TFM should have the bright style's foreground
	if fg, _, _ := buf[1][1].Style.Decompose(); fg != tcell.ColorWhite {
		t.Errorf("expected TFM foreground=white, got %v", fg)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run TestStatusBarStyledSegments -v`
Expected: Compilation error — `StyledSegment` and `SetLeftSegments` undefined.

**Step 3: Write the implementation**

In `texelui/widgets/statusbar.go`:

1. Add the `StyledSegment` type after the `TimedMessage` struct (after line 33):

```go
// StyledSegment is a text fragment with optional custom styling.
// Used for mode indicators where each segment needs independent styling.
type StyledSegment struct {
	Text  string
	Style tcell.Style // Zero value = use default hint style (dimmed)
}
```

2. Add `leftSegments` field to the `StatusBar` struct (after line 42, alongside `leftText`):

```go
	leftSegments []StyledSegment // Styled left content (overrides leftText when set)
```

3. Add `SetLeftSegments` method (after `ClearKeyHints`, around line 476):

```go
// SetLeftSegments sets styled content for the left side of the status bar.
// Takes priority over KeyHintsProvider hints when set.
// Pass nil to clear and revert to KeyHintsProvider behavior.
func (s *StatusBar) SetLeftSegments(segments []StyledSegment) {
	s.mu.Lock()
	s.leftSegments = segments
	s.mu.Unlock()
	s.invalidate()
}
```

4. Modify `Draw` method (at line 321-378) to use segments when available. Replace the section that reads `leftText` and draws it. After `s.mu.Lock()` at line 321 and before `s.mu.Unlock()` at line 327, add a check:

```go
	s.mu.Lock()
	segments := s.leftSegments
	if segments == nil {
		s.updateKeyHintsLocked()
	}
	leftText := s.leftText
	activeMsg := s.getActiveMessage()
	s.mu.Unlock()
```

Then replace the left-side drawing block (lines 371-379) with:

```go
	// Draw left content
	if segments != nil {
		// Draw styled segments
		xx := s.Rect.X + 1
		for _, seg := range segments {
			for _, r := range seg.Text {
				if xx >= s.Rect.X+s.Rect.W-1 {
					break
				}
				style := seg.Style
				if style == (tcell.Style{}) {
					// Zero value: use default hint style
					hintFg := tm.GetSemanticColor("text.secondary")
					if hintFg == tcell.ColorDefault {
						hintFg = tcell.ColorGray
					}
					style = tcell.StyleDefault.Foreground(hintFg).Background(bg)
				}
				p.SetCell(xx, contentY, r, style)
				xx++
			}
		}
	} else if leftText != "" {
		hintFg := tm.GetSemanticColor("text.secondary")
		if hintFg == tcell.ColorDefault {
			hintFg = tcell.ColorGray
		}
		hintStyle := tcell.StyleDefault.Foreground(hintFg).Background(bg)
		p.DrawText(s.Rect.X+1, contentY, leftText, hintStyle)
	}
```

Also update the `leftRunes` calculation to account for segments width (for right-side message truncation):

```go
	// Calculate left content width for message spacing
	leftWidth := 0
	if segments != nil {
		for _, seg := range segments {
			leftWidth += len([]rune(seg.Text))
		}
	} else {
		leftWidth = len(leftRunes)
	}
```

Use `leftWidth` instead of `len(leftRunes)` in the right-side spacing calculations.

**Step 4: Run test to verify it passes**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run TestStatusBarStyledSegments -v`
Expected: PASS

**Step 5: Run all existing StatusBar tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run TestStatusBar -v`
Expected: All PASS (existing tests use KeyHintsProvider, not affected by new code path)

**Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelui
git add widgets/statusbar.go widgets/statusbar_test.go
git commit -m "Add StyledSegment support to StatusBar left side"
```

---

### Task 2: Add runtime enable/disable to transformer Pipeline

**Files:**
- Modify: `texelation/apps/texelterm/transformer/transformer.go:88-154`
- Test: `texelation/apps/texelterm/transformer/transformer_test.go`

**Step 1: Write the failing tests**

Add to `texelation/apps/texelterm/transformer/transformer_test.go`:

```go
func TestPipelineEnabled(t *testing.T) {
	p := &Pipeline{transformers: []Transformer{&stubTransformer{id: "a"}}}

	// Pipeline should be enabled by default
	if !p.Enabled() {
		t.Error("expected pipeline to be enabled by default")
	}

	p.SetEnabled(false)
	if p.Enabled() {
		t.Error("expected pipeline to be disabled after SetEnabled(false)")
	}

	p.SetEnabled(true)
	if !p.Enabled() {
		t.Error("expected pipeline to be re-enabled after SetEnabled(true)")
	}
}

func TestPipelineDisabledSkipsHandleLine(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}}
	p.SetEnabled(false)

	line := &parser.LogicalLine{}
	suppressed := p.HandleLine(1, line, false)

	if suppressed {
		t.Error("disabled pipeline should return false (no suppression)")
	}
	if s.handleCalls != 0 {
		t.Errorf("disabled pipeline should not call HandleLine, got %d calls", s.handleCalls)
	}
}

func TestPipelineDisabledSkipsNotify(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}}
	p.SetEnabled(false)

	p.NotifyPromptStart()
	p.NotifyCommandStart("ls")

	if s.promptCalls != 0 {
		t.Errorf("disabled pipeline should not call NotifyPromptStart, got %d", s.promptCalls)
	}
	if s.commandCalls != 0 {
		t.Errorf("disabled pipeline should not call NotifyCommandStart, got %d", s.commandCalls)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -run "TestPipelineEnabled|TestPipelineDisabledSkips" -v`
Expected: Compilation error — `Enabled` and `SetEnabled` undefined.

**Step 3: Write the implementation**

In `texelation/apps/texelterm/transformer/transformer.go`:

1. Add `enabled` field to Pipeline struct (line 90):

```go
type Pipeline struct {
	transformers      []Transformer
	enabled           bool
	insertFunc        func(beforeIdx int64, cells []parser.Cell)
	overlayFunc       func(lineIdx int64, cells []parser.Cell)
	persistNotifyFunc func(lineIdx int64)
}
```

2. Add `SetEnabled` and `Enabled` methods (after line 127):

```go
// SetEnabled enables or disables the pipeline at runtime.
// When disabled, HandleLine, NotifyPromptStart, and NotifyCommandStart are no-ops.
func (p *Pipeline) SetEnabled(on bool) {
	p.enabled = on
}

// Enabled returns whether the pipeline is currently enabled.
func (p *Pipeline) Enabled() bool {
	return p.enabled
}
```

3. Add early-return guards to `HandleLine` (line 132), `NotifyPromptStart` (line 143), and `NotifyCommandStart` (line 150):

```go
func (p *Pipeline) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) bool {
	if !p.enabled {
		return false
	}
	// ... existing logic
}

func (p *Pipeline) NotifyPromptStart() {
	if !p.enabled {
		return
	}
	// ... existing logic
}

func (p *Pipeline) NotifyCommandStart(cmd string) {
	if !p.enabled {
		return
	}
	// ... existing logic
}
```

4. Set `enabled: true` in `BuildPipeline` return (line 213):

```go
	return &Pipeline{transformers: transformers, enabled: true}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -run "TestPipelineEnabled|TestPipelineDisabledSkips" -v`
Expected: All PASS

**Step 5: Run all transformer tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/transformer/transformer.go apps/texelterm/transformer/transformer_test.go
git commit -m "Add runtime enable/disable to transformer Pipeline"
```

---

### Task 3: Add VTerm getter methods

**Files:**
- Modify: `texelation/apps/texelterm/parser/vterm.go:1430-1436`
- Test: `texelation/apps/texelterm/parser/vterm_test.go` (or inline verification)

**Step 1: Write the failing test**

Add to an appropriate test file (e.g., create a small test or add to existing `vterm_test.go`). If `vterm_test.go` doesn't exist, find the right test file:

```go
func TestVTermGetters(t *testing.T) {
	v := NewVTerm(80, 24)

	// insertMode defaults to false
	if v.InsertMode() {
		t.Error("expected InsertMode() false by default")
	}

	// wrapEnabled defaults based on construction
	// (WithWrap sets it; default NewVTerm does NOT set wrapEnabled)
	v2 := NewVTerm(80, 24, WithWrap(true))
	if !v2.WrapEnabled() {
		t.Error("expected WrapEnabled() true when constructed with WithWrap(true)")
	}

	v3 := NewVTerm(80, 24, WithReflow(true))
	if !v3.ReflowEnabled() {
		t.Error("expected ReflowEnabled() true when constructed with WithReflow(true)")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run TestVTermGetters -v`
Expected: Compilation error — `InsertMode`, `WrapEnabled`, `ReflowEnabled` undefined.

**Step 3: Write the implementation**

In `texelation/apps/texelterm/parser/vterm.go`, add after the existing getters block (after line 1436):

```go
// InsertMode returns true if the terminal is in insert mode (IRM).
func (v *VTerm) InsertMode() bool { return v.insertMode }

// WrapEnabled returns true if line wrapping is enabled for the main screen buffer.
func (v *VTerm) WrapEnabled() bool { return v.wrapEnabled }

// ReflowEnabled returns true if reflow on resize is enabled.
func (v *VTerm) ReflowEnabled() bool { return v.reflowEnabled }
```

**Step 4: Run test to verify it passes**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run TestVTermGetters -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/vterm_test.go
git commit -m "Add InsertMode, WrapEnabled, ReflowEnabled getters to VTerm"
```

---

### Task 4: Integrate StatusBar into TexelTerm (struct + init + lifecycle)

**Files:**
- Modify: `texelation/apps/texelterm/term.go:58-111` (struct), `:119-133` (New), `:1341-1433` (init), `:1737-1776` (Stop)

**Step 1: Add fields to TexelTerm struct**

In `texelation/apps/texelterm/term.go`, add to the struct (around line 110, after scrollbar field):

```go
	// Status bar (reuses texelui StatusBar widget)
	statusBar *widgets.StatusBar

	// Transformer pipeline reference (for runtime toggle)
	pipeline *transformer.Pipeline
```

Add the import for `widgets`:

```go
	"github.com/framegrace/texelui/widgets"
```

**Step 2: Initialize StatusBar in `New()`**

In the `New()` function (line 119-133), create the StatusBar:

```go
func New(title, command string) texelcore.App {
	term := &TexelTerm{
		title:        title,
		command:      command,
		width:        80,
		height:       24,
		stop:         make(chan struct{}),
		colorPalette: newDefaultPalette(),
		closeCh:      make(chan struct{}),
		restartCh:    make(chan struct{}, 1),
		controlBus:   texelcore.NewControlBus(),
		statusBar:    widgets.NewStatusBar(),
	}

	return term
}
```

**Step 3: Store pipeline reference in `initializeVTermFirstRun`**

In `initializeVTermFirstRun()` (line 1417), store the pipeline:

```go
	pipeline := transformer.BuildPipeline(cfg)
	a.pipeline = pipeline  // Store for runtime toggle
	if pipeline != nil {
		// ... existing wiring code stays the same
	}
```

**Step 4: Start StatusBar in `Run()` and wire refresh**

The StatusBar needs `Start()` for its message expiration ticker. In `Run()` (line 1108), or more precisely, we need to start it when we have a refresh notifier. The best place is in `SetRefreshNotifier`:

Find the existing `SetRefreshNotifier` method:

```go
func (a *TexelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.mu.Lock()
	a.refreshChan = refreshChan
	a.mu.Unlock()
}
```

Add StatusBar wiring:

```go
func (a *TexelTerm) SetRefreshNotifier(refreshChan chan<- bool) {
	a.mu.Lock()
	a.refreshChan = refreshChan
	if a.statusBar != nil {
		a.statusBar.SetRefreshNotifier(refreshChan)
		a.statusBar.Start()
	}
	a.mu.Unlock()
}
```

**Step 5: Stop StatusBar in `Stop()`**

In `Stop()` (line 1737), add before closing the memory buffer:

```go
	// Stop status bar ticker
	if a.statusBar != nil {
		a.statusBar.Stop()
	}
```

**Step 6: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`
Expected: Compiles without errors.

**Step 7: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Add StatusBar and pipeline fields to TexelTerm with lifecycle wiring"
```

---

### Task 5: Modify Resize to account for status bar height

**Files:**
- Modify: `texelation/apps/texelterm/term.go:1695-1735` (Resize method)

**Step 1: Modify `Resize()`**

The status bar takes 2 rows. The VTerm and PTY get `rows - 2`. The StatusBar widget gets positioned at the bottom.

```go
func (a *TexelTerm) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	if a.renderDebugLog != nil {
		a.renderDebugLog("App Resize request: %dx%d", cols, rows)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width = cols
	a.height = rows

	// Reserve 2 rows for status bar (1 separator + 1 content)
	const statusBarHeight = 2
	termRows := rows - statusBarHeight
	if termRows < 1 {
		termRows = 1
	}

	// Calculate terminal width (accounting for scrollbar if visible)
	termWidth := cols
	if a.scrollbar != nil && a.scrollbar.IsVisible() {
		termWidth = cols - ScrollBarWidth
		if termWidth < 1 {
			termWidth = 1
		}
	}

	if a.vterm != nil {
		a.vterm.Resize(termWidth, termRows)
	}

	if a.scrollbar != nil {
		a.scrollbar.Resize(termRows)
	}

	if a.mouseCoordinator != nil {
		a.mouseCoordinator.SetSize(termWidth, termRows)
	}

	if a.historyNavigator != nil {
		a.historyNavigator.Resize(termWidth, termRows)
	}

	// Position status bar at the bottom
	if a.statusBar != nil {
		a.statusBar.SetPosition(0, termRows)
		a.statusBar.Resize(cols, statusBarHeight)
	}

	if a.pty != nil {
		pty.Setsize(a.pty, &pty.Winsize{Rows: uint16(termRows), Cols: uint16(termWidth)})
	}
}
```

**Step 2: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`
Expected: Compiles without errors.

**Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Reserve bottom 2 rows for status bar in Resize"
```

---

### Task 6: Modify Render to draw StatusBar via Painter bridge

**Files:**
- Modify: `texelation/apps/texelterm/term.go:462-548` (Render method)

**Step 1: Modify `Render()`**

The render buffer must now include terminal rows + 2 status bar rows. The StatusBar draws into the bottom 2 rows via a Painter.

Replace the current `Render()` method. Key changes:
- Buffer height is `termRows + statusBarHeight` (= `a.height`)
- VTerm grid only fills `rows 0..termRows-1`
- StatusBar draws into `rows termRows..termRows+1` via Painter
- Update mode indicators before drawing StatusBar

```go
func (a *TexelTerm) Render() [][]texelcore.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.vterm == nil {
		return nil
	}

	vtermGrid := a.vterm.Grid()
	termRows := len(vtermGrid)
	if termRows == 0 {
		return nil
	}
	vtermCols := len(vtermGrid[0])

	// Calculate total output width (terminal + scrollbar if visible)
	totalCols := vtermCols
	scrollbarVisible := a.scrollbar != nil && a.scrollbar.IsVisible()
	if scrollbarVisible {
		totalCols = vtermCols + ScrollBarWidth
	}

	// Total height includes status bar
	const statusBarHeight = 2
	totalRows := termRows + statusBarHeight

	// Resize buffer if needed
	if len(a.buf) != totalRows || (totalRows > 0 && len(a.buf[0]) != totalCols) {
		a.buf = make([][]texelcore.Cell, totalRows)
		for y := range a.buf {
			a.buf[y] = make([]texelcore.Cell, totalCols)
		}
		a.vterm.MarkAllDirty()
	}

	cursorX, cursorY := a.vterm.Cursor()
	cursorVisible := a.vterm.CursorVisible() && a.vterm.AtLiveEdge()
	dirtyLines, allDirty := a.vterm.DirtyLines()

	a.logRenderDebug(vtermGrid, cursorX, cursorY, dirtyLines, allDirty)

	renderLine := func(y int) {
		for x := 0; x < vtermCols; x++ {
			parserCell := vtermGrid[y][x]
			a.buf[y][x] = a.applyParserStyle(parserCell)
			if cursorVisible && x == cursorX && y == cursorY {
				a.buf[y][x].Style = a.buf[y][x].Style.Reverse(true)
			}
		}
	}

	if allDirty {
		for y := 0; y < termRows; y++ {
			renderLine(y)
		}
	} else {
		for y := range dirtyLines {
			if y >= 0 && y < termRows {
				renderLine(y)
			}
		}
	}

	a.vterm.ClearDirty()
	a.applySelectionHighlightLocked(a.buf)

	// Composite scrollbar on the right side (non-overlay)
	if scrollbarVisible {
		scrollbarGrid := a.scrollbar.Render()
		if scrollbarGrid != nil {
			for y := 0; y < termRows && y < len(scrollbarGrid); y++ {
				for x := 0; x < ScrollBarWidth && x < len(scrollbarGrid[y]); x++ {
					a.buf[y][vtermCols+x] = scrollbarGrid[y][x]
				}
			}
		}
	}

	if a.confirmClose {
		a.drawConfirmation(a.buf)
	}

	// Render history navigator overlay (Phase 4 - Disk Layer)
	if a.historyNavigator != nil && a.historyNavigator.IsVisible() {
		a.buf = a.historyNavigator.Render(a.buf)
	}

	// Render status bar via Painter bridge
	if a.statusBar != nil {
		a.updateModeIndicatorsLocked()
		p := texelcore.NewPainter(a.buf, texelcore.Rect{
			X: 0, Y: 0, W: totalCols, H: totalRows,
		})
		a.statusBar.Draw(p)
	}

	return a.buf
}
```

**Step 2: Add `updateModeIndicatorsLocked` helper**

Add after the Render method:

```go
// updateModeIndicatorsLocked rebuilds the status bar mode indicators from current state.
// Must be called with a.mu held.
func (a *TexelTerm) updateModeIndicatorsLocked() {
	if a.statusBar == nil || a.vterm == nil {
		return
	}

	tm := theming.ForApp("texelterm")
	brightFg := tm.GetSemanticColor("text.primary")
	dimFg := tm.GetSemanticColor("text.muted")
	bg := tm.GetSemanticColor("bg.surface")

	if brightFg == tcell.ColorDefault {
		brightFg = tcell.ColorWhite
	}
	if dimFg == tcell.ColorDefault {
		dimFg = tcell.ColorGray
	}
	if bg == tcell.ColorDefault {
		bg = tcell.ColorBlack
	}

	bright := tcell.StyleDefault.Foreground(brightFg).Background(bg)
	dim := tcell.StyleDefault.Foreground(dimFg).Background(bg)
	space := widgets.StyledSegment{Text: " ", Style: dim}

	styleFor := func(enabled bool) tcell.Style {
		if enabled {
			return bright
		}
		return dim
	}

	var segments []widgets.StyledSegment

	// TFM - Transformer pipeline
	tfmEnabled := a.pipeline != nil && a.pipeline.Enabled()
	segments = append(segments, widgets.StyledSegment{Text: "TFM", Style: styleFor(tfmEnabled)})
	segments = append(segments, space)

	// INS/RPL - Insert/Replace mode
	if a.vterm.InsertMode() {
		segments = append(segments, widgets.StyledSegment{Text: "RPL", Style: bright})
	} else {
		segments = append(segments, widgets.StyledSegment{Text: "INS", Style: bright})
	}
	segments = append(segments, space)

	// TUI/NRM - TUI detection
	fwd := a.vterm.FixedWidthDetector()
	if fwd != nil && fwd.IsInTUIMode() {
		segments = append(segments, widgets.StyledSegment{Text: "TUI", Style: bright})
	} else {
		segments = append(segments, widgets.StyledSegment{Text: "NRM", Style: bright})
	}
	segments = append(segments, space)

	// WRP - Wrap enabled
	segments = append(segments, widgets.StyledSegment{Text: "WRP", Style: styleFor(a.vterm.WrapEnabled())})
	segments = append(segments, space)

	// RFL - Reflow enabled
	segments = append(segments, widgets.StyledSegment{Text: "RFL", Style: styleFor(a.vterm.ReflowEnabled())})
	segments = append(segments, space)

	// ALT - Alt screen
	segments = append(segments, widgets.StyledSegment{Text: "ALT", Style: styleFor(a.vterm.InAltScreen())})

	a.statusBar.SetLeftSegments(segments)
}
```

**Note:** Check if `fixedWidthDetector()` is exported. Based on the codebase exploration, it's lowercase (`fixedWidthDetector()`). We may need to either:
- Export it: rename to `FixedWidthDetector()` in vterm_memory_buffer.go, or
- Add a `IsInTUIMode() bool` method directly on VTerm.

Check `vterm_memory_buffer.go:1707` — if `fixedWidthDetector()` is unexported, add a public wrapper:

In `texelation/apps/texelterm/parser/vterm.go` (near the other getters):

```go
// IsInTUIMode returns true if the terminal has detected a TUI application.
func (v *VTerm) IsInTUIMode() bool {
	fwd := v.fixedWidthDetector()
	return fwd != nil && fwd.IsInTUIMode()
}
```

Then in `updateModeIndicatorsLocked`, use `a.vterm.IsInTUIMode()` instead of accessing `fixedWidthDetector()` directly.

**Step 3: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`
Expected: Compiles without errors. Fix any import issues.

**Step 4: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Render StatusBar with mode indicators via Painter bridge"
```

---

### Task 7: Replace `toggleOverlay` with `toggleTransformers`

**Files:**
- Modify: `texelation/apps/texelterm/term.go:846-887`

**Step 1: Replace the Ctrl+T handler and toggle method**

Update the Ctrl+T handler (line 846-850) — already calls `toggleOverlay()`, just change the name:

```go
	// Handle Ctrl+T to toggle transformers + overlay
	if ev.Key() == tcell.KeyCtrlT {
		a.toggleTransformers()
		return
	}
```

Replace `toggleOverlay()` (lines 879-887) with:

```go
// toggleTransformers enables/disables the transformer pipeline and overlay visibility.
func (a *TexelTerm) toggleTransformers() {
	if a.vterm == nil {
		return
	}
	if a.pipeline != nil {
		newState := !a.pipeline.Enabled()
		a.pipeline.SetEnabled(newState)
		a.vterm.SetShowOverlay(newState)
		a.vterm.MarkAllDirty()
		if a.statusBar != nil {
			if newState {
				a.statusBar.ShowMessage("Transformers ON")
			} else {
				a.statusBar.ShowMessage("Transformers OFF")
			}
		}
	} else {
		// No pipeline configured — just toggle overlay visibility
		current := a.vterm.ShowOverlay()
		a.vterm.SetShowOverlay(!current)
		a.vterm.MarkAllDirty()
	}
}
```

**Step 2: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`
Expected: Compiles. No references to old `toggleOverlay` remain.

**Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Replace toggleOverlay with toggleTransformers for Ctrl+T"
```

---

### Task 8: Add VTerm.IsInTUIMode getter (if needed)

**Files:**
- Modify: `texelation/apps/texelterm/parser/vterm.go`

This task is only needed if `fixedWidthDetector()` is unexported (lowercase). Based on exploration, it IS unexported.

**Step 1: Add the method**

In `texelation/apps/texelterm/parser/vterm.go`, near the other getters:

```go
// IsInTUIMode returns true if the terminal has detected a TUI application.
func (v *VTerm) IsInTUIMode() bool {
	fwd := v.fixedWidthDetector()
	return fwd != nil && fwd.IsInTUIMode()
}
```

**Step 2: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`
Expected: Compiles.

**Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/parser/vterm.go
git commit -m "Add IsInTUIMode getter to VTerm"
```

---

### Task 9: End-to-end integration test

**Files:**
- Test: `texelation/apps/texelterm/term_statusbar_test.go` (new)

**Step 1: Write the integration test**

Create `texelation/apps/texelterm/term_statusbar_test.go`:

```go
package texelterm

import (
	"testing"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

func TestStatusBarRendered(t *testing.T) {
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 24)

	// Set up refresh notifier (starts StatusBar ticker)
	ch := make(chan bool, 1)
	app.SetRefreshNotifier(ch)
	defer app.statusBar.Stop()

	// Without VTerm initialized, Render returns nil
	buf := app.Render()
	if buf != nil {
		t.Fatal("expected nil render before VTerm init")
	}
}

func TestStatusBarHeight(t *testing.T) {
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 26)

	// The VTerm should be sized to 80x24 (26 - 2 for status bar)
	// We can't check VTerm directly without Run(), but we can verify
	// the app's stored dimensions
	if app.height != 26 {
		t.Errorf("expected height=26, got %d", app.height)
	}
}

func TestToggleTransformersNoopWithoutPipeline(t *testing.T) {
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 24)

	ch := make(chan bool, 1)
	app.SetRefreshNotifier(ch)
	defer app.statusBar.Stop()

	// Should not panic when pipeline is nil
	app.toggleTransformers()
}

func TestModeIndicatorSegments(t *testing.T) {
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 24)

	ch := make(chan bool, 1)
	app.SetRefreshNotifier(ch)
	defer app.statusBar.Stop()

	// Initialize a minimal VTerm for testing
	app.mu.Lock()
	app.vterm = newTestVTerm(78, 22) // 80-scrollbar, 24-statusbar
	app.mu.Unlock()

	// Build mode indicators
	app.mu.Lock()
	app.updateModeIndicatorsLocked()
	app.mu.Unlock()

	// Render and check that status bar area has content
	buf := app.Render()
	if buf == nil {
		t.Fatal("expected non-nil render")
	}

	// Status bar is in the last 2 rows
	statusRow := len(buf) - 1
	// Check that some cells have non-space content (mode indicators)
	hasContent := false
	for x := 0; x < len(buf[statusRow]); x++ {
		if buf[statusRow][x].Ch != ' ' && buf[statusRow][x].Ch != 0 && buf[statusRow][x].Ch != '─' {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("expected mode indicator content in status bar row")
	}
}
```

Note: `newTestVTerm` may need to be a simple helper. If `parser.NewVTerm` is accessible, use it directly. The test should be adjusted based on what's possible without spawning a real shell.

**Step 2: Run the test**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/ -run "TestStatusBar|TestToggleTransformers|TestModeIndicator" -v`
Expected: All PASS

**Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term_statusbar_test.go
git commit -m "Add integration tests for status bar and transformer toggle"
```

---

### Task 10: Run full test suite and verify

**Step 1: Run texelui tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./... -v`
Expected: All PASS

**Step 2: Run texelation tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./... -v -timeout 120s`
Expected: All PASS (or known flaky tests only)

**Step 3: Run manual smoke test**

Run texelterm standalone and verify:
1. Status bar appears at the bottom with mode indicators
2. `TFM INS NRM WRP RFL ALT` visible (some bright, some dim depending on state)
3. Press Ctrl+T — message "Transformers OFF" appears, TFM indicator dims
4. Press Ctrl+T again — message "Transformers ON" appears, TFM indicator brightens
5. The terminal area is 2 rows shorter than before (status bar takes space)

**Step 4: Final commit (if any fixes needed)**

```bash
git add -A
git commit -m "Fix issues found during smoke testing"
```
