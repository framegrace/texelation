// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/format.go
// Summary: Output formatting utilities for terminal comparison testing.

package testutil

import (
	"fmt"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// GridToString converts a cell grid to a readable string representation.
// Null runes are displayed as dots for visibility.
func GridToString(grid [][]parser.Cell) string {
	var sb strings.Builder
	for y, row := range grid {
		sb.WriteString("[")
		for _, cell := range row {
			if cell.Rune == 0 || cell.Rune == ' ' {
				sb.WriteRune('.')
			} else {
				sb.WriteRune(cell.Rune)
			}
		}
		sb.WriteString("]")
		if y < len(grid)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// GridToStringWithCursor converts a grid to string with cursor position marked.
func GridToStringWithCursor(grid [][]parser.Cell, cursorX, cursorY int) string {
	var sb strings.Builder
	for y, row := range grid {
		sb.WriteString("[")
		for x, cell := range row {
			if x == cursorX && y == cursorY {
				sb.WriteString("\u2588") // Full block to mark cursor
			} else if cell.Rune == 0 || cell.Rune == ' ' {
				sb.WriteRune('.')
			} else {
				sb.WriteRune(cell.Rune)
			}
		}
		sb.WriteString("]")
		sb.WriteString(fmt.Sprintf(" |%d", y))
		if y == cursorY {
			sb.WriteString(" <-- cursor")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// CellsToString converts a row of cells to a string.
func CellsToString(cells []parser.Cell) string {
	var sb strings.Builder
	for _, cell := range cells {
		if cell.Rune == 0 {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(cell.Rune)
		}
	}
	return sb.String()
}

// EscapeSequenceLog formats raw bytes as a readable escape sequence log.
// Shows hex values for control characters and escape sequences.
func EscapeSequenceLog(data []byte) string {
	var sb strings.Builder
	i := 0
	for i < len(data) {
		b := data[i]
		switch {
		case b == 0x1b: // ESC
			sb.WriteString("\n<ESC>")
			i++
			if i < len(data) {
				next := data[i]
				switch next {
				case '[':
					sb.WriteString("[")
					i++
					// Read CSI sequence until final byte
					for i < len(data) && data[i] >= 0x20 && data[i] <= 0x3f {
						sb.WriteByte(data[i])
						i++
					}
					if i < len(data) && data[i] >= 0x40 && data[i] <= 0x7e {
						sb.WriteByte(data[i])
						i++
					}
				case ']':
					sb.WriteString("]")
					i++
					// Read OSC until BEL or ST
					for i < len(data) && data[i] != 0x07 && data[i] != 0x1b {
						if data[i] >= 0x20 && data[i] < 0x7f {
							sb.WriteByte(data[i])
						} else {
							sb.WriteString(fmt.Sprintf("\\x%02x", data[i]))
						}
						i++
					}
					if i < len(data) && data[i] == 0x07 {
						sb.WriteString("<BEL>")
						i++
					}
				default:
					sb.WriteString(fmt.Sprintf("%c", next))
					i++
				}
			}
		case b == '\n':
			sb.WriteString("<LF>\n")
			i++
		case b == '\r':
			sb.WriteString("<CR>")
			i++
		case b == '\t':
			sb.WriteString("<TAB>")
			i++
		case b == 0x08:
			sb.WriteString("<BS>")
			i++
		case b == 0x07:
			sb.WriteString("<BEL>")
			i++
		case b < 0x20:
			sb.WriteString(fmt.Sprintf("<0x%02x>", b))
			i++
		case b >= 0x20 && b < 0x7f:
			sb.WriteByte(b)
			i++
		default:
			sb.WriteString(fmt.Sprintf("\\x%02x", b))
			i++
		}
	}
	return sb.String()
}

// ColorToString formats a Color for display.
func ColorToString(c parser.Color) string {
	switch c.Mode {
	case parser.ColorModeDefault:
		return "default"
	case parser.ColorModeStandard:
		names := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}
		if int(c.Value) < len(names) {
			return names[c.Value]
		}
		return fmt.Sprintf("std(%d)", c.Value)
	case parser.ColorMode256:
		return fmt.Sprintf("256(%d)", c.Value)
	case parser.ColorModeRGB:
		return fmt.Sprintf("rgb(%d,%d,%d)", c.R, c.G, c.B)
	default:
		return "unknown"
	}
}

// AttrToString formats attributes for display.
func AttrToString(attr parser.Attribute) string {
	var parts []string
	if attr&parser.AttrBold != 0 {
		parts = append(parts, "bold")
	}
	if attr&parser.AttrUnderline != 0 {
		parts = append(parts, "underline")
	}
	if attr&parser.AttrReverse != 0 {
		parts = append(parts, "reverse")
	}
	if len(parts) == 0 {
		return "normal"
	}
	return strings.Join(parts, "+")
}

// CellToDetailedString formats a cell with all its attributes.
func CellToDetailedString(c parser.Cell) string {
	r := c.Rune
	if r == 0 {
		r = ' '
	}
	return fmt.Sprintf("'%c' fg=%s bg=%s attr=%s wrapped=%v",
		r, ColorToString(c.FG), ColorToString(c.BG), AttrToString(c.Attr), c.Wrapped)
}

// ============================================================================
// Enhanced Formatting for Full Color/Attribute Comparison
// ============================================================================

// FormatEnhancedDivergence formats a divergence with full cell details.
func FormatEnhancedDivergence(d *EnhancedDivergence) string {
	var sb strings.Builder

	sb.WriteString("=== DIVERGENCE FOUND (WITH COLORS) ===\n")
	sb.WriteString(fmt.Sprintf("Byte range: %d-%d\n", d.ByteIndex, d.ByteEndIndex))
	sb.WriteString(fmt.Sprintf("Chunk (readable): %s\n", d.ChunkReadable))
	sb.WriteString(fmt.Sprintf("\nComparison: %s\n", d.Comparison.Summary))
	sb.WriteString(fmt.Sprintf("  Total cells: %d\n", d.Comparison.TotalCells))
	sb.WriteString(fmt.Sprintf("  Character diffs: %d\n", d.Comparison.CharDiffs))
	sb.WriteString(fmt.Sprintf("  Color diffs: %d\n", d.Comparison.ColorDiffs))
	sb.WriteString(fmt.Sprintf("  Attribute diffs: %d\n", d.Comparison.AttrDiffs))

	// Show first few differences
	maxShow := 15
	sb.WriteString("\nFirst differences:\n")
	for i, diff := range d.Comparison.Differences {
		if i >= maxShow {
			sb.WriteString(fmt.Sprintf("... and %d more differences\n",
				len(d.Comparison.Differences)-maxShow))
			break
		}
		sb.WriteString(fmt.Sprintf("  (%d,%d) [%s]: %s\n",
			diff.X, diff.Y, diff.DiffType, diff.DiffDesc))
	}

	return sb.String()
}

// FormatEnhancedDiff formats a single cell difference with full details.
func FormatEnhancedDiff(diff EnhancedDiff) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Position (%d,%d) - Type: %s\n", diff.X, diff.Y, diff.DiffType))
	sb.WriteString("  Reference (tmux):\n")
	sb.WriteString(fmt.Sprintf("    char: %s (%s)\n", diff.Reference.Rune, diff.Reference.RuneHex))
	sb.WriteString(fmt.Sprintf("    fg: %s\n", diff.Reference.FG.Display))
	sb.WriteString(fmt.Sprintf("    bg: %s\n", diff.Reference.BG.Display))
	sb.WriteString(fmt.Sprintf("    attr: %s\n", diff.Reference.Attr.Display))
	sb.WriteString("  Texelterm:\n")
	sb.WriteString(fmt.Sprintf("    char: %s (%s)\n", diff.Texelterm.Rune, diff.Texelterm.RuneHex))
	sb.WriteString(fmt.Sprintf("    fg: %s\n", diff.Texelterm.FG.Display))
	sb.WriteString(fmt.Sprintf("    bg: %s\n", diff.Texelterm.BG.Display))
	sb.WriteString(fmt.Sprintf("    attr: %s\n", diff.Texelterm.Attr.Display))
	return sb.String()
}

// FormatCellComparison shows before/after for a cell in a compact format.
func FormatCellComparison(ref, actual CellInfo) string {
	return fmt.Sprintf("'%s'[%s/%s/%s] vs '%s'[%s/%s/%s]",
		ref.Rune, ref.FG.Display, ref.BG.Display, ref.Attr.Display,
		actual.Rune, actual.FG.Display, actual.BG.Display, actual.Attr.Display)
}

// FormatEscapeContext shows escape sequences around a byte position.
func FormatEscapeContext(data []byte, byteIndex int, contextSize int) string {
	start := byteIndex - contextSize
	if start < 0 {
		start = 0
	}
	end := byteIndex + contextSize
	if end > len(data) {
		end = len(data)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Context around byte %d:\n", byteIndex))
	sb.WriteString("  Before: ")
	if start < byteIndex {
		sb.WriteString(EscapeSequenceLog(data[start:byteIndex]))
	}
	sb.WriteString("\n  After: ")
	if byteIndex < end {
		sb.WriteString(EscapeSequenceLog(data[byteIndex:end]))
	}
	sb.WriteString("\n")
	return sb.String()
}

// FormatEnhancedResult formats an EnhancedComparisonResult for human readability.
func FormatEnhancedResult(r *EnhancedComparisonResult) string {
	var sb strings.Builder

	if r.Match {
		sb.WriteString("=== COMPARISON PASSED ===\n")
		sb.WriteString(fmt.Sprintf("Total cells: %d\n", r.TotalCells))
		sb.WriteString(fmt.Sprintf("Bytes processed: %d\n", r.BytesProcessed))
		return sb.String()
	}

	sb.WriteString("=== COMPARISON FAILED ===\n")
	sb.WriteString(fmt.Sprintf("Summary: %s\n", r.Summary))
	sb.WriteString(fmt.Sprintf("Total cells: %d\n", r.TotalCells))
	sb.WriteString(fmt.Sprintf("Differences: %d\n", r.DiffCount))
	sb.WriteString(fmt.Sprintf("  Character: %d\n", r.CharDiffs))
	sb.WriteString(fmt.Sprintf("  Color: %d\n", r.ColorDiffs))
	sb.WriteString(fmt.Sprintf("  Attribute: %d\n", r.AttrDiffs))
	sb.WriteString(fmt.Sprintf("Bytes processed: %d\n", r.BytesProcessed))

	// Group differences by type
	charDiffs := FilterDiffsByType(r.Differences, DiffTypeChar)
	colorDiffs := FilterDiffsByType(r.Differences, DiffTypeFG, DiffTypeBG)
	attrDiffs := FilterDiffsByType(r.Differences, DiffTypeAttr)
	combinedDiffs := FilterDiffsByType(r.Differences, DiffTypeCombined)

	if len(charDiffs) > 0 {
		sb.WriteString("\n--- Character Differences ---\n")
		for i, diff := range charDiffs {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(charDiffs)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  (%d,%d): %s\n", diff.X, diff.Y, diff.DiffDesc))
		}
	}

	if len(colorDiffs) > 0 {
		sb.WriteString("\n--- Color Differences ---\n")
		for i, diff := range colorDiffs {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(colorDiffs)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  (%d,%d): %s\n", diff.X, diff.Y, diff.DiffDesc))
		}
	}

	if len(attrDiffs) > 0 {
		sb.WriteString("\n--- Attribute Differences ---\n")
		for i, diff := range attrDiffs {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(attrDiffs)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  (%d,%d): %s\n", diff.X, diff.Y, diff.DiffDesc))
		}
	}

	if len(combinedDiffs) > 0 {
		sb.WriteString("\n--- Combined Differences ---\n")
		for i, diff := range combinedDiffs {
			if i >= 10 {
				sb.WriteString(fmt.Sprintf("... and %d more\n", len(combinedDiffs)-10))
				break
			}
			sb.WriteString(fmt.Sprintf("  (%d,%d): %s\n", diff.X, diff.Y, diff.DiffDesc))
		}
	}

	return sb.String()
}

// FormatGridRowWithColors formats a single row showing color info.
func FormatGridRowWithColors(row []parser.Cell, maxWidth int) string {
	var sb strings.Builder
	sb.WriteString("[")
	for x := 0; x < maxWidth && x < len(row); x++ {
		cell := row[x]
		r := cell.Rune
		if r == 0 {
			r = '.'
		}
		sb.WriteRune(r)
	}
	sb.WriteString("]")

	// Add color summary
	colorChanges := 0
	prevBG := parser.Color{Mode: parser.ColorModeDefault}
	for x := 0; x < maxWidth && x < len(row); x++ {
		if row[x].BG != prevBG {
			colorChanges++
			prevBG = row[x].BG
		}
	}
	if colorChanges > 0 {
		sb.WriteString(fmt.Sprintf(" (%d color changes)", colorChanges))
	}

	return sb.String()
}
