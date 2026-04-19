// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// TestStore_TruncateLine verifies the basic contract: truncate drops cells
// at or past col without growing the line past its current length.
func TestStore_TruncateLine(t *testing.T) {
	s := NewStore(80)
	fillRow(s, 0, "hello world", false)
	if got := len(s.GetLine(0)); got != 11 {
		t.Fatalf("setup: line length = %d, want 11", got)
	}

	s.TruncateLine(0, 5)
	got := s.GetLine(0)
	if len(got) != 5 {
		t.Errorf("after TruncateLine(5): length = %d, want 5", len(got))
	}
	if string(cellsToStringSparse(got)) != "hello" {
		t.Errorf("after TruncateLine(5): content = %q, want %q", cellsToStringSparse(got), "hello")
	}

	// Truncate past end is a no-op.
	s.TruncateLine(0, 100)
	if got := len(s.GetLine(0)); got != 5 {
		t.Errorf("TruncateLine past end should be no-op: length = %d, want 5", got)
	}

	// Truncate on absent line is a no-op.
	s.TruncateLine(999, 3)
	if got := s.GetLine(999); got != nil {
		t.Errorf("TruncateLine on absent line should stay absent, got %v", got)
	}

	// Truncate to 0 leaves an empty (but existing) line.
	s.TruncateLine(0, 0)
	if got := s.GetLine(0); len(got) != 0 {
		t.Errorf("TruncateLine(0): length = %d, want 0", len(got))
	}
}

// TestEraseToEndOfLine_DoesNotInflateLine is the core regression:
// previously EraseToEndOfLine padded every column from col..width-1 with
// Cell{}, so a two-char prompt at the viewport bottom ended up stored as a
// line of length=width. reflow then treated all those trailing blanks as
// content and the prompt row wrapped/unwrapped on resize. Now the stored
// length must track actual content.
func TestEraseToEndOfLine_DoesNotInflateLine(t *testing.T) {
	const cols = 80
	store := NewStore(cols)
	ww := NewWriteWindow(store, cols, 5)

	ww.WriteCell(parser.Cell{Rune: '>'})
	ww.WriteCell(parser.Cell{Rune: ' '})
	// Cursor now at (0, 2). Shell issues ESC[K to clear to EOL.
	_, col := ww.Cursor()
	ww.EraseToEndOfLine(col)

	line := store.GetLine(0)
	if len(line) != 2 {
		t.Fatalf("line length after ESC[K at col 2: got %d, want 2 (was %d under the padding bug)", len(line), cols)
	}
	if line[0].Rune != '>' || line[1].Rune != ' ' {
		t.Errorf("content corrupted: got %q", cellsToStringSparse(line))
	}

	// Erase at col=0 over an already-written line should empty it entirely.
	fillRow(store, 1, "garbage here", false)
	ww.WriteCell(parser.Cell{Rune: 'x'}) // advance cursor away from row 0
	// Move cursor to row 1, col 0 manually by writing a newline then rewinding is
	// complex; easier: use the store directly via truncate semantics.
	store.TruncateLine(1, 0)
	if got := store.GetLine(1); len(got) != 0 {
		t.Errorf("TruncateLine(1, 0) via direct call: length = %d, want 0", len(got))
	}
}

// TestEraseToEndOfLine_PreservesPrefix confirms that cells before col are kept
// and cells at or past col are dropped (so they read back as blank Cell{}).
func TestEraseToEndOfLine_PreservesPrefix(t *testing.T) {
	const cols = 20
	store := NewStore(cols)
	ww := NewWriteWindow(store, cols, 5)

	for _, r := range "abcdefghij" {
		ww.WriteCell(parser.Cell{Rune: r})
	}
	// Cursor at (0,10). Erase from col 4.
	ww.EraseToEndOfLine(4)

	line := store.GetLine(0)
	if len(line) != 4 {
		t.Fatalf("line length: got %d, want 4", len(line))
	}
	if cellsToStringSparse(line) != "abcd" {
		t.Errorf("prefix: got %q, want %q", cellsToStringSparse(line), "abcd")
	}
	// Cells past col read as blank.
	if got := store.Get(0, 5).Rune; got != 0 {
		t.Errorf("store.Get(0,5) past truncated line: rune=%q, want 0", got)
	}
}

// TestPromptLine_DoesNotReflowOnResize is the user-observable regression:
// a shell prompt at the bottom of the viewport, after ESC[K, must stay a
// single physical row when the viewport shrinks. Previously the prompt's
// stored line was width-inflated and would wrap into 2 rows at half width.
func TestPromptLine_DoesNotReflowOnResize(t *testing.T) {
	const wideCols = 80
	store := NewStore(wideCols)
	ww := NewWriteWindow(store, wideCols, 5)

	// Write a typical two-character prompt then ESC[K.
	ww.WriteCell(parser.Cell{Rune: '>'})
	ww.WriteCell(parser.Cell{Rune: ' '})
	_, col := ww.Cursor()
	ww.EraseToEndOfLine(col)

	// Walk the chain at row 0 — should terminate at row 0 (single row, not wrapped).
	end, nowrap := walkChain(store, 0, 256)
	if end != 0 {
		t.Fatalf("chain end: got %d, want 0 (prompt must be a single-row chain)", end)
	}

	// Render at half-width — prompt must still be exactly one row.
	const narrowCols = 40
	rows := chainReflowedRowCount(store, 0, end, narrowCols, nowrap)
	if rows != 1 {
		t.Errorf("prompt at viewWidth=%d: reflowed rows = %d, want 1", narrowCols, rows)
	}

	// And at an even narrower width.
	rows = chainReflowedRowCount(store, 0, end, 20, nowrap)
	if rows != 1 {
		t.Errorf("prompt at viewWidth=20: reflowed rows = %d, want 1", rows)
	}

	// And when widening back.
	rows = chainReflowedRowCount(store, 0, end, 80, nowrap)
	if rows != 1 {
		t.Errorf("prompt at viewWidth=80: reflowed rows = %d, want 1", rows)
	}
}

// TestGenuineWrappedContent_StillReflows guards against over-correction —
// real wrapped content (not trailing erase padding) must still reflow.
func TestGenuineWrappedContent_StillReflows(t *testing.T) {
	const cols = 80
	store := NewStore(cols)
	// Fill row 0 with 80 chars wrapping into row 1's first 5 chars.
	fillRow(store, 0, strings.Repeat("x", 80), true)
	fillRow(store, 1, "yyyyy", false)

	end, nowrap := walkChain(store, 0, 256)
	if end != 1 {
		t.Fatalf("chain should extend into row 1: end=%d, want 1", end)
	}
	if rows := chainReflowedRowCount(store, 0, end, 40, nowrap); rows != 3 {
		t.Errorf("chain at viewWidth=40: rows = %d, want 3 (80+5 chars = 3 rows of 40)", rows)
	}
	if rows := chainReflowedRowCount(store, 0, end, 80, nowrap); rows != 2 {
		t.Errorf("chain at viewWidth=80: rows = %d, want 2 (80+5 chars = 2 rows of 80)", rows)
	}
}
