// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogicalLinePersistence_WriteAndRead(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "test.lhist")

	// Create some logical lines
	lines := []*LogicalLine{
		NewLogicalLineFromCells(makeCells("Hello World")),
		NewLogicalLineFromCells(makeCells("This is a longer line that might wrap at different widths")),
		NewLogicalLineFromCells(makeCells("")), // Empty line
		NewLogicalLineFromCells(makeCells("Line with special chars: äöü 日本語")),
	}

	// Write to file
	err = WriteLogicalLines(histFile, lines)
	if err != nil {
		t.Fatalf("WriteLogicalLines failed: %v", err)
	}

	// Read back
	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("LoadLogicalLines failed: %v", err)
	}

	// Verify
	if len(loaded) != len(lines) {
		t.Fatalf("expected %d lines, got %d", len(lines), len(loaded))
	}

	for i, line := range loaded {
		original := lines[i]
		if line.Len() != original.Len() {
			t.Errorf("line %d: expected len %d, got %d", i, original.Len(), line.Len())
			continue
		}

		for j := 0; j < line.Len(); j++ {
			if line.Cells[j].Rune != original.Cells[j].Rune {
				t.Errorf("line %d, cell %d: expected rune %q, got %q",
					i, j, original.Cells[j].Rune, line.Cells[j].Rune)
			}
		}
	}
}

func TestLogicalLinePersistence_PreservesColors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "test.lhist")

	// Create line with various colors
	line := NewLogicalLine()
	line.Append(Cell{Rune: 'R', FG: Color{Mode: ColorModeRGB, R: 255, G: 0, B: 0}, BG: DefaultBG})
	line.Append(Cell{Rune: 'G', FG: Color{Mode: ColorModeRGB, R: 0, G: 255, B: 0}, BG: DefaultBG})
	line.Append(Cell{Rune: 'B', FG: Color{Mode: ColorModeRGB, R: 0, G: 0, B: 255}, BG: DefaultBG})
	line.Append(Cell{Rune: '8', FG: Color{Mode: ColorMode256, Value: 196}, BG: DefaultBG})

	lines := []*LogicalLine{line}

	err = WriteLogicalLines(histFile, lines)
	if err != nil {
		t.Fatalf("WriteLogicalLines failed: %v", err)
	}

	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("LoadLogicalLines failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 line, got %d", len(loaded))
	}

	loadedLine := loaded[0]
	if loadedLine.Len() != 4 {
		t.Fatalf("expected 4 cells, got %d", loadedLine.Len())
	}

	// Check RGB red
	if loadedLine.Cells[0].FG.Mode != ColorModeRGB ||
		loadedLine.Cells[0].FG.R != 255 ||
		loadedLine.Cells[0].FG.G != 0 ||
		loadedLine.Cells[0].FG.B != 0 {
		t.Errorf("RGB red not preserved: %+v", loadedLine.Cells[0].FG)
	}

	// Check 256-color
	if loadedLine.Cells[3].FG.Mode != ColorMode256 ||
		loadedLine.Cells[3].FG.Value != 196 {
		t.Errorf("256-color not preserved: %+v", loadedLine.Cells[3].FG)
	}
}

func TestLogicalLinePersistence_PreservesAttributes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "test.lhist")

	// Create line with attributes
	line := NewLogicalLine()
	line.Append(Cell{Rune: 'B', FG: DefaultFG, BG: DefaultBG, Attr: AttrBold})
	line.Append(Cell{Rune: 'R', FG: DefaultFG, BG: DefaultBG, Attr: AttrReverse})
	line.Append(Cell{Rune: 'U', FG: DefaultFG, BG: DefaultBG, Attr: AttrUnderline})
	line.Append(Cell{Rune: 'X', FG: DefaultFG, BG: DefaultBG, Attr: AttrBold | AttrReverse | AttrUnderline})

	lines := []*LogicalLine{line}

	err = WriteLogicalLines(histFile, lines)
	if err != nil {
		t.Fatalf("WriteLogicalLines failed: %v", err)
	}

	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("LoadLogicalLines failed: %v", err)
	}

	loadedLine := loaded[0]

	if loadedLine.Cells[0].Attr != AttrBold {
		t.Errorf("bold not preserved: %v", loadedLine.Cells[0].Attr)
	}
	if loadedLine.Cells[1].Attr != AttrReverse {
		t.Errorf("reverse not preserved: %v", loadedLine.Cells[1].Attr)
	}
	if loadedLine.Cells[2].Attr != AttrUnderline {
		t.Errorf("underline not preserved: %v", loadedLine.Cells[2].Attr)
	}
	if loadedLine.Cells[3].Attr != (AttrBold | AttrReverse | AttrUnderline) {
		t.Errorf("combined attrs not preserved: %v", loadedLine.Cells[3].Attr)
	}
}

func TestLogicalLinePersistence_EmptyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "test.lhist")

	// Write empty history
	err = WriteLogicalLines(histFile, []*LogicalLine{})
	if err != nil {
		t.Fatalf("WriteLogicalLines failed: %v", err)
	}

	// Read back
	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("LoadLogicalLines failed: %v", err)
	}

	if len(loaded) != 0 {
		t.Errorf("expected 0 lines, got %d", len(loaded))
	}
}

func TestLogicalLinePersistence_NonExistentFile(t *testing.T) {
	// Reading a non-existent file should return empty, not error
	loaded, err := LoadLogicalLines("/nonexistent/path/file.lhist")
	if err != nil {
		t.Errorf("expected nil error for non-existent file, got: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for non-existent file, got %d lines", len(loaded))
	}
}

func TestLogicalLinePersistence_LongLine(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	histFile := filepath.Join(tmpDir, "test.lhist")

	// Create a very long line (1000 chars - would wrap many times at 80 cols)
	longText := ""
	for i := 0; i < 1000; i++ {
		longText += string(rune('A' + (i % 26)))
	}

	lines := []*LogicalLine{
		NewLogicalLineFromCells(makeCells(longText)),
	}

	err = WriteLogicalLines(histFile, lines)
	if err != nil {
		t.Fatalf("WriteLogicalLines failed: %v", err)
	}

	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("LoadLogicalLines failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 line, got %d", len(loaded))
	}

	if loaded[0].Len() != 1000 {
		t.Errorf("expected 1000 chars, got %d", loaded[0].Len())
	}

	// Verify content
	for i := 0; i < 1000; i++ {
		expected := rune('A' + (i % 26))
		if loaded[0].Cells[i].Rune != expected {
			t.Errorf("char %d: expected %c, got %c", i, expected, loaded[0].Cells[i].Rune)
			break
		}
	}
}

func TestConvertPhysicalToLogical_SimpleLines(t *testing.T) {
	// Physical lines without wrapping -> one logical line each
	physical := [][]Cell{
		makeCells("Line 1"),
		makeCells("Line 2"),
		makeCells("Line 3"),
	}

	logical := ConvertPhysicalToLogical(physical)

	if len(logical) != 3 {
		t.Fatalf("expected 3 logical lines, got %d", len(logical))
	}

	expected := []string{"Line 1", "Line 2", "Line 3"}
	for i, exp := range expected {
		got := cellsToString(logical[i].Cells)
		if got != exp {
			t.Errorf("line %d: expected %q, got %q", i, exp, got)
		}
	}
}

func TestConvertPhysicalToLogical_WrappedLines(t *testing.T) {
	// Physical lines with wrapping -> joined into one logical line
	line1 := makeCells("Hello")
	line1[len(line1)-1].Wrapped = true // Mark last cell as wrapped

	line2 := makeCells(" World")
	// line2 is not wrapped - ends the logical line

	physical := [][]Cell{line1, line2}

	logical := ConvertPhysicalToLogical(physical)

	if len(logical) != 1 {
		t.Fatalf("expected 1 logical line (wrapped), got %d", len(logical))
	}

	got := cellsToString(logical[0].Cells)
	if got != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", got)
	}
}

func TestConvertPhysicalToLogical_MultipleWraps(t *testing.T) {
	// A very long line wrapped multiple times
	part1 := makeCells("AAAA")
	part1[len(part1)-1].Wrapped = true

	part2 := makeCells("BBBB")
	part2[len(part2)-1].Wrapped = true

	part3 := makeCells("CCCC")
	// Not wrapped - ends logical line

	nextLine := makeCells("Next")

	physical := [][]Cell{part1, part2, part3, nextLine}

	logical := ConvertPhysicalToLogical(physical)

	if len(logical) != 2 {
		t.Fatalf("expected 2 logical lines, got %d", len(logical))
	}

	got1 := cellsToString(logical[0].Cells)
	if got1 != "AAAABBBBCCCC" {
		t.Errorf("first logical line: expected 'AAAABBBBCCCC', got %q", got1)
	}

	got2 := cellsToString(logical[1].Cells)
	if got2 != "Next" {
		t.Errorf("second logical line: expected 'Next', got %q", got2)
	}
}

func TestConvertPhysicalToLogical_EmptyLines(t *testing.T) {
	physical := [][]Cell{
		makeCells("Line 1"),
		{}, // Empty line
		makeCells("Line 3"),
	}

	logical := ConvertPhysicalToLogical(physical)

	if len(logical) != 3 {
		t.Fatalf("expected 3 logical lines, got %d", len(logical))
	}

	if logical[1].Len() != 0 {
		t.Errorf("expected empty line 1, got len %d", logical[1].Len())
	}
}

// Note: TestScrollbackHistory_SaveAndLoad was removed as part of DisplayBuffer cleanup.
// The ScrollbackHistory type has been replaced by MemoryBuffer.
