// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser_test

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	_ "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
)

func feedBytesReflow(p *parser.Parser, data []byte) {
	for _, b := range string(data) {
		p.Parse(b)
	}
}

// Fill a line longer than 80, resize narrower, verify it reflows.
func TestVTerm_Reflow_WidenAndNarrow(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	p := parser.NewParser(v)
	long := strings.Repeat("abcdefghij", 12) // 120 chars
	feedBytesReflow(p, []byte(long))

	grid := v.Grid()
	row0 := cellsToStringParserReflow(grid[0])
	if !strings.HasPrefix(row0, "abcdefghij") {
		t.Fatalf("row 0 at width 80: %q", row0)
	}

	v.Resize(40, 24)
	grid = v.Grid()
	joined := cellsToStringParserReflow(grid[0]) + cellsToStringParserReflow(grid[1]) + cellsToStringParserReflow(grid[2])
	if !strings.Contains(joined, long) {
		t.Errorf("after narrow resize, content did not reflow; joined=%q", joined)
	}

	v.Resize(120, 24)
	grid = v.Grid()
	row0 = cellsToStringParserReflow(grid[0])
	if !strings.HasPrefix(row0, long) {
		t.Errorf("after widen, single row should hold full line; got %q", row0)
	}
}

func TestVTerm_Reflow_NoWrapRowsSurviveResize(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	p := parser.NewParser(v)
	feedBytesReflow(p, []byte("\x1b[2;5r"))
	feedBytesReflow(p, []byte("\x1b[HABCDE"))
	feedBytesReflow(p, []byte("\x1b[r"))
	v.Resize(3, 24)
	grid := v.Grid()
	if !strings.HasPrefix(cellsToStringParserReflow(grid[0]), "ABC") {
		t.Errorf("NoWrap row should clip at width 3, got %q", cellsToStringParserReflow(grid[0]))
	}
}

func cellsToStringParserReflow(cells []parser.Cell) string {
	b := strings.Builder{}
	for _, c := range cells {
		if c.Rune == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}
