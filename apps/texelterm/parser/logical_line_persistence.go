// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/logical_line_persistence.go
// Summary: Persistence for logical lines (scrollback reflow format).
// Usage: Save/load LogicalLine and ScrollbackHistory to disk.

package parser

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const (
	logicalHistoryMagicV1 = "TXLHIST1" // 8 bytes - v1 format (no overlay)
	logicalHistoryMagicV2 = "TXLHIST2" // 8 bytes - v2 format (overlay + synthetic)
	logicalCellSize       = 16         // rune(4) + fg(5) + bg(5) + attr(1) + reserved(1)
)

// WriteLogicalLines writes a slice of LogicalLines to a file.
// Format: [magic:8][line_count:4][lines...]
// Each line: [cell_count:4][cells...]
// Each cell: [rune:4][fg_mode:1][fg_value:4][bg_mode:1][bg_value:4][attr:1][reserved:1]
func WriteLogicalLines(path string, lines []*LogicalLine) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	// Write magic
	if _, err := writer.WriteString(logicalHistoryMagicV2); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write line count
	lineCountBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lineCountBuf, uint32(len(lines)))
	if _, err := writer.Write(lineCountBuf); err != nil {
		return fmt.Errorf("failed to write line count: %w", err)
	}

	// Write each line
	cellBuf := make([]byte, logicalCellSize)
	for i, line := range lines {
		if err := writeLogicalLine(writer, line, cellBuf); err != nil {
			return fmt.Errorf("failed to write line %d: %w", i, err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	return nil
}

// writeLogicalLine writes a single logical line in v2 format.
// Format: [flags:1][cell_count:4][cells...][overlay_width:4][overlay_count:4][overlay_cells...]
func writeLogicalLine(w io.Writer, line *LogicalLine, cellBuf []byte) error {
	// Flags byte: bit 0 = has overlay, bit 1 = synthetic
	var flags byte
	if line.Overlay != nil {
		flags |= 0x01
	}
	if line.Synthetic {
		flags |= 0x02
	}
	if _, err := w.Write([]byte{flags}); err != nil {
		return err
	}

	// Cell count + cells
	countBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBuf, uint32(len(line.Cells)))
	if _, err := w.Write(countBuf); err != nil {
		return err
	}
	for _, cell := range line.Cells {
		encodeLogicalCell(cell, cellBuf)
		if _, err := w.Write(cellBuf); err != nil {
			return err
		}
	}

	// Overlay (if present)
	if flags&0x01 != 0 {
		widthBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(widthBuf, uint32(line.OverlayWidth))
		if _, err := w.Write(widthBuf); err != nil {
			return err
		}

		binary.LittleEndian.PutUint32(countBuf, uint32(len(line.Overlay)))
		if _, err := w.Write(countBuf); err != nil {
			return err
		}
		for _, cell := range line.Overlay {
			encodeLogicalCell(cell, cellBuf)
			if _, err := w.Write(cellBuf); err != nil {
				return err
			}
		}
	}

	return nil
}

// encodeLogicalCell encodes a Cell into the buffer.
// Format: rune(4) + fg_mode(1) + fg_value(4) + bg_mode(1) + bg_value(4) + attr(1) + reserved(1)
func encodeLogicalCell(cell Cell, buf []byte) {
	// Rune (4 bytes)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(cell.Rune))

	// Foreground: mode(1) + value(4)
	buf[4] = byte(cell.FG.Mode)
	binary.LittleEndian.PutUint32(buf[5:9], encodeColorValue(cell.FG))

	// Background: mode(1) + value(4)
	buf[9] = byte(cell.BG.Mode)
	binary.LittleEndian.PutUint32(buf[10:14], encodeColorValue(cell.BG))

	// Attributes (1 byte)
	buf[14] = byte(cell.Attr)

	// Reserved (1 byte)
	buf[15] = 0
}

// encodeColorValue encodes a Color's value into a uint32.
func encodeColorValue(c Color) uint32 {
	if c.Mode == ColorModeRGB {
		return (uint32(c.R) << 16) | (uint32(c.G) << 8) | uint32(c.B)
	}
	return uint32(c.Value)
}

// LoadLogicalLines reads logical lines from a file.
// Supports both TXLHIST1 (v1) and TXLHIST2 (v2) formats.
// Returns nil, nil if file doesn't exist (not an error).
func LoadLogicalLines(path string) ([]*LogicalLine, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	magic := make([]byte, 8)
	if _, err := io.ReadFull(reader, magic); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read magic: %w", err)
	}

	var version int
	switch string(magic) {
	case logicalHistoryMagicV1:
		version = 1
	case logicalHistoryMagicV2:
		version = 2
	default:
		return nil, fmt.Errorf("invalid file magic: %s", string(magic))
	}

	lineCountBuf := make([]byte, 4)
	if _, err := io.ReadFull(reader, lineCountBuf); err != nil {
		return nil, fmt.Errorf("failed to read line count: %w", err)
	}
	lineCount := binary.LittleEndian.Uint32(lineCountBuf)

	lines := make([]*LogicalLine, 0, lineCount)
	cellBuf := make([]byte, logicalCellSize)

	for i := uint32(0); i < lineCount; i++ {
		line, err := readLogicalLine(reader, cellBuf, version)
		if err != nil {
			return nil, fmt.Errorf("failed to read line %d: %w", i, err)
		}
		lines = append(lines, line)
	}

	return lines, nil
}

// readLogicalLine reads a single logical line from the reader.
// v1: [cell_count:4][cells...]
// v2: [flags:1][cell_count:4][cells...][overlay_width:4][overlay_count:4][overlay_cells...]
func readLogicalLine(r io.Reader, cellBuf []byte, version int) (*LogicalLine, error) {
	var flags byte
	if version >= 2 {
		flagsBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, flagsBuf); err != nil {
			return nil, err
		}
		flags = flagsBuf[0]
	}

	countBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, countBuf); err != nil {
		return nil, err
	}
	cellCount := binary.LittleEndian.Uint32(countBuf)

	cells := make([]Cell, cellCount)
	for i := uint32(0); i < cellCount; i++ {
		if _, err := io.ReadFull(r, cellBuf); err != nil {
			return nil, err
		}
		cells[i] = decodeLogicalCell(cellBuf)
	}

	line := &LogicalLine{
		Cells:     cells,
		Synthetic: flags&0x02 != 0,
	}

	if flags&0x01 != 0 {
		widthBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, widthBuf); err != nil {
			return nil, err
		}
		line.OverlayWidth = int(binary.LittleEndian.Uint32(widthBuf))

		if _, err := io.ReadFull(r, countBuf); err != nil {
			return nil, err
		}
		overlayCellCount := binary.LittleEndian.Uint32(countBuf)

		line.Overlay = make([]Cell, overlayCellCount)
		for i := uint32(0); i < overlayCellCount; i++ {
			if _, err := io.ReadFull(r, cellBuf); err != nil {
				return nil, err
			}
			line.Overlay[i] = decodeLogicalCell(cellBuf)
		}
	}

	return line, nil
}

// decodeLogicalCell decodes a Cell from the buffer.
func decodeLogicalCell(buf []byte) Cell {
	cell := Cell{}

	// Rune
	cell.Rune = rune(binary.LittleEndian.Uint32(buf[0:4]))

	// Foreground
	cell.FG.Mode = ColorMode(buf[4])
	cell.FG = decodeColorFromValue(cell.FG.Mode, binary.LittleEndian.Uint32(buf[5:9]))

	// Background
	cell.BG.Mode = ColorMode(buf[9])
	cell.BG = decodeColorFromValue(cell.BG.Mode, binary.LittleEndian.Uint32(buf[10:14]))

	// Attributes
	cell.Attr = Attribute(buf[14])

	// Note: Wrapped flag is not stored in logical lines (it's implicit in line breaks)

	return cell
}

// decodeColorFromValue decodes a Color from mode and value.
func decodeColorFromValue(mode ColorMode, value uint32) Color {
	if mode == ColorModeRGB {
		return Color{
			Mode: ColorModeRGB,
			R:    uint8((value >> 16) & 0xFF),
			G:    uint8((value >> 8) & 0xFF),
			B:    uint8(value & 0xFF),
		}
	}
	return Color{
		Mode:  mode,
		Value: uint8(value & 0xFF),
	}
}

// Note: SaveScrollbackHistory and LoadScrollbackHistory were removed
// as part of the DisplayBuffer cleanup. MemoryBuffer uses its own persistence.

// ConvertPhysicalToLogical converts physical lines (with Wrapped flag) to logical lines.
// This is used to migrate from the old storage format.
// Lines where Wrapped=true are joined with the following line.
func ConvertPhysicalToLogical(physical [][]Cell) []*LogicalLine {
	if len(physical) == 0 {
		return nil
	}

	var result []*LogicalLine
	var currentCells []Cell

	for _, physLine := range physical {
		// Append this physical line's content to current logical line
		currentCells = append(currentCells, physLine...)

		// Check if last cell has Wrapped=true
		if len(physLine) > 0 && physLine[len(physLine)-1].Wrapped {
			// Continue accumulating - don't commit yet
			// Remove the Wrapped flag from the cell we copied
			if len(currentCells) > 0 {
				currentCells[len(currentCells)-1].Wrapped = false
			}
		} else {
			// End of logical line - commit it
			line := &LogicalLine{Cells: currentCells}
			line.TrimTrailingSpaces()
			result = append(result, line)
			currentCells = nil
		}
	}

	// Handle any remaining content (shouldn't happen in well-formed data)
	if len(currentCells) > 0 {
		line := &LogicalLine{Cells: currentCells}
		line.TrimTrailingSpaces()
		result = append(result, line)
	}

	return result
}
