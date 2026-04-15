// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/resize_cursor_sync_test.go
// Summary: Sparse-native ports of the non-reflow resize tests from
// vterm_memory_buffer_test.go. Covers:
//   - Short content that always fits in the new width (no wrap involved)
//   - Grow / shrink / height-only variants
//   - Cursor-to-grid consistency after each resize
//
// Tests dropped relative to pre-sparse:
//   - `TestVTerm_ResizeWidthWrapDiagnostic` — asserts a 45-char prompt wraps
//     to two physical rows when width decreases from 80 → 30. The sparse
//     store stores lines at their original written width; Grid() truncates
//     display to the current width. There is no display-time wrap: a
//     too-wide line is simply clipped. This test's wrap expectation is
//     fundamentally incompatible with sparse's no-reflow design.
//   - `TestResizeWidth_FullViewport_CursorGridConsistency` — exercises
//     rapid width changes down to width=5 with a prompt that is longer
//     than 5 cells. Same reason as above: the test's invariant ("last
//     non-empty grid row == cursor row") depends on reflow placing the
//     prompt's tail onto a new physical row, which sparse does not do.

package parser

import (
	"fmt"
	"strings"
	"testing"
)

// TestVTerm_MemoryBufferResize_ShortContent verifies that resizing with
// short content preserves it in the grid at the new dimensions.
func TestVTerm_MemoryBufferResize_ShortContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "Hello, World!\r\nLine 2")

	v.Resize(40, 12)

	grid := v.Grid()
	if len(grid) != 12 {
		t.Errorf("rows: got %d, want 12", len(grid))
	}
	if len(grid[0]) != 40 {
		t.Errorf("cols: got %d, want 40", len(grid[0]))
	}

	row0 := strings.TrimRight(gridRowToString(grid[0]), " ")
	if row0 != "Hello, World!" {
		t.Errorf("content lost after resize: row 0 = %q, want 'Hello, World!'", row0)
	}
}

// TestVTerm_ResizeBeforeContentCursorSync_Grow verifies that growing the
// terminal early (before much content is written) keeps the cursor on the
// same grid row as the prompt.
func TestVTerm_ResizeBeforeContentCursorSync_Grow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "user@host:~$ ")

	// Sanity: before resize, grid[cursorY] contains the prompt.
	if row := cellsToString(v.Grid()[v.cursorY]); !strings.Contains(row, "user@host") {
		t.Fatalf("before resize: grid[cursorY=%d] = %q, want prompt", v.cursorY, row)
	}

	v.Resize(120, 30)

	grid := v.Grid()
	cx, cy := v.Cursor()
	if cy < 0 || cy >= len(grid) {
		t.Fatalf("after grow: cursorY=%d out of bounds (grid=%d rows)", cy, len(grid))
	}
	if row := cellsToString(grid[cy]); !strings.Contains(row, "user@host:~$") {
		for y, r := range grid {
			s := strings.TrimRight(cellsToString(r), " ")
			if strings.Contains(s, "user@host:~$") {
				t.Errorf("CURSOR DESYNC: cursor row %d but prompt on row %d (cursor=%d,%d)", cy, y, cx, cy)
				return
			}
		}
		t.Errorf("after grow: grid[cursorY=%d] = %q, no row contained prompt", cy, row)
	}

	// After typing, the typed characters must extend the prompt row.
	parseString(p, "ls -la")
	cy3 := v.cursorY
	grid3 := v.Grid()
	if cy3 < 0 || cy3 >= len(grid3) {
		t.Fatalf("after typing: cursorY=%d out of bounds", cy3)
	}
	if row := cellsToString(grid3[cy3]); !strings.Contains(row, "user@host:~$ ls -la") {
		t.Errorf("after typing: grid[cursorY=%d] = %q, want prompt+command", cy3, row)
	}
}

// TestVTerm_ResizeBeforeContentCursorSync_Shrink verifies the same invariant
// when shrinking instead of growing.
func TestVTerm_ResizeBeforeContentCursorSync_Shrink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	v := NewVTerm(120, 30)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	parseString(p, "user@host:~$ ")

	v.Resize(80, 24)

	grid := v.Grid()
	cy := v.cursorY
	if cy < 0 || cy >= len(grid) {
		t.Fatalf("after shrink: cursorY=%d out of bounds (grid=%d rows)", cy, len(grid))
	}
	// Prompt is 13 chars and still fits in width 80 — content is not reflowed.
	if row := cellsToString(grid[cy])[:13]; row != "user@host:~$ " {
		for y, r := range grid {
			s := cellsToString(r)
			if len(s) >= 13 && s[:13] == "user@host:~$ " {
				t.Errorf("CURSOR DESYNC: cursor row %d but prompt on row %d", cy, y)
				return
			}
		}
		t.Errorf("after shrink: grid[cursorY=%d][:13] = %q, want 'user@host:~$ '", cy, row)
	}
}

// TestResize_HeightChangeCursorDesync verifies that height-only changes
// (decrease then increase) keep the cursor aligned with the prompt row.
// Height changes don't involve width reflow so this is squarely within the
// sparse model's responsibilities (Rule 5/6 from the design spec).
func TestResize_HeightChangeCursorDesync(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	width, height := 40, 24
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	prompt := "marc@host:~$ "
	parseString(p, prompt)

	// Sanity.
	if !strings.Contains(cellsToString(v.Grid()[v.cursorY]), "marc@host") {
		t.Fatalf("initial: grid[cursorY=%d] should contain prompt", v.cursorY)
	}

	// Height decrease (24 → 12).
	v.Resize(width, 12)
	if row := cellsToString(v.Grid()[v.cursorY]); !strings.Contains(row, "marc@host") {
		t.Errorf("after height 24→12: cursorY=%d, row=%q", v.cursorY, row)
	}

	// Height increase (12 → 24).
	v.Resize(width, 24)
	if row := cellsToString(v.Grid()[v.cursorY]); !strings.Contains(row, "marc@host") {
		t.Errorf("after height 12→24: cursorY=%d, row=%q", v.cursorY, row)
	}
}

// TestResize_HeightChangeCursorDesync_FullViewport verifies the same
// invariant when the viewport is FULL before the resize. This exercises
// the Rule-5 path where writeTop must adjust so the shell prompt stays
// anchored at the new bottom row.
func TestResize_HeightChangeCursorDesync_FullViewport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	width, height := 40, 24
	v := NewVTerm(width, height)
	v.EnableMemoryBuffer()
	p := NewParser(v)

	// Fill the viewport with more than height lines so it's full.
	for i := 0; i < 30; i++ {
		parseString(p, fmt.Sprintf("output %02d\r\n", i))
	}
	parseString(p, "user@host:~$ ")

	// Before resize, the prompt should be on the last non-empty row.
	grid0 := v.Grid()
	if !strings.Contains(cellsToString(grid0[v.cursorY]), "user@host") {
		t.Fatalf("before resize: grid[cursorY=%d] should contain prompt", v.cursorY)
	}

	// Shrink height. Shell prompt must stay anchored at the new bottom.
	v.Resize(width, 10)
	grid1 := v.Grid()
	if v.cursorY < 0 || v.cursorY >= len(grid1) {
		t.Fatalf("after shrink: cursorY=%d out of bounds (grid=%d rows)", v.cursorY, len(grid1))
	}
	if row := cellsToString(grid1[v.cursorY]); !strings.Contains(row, "user@host") {
		t.Errorf("after height shrink: grid[cursorY=%d] = %q, want prompt", v.cursorY, row)
	}
}
