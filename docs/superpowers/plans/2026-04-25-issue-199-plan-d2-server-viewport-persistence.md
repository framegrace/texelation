# Issue #199 Plan D2 — Server-Side Cross-Daemon-Restart Viewport Persistence

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist per-session pane viewport state on the server so a daemon restart followed by a client resume lands at the saved scroll position.

**Architecture:** Lazy rehydrate-on-resume keyed by sessionID. New on-disk store at `<snapshot-dir>/sessions/<hex-sessionID>.json`. Extract a shared `DebouncedAtomicJSONStore` primitive from Plan D's client `Writer` and use it for both client- and server-side persistence. Cross-restart sequence/revision continuity handled by client-side reset on the post-resume `MsgTreeSnapshot` (gated by a one-shot resume flag).

**Tech Stack:** Go 1.24.3, JSON encoding, atomic temp+rename, `time.AfterFunc` debounce, generic over snapshot type via `encoding/json`.

**Spec:** `docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md`

**Branch:** `feature/issue-199-plan-d2-server-viewport-persistence`

---

## Phase 1: Extract `DebouncedAtomicJSONStore` primitive (Tasks 1–4)

**Why first:** D2's session writer is the second consumer of debounced atomic JSON persistence. Extracting now means D2 doesn't introduce a third near-duplicate writer. Tasks 1–4 produce the primitive and refactor Plan D's client `Writer` onto it; only after that does D2-specific code start landing.

### Task 1: Create `atomicjson` package skeleton with `Save`/`Load`/`Wipe`

**Files:**
- Create: `internal/persistence/atomicjson/store.go`
- Test: `internal/persistence/atomicjson/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/persistence/atomicjson/store_test.go
package atomicjson

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakePayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.json")
	in := &fakePayload{Name: "alpha", Count: 7}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out == nil || out.Name != "alpha" || out.Count != 7 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestLoadMissingReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	out, err := Load[fakePayload](filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil for missing file, got %+v", out)
	}
}

func TestLoadCorruptReturnsNilNilAndDeletes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil on parse error, got %+v", out)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file wiped after parse error, got stat=%v", err)
	}
}

func TestWipeIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := Wipe(path); err != nil {
		t.Fatalf("Wipe missing: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Wipe(path); err != nil {
		t.Fatalf("Wipe existing: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/persistence/atomicjson/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/persistence/atomicjson/store.go
//
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package atomicjson provides a small set of helpers for persisting
// "latest-wins" snapshot state to disk: atomic temp+rename writes,
// crash-safe loads with corrupt-file recovery, and a debounced writer
// (see Store) shared between the client (Plan D) and server (Plan D2)
// session-state persistence layers.
//
// This is NOT an event journal. State is overwritten in place; there
// is no replay. Use the existing terminal write_ahead_log.go for
// append-only data.

package atomicjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Save writes v to path atomically: encode to a sibling .tmp file,
// fsync-by-rename. A crash mid-write leaves either the previous
// contents or the new file, never partial data.
func Save[T any](path string, v *T) error {
	if v == nil {
		return errors.New("atomicjson: nil payload")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("atomicjson: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".atomicjson.tmp-*")
	if err != nil {
		return fmt.Errorf("atomicjson: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("atomicjson: temp cleanup failed: %v", err)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("atomicjson: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomicjson: close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("atomicjson: rename: %w", err)
	}
	return nil
}

// Load reads a JSON-encoded T from path. Returns:
//   - (nil, nil) if path is missing.
//   - (nil, nil) if path exists but parse fails — the corrupt file is
//     deleted (logged) so the next save replaces it cleanly. Project has
//     no back-compat constraint; auto-migration is explicitly out of scope.
//   - (v, nil) on success.
//   - (nil, err) only on disk-level errors that prevent recovery.
func Load[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("atomicjson: read: %w", err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		if werr := Wipe(path); werr != nil {
			log.Printf("atomicjson: parse failed (%v); wipe also failed (%v)", err, werr)
		} else {
			log.Printf("atomicjson: parse failed (%v); file wiped", err)
		}
		return nil, nil
	}
	return &v, nil
}

// Wipe removes path. Idempotent — missing-file is not an error.
func Wipe(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("atomicjson: wipe: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/persistence/atomicjson/ -run 'TestSave|TestLoad|TestWipe' -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/persistence/atomicjson/
git commit -m "Plan D2 task 1: atomicjson package with Save/Load/Wipe primitives"
```

---

### Task 2: Add debounced `Store` to `atomicjson`

**Files:**
- Modify: `internal/persistence/atomicjson/store.go`
- Modify: `internal/persistence/atomicjson/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `store_test.go`:

```go
import (
	"sync"
	"sync/atomic"
	"time"
)

func TestStoreCoalescesUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 50*time.Millisecond)
	defer st.Close()

	for i := 0; i < 10; i++ {
		st.Update(fakePayload{Name: "x", Count: i})
	}
	time.Sleep(150 * time.Millisecond)

	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out == nil || out.Count != 9 {
		t.Fatalf("expected last-wins Count=9, got %+v", out)
	}
}

func TestStoreFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 1*time.Hour)
	st.Update(fakePayload{Name: "y", Count: 42})
	st.Close()

	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out == nil || out.Count != 42 {
		t.Fatalf("Close did not flush: %+v", out)
	}
}

func TestStoreUpdateAfterCloseIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 50*time.Millisecond)
	st.Update(fakePayload{Name: "z", Count: 1})
	st.Close()
	st.Update(fakePayload{Name: "z", Count: 999})
	time.Sleep(100 * time.Millisecond)

	out, _ := Load[fakePayload](path)
	if out == nil || out.Count != 1 {
		t.Fatalf("post-Close Update leaked: %+v", out)
	}
}

func TestStoreSerializesSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 5*time.Millisecond)

	var inFlight atomic.Int32
	var maxParallel atomic.Int32
	st.SetSaverForTest(func(p string, v *fakePayload) error {
		n := inFlight.Add(1)
		if n > maxParallel.Load() {
			maxParallel.Store(n)
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		return Save(p, v)
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st.Update(fakePayload{Count: i})
		}(i)
	}
	wg.Wait()
	st.Close()

	if mp := maxParallel.Load(); mp > 1 {
		t.Fatalf("expected serialized saves, observed max parallel=%d", mp)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/persistence/atomicjson/ -run TestStore -v`
Expected: FAIL — `NewStore` undefined.

- [ ] **Step 3: Implement `Store`**

Append to `store.go`:

```go
import (
	"sync"
	"time"
)

// SaveFunc is the function Store invokes to persist payloads. Defaults
// to Save[T]; tests in the same package may inject alternatives via
// SetSaverForTest.
type SaveFunc[T any] func(path string, v *T) error

// Store is a debounced, latest-wins JSON writer. Concurrent calls to
// Update coalesce: the most recent payload at flush time wins, and the
// in-between values are discarded (intentional — this is snapshot state,
// not an event log).
//
// Concurrency model:
//   - mu protects state/timer/closed/lastSaveErr (lifecycle).
//   - saveMu serializes the actual disk write so tick and Flush can
//     never overlap.
//   - wg tracks tick/flush goroutines so Close blocks until all complete.
//
// Save errors are logged with per-error-string deduplication (one log
// per distinct error every 5 minutes, plus a recovery line on success
// after failure). Crash-loss is bounded by the debounce window.
type Store[T any] struct {
	path     string
	debounce time.Duration

	mu            sync.Mutex
	pending       *T
	timer         *time.Timer
	closed        bool
	lastSaveErr   string
	lastSaveErrAt time.Time

	saveMu sync.Mutex
	wg     sync.WaitGroup
	saver  SaveFunc[T]
}

// NewStore returns a Store that writes JSON-encoded T to path,
// debouncing successive Update calls by debounce.
func NewStore[T any](path string, debounce time.Duration) *Store[T] {
	return &Store[T]{
		path:     path,
		debounce: debounce,
		saver:    Save[T],
	}
}

// SetSaverForTest swaps the underlying save implementation. Only for
// in-package tests; production code uses the default Save[T].
func (s *Store[T]) SetSaverForTest(fn SaveFunc[T]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saver = fn
}

// Update schedules a debounced save with v as the new pending value.
// Calls after Close are silently dropped.
func (s *Store[T]) Update(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	cp := v
	s.pending = &cp
	if s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.tick)
	}
}

// Flush writes any pending value synchronously.
func (s *Store[T]) Flush() {
	s.wg.Add(1)
	defer s.wg.Done()

	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	v := s.pending
	s.pending = nil
	s.mu.Unlock()

	if v != nil {
		s.doSave(*v)
	}
}

// Close flushes any pending state, blocks for in-flight ticks, and
// rejects subsequent Update calls.
func (s *Store[T]) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.mu.Unlock()

	s.Flush()
	s.wg.Wait()
}

func (s *Store[T]) tick() {
	s.wg.Add(1)
	defer s.wg.Done()

	s.mu.Lock()
	if s.closed || s.pending == nil {
		s.timer = nil
		s.mu.Unlock()
		return
	}
	v := *s.pending
	s.pending = nil
	s.timer = nil
	s.mu.Unlock()

	s.doSave(v)

	s.mu.Lock()
	if s.pending != nil && !s.closed && s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.tick)
	}
	s.mu.Unlock()
}

const saveErrRelogInterval = 5 * time.Minute

func (s *Store[T]) doSave(v T) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	err := s.saver(s.path, &v)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		es := err.Error()
		now := time.Now()
		if es != s.lastSaveErr || now.Sub(s.lastSaveErrAt) >= saveErrRelogInterval {
			log.Printf("atomicjson: save failed (%v); will retry on next change", err)
			s.lastSaveErr = es
			s.lastSaveErrAt = now
		}
		return
	}
	if s.lastSaveErr != "" {
		log.Printf("atomicjson: save recovered after prior failure")
		s.lastSaveErr = ""
		s.lastSaveErrAt = time.Time{}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/persistence/atomicjson/ -race -v`
Expected: PASS for all tests, including `-race`.

- [ ] **Step 5: Commit**

```bash
git add internal/persistence/atomicjson/
git commit -m "Plan D2 task 2: debounced Store for atomic JSON persistence"
```

---

### Task 3: Refactor `internal/runtime/client/persistence.go` `Writer` onto `atomicjson.Store`

**Files:**
- Modify: `internal/runtime/client/persistence.go`

- [ ] **Step 1: Verify Plan D's existing tests are green before touching anything**

Run: `go test ./internal/runtime/client/ -run TestWriter -v -race`
Expected: PASS — establishes the pre-refactor baseline.

- [ ] **Step 2: Replace `Writer` body with a thin `atomicjson.Store` wrapper**

Locate the `Writer` type, `NewWriter`, `Update`, `Flush`, `Close`, `tick`, `doSave`, and the `saveErrRelogInterval` constant in `internal/runtime/client/persistence.go`. Replace all of them with this implementation, keeping the existing `Save`/`Load`/`Wipe`/`SaveFunc`/path-resolution code untouched:

```go
// Writer debounces saves of ClientState to a file. This is now a thin
// shim over atomicjson.Store[ClientState]; behavior is unchanged.
//
// Plan D shipped a bespoke implementation that has since been promoted
// to internal/persistence/atomicjson for reuse by Plan D2's server-side
// session writer. Existing call sites (NewWriter, Update, Flush, Close)
// continue to work exactly as before.
type Writer struct {
	store *atomicjson.Store[ClientState]
}

func NewWriter(filePath string, debounce time.Duration) *Writer {
	return &Writer{store: atomicjson.NewStore[ClientState](filePath, debounce)}
}

func (w *Writer) Update(s ClientState) { w.store.Update(s) }
func (w *Writer) Flush()               { w.store.Flush() }
func (w *Writer) Close()               { w.store.Close() }
```

Add the import:

```go
import (
	"github.com/framegrace/texelation/internal/persistence/atomicjson"
)
```

Remove the now-unused fields/types: the long `Writer` struct definition (mu/saveMu/timer/state/etc.), `tick`, `doSave`, `SaveFunc` type, `saveErrRelogInterval` constant. (Keep `Save`, `Load`, `Wipe`, `ResolvePath`, `validClientName`, `ValidateClientName`, `ErrInvalidClientName`, `decodeHex16`, `socketHash`, `ClientState` and its JSON shim.)

- [ ] **Step 3: Update `persistence_test.go` for any test that probed the removed internals**

Tests that referenced `Writer.saver`, `Writer.lastSaveErr`, or other internal fields directly need to migrate. If a test set the saver via field-assignment (`w.saver = ...`), replace that with the new injection point. The simplest path is to check whether such tests exist:

Run: `grep -nE 'w\.saver|w\.lastSave|w\.state\b|w\.timer\b' internal/runtime/client/persistence_test.go`

For each match, rewrite to use the public surface. If a test specifically exercised `Writer`'s internal serialization or error-dedup logic, delete it — that coverage is now in `internal/persistence/atomicjson/store_test.go`. If a test's intent was integration-style (set up a Writer, fire updates, observe disk), keep it, but use `atomicjson.Store`'s `SetSaverForTest` via a small helper if needed:

```go
// In persistence_test.go, if a test needs to inject a custom saver:
//   w := NewWriter(path, 50*time.Millisecond)
//   w.store.SetSaverForTest(func(p string, s *ClientState) error { ... })
// Make w.store accessible by lowercasing — already done above. If the
// test is in the same package, direct field access works.
```

If no internal-field references exist (the public surface was the only thing under test), no test changes are needed.

- [ ] **Step 4: Run all client persistence tests**

Run: `go test ./internal/runtime/client/ -race -v`
Expected: PASS — same suite as Step 1, now exercising the refactored writer.

- [ ] **Step 5: Run the broader client test suite to catch any consumer break**

Run: `go test ./internal/runtime/client/... ./client/... -race`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/client/persistence.go internal/runtime/client/persistence_test.go
git commit -m "Plan D2 task 3: refactor client Writer onto atomicjson.Store"
```

---

### Task 4: Confirm Plan D regression suite is green

**Files:** none (verification-only)

- [ ] **Step 1: Run Plan D's full integration suite**

Run: `go test ./internal/runtime/client/ ./internal/runtime/server/ -race -count=1`
Expected: PASS for all tests including `TestPersistedSession*`, viewport-resume integration tests, and existing Plan D `TestWriter*` cases.

- [ ] **Step 2: Manual smoke test of Plan D persistence path**

```bash
make build
mkdir -p /tmp/d2-smoke
TEXELATION_STATE_PATH=/tmp/d2-smoke ./bin/texelation start
# In another terminal:
./bin/texel-client --socket=/tmp/texelation.sock
# Scroll a pane up, exit client cleanly. Restart client. Verify pane scroll position survives.
./bin/texel-client --socket=/tmp/texelation.sock
./bin/texelation stop
```

Expected: scroll position survives client restart (Plan D behavior, unchanged).

- [ ] **Step 3: No commit (verification step).**

---

## Phase 2: Server-side `StoredSession` schema and disk I/O (Tasks 5–6)

### Task 5: Define `StoredSession` schema with hex `[16]byte` JSON marshaling

**Files:**
- Create: `internal/runtime/server/session_persistence.go`
- Test: `internal/runtime/server/session_persistence_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
	if !strings.Contains(string(data), `"paneID":"aabb00000000000000000000000000000"`[:42]) {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/server/ -run TestStoredSessionRoundTrip -v`
Expected: FAIL — `StoredSession` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/runtime/server/session_persistence.go
//
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Server-side cross-daemon-restart session/viewport persistence.
// One file per session at <basedir>/sessions/<hex-sessionID>.json.
// See docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md.

package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StoredSessionSchemaVersion is the on-disk format version. Bump on
// incompatible changes; older files are deleted on boot scan with a log
// line (project has no back-compat constraint).
const StoredSessionSchemaVersion = 1

// StoredSession is the on-disk representation of cross-restart session
// state. Latest-wins snapshot — there is no replay log.
type StoredSession struct {
	SchemaVersion  int                  `json:"-"` // shadowed by jsonShape
	SessionID      [16]byte             `json:"-"`
	LastActive     time.Time            `json:"-"`
	Pinned         bool                 `json:"-"`
	PaneViewports  []StoredPaneViewport `json:"-"`
	Label          string               `json:"-"`
	PaneCount      int                  `json:"-"`
	FirstPaneTitle string               `json:"-"`
}

// StoredPaneViewport mirrors protocol.PaneViewportState plus a Rows/Cols
// pair (matching what's stored on the wire and on the client today).
type StoredPaneViewport struct {
	PaneID         [16]byte `json:"-"`
	AltScreen      bool     `json:"altScreen"`
	AutoFollow     bool     `json:"autoFollow"`
	ViewBottomIdx  int64    `json:"viewBottomIdx"`
	WrapSegmentIdx uint16   `json:"wrapSegmentIdx"`
	Rows           uint16   `json:"rows"`
	Cols           uint16   `json:"cols"`
}

type sessionJSONShape struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	SessionID      string                    `json:"sessionID"`
	LastActive     time.Time                 `json:"lastActive"`
	Pinned         bool                      `json:"pinned"`
	PaneViewports  []paneViewportJSONShape   `json:"paneViewports"`
	Label          string                    `json:"label"`
	PaneCount      int                       `json:"paneCount"`
	FirstPaneTitle string                    `json:"firstPaneTitle"`
}

type paneViewportJSONShape struct {
	PaneID         string `json:"paneID"`
	AltScreen      bool   `json:"altScreen"`
	AutoFollow     bool   `json:"autoFollow"`
	ViewBottomIdx  int64  `json:"viewBottomIdx"`
	WrapSegmentIdx uint16 `json:"wrapSegmentIdx"`
	Rows           uint16 `json:"rows"`
	Cols           uint16 `json:"cols"`
}

func (s StoredSession) MarshalJSON() ([]byte, error) {
	out := sessionJSONShape{
		SchemaVersion:  s.SchemaVersion,
		SessionID:      hex.EncodeToString(s.SessionID[:]),
		LastActive:     s.LastActive,
		Pinned:         s.Pinned,
		Label:          s.Label,
		PaneCount:      s.PaneCount,
		FirstPaneTitle: s.FirstPaneTitle,
	}
	out.PaneViewports = make([]paneViewportJSONShape, len(s.PaneViewports))
	for i, p := range s.PaneViewports {
		out.PaneViewports[i] = paneViewportJSONShape{
			PaneID:         hex.EncodeToString(p.PaneID[:]),
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			Rows:           p.Rows,
			Cols:           p.Cols,
		}
	}
	return json.Marshal(&out)
}

func (s *StoredSession) UnmarshalJSON(data []byte) error {
	var in sessionJSONShape
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	s.SchemaVersion = in.SchemaVersion
	if err := decodeHex16Session(in.SessionID, &s.SessionID); err != nil {
		return fmt.Errorf("sessionID: %w", err)
	}
	s.LastActive = in.LastActive
	s.Pinned = in.Pinned
	s.Label = in.Label
	s.PaneCount = in.PaneCount
	s.FirstPaneTitle = in.FirstPaneTitle
	s.PaneViewports = make([]StoredPaneViewport, len(in.PaneViewports))
	for i, p := range in.PaneViewports {
		var pid [16]byte
		if err := decodeHex16Session(p.PaneID, &pid); err != nil {
			return fmt.Errorf("paneViewports[%d].paneID: %w", i, err)
		}
		s.PaneViewports[i] = StoredPaneViewport{
			PaneID:         pid,
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			Rows:           p.Rows,
			Cols:           p.Cols,
		}
	}
	return nil
}

func decodeHex16Session(s string, out *[16]byte) error {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if len(b) != 16 {
		return fmt.Errorf("expected 16 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/server/ -run TestStoredSessionRoundTrip -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/session_persistence.go internal/runtime/server/session_persistence_test.go
git commit -m "Plan D2 task 5: StoredSession schema with hex JSON marshaling"
```

---

### Task 6: Add path resolver and disk-scan helpers for the session directory

**Files:**
- Modify: `internal/runtime/server/session_persistence.go`
- Modify: `internal/runtime/server/session_persistence_test.go`

- [ ] **Step 1: Write the failing test**

Append to `session_persistence_test.go`:

```go
import (
	"os"
	"path/filepath"
)

func TestSessionFilePath(t *testing.T) {
	got := SessionFilePath("/var/lib/texel", [16]byte{0x12, 0x34, 0xab})
	want := "/var/lib/texel/sessions/1234ab0000000000000000000000000000.json"[:len("/var/lib/texel/sessions/")] + "1234ab0000000000000000000000000000.json"
	// Trim because line above is awkward; just check the suffix exactly.
	if filepath.Dir(got) != "/var/lib/texel/sessions" {
		t.Fatalf("expected dir /var/lib/texel/sessions, got %s", filepath.Dir(got))
	}
	if filepath.Base(got) != "1234ab0000000000000000000000000000.json" {
		t.Fatalf("expected hex filename, got %s", filepath.Base(got))
	}
	_ = want
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
	// Corrupt file
	corruptPath := filepath.Join(sessDir, "deadbeef00000000000000000000000000.json")
	if err := os.WriteFile(corruptPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Schema mismatch
	mismatch := StoredSession{SchemaVersion: 999, SessionID: [16]byte{0x02}, LastActive: time.Now()}
	mismatchPath := SessionFilePath(dir, mismatch.SessionID)
	mdata, _ := json.Marshal(&mismatch)
	if err := os.WriteFile(mismatchPath, mdata, 0o600); err != nil {
		t.Fatal(err)
	}
	// Non-JSON file (e.g. README) — should be ignored, not deleted
	readme := filepath.Join(sessDir, "README.txt")
	if err := os.WriteFile(readme, []byte("hello"), 0o600); err != nil {
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
	// Corrupt and mismatch files should be deleted.
	if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
		t.Fatalf("expected corrupt file deleted, stat=%v", err)
	}
	if _, err := os.Stat(mismatchPath); !os.IsNotExist(err) {
		t.Fatalf("expected schema-mismatch file deleted, stat=%v", err)
	}
	// Non-JSON file is left alone.
	if _, err := os.Stat(readme); err != nil {
		t.Fatalf("non-JSON file should be untouched, stat=%v", err)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/server/ -run 'TestSessionFilePath|TestScanSessionsDir' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement the helpers**

Append to `session_persistence.go`:

```go
import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/framegrace/texelation/internal/persistence/atomicjson"
)

// SessionsDirName is the leaf directory under <basedir> that holds
// per-session files.
const SessionsDirName = "sessions"

// SessionFilePath returns the on-disk path for sessionID under basedir.
func SessionFilePath(basedir string, id [16]byte) string {
	return filepath.Join(basedir, SessionsDirName, hex.EncodeToString(id[:])+".json")
}

// ScanSessionsDir reads <basedir>/sessions/, parses each *.json file,
// and returns a map keyed by SessionID. Files that fail to parse, that
// declare an unknown SchemaVersion, or whose filename hex does not match
// the contents are deleted (logged). Non-JSON files (anything not
// matching the *.json extension) are left untouched. Missing directory
// is not an error — returns an empty map.
func ScanSessionsDir(basedir string) (map[[16]byte]*StoredSession, error) {
	out := make(map[[16]byte]*StoredSession)
	dir := filepath.Join(basedir, SessionsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("server: scan sessions dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		s, lerr := atomicjson.Load[StoredSession](path)
		if lerr != nil {
			log.Printf("server: session scan: load %s: %v", path, lerr)
			continue
		}
		if s == nil {
			// atomicjson.Load already deleted a corrupt file; nothing more to do.
			continue
		}
		if s.SchemaVersion != StoredSessionSchemaVersion {
			log.Printf("server: session scan: %s schema=%d wanted=%d; deleting",
				path, s.SchemaVersion, StoredSessionSchemaVersion)
			if werr := atomicjson.Wipe(path); werr != nil {
				log.Printf("server: session scan: wipe failed: %v", werr)
			}
			continue
		}
		// Sanity check filename matches contents.
		expectedName := hex.EncodeToString(s.SessionID[:]) + ".json"
		if name != expectedName {
			log.Printf("server: session scan: filename %s does not match sessionID %s; deleting",
				name, expectedName)
			if werr := atomicjson.Wipe(path); werr != nil {
				log.Printf("server: session scan: wipe failed: %v", werr)
			}
			continue
		}
		out[s.SessionID] = s
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run 'TestSessionFilePath|TestScanSessionsDir|TestStoredSession' -v`
Expected: PASS for all three groups.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/session_persistence.go internal/runtime/server/session_persistence_test.go
git commit -m "Plan D2 task 6: SessionFilePath + ScanSessionsDir helpers"
```

---

## Phase 3: Manager rehydration index and boot scan (Tasks 7–8)

### Task 7: Add `Manager.persistedSessions` index and `LookupOrRehydrate`

**Files:**
- Modify: `internal/runtime/server/manager.go`
- Test: `internal/runtime/server/manager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `manager_test.go`:

```go
import (
	"time"
)

func TestManagerLookupOrRehydrate_LiveSessionWins(t *testing.T) {
	mgr := NewManager()
	live, err := mgr.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{
		live.ID(): {SessionID: live.ID()}, // shadowed; live should win
	})
	got, err := mgr.LookupOrRehydrate(live.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got != live {
		t.Fatalf("expected live session, got different instance")
	}
}

func TestManagerLookupOrRehydrate_RehydratesFromDisk(t *testing.T) {
	mgr := NewManager()
	id := [16]byte{0xab}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now(),
		PaneViewports: []StoredPaneViewport{{
			PaneID:        [16]byte{0x11},
			ViewBottomIdx: 555,
			Rows:          24,
			Cols:          80,
		}},
	}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{id: stored})

	sess, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("LookupOrRehydrate: %v", err)
	}
	if sess.ID() != id {
		t.Fatalf("rehydrated session ID mismatch: %x", sess.ID())
	}
	// Pre-seeded viewport must be present.
	vp, ok := sess.Viewport([16]byte{0x11})
	if !ok {
		t.Fatalf("expected pre-seeded viewport from disk")
	}
	if vp.ViewBottomIdx != 555 {
		t.Fatalf("expected ViewBottomIdx=555, got %d", vp.ViewBottomIdx)
	}
	// After rehydration, the disk-side index entry is consumed.
	if _, err := mgr.LookupOrRehydrate(id); err != nil {
		t.Fatalf("second lookup after rehydration must hit live cache, got %v", err)
	}
}

func TestManagerLookupOrRehydrate_UnknownReturnsErr(t *testing.T) {
	mgr := NewManager()
	if _, err := mgr.LookupOrRehydrate([16]byte{0xff}); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/server/ -run TestManagerLookupOrRehydrate -v`
Expected: FAIL — `SetPersistedSessions`/`LookupOrRehydrate` undefined.

- [ ] **Step 3: Implement on `Manager`**

Modify `internal/runtime/server/manager.go`:

```go
// Manager tracks active sessions and coordinates creation/lookup.
type Manager struct {
	mu                sync.RWMutex
	sessions          map[[16]byte]*Session
	persistedSessions map[[16]byte]*StoredSession // populated at boot scan; consumed on first resume
	maxDiffs          int
}

func NewManager() *Manager {
	return &Manager{
		sessions:          make(map[[16]byte]*Session),
		persistedSessions: make(map[[16]byte]*StoredSession),
		maxDiffs:          512,
	}
}
```

Add the new methods (place after `Lookup`):

```go
// SetPersistedSessions seeds the rehydration index. Typically called
// once at boot from server_boot.go after ScanSessionsDir runs. Replaces
// any prior index — callers should pass the full result of the scan.
func (m *Manager) SetPersistedSessions(loaded map[[16]byte]*StoredSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistedSessions = make(map[[16]byte]*StoredSession, len(loaded))
	for id, s := range loaded {
		m.persistedSessions[id] = s
	}
}

// LookupOrRehydrate returns an existing live Session, or rehydrates
// one from the persisted index if present. The persisted entry is
// consumed (removed from the index) on rehydration; subsequent writes
// flow through the live Session's writer. Returns ErrSessionNotFound
// when the ID is unknown to both live and persisted maps.
func (m *Manager) LookupOrRehydrate(id [16]byte) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	stored, ok := m.persistedSessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	delete(m.persistedSessions, id)
	sess := NewSession(id, m.maxDiffs)
	// Pre-seed viewports from disk so the publisher has a clip window
	// even before the client's MsgResumeRequest arrives. The client's
	// fresher PaneViewports overwrite these via Session.ApplyResume.
	preSeedViewports(sess, stored.PaneViewports)
	m.sessions[id] = sess
	return sess, nil
}

// preSeedViewports populates Session.viewports from a list of stored
// entries. Defined as a free function so tests can call it without a
// resume payload round-trip.
func preSeedViewports(sess *Session, vps []StoredPaneViewport) {
	for _, p := range vps {
		top := p.ViewBottomIdx - int64(p.Rows) + 1
		bottom := p.ViewBottomIdx
		if top > p.ViewBottomIdx { // overflow guard, mirrors ClientViewports.ApplyResume
			top, bottom = 0, 0
		} else if top < 0 {
			top = 0
		}
		sess.viewports.byPaneID[p.PaneID] = ClientViewport{
			AltScreen:     p.AltScreen,
			ViewTopIdx:    top,
			ViewBottomIdx: bottom,
			Rows:          p.Rows,
			Cols:          p.Cols,
			AutoFollow:    p.AutoFollow,
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run TestManagerLookupOrRehydrate -v -race`
Expected: PASS for all three subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/manager.go internal/runtime/server/manager_test.go
git commit -m "Plan D2 task 7: Manager.LookupOrRehydrate from persisted index"
```

---

### Task 8: Boot-scan integration in `server_boot.go` and CLI plumbing

**Files:**
- Modify: `internal/runtime/server/server.go` (add `basedir` field)
- Modify: `internal/runtime/server/server_boot.go` (call `ScanSessionsDir` + `SetPersistedSessions`)
- Modify: `cmd/texel-server/main.go` (derive basedir from `--snapshot` flag's parent directory)

- [ ] **Step 1: Inspect current `Server` struct**

Run: `grep -n "type Server struct\|snapshotStore\|basedir" internal/runtime/server/server.go | head -20`

This shows where to insert the `basedir` field. Note the field name used for the snapshot store path; the basedir is the parent directory.

- [ ] **Step 2: Write the failing test**

Create or append to `internal/runtime/server/server_boot_test.go`:

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPersistedSessionsAtBoot(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x99, 0x88}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		PaneViewports: []StoredPaneViewport{{PaneID: [16]byte{0x77}, ViewBottomIdx: 9000, Rows: 24, Cols: 80}},
	}
	path := SessionFilePath(dir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(&stored)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	if err := LoadPersistedSessions(mgr, dir); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	sess, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("rehydrate after boot scan: %v", err)
	}
	if sess.ID() != id {
		t.Fatalf("rehydrated wrong session: %x", sess.ID())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/server/ -run TestLoadPersistedSessionsAtBoot -v`
Expected: FAIL — `LoadPersistedSessions` undefined.

- [ ] **Step 4: Implement the boot-scan helper**

Append to `internal/runtime/server/server_boot.go`:

```go
// LoadPersistedSessions runs ScanSessionsDir against basedir and seeds
// the manager's rehydration index. Failure to scan (e.g., disk error)
// is non-fatal — the server boots without rehydration support and
// future MsgResumeRequest for unknown IDs falls through to
// ErrSessionNotFound, exactly as before Plan D2.
func LoadPersistedSessions(mgr *Manager, basedir string) error {
	if basedir == "" {
		return nil
	}
	loaded, err := ScanSessionsDir(basedir)
	if err != nil {
		log.Printf("server: persisted session scan failed: %v", err)
		return err
	}
	mgr.SetPersistedSessions(loaded)
	if len(loaded) > 0 {
		log.Printf("[BOOT] loaded %d persisted session(s) from %s", len(loaded), basedir)
	}
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/runtime/server/ -run TestLoadPersistedSessionsAtBoot -v`
Expected: PASS.

- [ ] **Step 6: Wire `LoadPersistedSessions` into `cmd/texel-server/main.go`**

In `cmd/texel-server/main.go`, after the snapshot path is resolved (around lines 194 and 234, both branches), derive the basedir as `filepath.Dir(snapPath)` and call `server.LoadPersistedSessions(mgr, basedir)`.

Locate the existing snapshot wiring. The pattern looks like:

```go
snapPath := *snapshotPath
if snapPath == "" {
    // ...derive default...
}
store := server.NewSnapshotStore(snapPath)
// (existing wiring)
```

Immediately after `store := server.NewSnapshotStore(snapPath)` in **both** branches, add:

```go
if err := server.LoadPersistedSessions(mgr, filepath.Dir(snapPath)); err != nil {
    log.Printf("warning: could not load persisted sessions: %v", err)
}
```

Where `mgr` is the `*server.Manager` instance. If `mgr` doesn't exist as a local variable yet at that point, locate where `Manager` is constructed in the boot path and call `LoadPersistedSessions` immediately after construction (the call must happen before the listener accepts connections).

- [ ] **Step 7: Verify the binary builds and existing tests pass**

Run: `make build && go test ./internal/runtime/server/ -race -count=1`
Expected: build succeeds; no test regressions.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/server/server_boot.go internal/runtime/server/server_boot_test.go cmd/texel-server/main.go
git commit -m "Plan D2 task 8: boot scan loads persisted sessions into rehydration index"
```

---

## Phase 4: Wire `Session` writer (Tasks 9–11)

### Task 9: Attach a `*atomicjson.Store[StoredSession]` to `Session`

**Files:**
- Modify: `internal/runtime/server/session.go`
- Test: `internal/runtime/server/session_test.go`

- [ ] **Step 1: Write the failing test**

Append to `session_test.go`:

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

func TestSessionWriterPersistsViewportUpdate(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xde, 0xad}
	sess := NewSession(id, 100)
	sess.AttachWriter(SessionFilePath(dir, id), 25*time.Millisecond)
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        [16]byte{0xaa},
		ViewBottomIdx: 12345,
		ViewportRows:  24,
		ViewportCols:  80,
	})

	// Wait for debounce to flush.
	time.Sleep(80 * time.Millisecond)

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var got StoredSession
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != id {
		t.Fatalf("expected sessionID %x, got %x", id, got.SessionID)
	}
	if got.SchemaVersion != StoredSessionSchemaVersion {
		t.Fatalf("schema version: got %d", got.SchemaVersion)
	}
	if len(got.PaneViewports) != 1 || got.PaneViewports[0].ViewBottomIdx != 12345 {
		t.Fatalf("pane viewports mismatch: %+v", got.PaneViewports)
	}
	if got.LastActive.IsZero() {
		t.Fatalf("LastActive should be set")
	}
	_ = filepath.Dir // silence unused
}

func TestSessionWriterCloseFlushes(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xbe, 0xef}
	sess := NewSession(id, 100)
	sess.AttachWriter(SessionFilePath(dir, id), 1*time.Hour) // long debounce
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{PaneID: [16]byte{0x01}, ViewBottomIdx: 1, ViewportRows: 1, ViewportCols: 1})
	sess.Close() // must flush

	if _, err := os.Stat(SessionFilePath(dir, id)); err != nil {
		t.Fatalf("Close did not flush: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/server/ -run TestSessionWriter -v`
Expected: FAIL — `AttachWriter` undefined.

- [ ] **Step 3: Implement on `Session`**

Modify `internal/runtime/server/session.go`:

Add to imports: `"github.com/framegrace/texelation/internal/persistence/atomicjson"`.

Add a `writer` field to `Session`:

```go
type Session struct {
	id             [16]byte
	mu             sync.Mutex
	nextSequence   uint64
	diffs          []DiffPacket
	lastSnapshot   time.Time
	closed         bool
	maxDiffs       int
	droppedDiffs   uint64
	lastDroppedSeq uint64
	viewports      *ClientViewports
	revisionsMu    sync.Mutex
	revisions      map[[16]byte]uint32

	// Plan D2: cross-restart persistence. Nil-safe: when nil (no
	// disk path resolved), all hook calls are no-ops.
	writer    *atomicjson.Store[StoredSession]
	storedMu  sync.Mutex
	storedMeta storedMeta // updated by RecordPaneActivity, written through writer
}

// storedMeta is the in-memory mirror of the Plan F session-level metadata
// fields. Held alongside viewports and updated via dedicated hooks so the
// writer can build a complete StoredSession on each Update.
type storedMeta struct {
	pinned         bool
	label          string
	paneCount      int
	firstPaneTitle string
}
```

Add the methods (place after `NewSession`):

```go
// AttachWriter wires up cross-restart persistence for this session. May
// be called once at session creation (for fresh sessions) or after
// rehydration (for sessions reconstructed from disk via Manager). Safe
// to call before any Apply*/Enqueue* hooks.
func (s *Session) AttachWriter(path string, debounce time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil {
		return
	}
	s.writer = atomicjson.NewStore[StoredSession](path, debounce)
}

// schedulePersist builds a StoredSession snapshot from current state
// and hands it to the writer. Must be called outside s.mu (the writer's
// internal mutex orders against its own state) but is itself safe to
// call from any goroutine.
func (s *Session) schedulePersist() {
	if s.writer == nil {
		return
	}
	s.storedMu.Lock()
	meta := s.storedMeta
	s.storedMu.Unlock()

	vps := s.viewports.Snapshot()
	stored := StoredSession{
		SchemaVersion:  StoredSessionSchemaVersion,
		SessionID:      s.id,
		LastActive:     time.Now().UTC(),
		Pinned:         meta.pinned,
		Label:          meta.label,
		PaneCount:      meta.paneCount,
		FirstPaneTitle: meta.firstPaneTitle,
		PaneViewports:  make([]StoredPaneViewport, 0, len(vps)),
	}
	for paneID, v := range vps {
		stored.PaneViewports = append(stored.PaneViewports, StoredPaneViewport{
			PaneID:        paneID,
			AltScreen:     v.AltScreen,
			AutoFollow:    v.AutoFollow,
			ViewBottomIdx: v.ViewBottomIdx,
			Rows:          v.Rows,
			Cols:          v.Cols,
		})
	}
	s.writer.Update(stored)
}
```

Hook into `ApplyViewportUpdate`, `ApplyResume`, and `EnqueueDiff`/`EnqueueImage`. For `Apply*` methods, replace:

```go
func (s *Session) ApplyViewportUpdate(u protocol.ViewportUpdate) {
	s.viewports.Apply(u)
	s.schedulePersist()
}

func (s *Session) ApplyResume(states []protocol.PaneViewportState, paneExists func(id [16]byte) bool) {
	s.viewports.ApplyResume(states, paneExists)
	s.schedulePersist()
}
```

For `EnqueueDiff` and `EnqueueImage`, after the existing body's success path, add:

```go
	// schedulePersist outside the diff-queue lock; only LastActive
	// changes from this code path (no viewport mutation), but the
	// writer debounces so frequent enqueues coalesce.
	s.schedulePersist()
```

(Place the call just before `return nil` and outside `s.mu`. Refactor by capturing the success in a local boolean if needed, e.g.:)

```go
func (s *Session) EnqueueDiff(delta protocol.BufferDelta) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	payload, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	seq := s.nextSequence + 1
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgBufferDelta,
		Flags:     protocol.FlagChecksum,
		SessionID: s.id,
		Sequence:  seq,
	}
	s.diffs = append(s.diffs, DiffPacket{Sequence: seq, Payload: payload, Message: hdr})
	s.nextSequence = seq
	if s.maxDiffs > 0 && len(s.diffs) > s.maxDiffs {
		drop := len(s.diffs) - s.maxDiffs
		s.recordDrop(drop)
		s.diffs = append([]DiffPacket(nil), s.diffs[drop:]...)
	}
	s.mu.Unlock()
	s.schedulePersist()
	return nil
}
```

Apply the same pattern to `EnqueueImage`.

Modify `Close` to flush and shut down the writer:

```go
func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	s.diffs = nil
	w := s.writer
	s.writer = nil
	s.mu.Unlock()
	if w != nil {
		w.Close() // flushes pending state synchronously
	}
}
```

Add `RecordPaneActivity` for Plan F metadata (called from Task 12):

```go
// RecordPaneActivity updates the session-level pane metadata used by
// Plan F's session-discovery picker. Triggers a debounced write.
func (s *Session) RecordPaneActivity(paneCount int, firstPaneTitle string) {
	s.storedMu.Lock()
	s.storedMeta.paneCount = paneCount
	s.storedMeta.firstPaneTitle = firstPaneTitle
	s.storedMu.Unlock()
	s.schedulePersist()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run TestSessionWriter -race -v`
Expected: PASS for both `TestSessionWriterPersistsViewportUpdate` and `TestSessionWriterCloseFlushes`.

- [ ] **Step 5: Verify no existing test regressed**

Run: `go test ./internal/runtime/server/ -race -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/session.go internal/runtime/server/session_test.go
git commit -m "Plan D2 task 9: attach atomicjson writer to Session for cross-restart persistence"
```

---

### Task 10: Wire `AttachWriter` into the session creation paths

**Files:**
- Modify: `internal/runtime/server/manager.go`

- [ ] **Step 1: Write the failing test**

Append to `manager_test.go`:

```go
func TestManagerNewSessionAttachesWriter(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	mgr.SetPersistencePath(dir, 25*time.Millisecond)

	sess, err := mgr.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xaa}, ViewBottomIdx: 1, ViewportRows: 1, ViewportCols: 1,
	})
	time.Sleep(80 * time.Millisecond)

	if _, err := os.Stat(SessionFilePath(dir, sess.ID())); err != nil {
		t.Fatalf("expected session file written, got stat=%v", err)
	}
}

func TestManagerLookupOrRehydrate_AttachesWriter(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	mgr.SetPersistencePath(dir, 25*time.Millisecond)

	id := [16]byte{0xab}
	stored := &StoredSession{SchemaVersion: StoredSessionSchemaVersion, SessionID: id, LastActive: time.Now()}
	mgr.SetPersistedSessions(map[[16]byte]*StoredSession{id: stored})

	sess, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xbb}, ViewBottomIdx: 99, ViewportRows: 1, ViewportCols: 1,
	})
	time.Sleep(80 * time.Millisecond)
	if _, err := os.Stat(SessionFilePath(dir, id)); err != nil {
		t.Fatalf("rehydrated session not persisting: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/server/ -run 'TestManagerNewSessionAttachesWriter|TestManagerLookupOrRehydrate_AttachesWriter' -v`
Expected: FAIL — `SetPersistencePath` undefined.

- [ ] **Step 3: Implement on `Manager`**

Modify `manager.go`:

```go
type Manager struct {
	mu                sync.RWMutex
	sessions          map[[16]byte]*Session
	persistedSessions map[[16]byte]*StoredSession
	maxDiffs          int

	// Plan D2: when set, every new or rehydrated Session attaches an
	// atomicjson writer at <persistBasedir>/sessions/<hex-id>.json.
	persistBasedir   string
	persistDebounce  time.Duration
}

// SetPersistencePath enables cross-restart session persistence. Path is
// the basedir (parent of "sessions/"); debounce typically 250ms in prod
// and 25ms in tests. Idempotent — subsequent calls update the path for
// future sessions only; in-flight writers continue using their existing
// paths.
func (m *Manager) SetPersistencePath(basedir string, debounce time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistBasedir = basedir
	m.persistDebounce = debounce
}
```

In `NewSession`, after `m.sessions[id] = session`, attach the writer:

```go
func (m *Manager) NewSession() (*Session, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}

	m.mu.Lock()
	session := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		session.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	m.sessions[id] = session
	m.mu.Unlock()
	return session, nil
}
```

In `LookupOrRehydrate`, after the rehydrated session is registered, attach the writer:

```go
func (m *Manager) LookupOrRehydrate(id [16]byte) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	stored, ok := m.persistedSessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	delete(m.persistedSessions, id)
	sess := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		sess.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	preSeedViewports(sess, stored.PaneViewports)
	m.sessions[id] = sess
	return sess, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run 'TestManager.*Writer' -race -v`
Expected: PASS.

- [ ] **Step 5: Wire into `cmd/texel-server/main.go`**

In both branches where `LoadPersistedSessions(mgr, filepath.Dir(snapPath))` is called (Task 8 added these), immediately follow with:

```go
mgr.SetPersistencePath(filepath.Dir(snapPath), 250*time.Millisecond)
```

- [ ] **Step 6: Verify the build**

Run: `make build && go test ./internal/runtime/server/ -race -count=1`
Expected: build succeeds, suite green.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/server/manager.go internal/runtime/server/manager_test.go cmd/texel-server/main.go
git commit -m "Plan D2 task 10: Manager attaches writers to new and rehydrated sessions"
```

---

### Task 11: Hook pane-tree metadata into the writer (Plan F preparation)

**Files:**
- Modify: `internal/runtime/server/desktop_publisher.go` (or wherever pane-tree changes are observed; locate via grep)
- Test: `internal/runtime/server/session_test.go`

- [ ] **Step 1: Locate the pane-add/remove fire site**

Run: `grep -nE 'PaneCount|onPaneAdd|onPaneRemove|TreeChanged|pane add' internal/runtime/server/*.go | head -10`

The desktop publisher emits `MsgTreeSnapshot` whenever the tree changes. That's the right call site to refresh `PaneCount` and `FirstPaneTitle`.

- [ ] **Step 2: Write the failing test**

Append to `session_test.go`:

```go
func TestRecordPaneActivityPersists(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xfe, 0xed}
	sess := NewSession(id, 100)
	sess.AttachWriter(SessionFilePath(dir, id), 25*time.Millisecond)
	defer sess.Close()

	sess.RecordPaneActivity(3, "bash")
	time.Sleep(80 * time.Millisecond)

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got StoredSession
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PaneCount != 3 || got.FirstPaneTitle != "bash" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/runtime/server/ -run TestRecordPaneActivityPersists -v`
Expected: PASS already (Task 9 implemented `RecordPaneActivity`). If FAIL, double-check Task 9.

If PASS, this proves the hook works in isolation. The remaining work is calling it from the publisher.

- [ ] **Step 4: Identify the publisher's snapshot dispatch**

Run: `grep -n "TreeSnapshot\|Publish\b\|broadcastSnapshot" internal/runtime/server/desktop_publisher.go | head -10`

- [ ] **Step 5: Add the call site**

In `desktop_publisher.go`, find the function that builds the `protocol.TreeSnapshot` for emit. It will iterate panes and produce `[]protocol.PaneSnapshot`. After computing the snapshot, derive `paneCount` and `firstPaneTitle`:

```go
// (At the top of the snapshot-emit function, after panes are gathered.)
paneCount := len(snap.Panes)
firstPaneTitle := ""
if len(snap.Panes) > 0 {
	firstPaneTitle = snap.Panes[0].Title
}
```

The publisher fans out to multiple sessions. For each session that receives a snapshot, call `session.RecordPaneActivity(paneCount, firstPaneTitle)`. Locate the existing per-session loop (it will be where `session.EnqueueDiff` / `session.EnqueueImage` is invoked, or a sibling function). Add the call there.

If no per-session iteration exists at the snapshot site (i.e. snapshot is sent to a single session at a time via `Connection`), call `c.session.RecordPaneActivity(...)` from `connection_handler.go` immediately after the post-resume snapshot is sent (the existing `MsgTreeSnapshot` write block in the `MsgResumeRequest` case from line ~211 onward). Use the same `paneCount`/`firstPaneTitle` derivation.

- [ ] **Step 6: Verify the broader suite stays green**

Run: `go test ./internal/runtime/server/ -race -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/server/desktop_publisher.go internal/runtime/server/connection_handler.go internal/runtime/server/session_test.go
git commit -m "Plan D2 task 11: record pane activity into session writer (Plan F prep)"
```

---

## Phase 5: Resume rehydration in handshake (Task 12)

### Task 12: `handleHandshake` consults rehydration index on cache miss

**Files:**
- Modify: `internal/runtime/server/handshake.go`
- Test: `internal/runtime/server/handshake_test.go` (or similar; create if missing)

- [ ] **Step 1: Write the failing integration test**

Create `internal/runtime/server/d2_resume_integration_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/framegrace/texelation/internal/runtime/server/testutil"
	"github.com/framegrace/texelation/protocol"
)

func TestD2_ResumeRehydratesUnknownSession(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x12, 0x34}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		PaneViewports: []StoredPaneViewport{{
			PaneID:        [16]byte{0x77},
			ViewBottomIdx: 9000,
			Rows:          24,
			Cols:          80,
		}},
	}
	path := SessionFilePath(dir, id)
	if err := os.MkdirAll(filepathFromPath(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(&stored)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	if err := LoadPersistedSessions(mgr, dir); err != nil {
		t.Fatal(err)
	}
	mgr.SetPersistencePath(dir, 25*time.Millisecond)

	// Simulate a Hello → Welcome → ConnectRequest{SessionID=id} handshake.
	clientToServer, serverToClient := testutil.NewMemConnPair()
	defer clientToServer.Close()
	defer serverToClient.Close()

	go func() {
		// Client side: write Hello, ConnectRequest{id}; read Welcome and ConnectAccept.
		_ = protocol.WriteMessage(clientToServer, protocol.Header{Version: protocol.Version, Type: protocol.MsgHello}, mustEncodeHello(t))
		// Read Welcome
		_, _, _ = protocol.ReadMessage(clientToServer)
		// Send ConnectRequest with persisted ID
		payload, _ := protocol.EncodeConnectRequest(protocol.ConnectRequest{SessionID: id})
		_ = protocol.WriteMessage(clientToServer, protocol.Header{Version: protocol.Version, Type: protocol.MsgConnectRequest}, payload)
		// Read ConnectAccept
		_, _, _ = protocol.ReadMessage(clientToServer)
	}()

	sess, resuming, err := handleHandshake(serverToClient, mgr)
	if err != nil {
		t.Fatalf("handleHandshake: %v", err)
	}
	if !resuming {
		t.Fatalf("expected resuming=true for known sessionID")
	}
	if sess.ID() != id {
		t.Fatalf("expected rehydrated session %x, got %x", id, sess.ID())
	}
	vp, ok := sess.Viewport([16]byte{0x77})
	if !ok || vp.ViewBottomIdx != 9000 {
		t.Fatalf("rehydrated viewport missing or wrong: ok=%v vp=%+v", ok, vp)
	}
}

func mustEncodeHello(t *testing.T) []byte {
	t.Helper()
	out, err := protocol.EncodeHello(protocol.Hello{ClientName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// filepathFromPath strips the filename to return the directory. Avoids
// importing filepath in two places when the tests already heavily lean
// on raw paths.
func filepathFromPath(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

var _ = bytes.NewReader
```

(If `testutil.NewMemConnPair` doesn't exist with that exact name, locate the equivalent helper used by other tests in this package — `grep -n "MemConn\|memConn\|NewMemConnPair" internal/runtime/server/*.go internal/runtime/server/testutil/*.go` — and use the actual symbol.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/server/ -run TestD2_ResumeRehydratesUnknownSession -v`
Expected: FAIL — `handleHandshake` calls `mgr.Lookup`, which returns `ErrSessionNotFound`.

- [ ] **Step 3: Modify `handleHandshake`**

In `internal/runtime/server/handshake.go`, change the lookup branch:

```go
} else {
	session, err = mgr.LookupOrRehydrate(connectReq.SessionID)
	if err != nil {
		return nil, false, err
	}
}
```

(Replace the previous `mgr.Lookup` call. `LookupOrRehydrate` falls back to `ErrSessionNotFound` for unknown IDs, preserving the existing behavior for non-rehydratable cases.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/server/ -run TestD2_ResumeRehydratesUnknownSession -race -v`
Expected: PASS.

- [ ] **Step 5: Run the full server suite**

Run: `go test ./internal/runtime/server/ -race -count=1`
Expected: PASS, including pre-existing handshake tests.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/handshake.go internal/runtime/server/d2_resume_integration_test.go
git commit -m "Plan D2 task 12: handshake rehydrates persisted sessions on cache miss"
```

---

## Phase 6: Client-side reset on post-resume snapshot (Tasks 13–14)

### Task 13: Add a one-shot resume flag to client state and reset `BufferCache.pane.Revision` on the post-resume `MsgTreeSnapshot`

**Files:**
- Modify: `internal/runtime/client/client_state.go` (add the flag)
- Modify: `internal/runtime/client/app.go` (set flag when `awaitResume`-style resume path runs)
- Modify: `internal/runtime/client/protocol_handler.go` (consume flag in `MsgTreeSnapshot` case)
- Modify: `client/buffercache.go` (add a method that resets per-pane revisions)
- Test: `internal/runtime/client/protocol_handler_test.go` (or new file)

- [ ] **Step 1: Locate `clientState` and the resume flow**

Run: `grep -n "type clientState\|fullRenderNeeded\|awaitResume\|RequestResume" internal/runtime/client/*.go | head -20`

- [ ] **Step 2: Add a `resetOnNextSnapshot` field**

In `internal/runtime/client/client_state.go`, add to the `clientState` struct:

```go
// resetOnNextSnapshot is set by the resume flow before MsgResumeRequest
// is sent. The next MsgTreeSnapshot received resets per-pane Revision
// and the top-level lastSequence to zero, then clears the flag. Plan D2:
// this is the single synchronization barrier that lets a still-alive
// client recover from a daemon restart without dedup-dropping the new
// daemon's low-numbered messages as stale.
//
// Steady-state TreeSnapshots (workspace ops, splits) MUST NOT reset.
// The flag is cleared atomically with the reset so only the FIRST
// post-resume snapshot consumes it.
resetOnNextSnapshot atomic.Bool
```

Add the import `"sync/atomic"` if not present.

- [ ] **Step 3: Add `BufferCache.ResetRevisions()`**

In `client/buffercache.go`, add:

```go
// ResetRevisions zeros every cached pane's Revision counter. Used by
// the client on the post-resume MsgTreeSnapshot to acknowledge that
// the server has restarted and the in-memory revision stream restarts
// from scratch. See docs/superpowers/specs/2026-04-26-issue-199-plan-d2-...md.
func (c *BufferCache) ResetRevisions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pane := range c.panes {
		pane.Revision = 0
	}
}
```

(Adjust the field/lock names if the actual cache struct uses different identifiers — `grep -n "type BufferCache struct\|c.panes\|c.mu" client/buffercache.go` to check.)

- [ ] **Step 4: Write the failing test**

Create `internal/runtime/client/d2_resume_reset_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"sync/atomic"
	"testing"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

func TestPostResumeSnapshotResetsRevisionAndSequence(t *testing.T) {
	cache := client.NewBufferCache()
	// Seed a pane with a high revision (simulates pre-restart state).
	cache.ApplyDelta(protocol.BufferDelta{
		PaneID:   [16]byte{0xaa},
		Revision: 50,
		Rows:     1, Cols: 1,
		AltScreen: true,
	})
	if r := cache.PaneRevision([16]byte{0xaa}); r != 50 {
		t.Fatalf("seed: expected revision=50, got %d", r)
	}

	state := newClientStateForTest(cache)
	state.resetOnNextSnapshot.Store(true)
	var lastSeq atomic.Uint64
	lastSeq.Store(9999)

	// Simulate post-resume MsgTreeSnapshot arriving.
	deliverEmptyTreeSnapshotForTest(state, &lastSeq)

	if state.resetOnNextSnapshot.Load() {
		t.Fatalf("flag should be cleared after consumption")
	}
	if r := cache.PaneRevision([16]byte{0xaa}); r != 0 {
		t.Fatalf("expected revision reset to 0, got %d", r)
	}
	if lastSeq.Load() != 0 {
		t.Fatalf("expected lastSequence reset to 0, got %d", lastSeq.Load())
	}
}

func TestSteadyStateSnapshotDoesNotReset(t *testing.T) {
	cache := client.NewBufferCache()
	cache.ApplyDelta(protocol.BufferDelta{PaneID: [16]byte{0xaa}, Revision: 7, Rows: 1, Cols: 1, AltScreen: true})

	state := newClientStateForTest(cache)
	// Flag NOT set.
	var lastSeq atomic.Uint64
	lastSeq.Store(123)

	deliverEmptyTreeSnapshotForTest(state, &lastSeq)

	if r := cache.PaneRevision([16]byte{0xaa}); r != 7 {
		t.Fatalf("steady-state snapshot reset revision unexpectedly: %d", r)
	}
	if lastSeq.Load() != 123 {
		t.Fatalf("steady-state snapshot reset lastSequence unexpectedly: %d", lastSeq.Load())
	}
}
```

The helpers `newClientStateForTest` and `deliverEmptyTreeSnapshotForTest` need to expose a small surface for testing without the full network plumbing. Add them in a new `internal/runtime/client/test_helpers_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"sync"
	"sync/atomic"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

func newClientStateForTest(cache *client.BufferCache) *clientState {
	return &clientState{
		cache:       cache,
		paneCaches:  make(map[[16]byte]*client.PaneCache),
		paneCachesMu: sync.Mutex{},
	}
}

func deliverEmptyTreeSnapshotForTest(state *clientState, lastSeq *atomic.Uint64) {
	// Inline the relevant excerpt from handleControlMessage's
	// MsgTreeSnapshot case so the test exercises only the reset hook.
	snap := protocol.TreeSnapshot{}
	state.cache.ApplySnapshot(snap)
	if state.resetOnNextSnapshot.CompareAndSwap(true, false) {
		state.cache.ResetRevisions()
		lastSeq.Store(0)
	}
}
```

(If `client.BufferCache.PaneRevision` doesn't exist, add a small accessor in `client/buffercache.go`:

```go
// PaneRevision returns the cached revision for paneID, or 0 if absent.
// Test-friendly accessor for Plan D2 reset verification.
func (c *BufferCache) PaneRevision(paneID [16]byte) uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if p, ok := c.panes[paneID]; ok {
		return p.Revision
	}
	return 0
}
```

— matching the actual struct names.)

- [ ] **Step 5: Run tests to verify they fail**

Run: `go test ./internal/runtime/client/ -run 'TestPostResumeSnapshotResetsRevisionAndSequence|TestSteadyStateSnapshotDoesNotReset' -v`
Expected: FAIL — `resetOnNextSnapshot` field exists (Step 2) but the production handler does not yet consume it.

- [ ] **Step 6: Wire the reset into the production handler**

In `internal/runtime/client/protocol_handler.go`, locate the `MsgTreeSnapshot` case (around line 44) and add at the very start of the `case` block:

```go
case protocol.MsgTreeSnapshot:
	snap, err := protocol.DecodeTreeSnapshot(payload)
	if err != nil {
		log.Printf("decode snapshot failed: %v", err)
		return false
	}
	cache.ApplySnapshot(snap)
	if state.resetOnNextSnapshot.CompareAndSwap(true, false) {
		state.cache.ResetRevisions()
		if lastSequence != nil {
			lastSequence.Store(0)
		}
	}
	state.fullRenderNeeded = true
	// ... (existing rest of the case unchanged)
```

The CAS ensures the reset runs at most once even if multiple snapshots arrive concurrently in pathological cases.

- [ ] **Step 7: Set the flag in the resume path**

In `internal/runtime/client/app.go`, locate the `simple.RequestResume(...)` call (around line 187 per the earlier grep). Immediately before that call, add:

```go
state.resetOnNextSnapshot.Store(true)
```

This arms the flag right before sending `MsgResumeRequest`. The next `MsgTreeSnapshot` (which the server emits as part of the resume response per `connection_handler.go`'s existing flow) consumes it.

- [ ] **Step 8: Run the test suite**

Run: `go test ./internal/runtime/client/ -run 'TestPostResumeSnapshotResetsRevisionAndSequence|TestSteadyStateSnapshotDoesNotReset' -race -v`
Expected: PASS for both.

Run: `go test ./internal/runtime/client/ ./client/ -race -count=1`
Expected: full suite green.

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/client/ client/buffercache.go
git commit -m "Plan D2 task 13: reset client revision/sequence on post-resume snapshot"
```

---

### Task 14: Cross-restart end-to-end integration test on memconn

**Files:**
- Create: `internal/runtime/server/d2_cross_restart_integration_test.go`

- [ ] **Step 1: Write the integration test**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package server

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// TestD2_FullCrossRestartCycle exercises the daemon-restart resume path
// end-to-end at the unit level: write a session file as if a previous
// daemon had persisted state, simulate boot scan, and verify a fresh
// Manager rehydrates the session and a subsequent resume seeds viewports.
func TestD2_FullCrossRestartCycle(t *testing.T) {
	dir := t.TempDir()

	// === Phase A: previous daemon writes a session file ===
	id := [16]byte{0xca, 0xfe}
	prevMgr := NewManager()
	prevMgr.SetPersistencePath(dir, 10*time.Millisecond)
	prevSess, err := prevMgrNewSessionWithID(prevMgr, id) // helper: bypass random ID generation
	if err != nil {
		t.Fatal(err)
	}
	prevSess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        [16]byte{0xab, 0xcd},
		ViewBottomIdx: 7777,
		ViewportRows:  30,
		ViewportCols:  100,
		AutoFollow:    false,
	})
	time.Sleep(40 * time.Millisecond)
	prevSess.Close() // flushes synchronously

	// === Phase B: new daemon boots, scans, indexes ===
	newMgr := NewManager()
	newMgr.SetPersistencePath(dir, 10*time.Millisecond)
	if err := LoadPersistedSessions(newMgr, dir); err != nil {
		t.Fatal(err)
	}

	// === Phase C: resume request lands and rehydrates ===
	sess, err := newMgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("LookupOrRehydrate: %v", err)
	}
	defer sess.Close()
	if sess.ID() != id {
		t.Fatalf("rehydrated wrong session: %x", sess.ID())
	}
	vp, ok := sess.Viewport([16]byte{0xab, 0xcd})
	if !ok {
		t.Fatalf("expected pre-seeded viewport for pane")
	}
	if vp.ViewBottomIdx != 7777 || vp.Rows != 30 || vp.Cols != 100 {
		t.Fatalf("viewport fields wrong: %+v", vp)
	}

	// === Phase D: client's MsgResumeRequest carries fresher data ===
	sess.ApplyResume([]protocol.PaneViewportState{{
		PaneID:        [16]byte{0xab, 0xcd},
		ViewBottomIdx: 8000, // fresher than disk's 7777
		ViewportRows:  30,
		ViewportCols:  100,
		AutoFollow:    false,
	}}, nil)
	vp, _ = sess.Viewport([16]byte{0xab, 0xcd})
	if vp.ViewBottomIdx != 8000 {
		t.Fatalf("client-fresh value should win over pre-seed; got %d", vp.ViewBottomIdx)
	}

	// === Phase E: live update writes back to disk ===
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID:        [16]byte{0xab, 0xcd},
		ViewBottomIdx: 8500,
		ViewportRows:  30,
		ViewportCols:  100,
	})
	time.Sleep(40 * time.Millisecond)

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var stored StoredSession
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range stored.PaneViewports {
		if p.PaneID == [16]byte{0xab, 0xcd} && p.ViewBottomIdx == 8500 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected updated viewport persisted, got %+v", stored.PaneViewports)
	}
}

// prevMgrNewSessionWithID is a test-only constructor that bypasses the
// random ID generator so the test controls which file we end up reading
// in Phase B. Mirror NewSession's body but inject id.
func prevMgrNewSessionWithID(m *Manager, id [16]byte) (*Session, error) {
	m.mu.Lock()
	session := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		session.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	m.sessions[id] = session
	m.mu.Unlock()
	return session, nil
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestD2_FullCrossRestartCycle -race -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/server/d2_cross_restart_integration_test.go
git commit -m "Plan D2 task 14: end-to-end cross-restart integration test"
```

---

## Phase 7: Verification and final polish (Task 15)

### Task 15: Full test sweep + manual e2e prep

**Files:** none (verification + commit-message-only changes if needed)

- [ ] **Step 1: Run the full test suite**

Run: `go test ./... -race -count=1`
Expected: PASS across the entire repo.

- [ ] **Step 2: Run `gofmt`**

Run: `gofmt -d ./internal/persistence/atomicjson/ ./internal/runtime/server/ ./internal/runtime/client/ ./client/`
Expected: no output. If diff appears, run `gofmt -w` on the listed files and commit.

- [ ] **Step 3: Run `go vet`**

Run: `go vet ./...`
Expected: clean.

- [ ] **Step 4: Build all binaries**

Run: `make build && make build-apps`
Expected: success, no warnings.

- [ ] **Step 5: Manual e2e — daemon-restart resume**

```bash
# Clean slate
rm -rf ~/.texelation/sessions
make build

# Start daemon + client
./bin/texelation start
./bin/texel-client --socket=/tmp/texelation.sock &
CLIENT_PID=$!

# Inside the client: open texelterm, scroll up several pages, note the position.
# Then in another terminal:
./bin/texelation stop      # SIGTERM the daemon
ls ~/.texelation/sessions  # expect a *.json file
./bin/texelation start     # daemon starts fresh, scans the dir

# Client should detect the disconnect and reconnect (existing Plan D behavior).
# Verify the pane lands at the previously-noted scroll position.

kill $CLIENT_PID
./bin/texelation stop
```

Expected: scroll position survives daemon restart.

- [ ] **Step 6: Manual e2e — daemon crash (SIGKILL)**

```bash
make build
./bin/texelation start
./bin/texel-client --socket=/tmp/texelation.sock &
CLIENT_PID=$!

# Scroll a pane in the client.
# Then crash the daemon hard:
pkill -9 -f texel-server
sleep 1
./bin/texelation start

# Reconnect should rehydrate. Half-written file (if any) should be wiped.

kill $CLIENT_PID
./bin/texelation stop
```

Expected: scroll position survives or falls back cleanly to "snap to oldest" without panics. Check daemon log for any "atomicjson: parse failed" entries — they're acceptable when sigkill caught a half-write.

- [ ] **Step 7: Manual e2e — pinned sessions are forward-compat-safe**

```bash
# Edit a session file by hand, set "pinned": true, restart the daemon.
SESS=$(ls ~/.texelation/sessions/*.json | head -1)
jq '.pinned = true' $SESS > /tmp/p.json && mv /tmp/p.json $SESS
./bin/texelation stop && ./bin/texelation start
ls ~/.texelation/sessions  # file still present after boot scan
jq '.pinned' $SESS         # still true
```

Expected: file survives boot scan; field round-trips. (No consumer wires `Pinned` yet — this is forward-compat verification only.)

- [ ] **Step 8: Update memory after the PR merges**

After PR merge, update `~/.claude/projects/-home-marc-projects-texel-texelation/memory/project_issue199_progress.md`: flip Plan D2 row to `MERGED`, append an execution-notes section describing what shipped, and bump the active plan to F or C/E.

(This step is a reminder; perform after PR merge, not as part of the implementation commits.)

- [ ] **Step 9: Final commit if any formatting/vet fixes were needed**

If Steps 2–3 produced changes:

```bash
git add -A
git commit -m "Plan D2 task 15: gofmt/vet polish"
```

Otherwise, no commit.

---

## Self-Review Checklist (run before declaring the plan done)

- [ ] **Spec coverage**: every section of `2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md` has a task that implements it. Storage shape (Tasks 5–8), writer abstraction (Tasks 1–3), no-auto-GC (Task 6 — only corrupt files deleted), schema with reserved Plan F fields (Task 5), client-side reset on snapshot (Task 13), pre-seed-then-client-overrides (Tasks 7 + 14), pinned-not-consumed-yet (Task 15 manual). ✓
- [ ] **No placeholders**: every step contains exact code, file paths, and commands. The grep-and-locate steps in Tasks 8/11/13 are deliberate — those depend on the actual struct/field names in the codebase, which a fresh agent must look up rather than guess. The grep command and the line range to inspect are given exactly.
- [ ] **Type consistency**: `StoredSession`, `StoredPaneViewport`, `LookupOrRehydrate`, `AttachWriter`, `RecordPaneActivity`, `SessionFilePath`, `ScanSessionsDir`, `LoadPersistedSessions`, `SetPersistedSessions`, `SetPersistencePath`, `resetOnNextSnapshot`, `ResetRevisions`, `PaneRevision` — all symbols are defined in earlier tasks before later tasks reference them.
- [ ] **Test coverage**: at least one TDD red-green pair per task; integration tests cover the full cross-restart cycle.
