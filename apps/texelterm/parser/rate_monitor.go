// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/rate_monitor.go
// Summary: RateMonitor tracks write rate using counter-based sampling.
//
// Architecture:
//
//	RateMonitor uses a simple counter and periodic sampling to calculate
//	write rate. RecordWrite() increments a counter (O(1), no timestamp).
//	CalculateRate() computes the rate from the counter delta since the
//	last sample.
//
//	This is much cheaper than the previous ring-buffer approach, which
//	required time.Now() on every write and O(n) scans on rate calculation.
//
//	Thread-safety is NOT provided; the caller (AdaptivePersistence) must
//	synchronize access.

package parser

import "time"

// RateMonitor tracks write rate using counter-based sampling.
// Not thread-safe; caller must synchronize.
type RateMonitor struct {
	count     int64     // Total writes since creation/reset
	lastCount int64     // Count at last rate sample
	lastTime  time.Time // Time at last rate sample
	lastRate  float64   // Cached rate from last sample
}

// NewRateMonitor creates a rate monitor.
// The windowSize parameter is accepted for API compatibility but ignored;
// rate is computed from counter deltas between samples.
func NewRateMonitor(windowSize int) *RateMonitor {
	return &RateMonitor{}
}

// RecordWrite records a write event. O(1), no timestamp needed.
func (rm *RateMonitor) RecordWrite() {
	rm.count++
}

// CalculateRate returns writes per second since the last sample.
// On the first call, establishes the baseline and returns 0.
func (rm *RateMonitor) CalculateRate(now time.Time) float64 {
	if rm.lastTime.IsZero() {
		rm.lastTime = now
		rm.lastCount = rm.count
		return 0
	}

	elapsed := now.Sub(rm.lastTime).Seconds()
	if elapsed <= 0 {
		return rm.lastRate
	}

	delta := rm.count - rm.lastCount
	rate := float64(delta) / elapsed

	rm.lastCount = rm.count
	rm.lastTime = now
	rm.lastRate = rate
	return rate
}

// Reset clears all state.
func (rm *RateMonitor) Reset() {
	rm.count = 0
	rm.lastCount = 0
	rm.lastTime = time.Time{}
	rm.lastRate = 0
}

// Size returns the total number of writes recorded since creation/reset.
func (rm *RateMonitor) Size() int {
	return int(rm.count)
}
