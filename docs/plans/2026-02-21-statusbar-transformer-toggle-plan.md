# Texelterm Status Bar & Transformer Toggle Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a status bar to texelterm with clickable ToggleButton mode indicators, and make Ctrl+T fully disable/enable the transformer pipeline (not just overlay visibility).

**Architecture:** Create a new `ToggleButton` widget in texelui. Extend `StatusBar` with `SetLeftWidgets()` to host child widgets (ToggleButtons) on the left side with mouse forwarding. Bridge the StatusBar into TexelTerm's render pipeline via `core.Painter`. Add runtime enable/disable to the transformer `Pipeline`. Route mouse clicks on the status bar area to the StatusBar widget.

**Tech Stack:** Go, texelui (core.Painter, widgets.StatusBar, widgets.ToggleButton), tcell

---

### Task 1: Create ToggleButton widget

**Files:**
- Create: `texelui/widgets/togglebutton.go`
- Test: `texelui/widgets/togglebutton_test.go`

**Step 1: Write the failing test**

Create `texelui/widgets/togglebutton_test.go`:

```go
package widgets

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelui/core"
)

func TestToggleButtonDraw(t *testing.T) {
	tb := NewToggleButton("TFM")
	tb.SetPosition(0, 0)

	buf := make([][]core.Cell, 1)
	buf[0] = make([]core.Cell, 10)
	p := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 10, H: 1})

	// Inactive: normal style (not reversed)
	tb.Active = false
	tb.Draw(p)

	got := string([]rune{buf[0][0].Ch, buf[0][1].Ch, buf[0][2].Ch})
	if got != "TFM" {
		t.Errorf("expected 'TFM', got %q", got)
	}
	fg, bg, _ := buf[0][0].Style.Decompose()
	if fg == bg {
		t.Error("inactive style should have distinct fg and bg")
	}

	// Active: reversed style
	tb.Active = true
	tb.Draw(p)
	fg2, bg2, _ := buf[0][0].Style.Decompose()
	if fg2 != bg || bg2 != fg {
		t.Errorf("active style should reverse fg/bg: got fg=%v bg=%v, expected fg=%v bg=%v", fg2, bg2, bg, fg)
	}
}

func TestToggleButtonSize(t *testing.T) {
	tb := NewToggleButton("WRP")
	if tb.Rect.W != 3 {
		t.Errorf("expected width=3 for 'WRP', got %d", tb.Rect.W)
	}
	if tb.Rect.H != 1 {
		t.Errorf("expected height=1, got %d", tb.Rect.H)
	}
}

func TestToggleButtonClick(t *testing.T) {
	tb := NewToggleButton("TFM")
	tb.SetPosition(5, 0)

	var toggled bool
	var newState bool
	tb.OnToggle = func(active bool) {
		toggled = true
		newState = active
	}

	// Click inside bounds
	ev := tcell.NewEventMouse(6, 0, tcell.Button1, 0)
	handled := tb.HandleMouse(ev)

	if !handled {
		t.Error("expected click to be handled")
	}
	if !toggled {
		t.Error("expected OnToggle callback to fire")
	}
	if !newState {
		t.Error("expected Active=true after toggle from false")
	}
	if !tb.Active {
		t.Error("expected Active field to be true")
	}
}

func TestToggleButtonClickOutside(t *testing.T) {
	tb := NewToggleButton("TFM")
	tb.SetPosition(5, 0)

	var toggled bool
	tb.OnToggle = func(active bool) { toggled = true }

	// Click outside bounds
	ev := tcell.NewEventMouse(0, 0, tcell.Button1, 0)
	handled := tb.HandleMouse(ev)

	if handled {
		t.Error("expected click outside to not be handled")
	}
	if toggled {
		t.Error("expected OnToggle not to fire for click outside")
	}
}

func TestToggleButtonNoCallback(t *testing.T) {
	tb := NewToggleButton("TFM")
	tb.SetPosition(0, 0)

	// No OnToggle set — should not panic
	ev := tcell.NewEventMouse(1, 0, tcell.Button1, 0)
	tb.HandleMouse(ev)

	// Active should still toggle
	if !tb.Active {
		t.Error("expected Active=true even without OnToggle callback")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run TestToggleButton -v`
Expected: Compilation error — `NewToggleButton` undefined.

**Step 3: Write the implementation**

Create `texelui/widgets/togglebutton.go`:

```go
package widgets

import (
	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/theme"
)

// ToggleButton is a compact clickable indicator that shows on/off state.
// Active = reversed style (FG/BG swapped), Inactive = normal style.
// Designed for status bar mode indicators (e.g., "TFM", "WRP", "INS").
type ToggleButton struct {
	core.BaseWidget
	Label    string
	Active   bool
	OnToggle func(active bool)
	Style    tcell.Style

	inv func(core.Rect)
}

// NewToggleButton creates a toggle button with the given label.
// Width = len(label), height = 1. Not focusable (intended for status bars).
func NewToggleButton(label string) *ToggleButton {
	tb := &ToggleButton{
		Label: label,
	}

	tm := theme.Get()
	fg := tm.GetSemanticColor("text.primary")
	bg := tm.GetSemanticColor("bg.surface")
	if fg == tcell.ColorDefault {
		fg = tcell.ColorWhite
	}
	if bg == tcell.ColorDefault {
		bg = tcell.ColorBlack
	}
	tb.Style = tcell.StyleDefault.Foreground(fg).Background(bg)

	tb.SetPosition(0, 0)
	tb.Resize(len([]rune(label)), 1)
	tb.SetFocusable(false)
	return tb
}

// Draw renders the toggle button.
// Active = reversed style, Inactive = normal style.
func (tb *ToggleButton) Draw(p *core.Painter) {
	style := tb.Style
	if tb.Active {
		fg, bg, attr := style.Decompose()
		style = tcell.StyleDefault.Foreground(bg).Background(fg).Attributes(attr)
	}
	p.DrawText(tb.Rect.X, tb.Rect.Y, tb.Label, style)
}

// HandleMouse handles mouse clicks. Left click toggles Active state.
func (tb *ToggleButton) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !tb.HitTest(x, y) {
		return false
	}
	if ev.Buttons()&tcell.Button1 != 0 {
		tb.Active = !tb.Active
		tb.invalidate()
		if tb.OnToggle != nil {
			tb.OnToggle(tb.Active)
		}
		return true
	}
	return false
}

// SetInvalidator implements core.InvalidationAware.
func (tb *ToggleButton) SetInvalidator(fn func(core.Rect)) {
	tb.inv = fn
}

func (tb *ToggleButton) invalidate() {
	if tb.inv != nil {
		tb.inv(tb.Rect)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run TestToggleButton -v`
Expected: All PASS

**Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelui
git add widgets/togglebutton.go widgets/togglebutton_test.go
git commit -m "Add ToggleButton widget for status bar mode indicators"
```

---

### Task 2: Add `SetLeftWidgets` and `HandleMouse` to StatusBar

**Files:**
- Modify: `texelui/widgets/statusbar.go`
- Test: `texelui/widgets/statusbar_test.go`

**Step 1: Write the failing tests**

Add to `texelui/widgets/statusbar_test.go`:

```go
func TestStatusBarLeftWidgets(t *testing.T) {
	sb := NewStatusBar()
	sb.SetPosition(0, 0)
	sb.Resize(40, 2)

	tb1 := NewToggleButton("TFM")
	tb1.Active = true
	tb2 := NewToggleButton("INS")

	sb.SetLeftWidgets([]core.Widget{tb1, tb2})

	buf := make([][]core.Cell, 2)
	for y := range buf {
		buf[y] = make([]core.Cell, 40)
	}
	p := core.NewPainter(buf, core.Rect{X: 0, Y: 0, W: 40, H: 2})
	sb.Draw(p)

	// Row 1 (content row): toggle buttons should appear
	// tb1 "TFM" at x=1, tb2 "INS" at x=5 (after 1-char gap)
	got := string([]rune{buf[1][1].Ch, buf[1][2].Ch, buf[1][3].Ch})
	if got != "TFM" {
		t.Errorf("expected 'TFM' at x=1, got %q", got)
	}
	got2 := string([]rune{buf[1][5].Ch, buf[1][6].Ch, buf[1][7].Ch})
	if got2 != "INS" {
		t.Errorf("expected 'INS' at x=5, got %q", got2)
	}
}

func TestStatusBarHandleMouse(t *testing.T) {
	sb := NewStatusBar()
	sb.SetPosition(0, 10)
	sb.Resize(40, 2)

	var toggled bool
	tb := NewToggleButton("TFM")
	tb.OnToggle = func(active bool) { toggled = true }
	sb.SetLeftWidgets([]core.Widget{tb})

	// Force layout (normally done during Draw, but we need positions set)
	sb.layoutLeftWidgets()

	// Click on the toggle button area (content row = y=11, x=1..3)
	ev := tcell.NewEventMouse(1, 11, tcell.Button1, 0)
	handled := sb.HandleMouse(ev)

	if !handled {
		t.Error("expected mouse click on toggle button to be handled")
	}
	if !toggled {
		t.Error("expected OnToggle to fire")
	}
}

func TestStatusBarMouseOutside(t *testing.T) {
	sb := NewStatusBar()
	sb.SetPosition(0, 10)
	sb.Resize(40, 2)

	tb := NewToggleButton("TFM")
	sb.SetLeftWidgets([]core.Widget{tb})
	sb.layoutLeftWidgets()

	// Click outside status bar
	ev := tcell.NewEventMouse(0, 5, tcell.Button1, 0)
	handled := sb.HandleMouse(ev)

	if handled {
		t.Error("expected mouse click outside status bar to not be handled")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run "TestStatusBarLeftWidgets|TestStatusBarHandleMouse|TestStatusBarMouseOutside" -v`
Expected: Compilation error — `SetLeftWidgets`, `HandleMouse`, `layoutLeftWidgets` undefined.

**Step 3: Write the implementation**

In `texelui/widgets/statusbar.go`:

1. Add `leftWidgets` field to the struct (after `leftText` on line 42):

```go
	leftWidgets []core.Widget // Child widgets for left side (overrides leftText)
```

2. Add `SetLeftWidgets` method:

```go
// SetLeftWidgets sets widgets to display on the left side of the status bar.
// Widgets are positioned left-to-right with 1-char gaps during Draw.
// Takes priority over KeyHintsProvider hints when set.
// Pass nil to clear and revert to KeyHintsProvider behavior.
func (s *StatusBar) SetLeftWidgets(widgets []core.Widget) {
	s.mu.Lock()
	s.leftWidgets = widgets
	s.mu.Unlock()
	s.invalidate()
}
```

3. Add `layoutLeftWidgets` method that positions widgets sequentially:

```go
// layoutLeftWidgets positions left-side widgets sequentially on the content row.
// Returns the total width consumed.
func (s *StatusBar) layoutLeftWidgets() int {
	if len(s.leftWidgets) == 0 {
		return 0
	}
	contentY := s.Rect.Y + 1
	xx := s.Rect.X + 1 // 1-char left padding
	for i, w := range s.leftWidgets {
		w.SetPosition(xx, contentY)
		xx += w.Bounds().W
		if i < len(s.leftWidgets)-1 {
			xx++ // 1-char gap between widgets
		}
	}
	return xx - s.Rect.X - 1
}
```

Note: `w.Bounds()` should return `core.Rect`. Check if `BaseWidget` has a `Bounds()` method. If not, use the widget's `Rect` field directly — but since `core.Widget` is an interface, we need either a `Bounds()` method or type assertion. Check the `core.Widget` interface. If it doesn't have `Bounds()`, we may need to type-assert to `*ToggleButton` or add `Bounds()` to the interface.

Looking at `core.BaseWidget`, it has a public `Rect` field. But `core.Widget` is an interface. Check if there's a `Bounds()` method:
- `texelui/core/widget.go` — `BaseWidget.Bounds() Rect` should exist or `Rect` is embedded publicly.
- If `Widget` interface doesn't expose bounds, use `interface{ Bounds() core.Rect }` type assertion.

If needed, add to `core.Widget` interface or use a helper. The simplest approach: type-assert each widget to check for a `Bounds()` method, or store bounds info alongside the widget.

**Simpler alternative**: Since we control all left widgets (they're ToggleButtons), store position info during layout and use it in mouse handling:

```go
type leftWidgetEntry struct {
	widget core.Widget
	rect   core.Rect
}
```

But that adds complexity. Let's check if `BaseWidget` exposes `Rect`:

Looking at the code: `BaseWidget` has `Rect core.Rect` as a public field. And widgets embed `BaseWidget`. So if we type-assert to `interface{ Bounds() core.Rect }` or access the Rect via a method... Let me check.

Actually, looking at the existing Checkbox, Button etc., they all embed `core.BaseWidget` which has public `Rect`. But `core.Widget` is an interface and may not expose `Rect`. Let's use a `BoundsProvider` interface or just use `SetPosition` (which exists on Widget interface via BaseWidget).

**Practical approach**: Use the widget's `Rect` directly by type-asserting to `*ToggleButton`, or better, define a small interface:

```go
// boundsWidget is a Widget with accessible bounds for positioning.
type boundsWidget interface {
	core.Widget
	GetBounds() core.Rect
}
```

But this requires ToggleButton to implement `GetBounds()`. Let me check what BaseWidget provides...

The simplest: just keep track of positions ourselves during layout:

```go
func (s *StatusBar) layoutLeftWidgets() int {
	contentY := s.Rect.Y + 1
	xx := s.Rect.X + 1
	for i, w := range s.leftWidgets {
		wb := w.(interface{ GetBounds() core.Rect }).GetBounds()
		w.SetPosition(xx, contentY)
		xx += wb.W
		if i < len(s.leftWidgets)-1 {
			xx++
		}
	}
	return xx - s.Rect.X - 1
}
```

We need to verify `BaseWidget` has `GetBounds()` or equivalent. Check `texelui/core/widget.go`.

4. Modify `Draw` to handle left widgets:

In the `Draw` method, after setting up the content row background, add:

```go
	s.mu.Lock()
	leftWidgets := s.leftWidgets
	if leftWidgets == nil {
		s.updateKeyHintsLocked()
	}
	leftText := s.leftText
	activeMsg := s.getActiveMessage()
	s.mu.Unlock()

	// Draw left content
	leftWidth := 0
	if leftWidgets != nil {
		leftWidth = s.layoutLeftWidgets()
		for _, w := range leftWidgets {
			if drawer, ok := w.(interface{ Draw(*core.Painter) }); ok {
				drawer.Draw(p)
			}
		}
	} else if leftText != "" {
		// ... existing leftText drawing code
		leftWidth = len([]rune(leftText))
	}
```

Use `leftWidth` for right-side message spacing.

5. Add `HandleMouse` method:

```go
// HandleMouse forwards mouse events to left-side widgets.
// Returns true if a widget handled the event.
func (s *StatusBar) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	if !s.Rect.Contains(x, y) {
		return false
	}

	s.mu.Lock()
	widgets := s.leftWidgets
	s.mu.Unlock()

	for _, w := range widgets {
		if mh, ok := w.(interface{ HandleMouse(*tcell.EventMouse) bool }); ok {
			if mh.HandleMouse(ev) {
				s.invalidate()
				return true
			}
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run "TestStatusBarLeftWidgets|TestStatusBarHandleMouse|TestStatusBarMouseOutside" -v`
Expected: All PASS

**Step 5: Run all StatusBar tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./widgets/ -run TestStatusBar -v`
Expected: All PASS

**Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelui
git add widgets/statusbar.go widgets/statusbar_test.go
git commit -m "Add SetLeftWidgets and HandleMouse to StatusBar"
```

---

### Task 3: Add runtime enable/disable to transformer Pipeline

**Files:**
- Modify: `texelation/apps/texelterm/transformer/transformer.go:88-154`
- Test: `texelation/apps/texelterm/transformer/transformer_test.go`

**Step 1: Write the failing tests**

Add to `texelation/apps/texelterm/transformer/transformer_test.go`:

```go
func TestPipelineEnabled(t *testing.T) {
	p := &Pipeline{transformers: []Transformer{&stubTransformer{id: "a"}}}

	if !p.Enabled() {
		t.Error("expected pipeline to be enabled by default")
	}
	p.SetEnabled(false)
	if p.Enabled() {
		t.Error("expected disabled after SetEnabled(false)")
	}
	p.SetEnabled(true)
	if !p.Enabled() {
		t.Error("expected re-enabled after SetEnabled(true)")
	}
}

func TestPipelineDisabledSkipsHandleLine(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}}
	p.SetEnabled(false)

	suppressed := p.HandleLine(1, &parser.LogicalLine{}, false)
	if suppressed {
		t.Error("disabled pipeline should return false")
	}
	if s.handleCalls != 0 {
		t.Errorf("disabled pipeline should skip HandleLine, got %d calls", s.handleCalls)
	}
}

func TestPipelineDisabledSkipsNotify(t *testing.T) {
	s := &stubTransformer{id: "a"}
	p := &Pipeline{transformers: []Transformer{s}}
	p.SetEnabled(false)

	p.NotifyPromptStart()
	p.NotifyCommandStart("ls")

	if s.promptCalls != 0 {
		t.Errorf("disabled pipeline should skip NotifyPromptStart, got %d", s.promptCalls)
	}
	if s.commandCalls != 0 {
		t.Errorf("disabled pipeline should skip NotifyCommandStart, got %d", s.commandCalls)
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

2. Add methods (after `SetPersistNotifyFunc`, before `HandleLine`):

```go
// SetEnabled enables or disables the pipeline at runtime.
func (p *Pipeline) SetEnabled(on bool) { p.enabled = on }

// Enabled returns whether the pipeline is currently enabled.
func (p *Pipeline) Enabled() bool { return p.enabled }
```

3. Add early-return guard to `HandleLine`, `NotifyPromptStart`, `NotifyCommandStart`:

```go
func (p *Pipeline) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) bool {
	if !p.enabled {
		return false
	}
	// ... existing
}

func (p *Pipeline) NotifyPromptStart() {
	if !p.enabled {
		return
	}
	// ... existing
}

func (p *Pipeline) NotifyCommandStart(cmd string) {
	if !p.enabled {
		return
	}
	// ... existing
}
```

4. Set `enabled: true` in `BuildPipeline` return (line 213):

```go
	return &Pipeline{transformers: transformers, enabled: true}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/transformer/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/transformer/transformer.go apps/texelterm/transformer/transformer_test.go
git commit -m "Add runtime enable/disable to transformer Pipeline"
```

---

### Task 4: Add VTerm getter methods

**Files:**
- Modify: `texelation/apps/texelterm/parser/vterm.go:1430-1436`
- Test: `texelation/apps/texelterm/parser/vterm_test.go` (or nearby test file)

**Step 1: Write the failing test**

```go
func TestVTermGetters(t *testing.T) {
	v := NewVTerm(80, 24)
	if v.InsertMode() {
		t.Error("expected InsertMode() false by default")
	}

	v2 := NewVTerm(80, 24, WithWrap(true))
	if !v2.WrapEnabled() {
		t.Error("expected WrapEnabled() true with WithWrap(true)")
	}

	v3 := NewVTerm(80, 24, WithReflow(true))
	if !v3.ReflowEnabled() {
		t.Error("expected ReflowEnabled() true with WithReflow(true)")
	}
}

func TestVTermIsInTUIMode(t *testing.T) {
	v := NewVTerm(80, 24)
	// Default: not in TUI mode
	if v.IsInTUIMode() {
		t.Error("expected IsInTUIMode() false by default")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestVTermGetters|TestVTermIsInTUIMode" -v`
Expected: Compilation error.

**Step 3: Write the implementation**

In `texelation/apps/texelterm/parser/vterm.go`, after the existing getters (after line 1436):

```go
// InsertMode returns true if the terminal is in insert mode (IRM).
func (v *VTerm) InsertMode() bool { return v.insertMode }

// WrapEnabled returns true if line wrapping is enabled for the main screen buffer.
func (v *VTerm) WrapEnabled() bool { return v.wrapEnabled }

// ReflowEnabled returns true if reflow on resize is enabled.
func (v *VTerm) ReflowEnabled() bool { return v.reflowEnabled }

// IsInTUIMode returns true if the terminal has detected a TUI application.
func (v *VTerm) IsInTUIMode() bool {
	fwd := v.fixedWidthDetector()
	return fwd != nil && fwd.IsInTUIMode()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestVTermGetters|TestVTermIsInTUIMode" -v`
Expected: PASS

**Step 5: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/parser/vterm.go apps/texelterm/parser/vterm_test.go
git commit -m "Add InsertMode, WrapEnabled, ReflowEnabled, IsInTUIMode getters to VTerm"
```

---

### Task 5: Integrate StatusBar into TexelTerm (struct + init + lifecycle)

**Files:**
- Modify: `texelation/apps/texelterm/term.go`

**Step 1: Add fields to TexelTerm struct**

After the `scrollbar` field (around line 110):

```go
	// Status bar (reuses texelui StatusBar widget via Painter bridge)
	statusBar *widgets.StatusBar

	// Toggle buttons for status bar mode indicators
	tfmToggle *widgets.ToggleButton
	insToggle *widgets.ToggleButton
	tuiToggle *widgets.ToggleButton
	wrpToggle *widgets.ToggleButton
	rflToggle *widgets.ToggleButton
	altToggle *widgets.ToggleButton

	// Transformer pipeline reference (for runtime toggle)
	pipeline *transformer.Pipeline
```

Add import:

```go
	"github.com/framegrace/texelui/widgets"
```

**Step 2: Create toggle buttons and StatusBar in `New()`**

```go
func New(title, command string) texelcore.App {
	sb := widgets.NewStatusBar()

	tfm := widgets.NewToggleButton("TFM")
	tfm.Active = true // Transformers on by default
	ins := widgets.NewToggleButton("INS")
	ins.Active = true // Insert mode is the default
	tui := widgets.NewToggleButton("NRM")
	tui.Active = true
	wrp := widgets.NewToggleButton("WRP")
	rfl := widgets.NewToggleButton("RFL")
	alt := widgets.NewToggleButton("ALT")

	sb.SetLeftWidgets([]texelcore.Widget{tfm, ins, tui, wrp, rfl, alt})

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
		statusBar:    sb,
		tfmToggle:    tfm,
		insToggle:    ins,
		tuiToggle:    tui,
		wrpToggle:    wrp,
		rflToggle:    rfl,
		altToggle:    alt,
	}

	return term
}
```

**Note:** OnToggle callbacks are wired later in `initializeVTermFirstRun` when the VTerm and pipeline exist. For read-only indicators (INS, TUI, ALT), no OnToggle is set.

**Step 3: Store pipeline reference in `initializeVTermFirstRun`**

At line 1417, store the pipeline and wire the TFM toggle callback:

```go
	pipeline := transformer.BuildPipeline(cfg)
	a.pipeline = pipeline
	if pipeline != nil {
		a.vterm.OnLineCommit = pipeline.HandleLine
		a.vterm.OnPromptStart = pipeline.NotifyPromptStart
		pipeline.SetInsertFunc(a.vterm.RequestLineInsert)
		pipeline.SetOverlayFunc(a.vterm.RequestLineOverlay)
		pipeline.SetPersistNotifyFunc(a.vterm.NotifyLinePersist)

		origHandler := a.vterm.OnCommandStart
		a.vterm.OnCommandStart = func(cmd string) {
			if origHandler != nil {
				origHandler(cmd)
			}
			pipeline.NotifyCommandStart(cmd)
		}
	}

	// Wire toggle button callbacks (requires VTerm + pipeline to be set up)
	a.tfmToggle.OnToggle = func(active bool) {
		a.toggleTransformers()
	}
```

**Step 4: Wire StatusBar refresh notifier and lifecycle**

In `SetRefreshNotifier`:

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

In `Stop()`, before closing memory buffer:

```go
	if a.statusBar != nil {
		a.statusBar.Stop()
	}
```

**Step 5: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`
Expected: Compiles.

**Step 6: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Add StatusBar, ToggleButtons, and pipeline fields to TexelTerm"
```

---

### Task 6: Modify Resize to account for status bar height

**Files:**
- Modify: `texelation/apps/texelterm/term.go:1695-1735`

**Step 1: Modify `Resize()`**

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

**Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Reserve bottom 2 rows for status bar in Resize"
```

---

### Task 7: Modify Render to draw StatusBar via Painter bridge + mode indicator updates

**Files:**
- Modify: `texelation/apps/texelterm/term.go:462-548` (Render), add `updateModeIndicatorsLocked`

**Step 1: Modify `Render()`**

Key changes from the original:
- Buffer height = `termRows + statusBarHeight`
- VTerm grid fills rows `0..termRows-1`
- StatusBar draws into bottom 2 rows via Painter
- `updateModeIndicatorsLocked()` updates toggle button Active states before draw

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

	totalCols := vtermCols
	scrollbarVisible := a.scrollbar != nil && a.scrollbar.IsVisible()
	if scrollbarVisible {
		totalCols = vtermCols + ScrollBarWidth
	}

	const statusBarHeight = 2
	totalRows := termRows + statusBarHeight

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

**Step 2: Add `updateModeIndicatorsLocked`**

This updates the Active state of each ToggleButton from current VTerm state:

```go
// updateModeIndicatorsLocked updates toggle button states from current terminal state.
// Must be called with a.mu held.
func (a *TexelTerm) updateModeIndicatorsLocked() {
	if a.vterm == nil {
		return
	}

	// TFM - transformer pipeline
	a.tfmToggle.Active = a.pipeline != nil && a.pipeline.Enabled()

	// INS/RPL - insert/replace mode
	if a.vterm.InsertMode() {
		a.insToggle.Label = "RPL"
		a.insToggle.Active = true
	} else {
		a.insToggle.Label = "INS"
		a.insToggle.Active = true
	}

	// TUI/NRM - TUI detection
	if a.vterm.IsInTUIMode() {
		a.tuiToggle.Label = "TUI"
		a.tuiToggle.Active = true
	} else {
		a.tuiToggle.Label = "NRM"
		a.tuiToggle.Active = true
	}

	// WRP - wrap
	a.wrpToggle.Active = a.vterm.WrapEnabled()

	// RFL - reflow
	a.rflToggle.Active = a.vterm.ReflowEnabled()

	// ALT - alt screen
	a.altToggle.Active = a.vterm.InAltScreen()
}
```

**Step 3: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`

**Step 4: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Render StatusBar with ToggleButton mode indicators via Painter bridge"
```

---

### Task 8: Replace `toggleOverlay` with `toggleTransformers` + mouse routing

**Files:**
- Modify: `texelation/apps/texelterm/term.go:846-887` (Ctrl+T handler), `:955-991` (HandleMouse)

**Step 1: Replace toggle method**

Replace `toggleOverlay()` (lines 879-887):

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
		a.tfmToggle.Active = newState
		if a.statusBar != nil {
			if newState {
				a.statusBar.ShowMessage("Transformers ON")
			} else {
				a.statusBar.ShowMessage("Transformers OFF")
			}
		}
	} else {
		// No pipeline — toggle overlay visibility only
		current := a.vterm.ShowOverlay()
		a.vterm.SetShowOverlay(!current)
		a.vterm.MarkAllDirty()
	}
}
```

Update Ctrl+T handler (line 846-850):

```go
	// Handle Ctrl+T to toggle transformers + overlay
	if ev.Key() == tcell.KeyCtrlT {
		a.toggleTransformers()
		return
	}
```

**Step 2: Add mouse routing for status bar**

In `HandleMouse()` (line 955), add status bar check before mouse coordinator:

```go
func (a *TexelTerm) HandleMouse(ev *tcell.EventMouse) {
	if ev == nil {
		return
	}

	// Check if history navigator is visible and wants the event
	if a.historyNavigator != nil && a.historyNavigator.IsVisible() {
		if a.historyNavigator.HandleMouse(ev) {
			a.requestRefresh()
			return
		}
	}

	x, y := ev.Position()
	buttons := ev.Buttons()

	// Check if click is on the scrollbar
	if a.scrollbar != nil && a.scrollbar.IsVisible() {
		scrollbarX := a.width - ScrollBarWidth
		if x >= scrollbarX {
			if buttons&tcell.Button1 != 0 {
				localX := x - scrollbarX
				if targetOffset, ok := a.scrollbar.HandleClick(localX, y); ok {
					a.scrollToOffsetWithResultSelection(targetOffset)
					return
				}
			}
			return
		}
	}

	// Check if click is on the status bar
	if a.statusBar != nil {
		const statusBarHeight = 2
		termRows := a.height - statusBarHeight
		if y >= termRows {
			if a.statusBar.HandleMouse(ev) {
				a.requestRefresh()
			}
			return
		}
	}

	// Delegate to mouse coordinator for terminal content
	if a.mouseCoordinator != nil {
		a.mouseCoordinator.HandleMouse(ev)
	}
	a.requestRefresh()
}
```

**Step 3: Verify compilation**

Run: `cd /home/marc/projects/texel/texelation && go build ./apps/texelterm/`

**Step 4: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term.go
git commit -m "Replace toggleOverlay with toggleTransformers, add status bar mouse routing"
```

---

### Task 9: Integration tests

**Files:**
- Create: `texelation/apps/texelterm/term_statusbar_test.go`

**Step 1: Write tests**

```go
package texelterm

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestStatusBarRenderOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 26)

	ch := make(chan bool, 1)
	app.SetRefreshNotifier(ch)
	defer app.statusBar.Stop()

	// Initialize VTerm manually for testing
	app.mu.Lock()
	app.vterm = parser.NewVTerm(80, 24, parser.WithWrap(true), parser.WithReflow(true))
	app.mu.Unlock()

	buf := app.Render()
	if buf == nil {
		t.Fatal("expected non-nil render")
	}

	// Buffer should be 26 rows (24 terminal + 2 status bar)
	if len(buf) != 26 {
		t.Errorf("expected 26 rows, got %d", len(buf))
	}

	// Separator row (row 24) should have '─' characters
	if buf[24][0].Ch != '─' {
		t.Errorf("expected separator '─' at row 24, got %q", buf[24][0].Ch)
	}

	// Content row (row 25) should have toggle button text
	// First toggle is "TFM" at x=1
	got := string([]rune{buf[25][1].Ch, buf[25][2].Ch, buf[25][3].Ch})
	if got != "TFM" {
		t.Errorf("expected 'TFM' at row 25 x=1, got %q", got)
	}
}

func TestToggleTransformersNoPipeline(t *testing.T) {
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 24)

	ch := make(chan bool, 1)
	app.SetRefreshNotifier(ch)
	defer app.statusBar.Stop()

	// Should not panic when pipeline is nil
	app.toggleTransformers()
}

func TestToggleTransformersWithPipeline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	app := New("test", "/bin/sh").(*TexelTerm)
	app.Resize(80, 26)

	ch := make(chan bool, 1)
	app.SetRefreshNotifier(ch)
	defer app.statusBar.Stop()

	// Set up VTerm and a mock pipeline
	app.mu.Lock()
	app.vterm = parser.NewVTerm(80, 24)
	// We can't easily build a real pipeline without config, so test the toggle button directly
	app.mu.Unlock()

	// Without pipeline, tfmToggle reflects nil pipeline state
	app.mu.Lock()
	app.updateModeIndicatorsLocked()
	app.mu.Unlock()

	if app.tfmToggle.Active {
		t.Error("expected TFM inactive when pipeline is nil")
	}
}
```

**Step 2: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/ -run "TestStatusBarRender|TestToggleTransformers" -v`
Expected: All PASS

**Step 3: Commit**

```bash
cd /home/marc/projects/texel/texelation
git add apps/texelterm/term_statusbar_test.go
git commit -m "Add integration tests for status bar and transformer toggle"
```

---

### Task 10: Full test suite + smoke test

**Step 1: Run texelui tests**

Run: `cd /home/marc/projects/texel/texelui && go test ./... -v`
Expected: All PASS

**Step 2: Run texelation tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./... -v -timeout 120s`
Expected: All PASS

**Step 3: Manual smoke test**

Run texelterm standalone and verify:
1. Status bar at bottom with toggle buttons: `TFM INS NRM WRP RFL ALT`
2. Active toggles appear reversed (inverted FG/BG)
3. Click on `TFM` — toggles transformers, message "Transformers OFF/ON" appears
4. Press Ctrl+T — same effect as clicking TFM
5. Terminal content area is 2 rows shorter
6. `TUI`/`NRM` and `INS`/`RPL` change when running TUI apps or setting insert mode
7. `ALT` lights up when entering alt screen (e.g., `vim`)
