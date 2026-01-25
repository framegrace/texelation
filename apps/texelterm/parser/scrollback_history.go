package parser

import (
	"fmt"
	"sync"
)

// DefaultMemoryLines is the default number of lines to keep in memory for scrollback.
// This is used by term.go when no config value is specified.
const DefaultMemoryLines = 100000

// ScrollbackHistoryConfig holds configuration for scrollback history.
type ScrollbackHistoryConfig struct {
	// MaxMemoryLines is the maximum logical lines to keep in memory.
	// When exceeded, oldest lines are unloaded (but remain on disk).
	// Default: 5000
	MaxMemoryLines int

	// MarginAbove is how many lines to load above the visible window.
	// Default: 1000
	MarginAbove int

	// MarginBelow is how many lines to keep below the visible window.
	// Default: 500
	MarginBelow int

	// DiskPath is the path for persistent storage. Empty disables persistence.
	DiskPath string

	// DiskConfig holds additional disk storage options.
	DiskConfig DiskHistoryConfig
}

// DefaultScrollbackHistoryConfig returns sensible defaults.
func DefaultScrollbackHistoryConfig() ScrollbackHistoryConfig {
	return ScrollbackHistoryConfig{
		MaxMemoryLines: 5000,
		MarginAbove:    1000,
		MarginBelow:    500,
		DiskPath:       "",
	}
}

// ScrollbackHistory stores logical lines for terminal scrollback.
// It implements a three-level architecture:
//   - Disk: All lines ever written (via DiskHistory)
//   - Memory: A sliding window of lines (~5000) for fast access
//   - Display: Physical lines at current width (handled by DisplayBuffer)
//
// When display needs more lines than memory has, ScrollbackHistory
// loads from disk on demand.
type ScrollbackHistory struct {
	// config holds all configurable parameters
	config ScrollbackHistoryConfig

	// lines stores logical lines currently in memory.
	// This is a sliding window into the full history.
	lines []*LogicalLine

	// windowStart is the global line index of lines[0].
	// Global indices are 0-based from the start of all history.
	windowStart int64

	// totalLines is the total number of lines ever committed (in memory + on disk).
	totalLines int64

	// disk provides persistent storage (nil if persistence disabled).
	disk *DiskHistory

	// dirty tracks whether history has uncommitted changes.
	dirty bool

	mu sync.RWMutex
}

// NewScrollbackHistory creates a new scrollback history with the given configuration.
func NewScrollbackHistory(config ScrollbackHistoryConfig) *ScrollbackHistory {
	if config.MaxMemoryLines <= 0 {
		config.MaxMemoryLines = DefaultMaxMemoryLines
	}
	if config.MarginAbove <= 0 {
		config.MarginAbove = 1000 // History margins differ from display buffer margins
	}
	if config.MarginBelow <= 0 {
		config.MarginBelow = 500 // History margins differ from display buffer margins
	}

	h := &ScrollbackHistory{
		config:      config,
		lines:       make([]*LogicalLine, 0, min(config.MaxMemoryLines, 1000)),
		windowStart: 0,
		totalLines:  0,
		dirty:       false,
	}

	return h
}

// NewScrollbackHistoryWithDisk creates a scrollback history with disk persistence.
// If the disk file exists and has valid format, loads from it.
// If it's an old format or doesn't exist, starts fresh.
func NewScrollbackHistoryWithDisk(config ScrollbackHistoryConfig) (*ScrollbackHistory, error) {
	h := NewScrollbackHistory(config)

	if config.DiskPath == "" {
		return h, nil
	}

	// Try to open existing history
	existing, err := OpenDiskHistory(config.DiskPath)
	if err != nil {
		// Invalid format - start fresh (error is expected for new files)
	} else if existing != nil {
		// Valid existing history - use it
		h.disk = existing
		h.totalLines = existing.LineCount()
		h.windowStart = h.totalLines // Start with empty memory window at the end

		// Load recent lines into memory
		if h.totalLines > 0 {
			loadCount := min(int64(config.MaxMemoryLines), h.totalLines)
			h.windowStart = h.totalLines - loadCount
			lines, err := existing.ReadLineRange(h.windowStart, h.totalLines)
			if err != nil {
				existing.Close()
				return nil, fmt.Errorf("failed to load initial lines: %w", err)
			}
			h.lines = lines
		}

		// Enable write mode so we can append new lines
		if err := existing.EnableWriteMode(); err != nil {
			existing.Close()
			return nil, fmt.Errorf("failed to enable write mode: %w", err)
		}

		return h, nil
	}

	// No existing file or old format - create new
	diskConfig := config.DiskConfig
	if diskConfig.Path == "" {
		diskConfig.Path = config.DiskPath
	}
	disk, err := CreateDiskHistory(diskConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create disk history: %w", err)
	}
	h.disk = disk

	return h, nil
}

// Len returns the number of logical lines currently in memory.
func (h *ScrollbackHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.lines)
}

// TotalLen returns the total number of lines ever committed (memory + disk).
func (h *ScrollbackHistory) TotalLen() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.totalLines
}

// WindowStart returns the global index of the first line in memory.
func (h *ScrollbackHistory) WindowStart() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.windowStart
}

// WindowEnd returns the global index just past the last line in memory.
func (h *ScrollbackHistory) WindowEnd() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.windowStart + int64(len(h.lines))
}

// MaxMemoryLines returns the configured maximum memory lines.
func (h *ScrollbackHistory) MaxMemoryLines() int {
	return h.config.MaxMemoryLines
}

// Get returns the logical line at the given memory index (0-based into memory window).
// Returns nil if index is out of bounds.
// For global index access, use GetGlobal.
func (h *ScrollbackHistory) Get(index int) *LogicalLine {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if index < 0 || index >= len(h.lines) {
		return nil
	}
	return h.lines[index]
}

// GetGlobal returns the logical line at the given global index.
// If the line is not in memory but is on disk, loads it.
// Returns nil if index is out of bounds.
func (h *ScrollbackHistory) GetGlobal(globalIndex int64) *LogicalLine {
	h.mu.Lock()
	defer h.mu.Unlock()

	if globalIndex < 0 || globalIndex >= h.totalLines {
		return nil
	}

	// Check if in memory window
	memIndex := globalIndex - h.windowStart
	if memIndex >= 0 && memIndex < int64(len(h.lines)) {
		return h.lines[memIndex]
	}

	// Not in memory - need to load from disk
	if h.disk == nil {
		return nil // No disk backing, line was trimmed
	}

	line, err := h.disk.ReadLine(globalIndex)
	if err != nil {
		return nil
	}
	return line
}

// Append adds a new logical line to the end of history.
// The line is added to both memory and disk (if enabled).
func (h *ScrollbackHistory) Append(line *LogicalLine) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Write to disk first (if enabled)
	if h.disk != nil {
		// Ignore disk write errors - continue with memory-only
		_ = h.disk.AppendLine(line)
	}

	// Add to memory
	h.lines = append(h.lines, line)
	h.totalLines++
	h.dirty = true

	// Trim memory if over capacity
	h.trimAboveLocked()
}

// AppendCells is a convenience method that creates a logical line from cells and appends it.
func (h *ScrollbackHistory) AppendCells(cells []Cell) {
	h.Append(NewLogicalLineFromCells(cells))
}

// PopLast removes and returns the last line from history.
// Returns nil if history is empty or the last line has been flushed to disk.
// This is used for "uncommitting" recently committed lines during bash redraw.
func (h *ScrollbackHistory) PopLast() *LogicalLine {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.lines) == 0 {
		return nil
	}

	// We can only pop if the last line is in our memory window
	// (not yet flushed to disk or trimmed)
	lastIdx := len(h.lines) - 1
	line := h.lines[lastIdx]

	// Remove from memory
	h.lines[lastIdx] = nil // Help GC
	h.lines = h.lines[:lastIdx]
	h.totalLines--
	h.dirty = true

	// Note: We don't remove from disk. If disk persistence is enabled,
	// the disk may still have this line. This is acceptable because:
	// 1. PopLast is only used for temporary uncommits during editing
	// 2. The line will be re-committed when editing completes
	// 3. Disk truncation is complex and rarely needed

	return line
}

// LoadAbove loads older lines from disk into the memory window.
// Returns the number of lines actually loaded.
func (h *ScrollbackHistory) LoadAbove(count int) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.disk == nil || h.windowStart <= 0 || count <= 0 {
		return 0
	}

	// Calculate range to load
	loadEnd := h.windowStart
	loadStart := max(0, loadEnd-int64(count))
	actualCount := int(loadEnd - loadStart)

	if actualCount <= 0 {
		return 0
	}

	// Read from disk
	lines, err := h.disk.ReadLineRange(loadStart, loadEnd)
	if err != nil {
		return 0
	}

	// Prepend to memory
	h.lines = append(lines, h.lines...)
	h.windowStart = loadStart

	// Trim below if memory is too large
	h.trimBelowLocked()

	return len(lines)
}

// LoadBelow loads newer lines from disk into the memory window.
// This is less common - usually new lines are appended directly.
// Returns the number of lines actually loaded.
func (h *ScrollbackHistory) LoadBelow(count int) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.disk == nil || count <= 0 {
		return 0
	}

	windowEnd := h.windowStart + int64(len(h.lines))
	if windowEnd >= h.totalLines {
		return 0 // Already at the end
	}

	// Calculate range to load
	loadStart := windowEnd
	loadEnd := min(h.totalLines, loadStart+int64(count))
	actualCount := int(loadEnd - loadStart)

	if actualCount <= 0 {
		return 0
	}

	// Read from disk
	lines, err := h.disk.ReadLineRange(loadStart, loadEnd)
	if err != nil {
		return 0
	}

	// Append to memory
	h.lines = append(h.lines, lines...)

	// Trim above if memory is too large
	h.trimAboveLocked()

	return len(lines)
}

// trimAboveLocked removes oldest lines when memory exceeds capacity.
// Caller must hold the lock.
func (h *ScrollbackHistory) trimAboveLocked() {
	excess := len(h.lines) - h.config.MaxMemoryLines
	if excess <= 0 {
		return
	}

	for i := 0; i < excess; i++ {
		h.lines[i] = nil
	}
	h.lines = h.lines[excess:]
	h.windowStart += int64(excess)
}

// trimBelowLocked removes newest lines when memory exceeds capacity.
// Caller must hold the lock.
func (h *ScrollbackHistory) trimBelowLocked() {
	excess := len(h.lines) - h.config.MaxMemoryLines
	if excess <= 0 {
		return
	}

	// Remove from the end (newest lines)
	newLen := len(h.lines) - excess
	for i := newLen; i < len(h.lines); i++ {
		h.lines[i] = nil
	}
	h.lines = h.lines[:newLen]
}

// Clear removes all lines from memory (disk is NOT cleared).
func (h *ScrollbackHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.lines {
		h.lines[i] = nil
	}
	h.lines = h.lines[:0]
	h.windowStart = h.totalLines // Window is now empty, at the end
	h.dirty = true
}

// ClearScrollback clears all committed history (ED 3 behavior).
// This removes both in-memory lines and resets the disk history.
// After this call, the history is empty but ready to receive new content.
func (h *ScrollbackHistory) ClearScrollback() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Clear memory
	for i := range h.lines {
		h.lines[i] = nil
	}
	h.lines = h.lines[:0]

	// Reset indices
	h.windowStart = 0
	h.totalLines = 0
	h.dirty = true

	// If we have disk backing, close and recreate it
	// This effectively truncates the history file
	if h.disk != nil {
		diskPath := h.config.DiskPath
		h.disk.Close()
		h.disk = nil

		// Recreate empty disk history
		diskConfig := h.config.DiskConfig
		if diskConfig.Path == "" {
			diskConfig.Path = diskPath
		}
		disk, err := CreateDiskHistory(diskConfig)
		if err == nil {
			h.disk = disk
		}
	}
}

// IsDirty returns whether history has changes since last MarkClean.
func (h *ScrollbackHistory) IsDirty() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.dirty
}

// MarkClean clears the dirty flag.
func (h *ScrollbackHistory) MarkClean() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dirty = false
}

// GetRange returns a slice of logical lines from start to end (memory indices).
// Indices are clamped to valid bounds.
func (h *ScrollbackHistory) GetRange(start, end int) []*LogicalLine {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if start < 0 {
		start = 0
	}
	if end > len(h.lines) {
		end = len(h.lines)
	}
	if start >= end {
		return nil
	}
	return h.lines[start:end]
}

// GetGlobalRange returns lines from globalStart to globalEnd.
// Loads from disk if necessary.
func (h *ScrollbackHistory) GetGlobalRange(globalStart, globalEnd int64) []*LogicalLine {
	h.mu.Lock()
	defer h.mu.Unlock()

	if globalStart < 0 {
		globalStart = 0
	}
	if globalEnd > h.totalLines {
		globalEnd = h.totalLines
	}
	if globalStart >= globalEnd {
		return nil
	}

	// Check if entirely in memory
	windowEnd := h.windowStart + int64(len(h.lines))
	if globalStart >= h.windowStart && globalEnd <= windowEnd {
		// Entirely in memory
		memStart := int(globalStart - h.windowStart)
		memEnd := int(globalEnd - h.windowStart)
		return h.lines[memStart:memEnd]
	}

	// Need to load from disk
	if h.disk == nil {
		// No disk - return what we have in memory
		memStart := max(0, int(globalStart-h.windowStart))
		memEnd := min(len(h.lines), int(globalEnd-h.windowStart))
		if memStart >= memEnd {
			return nil
		}
		return h.lines[memStart:memEnd]
	}

	lines, err := h.disk.ReadLineRange(globalStart, globalEnd)
	if err != nil {
		return nil
	}
	return lines
}

// LastN returns the last n logical lines from memory (or fewer if smaller).
func (h *ScrollbackHistory) LastN(n int) []*LogicalLine {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n <= 0 {
		return nil
	}
	start := len(h.lines) - n
	if start < 0 {
		start = 0
	}
	return h.lines[start:]
}

// All returns all logical lines in memory.
func (h *ScrollbackHistory) All() []*LogicalLine {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lines
}

// WrapToWidth wraps a range of memory lines to physical lines at the given width.
func (h *ScrollbackHistory) WrapToWidth(start, end, width int) []PhysicalLine {
	lines := h.GetRange(start, end)
	var result []PhysicalLine

	for i, line := range lines {
		logicalIdx := int(h.windowStart) + start + i
		physical := line.WrapToWidth(width)
		for j := range physical {
			physical[j].LogicalIndex = logicalIdx
		}
		result = append(result, physical...)
	}

	return result
}

// WrapGlobalToWidth wraps a range of global lines to physical lines.
func (h *ScrollbackHistory) WrapGlobalToWidth(globalStart, globalEnd int64, width int) []PhysicalLine {
	lines := h.GetGlobalRange(globalStart, globalEnd)
	var result []PhysicalLine

	for i, line := range lines {
		logicalIdx := int(globalStart) + i
		physical := line.WrapToWidth(width)
		for j := range physical {
			physical[j].LogicalIndex = logicalIdx
		}
		result = append(result, physical...)
	}

	return result
}

// PhysicalLineCount returns how many physical lines the in-memory history would produce.
func (h *ScrollbackHistory) PhysicalLineCount(width int) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if width <= 0 {
		width = DefaultWidth
	}
	count := 0
	for _, line := range h.lines {
		cells := len(line.Cells)
		if cells == 0 {
			count++
		} else {
			count += (cells + width - 1) / width
		}
	}
	return count
}

// FindLogicalIndexForPhysicalLine returns the logical line index and offset
// for a given physical line number at the specified width (memory-relative).
func (h *ScrollbackHistory) FindLogicalIndexForPhysicalLine(physicalLine, width int) (logicalIndex, offset int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if width <= 0 {
		width = DefaultWidth
	}
	if physicalLine < 0 {
		return -1, 0
	}

	currentPhysical := 0
	for i, line := range h.lines {
		cells := len(line.Cells)
		var linesForThis int
		if cells == 0 {
			linesForThis = 1
		} else {
			linesForThis = (cells + width - 1) / width
		}

		if currentPhysical+linesForThis > physicalLine {
			offsetWithinLogical := physicalLine - currentPhysical
			return i, offsetWithinLogical * width
		}
		currentPhysical += linesForThis
	}

	return -1, 0
}

// HasDiskBacking returns whether disk persistence is enabled.
func (h *ScrollbackHistory) HasDiskBacking() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.disk != nil
}

// CanLoadAbove returns whether there are more lines on disk above the memory window.
func (h *ScrollbackHistory) CanLoadAbove() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.disk != nil && h.windowStart > 0
}

// CanLoadBelow returns whether there are more lines on disk below the memory window.
func (h *ScrollbackHistory) CanLoadBelow() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	windowEnd := h.windowStart + int64(len(h.lines))
	return h.disk != nil && windowEnd < h.totalLines
}

// Close closes the disk backing (if any).
func (h *ScrollbackHistory) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.disk != nil {
		err := h.disk.Close()
		h.disk = nil
		return err
	}
	return nil
}

// Config returns a copy of the configuration.
func (h *ScrollbackHistory) Config() ScrollbackHistoryConfig {
	return h.config
}

// TruncateTo truncates the in-memory lines to the specified global line index.
// This is used for TUI mode to clear and replace viewport content.
// IMPORTANT: Disk content is NOT modified - only memory is truncated.
// On next load from disk, full history will be restored.
//
// This enables the "truncate + append" pattern for TUI content preservation:
// 1. TruncateTo(liveViewportStart) - clears previous frozen content
// 2. Append new frozen lines
// 3. liveViewportStart updated to new end
func (h *ScrollbackHistory) TruncateTo(newLen int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if newLen < 0 {
		newLen = 0
	}
	if newLen >= h.totalLines {
		return // Nothing to truncate
	}

	// Calculate where this falls in the memory window
	windowEnd := h.windowStart + int64(len(h.lines))

	if newLen <= h.windowStart {
		// Truncation point is before or at the start of our memory window.
		// Clear all memory, but can't affect disk.
		for i := range h.lines {
			h.lines[i] = nil
		}
		h.lines = h.lines[:0]
		h.totalLines = newLen
		h.windowStart = newLen
		h.dirty = true
		return
	}

	if newLen >= windowEnd {
		// Nothing in memory to truncate (truncation point is beyond memory)
		h.totalLines = newLen
		h.dirty = true
		return
	}

	// Truncation point is within our memory window
	newMemLen := int(newLen - h.windowStart)

	// Nil out truncated lines for GC
	for i := newMemLen; i < len(h.lines); i++ {
		h.lines[i] = nil
	}
	h.lines = h.lines[:newMemLen]
	h.totalLines = newLen
	h.dirty = true

	// Note: Disk is NOT modified. On reload, full history will be restored.
	// This is intentional for TUI content preservation - we only truncate
	// memory to make room for fresh frozen content.
}
