// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/browse.go
// Summary: Semantic terminal browser that drives Chromium via CDP
//          and renders web content through the Accessibility Tree.

package texelbrowse

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/framegrace/texelui/adapter"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/scroll"
	"github.com/framegrace/texelui/widgets"
	"github.com/gdamore/tcell/v2"
)

// browseLogFile holds the open log file for the package lifetime.
var browseLogFile *os.File

func init() {
	// Always redirect log output to a file to avoid mangling terminal display.
	// Errors from chromedp and CDP will be captured in this file.
	var err error
	browseLogFile, err = os.OpenFile("/tmp/texelbrowse.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(browseLogFile)
		log.SetFlags(log.Ltime | log.Lmicroseconds)
		// Also redirect stderr so any stray fmt.Fprintf(os.Stderr, ...)
		// or panic output goes to the log file instead of the terminal.
		os.Stderr = browseLogFile
	} else {
		log.SetOutput(io.Discard)
	}
}

// pageBody is a simple container that holds the rendered page widgets.
// It does not scroll — scrolling is handled by the parent ScrollPane.
// It implements ChildContainer, FocusCycler, MouseAware, and HitTester
// so that ScrollPane can delegate focus, input, and mouse events.
type pageBody struct {
	core.BaseWidget
	children   []core.Widget
	positions  []layoutPos // layout positions relative to (0,0)
	focusIndex int
	inv        func(core.Rect)
}

// layoutPos stores a widget's position as set by the LayoutManager.
type layoutPos struct{ x, y int }

func newPageBody() *pageBody {
	pb := &pageBody{focusIndex: -1}
	pb.SetFocusable(true)
	return pb
}

// SetChildren replaces all children and captures their layout positions.
// The caller must have already positioned widgets via LayoutManager.Arrange.
func (pb *pageBody) SetChildren(ws []core.Widget) {
	pb.children = ws
	pb.positions = make([]layoutPos, len(ws))
	for i, w := range ws {
		x, y := w.Position()
		pb.positions[i] = layoutPos{x, y}
	}
	pb.focusIndex = -1
	pb.repositionChildren()
}

// contentHeight returns the total height of all children based on
// their layout positions.
func (pb *pageBody) contentHeight() int {
	maxY := 0
	for i, w := range pb.children {
		_, h := w.Size()
		if end := pb.positions[i].y + h; end > maxY {
			maxY = end
		}
	}
	return maxY
}

// SetPosition updates position and repositions all children.
func (pb *pageBody) SetPosition(x, y int) {
	pb.BaseWidget.SetPosition(x, y)
	pb.repositionChildren()
}

// repositionChildren offsets all children by the body's current position.
func (pb *pageBody) repositionChildren() {
	bx, by := pb.Position()
	for i, w := range pb.children {
		w.SetPosition(pb.positions[i].x+bx, pb.positions[i].y+by)
	}
}

// SetInvalidator implements core.InvalidationAware.
func (pb *pageBody) SetInvalidator(fn func(core.Rect)) {
	pb.inv = fn
	for _, w := range pb.children {
		if ia, ok := w.(core.InvalidationAware); ok {
			ia.SetInvalidator(fn)
		}
	}
}

// Draw renders all children. Clipping is handled by the parent ScrollPane.
func (pb *pageBody) Draw(p *core.Painter) {
	for _, w := range pb.children {
		w.Draw(p)
	}
}

// HandleKey delegates to the focused child.
func (pb *pageBody) HandleKey(ev *tcell.EventKey) bool {
	focusables := pb.focusableChildren()
	if pb.focusIndex >= 0 && pb.focusIndex < len(focusables) {
		return focusables[pb.focusIndex].HandleKey(ev)
	}
	return false
}

// Focus focuses the previously focused child (or the first one).
func (pb *pageBody) Focus() {
	pb.BaseWidget.Focus()
	focusables := pb.focusableChildren()
	if len(focusables) == 0 {
		return
	}
	if pb.focusIndex < 0 || pb.focusIndex >= len(focusables) {
		pb.focusIndex = 0
	}
	focusables[pb.focusIndex].Focus()
}

// Blur blurs all children.
func (pb *pageBody) Blur() {
	for _, w := range pb.children {
		w.Blur()
	}
	pb.BaseWidget.Blur()
}

// CycleFocus implements core.FocusCycler.
func (pb *pageBody) CycleFocus(forward bool) bool {
	focusables := pb.focusableChildren()
	if len(focusables) == 0 {
		return false
	}

	currentIdx := -1
	for i, w := range focusables {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			currentIdx = i
			break
		}
	}

	if currentIdx < 0 {
		if forward {
			focusables[0].Focus()
			pb.focusIndex = 0
		} else {
			focusables[len(focusables)-1].Focus()
			pb.focusIndex = len(focusables) - 1
		}
		return true
	}

	var nextIdx int
	if forward {
		nextIdx = currentIdx + 1
		if nextIdx >= len(focusables) {
			return false
		}
	} else {
		nextIdx = currentIdx - 1
		if nextIdx < 0 {
			return false
		}
	}

	focusables[currentIdx].Blur()
	focusables[nextIdx].Focus()
	pb.focusIndex = nextIdx
	return true
}

// TrapsFocus implements core.FocusCycler.
func (pb *pageBody) TrapsFocus() bool { return false }

// VisitChildren implements core.ChildContainer.
func (pb *pageBody) VisitChildren(fn func(core.Widget)) {
	for _, w := range pb.children {
		fn(w)
	}
}

// HandleMouse routes mouse clicks to child widgets.
func (pb *pageBody) HandleMouse(ev *tcell.EventMouse) bool {
	x, y := ev.Position()
	buttons := ev.Buttons()

	// For wheel events, let parent ScrollPane handle.
	if buttons&(tcell.WheelUp|tcell.WheelDown) != 0 {
		return false
	}

	for _, w := range pb.children {
		if w.HitTest(x, y) && w.Focusable() {
			if buttons&tcell.Button1 != 0 {
				pb.blurAll()
				w.Focus()
				pb.updateFocusIndex()
			}
			if ma, ok := w.(core.MouseAware); ok {
				return ma.HandleMouse(ev)
			}
			return true
		}
	}
	return false
}

// WidgetAt implements core.HitTester.
func (pb *pageBody) WidgetAt(x, y int) core.Widget {
	for _, w := range pb.children {
		if w.HitTest(x, y) && w.Focusable() {
			return w
		}
	}
	return nil
}

func (pb *pageBody) focusableChildren() []core.Widget {
	var out []core.Widget
	for _, w := range pb.children {
		if w.Focusable() {
			out = append(out, w)
		}
	}
	return out
}

func (pb *pageBody) blurAll() {
	for _, w := range pb.children {
		w.Blur()
	}
}

func (pb *pageBody) updateFocusIndex() {
	focusables := pb.focusableChildren()
	for i, w := range focusables {
		if fs, ok := w.(core.FocusState); ok && fs.IsFocused() {
			pb.focusIndex = i
			return
		}
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
	body      *pageBody
	scroller  *scroll.ScrollPane
	statusBar *widgets.StatusBar
	mode      DisplayMode
	modeForce bool

	profileDir string
}

// rootPanel is a simple container that stacks the URL bar (row 0)
// and scroll pane (rows 1..N). It implements Widget, ChildContainer,
// FocusCycler, MouseAware, and HitTester so the UIManager can
// traverse between URL bar and content and route mouse events.
type rootPanel struct {
	core.BaseWidget
	urlBar   *widgets.Input
	scroller *scroll.ScrollPane
	inv      func(core.Rect)
}

func newRootPanel(urlBar *widgets.Input, scroller *scroll.ScrollPane) *rootPanel {
	rp := &rootPanel{
		urlBar:   urlBar,
		scroller: scroller,
	}
	rp.SetFocusable(true)
	return rp
}

// SetInvalidator implements core.InvalidationAware.
func (rp *rootPanel) SetInvalidator(fn func(core.Rect)) {
	rp.inv = fn
	rp.urlBar.SetInvalidator(fn)
	rp.scroller.SetInvalidator(fn)
}

// Draw renders URL bar at the top, then scroll pane below.
func (rp *rootPanel) Draw(p *core.Painter) {
	rp.urlBar.Draw(p)
	rp.scroller.Draw(p)
}

// Resize positions URL bar at y=0 (1 row) and scroll pane below.
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
	rp.scroller.SetPosition(x, contentY)
	rp.scroller.Resize(w, contentH)
}

// HandleKey delegates to focused child.
func (rp *rootPanel) HandleKey(ev *tcell.EventKey) bool {
	if rp.urlBar.IsFocused() {
		return rp.urlBar.HandleKey(ev)
	}
	return rp.scroller.HandleKey(ev)
}

// HandleMouse routes mouse events to URL bar or scroll pane.
func (rp *rootPanel) HandleMouse(ev *tcell.EventMouse) bool {
	_, y := ev.Position()
	buttons := ev.Buttons()

	// Wheel events go to the scroll pane regardless of position.
	if buttons&(tcell.WheelUp|tcell.WheelDown) != 0 {
		return rp.scroller.HandleMouse(ev)
	}

	// Route clicks by Y position.
	urlY := rp.urlBar.Rect.Y
	if y == urlY {
		return rp.urlBar.HandleMouse(ev)
	}
	return rp.scroller.HandleMouse(ev)
}

// WidgetAt implements core.HitTester for click-to-focus.
func (rp *rootPanel) WidgetAt(x, y int) core.Widget {
	if !rp.HitTest(x, y) {
		return nil
	}
	if rp.urlBar.HitTest(x, y) {
		return rp.urlBar
	}
	if dw := rp.scroller.WidgetAt(x, y); dw != nil {
		return dw
	}
	return rp
}

// Focus focuses the URL bar by default.
func (rp *rootPanel) Focus() {
	rp.BaseWidget.Focus()
	rp.urlBar.Focus()
}

// Blur blurs both children.
func (rp *rootPanel) Blur() {
	rp.urlBar.Blur()
	rp.scroller.Blur()
	rp.BaseWidget.Blur()
}

// VisitChildren implements core.ChildContainer.
func (rp *rootPanel) VisitChildren(fn func(core.Widget)) {
	fn(rp.urlBar)
	fn(rp.scroller)
}

// CycleFocus implements core.FocusCycler — toggles between URL bar and content.
func (rp *rootPanel) CycleFocus(forward bool) bool {
	urlFocused := rp.urlBar.IsFocused()
	contentFocused := rp.scroller.IsFocused() || core.IsDescendantFocused(rp.scroller)

	if urlFocused {
		if forward {
			rp.urlBar.Blur()
			rp.scroller.Focus()
			return true
		}
		return false
	}

	if contentFocused {
		if rp.scroller.CycleFocus(forward) {
			return true
		}
		if !forward {
			rp.scroller.Blur()
			rp.urlBar.Focus()
			return true
		}
		return false
	}

	rp.urlBar.Focus()
	return true
}

// TrapsFocus returns true — this is the root container and wraps focus.
func (rp *rootPanel) TrapsFocus() bool { return true }

// HitTest returns true if the point is within the root panel bounds.
func (rp *rootPanel) HitTest(x, y int) bool { return rp.Rect.Contains(x, y) }

// New creates a new BrowseApp. If startURL is empty, no page is loaded initially.
func New(startURL string) core.App {
	ui := core.NewUIManager()
	ui.AdvanceFocusOnEnter = false

	urlBar := widgets.NewInput()
	urlBar.Placeholder = "Enter URL..."
	urlBar.SetHelpText("Ctrl+L: Focus URL bar")

	body := newPageBody()
	scroller := scroll.NewScrollPane()
	scroller.SetChild(body)

	root := newRootPanel(urlBar, scroller)

	app := &BrowseApp{
		startURL: startURL,
		urlBar:   urlBar,
		body:     body,
		scroller: scroller,
		layout:   NewLayoutManager(80, 24),
	}

	app.UIApp = adapter.NewUIApp("TexelBrowse", ui)
	ui.SetRootWidget(root)

	app.statusBar = app.UIApp.StatusBar()
	if app.statusBar != nil {
		app.statusBar.SetHintText("No page loaded")
	}

	app.mapper = NewMapper(func(backendNodeID int64) {
		go app.clickNode(backendNodeID)
	})
	app.mapper.SetOnType(func(backendNodeID int64, text string) {
		go app.typeNode(backendNodeID, text)
	})

	urlBar.OnSubmit = func(text string) {
		go app.navigateTo(normalizeURL(text))
	}

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

	// Layout — use the scroller's viewport width, full virtual height.
	w, _ := app.scroller.Size()
	app.layout.Resize(w, 0)
	app.layout.SetMode(mode)
	app.layout.Arrange(ws)

	app.mu.Unlock()

	// Update page body and scroll pane content height.
	app.body.SetChildren(ws)
	app.scroller.SetContentHeight(app.body.contentHeight())

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
// After clicking, it waits briefly for any navigation or DOM change,
// then re-fetches the page to reflect the updated state.
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
		return
	}

	// Wait briefly for JS handlers and potential DOM mutations,
	// then re-fetch the page. Full navigations are also handled
	// by the OnLoading callback, but SPA-style updates only
	// modify the DOM without triggering a load event.
	go func() {
		time.Sleep(500 * time.Millisecond)
		app.fetchAndRender()
	}()
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
