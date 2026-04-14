// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import "sync"

// ViewWindow is the user-facing portion of a sparse terminal. It owns the
// viewBottom anchor and the autoFollow flag, and it responds to write-window
// events when following.
//
// ViewWindow does not read from the Store directly — it only tracks the
// coordinate pair (viewTop, viewBottom) for the caller to project.
// ViewWindow is safe for concurrent use.
type ViewWindow struct {
	mu         sync.Mutex
	width      int
	height     int
	viewBottom int64
	autoFollow bool
}

// NewViewWindow creates a ViewWindow in autoFollow mode. viewBottom starts
// at height-1 so a fresh terminal projects rows [0, height-1].
func NewViewWindow(width, height int) *ViewWindow {
	return &ViewWindow{
		width:      width,
		height:     height,
		viewBottom: int64(height - 1),
		autoFollow: true,
	}
}

// Width returns the current column width.
func (v *ViewWindow) Width() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.width
}

// Height returns the current row height.
func (v *ViewWindow) Height() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.height
}

// IsFollowing reports whether the view is tracking the write window.
func (v *ViewWindow) IsFollowing() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.autoFollow
}

// VisibleRange returns the (top, bottom) globalIdx pair that the caller
// should project from the Store.
func (v *ViewWindow) VisibleRange() (top, bottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.viewBottom - int64(v.height) + 1, v.viewBottom
}

// OnWriteBottomChanged is called when the bottom of the write window moves.
// newWriteBottom is the new WriteWindow.WriteBottom() value. If autoFollow
// is true, viewBottom advances to match — but never retreats. A resize that
// shrinks writeBottom must not pull viewBottom back; the view stays anchored
// until new content pushes past the old position.
func (v *ViewWindow) OnWriteBottomChanged(newWriteBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.autoFollow && newWriteBottom > v.viewBottom {
		v.viewBottom = newWriteBottom
	}
}

// OnWriteTopChanged is called when the WriteWindow retreats its top on grow
// (i.e. the write window expands upward). Despite the name referring to the
// top, callers must pass newWriteBottom — the new WriteWindow.WriteBottom()
// value — because that is what viewBottom tracks. Only advances viewBottom
// forward, never retreats it.
func (v *ViewWindow) OnWriteTopChanged(newWriteBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.autoFollow && newWriteBottom > v.viewBottom {
		v.viewBottom = newWriteBottom
	}
}

// ScrollUp detaches from the live edge and moves viewBottom up by n lines.
// viewBottom is clamped to at least height-1 (can't show negative globalIdxs
// as the view bottom).
func (v *ViewWindow) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = false
	v.viewBottom -= int64(n)
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
}

// ScrollDown moves viewBottom down by n lines toward the live edge. writeBottom
// is the current WriteWindow bottom; ScrollDown will not move past it. If
// viewBottom reaches writeBottom, autoFollow is automatically re-engaged.
func (v *ViewWindow) ScrollDown(n int, writeBottom int64) {
	if n <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom += int64(n)
	if v.viewBottom >= writeBottom {
		v.viewBottom = writeBottom
		v.autoFollow = true
	}
}

// ScrollToBottom snaps viewBottom to writeBottom and re-engages autoFollow.
func (v *ViewWindow) ScrollToBottom(writeBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom = writeBottom
	v.autoFollow = true
}

// OnInput is called when the user types or clicks in the pane. Re-engages
// autoFollow at the current writeBottom.
func (v *ViewWindow) OnInput(writeBottom int64) {
	v.ScrollToBottom(writeBottom)
}

// Resize changes the viewport dimensions. When autoFollow is active,
// viewBottom snaps to the write window's bottom so that the view always
// shows the same range the shell writes into. When scrolled back
// (autoFollow off), viewBottom stays fixed — the user's scroll position
// is preserved.
func (v *ViewWindow) Resize(newWidth, newHeight int, newWriteBottom int64) {
	if newWidth <= 0 || newHeight <= 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.width = newWidth
	v.height = newHeight
	if v.autoFollow {
		v.viewBottom = newWriteBottom
	}
	// If expansion would show negative globalIdxs, raise viewBottom.
	minBottom := int64(v.height - 1)
	if v.viewBottom < minBottom {
		v.viewBottom = minBottom
	}
}
