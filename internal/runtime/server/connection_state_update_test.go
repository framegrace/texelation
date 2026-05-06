// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"github.com/framegrace/texelation/texel"
)

// TestStatePayloadToProtocol_PropagatesZoomFields guards issue #235's
// real root cause: the original sendStateUpdate built protocol.StateUpdate
// without copying state.Zoomed and state.ZoomedPaneID, so the wire
// message always carried Zoomed=false regardless of the desktop's
// actual state. The fix copies both fields; this test would fail on
// pre-fix code.
func TestStatePayloadToProtocol_PropagatesZoomFields(t *testing.T) {
	want := [16]byte{0x42}
	in := texel.StatePayload{
		Zoomed:       true,
		ZoomedPaneID: want,
	}

	got := statePayloadToProtocol(in)

	if !got.Zoomed {
		t.Fatalf("Zoomed not propagated: want true, got false")
	}
	if got.ZoomedPaneID != want {
		t.Fatalf("ZoomedPaneID not propagated: want %x, got %x", want, got.ZoomedPaneID)
	}
}

// TestStatePayloadToProtocol_NotZoomedDefaults verifies the converter
// honors the unzoomed state (no implicit "stuck zoom" via field reuse).
func TestStatePayloadToProtocol_NotZoomedDefaults(t *testing.T) {
	in := texel.StatePayload{Zoomed: false}
	got := statePayloadToProtocol(in)
	if got.Zoomed {
		t.Fatalf("expected Zoomed=false, got true")
	}
	if got.ZoomedPaneID != ([16]byte{}) {
		t.Fatalf("expected zero ZoomedPaneID, got %x", got.ZoomedPaneID)
	}
}
