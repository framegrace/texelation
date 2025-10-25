// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/tviewbridge/virtual_screen.go
// Summary: Virtual tcell.Screen implementation that captures draw calls into a buffer.
// Usage: Used by TViewApp to bridge tview rendering into texel.Cell buffers.
// Notes: Implements tcell.Screen interface without owning a real terminal.

package tviewbridge

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/texel"
)

// VirtualScreen implements tcell.Screen by capturing all draw calls into an
// in-memory cell buffer. Uses double buffering to ensure readers always see
// complete frames (no tearing or partial draws).
type VirtualScreen struct {
	width, height int

	// Double buffering: tview draws to backBuffer, Show() swaps to frontBuffer
	frontBuffer [][]texel.Cell
	backBuffer  [][]texel.Cell

	style         tcell.Style
	mu            sync.RWMutex

	// Cursor state
	cursorX, cursorY  int
	cursorVisible     bool

	// Event handling
	eventChan chan tcell.Event
	stopChan  chan struct{}

	// Drawing state
	dirty           bool
	contentDrawn    bool // Track if content has been drawn since last Clear()
	onFirstShow     func() // Callback for first successful Show() (buffer swap)

	// Refresh notification
	refreshChan chan<- bool // Notify when frame is ready (called in Show())
}

// NewVirtualScreen creates a new virtual screen with the given dimensions.
func NewVirtualScreen(width, height int) *VirtualScreen {
	vs := &VirtualScreen{
		width:     width,
		height:    height,
		style:     tcell.StyleDefault,
		eventChan: make(chan tcell.Event, 32),
		stopChan:  make(chan struct{}),
	}
	vs.allocateBuffers()
	return vs
}

// allocateBuffers creates both front and back buffers
func (vs *VirtualScreen) allocateBuffers() {
	vs.frontBuffer = vs.createBuffer()
	vs.backBuffer = vs.createBuffer()
}

// createBuffer creates a single buffer with the current dimensions
func (vs *VirtualScreen) createBuffer() [][]texel.Cell {
	buffer := make([][]texel.Cell, vs.height)
	for y := 0; y < vs.height; y++ {
		buffer[y] = make([]texel.Cell, vs.width)
		for x := 0; x < vs.width; x++ {
			buffer[y][x] = texel.Cell{
				Ch:    ' ',
				Style: tcell.StyleDefault,
			}
		}
	}
	return buffer
}

// Init initializes the screen (no-op for virtual screen).
func (vs *VirtualScreen) Init() error {
	return nil
}

// Fini finalizes the screen.
func (vs *VirtualScreen) Fini() {
	close(vs.stopChan)
}

// Clear clears the screen (clears backBuffer in place, not creating a new one).
// This ensures that if Show() is called right after Clear(), we don't swap an
// incomplete buffer to the front - frontBuffer stays unchanged until new content is drawn.
func (vs *VirtualScreen) Clear() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// If backBuffer doesn't match current dimensions (can happen during resize),
	// recreate it. Otherwise clear in place.
	if len(vs.backBuffer) != vs.height || (len(vs.backBuffer) > 0 && len(vs.backBuffer[0]) != vs.width) {
		vs.backBuffer = vs.createBuffer()
	} else {
		// Clear existing backBuffer in place
		for y := 0; y < vs.height && y < len(vs.backBuffer); y++ {
			for x := 0; x < vs.width && x < len(vs.backBuffer[y]); x++ {
				vs.backBuffer[y][x] = texel.Cell{
					Ch:    ' ',
					Style: tcell.StyleDefault,
				}
			}
		}
	}
	vs.dirty = true
	vs.contentDrawn = false // Mark that no content has been drawn yet
}

// Fill fills the screen with the given rune and style (fills backBuffer).
func (vs *VirtualScreen) Fill(r rune, style tcell.Style) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// Ensure backBuffer matches current dimensions
	if len(vs.backBuffer) != vs.height || (len(vs.backBuffer) > 0 && len(vs.backBuffer[0]) != vs.width) {
		vs.backBuffer = vs.createBuffer()
	}

	for y := 0; y < vs.height && y < len(vs.backBuffer); y++ {
		for x := 0; x < vs.width && x < len(vs.backBuffer[y]); x++ {
			vs.backBuffer[y][x] = texel.Cell{Ch: r, Style: style}
		}
	}
	vs.dirty = true
	vs.contentDrawn = true // Mark that content has been drawn
}

// SetContent sets the content at the given position (writes to backBuffer).
func (vs *VirtualScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if x < 0 || x >= vs.width || y < 0 || y >= vs.height {
		return
	}

	// Ensure backBuffer matches current dimensions and has the row
	if y >= len(vs.backBuffer) || x >= len(vs.backBuffer[y]) {
		// Recreate backBuffer if dimensions don't match
		if len(vs.backBuffer) != vs.height || (len(vs.backBuffer) > 0 && len(vs.backBuffer[0]) != vs.width) {
			vs.backBuffer = vs.createBuffer()
		}
		// Double-check bounds after recreating
		if y >= len(vs.backBuffer) || x >= len(vs.backBuffer[y]) {
			return
		}
	}

	// For now, ignore combining characters (tview rarely uses them)
	vs.backBuffer[y][x] = texel.Cell{
		Ch:    primary,
		Style: style,
	}
	vs.dirty = true
	vs.contentDrawn = true // Mark that content has been drawn
}

// GetContent returns the content at the given position (reads from frontBuffer).
func (vs *VirtualScreen) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	// Check against actual buffer dimensions, not vs.width/vs.height
	// During resize, width/height may be updated before frontBuffer is resized
	if y < 0 || y >= len(vs.frontBuffer) {
		return ' ', nil, tcell.StyleDefault, 1
	}
	if x < 0 || x >= len(vs.frontBuffer[y]) {
		return ' ', nil, tcell.StyleDefault, 1
	}

	cell := vs.frontBuffer[y][x]
	return cell.Ch, nil, cell.Style, 1
}

// SetStyle sets the default style.
func (vs *VirtualScreen) SetStyle(style tcell.Style) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.style = style
}

// ShowCursor shows the cursor at the given position.
func (vs *VirtualScreen) ShowCursor(x, y int) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.cursorX = x
	vs.cursorY = y
	vs.cursorVisible = true
}

// HideCursor hides the cursor.
func (vs *VirtualScreen) HideCursor() {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.cursorVisible = false
}

// Size returns the screen dimensions.
func (vs *VirtualScreen) Size() (int, int) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.width, vs.height
}

// PollEvent returns the next event (blocks until an event is available).
func (vs *VirtualScreen) PollEvent() tcell.Event {
	select {
	case ev := <-vs.eventChan:
		return ev
	case <-vs.stopChan:
		return nil
	}
}

// PostEvent posts an event to the event queue.
func (vs *VirtualScreen) PostEvent(ev tcell.Event) error {
	select {
	case vs.eventChan <- ev:
		return nil
	case <-vs.stopChan:
		return nil
	default:
		// Drop event if channel is full
		return nil
	}
}

// HasPendingEvent returns true if there are pending events.
func (vs *VirtualScreen) HasPendingEvent() bool {
	return len(vs.eventChan) > 0
}

// Show displays any pending changes (no-op for virtual screen).
// Show swaps the back buffer to the front buffer, making the completed frame visible.
// This is called by tview after it finishes drawing a frame, ensuring readers always
// see complete, consistent frames (no tearing or partial draws).
// If no content has been drawn since Clear() (contentDrawn=false), skip the swap to
// avoid showing empty/partial frames.
func (vs *VirtualScreen) Show() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// Only swap if content has actually been drawn since Clear()
	// This prevents showing empty buffers when tview calls Clear() then Show()
	// before drawing any content
	if vs.contentDrawn {
		// Swap buffers: back becomes front, front becomes back
		vs.frontBuffer, vs.backBuffer = vs.backBuffer, vs.frontBuffer
		vs.contentDrawn = false // Reset for next frame

		// Call onFirstShow callback if this is the first successful swap
		if vs.onFirstShow != nil {
			callback := vs.onFirstShow
			vs.onFirstShow = nil // Clear so it only fires once
			// Call outside the lock to avoid deadlock
			vs.mu.Unlock()
			callback()
			vs.mu.Lock()
		}

		// Notify that a new frame is ready for rendering
		// This triggers the pane to call Render() and publish the updated buffer
		if vs.refreshChan != nil {
			select {
			case vs.refreshChan <- true:
			default: // Don't block if channel is full
			}
		}
	}

	vs.dirty = false
}

// SetOnFirstShow sets a callback that will be called once when the first
// frame is successfully drawn and swapped to the front buffer.
func (vs *VirtualScreen) SetOnFirstShow(callback func()) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.onFirstShow = callback
}

// SetRefreshNotifier sets the channel used to signal when a frame is ready.
// Called in Show() after buffer swap to trigger pane refresh and publishing.
func (vs *VirtualScreen) SetRefreshNotifier(ch chan<- bool) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.refreshChan = ch
}

// Sync synchronizes the screen (no-op for virtual screen).
func (vs *VirtualScreen) Sync() {
	// No-op
}

// Beep emits a beep (no-op for virtual screen).
func (vs *VirtualScreen) Beep() error {
	return nil
}

// Colors returns the number of colors supported (use 256 colors).
func (vs *VirtualScreen) Colors() int {
	return 256
}

// SetCell is a convenience method (not part of tcell.Screen interface).
func (vs *VirtualScreen) SetCell(x, y int, style tcell.Style, ch ...rune) {
	if len(ch) > 0 {
		vs.SetContent(x, y, ch[0], ch[1:], style)
	}
}

// ChannelEvents is required by tcell.Screen but not used in our case.
func (vs *VirtualScreen) ChannelEvents(ch chan<- tcell.Event, quit <-chan struct{}) {
	// We use PostEvent instead
}

// RegisterRuneFallback registers a fallback for runes (no-op).
func (vs *VirtualScreen) RegisterRuneFallback(orig rune, fallback string) {
	// No-op
}

// UnregisterRuneFallback unregisters a fallback (no-op).
func (vs *VirtualScreen) UnregisterRuneFallback(orig rune) {
	// No-op
}

// CanDisplay returns true if the screen can display the given rune.
func (vs *VirtualScreen) CanDisplay(r rune, checkFallbacks bool) bool {
	return true // Assume we can display everything
}

// HasMouse returns true if the screen supports mouse events.
func (vs *VirtualScreen) HasMouse() bool {
	return false // For now, no mouse support
}

// HasKey returns true if the screen can differentiate the given key.
func (vs *VirtualScreen) HasKey(key tcell.Key) bool {
	return true // Assume all keys are supported
}

// Resize changes the dimensions of the virtual screen.
// Resize changes the dimensions of the virtual screen.
// IMPORTANT: Only resizes backBuffer (what tview draws to). Keeps frontBuffer unchanged
// until tview finishes drawing to the new size and calls Show(). This prevents showing
// empty buffers during resize - we keep displaying the old (complete) content until the
// new content is ready.
func (vs *VirtualScreen) Resize(x, y, width, height int) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	vs.width = width
	vs.height = height

	// Only resize backBuffer - keep frontBuffer showing old content
	vs.backBuffer = vs.createBuffer()
	vs.dirty = true
	vs.contentDrawn = false // Mark that new content needs to be drawn for new size

	// Post resize event
	ev := tcell.NewEventResize(width, height)
	select {
	case vs.eventChan <- ev:
	default:
	}
}

// GetBuffer returns a copy of the current cell buffer for rendering.
// GetBuffer returns a copy of the front buffer (always a complete frame).
// Reads from frontBuffer which contains the last completed frame after Show().
// Note: During resize, frontBuffer may have different dimensions than vs.width/vs.height
// (it keeps the old size until tview finishes drawing the new size and calls Show()).
func (vs *VirtualScreen) GetBuffer() [][]texel.Cell {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	// Guard: Don't return buffer if we're mid-frame (Clear called but Show not called)
	// This prevents returning a buffer that might get cleared in-place after we return it
	// contentDrawn=false means either:
	// - Clear() was just called and drawing hasn't started yet
	// - Drawing started but Show() hasn't been called yet
	// In either case, frontBuffer is the last complete frame and safe to return
	// But we need to deep copy it because after we release the lock, Show() might swap it
	// to backBuffer, and then Clear() might modify it in place

	// Use frontBuffer's actual dimensions, not vs.width/vs.height
	// (they may differ during resize)
	if len(vs.frontBuffer) == 0 {
		// Return properly initialized blank buffer with current dimensions if frontBuffer is empty
		buffer := make([][]texel.Cell, vs.height)
		for y := 0; y < vs.height; y++ {
			buffer[y] = make([]texel.Cell, vs.width)
			// Initialize all cells with space character and default style
			// (zero-valued cells have Ch=0 which renders as garbage)
			for x := 0; x < vs.width; x++ {
				buffer[y][x] = texel.Cell{
					Ch:    ' ',
					Style: tcell.StyleDefault,
				}
			}
		}
		return buffer
	}

	// Deep copy frontBuffer to prevent caller from seeing in-place modifications
	// This is necessary because:
	// 1. We return frontBuffer reference
	// 2. After releasing lock, Show() might swap frontBuffer->backBuffer
	// 3. Then Clear() modifies backBuffer in-place
	// 4. Caller still holding old reference would see cleared cells!
	height := len(vs.frontBuffer)
	width := 0
	if height > 0 {
		width = len(vs.frontBuffer[0])
	}

	buffer := make([][]texel.Cell, height)
	for y := 0; y < height; y++ {
		buffer[y] = make([]texel.Cell, width)
		copy(buffer[y], vs.frontBuffer[y])
	}
	return buffer
}

// EnableMouse enables mouse events (no-op).
func (vs *VirtualScreen) EnableMouse(...tcell.MouseFlags) {
	// No-op
}

// DisableMouse disables mouse events (no-op).
func (vs *VirtualScreen) DisableMouse() {
	// No-op
}

// EnablePaste enables bracketed paste mode (no-op).
func (vs *VirtualScreen) EnablePaste() {
	// No-op
}

// DisablePaste disables bracketed paste mode (no-op).
func (vs *VirtualScreen) DisablePaste() {
	// No-op
}

// EnableFocus enables focus reporting (no-op).
func (vs *VirtualScreen) EnableFocus() {
	// No-op
}

// DisableFocus disables focus reporting (no-op).
func (vs *VirtualScreen) DisableFocus() {
	// No-op
}

// SetSize is an alias for Resize.
func (vs *VirtualScreen) SetSize(width, height int) {
	vs.Resize(0, 0, width, height)
}

// LockRegion locks a region for atomic updates (no-op).
func (vs *VirtualScreen) LockRegion(x, y, w, h int, lock bool) {
	// No-op
}

// UnlockRegion unlocks a region (no-op).
func (vs *VirtualScreen) UnlockRegion(restore bool) {
	// No-op
}

// Suspend suspends the screen (no-op).
func (vs *VirtualScreen) Suspend() error {
	return nil
}

// Resume resumes the screen (no-op).
func (vs *VirtualScreen) Resume() error {
	return nil
}

// SetCursorStyle sets the cursor style (no-op).
func (vs *VirtualScreen) SetCursorStyle(style tcell.CursorStyle, color ...tcell.Color) {
	// No-op
}

// GetCursorStyle returns the current cursor style.
func (vs *VirtualScreen) GetCursorStyle() tcell.CursorStyle {
	return tcell.CursorStyleDefault
}

// Tty returns the underlying tty interface (nil for virtual screen).
func (vs *VirtualScreen) Tty() (tcell.Tty, bool) {
	return nil, false
}

// GetCells returns a copy of the internal cell array (reads from frontBuffer).
func (vs *VirtualScreen) GetCells(x, y, w int) []tcell.SimCell {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	if y < 0 || y >= vs.height {
		return nil
	}

	cells := make([]tcell.SimCell, 0, w)
	for i := 0; i < w && x+i < vs.width; i++ {
		cell := vs.frontBuffer[y][x+i]
		cells = append(cells, tcell.SimCell{
			Runes: []rune{cell.Ch},
			Style: cell.Style,
		})
	}
	return cells
}

// SetRuneInterrupt sets the interrupt handler for rune input (no-op).
func (vs *VirtualScreen) SetRuneInterrupt(f func(r rune) bool) {
	// No-op
}

// WatchFile watches a file descriptor for events (no-op).
func (vs *VirtualScreen) WatchFile(fd int) error {
	return nil
}

// StopWatchingFile stops watching a file descriptor (no-op).
func (vs *VirtualScreen) StopWatchingFile(fd int) {
	// No-op
}

// CharacterSet returns the character set (always UTF-8).
func (vs *VirtualScreen) CharacterSet() string {
	return "UTF-8"
}

// PostEventWait posts an event and waits for it to be processed.
func (vs *VirtualScreen) PostEventWait(ev tcell.Event) {
	vs.PostEvent(ev)
	// In a real implementation, might wait for ack
	time.Sleep(1 * time.Millisecond)
}

// GetClipboard returns the clipboard content (no-op for virtual screen).
func (vs *VirtualScreen) GetClipboard() {
	// No-op
}

// SetClipboard sets the clipboard content (no-op for virtual screen).
func (vs *VirtualScreen) SetClipboard(data []byte) {
	// No-op
}

// SetTitle sets the window title (no-op for virtual screen).
func (vs *VirtualScreen) SetTitle(title string) {
	// No-op
}
