// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/apps/texelterm/transformer"
)

func makeCells(s string) *parser.LogicalLine {
	cells := make([]parser.Cell, len([]rune(s)))
	for i, r := range []rune(s) {
		cells[i] = parser.Cell{Rune: r, FG: parser.DefaultFG, BG: parser.DefaultBG}
	}
	return &parser.LogicalLine{Cells: cells}
}

func TestRegistration(t *testing.T) {
	_, ok := transformer.Lookup("tablefmt")
	if !ok {
		t.Fatal("tablefmt should be registered via init()")
	}
}

func TestPassThrough_Scanning(t *testing.T) {
	f := New(1000)
	f.NotifyPromptStart()

	line := makeCells("hello world this is plain text")
	f.HandleLine(0, line, true)

	if f.ShouldSuppress(0) {
		t.Error("expected plain text to not be suppressed")
	}
}

func TestConfigMaxBufferRows(t *testing.T) {
	factory, ok := transformer.Lookup("tablefmt")
	if !ok {
		t.Fatal("tablefmt not registered")
	}

	t.Run("default", func(t *testing.T) {
		tr, err := factory(transformer.Config{})
		if err != nil {
			t.Fatal(err)
		}
		tf := tr.(*TableFormatter)
		if tf.maxBufferRows != 1000 {
			t.Errorf("expected default maxBufferRows=1000, got %d", tf.maxBufferRows)
		}
	})

	t.Run("custom", func(t *testing.T) {
		tr, err := factory(transformer.Config{"max_buffer_rows": float64(500)})
		if err != nil {
			t.Fatal(err)
		}
		tf := tr.(*TableFormatter)
		if tf.maxBufferRows != 500 {
			t.Errorf("expected maxBufferRows=500, got %d", tf.maxBufferRows)
		}
	})
}

func TestPromptTransitionFlushes(t *testing.T) {
	f := New(1000)
	f.NotifyPromptStart()
	var inserted int
	f.SetInsertFunc(func(_ int64, _ []parser.Cell) { inserted++ })

	// Feed command output lines that look like a space-aligned table.
	f.HandleLine(0, makeCells("NAME   READY  STATUS"), true)
	f.HandleLine(1, makeCells("nginx  1/1    Running"), true)

	if f.state != stateBuffering {
		t.Errorf("expected stateBuffering for table-like output, got %d", f.state)
	}

	// Transition to prompt (isCommand=false) should flush the buffer.
	f.HandleLine(2, makeCells("$ "), false)

	if f.state != stateScanning {
		t.Errorf("expected stateScanning after flush, got %d", f.state)
	}
	if len(f.buffer) != 0 {
		t.Errorf("expected empty buffer after flush, got %d entries", len(f.buffer))
	}
	if inserted == 0 {
		t.Error("expected insertFunc called during prompt flush")
	}
}

func TestNoShellIntegration_TreatsAllAsCommand(t *testing.T) {
	f := New(1000)
	// Do NOT call NotifyPromptStart — no shell integration.

	line := makeCells("hello world")
	f.HandleLine(0, line, false)

	// Without shell integration, isCommand is effectively true.
	// The line should be processed (not skipped).
	if f.ShouldSuppress(0) {
		t.Error("expected no suppression for plain text without shell integration")
	}
}

func TestSetInsertFunc(t *testing.T) {
	f := New(1000)
	called := false
	f.SetInsertFunc(func(beforeIdx int64, cells []parser.Cell) {
		called = true
	})

	if f.insertFunc == nil {
		t.Error("expected insertFunc to be set")
	}

	f.insertFunc(0, nil)
	if !called {
		t.Error("expected insertFunc to be callable")
	}
}

func TestBuffering_MDTable(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
		"| Bob   | 25  |",
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
		if !tf.ShouldSuppress(int64(i)) {
			t.Errorf("line %d: expected suppression during buffering", i)
		}
	}
	if tf.state != stateBuffering {
		t.Errorf("expected stateBuffering, got %d", tf.state)
	}
	if len(tf.buffer) != 4 {
		t.Errorf("expected 4 buffered lines, got %d", len(tf.buffer))
	}
}

func TestBuffering_FlushOnNonTableLine(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()
	var inserted int
	tf.SetInsertFunc(func(_ int64, _ []parser.Cell) { inserted++ })

	lines := []string{
		"| Name  | Age |",
		"| ----- | --- |",
		"| Alice | 30  |",
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	tf.HandleLine(3, makeCells("This is not a table line"), true)

	if tf.state != stateScanning {
		t.Errorf("expected stateScanning after flush, got %d", tf.state)
	}
	if inserted == 0 {
		t.Error("expected insertFunc to be called during flush")
	}
}

func TestBuffering_FlushOnPrompt(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()
	var inserted int
	tf.SetInsertFunc(func(_ int64, _ []parser.Cell) { inserted++ })

	for i, s := range []string{"| A | B |", "| - | - |", "| x | y |"} {
		tf.HandleLine(int64(i), makeCells(s), true)
	}
	tf.HandleLine(3, makeCells("$ "), false)

	if tf.state != stateScanning {
		t.Errorf("expected stateScanning after prompt, got %d", tf.state)
	}
	if inserted == 0 {
		t.Error("expected insertFunc called on prompt flush")
	}
}

func TestBuffering_LimitExceeded(t *testing.T) {
	tf := New(3)
	tf.NotifyPromptStart()
	var insertedLines [][]parser.Cell
	tf.SetInsertFunc(func(_ int64, cells []parser.Cell) {
		insertedLines = append(insertedLines, cells)
	})

	lines := []string{
		"| A | B |",
		"| - | - |",
		"| x | y |",
		"| z | w |", // 4th line exceeds limit of 3
	}
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
	}

	// After exceeding limit, buffer should have been flushed raw.
	if tf.state == stateBuffering && len(tf.buffer) > 3 {
		t.Error("buffer should have been flushed when limit exceeded")
	}
	// Raw emission should not contain box-drawing characters.
	for _, cells := range insertedLines {
		for _, c := range cells {
			if c.Rune == '\u256d' || c.Rune == '\u2570' {
				t.Error("expected raw emission, got box-drawing")
			}
		}
	}
}

func TestScanning_PlainTextPassThrough(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	tf.HandleLine(0, makeCells("just plain text"), true)
	if tf.ShouldSuppress(0) {
		t.Error("non-table lines should not be suppressed")
	}
	if tf.state != stateScanning {
		t.Errorf("should remain scanning, got %d", tf.state)
	}
}

func TestBuffering_SpaceAligned(t *testing.T) {
	tf := New(1000)
	tf.NotifyPromptStart()

	lines := []string{
		"NAME                 STATUS    AGE     VERSION",
		"nginx-pod            Running   5d      1.21.0",
		"redis-pod            Running   3d      7.0.5",
		"postgres-pod         Running   10d     15.2",
	}
	suppressed := false
	for i, s := range lines {
		tf.HandleLine(int64(i), makeCells(s), true)
		if tf.ShouldSuppress(int64(i)) {
			suppressed = true
		}
	}
	if !suppressed {
		t.Error("expected at least some lines suppressed for space-aligned table")
	}
}
