// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/testutil/replayer.go
// Summary: VTerm replay engine with dirty tracking simulation.
//
// This replayer is critical for detecting visual bugs. Per CLAUDE.md:
// "Visual glitches in texelterm often pass unit tests because Grid() returns
// correct data, but the actual rendered output is wrong due to dirty line
// tracking issues."
//
// The replayer simulates the actual render path by:
// 1. Feeding escape sequences through the parser
// 2. Only updating the render buffer for dirty lines
// 3. Comparing render buffer vs Grid() to detect mismatches

package testutil

import (
	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// Replayer feeds recordings into VTerm and simulates render flow.
type Replayer struct {
	vterm     *parser.VTerm
	parser    *parser.Parser
	recording *Recording

	// renderBuf simulates what the user actually sees.
	// Only updated for dirty lines, like the real renderer.
	renderBuf [][]parser.Cell

	// responseQueue holds terminal responses (DSR, DA, etc.) to be fed back
	responseQueue []byte

	// Tracking
	width, height int
	byteIndex     int // Current position in recording sequences
	renderCount   int // Number of SimulateRender calls
}

// Snapshot captures the state at a point in time.
type Snapshot struct {
	// Grid is the logical state (what VTerm thinks should be displayed)
	Grid [][]parser.Cell

	// RenderBuf is the simulated visual state (what user would see)
	// May differ from Grid if dirty tracking is broken
	RenderBuf [][]parser.Cell

	// Cursor position
	CursorX, CursorY int

	// Dirty state at time of snapshot
	DirtyLines map[int]bool
	AllDirty   bool

	// Position in replay
	ByteIndex   int
	RenderCount int
}

// NewReplayer creates a new replayer for a recording.
func NewReplayer(recording *Recording) *Replayer {
	width := recording.Metadata.Width
	height := recording.Metadata.Height

	vterm := parser.NewVTerm(width, height)
	vterm.EnableDisplayBuffer()

	p := parser.NewParser(vterm)

	// Initialize render buffer
	renderBuf := make([][]parser.Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]parser.Cell, width)
	}

	r := &Replayer{
		vterm:     vterm,
		parser:    p,
		recording: recording,
		renderBuf: renderBuf,
		width:     width,
		height:    height,
	}

	// Wire up terminal response callback (DSR, DA, etc.)
	// Responses are queued and fed back through the parser
	vterm.WriteToPty = func(data []byte) {
		r.responseQueue = append(r.responseQueue, data...)
	}

	return r
}

// NewReplayerWithSize creates a replayer with explicit dimensions.
// Useful for testing with different terminal sizes than recorded.
func NewReplayerWithSize(recording *Recording, width, height int) *Replayer {
	vterm := parser.NewVTerm(width, height)
	vterm.EnableDisplayBuffer()

	p := parser.NewParser(vterm)

	// Initialize render buffer
	renderBuf := make([][]parser.Cell, height)
	for y := range renderBuf {
		renderBuf[y] = make([]parser.Cell, width)
	}

	r := &Replayer{
		vterm:     vterm,
		parser:    p,
		recording: recording,
		renderBuf: renderBuf,
		width:     width,
		height:    height,
	}

	// Wire up terminal response callback
	vterm.WriteToPty = func(data []byte) {
		r.responseQueue = append(r.responseQueue, data...)
	}

	return r
}

// VTerm returns the underlying VTerm for direct access.
func (r *Replayer) VTerm() *parser.VTerm {
	return r.vterm
}

// PlayAll feeds all sequences through the parser.
// Does NOT simulate render - call SimulateRender() after to update renderBuf.
// Note: This properly handles UTF-8 by converting bytes to string first.
func (r *Replayer) PlayAll() {
	// Convert to string for proper UTF-8 rune iteration
	for _, ch := range string(r.recording.Sequences) {
		r.parser.Parse(ch)
	}
	r.byteIndex = len(r.recording.Sequences)
}

// PlayBytes feeds a specific number of bytes through the parser.
// Returns the number of bytes actually played (may be less if at end).
func (r *Replayer) PlayBytes(n int) int {
	played := 0
	for played < n && r.byteIndex < len(r.recording.Sequences) {
		r.parser.Parse(rune(r.recording.Sequences[r.byteIndex]))
		r.byteIndex++
		played++
	}
	return played
}

// PlayOne feeds a single byte through the parser.
// Returns false if at end of recording.
func (r *Replayer) PlayOne() bool {
	if r.byteIndex >= len(r.recording.Sequences) {
		return false
	}
	r.parser.Parse(rune(r.recording.Sequences[r.byteIndex]))
	r.byteIndex++
	return true
}

// PlayString feeds a string through the parser (for synthetic input).
func (r *Replayer) PlayString(s string) {
	for _, ch := range s {
		r.parser.Parse(ch)
	}
}

// GetResponses returns all terminal responses generated during replay.
// These are responses the terminal would send to the application (DSR, DA, etc.)
// Useful for verifying the terminal responds correctly to queries.
func (r *Replayer) GetResponses() []byte {
	return r.responseQueue
}

// ClearResponses clears the response queue.
func (r *Replayer) ClearResponses() {
	r.responseQueue = nil
}

// SimulateRender updates renderBuf based on dirty tracking.
// This is the critical function that simulates actual rendering behavior.
//
// From CLAUDE.md:
// "Tests must simulate the actual render path with dirty tracking"
func (r *Replayer) SimulateRender() {
	dirtyLines, allDirty := r.vterm.GetDirtyLines()
	vtermGrid := r.vterm.Grid()

	if allDirty {
		// Full screen redraw
		for y := 0; y < r.height && y < len(vtermGrid); y++ {
			copy(r.renderBuf[y], vtermGrid[y])
		}
	} else {
		// Only update dirty lines
		for y := range dirtyLines {
			if y >= 0 && y < r.height && y < len(vtermGrid) {
				copy(r.renderBuf[y], vtermGrid[y])
			}
		}
	}

	r.vterm.ClearDirty()
	r.renderCount++

	// Normalize fully blank rows (spaces only) to default colors to match
	// reference terminals that don't retain transient SGR on padding.
	for y := 0; y < len(vtermGrid); y++ {
		if isBlankRow(vtermGrid[y]) {
			normalizeRowToDefault(vtermGrid[y])
			normalizeRowToDefault(r.renderBuf[y])
		}
	}
}

// PlayAndRender feeds all sequences and renders after each byte.
// This is the most thorough mode - catches all dirty tracking issues.
func (r *Replayer) PlayAndRender() {
	for r.PlayOne() {
		r.SimulateRender()
	}
}

// PlayAndRenderChunks feeds sequences in chunks, rendering after each chunk.
// chunkSize controls the granularity (smaller = more render cycles = slower but thorough).
func (r *Replayer) PlayAndRenderChunks(chunkSize int) {
	for r.byteIndex < len(r.recording.Sequences) {
		r.PlayBytes(chunkSize)
		r.SimulateRender()
	}
}

// GetSnapshot captures the current state.
func (r *Replayer) GetSnapshot() *Snapshot {
	grid := r.vterm.Grid()
	dirtyLines, allDirty := r.vterm.GetDirtyLines()

	// Deep copy grid
	gridCopy := make([][]parser.Cell, len(grid))
	for y := range grid {
		gridCopy[y] = make([]parser.Cell, len(grid[y]))
		copy(gridCopy[y], grid[y])
	}

	// Deep copy render buffer
	renderCopy := make([][]parser.Cell, len(r.renderBuf))
	for y := range r.renderBuf {
		renderCopy[y] = make([]parser.Cell, len(r.renderBuf[y]))
		copy(renderCopy[y], r.renderBuf[y])
	}

	// Copy dirty lines map
	dirtyCopy := make(map[int]bool)
	for k, v := range dirtyLines {
		dirtyCopy[k] = v
	}

	cursorX, cursorY := r.vterm.Cursor()

	return &Snapshot{
		Grid:        gridCopy,
		RenderBuf:   renderCopy,
		CursorX:     cursorX,
		CursorY:     cursorY,
		DirtyLines:  dirtyCopy,
		AllDirty:    allDirty,
		ByteIndex:   r.byteIndex,
		RenderCount: r.renderCount,
	}
}

// GetGrid returns the current logical grid (VTerm's view).
func (r *Replayer) GetGrid() [][]parser.Cell {
	return r.vterm.Grid()
}

// GetRenderBuf returns the current render buffer (simulated visual state).
func (r *Replayer) GetRenderBuf() [][]parser.Cell {
	return r.renderBuf
}

// GetCursor returns the current cursor position.
func (r *Replayer) GetCursor() (x, y int) {
	return r.vterm.Cursor()
}

// GetDirtyLines returns the current dirty tracking state.
func (r *Replayer) GetDirtyLines() (map[int]bool, bool) {
	return r.vterm.GetDirtyLines()
}

// AtEnd returns true if all bytes have been played.
func (r *Replayer) AtEnd() bool {
	return r.byteIndex >= len(r.recording.Sequences)
}

// Reset restarts the replay from the beginning.
func (r *Replayer) Reset() {
	r.vterm = parser.NewVTerm(r.width, r.height)
	r.vterm.EnableDisplayBuffer()
	r.parser = parser.NewParser(r.vterm)

	// Re-wire response callback
	r.vterm.WriteToPty = func(data []byte) {
		r.responseQueue = append(r.responseQueue, data...)
	}

	// Clear render buffer
	for y := range r.renderBuf {
		for x := range r.renderBuf[y] {
			r.renderBuf[y][x] = parser.Cell{}
		}
	}

	r.byteIndex = 0
	r.renderCount = 0
	r.responseQueue = nil
}

// ByteIndex returns the current position in the recording.
func (r *Replayer) ByteIndex() int {
	return r.byteIndex
}

// TotalBytes returns the total number of bytes in the recording.
func (r *Replayer) TotalBytes() int {
	return len(r.recording.Sequences)
}

// HasVisualMismatch checks if renderBuf differs from Grid().
// Returns true if there's a mismatch (indicating dirty tracking bug).
func (r *Replayer) HasVisualMismatch() bool {
	grid := r.vterm.Grid()
	for y := 0; y < r.height && y < len(grid) && y < len(r.renderBuf); y++ {
		for x := 0; x < r.width && x < len(grid[y]) && x < len(r.renderBuf[y]); x++ {
			if r.renderBuf[y][x].Rune != grid[y][x].Rune {
				return true
			}
		}
	}
	return false
}

// FindVisualMismatches returns all cells where renderBuf differs from Grid().
func (r *Replayer) FindVisualMismatches() []CellMismatch {
	var mismatches []CellMismatch
	grid := r.vterm.Grid()

	for y := 0; y < r.height && y < len(grid) && y < len(r.renderBuf); y++ {
		for x := 0; x < r.width && x < len(grid[y]) && x < len(r.renderBuf[y]); x++ {
			render := r.renderBuf[y][x]
			logical := grid[y][x]

			if render.Rune != logical.Rune ||
				render.FG != logical.FG ||
				render.BG != logical.BG ||
				render.Attr != logical.Attr {
				mismatches = append(mismatches, CellMismatch{
					X:        x,
					Y:        y,
					Rendered: render,
					Logical:  logical,
				})
			}
		}
	}

	return mismatches
}

// CellMismatch represents a difference between rendered and logical state.
type CellMismatch struct {
	X, Y     int
	Rendered parser.Cell
	Logical  parser.Cell
}
