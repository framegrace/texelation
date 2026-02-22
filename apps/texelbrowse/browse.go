// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/browse.go
// Summary: Semantic terminal browser that drives Chromium via CDP
//          and renders web content through the Accessibility Tree.

package texelbrowse

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// ContentPanel is a vertically scrolling container that holds the
// rendered page widgets. It implements Widget, ChildContainer, and
// FocusCycler so that the UIManager can discover children and cycle
// focus among them with Tab/Shift-Tab.
type ContentPanel struct {
	core.BaseWidget
	mu         sync.Mutex
	children   []core.Widget
	scrollY    int
	focusIndex int
	inv        func(core.Rect)
}

// newContentPanel creates an empty content panel.
func newContentPanel() *ContentPanel {
	cp := &ContentPanel{
		focusIndex: -1,
	}
	cp.SetFocusable(true)
	return cp
}

// SetChildren replaces all children. Focus and scroll position are reset.
func (cp *ContentPanel) SetChildren(ws []core.Widget) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.children = ws
	cp.scrollY = 0
	cp.focusIndex = -1
	cp.invalidateLocked()
}

// SetInvalidator implements core.InvalidationAware.
func (cp *ContentPanel) SetInvalidator(fn func(core.Rect)) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.inv = fn
	for _, w := range cp.children {
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// Draw renders visible children offset by scrollY.
func (cp *ContentPanel) Draw(p *core.Painter) {
	cp.mu.Lock()
	children := make([]core.Widget, len(cp.children))
	copy(children, cp.children)
	scrollY := cp.scrollY
	cp.mu.Unlock()

	_, panelH := cp.Size()

	for _, w := range children {
		_, wy := w.Position()
		_, wh := w.Size()

		// Apply scroll offset: widget's visual Y = original Y - scrollY + panel Y
		visY := wy - scrollY + cp.Rect.Y
		if visY+wh <= cp.Rect.Y || visY >= cp.Rect.Y+panelH {
			continue // off-screen
		}
		// Temporarily adjust position for drawing
		origX, origY := w.Position()
		w.SetPosition(origX, visY)
		w.Draw(p)
		w.SetPosition(origX, origY)
	}
}

// HandleKey processes keys: Tab/Shift-Tab cycle focus, Page Up/Down
// scroll the viewport.
func (cp *ContentPanel) HandleKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyPgUp:
		cp.mu.Lock()
		_, h := cp.Size()
		cp.scrollY -= h
		if cp.scrollY < 0 {
			cp.scrollY = 0
		}
		cp.mu.Unlock()
		cp.invalidate()
		return true

	case tcell.KeyPgDn:
		cp.mu.Lock()
		_, h := cp.Size()
		cp.scrollY += h
		maxScroll := cp.contentHeight() - h
		if maxScroll < 0 {
			maxScroll = 0
		}
		if cp.scrollY > maxScroll {
			cp.scrollY = maxScroll
		}
		cp.mu.Unlock()
		cp.invalidate()
		return true
	}

	// Delegate to focused child
	cp.mu.Lock()
	idx := cp.focusIndex
	focusables := cp.focusableChildren()
	cp.mu.Unlock()

	if idx >= 0 && idx < len(focusables) {
		return focusables[idx].HandleKey(ev)
	}
	return false
}

// Focus focuses the panel and restores focus to the previously focused child.
func (cp *ContentPanel) Focus() {
	cp.BaseWidget.Focus()
	cp.mu.Lock()
	defer cp.mu.Unlock()
	focusables := cp.focusableChildren()
	if len(focusables) == 0 {
		return
	}
	if cp.focusIndex < 0 || cp.focusIndex >= len(focusables) {
		cp.focusIndex = 0
	}
	focusables[cp.focusIndex].Focus()
}

// Blur blurs all children and the panel.
func (cp *ContentPanel) Blur() {
	cp.mu.Lock()
	for _, w := range cp.children {
		w.Blur()
	}
	cp.mu.Unlock()
	cp.BaseWidget.Blur()
}

// CycleFocus implements core.FocusCycler.
func (cp *ContentPanel) CycleFocus(forward bool) bool {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	focusables := cp.focusableChildren()
	if len(focusables) == 0 {
		return false
	}

	// Find currently focused
	currentIdx := -1
	for i, w := range focusables {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			currentIdx = i
			break
		}
	}

	if currentIdx < 0 {
		// Nothing focused, focus first or last
		if forward {
			focusables[0].Focus()
			cp.focusIndex = 0
		} else {
			focusables[len(focusables)-1].Focus()
			cp.focusIndex = len(focusables) - 1
		}
		cp.ensureVisible(cp.focusIndex)
		cp.invalidateLocked()
		return true
	}

	var nextIdx int
	if forward {
		nextIdx = currentIdx + 1
		if nextIdx >= len(focusables) {
			return false // at boundary, let parent handle
		}
	} else {
		nextIdx = currentIdx - 1
		if nextIdx < 0 {
			return false // at boundary
		}
	}

	focusables[currentIdx].Blur()
	focusables[nextIdx].Focus()
	cp.focusIndex = nextIdx
	cp.ensureVisible(nextIdx)
	cp.invalidateLocked()
	return true
}

// TrapsFocus implements core.FocusCycler. Returns false so that
// focus can escape back to the URL bar.
func (cp *ContentPanel) TrapsFocus() bool {
	return false
}

// VisitChildren implements core.ChildContainer.
func (cp *ContentPanel) VisitChildren(fn func(core.Widget)) {
	cp.mu.Lock()
	children := make([]core.Widget, len(cp.children))
	copy(children, cp.children)
	cp.mu.Unlock()
	for _, w := range children {
		fn(w)
	}
}

// focusableChildren returns all focusable children in order.
// Must be called with cp.mu held.
func (cp *ContentPanel) focusableChildren() []core.Widget {
	var out []core.Widget
	for _, w := range cp.children {
		if w.Focusable() {
			out = append(out, w)
		}
	}
	return out
}

// contentHeight returns the total height of all children.
// Must be called with cp.mu held.
func (cp *ContentPanel) contentHeight() int {
	maxY := 0
	for _, w := range cp.children {
		_, wy := w.Position()
		_, wh := w.Size()
		if end := wy + wh; end > maxY {
			maxY = end
		}
	}
	return maxY
}

// ensureVisible adjusts scrollY so that the widget at focusableIndex
// is visible. Must be called with cp.mu held.
func (cp *ContentPanel) ensureVisible(focusableIndex int) {
	focusables := cp.focusableChildren()
	if focusableIndex < 0 || focusableIndex >= len(focusables) {
		return
	}
	w := focusables[focusableIndex]
	_, wy := w.Position()
	_, wh := w.Size()
	_, panelH := cp.Size()

	if wy < cp.scrollY {
		cp.scrollY = wy
	} else if wy+wh > cp.scrollY+panelH {
		cp.scrollY = wy + wh - panelH
	}
}

func (cp *ContentPanel) invalidate() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.invalidateLocked()
}

func (cp *ContentPanel) invalidateLocked() {
	if cp.inv != nil {
		cp.inv(cp.Rect)
	}
}

// BrowseApp is a semantic terminal browser that drives Chromium via CDP
// and renders web content through the Accessibility Tree.
type BrowseApp struct {
	*adapter.UIApp

	mu        sync.Mutex
	startURL  string
	engine    *Engine
	tab       *Tab
	doc       *Document
	mapper    *Mapper
	layout    *LayoutManager
	urlBar    *widgets.Input
	content   *ContentPanel
	statusBar *widgets.StatusBar
	mode      DisplayMode
	modeForce bool

	profileDir string
}

// rootPanel is a simple container that stacks the URL bar (row 0)
// and content panel (rows 1..N). It implements Widget, ChildContainer,
// and FocusCycler so the UIManager can traverse between URL bar and content.
type rootPanel struct {
	core.BaseWidget
	urlBar  *widgets.Input
	content *ContentPanel
	inv     func(core.Rect)
}

func newRootPanel(urlBar *widgets.Input, content *ContentPanel) *rootPanel {
	rp := &rootPanel{
		urlBar:  urlBar,
		content: content,
	}
	rp.SetFocusable(true)
	return rp
}

// SetInvalidator implements core.InvalidationAware.
func (rp *rootPanel) SetInvalidator(fn func(core.Rect)) {
	rp.inv = fn
	rp.urlBar.SetInvalidator(fn)
	rp.content.SetInvalidator(fn)
}

// Draw renders URL bar at the top, then content below.
func (rp *rootPanel) Draw(p *core.Painter) {
	rp.urlBar.Draw(p)
	rp.content.Draw(p)
}

// Resize positions URL bar at y=0 (1 row) and content below.
func (rp *rootPanel) Resize(w, h int) {
	rp.BaseWidget.Resize(w, h)
	rp.layoutChildren()
}

// SetPosition updates position and relays out children.
func (rp *rootPanel) SetPosition(x, y int) {
	rp.BaseWidget.SetPosition(x, y)
	rp.layoutChildren()
}

func (rp *rootPanel) layoutChildren() {
	x, y := rp.Position()
	w, h := rp.Size()

	rp.urlBar.SetPosition(x, y)
	rp.urlBar.Resize(w, 1)

	contentY := y + 1
	contentH := h - 1
	if contentH < 0 {
		contentH = 0
	}
	rp.content.SetPosition(x, contentY)
	rp.content.Resize(w, contentH)
}

// HandleKey delegates to focused child.
func (rp *rootPanel) HandleKey(ev *tcell.EventKey) bool {
	// Route to whichever child is focused
	if rp.urlBar.IsFocused() {
		return rp.urlBar.HandleKey(ev)
	}
	return rp.content.HandleKey(ev)
}

// Focus focuses the URL bar by default.
func (rp *rootPanel) Focus() {
	rp.BaseWidget.Focus()
	rp.urlBar.Focus()
}

// Blur blurs both children.
func (rp *rootPanel) Blur() {
	rp.urlBar.Blur()
	rp.content.Blur()
	rp.BaseWidget.Blur()
}

// VisitChildren implements core.ChildContainer.
func (rp *rootPanel) VisitChildren(fn func(core.Widget)) {
	fn(rp.urlBar)
	fn(rp.content)
}

// CycleFocus implements core.FocusCycler — toggles between URL bar and content.
func (rp *rootPanel) CycleFocus(forward bool) bool {
	urlFocused := rp.urlBar.IsFocused()
	contentFocused := rp.content.IsFocused() || core.IsDescendantFocused(rp.content)

	if urlFocused {
		if forward {
			rp.urlBar.Blur()
			rp.content.Focus()
			return true
		}
		return false // at boundary going backward from URL bar
	}

	if contentFocused {
		// Try cycling within content first
		if rp.content.CycleFocus(forward) {
			return true
		}
		// Content exhausted — move to URL bar if going backward
		if !forward {
			rp.content.Blur()
			rp.urlBar.Focus()
			return true
		}
		return false // at boundary going forward past content
	}

	// Nothing focused, focus URL bar
	rp.urlBar.Focus()
	return true
}

// TrapsFocus returns true — this is the root container and wraps focus.
func (rp *rootPanel) TrapsFocus() bool {
	return true
}

// HitTest returns true if the point is within the root panel bounds.
func (rp *rootPanel) HitTest(x, y int) bool {
	return rp.Rect.Contains(x, y)
}

// New creates a new BrowseApp. If startURL is empty, no page is loaded initially.
func New(startURL string) core.App {
	ui := core.NewUIManager()
	ui.AdvanceFocusOnEnter = false // We handle Enter ourselves for URL bar

	urlBar := widgets.NewInput()
	urlBar.Placeholder = "Enter URL..."
	urlBar.SetHelpText("Ctrl+L: Focus URL bar")

	content := newContentPanel()

	root := newRootPanel(urlBar, content)

	app := &BrowseApp{
		startURL: startURL,
		urlBar:   urlBar,
		content:  content,
		layout:   NewLayoutManager(80, 24),
	}

	app.UIApp = adapter.NewUIApp("TexelBrowse", ui)
	ui.SetRootWidget(root)

	app.statusBar = app.UIApp.StatusBar()
	if app.statusBar != nil {
		app.statusBar.SetHintText("No page loaded")
	}

	// Mapper: click callback dispatches CDP click in a goroutine
	app.mapper = NewMapper(func(backendNodeID int64) {
		go app.clickNode(backendNodeID)
	})
	app.mapper.SetOnType(func(backendNodeID int64, text string) {
		go app.typeNode(backendNodeID, text)
	})

	// Wire URL bar submit
	urlBar.OnSubmit = func(text string) {
		go app.navigateTo(normalizeURL(text))
	}

	// Focus URL bar initially
	ui.Focus(urlBar)

	return app
}

// Run launches the Engine and Tab, then navigates to startURL if provided.
func (app *BrowseApp) Run() error {
	profileDir := app.profileDir
	if profileDir == "" {
		home, _ := os.UserHomeDir()
		profileDir = filepath.Join(home, ".texelation", "texelbrowse", "profile")
	}

	engine, err := NewEngine(profileDir)
	if err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Failed to launch browser: " + err.Error())
		}
		return app.UIApp.Run()
	}

	tab, err := engine.NewTab()
	if err != nil {
		engine.Close()
		if app.statusBar != nil {
			app.statusBar.ShowError("Failed to create tab: " + err.Error())
		}
		return app.UIApp.Run()
	}

	app.mu.Lock()
	app.engine = engine
	app.tab = tab
	app.mu.Unlock()

	// Wire tab callbacks
	tab.OnNavigate = func(url, title string) {
		app.urlBar.Text = url
		app.urlBar.CaretPos = len([]rune(url))
		if app.statusBar != nil {
			app.statusBar.SetHintText(title)
		}
		app.UI().InvalidateAll()
	}
	tab.OnLoading = func(loading bool) {
		if loading {
			if app.statusBar != nil {
				app.statusBar.SetHintText("Loading...")
			}
		} else {
			go app.fetchAndRender()
		}
	}

	// Navigate to start URL if provided
	if app.startURL != "" {
		go app.navigateTo(normalizeURL(app.startURL))
	}

	return app.UIApp.Run()
}

// Stop shuts down the tab and engine.
func (app *BrowseApp) Stop() {
	app.mu.Lock()
	tab := app.tab
	engine := app.engine
	app.tab = nil
	app.engine = nil
	app.mu.Unlock()

	if tab != nil {
		tab.Close()
	}
	if engine != nil {
		engine.Close()
	}

	app.UIApp.Stop()
}

// HandleKey intercepts global bindings before delegating to UIApp.
func (app *BrowseApp) HandleKey(ev *tcell.EventKey) {
	// Ctrl+L: focus URL bar
	if ev.Key() == tcell.KeyCtrlL {
		app.UI().Focus(app.urlBar)
		app.urlBar.CaretPos = len([]rune(app.urlBar.Text))
		app.UI().InvalidateAll()
		return
	}

	// Ctrl+R: reload
	if ev.Key() == tcell.KeyCtrlR {
		go app.reload()
		return
	}

	// Alt+Left: back
	if ev.Key() == tcell.KeyLeft && ev.Modifiers()&tcell.ModAlt != 0 {
		go app.back()
		return
	}

	// Alt+Right: forward
	if ev.Key() == tcell.KeyRight && ev.Modifiers()&tcell.ModAlt != 0 {
		go app.forward()
		return
	}

	// Ctrl+M: toggle reading/form mode
	if ev.Key() == tcell.KeyCtrlM {
		app.toggleMode()
		return
	}

	// Delegate to UIApp (which delegates to UIManager)
	app.UIApp.HandleKey(ev)
}

// navigateTo loads a URL in the tab and refreshes the rendered page.
func (app *BrowseApp) navigateTo(url string) {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	if app.statusBar != nil {
		app.statusBar.SetHintText("Navigating...")
	}

	if err := tab.Navigate(url); err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Navigation failed: " + err.Error())
		}
		return
	}

	app.fetchAndRender()
}

// reload reloads the current page.
func (app *BrowseApp) reload() {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	if app.statusBar != nil {
		app.statusBar.SetHintText("Reloading...")
	}

	if err := tab.Reload(); err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Reload failed: " + err.Error())
		}
		return
	}

	app.fetchAndRender()
}

// back navigates backward in history.
func (app *BrowseApp) back() {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	if err := tab.Back(); err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Back: " + err.Error())
		}
		return
	}

	app.fetchAndRender()
}

// forward navigates forward in history.
func (app *BrowseApp) forward() {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	if err := tab.Forward(); err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Forward: " + err.Error())
		}
		return
	}

	app.fetchAndRender()
}

// fetchAndRender fetches the AX tree, builds a Document, maps to widgets,
// arranges layout, and updates the content panel.
func (app *BrowseApp) fetchAndRender() {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	doc, err := tab.FetchDocument()
	if err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Fetch failed: " + err.Error())
		}
		return
	}

	app.mu.Lock()
	app.doc = doc

	// Determine display mode
	mode := app.mode
	if !app.modeForce {
		mode = doc.SuggestedMode()
		app.mode = mode
	}

	// Map nodes to widgets
	ws := app.mapper.MapDocument(doc)

	// Layout
	w, h := app.content.Size()
	app.layout.Resize(w, h)
	app.layout.SetMode(mode)
	app.layout.Arrange(ws)

	app.mu.Unlock()

	// Update content panel
	app.content.SetChildren(ws)

	// Update URL bar text
	url, title := tab.Location()
	app.urlBar.Text = url
	app.urlBar.CaretPos = len([]rune(url))

	if app.statusBar != nil {
		modeLabel := "Reading"
		if mode == ModeForm {
			modeLabel = "Form"
		}
		app.statusBar.SetHintText(title + " [" + modeLabel + "]")
	}

	app.UI().InvalidateAll()
}

// toggleMode switches between reading and form display modes.
func (app *BrowseApp) toggleMode() {
	app.mu.Lock()
	if app.mode == ModeReading {
		app.mode = ModeForm
	} else {
		app.mode = ModeReading
	}
	app.modeForce = true
	doc := app.doc
	app.mu.Unlock()

	if doc != nil {
		app.fetchAndRender()
	}
}

// clickNode dispatches a CDP click to the given backend node.
func (app *BrowseApp) clickNode(backendNodeID int64) {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	if err := tab.ClickNode(backendNodeID); err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Click failed: " + err.Error())
		}
	}
}

// typeNode dispatches text input to the given backend node.
func (app *BrowseApp) typeNode(backendNodeID int64, text string) {
	app.mu.Lock()
	tab := app.tab
	app.mu.Unlock()

	if tab == nil {
		return
	}

	if err := tab.SetValue(backendNodeID, text); err != nil {
		if app.statusBar != nil {
			app.statusBar.ShowError("Type failed: " + err.Error())
		}
	}
}
