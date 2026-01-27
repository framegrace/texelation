// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/fixed_width_detector.go
// Summary: FixedWidthDetector automatically flags lines as fixed-width (non-reflowable).
//
// Architecture:
//
//	TUI applications like vim, htop, codex use escape sequences that indicate
//	fixed-width content: scroll regions, cursor jumps, and cursor hiding.
//	This detector watches for these patterns and flags affected lines in
//	MemoryBuffer so they won't reflow on terminal resize.
//
//	Detection signals:
//	  - Non-full-screen scroll region (DECSTBM)
//	  - Large cursor jumps (CUP moving more than 1 row)
//	  - Cursor visibility changes (DECTCEM)
//
//	Lines are flagged immediately via MemoryBuffer.SetLineFixed(lineIdx, width).

package parser

import "strconv"

// FixedWidthDetectorConfig holds configuration for detection behavior.
type FixedWidthDetectorConfig struct {
	// JumpThreshold is how many consecutive cursor jumps trigger TUI detection.
	// Default: 2
	JumpThreshold int

	// MinJumpDistance is the minimum rows to count as a "jump".
	// Movements of 0-1 rows are considered normal (linefeed, same line).
	// Default: 2
	MinJumpDistance int

	// FlagOnScrollRegion flags all lines written within a non-full-screen
	// scroll region as fixed-width.
	// Default: true
	FlagOnScrollRegion bool

	// FlagOnCursorJumps flags lines at the destination of cursor jumps
	// when jump threshold is reached.
	// Default: true
	FlagOnCursorJumps bool
}

// DefaultFixedWidthDetectorConfig returns sensible defaults.
func DefaultFixedWidthDetectorConfig() FixedWidthDetectorConfig {
	return FixedWidthDetectorConfig{
		JumpThreshold:      2,
		MinJumpDistance:    2,
		FlagOnScrollRegion: true,
		FlagOnCursorJumps:  true,
	}
}

// FixedWidthDetector tracks TUI patterns and flags lines as fixed-width.
// It monitors cursor movements and scroll regions to detect TUI behavior,
// then marks affected lines in MemoryBuffer as non-reflowable.
type FixedWidthDetector struct {
	memBuf *MemoryBuffer
	config FixedWidthDetectorConfig

	// Scroll region state
	inScrollRegion bool
	termHeight     int
	termWidth      int

	// Cursor jump tracking
	lastCursorY      int
	consecutiveJumps int

	// Cursor visibility (diagnostic only)
	cursorHidden bool

	// Accumulated TUI signals for diagnostics
	signalCount int
}

// NewFixedWidthDetector creates a new detector with default configuration.
func NewFixedWidthDetector(memBuf *MemoryBuffer) *FixedWidthDetector {
	return NewFixedWidthDetectorWithConfig(memBuf, DefaultFixedWidthDetectorConfig())
}

// NewFixedWidthDetectorWithConfig creates a new detector with custom configuration.
func NewFixedWidthDetectorWithConfig(memBuf *MemoryBuffer, config FixedWidthDetectorConfig) *FixedWidthDetector {
	if config.JumpThreshold <= 0 {
		config.JumpThreshold = 2
	}
	if config.MinJumpDistance <= 0 {
		config.MinJumpDistance = 2
	}

	return &FixedWidthDetector{
		memBuf:     memBuf,
		config:     config,
		termHeight: DefaultHeight,
		termWidth:  DefaultWidth,
	}
}

// --- Event Handlers (called by VTerm) ---

// OnCursorMove is called when the cursor moves to a new row.
// Large jumps indicate TUI behavior; consecutive jumps trigger fixed-width flagging.
func (d *FixedWidthDetector) OnCursorMove(newY int) {
	jump := newY - d.lastCursorY
	if jump < 0 {
		jump = -jump
	}

	if jump >= d.config.MinJumpDistance {
		d.consecutiveJumps++
		d.signalCount++

		if d.config.FlagOnCursorJumps && d.consecutiveJumps >= d.config.JumpThreshold {
			d.flagCurrentLine()
		}
	} else if jump == 0 {
		d.consecutiveJumps = 0
	}

	d.lastCursorY = newY
}

// OnWrite is called when a character is written at the given line.
// If inside a non-full-screen scroll region, the line is flagged.
func (d *FixedWidthDetector) OnWrite(lineIdx int64, width int) {
	if d.config.FlagOnScrollRegion && d.inScrollRegion {
		d.setLineFixed(lineIdx, width)
	}
}

// OnScrollRegionSet is called when DECSTBM sets scroll margins.
// Non-full-screen scroll regions indicate TUI mode.
func (d *FixedWidthDetector) OnScrollRegionSet(top, bottom, height int) {
	d.termHeight = height

	isFullScreen := (top == 0 && bottom == height-1)
	if isFullScreen {
		d.inScrollRegion = false
		return
	}

	d.inScrollRegion = true
	d.signalCount++
}

// OnScrollRegionClear is called when scroll region is reset to full screen.
func (d *FixedWidthDetector) OnScrollRegionClear() {
	d.inScrollRegion = false
}

// OnCursorVisibilityChange is called when cursor visibility changes.
// Cursor hiding is common in TUI apps but used as a supporting signal only.
func (d *FixedWidthDetector) OnCursorVisibilityChange(hidden bool) {
	if d.cursorHidden != hidden {
		d.cursorHidden = hidden
		if hidden {
			d.signalCount++
		}
	}
}

// OnResize is called when the terminal is resized.
func (d *FixedWidthDetector) OnResize(width, height int) {
	d.termWidth = width
	d.termHeight = height
}

// --- Manual Control ---

// ForceFixedWidth explicitly marks a line as fixed-width.
func (d *FixedWidthDetector) ForceFixedWidth(lineIdx int64, width int) {
	d.setLineFixed(lineIdx, width)
}

// ClearFixedWidth removes the fixed-width flag from a line.
func (d *FixedWidthDetector) ClearFixedWidth(lineIdx int64) {
	if d.memBuf != nil {
		line := d.memBuf.GetLine(lineIdx)
		if line != nil {
			line.FixedWidth = 0
		}
	}
}

// --- Status Methods ---

// IsInTUIMode returns whether the detector thinks we're in TUI mode.
// This is true if a non-full-screen scroll region is active or if
// consecutive cursor jumps indicate TUI behavior.
func (d *FixedWidthDetector) IsInTUIMode() bool {
	return d.inScrollRegion || d.consecutiveJumps >= d.config.JumpThreshold
}

// InScrollRegion returns whether a non-full-screen scroll region is active.
func (d *FixedWidthDetector) InScrollRegion() bool {
	return d.inScrollRegion
}

// ConsecutiveJumps returns the current count of consecutive cursor jumps.
func (d *FixedWidthDetector) ConsecutiveJumps() int {
	return d.consecutiveJumps
}

// SignalCount returns the total number of TUI signals received.
func (d *FixedWidthDetector) SignalCount() int {
	return d.signalCount
}

// CursorHidden returns whether the cursor is currently hidden.
func (d *FixedWidthDetector) CursorHidden() bool {
	return d.cursorHidden
}

// Config returns a copy of the detector configuration.
func (d *FixedWidthDetector) Config() FixedWidthDetectorConfig {
	return d.config
}

// String returns a debug string showing detector state.
func (d *FixedWidthDetector) String() string {
	return "FixedWidthDetector{" +
		"inScrollRegion=" + strconv.FormatBool(d.inScrollRegion) +
		", consecutiveJumps=" + strconv.Itoa(d.consecutiveJumps) +
		", cursorHidden=" + strconv.FormatBool(d.cursorHidden) +
		", signalCount=" + strconv.Itoa(d.signalCount) +
		"}"
}

// --- Internal Helpers ---

// setLineFixed safely flags a line as fixed-width if memBuf is available.
func (d *FixedWidthDetector) setLineFixed(lineIdx int64, width int) {
	if d.memBuf != nil {
		d.memBuf.SetLineFixed(lineIdx, width)
	}
}

// flagCurrentLine flags the current cursor line as fixed-width.
func (d *FixedWidthDetector) flagCurrentLine() {
	if d.memBuf == nil {
		return
	}
	d.setLineFixed(d.memBuf.CursorLine(), d.memBuf.TermWidth())
}
