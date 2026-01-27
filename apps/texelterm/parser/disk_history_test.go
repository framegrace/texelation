package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiskHistory_CreateAndWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	// Create new history
	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create disk history: %v", err)
	}

	// Write some lines
	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'H'}, {Rune: 'e'}, {Rune: 'l'}, {Rune: 'l'}, {Rune: 'o'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'W'}, {Rune: 'o'}, {Rune: 'r'}, {Rune: 'l'}, {Rune: 'd'}}),
		NewLogicalLineFromCells([]Cell{}), // Empty line
	}

	for _, line := range lines {
		if err := dh.AppendLine(line); err != nil {
			t.Fatalf("Failed to append line: %v", err)
		}
	}

	if dh.LineCount() != 3 {
		t.Errorf("Expected 3 lines, got %d", dh.LineCount())
	}

	if err := dh.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("History file not created: %v", err)
	}
}

func TestDiskHistory_OpenAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	// Create and write
	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'A', FG: Color{Mode: ColorModeStandard, Value: 1}}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'B', BG: Color{Mode: ColorModeRGB, R: 255, G: 128, B: 64}}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'C', Attr: AttrBold | AttrUnderline}}),
	}

	for _, line := range lines {
		if err := dh.AppendLine(line); err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}
	dh.Close()

	// Reopen and read
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	if dh2.LineCount() != 3 {
		t.Errorf("Expected 3 lines, got %d", dh2.LineCount())
	}

	// Read individual lines
	for i, expected := range lines {
		line, err := dh2.ReadLine(int64(i))
		if err != nil {
			t.Fatalf("Failed to read line %d: %v", i, err)
		}
		if line == nil {
			t.Fatalf("Line %d is nil", i)
		}
		if len(line.Cells) != len(expected.Cells) {
			t.Errorf("Line %d: expected %d cells, got %d", i, len(expected.Cells), len(line.Cells))
			continue
		}
		for j, cell := range line.Cells {
			exp := expected.Cells[j]
			if cell.Rune != exp.Rune {
				t.Errorf("Line %d cell %d: rune mismatch: %c vs %c", i, j, cell.Rune, exp.Rune)
			}
			if cell.FG != exp.FG {
				t.Errorf("Line %d cell %d: FG mismatch: %+v vs %+v", i, j, cell.FG, exp.FG)
			}
			if cell.BG != exp.BG {
				t.Errorf("Line %d cell %d: BG mismatch: %+v vs %+v", i, j, cell.BG, exp.BG)
			}
			if cell.Attr != exp.Attr {
				t.Errorf("Line %d cell %d: Attr mismatch: %d vs %d", i, j, cell.Attr, exp.Attr)
			}
		}
	}
}

func TestDiskHistory_ReadLineRange(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	// Write 10 lines
	for i := 0; i < 10; i++ {
		line := NewLogicalLineFromCells([]Cell{{Rune: rune('0' + i)}})
		if err := dh.AppendLine(line); err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}
	dh.Close()

	// Reopen and read range
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	// Read middle range [3, 7)
	lines, err := dh2.ReadLineRange(3, 7)
	if err != nil {
		t.Fatalf("Failed to read range: %v", err)
	}

	if len(lines) != 4 {
		t.Errorf("Expected 4 lines, got %d", len(lines))
	}

	for i, line := range lines {
		expected := rune('0' + 3 + i)
		if len(line.Cells) != 1 || line.Cells[0].Rune != expected {
			t.Errorf("Line %d: expected %c, got %v", i, expected, line.Cells)
		}
	}
}

func TestDiskHistory_ReadOutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	line := NewLogicalLineFromCells([]Cell{{Rune: 'X'}})
	dh.AppendLine(line)
	dh.Close()

	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	// Read out of bounds
	result, err := dh2.ReadLine(-1)
	if err != nil {
		t.Errorf("Unexpected error for negative index: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for negative index")
	}

	result, err = dh2.ReadLine(100)
	if err != nil {
		t.Errorf("Unexpected error for large index: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for large index")
	}

	// Range out of bounds (clamped)
	lines, err := dh2.ReadLineRange(-5, 100)
	if err != nil {
		t.Fatalf("Failed to read range: %v", err)
	}
	if len(lines) != 1 {
		t.Errorf("Expected 1 line (clamped), got %d", len(lines))
	}
}

func TestDiskHistory_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.hist")

	dh, err := OpenDiskHistory(path)
	if err != nil {
		t.Errorf("Expected nil error for nonexistent file, got: %v", err)
	}
	if dh != nil {
		t.Errorf("Expected nil for nonexistent file")
	}
}

func TestDiskHistory_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	// Create and immediately close (0 lines)
	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}
	dh.Close()

	// Reopen
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	if dh2.LineCount() != 0 {
		t.Errorf("Expected 0 lines, got %d", dh2.LineCount())
	}
}

func TestDiskHistory_LargeLines(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	// Create a line with 1000 cells
	cells := make([]Cell, 1000)
	for i := range cells {
		cells[i] = Cell{Rune: rune('A' + (i % 26))}
	}
	largeLine := NewLogicalLineFromCells(cells)

	if err := dh.AppendLine(largeLine); err != nil {
		t.Fatalf("Failed to append large line: %v", err)
	}
	dh.Close()

	// Reopen and verify
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	line, err := dh2.ReadLine(0)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(line.Cells) != 1000 {
		t.Errorf("Expected 1000 cells, got %d", len(line.Cells))
	}

	for i, cell := range line.Cells {
		expected := rune('A' + (i % 26))
		if cell.Rune != expected {
			t.Errorf("Cell %d: expected %c, got %c", i, expected, cell.Rune)
			break
		}
	}
}

func TestDiskHistory_ColorModes(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}

	// Test all color modes
	cells := []Cell{
		{Rune: 'A', FG: Color{Mode: ColorModeDefault}},
		{Rune: 'B', FG: Color{Mode: ColorModeStandard, Value: 7}},
		{Rune: 'C', FG: Color{Mode: ColorMode256, Value: 200}},
		{Rune: 'D', FG: Color{Mode: ColorModeRGB, R: 100, G: 150, B: 200}},
	}

	if err := dh.AppendLine(NewLogicalLineFromCells(cells)); err != nil {
		t.Fatalf("Failed to append: %v", err)
	}
	dh.Close()

	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	line, err := dh2.ReadLine(0)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	for i, cell := range line.Cells {
		exp := cells[i]
		if cell.FG.Mode != exp.FG.Mode {
			t.Errorf("Cell %d: mode mismatch %v vs %v", i, cell.FG.Mode, exp.FG.Mode)
		}
		if cell.FG.Mode == ColorModeRGB {
			if cell.FG.R != exp.FG.R || cell.FG.G != exp.FG.G || cell.FG.B != exp.FG.B {
				t.Errorf("Cell %d: RGB mismatch", i)
			}
		} else if cell.FG.Value != exp.FG.Value {
			t.Errorf("Cell %d: value mismatch %d vs %d", i, cell.FG.Value, exp.FG.Value)
		}
	}
}

func BenchmarkDiskHistory_AppendLine(b *testing.B) {
	tmpDir := b.TempDir()
	path := filepath.Join(tmpDir, "bench.hist")

	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		b.Fatalf("Failed to create: %v", err)
	}
	defer dh.Close()

	// 80 character line
	cells := make([]Cell, 80)
	for i := range cells {
		cells[i] = Cell{Rune: 'X'}
	}
	line := NewLogicalLineFromCells(cells)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dh.AppendLine(line)
	}
}

func BenchmarkDiskHistory_ReadLine(b *testing.B) {
	tmpDir := b.TempDir()
	path := filepath.Join(tmpDir, "bench.hist")

	// Create file with 10000 lines
	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		b.Fatalf("Failed to create: %v", err)
	}

	cells := make([]Cell, 80)
	for i := range cells {
		cells[i] = Cell{Rune: 'X'}
	}
	line := NewLogicalLineFromCells(cells)

	for i := 0; i < 10000; i++ {
		dh.AppendLine(line)
	}
	dh.Close()

	// Reopen for reading
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		b.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dh2.ReadLine(int64(i % 10000))
	}
}

func TestDiskHistory_FixedWidth(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	// Create new history
	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create disk history: %v", err)
	}

	// Write lines with different FixedWidth values
	lines := []*LogicalLine{
		{Cells: []Cell{{Rune: 'A'}, {Rune: 'B'}}, FixedWidth: 0},   // Normal reflow
		{Cells: []Cell{{Rune: 'C'}, {Rune: 'D'}}, FixedWidth: 80},  // Fixed at 80
		{Cells: []Cell{{Rune: 'E'}, {Rune: 'F'}}, FixedWidth: 120}, // Fixed at 120
	}

	for _, line := range lines {
		if err := dh.AppendLine(line); err != nil {
			t.Fatalf("Failed to append line: %v", err)
		}
	}

	if err := dh.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Reopen and read
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	// Verify FixedWidth is preserved
	expectedWidths := []int{0, 80, 120}
	for i, expected := range expectedWidths {
		line, err := dh2.ReadLine(int64(i))
		if err != nil {
			t.Fatalf("Failed to read line %d: %v", i, err)
		}
		if line.FixedWidth != expected {
			t.Errorf("Line %d: expected FixedWidth=%d, got %d", i, expected, line.FixedWidth)
		}
	}
}

func TestDiskHistory_FixedWidthReflow(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.hist")

	// Create new history
	config := DefaultDiskHistoryConfig(path)
	dh, err := CreateDiskHistory(config)
	if err != nil {
		t.Fatalf("Failed to create disk history: %v", err)
	}

	// Write a 20-char fixed-width line
	cells := make([]Cell, 20)
	for i := range cells {
		cells[i] = Cell{Rune: rune('A' + i%26)}
	}
	line := &LogicalLine{Cells: cells, FixedWidth: 20}

	if err := dh.AppendLine(line); err != nil {
		t.Fatalf("Failed to append: %v", err)
	}
	dh.Close()

	// Reopen and read
	dh2, err := OpenDiskHistory(path)
	if err != nil {
		t.Fatalf("Failed to open: %v", err)
	}
	defer dh2.Close()

	readLine, _ := dh2.ReadLine(0)

	// Verify it doesn't reflow when wrapped to smaller width
	wrapped := readLine.WrapToWidth(10)
	if len(wrapped) != 1 {
		t.Errorf("Fixed-width line should not wrap, got %d physical lines", len(wrapped))
	}
	if len(wrapped[0].Cells) != 10 {
		t.Errorf("Expected 10 cells (clipped), got %d", len(wrapped[0].Cells))
	}

	// Verify normal line (FixedWidth=0) DOES reflow
	normalLine := &LogicalLine{Cells: cells, FixedWidth: 0}
	normalWrapped := normalLine.WrapToWidth(10)
	if len(normalWrapped) != 2 {
		t.Errorf("Normal line should wrap to 2 physical lines, got %d", len(normalWrapped))
	}
}
