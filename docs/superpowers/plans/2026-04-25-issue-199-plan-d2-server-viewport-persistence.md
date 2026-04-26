# Issue #199 Plan D2 — Server-Side Cross-Daemon-Restart Viewport Persistence

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist per-session pane viewport state on the server so a daemon restart followed by a client resume lands at the saved scroll position.

**Architecture:** Lazy rehydrate-on-resume keyed by sessionID. New on-disk store at `<snapshot-dir>/sessions/<hex-sessionID>.json`. Extract a shared `DebouncedAtomicJSONStore` primitive from Plan D's client `Writer` and use it for both client- and server-side persistence. Cross-restart sequence/revision continuity handled by client-side reset on the post-resume `MsgTreeSnapshot` (gated by a one-shot resume flag).

**Tech Stack:** Go 1.24.3, JSON encoding, atomic temp+rename, `time.AfterFunc` debounce, generic over snapshot type via `encoding/json`.

**Spec:** `docs/superpowers/specs/2026-04-26-issue-199-plan-d2-server-viewport-persistence-design.md`

**Branch:** `feature/issue-199-plan-d2-server-viewport-persistence`

> **Plan revisions (three rounds of parallel-agent review):** The plan was revised three times in response to four-agent review passes before any code was written. Round 1 caught: `ClientViewports.ApplyPreSeed` for proper locking; dropped `EnqueueDiff`/`EnqueueImage` writer hooks (allocation risk); combined `LoadPersistedSessions` + `SetPersistencePath` into a single `Manager.EnablePersistence`; rewrote Task 11 as a real TDD pair; replaced non-existent `testutil.NewMemConnPair` with `testutil.NewMemPipe(64)`; cleared the resume flag on `RequestResume` error; added atomicjson failing-rename + `saveErrRelogInterval` dedup tests; replaced `prevMgrNewSessionWithID` escape hatch with `Manager.NewSessionWithID`; added e2e markers, orphan-tmp checks, and full client+daemon restart scenario. Round 2 caught: forward-ref bug (`preSeedViewports` → `ApplyPreSeed`); Task 10/8 wiring contradiction; `Session.schedulePersist` close-vs-update race (capture under lock); invalid `protocol.BufferDelta` literal fields (replaced with a `seedRevision` helper); rewrote failing-rename test; corrected `EnablePersistence` docstring; `TestD2_PhantomPaneFilterAfterPreSeed`; `time.Sleep` audit step. Round 3 caught: 11 sites used `protocol.ViewportUpdate{ViewportRows, ViewportCols}` but the real fields are `Rows`/`Cols`; the Manager variable is `manager` (not `mgr`) and `--from-scratch` mode skips `snapPath` so persistence must be gated; Task 14b imports duplicated `errors` (must merge into existing block); `TestSaveFailingRenameLeavesPriorFile` bypassed `atomicjson.Save` entirely (now uses package-level `renameFn` hook); phantom-pane unbounded growth across restarts (added `ClientViewports.PrunePhantoms`, called in `connection_handler` MsgResumeRequest path); `Manager.Close` and `SetDiffRetentionLimit` held `m.mu` during the now-blocking writer flush (Task 12b refactor drops the lock first); `bytes.Buffer` data race in dedup test (wrapped in `syncBuf`).

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

// renameFn is the rename primitive used by Save. Defaults to os.Rename
// in production; tests override it to simulate rename-failure paths
// (e.g. cross-device link, EACCES) without needing a real filesystem
// trigger. Reset to os.Rename in test cleanup.
var renameFn = os.Rename

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
	if err := renameFn(tmpPath, path); err != nil {
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
		Rows:          24,
		Cols:          80,
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
	sess.ApplyViewportUpdate(protocol.ViewportUpdate{PaneID: [16]byte{0x01}, ViewBottomIdx: 1, Rows: 1, Cols: 1})
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
		PaneID: [16]byte{0xaa}, ViewBottomIdx: 1, Rows: 1, Cols: 1,
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
		PaneID: [16]byte{0xbb}, ViewBottomIdx: 99, Rows: 1, Cols: 1,
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

In `LookupOrRehydrate`, after the rehydrated session is registered, attach the writer. Pre-seed viewports via the locked `ApplyPreSeed` method, then filter phantoms against the live pane tree if a predicate is available. Without the filter, a stored entry whose `PaneID` was destroyed during the prior daemon's lifetime would persist forever, growing `byPaneID` monotonically across restarts (Plan B review-findings #4 — Plan D2 closes that gap):

```go
// LookupOrRehydrate returns an existing live Session, or rehydrates one
// from disk if the persistedSessions index has it. Pre-seeds viewports
// from disk; the caller is expected to call PrunePhantomPanes(pred)
// after the live pane tree is known so dead PaneIDs are dropped before
// any writer flushes them back to disk.
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

Add a pruning method on `*ClientViewports` (next to `ApplyPreSeed`) so phantoms can be removed under the lock:

```go
// PrunePhantoms removes pane viewports whose IDs no longer exist
// according to the predicate. Called after rehydration and after the
// live pane tree is known. Without this, pre-seeded entries for panes
// destroyed during the prior daemon's lifetime would persist
// indefinitely and write back to disk, growing the on-disk file
// monotonically across restarts.
func (c *ClientViewports) PrunePhantoms(paneExists func(id [16]byte) bool) int {
	if paneExists == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	dropped := 0
	for id := range c.byPaneID {
		if !paneExists(id) {
			delete(c.byPaneID, id)
			dropped++
		}
	}
	return dropped
}
```

The connection handler then prunes phantoms exactly once, after `MsgResumeRequest` has identified the live pane tree (the existing `paneExists` predicate built from `desktop.AppByID(...)` at `connection_handler.go:163-175` is already the right shape):

```go
// In the MsgResumeRequest case in connection_handler.go, immediately
// after the existing `viewportsToApply` pruning loop and before
// c.session.ApplyResume:
if pruned := c.session.viewports.PrunePhantoms(func(p [16]byte) bool {
    return desktop.AppByID(p) != nil
}); pruned > 0 {
    debugLog.Printf("connection %x: pruned %d phantom pre-seed viewport(s)", c.session.ID(), pruned)
}
```

(Add this snippet to the existing handler block guarded by `if sinkOK && sink.Desktop() != nil`. The pre-seed entries for panes that no longer exist server-side are dropped before `ApplyResume` runs and before the writer ever flushes them back to disk.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/runtime/server/ -run 'TestManager.*Writer' -race -v`
Expected: PASS.

- [ ] **Step 5: Wire `EnablePersistence` into `cmd/texel-server/main.go`**

Task 8 deliberately did NOT touch `cmd/texel-server/main.go` — the production wiring lives entirely here.

The `*server.Manager` instance is bound to a local variable named `manager` (NOT `mgr`) at `cmd/texel-server/main.go:123`. The snapshot path is resolved inside the `if !*fromScratch` block (around lines 194 and 234, both branches resolve `snapPath` to a non-empty string). The listener entry point is `srv.Start(...)` around line 266.

Confirm the call sites first:

```bash
grep -n "manager :=\|snapPath :=\|srv.Start\|srv\.Listen" cmd/texel-server/main.go
```

Then add this call **inside the `if !*fromScratch` block, after `snapPath` is fully resolved and BEFORE `srv.Start(...)` is invoked**:

```go
if err := manager.EnablePersistence(filepath.Dir(snapPath), 250*time.Millisecond); err != nil {
    log.Printf("warning: could not enable persistence: %v", err)
}
```

**Why inside the `if !*fromScratch` block.** When the operator passes `--from-scratch`, `snapPath` is never assigned and `filepath.Dir("")` returns `"."`, which would land session files in the daemon's cwd — wrong. `--from-scratch` deliberately skips the snapshot store, and D2 persistence shares that intent: skip when the snapshot path itself is skipped. Persistence-from-scratch is opt-out aligned with snapshot-from-scratch.

Verify the ordering with:

```bash
grep -n "Listen\|Accept\|ListenAndServe\|EnablePersistence\|srv\.Start\|Server\.Start" cmd/texel-server/main.go
```

`EnablePersistence` MUST appear lexically (and run sequentially) before any line that opens the listener or starts the connection acceptor. Per the spec's "Boot-scan-before-listener invariant," a `MsgResumeRequest` arriving during the scan window would falsely return `ErrSessionNotFound`.

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

### Task 12b: Refactor `Manager.Close` and `SetDiffRetentionLimit` to drop `m.mu` before calling `Session.Close` / `Session.setMaxDiffs`

**Why this task exists.** Before D2, `Session.Close` was instant — it cleared the diff queue under `s.mu` and returned. After Task 9 attaches an atomicjson writer, `Session.Close` synchronously calls `w.Close()` which runs `Flush()` and waits on the writer's `wg`, doing disk I/O. The existing `Manager.Close(id)` (`internal/runtime/server/manager.go:67-74`) and `Manager.SetDiffRetentionLimit` (`:55-65`) both hold `m.mu` while iterating sessions and calling per-session methods that now block on disk. That blocks every other Manager call (`NewSession`, `Lookup`, `LookupOrRehydrate`, `ActiveSessions`, `SessionStats`) for the duration of the flush. Behavioral regression that should ship with D2, not in a separate cleanup PR.

**Files:**
- Modify: `internal/runtime/server/manager.go`
- Test: `internal/runtime/server/manager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `manager_test.go`:

```go
func TestManagerCloseDropsLockBeforeSessionClose(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager()
	if err := mgr.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	id := [16]byte{0xfe, 0xed}
	sess, err := mgr.NewSessionWithID(id)
	if err != nil {
		t.Fatal(err)
	}
	// Apply enough updates to make Close's flush non-trivial.
	for i := 0; i < 10; i++ {
		sess.ApplyViewportUpdate(protocol.ViewportUpdate{
			PaneID: [16]byte{byte(i)}, ViewBottomIdx: int64(i), Rows: 1, Cols: 1,
		})
	}

	closeStarted := make(chan struct{})
	closeDone := make(chan struct{})
	go func() {
		close(closeStarted)
		mgr.Close(id) // synchronous — must not block other Manager methods
		close(closeDone)
	}()
	<-closeStarted

	// While Close is running, ActiveSessions must return promptly. If
	// Close held m.mu through the disk flush, this call would block.
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatalf("ActiveSessions blocked while Close was running — m.mu held during disk I/O")
		case <-closeDone:
			return
		default:
			_ = mgr.ActiveSessions() // must not deadlock
			time.Sleep(time.Millisecond)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (or hangs)**

Run: `go test ./internal/runtime/server/ -run TestManagerCloseDropsLockBeforeSessionClose -timeout=10s -v`
Expected: FAIL or TIMEOUT — the current `Manager.Close` holds `m.mu` through `session.Close()`.

- [ ] **Step 3: Refactor `Manager.Close` and `SetDiffRetentionLimit`**

In `internal/runtime/server/manager.go`:

```go
// Close removes the session from the live map and tears it down. The
// teardown call (which now blocks on disk I/O via the atomicjson
// writer's flush) runs OUTSIDE m.mu so other Manager methods don't
// stall behind a slow flush.
func (m *Manager) Close(id [16]byte) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		session.Close() // disk flush — outside m.mu
	}
}

// SetDiffRetentionLimit applies the new limit to all live sessions.
// Capture the slice under m.mu, then walk it without the lock — the
// per-session call may take a per-session lock and we don't want to
// block other Manager ops on that.
func (m *Manager) SetDiffRetentionLimit(limit int) {
	if limit < 0 {
		limit = 0
	}
	m.mu.Lock()
	m.maxDiffs = limit
	live := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		live = append(live, s)
	}
	m.mu.Unlock()
	for _, s := range live {
		s.setMaxDiffs(limit)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/runtime/server/ -run TestManagerCloseDropsLockBeforeSessionClose -race -v`
Expected: PASS.

- [ ] **Step 5: Run the full Manager suite**

Run: `go test ./internal/runtime/server/ -run TestManager -race -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/manager.go internal/runtime/server/manager_test.go
git commit -m "Plan D2 task 12b: drop m.mu before Session.Close to allow concurrent Manager ops"
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
		Rows:          30,
		Cols:          100,
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
		Rows:          30,
		Cols:          100,
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
		PaneID: [16]byte{0xff}, ViewBottomIdx: 1, Rows: 1, Cols: 1,
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
		PaneID: [16]byte{0x01}, ViewBottomIdx: 1, Rows: 1, Cols: 1,
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
					PaneID:        [16]byte{byte(g)},
					ViewBottomIdx: int64(i),
					Rows:          24,
					Cols:          80,
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

// TestD2_PhantomPaneFilterAfterPreSeed: pre-seed places viewports for
// real and dead panes; PrunePhantoms drops the dead one against the
// live pane tree; ApplyResume's paneExists filter drops the
// client-supplied phantom. Both filters must work — without the first,
// dead PaneIDs persist back to disk and grow monotonically across
// daemon restarts (Plan B review-findings #4).
func TestD2_PhantomPaneFilterAfterPreSeed(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xee, 0xee}
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

	// Pre-seed accepts everything on disk (pruning is the next step).
	if _, ok := sess.Viewport(realPane); !ok {
		t.Fatalf("real pane viewport missing after pre-seed")
	}
	if _, ok := sess.Viewport(deadPane); !ok {
		t.Fatalf("phantom pane should be pre-seeded too — pruning runs next")
	}

	// Simulate the connection handler's prune-against-live-tree call
	// (see connection_handler.go MsgResumeRequest case in Task 7's
	// integration). paneExists returns true only for realPane.
	paneExists := func(p [16]byte) bool { return p == realPane }
	if dropped := sess.viewports.PrunePhantoms(paneExists); dropped != 1 {
		t.Fatalf("PrunePhantoms: got %d, want 1", dropped)
	}

	// deadPane gone from server-side viewport map. realPane still here.
	if _, ok := sess.Viewport(realPane); !ok {
		t.Fatalf("real pane should remain after prune")
	}
	if _, ok := sess.Viewport(deadPane); ok {
		t.Fatalf("dead pane MUST be pruned to prevent disk-side growth")
	}

	// Client also sends a payload with its own phantom (e.g. raced
	// against a pane close). ApplyResume's paneExists filter drops it.
	clientPayload := []protocol.PaneViewportState{
		{PaneID: realPane, ViewBottomIdx: 150, ViewportRows: 24, ViewportCols: 80},
		{PaneID: [16]byte{0xde, 0xad}, ViewBottomIdx: 999, ViewportRows: 24, ViewportCols: 80},
	}
	sess.ApplyResume(clientPayload, paneExists)

	if vp, _ := sess.Viewport(realPane); vp.ViewBottomIdx != 150 {
		t.Fatalf("realPane: got %d want 150", vp.ViewBottomIdx)
	}
	if _, ok := sess.Viewport([16]byte{0xde, 0xad}); ok {
		t.Fatalf("client-supplied phantom must be filtered by paneExists")
	}

	// Force a flush; the writer must NOT persist the pruned phantom or
	// the client phantom back to disk. This is the key cross-restart
	// hygiene assertion: persisted file shrinks when phantoms exist.
	sess.FlushPersistForTest()
	loaded, _ := os.ReadFile(SessionFilePath(dir, id))
	var afterFlush StoredSession
	if err := json.Unmarshal(loaded, &afterFlush); err != nil {
		t.Fatal(err)
	}
	for _, p := range afterFlush.PaneViewports {
		if p.PaneID == deadPane || p.PaneID == [16]byte{0xde, 0xad} {
			t.Fatalf("phantom pane persisted to disk: %x", p.PaneID)
		}
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

Append to `internal/persistence/atomicjson/store_test.go`. The file already has an import block from Tasks 1 and 2 with at least `errors`, `os`, `path/filepath`, `sync`, `sync/atomic`, `testing`, `time`. **Do not append a second `import (...)` block** — Go forbids re-importing an already-imported package, and `errors` would collide. Instead, **merge** these new imports into the existing block:

```
"bytes"   // new — log buffer in TestSaveErrRelogInterval
"log"     // new — log.SetOutput
"strings" // new — strings.Count / strings.HasPrefix
```

Then append the test functions:

```go

// TestSaveFailingRenameLeavesPriorFile exercises the REAL atomicjson.Save
// via the package-level renameFn hook. After a successful initial save,
// renameFn is swapped to one that always returns an error. A subsequent
// Save must:
//
//   1. Return the rename error (so callers know the save didn't land).
//   2. Leave the canonical file intact (atomic-rename guarantee).
//   3. Trigger the deferred os.Remove(tmpPath) cleanup so no orphan
//      tmps accumulate in the directory.
//
// This is the spec-promised "atomic-write semantics" property — testing
// it through Save (rather than a saver-injection bypass) means the
// production deferred-cleanup path is what's under test.
func TestSaveFailingRenameLeavesPriorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	// First save: succeeds, populates canonical file.
	good := &fakePayload{Name: "good", Count: 1}
	if err := Save(path, good); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Override the rename primitive to always fail. Restore on cleanup.
	origRename := renameFn
	renameFn = func(oldpath, newpath string) error {
		return errors.New("synthetic rename failure")
	}
	t.Cleanup(func() { renameFn = origRename })

	// Second save: encodes successfully into a tmp file, but renameFn
	// returns an error. Save's defer runs os.Remove(tmpPath).
	bad := &fakePayload{Name: "replacement", Count: 999}
	err := Save(path, bad)
	if err == nil {
		t.Fatalf("Save expected to return rename error, got nil")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Fatalf("Save error should mention rename, got: %v", err)
	}

	// Canonical file still has the original payload.
	got, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.Name != "good" || got.Count != 1 {
		t.Fatalf("canonical file modified by failing-rename path: got %+v", got)
	}

	// No orphan .atomicjson.tmp-* files leak. Save's deferred cleanup
	// is what we're verifying — without it, the tmp file from the
	// failed Save call would be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".atomicjson.tmp-") {
			t.Errorf("orphan tmp file leaked after failing rename: %s", name)
		}
	}
}

// TestSaveErrRelogInterval_DeduplicatesIdenticalErrors: repeated
// identical errors are logged once, not on every retry. A different
// error string logs immediately. Recovery (success after failure)
// emits one "save recovered" line.
//
// Concurrency note: the Store's debounced writer fires log.Printf from
// internal goroutines (timer ticks). Since log.SetOutput is global and
// bytes.Buffer is NOT concurrency-safe, we wrap the buffer in a mutex
// via syncBuf. Without the wrapper, -race flags a data race between
// the tick goroutine writing logs and the test reading buf.String().
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestSaveErrRelogInterval_DeduplicatesIdenticalErrors(t *testing.T) {
	buf := &syncBuf{}
	prev := log.Writer()
	log.SetOutput(buf)
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

	// 5 saves with the same error — exactly 1 log line. Flush after
	// every Update so the saver runs synchronously on the test
	// goroutine instead of via the debounce timer; this makes the
	// "exactly N log lines" count deterministic.
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

(`sync/atomic` and `sync` are already in the imports from Task 2; the new symbols only require the merged `bytes` / `log` / `strings` from Step 1 above.)

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

## Task 16 (Follow-up after PR merges): Pane top/bottom border rendering bug

**Status when this task was added (2026-04-26):** Plan D2 Tasks 1–15 are functionally complete; unit + integration tests pass; the daemon-restart rehydrate path works end-to-end. During manual e2e (Step 5 above) the user reported a visual glitch on rehydrate that turned out to be a *pre-existing* rendering bug — Plan D2 just made it visible. **Do not block the Plan D2 PR on this fix.** Land Plan D2, then attack this in a focused follow-up plan.

### Symptom (as reported during Plan D2 e2e on branch `feature/issue-199-plan-d2-server-viewport-persistence`)

After daemon restart + reconnect (screen 255×61, status 255×2 at top, workspace pane Rect 255×59 at y=2):

- Status bar renders correctly at the top.
- 3 "lighter gray" rows appear at the top of the pane area (rows 2–4 of the screen), with no border characters.
- Below those, content fills the rest of the pane area down to the last screen row.
- Top border missing. Bottom border missing. Left/right borders ARE present (because they're embedded in each content row, columns 0 and W-1).

User wording: *"3 Lighter gray rows (And first 3 lines of content). All lines till the bottom of the screen the terminal background color (with the rest of the content). Content goes from the status bar to the last line of the screen and from the 0 column to the last. No borders. all available space."*

### Root cause

The bug is in the pane-row → globalIdx mapping the client uses to render terminal panes. **Top/bottom borders never reach the client** for non-altScreen panes:

1. `texel/snapshot.go capturePaneSnapshot` builds `RowGlobalIdx` for the pane buffer with `RowGlobalIdx[0] = -1` (top border), `RowGlobalIdx[H-1] = -1` (bottom border). Texelterm's internal statusbar at `RowGlobalIdx[H-2]` is also `-1`. Only the inner content rows get valid gids.
2. `internal/runtime/server/desktop_publisher.go bufferToDelta` (around line 299) filters non-altScreen rows with `if gid < 0 { continue }`. That drops the 3 non-content rows.
3. So PaneCache on the client only ever has the content gids.
4. `internal/runtime/client/viewport_tracker.go onBufferDelta` (around line 263) computes `top := maxGid - int64(vp.Rows-1)`, treating ALL `vp.Rows` rows of the pane as content.
5. `internal/runtime/client/renderer.go rowSourceForPane` (around line 181) does `gid := vc.ViewTopIdx + int64(rowIdx)` and `pc.RowAt(gid)`. With pane.Rows=59 and only 56 content gids in cache, the gid range projected is `[maxGid-58 .. maxGid]`. The 3 lowest gids in that range are NOT in PaneCache → blank cells.
6. Those 3 blank rows land at rowIdx 0, 1, 2 — i.e., the TOP of the pane area, exactly where the user sees the "3 lighter gray rows."

### Worked example (matches the reported symptom)

Texelterm pane: pane.Rect 255×59 (W=255, H=59), drawableH=57, terminal grid = 56 content rows + 1 internal statusbar.

After rehydrate, sparse store restored at `writeTop=4960`, `cursor=5015`. So texelterm's `lastRowGlobalIdx = [4960, 4961, ..., 5015]` (56 entries). `capturePaneSnapshot` produces `RowGlobalIdx = [-1, 4960, 4961, ..., 5015, -1, -1]` (59 entries; index 0 = top border, 1..56 = content, 57 = internal statusbar, 58 = bottom border).

`bufferToDelta` (autoFollow path) emits rows for gids 4960..5015 with `delta.RowBase = lo = maxGid - Rows - overscan + 1`. PaneCache.main ends up with keys 4960..5015.

Client `onBufferDelta`: `maxGid=5015`, `vp.Rows=59`, so `top = 5015 - 58 = 4957`. `vp.ViewTopIdx = 4957`.

Renderer iterates rowIdx 0..58 (h=59):
- rowIdx 0 → gid 4957 (NOT in cache) → blank
- rowIdx 1 → gid 4958 (NOT in cache) → blank
- rowIdx 2 → gid 4959 (NOT in cache) → blank
- rowIdx 3 → gid 4960 (IN cache) — first content row
- ...
- rowIdx 58 → gid 5015 (IN cache) — last content row

That's exactly **3 blank rows at the top + 56 content rows** = 59 = pane.Height. The user's "3 lighter gray rows + content fills the rest" matches perfectly.

### Why clean start "looks fine"

The bug is not rehydrate-specific; it's always present. On clean start `maxGid` is small (cursor near the top of a fresh terminal), so `top = maxGid - 58` clamps to 0. The renderer queries gids 0..58. PaneCache has gids 0..N (just the prompt rows). Content fills from rowIdx 0 (overwriting where the top border *would* be) and blanks fill below. The user reads "prompt at top of pane, blanks below" as the natural look of an empty terminal — there's no visual cue that the top border was overwritten or that the bottom border is blank. Hence "clean start works fine" reports.

### Confirming via the persisted session file

The session JSON written at disconnect (`~/.texelation/sessions/<id>.json`) records `rows: 59, cols: 255, autoFollow: true, viewBottomIdx: 5016` for the texelterm pane. That confirms the publisher's autoFollow path runs at rehydrate time with vp.Rows=59 — i.e., the math above is what actually happens.

### Out-of-scope quick fixes already considered and rejected

- **`top := minGidInDelta - 1`** — works for the steady-state delta where minGid is the first content gid, but breaks for partial/incremental autoFollow deltas where minGid is from a small range (e.g., only the last few rows changed). Would silently corrupt the view on subsequent updates.
- **Setting `vp.Rows = numContentRows` from the client side** — can't, the client doesn't know how many non-content rows the pane has. Texelterm has 1 internal statusbar; other apps (clock, launcher) have 0.

### Recommended fix design (do this in a follow-up plan)

Pick one of these. **Option 4 + Option 2 in combination is probably the right shape** (server tells client where content sits inside the pane, client renders borders itself).

**Option 1 — Server emits border rows via a new flag.** Add `BufferDeltaBorderRows` flag and an extra field on `BufferDelta` carrying the top + bottom border row cells (flat-keyed). Client stores them in PaneCache and the renderer picks the right source based on `rowIdx == 0` / `rowIdx == H-1`. *Cost:* protocol change, more bandwidth on every delta.

**Option 2 — Client renders borders itself from `pane.Rect` + metadata.** The client already has `pane.Active`, `pane.Rect`, `pane.Title`, `pane.ZOrder`. It can draw the border using its own theme colors and route titles through `MsgPaneState`. *Cost:* duplicates server's `pane.border.Draw` logic on the client; needs theming parity. *Win:* zero protocol churn, no per-frame border re-shipping, fixes the "borders never arrive" problem at its root rather than papering over it.

**Option 3 — Populate `PaneSnapshot.Rows` in `treeCaptureToProtocol` for the initial frame so borders arrive with the snapshot.** *Cost:* every snapshot ships full buffer once; doesn't update borders if title/active change without a re-snapshot.

**Option 4 — Add `ContentTopRow`, `ContentBottomRow` fields to `protocol.PaneSnapshot`.** Server populates them from the pane's border thickness + texelterm's internal statusbar. Client uses them in `rowSourceForPane`:
```go
contentRowIdx := rowIdx - pane.ContentTopRow
if contentRowIdx < 0 || contentRowIdx > (pane.ContentBottomRow - pane.ContentTopRow) {
    return nil // border / decoration row
}
gid := vc.ViewTopIdx + int64(contentRowIdx)
row, found := pc.RowAt(gid)
```
And `onBufferDelta` becomes `top := maxGid - int64(numContentRows-1)` where `numContentRows = ContentBottomRow - ContentTopRow + 1`. *Cost:* protocol fields, capturePaneSnapshot needs to compute the metadata. *Win:* fixes the rowIdx → gid mapping at the point of truth without changing the publisher filter or shipping border cells.

### Touchpoints when fixing

- `texel/snapshot.go capturePaneSnapshot` (RowGlobalIdx assignment, possibly add ContentTopRow/Bottom)
- `internal/runtime/server/tree_convert.go treeCaptureToProtocol` (currently sets `Rows: nil`; would change for Option 3 or pass new fields for Option 4)
- `internal/runtime/server/desktop_publisher.go bufferToDelta` (gid filter; possibly add border-rows path for Option 1)
- `internal/runtime/client/viewport_tracker.go onBufferDelta` (top calculation)
- `internal/runtime/client/renderer.go rowSourceForPane` (rowIdx → gid mapping; possibly add border-rendering branch)
- `protocol/messages.go PaneSnapshot` and `BufferDelta` (new fields/flags depending on option)

### Verification plan

After fix lands:

1. **Unit:** golden-row tests for `bufferToDelta` proving border rows are conveyed (or equivalently, that `rowSourceForPane` falls through to the right source for rowIdx 0 and H-1).
2. **Integration:** capture pane render output for a 60×20 main-screen pane on (a) clean start, (b) cross-daemon-restart rehydrate at the live edge, (c) cross-daemon-restart rehydrate while scrolled mid-history. All three must show top + bottom + left + right borders. Use `internal/runtime/server/testutil/memconn.go` and snapshot-comparison.
3. **Manual e2e:** repeat Plan D2's Step 6 ("daemon AND client both restart"). Verify borders are visible and content sits inside them.

### Captured memory pointer

A condensed version of this analysis is also stored at `~/.claude/projects/-home-marc-projects-texel-texelation/memory/project_issue199_pane_border_render_bug.md` with the same root cause, options, and touchpoints — read either source when picking this up.

---

## Self-Review Checklist (run before declaring the plan done)

- [ ] **Spec coverage**: every test in the spec's "Test plan" maps to a task in the plan; every test the plan introduces appears in the spec's list. Lock-bearing tests: `TestD2_FullCrossRestartCycle`, `TestD2_PinnedRoundTrip`, `TestD2_FileSurvivesSessionClose`, `TestD2_ConcurrentUpdates`, `TestD2_RehydrateRaceForSameID`, `TestD2_PhantomPaneFilterAfterPreSeed` (now asserts pruning, not just filter), `TestApplyPostResumeReset_*`, `TestSaveFailingRenameLeavesPriorFile` (via real `Save` + `renameFn` hook), `TestSaveErrRelogInterval_DeduplicatesIdenticalErrors` (with `syncBuf` data-race fix), `TestManagerCloseDropsLockBeforeSessionClose`, `TestManagerNewSessionWithID_*`. ✓
- [ ] **No placeholders**: every step contains exact code, file paths, and commands. Grep-and-locate steps provide the exact grep command and the line range to inspect.
- [ ] **Type consistency**: `StoredSession`, `StoredPaneViewport`, `LookupOrRehydrate`, `AttachWriter`, `RecordPaneActivity`, `FlushPersistForTest`, `SessionFilePath`, `ScanSessionsDir`, `LoadPersistedSessions`, `SetPersistedSessions`, `EnablePersistence`, `NewSessionWithID`, `ErrSessionAlreadyExists`, `ApplyPreSeed`, `PrunePhantoms`, `applyPostResumeReset`, `resetOnNextSnapshot`, `ResetRevisions`, `PaneRevision`, `paneActivityFromSnapshot`, `recordSnapshotActivity`, `seedRevision`, `renameFn`, `syncBuf` — all defined in earlier tasks before later tasks reference them.
- [ ] **Test coverage**: at least one TDD red-green pair per task; integration tests cover the full cross-restart cycle, the one-shot reset under multiple snapshots, the resume-error flag clear, file-survives-close, concurrent updates, rehydrate races, real-`Save` failing-rename safety, per-error-string log dedup, phantom-pane pruning (regression guard for Plan B finding #4), and `Manager.Close` lock-release-before-flush.
- [ ] **Lock discipline**: `ClientViewports.byPaneID` writes always go through the methods on `ClientViewports` (`Apply`, `ApplyResume`, `ApplyPreSeed`, `PrunePhantoms`). `Session.schedulePersist` reads `s.writer` under `s.mu` (close-vs-update race). `Manager.Close` and `SetDiffRetentionLimit` drop `m.mu` before per-session calls that block on disk. `BufferCache.ResetRevisions` uses `Lock` (the lock is `sync.RWMutex`).
- [ ] **Boot ordering**: `Manager.EnablePersistence` is the only production entry point and runs strictly before the listener (`srv.Start`) accepts connections. The wiring is gated inside `cmd/texel-server/main.go`'s `if !*fromScratch` block so `snapPath` is non-empty. `LoadPersistedSessions` and `SetPersistedSessions` remain available for tests but are not wired into `cmd/texel-server/main.go`.
- [ ] **Field-name correctness**: `protocol.ViewportUpdate{}` uses `Rows`/`Cols` (NOT `ViewportRows`/`ViewportCols`); `protocol.PaneViewportState{}` uses `ViewportRows`/`ViewportCols`. The plan deliberately uses each in the right context.
