package core

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/theme"
)

// UIManager owns a small widget tree (floating for MVP) and composes to a buffer.
type UIManager struct {
	mu       sync.Mutex
	W, H     int
	widgets  []Widget // z-ordered: later entries draw on top
	bgStyle  tcell.Style
	notifier chan<- bool
	focused  Widget
	buf      [][]texel.Cell
	dirty    []Rect
	lay      Layout
	capture  Widget
}

func NewUIManager() *UIManager {
	tm := theme.Get()
	bg := tm.GetColor("ui", "surface_bg", tcell.ColorBlack)
	fg := tm.GetColor("ui", "surface_fg", tcell.ColorWhite)
	return &UIManager{bgStyle: tcell.StyleDefault.Background(bg).Foreground(fg)}
}

func (u *UIManager) SetRefreshNotifier(ch chan<- bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.notifier = ch
}

func (u *UIManager) RequestRefresh() {
	// notifier is read under lock or we assume atomic pointer/channel safe?
	// Channel send is safe. Reading u.notifier needs lock if it changes.
	u.mu.Lock()
	ch := u.notifier
	u.mu.Unlock()

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
	u.invalidateAllLocked()
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

	// Focus traversal on Tab/Shift-Tab
	if ev.Key() == tcell.KeyTab {
		if ev.Modifiers()&tcell.ModShift != 0 {
			u.focusPrevDeepLocked()
		} else {
			u.focusNextDeepLocked()
		}
		u.invalidateAllLocked()
		return true
	}

	if u.focused != nil && u.focused.HandleKey(ev) {
		// Fallback: if widget didn't mark anything dirty, redraw everything
		if len(u.dirty) == 0 {
			u.invalidateAllLocked()
		} else {
			// Release lock before calling RequestRefresh? 
			// No, RequestRefresh handles its own locking safely (releases before send).
			// Wait, RequestRefresh locks mu internally to read notifier.
			// Recursive lock! UIManager.mu is not recursive.
			// We must unlock before calling RequestRefresh, OR make RequestRefresh assume lock, OR use internal variable.
			// FIX: Call requestRefreshLocked or similar.
			u.requestRefreshLocked()
		}
		return true
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

	// Start capture on press over a widget
	if !prevIsDown && nowDown {
		if w := u.topmostAtLocked(x, y); w != nil {
			u.focusLocked(w)
			u.capture = w
			if mw, ok := w.(MouseAware); ok {
				_ = mw.HandleMouse(ev)
			}
			u.invalidateAllLocked()
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
		u.invalidateAllLocked()
		return true
	}
	// Wheel-only events over topmost
	if buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0 {
		if w := u.topmostAtLocked(x, y); w != nil {
			if mw, ok := w.(MouseAware); ok {
				_ = mw.HandleMouse(ev)
				u.invalidateAllLocked()
				return true
			}
		}
	}
	return false
}

func (u *UIManager) topmostAtLocked(x, y int) Widget {
	for i := len(u.widgets) - 1; i >= 0; i-- {
		if w := deepHit(u.widgets[i], x, y); w != nil {
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

// Deep focus traversal across all widgets in z-order (top-level order, then children).
func (u *UIManager) focusNextDeepLocked() {
	order := u.flattenFocusableLocked()
	if len(order) == 0 {
		return
	}
	cur := -1
	for i, w := range order {
		if w == u.focused {
			cur = i
			break
		}
	}
	next := (cur + 1) % len(order)
	u.focusLocked(order[next])
}

func (u *UIManager) focusPrevDeepLocked() {
	order := u.flattenFocusableLocked()
	if len(order) == 0 {
		return
	}
	cur := -1
	for i, w := range order {
		if w == u.focused {
			cur = i
			break
		}
	}
	prev := cur - 1
	if prev < 0 {
		prev = len(order) - 1
	}
	u.focusLocked(order[prev])
}

func (u *UIManager) flattenFocusableLocked() []Widget {
	var out []Widget
	var visit func(w Widget)
	visit = func(w Widget) {
		if w.Focusable() {
			out = append(out, w)
		}
		if cc, ok := w.(ChildContainer); ok {
			cc.VisitChildren(func(child Widget) { visit(child) })
		}
	}
	for _, w := range u.widgets {
		visit(w)
	}
	return out
}

// Invalidate marks a region for redraw.
// Thread-safe.
func (u *UIManager) Invalidate(r Rect) {
	u.mu.Lock()
	defer u.mu.Unlock()
	
	if r.W <= 0 || r.H <= 0 {
		return
	}
	u.dirty = append(u.dirty, r)
	u.requestRefreshLocked()
}

// InvalidateAll marks the whole surface for redraw.
// Public version.
func (u *UIManager) InvalidateAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.invalidateAllLocked()
}

// Internal helper
func (u *UIManager) invalidateAllLocked() {
	r := Rect{X: 0, Y: 0, W: u.W, H: u.H}
	u.dirty = append(u.dirty, r)
	u.requestRefreshLocked()
}

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

// Render updates dirty regions and returns the framebuffer.
func (u *UIManager) Render() [][]texel.Cell {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.ensureBufferLocked()

	if len(u.dirty) == 0 {
		// No specific dirty regions requested: compose full frame.
		// Note: if this is a fresh render (no dirty), we assume full redraw needed if it was requested?
		// Or just return buffer? 
		// Original logic: if len(dirty) == 0 -> full redraw.
		full := Rect{X: 0, Y: 0, W: u.W, H: u.H}
		p := NewPainter(u.buf, full)
		p.Fill(full, ' ', u.bgStyle)
		for _, w := range u.widgets {
			w.Draw(p)
		}
		return u.buf
	}

	// Copy dirty list to avoid holding it? No, we consume it.
	dirtyCopy := u.dirty
	u.dirty = nil // clear it

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
		for _, w := range u.widgets {
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
