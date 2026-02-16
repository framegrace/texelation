// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import "github.com/framegrace/texelation/apps/texelterm/parser"

// renderTable converts a tableStructure into formatted lines of Cells with
// box-drawing borders. Returns nil for nil or empty input.
func renderTable(ts *tableStructure) [][]parser.Cell {
	if ts == nil || len(ts.columns) == 0 || len(ts.rows) == 0 {
		return nil
	}

	colWidths := computeColumnWidths(ts)

	var result [][]parser.Cell
	result = append(result, makeHBorder(colWidths, '╭', '┬', '╮', '─'))

	for ri, row := range ts.rows {
		result = append(result, makeDataRow(row, colWidths, ts.columns))
		if ri == ts.headerRow {
			result = append(result, makeHBorder(colWidths, '├', '┼', '┤', '─'))
		}
	}

	result = append(result, makeHBorder(colWidths, '╰', '┴', '╯', '─'))
	classifyAndColorize(ts, result)
	transferOriginalColors(ts, result)
	return result
}

// computeColumnWidths returns the maximum content width per column across all
// rows, measured in runes.
func computeColumnWidths(ts *tableStructure) []int {
	widths := make([]int, len(ts.columns))
	for _, row := range ts.rows {
		for ci, cell := range row {
			if ci >= len(widths) {
				break
			}
			w := len([]rune(cell))
			if w > widths[ci] {
				widths[ci] = w
			}
		}
	}
	return widths
}

// makeHBorder builds a horizontal border line such as ╭───┬───╮.
// Each column segment is (colWidth + 2) fill characters to account for the
// one-space padding on each side. All cells receive AttrDim.
func makeHBorder(colWidths []int, left, junction, right, fill rune) []parser.Cell {
	// Total width: 1 (left) + sum(colWidth+2) + (ncols-1) junctions + 1 (right)
	n := 2 // left + right
	for _, w := range colWidths {
		n += w + 2
	}
	n += len(colWidths) - 1 // junctions between columns

	cells := make([]parser.Cell, 0, n)
	cells = append(cells, dimCell(left))

	for ci, w := range colWidths {
		for range w + 2 {
			cells = append(cells, dimCell(fill))
		}
		if ci < len(colWidths)-1 {
			cells = append(cells, dimCell(junction))
		}
	}

	cells = append(cells, dimCell(right))
	return cells
}

// makeDataRow builds a data row like │ Alice │ New York │.
// Content cells use DefaultFG/DefaultBG with no attributes.
// Border characters (│) use AttrDim.
func makeDataRow(row []string, colWidths []int, columns []columnInfo) []parser.Cell {
	n := 2 // left + right border
	for _, w := range colWidths {
		n += w + 2 // padding spaces
	}
	n += len(colWidths) - 1 // interior borders

	cells := make([]parser.Cell, 0, n)
	cells = append(cells, dimCell('│'))

	for ci, w := range colWidths {
		value := ""
		if ci < len(row) {
			value = string([]rune(row[ci]))
		}

		runes := []rune(value)
		if len(runes) > w {
			runes = runes[:w]
		}

		al := alignLeft
		if ci < len(columns) {
			al = columns[ci].align
		}

		padded := alignValue(runes, w, al)

		cells = append(cells, contentCell(' '))
		for _, r := range padded {
			cells = append(cells, contentCell(r))
		}
		cells = append(cells, contentCell(' '))

		if ci < len(colWidths)-1 {
			cells = append(cells, dimCell('│'))
		}
	}

	cells = append(cells, dimCell('│'))
	return cells
}

// alignValue pads a rune slice to the given width according to the alignment.
func alignValue(runes []rune, width int, al alignment) []rune {
	if len(runes) >= width {
		return runes[:width]
	}

	padding := width - len(runes)
	result := make([]rune, width)

	switch al {
	case alignRight:
		for i := range padding {
			result[i] = ' '
		}
		copy(result[padding:], runes)

	case alignCenter:
		leftPad := padding / 2
		rightPad := padding - leftPad
		for i := range leftPad {
			result[i] = ' '
		}
		copy(result[leftPad:], runes)
		for i := range rightPad {
			result[leftPad+len(runes)+i] = ' '
		}

	default: // alignLeft
		copy(result, runes)
		for i := len(runes); i < width; i++ {
			result[i] = ' '
		}
	}

	return result
}

// dimCell creates a Cell with the given rune and AttrDim for border characters.
func dimCell(r rune) parser.Cell {
	return parser.Cell{
		Rune: r,
		FG:   parser.DefaultFG,
		BG:   parser.DefaultBG,
		Attr: parser.AttrDim,
	}
}

// contentCell creates a Cell with the given rune and default colors, no attributes.
func contentCell(r rune) parser.Cell {
	return parser.Cell{
		Rune: r,
		FG:   parser.DefaultFG,
		BG:   parser.DefaultBG,
	}
}

// transferOriginalColors copies non-default FG colors from the original terminal
// cells to the rendered table output. This preserves colors from commands like
// `ls --color` (blue for directories, green for executables, etc.) that would
// otherwise be replaced by tablefmt's inferred column-type colors.
func transferOriginalColors(ts *tableStructure, renderedLines [][]parser.Cell) {
	if ts == nil || len(ts.originalCells) == 0 {
		return
	}

	dataLineStart := 1 // skip top border
	dataRowIdx := 0
	for i := dataLineStart; i < len(renderedLines)-1; i++ {
		cells := renderedLines[i]
		if len(cells) > 0 && isBorderLine(cells) {
			continue
		}
		if dataRowIdx >= len(ts.originalCells) {
			break
		}
		origCells := ts.originalCells[dataRowIdx]
		transferRowColors(cells, origCells)
		dataRowIdx++
	}
}

// transferRowColors walks through a rendered row and its corresponding original
// cells, matching content characters by rune. When a match is found and the
// original cell has a non-default FG color, that color is copied to the
// rendered cell.
func transferRowColors(rendered []parser.Cell, original []parser.Cell) {
	origCursor := 0
	for i := range rendered {
		c := &rendered[i]
		// Skip border cells (dim │ characters).
		if c.Attr&parser.AttrDim != 0 {
			continue
		}
		// Skip padding spaces.
		if c.Rune == ' ' {
			continue
		}
		// Advance origCursor to find a matching rune in the original cells.
		for origCursor < len(original) && original[origCursor].Rune != c.Rune {
			origCursor++
		}
		if origCursor >= len(original) {
			break
		}
		// Transfer non-default FG from original.
		if original[origCursor].FG.Mode != parser.ColorModeDefault {
			c.FG = original[origCursor].FG
		}
		origCursor++
	}
}
