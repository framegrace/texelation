// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

// MainScreen is the interface that sparse.Terminal satisfies. Defined in
// the parser package to avoid an import cycle (parser -> sparse -> parser).
// VTerm holds a MainScreen and dual-writes to it during the transition;
// after integration the legacy memBufState path is deleted and MainScreen
// becomes the sole main-screen implementation.
type MainScreen interface {
	WriteCell(cell Cell)
	Newline()
	CarriageReturn()
	SetCursor(row, col int)
	Cursor() (globalIdx int64, col int)
	CursorRow() int
	WriteTop() int64
	WriteBottom() int64
	ContentEnd() int64
	Resize(newWidth, newHeight int)
	EraseDisplay()
	EraseLine()
	EraseToEndOfLine(col int)
	EraseFromStartOfLine(col int)
	SetLine(globalIdx int64, cells []Cell)
	Grid() [][]Cell

	// LoadFromPageStore populates the main screen with all lines currently
	// stored in the given PageStore. Used on session restore to replay
	// persistent scrollback into the sparse store.
	LoadFromPageStore(ps *PageStore) error

	// RestoreState forcibly sets the write window's cursor and anchor,
	// used during session restore to match the saved WAL metadata.
	RestoreState(writeTop, cursorGlobalIdx int64, cursorCol int)
}

// MainScreenFactory creates a MainScreen for the given dimensions.
// Set by the sparse package's init or by the application layer.
// If nil, no MainScreen is created and the legacy path is used alone.
var MainScreenFactory func(width, height int) MainScreen
