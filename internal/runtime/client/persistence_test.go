package clientruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestWriter_CoalescesRapidUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	w := NewWriter(path, 20*time.Millisecond)
	defer w.Close()

	for i := 0; i < 50; i++ {
		w.Update(ClientState{
			SocketPath:   "/tmp/x.sock",
			SessionID:    [16]byte{byte(i)},
			LastSequence: uint64(i),
			WrittenAt:    time.Now(),
		})
	}

	time.Sleep(200 * time.Millisecond)

	got, err := Load(path, "/tmp/x.sock")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatalf("expected file to exist")
	}
	if got.LastSequence != 49 {
		t.Errorf("expected coalesced last write LastSequence=49, got %d", got.LastSequence)
	}
}

func TestWriter_FlushSyncsLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	w := NewWriter(path, 1*time.Hour)
	w.Update(ClientState{SocketPath: "/tmp/x.sock", SessionID: [16]byte{42}, LastSequence: 7, WrittenAt: time.Now()})
	w.Flush()

	got, err := Load(path, "/tmp/x.sock")
	if err != nil || got == nil {
		t.Fatalf("expected file from Flush, got err=%v state=%v", err, got)
	}
	if got.LastSequence != 7 {
		t.Errorf("expected LastSequence=7 from Flush, got %d", got.LastSequence)
	}
	w.Close()
}

func TestWriter_CloseFlushes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	w := NewWriter(path, 1*time.Hour)
	w.Update(ClientState{SocketPath: "/tmp/x.sock", SessionID: [16]byte{9}, LastSequence: 3, WrittenAt: time.Now()})
	w.Close()

	got, _ := Load(path, "/tmp/x.sock")
	if got == nil || got.LastSequence != 3 {
		t.Errorf("expected Close to Flush, got %+v", got)
	}
}

func TestWriter_CloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	w := NewWriter(path, 1*time.Hour)
	w.Update(ClientState{SocketPath: "/tmp/x.sock", SessionID: [16]byte{9}, LastSequence: 3, WrittenAt: time.Now()})
	w.Close()
	w.Close() // must not panic on re-close
}

func TestWriter_SlowSaveSkipsIfBusy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	var saveCount atomic.Int32
	slowSaveStarted := make(chan struct{}, 4)
	slowSaveCanFinish := make(chan struct{})

	w := NewWriter(path, 5*time.Millisecond)
	w.saver = func(p string, s *ClientState) error {
		saveCount.Add(1)
		slowSaveStarted <- struct{}{}
		<-slowSaveCanFinish
		return Save(p, s)
	}

	w.Update(ClientState{SocketPath: "/tmp/x.sock", SessionID: [16]byte{1}, LastSequence: 1, WrittenAt: time.Now()})

	<-slowSaveStarted

	for i := 2; i <= 50; i++ {
		w.Update(ClientState{
			SocketPath:   "/tmp/x.sock",
			SessionID:    [16]byte{byte(i)},
			LastSequence: uint64(i),
			WrittenAt:    time.Now(),
		})
	}

	close(slowSaveCanFinish)

	w.Close()

	got := saveCount.Load()
	if got > 2 {
		t.Errorf("expected at most 2 saves (initial + coalesced follow-up), got %d", got)
	}
	if got < 2 {
		t.Errorf("expected the follow-up save to run after slow save released, got only %d", got)
	}

	state, err := Load(path, "/tmp/x.sock")
	if err != nil || state == nil {
		t.Fatalf("Load: state=%v err=%v", state, err)
	}
	if state.LastSequence != 50 {
		t.Errorf("expected coalesced LastSequence=50, got %d", state.LastSequence)
	}
}

// TestStaleSessionWipeAndReplace verifies the disk-side mechanic of
// Plan D's stale-session recovery: a stale ClientState file is
// loadable, can be wiped cleanly, and a fresh state can be written
// over the wiped path without leftover artifacts.
//
// The actual server-side rejection (simple.Connect returning err on
// an unknown sessionID) is exercised by manual e2e in Task 20;
// here we just lock the disk-layer contract.
func TestStaleSessionWipeAndReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	socketPath := "/tmp/test-stale.sock"

	// Seed a stale state file (pretend a previous run left it behind
	// pointing at a session the server now rejects).
	stale := ClientState{
		SocketPath:    socketPath,
		SessionID:     [16]byte{0xde, 0xad, 0xbe, 0xef},
		LastSequence:  99,
		WrittenAt:     time.Now().UTC(),
		PaneViewports: nil,
	}
	if err := Save(path, &stale); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	// Step 1: Load returns the stale state successfully — the file
	// is well-formed; the rejection comes from the server side.
	loaded, err := Load(path, socketPath)
	if err != nil || loaded == nil {
		t.Fatalf("expected stale state to load, got err=%v state=%v", err, loaded)
	}
	if loaded.SessionID != stale.SessionID {
		t.Errorf("loaded SessionID mismatch")
	}

	// Step 2: Wipe removes the file (the recovery path in app.go
	// calls this immediately after observing the connect rejection).
	if err := Wipe(path); err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file removed after Wipe, stat err=%v", err)
	}

	// Step 3: A fresh state writes cleanly over the wiped path.
	fresh := ClientState{
		SocketPath:   socketPath,
		SessionID:    [16]byte{0xfe, 0xed, 0xfa, 0xce},
		LastSequence: 0,
		WrittenAt:    time.Now().UTC(),
	}
	if err := Save(path, &fresh); err != nil {
		t.Fatalf("post-wipe Save: %v", err)
	}
	got, err := Load(path, socketPath)
	if err != nil || got == nil {
		t.Fatalf("post-wipe Load: state=%v err=%v", got, err)
	}
	if got.SessionID == stale.SessionID {
		t.Errorf("post-wipe state still holds stale sessionID")
	}
	if got.SessionID != fresh.SessionID {
		t.Errorf("post-wipe state SessionID mismatch")
	}
}
