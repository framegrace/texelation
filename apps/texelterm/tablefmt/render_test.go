// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func cellsToString(cells []parser.Cell) string {
	rs := make([]rune, len(cells))
	for i, c := range cells {
		rs[i] = c.Rune
	}
	return string(rs)
}

func TestRenderTable_BasicMD(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: 0,
		rows:      [][]string{{"Name", "City"}, {"Alice", "New York"}, {"Bob", "London"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// top + header + sep + 2 data + bottom = 6
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d", len(lines))
	}
	top := []rune(cellsToString(lines[0]))
	if top[0] != '╭' {
		t.Errorf("top start: expected ╭, got %c", top[0])
	}
	if top[len(top)-1] != '╮' {
		t.Errorf("top end: expected ╮, got %c", top[len(top)-1])
	}
	bot := []rune(cellsToString(lines[5]))
	if bot[0] != '╰' {
		t.Errorf("bottom start: expected ╰, got %c", bot[0])
	}
	if bot[len(bot)-1] != '╯' {
		t.Errorf("bottom end: expected ╯, got %c", bot[len(bot)-1])
	}
	header := []rune(cellsToString(lines[1]))
	if header[0] != '│' {
		t.Errorf("header start: expected │, got %c", header[0])
	}
	sep := []rune(cellsToString(lines[2]))
	if sep[0] != '├' {
		t.Errorf("sep start: expected ├, got %c", sep[0])
	}
}

func TestRenderTable_RightAlignment(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignRight}},
		headerRow: 0,
		rows:      [][]string{{"Item", "Price"}, {"Apple", "1.50"}, {"Banana", "20.00"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// Check data row with "1.50" is right-aligned (has leading spaces).
	dataRow := cellsToString(lines[3])
	t.Logf("data row: %q", dataRow)
	// "Price" column width = 5 ("Price" and "20.00" are both 5 chars).
	// "1.50" right-aligned in 5 chars = " 1.50".
	// With padding: "│ " + " 1.50" + " │"
	// Verify the value is right-aligned by checking the rune content.
	runes := []rune(dataRow)
	// Find the second │ (after the first column).
	secondBorder := -1
	borderCount := 0
	for i, r := range runes {
		if r == '│' {
			borderCount++
			if borderCount == 2 {
				secondBorder = i
				break
			}
		}
	}
	if secondBorder < 0 {
		t.Fatal("could not find second border")
	}
	// After the second │, the content should be: space + " 1.50" + space + │
	// That means runes[secondBorder+1] = ' ' (padding), runes[secondBorder+2] = ' ' (alignment pad).
	if runes[secondBorder+1] != ' ' || runes[secondBorder+2] != ' ' {
		t.Errorf("expected leading space for right-aligned '1.50', got %q",
			string(runes[secondBorder+1:secondBorder+3]))
	}
}

func TestRenderTable_NoHeader(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"a", "b"}, {"c", "d"}},
		tableType: tableSpaceAligned,
	}
	lines := renderTable(ts)
	// top + 2 data + bottom = 4 (no separator)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
}

func TestRenderTable_UniformWidth(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: 0,
		rows:      [][]string{{"Short", "LongerValue"}, {"A", "B"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	width := len(lines[0])
	for i, line := range lines {
		if len(line) != width {
			t.Errorf("line %d: width %d != expected %d", i, len(line), width)
		}
	}
}

func TestRenderTable_DimBorders(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"data"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	for _, cell := range lines[0] { // top border
		if cell.Attr&parser.AttrDim == 0 {
			t.Errorf("border char %c should have dim attribute", cell.Rune)
		}
	}
}

func TestRenderTable_CenterAlignment(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignCenter}},
		headerRow: -1,
		rows:      [][]string{{"ab"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	if len(lines) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestRenderTable_EmptyInput(t *testing.T) {
	if renderTable(nil) != nil {
		t.Error("nil input should return nil")
	}
	if renderTable(&tableStructure{}) != nil {
		t.Error("empty structure should return nil")
	}
}

func TestRenderTable_ContentCellsNoAttr(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"hello"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// lines[1] is the data row: │ hello │
	// Content cells (indices 1..6 = space + h,e,l,l,o + space) should have no attr.
	dataRow := lines[1]
	// Skip first cell (border) and last cell (border).
	for i := 1; i < len(dataRow)-1; i++ {
		if dataRow[i].Attr != 0 {
			t.Errorf("content cell %d (%c) should have no attribute, got %s",
				i, dataRow[i].Rune, dataRow[i].Attr)
		}
	}
	// Border cells should have dim.
	if dataRow[0].Attr&parser.AttrDim == 0 {
		t.Error("left border should have dim attribute")
	}
	if dataRow[len(dataRow)-1].Attr&parser.AttrDim == 0 {
		t.Error("right border should have dim attribute")
	}
}

func TestRenderTable_MultiColumnStructure(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignRight}, {align: alignCenter}},
		headerRow: 0,
		rows: [][]string{
			{"Name", "Score", "Grade"},
			{"Alice", "95", "A"},
			{"Bob", "80", "B+"},
		},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	// top + header + sep + 2 data + bottom = 6
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d", len(lines))
	}

	// All lines should have the same width.
	width := len(lines[0])
	for i, line := range lines {
		if len(line) != width {
			t.Errorf("line %d: width %d != expected %d", i, len(line), width)
		}
	}

	// Log for visual inspection.
	for i, line := range lines {
		t.Logf("line %d: %q", i, cellsToString(line))
	}
}

func TestRenderTable_FewerCellsThanColumns(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}, {align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"a", "b", "c"}, {"x"}}, // second row missing cells
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	if len(lines) != 4 { // top + 2 data + bottom
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	// All lines should still have uniform width.
	width := len(lines[0])
	for i, line := range lines {
		if len(line) != width {
			t.Errorf("line %d: width %d != expected %d", i, len(line), width)
		}
	}
}
