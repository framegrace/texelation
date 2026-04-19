// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

type stubNotifier struct {
	ranges [][2]int64
}

func (s *stubNotifier) NotifyClearRange(lo, hi int64) {
	s.ranges = append(s.ranges, [2]int64{lo, hi})
}

// makeTestRow creates a row of width `w` filled with rune `r`.
func makeTestRow(w int, r rune) []parser.Cell {
	row := make([]parser.Cell, w)
	for i := 0; i < w; i++ {
		row[i] = parser.Cell{Rune: r}
	}
	return row
}

func TestTerminal_ClearRangePersistent_ClearsAndNotifies(t *testing.T) {
	term := NewTerminal(80, 24)
	// Seed a line at gi=5 so we can assert in-memory removal.
	term.SetLine(5, makeTestRow(80, 'x'))
	n := &stubNotifier{}
	term.SetClearNotifier(n)
	term.ClearRangePersistent(3, 7)
	if got := term.ReadLine(5); got != nil {
		t.Errorf("in-memory line 5 still present after clear: %v", got)
	}
	if len(n.ranges) != 1 || n.ranges[0] != [2]int64{3, 7} {
		t.Errorf("notifier ranges = %v, want [[3 7]]", n.ranges)
	}
}

func TestTerminal_ClearRangePersistent_NoNotifierIsFine(t *testing.T) {
	term := NewTerminal(80, 24)
	term.SetLine(5, makeTestRow(80, 'x'))
	// No SetClearNotifier call — ClearRangePersistent must not panic.
	term.ClearRangePersistent(3, 7)
	if got := term.ReadLine(5); got != nil {
		t.Error("in-memory line 5 still present")
	}
}
