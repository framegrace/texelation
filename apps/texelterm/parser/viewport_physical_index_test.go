// Copyright 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser

import (
	"fmt"
	"testing"
)

func TestPhysicalLineIndex_BasicCount(t *testing.T) {
	mb := setupTestBuffer([]string{"Hello", "World", "Test"}, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// 3 short lines at width 80 = 3 physical lines
	if got := idx.TotalPhysicalLines(); got != 3 {
		t.Errorf("expected 3 total physical lines, got %d", got)
	}
	if got := idx.Count(); got != 3 {
		t.Errorf("expected 3 tracked lines, got %d", got)
	}
}

func TestPhysicalLineIndex_LongLineWrapping(t *testing.T) {
	// 200-char line at width 80 = ceil(200/80) = 3 physical lines
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'A'
	}
	mb := setupTestBuffer([]string{string(long), "Short"}, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// Line 0: 200 chars / 80 = 3 physical lines
	// Line 1: "Short" = 1 physical line
	if got := idx.TotalPhysicalLines(); got != 4 {
		t.Errorf("expected 4 total physical lines, got %d", got)
	}
	if got := idx.PhysicalCountFor(0); got != 3 {
		t.Errorf("expected 3 physical lines for long line, got %d", got)
	}
	if got := idx.PhysicalCountFor(1); got != 1 {
		t.Errorf("expected 1 physical line for short line, got %d", got)
	}
}

func TestPhysicalLineIndex_FixedWidthLine(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Write a line with lots of content
	for range 200 {
		mb.Write('X', DefaultFG, DefaultBG, 0)
	}
	mb.SetLineFixed(0, 80) // Mark as fixed width

	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 40) // Narrower viewport

	idx.Build()

	// Fixed-width lines always produce 1 physical line regardless of content
	if got := idx.TotalPhysicalLines(); got != 1 {
		t.Errorf("expected 1 physical line for fixed-width, got %d", got)
	}
}

func TestPhysicalLineIndex_EmptyLine(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	mb.SetTermWidth(80)

	// Create an empty line by writing newline
	mb.NewLine()
	mb.CarriageReturn()
	mb.Write('A', DefaultFG, DefaultBG, 0)

	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// Empty line = 1 physical line, "A" line = 1 physical line
	if got := idx.TotalPhysicalLines(); got != 2 {
		t.Errorf("expected 2 total physical lines, got %d", got)
	}
}

func TestPhysicalLineIndex_WidthChange(t *testing.T) {
	long := make([]byte, 160)
	for i := range long {
		long[i] = 'B'
	}
	mb := setupTestBuffer([]string{string(long)}, 80)
	reader := NewMemoryBufferReader(mb)

	// At width 80: ceil(160/80) = 2
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()
	if got := idx.TotalPhysicalLines(); got != 2 {
		t.Errorf("expected 2 at width 80, got %d", got)
	}

	// Simulate width change: invalidate and rebuild at width 40
	idx.Invalidate()
	idx.width = 40
	idx.Build()
	// At width 40: ceil(160/40) = 4
	if got := idx.TotalPhysicalLines(); got != 4 {
		t.Errorf("expected 4 at width 40, got %d", got)
	}
}

func TestPhysicalLineIndex_Eviction(t *testing.T) {
	// Create buffer with 10 lines, evict first 3
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("Line%d", i)
	}
	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)

	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	if got := idx.TotalPhysicalLines(); got != 10 {
		t.Errorf("expected 10 before eviction, got %d", got)
	}

	// Simulate eviction of 3 lines
	oldBase := idx.BaseOffset()
	idx.HandleEviction(oldBase+3, 3)

	if got := idx.TotalPhysicalLines(); got != 7 {
		t.Errorf("expected 7 after eviction, got %d", got)
	}
	if got := idx.Count(); got != 7 {
		t.Errorf("expected count 7 after eviction, got %d", got)
	}
	if got := idx.BaseOffset(); got != oldBase+3 {
		t.Errorf("expected baseOffset %d, got %d", oldBase+3, got)
	}
}

func TestPhysicalLineIndex_EvictionAll(t *testing.T) {
	mb := setupTestBuffer([]string{"A", "B", "C"}, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// Evict more than we have — should invalidate
	idx.HandleEviction(100, 10)
	if idx.ContentVersion() != -1 {
		t.Error("expected invalidation after evicting all lines")
	}
}

func TestPhysicalLineIndex_Append(t *testing.T) {
	mb := setupTestBuffer([]string{"Hello", "World"}, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	if got := idx.TotalPhysicalLines(); got != 2 {
		t.Errorf("expected 2 before append, got %d", got)
	}

	// Append more content to the buffer
	mb.NewLine()
	mb.CarriageReturn()
	for _, r := range "NewLine" {
		mb.Write(r, DefaultFG, DefaultBG, 0)
	}

	// HandleAppend should pick up the new line
	idx.HandleAppend(reader.GlobalEnd())

	if got := idx.TotalPhysicalLines(); got != 3 {
		t.Errorf("expected 3 after append, got %d", got)
	}
	if got := idx.Count(); got != 3 {
		t.Errorf("expected count 3 after append, got %d", got)
	}
}

func TestPhysicalLineIndex_PhysicalToLogical(t *testing.T) {
	// 3 lines: "Short" (1 phys), 160-char (2 phys at w=80), "End" (1 phys)
	long := make([]byte, 160)
	for i := range long {
		long[i] = 'X'
	}
	mb := setupTestBuffer([]string{"Short", string(long), "End"}, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// Total: 1 + 2 + 1 = 4 physical lines
	if got := idx.TotalPhysicalLines(); got != 4 {
		t.Fatalf("expected 4 total, got %d", got)
	}

	tests := []struct {
		physIdx     int64
		wantLogical int64
		wantOffset  int
	}{
		{0, 0, 0}, // Physical 0 → logical line 0, offset 0
		{1, 1, 0}, // Physical 1 → logical line 1, offset 0 (first wrap)
		{2, 1, 1}, // Physical 2 → logical line 1, offset 1 (second wrap)
		{3, 2, 0}, // Physical 3 → logical line 2, offset 0
	}

	for _, tt := range tests {
		logIdx, offset := idx.PhysicalToLogical(tt.physIdx)
		if logIdx != tt.wantLogical || offset != tt.wantOffset {
			t.Errorf("PhysicalToLogical(%d) = (%d, %d), want (%d, %d)",
				tt.physIdx, logIdx, offset, tt.wantLogical, tt.wantOffset)
		}
	}
}

func TestPhysicalLineIndex_PhysicalToLogicalEmpty(t *testing.T) {
	mb := NewMemoryBuffer(MemoryBufferConfig{MaxLines: 100, EvictionBatch: 10})
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// Empty index should return baseOffset
	logIdx, offset := idx.PhysicalToLogical(0)
	if logIdx != 0 || offset != 0 {
		t.Errorf("expected (0, 0) for empty index, got (%d, %d)", logIdx, offset)
	}
}

func TestPhysicalLineIndex_MatchesBuilderOutput(t *testing.T) {
	// Verify that physicalLinesFor matches the actual WrapToWidth output
	lines := []string{
		"",                                       // empty
		"Short",                                  // fits in one line
		"ExactlyEightyCharsxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", // exactly 80
		"This line is a bit longer than 80 characters so it will wrap to the next line, yes indeed it will", // >80
	}

	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)
	builder := NewPhysicalLineBuilder(80)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	for i := int64(0); i < reader.GlobalEnd(); i++ {
		line := reader.GetLine(i)
		builderCount := len(builder.BuildLine(line, i))
		indexCount := idx.PhysicalCountFor(i)

		if builderCount != indexCount {
			t.Errorf("line %d: builder says %d physical lines, index says %d (cells=%d)",
				i, builderCount, indexCount, len(line.Cells))
		}
	}
}

func TestPhysicalLineIndex_PrefixSumAt(t *testing.T) {
	// 3 lines: 1 phys + 2 phys + 1 phys
	long := make([]byte, 160)
	for i := range long {
		long[i] = 'Y'
	}
	mb := setupTestBuffer([]string{"A", string(long), "B"}, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	// prefixSum[0] = 0, prefixSum[1] = 1, prefixSum[2] = 3, prefixSum[3] = 4
	if got := idx.PrefixSumAt(0); got != 0 {
		t.Errorf("PrefixSumAt(0) = %d, want 0", got)
	}
	if got := idx.PrefixSumAt(1); got != 1 {
		t.Errorf("PrefixSumAt(1) = %d, want 1", got)
	}
	if got := idx.PrefixSumAt(2); got != 3 {
		t.Errorf("PrefixSumAt(2) = %d, want 3", got)
	}
	if got := idx.PrefixSumAt(3); got != 4 {
		t.Errorf("PrefixSumAt(3) = %d, want 4", got)
	}
}

// --- Benchmarks ---

func BenchmarkPhysicalLineIndex_Build(b *testing.B) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "Benchmark test line that is long enough to potentially wrap"
	}
	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)

	b.ResetTimer()
	for range b.N {
		idx.Build()
	}
}

func BenchmarkPhysicalLineIndex_Build_50k(b *testing.B) {
	lines := make([]string, 50000)
	for i := range lines {
		lines[i] = "Benchmark test line that is long enough to potentially wrap"
	}
	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)

	b.ResetTimer()
	for range b.N {
		idx.Build()
	}
}

func BenchmarkPhysicalLineIndex_TotalAfterBuild(b *testing.B) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "Benchmark test line that is long enough to potentially wrap"
	}
	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	b.ResetTimer()
	for range b.N {
		_ = idx.TotalPhysicalLines()
	}
}

func BenchmarkPhysicalLineIndex_PhysicalToLogical(b *testing.B) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = "Benchmark test line that is long enough to potentially wrap"
	}
	mb := setupTestBuffer(lines, 80)
	reader := NewMemoryBufferReader(mb)
	idx := NewPhysicalLineIndex(reader, 80)
	idx.Build()

	total := idx.TotalPhysicalLines()
	mid := total / 2

	b.ResetTimer()
	for range b.N {
		idx.PhysicalToLogical(mid)
	}
}
