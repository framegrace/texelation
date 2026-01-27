// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/rate_monitor.go
// Summary: RateMonitor tracks write rate using a ring buffer of timestamps.
//
// Architecture:
//
//	RateMonitor uses a fixed-size ring buffer to store timestamps of recent
//	write operations. It calculates the write rate using a sliding window
//	approach, counting writes that occurred within the last N duration.
//
//	Thread-safety is NOT provided; the caller (AdaptivePersistence) must
//	synchronize access.

package parser

import "time"

// RateMonitor tracks write rate using a ring buffer of timestamps.
// Not thread-safe; caller must synchronize.
type RateMonitor struct {
	timestamps []time.Time // Ring buffer of write timestamps
	head       int         // Next write position in ring buffer
	size       int         // Current number of entries (0 to windowSize)
	windowSize int         // Maximum entries to track
}

// NewRateMonitor creates a rate monitor with the specified window size.
// If windowSize <= 0, defaults to 1000.
func NewRateMonitor(windowSize int) *RateMonitor {
	if windowSize <= 0 {
		windowSize = 1000
	}
	return &RateMonitor{
		timestamps: make([]time.Time, windowSize),
		head:       0,
		size:       0,
		windowSize: windowSize,
	}
}

// RecordWrite adds a timestamp to the ring buffer.
func (rm *RateMonitor) RecordWrite(t time.Time) {
	rm.timestamps[rm.head] = t
	rm.head = (rm.head + 1) % rm.windowSize
	if rm.size < rm.windowSize {
		rm.size++
	}
}

// CalculateRate returns writes per second within the given time window.
// The window parameter defines how far back in time to count writes.
// Returns 0 if no timestamps have been recorded or window is invalid.
func (rm *RateMonitor) CalculateRate(now time.Time, window time.Duration) float64 {
	if rm.size == 0 || window <= 0 {
		return 0
	}

	cutoff := now.Add(-window)
	count := 0

	// Iterate backwards in logical time order (most recent first)
	for i := 0; i < rm.size; i++ {
		idx := (rm.head - 1 - i + rm.windowSize) % rm.windowSize
		if rm.timestamps[idx].After(cutoff) {
			count++
		} else {
			// Walking backwards in time, can stop when outside window
			break
		}
	}

	// Convert count to rate (writes per second)
	return float64(count) / window.Seconds()
}

// Reset clears all recorded timestamps.
func (rm *RateMonitor) Reset() {
	rm.head = 0
	rm.size = 0
}

// Size returns the number of timestamps currently recorded.
func (rm *RateMonitor) Size() int {
	return rm.size
}
