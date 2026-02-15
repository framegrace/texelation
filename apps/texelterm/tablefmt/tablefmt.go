// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package tablefmt is an inline output transformer for texelterm that detects
// tabular command output (space-aligned columns, CSV, pipe-separated, Markdown
// tables) and re-renders it with box-drawing borders and per-column coloring.
// It self-registers via init() into the transformer registry.
package tablefmt

import (
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
	_ transformer.Transformer   = (*TableFormatter)(nil)
	_ transformer.LineInserter  = (*TableFormatter)(nil)
	_ transformer.LineSuppressor = (*TableFormatter)(nil)
)

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
}

// New creates a TableFormatter with the given maximum buffer size.
func New(maxBufferRows int) *TableFormatter {
	return &TableFormatter{
		maxBufferRows: maxBufferRows,
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
		// TODO: test line against detectors, transition to stateBuffering if match
	case stateBuffering:
		// TODO: accumulate or flush
	}
}

// ShouldSuppress implements transformer.LineSuppressor. Returns true if the
// most recently handled line was consumed into the buffer.
func (tf *TableFormatter) ShouldSuppress(lineIdx int64) bool {
	return tf.suppressed
}

// flush evaluates buffered lines and either renders them as a formatted table
// or passes them through unmodified. Resets state to scanning.
func (tf *TableFormatter) flush() {
	// TODO: evaluate confidence, render or pass through
	tf.buffer = tf.buffer[:0]
	tf.state = stateScanning
}
