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

// pageIndexEntry tracks which page contains each line.
type pageIndexEntry struct {
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

		// Add index entries for each line in this page
		for i := uint32(0); i < page.Header.LineCount; i++ {
			ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
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

	ps.nextGlobalIdx = ps.totalLineCount

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

// AppendLine writes a logical line to the page store.
// Uses time.Now() as the timestamp.
func (ps *PageStore) AppendLine(line *LogicalLine) error {
	return ps.AppendLineWithTimestamp(line, time.Now())
}

// AppendLineWithTimestamp writes a line with an explicit timestamp.
func (ps *PageStore) AppendLineWithTimestamp(line *LogicalLine, timestamp time.Time) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Ensure we have a current page
	if ps.currentPage == nil {
		if err := ps.startNewPage(); err != nil {
			return err
		}
	}

	// Try to add line to current page
	if !ps.currentPage.AddLine(line, timestamp, 0) {
		// Page is full, flush and start new page
		if err := ps.flushCurrentPage(); err != nil {
			return err
		}
		if err := ps.startNewPage(); err != nil {
			return err
		}

		// Add to new page (should always succeed for a fresh page)
		if !ps.currentPage.AddLine(line, timestamp, 0) {
			// Line is too large for a single page - this is a problem
			// For now, we add it anyway (may exceed 64KB)
			ps.currentPage.AddLine(line, timestamp, 0)
		}
	}

	// Update index
	ps.pageIndex = append(ps.pageIndex, pageIndexEntry{
		pageID:       ps.currentPage.Header.PageID,
		offsetInPage: int(ps.currentPage.Header.LineCount) - 1,
	})

	ps.totalLineCount++
	ps.nextGlobalIdx++

	return nil
}

// UpdateLine updates an existing line by global index.
// If the line is in the current (unflushed) page, updates in-place.
// If the line is in a flushed page, reloads, updates, and rewrites the page atomically.
// Returns error if the index doesn't exist.
func (ps *PageStore) UpdateLine(index int64, line *LogicalLine, timestamp time.Time) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if index < 0 || index >= ps.totalLineCount {
		return fmt.Errorf("line index %d out of bounds (0-%d)", index, ps.totalLineCount-1)
	}

	entry := ps.pageIndex[index]

	// Check if line is in current page (not yet flushed) - easy case
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.UpdateLine(entry.offsetInPage, line, timestamp)
	}

	// Line is in a flushed page - need to reload, update, and rewrite
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
// Returns nil if index is out of bounds.
func (ps *PageStore) ReadLine(index int64) (*LogicalLine, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if index < 0 || index >= ps.totalLineCount {
		return nil, nil
	}

	entry := ps.pageIndex[index]

	// Check if line is in current page (not yet flushed)
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.GetLine(entry.offsetInPage), nil
	}

	// Load page from disk
	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return nil, err
	}

	return page.GetLine(entry.offsetInPage), nil
}

// ReadLineRange reads a range of lines [start, end).
// Returns lines that exist within the range.
func (ps *PageStore) ReadLineRange(start, end int64) ([]*LogicalLine, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	// Clamp range
	if start < 0 {
		start = 0
	}
	if end > ps.totalLineCount {
		end = ps.totalLineCount
	}
	if start >= end {
		return nil, nil
	}

	result := make([]*LogicalLine, 0, end-start)

	// Group reads by page for efficiency
	var currentPageID uint64
	var currentPage *Page

	for i := start; i < end; i++ {
		entry := ps.pageIndex[i]

		// Check if line is in current (unflushed) page
		if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
			result = append(result, ps.currentPage.GetLine(entry.offsetInPage))
			continue
		}

		// Load page if needed
		if currentPage == nil || entry.pageID != currentPageID {
			var err error
			currentPage, err = ps.loadPage(entry.pageID)
			if err != nil {
				return nil, fmt.Errorf("failed to load page %d: %w", entry.pageID, err)
			}
			currentPageID = entry.pageID
		}

		result = append(result, currentPage.GetLine(entry.offsetInPage))
	}

	return result, nil
}

// ReadLineWithTimestamp reads a line and its timestamp.
func (ps *PageStore) ReadLineWithTimestamp(index int64) (*LogicalLine, time.Time, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if index < 0 || index >= ps.totalLineCount {
		return nil, time.Time{}, nil
	}

	entry := ps.pageIndex[index]

	// Check if line is in current page
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		line := ps.currentPage.GetLine(entry.offsetInPage)
		ts := ps.currentPage.GetTimestamp(entry.offsetInPage)
		return line, ts, nil
	}

	// Load page from disk
	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return nil, time.Time{}, err
	}

	line := page.GetLine(entry.offsetInPage)
	ts := page.GetTimestamp(entry.offsetInPage)
	return line, ts, nil
}

// LineCount returns the total number of lines written.
func (ps *PageStore) LineCount() int64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.totalLineCount
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

// GetTimestamp returns the timestamp for a line by global index.
func (ps *PageStore) GetTimestamp(index int64) (time.Time, error) {
	_, ts, err := ps.ReadLineWithTimestamp(index)
	return ts, err
}

// FindLineAt returns the global line index closest to the given time.
// If exact match not found, returns the line just before the time.
func (ps *PageStore) FindLineAt(t time.Time) (int64, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if ps.totalLineCount == 0 {
		return -1, nil
	}

	targetNano := t.UnixNano()

	// Binary search for the line
	low, high := int64(0), ps.totalLineCount-1

	for low < high {
		mid := (low + high + 1) / 2

		ts, err := ps.getTimestampUnlocked(mid)
		if err != nil {
			return -1, err
		}

		if ts.UnixNano() <= targetNano {
			low = mid
		} else {
			high = mid - 1
		}
	}

	return low, nil
}

// getTimestampUnlocked gets timestamp without locking (caller must hold lock).
func (ps *PageStore) getTimestampUnlocked(index int64) (time.Time, error) {
	if index < 0 || index >= ps.totalLineCount {
		return time.Time{}, nil
	}

	entry := ps.pageIndex[index]

	// Check if line is in current page
	if ps.currentPage != nil && entry.pageID == ps.currentPage.Header.PageID {
		return ps.currentPage.GetTimestamp(entry.offsetInPage), nil
	}

	// Load page from disk
	page, err := ps.loadPage(entry.pageID)
	if err != nil {
		return time.Time{}, err
	}

	return page.GetTimestamp(entry.offsetInPage), nil
}
