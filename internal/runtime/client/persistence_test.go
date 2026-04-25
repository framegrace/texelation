package clientruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

func TestClientState_RoundTrip(t *testing.T) {
	want := ClientState{
		SocketPath:   "/tmp/texelation.sock",
		SessionID:    [16]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
		LastSequence: 12345,
		WrittenAt:    time.Date(2026, 4, 26, 12, 34, 56, 0, time.UTC),
		PaneViewports: []protocol.PaneViewportState{{
			PaneID:         [16]byte{0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10, 0xfe, 0xdc, 0xba, 0x98, 0x76, 0x54, 0x32, 0x10},
			AltScreen:      false,
			AutoFollow:     true,
			ViewBottomIdx:  9876,
			WrapSegmentIdx: 0,
			ViewportRows:   24,
			ViewportCols:   80,
		}},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(&want); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Hex format is load-bearing — base64 is unfriendly to jq/grep.
	if !strings.Contains(buf.String(), `"0123456789abcdef0123456789abcdef"`) {
		t.Errorf("expected hex sessionID in JSON, got: %s", buf.String())
	}

	var got ClientState
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SocketPath != want.SocketPath {
		t.Errorf("SocketPath: got %q want %q", got.SocketPath, want.SocketPath)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("SessionID mismatch")
	}
	if got.LastSequence != want.LastSequence {
		t.Errorf("LastSequence: got %d want %d", got.LastSequence, want.LastSequence)
	}
	if !got.WrittenAt.Equal(want.WrittenAt) {
		t.Errorf("WrittenAt: got %v want %v", got.WrittenAt, want.WrittenAt)
	}
	if len(got.PaneViewports) != 1 {
		t.Fatalf("PaneViewports: got %d want 1", len(got.PaneViewports))
	}
	if got.PaneViewports[0] != want.PaneViewports[0] {
		t.Errorf("PaneViewport mismatch")
	}
}

func TestResolvePath_DefaultName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/test-xdg-state")
	t.Setenv(ClientNameEnvVar, "")

	got, err := ResolvePath("/run/texelation.sock", "")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	wantPrefix := "/tmp/test-xdg-state/texelation/client/"
	wantSuffix := "/default.json"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("path %q missing prefix %q", got, wantPrefix)
	}
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("path %q missing suffix %q", got, wantSuffix)
	}
}

func TestResolvePath_FlagPrecedence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/test-xdg-state")
	t.Setenv(ClientNameEnvVar, "fromenv")

	got, err := ResolvePath("/run/texelation.sock", "fromflag")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if !strings.HasSuffix(got, "/fromflag.json") {
		t.Errorf("flag should win over env: got %q", got)
	}
}

func TestResolvePath_EnvFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/test-xdg-state")
	t.Setenv(ClientNameEnvVar, "fromenv")

	got, err := ResolvePath("/run/texelation.sock", "")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if !strings.HasSuffix(got, "/fromenv.json") {
		t.Errorf("env should win when flag empty: got %q", got)
	}
}

func TestResolvePath_SocketHashStable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/test-xdg-state")
	a, _ := ResolvePath("/run/texelation.sock", "x")
	b, _ := ResolvePath("/run/texelation.sock", "x")
	if a != b {
		t.Errorf("hash unstable: %q vs %q", a, b)
	}
	c, _ := ResolvePath("/run/different.sock", "x")
	if a == c {
		t.Errorf("different sockets produced same hash: %q", a)
	}
}

func TestResolvePath_RejectsInvalidNames(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/test-xdg-state")
	cases := []string{
		"..", ".", "../escape", "with/slash", "with\\backslash",
		".hidden",                // leading dot
		"con", "CON", "Con.json", // Windows reserved + case + extension
		"nul", "aux", "prn",
		"com1", "COM9", "lpt5",
		"name with spaces", "with$dollar", "with;semi",
		"\x00", "\x00bad", "bad\x00", // NULL byte injection (Go os.Open historically truncates here)
	}
	for _, name := range cases {
		if _, err := ResolvePath("/run/x.sock", name); err == nil {
			t.Errorf("ResolvePath(%q) should have errored, got nil", name)
		}
	}
}

func TestSave_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "state.json")

	state := ClientState{
		SocketPath:   "/tmp/x.sock",
		SessionID:    [16]byte{1},
		LastSequence: 1,
		WrittenAt:    time.Now(),
	}

	if err := Save(path, &state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File exists with valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got ClientState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SessionID != state.SessionID {
		t.Errorf("round-trip via disk: SessionID mismatch")
	}

	// No leftover .tmp file. Fail loudly if ReadDir itself fails — a
	// silent zero-iteration loop here would mask a broken fixture.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".state.tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestSave_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	first := ClientState{SocketPath: "/tmp/a", SessionID: [16]byte{1}, LastSequence: 1, WrittenAt: time.Now()}
	if err := Save(path, &first); err != nil {
		t.Fatalf("Save first: %v", err)
	}

	second := ClientState{SocketPath: "/tmp/b", SessionID: [16]byte{2}, LastSequence: 99, WrittenAt: time.Now()}
	if err := Save(path, &second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	data, _ := os.ReadFile(path)
	var got ClientState
	_ = json.Unmarshal(data, &got)
	if got.SessionID != second.SessionID {
		t.Errorf("expected second state, got first")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.json")
	got, err := Load(path, "/tmp/x.sock")
	if err != nil {
		t.Fatalf("Load on missing file should succeed, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil state on missing file, got %+v", got)
	}
}

func TestLoad_SocketMismatchWipes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	state := ClientState{SocketPath: "/tmp/a.sock", SessionID: [16]byte{1}, LastSequence: 1, WrittenAt: time.Now()}
	if err := Save(path, &state); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	got, err := Load(path, "/tmp/b.sock") // different socket
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Errorf("mismatch should yield nil state")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should be wiped on mismatch, stat err=%v", err)
	}
}

func TestLoad_ParseErrorWipes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := Load(path, "/tmp/x.sock")
	if err != nil {
		t.Fatalf("Load should swallow parse errors, got %v", err)
	}
	if got != nil {
		t.Errorf("parse error should yield nil state")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file should be wiped on parse error")
	}
}

func TestLoad_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	want := ClientState{SocketPath: "/tmp/x.sock", SessionID: [16]byte{0xaa}, LastSequence: 7, WrittenAt: time.Now()}
	if err := Save(path, &want); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := Load(path, "/tmp/x.sock")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatalf("expected non-nil state")
	}
	if got.SessionID != want.SessionID || got.LastSequence != want.LastSequence {
		t.Errorf("round-trip mismatch")
	}
}
