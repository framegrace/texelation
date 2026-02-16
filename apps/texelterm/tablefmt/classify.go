// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"regexp"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// columnType identifies the semantic type of a column's values.
type columnType int

const (
	colText     columnType = iota // default FG
	colNumber                     // yellow
	colDateTime                   // cyan
	colPath                       // green
)

var (
	reColNumber   = regexp.MustCompile(`^-?[0-9][0-9,.]*%?$`)
	reColDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}|\d{1,3}[dhms]|<?\d+[dhms](\d+[dhms])*>?|\d{2}:\d{2}(:\d{2})?|\d{1,2}[A-Z][a-z]{2}\d{2,4}|\d+\.\d+[dhms])$`)
	reColPath     = regexp.MustCompile(`[/\\]|^\.\w+$|^[\w.-]+\.\w{1,5}$`)
)

// classifyValues determines the column type from cell values.
// Majority (>=60%) of non-empty values determines type for number and datetime.
// Path uses a lower threshold (>=40%) because paths are distinctive.
func classifyValues(values []string) columnType {
	numCount, dateCount, pathCount, total := 0, 0, 0, 0
	for _, val := range values {
		if val == "" || val == "-" || val == "<none>" {
			continue
		}
		total++
		if reColNumber.MatchString(val) {
			numCount++
		} else if reColDateTime.MatchString(val) {
			dateCount++
		} else if reColPath.MatchString(val) {
			pathCount++
		}
	}
	if total == 0 {
		return colText
	}
	if numCount*100/total >= 60 {
		return colNumber
	}
	if dateCount*100/total >= 60 {
		return colDateTime
	}
	if pathCount*100/total >= 40 {
		return colPath
	}
	return colText
}

// classifyAndColorize determines column types and applies colors to rendered
// data cells. Colors: Number=yellow(3), DateTime=cyan(6), Path=green(2),
// Text=default. Header row gets bold cyan.
func classifyAndColorize(ts *tableStructure, renderedLines [][]parser.Cell) {
	if ts == nil || len(ts.columns) == 0 || len(ts.rows) == 0 {
		return
	}

	colTypes := make([]columnType, len(ts.columns))
	for ci := range ts.columns {
		var values []string
		for ri, row := range ts.rows {
			if ri == ts.headerRow {
				continue
			}
			if ci < len(row) {
				values = append(values, row[ci])
			}
		}
		colTypes[ci] = classifyValues(values)
	}

	dataLineStart := 1 // skip top border
	dataRowIdx := 0
	for i := dataLineStart; i < len(renderedLines)-1; i++ {
		cells := renderedLines[i]
		if len(cells) > 0 && isBorderLine(cells) {
			continue
		}
		if dataRowIdx >= len(ts.rows) {
			break
		}
		isHeader := dataRowIdx == ts.headerRow
		colorizeRenderedRow(cells, colTypes, isHeader)
		dataRowIdx++
	}
}

var (
	colorYellow = parser.Color{Mode: parser.ColorModeStandard, Value: 3}
	colorCyan   = parser.Color{Mode: parser.ColorModeStandard, Value: 6}
	colorGreen  = parser.Color{Mode: parser.ColorModeStandard, Value: 2}
)

// colorizeRenderedRow applies FG colors and attributes to a single rendered row.
// For header rows, all non-border, non-space cells get bold cyan.
// For data rows, content cells get the color matching their column type.
func colorizeRenderedRow(cells []parser.Cell, colTypes []columnType, isHeader bool) {
	if isHeader {
		for i := range cells {
			c := &cells[i]
			if c.Attr&parser.AttrDim != 0 {
				continue
			}
			if c.Rune == ' ' {
				continue
			}
			c.FG = colorCyan
			c.Attr |= parser.AttrBold
		}
		return
	}

	colIdx := -1
	for i := range cells {
		c := &cells[i]
		if c.Attr&parser.AttrDim != 0 && c.Rune == '│' {
			colIdx++
			continue
		}
		if colIdx < 0 || colIdx >= len(colTypes) {
			continue
		}
		if c.Rune == ' ' {
			continue
		}
		switch colTypes[colIdx] {
		case colNumber:
			c.FG = colorYellow
		case colDateTime:
			c.FG = colorCyan
		case colPath:
			c.FG = colorGreen
		}
	}
}

// isBorderLine returns true if all non-space cells in the line have AttrDim set,
// indicating it is a border/separator line (top, bottom, or header separator).
func isBorderLine(cells []parser.Cell) bool {
	for _, c := range cells {
		if c.Rune == ' ' {
			continue
		}
		if c.Attr&parser.AttrDim == 0 {
			return false
		}
	}
	return true
}
