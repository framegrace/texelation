// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/viewport_cache.go
// Summary: ViewportCache provides caching for rendered viewport content.
//
// Architecture:
//
//	ViewportCache caches physical line renderings for a viewport.
//	It invalidates automatically when content or dimensions change.
//
//	Cache key consists of: (startGlobalIdx, endGlobalIdx, width, contentVersion)
//	Any change to these values invalidates the cache.
//
//	This component is used by ViewportWindow to avoid rebuilding
//	physical lines on every GetVisibleGrid() call.

package parser

import "sync/atomic"

// CacheEntry represents a cached viewport rendering.
type CacheEntry struct {
	// PhysicalLines for this viewport window
	PhysicalLines []PhysicalLine

	// StartGlobalIdx is the first logical line included
	StartGlobalIdx int64

	// EndGlobalIdx is one past the last logical line included
	EndGlobalIdx int64

	// ContentVersion is the version this cache was built from
	ContentVersion int64

	// Width is the display width this cache was built for
	Width int
}

// ViewportCache caches physical line renderings for a viewport.
// Invalidates automatically when content or dimensions change.
// Thread-safety must be managed by the caller (ViewportWindow).
type ViewportCache struct {
	// Current cached entry (nil if invalid)
	entry *CacheEntry

	// Dependencies
	reader  ContentReader
	builder *PhysicalLineBuilder

	// Statistics (useful for debugging/profiling)
	// Use atomic operations for thread-safe access
	hits   atomic.Int64
	misses atomic.Int64
}

// NewViewportCache creates a new cache with the given dependencies.
func NewViewportCache(reader ContentReader, builder *PhysicalLineBuilder) *ViewportCache {
	return &ViewportCache{
		reader:  reader,
		builder: builder,
	}
}

// Get returns cached physical lines for the given logical line range.
// Returns nil if cache is invalid (miss).
// Note: This method is a pure read when called with a read lock (does not modify entry).
// Statistics counters use atomic operations for thread-safety.
func (c *ViewportCache) Get(startGlobalIdx, endGlobalIdx int64, width int) []PhysicalLine {
	entry := c.entry
	if entry == nil {
		c.misses.Add(1)
		return nil
	}

	// Check if cache is still valid
	if entry.StartGlobalIdx != startGlobalIdx ||
		entry.EndGlobalIdx != endGlobalIdx ||
		entry.Width != width ||
		entry.ContentVersion != c.reader.ContentVersion() {
		c.misses.Add(1)
		// Don't invalidate here - let the caller rebuild and Set() will replace
		return nil
	}

	c.hits.Add(1)
	return entry.PhysicalLines
}

// Set caches physical lines for the given logical line range.
func (c *ViewportCache) Set(startGlobalIdx, endGlobalIdx int64, width int, physical []PhysicalLine) {
	c.entry = &CacheEntry{
		PhysicalLines:  physical,
		StartGlobalIdx: startGlobalIdx,
		EndGlobalIdx:   endGlobalIdx,
		ContentVersion: c.reader.ContentVersion(),
		Width:          width,
	}
}

// Invalidate clears the cache.
func (c *ViewportCache) Invalidate() {
	c.entry = nil
}

// Stats returns cache hit/miss statistics.
func (c *ViewportCache) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// ResetStats clears the hit/miss counters.
func (c *ViewportCache) ResetStats() {
	c.hits.Store(0)
	c.misses.Store(0)
}

// IsValid returns whether the cache contains a valid entry for the given parameters.
func (c *ViewportCache) IsValid(startGlobalIdx, endGlobalIdx int64, width int) bool {
	if c.entry == nil {
		return false
	}
	return c.entry.StartGlobalIdx == startGlobalIdx &&
		c.entry.EndGlobalIdx == endGlobalIdx &&
		c.entry.Width == width &&
		c.entry.ContentVersion == c.reader.ContentVersion()
}
