// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

/*
Package testutil provides a framework for debugging visual bugs in texelterm.

Visual bugs in terminal emulators often pass unit tests because the logical
grid contains correct data, but the actual rendered output is wrong due to
dirty line tracking issues. This framework helps detect and diagnose such bugs.

# The Problem

The terminal only re-renders rows marked as "dirty". If a bug causes:
  1. Content written to the wrong logical position
  2. Which maps to a different physical row than the cursor
  3. Only the cursor's row gets marked dirty
  4. The affected row never gets re-rendered -> visual glitch

# Quick Start

For reference terminal comparison (recommended):

	rec := testutil.NewRecording(80, 24)
	rec.AppendCSI("48;5;240m")  // Grey background
	rec.AppendText("Hello")
	rec.AppendCSI("0m")         // Reset

	cmp, err := testutil.NewReferenceComparator(rec)
	if err != nil {
	    // tmux not available
	}

	result, err := cmp.CompareAtEndWithFullDiff()
	if !result.Match {
	    fmt.Println(testutil.FormatEnhancedResult(result))
	}

For dirty tracking simulation:

	replayer := testutil.NewReplayer(rec)
	replayer.PlayAndRender()

	if replayer.HasVisualMismatch() {
	    mismatches := replayer.FindVisualMismatches()
	    for _, m := range mismatches {
	        fmt.Printf("(%d,%d): rendered=%q logical=%q\n",
	            m.X, m.Y, m.Rendered.Rune, m.Logical.Rune)
	    }
	}

# Core Components

## Recording (recorder.go)

Recordings capture terminal output in TXREC01 format:

	// Create programmatically
	rec := testutil.NewRecording(80, 24)
	rec.AppendText("Hello")
	rec.AppendCSI("31m")  // Red foreground
	rec.AppendCRLF()

	// Capture from a command
	rec, err := testutil.CaptureCommand("ls -la", 80, 24)

	// Save and load
	rec.Save("/tmp/session.txrec")
	rec, err := testutil.LoadRecording("/tmp/session.txrec")

## Replayer (replayer.go)

Replays recordings through VTerm with dirty tracking simulation:

	replayer := testutil.NewReplayer(rec)

	// Play all at once
	replayer.PlayAndRender()

	// Or step through
	for !replayer.AtEnd() {
	    replayer.PlayOne()
	    replayer.SimulateRender()
	    // Check state after each byte
	}

	// Access results
	grid := replayer.GetGrid()        // Logical state
	x, y := replayer.GetCursor()      // Cursor position
	snap := replayer.GetSnapshot()    // Full state snapshot

## Reference Comparator (reference.go)

Compares texelterm output against tmux (the ground truth):

	cmp, err := testutil.NewReferenceComparator(rec)

	// Compare final output
	result, err := cmp.CompareAtEnd()

	// Find first divergence point (useful for debugging)
	divergence, err := cmp.FindFirstDivergence(50) // check every 50 bytes

	// With full color support
	result, err := cmp.CompareAtEndWithFullDiff()

	// Get JSON output for automation
	jsonBytes, err := cmp.CompareWithFullDiffToJSON()

## Grid Comparison (comparator.go)

Compare grids and format differences:

	result := testutil.CompareGrids(expected, actual)
	fmt.Println(testutil.FormatSideBySide(grid1, grid2, 40))
	fmt.Println(testutil.FormatLineByLine(result))

## ANSI Parser (ansi_parser.go)

Parses ANSI escape sequences from tmux output:

	parser := testutil.NewANSIParser(80, 24)
	grid := parser.ParseTmuxOutput(output)

# Live Capture

To capture a live terminal session for debugging:

	TEXELTERM_CAPTURE=/tmp/capture.txrec ./bin/texelterm

Then reproduce the visual bug and exit. The recording can be analyzed:

	rec, err := testutil.LoadRecording("/tmp/capture.txrec")
	cmp, err := testutil.NewReferenceComparator(rec)
	divergence, err := cmp.FindFirstDivergence(50)

# Writing Tests

Example test for scroll region dirty tracking:

	func TestScrollRegionDirty(t *testing.T) {
	    rec := testutil.NewRecording(40, 10)
	    rec.AppendCSI("3;7r")    // Set scroll region
	    rec.AppendCSI("5;1H")    // Move to middle
	    rec.AppendSequence([]byte{0x1b, 'M'}) // Reverse index

	    // Compare against tmux
	    cmp, err := testutil.NewReferenceComparator(rec)
	    if err != nil {
	        t.Skip("tmux not available")
	    }

	    result, err := cmp.CompareAtEnd()
	    if !result.Match {
	        t.Errorf("Mismatch: %s", result.Summary)
	    }
	}

# Debugging Workflow

1. Reproduce the bug and capture it:

	TEXELTERM_CAPTURE=/tmp/bug.txrec ./bin/texelterm

2. Load and find divergence:

	rec, _ := testutil.LoadRecording("/tmp/bug.txrec")
	cmp, _ := testutil.NewReferenceComparator(rec)
	div, _ := cmp.FindFirstDivergence(20)
	fmt.Println(testutil.FormatDivergence(div))

3. Create a minimal test case from the divergent chunk.

4. Fix the bug and verify the test passes.

# Files Overview

  - recorder.go: TXREC01 format, shell capture
  - replayer.go: VTerm replay with dirty tracking
  - reference.go: tmux comparison (the ground truth)
  - comparator.go: Grid comparison and diff detection
  - ansi_parser.go: Parse ANSI sequences from tmux
  - format.go: Formatting utilities
  - json_output.go: JSON serialization
  - live_capture.go: Live session capture via TEXELTERM_CAPTURE
  - debug_capture.go: CaptureWriter helper
*/
package testutil
