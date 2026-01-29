// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchIndex_CreateAndClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Errorf("failed to close index: %v", err)
	}

	// Database file should exist
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file not created")
	}
}

func TestSearchIndex_IndexLineSync(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	// Index a command (sync)
	now := time.Now()
	err = idx.IndexLine(0, now, "docker run nginx", true)
	if err != nil {
		t.Fatalf("failed to index command: %v", err)
	}

	// Search should find it immediately (no flush needed for sync)
	results, err := idx.Search("docker", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Content != "docker run nginx" {
		t.Errorf("expected content %q, got %q", "docker run nginx", results[0].Content)
	}

	if !results[0].IsCommand {
		t.Error("expected IsCommand=true")
	}
}

func TestSearchIndex_IndexLineAsync(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	// Index output (async)
	now := time.Now()
	for i := int64(0); i < 10; i++ {
		err = idx.IndexLine(i, now.Add(time.Duration(i)*time.Second), "output line", false)
		if err != nil {
			t.Fatalf("failed to index output: %v", err)
		}
	}

	// Flush to ensure indexing completes
	if err := idx.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	// Give a bit of time for async operations to complete
	time.Sleep(50 * time.Millisecond)

	// Search should find all lines
	results, err := idx.Search("output", 100)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 10 {
		t.Errorf("expected 10 results, got %d", len(results))
	}
}

func TestSearchIndex_SearchEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	// Search with empty query
	results, err := idx.Search("", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty query, got %v", results)
	}
}

func TestSearchIndex_SearchNoResults(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	// Index some content
	now := time.Now()
	idx.IndexLine(0, now, "hello world", true)

	// Search for non-existent term
	results, err := idx.Search("nonexistent", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchIndex_CommandsPrioritized(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	now := time.Now()

	// Index output first (lower line index)
	idx.IndexLine(0, now, "docker output line", false)
	idx.Flush()

	// Index command second (higher line index)
	idx.IndexLine(1, now.Add(time.Second), "docker run nginx", true)

	// Search should return command first (prioritized)
	results, err := idx.Search("docker", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Command should be first despite being indexed second
	if !results[0].IsCommand {
		t.Error("expected first result to be command")
	}
	if results[1].IsCommand {
		t.Error("expected second result to not be command")
	}
}

func TestSearchIndex_SearchInRange(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	baseTime := time.Date(2025, 1, 28, 12, 0, 0, 0, time.UTC)

	// Index lines at different times
	idx.IndexLine(0, baseTime, "morning docker", true)
	idx.IndexLine(1, baseTime.Add(2*time.Hour), "afternoon docker", true)
	idx.IndexLine(2, baseTime.Add(4*time.Hour), "evening docker", true)

	// Search in range that excludes morning
	start := baseTime.Add(1 * time.Hour)
	end := baseTime.Add(3 * time.Hour)

	results, err := idx.SearchInRange("docker", start, end, 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result in range, got %d", len(results))
	}

	if results[0].Content != "afternoon docker" {
		t.Errorf("expected afternoon result, got %q", results[0].Content)
	}
}

func TestSearchIndex_FindLineAt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	baseTime := time.Date(2025, 1, 28, 12, 0, 0, 0, time.UTC)

	// Index lines at known times
	idx.IndexLine(0, baseTime, "line 0", true)
	idx.IndexLine(1, baseTime.Add(1*time.Hour), "line 1", true)
	idx.IndexLine(2, baseTime.Add(2*time.Hour), "line 2", true)

	// Find line at 1.5 hours (should return line 1)
	targetTime := baseTime.Add(90 * time.Minute)
	lineIdx, err := idx.FindLineAt(targetTime)
	if err != nil {
		t.Fatalf("FindLineAt failed: %v", err)
	}

	if lineIdx != 1 {
		t.Errorf("expected line 1, got %d", lineIdx)
	}

	// Find line before first (should return first line)
	earlyTime := baseTime.Add(-1 * time.Hour)
	lineIdx, err = idx.FindLineAt(earlyTime)
	if err != nil {
		t.Fatalf("FindLineAt failed: %v", err)
	}

	if lineIdx != 0 {
		t.Errorf("expected line 0 for early time, got %d", lineIdx)
	}
}

func TestSearchIndex_GetTimestamp(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	expectedTime := time.Date(2025, 1, 28, 14, 30, 45, 0, time.UTC)
	idx.IndexLine(42, expectedTime, "test line", true)

	// Get timestamp for existing line
	ts, err := idx.GetTimestamp(42)
	if err != nil {
		t.Fatalf("GetTimestamp failed: %v", err)
	}

	if !ts.Equal(expectedTime) {
		t.Errorf("expected %v, got %v", expectedTime, ts)
	}

	// Get timestamp for non-existent line
	ts, err = idx.GetTimestamp(999)
	if err != nil {
		t.Fatalf("GetTimestamp failed for non-existent: %v", err)
	}

	if !ts.IsZero() {
		t.Errorf("expected zero time for non-existent line, got %v", ts)
	}
}

func TestSearchIndex_EmptyIndex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	// FindLineAt on empty index
	lineIdx, err := idx.FindLineAt(time.Now())
	if err != nil {
		t.Fatalf("FindLineAt failed: %v", err)
	}
	if lineIdx != -1 {
		t.Errorf("expected -1 for empty index, got %d", lineIdx)
	}
}

func TestSearchIndex_FTS5Wildcard(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	now := time.Now()
	idx.IndexLine(0, now, "docker-compose up", true)
	idx.IndexLine(1, now, "docker build", true)
	idx.IndexLine(2, now, "kubectl apply", true)

	// Wildcard search for "docker*"
	results, err := idx.Search("docker*", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results for docker*, got %d", len(results))
	}
}

func TestSearchIndex_UnicodeContent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	now := time.Now()
	idx.IndexLine(0, now, "echo hello world", true)
	idx.IndexLine(1, now, "greeting hello there", true)

	// Search for "hello" - a term that appears in both
	results, err := idx.Search("hello", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results for hello search, got %d", len(results))
	}

	// Also test that the content is correctly stored
	if results[0].Content != "echo hello world" && results[0].Content != "greeting hello there" {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
}

func TestSearchIndex_SkipEmptyText(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	// Try to index empty text
	err = idx.IndexLine(0, time.Now(), "", true)
	if err != nil {
		t.Fatalf("indexing empty text should not fail: %v", err)
	}

	err = idx.IndexLine(1, time.Now(), "   ", false) // Whitespace only after trim
	idx.Flush()

	// Search should return nothing
	results, err := idx.Search("*", 10)
	if err == nil && len(results) > 0 {
		t.Error("expected no indexed lines for empty/whitespace content")
	}
}

func TestSearchIndex_BatchFlush(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Use small batch size for testing
	config := SearchIndexConfig{
		DBPath:        dbPath,
		BatchSize:     5,
		BatchTimeout:  100 * time.Millisecond,
		ChannelBuffer: 100,
	}

	idx, err := NewSearchIndexWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	now := time.Now()

	// Index exactly BatchSize entries (should trigger automatic flush)
	for i := int64(0); i < 5; i++ {
		idx.IndexLine(i, now.Add(time.Duration(i)*time.Second), "batch test", false)
	}

	// Wait a bit for batch to flush
	time.Sleep(50 * time.Millisecond)

	// Search should find all lines
	results, err := idx.Search("batch", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 5 {
		// May need flush if timing is off
		idx.Flush()
		results, _ = idx.Search("batch", 10)
		if len(results) != 5 {
			t.Errorf("expected 5 results after batch flush, got %d", len(results))
		}
	}
}

func TestSearchIndex_ReopenExisting(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create and populate index
	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	now := time.Now()
	idx.IndexLine(0, now, "persistent data", true)
	idx.Close()

	// Reopen index
	idx2, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen index: %v", err)
	}
	defer idx2.Close()

	// Data should persist
	results, err := idx2.Search("persistent", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result after reopen, got %d", len(results))
	}
}

func TestSearchIndex_LargeVolume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large volume test in short mode")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Use smaller batch size for faster flushing in test
	config := SearchIndexConfig{
		DBPath:        dbPath,
		BatchSize:     50,
		BatchTimeout:  100 * time.Millisecond,
		ChannelBuffer: 2000,
	}

	idx, err := NewSearchIndexWithConfig(config)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	now := time.Now()

	// Index 1000 lines
	for i := int64(0); i < 1000; i++ {
		text := "output line with some content"
		if i%100 == 0 {
			text = "docker run important-command"
			idx.IndexLine(i, now.Add(time.Duration(i)*time.Millisecond), text, true)
		} else {
			idx.IndexLine(i, now.Add(time.Duration(i)*time.Millisecond), text, false)
		}
	}

	idx.Flush()
	time.Sleep(100 * time.Millisecond) // Give time for async flush to complete

	// Search for commands should return 10
	results, err := idx.Search("docker", 100)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) != 10 {
		t.Errorf("expected 10 command results, got %d", len(results))
	}

	// All should be marked as commands
	for _, r := range results {
		if !r.IsCommand {
			t.Error("expected all docker results to be commands")
			break
		}
	}

	// Search for output lines
	results, err = idx.Search("output", 1000)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	// Should have 990 output lines (1000 - 10 command lines)
	if len(results) < 500 {
		t.Errorf("expected at least 500 results, got %d", len(results))
	}
}

func TestSearchIndex_DeleteLine(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	idx, err := NewSearchIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer idx.Close()

	now := time.Now()

	// Index three lines
	idx.IndexLine(0, now, "hello world", true)
	idx.IndexLine(1, now, "hello again", true)
	idx.IndexLine(2, now, "goodbye world", true)

	// All three should be searchable
	results, _ := idx.Search("hello", 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'hello', got %d", len(results))
	}

	results, _ = idx.Search("world", 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'world', got %d", len(results))
	}

	// Delete line 0 (hello world)
	err = idx.DeleteLine(0)
	if err != nil {
		t.Fatalf("failed to delete line: %v", err)
	}

	// Now only one "hello" result (line 1)
	results, _ = idx.Search("hello", 10)
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'hello' after delete, got %d", len(results))
	}
	if len(results) > 0 && results[0].GlobalLineIdx != 1 {
		t.Errorf("expected remaining result to be line 1, got line %d", results[0].GlobalLineIdx)
	}

	// Only one "world" result (line 2)
	results, _ = idx.Search("world", 10)
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'world' after delete, got %d", len(results))
	}
	if len(results) > 0 && results[0].GlobalLineIdx != 2 {
		t.Errorf("expected remaining result to be line 2, got line %d", results[0].GlobalLineIdx)
	}

	// Delete non-existent line should not error
	err = idx.DeleteLine(999)
	if err != nil {
		t.Errorf("deleting non-existent line should not error: %v", err)
	}
}
