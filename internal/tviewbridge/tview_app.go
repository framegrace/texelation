// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/tviewbridge/tview_app.go
// Summary: Wraps tview.Application to implement texel.App interface.
// Usage: Used to run tview widgets as pane applications in the desktop.
// Notes: Manages the lifecycle and event forwarding between tview and texel.

package tviewbridge

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"texelation/texel"
)

// TViewApp wraps a tview.Application to work as a texel.App.
// It uses a VirtualScreen to capture tview's rendering output and
// forwards key events from texel to tview.
type TViewApp struct {
	app           *tview.Application
	screen        *VirtualScreen
	root          tview.Primitive
	width, height int
	title         string
	refreshChan   chan<- bool

	mu             sync.Mutex
	running        bool
	stopCh         chan struct{}
	dirty          bool // Track if buffer needs redrawing
	firstFrameCh   chan struct{} // Signals when first frame is drawn
	firstFrameDone bool
}

// NewTViewApp creates a new TViewApp with the given title and root primitive.
func NewTViewApp(title string, root tview.Primitive) *TViewApp {
	return &TViewApp{
		title:        title,
		root:         root,
		stopCh:       make(chan struct{}),
		firstFrameCh: make(chan struct{}),
	}
}

// Run starts the tview application in a background goroutine.
// The tview event loop runs independently, and Render() reads from
// the VirtualScreen buffer whenever needed (thread-safe).
func (t *TViewApp) Run() error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return nil
	}

	// Create virtual screen with current dimensions
	if t.width == 0 || t.height == 0 {
		t.width = 80
		t.height = 24
	}
	t.screen = NewVirtualScreen(t.width, t.height)

	// Initialize the virtual screen
	if err := t.screen.Init(); err != nil {
		t.mu.Unlock()
		return err
	}

	// Create tview application
	t.app = tview.NewApplication()
	t.app.SetScreen(t.screen)
	t.app.SetRoot(t.root, true)
	t.app.SetFocus(t.root)

	// Set up callback to signal when first frame is drawn
	t.screen.SetOnFirstShow(func() {
		close(t.firstFrameCh)
	})

	t.running = true
	t.mu.Unlock()

	// Start tview's event loop in a background goroutine
	// This lets tview run at its own pace, and we just read from
	// the VirtualScreen buffer whenever Render() is called
	go func() {
		// Run tview's event loop (blocks until Stop() is called)
		if err := t.app.Run(); err != nil {
			// Log error but don't crash the app
			println("TViewApp: tview.Run() error:", err.Error())
		}
	}()

	// Wait for tview to draw the first frame before returning
	// This ensures Render() always returns a valid buffer (not empty)
	select {
	case <-t.firstFrameCh:
		// First frame drawn successfully
		t.mu.Lock()
		t.firstFrameDone = true
		t.mu.Unlock()
	case <-time.After(500 * time.Millisecond):
		// Timeout - proceed anyway
		println("TViewApp: Warning - timeout waiting for first frame")
	}

	// Send initial refresh notification after setup
	if t.refreshChan != nil {
		select {
		case t.refreshChan <- true:
		default:
		}
	}

	return nil
}

// Stop terminates the tview application.
func (t *TViewApp) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return
	}

	if t.app != nil {
		t.app.Stop()
	}

	close(t.stopCh)
	t.running = false
}

// Resize informs the application that the pane's dimensions have changed.
func (t *TViewApp) Resize(cols, rows int) {
	t.mu.Lock()
	t.width = cols
	t.height = rows
	t.mu.Unlock()

	if t.screen != nil {
		t.screen.Resize(0, 0, cols, rows)
	}

	// DON'T manually call Draw() and Show() here - let tview's event loop handle it.
	// If we call Show() before tview finishes drawing, we swap an empty buffer to the front!
	// The tview event loop will call Draw() and Show() naturally when it processes the resize event.

	// Request a refresh after resize
	if t.refreshChan != nil {
		select {
		case t.refreshChan <- true:
		default:
		}
	}
}

// Render returns the application's current visual state as a 2D buffer of Cells.
// This reads from the VirtualScreen buffer which is continuously updated by
// the tview event loop running in the background (thread-safe).
func (t *TViewApp) Render() [][]texel.Cell {
	if t.screen == nil {
		// Return empty buffer if not initialized
		t.mu.Lock()
		w, h := t.width, t.height
		t.mu.Unlock()

		if w == 0 || h == 0 {
			return [][]texel.Cell{}
		}

		buffer := make([][]texel.Cell, h)
		for y := 0; y < h; y++ {
			buffer[y] = make([]texel.Cell, w)
			for x := 0; x < w; x++ {
				buffer[y][x] = texel.Cell{
					Ch:    ' ',
					Style: tcell.StyleDefault,
				}
			}
		}
		return buffer
	}

	// Read the current buffer from VirtualScreen (thread-safe)
	return t.screen.GetBuffer()
}

// GetTitle returns the title of the application.
func (t *TViewApp) GetTitle() string {
	return t.title
}

// HandleKey forwards a key event to the tview application.
func (t *TViewApp) HandleKey(ev *tcell.EventKey) {
	if t.app == nil || !t.running {
		return
	}

	// Post the event to the virtual screen
	t.screen.PostEvent(ev)

	// Manually trigger a draw to process the event
	t.app.Draw()
	t.screen.Show()

	// Notify that we need a refresh (only if refresh chan is set)
	if t.refreshChan != nil {
		select {
		case t.refreshChan <- true:
		default:
		}
	}
}

// SetRefreshNotifier sets the channel used to signal refresh requests.
func (t *TViewApp) SetRefreshNotifier(ch chan<- bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refreshChan = ch
}

// QueueUpdate queues a function to be executed on the tview thread.
// This is useful for updating tview widgets from outside the main loop.
func (t *TViewApp) QueueUpdate(f func()) {
	if t.app != nil {
		// Execute the update function
		f()

		// Manually trigger a draw
		t.app.Draw()
		t.screen.Show()

		// Request a refresh after the update
		if t.refreshChan != nil {
			select {
			case t.refreshChan <- true:
			default:
			}
		}
	}
}

// GetApplication returns the underlying tview.Application for advanced use cases.
func (t *TViewApp) GetApplication() *tview.Application {
	return t.app
}

// GetRoot returns the root primitive.
func (t *TViewApp) GetRoot() tview.Primitive {
	return t.root
}

// SetRoot sets a new root primitive and updates the display.
func (t *TViewApp) SetRoot(root tview.Primitive) {
	t.mu.Lock()
	t.root = root
	t.mu.Unlock()

	if t.app != nil {
		t.app.SetRoot(root, true)

		// Manually trigger a draw
		t.app.Draw()
		t.screen.Show()

		// Request a refresh after changing root
		if t.refreshChan != nil {
			select {
			case t.refreshChan <- true:
			default:
			}
		}
	}
}
