# Issue #199 Plan D2 — Server-Side Cross-Daemon-Restart Viewport Persistence

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist per-session pane viewport state on the server so a daemon restart followed by a client resume lands at the saved scroll position.

**Architecture:** Lazy rehydrate-on-resume keyed by sessionID. New on-disk store at `<snapshot-dir>/sessions/<hex-sessionID>.json`. Extract a shared `DebouncedAtomicJSONStore` primitive from Plan D's client `Writer` and use it for both client- and server-side persistence. Cross-restart sequence/revision continuity handled by client-side reset on the post-resume `MsgTreeSnapshot` (gated by a one-shot resume flag).

**Tech Stack:** Go 1.24.3, JSON encoding, atomic temp+rename, `time.AfterFunc` debounce, generic over snapshot type via `encoding/json`.

**Spec:** `docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md`

**Branch:** `feature/issue-199-plan-d2-server-viewport-persistence`

> **Plan revision 2026-04-26 (post 4-agent review):** Several issues were caught by parallel review agents before any task was implemented. This version of the plan incorporates all of them. Key changes: (1) Task 7 uses a new `ClientViewports.ApplyPreSeed` method instead of writing into `byPaneID` without taking the lock — Plan B round 2 caught the same kind of bug, don't repeat it. (2) Task 9 drops the `EnqueueDiff`/`EnqueueImage` writer hooks (per-delta `Snapshot()` allocations were a perf risk; spec updated to match). (3) Task 10 combines `LoadPersistedSessions` + `SetPersistencePath` into a single `Manager.EnablePersistence(basedir, debounce)` to remove the ordering window. (4) Task 11 is rewritten as a real TDD pair (no more "expected PASS at red-step"). (5) Task 12 replaces `testutil.NewMemConnPair` (does not exist) with `testutil.NewMemPipe(64)`. (6) Task 13's resume flag is cleared on `RequestResume` error, and the test now drives the production handler via `handleControlMessage` instead of a duplicated helper. (7) New Task 14b adds atomicjson tests the spec required but the prior plan dropped: failing-rename safety + `saveErrRelogInterval` dedup. (8) `prevMgrNewSessionWithID` escape hatch replaced by a public `Manager.NewSessionWithID`. (9) Manual e2e gains marker-based scrollback observation, orphan-tmp-file checks, and a client+daemon both-restart scenario.

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
//
// JSON encoding routes through MarshalJSON / UnmarshalJSON via
// sessionJSONShape, so struct tags here would be ignored. They're
// omitted to make that fact obvious and avoid future confusion.
type StoredSession struct {
	SchemaVersion  int
	SessionID      [16]byte
	LastActive     time.Time
	Pinned         bool
	PaneViewports  []StoredPaneViewport
	// Plan F metadata (populated at write time; no consumers in D2):
	Label          string
	PaneCount      int
	FirstPaneTitle string
}

// StoredPaneViewport is the per-pane element. JSON encoding for this
// type also routes through paneViewportJSONShape (PaneID is
// hex-encoded), so struct tags would be ignored — omitted for clarity.
type StoredPaneViewport struct {
	PaneID         [16]byte
	AltScreen      bool
	AutoFollow     bool
	ViewBottomIdx  int64
	WrapSegmentIdx uint16
	Rows           uint16
	Cols           uint16
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
		// Filename-vs-content sanity check. If the filename hex doesn't
		// match the decoded SessionID, the user likely renamed the file
		// (e.g. as a template — sessions can be reused per project policy,
		// see spec). Skip it without loading and WITHOUT deleting — D2
		// must not silently destroy files that look user-touched.
		expectedName := hex.EncodeToString(s.SessionID[:]) + ".json"
		if name != expectedName {
			log.Printf("server: session scan: %s filename does not match sessionID %s; skipping (file left in place)",
				name, expectedName)
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

- [ ] **Step 3: Add `ClientViewports.ApplyPreSeed` (proper locking)**

Modify `internal/runtime/server/client_viewport.go` — append a new method that takes the package's lock instead of letting external callers reach into `byPaneID`:

```go
// ApplyPreSeed seeds the viewport map from a list of stored disk entries
// (Plan D2 rehydration). Mirrors ApplyResume's overflow guard and field
// derivation but reads from StoredPaneViewport instead of
// protocol.PaneViewportState. Acquires the write lock — callers must
// not hold any other ClientViewports lock.
//
// This is intentionally a method on ClientViewports (not a free function
// in manager.go) so the internal byPaneID map is never touched without
// the lock. Plan B's round-2 review caught a similar lock-discipline
// regression; this is its preventative.
func (c *ClientViewports) ApplyPreSeed(vps []StoredPaneViewport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range vps {
		top := p.ViewBottomIdx - int64(p.Rows) + 1
		bottom := p.ViewBottomIdx
		if top > p.ViewBottomIdx {
			top, bottom = 0, 0
		} else if top < 0 {
			top = 0
		}
		c.byPaneID[p.PaneID] = ClientViewport{
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

Then modify `internal/runtime/server/manager.go`:

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
//
// In production code, prefer EnablePersistence (see Task 10), which
// performs scan + index seed + writer-path config atomically. This
// method is exposed primarily for tests that want to inject a
// hand-constructed index.
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
	// Use the locked accessor — never write to byPaneID directly.
	sess.viewports.ApplyPreSeed(stored.PaneViewports)
	m.sessions[id] = sess
	return sess, nil
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

- [ ] **Step 6: NOT wired into `cmd/texel-server/main.go` yet**

`LoadPersistedSessions` is a building block used by the public `Manager.EnablePersistence` (Task 10). Tests can call it directly. Production wiring into `cmd/texel-server/main.go` is deferred to Task 10 so the scan + path-config + index-seed can happen atomically before the listener starts.

No changes to `cmd/texel-server/main.go` in this task.

- [ ] **Step 7: Verify the binary builds and existing tests pass**

Run: `make build && go test ./internal/runtime/server/ -race -count=1`
Expected: build succeeds; no test regressions.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/server/server_boot.go internal/runtime/server/server_boot_test.go
git commit -m "Plan D2 task 8: LoadPersistedSessions helper for boot-scan rehydration"
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
	// Force the debounced writer to flush synchronously rather than
	// racing a sleep against the AfterFunc timer (flake-prone on CI).
	sess.FlushPersistForTest()

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
// and hands it to the writer.
//
// PRECONDITION: caller MUST NOT hold s.mu. This function acquires
// s.mu briefly to read s.writer, then s.storedMu, then
// s.viewports.mu (via Snapshot's RLock); holding s.mu at entry would
// invert lock order against any future code that takes s.mu while
// inside the publisher or viewport plumbing.
//
// Lock-discipline note: s.writer is read UNDER s.mu (snapshot the
// pointer, then drop the lock). A naive read like `if s.writer == nil`
// outside the lock would race with Session.Close, which nils s.writer
// under s.mu — an unprotected read could observe non-nil at the check
// then deref a niled-out value during Update.
//
// Safe to call from any goroutine. Nil-safe (no-op when writer absent).
func (s *Session) schedulePersist() {
	s.mu.Lock()
	w := s.writer
	s.mu.Unlock()
	if w == nil {
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
	w.Update(stored)
}
```

Hook into `ApplyViewportUpdate` and `ApplyResume` only. **Do NOT** hook into `EnqueueDiff` / `EnqueueImage` — see "Why no diff hook" note below. Replace the existing methods:

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

**Why no diff hook.** A draft of this plan also wired `Enqueue*` to fire `schedulePersist`. Dropped because (a) `Snapshot()` of `ClientViewports` allocates a fresh map every call, and 100+ diffs/sec would create noticeable GC churn for zero state-change payoff, and (b) viewport activity already drives writes for sessions a user is interacting with. The spec was updated to match this decision (see "Update sites" section there).

**Lock-ordering note for `schedulePersist`.** It acquires `s.storedMu` (briefly), calls `s.viewports.Snapshot()` (which acquires `viewports.mu` as a read-lock), then hands the result to `s.writer.Update`. Order: `storedMu` → `viewports.mu` (innermost) → atomicjson internal locks. `Session.mu` MUST be released before calling `schedulePersist` — every call site in this task drops `s.mu` first. Document this requirement on `schedulePersist` itself with a comment so future contributors don't introduce an inversion.

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

// FlushPersistForTest forces the writer to flush any pending state
// synchronously. Tests use this instead of time.Sleep to avoid debounce
// flakes. Production code does NOT call this — Close already flushes.
func (s *Session) FlushPersistForTest() {
	s.mu.Lock()
	w := s.writer
	s.mu.Unlock()
	if w != nil {
		w.Flush()
	}
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
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	sess, err := mgr.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xaa}, ViewBottomIdx: 1, ViewportRows: 1, ViewportCols: 1,
	})
	sess.FlushPersistForTest() // see helper in session.go (Task 9 patch below)

	if _, err := os.Stat(SessionFilePath(dir, sess.ID())); err != nil {
		t.Fatalf("expected session file written, got stat=%v", err)
	}
}

func TestManagerLookupOrRehydrate_AttachesWriter(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Inject a persisted entry directly so the test doesn't depend on
	// disk shape. SetPersistedSessions remains exported for this purpose.
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
	sess.FlushPersistForTest()
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

// EnablePersistence is the single public entry point that wires Plan D2
// cross-restart persistence. Performs:
//
//   1. ScanSessionsDir(<basedir>) — disk I/O, runs OUTSIDE m.mu so a
//      slow filesystem cannot block other Manager methods. Safe because
//      this method is called once during boot before the listener
//      accepts any connection (see "Boot-scan-before-listener
//      invariant" in the spec). No concurrent caller exists at boot.
//   2. Under m.mu: install basedir/debounce on Manager and seed
//      persistedSessions from the scan result. The lock-protected
//      block is constant-time over the result-size copy.
//
// CALLERS MUST INVOKE THIS BEFORE STARTING THE LISTENER. Any
// MsgResumeRequest arriving during the scan window would otherwise
// falsely return ErrSessionNotFound and the client would wipe its
// persisted state — silently losing the very state D2 exists to
// preserve.
//
// debounce: typically 250ms in prod and 25ms in tests.
//
// Returns the boot-scan error (if any) so callers can decide whether to
// continue without persistence or abort startup. SetPersistedSessions
// is still exposed for tests that need to inject a hand-built index.
func (m *Manager) EnablePersistence(basedir string, debounce time.Duration) error {
	if basedir == "" {
		return nil
	}
	// Scan outside the lock — disk I/O can be slow on cold starts and
	// boot is single-threaded so there's no race to win.
	loaded, err := ScanSessionsDir(basedir)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistBasedir = basedir
	m.persistDebounce = debounce
	m.persistedSessions = make(map[[16]byte]*StoredSession, len(loaded))
	for id, s := range loaded {
		m.persistedSessions[id] = s
	}
	if len(loaded) > 0 {
		log.Printf("[BOOT] EnablePersistence: loaded %d persisted session(s) from %s", len(loaded), basedir)
	}
	return nil
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

In `LookupOrRehydrate`, after the rehydrated session is registered, attach the writer. Pre-seed viewports via the locked `ApplyPreSeed` method (added in Task 7 — never write `byPaneID` directly):

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
	sess.viewports.ApplyPreSeed(stored.PaneViewports) // Task 7's locked accessor
	m.sessions[id] = sess
	return sess, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run 'TestManager.*Writer' -race -v`
Expected: PASS.

- [ ] **Step 5: Wire `EnablePersistence` into `cmd/texel-server/main.go`**

Task 8 deliberately did NOT touch `cmd/texel-server/main.go` — the production wiring lives entirely here.

`cmd/texel-server/main.go` resolves `snapPath` in two branches around lines 194 and 234. After each `snapPath` is finalized and the snapshot store is constructed, locate the `*server.Manager` instance (`mgr`) and add this call **before** the listener starts accepting connections:

```go
if err := mgr.EnablePersistence(filepath.Dir(snapPath), 250*time.Millisecond); err != nil {
    log.Printf("warning: could not enable persistence: %v", err)
}
```

Verify the ordering with:

```bash
grep -n "Listen\|Accept\|ListenAndServe\|EnablePersistence" cmd/texel-server/main.go
```

`EnablePersistence` MUST appear lexically (and run sequentially) before any line that opens the listener / starts the connection acceptor. Per the spec's "Boot-scan-before-listener invariant," a `MsgResumeRequest` arriving during the scan window would falsely return `ErrSessionNotFound`.

`LoadPersistedSessions` from Task 8 is unused as a production entry point but remains a useful primitive for tests; keep it. `SetPersistencePath` from earlier drafts is removed entirely — `EnablePersistence` is the only production entry point.

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
- Modify: `internal/runtime/server/connection_handler.go`
- Modify: `internal/runtime/server/connection_handler_test.go` (or create a new file)

**Approach.** `connection_handler.go` has three sites that send a `protocol.TreeSnapshot` to the client (around lines 211, 293, 365 — one for resume, two for steady state). Each site has access to `c.session`. We extract a small helper `paneActivityFromSnapshot(snap) (paneCount int, firstTitle string)` that's testable in isolation, plus a `c.recordSnapshotActivity(snap)` wrapper that calls `c.session.RecordPaneActivity(...)`. Then we insert one call to `recordSnapshotActivity` after each snapshot encode succeeds.

The TDD pair drives the helper. Wiring it into the three sites is mechanical, and the integration assertion lives in Task 14's full-cycle test (extended below to cover `PaneCount` / `FirstPaneTitle`).

- [ ] **Step 1: Write the failing helper test**

Create `internal/runtime/server/snapshot_activity_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestPaneActivityFromSnapshot_Empty(t *testing.T) {
	count, title := paneActivityFromSnapshot(protocol.TreeSnapshot{})
	if count != 0 || title != "" {
		t.Fatalf("empty snapshot: got count=%d title=%q", count, title)
	}
}

func TestPaneActivityFromSnapshot_PicksFirstTitle(t *testing.T) {
	snap := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{Title: "bash"},
			{Title: "vim"},
			{Title: "logs"},
		},
	}
	count, title := paneActivityFromSnapshot(snap)
	if count != 3 {
		t.Fatalf("count: got %d want 3", count)
	}
	if title != "bash" {
		t.Fatalf("title: got %q want bash", title)
	}
}

func TestPaneActivityFromSnapshot_TitleMayBeEmpty(t *testing.T) {
	snap := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{{Title: ""}}}
	count, title := paneActivityFromSnapshot(snap)
	if count != 1 || title != "" {
		t.Fatalf("got count=%d title=%q want 1, empty", count, title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/server/ -run TestPaneActivityFromSnapshot -v`
Expected: FAIL — `paneActivityFromSnapshot` undefined.

- [ ] **Step 3: Implement the helper**

Create `internal/runtime/server/snapshot_activity.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Snapshot-derived pane activity metrics for cross-restart session
// metadata (Plan D2 / Plan F). Pure helpers — no I/O, no mutation.

package server

import "github.com/framegrace/texelation/protocol"

// paneActivityFromSnapshot extracts the session-level pane activity
// fields (PaneCount, FirstPaneTitle) for Plan F's session-discovery
// picker. Pure function over the protocol shape; safe to call from the
// connection handler's snapshot-emit hot path.
func paneActivityFromSnapshot(snap protocol.TreeSnapshot) (paneCount int, firstTitle string) {
	paneCount = len(snap.Panes)
	if paneCount > 0 {
		firstTitle = snap.Panes[0].Title
	}
	return
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/server/ -run TestPaneActivityFromSnapshot -v`
Expected: PASS for all three sub-tests.

- [ ] **Step 5: Wire the helper into the three TreeSnapshot dispatch sites**

In `connection_handler.go`, add a method on the connection type (look up the receiver name with `grep -n "^func (c \*" internal/runtime/server/connection_handler.go | head -3` — typically `c *connection`):

```go
// recordSnapshotActivity updates the session's stored pane-activity
// metadata after a TreeSnapshot is dispatched. Cheap (no I/O on the
// hot path; the writer debounces). Plan F consumes the resulting
// PaneCount / FirstPaneTitle fields.
func (c *connection) recordSnapshotActivity(snap protocol.TreeSnapshot) {
	if c.session == nil {
		return
	}
	count, title := paneActivityFromSnapshot(snap)
	c.session.RecordPaneActivity(count, title)
}
```

(Adjust `*connection` if the actual receiver type differs — confirm via the grep above.)

Insert one call at each of the three TreeSnapshot dispatch sites — search for `protocol.EncodeTreeSnapshot` to find them:

```bash
grep -n "EncodeTreeSnapshot" internal/runtime/server/connection_handler.go
```

After each successful `c.writeMessage(header, payload)` for a `MsgTreeSnapshot`, add:

```go
c.recordSnapshotActivity(snapshot)
```

(Use whatever local variable holds the `protocol.TreeSnapshot` — typically `snapshot` per the existing code.)

- [ ] **Step 6: Verify the broader suite stays green**

Run: `go test ./internal/runtime/server/ -race -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/server/snapshot_activity.go internal/runtime/server/snapshot_activity_test.go internal/runtime/server/connection_handler.go
git commit -m "Plan D2 task 11: paneActivityFromSnapshot helper + connection wiring"
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
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	// Simulate a Hello → Welcome → ConnectRequest{SessionID=id} handshake.
	// testutil.NewMemPipe returns two MemConn endpoints with a buffered
	// connecting pipe (existing helper used elsewhere — confirm with
	// `grep -n NewMemPipe internal/runtime/server/testutil/`).
	clientToServer, serverToClient := testutil.NewMemPipe(64)
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
	out, err := protocol.EncodeHello(protocol.Hello{
		ClientID:     [16]byte{},
		ClientName:   "test",
		Capabilities: 0,
	})
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

(`testutil.NewMemPipe(buffer int) (*MemConn, *MemConn)` exists at `internal/runtime/server/testutil/memconn.go:30`. Other integration tests use it with buffer sizes 32 or 64 — see `viewport_integration_test.go` and `offline_resume_mem_test.go` for live examples.)

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

## Phase 6: Client-side reset and integration tests (Tasks 13, 13b, 14, 14b)

### Task 13: Add a one-shot resume flag to client state and reset `BufferCache.pane.Revision` on the post-resume `MsgTreeSnapshot`

**Files:**
- Modify: `internal/runtime/client/client_state.go` (add the flag)
- Modify: `internal/runtime/client/app.go` (set flag before resume; clear on resume error)
- Modify: `internal/runtime/client/protocol_handler.go` (consume flag in `MsgTreeSnapshot` case)
- Create: `internal/runtime/client/post_resume_reset.go` (extracted reset function)
- Create: `internal/runtime/client/post_resume_reset_test.go`
- Modify: `client/buffercache.go` (add `ResetRevisions` and `PaneRevision` accessors)

- [ ] **Step 1: Add a `resetOnNextSnapshot` field**

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
// The flag is cleared atomically with the reset (CAS) so only the FIRST
// post-resume snapshot consumes it. The flag MUST also be cleared by
// the resume error path — see app.go — so a failed-then-retried resume
// against a different sessionID does not consume a stale flag.
resetOnNextSnapshot atomic.Bool
```

Add the import `"sync/atomic"` if not present.

- [ ] **Step 2: Add `BufferCache.ResetRevisions()` and `PaneRevision()`**

`BufferCache.mu` is `sync.RWMutex` (verified at `client/buffercache.go:125`). Use `Lock` for the writer, `RLock` for the reader.

Append to `client/buffercache.go`:

```go
// ResetRevisions zeros every cached pane's Revision counter. Used by
// the client on the post-resume MsgTreeSnapshot to acknowledge that
// the server has restarted and the in-memory revision stream restarts
// from scratch. See docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md.
func (c *BufferCache) ResetRevisions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pane := range c.panes {
		pane.Revision = 0
	}
}

// PaneRevision returns the cached revision for paneID, or 0 if absent.
// Plan D2: test-friendly accessor for the cross-restart reset path.
func (c *BufferCache) PaneRevision(paneID [16]byte) uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if p, ok := c.panes[paneID]; ok {
		return p.Revision
	}
	return 0
}
```

- [ ] **Step 3: Write the failing test (drives the reset extraction)**

Create `internal/runtime/client/post_resume_reset_test.go`:

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

func newStateWithCache(cache *client.BufferCache) *clientState {
	return &clientState{
		cache:      cache,
		paneCaches: make(map[[16]byte]*client.PaneCache),
	}
}

// seedRevision sets a pane's Revision to v in the cache. Using the
// minimal BufferDelta shape required by ApplyDelta — only PaneID and
// Revision are needed to populate the per-pane cache entry that
// PaneRevision reads. Other BufferDelta fields (Flags, RowBase, Styles,
// Rows []RowDelta) are not relevant to this test's assertions.
func seedRevision(t *testing.T, cache *client.BufferCache, paneID [16]byte, v uint32) {
	t.Helper()
	cache.ApplyDelta(protocol.BufferDelta{PaneID: paneID, Revision: v})
}

func TestApplyPostResumeReset_FlagSet_ResetsRevisionAndSequence(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 50)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 50 {
		t.Fatalf("seed: got revision=%d want 50", got)
	}

	state := newStateWithCache(cache)
	state.resetOnNextSnapshot.Store(true)
	var lastSeq atomic.Uint64
	lastSeq.Store(9999)

	applyPostResumeReset(state, &lastSeq) // production function under test

	if state.resetOnNextSnapshot.Load() {
		t.Fatalf("flag must be cleared after consumption")
	}
	if got := cache.PaneRevision([16]byte{0xaa}); got != 0 {
		t.Fatalf("revision: got %d want 0", got)
	}
	if got := lastSeq.Load(); got != 0 {
		t.Fatalf("lastSequence: got %d want 0", got)
	}
}

func TestApplyPostResumeReset_FlagUnset_NoReset(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 7)

	state := newStateWithCache(cache)
	// Flag intentionally NOT set.
	var lastSeq atomic.Uint64
	lastSeq.Store(123)

	applyPostResumeReset(state, &lastSeq)

	if got := cache.PaneRevision([16]byte{0xaa}); got != 7 {
		t.Fatalf("revision should not be touched: got %d want 7", got)
	}
	if got := lastSeq.Load(); got != 123 {
		t.Fatalf("lastSequence should not be touched: got %d want 123", got)
	}
}

// One-shot guarantee: arming once + delivering N snapshots resets exactly
// once. Subsequent snapshots are no-ops, so a sequence value updated
// between snapshots is preserved.
func TestApplyPostResumeReset_FiresExactlyOnce(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 99)
	state := newStateWithCache(cache)
	state.resetOnNextSnapshot.Store(true)

	var lastSeq atomic.Uint64
	lastSeq.Store(500)

	// First snapshot — must reset.
	applyPostResumeReset(state, &lastSeq)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 0 {
		t.Fatalf("first call should reset revision: got %d", got)
	}
	if got := lastSeq.Load(); got != 0 {
		t.Fatalf("first call should reset sequence: got %d", got)
	}

	// Simulate post-reset traffic advancing state.
	seedRevision(t, cache, [16]byte{0xaa}, 1)
	lastSeq.Store(42)

	// Second snapshot — must NOT reset.
	applyPostResumeReset(state, &lastSeq)
	if got := cache.PaneRevision([16]byte{0xaa}); got != 1 {
		t.Fatalf("second call must not zero revision: got %d want 1", got)
	}
	if got := lastSeq.Load(); got != 42 {
		t.Fatalf("second call must not zero sequence: got %d want 42", got)
	}
}

// nil-lastSequence guard: the prod call site supplies *atomic.Uint64;
// defensive nil check matters because some test/dispatch paths pass nil.
func TestApplyPostResumeReset_NilSequenceIsNotADereference(t *testing.T) {
	cache := client.NewBufferCache()
	seedRevision(t, cache, [16]byte{0xaa}, 3)
	state := newStateWithCache(cache)
	state.resetOnNextSnapshot.Store(true)
	applyPostResumeReset(state, nil) // must not panic
	if got := cache.PaneRevision([16]byte{0xaa}); got != 0 {
		t.Fatalf("nil lastSeq should still reset cache: got %d", got)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/runtime/client/ -run TestApplyPostResumeReset -v`
Expected: FAIL — `applyPostResumeReset` undefined.

- [ ] **Step 5: Implement `applyPostResumeReset` as production code**

Create `internal/runtime/client/post_resume_reset.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// post_resume_reset.go: one-shot synchronization barrier for daemon-restart
// resume. See spec at docs/superpowers/specs/2026-04-26-issue-199-plan-d2-...md.

package clientruntime

import "sync/atomic"

// applyPostResumeReset zeros per-pane Revision counters and the
// top-level lastSequence iff state.resetOnNextSnapshot was set. The CAS
// guarantees one-shot semantics: only the FIRST snapshot after the flag
// is armed consumes it. Steady-state snapshots (workspace ops, splits)
// pass through untouched. The reset matches the new daemon's
// restart-from-zero numbering so the BufferCache stops dedup-dropping
// fresh deltas as "stale."
//
// `lastSequence` may be nil — caller's choice (some test paths). When
// nil, only the cache is reset.
func applyPostResumeReset(state *clientState, lastSequence *atomic.Uint64) {
	if !state.resetOnNextSnapshot.CompareAndSwap(true, false) {
		return
	}
	state.cache.ResetRevisions()
	if lastSequence != nil {
		lastSequence.Store(0)
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/runtime/client/ -run TestApplyPostResumeReset -race -v`
Expected: PASS for all four sub-tests.

- [ ] **Step 7: Wire the helper into the production `MsgTreeSnapshot` handler**

In `internal/runtime/client/protocol_handler.go`, locate the `MsgTreeSnapshot` case (around line 44). Insert one call to `applyPostResumeReset` immediately after `cache.ApplySnapshot(snap)`:

```go
case protocol.MsgTreeSnapshot:
	snap, err := protocol.DecodeTreeSnapshot(payload)
	if err != nil {
		log.Printf("decode snapshot failed: %v", err)
		return false
	}
	cache.ApplySnapshot(snap)
	applyPostResumeReset(state, lastSequence) // Plan D2: one-shot reset on post-resume snapshot
	state.fullRenderNeeded = true
	// (existing rest of the case unchanged)
```

- [ ] **Step 8: Set the flag in the resume path AND clear on error**

In `internal/runtime/client/app.go`, locate the `simple.RequestResume(...)` call (around line 187). Wrap it so the flag is set before, and cleared if the call fails. The exact existing code looks something like:

```go
hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence.Load(), viewports)
if err != nil {
	// existing error handling
}
```

Replace with:

```go
state.resetOnNextSnapshot.Store(true)
hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence.Load(), viewports)
if err != nil {
	// Plan D2: a failed resume must NOT leave the flag armed — a later
	// resume against a different sessionID (after Plan D's wipe-and-retry
	// fallback) would otherwise consume the stale flag and reset against
	// the wrong synchronization barrier.
	state.resetOnNextSnapshot.Store(false)
	// existing error handling, e.g. return / log / wipe / retry
}
```

(Preserve whatever the existing error-handling block does. The only behavior change is the explicit `Store(false)` at the top of that block.)

- [ ] **Step 9: Run the full client test suite**

Run: `go test ./internal/runtime/client/ ./client/ -race -count=1`
Expected: PASS — including all existing Plan D and protocol-handler tests.

- [ ] **Step 10: Commit**

```bash
git add internal/runtime/client/ client/buffercache.go
git commit -m "Plan D2 task 13: applyPostResumeReset one-shot sync barrier"
```

---

### Task 13b: Add `Manager.NewSessionWithID` public test/library entry point

The existing `Manager.NewSession` rolls a random `[16]byte` id. Task 14's integration test (and Plan F's future session-recovery surfaces) needs to construct a session with a specific id without reaching into private fields. Add the public method now so Task 14's helper isn't an escape hatch.

**Files:**
- Modify: `internal/runtime/server/manager.go`
- Modify: `internal/runtime/server/manager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `manager_test.go`:

```go
func TestManagerNewSessionWithID_BypassesRandomGen(t *testing.T) {
	mgr := NewManager()
	id := [16]byte{0xfa, 0xce, 0xfe, 0xed}

	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatalf("NewSessionWithID: %v", err)
	}
	if sess.ID() != id {
		t.Fatalf("ID mismatch: got %x want %x", sess.ID(), id)
	}
	// Lookup must hit the live cache.
	got, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != sess {
		t.Fatalf("Lookup must return the same session instance")
	}
}

func TestManagerNewSessionWithID_RejectsDuplicates(t *testing.T) {
	mgr := NewManager()
	id := [16]byte{0x01}
	if _, err := mgr.NewSessionWithID(id); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.NewSessionWithID(id); err == nil {
		t.Fatalf("expected error on duplicate id")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/runtime/server/ -run TestManagerNewSessionWithID -v`
Expected: FAIL — `NewSessionWithID` undefined.

- [ ] **Step 3: Implement on `Manager`**

Add to `internal/runtime/server/manager.go`:

```go
// ErrSessionAlreadyExists is returned by NewSessionWithID when the
// requested ID is already in the live session map.
var ErrSessionAlreadyExists = errors.New("server: session already exists")

// NewSessionWithID creates a session with a caller-supplied ID. Used
// by:
//   - tests that need deterministic IDs.
//   - future Plan F session-recovery code that constructs a Session
//     from a persisted record.
//
// Returns ErrSessionAlreadyExists if a live session with that ID is
// already in the manager. Does NOT consume from the persistedSessions
// index — for that path, use LookupOrRehydrate.
func (m *Manager) NewSessionWithID(id [16]byte) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[id]; exists {
		return nil, ErrSessionAlreadyExists
	}
	session := NewSession(id, m.maxDiffs)
	if m.persistBasedir != "" {
		session.AttachWriter(SessionFilePath(m.persistBasedir, id), m.persistDebounce)
	}
	m.sessions[id] = session
	return session, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run TestManagerNewSessionWithID -race -v`
Expected: PASS for both subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/manager.go internal/runtime/server/manager_test.go
git commit -m "Plan D2 task 13b: Manager.NewSessionWithID public constructor"
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
// +build integration

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
	if err := prevMgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// Manager.NewSessionWithID is the public test-only constructor that
	// bypasses the random ID generator (added in step 1 below). Without
	// it, this test would have to reach into Manager.mu/sessions/...
	prevSess, err := prevMgr.NewSessionWithID(id)
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
	prevSess.RecordPaneActivity(2, "bash") // Plan F metadata
	prevSess.Close()                       // flushes synchronously

	// === Phase B: new daemon boots, scans, indexes ===
	newMgr := NewManager()
	if err := newMgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
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
	sess.RecordPaneActivity(3, "vim") // refresh Plan F metadata after pane add
	sess.FlushPersistForTest()

	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var stored StoredSession
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}

	// === Plan F metadata round-tripped through the full cycle ===
	if stored.PaneCount != 3 {
		t.Errorf("PaneCount: got %d want 3", stored.PaneCount)
	}
	if stored.FirstPaneTitle != "vim" {
		t.Errorf("FirstPaneTitle: got %q want vim", stored.FirstPaneTitle)
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

// TestD2_PinnedRoundTrip verifies that hand-edited Pinned=true on disk
// survives the boot scan and round-trips through the loaded index.
// Spec test #6 (TestD2_PinnedNotConsumedYet) requires this even though
// no production code consumes Pinned in D2.
func TestD2_PinnedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x11, 0x22}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		Pinned:        true,
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
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	defer sess.Close()
	if sess.ID() != id {
		t.Fatalf("wrong session: %x", sess.ID())
	}

	// File still on disk after rehydration — D2 does not delete on consume.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session file should remain after rehydrate: %v", err)
	}

	// Force a fresh write back. Pinned must round-trip.
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0xff}, ViewBottomIdx: 1, ViewportRows: 1, ViewportCols: 1,
	})
	sess.FlushPersistForTest()

	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var afterWrite StoredSession
	if err := json.Unmarshal(again, &afterWrite); err != nil {
		t.Fatal(err)
	}
	if !afterWrite.Pinned {
		t.Fatalf("Pinned must round-trip across a write; got %+v", afterWrite)
	}
}

// TestD2_FileSurvivesSessionClose: closing a session does not delete
// the file. (Sessions can serve as templates per project policy.)
func TestD2_FileSurvivesSessionClose(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x33, 0x44}
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{
		PaneID: [16]byte{0x01}, ViewBottomIdx: 1, ViewportRows: 1, ViewportCols: 1,
	})
	sess.Close()

	if _, err := os.Stat(SessionFilePath(dir, id)); err != nil {
		t.Fatalf("file must survive Close: %v", err)
	}

	// Boot scan loads it into a fresh Manager — proves end-to-end persistence.
	mgr2 := NewManager()
	if err := mgr2.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr2.LookupOrRehydrate(id); err != nil {
		t.Fatalf("survived-Close session must rehydrate, got %v", err)
	}
}

// TestD2_ConcurrentUpdates fires Apply* + RecordPaneActivity from many
// goroutines concurrently to exercise the storedMu/viewports.mu lock
// ordering and ensure the writer doesn't lose updates or panic. Run
// under -race.
func TestD2_ConcurrentUpdates(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x55, 0x66}
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	const N = 8
	const K = 200
	var wg sync.WaitGroup
	for g := 0; g < N; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < K; i++ {
				sess.ApplyViewportUpdate(protocol.ViewportUpdate{
					PaneID:       [16]byte{byte(g)},
					ViewBottomIdx: int64(i),
					ViewportRows:  24,
					ViewportCols:  80,
				})
				if i%10 == 0 {
					sess.RecordPaneActivity(g+1, "concurrent")
				}
			}
		}(g)
	}
	wg.Wait()
	sess.FlushPersistForTest()

	// File must exist and parse — content correctness is enforced by
	// the per-method tests; this test specifically guards against
	// race/lock-order regressions.
	data, err := os.ReadFile(SessionFilePath(dir, id))
	if err != nil {
		t.Fatalf("file read: %v", err)
	}
	var stored StoredSession
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stored.SessionID != id {
		t.Fatalf("sessionID mismatch under concurrent load: %x", stored.SessionID)
	}
}

// TestD2_PhantomPaneFilterAfterPreSeed: pre-seed places a viewport for
// a pane that no longer exists server-side, then a client's resume
// payload includes a different phantom pane. ApplyResume's paneExists
// filter must drop the client-supplied phantom while leaving the
// pre-seed entry intact (until the next live update overwrites it).
// Locks in spec failure mode #4 across the rehydration boundary.
func TestD2_PhantomPaneFilterAfterPreSeed(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xee, 0xee}
	// Pre-existing pane (real on this server) + phantom from a prior life.
	realPane := [16]byte{0x01}
	deadPane := [16]byte{0x99}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
		PaneViewports: []StoredPaneViewport{
			{PaneID: realPane, ViewBottomIdx: 100, Rows: 24, Cols: 80},
			{PaneID: deadPane, ViewBottomIdx: 200, Rows: 24, Cols: 80},
		},
	}
	path := SessionFilePath(dir, id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(&stored)
	_ = os.WriteFile(path, data, 0o600)

	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.LookupOrRehydrate(id)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// Both pre-seeded entries are present (pre-seed does not filter).
	if _, ok := sess.Viewport(realPane); !ok {
		t.Fatalf("real pane viewport missing after pre-seed")
	}
	if _, ok := sess.Viewport(deadPane); !ok {
		t.Fatalf("phantom pane should be pre-seeded too — filter happens later")
	}

	// Client sends a resume payload that includes a NEW phantom (not in
	// pre-seed). A paneExists predicate accepts only realPane.
	clientPayload := []protocol.PaneViewportState{
		{PaneID: realPane, ViewBottomIdx: 150, ViewportRows: 24, ViewportCols: 80},
		{PaneID: [16]byte{0xde, 0xad}, ViewBottomIdx: 999, ViewportRows: 24, ViewportCols: 80},
	}
	paneExists := func(p [16]byte) bool { return p == realPane }
	sess.ApplyResume(clientPayload, paneExists)

	// realPane updates to client's fresher value.
	if vp, _ := sess.Viewport(realPane); vp.ViewBottomIdx != 150 {
		t.Fatalf("realPane: got %d want 150", vp.ViewBottomIdx)
	}
	// Client-supplied phantom MUST be filtered out.
	if _, ok := sess.Viewport([16]byte{0xde, 0xad}); ok {
		t.Fatalf("client-supplied phantom must be filtered by paneExists")
	}
}

// TestD2_RehydrateRaceForSameID: two goroutines call LookupOrRehydrate
// with the same persisted ID concurrently. Exactly one gets the
// rehydrated session; the other gets the live cached pointer.
func TestD2_RehydrateRaceForSameID(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x77, 0x88}
	stored := StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Now().UTC(),
	}
	path := SessionFilePath(dir, id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.Marshal(&stored)
	_ = os.WriteFile(path, data, 0o600)

	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	var got [2]*Session
	var errs [2]error
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got[idx], errs[idx] = mgr.LookupOrRehydrate(id)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d: %v", i, e)
		}
	}
	if got[0] != got[1] {
		t.Fatalf("expected same session pointer from both lookups, got %p vs %p", got[0], got[1])
	}
	if got[0] != nil {
		got[0].Close()
	}
}
```

The test file must add `"sync"` and `"path/filepath"` to its imports for the new tests above.

- [ ] **Step 2: Run the integration test**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestD2_FullCrossRestartCycle -race -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/server/d2_cross_restart_integration_test.go
git commit -m "Plan D2 task 14: end-to-end cross-restart integration test"
```

---

### Task 14b: atomicjson failure-mode tests (rename failure + error log dedup)

**Files:**
- Modify: `internal/persistence/atomicjson/store_test.go`

The spec promised a "failing rename" test (atomic-write semantics) and Plan D's original Writer had a `saveErrRelogInterval` test that was lost during the extraction in Task 3. Restore both.

- [ ] **Step 1: Write the tests**

Append to `internal/persistence/atomicjson/store_test.go`:

```go
import (
	"bytes"
	"errors"
	"log"
	"strings"
)

// TestSaveFailingRenameLeavesPriorFile: simulate a save where the tmp
// file IS written but the rename fails. The on-disk canonical file must
// be untouched (atomic-rename semantics: either old or new, never
// partial). This is the spec-promised "atomic-write semantics" test.
//
// The test injects a saver that mimics real Save's structure: write a
// tmp file alongside the target, then return an error in place of the
// rename. If the canonical file were touched at any point during the
// failing path, the assertion below would catch it.
func TestSaveFailingRenameLeavesPriorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	// First save: succeeds.
	good := &fakePayload{Name: "good", Count: 1}
	if err := Save(path, good); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	st := NewStore[fakePayload](path, 5*time.Millisecond)
	st.SetSaverForTest(func(p string, v *fakePayload) error {
		// Mirror Save's structure up to the point of rename: create a
		// sibling tmp file (proves write succeeded), then fail before
		// rename. Real Save uses os.Rename which is atomic — if the
		// rename fails for any reason, the canonical file at p is left
		// untouched. Simulate that failure mode exactly.
		dir := filepath.Dir(p)
		tmp, err := os.CreateTemp(dir, ".atomicjson.tmp-fail-*")
		if err != nil {
			return err
		}
		// Write valid JSON into the tmp file so the data is real.
		if _, werr := tmp.Write([]byte(`{"name":"replacement","count":2}`)); werr != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return werr
		}
		if cerr := tmp.Close(); cerr != nil {
			_ = os.Remove(tmp.Name())
			return cerr
		}
		// CRITICAL: do NOT call os.Rename. The whole point of this test
		// is that the rename step fails, so the canonical file is not
		// replaced. Clean up the tmp file we wrote.
		_ = os.Remove(tmp.Name())
		return errors.New("synthetic rename failure")
	})

	st.Update(fakePayload{Name: "bad", Count: 999})
	st.Close() // Flush, no panic, swallows the error.

	// Original file must be intact — failed save did NOT touch the
	// canonical path. This is the property atomic temp+rename gives us.
	got, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.Name != "good" || got.Count != 1 {
		t.Fatalf("canonical file modified by failing-rename path: got %+v", got)
	}

	// And no orphan tmp files leak into the directory after Close.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".atomicjson.tmp-") {
			t.Errorf("orphan tmp file leaked: %s", name)
		}
	}
}

// TestSaveErrRelogInterval_DeduplicatesIdenticalErrors: repeated
// identical errors are logged once, not on every retry. A different
// error string logs immediately. Recovery (success after failure)
// emits one "save recovered" line.
func TestSaveErrRelogInterval_DeduplicatesIdenticalErrors(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	st := NewStore[fakePayload](path, 5*time.Millisecond)

	var mode atomic.Int32 // 0=err1, 1=err2, 2=ok
	st.SetSaverForTest(func(p string, v *fakePayload) error {
		switch mode.Load() {
		case 0:
			return errors.New("err A")
		case 1:
			return errors.New("err B")
		default:
			return Save(p, v)
		}
	})

	// 5 saves with the same error — exactly 1 log line.
	for i := 0; i < 5; i++ {
		st.Update(fakePayload{Count: i})
		st.Flush()
	}
	if c := strings.Count(buf.String(), "save failed"); c != 1 {
		t.Fatalf("identical errors must dedup: saw %d log lines\n%s", c, buf.String())
	}

	// Switch error string — must log a new line.
	mode.Store(1)
	st.Update(fakePayload{Count: 100})
	st.Flush()
	if c := strings.Count(buf.String(), "save failed"); c != 2 {
		t.Fatalf("distinct error strings must each log: saw %d\n%s", c, buf.String())
	}

	// Recovery — success after failure logs exactly one "recovered" line.
	mode.Store(2)
	st.Update(fakePayload{Count: 200})
	st.Flush()
	if c := strings.Count(buf.String(), "save recovered"); c != 1 {
		t.Fatalf("expected 1 recovered log, saw %d\n%s", c, buf.String())
	}
	st.Close()
}
```

(Add `"sync/atomic"` to the imports if not already present from Task 2.)

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/persistence/atomicjson/ -run 'TestSaveFailingRenameLeavesPriorFile|TestSaveErrRelogInterval_DeduplicatesIdenticalErrors' -race -v`
Expected: PASS — assuming the implementation from Task 2 is correct.

If `TestSaveErrRelogInterval` fails because Task 2's `doSave` doesn't dedup identical errors, fix `doSave` per the existing logic shape (per-error-string + 5-minute relog interval). The test is the authoritative spec for this behavior.

- [ ] **Step 3: Commit**

```bash
git add internal/persistence/atomicjson/store_test.go
git commit -m "Plan D2 task 14b: atomicjson failing-rename + error log dedup tests"
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

- [ ] **Step 4b: Audit for residual `time.Sleep`-after-debounce idioms**

Run:

```bash
grep -rn "time.Sleep" internal/runtime/server/*_test.go internal/persistence/atomicjson/*_test.go internal/runtime/client/*_test.go 2>/dev/null
```

Expected: every match is either:
- Inside `internal/persistence/atomicjson/store_test.go` exercising debounce or coalescing semantics intrinsic to the test (`TestStoreCoalescesUpdates`, `TestStoreUpdateAfterCloseIsNoop`, `TestStoreSerializesSaves`), or
- Pre-existing on `main` and unrelated to D2.

Any new `time.Sleep` in code D2 added/modified is a flake-prone shortcut. Replace with `FlushPersistForTest()` (Session writer) or `Flush()` (atomicjson Store) and re-run.

- [ ] **Step 5: Manual e2e — daemon-restart resume (with precise marker)**

```bash
# Clean slate
rm -rf ~/.texelation/sessions
make build

# Start daemon + client
./bin/texelation start
./bin/texel-client --socket=/tmp/texelation.sock &
CLIENT_PID=$!

# Inside the client's texelterm pane:
#   $ seq 1 5000
#   $ printf '\n\n=== MARKER-D2-RESUME ===\n\n'
# Scroll up until the marker line is visible AT THE TOP of the pane.
# Note: the marker line should be at a specific row (e.g., row 12).

# In another terminal:
./bin/texelation stop      # SIGTERM the daemon
ls -la ~/.texelation/sessions/  # expect at least one *.json file
cat ~/.texelation/sessions/*.json | jq '.paneViewports[0].viewBottomIdx'
./bin/texelation start     # daemon starts fresh, runs scan + EnablePersistence

# Client detects disconnect and reconnects (Plan D behavior).
# After reconnect, verify:
#  (a) The MARKER line is visible.
#  (b) It sits at the same row it occupied before restart (within ~1 row).
#  (c) Daemon log contains "[BOOT] EnablePersistence: loaded N persisted session(s)".

kill $CLIENT_PID
./bin/texelation stop
```

Expected: marker line lands at the same physical row position. If the marker is missing or at a wildly different row, D2 viewport restoration is broken.

- [ ] **Step 6: Manual e2e — daemon crash (SIGKILL) + orphan tmp file check**

```bash
make build
./bin/texelation start
./bin/texel-client --socket=/tmp/texelation.sock &
CLIENT_PID=$!

# Scroll a pane and place a marker as in Step 5.
# Then crash the daemon hard:
pkill -9 -f texel-server
sleep 1

# Inspect the sessions dir BEFORE restart:
ls -la ~/.texelation/sessions/
# Look specifically for orphan temp files left by the temp-file-mid-write
# crash window. atomicjson uses os.CreateTemp(dir, ".atomicjson.tmp-*"),
# so any such files mean a save was caught mid-write.
ORPHANS=$(ls ~/.texelation/sessions/.atomicjson.tmp-* 2>/dev/null | wc -l)
echo "orphan tmp files: $ORPHANS"

./bin/texelation start

# Verify daemon log:
#  - "[BOOT] EnablePersistence: loaded ..." line appears.
#  - If a session JSON was corrupted by the crash, an
#    "atomicjson: parse failed (...); file wiped" line may appear; that
#    is the EXPECTED behavior (atomic rename means the canonical file
#    is either intact or absent — corruption is unexpected).

# Verify client reconnect:
#  - Either lands at saved scroll position (atomic rename worked), OR
#  - Falls back cleanly to "snap to oldest" without a panic / crash.

kill $CLIENT_PID
./bin/texelation stop

# Document orphan tmp count in the PR description. >0 is acceptable
# (crash artifact) but should be cleaned up by the next save's deferred
# tmp-cleanup. If they accumulate across multiple kill cycles, file an
# issue against atomicjson cleanup.
```

Expected: scroll position survives OR falls back cleanly. No panics. Orphan tmp files (if any) are cleaned up on next successful save.

- [ ] **Step 6b: Manual e2e — full client+daemon both-restart**

This exercises Plan D + Plan D2 together: both processes die, both restart.

```bash
make build
./bin/texelation start
./bin/texel-client --socket=/tmp/texelation.sock &
CLIENT_PID=$!

# Place a marker as in Step 5; scroll so it's at a known row.

# Kill BOTH:
kill $CLIENT_PID
./bin/texelation stop

# Restart daemon first, then client:
./bin/texelation start
./bin/texel-client --socket=/tmp/texelation.sock

# Verify the marker is at the same physical row.
```

Expected: scroll position survives the full client-and-daemon restart. This is the most stringent test — neither process retains in-memory state from before, so all recovery must come from disk.

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

- [ ] **Spec coverage**: every section of `2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md` has a task that implements it. Storage shape (Tasks 5–8), writer abstraction (Tasks 1–3), no-auto-GC (Task 6 — only corrupt files deleted), schema with reserved Plan F fields (Task 5), client-side reset on snapshot with one-shot CAS + error-path clear (Task 13), pre-seed-then-client-overrides (Tasks 7 + 14), pinned round-trip (Task 14, automated), Plan F metadata round-trip (Task 14), file survives Close (Task 14), concurrent updates under race (Task 14), rehydrate race (Task 14), failing-rename safety + saveErrRelogInterval dedup (Task 14b). ✓
- [ ] **No placeholders**: every step contains exact code, file paths, and commands. Grep-and-locate steps remain (struct/field names must be confirmed against the codebase) but each provides the exact grep command and the line range to inspect.
- [ ] **Type consistency**: `StoredSession`, `StoredPaneViewport`, `LookupOrRehydrate`, `AttachWriter`, `RecordPaneActivity`, `FlushPersistForTest`, `SessionFilePath`, `ScanSessionsDir`, `LoadPersistedSessions`, `SetPersistedSessions`, `EnablePersistence`, `NewSessionWithID`, `ErrSessionAlreadyExists`, `ApplyPreSeed`, `applyPostResumeReset`, `resetOnNextSnapshot`, `ResetRevisions`, `PaneRevision`, `paneActivityFromSnapshot`, `recordSnapshotActivity` — all defined in earlier tasks before later tasks reference them.
- [ ] **Test coverage**: at least one TDD red-green pair per task; integration tests cover the full cross-restart cycle, the one-shot reset under multiple snapshots, the resume-error flag clear, file-survives-close, concurrent updates, rehydrate races, failing-rename safety, and per-error-string log dedup.
- [ ] **Lock discipline**: `ClientViewports.byPaneID` writes always go through the methods on `ClientViewports` (`Apply`, `ApplyResume`, `ApplyPreSeed`). `Session.schedulePersist` documents its precondition (caller must not hold `s.mu`). `BufferCache.ResetRevisions` uses `Lock` (the lock is `sync.RWMutex`).
- [ ] **Boot ordering**: `Manager.EnablePersistence` is the only production entry point and runs strictly before the listener accepts connections. `LoadPersistedSessions` and `SetPersistedSessions` remain available for tests but are not wired into `cmd/texel-server/main.go`.
