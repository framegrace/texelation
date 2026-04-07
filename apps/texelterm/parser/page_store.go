// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/page_store.go
// Summary: Page-based storage manager for terminal history.
//
// PageStore manages a directory of 64KB page files, providing:
//   - Buffered writes with automatic page flushing at 64KB boundary
//   - O(1) line lookup via in-memory page index
//   - Per-line timestamps for time-based navigation
//   - HistoryWriter interface for persistence abstraction
//
// Directory structure:
//
//	~/.local/share/texelation/history/terminals/<uuid>/pages/
//	├── 00000001.page
//	├── 00000002.page
//	└── ...

package parser

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ViewportState stores the terminal's viewport position for session restoration.
type ViewportState struct {
	// ScrollOffset is the number of physical lines scrolled back from the live edge.
	// 0 means at live edge (showing most recent content).
	ScrollOffset int64 `json:"scroll_offset"`

	// LiveEdgeBase is the global line index at the top of the live viewport.
	LiveEdgeBase int64 `json:"live_edge_base"`

	// CursorX is the cursor column position (0-indexed).
	CursorX int `json:"cursor_x"`

	// CursorY is the cursor row position relative to liveEdgeBase (0-indexed).
	CursorY int `json:"cursor_y"`

	// SavedAt is when the state was saved.
	SavedAt time.Time `json:"saved_at"`

	// PromptStartLine is the global line index of the last shell prompt start.
	// -1 means unknown (e.g., old WAL without this field).
	PromptStartLine int64 `json:"prompt_start_line"`

	// WorkingDir is the last known working directory from OSC 7.
	// Empty means unknown.
	WorkingDir string `json:"working_dir"`
}

// PageStoreConfig holds configuration for the page store.
type PageStoreConfig struct {
	// BaseDir is the base directory for all history storage.
	// Default: ~/.local/share/texelation/history
	BaseDir string

	// TerminalID is the UUID of the terminal.
	TerminalID string

	// TargetPageSize is the target page size in bytes.
	// Pages are flushed when they would exceed this size.
	// Default: 64 * 1024 (64KB)
	TargetPageSize int

	// SyncWrites forces fsync after each page write.
	// Slower but safer against crashes.
	SyncWrites bool
}

// DefaultPageStoreConfig returns sensible defaults.
func DefaultPageStoreConfig(baseDir, terminalID string) PageStoreConfig {
	return PageStoreConfig{
		BaseDir:        baseDir,
		TerminalID:     terminalID,
		TargetPageSize: TargetPageSize,
		SyncWrites:     false,
	}
}

// PageStore manages a collection of 64KB page files.
type PageStore struct {
	config PageStoreConfig

	// Directory for page files
	pagesDir string

	// Terminal directory (parent of pagesDir)
	terminalDir string

	// Current page being written
	currentPage *Page

	// Page state tracking
	nextPageID     uint64 // Next page ID to use
	nextGlobalIdx  int64  // Next global line index
	totalLineCount int64  // Total lines written

	// Page index: maps global line index to (pageID, offsetInPage)
	pageIndex []pageIndexEntry

	// File handle for writing current page
	currentFile   *os.File
	currentWriter *bufio.Writer

	mu sync.RWMutex
}

// pageIndexEntry tracks which page contains each line, keyed by global index.
type pageIndexEntry struct {
	globalIdx    int64  // Global line index this entry represents
	pageID       uint64 // Page containing this line
	offsetInPage int    // Line's index within the page (0-based)
}

// CreatePageStore creates a new page store, replacing any existing one.
func CreatePageStore(config PageStoreConfig) (*PageStore, error) {
	terminalDir := filepath.Join(config.BaseDir, "terminals", config.TerminalID)
	pagesDir := filepath.Join(terminalDir, "pages")

	// Create directory structure
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pages directory: %w", err)
	}

	// Remove any existing page files (clean start)
	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read pages directory: %w", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".page") {
			path := filepath.Join(pagesDir, entry.Name())
			if err := os.Remove(path); err != nil {
				return nil, fmt.Errorf("failed to remove old page %s: %w", entry.Name(), err)
			}
		}
	}

	ps := &PageStore{
		config:         config,
		pagesDir:       pagesDir,
		terminalDir:    terminalDir,
		nextPageID:     1,
		nextGlobalIdx:  0,
		totalLineCount: 0,
		pageIndex:      make([]pageIndexEntry, 0, 1000),
	}

	// Initialize first page
	if err := ps.startNewPage(); err != nil {
		return nil, err
	}

	return ps, nil
}

// OpenPageStore opens an existing page store.
// Returns nil, nil if the directory doesn't exist.
func OpenPageStore(config PageStoreConfig) (*PageStore, error) {
	terminalDir := filepath.Join(config.BaseDir, "terminals", config.TerminalID)
	pagesDir := filepath.Join(terminalDir, "pages")

	// Check if directory exists
	info, err := os.Stat(pagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat pages directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("pages path is not a directory: %s", pagesDir)
	}

	ps := &PageStore{
		config:      config,
		pagesDir:    pagesDir,
		terminalDir: terminalDir,
		pageIndex:   make([]pageIndexEntry, 0, 1000),
	}

	// Scan existing pages to build index
	if err := ps.rebuildIndex(); err != nil {
		return nil, fmt.Errorf("failed to rebuild index: %w", err)
	}

	// Initialize for appending
	if err := ps.prepareForAppend(); err != nil {
		return nil, fmt.Errorf("failed to prepare for append: %w", err)
	}

	return ps, nil
}

// rebuildIndex scans the pages directory to rebuild the line index.
func (ps *PageStore) rebuildIndex() error {
	entries, err := os.ReadDir(ps.pagesDir)
	if err != nil {
		return fmt.Errorf("failed to read pages directory: %w", err)
	}

	// Collect and sort page files by ID
	type pageInfo struct {
		id   uint64
		path string
	}
	var pages []pageInfo

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".page") {
			continue
		}

		// Parse page ID from filename (e.g., "00000001.page")
		name := strings.TrimSuffix(entry.Name(), ".page")
		id, err := strconv.ParseUint(name, 10, 64)
		if err != nil {
			continue // Skip malformed filenames
		}

		pages = append(pages, pageInfo{
			id:   id,
			path: filepath.Join(ps.pagesDir, entry.Name()),
		})
	}

	// Sort by page ID
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].id < pages[j].id
	})

	// Read each page header to build index
	for _, pi := range pages {
		page, err := ps.readPageHeader(pi.path)
		if err != nil {
			return fmt.Errorf("failed to read page %d header: %w", pi.id, err)
		}

		baseGlobal := int64(page.Header.FirstGlobalIdx)
		// Add index entries for each line in this page
		for i := uint32(0); i < page.Header.LineCount; i++ {
			ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
				globalIdx:    baseGlobal + int64(i),
				pageID:       pi.id,
				offsetInPage: int(i),
			})
		}

		ps.totalLineCount += int64(page.Header.LineCount)

		// Track highest page ID
		if pi.id >= ps.nextPageID {
			ps.nextPageID = pi.id + 1
		}
	}

	// Set nextGlobalIdx to the logical end (highest stored globalIdx + 1).
	if len(ps.pageIndex) > 0 {
		last := ps.pageIndex[len(ps.pageIndex)-1]
		ps.nextGlobalIdx = last.globalIdx + 1
	} else {
		ps.nextGlobalIdx = 0
	}

	return nil
}

// readPageHeader reads just the header from a page file.
func (ps *PageStore) readPageHeader(path string) (*Page, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	page := &Page{}
	_, err = page.readHeader(file)
	if err != nil {
		return nil, err
	}

	return page, nil
}

// prepareForAppend sets up the page store for appending new lines.
func (ps *PageStore) prepareForAppend() error {
	// If there are existing pages, check if the last one is full
	if ps.totalLineCount > 0 {
		// Find the last page
		lastPageID := ps.pageIndex[len(ps.pageIndex)-1].pageID

		// Load the last page to check if it's full
		lastPage, err := ps.loadPage(lastPageID)
		if err != nil {
			return fmt.Errorf("failed to load last page: %w", err)
		}

		// If the page is not full, use it as current page
		if lastPage.Size() < TargetPageSize {
			ps.currentPage = lastPage
			// We don't open for append - we'll rewrite the page on flush
			return nil
		}
	}

	// Start a new page
	return ps.startNewPage()
}

// startNewPage initializes a new empty page.
func (ps *PageStore) startNewPage() error {
	// Close current page if open
	if ps.currentFile != nil {
		if err := ps.flushCurrentPage(); err != nil {
			return err
		}
	}

	ps.currentPage = NewPage(ps.nextPageID, uint64(ps.nextGlobalIdx))
	ps.nextPageID++

	return nil
}

// writePageToDisk atomically writes a page to disk using temp file + rename.
// This is a shared helper used by both flushCurrentPage and updateLineInFlushedPage.
func (ps *PageStore) writePageToDisk(page *Page, path string) error {
	tmpPath := path + ".tmp"

	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp page file: %w", err)
	}

	writer := bufio.NewWriter(file)
	if _, err := page.WriteTo(writer); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write page: %w", err)
	}

	if err := writer.Flush(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to flush page: %w", err)
	}

	if ps.config.SyncWrites {
		if err := file.Sync(); err != nil {
			file.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("failed to sync page: %w", err)
		}
	}

	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close page file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename page file: %w", err)
	}

	return nil
}

// flushCurrentPage writes the current page to disk.
func (ps *PageStore) flushCurrentPage() error {
	if ps.currentPage == nil || ps.currentPage.Header.LineCount == 0 {
		return nil
	}

	// Update page state
	ps.currentPage.Header.State = PageStateWarm

	// Write to disk
	path := ps.pageFilePath(ps.currentPage.Header.PageID)
	if err := ps.writePageToDisk(ps.currentPage, path); err != nil {
		return err
	}

	// Clear current page
	ps.currentPage = nil
	ps.currentFile = nil
	ps.currentWriter = nil

	return nil
}

// loadPage reads a complete page from disk.
func (ps *PageStore) loadPage(pageID uint64) (*Page, error) {
	path := ps.pageFilePath(pageID)

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open page %d: %w", pageID, err)
	}
	defer file.Close()

	page := &Page{}
	_, err = page.ReadFrom(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read page %d: %w", pageID, err)
	}

	return page, nil
}

// pageFilePath returns the file path for a page ID.
func (ps *PageStore) pageFilePath(pageID uint64) string {
	return filepath.Join(ps.pagesDir, fmt.Sprintf("%08d.page", pageID))
}

// AppendLineWithGlobalIdx writes a line at the specified global index.
// globalIdx must be strictly greater than every previously stored globalIdx.
// If globalIdx is not contiguous with the current page, the current page is
// flushed and a new page is started anchored at globalIdx.
func (ps *PageStore) AppendLineWithGlobalIdx(globalIdx int64, line *LogicalLine, timestamp time.Time) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if globalIdx < ps.nextGlobalIdx {
		return fmt.Errorf("globalIdx %d must be >= nextGlobalIdx %d", globalIdx, ps.nextGlobalIdx)
	}

	// Determine if we need a fresh page: no current page, or a gap, or we
	// simply need to start fresh because the current page was flushed.
	needNewPage := ps.currentPage == nil
	if !needNewPage {
		expectedNext := int64(ps.currentPage.Header.FirstGlobalIdx) + int64(ps.currentPage.Header.LineCount)
		if globalIdx != expectedNext {
			// Gap — flush and start a new page anchored at globalIdx.
			if err := ps.flushCurrentPage(); err != nil {
				return err
			}
			needNewPage = true
		}
	}

	if needNewPage {
		ps.currentPage = NewPage(ps.nextPageID, uint64(globalIdx))
		ps.nextPageID++
	}

	// Try to add line to current page.
	if !ps.currentPage.AddLine(line, timestamp, 0) {
		// Page is full — flush and start a new page anchored at globalIdx.
		if err := ps.flushCurrentPage(); err != nil {
			return err
		}
		ps.currentPage = NewPage(ps.nextPageID, uint64(globalIdx))
		ps.nextPageID++
		if !ps.currentPage.AddLine(line, timestamp, 0) {
			// Oversized line — add anyway (same behavior as the old path).
			ps.currentPage.AddLine(line, timestamp, 0)
		}
	}

	// Update index.
	ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
		globalIdx:    globalIdx,
		pageID:       ps.currentPage.Header.PageID,
		offsetInPage: int(ps.currentPage.Header.LineCount) - 1,
	})

	ps.totalLineCount++
	ps.nextGlobalIdx = globalIdx + 1

	return nil
}

// findByGlobalIdx does a binary search on pageIndex for the entry matching
// globalIdx. Returns (entry, true) if found, (zero, false) otherwise.
// Caller must hold ps.mu (read or write).
func (ps *PageStore) findByGlobalIdx(globalIdx int64) (pageIndexEntry, bool) {
	n := len(ps.pageIndex)
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		if ps.pageIndex[mid].globalIdx < globalIdx {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < n && ps.pageIndex[lo].globalIdx == globalIdx {
		return ps.pageIndex[lo], true
	}
	return pageIndexEntry{}, false
}

// UpdateLine updates an existing line by global index.
// Returns an error if the global index is not stored.
func (ps *PageStore) UpdateLine(globalIdx int64, line *LogicalLine, timestamp time.Time) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	entry, ok := ps.findByGlobalIdx(globalIdx)
	if !ok {
		return fmt.Errorf("line %d not present in PageStore", globalIdx)
	}

	// Check if line is in current (unflushed) page.
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.UpdateLine(entry.offsetInPage, line, timestamp)
	}

	// Line is in a flushed page — reload, update, rewrite atomically.
	return ps.updateLineInFlushedPage(entry.pageID, entry.offsetInPage, line, timestamp)
}

// updateLineInFlushedPage loads a page from disk, updates a line, and rewrites it atomically.
func (ps *PageStore) updateLineInFlushedPage(pageID uint64, offsetInPage int, line *LogicalLine, timestamp time.Time) error {
	// Load the page
	page, err := ps.loadPage(pageID)
	if err != nil {
		return fmt.Errorf("failed to load page %d: %w", pageID, err)
	}

	// Update the line
	if err := page.UpdateLine(offsetInPage, line, timestamp); err != nil {
		return fmt.Errorf("failed to update line in page: %w", err)
	}

	// Rewrite the page atomically
	path := ps.pageFilePath(pageID)
	return ps.writePageToDisk(page, path)
}

// ReadLine reads a single line by global index.
// Returns (nil, nil) if the global index is not stored (gap or out of range).
func (ps *PageStore) ReadLine(globalIdx int64) (*LogicalLine, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	entry, ok := ps.findByGlobalIdx(globalIdx)
	if !ok {
		return nil, nil
	}

	// Check if line is in current page (not yet flushed).
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.GetLine(entry.offsetInPage), nil
	}

	// Load page from disk.
	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return nil, err
	}
	return page.GetLine(entry.offsetInPage), nil
}

// ReadLineRange reads a range of lines [start, end) by global index.
// Returns a slice of length (end - start), with nil entries for gaps
// or out-of-range indices. Caller can index directly as result[globalIdx - start].
func (ps *PageStore) ReadLineRange(start, end int64) ([]*LogicalLine, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if start < 0 {
		start = 0
	}
	if end <= start {
		return nil, nil
	}

	result := make([]*LogicalLine, end-start)

	// Find the first stored entry with globalIdx >= start.
	lo, hi := 0, len(ps.pageIndex)
	for lo < hi {
		mid := (lo + hi) / 2
		if ps.pageIndex[mid].globalIdx < start {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	// Walk entries in order, loading pages lazily and batching reads.
	var currentPageID uint64
	var currentPage *Page
	for i := lo; i < len(ps.pageIndex); i++ {
		entry := ps.pageIndex[i]
		if entry.globalIdx >= end {
			break
		}

		var line *LogicalLine
		if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
			line = ps.currentPage.GetLine(entry.offsetInPage)
		} else {
			if currentPage == nil || entry.pageID != currentPageID {
				p, err := ps.loadPage(entry.pageID)
				if err != nil {
					return nil, fmt.Errorf("failed to load page %d: %w", entry.pageID, err)
				}
				currentPage = p
				currentPageID = entry.pageID
			}
			line = currentPage.GetLine(entry.offsetInPage)
		}

		result[entry.globalIdx-start] = line
	}

	return result, nil
}

// ReadLineWithTimestamp reads a line and its timestamp by global index.
// Returns (nil, zero, nil) if the global index is not stored.
func (ps *PageStore) ReadLineWithTimestamp(globalIdx int64) (*LogicalLine, time.Time, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	entry, ok := ps.findByGlobalIdx(globalIdx)
	if !ok {
		return nil, time.Time{}, nil
	}

	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		line := ps.currentPage.GetLine(entry.offsetInPage)
		ts := ps.currentPage.GetTimestamp(entry.offsetInPage)
		return line, ts, nil
	}

	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return nil, time.Time{}, err
	}
	return page.GetLine(entry.offsetInPage), page.GetTimestamp(entry.offsetInPage), nil
}

// LineCount returns the logical end of the global-index space:
// the highest stored global index plus one (zero if empty).
// Note: this may exceed the number of stored lines when gaps exist.
// Use StoredLineCount for the actual stored count.
func (ps *PageStore) LineCount() int64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.nextGlobalIdx
}

// HasLine reports whether a line is actually stored at the given global index.
// Unlike LineCount (which returns the logical end), this checks the sparse
// storage directly — gap indices return false even though they lie within
// [0, LineCount()).
func (ps *PageStore) HasLine(globalIdx int64) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	_, ok := ps.findByGlobalIdx(globalIdx)
	return ok
}

// StoredLineCount returns the number of lines actually stored.
// This may be less than LineCount() when there are gaps in the
// global-index space (e.g., from LineFeed operations that advanced
// the live edge without dirtying intermediate lines).
func (ps *PageStore) StoredLineCount() int64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.totalLineCount
}

// StoredLineCountBelow returns the number of stored lines whose globalIdx
// is strictly less than the given index. Used by scroll math to count the
// actual scrollable disk content (excluding sparse gaps), so the viewport
// doesn't think there are ~60K phantom rows above when most of the
// global-index range below memBuf is gaps.
func (ps *PageStore) StoredLineCountBelow(globalIdx int64) int64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	// Binary search for the first pageIndex entry with globalIdx >= the
	// argument. The result's lower bound is the count of entries strictly
	// less than globalIdx.
	n := len(ps.pageIndex)
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		if ps.pageIndex[mid].globalIdx < globalIdx {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return int64(lo)
}

// Close flushes the current page and closes the store.
func (ps *PageStore) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Flush current page if it has content
	if ps.currentPage != nil && ps.currentPage.Header.LineCount > 0 {
		if err := ps.flushCurrentPage(); err != nil {
			return err
		}
	}

	return nil
}

// Path returns the base directory path.
func (ps *PageStore) Path() string {
	return ps.pagesDir
}

// Flush forces the current page to be written to disk.
// Useful for ensuring data durability without closing the store.
func (ps *PageStore) Flush() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.currentPage != nil && ps.currentPage.Header.LineCount > 0 {
		if err := ps.flushCurrentPage(); err != nil {
			return err
		}

		// Start new page with NEW pageID for future appends
		// (currentPage is now nil after flushCurrentPage)
		ps.currentPage = NewPage(ps.nextPageID, uint64(ps.nextGlobalIdx))
		ps.nextPageID++
	}

	return nil
}

// GetTimestamp returns the timestamp for the line at the given global index.
// Returns zero time if the index is not stored.
func (ps *PageStore) GetTimestamp(globalIdx int64) (time.Time, error) {
	_, ts, err := ps.ReadLineWithTimestamp(globalIdx)
	return ts, err
}

// FindLineAt returns the global index of the stored line closest to (but not
// after) the given time. Returns -1 if no lines are stored.
func (ps *PageStore) FindLineAt(t time.Time) (int64, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	n := len(ps.pageIndex)
	if n == 0 {
		return -1, nil
	}

	targetNano := t.UnixNano()
	lo, hi := 0, n-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		ts, err := ps.getTimestampAtPosUnlocked(mid)
		if err != nil {
			return -1, err
		}
		if ts.UnixNano() <= targetNano {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return ps.pageIndex[lo].globalIdx, nil
}

// getTimestampAtPosUnlocked returns the timestamp for the pageIndex entry at
// position `pos` (not by globalIdx). Caller must hold lock.
func (ps *PageStore) getTimestampAtPosUnlocked(pos int) (time.Time, error) {
	if pos < 0 || pos >= len(ps.pageIndex) {
		return time.Time{}, nil
	}
	entry := ps.pageIndex[pos]

	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.GetTimestamp(entry.offsetInPage), nil
	}

	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return time.Time{}, err
	}
	return page.GetTimestamp(entry.offsetInPage), nil
}
