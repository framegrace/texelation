// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestClassifyColumn_Number(t *testing.T) {
	values := []string{"42", "100", "7", "1,234"}
	got := classifyValues(values)
	if got != colNumber {
		t.Errorf("expected colNumber, got %d", got)
	}
}

func TestClassifyColumn_NumberWithPercent(t *testing.T) {
	values := []string{"42%", "100%", "7%"}
	got := classifyValues(values)
	if got != colNumber {
		t.Errorf("expected colNumber, got %d", got)
	}
}

func TestClassifyColumn_NegativeNumber(t *testing.T) {
	values := []string{"-42", "100", "-7", "0"}
	got := classifyValues(values)
	if got != colNumber {
		t.Errorf("expected colNumber, got %d", got)
	}
}

func TestClassifyColumn_DateTime(t *testing.T) {
	values := []string{"5d", "3d", "10d", "2d"}
	got := classifyValues(values)
	if got != colDateTime {
		t.Errorf("expected colDateTime, got %d", got)
	}
}

func TestClassifyColumn_DateTimeISO(t *testing.T) {
	values := []string{"2024-01-15", "2024-02-20", "2024-03-10"}
	got := classifyValues(values)
	if got != colDateTime {
		t.Errorf("expected colDateTime, got %d", got)
	}
}

func TestClassifyColumn_DateTimeHHMM(t *testing.T) {
	values := []string{"12:30", "08:45", "23:59:59"}
	got := classifyValues(values)
	if got != colDateTime {
		t.Errorf("expected colDateTime, got %d", got)
	}
}

func TestClassifyColumn_Path(t *testing.T) {
	values := []string{"/etc/nginx.conf", "/etc/redis.conf", "/var/lib/pg"}
	got := classifyValues(values)
	if got != colPath {
		t.Errorf("expected colPath, got %d", got)
	}
}

func TestClassifyColumn_PathDotfile(t *testing.T) {
	values := []string{".bashrc", ".gitignore", ".vimrc"}
	got := classifyValues(values)
	if got != colPath {
		t.Errorf("expected colPath, got %d", got)
	}
}

func TestClassifyColumn_PathFilename(t *testing.T) {
	values := []string{"main.go", "server.py", "config.yaml"}
	got := classifyValues(values)
	if got != colPath {
		t.Errorf("expected colPath, got %d", got)
	}
}

func TestClassifyColumn_Text(t *testing.T) {
	values := []string{"Running", "Pending", "Running"}
	got := classifyValues(values)
	if got != colText {
		t.Errorf("expected colText, got %d", got)
	}
}

func TestClassifyColumn_MixedDefaultsToText(t *testing.T) {
	values := []string{"42", "hello", "/etc/foo", "2024-01-01", "world"}
	got := classifyValues(values)
	if got != colText {
		t.Errorf("expected colText for mixed values, got %d", got)
	}
}

func TestClassifyColumn_EmptyValues(t *testing.T) {
	values := []string{"", "", "-", "<none>"}
	got := classifyValues(values)
	if got != colText {
		t.Errorf("expected colText for empty/placeholder values, got %d", got)
	}
}

func TestClassifyColumn_NilSlice(t *testing.T) {
	got := classifyValues(nil)
	if got != colText {
		t.Errorf("expected colText for nil, got %d", got)
	}
}

func TestClassifyAndColorize_HeaderBoldCyan(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: 0,
		rows:      [][]string{{"Name", "Age"}, {"Alice", "30"}, {"Bob", "25"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	classifyAndColorize(ts, lines)

	// Line 1 is the header data row (line 0 is top border).
	headerLine := lines[1]
	for _, c := range headerLine {
		if c.Attr&parser.AttrDim != 0 {
			continue // border cell
		}
		if c.Rune == ' ' {
			continue // padding
		}
		if c.FG != colorCyan {
			t.Errorf("header cell %c: expected cyan FG, got %+v", c.Rune, c.FG)
		}
		if c.Attr&parser.AttrBold == 0 {
			t.Errorf("header cell %c: expected bold attribute", c.Rune)
		}
	}
}

func TestClassifyAndColorize_NumberColumn(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignRight}},
		headerRow: 0,
		rows:      [][]string{{"Name", "Count"}, {"Alice", "42"}, {"Bob", "100"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	classifyAndColorize(ts, lines)

	// Lines layout: 0=top, 1=header, 2=sep, 3=Alice row, 4=Bob row, 5=bottom.
	// Check data row (line 3) — second column should be yellow.
	dataRow := lines[3]
	colIdx := -1
	for _, c := range dataRow {
		if c.Attr&parser.AttrDim != 0 && c.Rune == '│' {
			colIdx++
			continue
		}
		if colIdx == 1 && c.Rune != ' ' {
			if c.FG != colorYellow {
				t.Errorf("number cell %c: expected yellow FG, got %+v", c.Rune, c.FG)
			}
		}
	}
}

func TestClassifyAndColorize_TextColumnNoColor(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}},
		headerRow: 0,
		rows:      [][]string{{"Status"}, {"Running"}, {"Pending"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	classifyAndColorize(ts, lines)

	// Line 3 is first data row after header + separator.
	dataRow := lines[3]
	for _, c := range dataRow {
		if c.Attr&parser.AttrDim != 0 {
			continue // border
		}
		if c.Rune == ' ' {
			continue // padding
		}
		if c.FG != parser.DefaultFG {
			t.Errorf("text cell %c: expected default FG, got %+v", c.Rune, c.FG)
		}
	}
}

func TestClassifyAndColorize_PathColumn(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: 0,
		rows:      [][]string{{"Name", "File"}, {"srv", "/etc/nginx.conf"}, {"db", "/var/lib/pg"}},
		tableType: tableMarkdown,
	}
	lines := renderTable(ts)
	classifyAndColorize(ts, lines)

	// Check data row (line 3) — second column should be green.
	dataRow := lines[3]
	colIdx := -1
	for _, c := range dataRow {
		if c.Attr&parser.AttrDim != 0 && c.Rune == '│' {
			colIdx++
			continue
		}
		if colIdx == 1 && c.Rune != ' ' {
			if c.FG != colorGreen {
				t.Errorf("path cell %c: expected green FG, got %+v", c.Rune, c.FG)
			}
		}
	}
}

func TestClassifyAndColorize_NoHeader(t *testing.T) {
	ts := &tableStructure{
		columns:   []columnInfo{{align: alignLeft}, {align: alignLeft}},
		headerRow: -1,
		rows:      [][]string{{"42", "/etc/foo"}, {"100", "/var/bar"}},
		tableType: tableSpaceAligned,
	}
	lines := renderTable(ts)
	classifyAndColorize(ts, lines)

	// Line 1 is the first data row (no header, no separator).
	dataRow := lines[1]
	colIdx := -1
	foundYellow := false
	foundGreen := false
	for _, c := range dataRow {
		if c.Attr&parser.AttrDim != 0 && c.Rune == '│' {
			colIdx++
			continue
		}
		if c.Rune == ' ' {
			continue
		}
		if colIdx == 0 && c.FG == colorYellow {
			foundYellow = true
		}
		if colIdx == 1 && c.FG == colorGreen {
			foundGreen = true
		}
	}
	if !foundYellow {
		t.Error("expected yellow cells in number column")
	}
	if !foundGreen {
		t.Error("expected green cells in path column")
	}
}

func TestClassifyAndColorize_NilSafe(t *testing.T) {
	// Should not panic.
	classifyAndColorize(nil, nil)
	classifyAndColorize(&tableStructure{}, nil)
	classifyAndColorize(&tableStructure{columns: []columnInfo{{}}}, nil)
	classifyAndColorize(&tableStructure{
		columns: []columnInfo{{}},
		rows:    [][]string{},
	}, nil)
}
