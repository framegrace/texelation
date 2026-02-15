# Dual-Layer Line System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Each terminal line carries both original and optional formatted overlay content, both persisted, with a global toggle to switch views.

**Architecture:** LogicalLine gains `Overlay []Cell`, `OverlayWidth int`, and `Synthetic bool` fields. Transformers write to `Overlay` instead of mutating `Cells`. The viewport reads the active layer based on a global toggle. Persistence uses a new TXLHIST2 format encoding both layers.

**Tech Stack:** Go 1.24, texelterm parser package, page-based persistence (PageStore/WAL)

**Design doc:** `docs/plans/2026-02-15-dual-layer-lines-design.md`

---

### Task 1: Add Overlay Fields to LogicalLine

Add `Overlay`, `OverlayWidth`, and `Synthetic` fields to LogicalLine. Update `Clone()` to deep-copy overlay. Update `WrapToWidth()` to handle overlay rendering.

**Files:**
- Modify: `apps/texelterm/parser/logical_line.go:13-23` (struct definition)
- Modify: `apps/texelterm/parser/logical_line.go:94-99` (Clone method)
- Test: `apps/texelterm/parser/logical_line_test.go`

**Step 1: Write the failing tests**

```go
func TestLogicalLine_OverlayFields(t *testing.T) {
	line := NewLogicalLine()
	if line.Overlay != nil {
		t.Error("new line should have nil Overlay")
	}
	if line.OverlayWidth != 0 {
		t.Error("new line should have zero OverlayWidth")
	}
	if line.Synthetic {
		t.Error("new line should not be Synthetic")
	}
}

func TestLogicalLine_CloneWithOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("original"))
	line.Overlay = []Cell{
		{Rune: 'F', FG: DefaultFG, BG: DefaultBG},
		{Rune: 'M', FG: DefaultFG, BG: DefaultBG},
		{Rune: 'T', FG: DefaultFG, BG: DefaultBG},
	}
	line.OverlayWidth = 80
	line.Synthetic = true

	clone := line.Clone()

	// Verify overlay was deep-copied
	if len(clone.Overlay) != 3 {
		t.Fatalf("expected overlay len 3, got %d", len(clone.Overlay))
	}
	if clone.OverlayWidth != 80 {
		t.Errorf("expected OverlayWidth 80, got %d", clone.OverlayWidth)
	}
	if !clone.Synthetic {
		t.Error("expected Synthetic=true on clone")
	}

	// Verify no aliasing
	clone.Overlay[0].Rune = 'X'
	if line.Overlay[0].Rune != 'F' {
		t.Error("overlay should be deep-copied, not aliased")
	}
}

func TestLogicalLine_CloneNilOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("no overlay"))
	clone := line.Clone()
	if clone.Overlay != nil {
		t.Error("clone of line without overlay should have nil Overlay")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestLogicalLine_Overlay|TestLogicalLine_CloneWith|TestLogicalLine_CloneNil" -v`
Expected: FAIL — `line.Overlay` field does not exist.

**Step 3: Add the fields and update Clone**

In `apps/texelterm/parser/logical_line.go`, update the struct:

```go
type LogicalLine struct {
	Cells        []Cell
	FixedWidth   int
	Overlay      []Cell // Formatted content set by transformers (nil = no overlay)
	OverlayWidth int    // Width overlay was created at (always fixed-width)
	Synthetic    bool   // True = transformer-inserted line, hidden in original view
}
```

Update `Clone()`:

```go
func (l *LogicalLine) Clone() *LogicalLine {
	clone := NewLogicalLineFromCells(l.Cells)
	clone.FixedWidth = l.FixedWidth
	clone.Synthetic = l.Synthetic
	clone.OverlayWidth = l.OverlayWidth
	if l.Overlay != nil {
		clone.Overlay = make([]Cell, len(l.Overlay))
		copy(clone.Overlay, l.Overlay)
	}
	return clone
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestLogicalLine_Overlay|TestLogicalLine_CloneWith|TestLogicalLine_CloneNil" -v`
Expected: PASS

**Step 5: Run full test suite to check for regressions**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -count=1`
Expected: PASS (existing tests unaffected — new fields have zero values)

**Step 6: Commit**

```bash
git add apps/texelterm/parser/logical_line.go apps/texelterm/parser/logical_line_test.go
git commit -m "feat: add Overlay, OverlayWidth, Synthetic fields to LogicalLine"
```

---

### Task 2: Add OverlayWrapToWidth Method

Add a method that returns physical lines from the overlay (fixed-width) or falls back to normal cells. This is the rendering primitive used by the viewport.

**Files:**
- Modify: `apps/texelterm/parser/logical_line.go` (add method)
- Test: `apps/texelterm/parser/logical_line_test.go`

**Step 1: Write the failing tests**

```go
func TestLogicalLine_ActiveContent_NoOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("Hello World"))
	// showOverlay=true but no overlay → falls back to Cells
	physical := line.ActiveWrapToWidth(10, true)
	if len(physical) != 2 {
		t.Fatalf("expected 2 physical lines (wrap at 10), got %d", len(physical))
	}
}

func TestLogicalLine_ActiveContent_WithOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("Hello World"))
	line.Overlay = makeCells("| Hello | World |")
	line.OverlayWidth = 80

	// showOverlay=true, has overlay → use overlay as fixed-width
	physical := line.ActiveWrapToWidth(40, true)
	if len(physical) != 1 {
		t.Fatalf("overlay should produce 1 physical line (fixed-width), got %d", len(physical))
	}
	// Should be clipped/padded to viewport width (40)
	if len(physical[0].Cells) != 40 {
		t.Errorf("expected 40 cells, got %d", len(physical[0].Cells))
	}

	// showOverlay=false → use original Cells (wraps normally)
	physical = line.ActiveWrapToWidth(10, false)
	if len(physical) != 2 {
		t.Fatalf("original should wrap to 2 lines at width 10, got %d", len(physical))
	}
}

func TestLogicalLine_ActiveContent_Synthetic(t *testing.T) {
	line := &LogicalLine{
		Synthetic:    true,
		Overlay:      makeCells("+---------+"),
		OverlayWidth: 80,
	}

	// showOverlay=true → show overlay
	physical := line.ActiveWrapToWidth(80, true)
	if len(physical) != 1 {
		t.Fatalf("expected 1 physical line, got %d", len(physical))
	}

	// showOverlay=false → synthetic lines are skipped (return nil)
	physical = line.ActiveWrapToWidth(80, false)
	if physical != nil {
		t.Errorf("synthetic lines should return nil when showOverlay=false, got %d lines", len(physical))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestLogicalLine_ActiveContent" -v`
Expected: FAIL — method does not exist.

**Step 3: Implement ActiveWrapToWidth**

Add to `apps/texelterm/parser/logical_line.go`:

```go
// ActiveWrapToWidth returns physical lines for the active content layer.
// When showOverlay is true and Overlay is set, renders overlay as fixed-width.
// When showOverlay is false, renders Cells normally (Synthetic lines return nil).
func (l *LogicalLine) ActiveWrapToWidth(width int, showOverlay bool) []PhysicalLine {
	if !showOverlay {
		if l.Synthetic {
			return nil // Synthetic lines hidden in original view
		}
		return l.WrapToWidth(width)
	}

	// Overlay mode: use overlay if present, fall back to Cells
	if l.Overlay != nil {
		overlay := &LogicalLine{
			Cells:      l.Overlay,
			FixedWidth: l.OverlayWidth,
		}
		if overlay.FixedWidth == 0 {
			overlay.FixedWidth = len(l.Overlay)
		}
		return overlay.WrapToWidth(width)
	}

	return l.WrapToWidth(width)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestLogicalLine_ActiveContent" -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add apps/texelterm/parser/logical_line.go apps/texelterm/parser/logical_line_test.go
git commit -m "feat: add ActiveWrapToWidth for overlay-aware line rendering"
```

---

### Task 3: Update Page Encoding for TXLHIST2

Update `encodeLineData` / `decodeLineData` in `page.go` to encode/decode overlay fields. Support reading old format lines (no flags byte) for backward compatibility.

**Files:**
- Modify: `apps/texelterm/parser/page.go:564-619` (encodeLineData, decodeLineData)
- Test: `apps/texelterm/parser/page_test.go` (create if needed, or add to existing)

**Step 1: Write the failing tests**

Create or add to `apps/texelterm/parser/page_encode_test.go`:

```go
package parser

import "testing"

func TestEncodeDecodeLineData_NoOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("hello"))
	line.FixedWidth = 42

	data := encodeLineData(line)
	decoded, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.FixedWidth != 42 {
		t.Errorf("FixedWidth: expected 42, got %d", decoded.FixedWidth)
	}
	if len(decoded.Cells) != 5 {
		t.Errorf("Cells: expected 5, got %d", len(decoded.Cells))
	}
	if decoded.Overlay != nil {
		t.Error("Overlay should be nil for line without overlay")
	}
	if decoded.Synthetic {
		t.Error("Synthetic should be false")
	}
}

func TestEncodeDecodeLineData_WithOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(makeCells("original"))
	line.Overlay = makeCells("formatted content here")
	line.OverlayWidth = 80
	line.Synthetic = false

	data := encodeLineData(line)
	decoded, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(decoded.Cells) != len(line.Cells) {
		t.Errorf("Cells len: expected %d, got %d", len(line.Cells), len(decoded.Cells))
	}
	if len(decoded.Overlay) != len(line.Overlay) {
		t.Errorf("Overlay len: expected %d, got %d", len(line.Overlay), len(decoded.Overlay))
	}
	if decoded.OverlayWidth != 80 {
		t.Errorf("OverlayWidth: expected 80, got %d", decoded.OverlayWidth)
	}
	for i, c := range decoded.Overlay {
		if c.Rune != line.Overlay[i].Rune {
			t.Errorf("overlay cell %d: expected %q, got %q", i, line.Overlay[i].Rune, c.Rune)
		}
	}
}

func TestEncodeDecodeLineData_Synthetic(t *testing.T) {
	line := &LogicalLine{
		Synthetic:    true,
		Overlay:      makeCells("+----+"),
		OverlayWidth: 80,
	}

	data := encodeLineData(line)
	decoded, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !decoded.Synthetic {
		t.Error("expected Synthetic=true")
	}
	if len(decoded.Cells) != 0 {
		t.Errorf("synthetic line should have 0 cells, got %d", len(decoded.Cells))
	}
	if len(decoded.Overlay) != 6 {
		t.Errorf("expected 6 overlay cells, got %d", len(decoded.Overlay))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestEncodeDecodeLineData_" -v`
Expected: FAIL — overlay/synthetic not encoded/decoded.

**Step 3: Update encodeLineData and decodeLineData**

In `apps/texelterm/parser/page.go`, replace `encodeLineData`:

```go
// encodeLineData serializes a LogicalLine to bytes.
// Format (v2): Flags(1) + CellCount(4) + FixedWidth(4) + Cells(N*16) + [OverlayWidth(4) + OverlayCellCount(4) + OverlayCells(M*16)]
func encodeLineData(line *LogicalLine) []byte {
	// Compute flags
	var flags byte
	if line.Overlay != nil {
		flags |= 0x01
	}
	if line.Synthetic {
		flags |= 0x02
	}

	cellCount := uint32(len(line.Cells))
	size := 1 + 4 + 4 + int(cellCount)*PageCellSize // flags + cellCount + fixedWidth + cells

	if flags&0x01 != 0 {
		size += 4 + 4 + len(line.Overlay)*PageCellSize // overlayWidth + overlayCellCount + overlayCells
	}

	buf := make([]byte, size)
	offset := 0

	// Flags (1 byte)
	buf[offset] = flags
	offset++

	// CellCount (4 bytes)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], cellCount)
	offset += 4

	// FixedWidth (4 bytes)
	binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(line.FixedWidth))
	offset += 4

	// Cells (CellCount * 16 bytes)
	cellBuf := make([]byte, PageCellSize)
	for _, cell := range line.Cells {
		encodeCell(cell, cellBuf)
		copy(buf[offset:offset+PageCellSize], cellBuf)
		offset += PageCellSize
	}

	// Overlay section (if present)
	if flags&0x01 != 0 {
		// OverlayWidth (4 bytes)
		binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(line.OverlayWidth))
		offset += 4

		// OverlayCellCount (4 bytes)
		overlayCellCount := uint32(len(line.Overlay))
		binary.LittleEndian.PutUint32(buf[offset:offset+4], overlayCellCount)
		offset += 4

		// Overlay Cells
		for _, cell := range line.Overlay {
			encodeCell(cell, cellBuf)
			copy(buf[offset:offset+PageCellSize], cellBuf)
			offset += PageCellSize
		}
	}

	return buf
}
```

Replace `decodeLineData`:

```go
// decodeLineData deserializes bytes to a LogicalLine.
// Supports both v1 (no flags byte) and v2 (with flags) formats.
func decodeLineData(data []byte) (*LogicalLine, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("line data too short: %d bytes", len(data))
	}

	// Detect format: v2 has a flags byte where v1 has CellCount.
	// In v1, first 4 bytes are CellCount (uint32) which is always >= 0.
	// In v2, first byte is flags (0x00-0x03 for valid flags).
	// Heuristic: if data[0] <= 0x03 AND the rest of bytes starting at offset 1
	// parse correctly as v2, use v2. But a simpler approach: v2 encodes flags
	// byte first. Since we're writing v2 from now on and v1 files have CellCount
	// first (which can be 0-N), we need a reliable way to distinguish.
	//
	// Solution: v1 starts with CellCount(4)+FixedWidth(4), so first 4 bytes
	// represent CellCount. In v2, first byte is flags (0x00-0x03), followed by
	// CellCount(4). We can distinguish by checking: if we parse as v1 and the
	// expected data length matches, use v1. Otherwise try v2.
	//
	// Better solution: the caller (Page/WAL) knows the format version.
	// For now, try v2 first, fall back to v1.
	line, err := decodeLineDataV2(data)
	if err != nil {
		return decodeLineDataV1(data)
	}
	return line, nil
}

// decodeLineDataV1 decodes the original format: CellCount(4) + FixedWidth(4) + Cells
func decodeLineDataV1(data []byte) (*LogicalLine, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("v1 line data too short: %d bytes", len(data))
	}

	cellCount := binary.LittleEndian.Uint32(data[0:4])
	fixedWidth := binary.LittleEndian.Uint32(data[4:8])

	expectedSize := 8 + int(cellCount)*PageCellSize
	if len(data) < expectedSize {
		return nil, fmt.Errorf("v1 line data truncated: expected %d, got %d", expectedSize, len(data))
	}

	cells := make([]Cell, cellCount)
	offset := 8
	for i := uint32(0); i < cellCount; i++ {
		cells[i] = decodeCell(data[offset : offset+PageCellSize])
		offset += PageCellSize
	}

	return &LogicalLine{
		Cells:      cells,
		FixedWidth: int(fixedWidth),
	}, nil
}

// decodeLineDataV2 decodes the new format: Flags(1) + CellCount(4) + FixedWidth(4) + Cells + [Overlay]
func decodeLineDataV2(data []byte) (*LogicalLine, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("v2 line data too short: %d bytes", len(data))
	}

	flags := data[0]
	if flags & ^byte(0x03) != 0 {
		return nil, fmt.Errorf("invalid flags: 0x%02x", flags)
	}

	offset := 1

	cellCount := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	fixedWidth := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	expectedSize := offset + int(cellCount)*PageCellSize
	if flags&0x01 != 0 {
		expectedSize += 8 // overlayWidth + overlayCellCount (minimum)
	}
	if len(data) < expectedSize {
		return nil, fmt.Errorf("v2 line data truncated at cells")
	}

	cells := make([]Cell, cellCount)
	for i := uint32(0); i < cellCount; i++ {
		cells[i] = decodeCell(data[offset : offset+PageCellSize])
		offset += PageCellSize
	}

	line := &LogicalLine{
		Cells:      cells,
		FixedWidth: int(fixedWidth),
		Synthetic:  flags&0x02 != 0,
	}

	// Overlay section
	if flags&0x01 != 0 {
		if len(data) < offset+8 {
			return nil, fmt.Errorf("v2 overlay header truncated")
		}

		line.OverlayWidth = int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4

		overlayCellCount := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4

		overlayEnd := offset + int(overlayCellCount)*PageCellSize
		if len(data) < overlayEnd {
			return nil, fmt.Errorf("v2 overlay cells truncated")
		}

		line.Overlay = make([]Cell, overlayCellCount)
		for i := uint32(0); i < overlayCellCount; i++ {
			line.Overlay[i] = decodeCell(data[offset : offset+PageCellSize])
			offset += PageCellSize
		}
	}

	// Validate total consumed matches data length
	if offset != len(data) {
		return nil, fmt.Errorf("v2 decode consumed %d bytes but data has %d", offset, len(data))
	}

	return line, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestEncodeDecodeLineData_" -v`
Expected: PASS

**Step 5: Run full test suite (persistence + page tests)**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -count=1`
Expected: PASS — v1 data still decodes correctly via fallback.

**Step 6: Commit**

```bash
git add apps/texelterm/parser/page.go apps/texelterm/parser/page_encode_test.go
git commit -m "feat: add TXLHIST2 encoding with overlay/synthetic support"
```

---

### Task 4: Update LogicalLine Persistence (WriteLogicalLines/LoadLogicalLines)

Update the file-level persistence (`logical_line_persistence.go`) to handle overlay fields. Bump magic to `TXLHIST2`. Support reading `TXLHIST1` files.

**Files:**
- Modify: `apps/texelterm/parser/logical_line_persistence.go`
- Test: `apps/texelterm/parser/logical_line_persistence_test.go`

**Step 1: Write the failing tests**

```go
func TestLogicalLinePersistence_OverlayRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, "test.lhist")

	lines := []*LogicalLine{
		// Normal line, no overlay
		NewLogicalLineFromCells(makeCells("original only")),
		// Line with overlay
		func() *LogicalLine {
			l := NewLogicalLineFromCells(makeCells("raw output"))
			l.Overlay = makeCells("| raw | output |")
			l.OverlayWidth = 80
			return l
		}(),
		// Synthetic line
		{
			Synthetic:    true,
			Overlay:      makeCells("+------+--------+"),
			OverlayWidth: 80,
		},
	}

	if err := WriteLogicalLines(histFile, lines); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(loaded) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(loaded))
	}

	// Line 0: no overlay
	if loaded[0].Overlay != nil {
		t.Error("line 0 should have nil overlay")
	}

	// Line 1: has overlay
	if len(loaded[1].Overlay) != len(lines[1].Overlay) {
		t.Errorf("line 1 overlay: expected %d cells, got %d", len(lines[1].Overlay), len(loaded[1].Overlay))
	}
	if loaded[1].OverlayWidth != 80 {
		t.Errorf("line 1 OverlayWidth: expected 80, got %d", loaded[1].OverlayWidth)
	}

	// Line 2: synthetic
	if !loaded[2].Synthetic {
		t.Error("line 2 should be Synthetic")
	}
	if len(loaded[2].Cells) != 0 {
		t.Errorf("synthetic line should have 0 cells, got %d", len(loaded[2].Cells))
	}
	if len(loaded[2].Overlay) != len(lines[2].Overlay) {
		t.Errorf("line 2 overlay: expected %d cells, got %d", len(lines[2].Overlay), len(loaded[2].Overlay))
	}
}

func TestLogicalLinePersistence_BackwardCompatV1(t *testing.T) {
	tmpDir := t.TempDir()
	histFile := filepath.Join(tmpDir, "v1.lhist")

	// Write lines using v1 format (current code before changes)
	lines := []*LogicalLine{
		NewLogicalLineFromCells(makeCells("line one")),
		NewLogicalLineFromCells(makeCells("line two")),
	}
	if err := WriteLogicalLines(histFile, lines); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// After updating to v2, reading v1 should still work
	loaded, err := LoadLogicalLines(histFile)
	if err != nil {
		t.Fatalf("load v1 failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(loaded))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestLogicalLinePersistence_Overlay|TestLogicalLinePersistence_BackwardCompat" -v`
Expected: FAIL — overlay not persisted.

**Step 3: Update persistence code**

In `apps/texelterm/parser/logical_line_persistence.go`:

1. Add `logicalHistoryMagicV2 = "TXLHIST2"`.
2. Update `WriteLogicalLines` to write `TXLHIST2` magic.
3. Update `writeLogicalLine` to encode flags + overlay.
4. Update `LoadLogicalLines` to accept both magics.
5. Update `readLogicalLine` to decode v2 format (detect by magic).

The implementation should follow the same flags-byte approach as the page encoding in Task 3, but using the file-level serialization format (which uses `writeLogicalLine`/`readLogicalLine`).

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestLogicalLinePersistence_" -v`
Expected: PASS

**Step 5: Full suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add apps/texelterm/parser/logical_line_persistence.go apps/texelterm/parser/logical_line_persistence_test.go
git commit -m "feat: TXLHIST2 persistence with overlay/synthetic support"
```

---

### Task 5: Update PhysicalLineBuilder for Overlay-Aware Rendering

Modify `PhysicalLineBuilder` to accept a `showOverlay` flag and use `ActiveWrapToWidth`. Skip synthetic lines when overlay is off.

**Files:**
- Modify: `apps/texelterm/parser/viewport_physical_builder.go`
- Test: `apps/texelterm/parser/viewport_window_test.go`

**Step 1: Write the failing tests**

```go
func TestPhysicalLineBuilder_OverlayMode(t *testing.T) {
	builder := NewPhysicalLineBuilder(40)

	// Line with overlay
	line := NewLogicalLineFromCells(vwMakeCells("Hello World Original"))
	line.Overlay = vwMakeCells("| Hello | World |")
	line.OverlayWidth = 40

	// Overlay mode: should render overlay (fixed-width, 1 physical line)
	builder.SetShowOverlay(true)
	physical := builder.BuildLine(line, 100)
	if len(physical) != 1 {
		t.Fatalf("overlay mode: expected 1 physical line, got %d", len(physical))
	}

	// Original mode: should render Cells (may wrap)
	builder.SetShowOverlay(false)
	physical = builder.BuildLine(line, 100)
	if len(physical) != 1 { // "Hello World Original" fits in 40 cols
		t.Fatalf("original mode: expected 1 physical line, got %d", len(physical))
	}
}

func TestPhysicalLineBuilder_SkipSyntheticInOriginalMode(t *testing.T) {
	builder := NewPhysicalLineBuilder(40)

	synthetic := &LogicalLine{
		Synthetic:    true,
		Overlay:      vwMakeCells("+--------+"),
		OverlayWidth: 40,
	}

	builder.SetShowOverlay(true)
	physical := builder.BuildLine(synthetic, 100)
	if len(physical) != 1 {
		t.Fatalf("overlay mode: expected 1 line for synthetic, got %d", len(physical))
	}

	builder.SetShowOverlay(false)
	physical = builder.BuildLine(synthetic, 100)
	if physical != nil {
		t.Fatalf("original mode: synthetic lines should return nil, got %d lines", len(physical))
	}
}

func TestPhysicalLineBuilder_BuildRangeSkipsSynthetic(t *testing.T) {
	builder := NewPhysicalLineBuilder(40)
	builder.SetShowOverlay(false)

	lines := []*LogicalLine{
		NewLogicalLineFromCells(vwMakeCells("Line1")),
		{Synthetic: true, Overlay: vwMakeCells("+---+"), OverlayWidth: 40},
		NewLogicalLineFromCells(vwMakeCells("Line2")),
	}

	physical := builder.BuildRange(lines, 100)
	// Should have 2 physical lines (synthetic skipped)
	if len(physical) != 2 {
		t.Fatalf("expected 2 physical lines (synthetic skipped), got %d", len(physical))
	}
	if physical[0].LogicalIndex != 100 {
		t.Errorf("first line: expected LogicalIndex 100, got %d", physical[0].LogicalIndex)
	}
	if physical[1].LogicalIndex != 102 {
		t.Errorf("second line: expected LogicalIndex 102, got %d", physical[1].LogicalIndex)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestPhysicalLineBuilder_Overlay|TestPhysicalLineBuilder_SkipSynthetic|TestPhysicalLineBuilder_BuildRangeSkipsSynthetic" -v`
Expected: FAIL — `SetShowOverlay` does not exist.

**Step 3: Update PhysicalLineBuilder**

In `apps/texelterm/parser/viewport_physical_builder.go`:

```go
type PhysicalLineBuilder struct {
	width       int
	showOverlay bool // When true, use overlay content if available
}

func (b *PhysicalLineBuilder) SetShowOverlay(show bool) {
	b.showOverlay = show
}

func (b *PhysicalLineBuilder) ShowOverlay() bool {
	return b.showOverlay
}

func (b *PhysicalLineBuilder) BuildLine(line *LogicalLine, globalIdx int64) []PhysicalLine {
	if line == nil {
		return []PhysicalLine{{
			Cells:        make([]Cell, 0),
			LogicalIndex: int(globalIdx),
			Offset:       0,
		}}
	}

	physical := line.ActiveWrapToWidth(b.width, b.showOverlay)
	if physical == nil {
		return nil // Synthetic line hidden in original mode
	}

	for i := range physical {
		physical[i].LogicalIndex = int(globalIdx)
	}

	return physical
}

func (b *PhysicalLineBuilder) BuildRange(lines []*LogicalLine, startGlobalIdx int64) []PhysicalLine {
	if len(lines) == 0 {
		return nil
	}

	result := make([]PhysicalLine, 0, len(lines)*2)

	for i, line := range lines {
		globalIdx := startGlobalIdx + int64(i)
		physical := b.BuildLine(line, globalIdx)
		if physical != nil {
			result = append(result, physical...)
		}
	}

	return result
}
```

**Step 4: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestPhysicalLineBuilder_" -v`
Expected: PASS

**Step 5: Full suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add apps/texelterm/parser/viewport_physical_builder.go apps/texelterm/parser/viewport_window_test.go
git commit -m "feat: overlay-aware PhysicalLineBuilder with synthetic line skipping"
```

---

### Task 6: Add Toggle to ViewportWindow

Add `SetShowOverlay`/`ShowOverlay` to ViewportWindow. Setting it invalidates cache and recomputes physical lines.

**Files:**
- Modify: `apps/texelterm/parser/viewport_window.go`
- Test: `apps/texelterm/parser/viewport_window_test.go`

**Step 1: Write the failing test**

```go
func TestViewportWindow_ToggleOverlay(t *testing.T) {
	// Create a MemoryBuffer with lines that have overlays
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, TermWidth: 40})

	// Write some content
	for _, r := range "Hello World" {
		mb.Write(r, DefaultFG, DefaultBG, 0)
	}
	mb.NewLine()

	// Set overlay on line 0
	line := mb.GetLine(0)
	line.Overlay = vwMakeCells("| Hello | World |")
	line.OverlayWidth = 40

	vw := NewViewportWindow(mb, 40, 5)

	// Default: showOverlay=true (set on builder)
	vw.SetShowOverlay(true)
	grid1 := vw.GetVisibleGrid()
	// Should show overlay content
	row0text := ""
	for _, c := range grid1[0] {
		if c.Rune != 0 {
			row0text += string(c.Rune)
		}
	}
	if !strings.Contains(row0text, "| Hello | World |") {
		t.Errorf("overlay mode should show overlay content, got: %q", row0text)
	}

	// Toggle to original
	vw.SetShowOverlay(false)
	grid2 := vw.GetVisibleGrid()
	row0text = ""
	for _, c := range grid2[0] {
		if c.Rune != 0 {
			row0text += string(c.Rune)
		}
	}
	if !strings.Contains(row0text, "Hello World") {
		t.Errorf("original mode should show original content, got: %q", row0text)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestViewportWindow_ToggleOverlay" -v`
Expected: FAIL — `SetShowOverlay` does not exist on ViewportWindow.

**Step 3: Add toggle to ViewportWindow**

In `apps/texelterm/parser/viewport_window.go`, add:

```go
// SetShowOverlay toggles between formatted (overlay) and original content.
// Invalidates the viewport cache since physical line layout changes.
func (vw *ViewportWindow) SetShowOverlay(show bool) {
	vw.mu.Lock()
	defer vw.mu.Unlock()

	if vw.builder.ShowOverlay() == show {
		return // No change
	}
	vw.builder.SetShowOverlay(show)
	vw.cache.Invalidate()
}

// ShowOverlay returns the current overlay display state.
func (vw *ViewportWindow) ShowOverlay() bool {
	vw.mu.RLock()
	defer vw.mu.RUnlock()
	return vw.builder.ShowOverlay()
}
```

**Step 4: Run test**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -run "TestViewportWindow_ToggleOverlay" -v`
Expected: PASS

**Step 5: Full suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/parser/ -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add apps/texelterm/parser/viewport_window.go apps/texelterm/parser/viewport_window_test.go
git commit -m "feat: add overlay toggle to ViewportWindow"
```

---

### Task 7: Change RequestLineReplace to RequestLineOverlay

Rename `RequestLineReplace` → `RequestLineOverlay` in VTerm. Instead of overwriting `line.Cells`, it sets `line.Overlay` and `line.OverlayWidth`.

**Files:**
- Modify: `apps/texelterm/parser/vterm.go:1290-1300` (method)
- Modify: `apps/texelterm/term.go:1389` (wiring)
- Modify: `apps/texelterm/transformer/transformer.go` (interface name/method)

**Step 1: Update VTerm method**

In `apps/texelterm/parser/vterm.go`, rename and modify `RequestLineReplace`:

```go
// RequestLineOverlay sets overlay content on an existing line without modifying
// the original Cells. Used by transformers to provide formatted views.
func (v *VTerm) RequestLineOverlay(lineIdx int64, cells []Cell) {
	if !v.IsMemoryBufferEnabled() {
		return
	}
	line := v.memBufState.memBuf.GetLine(lineIdx)
	if line == nil {
		return
	}
	line.Overlay = cells
	line.OverlayWidth = len(cells)
}
```

**Step 2: Update RequestLineInsert to create synthetic lines**

In `apps/texelterm/parser/vterm.go`, update `RequestLineInsert`:

```go
func (v *VTerm) RequestLineInsert(beforeIdx int64, cells []Cell) {
	if !v.IsMemoryBufferEnabled() {
		return
	}
	v.memBufState.memBuf.InsertLine(beforeIdx)
	line := v.memBufState.memBuf.GetLine(beforeIdx)
	if line == nil {
		return
	}
	// Inserted lines are synthetic (transformer-generated).
	// Content goes in Overlay, not Cells.
	line.Overlay = cells
	line.OverlayWidth = len(cells)
	line.Synthetic = true
	v.commitInsertOffset++
	cursorGlobal := v.memBufState.liveEdgeBase + int64(v.cursorY)
	if beforeIdx <= cursorGlobal {
		v.memBufState.liveEdgeBase++
	}
}
```

**Step 3: Update transformer interface**

In `apps/texelterm/transformer/transformer.go`, rename `LineReplacer` → `LineOverlayer` and `SetReplaceFunc` → `SetOverlayFunc`. Update `Pipeline.SetReplaceFunc` → `Pipeline.SetOverlayFunc`.

**Step 4: Update term.go wiring**

In `apps/texelterm/term.go`, change:

```go
pipeline.SetInsertFunc(a.vterm.RequestLineInsert)
pipeline.SetOverlayFunc(a.vterm.RequestLineOverlay)
```

**Step 5: Update tablefmt to use new names**

In `apps/texelterm/tablefmt/tablefmt.go`:
- Rename `replaceFunc` field → `overlayFunc`
- Rename `SetReplaceFunc` → `SetOverlayFunc`
- Update `emitRendered` to call `tf.overlayFunc`
- Update `flushRaw`: when flushing raw, overlay lines should have their overlay cleared (not replace cells with original content). Since the original content was never mutated, `flushRaw` just needs to clear any partial overlay state — but since lines haven't been modified, this is simpler: just call `overlayFunc` with `nil` to clear overlays, or simply don't set overlays at all (the original Cells are already intact).

Actually, `flushRaw` restores buffered lines. In the new model, since `Cells` was never mutated, `flushRaw` just needs to ensure no overlays are set. The simplest implementation: do nothing (Cells are already correct), just `resetState()`.

**Step 6: Update compile-time interface checks**

In `tablefmt.go`, update:
```go
var _ transformer.LineOverlayer = (*TableFormatter)(nil)
```

**Step 7: Run all tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && go test ./apps/texelterm/... -count=1`
Expected: PASS

**Step 8: Commit**

```bash
git add apps/texelterm/parser/vterm.go apps/texelterm/term.go \
       apps/texelterm/transformer/transformer.go \
       apps/texelterm/tablefmt/tablefmt.go
git commit -m "refactor: rename LineReplacer to LineOverlayer, overlay instead of replace"
```

---

### Task 8: Fix Suppression/Persistence Flow

Currently, suppressed lines are cleared and never persisted. In the new model:
- Buffered lines are still suppressed (deferred), but on flush they get overlays set and must be persisted.
- The `line.Clear()` after suppression must be removed — original content stays.
- After transformer flush, notify persistence for all affected lines.

**Files:**
- Modify: `apps/texelterm/parser/vterm_memory_buffer.go:607-619` (OnLineCommit block)
- Modify: `apps/texelterm/tablefmt/tablefmt.go` (flush notification)

**Step 1: Remove line.Clear() on suppression**

In `apps/texelterm/parser/vterm_memory_buffer.go`, the block around line 607-619:

Current:
```go
if v.OnLineCommit(currentGlobal, line, v.CommandActive) {
    line.Clear()
    return
}
```

Change to:
```go
if v.OnLineCommit(currentGlobal, line, v.CommandActive) {
    // Line is being buffered by a transformer.
    // Don't clear it — original content stays in Cells.
    // Don't persist yet — transformer will notify when ready.
    return
}
```

**Step 2: Add persistence notification after transformer flush**

The transformer needs a way to notify persistence that lines are ready. Add a new callback interface:

In `apps/texelterm/transformer/transformer.go`:

```go
// LinePersistNotifier allows transformers to notify that lines are ready for persistence.
type LinePersistNotifier interface {
	SetPersistNotifyFunc(fn func(lineIdx int64))
}
```

In `Pipeline`, add `persistNotifyFunc` and wire it similarly to insert/overlay funcs.

In VTerm, expose a method `NotifyLinePersist(lineIdx int64)` that calls the persistence layer:

```go
func (v *VTerm) NotifyLinePersist(lineIdx int64) {
	if v.memBufState.persistence != nil {
		v.memBufState.persistence.NotifyWrite(lineIdx)
	}
}
```

Wire it in `term.go`:

```go
pipeline.SetPersistNotifyFunc(a.vterm.NotifyLinePersist)
```

In `tablefmt.go`, after setting overlays in `emitRendered` and after `flushRaw`, call `persistNotifyFunc` for each affected line:

```go
func (tf *TableFormatter) emitRendered(rendered [][]parser.Cell) {
	nBuf := len(tf.buffer)
	insertBase := tf.buffer[nBuf-1].lineIdx + 1
	extraCount := int64(0)
	for i, row := range rendered {
		if i < nBuf && tf.overlayFunc != nil {
			tf.overlayFunc(tf.buffer[i].lineIdx, row)
			if tf.persistNotifyFunc != nil {
				tf.persistNotifyFunc(tf.buffer[i].lineIdx)
			}
		} else if tf.insertFunc != nil {
			tf.insertFunc(insertBase+extraCount, row)
			extraCount++
			// Inserted lines are automatically at their position
			if tf.persistNotifyFunc != nil {
				tf.persistNotifyFunc(insertBase + extraCount - 1)
			}
		}
	}
}
```

For `flushRaw`, since Cells were never mutated, we just need to persist the originals:

```go
func (tf *TableFormatter) flushRaw() {
	// Original Cells were never mutated, just notify persistence
	if tf.persistNotifyFunc != nil {
		for _, bl := range tf.buffer {
			tf.persistNotifyFunc(bl.lineIdx)
		}
	}
	tf.resetState()
}
```

**Step 3: Run all tests**

Run: `cd /home/marc/projects/texel/texelation && go build ./... && go test ./apps/texelterm/... -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add apps/texelterm/parser/vterm_memory_buffer.go \
       apps/texelterm/parser/vterm.go \
       apps/texelterm/transformer/transformer.go \
       apps/texelterm/tablefmt/tablefmt.go \
       apps/texelterm/term.go
git commit -m "fix: preserve original content on suppression, add persist notification"
```

---

### Task 9: Wire Toggle Keybind in term.go

Add a `Ctrl+T` keybind (or configurable) in the terminal that toggles `showOverlay` on the viewport window. The keybind should invalidate the viewport and trigger a redraw.

**Files:**
- Modify: `apps/texelterm/term.go` (key handling, around line 806)

**Step 1: Add toggle method to TexelTerm**

In `apps/texelterm/term.go`, add a method:

```go
func (a *App) toggleOverlay() {
	if a.viewportWindow == nil {
		return
	}
	current := a.viewportWindow.ShowOverlay()
	a.viewportWindow.SetShowOverlay(!current)
	// Trigger full redraw
	a.vterm.MarkAllDirty()
}
```

**Step 2: Add keybind in HandleKey**

In the `HandleKey` method, add a case for `Ctrl+T` before the key is sent to the PTY:

```go
case tcell.KeyCtrlT:
    a.toggleOverlay()
    return true
```

Note: Check that `Ctrl+T` doesn't conflict with existing bindings. If it does, use a different binding. Look at `handleAltScrollKey` (lines 707-730) and `keyToEscapeSequence` (lines 732-804) for the pattern.

**Step 3: Test manually**

Build and run texelterm. Verify `Ctrl+T` toggles between formatted and original views.

Run: `cd /home/marc/projects/texel/texelation && make build-apps`
Expected: Build succeeds.

**Step 4: Commit**

```bash
git add apps/texelterm/term.go
git commit -m "feat: add Ctrl+T keybind to toggle overlay/original view"
```

---

### Task 10: Integration Test — Full Pipeline

Write an integration test that exercises the complete flow: terminal output → transformer formats → overlay set → persist → reload → toggle shows both views.

**Files:**
- Create: `apps/texelterm/tablefmt/integration_test.go`

**Step 1: Write the test**

```go
package tablefmt

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestTableFmt_OverlayIntegration(t *testing.T) {
	// Set up a TableFormatter with overlay/insert/persist callbacks
	tf := New(1000)

	var overlays map[int64][]parser.Cell
	var inserts []struct {
		idx   int64
		cells []parser.Cell
	}
	var persisted []int64

	overlays = make(map[int64][]parser.Cell)

	tf.SetOverlayFunc(func(lineIdx int64, cells []parser.Cell) {
		overlays[lineIdx] = cells
	})
	tf.SetInsertFunc(func(beforeIdx int64, cells []parser.Cell) {
		inserts = append(inserts, struct {
			idx   int64
			cells []parser.Cell
		}{beforeIdx, cells})
	})
	tf.SetPersistNotifyFunc(func(lineIdx int64) {
		persisted = append(persisted, lineIdx)
	})

	// Simulate CSV output lines
	csvLines := []string{
		"Name,Age,City",
		"Alice,30,NYC",
		"Bob,25,LA",
	}

	// Feed lines as command output
	tf.NotifyPromptStart()
	for i, text := range csvLines {
		line := parser.NewLogicalLineFromCells(makePlainCells(text))
		tf.HandleLine(int64(i), line, true)
	}

	// Flush by simulating prompt (command → prompt transition)
	tf.HandleLine(3, parser.NewLogicalLine(), false)

	// Verify: original lines should NOT have been mutated
	// (We can't check this here since we don't have the actual LogicalLines,
	// but we can verify overlays were set instead of cell replacement)

	// Verify overlays or inserts were created
	totalFormatted := len(overlays) + len(inserts)
	if totalFormatted == 0 {
		t.Error("expected some overlay/insert operations after table flush")
	}

	// Verify persist was called for buffered lines
	if len(persisted) == 0 {
		t.Error("expected persistence notifications after flush")
	}

	t.Logf("Overlays: %d, Inserts: %d, Persisted: %d",
		len(overlays), len(inserts), len(persisted))
}

func makePlainCells(text string) []parser.Cell {
	cells := make([]parser.Cell, len(text))
	for i, r := range text {
		cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}
	return cells
}
```

**Step 2: Run test**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/tablefmt/ -run "TestTableFmt_OverlayIntegration" -v`
Expected: PASS

**Step 3: Commit**

```bash
git add apps/texelterm/tablefmt/integration_test.go
git commit -m "test: add overlay integration test for tablefmt pipeline"
```

---

### Task 11: Update txfmt Transformer for Overlay

The `txfmt` transformer (syntax colorizer) also modifies line cells. It needs to write to `Overlay` instead of mutating `Cells`. Since txfmt doesn't buffer/suppress (it modifies lines in-place during HandleLine), it needs to set overlay directly.

**Files:**
- Modify: `apps/texelterm/txfmt/txfmt.go`
- Test: `apps/texelterm/txfmt/txfmt_test.go` (if exists)

**Step 1: Read current txfmt implementation**

Read `apps/texelterm/txfmt/txfmt.go` to understand how it modifies lines. It likely calls `line.Cells[i] = coloredCell` directly in `HandleLine`.

**Step 2: Change to write overlay**

Instead of modifying `line.Cells` directly, txfmt should:
1. Clone `line.Cells` into a new slice
2. Apply colorization to the clone
3. Set `line.Overlay = colorizedClone`
4. Set `line.OverlayWidth = len(colorizedClone)`

This way the original `Cells` remain untouched.

**Step 3: Run tests**

Run: `cd /home/marc/projects/texel/texelation && go test ./apps/texelterm/txfmt/ -count=1`
Expected: PASS

**Step 4: Full build**

Run: `cd /home/marc/projects/texel/texelation && go build ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add apps/texelterm/txfmt/txfmt.go
git commit -m "refactor: txfmt writes to overlay instead of mutating cells"
```

---

### Task 12: End-to-End Manual Verification

Verify the complete system works end-to-end.

**Step 1: Build**

Run: `cd /home/marc/projects/texel/texelation && make build-apps`
Expected: Clean build.

**Step 2: Run full test suite**

Run: `cd /home/marc/projects/texel/texelation && go test ./... -count=1`
Expected: All pass.

**Step 3: Manual testing checklist**

1. Start texelterm standalone: `./bin/texelterm`
2. Run a command that produces CSV: `cat sample.csv`
3. Verify table is formatted with borders
4. Press `Ctrl+T` — verify original CSV appears (no formatting)
5. Press `Ctrl+T` again — verify formatted table reappears
6. Scroll up through history — verify toggle works on scrolled content
7. Exit and restart texelterm
8. Scroll back — verify both formatted and original content are available
9. Press `Ctrl+T` — verify toggle works after restart
10. Run `ls -la` — verify txfmt colorization appears in overlay mode and raw output in original mode

**Step 4: Final commit with any fixes**

```bash
git add -A
git commit -m "fix: end-to-end verification fixes for dual-layer system"
```

---

## Task Summary

| Task | Component | Files |
|------|-----------|-------|
| 1 | LogicalLine fields | `logical_line.go`, `logical_line_test.go` |
| 2 | ActiveWrapToWidth method | `logical_line.go`, `logical_line_test.go` |
| 3 | Page encoding (TXLHIST2) | `page.go`, `page_encode_test.go` |
| 4 | File persistence (TXLHIST2) | `logical_line_persistence.go`, `*_test.go` |
| 5 | PhysicalLineBuilder overlay | `viewport_physical_builder.go`, `*_test.go` |
| 6 | ViewportWindow toggle | `viewport_window.go`, `*_test.go` |
| 7 | VTerm overlay methods | `vterm.go`, `term.go`, `transformer.go`, `tablefmt.go` |
| 8 | Suppression/persistence fix | `vterm_memory_buffer.go`, `vterm.go`, `tablefmt.go` |
| 9 | Ctrl+T keybind | `term.go` |
| 10 | Integration test | `tablefmt/integration_test.go` |
| 11 | txfmt overlay update | `txfmt/txfmt.go` |
| 12 | End-to-end verification | Manual testing |
