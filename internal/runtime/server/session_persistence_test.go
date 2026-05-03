// internal/runtime/server/session_persistence_test.go
package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

func TestStoredSession_LayoutRoundTrip(t *testing.T) {
	in := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     [16]byte{0xAA, 0xBB, 0xCC},
		LastActive:    time.Unix(1714752000, 0).UTC(),
		Pinned:        true,
		Label:         "with-layout",
		PaneCount:     2,
		Layout: &protocol.TreeNodeSnapshot{
			PaneIndex:   -1,
			Split:       protocol.SplitHorizontal,
			SplitRatios: []float32{0.5, 0.5},
			Children: []protocol.TreeNodeSnapshot{
				{PaneIndex: 0, Split: protocol.SplitNone},
				{PaneIndex: 1, Split: protocol.SplitNone},
			},
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out StoredSession
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Layout == nil {
		t.Fatalf("expected Layout populated, got nil")
	}
	if !reflect.DeepEqual(in.Layout, out.Layout) {
		t.Fatalf("layout round-trip mismatch:\n in=%#v\nout=%#v", in.Layout, out.Layout)
	}
}

func TestScanSessionsDir_RemovesOrphanPNGs(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	keepID := [16]byte{0x01}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     keepID,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	keepHex := pickerHex16(keepID)
	if err := os.WriteFile(filepath.Join(sessions, keepHex+".json"), data, 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessions, keepHex+".png"), []byte("matched"), 0o644); err != nil {
		t.Fatalf("write keep png: %v", err)
	}
	orphanID := [16]byte{0xDE}
	orphanPath := filepath.Join(sessions, pickerHex16(orphanID)+".png")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan png: %v", err)
	}
	tmpPath := filepath.Join(sessions, "00112233445566778899aabbccddeeff.png.tmp")
	if err := os.WriteFile(tmpPath, []byte("crashed"), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if _, err := ScanSessionsDir(dir); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("expected orphan PNG removed, stat err=%v", err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected orphan tmp removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessions, keepHex+".png")); err != nil {
		t.Errorf("expected matched PNG retained, stat err=%v", err)
	}
}

// pickerHex16 mirrors hex.EncodeToString without pulling encoding/hex
// into this test file's imports.
func pickerHex16(id [16]byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 32)
	for i, b := range id {
		out[2*i] = digits[b>>4]
		out[2*i+1] = digits[b&0x0F]
	}
	return string(out)
}

func TestStoredSession_LayoutAbsentInOlderJSON(t *testing.T) {
	older := []byte(`{
		"schemaVersion": 1,
		"sessionID": "aabbccddeeff00112233445566778899",
		"lastActive": "2026-04-26T00:00:00Z",
		"pinned": false,
		"paneViewports": [],
		"label": "legacy",
		"paneCount": 1,
		"firstPaneTitle": ""
	}`)
	var out StoredSession
	if err := json.Unmarshal(older, &out); err != nil {
		t.Fatalf("unmarshal pre-F.1 JSON: %v", err)
	}
	if out.Layout != nil {
		t.Fatalf("expected Layout=nil for legacy JSON, got %#v", out.Layout)
	}
	if out.Label != "legacy" {
		t.Fatalf("expected Label preserved, got %q", out.Label)
	}
}

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

func TestSessionFilePath(t *testing.T) {
	got := SessionFilePath("/var/lib/texel", [16]byte{0x12, 0x34, 0xab})
	wantDir := "/var/lib/texel/sessions"
	wantBase := "1234ab00000000000000000000000000.json" // 32 hex chars + .json
	if filepath.Dir(got) != wantDir {
		t.Fatalf("expected dir %s, got %s", wantDir, filepath.Dir(got))
	}
	if filepath.Base(got) != wantBase {
		t.Fatalf("expected base %s, got %s", wantBase, filepath.Base(got))
	}
}

func TestScanSessionsDir(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Good file
	good := StoredSession{SchemaVersion: 1, SessionID: [16]byte{0x01}, LastActive: time.Now()}
	goodPath := SessionFilePath(dir, good.SessionID)
	data, _ := json.Marshal(&good)
	if err := os.WriteFile(goodPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Corrupt file (DELETED on scan)
	corruptPath := filepath.Join(sessDir, "deadbeef00000000000000000000000000.json")
	if err := os.WriteFile(corruptPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Schema mismatch (DELETED on scan)
	mismatch := StoredSession{SchemaVersion: 999, SessionID: [16]byte{0x02}, LastActive: time.Now()}
	mismatchPath := SessionFilePath(dir, mismatch.SessionID)
	mdata, _ := json.Marshal(&mismatch)
	if err := os.WriteFile(mismatchPath, mdata, 0o600); err != nil {
		t.Fatal(err)
	}
	// Non-JSON file (e.g. README) — IGNORED, not deleted
	readme := filepath.Join(sessDir, "README.txt")
	if err := os.WriteFile(readme, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Filename-vs-content mismatch (e.g. user renamed a file to use it
	// as a template) — SKIPPED but NOT deleted. Treated as user-touched
	// data; D2 leaves it alone.
	tplContent := StoredSession{SchemaVersion: 1, SessionID: [16]byte{0xaa}, LastActive: time.Now()}
	tplPath := filepath.Join(sessDir, "my-template.json")
	tplData, _ := json.Marshal(&tplContent)
	if err := os.WriteFile(tplPath, tplData, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := ScanSessionsDir(dir)
	if err != nil {
		t.Fatalf("ScanSessionsDir: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 loaded session, got %d", len(loaded))
	}
	if _, ok := loaded[good.SessionID]; !ok {
		t.Fatalf("expected good session loaded")
	}
	// Corrupt and schema-mismatch files SHOULD be deleted.
	if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
		t.Fatalf("expected corrupt file deleted, stat=%v", err)
	}
	if _, err := os.Stat(mismatchPath); !os.IsNotExist(err) {
		t.Fatalf("expected schema-mismatch file deleted, stat=%v", err)
	}
	// Non-JSON and filename-mismatch files MUST be left alone.
	if _, err := os.Stat(readme); err != nil {
		t.Fatalf("non-JSON file should be untouched, stat=%v", err)
	}
	if _, err := os.Stat(tplPath); err != nil {
		t.Fatalf("filename-mismatch (template) file should be untouched, stat=%v", err)
	}
}

func TestScanSessionsDirMissingIsEmpty(t *testing.T) {
	dir := t.TempDir()
	loaded, err := ScanSessionsDir(dir)
	if err != nil {
		t.Fatalf("ScanSessionsDir: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected 0, got %d", len(loaded))
	}
}
