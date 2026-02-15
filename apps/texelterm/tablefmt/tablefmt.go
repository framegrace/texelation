// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package tablefmt is an inline output transformer for texelterm that detects
// tabular command output (space-aligned columns, CSV, pipe-separated, Markdown
// tables) and re-renders it with box-drawing borders and per-column coloring.
// It self-registers via init() into the transformer registry.
package tablefmt

import (
	"strings"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
)

func init() {
	transformer.Register("tablefmt", func(cfg transformer.Config) (transformer.Transformer, error) {
		maxRows := 1000
		if v, ok := cfg["max_buffer_rows"].(float64); ok && v > 0 {
			maxRows = int(v)
		}
		return New(maxRows), nil
	})
}

// Compile-time interface verification.
var (
	_ transformer.Transformer         = (*TableFormatter)(nil)
	_ transformer.LineInserter        = (*TableFormatter)(nil)
	_ transformer.LineOverlayer       = (*TableFormatter)(nil)
	_ transformer.LineSuppressor      = (*TableFormatter)(nil)
	_ transformer.LinePersistNotifier = (*TableFormatter)(nil)
)

// detectorThreshold pairs a detector with its minimum score for table parsing.
type detectorThreshold struct {
	detector  tableDetector
	threshold float64
}

// state tracks whether the formatter is scanning for table headers or
// buffering rows within a detected table.
type state int

const (
	stateScanning  state = iota // looking for table-like patterns
	stateBuffering              // accumulating rows within a detected table
)

// bufferedLine pairs a logical line with its global line index so that
// deferred rendering can insert/suppress at the correct positions.
type bufferedLine struct {
	lineIdx int64
	line    *parser.LogicalLine
}

// TableFormatter detects and reformats tabular command output. It implements
// transformer.Transformer, transformer.LineInserter, and
// transformer.LineSuppressor.
type TableFormatter struct {
	state               state
	buffer              []*bufferedLine
	maxBufferRows       int
	suppressed          bool
	wasCommand          bool
	hasShellIntegration bool
	insertFunc          func(beforeIdx int64, cells []parser.Cell)
	overlayFunc         func(lineIdx int64, cells []parser.Cell)
	persistNotifyFunc   func(lineIdx int64)
	detectors           []detectorThreshold
	activeDetector      tableDetector
}

// New creates a TableFormatter with the given maximum buffer size.
func New(maxBufferRows int) *TableFormatter {
	return &TableFormatter{
		maxBufferRows: maxBufferRows,
		detectors: []detectorThreshold{
			{&markdownDetector{}, 0.95},
			{&pipeDetector{}, 0.7},
			{&spaceAlignedDetector{}, 0.6},
			{&csvDetector{}, 0.5},
		},
	}
}

// SetInsertFunc implements transformer.LineInserter.
func (tf *TableFormatter) SetInsertFunc(fn func(beforeIdx int64, cells []parser.Cell)) {
	tf.insertFunc = fn
}

// SetOverlayFunc implements transformer.LineOverlayer.
func (tf *TableFormatter) SetOverlayFunc(fn func(lineIdx int64, cells []parser.Cell)) {
	tf.overlayFunc = fn
}

// SetPersistNotifyFunc implements transformer.LinePersistNotifier.
func (tf *TableFormatter) SetPersistNotifyFunc(fn func(lineIdx int64)) {
	tf.persistNotifyFunc = fn
}

// NotifyPromptStart signals that shell integration is active.
func (tf *TableFormatter) NotifyPromptStart() {
	tf.hasShellIntegration = true
}

// HandleLine processes a committed line. It tracks command/prompt transitions
// to know when output starts and ends.
func (tf *TableFormatter) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) {
	tf.suppressed = false
	effectiveIsCommand := isCommand || !tf.hasShellIntegration

	// Command-to-prompt transition: flush any buffered table.
	if tf.wasCommand && !effectiveIsCommand {
		tf.flush()
	}
	tf.wasCommand = effectiveIsCommand

	if !effectiveIsCommand {
		return
	}

	switch tf.state {
	case stateScanning:
		tf.handleScanning(lineIdx, line)
	case stateBuffering:
		tf.handleBuffering(lineIdx, line)
	}
}

// handleScanning tests a line against all detectors. A single line cannot
// be reliably scored (detectors need multiple rows), so we use Compatible
// as a lightweight check to trigger speculative buffering. The full Score
// evaluation happens in flush when the buffer is complete.
func (tf *TableFormatter) handleScanning(lineIdx int64, line *parser.LogicalLine) {
	text := extractPlainText(line)
	if len(strings.TrimSpace(text)) == 0 {
		return
	}

	for _, dt := range tf.detectors {
		if dt.detector.Compatible(text) && looksLikeTableCandidate(text, dt.detector) {
			tf.state = stateBuffering
			tf.activeDetector = dt.detector
			tf.buffer = append(tf.buffer, &bufferedLine{
				lineIdx: lineIdx,
				line:    line.Clone(),
			})
			tf.suppressed = true
			return
		}
	}
}

// looksLikeTableCandidate performs a lightweight check to determine whether
// a single line is plausible as the start of a table for a given detector.
// This avoids false positives from overly permissive Compatible methods
// (e.g., spaceAlignedDetector returns true for all lines).
func looksLikeTableCandidate(text string, d tableDetector) bool {
	trimmed := strings.TrimSpace(text)
	switch d.(type) {
	case *markdownDetector, *pipeDetector:
		return strings.Contains(trimmed, "|")
	case *spaceAlignedDetector:
		// Require at least one double-space gap and minimum length.
		return len(trimmed) >= 20 && strings.Contains(trimmed, "  ")
	case *csvDetector:
		// Require at least 2 delimiters (3+ columns) to avoid matching
		// JSON trailing commas, prose, etc.
		return strings.Count(trimmed, ",") >= 2 || strings.Count(trimmed, "\t") >= 2
	}
	return false
}

// handleBuffering checks whether a new line is compatible with the active
// detector. Compatible lines are cloned and buffered. When the buffer exceeds
// maxBufferRows or a line is incompatible, the buffer is flushed.
func (tf *TableFormatter) handleBuffering(lineIdx int64, line *parser.LogicalLine) {
	text := extractPlainText(line)

	if !tf.activeDetector.Compatible(text) {
		tf.flush()
		tf.handleScanning(lineIdx, line)
		return
	}

	// Buffer limit exceeded: flush raw, then re-process through scanning.
	if len(tf.buffer) >= tf.maxBufferRows {
		tf.flushRaw()
		tf.handleScanning(lineIdx, line)
		return
	}

	tf.buffer = append(tf.buffer, &bufferedLine{
		lineIdx: lineIdx,
		line:    line.Clone(),
	})
	tf.suppressed = true
}

// ShouldSuppress implements transformer.LineSuppressor. Returns true if the
// most recently handled line was consumed into the buffer.
func (tf *TableFormatter) ShouldSuppress(lineIdx int64) bool {
	return tf.suppressed
}

// flush evaluates buffered lines and either renders them as a formatted table
// or passes them through unmodified. Resets state to scanning.
func (tf *TableFormatter) flush() {
	if len(tf.buffer) == 0 {
		tf.resetState()
		return
	}

	// Collect plain text from buffered lines.
	lines := make([]string, len(tf.buffer))
	for i, bl := range tf.buffer {
		lines[i] = extractPlainText(bl.line)
	}

	// Score with all detectors, pick the highest-scoring one above threshold.
	var bestDetector tableDetector
	bestScore := 0.0
	for _, dt := range tf.detectors {
		score := dt.detector.Score(lines)
		if score >= dt.threshold && score > bestScore {
			bestScore = score
			bestDetector = dt.detector
		}
	}

	if bestDetector != nil {
		ts := bestDetector.Parse(lines)
		if ts != nil {
			rendered := renderTable(ts)
			if rendered != nil {
				tf.emitRendered(rendered)
				tf.resetState()
				return
			}
		}
	}

	tf.flushRaw()
}

// emitRendered writes rendered table rows into the buffer. Suppressed lines
// are overlaid via overlayFunc; any extra rows are inserted.
func (tf *TableFormatter) emitRendered(rendered [][]parser.Cell) {
	nBuf := len(tf.buffer)
	insertBase := tf.buffer[nBuf-1].lineIdx + 1
	extraCount := int64(0)
	for i, row := range rendered {
		if i < nBuf && tf.overlayFunc != nil {
			tf.overlayFunc(tf.buffer[i].lineIdx, row)
			if tf.persistNotifyFunc != nil {
				tf.persistNotifyFunc(tf.buffer[i].lineIdx)
			}
		} else if tf.insertFunc != nil {
			tf.insertFunc(insertBase+extraCount, row)
			if tf.persistNotifyFunc != nil {
				tf.persistNotifyFunc(insertBase + extraCount)
			}
			extraCount++
		}
	}
}

// flushRaw restores all buffered lines without formatting. Original Cells were
// never mutated, so we just notify persistence for each buffered line.
func (tf *TableFormatter) flushRaw() {
	// Original Cells were never mutated. Notify persistence for each
	// buffered line so they get written to disk.
	if tf.persistNotifyFunc != nil {
		for _, bl := range tf.buffer {
			tf.persistNotifyFunc(bl.lineIdx)
		}
	}
	tf.resetState()
}

// resetState clears the buffer and returns to scanning.
func (tf *TableFormatter) resetState() {
	tf.buffer = tf.buffer[:0]
	tf.state = stateScanning
	tf.activeDetector = nil
	// Reset stateful detectors so stale delimiter info doesn't leak
	// between tables.
	for _, dt := range tf.detectors {
		if cd, ok := dt.detector.(*csvDetector); ok {
			cd.detectedDelim = 0
			cd.detectedCount = 0
		}
	}
}

// extractPlainText returns the rune content of a logical line as a string.
func extractPlainText(line *parser.LogicalLine) string {
	var b strings.Builder
	for _, cell := range line.Cells {
		if cell.Rune != 0 {
			b.WriteRune(cell.Rune)
		}
	}
	return b.String()
}
