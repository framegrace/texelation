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
