// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/click_detector.go
// Summary: Reusable multi-click detection with configurable timeout.

package texelterm

import "time"

// ClickType represents the type of click detected.
type ClickType int

const (
	SingleClick    ClickType = 1
	DoubleClick    ClickType = 2
	TripleClick    ClickType = 3
	QuadrupleClick ClickType = 4
	QuintupleClick ClickType = 5

	// MaxClickType is the highest detectable multi-click count.
	MaxClickType = QuintupleClick
)

// DefaultMultiClickTimeout is the maximum time between clicks for multi-click detection.
const DefaultMultiClickTimeout = 500 * time.Millisecond

// ClickDetector tracks click timing and position to detect multi-clicks.
// It is reusable across any application that needs multi-click detection.
//
// The count saturates at MaxClickType — additional clicks within the
// window stay at MaxClickType rather than cycling back to SingleClick.
// This keeps "select-the-largest-thing" gestures (quintuple-click for
// the whole command) stable under sloppy fast clicking; the user can
// always pause to drop back to a single click.
type ClickDetector struct {
	timeout       time.Duration
	lastClickTime time.Time
	lastClickLine int
	lastClickCol  int
	clickCount    int
}

// NewClickDetector creates a new click detector with the specified timeout.
// Use DefaultMultiClickTimeout for standard terminal behavior.
func NewClickDetector(timeout time.Duration) *ClickDetector {
	return &ClickDetector{
		timeout: timeout,
	}
}

// DetectClick analyzes a click at the given position and returns the click type.
// Consecutive clicks at the same position within the timeout are counted as
// multi-clicks; the count saturates at MaxClickType.
func (c *ClickDetector) DetectClick(line, col int) ClickType {
	now := time.Now()
	samePosition := line == c.lastClickLine && col == c.lastClickCol
	withinTimeout := now.Sub(c.lastClickTime) < c.timeout

	if samePosition && withinTimeout {
		c.clickCount++
		if c.clickCount > int(MaxClickType) {
			c.clickCount = int(MaxClickType)
		}
	} else {
		c.clickCount = 1
	}

	c.lastClickTime = now
	c.lastClickLine = line
	c.lastClickCol = col

	return ClickType(c.clickCount)
}

// Reset clears the click history, causing the next click to be treated as single-click.
func (c *ClickDetector) Reset() {
	c.clickCount = 0
	c.lastClickTime = time.Time{}
	c.lastClickLine = 0
	c.lastClickCol = 0
}

// LastClickPosition returns the position of the last detected click.
func (c *ClickDetector) LastClickPosition() (line, col int) {
	return c.lastClickLine, c.lastClickCol
}

// LastClickTime returns the time of the last detected click.
func (c *ClickDetector) LastClickTime() time.Time {
	return c.lastClickTime
}

// ClickCount returns the current click count (0 before any click, 1..MaxClickType).
func (c *ClickDetector) ClickCount() int {
	return c.clickCount
}
