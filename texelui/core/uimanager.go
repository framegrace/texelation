package core

import (
	"sort"
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/theme"
)

// UIManager owns a small widget tree (floating for MVP) and composes to a buffer.
type UIManager struct {
	mu       sync.Mutex // protects widgets, layout, focus, capture, buffer
	dirtyMu  sync.Mutex // protects dirty list and notifier
	W, H     int
	widgets  []Widget // z-ordered: later entries draw on top
	bgStyle  tcell.Style
	notifier chan<- bool
	focused  Widget
	buf      [][]texel.Cell
	dirty    []Rect
	lay      Layout
	capture  Widget

	// AdvanceFocusOnEnter controls whether pressing Enter in a widget
	// automatically advances focus to the next widget. Enabled by default.
	// Useful for form-style data entry.
	AdvanceFocusOnEnter bool
}

func NewUIManager() *UIManager {
	tm := theme.Get()
	bg := tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
	fg := tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
	return &UIManager{
		bgStyle:             tcell.StyleDefault.Background(bg).Foreground(fg),
		AdvanceFocusOnEnter: true, // Enable by default for form-style data entry
	}
}

func (u *UIManager) SetRefreshNotifier(ch chan<- bool) {
	u.dirtyMu.Lock()
	defer u.dirtyMu.Unlock()
	u.notifier = ch
}

func (u *UIManager) RequestRefresh() {
	u.dirtyMu.Lock()
	ch := u.notifier
	u.dirtyMu.Unlock()

	if ch == nil {
		return
	}
	select {
	case ch <- true:
	default:
	}
}

// BlinkTick calls BlinkTick on all widgets that implement BlinkAware,
// allowing them to update internal blink state and invalidate caret regions.
// BlinkTick was used for caret blinking; deprecated and no-op.

func (u *UIManager) Resize(w, h int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.dirtyMu.Lock()
	defer u.dirtyMu.Unlock()

	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	u.W, u.H = w, h
	// Resize framebuffer and invalidate all
	u.buf = nil
	u.invalidateAllLocked()
}

func (u *UIManager) AddWidget(w Widget) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.widgets = append(u.widgets, w)
	u.propagateInvalidator(w)
	// Ensure a first full draw after adding widgets
	u.dirtyMu.Lock()
	u.invalidateAllLocked()
	u.dirtyMu.Unlock()
}

func (u *UIManager) propagateInvalidator(w Widget) {
	if ia, ok := w.(InvalidationAware); ok {
		ia.SetInvalidator(u.Invalidate)
	}
	if cc, ok := w.(ChildContainer); ok {
		cc.VisitChildren(func(child Widget) { u.propagateInvalidator(child) })
	}
}

// SetLayout sets the layout manager (defaults to Absolute).
func (u *UIManager) SetLayout(l Layout) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lay = l
}

func (u *UIManager) Focus(w Widget) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.focusLocked(w)
}

func (u *UIManager) focusLocked(w Widget) {
	if w == nil || !w.Focusable() {
		return
	}
	if u.focused == w {
		return
	}
	if u.focused != nil {
		u.focused.Blur()
	}
	u.focused = w
	if u.focused != nil {
		u.focused.Focus()
	}
}

func (u *UIManager) HandleKey(ev *tcell.EventKey) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Check if focused widget is modal - if so, it gets ALL input (including Tab)
	if u.focused != nil {
		if modal, ok := u.focused.(Modal); ok && modal.IsModal() {
			if u.focused.HandleKey(ev) {
				u.dirtyMu.Lock()
				if len(u.dirty) == 0 {
					u.invalidateAllLocked()
				} else {
					u.requestRefreshLocked()
				}
				u.dirtyMu.Unlock()
				return true
			}
			// Modal widget didn't handle it, but we still don't do focus traversal
			return false
		}
	}

	// Let focused widget handle the key first
	if u.focused != nil && u.focused.HandleKey(ev) {
		// Widget handled it
		u.dirtyMu.Lock()
		if len(u.dirty) == 0 {
			u.invalidateAllLocked()
		} else {
			u.requestRefreshLocked()
		}
		u.dirtyMu.Unlock()

		// Advance focus after Enter for form-style data entry (if container supports it)
		if u.AdvanceFocusOnEnter && ev.Key() == tcell.KeyEnter {
			if fc, ok := u.focused.(FocusCycler); ok {
				fc.CycleFocus(true)
				u.dirtyMu.Lock()
				u.invalidateAllLocked()
				u.dirtyMu.Unlock()
			}
		}

		return true
	}

	// Tab/Shift-Tab: delegate to root container's focus cycling
	if ev.Key() == tcell.KeyTab || ev.Key() == tcell.KeyBacktab {
		forward := ev.Key() == tcell.KeyTab && ev.Modifiers()&tcell.ModShift == 0
		// Find the root container that should handle focus cycling
		if u.cycleFocusLocked(forward) {
			u.dirtyMu.Lock()
			u.invalidateAllLocked()
			u.dirtyMu.Unlock()
			return true
		}
	}

	return false
}

// cycleFocusLocked finds the appropriate FocusCycler and cycles focus.
// It walks up the widget hierarchy to find a container that can cycle focus.
func (u *UIManager) cycleFocusLocked(forward bool) bool {
	// Try focused widget first if it's a FocusCycler
	if fc, ok := u.focused.(FocusCycler); ok {
		if fc.CycleFocus(forward) {
			return true
		}
	}

	// Try to find a parent container that can handle focus cycling
	for _, w := range u.widgets {
		if fc, ok := w.(FocusCycler); ok {
			// Check if this container contains the focused widget
			if u.containsWidgetLocked(w, u.focused) {
				if fc.CycleFocus(forward) {
					return true
				}
			}
		}
	}

	// No container handled it - try cycling among root widgets
	return u.cycleRootWidgetsLocked(forward)
}

// containsWidgetLocked checks if container w contains widget target.
func (u *UIManager) containsWidgetLocked(w, target Widget) bool {
	if w == target {
		return true
	}
	if cc, ok := w.(ChildContainer); ok {
		found := false
		cc.VisitChildren(func(child Widget) {
			if found {
				return
			}
			if u.containsWidgetLocked(child, target) {
				found = true
			}
		})
		return found
	}
	return false
}

// cycleRootWidgetsLocked cycles focus among root-level widgets.
func (u *UIManager) cycleRootWidgetsLocked(forward bool) bool {
	if len(u.widgets) == 0 {
		return false
	}

	// Find current root widget index
	currentIdx := -1
	for i, w := range u.widgets {
		if u.containsWidgetLocked(w, u.focused) {
			currentIdx = i
			break
		}
	}

	// Find next focusable root widget
	n := len(u.widgets)
	for offset := 1; offset <= n; offset++ {
		var idx int
		if forward {
			idx = (currentIdx + offset) % n
		} else {
			idx = (currentIdx - offset + n) % n
		}
		w := u.widgets[idx]
		if w.Focusable() {
			u.focusLocked(w)
			return true
		}
	}

	return false
}

// HandleMouse routes mouse events for click-to-focus and optional capture drags.
func (u *UIManager) HandleMouse(ev *tcell.EventMouse) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	x, y := ev.Position()
	buttons := ev.Buttons()
	prevIsDown := u.capture != nil
	nowDown := buttons&tcell.Button1 != 0

	// Check if focused widget is modal - dismiss on click outside
	if u.focused != nil && nowDown && !prevIsDown {
		if modal, ok := u.focused.(Modal); ok && modal.IsModal() {
			// Check if click is outside the modal widget
			if !u.focused.HitTest(x, y) {
				modal.DismissModal()
				u.dirtyMu.Lock()
				u.invalidateAllLocked()
				u.dirtyMu.Unlock()
				return true
			}
		}
	}

	// Start capture on press over a widget
	if !prevIsDown && nowDown {
		if w := u.topmostAtLocked(x, y); w != nil {
			u.focusLocked(w)
			u.capture = w
			if mw, ok := w.(MouseAware); ok {
				_ = mw.HandleMouse(ev)
			}
			u.dirtyMu.Lock()
			u.invalidateAllLocked()
			u.dirtyMu.Unlock()
			return true
		}
		return false
	}

	// While captured, forward all mouse events
	if u.capture != nil {
		if mw, ok := u.capture.(MouseAware); ok {
			_ = mw.HandleMouse(ev)
		}
		// Release on button up
		if prevIsDown && !nowDown {
			u.capture = nil
		}
		u.dirtyMu.Lock()
		u.invalidateAllLocked()
		u.dirtyMu.Unlock()
		return true
	}
	// Wheel-only events over topmost
	if buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0 {
		if w := u.topmostAtLocked(x, y); w != nil {
			if mw, ok := w.(MouseAware); ok {
				_ = mw.HandleMouse(ev)
				u.dirtyMu.Lock()
				u.invalidateAllLocked()
				u.dirtyMu.Unlock()
				return true
			}
		}
	}

	// Mouse move events (no buttons pressed) - forward to widget under cursor for hover tracking
	if buttons == tcell.ButtonNone {
		if w := u.topmostAtLocked(x, y); w != nil {
			if mw, ok := w.(MouseAware); ok {
				if mw.HandleMouse(ev) {
					u.dirtyMu.Lock()
					u.requestRefreshLocked()
					u.dirtyMu.Unlock()
					return true
				}
			}
		}
	}

	return false
}

func (u *UIManager) topmostAtLocked(x, y int) Widget {
	// Get widgets sorted by z-index, then iterate in reverse to find topmost
	sorted := u.sortedWidgetsLocked()
	for i := len(sorted) - 1; i >= 0; i-- {
		if w := deepHit(sorted[i], x, y); w != nil {
			return w
		}
	}
	return nil
}

func deepHit(w Widget, x, y int) Widget {
	if ht, ok := w.(HitTester); ok {
		if dw := ht.WidgetAt(x, y); dw != nil {
			return dw
		}
	}
	if w.HitTest(x, y) {
		return w
	}
	if cc, ok := w.(ChildContainer); ok {
		var res Widget
		cc.VisitChildren(func(child Widget) {
			if res != nil {
				return
			}
			if dw := deepHit(child, x, y); dw != nil {
				res = dw
			}
		})
		return res
	}
	return nil
}


// Invalidate marks a region for redraw.
// Thread-safe.
func (u *UIManager) Invalidate(r Rect) {
	u.dirtyMu.Lock()
	defer u.dirtyMu.Unlock()

	if r.W <= 0 || r.H <= 0 {
		return
	}
	u.dirty = append(u.dirty, r)
	u.requestRefreshLocked()
}

// InvalidateAll marks the whole surface for redraw.
// Public version.
func (u *UIManager) InvalidateAll() {
	u.dirtyMu.Lock()
	defer u.dirtyMu.Unlock()
	u.invalidateAllLocked()
}

// Internal helper - assumes dirtyMu is held
func (u *UIManager) invalidateAllLocked() {
	r := Rect{X: 0, Y: 0, W: u.W, H: u.H}
	u.dirty = append(u.dirty, r)
	u.requestRefreshLocked()
}

// Internal helper - assumes dirtyMu is held
func (u *UIManager) requestRefreshLocked() {
	if u.notifier == nil {
		return
	}
	select {
	case u.notifier <- true:
	default:
	}
}

func (u *UIManager) ensureBufferLocked() {
	// Capture W and H to avoid race if they change during loop (though we hold lock now)
	h := u.H
	w := u.W
	if u.buf != nil && len(u.buf) == h && (h == 0 || len(u.buf[0]) == w) {
		return
	}
	u.buf = make([][]texel.Cell, h)
	for y := 0; y < h; y++ {
		row := make([]texel.Cell, w)
		for x := 0; x < w; x++ {
			row[x] = texel.Cell{Ch: ' ', Style: u.bgStyle}
		}
		u.buf[y] = row
	}
}

// getZIndex returns the z-index of a widget (0 if not a ZIndexer).
func getZIndex(w Widget) int {
	if zi, ok := w.(ZIndexer); ok {
		return zi.ZIndex()
	}
	return 0
}

// sortedWidgetsLocked returns a copy of widgets sorted by z-index (stable sort).
func (u *UIManager) sortedWidgetsLocked() []Widget {
	sorted := make([]Widget, len(u.widgets))
	copy(sorted, u.widgets)
	sort.SliceStable(sorted, func(i, j int) bool {
		return getZIndex(sorted[i]) < getZIndex(sorted[j])
	})
	return sorted
}

// Render updates dirty regions and returns the framebuffer.
func (u *UIManager) Render() [][]texel.Cell {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.ensureBufferLocked()

	u.dirtyMu.Lock()
	// Copy dirty list to avoid holding it? No, we consume it.
	dirtyCopy := u.dirty
	u.dirty = nil // clear it
	u.dirtyMu.Unlock()

	// Get widgets sorted by z-index for correct draw order
	sorted := u.sortedWidgetsLocked()

	if len(dirtyCopy) == 0 {
		// No specific dirty regions requested: compose full frame.
		full := Rect{X: 0, Y: 0, W: u.W, H: u.H}
		p := NewPainter(u.buf, full)
		p.Fill(full, ' ', u.bgStyle)
		for _, w := range sorted {
			w.Draw(p)
		}
		return u.buf
	}

	// Merge dirty rects to reduce redraw area, but keep multiple clips
	merged := mergeRects(dirtyCopy)
	for _, clip := range merged {
		// Clip against surface bounds
		if clip.X < 0 {
			clip.W += clip.X
			clip.X = 0
		}
		if clip.Y < 0 {
			clip.H += clip.Y
			clip.Y = 0
		}
		if clip.X+clip.W > u.W {
			clip.W = u.W - clip.X
		}
		if clip.Y+clip.H > u.H {
			clip.H = u.H - clip.Y
		}
		if clip.W <= 0 || clip.H <= 0 {
			continue
		}

		p := NewPainter(u.buf, clip)
		// Clear dirty region
		p.Fill(clip, ' ', u.bgStyle)
		// Draw widgets intersecting clip
		for _, w := range sorted {
			wx, wy := w.Position()
			ww, wh := w.Size()
			wr := Rect{X: wx, Y: wy, W: ww, H: wh}
			if rectsOverlap(wr, clip) {
				w.Draw(p)
			}
		}
	}
	return u.buf
}

func rectsOverlap(a, b Rect) bool {
	if a.W <= 0 || a.H <= 0 || b.W <= 0 || b.H <= 0 {
		return false
	}
	ax1 := a.X + a.W
	ay1 := a.Y + a.H
	bx1 := b.X + b.W
	by1 := b.Y + b.H
	return a.X < bx1 && ax1 > b.X && a.Y < by1 && ay1 > b.Y
}

// mergeRects unions overlapping or edge-adjacent rectangles into a compact set.
func mergeRects(in []Rect) []Rect {
	out := make([]Rect, 0, len(in))
	// Copy and normalize (remove zero-sized)
	for _, r := range in {
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		out = append(out, r)
	}
	// Iteratively merge until stable
	changed := true
	for changed {
		changed = false
		for i := 0; i < len(out) && !changed; i++ {
			for j := i + 1; j < len(out) && !changed; j++ {
				if rectsTouchOrOverlap(out[i], out[j]) {
					out[i] = union(out[i], out[j])
					out = append(out[:j], out[j+1:]...)
					changed = true
				}
			}
		}
	}
	return out
}

func rectsTouchOrOverlap(a, b Rect) bool {
	// Overlap
	if rectsOverlap(a, b) {
		return true
	}
	// Edge adjacency (share edge or corner)
	ax1 := a.X + a.W
	ay1 := a.Y + a.H
	bx1 := b.X + b.W
	by1 := b.Y + b.H
	horizontallyAdjacent := (ax1 == b.X || bx1 == a.X) && !(a.Y >= by1 || ay1 <= b.Y)
	verticallyAdjacent := (ay1 == b.Y || by1 == a.Y) && !(a.X >= bx1 || ax1 <= b.X)
	// Corner adjacency allowed to merge into larger block
	cornerAdjacent := (ax1 == b.X || bx1 == a.X) && (ay1 == b.Y || by1 == a.Y)
	return horizontallyAdjacent || verticallyAdjacent || cornerAdjacent
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func union(a, b Rect) Rect {
	x0 := min(a.X, b.X)
	y0 := min(a.Y, b.Y)
	x1 := max(a.X+a.W, b.X+b.W)
	y1 := max(a.Y+a.H, b.Y+b.H)
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}
