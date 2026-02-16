// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// TestTableFmt_OverlayIntegration exercises the complete pipeline:
// terminal output -> transformer detects CSV -> overlay/insert callbacks -> persist notifications.
func TestTableFmt_OverlayIntegration(t *testing.T) {
	tf := New(1000)

	overlays := make(map[int64][]parser.Cell)
	var inserts []struct {
		idx   int64
		cells []parser.Cell
	}
	var persisted []int64

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

	// Simulate CSV output lines (3 lines with 3 columns each).
	csvLines := []string{
		"Name,Age,City",
		"Alice,30,NYC",
		"Bob,25,LA",
	}

	// Enable shell integration so isCommand flag is respected.
	tf.NotifyPromptStart()

	// Feed lines as command output (isCommand=true).
	for i, text := range csvLines {
		line := parser.NewLogicalLineFromCells(makePlainCells(text))
		tf.HandleLine(int64(i), line, true)
	}

	// Verify lines were suppressed while buffering.
	if tf.state != stateBuffering {
		t.Errorf("expected stateBuffering after CSV lines, got %d", tf.state)
	}

	// Flush by simulating prompt (command -> prompt transition).
	// The empty line with isCommand=false triggers flush.
	tf.HandleLine(3, parser.NewLogicalLine(), false)

	// After flush, state should be back to scanning.
	if tf.state != stateScanning {
		t.Errorf("expected stateScanning after flush, got %d", tf.state)
	}

	// Verify overlays or inserts were created.
	totalFormatted := len(overlays) + len(inserts)
	if totalFormatted == 0 {
		t.Error("expected overlay/insert operations after table flush")
	}

	// Verify persist was called for buffered lines.
	if len(persisted) == 0 {
		t.Error("expected persistence notifications after flush")
	}

	t.Logf("Overlays: %d, Inserts: %d, Persisted: %d",
		len(overlays), len(inserts), len(persisted))
}

// makePlainCells creates a slice of cells with default colors from a string.
func makePlainCells(text string) []parser.Cell {
	cells := make([]parser.Cell, len([]rune(text)))
	for i, r := range text {
		cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}
	return cells
}
