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
	SingleClick ClickType = 1
	DoubleClick ClickType = 2
	TripleClick ClickType = 3
)

// DefaultMultiClickTimeout is the maximum time between clicks for multi-click detection.
const DefaultMultiClickTimeout = 500 * time.Millisecond

// ClickDetector tracks click timing and position to detect multi-clicks.
// It is reusable across any application that needs multi-click detection.
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
// Consecutive clicks at the same position within the timeout are counted as multi-clicks.
// The click count cycles: 1 → 2 → 3 → 1 (resets after triple-click).
func (c *ClickDetector) DetectClick(line, col int) ClickType {
	now := time.Now()
	samePosition := line == c.lastClickLine && col == c.lastClickCol
	withinTimeout := now.Sub(c.lastClickTime) < c.timeout

	if samePosition && withinTimeout {
		c.clickCount++
		// Reset to 1 after triple-click for continuous clicking
		if c.clickCount > 3 {
			c.clickCount = 1
		}
	} else {
		c.clickCount = 1
	}

	c.lastClickTime = now
	c.lastClickLine = line
	c.lastClickCol = col

	switch c.clickCount {
	case 1:
		return SingleClick
	case 2:
		return DoubleClick
	default:
		return TripleClick
	}
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

// ClickCount returns the current click count (1, 2, or 3).
func (c *ClickDetector) ClickCount() int {
	if c.clickCount == 0 {
		return 0
	}
	return c.clickCount
}
