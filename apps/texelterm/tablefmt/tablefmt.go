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
	_ transformer.Transformer    = (*TableFormatter)(nil)
	_ transformer.LineInserter   = (*TableFormatter)(nil)
	_ transformer.LineSuppressor = (*TableFormatter)(nil)
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
		return strings.ContainsAny(trimmed, ",\t")
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
		// Parse the table. Rendering will be wired in a later task;
		// for now emit the raw lines.
		_ = bestDetector.Parse(lines)
	}

	tf.flushRaw()
}

// flushRaw emits all buffered lines via insertFunc without any formatting.
func (tf *TableFormatter) flushRaw() {
	if tf.insertFunc != nil {
		for _, bl := range tf.buffer {
			tf.insertFunc(bl.lineIdx, bl.line.Cells)
		}
	}
	tf.resetState()
}

// resetState clears the buffer and returns to scanning.
func (tf *TableFormatter) resetState() {
	tf.buffer = tf.buffer[:0]
	tf.state = stateScanning
	tf.activeDetector = nil
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
