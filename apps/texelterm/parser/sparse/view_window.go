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
