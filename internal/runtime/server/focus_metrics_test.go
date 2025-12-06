// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/focus_metrics_test.go
// Summary: Exercises focus metrics behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestFocusMetricsRecordsStats(t *testing.T) {
	var buf bytes.Buffer
	metrics := NewFocusMetrics(log.New(&buf, "", 0))

	var id [16]byte
	copy(id[:], []byte("focus-metric-demo"))

	metrics.PaneFocused(id)
	metrics.PaneFocused(id)

	stats := metrics.Snapshot()
	if stats.Changes != 2 {
		t.Fatalf("expected 2 focus changes, got %d", stats.Changes)
	}
	if stats.LastPaneID != id {
		t.Fatalf("unexpected last pane id: %x", stats.LastPaneID)
	}
	if stats.LastChange.IsZero() {
		t.Fatalf("expected last change timestamp to be set")
	}

	output := buf.String()
	if output == "" {
		t.Fatalf("expected log output for focus metric")
	}
	if !strings.Contains(output, "metric focus") {
		t.Fatalf("unexpected log output: %q", output)
	}
}
