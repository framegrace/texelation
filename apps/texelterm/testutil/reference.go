// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/reference.go
// Summary: Reference terminal comparison using tmux as the ground truth.
//
// This allows comparing texelterm's output against a real terminal (tmux)
// to find exactly where they diverge. Useful for debugging visual bugs.
//
// Usage:
//   cmp, err := NewReferenceComparator(rec)
//   result := cmp.CompareAtEnd()
//   // or for step-by-step:
//   firstDiff := cmp.FindFirstDivergence(100) // check every 100 bytes

package testutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// ReferenceComparator compares texelterm against a reference terminal (tmux).
type ReferenceComparator struct {
	recording *Recording
	replayer  *Replayer

	// tmux session info
	tmuxSession string
	width       int
	height      int

	// Accumulated sequences for incremental comparison
	accumulatedData []byte

	// Comparison results
	divergencePoint int    // Byte index where first divergence occurred
	divergenceDesc  string // Description of the divergence
}

// NewReferenceComparator creates a comparator for a recording.
// Requires tmux to be installed and available in PATH.
func NewReferenceComparator(rec *Recording) (*ReferenceComparator, error) {
	// Validate recording dimensions
	if rec.Metadata.Width <= 0 || rec.Metadata.Height <= 0 {
		return nil, fmt.Errorf("invalid recording dimensions: %dx%d",
			rec.Metadata.Width, rec.Metadata.Height)
	}

	// Check if tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux not found in PATH: %w", err)
	}

	replayer := NewReplayer(rec)

	rc := &ReferenceComparator{
		recording:       rec,
		replayer:        replayer,
		width:           rec.Metadata.Width,
		height:          rec.Metadata.Height,
		divergencePoint: -1,
	}

	return rc, nil
}

// startTmuxSession creates a detached tmux session with specific dimensions.
// It runs a simple command that waits for input rather than a shell to avoid
// prompt interference.
func (rc *ReferenceComparator) startTmuxSession() error {
	// Generate unique session name
	rc.tmuxSession = fmt.Sprintf("texelterm-test-%d", time.Now().UnixNano())

	// Create new detached session with specific size running 'sleep infinity'
	// This gives us a clean terminal with no shell prompt
	cmd := exec.Command("tmux", "new-session", "-d",
		"-s", rc.tmuxSession,
		"-x", strconv.Itoa(rc.width),
		"-y", strconv.Itoa(rc.height),
		"sleep", "infinity",
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tmux session: %v, output: %s", err, output)
	}

	// Wait a moment for the session to initialize
	time.Sleep(50 * time.Millisecond)

	return nil
}

// stopTmuxSession kills the tmux session.
func (rc *ReferenceComparator) stopTmuxSession() {
	if rc.tmuxSession == "" {
		return
	}

	cmd := exec.Command("tmux", "kill-session", "-t", rc.tmuxSession)
	cmd.Run() // Ignore errors
	rc.tmuxSession = ""
}

// sendToTmux sends raw bytes to the tmux session by respawning the pane
// with a cat command that outputs the accumulated data.
func (rc *ReferenceComparator) sendToTmux(data []byte) error {
	if rc.tmuxSession == "" {
		return fmt.Errorf("no tmux session")
	}

	// Accumulate data (for incremental comparison, we need all data sent so far)
	rc.accumulatedData = append(rc.accumulatedData, data...)

	// Write accumulated data to a temp file
	tmpFile, err := os.CreateTemp("", "tmux-output-*.bin")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	// Don't defer remove - we need the file to exist for cat

	if _, err := tmpFile.Write(rc.accumulatedData); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write to temp file: %w", err)
	}
	tmpFile.Close()

	// Respawn the pane with a shell command that outputs our data, then sleeps
	// The cat outputs to stdout which tmux's terminal emulator processes
	shellCmd := fmt.Sprintf("cat %q; rm -f %q; sleep infinity", tmpPath, tmpPath)
	cmd := exec.Command("tmux", "respawn-pane", "-k",
		"-t", rc.tmuxSession,
		"sh", "-c", shellCmd,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("respawn-pane failed: %v, output: %s", err, output)
	}

	// Wait for cat to complete and tmux to process the output
	time.Sleep(50 * time.Millisecond)

	return nil
}

// captureTmuxPane captures the current content of the tmux pane.
func (rc *ReferenceComparator) captureTmuxPane() ([][]rune, error) {
	if rc.tmuxSession == "" {
		return nil, fmt.Errorf("no tmux session")
	}

	// tmux capture-pane -t <session> -p (print to stdout)
	cmd := exec.Command("tmux", "capture-pane",
		"-t", rc.tmuxSession,
		"-p",             // print to stdout
		"-e",             // include escape sequences (for color info)
		"-J",             // join wrapped lines
		"-S", "0",        // start from line 0
		"-E", strconv.Itoa(rc.height-1), // end at last line
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("capture-pane failed: %w", err)
	}

	// Parse output into grid
	lines := bytes.Split(output, []byte("\n"))
	grid := make([][]rune, rc.height)

	for y := 0; y < rc.height; y++ {
		grid[y] = make([]rune, rc.width)
		for x := range grid[y] {
			grid[y][x] = ' '
		}

		if y < len(lines) {
			// Convert line to runes, handling escape sequences
			lineRunes := stripANSI(string(lines[y]))
			for x, r := range lineRunes {
				if x < rc.width {
					grid[y][x] = r
				}
			}
		}
	}

	return grid, nil
}

// stripANSI removes ANSI escape sequences from a string, returning just the text.
func stripANSI(s string) []rune {
	var result []rune
	inEscape := false

	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			// End of escape sequence on letter or ~
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '~' {
				inEscape = false
			}
			continue
		}
		result = append(result, r)
	}

	return result
}

// vtermGridToRunes converts VTerm grid to rune grid for comparison.
func vtermGridToRunes(grid [][]parser.Cell) [][]rune {
	result := make([][]rune, len(grid))
	for y := range grid {
		result[y] = make([]rune, len(grid[y]))
		for x := range grid[y] {
			r := grid[y][x].Rune
			if r == 0 {
				r = ' '
			}
			result[y][x] = r
		}
	}
	return result
}

// CompareAtEnd feeds all sequences and compares final output.
func (rc *ReferenceComparator) CompareAtEnd() (*RefComparisonResult, error) {
	// Start tmux
	if err := rc.startTmuxSession(); err != nil {
		return nil, err
	}
	defer rc.stopTmuxSession()

	// Send all sequences to tmux
	if err := rc.sendToTmux(rc.recording.Sequences); err != nil {
		return nil, fmt.Errorf("send to tmux: %w", err)
	}

	// Capture tmux output
	tmuxGrid, err := rc.captureTmuxPane()
	if err != nil {
		return nil, fmt.Errorf("capture tmux: %w", err)
	}

	// Play through texelterm
	rc.replayer.PlayAll()
	rc.replayer.SimulateRender()

	// Get texelterm output
	texelGrid := vtermGridToRunes(rc.replayer.GetGrid())

	// Compare
	return compareRuneGrids(tmuxGrid, texelGrid, rc.width, rc.height), nil
}

// FindFirstDivergence feeds sequences incrementally and finds where outputs first differ.
// chunkSize controls how many bytes to process between comparisons (smaller = slower but more precise).
func (rc *ReferenceComparator) FindFirstDivergence(chunkSize int) (*DivergenceResult, error) {
	// Start tmux
	if err := rc.startTmuxSession(); err != nil {
		return nil, err
	}
	defer rc.stopTmuxSession()

	byteIndex := 0
	sequences := rc.recording.Sequences

	for byteIndex < len(sequences) {
		// Determine chunk to send, avoiding UTF-8 boundary issues
		endIndex := byteIndex + chunkSize
		if endIndex > len(sequences) {
			endIndex = len(sequences)
		}
		// Adjust to avoid cutting UTF-8 sequences
		endIndex = adjustForUTF8(sequences, endIndex)
		if endIndex <= byteIndex {
			// Can't make progress, force at least one byte
			endIndex = byteIndex + 1
		}

		chunk := sequences[byteIndex:endIndex]

		// Send to tmux (this accumulates data and respawns with ALL data so far)
		if err := rc.sendToTmux(chunk); err != nil {
			return nil, fmt.Errorf("send chunk to tmux at byte %d: %w", byteIndex, err)
		}

		// Create a fresh replayer with all accumulated data
		// (since tmux respawns with all data, texelterm must also replay from scratch)
		accRec := &Recording{
			Metadata:  rc.recording.Metadata,
			Sequences: rc.accumulatedData,
		}
		replayer := NewReplayer(accRec)
		replayer.PlayAll()
		replayer.SimulateRender()

		// Capture and compare
		tmuxGrid, err := rc.captureTmuxPane()
		if err != nil {
			return nil, fmt.Errorf("capture at byte %d: %w", endIndex, err)
		}

		texelGrid := vtermGridToRunes(replayer.GetGrid())

		comparison := compareRuneGrids(tmuxGrid, texelGrid, rc.width, rc.height)
		if !comparison.Match {
			// Found divergence!
			return &DivergenceResult{
				ByteIndex:      byteIndex,
				ByteEndIndex:   endIndex,
				ChunkProcessed: chunk,
				Comparison:     comparison,
				TmuxGrid:       tmuxGrid,
				TexelGrid:      texelGrid,
			}, nil
		}

		byteIndex = endIndex
	}

	// No divergence found
	return nil, nil
}

// adjustForUTF8 adjusts an index to avoid cutting in the middle of a UTF-8 sequence.
// It moves the index backward until it's at a valid UTF-8 boundary.
func adjustForUTF8(data []byte, index int) int {
	if index >= len(data) {
		return len(data)
	}
	// Move backward while we're in the middle of a UTF-8 sequence
	// UTF-8 continuation bytes have the form 10xxxxxx (0x80-0xBF)
	for index > 0 && data[index] >= 0x80 && data[index] <= 0xBF {
		index--
	}
	return index
}

// RefComparisonResult holds the result of comparing reference vs texelterm.
type RefComparisonResult struct {
	Match       bool
	Differences []RefDiff
	Summary     string
}

// RefDiff represents a single cell difference.
type RefDiff struct {
	X, Y       int
	Reference  rune
	Texelterm  rune
}

// DivergenceResult holds details about where outputs diverged.
type DivergenceResult struct {
	ByteIndex      int
	ByteEndIndex   int
	ChunkProcessed []byte
	Comparison     *RefComparisonResult
	TmuxGrid       [][]rune
	TexelGrid      [][]rune
}

// compareRuneGrids compares two rune grids.
func compareRuneGrids(ref, actual [][]rune, width, height int) *RefComparisonResult {
	result := &RefComparisonResult{Match: true}

	for y := 0; y < height && y < len(ref) && y < len(actual); y++ {
		for x := 0; x < width && x < len(ref[y]) && x < len(actual[y]); x++ {
			refRune := ref[y][x]
			actRune := actual[y][x]

			// Normalize spaces
			if refRune == 0 {
				refRune = ' '
			}
			if actRune == 0 {
				actRune = ' '
			}

			if refRune != actRune {
				result.Match = false
				result.Differences = append(result.Differences, RefDiff{
					X:         x,
					Y:         y,
					Reference: refRune,
					Texelterm: actRune,
				})
			}
		}
	}

	if len(result.Differences) > 0 {
		result.Summary = fmt.Sprintf("Found %d differences", len(result.Differences))
	} else {
		result.Summary = "Outputs match"
	}

	return result
}

// FormatDivergence formats a divergence result for debugging.
func FormatDivergence(d *DivergenceResult) string {
	var sb strings.Builder

	sb.WriteString("=== DIVERGENCE FOUND ===\n")
	sb.WriteString(fmt.Sprintf("Byte range: %d-%d\n", d.ByteIndex, d.ByteEndIndex))
	sb.WriteString(fmt.Sprintf("Chunk (escaped): %q\n", string(d.ChunkProcessed)))
	sb.WriteString(fmt.Sprintf("Chunk (readable): %s\n", EscapeSequenceLog(d.ChunkProcessed)))
	sb.WriteString(fmt.Sprintf("\nDifferences: %d\n", len(d.Comparison.Differences)))

	// Show first few differences
	maxShow := 10
	for i, diff := range d.Comparison.Differences {
		if i >= maxShow {
			sb.WriteString(fmt.Sprintf("... and %d more differences\n", len(d.Comparison.Differences)-maxShow))
			break
		}
		sb.WriteString(fmt.Sprintf("  (%d,%d): tmux=%q texelterm=%q\n",
			diff.X, diff.Y,
			string(diff.Reference),
			string(diff.Texelterm)))
	}

	// Side-by-side grid comparison
	sb.WriteString("\nTMUX (reference):\n")
	for y := 0; y < len(d.TmuxGrid) && y < 10; y++ {
		sb.WriteString(fmt.Sprintf("  %2d: [", y))
		for x := 0; x < len(d.TmuxGrid[y]) && x < 40; x++ {
			r := d.TmuxGrid[y][x]
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		sb.WriteString("]\n")
	}

	sb.WriteString("\nTEXELTERM:\n")
	for y := 0; y < len(d.TexelGrid) && y < 10; y++ {
		sb.WriteString(fmt.Sprintf("  %2d: [", y))
		for x := 0; x < len(d.TexelGrid[y]) && x < 40; x++ {
			r := d.TexelGrid[y][x]
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		sb.WriteString("]\n")
	}

	return sb.String()
}

// QuickCompare is a convenience function to compare a recording against reference.
// Returns nil if outputs match, or a formatted diff string if they differ.
func QuickCompare(rec *Recording) (string, error) {
	cmp, err := NewReferenceComparator(rec)
	if err != nil {
		return "", err
	}

	result, err := cmp.CompareAtEnd()
	if err != nil {
		return "", err
	}

	if result.Match {
		return "", nil // No diff, outputs match
	}

	return result.Summary + "\n" + formatRefDiffs(result.Differences), nil
}

// formatRefDiffs formats reference differences for display.
func formatRefDiffs(diffs []RefDiff) string {
	var sb strings.Builder

	maxShow := 20
	for i, diff := range diffs {
		if i >= maxShow {
			sb.WriteString(fmt.Sprintf("... and %d more\n", len(diffs)-maxShow))
			break
		}
		sb.WriteString(fmt.Sprintf("  (%d,%d): ref=%q actual=%q\n",
			diff.X, diff.Y,
			string(diff.Reference),
			string(diff.Texelterm)))
	}

	return sb.String()
}

// ============================================================================
// Enhanced Comparison with Full Color/Attribute Support
// ============================================================================

// captureTmuxPaneWithColors captures tmux output and parses to Cell grid.
// Uses ANSIParser to preserve color and attribute information.
func (rc *ReferenceComparator) captureTmuxPaneWithColors() ([][]parser.Cell, error) {
	if rc.tmuxSession == "" {
		return nil, fmt.Errorf("no tmux session")
	}

	// Capture with escape sequences
	cmd := exec.Command("tmux", "capture-pane",
		"-t", rc.tmuxSession,
		"-p",                             // print to stdout
		"-e",                             // include escape sequences
		"-S", "0",                        // start from line 0
		"-E", strconv.Itoa(rc.height-1), // end at last line
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("capture-pane failed: %w", err)
	}

	// Parse ANSI sequences to Cell grid
	ansiParser := NewANSIParser(rc.width, rc.height)
	return ansiParser.ParseTmuxOutput(output), nil
}

// CompareAtEndWithFullDiff performs complete comparison including colors/attrs.
// Returns an EnhancedComparisonResult with JSON-serializable data.
func (rc *ReferenceComparator) CompareAtEndWithFullDiff() (*EnhancedComparisonResult, error) {
	// Start tmux
	if err := rc.startTmuxSession(); err != nil {
		return nil, err
	}
	defer rc.stopTmuxSession()

	// Send all sequences to tmux
	if err := rc.sendToTmux(rc.recording.Sequences); err != nil {
		return nil, fmt.Errorf("send to tmux: %w", err)
	}

	// Capture tmux output with colors
	tmuxGrid, err := rc.captureTmuxPaneWithColors()
	if err != nil {
		return nil, fmt.Errorf("capture tmux: %w", err)
	}

	// Play through texelterm
	rc.replayer.PlayAll()
	rc.replayer.SimulateRender()

	// Get texelterm output
	texelGrid := rc.replayer.GetGrid()

	// Compare with full cell info
	result := EnhancedCompareGrids(tmuxGrid, texelGrid, rc.width, rc.height)
	result.BytesProcessed = len(rc.recording.Sequences)

	return result, nil
}

// FindFirstDivergenceWithFullDiff finds first divergence with complete cell info.
// Returns an EnhancedDivergence with full color/attribute details.
func (rc *ReferenceComparator) FindFirstDivergenceWithFullDiff(chunkSize int) (*EnhancedDivergence, error) {
	// Start tmux
	if err := rc.startTmuxSession(); err != nil {
		return nil, err
	}
	defer rc.stopTmuxSession()

	byteIndex := 0
	sequences := rc.recording.Sequences

	for byteIndex < len(sequences) {
		// Determine chunk to send
		endIndex := byteIndex + chunkSize
		if endIndex > len(sequences) {
			endIndex = len(sequences)
		}
		endIndex = adjustForUTF8(sequences, endIndex)
		if endIndex <= byteIndex {
			endIndex = byteIndex + 1
		}

		chunk := sequences[byteIndex:endIndex]

		// Send to tmux
		if err := rc.sendToTmux(chunk); err != nil {
			return nil, fmt.Errorf("send chunk to tmux at byte %d: %w", byteIndex, err)
		}

		// Create a fresh replayer with all accumulated data
		accRec := &Recording{
			Metadata:  rc.recording.Metadata,
			Sequences: rc.accumulatedData,
		}
		replayer := NewReplayer(accRec)
		replayer.PlayAll()
		replayer.SimulateRender()

		// Capture tmux with colors
		tmuxGrid, err := rc.captureTmuxPaneWithColors()
		if err != nil {
			return nil, fmt.Errorf("capture at byte %d: %w", endIndex, err)
		}

		// Get texelterm output
		texelGrid := replayer.GetGrid()

		// Compare with full cell info
		comparison := EnhancedCompareGrids(tmuxGrid, texelGrid, rc.width, rc.height)
		comparison.BytesProcessed = len(rc.accumulatedData)

		if !comparison.Match {
			// Found divergence!
			return CreateEnhancedDivergence(byteIndex, endIndex, chunk, comparison), nil
		}

		byteIndex = endIndex
	}

	// No divergence found
	return nil, nil
}

// CompareWithFullDiffToJSON performs comparison and returns JSON output.
// This is the main entry point for automated testing.
func (rc *ReferenceComparator) CompareWithFullDiffToJSON() ([]byte, error) {
	result, err := rc.CompareAtEndWithFullDiff()
	if err != nil {
		return nil, err
	}
	return result.ToJSONPretty()
}

// FindDivergenceToJSON finds divergence and returns JSON output.
func (rc *ReferenceComparator) FindDivergenceToJSON(chunkSize int) ([]byte, error) {
	divergence, err := rc.FindFirstDivergenceWithFullDiff(chunkSize)
	if err != nil {
		return nil, err
	}
	if divergence == nil {
		// No divergence - return a success result
		result := &EnhancedComparisonResult{
			Match:          true,
			Summary:        "No divergence found - outputs match throughout",
			BytesProcessed: len(rc.recording.Sequences),
		}
		return result.ToJSONPretty()
	}
	return DivergenceToJSON(divergence)
}
