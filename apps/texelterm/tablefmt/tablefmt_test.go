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

	// Feed command output lines (stateScanning, no suppression).
	f.HandleLine(0, makeCells("NAME   READY  STATUS"), true)
	f.HandleLine(1, makeCells("nginx  1/1    Running"), true)

	if f.state != stateScanning {
		t.Errorf("expected stateScanning, got %d", f.state)
	}

	// Transition to prompt (isCommand=false) should call flush.
	f.HandleLine(2, makeCells("$ "), false)

	if f.state != stateScanning {
		t.Errorf("expected stateScanning after flush, got %d", f.state)
	}
	if len(f.buffer) != 0 {
		t.Errorf("expected empty buffer after flush, got %d entries", len(f.buffer))
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
