// internal/runtime/server/session_persistence_test.go
package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStoredSessionRoundTrip(t *testing.T) {
	in := StoredSession{
		SchemaVersion: 1,
		SessionID:     [16]byte{0x01, 0x02, 0x03},
		LastActive:    time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		Pinned:        true,
		PaneViewports: []StoredPaneViewport{{
			PaneID:         [16]byte{0xaa, 0xbb},
			AltScreen:      false,
			AutoFollow:     true,
			ViewBottomIdx:  1234,
			WrapSegmentIdx: 0,
			Rows:           24,
			Cols:           80,
		}},
		Label:          "my-session",
		PaneCount:      1,
		FirstPaneTitle: "bash",
	}
	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"sessionID":"01020300000000000000000000000000"`) {
		t.Fatalf("expected lowercase hex sessionID, got: %s", data)
	}
	if !strings.Contains(string(data), `"paneID":"aabb0000000000000000000000000000"`) {
		t.Fatalf("expected lowercase hex paneID, got: %s", data)
	}
	var out StoredSession
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.SchemaVersion != in.SchemaVersion ||
		out.SessionID != in.SessionID ||
		!out.LastActive.Equal(in.LastActive) ||
		out.Pinned != in.Pinned ||
		out.Label != in.Label ||
		out.PaneCount != in.PaneCount ||
		out.FirstPaneTitle != in.FirstPaneTitle {
		t.Fatalf("round-trip mismatch:\nin = %+v\nout= %+v", in, out)
	}
	if len(out.PaneViewports) != 1 || out.PaneViewports[0] != in.PaneViewports[0] {
		t.Fatalf("PaneViewports round-trip mismatch:\nin = %+v\nout= %+v", in.PaneViewports, out.PaneViewports)
	}
}
