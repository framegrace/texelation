# Issue #199 — Plan B: Viewport-Aware Resume Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `MsgResumeRequest` with per-pane viewport state so a client reconnecting within the same server lifetime lands at the exact scrolled-back position it left, honoring a precise wrap-segment anchor and autoFollow flag per pane.

**Architecture:** Plan A introduced clip-on-emit at the publisher driven by `ClientViewports`, populated from `MsgViewportUpdate`. This plan adds a per-pane payload on `MsgResumeRequest` carrying `{ViewBottomIdx, WrapSegmentIdx, AutoFollow, AltScreen, Rows, Cols}`. On resume, the server (a) walks the sparse store upward from `(ViewBottomIdx, WrapSegmentIdx)` to compute a `(viewAnchor, viewAnchorOffset)` pair, (b) re-seats the pane's `sparse.ViewWindow` at that position with the supplied `autoFollow`, and (c) re-renders. The publisher then clips on the first post-resume publish. Missing-anchor (ViewBottomIdx below `Store.OldestRetained()`) snaps to oldest with `autoFollow=false` (Policy A). Alt-screen entries in the payload skip scroll resolution. Client populates the payload from its live per-pane `paneViewport` trackers at resume-send time; no disk persistence (that's Plan D).

**Tech Stack:** Go 1.24.3, `protocol/` wire framing, `apps/texelterm/parser/sparse` for view window math, `internal/runtime/server` for session/resume dispatch, `internal/runtime/client` for tracker plumbing.

---

## File Structure

### Protocol (new + modified)
- **Modify** `protocol/messages.go` — extend `ResumeRequest` struct, `EncodeResumeRequest`, `DecodeResumeRequest` with a `PaneViewports []PaneViewportState` tail.
- **Create** `protocol/pane_viewport_state.go` — `PaneViewportState` struct + its own encoder/decoder helpers (kept separate from `messages.go` to mirror the file-per-message-family convention Plan A used for `viewport.go` / `buffer_delta.go`).
- **Create** `protocol/pane_viewport_state_test.go` — unit round-trip + validate tests.
- **Modify** `protocol/resume_test.go` (if present; else create new file `protocol/resume_viewport_test.go`) — `ResumeRequest` round-trip with empty and populated `PaneViewports`.

### Sparse store (new walk helper + view restore)
- **Modify** `apps/texelterm/parser/sparse/view_window.go` — add `WalkUpwardFromBottom(s, viewBottom, wrapSeg, rows, width, reflowOff) (anchor int64, offset int, policy WalkPolicy)` and `SetAutoFollow(bool)` setter.
- **Create** `apps/texelterm/parser/sparse/walk_upward_test.go` — helper unit tests (in-range, tail of chain, mid-chain, missing-anchor, alt-screen caller short-circuits upstream).
- **Modify** `apps/texelterm/parser/sparse/terminal.go` — add `RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)`.

### VTerm / main-screen bridge
- **Modify** `apps/texelterm/parser/main_screen.go` — add `RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)` to the `MainScreen` interface.
- **Modify** `apps/texelterm/parser/vterm_main_screen.go` — satisfy the new interface method by delegating to `v.mainScreen.RestoreViewport(...)`.

### Texelterm pane app
- **Modify** `apps/texelterm/term.go` — add `RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)` delegating to `a.vterm.RestoreViewport`.

### Texel core (pane-by-id lookup)
- **Modify** `texel/runtime_interfaces.go` — add `ViewportRestorer` interface:
  ```go
  type ViewportRestorer interface {
      RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)
  }
  ```
- **Modify** `texel/desktop_engine_core.go` — add `RestorePaneViewport(paneID [16]byte, viewBottom int64, wrapSeg uint16, autoFollow bool) bool` that walks workspace trees (or reuses `forEachPane`), finds the pane with matching `ID()`, type-asserts its app to `ViewportRestorer`, and calls it. Returns true on success.

### Server resume dispatch
- **Modify** `internal/runtime/server/connection_handler.go` — `MsgResumeRequest` case: for each `PaneViewportState` in the request (ignoring `AltScreen=true` entries), call `desktop.RestorePaneViewport(...)` BEFORE `provider.Snapshot()`. For entries, also seed `session.viewports` so the first publish has a `ClientViewport` already. Missing-anchor handling lives inside `WalkUpwardFromBottom`; the handler doesn't special-case it.
- **Modify** `internal/runtime/server/client_viewport.go` — add `ApplyResume(ps []protocol.PaneViewportState)` that seeds entries.

### Client resume send
- **Modify** `internal/runtime/client/viewport_tracker.go` — add `WrapSegmentIdx uint16` to `paneViewport` + `paneViewportCopy`; have `snapshotDirty` carry it; add `SetBottomWrapSegment(id, idx)` setter (called by renderer when rendering completes); flushFrame encodes it on `MsgViewportUpdate`.
- **Modify** `internal/runtime/client/app.go` — before `simple.RequestResume(...)`, gather `[]protocol.PaneViewportState` from `state.viewports` and pass to a new helper.
- **Modify** `client/simple_client.go` — `RequestResume` gains an extra parameter `paneViewports []protocol.PaneViewportState`.
- **Modify** `client/cmd/texel-headless/main.go` — update the one call site with an empty slice.

### Renderer wiring for wrap-segment
- **Modify** `internal/runtime/client/render.go` (or equivalent — find the per-frame render path that reads `paneCache` + row globalIdxs) — after each pane render, count the consecutive bottom rows that share the bottom row's globalIdx; if N such rows, call `state.viewports.SetBottomWrapSegment(paneID, uint16(N-1))`.

### Tests
- **Create** `internal/runtime/server/resume_viewport_integration_test.go` — three integration tests: valid anchor in-store, missing anchor snap-to-oldest, alt-screen skip.

---

## Task 1 — Wire: `PaneViewportState` type + `ResumeRequest` extension

**Files:**
- Create: `protocol/pane_viewport_state.go`
- Create: `protocol/pane_viewport_state_test.go`
- Modify: `protocol/messages.go` (ResumeRequest struct + Encode/Decode)
- Create (if absent): `protocol/resume_viewport_test.go`

### 1a. Write `PaneViewportState` struct + encoder/decoder test

- [ ] **Step 1: Write the failing test**

Create `protocol/pane_viewport_state_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"errors"
	"testing"
)

func TestPaneViewportState_RoundTrip(t *testing.T) {
	in := PaneViewportState{
		PaneID:         [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		AltScreen:      false,
		ViewBottomIdx:  123456,
		WrapSegmentIdx: 3,
		AutoFollow:     true,
		ViewportRows:   24,
		ViewportCols:   80,
	}
	raw, err := EncodePaneViewportState(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, consumed, err := DecodePaneViewportState(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(raw) {
		t.Fatalf("consumed=%d len=%d", consumed, len(raw))
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestPaneViewportState_AltScreen(t *testing.T) {
	// AltScreen=true: scroll fields are payload-valid but ignored semantically.
	// The wire must still round-trip them so the encoder is lossless.
	in := PaneViewportState{
		PaneID:         [16]byte{0xaa},
		AltScreen:      true,
		ViewBottomIdx:  0,
		WrapSegmentIdx: 0,
		AutoFollow:     false,
		ViewportRows:   10,
		ViewportCols:   40,
	}
	raw, err := EncodePaneViewportState(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, _, err := DecodePaneViewportState(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
}

func TestPaneViewportState_ZeroDim(t *testing.T) {
	bad := PaneViewportState{
		PaneID:       [16]byte{1},
		ViewportRows: 0,
		ViewportCols: 80,
	}
	if _, err := EncodePaneViewportState(bad); !errors.Is(err, ErrPaneViewportZeroDim) {
		t.Fatalf("want ErrPaneViewportZeroDim, got %v", err)
	}
}

func TestPaneViewportState_ShortPayload(t *testing.T) {
	short := make([]byte, 10)
	if _, _, err := DecodePaneViewportState(short); !errors.Is(err, ErrPayloadShort) {
		t.Fatalf("want ErrPayloadShort, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./protocol/ -run TestPaneViewportState -v`
Expected: FAIL with `undefined: PaneViewportState` / `undefined: EncodePaneViewportState`.

- [ ] **Step 3: Implement the struct + encoder/decoder**

Create `protocol/pane_viewport_state.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/pane_viewport_state.go
// Summary: PaneViewportState wire type + encoder/decoder for viewport-aware resume.
// Usage: Carried inside ResumeRequest.PaneViewports; server uses it to re-seat
//
//	each pane's sparse.ViewWindow before the first post-resume publish.

package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// ErrPaneViewportZeroDim is returned when ViewportRows or ViewportCols is zero.
var ErrPaneViewportZeroDim = errors.New("protocol: pane viewport has zero dimension")

// PaneViewportState is the per-pane resume payload in MsgResumeRequest. It
// describes where each pane was scrolled to at disconnect so the server can
// land the pane's ViewWindow exactly there on reconnect.
//
// AltScreen=true causes the server to skip scroll resolution; the pane's own
// alt-screen buffer is sent verbatim on first paint.
//
// AutoFollow=true causes the server to clamp ViewBottomIdx to Store.Max() at
// first-paint time, so the client lands at the live edge. Scroll fields are
// still required on the wire so the server can fall back if AutoFollow flips
// while the payload is in flight (cheap defensive default, not correctness).
type PaneViewportState struct {
	PaneID         [16]byte
	AltScreen      bool
	AutoFollow     bool
	ViewBottomIdx  int64
	WrapSegmentIdx uint16
	ViewportRows   uint16
	ViewportCols   uint16
}

// EncodedPaneViewportStateSize is the fixed wire size per entry:
// 16 (paneID) + 1 (bools) + 8 (ViewBottomIdx) + 2 (WrapSegmentIdx) + 2 (Rows) + 2 (Cols) = 31.
const EncodedPaneViewportStateSize = 31

func (p PaneViewportState) Validate() error {
	if p.ViewportRows == 0 || p.ViewportCols == 0 {
		return ErrPaneViewportZeroDim
	}
	return nil
}

func EncodePaneViewportState(p PaneViewportState) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(make([]byte, 0, EncodedPaneViewportStateSize))
	if _, err := buf.Write(p.PaneID[:]); err != nil {
		return nil, err
	}
	var bools uint8
	if p.AltScreen {
		bools |= 1 << 0
	}
	if p.AutoFollow {
		bools |= 1 << 1
	}
	if err := buf.WriteByte(bools); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.ViewBottomIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.WrapSegmentIdx); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.ViewportRows); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, p.ViewportCols); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodePaneViewportState reads one entry from b and returns it along with the
// number of bytes consumed. Used by DecodeResumeRequest for the list tail.
func DecodePaneViewportState(b []byte) (PaneViewportState, int, error) {
	var p PaneViewportState
	if len(b) < EncodedPaneViewportStateSize {
		return p, 0, ErrPayloadShort
	}
	copy(p.PaneID[:], b[:16])
	bools := b[16]
	p.AltScreen = bools&(1<<0) != 0
	p.AutoFollow = bools&(1<<1) != 0
	p.ViewBottomIdx = int64(binary.LittleEndian.Uint64(b[17:25]))
	p.WrapSegmentIdx = binary.LittleEndian.Uint16(b[25:27])
	p.ViewportRows = binary.LittleEndian.Uint16(b[27:29])
	p.ViewportCols = binary.LittleEndian.Uint16(b[29:31])
	if p.ViewportRows == 0 || p.ViewportCols == 0 {
		return PaneViewportState{}, 0, ErrPaneViewportZeroDim
	}
	return p, EncodedPaneViewportStateSize, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./protocol/ -run TestPaneViewportState -v`
Expected: PASS (all four subtests).

- [ ] **Step 5: Commit**

```bash
git add protocol/pane_viewport_state.go protocol/pane_viewport_state_test.go
git commit -m "protocol: PaneViewportState wire type (#199 Plan B)"
```

### 1b. Extend `ResumeRequest` with `PaneViewports` tail

- [ ] **Step 1: Write the failing test**

Create `protocol/resume_viewport_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"testing"
)

func TestResumeRequest_RoundTripEmptyViewports(t *testing.T) {
	in := ResumeRequest{
		SessionID:    [16]byte{1, 2, 3},
		LastSequence: 42,
	}
	raw, err := EncodeResumeRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeResumeRequest(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.SessionID != in.SessionID || out.LastSequence != in.LastSequence {
		t.Fatalf("core fields mismatch: got %+v want %+v", out, in)
	}
	if len(out.PaneViewports) != 0 {
		t.Fatalf("PaneViewports: got %d want 0", len(out.PaneViewports))
	}
}

func TestResumeRequest_RoundTripWithViewports(t *testing.T) {
	in := ResumeRequest{
		SessionID:    [16]byte{9},
		LastSequence: 100,
		PaneViewports: []PaneViewportState{
			{PaneID: [16]byte{1}, ViewBottomIdx: 500, WrapSegmentIdx: 2, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
			{PaneID: [16]byte{2}, AltScreen: true, ViewportRows: 24, ViewportCols: 80},
		},
	}
	raw, err := EncodeResumeRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeResumeRequest(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.PaneViewports) != len(in.PaneViewports) {
		t.Fatalf("PaneViewports len: got %d want %d", len(out.PaneViewports), len(in.PaneViewports))
	}
	for i := range in.PaneViewports {
		if out.PaneViewports[i] != in.PaneViewports[i] {
			t.Fatalf("PaneViewports[%d]: got %+v want %+v", i, out.PaneViewports[i], in.PaneViewports[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./protocol/ -run TestResumeRequest_ -v`
Expected: FAIL — `ResumeRequest` has no `PaneViewports` field.

- [ ] **Step 3: Extend the struct + encoder/decoder**

Modify `protocol/messages.go`:

Replace the existing `ResumeRequest` struct (lines 48-52):

```go
// ResumeRequest asks the server to replay buffered diffs from a sequence point.
// PaneViewports (Plan B, #199) carries per-pane viewport state so the server
// can land each pane's ViewWindow at the exact position the client was
// viewing at disconnect. Empty slice = fresh-connect semantics.
type ResumeRequest struct {
	SessionID     [16]byte
	LastSequence  uint64
	PaneViewports []PaneViewportState
}
```

Replace `EncodeResumeRequest` (lines 340-347) with:

```go
func EncodeResumeRequest(r ResumeRequest) ([]byte, error) {
	if len(r.PaneViewports) > 0xFFFF {
		return nil, ErrBufferTooLarge
	}
	buf := bytes.NewBuffer(make([]byte, 0, 26+len(r.PaneViewports)*EncodedPaneViewportStateSize))
	buf.Write(r.SessionID[:])
	if err := binary.Write(buf, binary.LittleEndian, r.LastSequence); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.PaneViewports))); err != nil {
		return nil, err
	}
	for _, pv := range r.PaneViewports {
		raw, err := EncodePaneViewportState(pv)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	return buf.Bytes(), nil
}
```

If `ErrBufferTooLarge` is not already imported/exported from this file, it is defined in `protocol/buffer_delta.go` (same package), so no import change is needed.

Replace `DecodeResumeRequest` (lines 349-357) with:

```go
func DecodeResumeRequest(b []byte) (ResumeRequest, error) {
	var r ResumeRequest
	if len(b) < 26 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	r.LastSequence = binary.LittleEndian.Uint64(b[16:24])
	count := binary.LittleEndian.Uint16(b[24:26])
	offset := 26
	if count > 0 {
		r.PaneViewports = make([]PaneViewportState, 0, count)
		for i := uint16(0); i < count; i++ {
			pv, consumed, err := DecodePaneViewportState(b[offset:])
			if err != nil {
				return ResumeRequest{}, err
			}
			r.PaneViewports = append(r.PaneViewports, pv)
			offset += consumed
		}
	}
	if offset != len(b) {
		return ResumeRequest{}, ErrExtraBytes
	}
	return r, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./protocol/ -run TestResumeRequest_ -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Run existing resume tests to catch regressions**

Run: `go test ./protocol/ -run Resume -v`
Expected: All resume-related tests PASS.

- [ ] **Step 6: Commit**

```bash
git add protocol/messages.go protocol/resume_viewport_test.go
git commit -m "protocol: extend ResumeRequest with PaneViewports (#199 Plan B)"
```

---

## Task 2 — Sparse: `WalkUpwardFromBottom` helper + `SetAutoFollow`

**Files:**
- Modify: `apps/texelterm/parser/sparse/view_window.go`
- Create: `apps/texelterm/parser/sparse/walk_upward_test.go`

### 2a. `WalkPolicy` sentinel + `WalkUpwardFromBottom` helper

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/parser/sparse/walk_upward_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func makeRow(n int, ch rune) []parser.Cell {
	out := make([]parser.Cell, n)
	for i := range out {
		out[i] = parser.Cell{Ch: ch}
	}
	return out
}

// Populate a store with N unwrapped rows of content at globalIdx [0, N-1].
// Each row is NoWrap-set so reflow leaves them 1:1.
func makeFlatStore(t *testing.T, n int, width int) *Store {
	t.Helper()
	s := NewStore(width)
	for i := 0; i < n; i++ {
		s.SetLineWithNoWrap(int64(i), makeRow(width, 'a'+rune(i%26)), true)
	}
	return s
}

func TestWalkUpwardFromBottom_FlatRows_TailAligned(t *testing.T) {
	// 100 unwrapped rows, viewport 24x80. Ask for viewBottom=99, wrapSeg=0
	// (flat rows have a single sub-row, so wrapSeg is always 0). The walk
	// should land viewAnchor=99-23=76, offset=0, policy=PolicyAnchorInStore.
	s := makeFlatStore(t, 100, 80)
	anchor, offset, policy := WalkUpwardFromBottom(s, 99, 0, 24, 80, false)
	if policy != WalkPolicyAnchorInStore {
		t.Fatalf("policy: got %v want WalkPolicyAnchorInStore", policy)
	}
	if anchor != 76 {
		t.Fatalf("anchor: got %d want 76", anchor)
	}
	if offset != 0 {
		t.Fatalf("offset: got %d want 0", offset)
	}
}

func TestWalkUpwardFromBottom_MissingAnchor_SnapsToOldest(t *testing.T) {
	// 100 unwrapped rows but oldestRetained=50 (simulated by EvictBelow).
	// Ask viewBottom=10 (below oldest). Expect policy=MissingAnchor + anchor
	// = oldest retained (50).
	s := makeFlatStore(t, 100, 80)
	s.EvictBelow(50)
	anchor, _, policy := WalkUpwardFromBottom(s, 10, 0, 24, 80, false)
	if policy != WalkPolicyMissingAnchor {
		t.Fatalf("policy: got %v want WalkPolicyMissingAnchor", policy)
	}
	if anchor != 50 {
		t.Fatalf("anchor: got %d want 50 (oldest retained)", anchor)
	}
}

func TestWalkUpwardFromBottom_WrappedChain_TailSubRow(t *testing.T) {
	// Build a store with: 20 flat rows at [0,19], then one wrapped chain at
	// globalIdx 20 that reflows to 3 sub-rows at width 80, then 5 flat rows
	// at [21,25]. Viewport height 5 at width 80; viewBottom=21, wrapSeg=0
	// means the bottom is the flat row 21. Walking back 5 rows lands anchor
	// at 20 (chain head) with offset = chain-rows - (rows-1) = 3 - 4 = ... wait
	// For this test: 5 sub-rows total visible = [chain sub 1, chain sub 2,
	// row 21-4, ...]; we want (anchor=20, offset=2) since the top is the
	// third sub-row (index 2) of the chain.
	s := NewStore(80)
	for i := 0; i < 20; i++ {
		s.SetLineWithNoWrap(int64(i), makeRow(80, 'a'), true)
	}
	// A chain at gid=20 that reflows to 3 sub-rows at width 80: length 240
	// total, split into three rows of 80 with Wrapped on boundaries.
	wrapped := make([]parser.Cell, 240)
	for i := range wrapped {
		wrapped[i] = parser.Cell{Ch: 'W'}
	}
	// SetLine stores the chain as 3 rows at gid=20,21,22 with Wrapped flag
	// on sub-rows' last cell; then subsequent flat rows start at gid=23.
	s.SetLine(20, wrapped)
	for i := 0; i < 3; i++ {
		s.SetLineWithNoWrap(int64(23+i), makeRow(80, 'z'), true)
	}
	// viewBottom=25 (last flat row), wrapSeg=0. Viewport height=5 at width=80.
	// Display rows from top: ..., row 23 (flat), row 24 (flat), row 25 (flat)
	// means bottom 3 rows are flats. Above those: 2 sub-rows of the chain
	// (sub-rows 2 and 1). So top anchor = chain head (gid=20), offset=1.
	anchor, offset, policy := WalkUpwardFromBottom(s, 25, 0, 5, 80, false)
	if policy != WalkPolicyAnchorInStore {
		t.Fatalf("policy: got %v want WalkPolicyAnchorInStore", policy)
	}
	if anchor != 20 {
		t.Fatalf("anchor: got %d want 20 (chain head)", anchor)
	}
	if offset != 1 {
		t.Fatalf("offset: got %d want 1", offset)
	}
}
```

> **Note on test preconditions:** `Store` does not currently expose `EvictBelow`. Step 2 of this sub-task adds it. If the helper method already exists under a different name, adapt the test (but do NOT skip the missing-anchor case).

- [ ] **Step 2: Add `EvictBelow` helper to Store if missing**

Check if `Store.EvictBelow(gid int64)` exists:

Run: `grep -n 'func (s \*Store) EvictBelow\|EvictBelow\b' apps/texelterm/parser/sparse/store.go`

If no match, add it below `OldestRetained()`:

```go
// EvictBelow discards all rows with globalIdx < gid and raises OldestRetained
// to gid. Intended for test simulation of page-store eviction; production
// eviction flows through AdaptivePersistence.
func (s *Store) EvictBelow(gid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for g := range s.rows {
		if g < gid {
			delete(s.rows, g)
		}
	}
	if gid > s.oldestRetained {
		s.oldestRetained = gid
	}
}
```

(Field names — `s.mu`, `s.rows`, `s.oldestRetained` — must match the existing struct. Inspect `apps/texelterm/parser/sparse/store.go:1-60` to confirm the exact private fields before writing the body, and adapt accordingly.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWalkUpwardFromBottom -v`
Expected: FAIL with `undefined: WalkUpwardFromBottom` / `undefined: WalkPolicyAnchorInStore`.

- [ ] **Step 4: Implement the helper**

Append to `apps/texelterm/parser/sparse/view_window.go`:

```go
// WalkPolicy describes the outcome of WalkUpwardFromBottom.
type WalkPolicy uint8

const (
	// WalkPolicyAnchorInStore means viewBottom was resolvable in the store;
	// (anchor, offset) position the view as requested.
	WalkPolicyAnchorInStore WalkPolicy = iota
	// WalkPolicyMissingAnchor means viewBottom < Store.OldestRetained();
	// anchor is set to OldestRetained and the caller MUST force
	// autoFollow=false to honor the user's scroll-back intent (Policy A).
	WalkPolicyMissingAnchor
)

// WalkUpwardFromBottom walks chains upward starting from the wrapSeg-th sub-row
// of the chain whose head is at viewBottom (or the chain containing viewBottom
// if viewBottom is itself in the middle of a chain). It accumulates `rows`
// reflowed sub-rows at display width `width`, respecting NoWrap and the
// global-reflow-off override. Returns the (chain-head anchor, sub-row offset)
// pair to pass to ViewWindow.SetViewAnchor.
//
// Missing anchor: if viewBottom < Store.OldestRetained(), policy is
// WalkPolicyMissingAnchor and (anchor, offset) = (OldestRetained(), 0). The
// caller must force autoFollow=false before applying.
//
// Note: sparse chains are stored one-row-per-globalIdx with Wrapped flags on
// boundary cells. A "chain" starts at a globalIdx whose preceding row does
// NOT end in Wrapped and ends at the globalIdx whose own last cell lacks
// Wrapped. Reflow combines all chain rows and resplits at `width`.
func WalkUpwardFromBottom(s *Store, viewBottom int64, wrapSeg uint16, rows, width int, reflowOff bool) (int64, int, WalkPolicy) {
	if viewBottom < s.OldestRetained() {
		return s.OldestRetained(), 0, WalkPolicyMissingAnchor
	}
	if rows <= 0 {
		return viewBottom, int(wrapSeg), WalkPolicyAnchorInStore
	}
	maxSteps := 4 * rows
	if maxSteps < 4 {
		maxSteps = 4
	}
	// Start: find the chain that contains viewBottom, place ourselves at its
	// wrapSeg-th sub-row, and walk upward `rows-1` more sub-rows.
	chainStart := findChainStart(s, viewBottom, maxSteps)
	end, nowrap := walkChain(s, chainStart, maxSteps)
	if reflowOff {
		nowrap = true
	}

	// How many sub-rows does the bottom chain contribute, and where inside
	// it does wrapSeg land?
	bottomSubRows := chainReflowedRowCount(s, chainStart, end, width, nowrap)
	// Clamp wrapSeg to the last valid sub-row of the chain.
	ws := int(wrapSeg)
	if ws >= bottomSubRows {
		ws = bottomSubRows - 1
	}
	if ws < 0 {
		ws = 0
	}

	// Sub-rows consumed from the bottom chain toward the walk up:
	// the bottom sub-row plus `ws` sub-rows above it = ws+1 rows accounted
	// for so far (including the bottom display row itself).
	remaining := rows - (ws + 1)
	anchor := chainStart
	offset := 0
	if remaining <= 0 {
		// Viewport fits entirely inside the bottom chain.
		// offset = ws - (rows-1).
		offset = ws - (rows - 1)
		if offset < 0 {
			offset = 0
		}
		return anchor, offset, WalkPolicyAnchorInStore
	}

	// Walk chains upward one at a time until we've consumed `rows` sub-rows
	// or hit globalIdx 0.
	for remaining > 0 && anchor > 0 {
		prevGI := anchor - 1
		prevStart := findChainStart(s, prevGI, maxSteps)
		prevEnd, prevNoWrap := walkChain(s, prevStart, maxSteps)
		if reflowOff {
			prevNoWrap = true
		}
		prevRows := chainReflowedRowCount(s, prevStart, prevEnd, width, prevNoWrap)
		if prevRows >= remaining {
			anchor = prevStart
			offset = prevRows - remaining
			return anchor, offset, WalkPolicyAnchorInStore
		}
		remaining -= prevRows
		anchor = prevStart
	}
	// Ran off the top of history; pin to 0/oldest-retained.
	if anchor < s.OldestRetained() {
		anchor = s.OldestRetained()
	}
	offset = 0
	return anchor, offset, WalkPolicyAnchorInStore
}
```

The helpers `findChainStart`, `walkChain`, `chainReflowedRowCount` already exist in `view_window.go` (used by `ScrollUpRows` — search the file to confirm names and signatures before writing the body above).

- [ ] **Step 5: Run test**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestWalkUpwardFromBottom -v`
Expected: PASS (three subtests).

- [ ] **Step 6: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/walk_upward_test.go apps/texelterm/parser/sparse/store.go
git commit -m "sparse: WalkUpwardFromBottom helper + EvictBelow for tests (#199 Plan B)"
```

### 2b. `ViewWindow` setters: `SetAutoFollow(bool)` + `SetViewBottom(int64)`

Context: `ViewWindow` maintains three interdependent pieces of state — `viewAnchor`/`viewAnchorOffset` (drives `Render`), `viewBottom` (drives `VisibleRange` + autoFollow tracking), and `autoFollow`. Normal scroll flows (`ScrollUpRows` / `ScrollDownRows`) update all three in lockstep. The resume path must do the same, so we need thin setters for each.

- [ ] **Step 1: Write the failing test**

Append to `apps/texelterm/parser/sparse/walk_upward_test.go`:

```go
func TestViewWindow_SetAutoFollow(t *testing.T) {
	v := NewViewWindow(80, 24)
	if !v.IsFollowing() {
		t.Fatalf("IsFollowing default: got false want true")
	}
	v.SetAutoFollow(false)
	if v.IsFollowing() {
		t.Fatalf("after SetAutoFollow(false): got true want false")
	}
	v.SetAutoFollow(true)
	if !v.IsFollowing() {
		t.Fatalf("after SetAutoFollow(true): got false want true")
	}
}

func TestViewWindow_SetViewBottom(t *testing.T) {
	v := NewViewWindow(80, 24)
	v.SetViewBottom(100)
	top, bottom := v.VisibleRange()
	if bottom != 100 {
		t.Fatalf("VisibleRange bottom: got %d want 100", bottom)
	}
	if top != 100-24+1 {
		t.Fatalf("VisibleRange top: got %d want %d", top, 100-24+1)
	}
}
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestViewWindow_SetAutoFollow -v`
Expected: FAIL with `undefined: (*ViewWindow).SetAutoFollow`.

- [ ] **Step 3: Implement setters**

Append to `apps/texelterm/parser/sparse/view_window.go`:

```go
// SetAutoFollow explicitly sets the autoFollow flag. Used on resume to
// honor the client's saved autoFollow state (and to force off after a
// missing-anchor snap-to-oldest).
func (v *ViewWindow) SetAutoFollow(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.autoFollow = enabled
}

// SetViewBottom explicitly positions viewBottom (the globalIdx of the
// bottom display row — for a wrapped chain, the chain-head gid the bottom
// row belongs to). Used by the resume path alongside SetViewAnchor /
// SetAutoFollow to bring all three internal pieces of state into a
// consistent post-restore configuration. Clamped to the minimum valid
// viewBottom (height-1) to avoid negative scroll semantics.
func (v *ViewWindow) SetViewBottom(viewBottom int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.viewBottom = viewBottom
	v.clampViewBottom()
}
```

- [ ] **Step 4: Run test**

Run: `go test ./apps/texelterm/parser/sparse/ -run 'TestViewWindow_SetAutoFollow|TestViewWindow_SetViewBottom' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/view_window.go apps/texelterm/parser/sparse/walk_upward_test.go
git commit -m "sparse: ViewWindow.SetAutoFollow setter (#199 Plan B)"
```

---

## Task 3 — `Terminal.RestoreViewport`, `MainScreen` interface, VTerm, Term

### 3a. Terminal.RestoreViewport

**Files:**
- Modify: `apps/texelterm/parser/sparse/terminal.go`
- Modify: `apps/texelterm/parser/sparse/walk_upward_test.go`

- [ ] **Step 1: Write the failing test**

Append to `apps/texelterm/parser/sparse/walk_upward_test.go`:

```go
func TestTerminal_RestoreViewport_AnchorInStore(t *testing.T) {
	term := NewTerminal(80, 24)
	// Fill 100 rows so scrollback exists.
	for i := 0; i < 100; i++ {
		for _, r := range "line" {
			term.WriteCell(parser.Cell{Ch: r})
		}
		term.CarriageReturn()
		term.Newline()
	}
	if !term.IsFollowing() {
		t.Fatalf("pre: IsFollowing want true")
	}
	// Restore to viewBottom=50 (well inside the store), autoFollow=false.
	term.RestoreViewport(50, 0, false)
	if term.IsFollowing() {
		t.Fatalf("post: IsFollowing want false")
	}
	// VisibleRange bottom should reflect the requested viewBottom (within
	// one row — exact translation depends on walk semantics).
	_, bottom := term.VisibleRange()
	// The ViewWindow stores viewBottom in its own coordinates. Accept the
	// bottom falling within [viewBottom - height + 1, viewBottom + 1] due to
	// how SetViewAnchor derives viewBottom inside Terminal.RestoreViewport.
	if bottom < 49 || bottom > 51 {
		t.Fatalf("VisibleRange bottom: got %d want ~50", bottom)
	}
}

func TestTerminal_RestoreViewport_MissingAnchor_SnapsToOldest(t *testing.T) {
	// Missing-anchor (ViewBottomIdx < OldestRetained) only applies when
	// autoFollow=false. With autoFollow=true the server clamps to Max() via
	// OnWriteBottomChanged and missing-anchor is N/A.
	term := NewTerminal(80, 24)
	for i := 0; i < 10; i++ {
		for _, r := range "line" {
			term.WriteCell(parser.Cell{Ch: r})
		}
		term.CarriageReturn()
		term.Newline()
	}
	term.Store().EvictBelow(5)
	// Ask for viewBottom=2 (below retention) with autoFollow=false. Walk
	// helper returns MissingAnchor; anchor is snapped to oldest (5); the
	// view must stay non-following (policy A preserves scroll-back intent).
	term.RestoreViewport(2, 0, false)
	if term.IsFollowing() {
		t.Fatalf("IsFollowing want false after missing-anchor resume")
	}
	// We intentionally do NOT assert on VisibleRange here: with only 10
	// rows of content and height=24, clampViewBottom pins viewBottom at
	// height-1=23, which is greater than ContentEnd()=9. The meaningful
	// invariant for missing-anchor is that autoFollow is off (preserving
	// the user's scroll-back intent) — not a specific globalIdx.
}

func TestTerminal_RestoreViewport_AutoFollowClampsToMax(t *testing.T) {
	// autoFollow=true takes precedence over the scroll fields; the view
	// should track the live edge regardless of the supplied viewBottom.
	term := NewTerminal(80, 24)
	for i := 0; i < 100; i++ {
		for _, r := range "line" {
			term.WriteCell(parser.Cell{Ch: r})
		}
		term.CarriageReturn()
		term.Newline()
	}
	term.RestoreViewport(10 /* deliberately stale */, 0, true)
	if !term.IsFollowing() {
		t.Fatalf("IsFollowing want true after autoFollow=true resume")
	}
}
```

> **Note:** This test calls `term.Store()` which may not exist. Confirm — if absent, Task 3a Step 3 adds a `Store()` getter along with `RestoreViewport`.

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal_RestoreViewport -v`
Expected: FAIL with `undefined: (*Terminal).RestoreViewport` (and possibly `Store`).

- [ ] **Step 3: Implement `RestoreViewport` + `Store()` getter on Terminal**

Append to `apps/texelterm/parser/sparse/terminal.go`:

```go
// Store returns the sparse store backing this terminal. Exposed for
// test helpers and the publisher's viewport-aware resume path.
func (t *Terminal) Store() *Store { return t.store }

// RestoreViewport positions the view window to reproduce the client's
// last-known scrollback position. viewBottom is a chain-head globalIdx; the
// chain may reflow to multiple sub-rows and wrapSeg selects which.
//
// AutoFollow=true takes precedence over the scroll fields: the ViewWindow
// is set to autoFollow and will naturally clamp to Store.Max() via
// OnWriteBottomChanged. This matches the spec's "clamp ViewBottomIdx to
// Store.Max() at current server-side geometry" rule for AutoFollow resumes.
//
// AutoFollow=false honors viewBottom + wrapSeg exactly via
// WalkUpwardFromBottom, unless the anchor is below retention
// (missing-anchor policy forces autoFollow=false — already the caller's
// request, but documented for clarity).
//
// Must be called BEFORE the next Render so the subsequent publish emits
// rows inside the resumed viewport.
func (t *Terminal) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	if autoFollow {
		t.view.SetAutoFollow(true)
		return
	}
	width := t.view.Width()
	height := t.view.Height()
	// ViewWindow does not currently expose a GlobalReflowOff getter; the
	// resume path does not need to toggle it (reflow state is view-owned
	// and stable across resume). Pass false.
	anchor, offset, _ := WalkUpwardFromBottom(t.store, viewBottom, wrapSeg, height, width, false)
	t.view.SetViewAnchor(anchor, offset)
	// viewBottom setter clamps to height-1, which handles both in-store
	// (keeps caller's value) and missing-anchor (caller's stale viewBottom
	// clamps to the same "near the top of available content" position the
	// snapped anchor renders to). No need to branch on policy.
	t.view.SetViewBottom(viewBottom)
	t.view.SetAutoFollow(false)
}
```

If `t.view.Width()` / `t.view.Height()` aren't exported from `*ViewWindow`, add thin accessors (`Width()` / `Height()` look present already from grep). Confirm first.

- [ ] **Step 4: Run test**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestTerminal_RestoreViewport -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/terminal.go apps/texelterm/parser/sparse/walk_upward_test.go
git commit -m "sparse: Terminal.RestoreViewport + Store() getter (#199 Plan B)"
```

### 3b. MainScreen interface + VTerm delegate

**Files:**
- Modify: `apps/texelterm/parser/main_screen.go`
- Modify: `apps/texelterm/parser/vterm_main_screen.go`

- [ ] **Step 1: Extend the MainScreen interface**

Edit `apps/texelterm/parser/main_screen.go` — add at the end of the interface (just before the closing brace, after `RewindWriteTop`):

```go
	// RestoreViewport re-seats the main screen's view window to reproduce
	// the client's saved scrollback anchor. Caller (publisher resume path)
	// guarantees this is called BEFORE the next Render.
	// Missing-anchor policy (viewBottom below retention) is applied
	// internally and forces autoFollow=false regardless of the caller's
	// request.
	RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)
```

- [ ] **Step 2: Delegate from VTerm**

In `apps/texelterm/parser/vterm_main_screen.go`, add:

```go
// RestoreViewport is the VTerm-level entry point for Plan B viewport-aware
// resume. No-op when mainScreen is nil (parser-only tests) or when currently
// in alt-screen mode (alt-screen preserves its own state; callers MUST NOT
// invoke this for alt-screen panes — see PaneViewportState.AltScreen).
func (v *VTerm) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.RestoreViewport(viewBottom, wrapSeg, autoFollow)
}
```

Place alongside the other VTerm-level MainScreen pass-throughs. If the file does not already contain any such delegate, add it after the existing `IsFollowing()` delegate near line 617.

- [ ] **Step 3: Compile check**

Run: `go build ./apps/texelterm/...`
Expected: compiles clean. If `sparse.Terminal` does not satisfy the interface, the compiler surfaces the missing method — it should already exist from Task 3a.

- [ ] **Step 4: Commit**

```bash
git add apps/texelterm/parser/main_screen.go apps/texelterm/parser/vterm_main_screen.go
git commit -m "parser: RestoreViewport on MainScreen interface (#199 Plan B)"
```

### 3c. Term app (`apps/texelterm/term.go`) exposes RestoreViewport

**Files:**
- Modify: `apps/texelterm/term.go`
- Modify: `apps/texelterm/term_resume_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `apps/texelterm/term_resume_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelterm

import (
	"testing"
)

func TestTerm_RestoreViewport_ForwardsToVTerm(t *testing.T) {
	// Exercises the Term -> VTerm -> MainScreen -> Terminal.RestoreViewport
	// chain end-to-end. We construct a Term with a minimal environment
	// (no PTY — we never Run it), write a few lines through the VTerm
	// directly, then call Term.RestoreViewport and verify IsFollowing() / AtLiveEdge().
	a := NewTestTerm(80, 24) // helper to construct Term without spawning PTY
	if a.vterm == nil {
		t.Skip("NewTestTerm did not produce a VTerm; revise helper")
	}
	// Write 100 lines.
	for i := 0; i < 100; i++ {
		a.vterm.Write([]byte("line\r\n"))
	}
	if !a.vterm.AtLiveEdge() {
		t.Fatalf("pre: AtLiveEdge want true")
	}
	a.RestoreViewport(50, 0, false)
	if a.vterm.AtLiveEdge() {
		t.Fatalf("post: AtLiveEdge want false after restore to scrollback")
	}
}
```

> **Helper required:** `NewTestTerm(w, h)` either already exists in the test file of `apps/texelterm/` or must be added in a sibling `apps/texelterm/term_test_helpers.go`. If `Term` has a constructor that doesn't start a PTY (common pattern: `NewForTest` or similar), use that. Grep first:
> ```bash
> grep -rn 'func NewTerm\|func.*(w,\s*h\s*int)\s*\*Term' apps/texelterm/
> ```
> If no suitable helper exists, create a minimal one that builds the `Term` struct with a width/height and a fresh `VTerm` but leaves `storage`/`cmd` nil. Mark the helper `_test.go`-only so it doesn't bleed into production.

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./apps/texelterm/ -run TestTerm_RestoreViewport -v`
Expected: FAIL with `undefined: (*Term).RestoreViewport`.

- [ ] **Step 3: Implement delegate on Term**

In `apps/texelterm/term.go`, add (near the other VTerm-passthroughs such as `InAltScreen`):

```go
// RestoreViewport re-seats the terminal's main-screen view window to the
// globalIdx + wrap-segment pair the client was viewing before disconnect.
// No-op when the pane is in alt-screen; callers (publisher resume path) are
// expected to check PaneViewportState.AltScreen and skip alt panes.
func (a *Term) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	if a.vterm == nil {
		return
	}
	if a.vterm.InAltScreen() {
		return
	}
	a.vterm.RestoreViewport(viewBottom, wrapSeg, autoFollow)
}
```

- [ ] **Step 4: Run test**

Run: `go test ./apps/texelterm/ -run TestTerm_RestoreViewport -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/term.go apps/texelterm/term_resume_test.go apps/texelterm/term_test_helpers.go
git commit -m "texelterm: Term.RestoreViewport delegate (#199 Plan B)"
```

---

## Task 4 — `ViewportRestorer` interface + `DesktopEngine.RestorePaneViewport`

**Files:**
- Modify: `texel/runtime_interfaces.go`
- Modify: `texel/desktop_engine_core.go`
- Create: `texel/restore_pane_viewport_test.go`

### 4a. Interface + DesktopEngine lookup

- [ ] **Step 1: Write the failing test**

Create `texel/restore_pane_viewport_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texel

import (
	"testing"
)

// fakeViewportRestorerApp satisfies both App (minimally) and ViewportRestorer.
// Records the last RestoreViewport call for assertion.
type fakeViewportRestorerApp struct {
	App // unused methods — Term-like noop implementation
	lastViewBottom int64
	lastWrapSeg    uint16
	lastAutoFollow bool
	called         bool
}

func (f *fakeViewportRestorerApp) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	f.called = true
	f.lastViewBottom = viewBottom
	f.lastWrapSeg = wrapSeg
	f.lastAutoFollow = autoFollow
}

func TestDesktopEngine_RestorePaneViewport_ForwardsToApp(t *testing.T) {
	// Construct a DesktopEngine with a single pane hosting fakeViewportRestorerApp.
	// Harness details depend on existing DesktopEngine test helpers; reuse them.
	de := newTestDesktopWithOnePane(t) // helper: returns (de, paneID, app)
	id, app := de.firstPaneIDAndApp()

	// Replace app with a fake that records RestoreViewport calls.
	fake := &fakeViewportRestorerApp{App: app}
	de.swapPaneApp(id, fake) // helper for test-only app swap

	ok := de.RestorePaneViewport(id, 42, 1, false)
	if !ok {
		t.Fatalf("RestorePaneViewport: want true (pane found and restorer)")
	}
	if !fake.called {
		t.Fatalf("app.RestoreViewport not called")
	}
	if fake.lastViewBottom != 42 || fake.lastWrapSeg != 1 || fake.lastAutoFollow != false {
		t.Fatalf("forwarded args: got (%d,%d,%v) want (42,1,false)",
			fake.lastViewBottom, fake.lastWrapSeg, fake.lastAutoFollow)
	}
}

func TestDesktopEngine_RestorePaneViewport_UnknownPane(t *testing.T) {
	de := newTestDesktopWithOnePane(t)
	var unknown [16]byte
	unknown[0] = 0xff
	if ok := de.RestorePaneViewport(unknown, 0, 0, true); ok {
		t.Fatalf("RestorePaneViewport unknown id: want false")
	}
}

func TestDesktopEngine_RestorePaneViewport_NonRestorerApp(t *testing.T) {
	de := newTestDesktopWithOnePane(t)
	id, _ := de.firstPaneIDAndApp()
	// The default test app doesn't implement ViewportRestorer.
	if ok := de.RestorePaneViewport(id, 0, 0, true); ok {
		t.Fatalf("RestorePaneViewport on non-restorer app: want false")
	}
}
```

> **Helpers required:** `newTestDesktopWithOnePane`, `firstPaneIDAndApp`, `swapPaneApp`. Grep the existing `texel/` tests first:
> ```bash
> grep -rn 'newTestDesktop\|firstPaneIDAndApp\|func Test.*Engine' texel/*_test.go
> ```
> If the harness doesn't exist, build minimal helpers in `texel/desktop_engine_test_helpers.go` (test-only file — use `_test.go` suffix for the helpers too, e.g. `texel/testing_helpers_test.go`) using the same constructor path `capturePaneSnapshot` tests use.

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./texel/ -run TestDesktopEngine_RestorePaneViewport -v`
Expected: FAIL with `undefined: (*DesktopEngine).RestorePaneViewport` / `undefined: ViewportRestorer`.

- [ ] **Step 3: Add interface**

In `texel/runtime_interfaces.go`, add:

```go
// ViewportRestorer is implemented by pane apps (notably texelterm) that
// support viewport-aware resume. Called by DesktopEngine.RestorePaneViewport
// on MsgResumeRequest to re-seat the app's scrollback view before the first
// post-resume render. Apps that don't implement this interface (statusbar,
// launcher, etc.) are skipped — they don't have scrollback.
type ViewportRestorer interface {
	RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)
}
```

- [ ] **Step 4: Implement DesktopEngine.RestorePaneViewport**

In `texel/desktop_engine_core.go`, add near the existing `forEachPane` (line ~814):

```go
// RestorePaneViewport looks up the pane with id and, if its app implements
// ViewportRestorer, forwards the restore call. Returns true on success,
// false if the pane is unknown or its app is not a restorer. Safe to call
// while holding no desktop locks — iterates via forEachPane.
func (d *DesktopEngine) RestorePaneViewport(id [16]byte, viewBottom int64, wrapSeg uint16, autoFollow bool) bool {
	var found bool
	d.forEachPane(func(p *pane) {
		if found {
			return
		}
		if p.ID() != id {
			return
		}
		if p.app == nil {
			return
		}
		restorer, ok := p.app.(ViewportRestorer)
		if !ok {
			return
		}
		restorer.RestoreViewport(viewBottom, wrapSeg, autoFollow)
		found = true
	})
	return found
}
```

- [ ] **Step 5: Run test**

Run: `go test ./texel/ -run TestDesktopEngine_RestorePaneViewport -v`
Expected: PASS (three subtests).

- [ ] **Step 6: Commit**

```bash
git add texel/runtime_interfaces.go texel/desktop_engine_core.go texel/restore_pane_viewport_test.go texel/testing_helpers_test.go
git commit -m "texel: ViewportRestorer interface + DesktopEngine.RestorePaneViewport (#199 Plan B)"
```

---

## Task 5 — Server: handle `PaneViewports` on `MsgResumeRequest`

**Files:**
- Modify: `internal/runtime/server/connection_handler.go`
- Modify: `internal/runtime/server/client_viewport.go`
- Create: `internal/runtime/server/resume_viewport_integration_test.go`

### 5a. `ClientViewports.ApplyResume`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/server/client_viewport_resume_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestClientViewports_ApplyResume(t *testing.T) {
	cv := NewClientViewports()
	states := []protocol.PaneViewportState{
		{PaneID: [16]byte{1}, ViewBottomIdx: 100, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
		{PaneID: [16]byte{2}, AltScreen: true, ViewportRows: 24, ViewportCols: 80},
	}
	cv.ApplyResume(states)
	got1, ok := cv.Get([16]byte{1})
	if !ok {
		t.Fatalf("pane 1 not stored")
	}
	if got1.ViewBottomIdx != 100 || got1.AutoFollow != false || got1.AltScreen {
		t.Fatalf("pane 1: got %+v", got1)
	}
	got2, ok := cv.Get([16]byte{2})
	if !ok {
		t.Fatalf("pane 2 not stored")
	}
	if !got2.AltScreen {
		t.Fatalf("pane 2 AltScreen: got false want true")
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/runtime/server/ -run TestClientViewports_ApplyResume -v`
Expected: FAIL with `undefined: (*ClientViewports).ApplyResume`.

- [ ] **Step 3: Implement**

Append to `internal/runtime/server/client_viewport.go`:

```go
// ApplyResume seeds the viewport map from a ResumeRequest.PaneViewports list.
// ViewTopIdx is derived as ViewBottomIdx - Rows + 1, clamped to 0 for panes
// whose saved bottom is close to the origin; publisher clipping will use this
// for first-paint, after which the client's MsgViewportUpdate (sent via
// flushFrame once render settles) will reconcile exact values.
func (c *ClientViewports) ApplyResume(states []protocol.PaneViewportState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ps := range states {
		top := ps.ViewBottomIdx - int64(ps.ViewportRows) + 1
		if top < 0 {
			top = 0
		}
		c.byPaneID[ps.PaneID] = ClientViewport{
			AltScreen:     ps.AltScreen,
			ViewTopIdx:    top,
			ViewBottomIdx: ps.ViewBottomIdx,
			Rows:          ps.ViewportRows,
			Cols:          ps.ViewportCols,
			AutoFollow:    ps.AutoFollow,
		}
	}
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/runtime/server/ -run TestClientViewports_ApplyResume -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/client_viewport.go internal/runtime/server/client_viewport_resume_test.go
git commit -m "server: ClientViewports.ApplyResume seeds from ResumeRequest (#199 Plan B)"
```

### 5b. Connection handler: call RestorePaneViewport + ApplyResume before snapshot

- [ ] **Step 1: Write the failing integration test**

Create `internal/runtime/server/resume_viewport_integration_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/framegrace/texelation/apps/texelterm"
	"github.com/framegrace/texelation/internal/runtime/server/testutil"
	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

func TestIntegration_ResumeHonorsPaneViewport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Harness pattern mirrors TestIntegration_ClipsAndFetches. Build a
	// desktop with a single texelterm pane, feed 200 lines of output, then
	// simulate a resume with ViewBottomIdx=50.
	h := NewHarness(t) // reuse the Plan A integration harness; name may be NewFetchRangeHarness etc. — adjust.
	defer h.Close()

	paneID := h.FirstPaneID()
	// Feed 200 newline-terminated lines to the texelterm.
	for i := 0; i < 200; i++ {
		h.WriteToPane(paneID, []byte("line\r\n"))
	}
	h.Flush() // wait for the write window to absorb the feed

	// Send a ResumeRequest that asks the server to land at globalIdx=50.
	_, err := h.SendResume(protocol.ResumeRequest{
		SessionID:    h.SessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  50,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	})
	if err != nil {
		t.Fatalf("SendResume: %v", err)
	}

	// The harness should observe a TreeSnapshot + subsequent delta whose
	// RowGlobalIdx per pane falls inside [50-24+1, 50], i.e., centered on
	// globalIdx 50.
	snap := h.WaitForNextDelta(t, paneID, ctx)
	if snap.RowBase < 26 || snap.RowBase > 50 {
		t.Fatalf("RowBase: got %d want ~26 (ViewTopIdx-overscan for viewBottom=50)", snap.RowBase)
	}
}

func TestIntegration_ResumeMissingAnchor_SnapsToOldest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h := NewHarness(t)
	defer h.Close()
	paneID := h.FirstPaneID()
	for i := 0; i < 100; i++ {
		h.WriteToPane(paneID, []byte("line\r\n"))
	}
	h.EvictBelow(paneID, 40)
	h.Flush()

	_, err := h.SendResume(protocol.ResumeRequest{
		SessionID:    h.SessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			// autoFollow=false is load-bearing: missing-anchor policy only
			// applies when the client is explicitly scrolled back. With
			// autoFollow=true the server clamps to Max() and the missing
			// ViewBottomIdx is irrelevant.
			{PaneID: paneID, ViewBottomIdx: 10 /* below retention */, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
		},
	})
	if err != nil {
		t.Fatalf("SendResume: %v", err)
	}
	_ = h.WaitForNextDelta(t, paneID, ctx)
	// Missing-anchor snaps to oldest (40). Exact RowBase is derivation-
	// dependent; the invariant is "server-side pane is not following the
	// live edge", i.e. preserved scrolled-back intent.
	if !h.PaneFollowsFalse(paneID) {
		t.Fatalf("missing-anchor must force autoFollow=false")
	}
}

func TestIntegration_ResumeAltScreen_SkipsScrollResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	h := NewHarness(t)
	defer h.Close()
	paneID := h.FirstPaneID()
	// Put the pane into alt-screen.
	h.WriteToPane(paneID, []byte("\x1b[?1049h"))
	h.Flush()

	// Resume with AltScreen=true, viewBottom=999 — server must ignore scroll
	// fields and not blow up.
	_, err := h.SendResume(protocol.ResumeRequest{
		SessionID:    h.SessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{PaneID: paneID, AltScreen: true, ViewBottomIdx: 999, ViewportRows: 24, ViewportCols: 80},
		},
	})
	if err != nil {
		t.Fatalf("SendResume: %v", err)
	}
	// Subsequent delta carries Flags.AltScreen=1.
	snap := h.WaitForNextDelta(t, paneID, ctx)
	if snap.Flags&protocol.BufferDeltaAltScreen == 0 {
		t.Fatalf("expected alt-screen delta after resume")
	}
}

// Silence unused imports in skeleton. Remove once helpers are wired.
var (
	_ = texelterm.NewTestTerm
	_ = texel.ViewportRestorer(nil)
)
```

> **Harness gaps:** The Plan A integration harness (`internal/runtime/server/harness_test.go` or similar — confirm filename via `grep -rn 'func NewHarness\|NewFetchRangeHarness' internal/runtime/server/*_test.go`) exposes `NewHarness`, `FirstPaneID`, `WriteToPane`, `Flush`, `SendResume`, `WaitForNextDelta`. If `EvictBelow` or `PaneFollowsFalse` don't exist, extend the harness file in THIS task to add them — treat the additions as harness plumbing, not tested surface. Keep the additions small and local to `_test.go` files.

- [ ] **Step 2: Run — verify it fails**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestIntegration_Resume -v`
Expected: FAIL — connection handler doesn't yet apply PaneViewports.

- [ ] **Step 3: Wire the handler**

Edit `internal/runtime/server/connection_handler.go`. Locate the `case protocol.MsgResumeRequest` block (around line 127). Add BEFORE `if provider, ok := c.sink.(SnapshotProvider); ok {`:

```go
		// Apply per-pane viewport state first: re-seat each pane's
		// ViewWindow + seed ClientViewports so the snapshot + first
		// publish use the resumed coordinates instead of live edge.
		if sink, ok := c.sink.(*DesktopSink); ok && sink.Desktop() != nil {
			for _, ps := range request.PaneViewports {
				if !ps.AltScreen {
					sink.Desktop().RestorePaneViewport(ps.PaneID, ps.ViewBottomIdx, ps.WrapSegmentIdx, ps.AutoFollow)
				}
			}
		}
		c.session.viewports.ApplyResume(request.PaneViewports)
```

Inspect the block carefully — the session.viewports field is set via NewSession. Confirm the access path (may need a thin `Session.ApplyResume(states)` wrapper if `.viewports` is not exported). If only `Session.ApplyViewportUpdate(u)` is public, add:

```go
// ApplyResume seeds per-pane viewports from a ResumeRequest payload. Called
// by the connection handler before the first post-resume snapshot so the
// publisher clips correctly on the initial emit.
func (s *Session) ApplyResume(states []protocol.PaneViewportState) {
	s.viewports.ApplyResume(states)
}
```

and call `c.session.ApplyResume(request.PaneViewports)` from the handler.

- [ ] **Step 4: Run**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestIntegration_Resume -v`
Expected: PASS (three subtests).

If the harness doesn't yet have `EvictBelow`/`PaneFollowsFalse`, add them minimally (call `sparse.Terminal.Store().EvictBelow(...)` for the former; add a getter on `DesktopSink` that returns `!Term.IsFollowing()` for the latter).

- [ ] **Step 5: Run full server test package to catch regressions**

Run: `go test ./internal/runtime/server/`
Expected: PASS. Also run `go test -tags=integration ./internal/runtime/server/` — all integration tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/connection_handler.go internal/runtime/server/session.go internal/runtime/server/resume_viewport_integration_test.go
git commit -m "server: resume honors PaneViewports (#199 Plan B)"
```

---

## Task 6 — Client: `WrapSegmentIdx` tracking in `paneViewport`

**Files:**
- Modify: `internal/runtime/client/viewport_tracker.go`
- Create: `internal/runtime/client/viewport_tracker_wrap_seg_test.go`

### 6a. Add field + setter

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/client/viewport_tracker_wrap_seg_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestViewportTrackers_SetBottomWrapSegment(t *testing.T) {
	trackers := newViewportTrackers()
	id := [16]byte{0x11}
	// Pre-populate so the tracker exists (sim post-TreeSnapshot).
	vp := trackers.get(id)
	vp.mu.Lock()
	vp.Rows = 24
	vp.Cols = 80
	vp.ViewBottomIdx = 100
	vp.mu.Unlock()

	trackers.SetBottomWrapSegment(id, 3)

	vp2 := trackers.get(id)
	vp2.mu.Lock()
	got := vp2.WrapSegmentIdx
	vp2.mu.Unlock()
	if got != 3 {
		t.Fatalf("WrapSegmentIdx: got %d want 3", got)
	}
}

func TestViewportTrackers_SetBottomWrapSegment_MissingPane_NoCrash(t *testing.T) {
	trackers := newViewportTrackers()
	trackers.SetBottomWrapSegment([16]byte{0xff}, 5) // pane doesn't exist — must not panic
}

func TestSnapshotEntry_CarriesWrapSegment(t *testing.T) {
	trackers := newViewportTrackers()
	id := [16]byte{0x22}
	vp := trackers.get(id)
	vp.mu.Lock()
	vp.Rows = 24
	vp.Cols = 80
	vp.ViewBottomIdx = 50
	vp.WrapSegmentIdx = 7
	vp.dirty = true
	vp.mu.Unlock()

	entries, _ := trackers.snapshotDirty()
	if len(entries) != 1 {
		t.Fatalf("snapshotDirty: got %d entries want 1", len(entries))
	}
	if entries[0].vp.WrapSegmentIdx != 7 {
		t.Fatalf("WrapSegmentIdx in snapshot: got %d want 7", entries[0].vp.WrapSegmentIdx)
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/runtime/client/ -run TestViewportTrackers_SetBottomWrapSegment -v`
Expected: FAIL — no `WrapSegmentIdx` field / no `SetBottomWrapSegment` method.

- [ ] **Step 3: Add the field + setter + snapshot propagation**

In `internal/runtime/client/viewport_tracker.go`:

Add `WrapSegmentIdx uint16` to `paneViewport` (line ~47, in the struct body) and to `paneViewportCopy` (line ~159). Example locations — keep the ordering consistent with the struct layout in the file.

Extend `snapshotDirty` (around line 110) to include the field in the `paneViewportCopy{}` literal. Same for `clearDirty` (ensure the equality check includes WrapSegmentIdx — else a WrapSegment-only change wouldn't clear dirty properly). Add this to the `if` guard:
```go
vp.WrapSegmentIdx == expected.WrapSegmentIdx &&
```

Add setter:

```go
// SetBottomWrapSegment updates the tracker's WrapSegmentIdx — the sub-row
// index (within the chain at ViewBottomIdx) that occupies the bottommost
// display row. Called by the renderer after each pane render. No-op when
// the pane has no tracker yet.
func (t *viewportTrackers) SetBottomWrapSegment(id [16]byte, idx uint16) {
	t.mu.RLock()
	vp, ok := t.panes[id]
	t.mu.RUnlock()
	if !ok {
		return
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if vp.WrapSegmentIdx == idx {
		return
	}
	vp.WrapSegmentIdx = idx
	vp.dirty = true
}
```

Finally update `flushFrame` (around line 359) to replace the hardcoded `WrapSegmentIdx: 0` with `WrapSegmentIdx: vc.WrapSegmentIdx` (requires `vc` to be a `paneViewportCopy` carrying the field — which it now does from the change above).

- [ ] **Step 4: Run**

Run: `go test ./internal/runtime/client/ -run TestViewportTrackers_SetBottomWrapSegment -v`
Expected: PASS (three subtests).

Also run the existing viewport tracker tests:
Run: `go test ./internal/runtime/client/ -run TestViewport -v`
Expected: PASS (no regressions in dirty/clear/snapshot logic).

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/viewport_tracker.go internal/runtime/client/viewport_tracker_wrap_seg_test.go
git commit -m "client: track WrapSegmentIdx per pane (#199 Plan B)"
```

### 6b. Renderer populates WrapSegmentIdx

**Files:**
- Modify: the client's per-pane render path. Locate with:
  ```bash
  grep -rn 'paneCacheFor\|PaneCache\|RowAt\b' internal/runtime/client/ --include="*.go" | grep -v '_test.go'
  ```
  The per-pane render loop — where each display row is resolved to a `globalIdx` — is the place that knows the bottom-row's chain continuity. Confirm the file (likely `internal/runtime/client/render.go` or embedded in a larger file like `app.go`).

- [ ] **Step 1: Identify the pane-render function**

Run the grep above. The target is a function that iterates `y` over display rows and calls `paneCache.RowAt(...)` or similar. The function must already have access to the list of `(y, globalIdx)` pairs for the visible rows.

- [ ] **Step 2: Write the failing test**

If the render loop is pure (returns or mutates something observable), write a test that drives a known (y, globalIdx) sequence and asserts `SetBottomWrapSegment` is invoked with the right value. Likely pattern: extract a helper `computeBottomWrapSegment(rowGIs []int64) uint16` and unit-test it.

Create `internal/runtime/client/render_wrap_segment_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestComputeBottomWrapSegment_FlatRow(t *testing.T) {
	// Bottom row's gid is unique among the final rows → wrap seg = 0.
	rowGIs := []int64{10, 11, 12, 13}
	got := computeBottomWrapSegment(rowGIs)
	if got != 0 {
		t.Fatalf("got %d want 0", got)
	}
}

func TestComputeBottomWrapSegment_WrappedTail(t *testing.T) {
	// Bottom two rows share gid 20 → wrap seg = 1 (bottom is sub-row 1 of
	// the chain, counting from 0 within the visible portion).
	rowGIs := []int64{10, 11, 20, 20}
	got := computeBottomWrapSegment(rowGIs)
	if got != 1 {
		t.Fatalf("got %d want 1", got)
	}
}

func TestComputeBottomWrapSegment_AllSameGid(t *testing.T) {
	// All four visible rows are sub-rows of one chain → wrap seg = 3.
	rowGIs := []int64{50, 50, 50, 50}
	got := computeBottomWrapSegment(rowGIs)
	if got != 3 {
		t.Fatalf("got %d want 3", got)
	}
}

func TestComputeBottomWrapSegment_Empty(t *testing.T) {
	if got := computeBottomWrapSegment(nil); got != 0 {
		t.Fatalf("nil: got %d want 0", got)
	}
	if got := computeBottomWrapSegment([]int64{}); got != 0 {
		t.Fatalf("empty: got %d want 0", got)
	}
}

func TestComputeBottomWrapSegment_InvalidGid(t *testing.T) {
	// Bottom row gid = -1 (blank padding / border) → wrap seg = 0.
	rowGIs := []int64{10, 11, 12, -1}
	if got := computeBottomWrapSegment(rowGIs); got != 0 {
		t.Fatalf("got %d want 0", got)
	}
}
```

- [ ] **Step 3: Run — verify it fails**

Run: `go test ./internal/runtime/client/ -run TestComputeBottomWrapSegment -v`
Expected: FAIL — `undefined: computeBottomWrapSegment`.

- [ ] **Step 4: Implement the helper**

Add to the same file that owns the pane-render loop (or a new `internal/runtime/client/wrap_segment.go`):

```go
// computeBottomWrapSegment scans rowGIs upward from the bottom and counts
// consecutive entries sharing the same globalIdx as the bottom row. Returns
// that count minus 1 (the sub-row index of the bottom within the visible
// portion of the chain). Returns 0 for empty input or when the bottom row's
// globalIdx is -1 (blank / border / non-terminal).
//
// Interpretation: if the chain extends ABOVE the viewport (only partially
// visible), this value is the sub-row index within the VISIBLE portion, not
// within the chain. The server reconciles this on restore by walking
// backward `Rows` sub-rows starting from (ViewBottomIdx, WrapSegmentIdx).
// That lands the viewport consistently even when the chain extends above.
func computeBottomWrapSegment(rowGIs []int64) uint16 {
	if len(rowGIs) == 0 {
		return 0
	}
	bottom := rowGIs[len(rowGIs)-1]
	if bottom < 0 {
		return 0
	}
	count := 1
	for i := len(rowGIs) - 2; i >= 0; i-- {
		if rowGIs[i] != bottom {
			break
		}
		count++
	}
	return uint16(count - 1)
}
```

- [ ] **Step 5: Call the helper from the pane render path**

Find the render function that iterates visible rows. After the loop (or wherever the per-row `rowGI` slice is in scope), call:

```go
state.viewports.SetBottomWrapSegment(paneID, computeBottomWrapSegment(rowGIs))
```

If the pane-render path does not currently track `rowGIs`, build it inline: for each display row y, get its globalIdx from the cache lookup chain (the same lookup that decides which cell to draw). Store in a `[]int64` of length `rows` and pass to the helper.

- [ ] **Step 6: Run**

Run: `go test ./internal/runtime/client/ -run TestComputeBottomWrapSegment -v`
Expected: PASS.

Also run `go build ./...` to ensure the render wiring compiles.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/client/
git commit -m "client: populate WrapSegmentIdx from render rowGIs (#199 Plan B)"
```

---

## Task 7 — Client: send `PaneViewports` in `RequestResume`

**Files:**
- Modify: `client/simple_client.go`
- Modify: `internal/runtime/client/app.go`
- Modify: `client/cmd/texel-headless/main.go`
- Create: `client/simple_client_resume_test.go`

### 7a. Extend `RequestResume` signature

- [ ] **Step 1: Write the failing test**

Create `client/simple_client_resume_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
	"bytes"
	"net"
	"testing"

	"github.com/framegrace/texelation/protocol"
)

// A minimal net.Conn fake backed by a bytes.Buffer for outbound traffic.
// Read returns io.EOF after the handshake exchange finishes; we only use it
// to assert the encoded ResumeRequest body.
type captureConn struct {
	net.Conn
	out bytes.Buffer
}

func (c *captureConn) Write(p []byte) (int, error) {
	return c.out.Write(p)
}

func (c *captureConn) Read(p []byte) (int, error) {
	// Return a pre-canned ResumeData response so RequestResume returns.
	// Specifics don't matter — we only assert the write side.
	// For test simplicity, swallow the read by returning EOF.
	return 0, net.ErrClosed
}

func TestRequestResume_EncodesPaneViewports(t *testing.T) {
	// Build a client, invoke the new form of RequestResume with one pane
	// viewport, and parse the outbound bytes to confirm the payload was
	// the expected ResumeRequest.
	c := &SimpleClient{} // existing constructor pattern; adjust if needed
	conn := &captureConn{}
	sessionID := [16]byte{1, 2, 3}
	viewports := []protocol.PaneViewportState{
		{PaneID: [16]byte{9}, ViewBottomIdx: 42, AutoFollow: false, ViewportRows: 24, ViewportCols: 80},
	}
	_, _, _ = c.RequestResume(conn, sessionID, 7, viewports)

	// Outbound buffer contains the header + payload. Find the payload:
	// protocol.ReadMessage-style framing is 12+ bytes of header, then
	// payload. For brevity, decode by matching the length prefix.
	// (Actual decode: use protocol.ReadMessage against a bytes.Reader.)
	body := conn.out.Bytes()
	if len(body) == 0 {
		t.Fatalf("no bytes written")
	}
	// Parse: reuse protocol.ReadMessage with a bytes.Reader wrapping body.
	r := bytes.NewReader(body)
	hdr, payload, err := protocol.ReadMessage(r)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if hdr.Type != protocol.MsgResumeRequest {
		t.Fatalf("msg type: got %v want MsgResumeRequest", hdr.Type)
	}
	req, err := protocol.DecodeResumeRequest(payload)
	if err != nil {
		t.Fatalf("DecodeResumeRequest: %v", err)
	}
	if req.LastSequence != 7 {
		t.Fatalf("LastSequence: got %d want 7", req.LastSequence)
	}
	if len(req.PaneViewports) != 1 {
		t.Fatalf("PaneViewports len: got %d want 1", len(req.PaneViewports))
	}
	if req.PaneViewports[0].ViewBottomIdx != 42 {
		t.Fatalf("ViewBottomIdx: got %d want 42", req.PaneViewports[0].ViewBottomIdx)
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./client/ -run TestRequestResume_EncodesPaneViewports -v`
Expected: FAIL — signature mismatch (current RequestResume takes only 3 args).

- [ ] **Step 3: Extend `RequestResume`**

In `client/simple_client.go` replace `RequestResume`:

```go
// RequestResume sends a RESUME_REQUEST and returns the server response
// header/payload. paneViewports carries per-pane scrollback state so the
// server can re-seat each pane's ViewWindow at the client's saved position
// (#199 Plan B). Pass nil or an empty slice for fresh-connect semantics.
func (c *SimpleClient) RequestResume(conn net.Conn, sessionID [16]byte, sequence uint64, paneViewports []protocol.PaneViewportState) (protocol.Header, []byte, error) {
	req := protocol.ResumeRequest{SessionID: sessionID, LastSequence: sequence, PaneViewports: paneViewports}
	payload, err := protocol.EncodeResumeRequest(req)
	if err != nil {
		return protocol.Header{}, nil, err
	}
	if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgResumeRequest, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
		return protocol.Header{}, nil, err
	}
	return protocol.ReadMessage(conn)
}
```

- [ ] **Step 4: Fix up call sites**

`internal/runtime/client/app.go` line 107 and `client/cmd/texel-headless/main.go` line 78 — both need the new argument. At both sites, pass `nil` for now; Task 7b replaces app.go with a real aggregation.

Make the edits:
- `internal/runtime/client/app.go:107`: `simple.RequestResume(conn, sessionID, lastSequence, nil)`
- `client/cmd/texel-headless/main.go:78`: `simple.RequestResume(conn, sessionID, *lastSeq, nil)`

- [ ] **Step 5: Run**

Run: `go test ./client/ -run TestRequestResume_EncodesPaneViewports -v`
Expected: PASS.

Run: `go build ./...`
Expected: compiles.

- [ ] **Step 6: Commit**

```bash
git add client/simple_client.go client/simple_client_resume_test.go internal/runtime/client/app.go client/cmd/texel-headless/main.go
git commit -m "client: RequestResume takes PaneViewports (#199 Plan B)"
```

### 7b. `app.go` aggregates viewports from trackers

**Files:**
- Modify: `internal/runtime/client/app.go`
- Modify: `internal/runtime/client/viewport_tracker.go` (add exported snapshot-all)
- Create: `internal/runtime/client/viewport_tracker_aggregate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/client/viewport_tracker_aggregate_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import "testing"

func TestViewportTrackers_SnapshotAll(t *testing.T) {
	trackers := newViewportTrackers()
	id1 := [16]byte{1}
	id2 := [16]byte{2}
	vp1 := trackers.get(id1)
	vp1.mu.Lock()
	vp1.Rows = 24
	vp1.Cols = 80
	vp1.ViewBottomIdx = 100
	vp1.AutoFollow = false
	vp1.WrapSegmentIdx = 2
	vp1.mu.Unlock()
	vp2 := trackers.get(id2)
	vp2.mu.Lock()
	vp2.Rows = 10
	vp2.Cols = 40
	vp2.AltScreen = true
	vp2.mu.Unlock()

	got := trackers.snapshotAll()
	if len(got) != 2 {
		t.Fatalf("snapshotAll len: got %d want 2", len(got))
	}
	// Order isn't guaranteed; index by PaneID.
	byID := make(map[[16]byte]paneViewportCopy, 2)
	for _, e := range got {
		byID[e.id] = e.vp
	}
	if byID[id1].ViewBottomIdx != 100 || byID[id1].WrapSegmentIdx != 2 {
		t.Fatalf("pane 1: got %+v", byID[id1])
	}
	if !byID[id2].AltScreen {
		t.Fatalf("pane 2 AltScreen: got false")
	}
}

func TestViewportTrackers_SnapshotAll_SkipsZeroDim(t *testing.T) {
	trackers := newViewportTrackers()
	trackers.get([16]byte{1}) // zero Rows/Cols — not yet initialised
	got := trackers.snapshotAll()
	if len(got) != 0 {
		t.Fatalf("snapshotAll len: got %d want 0 (zero-dim entries skipped)", len(got))
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/runtime/client/ -run TestViewportTrackers_SnapshotAll -v`
Expected: FAIL — `undefined: (*viewportTrackers).snapshotAll`.

- [ ] **Step 3: Implement**

Append to `internal/runtime/client/viewport_tracker.go`:

```go
// snapshotAll returns a shallow copy of every tracked pane's viewport state,
// regardless of dirty flag. Used on resume-send to seed the outgoing
// PaneViewports list. Entries with zero dims (pane not yet initialised) are
// skipped — they carry no useful resume hint.
func (t *viewportTrackers) snapshotAll() []snapshotEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]snapshotEntry, 0, len(t.panes))
	for id, vp := range t.panes {
		vp.mu.Lock()
		if vp.Rows == 0 || vp.Cols == 0 {
			vp.mu.Unlock()
			continue
		}
		out = append(out, snapshotEntry{
			id: id,
			vp: paneViewportCopy{
				AltScreen:      vp.AltScreen,
				ViewTopIdx:     vp.ViewTopIdx,
				ViewBottomIdx:  vp.ViewBottomIdx,
				Rows:           vp.Rows,
				Cols:           vp.Cols,
				AutoFollow:     vp.AutoFollow,
				WrapSegmentIdx: vp.WrapSegmentIdx,
			},
		})
		vp.mu.Unlock()
	}
	return out
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/runtime/client/ -run TestViewportTrackers_SnapshotAll -v`
Expected: PASS.

- [ ] **Step 5: Wire into app.go**

In `internal/runtime/client/app.go`, find the resume path (line 106-112). Replace the `nil` argument with an aggregation call:

```go
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

Ensure `protocol` is in scope (it already is via existing imports; confirm).

- [ ] **Step 6: Run everything**

Run: `go test ./internal/runtime/client/`
Expected: PASS.

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/client/viewport_tracker.go internal/runtime/client/viewport_tracker_aggregate_test.go internal/runtime/client/app.go
git commit -m "client: aggregate PaneViewports from trackers on resume (#199 Plan B)"
```

---

## Task 8 — End-to-end integration test: disconnect + reconnect

**Files:**
- Create: `internal/runtime/server/resume_e2e_integration_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// End-to-end: start a harness, drive the pane to a known scrollback
// position, "disconnect" (close the memconn), then reconnect with a
// PaneViewportState matching the last-seen position, and verify the
// snapshot + first deltas land at the expected globalIdx range.
func TestIntegration_E2E_ResumeRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h := NewHarness(t)
	defer h.Close()
	paneID := h.FirstPaneID()

	// Feed 150 lines; drive the pane's view back to globalIdx=60.
	for i := 0; i < 150; i++ {
		h.WriteToPane(paneID, []byte("line\r\n"))
	}
	h.ScrollPaneTo(paneID, 60 /* viewBottom */)
	h.Flush()

	// Simulate the client "seeing" that position — record the ViewBottomIdx.
	// (Harness should expose the server-side ViewWindow's current VisibleRange.)
	_, bottomBefore := h.PaneVisibleRange(paneID)
	if bottomBefore == 0 {
		t.Fatalf("precondition: bottom = 0, expected nonzero after scroll")
	}

	// Simulate disconnect.
	h.Disconnect()

	// Reconnect: send ResumeRequest with the captured position.
	_, err := h.SendResume(protocol.ResumeRequest{
		SessionID:    h.SessionID(),
		LastSequence: 0,
		PaneViewports: []protocol.PaneViewportState{
			{
				PaneID:         paneID,
				AltScreen:      false,
				AutoFollow:     false,
				ViewBottomIdx:  bottomBefore,
				WrapSegmentIdx: 0,
				ViewportRows:   24,
				ViewportCols:   80,
			},
		},
	})
	if err != nil {
		t.Fatalf("SendResume: %v", err)
	}

	// Observe: the first post-resume delta's globalIdx range contains
	// bottomBefore.
	snap := h.WaitForNextDelta(t, paneID, ctx)
	_, hi := rangeOfDelta(snap)
	if hi < bottomBefore-1 || hi > bottomBefore+int64(24) /*overscan*/ {
		t.Fatalf("post-resume delta hi gid: got %d want ~%d (ViewBottomIdx + overscan)", hi, bottomBefore)
	}
}

func rangeOfDelta(d protocol.BufferDelta) (lo, hi int64) {
	lo = d.RowBase
	if len(d.Rows) == 0 {
		return lo, lo
	}
	max := int64(0)
	for _, r := range d.Rows {
		if int64(r.Row) > max {
			max = int64(r.Row)
		}
	}
	hi = d.RowBase + max
	return lo, hi
}
```

> **Harness extensions:** `ScrollPaneTo`, `PaneVisibleRange`, `Disconnect` may not exist. Add them to the integration harness in the same commit. They should be thin wrappers: `ScrollPaneTo` calls `Term.ScrollUp/ScrollDown` (or `texel.DesktopEngine.ScrollActivePane`) enough times to hit the target; `PaneVisibleRange` reads from the pane's texelterm's `VTerm.VisibleRange()`; `Disconnect` closes the memconn and reopens a fresh one retaining the session ID.

- [ ] **Step 2: Run — verify it fails or validate after harness work**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestIntegration_E2E_ResumeRoundTrip -v`
Expected: FAIL initially (missing harness methods).

- [ ] **Step 3: Implement harness extensions**

Locate the existing harness file (Plan A's — look for `NewHarness` or similar in `internal/runtime/server/*_test.go`). Extend it with the three helpers. Keep additions small.

- [ ] **Step 4: Run — confirm pass**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestIntegration_E2E_ResumeRoundTrip -v`
Expected: PASS.

Also: `go test ./... && go test -tags=integration ./internal/runtime/server/` — full pass.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/resume_e2e_integration_test.go internal/runtime/server/harness_test.go
git commit -m "tests: end-to-end viewport-aware resume (#199 Plan B)"
```

---

## Task 9 — Lint, fmt, final regression run, push, open PR

- [ ] **Step 1: Format + lint**

Run: `make fmt && make lint`
Expected: no diff / no vet errors.

- [ ] **Step 2: Full regression test**

Run: `make test && go test -tags=integration ./internal/runtime/server/`
Expected: all pass.

- [ ] **Step 3: Push**

```bash
git push -u origin feature/issue-199-plan-b-viewport-resume
```

- [ ] **Step 4: Open PR via `gh`**

```bash
gh pr create --title "Viewport-aware resume (#199, Plan B)" --body "$(cat <<'EOF'
## Summary
- Extends `MsgResumeRequest` with `PaneViewports []PaneViewportState`.
- Server re-seats each pane's `sparse.ViewWindow` on resume via new `WalkUpwardFromBottom` helper + `Terminal.RestoreViewport`; missing anchor (viewBottom below retention) snaps to oldest with autoFollow=false (Policy A).
- Client tracks `WrapSegmentIdx` per pane and sends `PaneViewports` in `RequestResume`. Alt-screen entries skip scroll resolution.
- Integration coverage: valid anchor, missing anchor, alt-screen, end-to-end round-trip.

## Spec
docs/superpowers/specs/2026-04-20-issue-199-viewport-only-rendering-design.md — sub-problem 2 / sequencing step 5.

## Plan
docs/superpowers/plans/2026-04-22-issue-199-plan-b-viewport-aware-resume.md

## Test plan
- [x] `go test ./protocol/` — wire round-trip.
- [x] `go test ./apps/texelterm/parser/sparse/` — walk helper + Terminal restore.
- [x] `go test ./texel/` — DesktopEngine.RestorePaneViewport.
- [x] `go test ./internal/runtime/server/` (unit + integration) — resume handler + clipping.
- [x] `go test ./internal/runtime/client/` — tracker + aggregation.
- [x] `go test ./client/` — simple_client wire.

## Scope exclusions (explicit — Plans D / E)
- No disk persistence of viewport state across server restart (Plan D).
- No statusbar "fetch pending" indicator (Plan E).
- No server-side selection migration (Plan C).

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 5: Update progress memory**

Update `/home/marc/.claude/projects/-home-marc-projects-texel-texelation/memory/project_issue199_progress.md`:
- Mark Plan B "**MERGED YYYY-MM-DD (PR #<N>)**" once merged (not at PR-open time).
- Bump "active plan" to Plan C.
- Add a one-paragraph `## What Plan B shipped` block mirroring Plan A's.

---

## Summary of touched files (for reviewer orientation)

- **Protocol wire:** `protocol/pane_viewport_state.go` (new), `protocol/messages.go` (resume), `protocol/pane_viewport_state_test.go`, `protocol/resume_viewport_test.go`.
- **Sparse viewport math:** `apps/texelterm/parser/sparse/view_window.go` (walk helper + SetAutoFollow), `apps/texelterm/parser/sparse/terminal.go` (RestoreViewport + Store getter), `apps/texelterm/parser/sparse/store.go` (EvictBelow, test-gated), sparse tests.
- **VTerm / Term:** `apps/texelterm/parser/main_screen.go` (interface), `apps/texelterm/parser/vterm_main_screen.go` (delegate), `apps/texelterm/term.go` (delegate + alt-screen guard), `apps/texelterm/term_resume_test.go`.
- **Texel core:** `texel/runtime_interfaces.go` (ViewportRestorer), `texel/desktop_engine_core.go` (RestorePaneViewport), `texel/restore_pane_viewport_test.go`.
- **Server:** `internal/runtime/server/client_viewport.go` (ApplyResume), `internal/runtime/server/session.go` (optional thin wrapper), `internal/runtime/server/connection_handler.go` (MsgResumeRequest dispatch), integration test files.
- **Client:** `internal/runtime/client/viewport_tracker.go` (WrapSegmentIdx field, setter, snapshotAll), `internal/runtime/client/app.go` (aggregation), `internal/runtime/client/render*.go` (computeBottomWrapSegment call site), `client/simple_client.go` (new arg), `client/cmd/texel-headless/main.go` (new arg).

## Self-review checklist (run before calling Plan B done)

1. **Spec coverage — sub-problem 2:**
   - [x] ResumeRequest extended with PaneViewports → Task 1.
   - [x] First-paint logic: AltScreen skip → Task 5b (handler filters `ps.AltScreen` and skips `RestorePaneViewport`). AutoFollow=true clamp to `Store.Max()` → Task 3a's `Terminal.RestoreViewport` skips the walk and sets `autoFollow=true`; `ViewWindow.OnWriteBottomChanged` naturally tracks the live edge.
   - [x] Honor ViewBottomIdx exactly → Task 2a.
   - [x] Missing-anchor snap-to-oldest + force autoFollow=false → Task 2a + Task 3a.
   - [x] Fresh-connect (no SessionID) unchanged → Task 1 (empty PaneViewports accepted).
2. **Spec coverage — alt-screen:**
   - [x] Resume with AltScreen=true skips scroll resolution → Task 5b.
3. **Placeholder scan:** no `TODO`, `TBD`, or "similar to Task N" references. Every step shows the code it writes.
4. **Type consistency:**
   - `PaneViewportState{PaneID, AltScreen, AutoFollow, ViewBottomIdx, WrapSegmentIdx, ViewportRows, ViewportCols}` used consistently across protocol + server + client.
   - `WalkPolicy{AnchorInStore, MissingAnchor}` used in sparse + referenced in Terminal.
   - `ViewportRestorer{RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)}` — same signature at every layer (Term, VTerm, MainScreen, Terminal, interface).
5. **Commits:** one per sub-task; frequent (10+ commits across the plan). Matches Plan A's cadence.
6. **Scope discipline:** No cross-restart persistence (Plan D), no selection changes (Plan C), no statusbar work (Plan E). Confirmed in PR body.
