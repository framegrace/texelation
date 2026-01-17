// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/enhanced_comparison_test.go
// Summary: Tests for enhanced terminal comparison with full color/attribute support.

package testutil_test

import (
	"encoding/json"
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/testutil"
)

// TestANSIParserBasicText tests basic text parsing.
func TestANSIParserBasicText(t *testing.T) {
	p := testutil.NewANSIParser(20, 5)
	grid := p.ParseTmuxOutput([]byte("Hello"))

	// Check "Hello" at position 0,0
	expected := []rune{'H', 'e', 'l', 'l', 'o'}
	for i, r := range expected {
		if grid[0][i].Rune != r {
			t.Errorf("Position %d: expected %q, got %q", i, r, grid[0][i].Rune)
		}
	}
}

// TestANSIParserColors tests color parsing.
func TestANSIParserColors(t *testing.T) {
	p := testutil.NewANSIParser(20, 5)
	// ESC[48;5;240m sets grey256 background, then text
	data := []byte("\x1b[48;5;240mGrey")
	grid := p.ParseTmuxOutput(data)

	// Check 'G' has grey background
	cell := grid[0][0]
	if cell.Rune != 'G' {
		t.Errorf("Expected 'G', got %q", cell.Rune)
	}
	if cell.BG.Mode != parser.ColorMode256 || cell.BG.Value != 240 {
		t.Errorf("Expected 256-color grey background, got mode=%d value=%d",
			cell.BG.Mode, cell.BG.Value)
	}
}

// TestANSIParserReset tests SGR reset.
func TestANSIParserReset(t *testing.T) {
	p := testutil.NewANSIParser(20, 5)
	// Set red, write A, reset, write B
	data := []byte("\x1b[31mA\x1b[0mB")
	grid := p.ParseTmuxOutput(data)

	// A should be red
	if grid[0][0].Rune != 'A' {
		t.Errorf("Expected 'A', got %q", grid[0][0].Rune)
	}
	if grid[0][0].FG.Mode != parser.ColorModeStandard || grid[0][0].FG.Value != 1 {
		t.Errorf("A should have red foreground")
	}

	// B should be default
	if grid[0][1].Rune != 'B' {
		t.Errorf("Expected 'B', got %q", grid[0][1].Rune)
	}
	if grid[0][1].FG.Mode != parser.ColorModeDefault {
		t.Errorf("B should have default foreground")
	}
}

// TestANSIParserCursorMovement tests cursor movement sequences.
func TestANSIParserCursorMovement(t *testing.T) {
	p := testutil.NewANSIParser(20, 10)
	// Write X at 0,0 then move to row 5, col 10 and write Y
	data := []byte("X\x1b[5;10HY")
	grid := p.ParseTmuxOutput(data)

	if grid[0][0].Rune != 'X' {
		t.Errorf("Expected 'X' at (0,0), got %q", grid[0][0].Rune)
	}
	// Row 5, col 10 (1-indexed) = row 4, col 9 (0-indexed)
	if grid[4][9].Rune != 'Y' {
		t.Errorf("Expected 'Y' at (4,9), got %q", grid[4][9].Rune)
	}
}

// TestEnhancedCompareGrids tests full cell comparison.
func TestEnhancedCompareGrids(t *testing.T) {
	// Create two grids with differences
	grid1 := [][]parser.Cell{
		{{Rune: 'H', BG: parser.Color{Mode: parser.ColorMode256, Value: 240}}, {Rune: 'i'}},
		{{Rune: 'X'}},
	}
	grid2 := [][]parser.Cell{
		{{Rune: 'H', BG: parser.Color{Mode: parser.ColorModeDefault}}, {Rune: 'i'}},
		{{Rune: 'Y'}},
	}

	result := testutil.EnhancedCompareGrids(grid1, grid2, 2, 2)

	if result.Match {
		t.Error("Expected comparison to fail")
	}
	if result.DiffCount != 2 {
		t.Errorf("Expected 2 diffs, got %d", result.DiffCount)
	}

	// Should have one color diff and one char diff
	hasColorDiff := false
	hasCharDiff := false
	for _, diff := range result.Differences {
		if diff.DiffType == testutil.DiffTypeBG {
			hasColorDiff = true
		}
		if diff.DiffType == testutil.DiffTypeChar {
			hasCharDiff = true
		}
	}
	if !hasColorDiff {
		t.Error("Expected color diff for first cell")
	}
	if !hasCharDiff {
		t.Error("Expected char diff for second row")
	}
}

// TestEnhancedComparisonJSON tests JSON serialization.
func TestEnhancedComparisonJSON(t *testing.T) {
	result := &testutil.EnhancedComparisonResult{
		Match:       false,
		Summary:     "Test summary",
		DiffCount:   1,
		CharDiffs:   1,
		TotalCells:  100,
		Differences: []testutil.EnhancedDiff{
			{
				X:        5,
				Y:        10,
				DiffType: testutil.DiffTypeChar,
				DiffDesc: "char: 'A' vs 'B'",
				Reference: testutil.CellInfo{
					Rune: "A",
					FG:   testutil.ColorInfo{Mode: "default", Display: "default"},
					BG:   testutil.ColorInfo{Mode: "default", Display: "default"},
				},
				Texelterm: testutil.CellInfo{
					Rune: "B",
					FG:   testutil.ColorInfo{Mode: "default", Display: "default"},
					BG:   testutil.ColorInfo{Mode: "default", Display: "default"},
				},
			},
		},
	}

	jsonBytes, err := result.ToJSON()
	if err != nil {
		t.Fatalf("JSON serialization failed: %v", err)
	}

	// Parse back
	var parsed testutil.EnhancedComparisonResult
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("JSON parse failed: %v", err)
	}

	if parsed.Match != result.Match {
		t.Error("Match field mismatch")
	}
	if parsed.DiffCount != result.DiffCount {
		t.Error("DiffCount mismatch")
	}
	if len(parsed.Differences) != 1 {
		t.Error("Differences count mismatch")
	}
	if parsed.Differences[0].X != 5 || parsed.Differences[0].Y != 10 {
		t.Error("Difference position mismatch")
	}
}

// TestColorToInfo tests color conversion to JSON format.
func TestColorToInfo(t *testing.T) {
	tests := []struct {
		name     string
		color    parser.Color
		expected string
	}{
		{"default", parser.Color{Mode: parser.ColorModeDefault}, "default"},
		{"standard red", parser.Color{Mode: parser.ColorModeStandard, Value: 1}, "red"},
		{"256 grey", parser.Color{Mode: parser.ColorMode256, Value: 240}, "256(240)"},
		{"RGB", parser.Color{Mode: parser.ColorModeRGB, R: 255, G: 128, B: 0}, "rgb(255,128,0)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info := testutil.ColorToInfo(tc.color)
			if info.Display != tc.expected {
				t.Errorf("Expected %q, got %q", tc.expected, info.Display)
			}
		})
	}
}

// TestFilterDiffsByType tests filtering differences.
func TestFilterDiffsByType(t *testing.T) {
	diffs := []testutil.EnhancedDiff{
		{X: 0, Y: 0, DiffType: testutil.DiffTypeChar},
		{X: 1, Y: 0, DiffType: testutil.DiffTypeFG},
		{X: 2, Y: 0, DiffType: testutil.DiffTypeBG},
		{X: 3, Y: 0, DiffType: testutil.DiffTypeAttr},
		{X: 4, Y: 0, DiffType: testutil.DiffTypeCombined},
	}

	// Filter for char only
	charDiffs := testutil.FilterDiffsByType(diffs, testutil.DiffTypeChar)
	if len(charDiffs) != 1 {
		t.Errorf("Expected 1 char diff, got %d", len(charDiffs))
	}

	// Filter for colors
	colorDiffs := testutil.FilterDiffsByType(diffs, testutil.DiffTypeFG, testutil.DiffTypeBG)
	if len(colorDiffs) != 2 {
		t.Errorf("Expected 2 color diffs, got %d", len(colorDiffs))
	}
}

// TestLiveCaptureBasic tests live capture functionality.
func TestLiveCaptureBasic(t *testing.T) {
	capture := testutil.NewLiveCapture("/tmp/test-capture.txrec")

	if err := capture.StartCapture(80, 24); err != nil {
		t.Fatalf("StartCapture failed: %v", err)
	}

	capture.CaptureBytes([]byte("Hello"))
	capture.CaptureBytes([]byte("\x1b[31mRed\x1b[0m"))

	// "Hello" = 5 bytes, "\x1b[31mRed\x1b[0m" = 12 bytes = 17 total
	expectedBytes := 5 + len("\x1b[31mRed\x1b[0m")
	if count := capture.GetByteCount(); count != expectedBytes {
		t.Errorf("Expected %d bytes captured, got %d", expectedBytes, count)
	}

	// Abort to clean up without writing
	capture.Abort()
}

// TestReferenceCompareWithColorsBasic tests enhanced reference comparison.
func TestReferenceCompareWithColorsBasic(t *testing.T) {
	rec := testutil.NewRecording(40, 10)
	rec.AppendText("Hello")
	rec.AppendCSI("31m") // Red
	rec.AppendText(" World")
	rec.AppendCSI("0m") // Reset

	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("CompareAtEndWithFullDiff failed: %v", err)
	}

	t.Logf("Match: %v", result.Match)
	t.Logf("Summary: %s", result.Summary)
	t.Logf("Total cells: %d", result.TotalCells)
	t.Logf("Differences: %d", result.DiffCount)

	if !result.Match {
		for i, diff := range result.Differences {
			if i > 10 {
				break
			}
			t.Logf("  (%d,%d) [%s]: %s", diff.X, diff.Y, diff.DiffType, diff.DiffDesc)
		}
	}
}

// TestReferenceCompareGreyBackground tests grey background comparison.
func TestReferenceCompareGreyBackground(t *testing.T) {
	rec := testutil.NewRecording(40, 10)

	// Set grey background and write text
	rec.AppendCSI("48;5;240m") // Grey 256 background
	rec.AppendText("Grey background text")
	rec.AppendCSI("0m") // Reset

	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if err != nil {
		t.Fatalf("CompareAtEndWithFullDiff failed: %v", err)
	}

	t.Logf("Match: %v", result.Match)
	t.Logf("Color differences: %d", result.ColorDiffs)

	// Log any color differences
	if result.ColorDiffs > 0 {
		t.Log("Color differences found:")
		colorDiffs := testutil.FilterDiffsByType(result.Differences, testutil.DiffTypeFG, testutil.DiffTypeBG)
		for i, diff := range colorDiffs {
			if i > 5 {
				break
			}
			t.Logf("  (%d,%d): %s", diff.X, diff.Y, diff.DiffDesc)
		}
	}
}

// TestJSONOutputFormat tests the JSON output is valid and parseable.
func TestJSONOutputFormat(t *testing.T) {
	rec := testutil.NewRecording(20, 5)
	rec.AppendText("Test")

	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
		t.Skipf("Reference comparator unavailable: %v", err)
	}

	jsonBytes, err := cmp.CompareWithFullDiffToJSON()
	if err != nil {
		t.Fatalf("CompareWithFullDiffToJSON failed: %v", err)
	}

	// Verify valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		t.Fatalf("Invalid JSON: %v\nJSON: %s", err, string(jsonBytes))
	}

	// Check expected fields
	if _, ok := result["match"]; !ok {
		t.Error("JSON missing 'match' field")
	}
	if _, ok := result["summary"]; !ok {
		t.Error("JSON missing 'summary' field")
	}
	if _, ok := result["differences"]; !ok {
		t.Error("JSON missing 'differences' field")
	}

	t.Logf("JSON output:\n%s", string(jsonBytes))
}

// TestFormatEnhancedResult tests human-readable formatting.
func TestFormatEnhancedResult(t *testing.T) {
	result := &testutil.EnhancedComparisonResult{
		Match:      false,
		Summary:    "Found 3 differences (2 char, 1 color, 0 attr)",
		DiffCount:  3,
		CharDiffs:  2,
		ColorDiffs: 1,
		TotalCells: 100,
		Differences: []testutil.EnhancedDiff{
			{X: 0, Y: 0, DiffType: testutil.DiffTypeChar, DiffDesc: "char: 'A' vs 'B'"},
			{X: 1, Y: 0, DiffType: testutil.DiffTypeChar, DiffDesc: "char: 'X' vs 'Y'"},
			{X: 2, Y: 0, DiffType: testutil.DiffTypeBG, DiffDesc: "bg: 256(240) vs default"},
		},
	}

	output := testutil.FormatEnhancedResult(result)

	if len(output) == 0 {
		t.Error("Expected non-empty formatted output")
	}

	t.Logf("Formatted output:\n%s", output)

	// Check for expected sections
	if !contains(output, "COMPARISON FAILED") {
		t.Error("Expected 'COMPARISON FAILED' in output")
	}
	if !contains(output, "Character Differences") {
		t.Error("Expected 'Character Differences' in output")
	}
	if !contains(output, "Color Differences") {
		t.Error("Expected 'Color Differences' in output")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
