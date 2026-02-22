// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/engine.go
// Summary: Browser engine layer wrapping chromedp to manage Chromium
//          lifecycle, tab navigation, AX tree fetching, and input
//          dispatch via CDP.

package texelbrowse

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Engine manages a Chromium process with a persistent profile directory.
// It provides tab creation and cleanup for the browser session.
type Engine struct {
	allocCtx      context.Context
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc

	mu   sync.Mutex
	tabs []*Tab
}

// NewEngine launches a headless Chromium instance with the given profile
// directory for persistent storage (cookies, cache, etc.).
func NewEngine(profileDir string) (*Engine, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(profileDir),
		chromedp.DisableGPU,
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)

	// The first context from the allocator owns the browser process.
	// Cancelling it shuts down Chromium. We keep it alive for the
	// engine's lifetime and create tabs as child contexts.
	//
	// Redirect chromedp's log/error output through the stdlib log
	// package so it goes to the debug log file (or is discarded)
	// rather than mangling the terminal display.
	browserCtx, browserCancel := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(log.Printf),
		chromedp.WithErrorf(log.Printf),
	)
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, fmt.Errorf("texelbrowse: failed to launch chromium: %w", err)
	}

	return &Engine{
		allocCtx:      allocCtx,
		allocCancel:   allocCancel,
		browserCtx:    browserCtx,
		browserCancel: browserCancel,
	}, nil
}

// NewTab opens a new browser tab backed by a fresh CDP target.
func (e *Engine) NewTab() (*Tab, error) {
	ctx, cancel := chromedp.NewContext(e.browserCtx)
	// Run an empty action to ensure the target is created.
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("texelbrowse: failed to create tab: %w", err)
	}

	tab := &Tab{
		ctx:    ctx,
		cancel: cancel,
		engine: e,
	}
	tab.setupListeners()

	e.mu.Lock()
	e.tabs = append(e.tabs, tab)
	e.mu.Unlock()

	return tab, nil
}

// Close shuts down all tabs and the Chromium process.
func (e *Engine) Close() {
	e.mu.Lock()
	tabs := make([]*Tab, len(e.tabs))
	copy(tabs, e.tabs)
	e.tabs = nil
	e.mu.Unlock()

	for _, t := range tabs {
		t.cancel()
	}
	e.browserCancel()
	e.allocCancel()
}

// removeTab removes a tab from the engine's tracking list.
func (e *Engine) removeTab(t *Tab) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, tab := range e.tabs {
		if tab == t {
			e.tabs = append(e.tabs[:i], e.tabs[i+1:]...)
			return
		}
	}
}

// Tab represents a single browser tab backed by a CDP target.
type Tab struct {
	ctx    context.Context
	cancel context.CancelFunc
	engine *Engine

	mu    sync.Mutex
	url   string
	title string

	// OnNavigate is called when the page URL or title changes.
	// It is invoked from a background goroutine and must not block.
	OnNavigate func(url, title string)

	// OnLoading is called when the page starts or finishes loading.
	// It is invoked from a background goroutine and must not block.
	OnLoading func(loading bool)
}

// Navigate loads a URL, waits for the body to be ready, then captures
// the final URL and title.
func (t *Tab) Navigate(url string) error {
	if err := chromedp.Run(t.ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
	); err != nil {
		return fmt.Errorf("texelbrowse: navigate to %q: %w", url, err)
	}
	return t.captureLocation()
}

// Back navigates backward in the tab's history.
// Uses low-level CDP calls and waitForLoad to avoid bfcache-related
// hangs with chromedp's responseAction/WaitReady, which may never
// return for cached back-forward navigations.
func (t *Tab) Back() error {
	if err := chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cur, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		if cur <= 0 || cur > int64(len(entries)-1) {
			return errors.New("texelbrowse: no previous history entry")
		}
		return page.NavigateToHistoryEntry(entries[cur-1].ID).Do(ctx)
	})); err != nil {
		return fmt.Errorf("texelbrowse: back: %w", err)
	}
	t.waitForLoad()
	return t.captureLocation()
}

// Forward navigates forward in the tab's history.
func (t *Tab) Forward() error {
	if err := chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		cur, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		if cur < 0 || cur >= int64(len(entries)-1) {
			return errors.New("texelbrowse: no forward history entry")
		}
		return page.NavigateToHistoryEntry(entries[cur+1].ID).Do(ctx)
	})); err != nil {
		return fmt.Errorf("texelbrowse: forward: %w", err)
	}
	t.waitForLoad()
	return t.captureLocation()
}

// Reload reloads the current page.
func (t *Tab) Reload() error {
	if err := chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return page.Reload().Do(ctx)
	})); err != nil {
		return fmt.Errorf("texelbrowse: reload: %w", err)
	}
	t.waitForLoad()
	return t.captureLocation()
}

// waitForLoad waits for a page load event or a short timeout,
// whichever comes first. This handles both normal loads and
// bfcache restores where lifecycle events may differ.
func (t *Tab) waitForLoad() {
	ch := make(chan struct{}, 1)
	lctx, lcancel := context.WithCancel(t.ctx)
	chromedp.ListenTarget(lctx, func(ev any) {
		switch ev.(type) {
		case *page.EventLoadEventFired, *page.EventFrameStoppedLoading:
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
	case <-t.ctx.Done():
	}
	lcancel()
}

// Location returns the current URL and title of the tab.
func (t *Tab) Location() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.url, t.title
}

// Close closes this tab's CDP target and removes it from the engine.
func (t *Tab) Close() {
	t.cancel()
	t.engine.removeTab(t)
}

// FetchDocument fetches the accessibility tree from the tab's page and
// converts it to a Document model. The returned document includes the
// tab's current URL and title.
//
// Uses raw CDP types instead of cdproto's accessibility package to avoid
// UnmarshalJSON errors when Chrome sends property names or value types
// that cdproto doesn't know about.
func (t *Tab) FetchDocument() (*Document, error) {
	var rawNodes []*rawAXNode
	if err := chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		rawNodes, err = fetchRawAXTree(ctx)
		return err
	})); err != nil {
		return nil, fmt.Errorf("texelbrowse: fetch AX tree: %w", err)
	}

	doc := buildDocumentFromRaw(rawNodes)

	t.mu.Lock()
	doc.URL = t.url
	doc.Title = t.title
	t.mu.Unlock()

	return doc, nil
}

// FocusNode focuses a DOM element by its BackendNodeID.
func (t *Tab) FocusNode(backendNodeID int64) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.Focus().WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).Do(ctx)
	}))
}

// ClickNode clicks the center of a DOM element identified by its
// BackendNodeID. It retrieves the element's box model, computes the
// center of the content quad, then dispatches mousePressed and
// mouseReleased events at that position.
func (t *Tab) ClickNode(backendNodeID int64) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		model, err := dom.GetBoxModel().WithBackendNodeID(cdp.BackendNodeID(backendNodeID)).Do(ctx)
		if err != nil {
			return fmt.Errorf("get box model: %w", err)
		}

		// Content quad is [x1,y1, x2,y2, x3,y3, x4,y4].
		// Compute center by averaging all four corners.
		q := model.Content
		if len(q) < 8 {
			return errors.New("texelbrowse: content quad has fewer than 8 values")
		}
		cx := (q[0] + q[2] + q[4] + q[6]) / 4
		cy := (q[1] + q[3] + q[5] + q[7]) / 4

		if err := input.DispatchMouseEvent(input.MousePressed, cx, cy).
			WithButton(input.Left).
			WithClickCount(1).
			Do(ctx); err != nil {
			return fmt.Errorf("mouse pressed: %w", err)
		}
		if err := input.DispatchMouseEvent(input.MouseReleased, cx, cy).
			WithButton(input.Left).
			WithClickCount(1).
			Do(ctx); err != nil {
			return fmt.Errorf("mouse released: %w", err)
		}
		return nil
	}))
}

// TypeText types text into the currently focused element using IME-style
// text insertion.
func (t *Tab) TypeText(text string) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.InsertText(text).Do(ctx)
	}))
}

// SetValue sets the value of an input element by focusing it, selecting
// all existing content with Ctrl+A, and inserting the new value.
func (t *Tab) SetValue(backendNodeID int64, value string) error {
	if err := t.FocusNode(backendNodeID); err != nil {
		return fmt.Errorf("texelbrowse: set value focus: %w", err)
	}
	// Select all existing content (Ctrl+A).
	if err := chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchKeyEvent(input.KeyRawDown).
			WithKey("a").
			WithCode("KeyA").
			WithWindowsVirtualKeyCode(65).
			WithModifiers(input.ModifierCtrl).
			Do(ctx); err != nil {
			return err
		}
		return input.DispatchKeyEvent(input.KeyUp).
			WithKey("a").
			WithCode("KeyA").
			WithWindowsVirtualKeyCode(65).
			WithModifiers(input.ModifierCtrl).
			Do(ctx)
	})); err != nil {
		return fmt.Errorf("texelbrowse: set value select all: %w", err)
	}
	if err := t.TypeText(value); err != nil {
		return fmt.Errorf("texelbrowse: set value insert: %w", err)
	}
	return nil
}

// PressKey sends a key press (rawKeyDown + keyUp) to the page.
// The key parameter is the DOM key value (e.g., "Enter", "Tab", "a"),
// code is the physical key code (e.g., "Enter", "Tab", "KeyA"),
// and keyCode is the Windows virtual key code (e.g., 13, 9, 65).
func (t *Tab) PressKey(key string, code string, keyCode int) error {
	return chromedp.Run(t.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchKeyEvent(input.KeyRawDown).
			WithKey(key).
			WithCode(code).
			WithWindowsVirtualKeyCode(int64(keyCode)).
			Do(ctx); err != nil {
			return fmt.Errorf("key down: %w", err)
		}
		if err := input.DispatchKeyEvent(input.KeyUp).
			WithKey(key).
			WithCode(code).
			WithWindowsVirtualKeyCode(int64(keyCode)).
			Do(ctx); err != nil {
			return fmt.Errorf("key up: %w", err)
		}
		return nil
	}))
}

// captureLocation fetches the current URL and title from the page
// and updates the tab's cached values.
func (t *Tab) captureLocation() error {
	var url, title string
	if err := chromedp.Run(t.ctx,
		chromedp.Location(&url),
		chromedp.Title(&title),
	); err != nil {
		return fmt.Errorf("texelbrowse: capture location: %w", err)
	}

	t.mu.Lock()
	t.url = url
	t.title = title
	cb := t.OnNavigate
	t.mu.Unlock()

	if cb != nil {
		cb(url, title)
	}
	return nil
}

// setupListeners registers CDP event listeners for page lifecycle events.
// Callbacks run on the CDP event goroutine, so any work that sends CDP
// messages is dispatched to a separate goroutine with a short timeout.
//
// Lifecycle events are already enabled by chromedp when attaching to the
// target (page.SetLifecycleEventsEnabled(true) in initContextBrowser).
func (t *Tab) setupListeners() {
	chromedp.ListenTarget(t.ctx, func(ev any) {
		switch ev.(type) {
		case *page.EventFrameStartedLoading:
			t.mu.Lock()
			cb := t.OnLoading
			t.mu.Unlock()
			if cb != nil {
				cb(true)
			}

		case *page.EventFrameStoppedLoading:
			t.mu.Lock()
			cb := t.OnLoading
			t.mu.Unlock()
			if cb != nil {
				cb(false)
			}
			// Re-fetch location after load completes. This runs on the
			// CDP event goroutine, so we must not call chromedp.Run
			// directly — spawn a goroutine with a timeout.
			go t.refreshLocation()

		case *page.EventFrameNavigated:
			go t.refreshLocation()
		}
	})
}

// refreshLocation fetches the current URL and title with a short timeout.
// It is safe to call from any goroutine.
func (t *Tab) refreshLocation() {
	ctx, cancel := context.WithTimeout(t.ctx, 5*time.Second)
	defer cancel()

	var url, title string
	if err := chromedp.Run(ctx,
		chromedp.Location(&url),
		chromedp.Title(&title),
	); err != nil {
		return // best-effort; ignore errors from closed contexts
	}

	t.mu.Lock()
	changed := t.url != url || t.title != title
	t.url = url
	t.title = title
	cb := t.OnNavigate
	t.mu.Unlock()

	if changed && cb != nil {
		cb(url, title)
	}
}
