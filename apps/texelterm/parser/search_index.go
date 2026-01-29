// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/search_index.go
// Summary: SQLite FTS5 search index for terminal history.
//
// Provides full-text search over terminal history with:
//   - Async batch indexing for regular output
//   - Sync indexing for commands (OSC 133)
//   - BM25 relevance ranking
//   - Time-based navigation

package parser

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SearchIndex provides full-text search over terminal history.
type SearchIndex interface {
	// IndexLine indexes a single line.
	// Commands (isCommand=true) are indexed synchronously for immediate searchability.
	// Output is queued for batch indexing.
	IndexLine(lineIdx int64, timestamp time.Time, text string, isCommand bool) error

	// Search executes an FTS5 search query.
	// Query uses SQLite FTS5 MATCH syntax (e.g., "docker AND run", "git*").
	// Returns up to limit results ordered by relevance (BM25).
	Search(query string, limit int) ([]SearchResult, error)

	// SearchInRange searches within a time range.
	SearchInRange(query string, start, end time.Time, limit int) ([]SearchResult, error)

	// FindLineAt returns the global line index closest to the given time.
	// Returns the line at or just before the given time.
	FindLineAt(t time.Time) (int64, error)

	// GetTimestamp returns the timestamp for a line by global index.
	GetTimestamp(lineIdx int64) (time.Time, error)

	// Flush blocks until all pending entries are indexed.
	Flush() error

	// Close flushes pending writes and closes the database.
	Close() error
}

// SearchResult represents a single search match.
type SearchResult struct {
	GlobalLineIdx int64
	Timestamp     time.Time
	Content       string
	IsCommand     bool
}

// SearchIndexConfig holds configuration for the search index.
type SearchIndexConfig struct {
	// DBPath is the path to the SQLite database file.
	DBPath string

	// BatchSize is the number of entries to accumulate before flushing.
	// Default: 100
	BatchSize int

	// BatchTimeout is how long to wait before flushing a partial batch.
	// Default: 5s
	BatchTimeout time.Duration

	// ChannelBuffer is the size of the async indexing channel.
	// Default: 1000
	ChannelBuffer int
}

// DefaultSearchIndexConfig returns sensible defaults.
func DefaultSearchIndexConfig(dbPath string) SearchIndexConfig {
	return SearchIndexConfig{
		DBPath:        dbPath,
		BatchSize:     100,
		BatchTimeout:  5 * time.Second,
		ChannelBuffer: 1000,
	}
}

// indexEntry represents a queued line to be indexed.
type indexEntry struct {
	lineIdx   int64
	timestamp time.Time
	text      string
	isCommand bool
}

// SQLiteSearchIndex implements SearchIndex using SQLite FTS5.
type SQLiteSearchIndex struct {
	config SearchIndexConfig
	db     *sql.DB

	// Async batching
	batchChan chan indexEntry
	stopCh    chan struct{}
	doneCh    chan struct{}
	flushCh   chan chan struct{}

	mu sync.RWMutex
}

// SQLite schema for the search index
const searchIndexSchema = `
-- Main content table
CREATE TABLE IF NOT EXISTS lines (
    id INTEGER PRIMARY KEY,           -- Global line index
    timestamp INTEGER NOT NULL,       -- UnixNano
    is_command INTEGER DEFAULT 0,     -- 1 if OSC 133 command
    content TEXT NOT NULL
);

-- FTS5 virtual table for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS lines_fts USING fts5(
    content,
    content='lines',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers to keep FTS5 in sync
CREATE TRIGGER IF NOT EXISTS lines_ai AFTER INSERT ON lines BEGIN
    INSERT INTO lines_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS lines_au AFTER UPDATE ON lines BEGIN
    INSERT INTO lines_fts(lines_fts, rowid, content) VALUES ('delete', old.id, old.content);
    INSERT INTO lines_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS lines_ad AFTER DELETE ON lines BEGIN
    INSERT INTO lines_fts(lines_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;

-- Index for time-based navigation
CREATE INDEX IF NOT EXISTS idx_lines_timestamp ON lines(timestamp);

-- Index for command filtering
CREATE INDEX IF NOT EXISTS idx_lines_command ON lines(is_command) WHERE is_command = 1;
`

// NewSearchIndex creates a new SQLite-backed search index.
func NewSearchIndex(dbPath string) (*SQLiteSearchIndex, error) {
	return NewSearchIndexWithConfig(DefaultSearchIndexConfig(dbPath))
}

// NewSearchIndexWithConfig creates a search index with custom configuration.
func NewSearchIndexWithConfig(config SearchIndexConfig) (*SQLiteSearchIndex, error) {
	// Ensure directory exists
	dir := filepath.Dir(config.DBPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open database with pragmas for performance and concurrency
	dsn := config.DBPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-8000)" + // 8MB cache
		"&_pragma=temp_store(MEMORY)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Execute schema
	if _, err := db.Exec(searchIndexSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	si := &SQLiteSearchIndex{
		config:    config,
		db:        db,
		batchChan: make(chan indexEntry, config.ChannelBuffer),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
		flushCh:   make(chan chan struct{}),
	}

	// Start background batch indexer
	go si.batchIndexer()

	return si, nil
}

// batchIndexer runs in a background goroutine, batching entries and flushing periodically.
func (si *SQLiteSearchIndex) batchIndexer() {
	defer close(si.doneCh)

	batch := make([]indexEntry, 0, si.config.BatchSize)
	timer := time.NewTimer(si.config.BatchTimeout)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		si.flushBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-si.batchChan:
			batch = append(batch, entry)
			if len(batch) >= si.config.BatchSize {
				flush()
				timer.Reset(si.config.BatchTimeout)
			}

		case <-timer.C:
			flush()
			timer.Reset(si.config.BatchTimeout)

		case done := <-si.flushCh:
			// Manual flush request - drain channel first
			draining := true
			for draining {
				select {
				case entry := <-si.batchChan:
					batch = append(batch, entry)
				default:
					draining = false
				}
			}
			flush()
			close(done)

		case <-si.stopCh:
			// Drain channel and flush before exit
			for {
				select {
				case entry := <-si.batchChan:
					batch = append(batch, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

// flushBatch writes a batch of entries to the database in a single transaction.
func (si *SQLiteSearchIndex) flushBatch(batch []indexEntry) {
	if len(batch) == 0 {
		return
	}

	si.mu.Lock()
	defer si.mu.Unlock()

	tx, err := si.db.Begin()
	if err != nil {
		log.Printf("[SEARCH_INDEX] Failed to begin transaction: %v", err)
		return
	}

	stmt, err := tx.Prepare("INSERT OR REPLACE INTO lines (id, timestamp, is_command, content) VALUES (?, ?, ?, ?)")
	if err != nil {
		log.Printf("[SEARCH_INDEX] Failed to prepare statement: %v", err)
		tx.Rollback()
		return
	}
	defer stmt.Close()

	for _, e := range batch {
		isCmd := 0
		if e.isCommand {
			isCmd = 1
		}
		if _, err := stmt.Exec(e.lineIdx, e.timestamp.UnixNano(), isCmd, e.text); err != nil {
			log.Printf("[SEARCH_INDEX] Failed to insert line %d: %v", e.lineIdx, err)
			tx.Rollback()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[SEARCH_INDEX] Failed to commit batch: %v", err)
	}
}

// IndexLine indexes a line. Commands are indexed immediately, output is batched.
func (si *SQLiteSearchIndex) IndexLine(lineIdx int64, timestamp time.Time, text string, isCommand bool) error {
	// Skip empty text
	if text == "" {
		return nil
	}

	entry := indexEntry{
		lineIdx:   lineIdx,
		timestamp: timestamp,
		text:      text,
		isCommand: isCommand,
	}

	if isCommand {
		// Commands indexed immediately for instant searchability
		return si.indexSync(entry)
	}

	// Output queued for batch indexing
	select {
	case si.batchChan <- entry:
		return nil
	default:
		// Channel full - try non-blocking, otherwise drop
		// In production, this would be logged as a warning
		return nil
	}
}

// indexSync writes a single entry synchronously.
func (si *SQLiteSearchIndex) indexSync(entry indexEntry) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	isCmd := 0
	if entry.isCommand {
		isCmd = 1
	}

	_, err := si.db.Exec(
		"INSERT OR REPLACE INTO lines (id, timestamp, is_command, content) VALUES (?, ?, ?, ?)",
		entry.lineIdx, entry.timestamp.UnixNano(), isCmd, entry.text,
	)
	return err
}

// Search executes an FTS5 search query.
func (si *SQLiteSearchIndex) Search(query string, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	si.mu.RLock()
	defer si.mu.RUnlock()

	// FTS5 query with BM25 ranking, commands prioritized
	rows, err := si.db.Query(`
		SELECT l.id, l.timestamp, l.content, l.is_command
		FROM lines_fts
		JOIN lines l ON l.id = lines_fts.rowid
		WHERE lines_fts MATCH ?
		ORDER BY l.is_command DESC, bm25(lines_fts)
		LIMIT ?
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer rows.Close()

	return si.scanResults(rows)
}

// SearchInRange searches within a time range.
func (si *SQLiteSearchIndex) SearchInRange(query string, start, end time.Time, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	si.mu.RLock()
	defer si.mu.RUnlock()

	rows, err := si.db.Query(`
		SELECT l.id, l.timestamp, l.content, l.is_command
		FROM lines_fts
		JOIN lines l ON l.id = lines_fts.rowid
		WHERE lines_fts MATCH ? AND l.timestamp >= ? AND l.timestamp <= ?
		ORDER BY l.is_command DESC, bm25(lines_fts)
		LIMIT ?
	`, query, start.UnixNano(), end.UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer rows.Close()

	return si.scanResults(rows)
}

// scanResults parses query results into SearchResult structs.
func (si *SQLiteSearchIndex) scanResults(rows *sql.Rows) ([]SearchResult, error) {
	var results []SearchResult

	for rows.Next() {
		var r SearchResult
		var tsNano int64
		var isCmd int

		if err := rows.Scan(&r.GlobalLineIdx, &tsNano, &r.Content, &isCmd); err != nil {
			continue // Skip malformed rows
		}

		r.Timestamp = time.Unix(0, tsNano)
		r.IsCommand = isCmd == 1
		results = append(results, r)
	}

	return results, rows.Err()
}

// FindLineAt returns the global line index closest to the given time.
func (si *SQLiteSearchIndex) FindLineAt(t time.Time) (int64, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()

	var lineIdx int64
	err := si.db.QueryRow(
		"SELECT id FROM lines WHERE timestamp <= ? ORDER BY timestamp DESC LIMIT 1",
		t.UnixNano(),
	).Scan(&lineIdx)

	if err == sql.ErrNoRows {
		// No line before this time, try to get the first line
		err = si.db.QueryRow(
			"SELECT id FROM lines ORDER BY timestamp ASC LIMIT 1",
		).Scan(&lineIdx)
	}

	if err == sql.ErrNoRows {
		return -1, nil // Empty index
	}

	return lineIdx, err
}

// GetTimestamp returns the timestamp for a line by global index.
func (si *SQLiteSearchIndex) GetTimestamp(lineIdx int64) (time.Time, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()

	var tsNano int64
	err := si.db.QueryRow(
		"SELECT timestamp FROM lines WHERE id = ?",
		lineIdx,
	).Scan(&tsNano)

	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(0, tsNano), nil
}

// Flush blocks until all pending entries are indexed.
func (si *SQLiteSearchIndex) Flush() error {
	done := make(chan struct{})
	select {
	case si.flushCh <- done:
		<-done
	case <-si.stopCh:
		// Already stopped
	}
	return nil
}

// Close flushes pending writes and closes the database.
func (si *SQLiteSearchIndex) Close() error {
	// Signal stop and wait for background goroutine
	close(si.stopCh)
	<-si.doneCh

	return si.db.Close()
}

// Compile-time interface check
var _ SearchIndex = (*SQLiteSearchIndex)(nil)
