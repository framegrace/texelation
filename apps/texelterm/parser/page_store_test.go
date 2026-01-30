package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Page Tests ---

func TestPage_NewPage(t *testing.T) {
	page := NewPage(1, 100)

	if string(page.Header.Magic[:]) != PageMagic {
		t.Errorf("Magic mismatch: got %q, want %q", page.Header.Magic[:], PageMagic)
	}
	if page.Header.Version != PageVersion {
		t.Errorf("Version mismatch: got %d, want %d", page.Header.Version, PageVersion)
	}
	if page.Header.PageID != 1 {
		t.Errorf("PageID mismatch: got %d, want 1", page.Header.PageID)
	}
	if page.Header.FirstGlobalIdx != 100 {
		t.Errorf("FirstGlobalIdx mismatch: got %d, want 100", page.Header.FirstGlobalIdx)
	}
	if page.Header.State != PageStateLive {
		t.Errorf("State mismatch: got %d, want %d", page.Header.State, PageStateLive)
	}
	if page.Header.LineCount != 0 {
		t.Errorf("LineCount mismatch: got %d, want 0", page.Header.LineCount)
	}
}

func TestPage_AddLine(t *testing.T) {
	page := NewPage(1, 0)
	now := time.Now()

	cells := []Cell{{Rune: 'H'}, {Rune: 'i'}}
	line := NewLogicalLineFromCells(cells)

	ok := page.AddLine(line, now, 0)
	if !ok {
		t.Fatal("AddLine should succeed for empty page")
	}

	if page.Header.LineCount != 1 {
		t.Errorf("LineCount should be 1, got %d", page.Header.LineCount)
	}
	if len(page.Lines) != 1 {
		t.Errorf("Lines should have 1 entry, got %d", len(page.Lines))
	}
	if len(page.Index) != 1 {
		t.Errorf("Index should have 1 entry, got %d", len(page.Index))
	}

	// Verify timestamps
	if page.Header.FirstTimestamp != now.UnixNano() {
		t.Errorf("FirstTimestamp mismatch")
	}
	if page.Header.LastTimestamp != now.UnixNano() {
		t.Errorf("LastTimestamp mismatch")
	}
}

func TestPage_AddLineWithFixedWidth(t *testing.T) {
	page := NewPage(1, 0)

	line := &LogicalLine{
		Cells:      []Cell{{Rune: 'X'}},
		FixedWidth: 80,
	}

	page.AddLine(line, time.Now(), 0)

	if page.Index[0].Flags&LineFlagFixedWidth == 0 {
		t.Error("FixedWidth flag should be set")
	}
}

func TestPage_WriteTo_ReadFrom_RoundTrip(t *testing.T) {
	// Create a page with some lines
	page := NewPage(42, 1000)
	now := time.Now()

	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'H'}, {Rune: 'e'}, {Rune: 'l'}, {Rune: 'l'}, {Rune: 'o'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'W', FG: Color{Mode: ColorModeRGB, R: 255, G: 128, B: 64}}}),
		NewLogicalLineFromCells([]Cell{{Rune: '!', Attr: AttrBold | AttrUnderline}}),
	}

	for i, line := range lines {
		ts := now.Add(time.Duration(i) * time.Second)
		page.AddLine(line, ts, 0)
	}

	// Serialize
	var buf bytes.Buffer
	n, err := page.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo failed: %v", err)
	}
	if n == 0 {
		t.Fatal("WriteTo wrote 0 bytes")
	}

	// Deserialize
	page2 := &Page{}
	_, err = page2.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}

	// Verify header
	if string(page2.Header.Magic[:]) != PageMagic {
		t.Errorf("Magic mismatch after round-trip")
	}
	if page2.Header.PageID != 42 {
		t.Errorf("PageID mismatch: got %d, want 42", page2.Header.PageID)
	}
	if page2.Header.FirstGlobalIdx != 1000 {
		t.Errorf("FirstGlobalIdx mismatch: got %d, want 1000", page2.Header.FirstGlobalIdx)
	}
	if page2.Header.LineCount != 3 {
		t.Errorf("LineCount mismatch: got %d, want 3", page2.Header.LineCount)
	}

	// Verify lines
	if len(page2.Lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d", len(page2.Lines))
	}

	// Verify first line content
	if len(page2.Lines[0].Cells) != 5 {
		t.Errorf("Line 0 cell count: got %d, want 5", len(page2.Lines[0].Cells))
	}
	for i, r := range []rune{'H', 'e', 'l', 'l', 'o'} {
		if page2.Lines[0].Cells[i].Rune != r {
			t.Errorf("Line 0 cell %d: got %c, want %c", i, page2.Lines[0].Cells[i].Rune, r)
		}
	}

	// Verify RGB color
	if page2.Lines[1].Cells[0].FG.Mode != ColorModeRGB {
		t.Error("Line 1 FG mode should be RGB")
	}
	if page2.Lines[1].Cells[0].FG.R != 255 || page2.Lines[1].Cells[0].FG.G != 128 || page2.Lines[1].Cells[0].FG.B != 64 {
		t.Error("Line 1 FG RGB values mismatch")
	}

	// Verify attributes
	if page2.Lines[2].Cells[0].Attr != (AttrBold | AttrUnderline) {
		t.Errorf("Line 2 Attr mismatch: got %d", page2.Lines[2].Cells[0].Attr)
	}

	// Verify timestamps
	for i := range lines {
		expectedTs := now.Add(time.Duration(i) * time.Second)
		gotTs := page2.GetTimestamp(i)
		if gotTs.UnixNano() != expectedTs.UnixNano() {
			t.Errorf("Timestamp %d mismatch: got %v, want %v", i, gotTs, expectedTs)
		}
	}
}

func TestPage_HeaderSize(t *testing.T) {
	// Ensure header is exactly 64 bytes
	page := NewPage(1, 0)
	var buf bytes.Buffer
	n, err := page.writeHeader(&buf)
	if err != nil {
		t.Fatalf("writeHeader failed: %v", err)
	}
	if n != PageHeaderSize {
		t.Errorf("Header size: got %d, want %d", n, PageHeaderSize)
	}
}

func TestPage_IsFull(t *testing.T) {
	page := NewPage(1, 0)

	// Create a line that will take significant space
	cells := make([]Cell, 1000)
	for i := range cells {
		cells[i] = Cell{Rune: 'X'}
	}
	line := NewLogicalLineFromCells(cells)
	lineSize := lineDataSize(line)

	// Add lines until page is full
	linesAdded := 0
	for !page.IsFull(line) {
		if !page.AddLine(line, time.Now(), 0) {
			break
		}
		linesAdded++
		if linesAdded > 100 {
			t.Fatal("Too many lines added without hitting limit")
		}
	}

	// Verify size is near target
	if page.Size() > TargetPageSize {
		t.Errorf("Page exceeds target size: %d > %d", page.Size(), TargetPageSize)
	}

	t.Logf("Added %d lines (each ~%d bytes), total page size: %d", linesAdded, lineSize, page.Size())
}

func TestPage_Size(t *testing.T) {
	page := NewPage(1, 0)

	// Empty page should be header only
	initialSize := page.Size()
	if initialSize != PageHeaderSize {
		t.Errorf("Empty page size: got %d, want %d", initialSize, PageHeaderSize)
	}

	// Add a line
	line := NewLogicalLineFromCells([]Cell{{Rune: 'A'}})
	page.AddLine(line, time.Now(), 0)

	// Size should increase by index entry + line data
	expectedSize := PageHeaderSize + LineIndexSize + lineDataSize(line)
	if page.Size() != expectedSize {
		t.Errorf("Page size after add: got %d, want %d", page.Size(), expectedSize)
	}
}

// --- PageStore Tests ---

func TestPageStore_CreateAndWrite(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal-123")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Write some lines
	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'H'}, {Rune: 'e'}, {Rune: 'l'}, {Rune: 'l'}, {Rune: 'o'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'W'}, {Rune: 'o'}, {Rune: 'r'}, {Rune: 'l'}, {Rune: 'd'}}),
		NewLogicalLineFromCells([]Cell{}), // Empty line
	}

	for _, line := range lines {
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine failed: %v", err)
		}
	}

	if ps.LineCount() != 3 {
		t.Errorf("LineCount: got %d, want 3", ps.LineCount())
	}

	if err := ps.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify directory structure
	pagesDir := filepath.Join(tmpDir, "terminals", "test-terminal-123", "pages")
	if _, err := os.Stat(pagesDir); err != nil {
		t.Fatalf("Pages directory not created: %v", err)
	}
}

func TestPageStore_OpenAndRead(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal-456")

	// Create and write
	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'A', FG: Color{Mode: ColorModeStandard, Value: 1}}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'B', BG: Color{Mode: ColorModeRGB, R: 255, G: 128, B: 64}}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'C', Attr: AttrBold | AttrUnderline}}),
	}

	for _, line := range lines {
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine failed: %v", err)
		}
	}
	ps.Close()

	// Reopen and read
	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	if ps2 == nil {
		t.Fatal("OpenPageStore returned nil")
	}
	defer ps2.Close()

	if ps2.LineCount() != 3 {
		t.Errorf("LineCount: got %d, want 3", ps2.LineCount())
	}

	// Read and verify each line
	for i, expected := range lines {
		line, err := ps2.ReadLine(int64(i))
		if err != nil {
			t.Fatalf("ReadLine %d failed: %v", i, err)
		}
		if line == nil {
			t.Fatalf("Line %d is nil", i)
		}
		if len(line.Cells) != len(expected.Cells) {
			t.Errorf("Line %d: cell count mismatch", i)
			continue
		}
		for j, cell := range line.Cells {
			exp := expected.Cells[j]
			if cell.Rune != exp.Rune {
				t.Errorf("Line %d cell %d: rune mismatch", i, j)
			}
			if cell.FG != exp.FG {
				t.Errorf("Line %d cell %d: FG mismatch: %+v vs %+v", i, j, cell.FG, exp.FG)
			}
			if cell.BG != exp.BG {
				t.Errorf("Line %d cell %d: BG mismatch", i, j)
			}
			if cell.Attr != exp.Attr {
				t.Errorf("Line %d cell %d: Attr mismatch", i, j)
			}
		}
	}
}

func TestPageStore_ReadLineRange(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Write 10 lines
	for i := 0; i < 10; i++ {
		line := NewLogicalLineFromCells([]Cell{{Rune: rune('0' + i)}})
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine failed: %v", err)
		}
	}
	ps.Close()

	// Reopen and read range
	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	// Read middle range [3, 7)
	lines, err := ps2.ReadLineRange(3, 7)
	if err != nil {
		t.Fatalf("ReadLineRange failed: %v", err)
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

func TestPageStore_ReadOutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	ps.AppendLine(NewLogicalLineFromCells([]Cell{{Rune: 'X'}}))
	ps.Close()

	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	// Read out of bounds
	result, err := ps2.ReadLine(-1)
	if err != nil {
		t.Errorf("Unexpected error for negative index: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for negative index")
	}

	result, err = ps2.ReadLine(100)
	if err != nil {
		t.Errorf("Unexpected error for large index: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil for large index")
	}

	// Range out of bounds (clamped)
	lines, err := ps2.ReadLineRange(-5, 100)
	if err != nil {
		t.Fatalf("ReadLineRange failed: %v", err)
	}
	if len(lines) != 1 {
		t.Errorf("Expected 1 line (clamped), got %d", len(lines))
	}
}

func TestPageStore_NonExistentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "nonexistent-terminal")

	ps, err := OpenPageStore(config)
	if err != nil {
		t.Errorf("Expected nil error for nonexistent directory, got: %v", err)
	}
	if ps != nil {
		t.Errorf("Expected nil for nonexistent directory")
	}
}

func TestPageStore_Timestamps(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	now := time.Now()
	timestamps := []time.Time{
		now,
		now.Add(1 * time.Second),
		now.Add(2 * time.Second),
	}

	for i, ts := range timestamps {
		line := NewLogicalLineFromCells([]Cell{{Rune: rune('0' + i)}})
		if err := ps.AppendLineWithTimestamp(line, ts); err != nil {
			t.Fatalf("AppendLineWithTimestamp failed: %v", err)
		}
	}
	ps.Close()

	// Reopen and verify timestamps
	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	for i, expected := range timestamps {
		ts, err := ps2.GetTimestamp(int64(i))
		if err != nil {
			t.Fatalf("GetTimestamp %d failed: %v", i, err)
		}
		if ts.UnixNano() != expected.UnixNano() {
			t.Errorf("Timestamp %d: got %v, want %v", i, ts, expected)
		}
	}
}

func TestPageStore_64KBBoundary(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Create lines that will fill a page
	// Each 1000-cell line is ~16KB in line data
	cells := make([]Cell, 1000)
	for i := range cells {
		cells[i] = Cell{Rune: 'X'}
	}
	line := NewLogicalLineFromCells(cells)

	// Add enough lines to create multiple pages (64KB / 16KB = ~4 lines per page)
	numLines := 20
	for i := 0; i < numLines; i++ {
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine %d failed: %v", i, err)
		}
	}

	if err := ps.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Check that multiple page files were created
	pagesDir := filepath.Join(tmpDir, "terminals", "test-terminal", "pages")
	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	pageFiles := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".page" {
			pageFiles++
		}
	}

	if pageFiles < 2 {
		t.Errorf("Expected multiple page files, got %d", pageFiles)
	}

	t.Logf("Created %d page files for %d lines", pageFiles, numLines)

	// Verify all lines can be read back
	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	if ps2.LineCount() != int64(numLines) {
		t.Errorf("LineCount: got %d, want %d", ps2.LineCount(), numLines)
	}

	for i := 0; i < numLines; i++ {
		readLine, err := ps2.ReadLine(int64(i))
		if err != nil {
			t.Fatalf("ReadLine %d failed: %v", i, err)
		}
		if readLine == nil {
			t.Fatalf("Line %d is nil", i)
		}
		if len(readLine.Cells) != 1000 {
			t.Errorf("Line %d: expected 1000 cells, got %d", i, len(readLine.Cells))
		}
	}
}

func TestPageStore_FixedWidth(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Write lines with different FixedWidth values
	lines := []*LogicalLine{
		{Cells: []Cell{{Rune: 'A'}}, FixedWidth: 0},
		{Cells: []Cell{{Rune: 'B'}}, FixedWidth: 80},
		{Cells: []Cell{{Rune: 'C'}}, FixedWidth: 120},
	}

	for _, line := range lines {
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine failed: %v", err)
		}
	}
	ps.Close()

	// Reopen and verify FixedWidth preserved
	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	expectedWidths := []int{0, 80, 120}
	for i, expected := range expectedWidths {
		line, err := ps2.ReadLine(int64(i))
		if err != nil {
			t.Fatalf("ReadLine %d failed: %v", i, err)
		}
		if line.FixedWidth != expected {
			t.Errorf("Line %d: FixedWidth got %d, want %d", i, line.FixedWidth, expected)
		}
	}
}

func TestPageStore_LargeLines(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Create a very large line (5000 cells = ~80KB line data)
	cells := make([]Cell, 5000)
	for i := range cells {
		cells[i] = Cell{Rune: rune('A' + (i % 26))}
	}
	largeLine := NewLogicalLineFromCells(cells)

	// This line exceeds 64KB by itself
	if err := ps.AppendLine(largeLine); err != nil {
		t.Fatalf("AppendLine failed: %v", err)
	}

	// Add a normal line after
	smallLine := NewLogicalLineFromCells([]Cell{{Rune: 'Z'}})
	if err := ps.AppendLine(smallLine); err != nil {
		t.Fatalf("AppendLine failed: %v", err)
	}

	ps.Close()

	// Verify both lines can be read
	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	if ps2.LineCount() != 2 {
		t.Errorf("LineCount: got %d, want 2", ps2.LineCount())
	}

	line0, err := ps2.ReadLine(0)
	if err != nil {
		t.Fatalf("ReadLine 0 failed: %v", err)
	}
	if len(line0.Cells) != 5000 {
		t.Errorf("Line 0: expected 5000 cells, got %d", len(line0.Cells))
	}

	line1, err := ps2.ReadLine(1)
	if err != nil {
		t.Fatalf("ReadLine 1 failed: %v", err)
	}
	if len(line1.Cells) != 1 || line1.Cells[0].Rune != 'Z' {
		t.Errorf("Line 1: expected single 'Z', got %v", line1.Cells)
	}
}

func TestPageStore_FindLineAt(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Write lines with known timestamps
	baseTime := time.Date(2025, 1, 28, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		ts := baseTime.Add(time.Duration(i) * time.Hour)
		line := NewLogicalLineFromCells([]Cell{{Rune: rune('0' + i)}})
		if err := ps.AppendLineWithTimestamp(line, ts); err != nil {
			t.Fatalf("AppendLineWithTimestamp failed: %v", err)
		}
	}
	ps.Close()

	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	// Test finding line at specific times
	tests := []struct {
		time        time.Time
		expectedIdx int64
	}{
		{baseTime, 0},                                   // Exact match first
		{baseTime.Add(5 * time.Hour), 5},                // Exact match middle
		{baseTime.Add(30 * time.Minute), 0},             // Between first and second
		{baseTime.Add(5*time.Hour + 30*time.Minute), 5}, // Between lines
		{baseTime.Add(9 * time.Hour), 9},                // Exact match last
	}

	for _, tc := range tests {
		idx, err := ps2.FindLineAt(tc.time)
		if err != nil {
			t.Errorf("FindLineAt(%v) failed: %v", tc.time, err)
			continue
		}
		if idx != tc.expectedIdx {
			t.Errorf("FindLineAt(%v): got %d, want %d", tc.time, idx, tc.expectedIdx)
		}
	}
}

func BenchmarkPageStore_AppendLine(b *testing.B) {
	tmpDir := b.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "bench-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		b.Fatalf("CreatePageStore failed: %v", err)
	}
	defer ps.Close()

	// 80 character line
	cells := make([]Cell, 80)
	for i := range cells {
		cells[i] = Cell{Rune: 'X'}
	}
	line := NewLogicalLineFromCells(cells)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps.AppendLine(line)
	}
}

func BenchmarkPageStore_ReadLine(b *testing.B) {
	tmpDir := b.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "bench-terminal")

	// Create and populate
	ps, err := CreatePageStore(config)
	if err != nil {
		b.Fatalf("CreatePageStore failed: %v", err)
	}

	cells := make([]Cell, 80)
	for i := range cells {
		cells[i] = Cell{Rune: 'X'}
	}
	line := NewLogicalLineFromCells(cells)

	for i := 0; i < 10000; i++ {
		ps.AppendLine(line)
	}
	ps.Close()

	// Reopen for reading
	ps2, err := OpenPageStore(config)
	if err != nil {
		b.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps2.ReadLine(int64(i % 10000))
	}
}

// --- UpdateLine Tests ---

func TestPage_UpdateLine(t *testing.T) {
	page := NewPage(1, 0)
	now := time.Now()

	// Add initial lines
	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'D'}, {Rune: 'E'}, {Rune: 'F'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'G'}, {Rune: 'H'}, {Rune: 'I'}}),
	}
	for _, line := range lines {
		page.AddLine(line, now, 0)
	}

	// Update middle line
	newLine := NewLogicalLineFromCells([]Cell{{Rune: 'X'}, {Rune: 'Y'}, {Rune: 'Z'}, {Rune: '!'}})
	newTs := now.Add(time.Hour)
	if err := page.UpdateLine(1, newLine, newTs); err != nil {
		t.Fatalf("UpdateLine failed: %v", err)
	}

	// Verify the line was updated
	got := page.GetLine(1)
	if len(got.Cells) != 4 {
		t.Errorf("Updated line should have 4 cells, got %d", len(got.Cells))
	}
	if got.Cells[0].Rune != 'X' {
		t.Errorf("First cell should be 'X', got %c", got.Cells[0].Rune)
	}
	if got.Cells[3].Rune != '!' {
		t.Errorf("Last cell should be '!', got %c", got.Cells[3].Rune)
	}

	// Verify timestamp was updated
	gotTs := page.GetTimestamp(1)
	if gotTs.UnixNano() != newTs.UnixNano() {
		t.Errorf("Timestamp should be updated: got %v, want %v", gotTs, newTs)
	}

	// Verify other lines weren't affected
	line0 := page.GetLine(0)
	if line0.Cells[0].Rune != 'A' {
		t.Errorf("Line 0 should be unchanged")
	}
	line2 := page.GetLine(2)
	if line2.Cells[0].Rune != 'G' {
		t.Errorf("Line 2 should be unchanged")
	}
}

func TestPage_UpdateLine_OutOfBounds(t *testing.T) {
	page := NewPage(1, 0)
	page.AddLine(NewLogicalLineFromCells([]Cell{{Rune: 'A'}}), time.Now(), 0)

	// Try to update non-existent line
	newLine := NewLogicalLineFromCells([]Cell{{Rune: 'X'}})
	if err := page.UpdateLine(5, newLine, time.Now()); err == nil {
		t.Error("UpdateLine should fail for out-of-bounds index")
	}

	// Try negative index
	if err := page.UpdateLine(-1, newLine, time.Now()); err == nil {
		t.Error("UpdateLine should fail for negative index")
	}
}

func TestPage_UpdateLine_FixedWidth(t *testing.T) {
	page := NewPage(1, 0)
	page.AddLine(NewLogicalLineFromCells([]Cell{{Rune: 'A'}}), time.Now(), 0)

	// Update to a fixed-width line
	fixedLine := &LogicalLine{
		Cells:      []Cell{{Rune: 'X'}, {Rune: 'Y'}},
		FixedWidth: 80,
	}
	if err := page.UpdateLine(0, fixedLine, time.Now()); err != nil {
		t.Fatalf("UpdateLine failed: %v", err)
	}

	// Verify flag is set
	if page.Index[0].Flags&LineFlagFixedWidth == 0 {
		t.Error("FixedWidth flag should be set after update")
	}
}

func TestPageStore_UpdateLine_CurrentPage(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}
	defer ps.Close()

	// Add some lines (all in current unflushed page)
	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'D'}, {Rune: 'E'}, {Rune: 'F'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'G'}, {Rune: 'H'}, {Rune: 'I'}}),
	}
	for _, line := range lines {
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine failed: %v", err)
		}
	}

	// Update the second line (still in current page)
	newLine := NewLogicalLineFromCells([]Cell{{Rune: 'X'}, {Rune: 'Y'}, {Rune: 'Z'}})
	newTs := time.Now().Add(time.Hour)
	if err := ps.UpdateLine(1, newLine, newTs); err != nil {
		t.Fatalf("UpdateLine failed: %v", err)
	}

	// Verify the update
	got, err := ps.ReadLine(1)
	if err != nil {
		t.Fatalf("ReadLine failed: %v", err)
	}
	if got.Cells[0].Rune != 'X' {
		t.Errorf("Expected 'X', got %c", got.Cells[0].Rune)
	}

	// Verify other lines unchanged
	line0, _ := ps.ReadLine(0)
	if line0.Cells[0].Rune != 'A' {
		t.Error("Line 0 should be unchanged")
	}
}

func TestPageStore_UpdateLine_FlushedPage(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}

	// Add some lines
	lines := []*LogicalLine{
		NewLogicalLineFromCells([]Cell{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'D'}, {Rune: 'E'}, {Rune: 'F'}}),
		NewLogicalLineFromCells([]Cell{{Rune: 'G'}, {Rune: 'H'}, {Rune: 'I'}}),
	}
	for _, line := range lines {
		if err := ps.AppendLine(line); err != nil {
			t.Fatalf("AppendLine failed: %v", err)
		}
	}

	// Flush to disk (lines now in flushed page)
	if err := ps.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Update a line in the flushed page
	newLine := NewLogicalLineFromCells([]Cell{{Rune: 'U'}, {Rune: 'P'}, {Rune: 'D'}})
	if err := ps.UpdateLine(1, newLine, time.Now()); err != nil {
		t.Fatalf("UpdateLine on flushed page failed: %v", err)
	}

	// Close and reopen to ensure persistence
	ps.Close()

	ps2, err := OpenPageStore(config)
	if err != nil {
		t.Fatalf("OpenPageStore failed: %v", err)
	}
	defer ps2.Close()

	// Verify the update persisted
	got, err := ps2.ReadLine(1)
	if err != nil {
		t.Fatalf("ReadLine failed: %v", err)
	}
	if got.Cells[0].Rune != 'U' || got.Cells[1].Rune != 'P' || got.Cells[2].Rune != 'D' {
		t.Errorf("Updated line not persisted correctly: got %c%c%c", got.Cells[0].Rune, got.Cells[1].Rune, got.Cells[2].Rune)
	}

	// Verify other lines unchanged
	line0, _ := ps2.ReadLine(0)
	if line0.Cells[0].Rune != 'A' {
		t.Error("Line 0 should be unchanged")
	}
	line2, _ := ps2.ReadLine(2)
	if line2.Cells[0].Rune != 'G' {
		t.Error("Line 2 should be unchanged")
	}
}

func TestPageStore_UpdateLine_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultPageStoreConfig(tmpDir, "test-terminal")

	ps, err := CreatePageStore(config)
	if err != nil {
		t.Fatalf("CreatePageStore failed: %v", err)
	}
	defer ps.Close()

	// Add one line
	ps.AppendLine(NewLogicalLineFromCells([]Cell{{Rune: 'A'}}))

	// Try to update a non-existent line
	newLine := NewLogicalLineFromCells([]Cell{{Rune: 'X'}})
	if err := ps.UpdateLine(100, newLine, time.Now()); err == nil {
		t.Error("UpdateLine should fail for non-existent index")
	}
}
