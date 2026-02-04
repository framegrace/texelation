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
	"strings"
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

	// DeleteLine removes a line from the index.
	// Called when a line is erased/cleared to prevent stale matches.
	DeleteLine(lineIdx int64) error

	// Search executes a substring search query using trigram matching.
	// Any substring of the indexed content can be matched (e.g., "ls -ls", "docker").
	// Returns up to limit results ordered by timestamp (newest first).
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

// Current schema version - increment this when schema changes require reindexing
const searchIndexSchemaVersion = 2

// SQLite schema for the search index
const searchIndexSchema = `
-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY
);

-- Main content table
CREATE TABLE IF NOT EXISTS lines (
    id INTEGER PRIMARY KEY,           -- Global line index
    timestamp INTEGER NOT NULL,       -- UnixNano
    is_command INTEGER DEFAULT 0,     -- 1 if OSC 133 command
    content TEXT NOT NULL
);

-- Index for time-based navigation
CREATE INDEX IF NOT EXISTS idx_lines_timestamp ON lines(timestamp);

-- Index for command filtering
CREATE INDEX IF NOT EXISTS idx_lines_command ON lines(is_command) WHERE is_command = 1;
`

// FTS schema - separate so we can rebuild it on version changes
const searchIndexFTSSchema = `
-- FTS5 virtual table for full-text search with trigram tokenizer
-- Trigram enables substring matching (e.g., "ls -ls", partial paths)
CREATE VIRTUAL TABLE IF NOT EXISTS lines_fts USING fts5(
    content,
    content='lines',
    content_rowid='id',
    tokenize='trigram'
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

	// Execute base schema (tables and indexes, not FTS)
	if _, err := db.Exec(searchIndexSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	// Check schema version and migrate if needed
	needsReindex, err := checkAndMigrateSchema(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to check schema version: %w", err)
	}

	// Create FTS schema
	if _, err := db.Exec(searchIndexFTSSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create FTS schema: %w", err)
	}

	// Rebuild FTS index if schema version changed
	if needsReindex {
		log.Printf("[SEARCH_INDEX] Schema version changed, rebuilding FTS index...")
		if err := rebuildFTSIndex(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to rebuild FTS index: %w", err)
		}
		log.Printf("[SEARCH_INDEX] FTS index rebuild complete")
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

// checkAndMigrateSchema checks the current schema version and prepares for migration if needed.
// Returns true if reindexing is needed.
func checkAndMigrateSchema(db *sql.DB) (bool, error) {
	// Get current version (0 if not set)
	var currentVersion int
	err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&currentVersion)
	if err == sql.ErrNoRows {
		currentVersion = 0
	} else if err != nil {
		// Table might not exist yet, treat as version 0
		currentVersion = 0
	}

	if currentVersion == searchIndexSchemaVersion {
		return false, nil // No migration needed
	}

	log.Printf("[SEARCH_INDEX] Migrating schema from version %d to %d", currentVersion, searchIndexSchemaVersion)

	// Drop existing FTS table and triggers to rebuild with new schema
	migrations := []string{
		"DROP TRIGGER IF EXISTS lines_ai",
		"DROP TRIGGER IF EXISTS lines_au",
		"DROP TRIGGER IF EXISTS lines_ad",
		"DROP TABLE IF EXISTS lines_fts",
	}

	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			return false, fmt.Errorf("migration failed on '%s': %w", stmt, err)
		}
	}

	// Update schema version
	_, err = db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", searchIndexSchemaVersion)
	if err != nil {
		return false, fmt.Errorf("failed to update schema version: %w", err)
	}

	return true, nil // Reindexing needed
}

// rebuildFTSIndex rebuilds the FTS index from existing data in the lines table.
func rebuildFTSIndex(db *sql.DB) error {
	// Count lines for progress logging
	var count int64
	db.QueryRow("SELECT COUNT(*) FROM lines").Scan(&count)
	log.Printf("[SEARCH_INDEX] Rebuilding index for %d lines...", count)

	// Populate FTS from existing data
	_, err := db.Exec("INSERT INTO lines_fts(rowid, content) SELECT id, content FROM lines")
	if err != nil {
		return fmt.Errorf("failed to populate FTS index: %w", err)
	}

	return nil
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

// DeleteLine removes a line from the search index.
// This is called when a line is erased to prevent stale search matches.
func (si *SQLiteSearchIndex) DeleteLine(lineIdx int64) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	_, err := si.db.Exec("DELETE FROM lines WHERE id = ?", lineIdx)
	return err
}

// Search executes a search query.
// Results are ordered by time (newest first) for intuitive history navigation.
// Next goes to older results, Prev goes to newer results.
// For queries shorter than 3 characters, uses LIKE since trigram tokenizer needs at least 3 chars.
func (si *SQLiteSearchIndex) Search(query string, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	si.mu.RLock()
	defer si.mu.RUnlock()

	var rows *sql.Rows
	var err error

	// Trigram tokenizer requires at least 3 characters to produce a trigram.
	// For shorter queries, fall back to LIKE which works for any length.
	if len(query) < 3 {
		// Use LIKE for short queries (case-insensitive via LOWER)
		likePattern := "%" + strings.ReplaceAll(strings.ReplaceAll(query, "%", "\\%"), "_", "\\_") + "%"
		rows, err = si.db.Query(`
			SELECT id, timestamp, content, is_command
			FROM lines
			WHERE content LIKE ? ESCAPE '\'
			ORDER BY timestamp DESC
			LIMIT ?
		`, likePattern, limit)
	} else {
		// With trigram tokenizer, wrap query in double quotes for literal substring matching.
		// This allows searching for patterns like "ls -ls" that contain special characters.
		quotedQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`

		// FTS5 query ordered by timestamp (newest first) for history navigation
		rows, err = si.db.Query(`
			SELECT l.id, l.timestamp, l.content, l.is_command
			FROM lines_fts
			JOIN lines l ON l.id = lines_fts.rowid
			WHERE lines_fts MATCH ?
			ORDER BY l.timestamp DESC
			LIMIT ?
		`, quotedQuery, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer rows.Close()

	return si.scanResults(rows)
}

// SearchInRange searches within a time range.
// For queries shorter than 3 characters, uses LIKE since trigram tokenizer needs at least 3 chars.
func (si *SQLiteSearchIndex) SearchInRange(query string, start, end time.Time, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	si.mu.RLock()
	defer si.mu.RUnlock()

	var rows *sql.Rows
	var err error

	// Trigram tokenizer requires at least 3 characters to produce a trigram.
	// For shorter queries, fall back to LIKE which works for any length.
	if len(query) < 3 {
		// Use LIKE for short queries (case-insensitive via LOWER)
		likePattern := "%" + strings.ReplaceAll(strings.ReplaceAll(query, "%", "\\%"), "_", "\\_") + "%"
		rows, err = si.db.Query(`
			SELECT id, timestamp, content, is_command
			FROM lines
			WHERE content LIKE ? ESCAPE '\' AND timestamp >= ? AND timestamp <= ?
			ORDER BY timestamp DESC
			LIMIT ?
		`, likePattern, start.UnixNano(), end.UnixNano(), limit)
	} else {
		// With trigram tokenizer, wrap query in double quotes for literal substring matching.
		quotedQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`

		rows, err = si.db.Query(`
			SELECT l.id, l.timestamp, l.content, l.is_command
			FROM lines_fts
			JOIN lines l ON l.id = lines_fts.rowid
			WHERE lines_fts MATCH ? AND l.timestamp >= ? AND l.timestamp <= ?
			ORDER BY l.timestamp DESC
			LIMIT ?
		`, quotedQuery, start.UnixNano(), end.UnixNano(), limit)
	}

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

// RebuildSearchIndex forces a rebuild of the FTS index for an existing database.
// This can be used from command line tools for manual reindexing.
func RebuildSearchIndex(dbPath string) error {
	// Open database
	dsn := dbPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-8000)" +
		"&_pragma=temp_store(MEMORY)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Check if lines table exists
	var tableExists int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='lines'").Scan(&tableExists)
	if err != nil || tableExists == 0 {
		return fmt.Errorf("no existing search index found at %s", dbPath)
	}

	log.Printf("[SEARCH_INDEX] Dropping existing FTS index...")

	// Drop existing FTS table and triggers
	drops := []string{
		"DROP TRIGGER IF EXISTS lines_ai",
		"DROP TRIGGER IF EXISTS lines_au",
		"DROP TRIGGER IF EXISTS lines_ad",
		"DROP TABLE IF EXISTS lines_fts",
	}
	for _, stmt := range drops {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to drop FTS: %w", err)
		}
	}

	// Recreate FTS schema
	log.Printf("[SEARCH_INDEX] Creating new FTS index with trigram tokenizer...")
	if _, err := db.Exec(searchIndexFTSSchema); err != nil {
		return fmt.Errorf("failed to create FTS schema: %w", err)
	}

	// Rebuild index
	if err := rebuildFTSIndex(db); err != nil {
		return err
	}

	// Update schema version
	if _, err := db.Exec("INSERT OR REPLACE INTO schema_version (version) VALUES (?)", searchIndexSchemaVersion); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	log.Printf("[SEARCH_INDEX] Reindex complete")
	return nil
}

// Compile-time interface check
var _ SearchIndex = (*SQLiteSearchIndex)(nil)
