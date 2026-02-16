package parser

import (
	"encoding/binary"
	"testing"
)

func peMakeCells(s string) []Cell {
	cells := make([]Cell, len(s))
	for i, r := range s {
		cells[i] = Cell{Rune: r, FG: DefaultFG, BG: DefaultBG}
	}
	return cells
}

func TestEncodeDecodeLineData_NoOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(peMakeCells("hello"))
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
	for i, c := range decoded.Cells {
		if c.Rune != line.Cells[i].Rune {
			t.Errorf("cell %d: expected %q, got %q", i, line.Cells[i].Rune, c.Rune)
		}
	}
	if decoded.Overlay != nil {
		t.Error("Overlay should be nil")
	}
	if decoded.Synthetic {
		t.Error("Synthetic should be false")
	}
}

func TestEncodeDecodeLineData_WithOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(peMakeCells("original"))
	line.Overlay = peMakeCells("formatted content here")
	line.OverlayWidth = 80

	data := encodeLineData(line)
	decoded, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(decoded.Cells) != len(line.Cells) {
		t.Errorf("Cells len: expected %d, got %d", len(line.Cells), len(decoded.Cells))
	}
	if len(decoded.Overlay) != len(line.Overlay) {
		t.Fatalf("Overlay len: expected %d, got %d", len(line.Overlay), len(decoded.Overlay))
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
		Overlay:      peMakeCells("+----+"),
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
		t.Errorf("synthetic should have 0 cells, got %d", len(decoded.Cells))
	}
	if len(decoded.Overlay) != 6 {
		t.Errorf("expected 6 overlay cells, got %d", len(decoded.Overlay))
	}
}

func TestDecodeLineData_V1Compat(t *testing.T) {
	// Manually encode a v1 format line: CellCount(4) + FixedWidth(4) + Cells
	cells := peMakeCells("test")
	cellCount := uint32(len(cells))
	size := 8 + int(cellCount)*PageCellSize
	data := make([]byte, size)

	binary.LittleEndian.PutUint32(data[0:4], cellCount)
	binary.LittleEndian.PutUint32(data[4:8], 0) // FixedWidth = 0

	cellBuf := make([]byte, PageCellSize)
	offset := 8
	for _, c := range cells {
		encodeCell(c, cellBuf)
		copy(data[offset:offset+PageCellSize], cellBuf)
		offset += PageCellSize
	}

	decoded, err := decodeLineData(data)
	if err != nil {
		t.Fatalf("v1 decode failed: %v", err)
	}
	if len(decoded.Cells) != 4 {
		t.Errorf("expected 4 cells, got %d", len(decoded.Cells))
	}
	if decoded.Overlay != nil {
		t.Error("v1 should have nil overlay")
	}
}

func TestLineDataSize_WithOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(peMakeCells("hi"))
	line.Overlay = peMakeCells("formatted")
	line.OverlayWidth = 40

	size := lineDataSize(line)
	data := encodeLineData(line)
	if size != len(data) {
		t.Errorf("lineDataSize=%d but encodeLineData produced %d bytes", size, len(data))
	}
}

func TestLineDataSize_NoOverlay(t *testing.T) {
	line := NewLogicalLineFromCells(peMakeCells("hello"))
	size := lineDataSize(line)
	data := encodeLineData(line)
	if size != len(data) {
		t.Errorf("lineDataSize=%d but encodeLineData produced %d bytes", size, len(data))
	}
}
