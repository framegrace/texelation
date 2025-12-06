// Package esctest provides a Go-native test framework for terminal emulation compliance.
//
// This package is derived from esctest2 by George Nachman and Thomas E. Dickey.
// Original project: https://github.com/ThomasDickey/esctest2
// License: GPL v2
//
// The tests have been converted from Python to Go to enable offline, deterministic
// testing of the texelterm terminal emulator without requiring Python or PTY interaction.
package esctest

// Point represents a cursor position or coordinate (1-indexed, matching VT conventions).
type Point struct {
	X int
	Y int
}

// NewPoint creates a new Point with the given coordinates.
func NewPoint(x, y int) Point {
	return Point{X: x, Y: y}
}

// Rect represents a rectangular region on the terminal screen (1-indexed).
type Rect struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

// NewRect creates a new Rect with the given bounds.
func NewRect(left, top, right, bottom int) Rect {
	return Rect{Left: left, Top: top, Right: right, Bottom: bottom}
}

// Width returns the width of the rectangle.
func (r Rect) Width() int {
	return r.Right - r.Left + 1
}

// Height returns the height of the rectangle.
func (r Rect) Height() int {
	return r.Bottom - r.Top + 1
}

// Size represents dimensions in cells or pixels.
type Size struct {
	Width  int
	Height int
}

// NewSize creates a new Size with the given dimensions.
func NewSize(width, height int) Size {
	return Size{Width: width, Height: height}
}
