// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/memory_buffer.go
// Summary: MemoryBuffer provides central storage for terminal content.
//
// Architecture:
//
//	MemoryBuffer is the single source of truth for terminal content.
//	It stores logical lines with global indexing that persists across
//	evictions, enabling consistent addressing for persistence and viewport.
//
//	VTerm writes here; ViewportWindow reads from here (Phase 3).
//	AdaptivePersistence monitors dirty lines for disk writes (Phase 2).

package parser

import (
	"slices"
	"sync"
)

// DefaultWidth is the default terminal width used when width is not specified.
const DefaultWidth = 80

// DefaultHeight is the default terminal height used when height is not specified.
const DefaultHeight = 24

// DefaultMemoryLines is the default number of lines to keep in memory scrollback.
const DefaultMemoryLines = 50000

// MemoryBufferConfig holds configuration for the memory buffer.
type MemoryBufferConfig struct {
	// MaxLines is the maximum number of logical lines to store.
	// When exceeded, oldest lines are evicted in batches.
	// Default: 50000
	MaxLines int

	// EvictionBatch is how many lines to evict at once when over capacity.
	// Batch eviction is more efficient than one-at-a-time.
	// Default: 1000
	EvictionBatch int
}

// DefaultMemoryBufferConfig returns sensible defaults.
func DefaultMemoryBufferConfig() MemoryBufferConfig {
	return MemoryBufferConfig{
		MaxLines:      50000,
		EvictionBatch: 1000,
	}
}

// DirtyTracker manages per-line dirty state for persistence.
// Extracted as a separate component for clean separation and testability.
// Thread-safety is managed by the containing MemoryBuffer.
type DirtyTracker struct {
	dirty map[int64]bool
}

// NewDirtyTracker creates a new dirty tracker.
func NewDirtyTracker() *DirtyTracker {
	return &DirtyTracker{
		dirty: make(map[int64]bool),
	}
}

// MarkDirty marks a line as dirty.
func (dt *DirtyTracker) MarkDirty(globalIdx int64) {
	dt.dirty[globalIdx] = true
}

// ClearDirty removes the dirty flag for a line.
func (dt *DirtyTracker) ClearDirty(globalIdx int64) {
	delete(dt.dirty, globalIdx)
}

// ClearAll removes all dirty flags.
func (dt *DirtyTracker) ClearAll() {
	dt.dirty = make(map[int64]bool)
}

// IsDirty returns whether a line is marked as dirty.
func (dt *DirtyTracker) IsDirty(globalIdx int64) bool {
	return dt.dirty[globalIdx]
}

// GetDirty returns a sorted slice of all dirty line indices.
// Sorted order is deterministic and helps with testing.
func (dt *DirtyTracker) GetDirty() []int64 {
	result := make([]int64, 0, len(dt.dirty))
	for idx := range dt.dirty {
		result = append(result, idx)
	}
	slices.Sort(result)
	return result
}

// DirtyCount returns the number of dirty lines.
func (dt *DirtyTracker) DirtyCount() int {
	return len(dt.dirty)
}

// RemoveBelow removes all dirty entries with indices below threshold.
// Used after eviction to clean up stale dirty entries.
func (dt *DirtyTracker) RemoveBelow(threshold int64) {
	for idx := range dt.dirty {
		if idx < threshold {
			delete(dt.dirty, idx)
		}
	}
}

// Cursor represents the current write position in the buffer.
// Encapsulates cursor state for cleaner method signatures.
type Cursor struct {
	GlobalLineIdx int64 // Global line index
	Col           int   // Column within line
}

// MemoryBuffer stores all terminal content as logical lines.
// This is the single source of truth for terminal content.
// Thread-safe for concurrent access.
//
// Uses a ring buffer for O(1) append and eviction operations.
// Global indexing persists across evictions via globalOffset tracking.
type MemoryBuffer struct {
	config MemoryBufferConfig

	// lines is a ring buffer of logical lines.
	// ringHead points to the oldest line (at globalOffset).
	// ringSize is the current number of stored lines.
	lines    []*LogicalLine
	ringHead int
	ringSize int

	// globalOffset is the global index of the line at ringHead.
	// When lines are evicted, globalOffset increases to maintain addressing.
	globalOffset int64

	// cursor tracks the current write position (separate from viewport scroll).
	cursor Cursor

	// termWidth is the current terminal width for new line creation.
	termWidth int

	// contentVersion increments on any write, used for cache invalidation.
	// ViewportWindow can compare versions to know when to rebuild its cache.
	contentVersion int64

	// dirtyTracker manages per-line dirty state for persistence.
	dirtyTracker *DirtyTracker

	mu sync.RWMutex
}

// NewMemoryBuffer creates a new memory buffer with the given configuration.
func NewMemoryBuffer(config MemoryBufferConfig) *MemoryBuffer {
	if config.MaxLines <= 0 {
		config.MaxLines = 50000
	}
	if config.EvictionBatch <= 0 {
		config.EvictionBatch = 1000
	}

	return &MemoryBuffer{
		config:         config,
		lines:          make([]*LogicalLine, config.MaxLines),
		ringHead:       0,
		ringSize:       0,
		globalOffset:   0,
		cursor:         Cursor{GlobalLineIdx: 0, Col: 0},
		termWidth:      DefaultWidth,
		contentVersion: 0,
		dirtyTracker:   NewDirtyTracker(),
	}
}

// --- Writing Operations ---

// Write writes a single-width character at the current cursor position.
// Advances cursor by one column after writing.
func (mb *MemoryBuffer) Write(r rune, fg, bg Color, attr Attribute) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	line := mb.ensureLineLocked(mb.cursor.GlobalLineIdx)
	if line == nil {
		return
	}

	cell := Cell{Rune: r, FG: fg, BG: bg, Attr: attr, Wide: false}
	line.SetCell(mb.cursor.Col, cell)

	mb.dirtyTracker.MarkDirty(mb.cursor.GlobalLineIdx)
	mb.contentVersion++
	mb.cursor.Col++
}

// WriteWide writes a wide (2-column) character at the current cursor position.
// Advances cursor by two columns after writing.
// Returns false if there's not enough room (cursor at or past termWidth-1).
func (mb *MemoryBuffer) WriteWide(r rune, fg, bg Color, attr Attribute, isWide bool) bool {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Validate: wide chars need at least 2 columns of space
	if isWide && mb.cursor.Col >= mb.termWidth-1 {
		return false
	}

	line := mb.ensureLineLocked(mb.cursor.GlobalLineIdx)
	if line == nil {
		return false
	}

	cell := Cell{Rune: r, FG: fg, BG: bg, Attr: attr, Wide: isWide}
	line.SetCell(mb.cursor.Col, cell)

	mb.dirtyTracker.MarkDirty(mb.cursor.GlobalLineIdx)
	mb.contentVersion++

	if isWide {
		// Wide char: place placeholder in next cell and advance by 2
		placeholder := Cell{Rune: 0, FG: fg, BG: bg, Attr: attr, Wide: false}
		line.SetCell(mb.cursor.Col+1, placeholder)
		mb.cursor.Col += 2
	} else {
		mb.cursor.Col++
	}

	return true
}

// SetCell directly sets a cell at the specified position.
// Creates the line if it doesn't exist.
func (mb *MemoryBuffer) SetCell(lineIdx int64, col int, cell Cell) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	line := mb.ensureLineLocked(lineIdx)
	if line == nil {
		return
	}

	line.SetCell(col, cell)
	mb.dirtyTracker.MarkDirty(lineIdx)
	mb.contentVersion++
}

// NewLine advances the cursor to the next line (linefeed behavior).
// Does NOT reset column to 0 (that's CarriageReturn's job).
func (mb *MemoryBuffer) NewLine() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.cursor.GlobalLineIdx++
}

// CarriageReturn moves the cursor to column 0 of the current line.
func (mb *MemoryBuffer) CarriageReturn() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.cursor.Col = 0
}

// --- Cursor Operations ---

// SetCursor moves the cursor to the specified global line index and column.
func (mb *MemoryBuffer) SetCursor(lineIdx int64, col int) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.cursor.GlobalLineIdx = lineIdx
	mb.cursor.Col = col
}

// GetCursor returns the current cursor position.
func (mb *MemoryBuffer) GetCursor() (lineIdx int64, col int) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.cursor.GlobalLineIdx, mb.cursor.Col
}

// CursorLine returns the current cursor line (global index).
func (mb *MemoryBuffer) CursorLine() int64 {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.cursor.GlobalLineIdx
}

// CursorCol returns the current cursor column.
func (mb *MemoryBuffer) CursorCol() int {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.cursor.Col
}

// SetTermWidth updates the terminal width.
// Used when the terminal is resized.
func (mb *MemoryBuffer) SetTermWidth(width int) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if width <= 0 {
		width = DefaultWidth
	}
	mb.termWidth = width
}

// TermWidth returns the current terminal width.
func (mb *MemoryBuffer) TermWidth() int {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.termWidth
}

// --- Reading Operations ---

// GetLine returns the logical line at the given global index.
// Returns nil if the line has been evicted or doesn't exist yet.
func (mb *MemoryBuffer) GetLine(globalIdx int64) *LogicalLine {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.getLineLocked(globalIdx)
}

// getLineLocked returns a line without locking (caller must hold lock).
func (mb *MemoryBuffer) getLineLocked(globalIdx int64) *LogicalLine {
	if globalIdx < mb.globalOffset {
		return nil // Evicted
	}

	offset := globalIdx - mb.globalOffset
	if offset >= int64(mb.ringSize) {
		return nil // Doesn't exist yet
	}

	ringIdx := (mb.ringHead + int(offset)) % len(mb.lines)
	return mb.lines[ringIdx]
}

// GetLineRange returns lines from start (inclusive) to end (exclusive).
// Skips lines that don't exist (evicted or not yet created).
func (mb *MemoryBuffer) GetLineRange(start, end int64) []*LogicalLine {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	if start < mb.globalOffset {
		start = mb.globalOffset
	}
	maxEnd := mb.globalOffset + int64(mb.ringSize)
	if end > maxEnd {
		end = maxEnd
	}
	if start >= end {
		return nil
	}

	result := make([]*LogicalLine, 0, end-start)
	for i := start; i < end; i++ {
		if line := mb.getLineLocked(i); line != nil {
			result = append(result, line)
		}
	}
	return result
}

// TotalLines returns the total number of lines stored in memory.
func (mb *MemoryBuffer) TotalLines() int64 {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return int64(mb.ringSize)
}

// GlobalOffset returns the global index of the oldest line in memory.
func (mb *MemoryBuffer) GlobalOffset() int64 {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.globalOffset
}

// GlobalEnd returns the global index just past the last line.
func (mb *MemoryBuffer) GlobalEnd() int64 {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.globalOffset + int64(mb.ringSize)
}

// ContentVersion returns the current content version number.
// Increments on any write operation, used for cache invalidation.
func (mb *MemoryBuffer) ContentVersion() int64 {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.contentVersion
}

// --- Fixed-Width Flags ---

// SetLineFixed marks a line as fixed-width (no reflow on resize).
// Creates the line if it doesn't exist.
// Validates that width > 0 and width <= reasonable maximum (10000).
func (mb *MemoryBuffer) SetLineFixed(globalIdx int64, width int) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Validate width bounds
	if width <= 0 || width > 10000 {
		return
	}

	// Ensure line exists (creates if needed)
	line := mb.ensureLineLocked(globalIdx)
	if line != nil {
		line.FixedWidth = width
		mb.dirtyTracker.MarkDirty(globalIdx)
		mb.contentVersion++
	}
}

// IsLineFixed returns whether a line is marked as fixed-width.
func (mb *MemoryBuffer) IsLineFixed(globalIdx int64) bool {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	line := mb.getLineLocked(globalIdx)
	return line != nil && line.FixedWidth > 0
}

// --- Dirty Tracking ---

// GetDirtyLines returns a sorted slice of global indices for dirty lines.
func (mb *MemoryBuffer) GetDirtyLines() []int64 {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.dirtyTracker.GetDirty()
}

// ClearDirty removes the dirty flag for a specific line.
func (mb *MemoryBuffer) ClearDirty(globalIdx int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.dirtyTracker.ClearDirty(globalIdx)
}

// ClearAllDirty removes all dirty flags.
func (mb *MemoryBuffer) ClearAllDirty() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.dirtyTracker.ClearAll()
}

// MarkDirty explicitly marks a line as dirty.
func (mb *MemoryBuffer) MarkDirty(globalIdx int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.dirtyTracker.MarkDirty(globalIdx)
}

// IsDirty returns whether a line is marked as dirty.
func (mb *MemoryBuffer) IsDirty(globalIdx int64) bool {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return mb.dirtyTracker.IsDirty(globalIdx)
}

// --- Line Operations ---

// EnsureLine ensures a line exists at the given global index.
// Creates empty lines as needed to fill gaps.
// Returns the line or nil if the index is before globalOffset.
func (mb *MemoryBuffer) EnsureLine(globalIdx int64) *LogicalLine {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.ensureLineLocked(globalIdx)
}

// ensureLineLocked creates a line if needed (caller must hold lock).
// Returns nil if the line cannot be created (e.g., eviction would move past it).
func (mb *MemoryBuffer) ensureLineLocked(globalIdx int64) *LogicalLine {
	if globalIdx < mb.globalOffset {
		return nil // Can't create lines before globalOffset
	}

	// Check if line already exists
	if line := mb.getLineLocked(globalIdx); line != nil {
		return line
	}

	// Calculate how many lines we need to create
	linesToCreate := int(globalIdx-mb.globalOffset) - mb.ringSize + 1
	if linesToCreate <= 0 {
		// Line should already exist but getLineLocked returned nil - shouldn't happen
		return mb.getLineLocked(globalIdx)
	}

	// Pre-check: will creating these lines trigger eviction past globalIdx?
	// This prevents unpredictable nil returns when the buffer is near capacity.
	newRingSize := mb.ringSize + linesToCreate
	if newRingSize > len(mb.lines) {
		// Would need to evict. Calculate new globalOffset after eviction.
		excess := newRingSize - len(mb.lines)
		evictCount := excess
		if evictCount < mb.config.EvictionBatch {
			evictCount = mb.config.EvictionBatch
		}
		newGlobalOffset := mb.globalOffset + int64(evictCount)

		if globalIdx < newGlobalOffset {
			// Eviction would move past requested index - can't create this line
			return nil
		}
	}

	// Now safe to create lines - eviction won't move past globalIdx
	for {
		targetOffset := globalIdx - mb.globalOffset
		if int64(mb.ringSize) > targetOffset {
			break
		}
		mb.appendNewLineLocked()
	}

	return mb.getLineLocked(globalIdx)
}

// appendNewLineLocked adds a new empty line to the ring buffer.
func (mb *MemoryBuffer) appendNewLineLocked() {
	newLine := NewLogicalLine()

	if mb.ringSize < len(mb.lines) {
		// Ring not full: add to tail
		ringIdx := (mb.ringHead + mb.ringSize) % len(mb.lines)
		mb.lines[ringIdx] = newLine
		mb.ringSize++
	} else {
		// Ring full: evict batch first
		mb.evictLocked(mb.config.EvictionBatch)
		// Now add to tail
		ringIdx := (mb.ringHead + mb.ringSize) % len(mb.lines)
		mb.lines[ringIdx] = newLine
		mb.ringSize++
	}
}

// InsertLine inserts a blank line at the given global index.
// Lines at and after this index are shifted down.
func (mb *MemoryBuffer) InsertLine(globalIdx int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if globalIdx < mb.globalOffset {
		return
	}

	offset := globalIdx - mb.globalOffset
	if offset > int64(mb.ringSize) {
		// Gap: just ensure line exists
		mb.ensureLineLocked(globalIdx)
		return
	}

	// Make room by evicting if needed
	if mb.ringSize >= len(mb.lines) {
		mb.evictLocked(mb.config.EvictionBatch)
	}

	// Shift lines down (from end to insertion point)
	insertIdx := int(offset)
	mb.ringSize++
	for i := mb.ringSize - 1; i > insertIdx; i-- {
		srcRingIdx := (mb.ringHead + i - 1) % len(mb.lines)
		dstRingIdx := (mb.ringHead + i) % len(mb.lines)
		mb.lines[dstRingIdx] = mb.lines[srcRingIdx]
	}

	// Insert new empty line
	ringIdx := (mb.ringHead + insertIdx) % len(mb.lines)
	mb.lines[ringIdx] = NewLogicalLine()

	// Mark all affected lines dirty
	for i := globalIdx; i < mb.globalOffset+int64(mb.ringSize); i++ {
		mb.dirtyTracker.MarkDirty(i)
	}
	mb.contentVersion++
}

// DeleteLine removes the line at the given global index.
// Lines after this index are shifted up.
func (mb *MemoryBuffer) DeleteLine(globalIdx int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if globalIdx < mb.globalOffset {
		return
	}

	offset := globalIdx - mb.globalOffset
	if offset >= int64(mb.ringSize) {
		return // Line doesn't exist
	}

	// Shift lines up (from deletion point to end)
	deleteIdx := int(offset)
	for i := deleteIdx; i < mb.ringSize-1; i++ {
		srcRingIdx := (mb.ringHead + i + 1) % len(mb.lines)
		dstRingIdx := (mb.ringHead + i) % len(mb.lines)
		mb.lines[dstRingIdx] = mb.lines[srcRingIdx]
	}

	// Clear last slot
	lastRingIdx := (mb.ringHead + mb.ringSize - 1) % len(mb.lines)
	mb.lines[lastRingIdx] = nil
	mb.ringSize--

	// Mark all affected lines dirty
	for i := globalIdx; i < mb.globalOffset+int64(mb.ringSize); i++ {
		mb.dirtyTracker.MarkDirty(i)
	}
	mb.contentVersion++
}

// --- Erase Operations ---

// EraseLine clears all content from a line.
func (mb *MemoryBuffer) EraseLine(globalIdx int64) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	line := mb.getLineLocked(globalIdx)
	if line != nil {
		line.Clear()
		mb.dirtyTracker.MarkDirty(globalIdx)
		mb.contentVersion++
	}
}

// EraseToEndOfLine clears cells from col to end of line.
func (mb *MemoryBuffer) EraseToEndOfLine(globalIdx int64, col int) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	line := mb.getLineLocked(globalIdx)
	if line != nil {
		line.Truncate(col)
		mb.dirtyTracker.MarkDirty(globalIdx)
		mb.contentVersion++
	}
}

// EraseFromStartOfLine clears cells from start of line to col (inclusive).
func (mb *MemoryBuffer) EraseFromStartOfLine(globalIdx int64, col int) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	line := mb.getLineLocked(globalIdx)
	if line == nil {
		return
	}

	// Replace cells 0 through col with default spaces
	for i := 0; i <= col && i < len(line.Cells); i++ {
		line.Cells[i] = Cell{Rune: ' ', FG: DefaultFG, BG: DefaultBG}
	}

	mb.dirtyTracker.MarkDirty(globalIdx)
	mb.contentVersion++
}

// --- Eviction ---

// Evict removes the oldest count lines from memory.
// Returns the actual number of lines evicted.
func (mb *MemoryBuffer) Evict(count int) int {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.evictLocked(count)
}

// evictLocked performs eviction without locking (caller must hold lock).
func (mb *MemoryBuffer) evictLocked(count int) int {
	if count <= 0 || mb.ringSize == 0 {
		return 0
	}
	if count > mb.ringSize {
		count = mb.ringSize
	}

	// Clear evicted line references (help GC)
	for i := 0; i < count; i++ {
		ringIdx := (mb.ringHead + i) % len(mb.lines)
		mb.lines[ringIdx] = nil
	}

	// Update ring state
	mb.ringHead = (mb.ringHead + count) % len(mb.lines)
	mb.ringSize -= count
	mb.globalOffset += int64(count)

	// Clean up dirty entries for evicted lines
	mb.dirtyTracker.RemoveBelow(mb.globalOffset)

	return count
}

// evictIfNeeded checks capacity and evicts if over limit.
// Called automatically after operations that add lines.
func (mb *MemoryBuffer) evictIfNeeded() int {
	if mb.ringSize <= mb.config.MaxLines {
		return 0
	}

	excess := mb.ringSize - mb.config.MaxLines
	evictCount := excess
	if evictCount < mb.config.EvictionBatch {
		evictCount = mb.config.EvictionBatch
	}
	if evictCount > mb.ringSize {
		evictCount = mb.ringSize
	}

	return mb.evictLocked(evictCount)
}

// --- Configuration ---

// Config returns a copy of the configuration.
func (mb *MemoryBuffer) Config() MemoryBufferConfig {
	return mb.config
}
