// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelterm/parser/mode_controller.go
// Summary: ModeController determines persistence mode based on write rate.
//
// Architecture:
//
//	ModeController encapsulates the logic for selecting persistence mode
//	and calculating debounce delays. It uses configurable rate thresholds
//	to switch between WriteThrough, Debounced, and BestEffort modes.
//
//	The debounce delay is calculated using linear interpolation between
//	configured min and max delays based on the current write rate.

package parser

import "time"

// ModeController determines persistence mode based on write rate.
// It also calculates adaptive debounce delays.
type ModeController struct {
	writeThroughMaxRate float64 // Below this: WriteThrough mode
	debouncedMaxRate    float64 // Below this: Debounced mode; above: BestEffort
}

// NewModeController creates a mode controller with the given rate thresholds.
// writeThroughMax: writes/sec below which WriteThrough mode is used (default: 10)
// debouncedMax: writes/sec below which Debounced mode is used (default: 100)
func NewModeController(writeThroughMax, debouncedMax float64) *ModeController {
	if writeThroughMax <= 0 {
		writeThroughMax = 10
	}
	if debouncedMax <= writeThroughMax {
		debouncedMax = writeThroughMax * 10
	}
	return &ModeController{
		writeThroughMaxRate: writeThroughMax,
		debouncedMaxRate:    debouncedMax,
	}
}

// DetermineMode returns the appropriate persistence mode for the given write rate.
func (mc *ModeController) DetermineMode(writeRate float64) PersistMode {
	switch {
	case writeRate < mc.writeThroughMaxRate:
		return PersistWriteThrough
	case writeRate < mc.debouncedMaxRate:
		return PersistDebounced
	default:
		return PersistBestEffort
	}
}

// CalculateDebounceDelay returns the delay for debounced mode based on write rate.
// The delay scales linearly from minDelay to maxDelay as rate increases from
// writeThroughMaxRate to debouncedMaxRate.
//
// This implements adaptive debouncing: faster writes get longer delays to
// reduce disk I/O overhead during high-throughput scenarios.
func (mc *ModeController) CalculateDebounceDelay(writeRate float64, minDelay, maxDelay time.Duration) time.Duration {
	// Below write-through threshold, use minimum delay
	if writeRate < mc.writeThroughMaxRate {
		return minDelay
	}

	// Calculate ratio within debounced range
	rateRange := mc.debouncedMaxRate - mc.writeThroughMaxRate
	if rateRange <= 0 {
		return minDelay
	}

	ratio := (writeRate - mc.writeThroughMaxRate) / rateRange
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}

	// Linear interpolation between min and max delay
	delayRange := maxDelay - minDelay
	delay := minDelay + time.Duration(float64(delayRange)*ratio)
	return delay
}

// WriteThroughMaxRate returns the configured threshold for WriteThrough mode.
func (mc *ModeController) WriteThroughMaxRate() float64 {
	return mc.writeThroughMaxRate
}

// DebouncedMaxRate returns the configured threshold for Debounced mode.
func (mc *ModeController) DebouncedMaxRate() float64 {
	return mc.debouncedMaxRate
}
