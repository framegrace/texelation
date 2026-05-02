// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Tests for the origin-based viewport <-> content mapping.

package parser

import (
	"testing"
	"time"
)

func TestAdvanceCells_StaysInGid(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 30)
	for i := range cells {
		cells[i] = Cell{Rune: rune('a' + (i % 26))}
	}
	v.mainScreen.SetLine(5, cells)

	gid, col := v.advanceCells(5, 0, 10)
	if gid != 5 || col != 10 {
		t.Errorf("advanceCells(5,0,10) = (%d,%d), want (5,10)", gid, col)
	}
}

func TestAdvanceCells_CrossesGidBoundary(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	left := make([]Cell, 80)
	for i := range left {
		left[i] = Cell{Rune: 'a'}
	}
	left[79].Wrapped = true
	v.mainScreen.SetLine(5, left)
	right := make([]Cell, 60)
	for i := range right {
		right[i] = Cell{Rune: 'b'}
	}
	v.mainScreen.SetLine(6, right)

	gid, col := v.advanceCells(5, 50, 40)
	if gid != 6 || col != 10 {
		t.Errorf("advanceCells(5,50,40) = (%d,%d), want (6,10)", gid, col)
	}
}

func TestAdvanceCells_PastContentTerminates(t *testing.T) {
	// Regression: walking past the store's last written gid must not
	// loop indefinitely.
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	v.mainScreen.SetLine(5, []Cell{{Rune: 'a'}, {Rune: 'b'}})

	done := make(chan struct{})
	go func() {
		v.advanceCells(5, 5, 100)
		close(done)
	}()
	select {
	case <-done:
		// Returned cleanly.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("advanceCells did not terminate within 100ms — likely infinite loop")
	}
}

func TestCellsBetween_StaysInGid(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 30)
	for i := range cells {
		cells[i] = Cell{Rune: 'a'}
	}
	v.mainScreen.SetLine(5, cells)

	steps, ok := v.cellsBetween(5, 0, 5, 10, 80)
	if !ok || steps != 10 {
		t.Errorf("cellsBetween(5,0 -> 5,10) = (%d,%v), want (10,true)", steps, ok)
	}
}

func TestCellsBetween_CrossesGidBoundary(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	left := make([]Cell, 80)
	for i := range left {
		left[i] = Cell{Rune: 'a'}
	}
	left[79].Wrapped = true
	v.mainScreen.SetLine(5, left)
	right := make([]Cell, 60)
	for i := range right {
		right[i] = Cell{Rune: 'b'}
	}
	v.mainScreen.SetLine(6, right)

	// Origin (5, 50), target (6, 10), max 80 cells. Steps: 30 (rest of gid 5) + 10 = 40.
	steps, ok := v.cellsBetween(5, 50, 6, 10, 80)
	if !ok || steps != 40 {
		t.Errorf("cellsBetween(5,50 -> 6,10) = (%d,%v), want (40,true)", steps, ok)
	}
}

func TestCellsBetween_TargetOutOfRange(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 10)
	v.mainScreen.SetLine(5, cells)

	_, ok := v.cellsBetween(5, 0, 100, 0, 50)
	if ok {
		t.Errorf("cellsBetween(5,0 -> 100,0) returned ok=true; expected false")
	}
}

func TestCellsBetween_TargetBeforeOrigin(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	cells := make([]Cell, 30)
	v.mainScreen.SetLine(5, cells)

	_, ok := v.cellsBetween(5, 20, 5, 5, 80)
	if ok {
		t.Errorf("cellsBetween(5,20 -> 5,5) returned ok=true; expected false")
	}
}

func TestViewportToContent_WrappedContinuationRow(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// 100-char line wraps once at width 80, then resize to 40 forces
	// a mid-gid wrap continuation: row 0 starts at (gid=0, col=0),
	// row 1 starts at (gid=0, col=40) — same gid. This is precisely
	// the case naive math gets wrong.
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	v.Resize(40, 24)
	// Render so mainScreenRowOrigin is populated.
	_, _ = v.GridWithRowIdx()

	// Sanity-check the test is exercising the intended layout:
	// row 1 must be a mid-gid wrap continuation (Col != 0), otherwise
	// the test reduces to naive math and tells us nothing.
	if v.mainScreenRowOrigin[1].Col == 0 {
		t.Fatalf("test setup didn't produce a mid-gid wrap continuation at row 1; got %+v",
			v.mainScreenRowOrigin[1])
	}

	// Click at row 1, col 5. The naive implementation returns
	// (visibleTop+1, 5). The origin-based implementation must return
	// the cell-bearing (gid, col) — origin[1].Gid + col-walk-from-origin.
	gid, col, _, ok := v.ViewportToContent(1, 5)
	if !ok {
		t.Fatalf("ViewportToContent(1, 5) ok=false")
	}

	// Independently re-derive what the answer should be from the cached
	// origin slice — this gives a check that doesn't pre-suppose the
	// implementation, but does require the origin slice to be built
	// correctly (which Phase 1 covers).
	o := v.mainScreenRowOrigin[1]
	wantGid, wantCol := o.Gid, o.Col+5
	// If +5 crosses a gid boundary in the chain, expand:
	storeCells := v.mainScreen.ReadLine(o.Gid)
	if wantCol >= len(storeCells) {
		wantCol -= len(storeCells)
		wantGid++
	}
	if gid != wantGid || col != wantCol {
		t.Errorf("ViewportToContent(1,5)=(gid=%d,col=%d), want (%d,%d)",
			gid, col, wantGid, wantCol)
	}
}

func TestContentToViewport_RoundTripNonWrapped(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	p := NewParser(v)
	for i := 0; i < 5; i++ {
		for _, r := range "hello" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	_, _ = v.GridWithRowIdx()

	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			gid, col, _, ok := v.ViewportToContent(y, x)
			if !ok {
				t.Fatalf("ViewportToContent(%d,%d) ok=false", y, x)
			}
			ry, rx, vis := v.ContentToViewport(gid, col)
			if !vis {
				t.Errorf("(%d,%d) → (%d,%d) → not visible", y, x, gid, col)
				continue
			}
			if ry != y || rx != x {
				t.Errorf("round-trip (%d,%d) → (gid=%d,col=%d) → (%d,%d)",
					y, x, gid, col, ry, rx)
			}
		}
	}
}

func TestContentToViewport_RoundTripWrappedChain(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// 160-char single logical line that fully fills both wrap rows at
	// width 80. (A 100-char line leaves row 1 with only 20 cells, where
	// x=40,79 are past-content positions that legitimately collapse via
	// advanceCells's past-content terminator — that's not what this
	// test is about.)
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUV" // 62+62+32 = 156 chars
	for _, r := range long {
		p.Parse(r)
	}
	// Pad to exactly 160 chars.
	for _, r := range "WXYZ" {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	_, _ = v.GridWithRowIdx()

	// Round-trip every (y, x) on the first two rows (the wrap chain).
	// All positions are within rendered content.
	for _, y := range []int{0, 1} {
		for _, x := range []int{0, 5, 40, 79} {
			gid, col, _, ok := v.ViewportToContent(y, x)
			if !ok {
				t.Fatalf("ViewportToContent(%d,%d) failed", y, x)
			}
			ry, rx, vis := v.ContentToViewport(gid, col)
			if !vis {
				t.Errorf("(%d,%d) → (%d,%d) → not visible", y, x, gid, col)
				continue
			}
			if ry != y || rx != x {
				t.Errorf("round-trip (%d,%d) → (gid=%d,col=%d) → (%d,%d)",
					y, x, gid, col, ry, rx)
			}
		}
	}
}

func TestRenderedGridAgreesWithRowOrigin(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// Mix of short non-wrapping lines and a long wrapped line so the
	// origin slice has both per-gid and per-cell-offset entries.
	p := NewParser(v)
	for i := 0; i < 5; i++ {
		for _, r := range "short" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	for i := 0; i < 5; i++ {
		for _, r := range "after" {
			p.Parse(r)
		}
		p.Parse('\r')
		p.Parse('\n')
	}
	grid, _ := v.GridWithRowIdx()

	for y, o := range v.mainScreenRowOrigin {
		if o.Gid == -1 {
			continue
		}
		drewRune := grid[y][0].Rune
		storeCells := v.mainScreen.ReadLine(o.Gid)
		if o.Col >= len(storeCells) {
			t.Errorf("row %d: origin (%d,%d) points past store row (len=%d)",
				y, o.Gid, o.Col, len(storeCells))
			continue
		}
		want := storeCells[o.Col].Rune
		if drewRune != want {
			t.Errorf("row %d col 0: drew rune=%q, but origin (%d,%d) says store rune=%q",
				y, drewRune, o.Gid, o.Col, want)
		}
	}
}

func TestRenderedGridAgreesWithMapperInterior(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZab" // 100 chars
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	grid, _ := v.GridWithRowIdx()

	cases := []struct{ y, x int }{
		{0, 5}, {0, 79}, {1, 0}, {1, 15},
	}
	for _, c := range cases {
		gid, col, _, ok := v.ViewportToContent(c.y, c.x)
		if !ok {
			t.Errorf("(%d,%d): ViewportToContent failed", c.y, c.x)
			continue
		}
		drew := grid[c.y][c.x].Rune
		storeCells := v.mainScreen.ReadLine(gid)
		if col >= len(storeCells) {
			t.Errorf("(%d,%d): mapped (%d,%d) past store len=%d",
				c.y, c.x, gid, col, len(storeCells))
			continue
		}
		want := storeCells[col].Rune
		if drew != want {
			t.Errorf("(%d,%d): drew %q, mapper says (%d,%d)→%q",
				c.y, c.x, drew, gid, col, want)
		}
	}
}
