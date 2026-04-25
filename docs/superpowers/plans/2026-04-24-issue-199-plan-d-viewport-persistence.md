# Plan D: Client-Side Session & Viewport Persistence (Issue #199) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist `sessionID`, `lastSequence`, and per-pane viewport state to disk on the client so a fresh `texel-client` process resumes against the still-running daemon and lands at the exact viewport it left.

**Architecture:** New `internal/runtime/client/persistence.go` provides `Load`/`Save`/`Wipe` over a JSON file at `${XDG_STATE_HOME:-~/.local/state}/texelation/client/<socketHash>/<clientName>.json`, atomic-replace via temp+rename. A debounced `Writer` (250ms, skip-if-busy) flushes tracker changes during a session and synchronously on clean exit. The existing zero-sessionID branch in `app.go:54-59` becomes a load-from-disk branch; the existing `simple.Connect` + `simple.RequestResume` scaffolding (already exercised end-to-end by `client/cmd/texel-headless/main.go`) is unchanged.

**Tech Stack:** Go 1.24.3, `crypto/sha256` for socket-hash, `encoding/json` for serialization, `os.Rename` for atomic replace, `time.AfterFunc` for debounce.

**Spec:** `docs/superpowers/specs/2026-04-26-issue-199-plan-d-client-persistence-design.md`
**Branch:** `feature/issue-199-plan-d-persistence` (off main, clean)
**Plan order:** Server-side carry-forwards (Phase 1) → persistence module (Phase 2) → app.go integration (Phase 3) → flag wiring (Phase 4) → integration tests (Phase 5) → verification (Phase 6).

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `protocol/protocol.go` | Modify | Add `MaxPayloadLen = 16 * 1024 * 1024` constant + `ErrPayloadTooLarge`; reject in `ReadMessage` before allocation |
| `protocol/protocol_test.go` | Modify | Cap regression test |
| `internal/runtime/server/client_viewport.go` | Modify | `ApplyResume` accepts a `paneExists func([16]byte) bool` predicate; phantom entries dropped before insert + drop-count log |
| `internal/runtime/server/client_viewport_test.go` | Modify | `TestApplyResume_PrunesPhantomPaneIDs` + `TestApplyResume_EvictedSessionWithPaneViewports` |
| `internal/runtime/server/session.go` | Modify | Wrapper `Session.ApplyResume` signature gains `paneExists` parameter to match underlying type |
| `internal/runtime/server/connection_handler.go` | Modify | Prune phantom paneIDs ONCE; pass pruned slice to both `ApplyResume` and `RestorePaneViewport` loop |
| `internal/runtime/client/protocol_handler.go` | Modify | `readLoop` and `handleControlMessage` signatures change `*uint64` → `*atomic.Uint64`; bodies use `Load()`/`Store()` (Task 10 — pre-existing race fix) |
| `internal/runtime/client/persistence.go` | **Create** | `ClientState` type, `Load`/`Save`/`Wipe`, `ResolvePath`, `ValidateClientName` + `ErrInvalidClientName` (so flag-validation can blame name without mis-blaming env), debounced `Writer` (with `WaitGroup`-based `Close`, throttled save-error logging, injectable `saver` for tests) |
| `internal/runtime/client/persistence_test.go` | **Create** | Round-trip, atomic-replace, mismatch wipe, parse-error wipe, debounce coalescing, slow-save skip-if-busy, idempotent `Close`, Windows-reserved-name rejection, multi-client isolation, stale-session wipe-and-replace |
| `internal/runtime/client/app.go` | Modify | Replace zero-sessionID branch (lines 53-66 region) with load-from-disk; convert `lastSequence` to `atomic.Uint64`; install `Writer` + `flushFrame` hook; defer-close-via-closure; handle `ErrSessionNotFound` → wipe + retry. **No eager initial seed** — first viewport tick triggers first save |
| `internal/runtime/client/app.go` | Modify | `Options` gains `ClientName string` |
| `internal/runtime/client/client_state.go` | Modify | `clientState` struct gains `persistSnapshot func()` field |
| `internal/runtime/client/viewport_tracker.go` | Modify | New `snapshotForPersistence` helper + single `state.persistSnapshot()` call at end of `flushFrame` |
| `cmd/texelation/main.go` | Modify | Register `--client-name` flag; `TEXELATION_CLIENT_NAME` env fallback. **Both** `clientrt.Options{}` construction sites must include `ClientName` |
| `client/cmd/texel-client/main.go` | Modify | Register `--client-name` flag; `TEXELATION_CLIENT_NAME` env fallback |
| `internal/runtime/server/resume_viewport_integration_test.go` | Modify | `TestIntegration_PersistedStateDrivesResumeRequest` — disk-roundtripped ClientState assembles into a `ResumeRequest` the server applies correctly. Uses existing `newMemHarness` pattern. |

---

## Phase 1 — Server-Side Carry-Forwards (lands first; unblocks PR if D's client work runs long)

### Task 1: `ReadMessage` 16MB payload cap

**Files:**
- Modify: `protocol/protocol.go:91-95` (error vars), `protocol/protocol.go:136-168` (`ReadMessage` body)
- Test: `protocol/protocol_test.go`

- [ ] **Step 1.1: Write the failing test**

Append to `protocol/protocol_test.go`:

```go
func TestReadMessage_PayloadTooLarge(t *testing.T) {
	// Construct a header that declares a payload of MaxPayloadLen + 1.
	var hdr [40]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 0x54584c01) // magic
	hdr[4] = Version
	hdr[5] = byte(MsgPing)
	hdr[6] = FlagChecksum // MUST be set BEFORE CRC computation; otherwise the CRC
	// is computed over a zero-flag header but the wire byte at offset 6 carries
	// the flag bit, and ReadMessage's verification CRC will mismatch — yielding
	// ErrChecksumMismatch instead of the expected ErrPayloadTooLarge.
	binary.LittleEndian.PutUint32(hdr[32:36], MaxPayloadLen+1)

	// CRC over bytes [4:36] (now includes the flag byte at offset 6).
	crc := crc32.NewIEEE()
	_, _ = crc.Write(hdr[4:36])
	binary.LittleEndian.PutUint32(hdr[36:40], crc.Sum32())

	r := bytes.NewReader(hdr[:])
	_, _, err := ReadMessage(r)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
}
```

- [ ] **Step 1.2: Run test to verify it fails**

```
go test ./protocol/ -run TestReadMessage_PayloadTooLarge -v
```
Expected: FAIL — `ErrPayloadTooLarge` is undefined.

- [ ] **Step 1.3: Add the constant + error**

In `protocol/protocol.go`, append to the existing `var (... )` error block (around line 91-95):

```go
var (
	ErrInvalidMagic     = errors.New("protocol: invalid magic")
	ErrUnsupportedVer   = errors.New("protocol: unsupported version")
	ErrShortPayload     = errors.New("protocol: payload shorter than declared length")
	ErrChecksumMismatch = errors.New("protocol: checksum mismatch")
	ErrPayloadTooLarge  = errors.New("protocol: payload exceeds MaxPayloadLen")
)

// MaxPayloadLen caps a single message's payload size to defend against
// malformed or hostile headers that would otherwise allocate up to 4GB
// (the uint32 limit of Header.PayloadLen). 16MiB comfortably exceeds
// any legitimate texelation message; revisit only if a real protocol
// addition needs more.
const MaxPayloadLen uint32 = 16 * 1024 * 1024
```

- [ ] **Step 1.4: Reject before allocation in `ReadMessage`**

In `protocol/protocol.go`, modify `ReadMessage` between the version check and the `make([]byte, hdr.PayloadLen)` call (line ~158-160). Replace:

```go
	if hdr.Version != Version {
		return hdr, nil, ErrUnsupportedVer
	}

	payload := make([]byte, hdr.PayloadLen)
```

with:

```go
	if hdr.Version != Version {
		return hdr, nil, ErrUnsupportedVer
	}

	if hdr.PayloadLen > MaxPayloadLen {
		return hdr, nil, ErrPayloadTooLarge
	}

	payload := make([]byte, hdr.PayloadLen)
```

- [ ] **Step 1.5: Run test to verify it passes**

```
go test ./protocol/ -run TestReadMessage_PayloadTooLarge -v
```
Expected: PASS.

- [ ] **Step 1.6: Run the full protocol test suite (no regressions)**

```
go test ./protocol/ -v
```
Expected: all PASS.

- [ ] **Step 1.7: Commit**

```bash
git add protocol/protocol.go protocol/protocol_test.go
git commit -m "Cap ReadMessage payload at 16MB (#199, Plan D)

Defends against malformed or hostile headers that would otherwise
allocate up to 4GB. Pre-existing security gap, in scope for Plan D
because cross-client persistence inherits wire exposure when a
recovered file is replayed against the server.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 2: `ClientViewports.ApplyResume` phantom-pane pruning

**Files:**
- Modify: `internal/runtime/server/client_viewport.go:97-120`
- Modify: `internal/runtime/server/connection_handler.go` (call site, find with grep)
- Test: `internal/runtime/server/client_viewport_test.go`

- [ ] **Step 2.1: Write the failing test**

Append to `internal/runtime/server/client_viewport_test.go`:

```go
func TestApplyResume_PrunesPhantomPaneIDs(t *testing.T) {
	cv := NewClientViewports()
	known := [16]byte{1}
	phantom := [16]byte{2}

	exists := func(id [16]byte) bool {
		return id == known
	}

	cv.ApplyResume([]protocol.PaneViewportState{
		{PaneID: known, ViewBottomIdx: 100, ViewportRows: 24, ViewportCols: 80, AutoFollow: true},
		{PaneID: phantom, ViewBottomIdx: 200, ViewportRows: 24, ViewportCols: 80, AutoFollow: false},
	}, exists)

	snap := cv.Snapshot()
	if _, ok := snap[known]; !ok {
		t.Errorf("expected known paneID in snapshot, missing")
	}
	if _, ok := snap[phantom]; ok {
		t.Errorf("expected phantom paneID dropped, present in snapshot")
	}
}
```

- [ ] **Step 2.2: Run test to verify it fails**

```
go test ./internal/runtime/server/ -run TestApplyResume_PrunesPhantomPaneIDs -v
```
Expected: FAIL — `ApplyResume` signature mismatch.

- [ ] **Step 2.3: Update `ClientViewports.ApplyResume` signature**

In `internal/runtime/server/client_viewport.go`, replace the `ApplyResume` function body (line 97 onward).

**IMPORTANT:** the existing function has a ~60-line doc-comment above it covering Policy A, the top/bottom overflow guard, and ViewBottomIdx semantics (these are all load-bearing per Plan B's review). **Preserve that doc-comment verbatim.** Only the function signature and body change. Append the new paragraph below to the existing doc-comment as a final paragraph; do not delete or rewrite anything above the function.

Append this paragraph to the END of the existing doc-comment (immediately above `func (c *ClientViewports) ApplyResume`):

```go
// The paneExists predicate filters out phantom paneIDs — IDs the client
// supplied that no longer exist server-side (closed pane during offline
// time, cross-restart drift, or a recovered persistence file pointing
// at a long-gone pane). Without pruning, the map grows unboundedly with
// stale entries on every cross-restart resume. Pass nil to disable
// pruning (tests). Logs once per call when entries are dropped, to
// surface the drop count for Plan F (session recovery) debugging.
```

Then replace the function itself with:

```go
func (c *ClientViewports) ApplyResume(states []protocol.PaneViewportState, paneExists func(id [16]byte) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dropped := 0
	for _, ps := range states {
		if paneExists != nil && !paneExists(ps.PaneID) {
			dropped++
			continue
		}
		top := ps.ViewBottomIdx - int64(ps.ViewportRows) + 1
		bottom := ps.ViewBottomIdx
		if top > ps.ViewBottomIdx {
			top, bottom = 0, 0
		} else if top < 0 {
			top = 0
		}
		c.byPaneID[ps.PaneID] = ClientViewport{
			AltScreen:     ps.AltScreen,
			ViewTopIdx:    top,
			ViewBottomIdx: bottom,
			Rows:          ps.ViewportRows,
			Cols:          ps.ViewportCols,
			AutoFollow:    ps.AutoFollow,
		}
	}
	if dropped > 0 {
		log.Printf("ClientViewports.ApplyResume: dropped %d phantom paneID entries (cross-restart or closed-pane race)", dropped)
	}
}
```

If `log` is not already imported in this file, add `"log"` to the import block.

- [ ] **Step 2.4: Update `Session.ApplyResume` wrapper signature**

`Session.ApplyResume` at `internal/runtime/server/session.go:76-78` is a thin wrapper around `ClientViewports.ApplyResume`. The connection handler calls the WRAPPER, not the underlying type. The wrapper's signature must match the new shape, otherwise `connection_handler.go` won't compile.

Replace:

```go
func (s *Session) ApplyResume(states []protocol.PaneViewportState) {
	s.viewports.ApplyResume(states)
}
```

with:

```go
func (s *Session) ApplyResume(states []protocol.PaneViewportState, paneExists func(id [16]byte) bool) {
	s.viewports.ApplyResume(states, paneExists)
}
```

- [ ] **Step 2.5: Update the call site in `connection_handler.go` — hoist desktop lookup, prune once, share with `RestorePaneViewport`**

Locate the `MsgResumeRequest` handler:

```
grep -n 'ApplyResume\|RestorePaneViewport' internal/runtime/server/connection_handler.go
```

The current shape (lines ~155-183 of `connection_handler.go`) is approximately:

```go
// Seed ClientViewports from the resume payload FIRST: ...
c.session.ApplyResume(request.PaneViewports)

// Then re-seat each non-alt-screen pane's ViewWindow ...
if sink, ok := c.sink.(*DesktopSink); ok && sink.Desktop() != nil {
    func() {
        defer func() { ... recover ... }()
        for _, ps := range request.PaneViewports {
            if !ps.AltScreen {
                sink.Desktop().RestorePaneViewport(ps.PaneID, ps.ViewBottomIdx, ps.WrapSegmentIdx, ps.AutoFollow)
            }
        }
    }()
}
```

**Important constraint:** the `desktop` (via `sink.Desktop()`) is only available inside the `if sink, ok := c.sink.(*DesktopSink); ok && sink.Desktop() != nil` guard. The phantom-pane predicate needs `AppByID`, which is on `desktop`. So pruning has to happen INSIDE the guard, with a fall-through for when the sink isn't a DesktopSink (in which case we just pass the full list without pruning — there's no harm; the publisher's per-pane lookup will skip phantoms anyway).

Replace with this restructured shape:

```go
// Plan D: hoist the desktop lookup so we can build a paneExists
// predicate once and use it BEFORE ApplyResume runs (otherwise
// ClientViewports accumulates phantom entries on cross-restart resumes).
// When the sink isn't a DesktopSink (test harnesses, fake sinks), we
// fall through with the unpruned slice — ApplyResume will accept it
// and downstream lookups handle missing panes gracefully.
viewportsToApply := request.PaneViewports
sink, sinkOK := c.sink.(*DesktopSink)
if sinkOK && sink.Desktop() != nil {
	desktop := sink.Desktop()
	pruned := make([]protocol.PaneViewportState, 0, len(request.PaneViewports))
	for _, ps := range request.PaneViewports {
		if desktop.AppByID(ps.PaneID) != nil {
			pruned = append(pruned, ps)
		}
	}
	if dropped := len(request.PaneViewports) - len(pruned); dropped > 0 {
		debugLog.Printf("connection %x: pruned %d phantom paneID entries from resume payload", c.session.ID(), dropped)
	}
	viewportsToApply = pruned
}

// Seed ClientViewports from the (possibly-pruned) resume payload FIRST: ...
// (Existing comment block about ordering / Policy A stays.)
// nil predicate — pruning already happened above; ApplyResume's own
// drop counter stays at zero so we don't double-log.
c.session.ApplyResume(viewportsToApply, nil)

// Then re-seat each non-alt-screen pane's ViewWindow ... (existing code,
// updated to iterate viewportsToApply instead of request.PaneViewports).
if sinkOK && sink.Desktop() != nil {
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("server: RestorePaneViewport panic: %v", r)
			}
		}()
		for _, ps := range viewportsToApply {
			if !ps.AltScreen {
				sink.Desktop().RestorePaneViewport(ps.PaneID, ps.ViewBottomIdx, ps.WrapSegmentIdx, ps.AutoFollow)
			}
		}
	}()
}
```

Key points:
- `sink, sinkOK := c.sink.(*DesktopSink)` is hoisted ONCE so the same predicate guard covers both the prune block and the subsequent RestorePaneViewport block.
- Drop-count log uses `debugLog.Printf` (per existing project convention at line 137 of the same file) rather than `log.Printf`, since legitimate cross-restart drops shouldn't appear at info-level on every reconnect.
- When sink is not a DesktopSink (test fakes), pruning is skipped — `viewportsToApply == request.PaneViewports` — and ApplyResume gets the unpruned set. This matches existing behavior; tests can opt into pruning by passing a real predicate to `ClientViewports.ApplyResume` directly.

**Also in this step: collapse the third sink re-assertion at the snapshot-publish block.** The existing `MsgResumeRequest` case has a THIRD `if sink, ok := c.sink.(*DesktopSink); ok { ... }` block at around line 198 (after the `SnapshotProvider` block) that runs `pub.ResetDiffState()` + `sink.Publish()`. After the hoist, that inner re-assertion would shadow the outer `sink`. Replace it with the hoisted variable:

```go
// BEFORE (line ~198):
if sink, ok := c.sink.(*DesktopSink); ok {
    if pub := sink.Publisher(); pub != nil {
        pub.ResetDiffState()
    }
    sink.Publish()
}

// AFTER:
if sinkOK {
    if pub := sink.Publisher(); pub != nil {
        pub.ResetDiffState()
    }
    sink.Publish()
}
```

The `Reset+Publish` ordering is documented as load-bearing in the existing comment block (lines 199-206); preserve that comment verbatim.

- [ ] **Step 2.6: Update any other non-test call sites**

```
grep -rn 'ApplyResume(' internal/runtime/server/ | grep -v _test.go
```

For each remaining non-test call site, add a second argument: `nil` if pruning is not needed at that site (because it's already a known-good slice), or an appropriate predicate otherwise.

- [ ] **Step 2.7: Run test to verify it passes**

```
go test ./internal/runtime/server/ -run TestApplyResume_PrunesPhantomPaneIDs -v
```
Expected: PASS.

- [ ] **Step 2.8: Update existing ApplyResume tests for the new signature**

```
grep -n 'ApplyResume(' internal/runtime/server/*_test.go
```

For each test that calls `ApplyResume`, add a second argument. Tests that don't care about pruning pass `nil` (which means "accept all"):

```go
cv.ApplyResume(states, nil)
```

Tests that explicitly want a known-pane predicate use the same shape as Step 2.3.

- [ ] **Step 2.9: Run the server test suite (no regressions)**

```
go test ./internal/runtime/server/ -v
```
Expected: all PASS.

- [ ] **Step 2.10: Commit**

```bash
git add internal/runtime/server/client_viewport.go internal/runtime/server/client_viewport_test.go internal/runtime/server/connection_handler.go internal/runtime/server/session.go internal/runtime/server/*_test.go
git commit -m "ClientViewports: prune phantom pane IDs at ApplyResume (#199, Plan D)

Plan B left the map growing unboundedly with stale paneIDs from
clients that resumed against a different pane tree. Plan D adds a
paneExists predicate; the connection handler passes
desktop.AppByID(id) != nil and prunes once at handler entry so the
RestorePaneViewport loop also benefits. Logs the drop count for
Plan F (session recovery) debugging.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 3: `TestApplyResume_EvictedSessionWithPaneViewports`

**Files:**
- Test: `internal/runtime/server/client_integration_test.go` (or `connection_handler_test.go` — pick the file that already exercises `MsgResumeRequest` + handshake; usually `client_integration_test.go`)

- [ ] **Step 3.1: Locate the existing eviction-test pattern**

```
grep -n 'ErrSessionNotFound' internal/runtime/server/*_test.go
```

Identify the test file that already constructs a session-not-found scenario; the new test mirrors its harness.

- [ ] **Step 3.2: Write the failing test**

Append to that file:

```go
// TestApplyResume_EvictedSessionWithPaneViewports verifies that a
// MsgResumeRequest carrying non-empty PaneViewports against an evicted
// session falls through cleanly with ErrSessionNotFound — no panic, no
// half-applied state, connection cleanly closed.
//
// Plan B's review surfaced this as a coverage gap: the eviction path
// and the PaneViewports path were tested separately but never together.
func TestApplyResume_EvictedSessionWithPaneViewports(t *testing.T) {
	srv, cleanup := newTestServer(t) // use existing harness in this file
	defer cleanup()

	// Simulate an evicted/expired sessionID.
	staleSession := [16]byte{0xde, 0xad, 0xbe, 0xef}

	// Client sends MsgResumeRequest with non-empty PaneViewports.
	req := protocol.ResumeRequest{
		SessionID:    staleSession,
		LastSequence: 42,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID: [16]byte{1}, ViewBottomIdx: 10,
				ViewportRows: 24, ViewportCols: 80,
				AutoFollow: false,
			},
		},
	}

	// Send and verify the server cleanly rejects without panic.
	hdr, _, err := srv.simulateResumeRequest(t, req) // helper from harness
	if err == nil {
		t.Fatalf("expected error on stale session, got nil")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if hdr.Type != protocol.MsgError {
		t.Errorf("expected MsgError header, got %v", hdr.Type)
	}
}
```

(The `simulateResumeRequest` helper may not exist with exactly that name — match the existing harness API. If no helper exists, write the wire-level send inline using the same pattern as the closest test in the file.)

- [ ] **Step 3.3: Run test to verify it fails OR passes immediately**

```
go test ./internal/runtime/server/ -run TestApplyResume_EvictedSessionWithPaneViewports -v
```

Expected: most likely PASS immediately (the server code path already handles this; the test documents the contract). If it FAILS with a panic or wrong error type, that's a real regression and must be fixed before commit.

- [ ] **Step 3.4: If it failed: fix the underlying code**

Trace the failure to its source in `connection_handler.go` and `session.go`. The fix should keep the pattern Plan B established — validate sessionID unconditionally before any state mutation. Add the fix to this same commit.

- [ ] **Step 3.5: Run the full server test suite**

```
go test ./internal/runtime/server/ -v
```
Expected: all PASS.

- [ ] **Step 3.6: Commit**

```bash
git add internal/runtime/server/client_integration_test.go
git commit -m "Test eviction + non-empty PaneViewports payload (#199, Plan D)

Plan B's review noted the eviction path and the PaneViewports payload
path were tested separately but never together. This regression test
locks the contract: MsgResumeRequest with non-empty PaneViewports
against an evicted session returns ErrSessionNotFound cleanly, with
no panic or half-applied state.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Phase 2 — Persistence Module (client-side)

### Task 4: `ClientState` type with JSON encoding (round-trip test first)

**Files:**
- Create: `internal/runtime/client/persistence.go`
- Create: `internal/runtime/client/persistence_test.go`

- [ ] **Step 4.1: Write the failing round-trip test**

Create `internal/runtime/client/persistence_test.go`:

```go
package clientruntime

import (
	"bytes"
	"encoding/json"
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
```

- [ ] **Step 4.2: Run test to verify it fails**

```
go test ./internal/runtime/client/ -run TestClientState_RoundTrip -v
```
Expected: FAIL — `ClientState` is undefined.

- [ ] **Step 4.3: Create `persistence.go` with the type and JSON marshalers**

Create `internal/runtime/client/persistence.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/persistence.go
// Summary: Client-side session and viewport persistence (issue #199 Plan D).
// Usage: Load on startup before simple.Connect; Save on debounced viewport
//   changes; Wipe on stale-session rejection. Runs in $XDG_STATE_HOME.

package clientruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// DefaultClientName is the slot used when --client-name and
// $TEXELATION_CLIENT_NAME are both unset. Single-client deployments
// touch nothing.
const DefaultClientName = "default"

// ClientNameEnvVar is the env var fallback for --client-name.
const ClientNameEnvVar = "TEXELATION_CLIENT_NAME"

// ClientState is the on-disk schema. Field semantics mirror
// protocol.PaneViewportState; JSON encoding uses lowercase hex for
// [16]byte values (jq-friendly, unlike base64).
type ClientState struct {
	SocketPath    string                        `json:"socketPath"`
	SessionID     [16]byte                      `json:"-"`
	LastSequence  uint64                        `json:"lastSequence"`
	WrittenAt     time.Time                     `json:"writtenAt"`
	PaneViewports []protocol.PaneViewportState  `json:"-"`

	// Hex shadow fields — populated for marshaling, consumed during
	// unmarshaling. See MarshalJSON / UnmarshalJSON.
}

// jsonShape is the literal on-disk schema. ClientState wraps it so
// callers see [16]byte fields and the JSON encoding is hex strings.
type jsonShape struct {
	SocketPath    string                 `json:"socketPath"`
	SessionID     string                 `json:"sessionID"`
	LastSequence  uint64                 `json:"lastSequence"`
	WrittenAt     time.Time              `json:"writtenAt"`
	PaneViewports []jsonPaneViewportState `json:"paneViewports"`
}

type jsonPaneViewportState struct {
	PaneID         string `json:"paneID"`
	AltScreen      bool   `json:"altScreen"`
	AutoFollow     bool   `json:"autoFollow"`
	ViewBottomIdx  int64  `json:"viewBottomIdx"`
	WrapSegmentIdx uint16 `json:"wrapSegmentIdx"`
	Rows           uint16 `json:"rows"`
	Cols           uint16 `json:"cols"`
}

func (s ClientState) MarshalJSON() ([]byte, error) {
	out := jsonShape{
		SocketPath:   s.SocketPath,
		SessionID:    hex.EncodeToString(s.SessionID[:]),
		LastSequence: s.LastSequence,
		WrittenAt:    s.WrittenAt,
	}
	out.PaneViewports = make([]jsonPaneViewportState, len(s.PaneViewports))
	for i, p := range s.PaneViewports {
		out.PaneViewports[i] = jsonPaneViewportState{
			PaneID:         hex.EncodeToString(p.PaneID[:]),
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			Rows:           p.ViewportRows,
			Cols:           p.ViewportCols,
		}
	}
	return json.Marshal(&out)
}

func (s *ClientState) UnmarshalJSON(data []byte) error {
	var in jsonShape
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	s.SocketPath = in.SocketPath
	if err := decodeHex16(in.SessionID, &s.SessionID); err != nil {
		return fmt.Errorf("sessionID: %w", err)
	}
	s.LastSequence = in.LastSequence
	s.WrittenAt = in.WrittenAt
	s.PaneViewports = make([]protocol.PaneViewportState, len(in.PaneViewports))
	for i, p := range in.PaneViewports {
		var pid [16]byte
		if err := decodeHex16(p.PaneID, &pid); err != nil {
			return fmt.Errorf("paneViewports[%d].paneID: %w", i, err)
		}
		s.PaneViewports[i] = protocol.PaneViewportState{
			PaneID:         pid,
			AltScreen:      p.AltScreen,
			AutoFollow:     p.AutoFollow,
			ViewBottomIdx:  p.ViewBottomIdx,
			WrapSegmentIdx: p.WrapSegmentIdx,
			ViewportRows:   p.Rows,
			ViewportCols:   p.Cols,
		}
	}
	return nil
}

func decodeHex16(s string, out *[16]byte) error {
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

- [ ] **Step 4.4: Run test to verify it passes**

```
go test ./internal/runtime/client/ -run TestClientState_RoundTrip -v
```
Expected: PASS.

- [ ] **Step 4.5: Commit**

```bash
git add internal/runtime/client/persistence.go internal/runtime/client/persistence_test.go
git commit -m "Add ClientState type with hex-encoded JSON (#199, Plan D)

Schema mirrors protocol.PaneViewportState; encodes [16]byte values
as lowercase hex (jq/grep-friendly) rather than base64.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 5: Path resolution (`ResolvePath`)

**Files:**
- Modify: `internal/runtime/client/persistence.go`
- Modify: `internal/runtime/client/persistence_test.go`

- [ ] **Step 5.1: Write the failing test**

Append to `persistence_test.go`:

```go
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
		".hidden",                            // leading dot
		"con", "CON", "Con.json",             // Windows reserved + case + extension
		"nul", "aux", "prn",
		"com1", "COM9", "lpt5",
		"name with spaces", "with$dollar", "with;semi",
	}
	for _, name := range cases {
		if _, err := ResolvePath("/run/x.sock", name); err == nil {
			t.Errorf("ResolvePath(%q) should have errored, got nil", name)
		}
	}
}
```

- [ ] **Step 5.2: Run test to verify it fails**

```
go test ./internal/runtime/client/ -run TestResolvePath -v
```
Expected: FAIL — `ResolvePath` is undefined.

- [ ] **Step 5.3: Implement `ResolvePath`**

Append to `persistence.go`:

```go
// ResolvePath returns the on-disk state file path for the given socket
// and client name. Precedence: explicit clientName arg → env
// $TEXELATION_CLIENT_NAME → DefaultClientName.
func ResolvePath(socketPath, clientName string) (string, error) {
	if socketPath == "" {
		return "", errors.New("persistence: empty socketPath")
	}
	abs, err := filepath.Abs(socketPath)
	if err != nil {
		return "", fmt.Errorf("persistence: abs socket path: %w", err)
	}
	name := strings.TrimSpace(clientName)
	if name == "" {
		name = strings.TrimSpace(os.Getenv(ClientNameEnvVar))
	}
	if name == "" {
		name = DefaultClientName
	}
	if !validClientName(name) {
		return "", fmt.Errorf("persistence: invalid clientName %q", name)
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("persistence: home dir: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "texelation", "client", socketHash(abs), name+".json"), nil
}

func socketHash(absSocketPath string) string {
	h := sha256.Sum256([]byte(absSocketPath))
	return hex.EncodeToString(h[:8]) // 16 hex chars
}

// ErrInvalidClientName is returned by ValidateClientName for any name
// that fails the client-name rules. Callers can distinguish "user-input
// is bad" from "environment is bad" (e.g., $HOME unreadable in
// ResolvePath) and report the right thing.
var ErrInvalidClientName = errors.New("persistence: invalid client name")

// ValidateClientName runs the name rules in isolation (no path resolution,
// no $HOME lookup, no socket hashing). Returns ErrInvalidClientName on
// failure so callers in cmd/texelation and cmd/texel-client can wrap it
// with a "invalid --client-name %q" message without misattributing
// HOME-dir or socket errors to the user's flag value.
func ValidateClientName(name string) error {
	if !validClientName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidClientName, name)
	}
	return nil
}

// validClientName rejects path-traversal, shell-meta characters, hidden
// files (leading-dot), and Windows reserved device names. The Makefile
// cross-compiles for Windows, so a client-name like "con" or "nul" must
// not be accepted — opening such a path blocks on win32.
func validClientName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if name[0] == '.' { // hidden / dotfiles
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	// Windows reserved device names (case-insensitive). The reserved list
	// also blocks names with a reserved stem followed by an extension
	// (e.g. "con.json"), so check the stem before the first dot.
	stem := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		stem = name[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}
```

- [ ] **Step 5.4: Run tests to verify they pass**

```
go test ./internal/runtime/client/ -run TestResolvePath -v
```
Expected: all PASS.

- [ ] **Step 5.5: Commit**

```bash
git add internal/runtime/client/persistence.go internal/runtime/client/persistence_test.go
git commit -m "Persistence: ResolvePath with --client-name + XDG_STATE_HOME (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 6: `Save` (atomic temp+rename)

- [ ] **Step 6.1: Write the failing test**

Append to `persistence_test.go`:

```go
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
```

- [ ] **Step 6.2: Run test to verify it fails**

```
go test ./internal/runtime/client/ -run TestSave -v
```
Expected: FAIL — `Save` is undefined.

- [ ] **Step 6.3: Implement `Save`**

Append to `persistence.go`:

```go
// Save writes state to filePath atomically: write to a sibling .tmp
// file, then os.Rename. Crash mid-write leaves either the old file
// or the new file, never partial.
//
// MkdirAll on the parent dir is idempotent.
func Save(filePath string, state *ClientState) error {
	if state == nil {
		return errors.New("persistence: nil state")
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("persistence: mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state.tmp-*")
	if err != nil {
		return fmt.Errorf("persistence: tempfile: %w", err)
	}
	tmpPath := tmp.Name()

	// Best-effort cleanup if anything below fails. Success path: rename
	// already consumed tmpPath, so Remove returns ErrNotExist (expected).
	// Failure path: tmpPath should still exist; log if Remove fails for
	// any reason other than ErrNotExist (would indicate filesystem trouble
	// and could otherwise accumulate orphan tmp files silently).
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("persistence: temp file cleanup failed: %v", err)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(state); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("persistence: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("persistence: close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("persistence: rename: %w", err)
	}
	return nil
}
```

- [ ] **Step 6.4: Run tests to verify they pass**

```
go test ./internal/runtime/client/ -run TestSave -v
```
Expected: all PASS.

- [ ] **Step 6.5: Commit**

```bash
git add internal/runtime/client/persistence.go internal/runtime/client/persistence_test.go
git commit -m "Persistence: atomic Save via temp+rename (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 7: `Load` with mismatch + parse-error wipe semantics

- [ ] **Step 7.1: Write the failing tests**

Append to `persistence_test.go`:

```go
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
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
```

- [ ] **Step 7.2: Run tests to verify they fail**

```
go test ./internal/runtime/client/ -run TestLoad -v
```
Expected: FAIL — `Load` is undefined.

- [ ] **Step 7.3: Implement `Load` and `Wipe`**

Append to `persistence.go`:

```go
// Load reads ClientState from filePath. Returns:
//   - (nil, nil) if file is missing — caller treats as fresh client.
//   - (nil, nil) after wiping the file if parse fails or socketPath
//     doesn't match — corrupt or stale-from-different-daemon, no
//     auto-migration (project has no back-compat constraint).
//   - (state, nil) on a valid load.
//   - (nil, err) only on disk-level errors that prevent recovery
//     (e.g., permission denied on Stat).
//
// Wipe failures inside the recovery branches are logged but not
// propagated — Load's caller has no useful action to take, but a wipe
// failure indicates filesystem trouble that the user deserves a
// diagnostic for. (Without the log, Load → wipe-fails → next start
// hits the same parse error → wipe-fails ad infinitum, silently.)
func Load(filePath, expectedSocketPath string) (*ClientState, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("persistence: read: %w", err)
	}

	var s ClientState
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupt; wipe and treat as fresh. Warn-level: suggests the
		// file was corrupted (process kill mid-write on a non-atomic
		// filesystem, or hand-edited).
		if werr := Wipe(filePath); werr != nil {
			log.Printf("persistence: parse failed (%v); wipe also failed (%v); will retry on next start", err, werr)
		} else {
			log.Printf("persistence: parse failed (%v); state file wiped, starting fresh", err)
		}
		return nil, nil
	}

	if s.SocketPath != expectedSocketPath {
		// Stale from a different daemon; wipe and treat as fresh.
		// Info-level: this is expected when a user's socket path
		// changes (e.g., dev rebuild with a different XDG_RUNTIME_DIR).
		if werr := Wipe(filePath); werr != nil {
			log.Printf("persistence: socketPath mismatch (file=%q expected=%q); wipe failed (%v)", s.SocketPath, expectedSocketPath, werr)
		} else {
			log.Printf("persistence: socketPath mismatch (file=%q expected=%q); state file wiped", s.SocketPath, expectedSocketPath)
		}
		return nil, nil
	}
	return &s, nil
}

// Wipe removes the state file. Idempotent.
func Wipe(filePath string) error {
	if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("persistence: wipe: %w", err)
	}
	return nil
}
```

- [ ] **Step 7.4: Run tests to verify they pass**

```
go test ./internal/runtime/client/ -run "TestLoad|TestWipe" -v
```
Expected: all PASS.

- [ ] **Step 7.5: Commit**

```bash
git add internal/runtime/client/persistence.go internal/runtime/client/persistence_test.go
git commit -m "Persistence: Load + Wipe with mismatch/parse-error fallthrough (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 8: Debounced `Writer` (skip-if-busy + flush)

- [ ] **Step 8.1: Write the failing tests**

Append to `persistence_test.go`:

```go
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

	// Wait for debounce + a margin.
	time.Sleep(200 * time.Millisecond)

	got, err := Load(path, "/tmp/x.sock")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatalf("expected file to exist")
	}
	// The latest update wins (LastSequence=49).
	if got.LastSequence != 49 {
		t.Errorf("expected coalesced last write LastSequence=49, got %d", got.LastSequence)
	}
}

func TestWriter_FlushSyncsLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	w := NewWriter(path, 1*time.Hour) // long debounce; only Flush should write
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

	// saveCount counts actual on-disk writes; slowSaveStarted/Done track
	// the in-flight save so the test can synchronize on it.
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

	// Trigger the first tick.
	w.Update(ClientState{SocketPath: "/tmp/x.sock", SessionID: [16]byte{1}, LastSequence: 1, WrittenAt: time.Now()})

	// Wait for the first save to actually start.
	<-slowSaveStarted

	// Bombard with updates while the save is blocked. None of these
	// should produce additional saves; instead they should coalesce
	// into a single follow-up save once the in-flight one completes.
	for i := 2; i <= 50; i++ {
		w.Update(ClientState{
			SocketPath:   "/tmp/x.sock",
			SessionID:    [16]byte{byte(i)},
			LastSequence: uint64(i),
			WrittenAt:    time.Now(),
		})
	}

	// Release the slow save; expect at most one follow-up.
	close(slowSaveCanFinish)

	// Allow the follow-up tick to fire and complete.
	w.Close()

	got := saveCount.Load()
	if got > 2 {
		t.Errorf("expected at most 2 saves (initial + coalesced follow-up), got %d", got)
	}
	if got < 2 {
		t.Errorf("expected the follow-up save to run after slow save released, got only %d", got)
	}

	// And the persisted state should reflect the LATEST update (49).
	state, err := Load(path, "/tmp/x.sock")
	if err != nil || state == nil {
		t.Fatalf("Load: state=%v err=%v", state, err)
	}
	if state.LastSequence != 50 {
		t.Errorf("expected coalesced LastSequence=50, got %d", state.LastSequence)
	}
}
```

- [ ] **Step 8.2: Run tests to verify they fail**

```
go test ./internal/runtime/client/ -run TestWriter -v
```
Expected: FAIL — `Writer`/`NewWriter` undefined.

- [ ] **Step 8.3: Implement `Writer`**

Append to `persistence.go`:

```go
// SaveFunc is the signature Writer uses to persist state. Defaults to
// the package-level Save; tests in the same package may inject their
// own (e.g. a slow / failing saver to exercise skip-if-busy).
type SaveFunc func(filePath string, state *ClientState) error

// Writer debounces saves of ClientState to a file. Save calls fire at
// most once per debounce window; rapid updates coalesce.
//
// Concurrency model:
//   - mu protects state/timer/closed/lastSaveErr (the lifecycle).
//   - saveMu serializes the actual disk write so tick() and Flush()
//     can never both call Save concurrently.
//   - wg tracks in-flight tick/flush goroutines so Close blocks until
//     they finish — without this, a tick scheduled by AfterFunc could
//     fire after Run returns and write to a dir that t.TempDir has
//     already cleaned up.
//
// Save errors are logged with simple per-error-string deduplication
// (one log per distinct error across consecutive failures, plus one
// recovery line when saves resume). Crash-loss is bounded by the
// debounce window — Plan D's design constraint: ≤250ms of viewport
// movement on hard kill, perceptually invisible to the user.
type Writer struct {
	filePath string
	debounce time.Duration

	mu             sync.Mutex
	state          *ClientState // latest pending state, nil when consumed
	timer          *time.Timer  // pending debounce timer, nil when none
	closed         bool
	lastSaveErr    string    // for throttled error logging — last error string seen
	lastSaveErrAt  time.Time // for throttled error logging — when we last logged it

	saveMu sync.Mutex // serializes actual Save() calls
	wg     sync.WaitGroup

	// saver is the function actually invoked to persist. Test code in
	// the same package may overwrite this on a fresh Writer to inject
	// slow/failing saves.
	saver SaveFunc
}

func NewWriter(filePath string, debounce time.Duration) *Writer {
	return &Writer{
		filePath: filePath,
		debounce: debounce,
		saver:    Save,
	}
}

// Update marks state and schedules a debounced Save. Cheap;
// non-blocking. Safe to call from any goroutine.
func (w *Writer) Update(s ClientState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	cp := s
	w.state = &cp
	if w.timer == nil {
		w.timer = time.AfterFunc(w.debounce, w.tick)
	}
	// If a timer is already pending or a tick is in-flight, the latest
	// state will be picked up when it next reads w.state under mu.
}

// Flush saves the latest pending state synchronously, dropping any
// pending debounce. Safe to call concurrent with Update. Idempotent.
func (w *Writer) Flush() {
	w.wg.Add(1)
	defer w.wg.Done()

	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	s := w.state
	w.state = nil
	w.mu.Unlock()

	if s != nil {
		w.doSave(*s)
	}
}

// Close flushes synchronously, stops further work, and waits for any
// in-flight tick/flush goroutines to finish. Safe to call multiple
// times.
func (w *Writer) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()

	w.Flush()
	w.wg.Wait()
}

// tick runs in time.AfterFunc's goroutine.
func (w *Writer) tick() {
	w.wg.Add(1)
	defer w.wg.Done()

	w.mu.Lock()
	if w.closed || w.state == nil {
		w.timer = nil
		w.mu.Unlock()
		return
	}
	s := *w.state
	w.state = nil
	w.timer = nil
	w.mu.Unlock()

	w.doSave(s)

	// If Update arrived during the save, schedule a follow-up tick.
	w.mu.Lock()
	if w.state != nil && !w.closed && w.timer == nil {
		w.timer = time.AfterFunc(w.debounce, w.tick)
	}
	w.mu.Unlock()
}

// doSave is the only call site for the actual saver. saveMu serializes
// it so tick and Flush never race each other on disk. Save failures
// are logged with throttle-by-error-string dedup so a stuck condition
// doesn't spam the log; a permanently failing condition (read-only
// disk, etc.) re-logs every saveErrRelogInterval to surface it.
const saveErrRelogInterval = 5 * time.Minute

func (w *Writer) doSave(s ClientState) {
	w.saveMu.Lock()
	defer w.saveMu.Unlock()

	err := w.saver(w.filePath, &s)

	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		es := err.Error()
		now := time.Now()
		// Log on first occurrence, on transition to a different error
		// string, and periodically (every saveErrRelogInterval) so a
		// stuck condition doesn't go silent forever.
		if es != w.lastSaveErr || now.Sub(w.lastSaveErrAt) >= saveErrRelogInterval {
			log.Printf("persistence: save failed (%v); will retry on next change", err)
			w.lastSaveErr = es
			w.lastSaveErrAt = now
		}
		return
	}
	if w.lastSaveErr != "" {
		log.Printf("persistence: save recovered after prior failure")
		w.lastSaveErr = ""
		w.lastSaveErrAt = time.Time{}
	}
}
```

- [ ] **Step 8.4: Run tests to verify they pass**

```
go test ./internal/runtime/client/ -run TestWriter -v
```
Expected: all PASS.

- [ ] **Step 8.5: Run race detector**

```
go test -race ./internal/runtime/client/ -run TestWriter -v
```
Expected: no race reports.

- [ ] **Step 8.6: Commit**

```bash
git add internal/runtime/client/persistence.go internal/runtime/client/persistence_test.go
git commit -m "Persistence: debounced Writer with skip-if-busy + Flush (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Phase 3 — `app.go` Integration

### Task 9: Add `ClientName` to `Options`

**Files:**
- Modify: `internal/runtime/client/app.go:34-40` (Options struct)

- [ ] **Step 9.1: Update Options struct**

In `internal/runtime/client/app.go`, replace the `Options` struct (around line 34-40):

```go
// Options configures the remote client runtime.
type Options struct {
	Socket                  string
	Reconnect               bool
	PanicLog                string
	ShowRestartNotification bool   // Show notification that server was restarted
	ClientName              string // --client-name slot for multi-client persistence (issue #199 Plan D)
}
```

- [ ] **Step 9.2: Run build to verify**

```
go build ./...
```
Expected: clean build.

- [ ] **Step 9.3: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Options.ClientName for multi-client persistence (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 10: Convert `lastSequence` to `atomic.Uint64` (pre-existing race fix)

**Files:**
- Modify: `internal/runtime/client/app.go:101` (declaration)
- Modify: `internal/runtime/client/protocol_handler.go:22, 41` (function signatures + body)
- Modify: `internal/runtime/client/app.go:120, 123, 131` (call sites; `lastSequence` may be passed/dereferenced)

**Why this task exists separately:** Plan D's `persistSnapshot` (added in Task 13) reads `lastSequence` from the render thread, while `readLoop`/`handleControlMessage` mutate it from the protocol-handler goroutine. Today the variable is a plain `uint64` mutated through a `*uint64` parameter — a latent data race that exists even before Plan D, but Plan D adds the second reader that makes `-race` find it. `pendingAck` and `lastAck` in the same file are already `atomic.Uint64`, so this conversion is a small, mechanical fix that brings `lastSequence` in line.

- [ ] **Step 10.1: Change the declaration in `app.go`**

In `internal/runtime/client/app.go`, find:

```go
lastSequence := uint64(0)
```

(currently around line 101). Replace with:

```go
var lastSequence atomic.Uint64
```

If `loadedState` later sets a starting value, use `lastSequence.Store(loadedState.LastSequence)` (this happens in Task 12, but capture the change here so the variable is always atomic).

- [ ] **Step 10.2: Change `readLoop` signature and body in `protocol_handler.go`**

Update the function signature at line 22:

```go
func readLoop(conn net.Conn, state *clientState, sessionID [16]byte, lastSequence *atomic.Uint64, renderCh chan<- struct{}, doneCh chan<- struct{}, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) {
```

Inside the function body, replace all uses of `*lastSequence` (read) with `lastSequence.Load()`, and `*lastSequence = X` (write) with `lastSequence.Store(X)`.

- [ ] **Step 10.3: Change `handleControlMessage` signature and body in `protocol_handler.go`**

Update line 41:

```go
func handleControlMessage(state *clientState, conn net.Conn, hdr protocol.Header, payload []byte, sessionID [16]byte, lastSequence *atomic.Uint64, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) bool {
```

Same body changes: `*lastSequence` → `lastSequence.Load()`; assignments → `lastSequence.Store(...)`.

In particular, **the nil-guarded read-modify-write at lines 86-88** must be rewritten:

```go
// BEFORE:
if lastSequence != nil && hdr.Sequence > *lastSequence {
    *lastSequence = hdr.Sequence
}
```

becomes:

```go
// AFTER (atomic-safe — note this is technically a check-then-set
// race-prone pattern, but Plan D's only writer is this loop, so
// any race against persistSnapshot's Load() is a benign read of
// "either the old or new value", both of which are valid sequences
// to persist. If a future change adds a SECOND writer, switch to
// CompareAndSwap):
if lastSequence != nil {
    cur := lastSequence.Load()
    if hdr.Sequence > cur {
        lastSequence.Store(hdr.Sequence)
    }
}
```

Search the file for any OTHER `*lastSequence` reads or writes and convert each. The only known sites are the body of `readLoop` and `handleControlMessage`.

- [ ] **Step 10.4: Run build to find every remaining call site**

```
go build ./...
```

The compiler will flag any remaining `*uint64` mismatch. Fix each by passing `&lastSequence` (which is now `*atomic.Uint64` automatically) and converting `*lastSequence` to `.Load()` / `.Store()` at the call site.

Likely sites:
- `app.go:120` — the existing `simple.RequestResume(conn, sessionID, lastSequence, viewports)` passes the value, not a pointer. Update to `simple.RequestResume(conn, sessionID, lastSequence.Load(), viewports)`.
- `app.go:123` — passes `&lastSequence` to `handleControlMessage`. No source change needed; the type matches automatically.
- `app.go:131` — passes `&lastSequence` to `readLoop`. Same.

- [ ] **Step 10.5: Run client tests**

```
go test ./internal/runtime/client/ -v
```
Expected: all PASS.

- [ ] **Step 10.6: Run race detector across the client package**

```
go test -race ./internal/runtime/client/...
```
Expected: no race reports. (This is the test that would have caught the latent bug; lock it in here.)

- [ ] **Step 10.7: Commit**

```bash
git add internal/runtime/client/app.go internal/runtime/client/protocol_handler.go
git commit -m "Convert lastSequence to atomic.Uint64 (#199, Plan D)

Latent data race: lastSequence was a plain uint64 mutated from
readLoop's goroutine via a *uint64 parameter, but other code paths
read the same variable. pendingAck/lastAck in the same file already
use atomic.Uint64 — this brings lastSequence in line.

Plan D adds a second reader (persistSnapshot from flushFrame) that
would make -race find the existing bug. Fix preemptively so the
Plan D commits land cleanly.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 11: Replace zero-sessionID branch with load-from-disk + stale-session retry around `Connect`

**Files:**
- Modify: `internal/runtime/client/app.go:53-66` (the existing init block — note: the OLD block spans more than just the dead `if !opts.Reconnect` lines; preserve `var writeMu sync.Mutex`, `debuglog.Printf`, etc.)

**IMPORTANT design note — why the retry goes here, not around `RequestResume`:**

The server's handshake at `internal/runtime/server/handshake.go:69-74` calls `mgr.Lookup(connectReq.SessionID)` for non-zero sessionIDs. If the session has been evicted (or the daemon was restarted without Plan D2 persistence), `Lookup` returns `ErrSessionNotFound`, the handshake function returns the error, the connection is closed by the server, and `simple.Connect`'s subsequent `protocol.ReadMessage` fails. **`simple.RequestResume` is never reached** — the failure surfaces at `Connect`. The retry must live here, around `Connect`, not around `RequestResume`.

**IMPORTANT — what to preserve:** the existing block from line 53 to ~66 includes:

```go
simple := client.NewSimpleClient(opts.Socket)
var sessionID [16]byte
if !opts.Reconnect {
    sessionID = [16]byte{}
}

accept, conn, err := simple.Connect(&sessionID)
if err != nil {
    return fmt.Errorf("connect failed: %w", err)
}
defer conn.Close()                // ← will be replaced with closure form below
var writeMu sync.Mutex            // ← MUST be preserved

debuglog.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))
```

The dead `var sessionID; if !opts.Reconnect` lines get replaced. `defer conn.Close()` gets converted to closure form (so the retry's reassignment of `conn` is also closed at exit). `var writeMu` and `debuglog.Printf` stay. The `if err != nil { return ... }` block also gets replaced (replaced with the retry logic below).

- [ ] **Step 11.1: Wire `ResolvePath` + `Load` + stale-session retry at startup**

In `internal/runtime/client/app.go`, replace this block (currently lines ~53-66):

```go
	simple := client.NewSimpleClient(opts.Socket)
	var sessionID [16]byte
	if !opts.Reconnect {
		sessionID = [16]byte{}
	}

	accept, conn, err := simple.Connect(&sessionID)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close()
	var writeMu sync.Mutex

	debuglog.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))
```

with:

```go
	simple := client.NewSimpleClient(opts.Socket)

	// Plan D: load persisted client state if any. Failures (missing,
	// parse error, mismatch) all yield (nil, nil) and we proceed as
	// fresh.
	statePath, statePathErr := ResolvePath(opts.Socket, opts.ClientName)
	if statePathErr != nil {
		log.Printf("persistence: path resolution failed (%v); running without persistence", statePathErr)
	}
	var loadedState *ClientState
	if statePath != "" {
		ls, err := Load(statePath, opts.Socket)
		if err != nil {
			log.Printf("persistence: load failed (%v); running fresh", err)
		} else {
			loadedState = ls
		}
	}

	var sessionID [16]byte
	if loadedState != nil {
		sessionID = loadedState.SessionID
	}

	accept, conn, err := simple.Connect(&sessionID)
	if err != nil && loadedState != nil {
		// We sent a non-zero sessionID from disk and Connect failed.
		// The dominant cause is a stale sessionID: the server has
		// evicted the session (or the daemon was restarted without
		// Plan D2 persistence). Wipe the stale state and retry fresh
		// with a zero sessionID.
		//
		// We don't try to disambiguate stale-session from transient
		// network failure — retrying once with zero ID is cheap and
		// the second failure (if there is one) surfaces below as the
		// terminal connect error.
		log.Printf("persistence: connect with persisted sessionID failed (%v); wiping state file and retrying fresh", err)
		if statePath != "" {
			if werr := Wipe(statePath); werr != nil {
				log.Printf("persistence: wipe failed (%v); next start may repeat this rejection", werr)
			}
		}
		loadedState = nil
		sessionID = [16]byte{}
		accept, conn, err = simple.Connect(&sessionID)
	}
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	// Closure form so the deferred Close picks up any subsequent
	// reassignment of conn (none today, but defends against future
	// re-connect logic). Drop-in replacement for the older
	// `defer conn.Close()` shape.
	defer func() { conn.Close() }()

	var writeMu sync.Mutex

	debuglog.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))
```

Note the closure-form defer replaces the bare `defer conn.Close()`. Future reassignment of `conn` (if any code below ever does it) will be visible to the deferred call.

- [ ] **Step 11.2: Run build**

```
go build ./...
```
Expected: clean build.

- [ ] **Step 11.3: Run client unit tests (no regressions)**

```
go test ./internal/runtime/client/ -v
```
Expected: all PASS.

- [ ] **Step 11.4: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Load persisted client state + stale-session retry on Connect (#199, Plan D)

The server's handshake fails (closes the connection) when a stale
sessionID is presented, so the failure surfaces at simple.Connect,
not simple.RequestResume. Retry-with-fresh-sessionID lives here
accordingly. The deferred Close uses a closure so any future
reassignment of conn is also closed at exit.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 12: Use loaded state to seed the resume request

**Files:**
- Modify: `internal/runtime/client/app.go:101-125` (the existing `if opts.Reconnect` block)

- [ ] **Step 12.1: Update the resume-request block to consume loaded state**

(The `defer conn.Close()` → closure form was already done in Task 11. Stale-session retry is also in Task 11. This task is now just "send the resume with the right inputs.")

Replace the current block (currently lines ~101-125):

```go
	lastSequence := uint64(0)

	var pendingAck atomic.Uint64
	var lastAck atomic.Uint64
	ackSignal := make(chan struct{}, 1)

	if opts.Reconnect {
		var viewports []protocol.PaneViewportState
		for _, e := range state.viewports.snapshotAll() {
			viewports = append(viewports, protocol.PaneViewportState{
				PaneID:         e.id,
				AltScreen:      e.vp.AltScreen,
				AutoFollow:     e.vp.AutoFollow,
				ViewBottomIdx:  e.vp.ViewBottomIdx,
				WrapSegmentIdx: e.vp.WrapSegmentIdx,
				ViewportRows:   e.vp.Rows,
				ViewportCols:   e.vp.Cols,
			})
		}
		if hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence, viewports); err != nil {
			return fmt.Errorf("resume request failed: %w", err)
		} else {
			handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence, &writeMu, &pendingAck, ackSignal)
		}
	}
```

(Note: Task 10 already converted `lastSequence` to `atomic.Uint64`, so `lastSequence := uint64(0)` is gone — it's now `var lastSequence atomic.Uint64` at the top of `Run`. The replacement below uses the atomic shape.)

Replace the block with the new code below. Stale-session retry now lives in Task 11 (around `simple.Connect`); `RequestResume` only fires for sessions that successfully completed the handshake, so any error here is a genuine resume failure (e.g., decode error of the response, malformed payload). We surface it via `return` rather than retry.

```go
	var lastSeqStart uint64
	if loadedState != nil {
		lastSeqStart = loadedState.LastSequence
	}
	lastSequence.Store(lastSeqStart) // lastSequence is atomic.Uint64 from Task 10

	var pendingAck atomic.Uint64
	var lastAck atomic.Uint64
	ackSignal := make(chan struct{}, 1)

	// Decide whether to send a resume: explicit --reconnect OR we
	// loaded a non-zero sessionID from disk.
	//
	// Note: state.cache and state.viewports are intentionally empty at
	// this point in a fresh-process invocation (no MsgTreeSnapshot
	// received yet, no panes rendered). The persisted PaneViewports
	// from disk are what feed the resume; live trackers are only used
	// for the same-process --reconnect case where they may be populated.
	shouldResume := opts.Reconnect || loadedState != nil
	if shouldResume {
		// Prefer persisted PaneViewports (fresh process, trackers map
		// is empty); fall back to live trackers for the same-process
		// reconnect case.
		var viewports []protocol.PaneViewportState
		if loadedState != nil && len(loadedState.PaneViewports) > 0 {
			viewports = loadedState.PaneViewports
		} else {
			for _, e := range state.viewports.snapshotAll() {
				viewports = append(viewports, protocol.PaneViewportState{
					PaneID:         e.id,
					AltScreen:      e.vp.AltScreen,
					AutoFollow:     e.vp.AutoFollow,
					ViewBottomIdx:  e.vp.ViewBottomIdx,
					WrapSegmentIdx: e.vp.WrapSegmentIdx,
					ViewportRows:   e.vp.Rows,
					ViewportCols:   e.vp.Cols,
				})
			}
		}

		hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence.Load(), viewports)
		if err != nil {
			// Resume against a session that completed handshake should
			// not normally fail. If it does, surface the error rather
			// than retrying — the connection is in an indeterminate
			// state and recovery is the user's job.
			return fmt.Errorf("resume request failed: %w", err)
		}
		handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence, &writeMu, &pendingAck, ackSignal)
	}
```

- [ ] **Step 12.2: Run build**

```
go build ./...
```
Expected: clean build.

- [ ] **Step 12.3: Run client unit tests**

```
go test ./internal/runtime/client/ -v
```
Expected: all PASS.

- [ ] **Step 12.4: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Seed resume request from persisted state (#199, Plan D)

Stale-session recovery is in Task 11 (around simple.Connect, where
the handshake actually fails); this commit just sends the resume
with the loaded sessionID/lastSequence/PaneViewports. RequestResume
errors at this point are genuine resume failures (decode error on
the response, etc.) and surface as terminal errors rather than
retries.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 13: Install the debounced Writer + flushFrame hook + flush on exit

**Files:**
- Modify: `internal/runtime/client/viewport_tracker.go` (add a `snapshotForPersistence` helper + hook a single persist call from `flushFrame`)
- Modify: `internal/runtime/client/client_state.go` (add `persistSnapshot func()` field)
- Modify: `internal/runtime/client/app.go` (instantiate Writer, set callback, defer Close)

The hook lives in `flushFrame` — already called once per render iteration with the dirty-tracker entries it just flushed to the wire. One hook there fires at most once per frame, naturally rate-limited by the render loop. No tracker-internal mutation hooks needed.

- [ ] **Step 13.1: Add a "snapshot for persistence" helper**

Append to `internal/runtime/client/viewport_tracker.go`:

```go
// snapshotForPersistence returns the same data as snapshotAll but as a
// []protocol.PaneViewportState ready to embed in a ClientState. Used
// by the Plan D persistence Writer.
func (t *viewportTrackers) snapshotForPersistence() []protocol.PaneViewportState {
	entries := t.snapshotAll()
	out := make([]protocol.PaneViewportState, 0, len(entries))
	for _, e := range entries {
		out = append(out, protocol.PaneViewportState{
			PaneID:         e.id,
			AltScreen:      e.vp.AltScreen,
			AutoFollow:     e.vp.AutoFollow,
			ViewBottomIdx:  e.vp.ViewBottomIdx,
			WrapSegmentIdx: e.vp.WrapSegmentIdx,
			ViewportRows:   e.vp.Rows,
			ViewportCols:   e.vp.Cols,
		})
	}
	return out
}
```

- [ ] **Step 13.2: Add `persistSnapshot` field on `clientState`**

Find the `clientState` struct definition:

```
grep -n 'type clientState struct' internal/runtime/client/
```

Append the field to the struct:

```go
	// persistSnapshot, if non-nil, is invoked once per flushFrame
	// iteration to schedule a debounced persist. Set by app.go when
	// Plan D persistence is active. Plan D / issue #199.
	persistSnapshot func()
```

- [ ] **Step 13.3: Hook `flushFrame` to call `persistSnapshot`**

In `internal/runtime/client/viewport_tracker.go`, at the very end of `flushFrame` (after the existing loops complete, just before the function returns), append:

```go
	// Plan D: persist client state once per frame that had any dirty
	// trackers. The Writer debounces (250ms) and skips if a save is
	// already in flight, so this is cheap; high render rates produce
	// a bounded write rate.
	if len(entries) > 0 && state.persistSnapshot != nil {
		state.persistSnapshot()
	}
```

- [ ] **Step 13.4: Wire Writer in app.go**

In `internal/runtime/client/app.go`, after the resume block (Task 12) and before screen init, add:

```go
	// Plan D: install debounced persistence Writer. nil-safe — if path
	// resolution failed, persistence is silently disabled.
	var persistWriter *Writer
	if statePath != "" {
		persistWriter = NewWriter(statePath, 250*time.Millisecond)
		defer persistWriter.Close() // flushes synchronously and waits for in-flight ticks
	}

	// persistSnapshot builds the current ClientState and hands it to
	// the debounced Writer. Called from flushFrame (rate-limited to
	// once per render iteration) and on exit.
	//
	// Note: lastSequence is atomic.Uint64 (Task 10), so .Load() is
	// race-safe even though readLoop mutates it from another goroutine.
	// sessionID is captured by reference — the retry in Task 12
	// reassigns it to the freshly-allocated session, and persistSnapshot
	// reads the current value at every invocation.
	//
	// IMPORTANT: there is NO eager initial seed. A persistSnapshot call
	// here would write LastSequence=0 with empty PaneViewports (because
	// no panes have rendered yet), which would overwrite the previous
	// session's state on a fast crash before the first frame. Wait for
	// the first real flushFrame to trigger the first save instead.
	persistSnapshot := func() {
		if persistWriter == nil {
			return
		}
		persistWriter.Update(ClientState{
			SocketPath:    opts.Socket,
			SessionID:     sessionID,
			LastSequence:  lastSequence.Load(),
			WrittenAt:     time.Now().UTC(),
			PaneViewports: state.viewports.snapshotForPersistence(),
		})
	}
	state.persistSnapshot = persistSnapshot
```

- [ ] **Step 13.5: Run build + tests + race detector**

```
go build ./...
go test ./internal/runtime/client/ -v
go test -race ./internal/runtime/client/...
```
Expected: clean build, all tests pass, no race reports.

- [ ] **Step 13.6: Commit**

```bash
git add internal/runtime/client/app.go internal/runtime/client/client_state.go internal/runtime/client/viewport_tracker.go
git commit -m "Wire debounced persistence Writer to flushFrame (#199, Plan D)

Single hook at the end of flushFrame (where dirty trackers are
already collected) drives a debounced persist. Once-per-render-frame
naturally bounds the write rate; the Writer's 250ms debounce + skip-
if-busy keeps the hot path from blocking on disk.

No eager initial seed: that would overwrite the previous session's
state with LastSequence=0 / empty PaneViewports if the user crashes
within the first 250ms. The first real flushFrame triggers the first
save instead.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Phase 4 — Flag Wiring

### Task 14: `--client-name` in `cmd/texelation/main.go`

**Files:**
- Modify: `cmd/texelation/main.go`

`cmd/texelation/main.go` constructs `clientrt.Options{}` at **two** sites — once for the `--client-only` mode and once for the default unified mode. Both must add the `ClientName` field, otherwise `--client-name` is silently ignored in one of the two modes.

- [ ] **Step 14.1: Register the flag**

Find the `reconnect` flag block:

```
grep -n 'reconnect.*Bool' cmd/texelation/main.go
```

Add the new flag adjacent to it:

```go
	clientName := fs.String("client-name", "", "Client identity slot for persistence (default: $TEXELATION_CLIENT_NAME or \"default\")")
```

- [ ] **Step 14.2: Validate `--client-name` and `TEXELATION_CLIENT_NAME` early; fail loudly on invalid**

After `fs.Parse(args)` returns, validate either the flag value (if set) or the env-var fallback (if the flag is empty but the env-var is set). The user explicitly opted into a named slot via either input; silently disabling persistence later would be a UX trap. Fail at startup with a clear error.

We use `clientrt.ValidateClientName` (added in Task 5 above) rather than `clientrt.ResolvePath` so a HOME-dir lookup failure on a minimal container / sandboxed unit doesn't get blamed on the user's flag value:

```go
	// Plan D: validate --client-name (or $TEXELATION_CLIENT_NAME)
	// early. Either input expresses the user's intent to use a named
	// persistence slot; silently disabling persistence later when
	// ResolvePath rejects the name is a UX trap. ValidateClientName
	// only checks the name itself — it does not touch $HOME or the
	// socket — so failures here are unambiguously "the user-supplied
	// name is invalid", not "your environment is misconfigured."
	if *clientName != "" {
		if err := clientrt.ValidateClientName(*clientName); err != nil {
			return fmt.Errorf("invalid --client-name %q: %w", *clientName, err)
		}
	} else if envName := os.Getenv(clientrt.ClientNameEnvVar); envName != "" {
		if err := clientrt.ValidateClientName(envName); err != nil {
			return fmt.Errorf("invalid $%s %q: %w", clientrt.ClientNameEnvVar, envName, err)
		}
	}
```

(`clientrt.ValidateClientName` and `clientrt.ClientNameEnvVar` are exported from `internal/runtime/client/persistence.go`, accessible via the existing `clientrt "github.com/framegrace/texelation/internal/runtime/client"` import. `os` and `fmt` should already be imported in `cmd/texelation/main.go`.)

- [ ] **Step 14.3: Locate ALL `clientrt.Options{}` construction sites**

```
grep -n 'clientrt.Options{' cmd/texelation/main.go
```

Expected: at least two sites — one for `--client-only` mode (around line 115) and one for the default unified mode (around line 130). Update each to include `ClientName: *clientName` in the struct literal.

For each, the new shape adds `ClientName: *clientName` to the existing struct literal. Match each site's existing field names — the file uses `*socketPath` (not `resolvedSocket`) and `*panicLog` (or `*panicLogPath`), so don't introduce undefined identifiers. Example:

```go
	opts := clientrt.Options{
		Socket:     *socketPath,           // existing field — match what's already there
		Reconnect:  *reconnect,            // existing field
		PanicLog:   *panicLog,             // existing field
		ClientName: *clientName,           // NEW from Plan D
	}
```

(Field shape varies slightly between the two sites — preserve each site's existing fields verbatim and only add `ClientName: *clientName`.)

- [ ] **Step 14.4: Run build**

```
go build ./cmd/texelation/
```
Expected: clean build.

- [ ] **Step 14.5: Verify both sites updated**

```
grep -A4 'clientrt.Options{' cmd/texelation/main.go
```

Expected: every `clientrt.Options{` literal contains `ClientName:`. If any one is missing, the flag is silently ignored in that mode.

- [ ] **Step 14.6: Commit**

```bash
git add cmd/texelation/main.go
git commit -m "texelation: --client-name flag (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 15: `--client-name` in `client/cmd/texel-client/main.go`

**Files:**
- Modify: `client/cmd/texel-client/main.go:32-44`

- [ ] **Step 15.1: Register the flag, validate early, propagate**

In `client/cmd/texel-client/main.go`, replace the existing flag block (lines ~32-44):

```go
	socket := fs.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := fs.Bool("reconnect", false, "Attempt to resume previous session")
	panicLogPath := fs.String("panic-log", "", "File to append panic stack traces")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := clientrt.Options{
		Socket:    *socket,
		Reconnect: *reconnect,
		PanicLog:  *panicLogPath,
	}
	return runClient(opts)
```

with:

```go
	socket := fs.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := fs.Bool("reconnect", false, "Attempt to resume previous session")
	panicLogPath := fs.String("panic-log", "", "File to append panic stack traces")
	clientName := fs.String("client-name", "", "Client identity slot for persistence (default: $TEXELATION_CLIENT_NAME or \"default\")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Plan D: validate --client-name (or $TEXELATION_CLIENT_NAME)
	// early. Either input expresses the user's intent to use a named
	// slot; silently disabling persistence later is a UX trap.
	// ValidateClientName checks only the name — never touches $HOME
	// or the socket — so failures unambiguously blame the right input.
	if *clientName != "" {
		if err := clientrt.ValidateClientName(*clientName); err != nil {
			return fmt.Errorf("invalid --client-name %q: %w", *clientName, err)
		}
	} else if envName := os.Getenv(clientrt.ClientNameEnvVar); envName != "" {
		if err := clientrt.ValidateClientName(envName); err != nil {
			return fmt.Errorf("invalid $%s %q: %w", clientrt.ClientNameEnvVar, envName, err)
		}
	}
	opts := clientrt.Options{
		Socket:     *socket,
		Reconnect:  *reconnect,
		PanicLog:   *panicLogPath,
		ClientName: *clientName,
	}
	return runClient(opts)
```

(Add `"fmt"` and `"os"` to the import block if not already present.)

- [ ] **Step 15.2: Run build**

```
go build ./client/cmd/texel-client/
```
Expected: clean build.

- [ ] **Step 15.3: Update `client/cmd/texel-client/main_test.go` if it asserts flag set**

```
grep -n 'flag\|reconnect' client/cmd/texel-client/main_test.go
```

If the test asserts the parsed Options, add `ClientName` to its expectations.

- [ ] **Step 15.4: Run tests**

```
go test ./client/cmd/texel-client/ -v
```
Expected: PASS.

- [ ] **Step 15.5: Commit**

```bash
git add client/cmd/texel-client/main.go client/cmd/texel-client/main_test.go
git commit -m "texel-client: --client-name flag (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Phase 5 — Integration Tests

### Harness reality check

The existing `*_integration_test.go` files in `internal/runtime/server/` use a memconn-based pattern (`newMemHarness` from `internal/runtime/server/testutil`) that:

- Spins up a `Manager` + `Session` over `net.Pipe`.
- Constructs `protocol.ResumeRequest` payloads manually and pushes them via `h.writeFrame(...)`.
- Asserts server-side behavior via `h.AwaitRow(...)`, `h.fakeApp.FeedRows(...)`, etc.

**There is NO harness that runs `clientruntime.Run` with a real screen, render loop, and `flushFrame` driver.** Building one would be substantial new work — out of scope for Plan D Layer 1. The realistic Phase 5 leans on:

1. **Unit tests of the persistence module** (Tasks 4-8) — cover Save / Load / Wipe / Writer mechanically.
2. **Server-side integration tests using `newMemHarness`** — verify that disk-roundtripped state assembles into a `ResumeRequest` the server processes correctly, that phantom-pane pruning fires end-to-end, and that an evicted session is rejected cleanly.
3. **Manual end-to-end verification** (Task 20) — the only place the full client → daemon → resume → screen path is exercised, and it's done by a human.

This phasing matches the spec's pragmatic stance (Layer 1 client persistence) without inventing a new test harness. Tasks 16-18 below reflect that.

Each test's `t.Setenv("XDG_STATE_HOME", t.TempDir())` isolates persistence per-test — no shared global state.

### Task 16: `TestIntegration_PersistedStateDrivesResumeRequest`

This integration test verifies that disk-roundtripped state assembles into a `ResumeRequest` that the server applies correctly. It does NOT exercise `clientruntime.Run` — that's covered by manual e2e in Task 20. The mechanically-checkable boundary is: persistence module → wire payload → server applies viewports.

**Files:**
- Test: `internal/runtime/server/resume_viewport_integration_test.go` (extends the existing Plan B test file with the same harness pattern).

- [ ] **Step 16.1: Write the test using `newMemHarness`**

Append to `internal/runtime/server/resume_viewport_integration_test.go`:

```go
// TestIntegration_PersistedStateDrivesResumeRequest verifies the
// disk → wire → server boundary of Plan D persistence. We construct
// a ClientState in memory, Save+Load it through persistence.go (so
// JSON encoding/decoding is exercised), build a ResumeRequest from
// the loaded fields, push it through newMemHarness, and assert the
// server applied the resumed viewport correctly.
//
// This stops short of running a full clientruntime.Run; that path
// is covered by Task 20's manual end-to-end verification. The test
// here proves the wire shape is consistent end-to-end.
func TestIntegration_PersistedStateDrivesResumeRequest(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	h := newMemHarness(t, 80, 24)

	// Feed 200 rows so a scrolled-back gid=50 is in range.
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	h.fakeApp.FeedRows(0, lines)

	// Build the ClientState a real client would have persisted: it
	// holds the same sessionID/paneID the harness allocated, plus a
	// scrolled-back PaneViewport.
	socketPath := "/tmp/test-d-rrq.sock" // sanity-check value; not actually opened
	statePath, err := clientruntime.ResolvePath(socketPath, "default")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	want := clientruntime.ClientState{
		SocketPath:   socketPath,
		SessionID:    h.sessionID(),
		LastSequence: 0,
		WrittenAt:    time.Now().UTC(),
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         h.paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  50,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	}

	// Save + Load — exercises JSON encode/decode round-trip.
	if err := clientruntime.Save(statePath, &want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := clientruntime.Load(statePath, socketPath)
	if err != nil || got == nil {
		t.Fatalf("Load: state=%v err=%v", got, err)
	}

	// Construct the ResumeRequest as app.go would, from the loaded state.
	resume := protocol.ResumeRequest{
		SessionID:     got.SessionID,
		LastSequence:  got.LastSequence,
		PaneViewports: got.PaneViewports,
	}
	payload, err := protocol.EncodeResumeRequest(resume)
	if err != nil {
		t.Fatalf("encode resume: %v", err)
	}
	h.writeFrame(protocol.MsgResumeRequest, payload, h.sessionID())

	// Server should apply the resumed viewport — render buffer ends at gid 50.
	// Same assertion as the existing TestIntegration_ResumeHonorsPaneViewport
	// (which uses an in-memory-built request); this one proves the loaded-
	// from-disk shape is wire-equivalent.
	h.AwaitRow(h.paneID, 48, 2*time.Second)
}
```

- [ ] **Step 16.2: Run test to verify it passes**

```
go test -tags=integration ./internal/runtime/server/ -run TestIntegration_PersistedStateDrivesResumeRequest -v
```
Expected: PASS. (If it fails, the failure is in serialization or in the protocol layer, not in the daemon harness — debug accordingly.)

- [ ] **Step 16.3: Commit**

```bash
git add internal/runtime/server/resume_viewport_integration_test.go
git commit -m "Integration: disk-roundtripped state drives ResumeRequest (#199, Plan D)

Verifies the persistence-module-to-wire boundary: a Save+Load round
trip produces a ResumeRequest the server applies correctly. Stops
short of a full clientruntime.Run; that's manual e2e (Task 20).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 17: `TestUnit_StaleSessionFallsBackClean`

The client-side wipe-and-retry path in Task 11 is the load-bearing recovery mechanic. Manual e2e (Task 20) exercises it end-to-end against a real daemon. For automated coverage, we verify the persistence module's contribution: parsing a stale state, recognizing the rejection signal, wiping cleanly, and accepting a fresh write afterward. The actual `simple.Connect` retry call lives in `app.go` and is exercised by Task 20; here we test the disk-side mechanic in isolation.

**Files:**
- Test: `internal/runtime/client/persistence_test.go`

- [ ] **Step 17.1: Write the test**

Append to `persistence_test.go`:

```go
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
	if _, err := os.Stat(path); !os.IsNotExist(err) {
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
```

- [ ] **Step 17.2: Run test**

```
go test ./internal/runtime/client/ -run TestStaleSessionWipeAndReplace -v
```
Expected: PASS.

- [ ] **Step 17.3: Commit**

```bash
git add internal/runtime/client/persistence_test.go
git commit -m "Test: stale sessionID wipe-and-replace mechanic (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 18: Multi-client isolation as a persistence-layer test

Multi-client isolation is a property of the persistence module's path resolution, not a server-side runtime invariant. Two clients with different `--client-name` end up at distinct file paths via `ResolvePath`; each `Save`/`Load` operates only on its own path. Pure unit-test territory.

**Files:**
- Test: `internal/runtime/client/persistence_test.go`

- [ ] **Step 18.1: Write the test**

Append to `persistence_test.go`:

```go
// TestMultiClientIsolation verifies that two distinct --client-name
// values against the same socket produce isolated state files: writing
// one does not affect the other, and each loads back independently.
//
// This is the disk-layer invariant; full multi-client e2e (two
// texelations, both running, one daemon) is verified manually in Task 20.
func TestMultiClientIsolation(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	socketPath := "/tmp/test-multiclient.sock"

	pathLeft, err := ResolvePath(socketPath, "left")
	if err != nil {
		t.Fatalf("ResolvePath left: %v", err)
	}
	pathRight, err := ResolvePath(socketPath, "right")
	if err != nil {
		t.Fatalf("ResolvePath right: %v", err)
	}
	if pathLeft == pathRight {
		t.Fatalf("expected distinct paths, got %q == %q", pathLeft, pathRight)
	}

	stateLeft := ClientState{
		SocketPath:   socketPath,
		SessionID:    [16]byte{0xa},
		LastSequence: 100,
		WrittenAt:    time.Now().UTC(),
	}
	stateRight := ClientState{
		SocketPath:   socketPath,
		SessionID:    [16]byte{0xb},
		LastSequence: 200,
		WrittenAt:    time.Now().UTC(),
	}

	if err := Save(pathLeft, &stateLeft); err != nil {
		t.Fatalf("Save left: %v", err)
	}
	if err := Save(pathRight, &stateRight); err != nil {
		t.Fatalf("Save right: %v", err)
	}

	gotLeft, err := Load(pathLeft, socketPath)
	if err != nil || gotLeft == nil {
		t.Fatalf("Load left: state=%v err=%v", gotLeft, err)
	}
	gotRight, err := Load(pathRight, socketPath)
	if err != nil || gotRight == nil {
		t.Fatalf("Load right: state=%v err=%v", gotRight, err)
	}

	if gotLeft.SessionID != stateLeft.SessionID {
		t.Errorf("left SessionID mismatch: got %v want %v", gotLeft.SessionID, stateLeft.SessionID)
	}
	if gotRight.SessionID != stateRight.SessionID {
		t.Errorf("right SessionID mismatch: got %v want %v", gotRight.SessionID, stateRight.SessionID)
	}
	if gotLeft.SessionID == gotRight.SessionID {
		t.Errorf("expected distinct SessionIDs across clients")
	}
	if gotLeft.LastSequence != 100 || gotRight.LastSequence != 200 {
		t.Errorf("LastSequence cross-contamination: left=%d right=%d", gotLeft.LastSequence, gotRight.LastSequence)
	}

	// Overwriting one slot must not affect the other.
	stateLeft.LastSequence = 999
	if err := Save(pathLeft, &stateLeft); err != nil {
		t.Fatalf("Save left overwrite: %v", err)
	}
	gotRightAgain, _ := Load(pathRight, socketPath)
	if gotRightAgain.LastSequence != 200 {
		t.Errorf("right state corrupted by left overwrite: LastSequence %d != 200", gotRightAgain.LastSequence)
	}
}
```

- [ ] **Step 18.2: Run test**

```
go test ./internal/runtime/client/ -run TestMultiClientIsolation -v
```
Expected: PASS.

- [ ] **Step 18.3: Commit**

```bash
git add internal/runtime/client/persistence_test.go
git commit -m "Test: multi-client persistence isolation by --client-name (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Phase 6 — Final Verification

### Task 19: Race detector pass + full suite

- [ ] **Step 19.1: Run race detector across changed packages**

```
go test -race ./internal/runtime/client/... ./internal/runtime/server/... ./protocol/...
```
Expected: no race reports, all PASS.

- [ ] **Step 19.2: Full repo test**

```
make test
```
Expected: all PASS.

- [ ] **Step 19.3: If any pre-existing failures: distinguish from regressions**

Compare results against `git stash; make test` baseline to confirm nothing introduced by Plan D. Pre-existing failures (per `project_issue199_planB_review_findings.md`: `TestClientReceivesTreeSnapshotAfterVerticalSplit` and ~10 siblings on main) are not Plan D's problem; document if present and proceed.

- [ ] **Step 19.4: No commit (verification only).**

---

### Task 20: Manual end-to-end verification

- [ ] **Step 20.1: Build**

```
make build
```

- [ ] **Step 20.2: Single-client resume test**

1. `./bin/texelation` — start daemon + client.
2. In the default pane, run `seq 1 200; bash` (gives ~200 rows of scrollback).
3. Scroll up by ~100 rows.
4. Quit the client (`Ctrl+Q` or whatever the project's quit binding is).
5. `./bin/texelation` — start a fresh client; daemon survives.
6. **Expected:** pane is at the same scroll position from step 3.
7. Inspect the file:

```bash
ls -la ~/.local/state/texelation/client/*/default.json
jq . ~/.local/state/texelation/client/*/default.json
```

- [ ] **Step 20.3: Multi-client test**

In two terminals:

```
./bin/texelation --client-name=left
./bin/texelation --client-name=right
```

Scroll each independently, exit, restart with the same names. Expected: each lands at its own saved scroll. Check both files exist:

```
ls -la ~/.local/state/texelation/client/*/{left,right}.json
```

- [ ] **Step 20.4: Stale-session fallback test**

1. Launch `./bin/texelation`; let it persist.
2. Force a daemon restart so the in-memory session is wiped:
   ```
   pkill texel-server
   ```
3. Launch `./bin/texelation` again.
4. **Expected:** client logs `persistence: resume rejected ... wiping state file and reconnecting fresh` (or similar). Subsequent operation is normal.
5. Confirm the file holds a new sessionID:
   ```
   jq .sessionID ~/.local/state/texelation/client/*/default.json
   ```

- [ ] **Step 20.5: Mark plan complete; flip progress memory**

After successful verification, update `/home/marc/.claude/projects/-home-marc-projects-texel-texelation/memory/project_issue199_progress.md`:
- Flip Plan D row to MERGED with the eventual PR # and squash hash (post-merge).
- Bump active plan to D2 or C as the user prefers.

(This step is post-merge; not part of the PR commits.)

---

## Self-Review Checklist (run before opening PR)

- [ ] All 20 tasks committed; `git log feature/issue-199-plan-d-persistence ^main --oneline` shows ~20 commits.
- [ ] No `TODO` / `FIXME` / `XXX` introduced in this branch (`git diff main...HEAD | grep -iE 'TODO|FIXME|XXX'`).
- [ ] `make clean && make build && make test` passes from a cold cache (defends against stale `.cache/` masking a real breakage).
- [ ] `go test -race ./internal/runtime/client/... ./internal/runtime/server/... ./protocol/...` passes (or pre-existing failures only — compare against `git stash; go test -race ...` baseline).
- [ ] No new files outside the planned File Structure table (excluding optional `client_integration_helpers_test.go` documented in Phase 5 notes).
- [ ] Spec sections covered: storage location & schema (Tasks 4–7), write cadence (Task 8), read cadence (Tasks 11–12), multi-client (Tasks 5, 14, 15, 18), error handling (Tasks 7, 11–12), server carry-forwards (Tasks 1–3), pre-existing race fix (Task 10), testing (Tasks 16–18). All ✓.
- [ ] Manual e2e (Task 20) verified on at least one of: single-client resume, stale-session fallback. Multi-client and Windows-style filename rejection covered by integration tests.

---

## Out of scope (deferred)

- Server-side cross-daemon-restart persistence → **Plan D2** (`docs/superpowers/plans/2026-04-25-issue-199-plan-d2-server-viewport-persistence.md`).
- Session recovery / discovery → **Plan F** (`docs/superpowers/plans/2026-04-26-issue-199-plan-f-session-recovery.md`). Plan D's schema is recovery-compatible without changes.
