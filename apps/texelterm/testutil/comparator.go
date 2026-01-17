// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/comparator.go
// Summary: Grid comparison and diff reporting for terminal testing.

package testutil

import (
	"fmt"
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// IssueType categorizes comparison issues.
type IssueType int

const (
	IssueCharMismatch      IssueType = iota // Character differs
	IssueColorMismatch                      // FG or BG color differs
	IssueAttrMismatch                       // Attributes differ
	IssueDirtyTracking                      // RenderBuf != Grid (visual bug)
	IssueCursorMismatch                     // Cursor position differs
	IssueLinefeedOnChar                     // Suspected linefeed-on-each-char bug
	IssueSparseOutput                       // Output appears on too many rows
)

// Severity indicates how serious an issue is.
type Severity int

const (
	SeverityInfo    Severity = iota // Informational
	SeverityWarning                 // Possible issue
	SeverityError                   // Definite problem
)

// Issue represents a single comparison issue.
type Issue struct {
	Type     IssueType
	Severity Severity
	X, Y     int    // Location (if applicable)
	Message  string // Human-readable description
	Expected string // What was expected
	Actual   string // What was found
}

// ComparisonResult holds the full comparison output.
type ComparisonResult struct {
	Passed bool
	Issues []Issue

	// Summary stats
	CharDiffs        int
	ColorDiffs       int
	AttrDiffs        int
	DirtyTrackingErr int
}

// CompareGrids compares two cell grids and returns differences.
func CompareGrids(expected, actual [][]parser.Cell) *ComparisonResult {
	result := &ComparisonResult{Passed: true}

	maxRows := max(len(expected), len(actual))
	for y := 0; y < maxRows; y++ {
		var expRow, actRow []parser.Cell
		if y < len(expected) {
			expRow = expected[y]
		}
		if y < len(actual) {
			actRow = actual[y]
		}

		maxCols := max(len(expRow), len(actRow))
		for x := 0; x < maxCols; x++ {
			var expCell, actCell parser.Cell
			if x < len(expRow) {
				expCell = expRow[x]
			}
			if x < len(actRow) {
				actCell = actRow[x]
			}

			// Compare rune
			if expCell.Rune != actCell.Rune {
				result.Issues = append(result.Issues, Issue{
					Type:     IssueCharMismatch,
					Severity: SeverityError,
					X:        x,
					Y:        y,
					Message:  fmt.Sprintf("Character mismatch at (%d,%d)", x, y),
					Expected: fmt.Sprintf("%q", runeOrSpace(expCell.Rune)),
					Actual:   fmt.Sprintf("%q", runeOrSpace(actCell.Rune)),
				})
				result.CharDiffs++
				result.Passed = false
			}

			// Compare colors
			if expCell.FG != actCell.FG || expCell.BG != actCell.BG {
				result.Issues = append(result.Issues, Issue{
					Type:     IssueColorMismatch,
					Severity: SeverityWarning,
					X:        x,
					Y:        y,
					Message:  fmt.Sprintf("Color mismatch at (%d,%d)", x, y),
					Expected: fmt.Sprintf("fg=%s bg=%s", ColorToString(expCell.FG), ColorToString(expCell.BG)),
					Actual:   fmt.Sprintf("fg=%s bg=%s", ColorToString(actCell.FG), ColorToString(actCell.BG)),
				})
				result.ColorDiffs++
			}

			// Compare attributes
			if expCell.Attr != actCell.Attr {
				result.Issues = append(result.Issues, Issue{
					Type:     IssueAttrMismatch,
					Severity: SeverityWarning,
					X:        x,
					Y:        y,
					Message:  fmt.Sprintf("Attribute mismatch at (%d,%d)", x, y),
					Expected: AttrToString(expCell.Attr),
					Actual:   AttrToString(actCell.Attr),
				})
				result.AttrDiffs++
			}
		}
	}

	return result
}

// CompareSnapshots compares two snapshots (includes cursor and dirty tracking).
func CompareSnapshots(expected, actual *Snapshot) *ComparisonResult {
	result := CompareGrids(expected.Grid, actual.Grid)

	// Compare cursor position
	if expected.CursorX != actual.CursorX || expected.CursorY != actual.CursorY {
		result.Issues = append(result.Issues, Issue{
			Type:     IssueCursorMismatch,
			Severity: SeverityError,
			X:        actual.CursorX,
			Y:        actual.CursorY,
			Message:  "Cursor position mismatch",
			Expected: fmt.Sprintf("(%d,%d)", expected.CursorX, expected.CursorY),
			Actual:   fmt.Sprintf("(%d,%d)", actual.CursorX, actual.CursorY),
		})
		result.Passed = false
	}

	return result
}

// DetectDirtyTrackingIssues checks if RenderBuf differs from Grid in a snapshot.
// This is critical for detecting visual bugs per CLAUDE.md.
func DetectDirtyTrackingIssues(snap *Snapshot) *ComparisonResult {
	result := CompareGrids(snap.Grid, snap.RenderBuf)

	// Reclassify all issues as dirty tracking errors
	for i := range result.Issues {
		if result.Issues[i].Type == IssueCharMismatch {
			result.Issues[i].Type = IssueDirtyTracking
			result.Issues[i].Message = fmt.Sprintf("Dirty tracking failure at (%d,%d): Grid has %s but render shows %s",
				result.Issues[i].X, result.Issues[i].Y,
				result.Issues[i].Expected, result.Issues[i].Actual)
			result.DirtyTrackingErr++
		}
	}

	return result
}

// DetectLinefeedOnChar checks for the specific "linefeed on each character" bug.
// Returns true if typing appears to cause excessive line feeds.
func DetectLinefeedOnChar(snap *Snapshot, inputLength int) bool {
	if inputLength == 0 {
		return false
	}

	// Count non-empty rows
	nonEmptyRows := 0
	for _, row := range snap.Grid {
		hasContent := false
		for _, cell := range row {
			if cell.Rune != 0 && cell.Rune != ' ' {
				hasContent = true
				break
			}
		}
		if hasContent {
			nonEmptyRows++
		}
	}

	// If each character went to a new line, nonEmptyRows would equal inputLength
	// A threshold of 50% suggests the bug
	return nonEmptyRows > inputLength/2 && inputLength > 3
}

// FormatSideBySide formats two grids side by side for visual comparison.
func FormatSideBySide(expected, actual [][]parser.Cell, width int) string {
	var sb strings.Builder

	// Header
	expHeader := "EXPECTED"
	actHeader := "ACTUAL"
	padding := strings.Repeat(" ", width-len(expHeader))
	sb.WriteString(expHeader + padding + " | " + actHeader + "\n")
	sb.WriteString(strings.Repeat("-", width) + "-+-" + strings.Repeat("-", width) + "\n")

	maxRows := max(len(expected), len(actual))
	for y := 0; y < maxRows; y++ {
		// Expected side
		if y < len(expected) {
			sb.WriteString(rowToString(expected[y], width))
		} else {
			sb.WriteString(strings.Repeat(" ", width))
		}

		sb.WriteString(" | ")

		// Actual side
		if y < len(actual) {
			sb.WriteString(rowToString(actual[y], width))
		} else {
			sb.WriteString(strings.Repeat(" ", width))
		}

		// Mark differences
		if y < len(expected) && y < len(actual) {
			if !rowsEqual(expected[y], actual[y]) {
				sb.WriteString(" <-- DIFF")
			}
		}

		sb.WriteString(fmt.Sprintf(" |%d\n", y))
	}

	return sb.String()
}

// FormatLineByLine formats comparison as a unified diff-style output.
func FormatLineByLine(result *ComparisonResult) string {
	var sb strings.Builder

	if result.Passed {
		sb.WriteString("PASSED: No differences found\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("FAILED: %d character diffs, %d color diffs, %d attr diffs, %d dirty tracking errors\n\n",
		result.CharDiffs, result.ColorDiffs, result.AttrDiffs, result.DirtyTrackingErr))

	// Group issues by row
	issuesByRow := make(map[int][]Issue)
	for _, issue := range result.Issues {
		issuesByRow[issue.Y] = append(issuesByRow[issue.Y], issue)
	}

	// Output by row
	for y := 0; y < 1000; y++ { // Arbitrary max
		issues, ok := issuesByRow[y]
		if !ok {
			continue
		}

		sb.WriteString(fmt.Sprintf("Row %d:\n", y))
		for _, issue := range issues {
			severity := "INFO"
			if issue.Severity == SeverityWarning {
				severity = "WARN"
			} else if issue.Severity == SeverityError {
				severity = "ERROR"
			}

			sb.WriteString(fmt.Sprintf("  [%s] col %d: %s\n", severity, issue.X, issue.Message))
			sb.WriteString(fmt.Sprintf("         expected: %s\n", issue.Expected))
			sb.WriteString(fmt.Sprintf("         actual:   %s\n", issue.Actual))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatEscapeLog returns the recording's escape sequences in readable format.
func FormatEscapeLog(rec *Recording) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Recording: %s\n", rec.Metadata.Description))
	sb.WriteString(fmt.Sprintf("Size: %dx%d\n", rec.Metadata.Width, rec.Metadata.Height))
	sb.WriteString(fmt.Sprintf("Shell: %s\n", rec.Metadata.Shell))
	sb.WriteString(fmt.Sprintf("Bytes: %d\n", len(rec.Sequences)))
	sb.WriteString(strings.Repeat("-", 60) + "\n")
	sb.WriteString(EscapeSequenceLog(rec.Sequences))
	sb.WriteString("\n" + strings.Repeat("-", 60) + "\n")

	return sb.String()
}

// FormatSnapshot formats a snapshot for debugging.
func FormatSnapshot(snap *Snapshot) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Snapshot at byte %d, render %d\n", snap.ByteIndex, snap.RenderCount))
	sb.WriteString(fmt.Sprintf("Cursor: (%d,%d)\n", snap.CursorX, snap.CursorY))
	sb.WriteString(fmt.Sprintf("AllDirty: %v, DirtyLines: %v\n", snap.AllDirty, snap.DirtyLines))
	sb.WriteString("\nGrid (logical):\n")
	sb.WriteString(GridToStringWithCursor(snap.Grid, snap.CursorX, snap.CursorY))
	sb.WriteString("\nRenderBuf (visual):\n")
	sb.WriteString(GridToStringWithCursor(snap.RenderBuf, snap.CursorX, snap.CursorY))

	return sb.String()
}

// Helper functions

func runeOrSpace(r rune) rune {
	if r == 0 {
		return ' '
	}
	return r
}

func rowToString(row []parser.Cell, width int) string {
	var sb strings.Builder
	for i := 0; i < width; i++ {
		if i < len(row) {
			if row[i].Rune == 0 {
				sb.WriteRune('.')
			} else {
				sb.WriteRune(row[i].Rune)
			}
		} else {
			sb.WriteRune('.')
		}
	}
	return sb.String()
}

func rowsEqual(a, b []parser.Cell) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Rune != b[i].Rune {
			return false
		}
	}
	return true
}
