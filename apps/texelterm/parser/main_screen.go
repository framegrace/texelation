// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

// MainScreen is the interface that sparse.Terminal satisfies. Defined in
// the parser package to avoid an import cycle (parser -> sparse -> parser).
// sparse.Terminal is the sole production implementation; VTerm drives the
// main-screen state exclusively through this interface.
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

	// SetLine and ClearRange are low-level store-manipulation primitives
	// that bypass the cursor / writeTop / HWM invariants. They exist for
	// operations outside the cursor-write model (erase, ICH/DCH, overlay
	// insert, scroll-region rejoin). Callers are responsible for keeping
	// the write window consistent with whatever they mutate.
	SetLine(globalIdx int64, cells []Cell)
	ClearRange(lo, hi int64)

	// IL/DL and partial scroll-region operations. cursorRow, marginTop, and
	// marginBottom are all relative to WriteTop (i.e., viewport-row indices).
	InsertLines(n, cursorRow, marginTop, marginBottom int)
	DeleteLines(n, cursorRow, marginTop, marginBottom int)
	// NewlineInRegion scrolls [marginTop, marginBottom] up by 1 without
	// advancing WriteTop. Use Newline() for full-screen line feeds.
	NewlineInRegion(marginTop, marginBottom int)
	Grid() [][]Cell

	// Scroll methods keep the sparse ViewWindow in sync with user
	// navigation. Without these, Grid() would always show the live edge.
	ScrollUp(n int)
	ScrollDown(n int)
	ScrollToBottom()
	OnInput()
	IsFollowing() bool

	// LoadFromPageStore populates the main screen with all lines currently
	// stored in the given PageStore. Used on session restore to replay
	// persistent scrollback into the sparse store.
	LoadFromPageStore(ps *PageStore) error

	// RestoreState forcibly sets the write window's cursor and anchor,
	// used during session restore to match the saved WAL metadata. hwm
	// seeds the writeBottom high-water mark; passing 0 or a value less
	// than writeTop+height-1 is equivalent to "unknown" and the
	// implementation falls back to deriving HWM from writeTop+height-1.
	RestoreState(writeTop, cursorGlobalIdx int64, cursorCol int, hwm int64)

	// WriteBottomHWM returns the high-water mark of writeBottom across
	// the session. Persisted into MainScreenState so that a grown
	// viewport on reload anchors against the true HWM rather than a
	// diminished value.
	WriteBottomHWM() int64

	// ReadLine returns a copy of the cells at globalIdx. Returns nil for gaps.
	ReadLine(globalIdx int64) []Cell

	// RowNoWrap reports whether the row at globalIdx is marked NoWrap.
	// Missing rows return false.
	RowNoWrap(globalIdx int64) bool

	// SetRowNoWrap marks the row at globalIdx as NoWrap. The flag is sticky
	// (passing false is a no-op). Called when DECSTBM is active.
	SetRowNoWrap(globalIdx int64, nowrap bool)

	// VisibleRange returns the (top, bottom) globalIdx pair of the current view.
	VisibleRange() (top, bottom int64)
}

// MainScreenFactory creates a MainScreen for the given dimensions. Set by
// the sparse package's init (via `import _ ".../parser/sparse"`). When nil
// — e.g., in parser-only unit tests that don't import sparse — no
// MainScreen is created and v.mainScreen stays nil; the MainScreen-gated
// methods on VTerm short-circuit in that case.
var MainScreenFactory func(width, height int) MainScreen
