// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/legacy_stubs.go
// Summary: Minimal storage types retained for AdaptivePersistence tests.
//
// These types were previously defined in memory_buffer.go (deleted during
// the sparse-viewport cutover). They are kept here so that:
//   - AdaptivePersistence tests (adaptive_persistence_test,
//     adaptive_persistence_recovery_test, fixed_width_detector_test) can
//     supply a MemoryBuffer as the LineStore argument and exercise
//     eviction semantics that sparse does not model;
//   - the LineStore interface still carries EvictedLine.
//
// MemoryBuffer is a simple in-memory ring-buffer for logical lines. It
// does NOT back VTerm's main screen (that is sparse.Terminal in
// production). It exists purely as a test fixture.

package parser

import "sync"

// DefaultWidth and DefaultHeight are the fallback terminal dimensions used when
// no explicit size has been set.  They were previously defined in memory_buffer.go.
const (
	DefaultWidth  = 80
	DefaultHeight = 24
)

// EvictedLine carries a line that has been removed from a MemoryBuffer ring
// before it could be flushed to disk.  Used by AdaptivePersistence.
type EvictedLine struct {
	GlobalIdx int64
	Line      *LogicalLine
}

// DefaultMemoryLines is the default in-memory line count for scrollback history.
const DefaultMemoryLines = 5000

// MemoryBufferOptions configures optional disk persistence for the main screen.
// Kept for API compatibility; the actual persistence path now uses WAL directly.
type MemoryBufferOptions struct {
	TerminalID    string
	DiskPath      string
	MaxLines      int // retained for API compatibility; ignored by the sparse path
	EvictionBatch int // retained for API compatibility; ignored by the sparse path
}

// MemoryBufferConfig configures a MemoryBuffer ring.
type MemoryBufferConfig struct {
	MaxLines      int
	EvictionBatch int
}

// DefaultMemoryBufferConfig returns sensible defaults.
func DefaultMemoryBufferConfig() MemoryBufferConfig {
	return MemoryBufferConfig{MaxLines: 5000, EvictionBatch: 100}
}

// MemoryBuffer is a simple append-only store of LogicalLines backed by a
// fixed-size ring.  Lines are identified by a monotonically increasing global
// index; once the ring is full the oldest batch is evicted.
//
// All public methods are safe for concurrent use.
type MemoryBuffer struct {
	mu       sync.Mutex
	config   MemoryBufferConfig
	lines    []*LogicalLine // ring indexed by (globalIdx - offset) % cap
	offset   int64          // global index of lines[0] slot
	end      int64          // global index one past the last written line
	version  int64          // bumped on every mutation
	termW    int
	cursorR  int64
	cursorC  int

	evictCb func([]EvictedLine)
}

// NewMemoryBuffer allocates a new empty MemoryBuffer.
func NewMemoryBuffer(cfg MemoryBufferConfig) *MemoryBuffer {
	if cfg.MaxLines <= 0 {
		cfg.MaxLines = 5000
	}
	if cfg.EvictionBatch <= 0 {
		cfg.EvictionBatch = 100
	}
	mb := &MemoryBuffer{
		config: cfg,
		lines:  make([]*LogicalLine, cfg.MaxLines),
		termW:  DefaultWidth,
	}
	return mb
}

// SetTermWidth records the current terminal width (used only for line storage).
func (m *MemoryBuffer) SetTermWidth(w int) {
	m.mu.Lock()
	m.termW = w
	m.mu.Unlock()
}

// SetCursor moves the logical write cursor to the given row and column.
// Rows past the current end are filled with blank lines.
func (m *MemoryBuffer) SetCursor(row int64, col int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cursorR = row
	m.cursorC = col
	m.ensureLineUnlocked(row)
}

// Write appends a rune at the current cursor position and advances the column.
func (m *MemoryBuffer) Write(ch rune, fg, bg Color, flags int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLineUnlocked(m.cursorR)
	line := m.getUnlocked(m.cursorR)
	if line == nil {
		return
	}
	// Extend cells to cursor column if needed.
	for len(line.Cells) <= m.cursorC {
		line.Cells = append(line.Cells, Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG})
	}
	line.Cells[m.cursorC] = Cell{Rune: ch, FG: fg, BG: bg, Attr: Attribute(flags)}
	m.cursorC++
	m.version++
}

// NewLine advances the cursor to the next logical row, creating it if needed.
func (m *MemoryBuffer) NewLine() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cursorR++
	m.cursorC = 0
	m.ensureLineUnlocked(m.cursorR)
	m.version++
}

// CarriageReturn moves the cursor column back to zero.
func (m *MemoryBuffer) CarriageReturn() {
	m.mu.Lock()
	m.cursorC = 0
	m.mu.Unlock()
}

// EnsureLine ensures the buffer contains a line at the given global index
// and returns it.
func (m *MemoryBuffer) EnsureLine(idx int64) *LogicalLine {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLineUnlocked(idx)
	return m.getUnlocked(idx)
}

// SetLineFixed marks the line at globalIdx as fixed-width.
func (m *MemoryBuffer) SetLineFixed(lineIdx int64, width int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	line := m.getUnlocked(lineIdx)
	if line != nil {
		line.FixedWidth = width
	}
}

// ClearDirty is a no-op (dirty tracking not implemented in stub).
func (m *MemoryBuffer) ClearDirty(globalIdx int64) {}

// SetPreEvictCallback registers a callback invoked just before lines are
// evicted from the ring.
func (m *MemoryBuffer) SetPreEvictCallback(cb func([]EvictedLine)) {
	m.mu.Lock()
	m.evictCb = cb
	m.mu.Unlock()
}

// GetLine returns the logical line at the given global index, or nil.
func (m *MemoryBuffer) GetLine(globalIdx int64) *LogicalLine {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getUnlocked(globalIdx)
}

// GetLineRange returns lines [start, end).
func (m *MemoryBuffer) GetLineRange(start, end int64) []*LogicalLine {
	m.mu.Lock()
	defer m.mu.Unlock()
	if end > m.end {
		end = m.end
	}
	result := make([]*LogicalLine, 0, end-start)
	for i := start; i < end; i++ {
		result = append(result, m.getUnlocked(i))
	}
	return result
}

// GlobalOffset returns the smallest readable global index.
func (m *MemoryBuffer) GlobalOffset() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offset
}

// GlobalEnd returns the global index one past the last line.
func (m *MemoryBuffer) GlobalEnd() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.end
}

// TotalLines returns the number of lines currently in the buffer.
func (m *MemoryBuffer) TotalLines() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.end - m.offset
}

// ContentVersion returns a version counter that increments on every write.
func (m *MemoryBuffer) ContentVersion() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.version
}

// CursorLine returns the current cursor row.
func (m *MemoryBuffer) CursorLine() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cursorR
}

// TermWidth returns the configured terminal width.
func (m *MemoryBuffer) TermWidth() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.termW
}

// --- internal helpers ---

func (m *MemoryBuffer) ensureLineUnlocked(idx int64) {
	cap := int64(m.config.MaxLines)
	for m.end <= idx {
		// Evict if the ring is full before placing the new line.
		if m.end-m.offset >= cap {
			m.evictBatch()
		}
		slot := m.end % cap
		if m.lines[slot] == nil {
			m.lines[slot] = &LogicalLine{}
		}
		m.end++
	}
}

func (m *MemoryBuffer) evictBatch() {
	batch := int64(m.config.EvictionBatch)
	cap := int64(m.config.MaxLines)
	evicted := make([]EvictedLine, 0, batch)
	for i := int64(0); i < batch && m.end-m.offset >= cap; i++ {
		slot := m.offset % cap
		line := m.lines[slot]
		evicted = append(evicted, EvictedLine{GlobalIdx: m.offset, Line: line})
		m.lines[slot] = nil
		m.offset++
	}
	if m.evictCb != nil && len(evicted) > 0 {
		m.evictCb(evicted)
	}
}

func (m *MemoryBuffer) getUnlocked(globalIdx int64) *LogicalLine {
	if globalIdx < m.offset || globalIdx >= m.end {
		return nil
	}
	cap := int64(m.config.MaxLines)
	slot := globalIdx % cap
	return m.lines[slot]
}
