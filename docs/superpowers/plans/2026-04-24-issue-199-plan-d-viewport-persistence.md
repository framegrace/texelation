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
| `internal/runtime/server/client_viewport.go` | Modify | `ApplyResume` accepts a `paneExists func([16]byte) bool` predicate; phantom entries dropped before insert |
| `internal/runtime/server/client_viewport_test.go` | Modify | `TestApplyResume_PrunesPhantomPaneIDs` + `TestApplyResume_EvictedSessionWithPaneViewports` |
| `internal/runtime/server/connection_handler.go` | Modify | Pass `desktop.AppByID(id) != nil` predicate at the `ApplyResume` call site |
| `internal/runtime/client/persistence.go` | **Create** | `ClientState` type, `Load`/`Save`/`Wipe`, `ResolvePath`, debounced `Writer` |
| `internal/runtime/client/persistence_test.go` | **Create** | Round-trip, atomic-replace, mismatch wipe, parse-error wipe, debounce coalescing |
| `internal/runtime/client/app.go` | Modify | Replace zero-sessionID branch (lines 54-59) with load-from-disk; install `Writer`; flush on exit; handle `ErrSessionNotFound` → wipe + retry |
| `internal/runtime/client/app.go` | Modify | `Options` gains `ClientName string` |
| `cmd/texelation/main.go` | Modify | Register `--client-name` flag; `TEXELATION_CLIENT_NAME` env fallback |
| `client/cmd/texel-client/main.go` | Modify | Register `--client-name` flag; `TEXELATION_CLIENT_NAME` env fallback |
| `internal/runtime/server/client_integration_test.go` | Modify | `TestIntegration_PersistedClientResumesViewport`, `TestIntegration_StaleSessionFallsBackClean`, `TestIntegration_MultiClientIsolation` |

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
	// Hdr.PayloadLen at offset 32-36
	binary.LittleEndian.PutUint32(hdr[32:36], MaxPayloadLen+1)

	// CRC over bytes [4:36] (zeros for everything except version/type/payloadLen).
	crc := crc32.NewIEEE()
	_, _ = crc.Write(hdr[4:36])
	binary.LittleEndian.PutUint32(hdr[36:40], crc.Sum32())
	hdr[6] = FlagChecksum

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

- [ ] **Step 2.3: Update `ApplyResume` signature**

In `internal/runtime/server/client_viewport.go`, replace the `ApplyResume` function (line 97 onward). The new signature accepts a `paneExists` predicate; entries whose paneID returns `false` are dropped:

```go
// ApplyResume seeds per-pane viewports from a resume payload.
//
// The paneExists predicate filters out phantom paneIDs — IDs the client
// supplied that no longer exist server-side (closed pane during offline
// time, cross-restart drift, or a recovered persistence file pointing
// at a long-gone pane). Without pruning, the map grows unboundedly with
// stale entries on every cross-restart resume.
//
// (Existing doc-comment about top/bottom derivation, Policy A, etc.,
// stays attached above this — keep verbatim.)
func (c *ClientViewports) ApplyResume(states []protocol.PaneViewportState, paneExists func(id [16]byte) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ps := range states {
		if paneExists != nil && !paneExists(ps.PaneID) {
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
}
```

- [ ] **Step 2.4: Update the call site in `connection_handler.go`**

Find the `ApplyResume` call:

```
grep -n 'ApplyResume' internal/runtime/server/connection_handler.go
```

At that line (it currently passes `request.PaneViewports`), update to:

```go
c.session.viewports.ApplyResume(request.PaneViewports, func(id [16]byte) bool {
	return desktop.AppByID(id) != nil
})
```

(Keep the same `desktop` reference the surrounding code already uses; if it's not in scope, use `c.sink.(*DesktopSink).engine` — match the pattern of the nearby `RestorePaneViewport` call.)

- [ ] **Step 2.5: Update any other call sites**

```
grep -rn 'ApplyResume(' internal/runtime/server/ | grep -v _test.go
```

For each non-test call site, pass `nil` as the second argument if pruning is not desired, or an appropriate predicate. (Test sites are updated in the next test task.)

- [ ] **Step 2.6: Run test to verify it passes**

```
go test ./internal/runtime/server/ -run TestApplyResume_PrunesPhantomPaneIDs -v
```
Expected: PASS.

- [ ] **Step 2.7: Update existing ApplyResume tests for the new signature**

```
grep -n 'ApplyResume(' internal/runtime/server/*_test.go
```

For each test that calls `ApplyResume`, add a second argument. Tests that don't care about pruning pass `nil` (which means "accept all"):

```go
cv.ApplyResume(states, nil)
```

Tests that explicitly want a known-pane predicate use the same shape as Step 2.3.

- [ ] **Step 2.8: Run the server test suite (no regressions)**

```
go test ./internal/runtime/server/ -v
```
Expected: all PASS.

- [ ] **Step 2.9: Commit**

```bash
git add internal/runtime/server/client_viewport.go internal/runtime/server/client_viewport_test.go internal/runtime/server/connection_handler.go internal/runtime/server/*_test.go
git commit -m "ClientViewports: prune phantom pane IDs at ApplyResume (#199, Plan D)

Plan B left the map growing unboundedly with stale paneIDs from
clients that resumed against a different pane tree. Plan D adds a
paneExists predicate; the connection handler passes
desktop.AppByID(id) != nil so cross-restart and recovery payloads
self-clean.

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

// validClientName rejects path-traversal and shell-meta characters.
// Restrict to ASCII alphanumerics, dot, dash, and underscore.
func validClientName(name string) bool {
	if name == "" || name == "." || name == ".." {
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

	// No leftover .tmp file.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
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

	// Best-effort cleanup if anything below fails.
	defer func() {
		_ = os.Remove(tmpPath)
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
		// Corrupt; wipe and treat as fresh.
		_ = Wipe(filePath)
		return nil, nil
	}

	if s.SocketPath != expectedSocketPath {
		// Stale from a different daemon; wipe and treat as fresh.
		_ = Wipe(filePath)
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
```

- [ ] **Step 8.2: Run tests to verify they fail**

```
go test ./internal/runtime/client/ -run TestWriter -v
```
Expected: FAIL — `Writer`/`NewWriter` undefined.

- [ ] **Step 8.3: Implement `Writer`**

Append to `persistence.go`:

```go
// Writer debounces saves of ClientState to a file. Save calls fire at
// most once per debounce window; rapid updates coalesce. If a Save is
// in flight when an update arrives, the update is buffered and a
// follow-up Save is scheduled when the in-flight one completes
// (skip-if-busy: the live writer is never blocked by disk).
//
// Crash-loss is bounded by the debounce window. Plan D's design
// constraint: ≤250ms of viewport movement on hard kill, perceptually
// invisible to the user.
type Writer struct {
	filePath string
	debounce time.Duration

	mu      sync.Mutex
	state   *ClientState // latest pending state, nil when consumed
	timer   *time.Timer  // pending debounce timer, nil when none
	busy    bool         // a Save is currently in flight
	closed  bool
	doneCh  chan struct{} // closed when Close completes
}

func NewWriter(filePath string, debounce time.Duration) *Writer {
	return &Writer{
		filePath: filePath,
		debounce: debounce,
		doneCh:   make(chan struct{}),
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
	if w.timer == nil && !w.busy {
		w.timer = time.AfterFunc(w.debounce, w.tick)
	}
	// else: a tick or in-flight save will pick up the latest state.
}

// Flush saves the latest pending state synchronously, dropping any
// pending debounce. Safe to call concurrent with Update.
func (w *Writer) Flush() {
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	s := w.state
	w.state = nil
	w.busy = true
	w.mu.Unlock()

	if s != nil {
		_ = Save(w.filePath, s) // log, don't crash
	}

	w.mu.Lock()
	w.busy = false
	// If Update arrived during the save, schedule a tick so it lands.
	if w.state != nil && !w.closed && w.timer == nil {
		w.timer = time.AfterFunc(w.debounce, w.tick)
	}
	w.mu.Unlock()
}

// Close flushes and stops the writer. Subsequent Updates are no-ops.
func (w *Writer) Close() {
	w.Flush()
	w.mu.Lock()
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()
	close(w.doneCh)
}

func (w *Writer) tick() {
	w.mu.Lock()
	if w.closed || w.state == nil {
		w.timer = nil
		w.mu.Unlock()
		return
	}
	s := *w.state
	w.state = nil
	w.timer = nil
	w.busy = true
	w.mu.Unlock()

	_ = Save(w.filePath, &s)

	w.mu.Lock()
	w.busy = false
	if w.state != nil && !w.closed {
		w.timer = time.AfterFunc(w.debounce, w.tick)
	}
	w.mu.Unlock()
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

### Task 10: Replace zero-sessionID branch with load-from-disk

**Files:**
- Modify: `internal/runtime/client/app.go:53-59` (the existing init block)

- [ ] **Step 10.1: Wire `ResolvePath` + `Load` at startup**

In `internal/runtime/client/app.go`, replace this block (currently lines ~53-59):

```go
	simple := client.NewSimpleClient(opts.Socket)
	var sessionID [16]byte
	if !opts.Reconnect {
		sessionID = [16]byte{}
	}

	accept, conn, err := simple.Connect(&sessionID)
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
```

- [ ] **Step 10.2: Run build**

```
go build ./...
```
Expected: clean build.

- [ ] **Step 10.3: Run client unit tests (no regressions)**

```
go test ./internal/runtime/client/ -v
```
Expected: all PASS.

- [ ] **Step 10.4: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Load persisted client state on startup (#199, Plan D)

Replaces the dead zero-sessionID branch. Plan B's resume scaffolding
(simple.Connect + simple.RequestResume) is unchanged; this just wires
the disk layer in front of it.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 11: Use loaded state to seed the resume request

**Files:**
- Modify: `internal/runtime/client/app.go:101-125` (the existing `if opts.Reconnect` block)

- [ ] **Step 11.1: Update the resume-request block**

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

with:

```go
	lastSequence := uint64(0)
	if loadedState != nil {
		lastSequence = loadedState.LastSequence
	}

	var pendingAck atomic.Uint64
	var lastAck atomic.Uint64
	ackSignal := make(chan struct{}, 1)

	// Decide whether to send a resume: explicit --reconnect OR we
	// loaded a non-zero sessionID from disk.
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

		hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence, viewports)
		if err != nil {
			// Stale sessionID is the dominant failure mode. Wipe and
			// start fresh; subsequent clean exit will write the new
			// sessionID. We log at info because this is an expected
			// part of normal recovery, not a bug.
			if statePath != "" {
				log.Printf("persistence: resume rejected (%v); wiping state file and reconnecting fresh", err)
				_ = Wipe(statePath)
			} else {
				log.Printf("persistence: resume rejected (%v); reconnecting fresh", err)
			}
			_ = conn.Close()

			// Reconnect with zero sessionID.
			sessionID = [16]byte{}
			loadedState = nil
			lastSequence = 0
			accept, conn, err = simple.Connect(&sessionID)
			if err != nil {
				return fmt.Errorf("reconnect after stale session failed: %w", err)
			}
			state.conn = conn
			state.sessionID = accept.SessionID
			sessionID = accept.SessionID
		} else {
			handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence, &writeMu, &pendingAck, ackSignal)
		}
	}
```

- [ ] **Step 11.2: Run build**

```
go build ./...
```
Expected: clean build.

- [ ] **Step 11.3: Run client unit tests**

```
go test ./internal/runtime/client/ -v
```
Expected: all PASS.

- [ ] **Step 11.4: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Seed resume from persisted state; wipe-and-retry on stale (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 12: Install the debounced Writer + flushFrame hook + flush on exit

**Files:**
- Modify: `internal/runtime/client/viewport_tracker.go` (add a `snapshotForPersistence` helper + hook a single persist call from `flushFrame`)
- Modify: `internal/runtime/client/client_state.go` (add `persistSnapshot func()` field)
- Modify: `internal/runtime/client/app.go` (instantiate Writer, set callback, defer Close)

The hook lives in `flushFrame` — already called once per render iteration with the dirty-tracker entries it just flushed to the wire. One hook there fires at most once per frame, naturally rate-limited by the render loop. No tracker-internal mutation hooks needed.

- [ ] **Step 12.1: Add a "snapshot for persistence" helper**

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

- [ ] **Step 12.2: Add `persistSnapshot` field on `clientState`**

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

- [ ] **Step 12.3: Hook `flushFrame` to call `persistSnapshot`**

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

- [ ] **Step 12.4: Wire Writer in app.go**

In `internal/runtime/client/app.go`, after the resume block (Task 11) and before screen init, add:

```go
	// Plan D: install debounced persistence Writer. nil-safe — if path
	// resolution failed, persistence is silently disabled.
	var persistWriter *Writer
	if statePath != "" {
		persistWriter = NewWriter(statePath, 250*time.Millisecond)
		defer persistWriter.Close() // flushes synchronously
	}

	// persistSnapshot builds the current ClientState and hands it to
	// the debounced Writer. Called from flushFrame (rate-limited to
	// once per render iteration) and on exit.
	persistSnapshot := func() {
		if persistWriter == nil {
			return
		}
		persistWriter.Update(ClientState{
			SocketPath:    opts.Socket,
			SessionID:     sessionID,
			LastSequence:  lastSequence,
			WrittenAt:     time.Now().UTC(),
			PaneViewports: state.viewports.snapshotForPersistence(),
		})
	}
	state.persistSnapshot = persistSnapshot
```

- [ ] **Step 12.5: Run build + tests**

```
go build ./...
go test ./internal/runtime/client/ -v
```
Expected: clean build, all tests pass.

- [ ] **Step 12.6: Commit**

```bash
git add internal/runtime/client/app.go internal/runtime/client/client_state.go internal/runtime/client/viewport_tracker.go
git commit -m "Wire debounced persistence Writer to flushFrame (#199, Plan D)

Single hook at the end of flushFrame (where dirty trackers are
already collected) drives a debounced persist. Once-per-render-frame
naturally bounds the write rate; the Writer's 250ms debounce + skip-
if-busy keeps the hot path from blocking on disk.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 13: First-paint persist seed

**Files:**
- Modify: `internal/runtime/client/app.go` (right after `state.persistSnapshot = persistSnapshot`)

- [ ] **Step 13.1: Seed an initial snapshot so the file exists from the first frame**

In `app.go`, immediately after `state.viewports.onChange = persistSnapshot`, add:

```go
	// Capture the initial sessionID immediately; subsequent viewport
	// changes will refresh PaneViewports + LastSequence. Without this
	// seed, a client that exits before the first viewport tick leaves
	// no state file behind.
	persistSnapshot()
```

- [ ] **Step 13.2: Run build**

```
go build ./...
```
Expected: clean build.

- [ ] **Step 13.3: Commit**

```bash
git add internal/runtime/client/app.go
git commit -m "Seed initial persistence snapshot at startup (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

## Phase 4 — Flag Wiring

### Task 14: `--client-name` in `cmd/texelation/main.go`

**Files:**
- Modify: `cmd/texelation/main.go` (around line 59 where `reconnect` is registered)

- [ ] **Step 14.1: Register the flag and propagate**

Find the `reconnect` flag block:

```
grep -n 'reconnect.*Bool' cmd/texelation/main.go
```

Add the new flag adjacent to it:

```go
	clientName := fs.String("client-name", "", "Client identity slot for persistence (default: $TEXELATION_CLIENT_NAME or \"default\")")
```

Find where `clientrt.Options{...}` is constructed in `cmd/texelation/main.go` and add the field:

```go
	opts := clientrt.Options{
		Socket:     resolvedSocket,
		Reconnect:  *reconnect,
		PanicLog:   *panicLog,
		ClientName: *clientName,
	}
```

- [ ] **Step 14.2: Run build**

```
go build ./cmd/texelation/
```
Expected: clean build.

- [ ] **Step 14.3: Commit**

```bash
git add cmd/texelation/main.go
git commit -m "texelation: --client-name flag (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 15: `--client-name` in `client/cmd/texel-client/main.go`

**Files:**
- Modify: `client/cmd/texel-client/main.go:32-44`

- [ ] **Step 15.1: Register the flag and propagate**

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
	opts := clientrt.Options{
		Socket:     *socket,
		Reconnect:  *reconnect,
		PanicLog:   *panicLogPath,
		ClientName: *clientName,
	}
	return runClient(opts)
```

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

### Task 16: `TestIntegration_PersistedClientResumesViewport`

**Files:**
- Test: `internal/runtime/server/client_integration_test.go`

- [ ] **Step 16.1: Identify the harness**

```
grep -n 'func TestIntegration_' internal/runtime/server/client_integration_test.go | head -5
```

The harness from Plan A/B (`newIntegrationServer` or similar) is what we extend. Match its setup pattern.

- [ ] **Step 16.2: Write the failing test**

Append to `client_integration_test.go`:

```go
// TestIntegration_PersistedClientResumesViewport verifies the
// end-to-end Plan D persistence path: client #1 connects, scrolls
// back, persists state, exits cleanly. Client #2 (a different
// process simulated by a fresh runtime) loads the persisted state
// and lands at the same viewport.
func TestIntegration_PersistedClientResumesViewport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	srv, cleanup := newIntegrationServer(t)
	defer cleanup()

	socketPath := srv.SocketPath()

	// --- Client #1: connect, scroll, persist, exit ---
	client1 := connectClient(t, socketPath, "test-slot") // helper that mirrors clientruntime.Run minimally
	scrollPaneTo(t, client1, /*paneIdx*/ 0, /*viewBottomIdx*/ 50)
	persistAndDisconnect(t, client1)

	// File should exist with the scrolled-back position.
	statePath, err := clientruntime.ResolvePath(socketPath, "test-slot")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	state, err := clientruntime.Load(statePath, socketPath)
	if err != nil || state == nil {
		t.Fatalf("expected persisted state, got err=%v state=%v", err, state)
	}
	if len(state.PaneViewports) == 0 {
		t.Fatalf("expected non-empty PaneViewports")
	}

	// --- Client #2: fresh runtime, loads file, resumes ---
	client2 := connectClient(t, socketPath, "test-slot") // same slot → loads file
	defer client2.Close()
	gotBottom := pollViewBottomIdx(t, client2, 0, 2*time.Second)
	if gotBottom != 50 {
		t.Errorf("expected viewBottomIdx=50 after resume, got %d", gotBottom)
	}
}
```

(Helper function names may differ; adapt to the harness already used in this file. The test pattern is what matters: scroll → persist → load → assert.)

- [ ] **Step 16.3: Run test to verify it fails (or skips)**

```
go test ./internal/runtime/server/ -run TestIntegration_PersistedClientResumesViewport -v
```
Expected: FAIL (helpers undefined) or compile error. Resolve by either adding the helpers or restructuring against existing helper names.

- [ ] **Step 16.4: Implement helpers if needed**

Implement `connectClient`, `scrollPaneTo`, `persistAndDisconnect`, `pollViewBottomIdx` if they don't exist. Reuse existing patterns from neighboring tests; do not invent new harness shapes.

- [ ] **Step 16.5: Run test to verify it passes**

```
go test ./internal/runtime/server/ -run TestIntegration_PersistedClientResumesViewport -v
```
Expected: PASS.

- [ ] **Step 16.6: Commit**

```bash
git add internal/runtime/server/client_integration_test.go
git commit -m "Integration: persisted client resumes scrolled-back viewport (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 17: `TestIntegration_StaleSessionFallsBackClean`

- [ ] **Step 17.1: Write the failing test**

Append to `client_integration_test.go`:

```go
// TestIntegration_StaleSessionFallsBackClean: a client persists a
// sessionID, the server forgets it (eviction / restart), the client
// reconnects with the stale ID, sees ErrSessionNotFound, wipes the
// file, and reconnects fresh. Subsequent clean exit writes the
// new sessionID.
func TestIntegration_StaleSessionFallsBackClean(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	srv, cleanup := newIntegrationServer(t)
	defer cleanup()
	socketPath := srv.SocketPath()
	statePath, _ := clientruntime.ResolvePath(socketPath, "test-slot")

	// Seed a state file with a fabricated, never-existed sessionID.
	stale := clientruntime.ClientState{
		SocketPath:    socketPath,
		SessionID:     [16]byte{0xde, 0xad, 0xbe, 0xef},
		LastSequence:  99,
		WrittenAt:     time.Now().UTC(),
		PaneViewports: nil,
	}
	if err := clientruntime.Save(statePath, &stale); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	// Connect a client; it should observe the rejection and wipe.
	client := connectClient(t, socketPath, "test-slot")
	defer client.Close()
	// Allow connect + resume + retry to complete.
	time.Sleep(200 * time.Millisecond)

	// File should now hold the freshly-allocated sessionID, not 0xdeadbeef.
	got, err := clientruntime.Load(statePath, socketPath)
	if err != nil {
		t.Fatalf("Load post-recovery: %v", err)
	}
	if got == nil {
		t.Fatalf("expected fresh state file after recovery, got nil")
	}
	if got.SessionID == stale.SessionID {
		t.Errorf("expected new sessionID after stale rejection, got the stale one")
	}
}
```

- [ ] **Step 17.2: Run test**

```
go test ./internal/runtime/server/ -run TestIntegration_StaleSessionFallsBackClean -v
```
Expected: PASS (or fail-and-fix until it passes).

- [ ] **Step 17.3: Commit**

```bash
git add internal/runtime/server/client_integration_test.go
git commit -m "Integration: stale sessionID falls back cleanly (#199, Plan D)

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>"
```

---

### Task 18: `TestIntegration_MultiClientIsolation`

- [ ] **Step 18.1: Write the test**

```go
// TestIntegration_MultiClientIsolation: two clients with different
// --client-name against the same socket maintain independent state.
// Scrolling client A does not affect client B's persisted file.
func TestIntegration_MultiClientIsolation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	srv, cleanup := newIntegrationServer(t)
	defer cleanup()
	socketPath := srv.SocketPath()

	a := connectClient(t, socketPath, "left")
	b := connectClient(t, socketPath, "right")

	scrollPaneTo(t, a, 0, 100)
	scrollPaneTo(t, b, 0, 200)

	persistAndDisconnect(t, a)
	persistAndDisconnect(t, b)

	pathA, _ := clientruntime.ResolvePath(socketPath, "left")
	pathB, _ := clientruntime.ResolvePath(socketPath, "right")

	stateA, _ := clientruntime.Load(pathA, socketPath)
	stateB, _ := clientruntime.Load(pathB, socketPath)
	if stateA == nil || stateB == nil {
		t.Fatalf("expected both files: A=%v B=%v", stateA, stateB)
	}
	if stateA.SessionID == stateB.SessionID {
		t.Errorf("expected distinct sessionIDs, got identical")
	}
	// Each holds its own scrolled-back position.
	if stateA.PaneViewports[0].ViewBottomIdx != 100 {
		t.Errorf("client A: viewBottomIdx %d != 100", stateA.PaneViewports[0].ViewBottomIdx)
	}
	if stateB.PaneViewports[0].ViewBottomIdx != 200 {
		t.Errorf("client B: viewBottomIdx %d != 200", stateB.PaneViewports[0].ViewBottomIdx)
	}
}
```

- [ ] **Step 18.2: Run test**

```
go test ./internal/runtime/server/ -run TestIntegration_MultiClientIsolation -v
```
Expected: PASS.

- [ ] **Step 18.3: Commit**

```bash
git add internal/runtime/server/client_integration_test.go
git commit -m "Integration: --client-name isolates persistence per client (#199, Plan D)

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
- [ ] `go test -race ./...` passes (or pre-existing failures only).
- [ ] No new files outside the planned File Structure table.
- [ ] Spec sections covered: storage location & schema (Tasks 4–7), write cadence (Task 8), read cadence (Tasks 10–11), multi-client (Tasks 5, 14, 15, 18), error handling (Tasks 7, 11), server carry-forwards (Tasks 1–3), testing (Tasks 16–18). All ✓.

---

## Out of scope (deferred)

- Server-side cross-daemon-restart persistence → **Plan D2** (`docs/superpowers/plans/2026-04-25-issue-199-plan-d2-server-viewport-persistence.md`).
- Session recovery / discovery → **Plan F** (`docs/superpowers/plans/2026-04-26-issue-199-plan-f-session-recovery.md`). Plan D's schema is recovery-compatible without changes.
