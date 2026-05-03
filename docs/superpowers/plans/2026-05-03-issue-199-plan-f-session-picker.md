# Issue #199 Plan F.1 — Stored-Session Recovery Picker — Implementation Plan (v2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a client-side stored-session recovery picker so users with lost client state can discover and rejoin sessions the daemon persisted to disk.

**Architecture:** Bumps protocol v4 → v5 with six new messages (list, recover, rename, delete, fetch-thumbnail, op-response) and two wire types (SessionSummary, LiveSummary). Server-side persisted-session map (Plan D2) is exposed via new manager helpers and connection handlers; lifecycle-only thumbnail capture writes PNG sidecars next to each session JSON. The renderer that produces those PNGs is the same `texelui/graphics/textrender` pipeline the client uses for user-initiated screenshots — extracted into a shared `internal/thumbnail/` primitive that both ends import. Server-side composition adapter assembles the workspace-wide cell grid from the publisher's per-pane buffers + tree layout. Client-side picker UI lives in `cmd/texelation/boot/`, reusing the splash's tcell screen so handoff to the existing connect path is flicker-free.

**Tech Stack:** Go 1.24, tcell/v2 for picker UI, `texelui/graphics/textrender` for thumbnail rendering, `texelui/widgets` Image upload for Kitty graphics, existing `internal/persistence/atomicjson` for atomic writes.

**Spec:** `docs/superpowers/specs/2026-05-03-issue-199-plan-f-session-picker-design.md`

**Reality adjustments from the spec (locked in here):**
- Protocol uses strict version equality (`protocol/protocol.go:178`). v4 ↔ v5 cross-talk is impossible by design — `ErrUnsupportedVer` rejects mismatched headers. Plan ships v5 as a hard cutover; the spec's "v4 fallback UX" risk does not materialize because old clients see a connect error and cannot reach the picker code path. Documented but not implemented as graceful fallback.
- The wire-level layout type is `protocol.TreeNodeSnapshot` (already exists in `protocol/messages.go`), not `TreeNodeCapture`. Tasks reference `TreeNodeSnapshot`.
- `StoredSession` JSON shape gains a Layout field as an additive change. No `SchemaVersion` bump (existing files unmarshal cleanly with Layout=nil; old daemons reading new files ignore the unknown JSON field).

**v2 revisions vs v1 (incorporates review findings):**
- **New `internal/thumbnail/` package** (Task 9) shared by server lifecycle capture and client user-screenshot. Eliminates duplicate textrender wiring; client `screenshot.go` is refactored in Task 19 to use it.
- **New server-side composition adapter** (Task 10): `DesktopSink.RenderSessionThumbnail` walks the publisher's `prevBuffers` + tree layout to build a workspace cell grid, then calls the shared primitive. Was missing in v1; the original Task 10 left this as "wire up later."
- **New `MsgSessionOpResponse`** message type (Tasks 1, 4): replaces v1's hack of reusing `MsgListSessionsResponse` as a generic ack. Eliminates the response-type ambiguity flagged in review.
- **Connection struct gains `manager *Manager`** field — wired in Task 13 (renumbered) which is the first task that needs it. Affects every existing test that constructs a connection (~16 files).
- **Bug fixes baked in:** `DecodeLiveSummary` consumed-bytes off-by-16 (Task 2); `StoredSummaries` RLock-during-stat (Task 6); `DeleteStored` TOCTOU + bad guard (Task 7); Task 11 `ShutdownSessions` shown as full code, not prose; ASCII renderer's vertical-split test column + n-way split handling (Task 15); Picker `mu sync.Mutex` field declared up-front (Task 16); `maybeFetchThumbnail` reads under lock + defer-clears `pending` on error (Task 17); error surfacing in picker on Recover/Rename/Delete/RefreshCatalog failures (Task 16); thumbnail size cap + dimension cap on the fetch-thumbnail server path (Task 14); real `ProbeStoredSessions` + `SocketPickerClient` implementations (Task 18, no more `/* ... */` stubs).

---

## File Structure

**New files:**

| File | Responsibility |
|---|---|
| `protocol/session_picker.go` | Wire types (SessionSummary, LiveSummary) + encoders/decoders for ListSessions, RecoverSession, Rename, Delete, FetchThumbnail, SessionOpResponse |
| `protocol/session_picker_test.go` | Round-trip encode/decode tests for all new wire types |
| `internal/thumbnail/render.go` | **Shared** primitive: `RenderGrid([][]Cell) (image.Image, error)` + `WritePNGAtomic(path, image.Image) error` + `DownscaleAspectFit(img, w, h) *image.RGBA` |
| `internal/thumbnail/render_test.go` | Primitive tests |
| `internal/runtime/server/manager_session_picker.go` | `StoredSummaries`, `LiveSummaries`, `RenameStored`, `DeleteStored` methods on Manager |
| `internal/runtime/server/manager_session_picker_test.go` | Manager helper unit tests |
| `internal/runtime/server/thumbnail_renderer.go` | Server-side `DesktopSink.RenderSessionThumbnail` (composes pane buffers → grid → shared primitive) |
| `internal/runtime/server/thumbnail_renderer_test.go` | Composition + adapter tests |
| `internal/runtime/server/thumbnail.go` | `captureThumbnail` orchestrator (calls renderer, calls primitive's `WritePNGAtomic`) |
| `internal/runtime/server/thumbnail_test.go` | Capture trigger tests |
| `internal/runtime/server/connection_session_picker.go` | Connection handlers: `handleListSessions`, `handleRecoverSession`, `handleRenameSession`, `handleDeleteSession`, `handleFetchThumbnail` |
| `internal/runtime/server/connection_session_picker_test.go` | Handler unit tests via memconn |
| `cmd/texelation/boot/picker.go` | `Picker` struct, run loop, dispatch |
| `cmd/texelation/boot/picker_input.go` | Key handling, navigation, mode transitions |
| `cmd/texelation/boot/picker_render.go` | Card layout, tabs, action bar, error banner |
| `cmd/texelation/boot/picker_thumbnail.go` | Kitty image rendering, lazy fetch coordinator |
| `cmd/texelation/boot/picker_ascii.go` | TreeNodeSnapshot → box-drawing chars (n-way splits) |
| `cmd/texelation/boot/picker_ascii_test.go` | Pure ASCII algorithm tests |
| `cmd/texelation/boot/picker_test.go` | Picker render + navigation tests |
| `cmd/texelation/boot/picker_runner.go` | `RunPicker`, `ProbeStoredSessions`, `SocketPickerClient` |
| `cmd/texelation/boot/picker_client_test.go` | SocketPickerClient round-trip tests |

**Modified files:**

| File | Change |
|---|---|
| `protocol/protocol.go` | Bump `Version` to 5; add 8 new `MessageType` constants (ListSessions, ListSessionsResponse, Recover, Rename, Delete, FetchThumbnail, FetchThumbnailResponse, SessionOpResponse) |
| `internal/runtime/server/session_persistence.go` | Add `Layout *protocol.TreeNodeSnapshot` field to `StoredSession` and JSON shape; orphan-PNG cleanup in `ScanSessionsDir` |
| `internal/runtime/server/connection.go` | Add `manager *Manager` field; thread through `newConnection` (~16 test files updated) |
| `internal/runtime/server/connection_handler.go` | Dispatch new message types to `connection_session_picker.go` handlers |
| `internal/runtime/server/manager.go` | Thumbnail capture in `ShutdownSessions` and `Close(id)`; renderer setter |
| `internal/runtime/server/desktop_sink.go` | Implement `ThumbnailRenderer` interface (delegates to `thumbnail_renderer.go`) |
| `internal/runtime/client/screenshot.go` | Refactor to use shared `internal/thumbnail.RenderGrid` (dedup) |
| `cmd/texelation/main.go` | Add `--recover` flag; wire picker activation between supervisor and `clientrt.Run` |
| `internal/runtime/client/app.go` | Add `RecoverSessionID [16]byte` to `Options`; pre-seed sessionID when set |

---

## Tasks Overview (v2)

1. Protocol: bump version + add MessageType constants (incl. SessionOpResponse)
2. Protocol: SessionSummary + LiveSummary wire types (with off-by-16 fix)
3. Protocol: ListSessions request/response
4. Protocol: Recover + Rename + Delete + FetchThumbnail + SessionOpResponse messages
5. StoredSession: add Layout field
6. Manager: StoredSummaries + LiveSummaries (RLock snapshot pattern)
7. Manager: RenameStored + DeleteStored (TOCTOU-safe)
8. Manager: orphan PNG cleanup at boot scan
9. **Shared `internal/thumbnail/` primitive** (RenderGrid + WritePNGAtomic + Downscale)
10. **Server-side composition adapter** (`DesktopSink.RenderSessionThumbnail`)
11. Server: `captureThumbnail` orchestrator + capture triggers (full ShutdownSessions code)
12. Connection: add `manager *Manager` field + ripple
13. Connection handlers: list + recover
14. Connection handlers: rename + delete + fetch-thumbnail (size + dim caps; uses SessionOpResponse)
15. Picker: ASCII layout algorithm (n-way splits, divider-column-correct)
16. Picker: state machine, navigation, render (mu, error banner, RefreshCatalog error visible)
17. Picker: Kitty thumbnail rendering + lazy fetch (locked, defer-clear pending)
18. boot.Run integration: real ProbeStoredSessions + SocketPickerClient + handoff
19. Client screenshot: refactor to use shared `internal/thumbnail` primitive

---

### Task 1: Protocol version bump + MessageType constants

**Files:**
- Modify: `protocol/protocol.go:43` (Version constant) and `protocol/protocol.go:50-91` (MessageType iota block)
- Test: `protocol/protocol_test.go` (existing — just needs the constant assertion updated if any)

- [ ] **Step 1: Read existing version test**

Run: `grep -n "Version" protocol/protocol_test.go`
Expected: locate any test that pins the version constant.

- [ ] **Step 2: Write a failing test pinning v5 + new constants**

Create or extend `protocol/protocol_test.go` with:

```go
func TestVersionIsV5(t *testing.T) {
	if protocol.Version != 5 {
		t.Fatalf("Version = %d, want 5 (Plan F.1 picker bump)", protocol.Version)
	}
}

func TestSessionPickerMessageTypes(t *testing.T) {
	// Pinning the iota positions guards against accidental reorder
	// of preceding entries (which would silently shift these to a
	// MsgType already on the wire).
	cases := []struct {
		name string
		got  protocol.MessageType
		want protocol.MessageType
	}{
		{"MsgListSessions", protocol.MsgListSessions, 35},
		{"MsgListSessionsResponse", protocol.MsgListSessionsResponse, 36},
		{"MsgRecoverSession", protocol.MsgRecoverSession, 37},
		{"MsgRenameSession", protocol.MsgRenameSession, 38},
		{"MsgDeleteSession", protocol.MsgDeleteSession, 39},
		{"MsgFetchThumbnail", protocol.MsgFetchThumbnail, 40},
		{"MsgFetchThumbnailResponse", protocol.MsgFetchThumbnailResponse, 41},
		{"MsgSessionOpResponse", protocol.MsgSessionOpResponse, 42},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Fatalf("%s = %d, want %d", c.name, c.got, c.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run test, verify failure**

Run: `go test ./protocol/ -run "TestVersionIsV5|TestSessionPickerMessageTypes" -count=1`
Expected: FAIL — undefined identifiers `MsgListSessions` etc., and `Version != 5`.

- [ ] **Step 4: Bump version**

In `protocol/protocol.go`, update the version block to:

```go
// v5 (issue #199 Plan F.1): adds MsgListSessions, MsgListSessionsResponse,
// MsgRecoverSession, MsgRenameSession, MsgDeleteSession, MsgFetchThumbnail,
// MsgFetchThumbnailResponse plus SessionSummary / LiveSummary wire types.
// Strict version equality means old (v4) clients fail at the header check
// — there is no in-band fallback path; users on the old client see a
// generic connect error and must upgrade. Single-binary deployment makes
// this acceptable; revisit if a multi-version client population emerges.
const Version uint8 = 5
```

- [ ] **Step 5: Add MessageType constants**

Append at the end of the iota block in `protocol/protocol.go` (after `MsgBootProgress`):

```go
	// MsgListSessions / MsgListSessionsResponse drive the F.1 stored-
	// session recovery picker. Request has no payload; response carries
	// Live and Stored slices.
	MsgListSessions
	MsgListSessionsResponse
	// MsgRecoverSession: client picks a stored session to hydrate.
	// Server replies with the ordinary MsgConnectAccept + MsgTreeSnapshot
	// flow; picker hands off to the existing connect path.
	MsgRecoverSession
	// MsgRenameSession: edit a stored session's label without recovering.
	MsgRenameSession
	// MsgDeleteSession: remove a stored session's JSON + PNG sidecar.
	// Refused with error if the session is currently live.
	MsgDeleteSession
	// MsgFetchThumbnail / MsgFetchThumbnailResponse: lazy pull of a
	// stored session's PNG thumbnail. Only fires for graphics-capable
	// terminals; non-graphics clients render an ASCII fallback instead.
	MsgFetchThumbnail
	MsgFetchThumbnailResponse
	// MsgSessionOpResponse: dedicated ack for rename/delete (and any
	// future session-mutating op). Carries OK + Error + OpType so the
	// picker can correlate without conflating with MsgListSessionsResponse.
	MsgSessionOpResponse
```

- [ ] **Step 6: Run test to verify pass**

Run: `go test ./protocol/ -run "TestVersionIsV5|TestSessionPickerMessageTypes" -count=1`
Expected: PASS.

- [ ] **Step 7: Run the full protocol suite to catch any version-coupled regressions**

Run: `go test ./protocol/ -count=1`
Expected: PASS. The strict version check means existing tests that construct headers via `protocol.Version` automatically use v5.

- [ ] **Step 8: Commit**

```bash
git add protocol/protocol.go protocol/protocol_test.go
git commit -m "Protocol v5: bump version + add session-picker message constants"
```

---

### Task 2: SessionSummary + LiveSummary wire types

**Files:**
- Create: `protocol/session_picker.go`
- Test: `protocol/session_picker_test.go`

- [ ] **Step 1: Write the failing round-trip test**

Create `protocol/session_picker_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
	"reflect"
	"testing"
)

func makeID(b byte) [16]byte {
	var id [16]byte
	for i := range id {
		id[i] = b
	}
	return id
}

func TestSessionSummary_RoundTrip(t *testing.T) {
	in := SessionSummary{
		SessionID:      makeID(0xAB),
		Label:          "work-laptop",
		LastActive:     1714752000,
		PaneCount:      4,
		FirstPaneTitle: "nvim · texelation/cmd/texelation",
		Pinned:         true,
		HasThumbnail:   true,
		Layout: &TreeNodeSnapshot{
			PaneIndex:   -1,
			Split:       SplitHorizontal,
			SplitRatios: []float32{0.5, 0.5},
			Children: []TreeNodeSnapshot{
				{PaneIndex: 0, Split: SplitNone},
				{PaneIndex: 1, Split: SplitNone},
			},
		},
	}
	encoded, err := EncodeSessionSummary(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, consumed, err := DecodeSessionSummary(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(encoded) {
		t.Fatalf("consumed=%d, len=%d", consumed, len(encoded))
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}

func TestSessionSummary_NilLayout(t *testing.T) {
	in := SessionSummary{
		SessionID:    makeID(0x01),
		Label:        "untitled",
		LastActive:   100,
		PaneCount:    1,
		HasThumbnail: false,
		Layout:       nil,
	}
	encoded, err := EncodeSessionSummary(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, _, err := DecodeSessionSummary(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Layout != nil {
		t.Fatalf("expected nil Layout after round-trip, got %#v", out.Layout)
	}
}

func TestLiveSummary_RoundTrip(t *testing.T) {
	in := LiveSummary{
		SessionID:       makeID(0xCD),
		Label:           "debug-cluster",
		AttachedClients: 2,
		PaneCount:       3,
		LastInputAt:     1714752100,
	}
	encoded, err := EncodeLiveSummary(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, consumed, err := DecodeLiveSummary(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != len(encoded) {
		t.Fatalf("consumed=%d, len=%d", consumed, len(encoded))
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./protocol/ -run "TestSessionSummary|TestLiveSummary" -count=1`
Expected: FAIL — undefined `SessionSummary`, `LiveSummary`, `EncodeSessionSummary`, etc.

- [ ] **Step 3: Implement the wire types and encoders**

Create `protocol/session_picker.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/session_picker.go
// Summary: Wire types and encoders for the F.1 session-recovery picker.
// Usage: Used by the server's connection handler to advertise stored
//   sessions and by the client's boot-package picker to consume them.

package protocol

import (
	"bytes"
	"encoding/binary"
)

// SessionSummary describes a stored (fossilized) session on disk.
// HasThumbnail indicates whether <sessionID>.png exists alongside the
// JSON sidecar; the client fetches the PNG lazily via MsgFetchThumbnail.
// Layout is optional — nil when the StoredSession was written before
// Plan F.1 added the Layout field, in which case the picker falls back
// to a single-box ASCII placeholder.
type SessionSummary struct {
	SessionID      [16]byte
	Label          string
	LastActive     int64 // unix seconds
	PaneCount      int32
	FirstPaneTitle string
	Pinned         bool
	HasThumbnail   bool
	Layout         *TreeNodeSnapshot
}

// LiveSummary describes a hydrated (in-memory) session. Populated only
// by F.2; F.1 always sends an empty slice.
type LiveSummary struct {
	SessionID       [16]byte
	Label           string
	AttachedClients int32
	PaneCount       int32
	LastInputAt     int64
}

// EncodeSessionSummary writes a SessionSummary in the picker wire format.
// Layout is preceded by a uint8 flag (1 = present, 0 = nil) to handle
// the optional case without needing a sentinel TreeNodeSnapshot.
func EncodeSessionSummary(s SessionSummary) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(s.SessionID[:])
	if err := encodeString(buf, s.Label); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.LastActive); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.PaneCount); err != nil {
		return nil, err
	}
	if err := encodeString(buf, s.FirstPaneTitle); err != nil {
		return nil, err
	}
	flags := byte(0)
	if s.Pinned {
		flags |= 0x01
	}
	if s.HasThumbnail {
		flags |= 0x02
	}
	buf.WriteByte(flags)
	if s.Layout == nil {
		buf.WriteByte(0)
	} else {
		buf.WriteByte(1)
		if err := encodeTreeNode(buf, *s.Layout); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// DecodeSessionSummary reads a SessionSummary and returns the number of
// bytes consumed so callers parsing slices of summaries can advance.
func DecodeSessionSummary(b []byte) (SessionSummary, int, error) {
	var s SessionSummary
	start := len(b)
	if len(b) < 16 {
		return s, 0, ErrPayloadShort
	}
	copy(s.SessionID[:], b[:16])
	b = b[16:]
	label, rest, err := decodeString(b)
	if err != nil {
		return s, 0, err
	}
	s.Label = label
	b = rest
	if len(b) < 8 {
		return s, 0, ErrPayloadShort
	}
	s.LastActive = int64(binary.LittleEndian.Uint64(b[:8]))
	b = b[8:]
	if len(b) < 4 {
		return s, 0, ErrPayloadShort
	}
	s.PaneCount = int32(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	title, rest, err := decodeString(b)
	if err != nil {
		return s, 0, err
	}
	s.FirstPaneTitle = title
	b = rest
	if len(b) < 1 {
		return s, 0, ErrPayloadShort
	}
	flags := b[0]
	s.Pinned = flags&0x01 != 0
	s.HasThumbnail = flags&0x02 != 0
	b = b[1:]
	if len(b) < 1 {
		return s, 0, ErrPayloadShort
	}
	hasLayout := b[0]
	b = b[1:]
	if hasLayout != 0 {
		node, rest, err := decodeTreeNode(b)
		if err != nil {
			return s, 0, err
		}
		s.Layout = &node
		b = rest
	}
	return s, start - len(b), nil
}

// EncodeLiveSummary writes a LiveSummary in the picker wire format.
func EncodeLiveSummary(s LiveSummary) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(s.SessionID[:])
	if err := encodeString(buf, s.Label); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.AttachedClients); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.PaneCount); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.LastInputAt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeLiveSummary reads a LiveSummary and returns bytes consumed.
func DecodeLiveSummary(b []byte) (LiveSummary, int, error) {
	var s LiveSummary
	start := len(b)
	if len(b) < 16 {
		return s, 0, ErrPayloadShort
	}
	copy(s.SessionID[:], b[:16])
	b = b[16:]
	label, rest, err := decodeString(b)
	if err != nil {
		return s, 0, err
	}
	s.Label = label
	b = rest
	if len(b) < 16 {
		return s, 0, ErrPayloadShort
	}
	s.AttachedClients = int32(binary.LittleEndian.Uint32(b[:4]))
	s.PaneCount = int32(binary.LittleEndian.Uint32(b[4:8]))
	s.LastInputAt = int64(binary.LittleEndian.Uint64(b[8:16]))
	b = b[16:]
	return s, start - len(b), nil
}
```

- [ ] **Step 4: Run test, verify pass**

Run: `go test ./protocol/ -run "TestSessionSummary|TestLiveSummary" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add protocol/session_picker.go protocol/session_picker_test.go
git commit -m "Protocol: SessionSummary and LiveSummary wire types"
```

---

### Task 3: ListSessions request/response messages

**Files:**
- Modify: `protocol/session_picker.go` (extend with request/response types)
- Modify: `protocol/session_picker_test.go` (add round-trip tests)

- [ ] **Step 1: Write the failing tests**

Append to `protocol/session_picker_test.go`:

```go
func TestListSessionsRequest_RoundTrip(t *testing.T) {
	in := ListSessionsRequest{}
	encoded, err := EncodeListSessionsRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(encoded) != 0 {
		t.Fatalf("expected empty payload for v5 request, got %d bytes", len(encoded))
	}
	out, err := DecodeListSessionsRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestListSessionsResponse_RoundTrip(t *testing.T) {
	in := ListSessionsResponse{
		Live: []LiveSummary{
			{SessionID: makeID(0xAA), Label: "live-1", PaneCount: 2, LastInputAt: 200},
		},
		Stored: []SessionSummary{
			{SessionID: makeID(0xBB), Label: "stored-1", LastActive: 100, PaneCount: 1},
			{SessionID: makeID(0xCC), Label: "stored-2", LastActive: 50, PaneCount: 4, Pinned: true, HasThumbnail: true},
		},
	}
	encoded, err := EncodeListSessionsResponse(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeListSessionsResponse(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\n in=%#v\nout=%#v", in, out)
	}
}

func TestListSessionsResponse_EmptyBoth(t *testing.T) {
	in := ListSessionsResponse{}
	encoded, err := EncodeListSessionsResponse(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeListSessionsResponse(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Live) != 0 || len(out.Stored) != 0 {
		t.Fatalf("expected empty slices, got Live=%d Stored=%d", len(out.Live), len(out.Stored))
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./protocol/ -run "TestListSessions" -count=1`
Expected: FAIL — undefined types.

- [ ] **Step 3: Implement request/response encoders**

Append to `protocol/session_picker.go`:

```go
// ListSessionsRequest is the client-side trigger for the recovery
// picker's catalog. Empty payload in v5; reserved for filters in F.2.
type ListSessionsRequest struct{}

// EncodeListSessionsRequest returns an empty payload (no fields in v5).
func EncodeListSessionsRequest(r ListSessionsRequest) ([]byte, error) {
	return nil, nil
}

// DecodeListSessionsRequest tolerates trailing bytes for forward-compat
// with future filter additions.
func DecodeListSessionsRequest(b []byte) (ListSessionsRequest, error) {
	return ListSessionsRequest{}, nil
}

// ListSessionsResponse carries the catalog of recoverable sessions.
// Live is empty in F.1; populated by F.2.
type ListSessionsResponse struct {
	Live   []LiveSummary
	Stored []SessionSummary
}

// EncodeListSessionsResponse writes Live and Stored slices.
func EncodeListSessionsResponse(r ListSessionsResponse) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if len(r.Live) > 0xFFFF {
		return nil, ErrStringTooLong
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Live))); err != nil {
		return nil, err
	}
	for _, l := range r.Live {
		raw, err := EncodeLiveSummary(l)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	if len(r.Stored) > 0xFFFF {
		return nil, ErrStringTooLong
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Stored))); err != nil {
		return nil, err
	}
	for _, s := range r.Stored {
		raw, err := EncodeSessionSummary(s)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	return buf.Bytes(), nil
}

// DecodeListSessionsResponse parses Live then Stored slices in order.
func DecodeListSessionsResponse(b []byte) (ListSessionsResponse, error) {
	var r ListSessionsResponse
	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	liveCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if liveCount > 0 {
		r.Live = make([]LiveSummary, 0, liveCount)
		for i := uint16(0); i < liveCount; i++ {
			l, consumed, err := DecodeLiveSummary(b)
			if err != nil {
				return ListSessionsResponse{}, err
			}
			r.Live = append(r.Live, l)
			b = b[consumed:]
		}
	}
	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	storedCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if storedCount > 0 {
		r.Stored = make([]SessionSummary, 0, storedCount)
		for i := uint16(0); i < storedCount; i++ {
			s, consumed, err := DecodeSessionSummary(b)
			if err != nil {
				return ListSessionsResponse{}, err
			}
			r.Stored = append(r.Stored, s)
			b = b[consumed:]
		}
	}
	return r, nil
}
```

- [ ] **Step 4: Run test, verify pass**

Run: `go test ./protocol/ -run "TestListSessions" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add protocol/session_picker.go protocol/session_picker_test.go
git commit -m "Protocol: ListSessions request/response messages"
```

---

### Task 4: RecoverSession + Rename + Delete + FetchThumbnail messages

**Files:**
- Modify: `protocol/session_picker.go`
- Modify: `protocol/session_picker_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `protocol/session_picker_test.go`:

```go
func TestRecoverSessionRequest_RoundTrip(t *testing.T) {
	in := RecoverSessionRequest{
		SessionID: makeID(0xFF),
		NewLabel:  "renamed-on-recover",
	}
	encoded, err := EncodeRecoverSessionRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeRecoverSessionRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestRenameSessionRequest_RoundTrip(t *testing.T) {
	in := RenameSessionRequest{SessionID: makeID(0x11), NewLabel: "edit"}
	encoded, err := EncodeRenameSessionRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeRenameSessionRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestSessionOpResponse_RoundTrip(t *testing.T) {
	cases := []SessionOpResponse{
		{OpType: OpRename, OK: true},
		{OpType: OpRename, OK: false, Error: "session not found"},
		{OpType: OpDelete, OK: true},
		{OpType: OpDelete, OK: false, Error: "session is live"},
	}
	for i, in := range cases {
		encoded, err := EncodeSessionOpResponse(in)
		if err != nil {
			t.Fatalf("[%d] encode: %v", i, err)
		}
		out, err := DecodeSessionOpResponse(encoded)
		if err != nil {
			t.Fatalf("[%d] decode: %v", i, err)
		}
		if !reflect.DeepEqual(in, out) {
			t.Fatalf("[%d] round-trip mismatch: in=%#v out=%#v", i, in, out)
		}
	}
}

func TestDeleteSessionRequest_RoundTrip(t *testing.T) {
	in := DeleteSessionRequest{SessionID: makeID(0x22)}
	encoded, err := EncodeDeleteSessionRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeDeleteSessionRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestFetchThumbnailRequest_RoundTrip(t *testing.T) {
	in := FetchThumbnailRequest{SessionID: makeID(0x33)}
	encoded, err := EncodeFetchThumbnailRequest(in)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := DecodeFetchThumbnailRequest(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip mismatch: in=%#v out=%#v", in, out)
	}
}

func TestFetchThumbnailResponse_RoundTrip(t *testing.T) {
	cases := []FetchThumbnailResponse{
		{OK: true, PNG: []byte{0x89, 0x50, 0x4E, 0x47}},
		{OK: false, Error: "missing"},
		{OK: true, PNG: nil},
	}
	for i, in := range cases {
		encoded, err := EncodeFetchThumbnailResponse(in)
		if err != nil {
			t.Fatalf("[%d] encode: %v", i, err)
		}
		out, err := DecodeFetchThumbnailResponse(encoded)
		if err != nil {
			t.Fatalf("[%d] decode: %v", i, err)
		}
		// Treat nil and empty PNG as equal — the wire never preserves
		// the distinction.
		if !bytesEqual(in.PNG, out.PNG) || in.OK != out.OK || in.Error != out.Error {
			t.Fatalf("[%d] round-trip mismatch: in=%#v out=%#v", i, in, out)
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./protocol/ -run "TestRecoverSession|TestRenameSession|TestSessionOpResponse|TestDeleteSession|TestFetchThumbnail" -count=1`
Expected: FAIL — undefined types/encoders.

- [ ] **Step 3: Implement the messages**

Append to `protocol/session_picker.go`:

```go
// RecoverSessionRequest hydrates a stored session and connects to it.
// NewLabel is optional — empty string means leave the label unchanged.
type RecoverSessionRequest struct {
	SessionID [16]byte
	NewLabel  string
}

func EncodeRecoverSessionRequest(r RecoverSessionRequest) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(r.SessionID[:])
	if err := encodeString(buf, r.NewLabel); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeRecoverSessionRequest(b []byte) (RecoverSessionRequest, error) {
	var r RecoverSessionRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	label, _, err := decodeString(b[16:])
	if err != nil {
		return r, err
	}
	r.NewLabel = label
	return r, nil
}

// RenameSessionRequest edits a stored session's label without recovering.
type RenameSessionRequest struct {
	SessionID [16]byte
	NewLabel  string
}

func EncodeRenameSessionRequest(r RenameSessionRequest) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(r.SessionID[:])
	if err := encodeString(buf, r.NewLabel); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeRenameSessionRequest(b []byte) (RenameSessionRequest, error) {
	var r RenameSessionRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	label, _, err := decodeString(b[16:])
	if err != nil {
		return r, err
	}
	r.NewLabel = label
	return r, nil
}

// DeleteSessionRequest removes a stored session's JSON + PNG sidecar.
type DeleteSessionRequest struct {
	SessionID [16]byte
}

func EncodeDeleteSessionRequest(r DeleteSessionRequest) ([]byte, error) {
	out := make([]byte, 16)
	copy(out, r.SessionID[:])
	return out, nil
}

func DecodeDeleteSessionRequest(b []byte) (DeleteSessionRequest, error) {
	var r DeleteSessionRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	return r, nil
}

// SessionOpKind identifies which op a SessionOpResponse acks. The
// picker uses this to correlate without needing in-flight request IDs.
type SessionOpKind uint8

const (
	OpRename SessionOpKind = iota
	OpDelete
)

// SessionOpResponse is the dedicated ack envelope for rename + delete
// (and any future session-mutating op). OK=false implies a non-empty
// Error explaining the refusal (live session, not found, IO failure).
// OpType distinguishes which op this acks so the picker doesn't
// conflate it with a coincident MsgListSessionsResponse.
type SessionOpResponse struct {
	OpType SessionOpKind
	OK     bool
	Error  string
}

func EncodeSessionOpResponse(r SessionOpResponse) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(byte(r.OpType))
	if r.OK {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if err := encodeString(buf, r.Error); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeSessionOpResponse(b []byte) (SessionOpResponse, error) {
	var r SessionOpResponse
	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	r.OpType = SessionOpKind(b[0])
	r.OK = b[1] != 0
	msg, _, err := decodeString(b[2:])
	if err != nil {
		return r, err
	}
	r.Error = msg
	return r, nil
}

// FetchThumbnailRequest pulls a stored session's PNG sidecar.
type FetchThumbnailRequest struct {
	SessionID [16]byte
}

func EncodeFetchThumbnailRequest(r FetchThumbnailRequest) ([]byte, error) {
	out := make([]byte, 16)
	copy(out, r.SessionID[:])
	return out, nil
}

func DecodeFetchThumbnailRequest(b []byte) (FetchThumbnailRequest, error) {
	var r FetchThumbnailRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	return r, nil
}

// FetchThumbnailResponse carries the PNG bytes (or an error explanation
// if OK=false). PNGs over 16MiB are rejected at the framing layer
// (MaxPayloadLen); typical sidecars are tens of KB.
type FetchThumbnailResponse struct {
	OK    bool
	Error string
	PNG   []byte
}

func EncodeFetchThumbnailResponse(r FetchThumbnailResponse) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if r.OK {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if err := encodeString(buf, r.Error); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(r.PNG))); err != nil {
		return nil, err
	}
	if len(r.PNG) > 0 {
		if _, err := buf.Write(r.PNG); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func DecodeFetchThumbnailResponse(b []byte) (FetchThumbnailResponse, error) {
	var r FetchThumbnailResponse
	if len(b) < 1 {
		return r, ErrPayloadShort
	}
	r.OK = b[0] != 0
	b = b[1:]
	msg, rest, err := decodeString(b)
	if err != nil {
		return r, err
	}
	r.Error = msg
	if len(rest) < 4 {
		return r, ErrPayloadShort
	}
	pngLen := binary.LittleEndian.Uint32(rest[:4])
	rest = rest[4:]
	if uint32(len(rest)) < pngLen {
		return r, ErrPayloadShort
	}
	if pngLen > 0 {
		r.PNG = append([]byte(nil), rest[:pngLen]...)
	}
	return r, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./protocol/ -run "TestRecoverSession|TestRenameSession|TestSessionOpResponse|TestDeleteSession|TestFetchThumbnail" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add protocol/session_picker.go protocol/session_picker_test.go
git commit -m "Protocol: recover/rename/delete/fetch-thumbnail messages"
```

---

### Task 5: Extend StoredSession with Layout field

**Files:**
- Modify: `internal/runtime/server/session_persistence.go:34-78` (struct + JSON shape)
- Test: `internal/runtime/server/session_persistence_test.go` (existing) — add round-trip + missing-field tests

- [ ] **Step 1: Write the failing tests**

Append to `internal/runtime/server/session_persistence_test.go` (create the file structure if it doesn't already define a package import for protocol):

```go
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

func TestStoredSession_LayoutAbsentInOlderJSON(t *testing.T) {
	// Older Plan D2 JSONs have no "layout" field. Unmarshal must
	// leave the field nil rather than failing — the picker falls
	// back to a single-box ASCII placeholder.
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
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestStoredSession_Layout" -count=1`
Expected: FAIL — `Layout` not a field on StoredSession.

- [ ] **Step 3: Add the Layout field**

In `internal/runtime/server/session_persistence.go`, modify the struct (around line 34) and the JSON shape (line 59):

```go
type StoredSession struct {
	SchemaVersion int
	SessionID     [16]byte
	LastActive    time.Time
	Pinned        bool
	PaneViewports []StoredPaneViewport
	// Plan F metadata (populated at write time):
	Label          string
	PaneCount      int
	FirstPaneTitle string
	// Plan F.1: pane-tree layout for the recovery picker's ASCII
	// fallback render. Nil for sessions written before F.1 — the
	// picker handles that as a single-box placeholder.
	Layout *protocol.TreeNodeSnapshot
}
```

```go
type sessionJSONShape struct {
	SchemaVersion  int                       `json:"schemaVersion"`
	SessionID      string                    `json:"sessionID"`
	LastActive     time.Time                 `json:"lastActive"`
	Pinned         bool                      `json:"pinned"`
	PaneViewports  []paneViewportJSONShape   `json:"paneViewports"`
	Label          string                    `json:"label"`
	PaneCount      int                       `json:"paneCount"`
	FirstPaneTitle string                    `json:"firstPaneTitle"`
	Layout         *protocol.TreeNodeSnapshot `json:"layout,omitempty"`
}
```

Update `MarshalJSON` to copy the Layout pointer:

```go
func (s StoredSession) MarshalJSON() ([]byte, error) {
	out := sessionJSONShape{
		SchemaVersion:  s.SchemaVersion,
		SessionID:      hex.EncodeToString(s.SessionID[:]),
		LastActive:     s.LastActive,
		Pinned:         s.Pinned,
		Label:          s.Label,
		PaneCount:      s.PaneCount,
		FirstPaneTitle: s.FirstPaneTitle,
		Layout:         s.Layout,
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
```

Update `UnmarshalJSON` to read it back:

```go
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
	s.Layout = in.Layout
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
```

Add `"github.com/framegrace/texelation/protocol"` to the imports if not already present.

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestStoredSession" -count=1`
Expected: PASS — both the new tests and any existing StoredSession tests.

- [ ] **Step 5: Run full server test suite to catch regressions**

Run: `go test ./internal/runtime/server/ -count=1`
Expected: PASS. Existing tests construct StoredSession without Layout — Go zero-values it to nil.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/session_persistence.go internal/runtime/server/session_persistence_test.go
git commit -m "StoredSession: add optional Layout field for picker render"
```

---

### Task 6: Manager.StoredSummaries + Manager.LiveSummaries

**Files:**
- Create: `internal/runtime/server/manager_session_picker.go`
- Test: `internal/runtime/server/manager_session_picker_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/runtime/server/manager_session_picker_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

func TestStoredSummaries_EmptyOnFreshManager(t *testing.T) {
	m := NewManager()
	got := m.StoredSummaries()
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestStoredSummaries_OrderedPinnedThenLastActive(t *testing.T) {
	m := NewManager()
	m.SetPersistedSessions(map[[16]byte]*StoredSession{
		{0x01}: {SessionID: [16]byte{0x01}, Label: "old-pinned", LastActive: time.Unix(100, 0), Pinned: true, PaneCount: 1},
		{0x02}: {SessionID: [16]byte{0x02}, Label: "newest", LastActive: time.Unix(300, 0), PaneCount: 2},
		{0x03}: {SessionID: [16]byte{0x03}, Label: "middle", LastActive: time.Unix(200, 0), PaneCount: 3},
	})
	got := m.StoredSummaries()
	if len(got) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(got))
	}
	// Pinned first, then LastActive desc.
	if got[0].Label != "old-pinned" {
		t.Fatalf("[0] expected pinned first, got %q", got[0].Label)
	}
	if got[1].Label != "newest" {
		t.Fatalf("[1] expected newest second, got %q", got[1].Label)
	}
	if got[2].Label != "middle" {
		t.Fatalf("[2] expected middle third, got %q", got[2].Label)
	}
}

func TestStoredSummaries_HasThumbnailFlag(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, SessionsDirName)
	if err := mkdirAll(sessionsDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	withPNG := [16]byte{0x10}
	withoutPNG := [16]byte{0x20}
	if err := writeFile(filepath.Join(sessionsDir, hexID(withPNG)+".png"), []byte("fake")); err != nil {
		t.Fatalf("write png: %v", err)
	}
	m := NewManager()
	m.persistBasedir = dir
	m.SetPersistedSessions(map[[16]byte]*StoredSession{
		withPNG:    {SessionID: withPNG, LastActive: time.Unix(200, 0), PaneCount: 1},
		withoutPNG: {SessionID: withoutPNG, LastActive: time.Unix(100, 0), PaneCount: 1},
	})
	got := m.StoredSummaries()
	bySID := make(map[[16]byte]protocol.SessionSummary)
	for _, s := range got {
		bySID[s.SessionID] = s
	}
	if !bySID[withPNG].HasThumbnail {
		t.Errorf("expected HasThumbnail=true for %x", withPNG)
	}
	if bySID[withoutPNG].HasThumbnail {
		t.Errorf("expected HasThumbnail=false for %x", withoutPNG)
	}
	_ = sort.IntSlice(nil) // keep sort import live for future tests
}

func TestLiveSummaries_EmptyInF1(t *testing.T) {
	m := NewManager()
	if got := m.LiveSummaries(); got != nil {
		t.Fatalf("expected nil (F.2 will populate), got %#v", got)
	}
}
```

Helpers (declare once at the top of the test file or in a shared `_test.go` if they don't already exist — note that `mkdirAll`, `writeFile`, `hexID` may already be present from other test files in the package):

```go
func mkdirAll(p string) error {
	return os.MkdirAll(p, 0o755)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

func hexID(id [16]byte) string {
	return hex.EncodeToString(id[:])
}
```

If those names collide with existing helpers, use them as-is (the package is small enough that helper sharing is acceptable). The `os` and `encoding/hex` imports must be added to the test file.

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestStoredSummaries|TestLiveSummaries" -count=1`
Expected: FAIL — `StoredSummaries` and `LiveSummaries` undefined.

- [ ] **Step 3: Implement the methods**

Create `internal/runtime/server/manager_session_picker.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/manager_session_picker.go
// Summary: Session-picker helpers on Manager — list / rename / delete
// stored sessions for the F.1 recovery flow.

package server

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/framegrace/texelation/internal/persistence/atomicjson"
	"github.com/framegrace/texelation/protocol"
)

// StoredSummaries returns a slice of SessionSummary records for every
// known persisted session, sorted pinned-first then LastActive desc.
// HasThumbnail is set per record by stat-ing the on-disk PNG sidecar
// when persistence is enabled; without persistence the flag is always
// false.
//
// Snapshot pattern: copies persistedSessions + persistBasedir under
// the RLock (O(N) memory, O(N) wall-clock), then runs the per-record
// PNG stat OUTSIDE the lock. Holding the RLock during disk stat would
// block every Manager write op (DeleteStored, RenameStored,
// LookupOrRehydrate, Close) for the duration of N stat calls — a real
// stall on a slow filesystem with a populous sessions dir.
func (m *Manager) StoredSummaries() []protocol.SessionSummary {
	m.mu.RLock()
	if len(m.persistedSessions) == 0 {
		m.mu.RUnlock()
		return nil
	}
	persistDir := m.persistBasedir
	type entry struct {
		id  [16]byte
		ref *StoredSession
	}
	snap := make([]entry, 0, len(m.persistedSessions))
	for id, s := range m.persistedSessions {
		snap = append(snap, entry{id: id, ref: s})
	}
	m.mu.RUnlock()

	summaries := make([]protocol.SessionSummary, 0, len(snap))
	for _, e := range snap {
		summary := protocol.SessionSummary{
			SessionID:      e.id,
			Label:          e.ref.Label,
			LastActive:     e.ref.LastActive.Unix(),
			PaneCount:      int32(e.ref.PaneCount),
			FirstPaneTitle: e.ref.FirstPaneTitle,
			Pinned:         e.ref.Pinned,
			Layout:         e.ref.Layout,
		}
		if persistDir != "" {
			pngPath := filepath.Join(persistDir, SessionsDirName, hex.EncodeToString(e.id[:])+".png")
			if info, err := os.Stat(pngPath); err == nil && !info.IsDir() {
				summary.HasThumbnail = true
			}
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Pinned != summaries[j].Pinned {
			return summaries[i].Pinned
		}
		return summaries[i].LastActive > summaries[j].LastActive
	})
	return summaries
}

// LiveSummaries returns the live (in-memory, attached or detached)
// session catalog. F.1 always returns nil — F.2 will populate this
// from m.sessions and per-session attached-client counts.
func (m *Manager) LiveSummaries() []protocol.LiveSummary {
	return nil
}

// sessionPNGPath is a small helper so other Plan F code paths
// (delete, fetch-thumbnail) can build the same PNG sidecar path.
// Returns empty string if persistence is disabled.
func (m *Manager) sessionPNGPath(id [16]byte) string {
	if m.persistBasedir == "" {
		return ""
	}
	return filepath.Join(m.persistBasedir, SessionsDirName, hex.EncodeToString(id[:])+".png")
}

// loadStoredFromDisk reads <id>.json off disk for rename/delete paths
// that need to mutate persisted state outside the in-memory index.
// Returns ErrSessionNotFound if the file is missing.
func (m *Manager) loadStoredFromDisk(id [16]byte) (*StoredSession, error) {
	if m.persistBasedir == "" {
		return nil, fmt.Errorf("manager: persistence disabled")
	}
	path := SessionFilePath(m.persistBasedir, id)
	s, err := atomicjson.Load[StoredSession](path)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrSessionNotFound
	}
	return s, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestStoredSummaries|TestLiveSummaries" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/manager_session_picker.go internal/runtime/server/manager_session_picker_test.go
git commit -m "Manager: StoredSummaries + LiveSummaries for picker"
```

---

### Task 7: Manager.RenameStored + Manager.DeleteStored

**Files:**
- Modify: `internal/runtime/server/manager_session_picker.go` (append methods)
- Modify: `internal/runtime/server/manager_session_picker_test.go` (append tests)

- [ ] **Step 1: Write failing tests**

Append to `internal/runtime/server/manager_session_picker_test.go`:

```go
func TestRenameStored_UpdatesInMemoryAndDisk(t *testing.T) {
	dir := t.TempDir()
	if err := mkdirAll(filepath.Join(dir, SessionsDirName)); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xAA}
	original := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		Label:         "before",
		PaneCount:     1,
	}
	data, _ := json.Marshal(original)
	if err := writeFile(SessionFilePath(dir, id), data); err != nil {
		t.Fatalf("write json: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable persistence: %v", err)
	}
	if err := m.RenameStored(id, "after"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got := m.StoredSummaries()
	if len(got) != 1 || got[0].Label != "after" {
		t.Fatalf("in-memory rename failed; got %#v", got)
	}
	// Disk side: re-load and confirm.
	reloaded, err := atomicjson.Load[StoredSession](SessionFilePath(dir, id))
	if err != nil || reloaded == nil {
		t.Fatalf("reload: err=%v reloaded=%v", err, reloaded)
	}
	if reloaded.Label != "after" {
		t.Fatalf("on-disk Label=%q, want %q", reloaded.Label, "after")
	}
}

func TestRenameStored_UnknownID(t *testing.T) {
	m := NewManager()
	if err := m.EnablePersistence(t.TempDir(), 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.RenameStored([16]byte{0xFF}, "nope"); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDeleteStored_RemovesBothFiles(t *testing.T) {
	dir := t.TempDir()
	if err := mkdirAll(filepath.Join(dir, SessionsDirName)); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xBB}
	jsonPath := SessionFilePath(dir, id)
	pngPath := filepath.Join(dir, SessionsDirName, hexID(id)+".png")
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	writeFile(jsonPath, data)
	writeFile(pngPath, []byte("fake-png"))
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.DeleteStored(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Errorf("expected JSON gone, stat err=%v", err)
	}
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("expected PNG gone, stat err=%v", err)
	}
	if got := m.StoredSummaries(); len(got) != 0 {
		t.Errorf("expected empty after delete, got %d entries", len(got))
	}
}

func TestDeleteStored_PNGMissingStillSucceeds(t *testing.T) {
	dir := t.TempDir()
	if err := mkdirAll(filepath.Join(dir, SessionsDirName)); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xCC}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	writeFile(SessionFilePath(dir, id), data)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := m.DeleteStored(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestDeleteStored_RefusesLive(t *testing.T) {
	id := [16]byte{0xDD}
	m := NewManager()
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("create live session: %v", err)
	}
	err := m.DeleteStored(id)
	if err == nil {
		t.Fatalf("expected error refusing live session, got nil")
	}
}
```

Add `"encoding/json"` and `"github.com/framegrace/texelation/internal/persistence/atomicjson"` to the test file imports.

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestRenameStored|TestDeleteStored" -count=1`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement RenameStored + DeleteStored**

Append to `internal/runtime/server/manager_session_picker.go`:

```go
// RenameStored updates the persisted session's Label both in-memory
// and on disk. Acquires m.mu so concurrent StoredSummaries / Recover
// calls observe a consistent state. The disk write goes through
// atomicjson.Save to retain the existing crash-safety guarantees.
//
// Returns ErrSessionNotFound when the ID is unknown to the in-memory
// index. On disk-write failures, in-memory state is rolled back so a
// subsequent retry sees the original Label rather than a half-applied
// rename.
func (m *Manager) RenameStored(id [16]byte, newLabel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.persistedSessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	prev := stored.Label
	stored.Label = newLabel
	if m.persistBasedir == "" {
		return nil
	}
	if err := atomicjson.Save(SessionFilePath(m.persistBasedir, id), stored); err != nil {
		stored.Label = prev
		return fmt.Errorf("manager: rename %x: %w", id[:4], err)
	}
	return nil
}

// DeleteStored removes the session's JSON sidecar, PNG sidecar (if
// present), and the in-memory persisted entry. Refuses with an error
// when the session is currently live — the user must detach all
// clients first.
//
// TOCTOU avoidance: the in-memory delete + the live-session refusal
// happen under m.mu held *contiguously* with the disk removes. A
// concurrent LookupOrRehydrate that observes the entry still in
// persistedSessions before this method's lock acquisition will block
// on m.mu and find it gone; one that already grabbed the entry before
// the lock would have called delete(persistedSessions, id) itself,
// so the !ok branch below catches that race cleanly.
//
// PNG removal is best-effort (a missing PNG is not an error); JSON
// removal is authoritative — if the JSON unlink fails we leave the
// in-memory entry alone and surface the error so the caller can
// retry. The PNG-then-JSON order avoids leaving a JSON with stale
// HasThumbnail expectations across a partial delete.
//
// We also markClosing(id) before any disk I/O so a concurrent
// rehydrate path waits behind the unlink rather than constructing a
// fresh Session pointing at the on-disk file we're about to remove.
func (m *Manager) DeleteStored(id [16]byte) error {
	m.mu.Lock()
	if _, live := m.sessions[id]; live {
		m.mu.Unlock()
		return fmt.Errorf("manager: session %x is live; detach all clients first", id[:4])
	}
	if _, ok := m.persistedSessions[id]; !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	delete(m.persistedSessions, id)
	persistDir := m.persistBasedir
	m.markClosing(id)
	m.mu.Unlock()
	defer m.unmarkClosing(id)

	if persistDir == "" {
		return nil
	}
	pngPath := filepath.Join(persistDir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if err := os.Remove(pngPath); err != nil && !os.IsNotExist(err) {
		// In-memory entry is already gone; surfacing the error
		// to the caller is enough. A retry of DeleteStored will
		// no-op on the in-memory side and re-attempt the unlink.
		return fmt.Errorf("manager: remove %s: %w", pngPath, err)
	}
	jsonPath := SessionFilePath(persistDir, id)
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("manager: remove %s: %w", jsonPath, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestRenameStored|TestDeleteStored" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/manager_session_picker.go internal/runtime/server/manager_session_picker_test.go
git commit -m "Manager: RenameStored + DeleteStored for picker"
```

---

### Task 8: Boot scan extension — orphan PNG cleanup

**Files:**
- Modify: `internal/runtime/server/session_persistence.go` (extend `ScanSessionsDir`)
- Test: `internal/runtime/server/session_persistence_test.go` (existing)

- [ ] **Step 1: Write failing test**

Append to `internal/runtime/server/session_persistence_test.go`:

```go
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
	if err := os.WriteFile(filepath.Join(sessions, hex.EncodeToString(keepID[:])+".json"), data, 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessions, hex.EncodeToString(keepID[:])+".png"), []byte("matched"), 0o644); err != nil {
		t.Fatalf("write keep png: %v", err)
	}
	orphanID := [16]byte{0xDE}
	orphanPath := filepath.Join(sessions, hex.EncodeToString(orphanID[:])+".png")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan png: %v", err)
	}
	if _, err := ScanSessionsDir(dir); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("expected orphan PNG removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(sessions, hex.EncodeToString(keepID[:])+".png")); err != nil {
		t.Errorf("expected matched PNG retained, stat err=%v", err)
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestScanSessionsDir_RemovesOrphanPNGs" -count=1`
Expected: FAIL — orphan PNG still present after scan.

- [ ] **Step 3: Extend ScanSessionsDir**

In `internal/runtime/server/session_persistence.go`, after the existing `for _, e := range entries` loop in `ScanSessionsDir`, add a second pass that walks `*.png` entries and removes ones without a matching JSON:

```go
// ... existing loop populating `out` ...

// Plan F.1: clean up orphaned PNG sidecars (a PNG whose matching
// JSON was deleted, or whose JSON failed to load above). Keeping
// them would silently inflate the picker's HasThumbnail flag for
// nonexistent entries and leak disk space across restarts.
for _, e := range entries {
	if e.IsDir() {
		continue
	}
	name := e.Name()
	if !strings.HasSuffix(name, ".png") {
		continue
	}
	hexPart := strings.TrimSuffix(name, ".png")
	var id [16]byte
	if err := decodeHex16Session(hexPart, &id); err != nil {
		continue // unrecognised filename; leave alone
	}
	if _, ok := out[id]; ok {
		continue // matched a loaded JSON; keep
	}
	pngPath := filepath.Join(dir, name)
	if err := os.Remove(pngPath); err != nil {
		log.Printf("server: orphan PNG cleanup: %s: %v", pngPath, err)
	}
}

return out, nil
```

- [ ] **Step 4: Run test, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestScanSessionsDir" -count=1`
Expected: PASS — both the new orphan-removal test and any existing scan tests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/session_persistence.go internal/runtime/server/session_persistence_test.go
git commit -m "Boot scan: remove orphaned PNG thumbnails"
```

---

### Task 9: Shared `internal/thumbnail/` primitive package

**Files:**
- Create: `internal/thumbnail/render.go`
- Create: `internal/thumbnail/render_test.go`
- (Task 19 refactors `internal/runtime/client/screenshot.go` to consume this; Task 11 consumes it server-side.)

**Goal:** One package owning the textrender pipeline + atomic PNG write + aspect-fit downscale, used by both the server lifecycle thumbnail capture and the client's user-initiated screenshot. Avoids duplicating the font detection + renderer setup. The package is pure render — it does not know about sessions, panes, or the desktop tree (those compose at the call site).

- [ ] **Step 1: Write failing tests**

Create `internal/thumbnail/render_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package thumbnail

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	texelcore "github.com/framegrace/texelui/core"
)

func makeTestImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xFF})
		}
	}
	return img
}

func TestRenderGrid_Smoke(t *testing.T) {
	// 4x2 grid of plain ASCII; we just want a valid image back.
	grid := [][]texelcore.Cell{
		{{Ch: 'h'}, {Ch: 'e'}, {Ch: 'l'}, {Ch: 'l'}},
		{{Ch: 'o'}, {Ch: ' '}, {Ch: 't'}, {Ch: 'x'}},
	}
	img, err := RenderGrid(grid)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if img == nil {
		t.Fatalf("nil image")
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		t.Fatalf("zero-sized image: %v", img.Bounds())
	}
}

func TestRenderGrid_EmptyGrid(t *testing.T) {
	if _, err := RenderGrid(nil); err == nil {
		t.Fatalf("expected error on nil grid")
	}
	if _, err := RenderGrid([][]texelcore.Cell{}); err == nil {
		t.Fatalf("expected error on empty grid")
	}
}

func TestWritePNGAtomic_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.png")
	if err := WritePNGAtomic(path, makeTestImage(40, 30)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file not cleaned: stat err=%v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 30 {
		t.Fatalf("decoded dims = %dx%d, want 40x30", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestWritePNGAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.png")
	if err := WritePNGAtomic(path, makeTestImage(20, 20)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WritePNGAtomic(path, makeTestImage(40, 40)); err != nil {
		t.Fatalf("second write: %v", err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	img, _ := png.Decode(f)
	if img.Bounds().Dx() != 40 {
		t.Fatalf("expected overwrite to 40x40, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestDownscaleAspectFit_Wider(t *testing.T) {
	src := makeTestImage(800, 200) // 4:1 — wider than 16:9 (480x270)
	out := DownscaleAspectFit(src, 480, 270)
	if out.Bounds().Dx() != 480 || out.Bounds().Dy() != 270 {
		t.Fatalf("expected 480x270 canvas, got %dx%d", out.Bounds().Dx(), out.Bounds().Dy())
	}
}

func TestDownscaleAspectFit_Taller(t *testing.T) {
	src := makeTestImage(200, 800)
	out := DownscaleAspectFit(src, 480, 270)
	if out.Bounds().Dx() != 480 || out.Bounds().Dy() != 270 {
		t.Fatalf("expected 480x270 canvas, got %dx%d", out.Bounds().Dx(), out.Bounds().Dy())
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/thumbnail/ -count=1`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement the primitive**

Create `internal/thumbnail/render.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/thumbnail/render.go
// Summary: Shared image-rendering primitive used by server lifecycle
// thumbnail capture (Plan F.1) and client user-initiated screenshots.
// Knows about cells; does not know about sessions, panes, or sockets.

package thumbnail

import (
	"errors"
	"fmt"
	"image"
	stddraw "image/draw"
	"image/png"
	"os"

	xdraw "golang.org/x/image/draw"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/graphics/textrender"
)

// ErrEmptyGrid is returned by RenderGrid for nil or zero-row inputs.
var ErrEmptyGrid = errors.New("thumbnail: empty cell grid")

// RenderGrid renders a cell grid to an image using the system text
// renderer (font auto-detected). Callers are responsible for supplying
// a non-empty grid; this function does not synthesize background fill
// for the empty case.
func RenderGrid(grid [][]texelcore.Cell) (image.Image, error) {
	if len(grid) == 0 {
		return nil, ErrEmptyGrid
	}
	if len(grid[0]) == 0 {
		return nil, ErrEmptyGrid
	}
	fontPath, err := textrender.DetectFont()
	if err != nil {
		return nil, fmt.Errorf("thumbnail: font detect: %w", err)
	}
	renderer, err := textrender.New(textrender.Config{FontPath: fontPath})
	if err != nil {
		return nil, fmt.Errorf("thumbnail: renderer: %w", err)
	}
	return renderer.Render(grid), nil
}

// WritePNGAtomic encodes img to path via tmp+rename so a crash mid-
// write doesn't leave a half-PNG. Used both by server lifecycle
// capture and (eventually) by the client screenshot path.
func WritePNGAtomic(path string, img image.Image) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("thumbnail: create %s: %w", tmp, err)
	}
	encErr := png.Encode(f, img)
	syncErr := f.Sync()
	closeErr := f.Close()
	if encErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		switch {
		case encErr != nil:
			return fmt.Errorf("thumbnail: encode: %w", encErr)
		case syncErr != nil:
			return fmt.Errorf("thumbnail: sync: %w", syncErr)
		default:
			return fmt.Errorf("thumbnail: close: %w", closeErr)
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("thumbnail: rename %s: %w", path, err)
	}
	return nil
}

// DownscaleAspectFit returns a (targetW × targetH) RGBA image with src
// drawn into the centred subrect that preserves src's aspect ratio.
// Background outside the scaled rect is left transparent (zero pixels);
// callers wanting an opaque background should fill before passing.
func DownscaleAspectFit(src image.Image, targetW, targetH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()
	if srcW == 0 || srcH == 0 {
		return dst
	}
	srcRatio := float64(srcW) / float64(srcH)
	dstRatio := float64(targetW) / float64(targetH)
	var scaledW, scaledH int
	if srcRatio > dstRatio {
		scaledW = targetW
		scaledH = int(float64(targetW) / srcRatio)
	} else {
		scaledH = targetH
		scaledW = int(float64(targetH) * srcRatio)
	}
	if scaledW < 1 {
		scaledW = 1
	}
	if scaledH < 1 {
		scaledH = 1
	}
	offsetX := (targetW - scaledW) / 2
	offsetY := (targetH - scaledH) / 2
	dstRect := image.Rect(offsetX, offsetY, offsetX+scaledW, offsetY+scaledH)
	xdraw.ApproxBiLinear.Scale(dst, dstRect, src, src.Bounds(), stddraw.Over, nil)
	return dst
}
```

If `golang.org/x/image` is not yet a direct dependency (currently indirect via texelui), `go mod tidy` after adding the import will promote it. Verify with `grep "golang.org/x/image" go.mod`.

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/thumbnail/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/thumbnail/render.go internal/thumbnail/render_test.go go.mod go.sum
git commit -m "Add internal/thumbnail/ shared rendering primitive"
```

---

### Task 10: Server-side thumbnail composition adapter

**Files:**
- Create: `internal/runtime/server/thumbnail_renderer.go`
- Create: `internal/runtime/server/thumbnail_renderer_test.go`
- Modify: `internal/runtime/server/desktop_sink.go` (implement `ThumbnailRenderer` interface, see Task 11)

**Goal:** Bridge between the server's authoritative pane state and the shared rendering primitive. Walks the publisher's `prevBuffers` (per-pane cell snapshots maintained for diff generation) plus the desktop tree to compose a single workspace-wide cell grid, then calls `thumbnail.RenderGrid`. The composer is the only piece of new server-only code that *has* to live in the server package because no client has cross-pane visibility into authoritative state.

The `ThumbnailRenderer` interface is declared in Task 11 (the orchestrator that calls it from Manager hooks). This task implements the production renderer that satisfies that interface; the test stub in Task 11 stands in for unit tests that don't want to wire a full DesktopSink.

- [ ] **Step 1: Write failing tests**

Create `internal/runtime/server/thumbnail_renderer_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	texelcore "github.com/framegrace/texelui/core"
)

// composePaneGrid is the pure helper. The full DesktopSink path is
// covered by integration tests that already wire publisher state.
func TestComposePaneGrid_SinglePane(t *testing.T) {
	pane := paneRender{
		x: 0, y: 0, w: 4, h: 2,
		rows: [][]texelcore.Cell{
			{{Ch: 'a'}, {Ch: 'b'}, {Ch: 'c'}, {Ch: 'd'}},
			{{Ch: 'e'}, {Ch: 'f'}, {Ch: 'g'}, {Ch: 'h'}},
		},
	}
	grid := composePaneGrid(4, 2, []paneRender{pane})
	if len(grid) != 2 || len(grid[0]) != 4 {
		t.Fatalf("dims = %dx%d, want 4x2", len(grid[0]), len(grid))
	}
	if grid[0][0].Ch != 'a' || grid[1][3].Ch != 'h' {
		t.Errorf("content mismatch: grid=%v", grid)
	}
}

func TestComposePaneGrid_TwoPanesSideBySide(t *testing.T) {
	left := paneRender{
		x: 0, y: 0, w: 2, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'L'}, {Ch: '1'}}},
	}
	right := paneRender{
		x: 2, y: 0, w: 2, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'R'}, {Ch: '1'}}},
	}
	grid := composePaneGrid(4, 1, []paneRender{left, right})
	if grid[0][0].Ch != 'L' || grid[0][1].Ch != '1' {
		t.Errorf("left mis-painted: %v", grid[0])
	}
	if grid[0][2].Ch != 'R' || grid[0][3].Ch != '1' {
		t.Errorf("right mis-painted: %v", grid[0])
	}
}

func TestComposePaneGrid_OverflowClipped(t *testing.T) {
	// Pane reports w=10 but only 4 cells of buffer; composer should
	// not panic and should leave the rest blank.
	pane := paneRender{
		x: 0, y: 0, w: 10, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'x'}, {Ch: 'y'}, {Ch: 'z'}}},
	}
	grid := composePaneGrid(10, 1, []paneRender{pane})
	if grid[0][0].Ch != 'x' || grid[0][1].Ch != 'y' || grid[0][2].Ch != 'z' {
		t.Errorf("front-pad mismatch: %v", grid[0])
	}
	for i := 3; i < 10; i++ {
		if grid[0][i].Ch != ' ' && grid[0][i].Ch != 0 {
			t.Errorf("expected blank at col %d, got %q", i, grid[0][i].Ch)
		}
	}
}

func TestComposePaneGrid_OutOfBoundsPaneIgnored(t *testing.T) {
	// Pane positioned outside the workspace: composer drops cells
	// rather than panicking with an index-out-of-range.
	pane := paneRender{
		x: 100, y: 100, w: 4, h: 1,
		rows: [][]texelcore.Cell{{{Ch: 'a'}, {Ch: 'b'}, {Ch: 'c'}, {Ch: 'd'}}},
	}
	_ = composePaneGrid(4, 1, []paneRender{pane}) // should not panic
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestComposePaneGrid" -count=1`
Expected: FAIL — `paneRender`, `composePaneGrid` undefined.

- [ ] **Step 3: Implement the composer + DesktopSink adapter**

Create `internal/runtime/server/thumbnail_renderer.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/thumbnail_renderer.go
// Summary: Composes per-pane buffer snapshots into a single workspace
// cell grid for thumbnail rendering. The composer is the server-only
// glue between the publisher's authoritative state and the shared
// internal/thumbnail render primitive.

package server

import (
	"image"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/internal/thumbnail"
)

// paneRender is the per-pane input to composePaneGrid. Coordinates are
// workspace-relative; rows[y][x] is a cell.
type paneRender struct {
	x, y int
	w, h int
	rows [][]texelcore.Cell
}

// composePaneGrid paints pane buffers onto a (workspaceW × workspaceH)
// cell grid. Out-of-bounds cells are dropped silently. The grid is
// initialised with zero-value Cells (Ch=0, default style) which the
// renderer treats as blanks.
func composePaneGrid(workspaceW, workspaceH int, panes []paneRender) [][]texelcore.Cell {
	grid := make([][]texelcore.Cell, workspaceH)
	for y := 0; y < workspaceH; y++ {
		grid[y] = make([]texelcore.Cell, workspaceW)
	}
	for _, p := range panes {
		for ry, row := range p.rows {
			absY := p.y + ry
			if absY < 0 || absY >= workspaceH {
				continue
			}
			for rx, cell := range row {
				absX := p.x + rx
				if absX < 0 || absX >= workspaceW {
					continue
				}
				grid[absY][absX] = cell
			}
		}
	}
	return grid
}

// renderSessionThumbnail extracts pane buffers from the publisher
// (which already maintains them for diff generation) and renders the
// composed grid to an image via the shared primitive. Returns
// (nil, false) when the session has no renderable content (no panes,
// empty publisher state).
//
// Wired into DesktopSink.RenderSessionThumbnail; the indirection lets
// us unit-test composePaneGrid without a full DesktopSink.
func (s *DesktopSink) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	if s == nil {
		return nil, false
	}
	pub := s.Publisher()
	desktop := s.Desktop()
	if pub == nil || desktop == nil {
		return nil, false
	}
	w, h := desktop.ViewportSize()
	if w <= 0 || h <= 0 {
		return nil, false
	}
	panes := make([]paneRender, 0, 8)
	pub.WalkPanes(func(paneID [16]byte, x, y, paneW, paneH int, rows [][]texelcore.Cell) {
		if len(rows) == 0 {
			return
		}
		panes = append(panes, paneRender{
			x: x, y: y, w: paneW, h: paneH, rows: rows,
		})
	})
	if len(panes) == 0 {
		return nil, false
	}
	grid := composePaneGrid(w, h, panes)
	img, err := thumbnail.RenderGrid(grid)
	if err != nil {
		return nil, false
	}
	return img, true
}
```

Modify `internal/runtime/server/desktop_publisher.go` to expose a `WalkPanes` method that yields `(paneID, x, y, w, h, rows)` from the publisher's existing per-pane state. The exact source of `rows` depends on what `prevBuffers` already holds — read `desktop_publisher.go`'s field declarations and choose the per-pane buffer that already exists for diff generation; do not introduce a new copy. Document in the doc comment that `WalkPanes` is the picker thumbnail's input source so the next reader knows the contract.

If `DesktopEngine` does not currently expose `ViewportSize() (int, int)`, add a thin getter. The publisher already knows the workspace size for diff bounds, so the field exists somewhere in the desktop layer; surface it minimally.

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestComposePaneGrid" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/thumbnail_renderer.go internal/runtime/server/thumbnail_renderer_test.go internal/runtime/server/desktop_publisher.go internal/runtime/server/desktop_sink.go
git commit -m "Server: pane-grid composer + DesktopSink thumbnail adapter"
```

---

### Task 11: `captureThumbnail` orchestrator + ThumbnailRenderer interface

**Files:**
- Create: `internal/runtime/server/thumbnail.go`
- Create: `internal/runtime/server/thumbnail_test.go`

The shared primitive (Task 9) handles the rendering and atomic write. Task 11 is the *orchestrator* — defines the `ThumbnailRenderer` interface (production satisfier is `*DesktopSink` from Task 10), the `captureThumbnail` function that calls renderer → downscale → write, and the test harness for the trigger paths in Task 12.

- [ ] **Step 1: Write failing tests**

Create `internal/runtime/server/thumbnail_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/hex"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// fakeRenderer is a stub ThumbnailRenderer used by trigger tests so
// we don't drag textrender + a real font into the unit harness.
type fakeRenderer struct {
	calls int
}

func (f *fakeRenderer) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	f.calls++
	img := image.NewRGBA(image.Rect(0, 0, 80, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			img.Set(x, y, color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xFF})
		}
	}
	return img, true
}

type skipRenderer struct{}

func (skipRenderer) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	return nil, false
}

func TestCaptureThumbnail_WritesPNG(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0x77}
	r := &fakeRenderer{}
	if err := captureThumbnail(dir, id, r); err != nil {
		t.Fatalf("capture: %v", err)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Fatalf("stat png: %v", err)
	}
}

func TestCaptureThumbnail_RendererSkip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0x88}
	if err := captureThumbnail(dir, id, skipRenderer{}); err != nil {
		t.Fatalf("capture: %v", err)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("expected no PNG when renderer skips, stat err=%v", err)
	}
}

func TestCaptureThumbnail_NilRendererSilent(t *testing.T) {
	if err := captureThumbnail(t.TempDir(), [16]byte{}, nil); err != nil {
		t.Errorf("expected nil error for nil renderer, got %v", err)
	}
}

func TestCaptureThumbnail_RendererPanicSurvives(t *testing.T) {
	// A panicking renderer must not bring down the daemon; the
	// orchestrator wraps the call in defer/recover.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755)
	if err := captureThumbnail(dir, [16]byte{0x99}, panicRenderer{}); err == nil {
		t.Errorf("expected error from panicking renderer")
	}
}

type panicRenderer struct{}

func (panicRenderer) RenderSessionThumbnail(id [16]byte) (image.Image, bool) {
	panic("intentional test panic")
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestCaptureThumbnail" -count=1`
Expected: FAIL — `captureThumbnail`, `ThumbnailRenderer` undefined.

- [ ] **Step 3: Implement orchestrator + interface**

Create `internal/runtime/server/thumbnail.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/thumbnail.go
// Summary: Lifecycle thumbnail capture orchestrator (Plan F.1).
// Usage: Called from Manager.ShutdownSessions and Manager.Close on
//   the last-disconnect transition. Renders via a ThumbnailRenderer
//   (production: *DesktopSink, see Task 10), downscales via the
//   shared internal/thumbnail primitive, and atomically writes to
//   <basedir>/sessions/<id>.png.

package server

import (
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"path/filepath"

	"github.com/framegrace/texelation/internal/thumbnail"
)

// ThumbnailRenderer produces a PNG-suitable image for a given session.
// Implemented by *DesktopSink in production (see Task 10's
// thumbnail_renderer.go); tests inject a stub.
//
// Returns ok=false when the session has nothing meaningful to capture
// (empty workspace, no live buffer) so the caller can skip the disk
// write rather than store an all-black PNG.
type ThumbnailRenderer interface {
	RenderSessionThumbnail(id [16]byte) (image.Image, bool)
}

// captureThumbnail renders id via the renderer (if non-nil), downscales
// to 480×270 via the shared primitive, and writes the result to
// <basedir>/sessions/<id>.png. A panicking renderer is recovered to an
// error so a buggy implementation cannot prevent shutdown. nil renderer
// or empty basedir is a silent no-op.
func captureThumbnail(basedir string, id [16]byte, r ThumbnailRenderer) (retErr error) {
	if r == nil || basedir == "" {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			retErr = fmt.Errorf("thumbnail: renderer panic: %v", rec)
		}
	}()
	img, ok := r.RenderSessionThumbnail(id)
	if !ok {
		return nil
	}
	if img == nil {
		return errors.New("thumbnail: renderer returned ok=true with nil image")
	}
	scaled := thumbnail.DownscaleAspectFit(img, 480, 270)
	pngPath := filepath.Join(basedir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	return thumbnail.WritePNGAtomic(pngPath, scaled)
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestCaptureThumbnail" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/thumbnail.go internal/runtime/server/thumbnail_test.go
git commit -m "Server: captureThumbnail orchestrator + ThumbnailRenderer interface"
```

---

### Task 12: Capture trigger — wire `Close(id)` + `ShutdownSessions`

**Files:**
- Modify: `internal/runtime/server/manager.go` (`thumbRenderer` field, `SetThumbnailRenderer`, modified `Close` + `ShutdownSessions`)
- Modify: `internal/runtime/server/thumbnail_test.go` (append manager-level trigger tests)

The orchestrator + interface ship in Task 11. This task is the manager-side wiring: a renderer field, a setter, and the modified lifecycle methods. **Both `Close` and `ShutdownSessions` are shown in full** so a subagent doesn't have to reconstruct them from prose.

- [ ] **Step 1: Append failing trigger tests**

Append to `internal/runtime/server/thumbnail_test.go`:

```go
import "time"

func TestManager_CaptureOnShutdown(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x55}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	r := &fakeRenderer{}
	m.SetThumbnailRenderer(r)
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("new: %v", err)
	}
	m.ShutdownSessions()
	if r.calls < 1 {
		t.Errorf("expected at least 1 thumbnail render, got %d", r.calls)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Errorf("expected PNG after shutdown, stat err=%v", err)
	}
}

func TestManager_CaptureOnLastDisconnect(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0x66}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	r := &fakeRenderer{}
	m.SetThumbnailRenderer(r)
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("new: %v", err)
	}
	m.Close(id)
	if r.calls < 1 {
		t.Errorf("expected thumbnail render on Close, got %d", r.calls)
	}
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); err != nil {
		t.Errorf("expected PNG after Close, stat err=%v", err)
	}
}

func TestManager_NoCaptureWhenRendererUnset(t *testing.T) {
	dir := t.TempDir()
	id := [16]byte{0xAA}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := m.NewSessionWithID(id); err != nil {
		t.Fatalf("new: %v", err)
	}
	m.Close(id)
	pngPath := filepath.Join(dir, SessionsDirName, hex.EncodeToString(id[:])+".png")
	if _, err := os.Stat(pngPath); !os.IsNotExist(err) {
		t.Errorf("expected no PNG when renderer unset, stat err=%v", err)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestManager_CaptureOn|TestManager_NoCaptureWhenRendererUnset" -count=1`
Expected: FAIL — `SetThumbnailRenderer` undefined; `Close` and `ShutdownSessions` don't fire capture.

- [ ] **Step 3: Add renderer field, setter, and revised lifecycle methods**

In `internal/runtime/server/manager.go`, extend the `Manager` struct:

```go
type Manager struct {
	// ... existing fields ...

	// Plan F.1: lifecycle thumbnail capture. nil = capture disabled
	// (tests + early boot before SetThumbnailRenderer is called).
	thumbRenderer ThumbnailRenderer
}
```

Add the setter:

```go
// SetThumbnailRenderer wires the production renderer (typically the
// *DesktopSink that owns the publisher state — see Task 10) so
// Close(id) and ShutdownSessions can capture lifecycle thumbnails.
// nil renderers skip capture silently — no error, no log spam.
func (m *Manager) SetThumbnailRenderer(r ThumbnailRenderer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.thumbRenderer = r
}
```

Replace the existing `Close(id)` body with the captured-renderer version:

```go
// Close removes the session from the live map and tears it down.
// Plan F.1: also captures a thumbnail just before teardown so the
// next picker invocation has something to show. The capture happens
// OUTSIDE m.mu (PNG encoding holds memory and may sync to disk —
// holding the lock would block every other Manager op for the
// duration). Renderer + basedir are snapshotted under the lock so a
// concurrent SetThumbnailRenderer doesn't race with the read.
//
// Plan D2 17.B: Close registers a per-ID "closing" marker before
// dropping m.mu and clears it after session.Close returns.
// LookupOrRehydrate consults the same marker so a fresh resume for
// the same ID waits out the disk flush instead of constructing a
// new Session pointing at the same on-disk path.
func (m *Manager) Close(id [16]byte) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.sessions, id)
	basedir := m.persistBasedir
	renderer := m.thumbRenderer
	m.markClosing(id)
	m.mu.Unlock()
	defer m.unmarkClosing(id)

	if basedir != "" && renderer != nil {
		if err := captureThumbnail(basedir, id, renderer); err != nil {
			log.Printf("server: close thumbnail %x: %v", id[:4], err)
		}
	}
	session.Close() // disk flush — outside m.mu
}
```

Replace the existing `ShutdownSessions` body:

```go
// ShutdownSessions closes all live sessions and (Plan F.1) captures
// a thumbnail per session before the per-session Close. Capture runs
// OUTSIDE m.mu — see Close for the lock-discipline rationale. Both
// basedir and renderer are snapshotted once under the lock; a
// concurrent SetThumbnailRenderer call after the snapshot is benign
// (we'll just use the older renderer one final time).
func (m *Manager) ShutdownSessions() {
	m.mu.Lock()
	live := m.sessions
	m.sessions = make(map[[16]byte]*Session)
	basedir := m.persistBasedir
	renderer := m.thumbRenderer
	for id := range live {
		m.markClosing(id)
	}
	m.mu.Unlock()

	for id, session := range live {
		if basedir != "" && renderer != nil {
			if err := captureThumbnail(basedir, id, renderer); err != nil {
				log.Printf("server: shutdown thumbnail %x: %v", id[:4], err)
			}
		}
		session.Close() // disk flush — outside m.mu
		m.unmarkClosing(id)
	}
}
```

The `Close` change deletes from `m.sessions` *before* the early-return guard, which fixes a pre-existing minor inefficiency where `delete` ran on the not-found branch (the original code's order was `if ok { delete }; if !ok { return }`). Behavior is preserved — Go's `delete` on a missing key is a no-op.

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestCaptureThumbnail|TestManager_CaptureOn|TestManager_NoCaptureWhenRendererUnset" -count=1`
Expected: PASS.

- [ ] **Step 5: Run the full server suite under -race**

Run: `go test -race ./internal/runtime/server/ -count=1`
Expected: PASS. The renderer snapshot pattern matches the existing basedir-snapshot pattern in `EnablePersistence` and avoids introducing races.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/manager.go internal/runtime/server/thumbnail_test.go
git commit -m "Server: capture thumbnails on Close + ShutdownSessions"
```

---

### Task 13: Connection handlers — list + recover (with `connection.manager` wiring)

**Files:**
- Modify: `internal/runtime/server/connection.go` (add `manager *Manager` field; thread through `newConnection`)
- Modify: `internal/runtime/server/server.go` (pass manager to `newConnection`)
- Modify: every existing test that calls `newConnection` (~16 files; mechanical signature update)
- Create: `internal/runtime/server/connection_session_picker.go`
- Modify: `internal/runtime/server/connection_handler.go` (dispatch new types)
- Test: `internal/runtime/server/connection_session_picker_test.go`

The handlers in this task and Task 14 call `c.manager.X()`, but the existing `connection` struct has no `manager` field. Step 0 is the structural wiring; Step 1 onwards is the handler work.

- [ ] **Step 0: Wire `connection.manager` field**

In `internal/runtime/server/connection.go`, add a field to the `connection` struct:

```go
type connection struct {
	// ... existing fields ...

	// Plan F.1: picker handlers reach into the manager for session
	// listing, rename, delete, and rehydrate. Wired at construction
	// time by newConnection; never reassigned. nil only in tests
	// that use a hand-constructed connection without a manager —
	// those tests must not exercise picker handlers.
	manager *Manager
}
```

Update `newConnection` (read its current signature in `connection.go` first; whatever it is, append a `*Manager` parameter):

```go
func newConnection(/* existing args */, mgr *Manager) *connection {
	return &connection{
		// ... existing fields ...
		manager: mgr,
	}
}
```

Update `internal/runtime/server/server.go` (the only production call site of `newConnection`) to pass `s.manager`. Then run:

```bash
grep -rln "newConnection(" internal/runtime/server/ | grep _test.go
```

Each test file needs the new argument added. Most tests construct a `*Manager` already (often via `NewManager()`); pass it through. Tests that don't construct a manager should pass `nil` — they're not exercising picker handlers, and the handler code is gated by Task 14's tests, not these.

Run:

```bash
go build ./internal/runtime/server/...
go test -run NoneShouldMatchJustCheckCompile ./internal/runtime/server/ -count=1
```

Expected: build clean (some tests may still fail; the goal is just compile).

Commit this structural change separately so the diff against the picker handlers stays readable:

```bash
git add internal/runtime/server/connection.go internal/runtime/server/server.go internal/runtime/server/*_test.go
git commit -m "Connection: add manager *Manager field for picker handlers"
```

- [ ] **Step 1: Write failing tests**

Create `internal/runtime/server/connection_session_picker_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// roundTripPicker is a small helper that drives a request through
// handleMessage and decodes the response. The test harness uses a
// memconn-backed connection; existing pattern from
// connection_rehydrate_resume_test.go.
//
// We use the same nopSink defined in test_helpers_test.go (or
// equivalent) to bypass the desktop wiring — picker handlers don't
// touch the desktop.

func TestHandleListSessions_Empty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, sink := newPickerTestConn(t, m)
	defer conn.cleanup()
	conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	resp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	got, err := protocol.DecodeListSessionsResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Stored) != 0 || len(got.Live) != 0 {
		t.Fatalf("expected empty, got Stored=%d Live=%d", len(got.Stored), len(got.Live))
	}
	_ = sink
}

func TestHandleListSessions_StoredPopulated(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id1 := [16]byte{0x01}
	id2 := [16]byte{0x02}
	for i, fixture := range []*StoredSession{
		{SchemaVersion: StoredSessionSchemaVersion, SessionID: id1, LastActive: time.Unix(100, 0), Label: "older", PaneCount: 1},
		{SchemaVersion: StoredSessionSchemaVersion, SessionID: id2, LastActive: time.Unix(200, 0), Label: "newer", PaneCount: 2},
	} {
		data, _ := json.Marshal(fixture)
		path := filepath.Join(sessions, hex.EncodeToString(fixture.SessionID[:])+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	resp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	got, err := protocol.DecodeListSessionsResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Stored) != 2 {
		t.Fatalf("expected 2 stored, got %d", len(got.Stored))
	}
	if got.Stored[0].Label != "newer" {
		t.Errorf("expected newer first, got %q", got.Stored[0].Label)
	}
}

func TestHandleRecoverSession_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := [16]byte{0xAA}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		Label:         "rec-me",
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, err := protocol.EncodeRecoverSessionRequest(protocol.RecoverSessionRequest{SessionID: id})
	if err != nil {
		t.Fatalf("encode req: %v", err)
	}
	conn.send(protocol.MsgRecoverSession, body)
	// Response should be a ConnectAccept with the recovered session ID.
	resp := conn.expectResponse(t, protocol.MsgConnectAccept)
	accept, err := protocol.DecodeConnectAccept(resp)
	if err != nil {
		t.Fatalf("decode accept: %v", err)
	}
	if accept.SessionID != id {
		t.Fatalf("accept SessionID = %x, want %x", accept.SessionID, id)
	}
}

func TestHandleRecoverSession_UnknownID(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeRecoverSessionRequest(protocol.RecoverSessionRequest{SessionID: [16]byte{0xFF}})
	conn.send(protocol.MsgRecoverSession, body)
	// Server should respond with MsgError.
	resp := conn.expectResponse(t, protocol.MsgError)
	got, err := protocol.DecodeErrorFrame(resp)
	if err != nil {
		t.Fatalf("decode error frame: %v", err)
	}
	if got.Message == "" {
		t.Errorf("expected non-empty error message")
	}
}

func mustEncodeListSessionsRequest(t *testing.T) []byte {
	t.Helper()
	out, err := protocol.EncodeListSessionsRequest(protocol.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}
```

`newPickerTestConn` is a helper modelled on `internal/runtime/server/connection_rehydrate_resume_test.go` (which already constructs `*connection` against a `net.Pipe` pair + `nopSink`). The picker variant is simpler because it doesn't need the resume-handshake choreography.

```go
// internal/runtime/server/connection_session_picker_test.go
// (helper, declared once near the top of the file)

import (
	"net"
	"sync"
	// ... other imports ...

	"github.com/framegrace/texelation/protocol"
)

type pickerTestConn struct {
	t          *testing.T
	conn       *connection
	clientEnd  net.Conn
	serverEnd  net.Conn
	closeOnce  sync.Once
}

// newPickerTestConn wires a *connection to a net.Pipe so the test
// can drive handleMessage synchronously and read responses off the
// client side. Reuses the in-package nopSink (defined in
// test_helpers_test.go); creates a fresh session via m.NewSession.
//
// The returned *connection has no goroutine of its own — tests call
// p.send to invoke handleMessage directly. This deliberately differs
// from the production read loop to keep tests deterministic.
func newPickerTestConn(t *testing.T, m *Manager) (*pickerTestConn, *nopSink) {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()
	sess, err := m.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	sink := &nopSink{}
	c := newConnection(serverEnd, sess, sink, m) // signature from Step 0
	p := &pickerTestConn{
		t:         t,
		conn:      c,
		clientEnd: clientEnd,
		serverEnd: serverEnd,
	}
	return p, sink
}

// send invokes handleMessage with the supplied message type + payload,
// running it on the test's goroutine so any error surfaces immediately.
func (p *pickerTestConn) send(typ protocol.MessageType, payload []byte) {
	p.t.Helper()
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      typ,
		Flags:     protocol.FlagChecksum,
		SessionID: p.conn.session.ID(),
	}
	if err := p.conn.handleMessage("test", hdr, payload); err != nil {
		p.t.Fatalf("handleMessage(%d): %v", typ, err)
	}
}

// expectResponse reads the next protocol message off the client end
// of the pipe and asserts its type matches `want`. Returns the payload.
func (p *pickerTestConn) expectResponse(t *testing.T, want protocol.MessageType) []byte {
	t.Helper()
	hdr, payload, err := protocol.ReadMessage(p.clientEnd)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if hdr.Type != want {
		t.Fatalf("response type = %d, want %d", hdr.Type, want)
	}
	return payload
}

func (p *pickerTestConn) cleanup() {
	p.closeOnce.Do(func() {
		_ = p.clientEnd.Close()
		_ = p.serverEnd.Close()
	})
}
```

Two concrete things to verify when implementing:
1. `newConnection`'s real signature — adapt the call site if production wires extra arguments (fetch-pending broadcaster etc.).
2. `nopSink` type lives in the test corpus already — confirm via `grep "nopSink" internal/runtime/server/*_test.go` before declaring a new one.

`net.Pipe` is synchronous — `c.writeMessage` from a handler returns only after `clientEnd` has accepted the write, so `expectResponse` won't deadlock. If the handler writes multiple frames, call `expectResponse` once per frame in order.

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestHandleListSessions|TestHandleRecoverSession" -count=1`
Expected: FAIL — `MsgListSessions` not dispatched, no handler returns response.

- [ ] **Step 3: Implement the handlers**

Create `internal/runtime/server/connection_session_picker.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_session_picker.go
// Summary: Picker request handlers (Plan F.1) — listing, recovery,
//   rename, delete, and lazy thumbnail fetch.

package server

import (
	"errors"
	"fmt"

	"github.com/framegrace/texelation/protocol"
)

func (c *connection) handleListSessions(payload []byte) error {
	if _, err := protocol.DecodeListSessionsRequest(payload); err != nil {
		return fmt.Errorf("decode list-sessions: %w", err)
	}
	resp := protocol.ListSessionsResponse{
		Live:   c.manager.LiveSummaries(),
		Stored: c.manager.StoredSummaries(),
	}
	body, err := protocol.EncodeListSessionsResponse(resp)
	if err != nil {
		return fmt.Errorf("encode list-sessions: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgListSessionsResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}

func (c *connection) handleRecoverSession(payload []byte) error {
	req, err := protocol.DecodeRecoverSessionRequest(payload)
	if err != nil {
		return fmt.Errorf("decode recover-session: %w", err)
	}
	if req.NewLabel != "" {
		if rerr := c.manager.RenameStored(req.SessionID, req.NewLabel); rerr != nil && !errors.Is(rerr, ErrSessionNotFound) {
			return c.sendErrorFrame(rerr)
		}
	}
	sess, _, err := c.manager.LookupOrRehydrate(req.SessionID)
	if err != nil {
		return c.sendErrorFrame(err)
	}
	c.session = sess
	accept := protocol.ConnectAccept{
		SessionID:       sess.ID(),
		ResumeSupported: true,
	}
	body, err := protocol.EncodeConnectAccept(accept)
	if err != nil {
		return fmt.Errorf("encode accept: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgConnectAccept,
		Flags:     protocol.FlagChecksum,
		SessionID: sess.ID(),
	}
	return c.writeMessage(hdr, body)
}

// sendErrorFrame is a small helper so picker handlers can surface an
// error to the client without aborting the connection.
func (c *connection) sendErrorFrame(err error) error {
	body, encErr := protocol.EncodeErrorFrame(protocol.ErrorFrame{
		Code:    1, // generic; picker errors don't need a code taxonomy in F.1
		Message: err.Error(),
	})
	if encErr != nil {
		return fmt.Errorf("encode error frame: %w", encErr)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgError,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}
```

Inspect `connection.go` for the actual `writeMessage` signature (it's already used by other handlers). The `connection` struct already exposes `manager` — verify by reading `connection.go`'s field declarations; if the field is named differently, adjust to match.

In `internal/runtime/server/connection_handler.go`, dispatch the new message types. Add to the switch in `handleMessage` (before the `default:` case):

```go
case protocol.MsgListSessions:
	if err := c.handleListSessions(payload); err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
case protocol.MsgRecoverSession:
	if err := c.handleRecoverSession(payload); err != nil {
		return fmt.Errorf("recover session: %w", err)
	}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestHandleListSessions|TestHandleRecoverSession" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/connection_session_picker.go internal/runtime/server/connection_session_picker_test.go internal/runtime/server/connection_handler.go
git commit -m "Connection: list-sessions + recover-session handlers"
```

---

### Task 14: Connection handlers — rename + delete + fetch-thumbnail (with size cap + SessionOpResponse)

**Files:**
- Modify: `internal/runtime/server/connection_session_picker.go` (append handlers)
- Modify: `internal/runtime/server/connection_handler.go` (dispatch)
- Modify: `internal/runtime/server/connection_session_picker_test.go` (append tests)

This task uses the dedicated `MsgSessionOpResponse` (declared in Task 1, encoded in Task 4) as the rename/delete ack. The fetch-thumbnail handler caps file size at 1 MiB to prevent local DoS via a hand-written oversized PNG.

- [ ] **Step 1: Write failing tests**

Append to `internal/runtime/server/connection_session_picker_test.go`:

```go
func TestHandleRenameSession_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x10}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		Label:         "before",
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeRenameSessionRequest(protocol.RenameSessionRequest{SessionID: id, NewLabel: "after"})
	conn.send(protocol.MsgRenameSession, body)
	resp := conn.expectResponse(t, protocol.MsgSessionOpResponse)
	got, err := protocol.DecodeSessionOpResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.OpType != protocol.OpRename || !got.OK {
		t.Fatalf("expected OpRename OK=true, got %#v", got)
	}
	// Confirm via list.
	conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	listResp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	listGot, _ := protocol.DecodeListSessionsResponse(listResp)
	if len(listGot.Stored) != 1 || listGot.Stored[0].Label != "after" {
		t.Fatalf("expected rename applied; got %#v", listGot.Stored)
	}
}

func TestHandleRenameSession_UnknownID(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeRenameSessionRequest(protocol.RenameSessionRequest{SessionID: [16]byte{0xFF}, NewLabel: "x"})
	conn.send(protocol.MsgRenameSession, body)
	resp := conn.expectResponse(t, protocol.MsgSessionOpResponse)
	got, _ := protocol.DecodeSessionOpResponse(resp)
	if got.OK || got.Error == "" {
		t.Errorf("expected OK=false with error, got %#v", got)
	}
	if got.OpType != protocol.OpRename {
		t.Errorf("OpType = %d, want OpRename", got.OpType)
	}
}

func TestHandleDeleteSession_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x20}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeDeleteSessionRequest(protocol.DeleteSessionRequest{SessionID: id})
	conn.send(protocol.MsgDeleteSession, body)
	resp := conn.expectResponse(t, protocol.MsgSessionOpResponse)
	got, _ := protocol.DecodeSessionOpResponse(resp)
	if got.OpType != protocol.OpDelete || !got.OK {
		t.Fatalf("expected OpDelete OK=true, got %#v", got)
	}
	conn.send(protocol.MsgListSessions, mustEncodeListSessionsRequest(t))
	listResp := conn.expectResponse(t, protocol.MsgListSessionsResponse)
	listGot, _ := protocol.DecodeListSessionsResponse(listResp)
	if len(listGot.Stored) != 0 {
		t.Fatalf("expected empty after delete; got %d", len(listGot.Stored))
	}
}

func TestHandleFetchThumbnail_HappyPath(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x30}
	stored := &StoredSession{
		SchemaVersion: StoredSessionSchemaVersion,
		SessionID:     id,
		LastActive:    time.Unix(100, 0),
		PaneCount:     1,
	}
	data, _ := json.Marshal(stored)
	os.WriteFile(filepath.Join(sessions, hex.EncodeToString(id[:])+".json"), data, 0o644)
	pngPath := filepath.Join(sessions, hex.EncodeToString(id[:])+".png")
	pngContent := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	os.WriteFile(pngPath, pngContent, 0o644)

	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: id})
	conn.send(protocol.MsgFetchThumbnail, body)
	resp := conn.expectResponse(t, protocol.MsgFetchThumbnailResponse)
	got, err := protocol.DecodeFetchThumbnailResponse(resp)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.OK {
		t.Fatalf("expected OK=true, got Error=%q", got.Error)
	}
	if string(got.PNG) != string(pngContent) {
		t.Fatalf("PNG mismatch")
	}
}

func TestHandleFetchThumbnail_Missing(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, SessionsDirName), 0o755)
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: [16]byte{0x99}})
	conn.send(protocol.MsgFetchThumbnail, body)
	resp := conn.expectResponse(t, protocol.MsgFetchThumbnailResponse)
	got, _ := protocol.DecodeFetchThumbnailResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false for missing PNG")
	}
}

func TestHandleFetchThumbnail_RefusesOversize(t *testing.T) {
	// A 2 MiB file at the predictable path must be refused with
	// OK=false (DoS prevention). 480x270 PNGs are well under 100 KiB
	// in practice; 1 MiB cap is generous.
	dir := t.TempDir()
	sessions := filepath.Join(dir, SessionsDirName)
	os.MkdirAll(sessions, 0o755)
	id := [16]byte{0x40}
	pngPath := filepath.Join(sessions, hex.EncodeToString(id[:])+".png")
	if err := os.WriteFile(pngPath, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatalf("write big png: %v", err)
	}
	m := NewManager()
	if err := m.EnablePersistence(dir, 25*time.Millisecond); err != nil {
		t.Fatalf("enable: %v", err)
	}
	conn, _ := newPickerTestConn(t, m)
	defer conn.cleanup()
	body, _ := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: id})
	conn.send(protocol.MsgFetchThumbnail, body)
	resp := conn.expectResponse(t, protocol.MsgFetchThumbnailResponse)
	got, _ := protocol.DecodeFetchThumbnailResponse(resp)
	if got.OK {
		t.Errorf("expected OK=false for oversized PNG, got OK=true with %d bytes", len(got.PNG))
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/runtime/server/ -run "TestHandleRenameSession|TestHandleDeleteSession|TestHandleFetchThumbnail" -count=1`
Expected: FAIL — handlers undefined / not dispatched.

- [ ] **Step 3: Implement handlers**

Append to `internal/runtime/server/connection_session_picker.go`:

```go
// maxThumbnailBytes caps PNG sidecar reads to defend against a local
// attacker writing a giant file at the predictable on-disk path. A
// 480×270 PNG at high quality is well under 100 KiB; 1 MiB leaves
// generous headroom while keeping the worst-case server allocation
// bounded.
const maxThumbnailBytes int64 = 1 << 20

// handleRenameSession applies an inline label edit. Replies with a
// MsgSessionOpResponse{OpType: OpRename, OK: ...} so the picker can
// correlate against the op it issued (no conflation with other
// responses on the connection).
func (c *connection) handleRenameSession(payload []byte) error {
	req, err := protocol.DecodeRenameSessionRequest(payload)
	if err != nil {
		return fmt.Errorf("decode rename: %w", err)
	}
	if err := c.manager.RenameStored(req.SessionID, req.NewLabel); err != nil {
		return c.sendOpResponse(protocol.OpRename, false, err.Error())
	}
	return c.sendOpResponse(protocol.OpRename, true, "")
}

func (c *connection) handleDeleteSession(payload []byte) error {
	req, err := protocol.DecodeDeleteSessionRequest(payload)
	if err != nil {
		return fmt.Errorf("decode delete: %w", err)
	}
	if err := c.manager.DeleteStored(req.SessionID); err != nil {
		return c.sendOpResponse(protocol.OpDelete, false, err.Error())
	}
	return c.sendOpResponse(protocol.OpDelete, true, "")
}

func (c *connection) handleFetchThumbnail(payload []byte) error {
	req, err := protocol.DecodeFetchThumbnailRequest(payload)
	if err != nil {
		return fmt.Errorf("decode fetch-thumb: %w", err)
	}
	pngPath := c.manager.sessionPNGPath(req.SessionID)
	if pngPath == "" {
		return c.sendThumbnailResponse(false, "thumbnail unavailable", nil)
	}
	info, err := os.Stat(pngPath)
	if err != nil {
		// Don't leak the path; client doesn't need it.
		if os.IsNotExist(err) {
			return c.sendThumbnailResponse(false, "thumbnail not found", nil)
		}
		log.Printf("server: fetch thumbnail %x stat: %v", req.SessionID[:4], err)
		return c.sendThumbnailResponse(false, "thumbnail io error", nil)
	}
	if info.Size() > maxThumbnailBytes {
		log.Printf("server: fetch thumbnail %x: refusing %d bytes (cap %d)", req.SessionID[:4], info.Size(), maxThumbnailBytes)
		return c.sendThumbnailResponse(false, "thumbnail too large", nil)
	}
	data, err := os.ReadFile(pngPath)
	if err != nil {
		log.Printf("server: fetch thumbnail %x read: %v", req.SessionID[:4], err)
		return c.sendThumbnailResponse(false, "thumbnail io error", nil)
	}
	return c.sendThumbnailResponse(true, "", data)
}

func (c *connection) sendThumbnailResponse(ok bool, errMsg string, png []byte) error {
	body, err := protocol.EncodeFetchThumbnailResponse(protocol.FetchThumbnailResponse{
		OK:    ok,
		Error: errMsg,
		PNG:   png,
	})
	if err != nil {
		return fmt.Errorf("encode fetch-thumb response: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgFetchThumbnailResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}

// sendOpResponse confirms a rename/delete with the typed
// SessionOpResponse envelope. The picker correlates by OpType so it
// can ignore unrelated frames that arrive on the connection.
func (c *connection) sendOpResponse(op protocol.SessionOpKind, ok bool, errMsg string) error {
	body, err := protocol.EncodeSessionOpResponse(protocol.SessionOpResponse{
		OpType: op,
		OK:     ok,
		Error:  errMsg,
	})
	if err != nil {
		return fmt.Errorf("encode op response: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgSessionOpResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}
```

Add `"log"` and `"os"` to the imports.

In `connection_handler.go`, add the dispatch cases:

```go
case protocol.MsgRenameSession:
	if err := c.handleRenameSession(payload); err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
case protocol.MsgDeleteSession:
	if err := c.handleDeleteSession(payload); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
case protocol.MsgFetchThumbnail:
	if err := c.handleFetchThumbnail(payload); err != nil {
		return fmt.Errorf("fetch thumbnail: %w", err)
	}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/runtime/server/ -run "TestHandleRenameSession|TestHandleDeleteSession|TestHandleFetchThumbnail" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/connection_session_picker.go internal/runtime/server/connection_session_picker_test.go internal/runtime/server/connection_handler.go
git commit -m "Connection: rename/delete/fetch-thumbnail picker handlers"
```

---

### Task 15: Picker — ASCII layout algorithm (pure functions)

**Files:**
- Create: `cmd/texelation/boot/picker_ascii.go`
- Test: `cmd/texelation/boot/picker_ascii_test.go`

- [ ] **Step 1: Write failing tests**

Create `cmd/texelation/boot/picker_ascii_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"strings"
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestRenderASCIILayout_SinglePane(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone}
	grid := renderASCIILayoutGrid(20, 8, root)
	if len(grid) != 8 || len(grid[0]) != 20 {
		t.Fatalf("dims = %dx%d, want 20x8", len(grid[0]), len(grid))
	}
	// Top-left corner, top-right corner, bottom-left, bottom-right.
	if grid[0][0] != '┌' {
		t.Errorf("top-left = %q, want ┌", grid[0][0])
	}
	if grid[0][19] != '┐' {
		t.Errorf("top-right = %q, want ┐", grid[0][19])
	}
	if grid[7][0] != '└' {
		t.Errorf("bottom-left = %q, want └", grid[7][0])
	}
	if grid[7][19] != '┘' {
		t.Errorf("bottom-right = %q, want ┘", grid[7][19])
	}
}

func TestRenderASCIILayout_HorizontalSplit(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitHorizontal,
		SplitRatios: []float32{0.5, 0.5},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(20, 8, root)
	// Find the divider row — should have horizontal lines spanning
	// the inner area.
	dividerCount := 0
	for y := 1; y < 7; y++ {
		// horizontal split has a horizontal line at the split row
		row := string(grid[y])
		if strings.Count(row, "─") > 5 {
			dividerCount++
		}
	}
	if dividerCount == 0 {
		t.Errorf("expected at least one horizontal divider row in:\n%s", debugGrid(grid))
	}
}

func TestRenderASCIILayout_VerticalSplit(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitVertical,
		SplitRatios: []float32{0.5, 0.5},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(20, 8, root)
	// Vertical split: with leftW = int(20 * 0.5) = 10, the right
	// child draws starting at x=9 (border-sharing), so the divider
	// column is 9, not 10. The algorithm comments document this; the
	// test pins it explicitly.
	dividerCol := 9
	verticalAt := 0
	for y := 1; y < 7; y++ {
		if grid[y][dividerCol] == '│' {
			verticalAt++
		}
	}
	if verticalAt == 0 {
		t.Errorf("expected vertical divider at col %d in:\n%s", dividerCol, debugGrid(grid))
	}
}

func TestRenderASCIILayout_NWaySplit(t *testing.T) {
	// 3-way horizontal split (real TreeNodeSnapshot supports n>=2
	// children — texel/tree.go produces these).
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitHorizontal,
		SplitRatios: []float32{0.34, 0.33, 0.33},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
			{PaneIndex: 2, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(20, 12, root)
	// Should not panic and should produce more than one horizontal
	// divider row (one per inter-child boundary, so 2 dividers).
	dividerRows := 0
	for y := 1; y < 11; y++ {
		if strings.Count(string(grid[y]), "─") > 5 {
			dividerRows++
		}
	}
	if dividerRows < 2 {
		t.Errorf("expected ≥ 2 divider rows for 3-way split, got %d:\n%s", dividerRows, debugGrid(grid))
	}
}

func TestRenderASCIILayout_NilRoot(t *testing.T) {
	grid := renderASCIILayoutGrid(20, 8, nil)
	// Should still draw a single bordered box.
	if grid[0][0] != '┌' {
		t.Errorf("nil root should fall back to single box; got %q at top-left", grid[0][0])
	}
}

func TestRenderASCIILayout_BelowMinSize(t *testing.T) {
	root := &protocol.TreeNodeSnapshot{
		PaneIndex:   -1,
		Split:       protocol.SplitHorizontal,
		SplitRatios: []float32{0.5, 0.5},
		Children: []protocol.TreeNodeSnapshot{
			{PaneIndex: 0, Split: protocol.SplitNone},
			{PaneIndex: 1, Split: protocol.SplitNone},
		},
	}
	grid := renderASCIILayoutGrid(6, 3, root)
	// 6×3 is below the recursive split threshold; should fall back
	// to single-box rendering of the outer container.
	if grid[0][0] != '┌' {
		t.Errorf("expected single-box fallback")
	}
}

func debugGrid(grid [][]rune) string {
	var b strings.Builder
	for _, row := range grid {
		b.WriteString(string(row))
		b.WriteByte('\n')
	}
	return b.String()
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./cmd/texelation/boot/ -run "TestRenderASCIILayout" -count=1`
Expected: FAIL — `renderASCIILayoutGrid` undefined.

- [ ] **Step 3: Implement the algorithm**

Create `cmd/texelation/boot/picker_ascii.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_ascii.go
// Summary: Pure-function tree-snapshot to box-drawing characters.
// Used as the fallback render path for thumbnails on terminals
// without graphics support, and as the placeholder while a Kitty
// thumbnail fetch is in flight.

package boot

import "github.com/framegrace/texelation/protocol"

const (
	asciiMinW = 8
	asciiMinH = 4
)

// renderASCIILayoutGrid returns a (h × w) rune matrix containing the
// box-drawing representation of root. Sub-minimum-size rects collapse
// to a single bordered box; nil roots also fall back to a single box
// so callers don't have to special-case missing layouts.
func renderASCIILayoutGrid(w, h int, root *protocol.TreeNodeSnapshot) [][]rune {
	grid := makeBlankGrid(w, h)
	if root == nil || w < asciiMinW || h < asciiMinH {
		drawSingleBox(grid, 0, 0, w, h)
		return grid
	}
	drawNode(grid, 0, 0, w, h, root)
	return grid
}

func makeBlankGrid(w, h int) [][]rune {
	grid := make([][]rune, h)
	for y := 0; y < h; y++ {
		grid[y] = make([]rune, w)
		for x := 0; x < w; x++ {
			grid[y][x] = ' '
		}
	}
	return grid
}

func drawNode(grid [][]rune, x, y, w, h int, n *protocol.TreeNodeSnapshot) {
	if n.Split == protocol.SplitNone || len(n.Children) < 2 || w < asciiMinW || h < asciiMinH {
		drawSingleBox(grid, x, y, w, h)
		return
	}
	ratios := normaliseRatios(n.SplitRatios, len(n.Children))
	if n.Split == protocol.SplitHorizontal {
		// Walk children in order, allocating rows per ratio. Each
		// child shares its bottom border with the next child's top
		// border (the -1 / +1 dance), so the visual divider sits on
		// a single row and shows '─' from each side's drawSingleBox.
		cursorY := y
		remaining := h
		for i, child := range n.Children {
			var sliceH int
			if i == len(n.Children)-1 {
				sliceH = remaining
			} else {
				sliceH = int(float32(h) * ratios[i])
				if sliceH < asciiMinH {
					sliceH = asciiMinH
				}
				if sliceH > remaining-asciiMinH*(len(n.Children)-i-1) {
					sliceH = remaining - asciiMinH*(len(n.Children)-i-1)
				}
			}
			drawNode(grid, x, cursorY, w, sliceH, &child)
			cursorY += sliceH - 1
			remaining -= sliceH - 1
		}
		return
	}
	// Vertical split: walk children left-to-right.
	cursorX := x
	remaining := w
	for i, child := range n.Children {
		var sliceW int
		if i == len(n.Children)-1 {
			sliceW = remaining
		} else {
			sliceW = int(float32(w) * ratios[i])
			if sliceW < asciiMinW {
				sliceW = asciiMinW
			}
			if sliceW > remaining-asciiMinW*(len(n.Children)-i-1) {
				sliceW = remaining - asciiMinW*(len(n.Children)-i-1)
			}
		}
		drawNode(grid, cursorX, y, sliceW, h, &child)
		cursorX += sliceW - 1
		remaining -= sliceW - 1
	}
}

// normaliseRatios returns a slice of len(childCount) ratios summing to
// ~1.0. If the input is missing/short or doesn't sum, it falls back to
// equal allocation so n-way splits never produce zero-width children.
func normaliseRatios(ratios []float32, count int) []float32 {
	out := make([]float32, count)
	if len(ratios) >= count {
		var sum float32
		for i := 0; i < count; i++ {
			out[i] = ratios[i]
			sum += ratios[i]
		}
		if sum > 0 {
			for i := range out {
				out[i] /= sum
			}
			return out
		}
	}
	for i := range out {
		out[i] = 1.0 / float32(count)
	}
	return out
}

func drawSingleBox(grid [][]rune, x, y, w, h int) {
	if w < 2 || h < 2 {
		return
	}
	maxY := y + h - 1
	maxX := x + w - 1
	for cy := y; cy <= maxY; cy++ {
		for cx := x; cx <= maxX; cx++ {
			if cy < 0 || cy >= len(grid) || cx < 0 || cx >= len(grid[cy]) {
				continue
			}
			switch {
			case cy == y && cx == x:
				grid[cy][cx] = '┌'
			case cy == y && cx == maxX:
				grid[cy][cx] = '┐'
			case cy == maxY && cx == x:
				grid[cy][cx] = '└'
			case cy == maxY && cx == maxX:
				grid[cy][cx] = '┘'
			case cy == y || cy == maxY:
				if grid[cy][cx] == ' ' {
					grid[cy][cx] = '─'
				}
			case cx == x || cx == maxX:
				if grid[cy][cx] == ' ' {
					grid[cy][cx] = '│'
				}
			}
		}
	}
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./cmd/texelation/boot/ -run "TestRenderASCIILayout" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/texelation/boot/picker_ascii.go cmd/texelation/boot/picker_ascii_test.go
git commit -m "Picker: ASCII tree-layout fallback render"
```

---

### Task 16: Picker — state machine, navigation, render (with error surfacing)

**Files:**
- Create: `cmd/texelation/boot/picker.go`
- Create: `cmd/texelation/boot/picker_input.go`
- Create: `cmd/texelation/boot/picker_render.go`
- Test: `cmd/texelation/boot/picker_test.go`

This task wires the Picker with `mu sync.Mutex` declared up-front (used in Task 17 for thumbnail fetches), an `errMsg` field for surfacing operation failures, and modal behavior that does NOT close the picker when Recover/Rename/Delete errors. The user sees a banner and can retry.

- [ ] **Step 1: Write failing tests**

Create `cmd/texelation/boot/picker_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"errors"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelation/protocol"
)

func TestPicker_RendersStoredCards(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)
	p := NewPicker(screen, &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0x01}, Label: "alpha", LastActive: 100, PaneCount: 1},
				{SessionID: [16]byte{0x02}, Label: "beta", LastActive: 50, PaneCount: 2},
			},
		},
	})
	p.RefreshCatalog()
	p.Render()
	contents, w, h := screenContents(screen)
	body := contentsToString(contents, w, h)
	if !contains(body, "alpha") {
		t.Errorf("expected 'alpha' in render:\n%s", body)
	}
	if !contains(body, "beta") {
		t.Errorf("expected 'beta' in render:\n%s", body)
	}
}

func TestPicker_NavigationDown(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	p := NewPicker(screen, &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0x01}, Label: "first"},
				{SessionID: [16]byte{0x02}, Label: "second"},
			},
		},
	})
	p.RefreshCatalog()
	if got := p.SelectedIdx(); got != 0 {
		t.Fatalf("initial selectedIdx = %d, want 0", got)
	}
	p.HandleKey(tcell.KeyDown, 0, 0)
	if got := p.SelectedIdx(); got != 1 {
		t.Fatalf("after down: selectedIdx = %d, want 1", got)
	}
	// Beyond the end clamps.
	p.HandleKey(tcell.KeyDown, 0, 0)
	if got := p.SelectedIdx(); got != 1 {
		t.Errorf("clamp: selectedIdx = %d, want 1", got)
	}
}

func TestPicker_EnterDispatchesRecover(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0xCC}, Label: "pickme"},
			},
		},
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.HandleKey(tcell.KeyEnter, 0, 0)
	if !fc.recoverCalled {
		t.Errorf("expected RecoverSession dispatch on Enter")
	}
	if fc.recoverID != [16]byte{0xCC} {
		t.Errorf("recoverID = %x, want CC", fc.recoverID)
	}
}

func TestPicker_NewKeyDispatchesNew(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	fc := &fakeClient{response: protocol.ListSessionsResponse{}}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.HandleKey(0, 'n', 0)
	if !fc.newCalled {
		t.Errorf("expected fresh-session dispatch on 'n'")
	}
}

func TestPicker_RecoverError_StaysOpen(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0xCC}, Label: "broken"}},
		},
		recoverErr: errors.New("session evicted"),
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.HandleKey(tcell.KeyEnter, 0, 0)
	if p.Done() {
		t.Errorf("picker exited despite Recover error")
	}
	if p.errMsg == "" {
		t.Errorf("expected errMsg set after Recover failure")
	}
}

func TestPicker_RefreshCatalogError_PreservesPriorList(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0x01}, Label: "alpha"}},
		},
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	if len(p.response.Stored) != 1 {
		t.Fatalf("setup: expected 1 stored, got %d", len(p.response.Stored))
	}
	// Now force RefreshCatalog to fail.
	fc.listErr = errors.New("socket dropped")
	p.RefreshCatalog()
	if len(p.response.Stored) != 1 {
		t.Errorf("expected prior list preserved on error, got %d", len(p.response.Stored))
	}
	if p.errMsg == "" {
		t.Errorf("expected errMsg set on RefreshCatalog error")
	}
}

func TestPicker_NavigationDismissesError(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: [16]byte{0x01}, Label: "first"},
				{SessionID: [16]byte{0x02}, Label: "second"},
			},
		},
	}
	p := NewPicker(screen, fc)
	p.RefreshCatalog()
	p.errMsg = "stale error"
	p.HandleKey(tcell.KeyDown, 0, 0)
	if p.errMsg != "" {
		t.Errorf("expected errMsg cleared after navigation, got %q", p.errMsg)
	}
}

// fakeClient stubs the picker's network transport for tests.
type fakeClient struct {
	response      protocol.ListSessionsResponse
	listErr       error
	recoverCalled bool
	recoverID     [16]byte
	recoverErr    error
	newCalled     bool
	renameErr     error
	deleteErr     error
}

func (f *fakeClient) ListSessions() (protocol.ListSessionsResponse, error) {
	if f.listErr != nil {
		return protocol.ListSessionsResponse{}, f.listErr
	}
	return f.response, nil
}
func (f *fakeClient) RecoverSession(id [16]byte, newLabel string) error {
	f.recoverCalled = true
	f.recoverID = id
	return f.recoverErr
}
func (f *fakeClient) RenameSession(id [16]byte, newLabel string) error { return f.renameErr }
func (f *fakeClient) DeleteSession(id [16]byte) error                  { return f.deleteErr }
func (f *fakeClient) FetchThumbnail(id [16]byte) ([]byte, error)       { return nil, nil }
func (f *fakeClient) StartFreshSession() {
	f.newCalled = true
}

// helpers
func screenContents(s tcell.SimulationScreen) ([]tcell.SimCell, int, int) {
	cells, w, h := s.GetContents()
	return cells, w, h
}

func contentsToString(cells []tcell.SimCell, w, h int) string {
	var b []byte
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) > 0 {
				b = append(b, []byte(string(c.Runes[0]))...)
			} else {
				b = append(b, ' ')
			}
		}
		b = append(b, '\n')
	}
	return string(b)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexOf(s, sub) != -1))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./cmd/texelation/boot/ -run "TestPicker_" -count=1`
Expected: FAIL — Picker undefined.

- [ ] **Step 3: Implement the picker**

Create `cmd/texelation/boot/picker.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker.go
// Summary: Stored-session recovery picker UI (issue #199 Plan F.1).
// Owns the tcell screen for the duration of the user's selection;
// hands off to the splash + clientrt pipeline once a choice is made.

package boot

import (
	"sync"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
)

// PickerClient is the network surface the picker needs. The boot
// runner constructs a real implementation against the unix socket;
// tests inject a fake.
type PickerClient interface {
	ListSessions() (protocol.ListSessionsResponse, error)
	RecoverSession(id [16]byte, newLabel string) error
	RenameSession(id [16]byte, newLabel string) error
	DeleteSession(id [16]byte) error
	FetchThumbnail(id [16]byte) ([]byte, error)
	StartFreshSession()
}

type pickerMode int

const (
	modeBrowse pickerMode = iota
	modeRename
	modeDeleteConfirm
)

type pickerTab int

const (
	tabLive pickerTab = iota
	tabStored
)

// Picker holds the picker's runtime state.
//
// mu guards thumbCache + pending against concurrent access from
// the lazy fetch goroutines spawned in Task 17. Render reads
// thumbCache under mu; the input handler does not touch it.
type Picker struct {
	screen tcell.Screen
	client PickerClient

	response    protocol.ListSessionsResponse
	activeTab   pickerTab
	selectedIdx int
	mode        pickerMode

	renameBuf []rune

	// errMsg, when non-empty, is rendered as a red banner above the
	// action bar. RefreshCatalog / Recover / Rename / Delete set it
	// when their underlying op fails; user dismisses by pressing any
	// navigation key.
	errMsg string

	mu          sync.Mutex
	thumbCache  map[[16]byte][]byte
	pending     map[[16]byte]bool
	hasGraphics bool

	done   bool
	choice pickerChoice
}

type pickerChoice int

const (
	choiceNone pickerChoice = iota
	choiceRecover
	choiceFresh
	choiceQuit
)

// NewPicker returns a picker bound to screen + client. Call
// RefreshCatalog before Render so the response is populated.
func NewPicker(screen tcell.Screen, client PickerClient) *Picker {
	return &Picker{
		screen:     screen,
		client:     client,
		activeTab:  tabStored,
		mode:       modeBrowse,
		thumbCache: make(map[[16]byte][]byte),
		pending:    make(map[[16]byte]bool),
	}
}

// RefreshCatalog fetches the catalog from the server. On error the
// previous response is preserved (so a transient socket blip doesn't
// wipe a freshly-shown list) and errMsg is set so the user sees a
// banner rather than silently emptying.
func (p *Picker) RefreshCatalog() {
	resp, err := p.client.ListSessions()
	if err != nil {
		p.errMsg = "Could not load sessions: " + err.Error()
		return
	}
	p.errMsg = ""
	p.response = resp
	if p.selectedIdx >= len(p.response.Stored) {
		p.selectedIdx = 0
	}
}

// SelectedIdx returns the currently highlighted index in the active
// tab's session list. Exposed for tests.
func (p *Picker) SelectedIdx() int { return p.selectedIdx }

// Done reports whether the picker has chosen an action; the caller's
// run loop should exit when true.
func (p *Picker) Done() bool { return p.done }

// Choice returns what the user picked once Done() is true.
func (p *Picker) Choice() pickerChoice { return p.choice }
```

Create `cmd/texelation/boot/picker_input.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import "github.com/gdamore/tcell/v2"

// HandleKey routes a tcell key event through the picker's mode-aware
// state machine. Tests call this directly; the run loop wraps real
// EventKey events.
func (p *Picker) HandleKey(key tcell.Key, ch rune, mods tcell.ModMask) {
	if p.mode == modeRename {
		p.handleRenameKey(key, ch)
		return
	}
	if p.mode == modeDeleteConfirm {
		p.handleDeleteConfirmKey(key, ch)
		return
	}
	// Any key dismisses a sticky error banner, even if it doesn't
	// otherwise navigate. The user has acknowledged the failure.
	p.errMsg = ""
	switch key {
	case tcell.KeyUp:
		if p.selectedIdx > 0 {
			p.selectedIdx--
		}
	case tcell.KeyDown:
		if p.selectedIdx < len(p.response.Stored)-1 {
			p.selectedIdx++
		}
	case tcell.KeyEnter:
		if len(p.response.Stored) > 0 {
			id := p.response.Stored[p.selectedIdx].SessionID
			if err := p.client.RecoverSession(id, ""); err != nil {
				// Keep the picker open and surface the error so the
				// user can pick a different session or retry. Don't
				// signal Done — recovery hasn't actually happened.
				p.errMsg = "Recover failed: " + err.Error()
				return
			}
			p.done = true
			p.choice = choiceRecover
		}
		return
	case tcell.KeyTab:
		if p.activeTab == tabStored {
			p.activeTab = tabLive
		} else {
			p.activeTab = tabStored
		}
		return
	case tcell.KeyEsc:
		p.done = true
		p.choice = choiceQuit
		return
	default:
	}
	switch ch {
	case 'j':
		if p.selectedIdx < len(p.response.Stored)-1 {
			p.selectedIdx++
		}
	case 'k':
		if p.selectedIdx > 0 {
			p.selectedIdx--
		}
	case 'n':
		p.client.StartFreshSession()
		p.done = true
		p.choice = choiceFresh
	case 'r':
		if len(p.response.Stored) > 0 {
			p.mode = modeRename
			p.renameBuf = []rune(p.response.Stored[p.selectedIdx].Label)
		}
	case 'd':
		if len(p.response.Stored) > 0 {
			p.mode = modeDeleteConfirm
		}
	case 'q':
		p.done = true
		p.choice = choiceQuit
	}
}

func (p *Picker) handleRenameKey(key tcell.Key, ch rune) {
	switch key {
	case tcell.KeyEsc:
		p.mode = modeBrowse
		p.renameBuf = nil
		return
	case tcell.KeyEnter:
		if len(p.response.Stored) > 0 {
			id := p.response.Stored[p.selectedIdx].SessionID
			newLabel := string(p.renameBuf)
			if err := p.client.RenameSession(id, newLabel); err != nil {
				p.errMsg = "Rename failed: " + err.Error()
				p.mode = modeBrowse
				p.renameBuf = nil
				return
			}
			p.response.Stored[p.selectedIdx].Label = newLabel
		}
		p.mode = modeBrowse
		p.renameBuf = nil
		return
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(p.renameBuf) > 0 {
			p.renameBuf = p.renameBuf[:len(p.renameBuf)-1]
		}
		return
	}
	if ch != 0 {
		p.renameBuf = append(p.renameBuf, ch)
	}
}

func (p *Picker) handleDeleteConfirmKey(key tcell.Key, ch rune) {
	switch ch {
	case 'y', 'Y':
		if len(p.response.Stored) > 0 {
			id := p.response.Stored[p.selectedIdx].SessionID
			if err := p.client.DeleteSession(id); err != nil {
				p.errMsg = "Delete failed: " + err.Error()
			} else {
				// Drop the entry locally so the cursor position
				// stays meaningful even if we don't refresh.
				p.response.Stored = append(p.response.Stored[:p.selectedIdx], p.response.Stored[p.selectedIdx+1:]...)
				if p.selectedIdx >= len(p.response.Stored) && p.selectedIdx > 0 {
					p.selectedIdx--
				}
			}
		}
		p.mode = modeBrowse
	case 'n', 'N':
		p.mode = modeBrowse
	}
	if key == tcell.KeyEsc {
		p.mode = modeBrowse
	}
}
```

Create `cmd/texelation/boot/picker_render.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
)

const (
	cardThumbW = 22
	cardThumbH = 8
	cardGap    = 1
)

// Render paints the picker to the screen. The caller is responsible
// for calling screen.Show() / screen.Sync() afterwards.
func (p *Picker) Render() {
	w, h := p.screen.Size()
	p.clear(w, h)
	p.drawHeader(w)
	p.drawTabs(w)
	p.drawCards(w, h)
	p.drawErrorBanner(w, h)
	p.drawActionBar(w, h)
	if p.mode == modeRename {
		p.drawRenameOverlay(w, h)
	}
	if p.mode == modeDeleteConfirm {
		p.drawDeleteConfirmOverlay(w, h)
	}
	p.screen.Show()
}

func (p *Picker) drawErrorBanner(w, h int) {
	if p.errMsg == "" {
		return
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	msg := p.errMsg
	if len(msg) > w-4 {
		msg = msg[:w-5] + "…"
	}
	for i, r := range msg {
		p.screen.SetContent(2+i, h-3, r, nil, style)
	}
}

func (p *Picker) clear(w, h int) {
	bg := tcell.StyleDefault
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p.screen.SetContent(x, y, ' ', nil, bg)
		}
	}
}

func (p *Picker) drawHeader(w int) {
	style := tcell.StyleDefault.Bold(true)
	title := "texelation — recover session"
	startX := (w - len(title)) / 2
	if startX < 0 {
		startX = 0
	}
	for i, r := range title {
		p.screen.SetContent(startX+i, 0, r, nil, style)
	}
}

func (p *Picker) drawTabs(w int) {
	live := fmt.Sprintf("[ Live (%d) ]", len(p.response.Live))
	stored := fmt.Sprintf("[ Stored (%d) ]", len(p.response.Stored))
	x := 2
	for i, r := range live {
		style := tcell.StyleDefault.Foreground(tcell.ColorGray)
		if p.activeTab == tabLive {
			style = tcell.StyleDefault.Bold(true)
		}
		p.screen.SetContent(x+i, 2, r, nil, style)
	}
	x += len(live) + 2
	for i, r := range stored {
		style := tcell.StyleDefault.Foreground(tcell.ColorGray)
		if p.activeTab == tabStored {
			style = tcell.StyleDefault.Bold(true)
		}
		p.screen.SetContent(x+i, 2, r, nil, style)
	}
}

func (p *Picker) drawCards(w, h int) {
	if p.activeTab != tabStored {
		return // Live tab empty in F.1
	}
	startY := 4
	for i, summary := range p.response.Stored {
		cardY := startY + i*(cardThumbH+cardGap)
		if cardY+cardThumbH+cardGap > h-2 {
			break
		}
		p.drawCard(2, cardY, summary, i == p.selectedIdx)
	}
}

func (p *Picker) drawCard(x, y int, s protocol.SessionSummary, selected bool) {
	bgStyle := tcell.StyleDefault
	if selected {
		bgStyle = bgStyle.Background(tcell.ColorDarkBlue)
	}
	// Thumbnail box (ASCII fallback for now; Kitty render added in Task 17)
	grid := renderASCIILayoutGrid(cardThumbW, cardThumbH, s.Layout)
	for cy := 0; cy < cardThumbH; cy++ {
		for cx := 0; cx < cardThumbW; cx++ {
			p.screen.SetContent(x+cx, y+cy, grid[cy][cx], nil, bgStyle)
		}
	}
	// Metadata column
	metaX := x + cardThumbW + 2
	p.drawText(metaX, y, fmt.Sprintf("Label:   %s", labelOrUntitled(s.Label)), bgStyle.Bold(true))
	p.drawText(metaX, y+1, fmt.Sprintf("Active:  %s", relativeTime(s.LastActive)), bgStyle)
	p.drawText(metaX, y+2, fmt.Sprintf("Panes:   %d", s.PaneCount), bgStyle)
	p.drawText(metaX, y+3, fmt.Sprintf("Title:   %s", truncate(s.FirstPaneTitle, 40)), bgStyle)
	if s.Pinned {
		p.drawText(metaX, y+4, "Pinned:  ★", bgStyle)
	}
}

func (p *Picker) drawActionBar(w, h int) {
	bar := "[Enter] recover   [n] new   [r] rename   [d] delete   [q] quit"
	style := tcell.StyleDefault.Foreground(tcell.ColorGray)
	startX := (w - len(bar)) / 2
	if startX < 0 {
		startX = 0
	}
	for i, r := range bar {
		p.screen.SetContent(startX+i, h-2, r, nil, style)
	}
}

func (p *Picker) drawRenameOverlay(w, h int) {
	prompt := fmt.Sprintf("Rename: %s", string(p.renameBuf))
	style := tcell.StyleDefault.Bold(true)
	for i, r := range prompt {
		p.screen.SetContent(2+i, h-4, r, nil, style)
	}
}

func (p *Picker) drawDeleteConfirmOverlay(w, h int) {
	if len(p.response.Stored) == 0 {
		return
	}
	prompt := fmt.Sprintf("Delete '%s'? [y/N]", labelOrUntitled(p.response.Stored[p.selectedIdx].Label))
	style := tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	for i, r := range prompt {
		p.screen.SetContent(2+i, h-4, r, nil, style)
	}
}

func (p *Picker) drawText(x, y int, s string, style tcell.Style) {
	for i, r := range s {
		p.screen.SetContent(x+i, y, r, nil, style)
	}
}

func labelOrUntitled(s string) string {
	if s == "" {
		return "Untitled"
	}
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func relativeTime(unixSec int64) string {
	if unixSec == 0 {
		return "—"
	}
	d := time.Since(time.Unix(unixSec, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
	}
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./cmd/texelation/boot/ -run "TestPicker_" -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/texelation/boot/picker.go cmd/texelation/boot/picker_input.go cmd/texelation/boot/picker_render.go cmd/texelation/boot/picker_test.go
git commit -m "Picker: state machine, navigation, render"
```

---

### Task 17: Picker — Kitty thumbnail rendering + lazy fetch (locked, defer-clear pending, dimension cap)

**Files:**
- Create: `cmd/texelation/boot/picker_thumbnail.go`
- Modify: `cmd/texelation/boot/picker_render.go` (route through `renderThumbnail`)
- Modify: `cmd/texelation/boot/picker_test.go` (test the upgrade path + locked accessor)

The Picker struct already has `mu` and `hasGraphics` fields from Task 16. This task adds the fetch coordinator and the render hook. **Critical correctness fixes vs v1:**
- Reads of `thumbCache` and `pending` happen under `mu` (was: unlocked, raced with the goroutine's writes).
- The fetch goroutine clears `pending[id]` in a `defer`, so a failed fetch doesn't strand the entry forever (was: only cleared on success → ASCII-forever bug).
- Test uses a `ThumbCached` accessor that takes the lock instead of poking the map directly under `-race`.
- Decoded PNG dimensions are capped via `png.DecodeConfig` before `png.Decode` to defend against pathological inputs (corrupt sidecar, hostile file at the predictable on-disk path).

- [ ] **Step 1: Write failing test**

Append to `cmd/texelation/boot/picker_test.go`:

```go
func TestPicker_FetchThumbnailUpgradesCard(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	id := [16]byte{0xEE}
	pngBytes := []byte("fake-png-bytes")
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{
				{SessionID: id, Label: "graphics", HasThumbnail: true},
			},
		},
		thumbBytes: pngBytes,
	}
	p := NewPicker(screen, fc)
	p.SetGraphicsCapable(true)
	p.RefreshCatalog()
	p.Render() // triggers the lazy fetch
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if p.ThumbCached(id) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !p.ThumbCached(id) {
		t.Fatalf("expected thumbnail cached within 500ms")
	}
	if !fc.fetchCalled {
		t.Errorf("expected FetchThumbnail dispatch")
	}
}

func TestPicker_NoFetchWhenGraphicsAbsent(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0x10}, HasThumbnail: true}},
		},
		thumbBytes: []byte("nope"),
	}
	p := NewPicker(screen, fc)
	p.SetGraphicsCapable(false)
	p.RefreshCatalog()
	p.Render()
	time.Sleep(20 * time.Millisecond)
	if fc.fetchCalled {
		t.Errorf("did not expect FetchThumbnail dispatch on text-only terminal")
	}
}

func TestPicker_FetchThumbnailErrorClearsPending(t *testing.T) {
	// Critical regression test for the v1 bug where a failed fetch
	// left pending[id]=true forever, preventing retry. After the
	// goroutine returns, pending must be cleared regardless of
	// outcome so a future Render can re-attempt (e.g., user toggles
	// tabs and back).
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)
	id := [16]byte{0xAB}
	fc := &fakeClient{
		response: protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: id, HasThumbnail: true}},
		},
		thumbErr: errors.New("transient"),
	}
	p := NewPicker(screen, fc)
	p.SetGraphicsCapable(true)
	p.RefreshCatalog()
	p.Render()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !p.IsPending(id) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if p.IsPending(id) {
		t.Fatalf("expected pending cleared after fetch error within 500ms")
	}
	if p.ThumbCached(id) {
		t.Errorf("expected thumbCache empty on fetch error")
	}
}
```

Update the `fakeClient` definition to support fetch error injection:

```go
type fakeClient struct {
	response      protocol.ListSessionsResponse
	listErr       error
	recoverCalled bool
	recoverID     [16]byte
	recoverErr    error
	newCalled     bool
	renameErr     error
	deleteErr     error
	fetchCalled   bool
	thumbBytes    []byte
	thumbErr      error
}

// ... existing methods ...

func (f *fakeClient) FetchThumbnail(id [16]byte) ([]byte, error) {
	f.fetchCalled = true
	if f.thumbErr != nil {
		return nil, f.thumbErr
	}
	return f.thumbBytes, nil
}
```

(Merge into the existing `fakeClient` from Task 16 — don't duplicate.)

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./cmd/texelation/boot/ -run "TestPicker_FetchThumbnailUpgradesCard|TestPicker_NoFetchWhenGraphicsAbsent|TestPicker_FetchThumbnailErrorClearsPending" -count=1`
Expected: FAIL — `SetGraphicsCapable`, `ThumbCached`, `IsPending` not exposed.

- [ ] **Step 3: Implement the thumbnail dispatcher**

Create `cmd/texelation/boot/picker_thumbnail.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_thumbnail.go
// Summary: Kitty thumbnail rendering + lazy fetch for the picker.
// Falls back to the ASCII layout when graphics are unavailable or
// the fetch is still in flight.

package boot

import (
	"bytes"
	"image"
	"image/png"
	"log"
)

// maxThumbnailDim caps PNG dimensions on the decode path. A 480×270
// PNG is well within this; anything larger is either corrupt or
// hostile (the 16 MiB protocol cap leaves room for adversarial
// dimension declarations within a small payload). Decoding without
// this check can OOM the picker on corrupted inputs.
const maxThumbnailDim = 4096

// SetGraphicsCapable tells the picker whether to dispatch
// FetchThumbnail requests + render Kitty images. False keeps the
// ASCII fallback exclusively (no network traffic for thumbnails).
func (p *Picker) SetGraphicsCapable(b bool) {
	p.hasGraphics = b
}

// ThumbCached returns true iff a PNG for id is in the local cache.
// Locked accessor for tests; production read sites in drawCard also
// take p.mu.
func (p *Picker) ThumbCached(id [16]byte) bool {
	p.mu.Lock()
	_, ok := p.thumbCache[id]
	p.mu.Unlock()
	return ok
}

// IsPending reports whether a fetch is in flight for id. Locked
// accessor for tests.
func (p *Picker) IsPending(id [16]byte) bool {
	p.mu.Lock()
	v := p.pending[id]
	p.mu.Unlock()
	return v
}

// thumbnailFor returns the cached PNG bytes for id, or nil if not
// cached. Caller does not need to hold p.mu — this method takes it.
func (p *Picker) thumbnailFor(id [16]byte) []byte {
	p.mu.Lock()
	data := p.thumbCache[id]
	p.mu.Unlock()
	return data
}

// maybeFetchThumbnail kicks off a non-blocking fetch for id if we
// haven't cached or pending one already. Called from the render
// loop's per-card pass. All map access is under p.mu; the goroutine
// always clears pending[id] in defer so a failed fetch doesn't
// strand the entry permanently (previously a "card stays ASCII
// forever" bug — see Task 17 in the plan).
func (p *Picker) maybeFetchThumbnail(id [16]byte, hasThumb bool) {
	if !p.hasGraphics || !hasThumb {
		return
	}
	p.mu.Lock()
	if _, cached := p.thumbCache[id]; cached {
		p.mu.Unlock()
		return
	}
	if p.pending[id] {
		p.mu.Unlock()
		return
	}
	p.pending[id] = true
	p.mu.Unlock()

	go func(targetID [16]byte) {
		defer func() {
			// Clear pending unconditionally so subsequent renders
			// can retry. Without this defer, an error path leaves
			// pending[id]=true forever — a real bug we exorcised
			// in v2. Recover from any panic in the client (the
			// transport could trip a runtime fault on a dropped
			// socket) so we don't crash the picker.
			if rec := recover(); rec != nil {
				log.Printf("picker: thumbnail fetch panic: %v", rec)
			}
			p.mu.Lock()
			delete(p.pending, targetID)
			p.mu.Unlock()
		}()
		data, err := p.client.FetchThumbnail(targetID)
		if err != nil {
			log.Printf("picker: thumbnail fetch %x: %v", targetID[:4], err)
			return
		}
		if len(data) == 0 {
			return
		}
		// Validate dimensions before storing. A png.DecodeConfig
		// failure means corrupt/non-PNG bytes; refuse to cache.
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			log.Printf("picker: thumbnail decode-config %x: %v", targetID[:4], err)
			return
		}
		if cfg.Width > maxThumbnailDim || cfg.Height > maxThumbnailDim {
			log.Printf("picker: thumbnail %x: refusing %dx%d (cap %d)", targetID[:4], cfg.Width, cfg.Height, maxThumbnailDim)
			return
		}
		p.mu.Lock()
		p.thumbCache[targetID] = data
		p.mu.Unlock()
	}(id)
}

// decodeCachedThumb decodes the PNG bytes for id into an image.Image,
// or returns nil if the bytes can't be decoded. The dimension check
// happens at fetch time (above), so this is purely the costlier
// full decode for paint use. Currently only used by the (future)
// Kitty emission path; the SimulationScreen unit tests don't exercise
// it.
func decodeCachedThumb(data []byte) image.Image {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}
```

In `picker_render.go`, replace the thumbnail-drawing block at the top of `drawCard` with the locked-read version:

```go
func (p *Picker) drawCard(x, y int, s protocol.SessionSummary, selected bool) {
	bgStyle := tcell.StyleDefault
	if selected {
		bgStyle = bgStyle.Background(tcell.ColorDarkBlue)
	}
	cached := p.thumbnailFor(s.SessionID)
	if cached != nil && p.hasGraphics {
		// Cached thumbnail available. The full Kitty escape-sequence
		// emission is out of scope for SimulationScreen; tests detect
		// the upgrade by checking the placeholder block char.
		for cy := 0; cy < cardThumbH; cy++ {
			for cx := 0; cx < cardThumbW; cx++ {
				p.screen.SetContent(x+cx, y+cy, '▓', nil, bgStyle)
			}
		}
	} else {
		grid := renderASCIILayoutGrid(cardThumbW, cardThumbH, s.Layout)
		for cy := 0; cy < cardThumbH; cy++ {
			for cx := 0; cx < cardThumbW; cx++ {
				p.screen.SetContent(x+cx, y+cy, grid[cy][cx], nil, bgStyle)
			}
		}
	}
	p.maybeFetchThumbnail(s.SessionID, s.HasThumbnail)

	// Metadata column (unchanged).
	metaX := x + cardThumbW + 2
	p.drawText(metaX, y, fmt.Sprintf("Label:   %s", labelOrUntitled(s.Label)), bgStyle.Bold(true))
	p.drawText(metaX, y+1, fmt.Sprintf("Active:  %s", relativeTime(s.LastActive)), bgStyle)
	p.drawText(metaX, y+2, fmt.Sprintf("Panes:   %d", s.PaneCount), bgStyle)
	p.drawText(metaX, y+3, fmt.Sprintf("Title:   %s", truncate(s.FirstPaneTitle, 40)), bgStyle)
	if s.Pinned {
		p.drawText(metaX, y+4, "Pinned:  ★", bgStyle)
	}
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./cmd/texelation/boot/ -run "TestPicker_" -count=1 -race`
Expected: PASS — including under `-race`. The locked accessors are critical here.

- [ ] **Step 5: Commit**

```bash
git add cmd/texelation/boot/picker_thumbnail.go cmd/texelation/boot/picker_render.go cmd/texelation/boot/picker_test.go
git commit -m "Picker: lazy thumbnail fetch with locked cache + deferred pending clear"
```

---

### Task 18: boot.Run integration — trigger logic + handoff

**Files:**
- Modify: `cmd/texelation/main.go` (add `--recover` flag, picker activation)
- Create: `cmd/texelation/boot/picker_runner.go` (orchestrates picker + client transport)
- Test: manual smoke test (no automated integration test in this task)

- [ ] **Step 1: Add `--recover` flag**

In `cmd/texelation/main.go` flag parsing block, add:

```go
recoverFlag := fs.Bool("recover", false, "Show the session-recovery picker even if client state is intact")
```

Pipe it through `clientrt.Options`:

```go
// Add to clientrt.Options struct in app.go (Task 14 of Plan D2 already
// landed; Plan F.1 extends with):
ShowRecoverPicker bool

// Wire from main.go:
clientOpts.ShowRecoverPicker = *recoverFlag
```

- [ ] **Step 2: Implement the picker runner**

Create `cmd/texelation/boot/picker_runner.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_runner.go
// Summary: Connect the picker to the texelation socket and run its
// event loop until the user picks. Returns the chosen action so the
// caller can decide how to proceed (recover -> normal connect path,
// fresh -> fresh-session path, quit -> exit).

package boot

import (
	"github.com/gdamore/tcell/v2"
)

// PickerOutcome captures what the user picked.
type PickerOutcome struct {
	Choice    pickerChoice
	SessionID [16]byte // populated when Choice == choiceRecover
}

// RunPicker drives the picker against client until the user makes a
// selection. Returns the outcome; callers translate it to a connect
// path. The screen is shared with the splash and clientrt; this
// function does not call Init/Fini.
func RunPicker(screen tcell.Screen, client PickerClient, hasGraphics bool) (PickerOutcome, error) {
	p := NewPicker(screen, client)
	p.SetGraphicsCapable(hasGraphics)
	p.RefreshCatalog()

	for !p.Done() {
		p.Render()
		ev := screen.PollEvent()
		switch e := ev.(type) {
		case *tcell.EventKey:
			p.HandleKey(e.Key(), e.Rune(), e.Modifiers())
		case *tcell.EventResize:
			screen.Sync()
		case *tcell.EventInterrupt:
			return PickerOutcome{Choice: choiceQuit}, nil
		}
	}

	out := PickerOutcome{Choice: p.choice}
	if p.choice == choiceRecover && len(p.response.Stored) > 0 {
		out.SessionID = p.response.Stored[p.selectedIdx].SessionID
	}
	return out, nil
}
```

- [ ] **Step 3: Wire trigger logic into `handleUnifiedMode`**

In `cmd/texelation/main.go` `handleUnifiedMode`, after `supervisor.EnsureRunning` but before constructing `clientOpts.Screen`, add the picker activation block:

```go
// Plan F.1: show the recovery picker when (a) the user passed
// --recover, OR (b) we have no persisted client state AND the server
// has stored sessions to offer.
//
// We construct a lightweight protocol client to fetch the catalog;
// if there's nothing to show, fall through to the ordinary connect
// path. The picker reuses the splash's tcell screen so the handoff
// is flicker-free.
shouldShowPicker := clientOpts.ShowRecoverPicker
if !shouldShowPicker {
	hasState := clientStateExists(clientOpts) // helper below
	if !hasState {
		// Quick probe: does the server have stored sessions?
		probe, err := boot.ProbeStoredSessions(clientOpts.Socket)
		if err == nil && probe > 0 {
			shouldShowPicker = true
		}
	}
}
if shouldShowPicker {
	splashApp.SetStage(boot.StageStarting)
	splashApp.SetDetail("Loading session list…")
	splashRunner.Wake()
	pickerClient, err := boot.NewSocketPickerClient(clientOpts.Socket)
	if err != nil {
		log.Printf("picker: socket client setup failed: %v; skipping picker", err)
	} else {
		stopSplash()
		outcome, err := boot.RunPicker(screen, pickerClient, graphics.DetectCapability() == texelcore.GraphicsKitty)
		_ = pickerClient.Close()
		if err != nil {
			log.Printf("picker: %v; falling back to fresh session", err)
		} else {
			switch outcome.Choice {
			case boot.PickerChoiceRecover:
				clientOpts.RecoverSessionID = outcome.SessionID
			case boot.PickerChoiceFresh:
				// fall through to ordinary connect path
			case boot.PickerChoiceQuit:
				return nil
			}
		}
		// Restart the splash so clientrt's status callbacks have
		// somewhere to render. Cleaner alternative would be to
		// thread directly into clientrt; for F.1 we accept the
		// splash restart.
		splashApp = boot.New("Texelation")
		splashRunner = boot.NewRunner(screen, splashApp)
		splashRunner.Start()
		splashStopped = false
	}
}
```

Helper `clientStateExists`:

```go
// clientStateExists is a thin wrapper: if Plan D's persistence path
// resolves AND the file is present and non-empty, return true. Used
// only for the picker-trigger heuristic.
func clientStateExists(opts clientrt.Options) bool {
	path, err := clientrt.ResolvePath(opts.Socket, opts.ClientName)
	if err != nil || path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return false
	}
	return true
}
```

In `boot/picker_runner.go`, expose the choice constants publicly so main.go can reference them:

```go
// Public re-exports for the choice constants (private constants
// declared in picker.go).
const (
	PickerChoiceNone    = choiceNone
	PickerChoiceRecover = choiceRecover
	PickerChoiceFresh   = choiceFresh
	PickerChoiceQuit    = choiceQuit
)
```

- [ ] **Step 4: Implement the socket picker client + probe (real code, no stubs)**

Create `cmd/texelation/boot/picker_client.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_client.go
// Summary: Socket-backed PickerClient + ProbeStoredSessions. Performs
// the protocol Hello/Welcome handshake and round-trips picker
// messages over a unix socket. Modelled on client/simple_client.go's
// dial-and-handshake helper.

package boot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// dialAndHandshake opens a unix socket connection to the texelation
// daemon and runs the Hello/Welcome handshake. Returns the conn +
// the assigned sessionID. The caller closes the conn.
func dialAndHandshake(socket string) (net.Conn, [16]byte, error) {
	var sid [16]byte
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return nil, sid, fmt.Errorf("dial %s: %w", socket, err)
	}
	var clientID [16]byte
	if _, err := rand.Read(clientID[:]); err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("rand client id: %w", err)
	}
	helloBody, err := protocol.EncodeHello(protocol.Hello{ClientID: clientID, ClientName: "texelation-picker"})
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("encode hello: %w", err)
	}
	helloHdr := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgHello,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(conn, helloHdr, helloBody); err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("write hello: %w", err)
	}
	hdr, payload, err := protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("read welcome: %w", err)
	}
	if hdr.Type != protocol.MsgWelcome {
		conn.Close()
		return nil, sid, fmt.Errorf("expected welcome, got message type %d", hdr.Type)
	}
	welcome, err := protocol.DecodeWelcome(payload)
	if err != nil {
		conn.Close()
		return nil, sid, fmt.Errorf("decode welcome: %w", err)
	}
	return conn, welcome.SessionID, nil
}

// ProbeStoredSessions performs Hello/Welcome + MsgListSessions and
// returns the count of stored sessions. The ephemeral session created
// by the handshake is left dangling on the server's side and gets
// reaped via the daemon's normal idle-session cleanup; the cost is
// tiny (one new session record, no panes ever attached).
func ProbeStoredSessions(socket string) (int, error) {
	conn, _, err := dialAndHandshake(socket)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	body, _ := protocol.EncodeListSessionsRequest(protocol.ListSessionsRequest{})
	hdr := protocol.Header{
		Version: protocol.Version,
		Type:    protocol.MsgListSessions,
		Flags:   protocol.FlagChecksum,
	}
	if err := protocol.WriteMessage(conn, hdr, body); err != nil {
		return 0, fmt.Errorf("write list-sessions: %w", err)
	}
	respHdr, respPayload, err := protocol.ReadMessage(conn)
	if err != nil {
		return 0, fmt.Errorf("read list-sessions: %w", err)
	}
	if respHdr.Type != protocol.MsgListSessionsResponse {
		return 0, fmt.Errorf("expected list-sessions response, got %d", respHdr.Type)
	}
	resp, err := protocol.DecodeListSessionsResponse(respPayload)
	if err != nil {
		return 0, fmt.Errorf("decode list-sessions: %w", err)
	}
	return len(resp.Stored), nil
}

// SocketPickerClient implements PickerClient against a live socket
// for the duration of the picker's UI session. One connection per
// picker invocation; closed when the picker exits.
type SocketPickerClient struct {
	conn      net.Conn
	sessionID [16]byte

	// mu serializes request/response round-trips so concurrent
	// picker callers (e.g., a goroutine fetching thumbnails while
	// the foreground render fires ListSessions) don't interleave
	// frames on the wire. Real picker traffic is rare enough that
	// the lock contention is irrelevant.
	mu sync.Mutex

	closeOnce sync.Once
}

// NewSocketPickerClient dials the socket and runs the handshake.
// Returns a client ready for use; caller must Close() when done.
func NewSocketPickerClient(socket string) (*SocketPickerClient, error) {
	conn, sid, err := dialAndHandshake(socket)
	if err != nil {
		return nil, err
	}
	return &SocketPickerClient{conn: conn, sessionID: sid}, nil
}

// roundTrip serialises a request frame, writes it, reads one response
// frame, and returns the response header + payload. The caller
// validates the response type. mu is held for the duration so
// interleaved sends from concurrent picker code can't garble each
// other.
func (s *SocketPickerClient) roundTrip(reqType protocol.MessageType, body []byte) (protocol.Header, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      reqType,
		Flags:     protocol.FlagChecksum,
		SessionID: s.sessionID,
	}
	if err := protocol.WriteMessage(s.conn, hdr, body); err != nil {
		return protocol.Header{}, nil, fmt.Errorf("write %d: %w", reqType, err)
	}
	respHdr, respPayload, err := protocol.ReadMessage(s.conn)
	if err != nil {
		return protocol.Header{}, nil, fmt.Errorf("read response: %w", err)
	}
	return respHdr, respPayload, nil
}

func (s *SocketPickerClient) ListSessions() (protocol.ListSessionsResponse, error) {
	body, _ := protocol.EncodeListSessionsRequest(protocol.ListSessionsRequest{})
	hdr, payload, err := s.roundTrip(protocol.MsgListSessions, body)
	if err != nil {
		return protocol.ListSessionsResponse{}, err
	}
	if hdr.Type != protocol.MsgListSessionsResponse {
		return protocol.ListSessionsResponse{}, fmt.Errorf("unexpected response type %d", hdr.Type)
	}
	return protocol.DecodeListSessionsResponse(payload)
}

func (s *SocketPickerClient) RecoverSession(id [16]byte, newLabel string) error {
	body, err := protocol.EncodeRecoverSessionRequest(protocol.RecoverSessionRequest{SessionID: id, NewLabel: newLabel})
	if err != nil {
		return err
	}
	hdr, payload, err := s.roundTrip(protocol.MsgRecoverSession, body)
	if err != nil {
		return err
	}
	switch hdr.Type {
	case protocol.MsgConnectAccept:
		// Recovery succeeded server-side; the picker hands off to
		// the connect path which will re-attach with this session ID.
		return nil
	case protocol.MsgError:
		ef, _ := protocol.DecodeErrorFrame(payload)
		return errors.New(ef.Message)
	default:
		return fmt.Errorf("unexpected response %d", hdr.Type)
	}
}

func (s *SocketPickerClient) RenameSession(id [16]byte, newLabel string) error {
	body, err := protocol.EncodeRenameSessionRequest(protocol.RenameSessionRequest{SessionID: id, NewLabel: newLabel})
	if err != nil {
		return err
	}
	return s.opRoundTrip(protocol.MsgRenameSession, body, protocol.OpRename)
}

func (s *SocketPickerClient) DeleteSession(id [16]byte) error {
	body, err := protocol.EncodeDeleteSessionRequest(protocol.DeleteSessionRequest{SessionID: id})
	if err != nil {
		return err
	}
	return s.opRoundTrip(protocol.MsgDeleteSession, body, protocol.OpDelete)
}

func (s *SocketPickerClient) opRoundTrip(reqType protocol.MessageType, body []byte, expectOp protocol.SessionOpKind) error {
	hdr, payload, err := s.roundTrip(reqType, body)
	if err != nil {
		return err
	}
	if hdr.Type != protocol.MsgSessionOpResponse {
		return fmt.Errorf("unexpected response %d", hdr.Type)
	}
	resp, err := protocol.DecodeSessionOpResponse(payload)
	if err != nil {
		return err
	}
	if resp.OpType != expectOp {
		return fmt.Errorf("op mismatch: got %d, want %d", resp.OpType, expectOp)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	return nil
}

func (s *SocketPickerClient) FetchThumbnail(id [16]byte) ([]byte, error) {
	body, err := protocol.EncodeFetchThumbnailRequest(protocol.FetchThumbnailRequest{SessionID: id})
	if err != nil {
		return nil, err
	}
	hdr, payload, err := s.roundTrip(protocol.MsgFetchThumbnail, body)
	if err != nil {
		return nil, err
	}
	if hdr.Type != protocol.MsgFetchThumbnailResponse {
		return nil, fmt.Errorf("unexpected response %d", hdr.Type)
	}
	resp, err := protocol.DecodeFetchThumbnailResponse(payload)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, errors.New(resp.Error)
	}
	return resp.PNG, nil
}

// StartFreshSession is a no-op for the socket client — the picker
// simply exits with choiceFresh and main.go takes the ordinary
// connect path (zero sessionID).
func (s *SocketPickerClient) StartFreshSession() {}

// Close shuts down the underlying socket. Safe to call multiple times.
func (s *SocketPickerClient) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
	})
	return err
}
```

The `_ = protocol.Version` placeholder previously in `picker_runner.go` is removed — the protocol package is now genuinely used.

Add a basic round-trip test in `cmd/texelation/boot/picker_client_test.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/framegrace/texelation/protocol"
)

// TestSocketPickerClient_ListSessions wires a fake server-side
// listener that speaks just enough protocol to validate the picker
// client's round-trip. We don't bring up a real texel-server; just
// verify the framing.
func TestSocketPickerClient_ListSessions(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Read Hello, write Welcome.
		hdr, _, err := protocol.ReadMessage(conn)
		if err != nil || hdr.Type != protocol.MsgHello {
			return
		}
		welBody, _ := protocol.EncodeWelcome(protocol.Welcome{SessionID: [16]byte{0xAA}, ServerName: "test"})
		welHdr := protocol.Header{Version: protocol.Version, Type: protocol.MsgWelcome, Flags: protocol.FlagChecksum, SessionID: [16]byte{0xAA}}
		_ = protocol.WriteMessage(conn, welHdr, welBody)

		// Read MsgListSessions, write a 1-entry response.
		_, _, _ = protocol.ReadMessage(conn)
		respBody, _ := protocol.EncodeListSessionsResponse(protocol.ListSessionsResponse{
			Stored: []protocol.SessionSummary{{SessionID: [16]byte{0xBB}, Label: "test", LastActive: 1, PaneCount: 1}},
		})
		respHdr := protocol.Header{Version: protocol.Version, Type: protocol.MsgListSessionsResponse, Flags: protocol.FlagChecksum, SessionID: [16]byte{0xAA}}
		_ = protocol.WriteMessage(conn, respHdr, respBody)
	}()

	// Give the listener a moment to be ready for accept.
	time.Sleep(20 * time.Millisecond)

	c, err := NewSocketPickerClient(socket)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer c.Close()

	resp, err := c.ListSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Stored) != 1 || resp.Stored[0].Label != "test" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}
```

Run: `go test ./cmd/texelation/boot/ -run "TestSocketPickerClient_" -count=1`
Expected: PASS.

- [ ] **Step 5: Wire `clientrt.Options.RecoverSessionID` into the connect path**

In `internal/runtime/client/app.go`, add a `RecoverSessionID [16]byte` field to `Options` and use it in the connect path: when set and `loadedState == nil`, call `simple.Connect(&opts.RecoverSessionID)` instead of `simple.Connect(&sessionID)` with the zero ID. The server treats this as a recover-by-ID resume (the new MsgRecoverSession path already covers this server-side; the existing ConnectRequest with a known ID also works since `LookupOrRehydrate` is keyed by ID).

```go
// Inside Run(), after loadedState resolution:
if loadedState == nil && opts.RecoverSessionID != ([16]byte{}) {
	sessionID = opts.RecoverSessionID
}
```

The simpler design: just pre-seed the sessionID and let the existing `simple.Connect(&sessionID)` flow do the rest. The server's `LookupOrRehydrate` in handshake.go (or wherever the connect handler lives) will rehydrate the persisted session if it matches. This avoids needing a separate code path for picker-driven recover.

- [ ] **Step 6: Manual smoke test**

```bash
make build
# Stop any running daemon
./bin/texelation --stop
# Wipe client state to force the no-state branch
rm -rf ~/.local/state/texelation
# First run creates a session
./bin/texelation
# Open a few panes, then quit gracefully (Ctrl-Q or your normal quit
# binding). The graceful shutdown should write a thumbnail.
# Wipe client state again
rm -rf ~/.local/state/texelation
# Now this run should hit the picker:
./bin/texelation
# Expected: picker UI appears with the previously-saved session
# listed. Pressing Enter recovers it; the session reopens with
# the same panes.
```

- [ ] **Step 7: Run the full test suite to catch regressions**

Run: `go test -race ./... -count=1`
Expected: PASS. The protocol bump (v4 → v5) cascaded through many tests; any failures here indicate a missed update.

- [ ] **Step 8: Commit**

```bash
git add cmd/texelation/main.go cmd/texelation/boot/picker_runner.go cmd/texelation/boot/picker_client.go cmd/texelation/boot/picker_client_test.go internal/runtime/client/app.go
git commit -m "boot.Run: wire picker activation + recover handoff"
```

---

### Task 19: Client screenshot — refactor to use shared `internal/thumbnail` primitive

**Files:**
- Modify: `internal/runtime/client/screenshot.go`

The existing `takeScreenshot` does the same `textrender.DetectFont` + `textrender.New` + render pipeline that the shared primitive (Task 9) now exposes. Refactoring removes the duplicate setup and ensures any future improvements to the rendering path benefit both code paths.

- [ ] **Step 1: Read the current screenshot path**

Run: `cat internal/runtime/client/screenshot.go`
Expected: see the existing `takeScreenshot` function with `textrender.DetectFont`, `textrender.New`, `renderer.Render(grid)`.

- [ ] **Step 2: Replace the rendering call with the shared primitive**

Modify `internal/runtime/client/screenshot.go`:

```go
package clientruntime

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	texelcore "github.com/framegrace/texelui/core"

	"github.com/framegrace/texelation/internal/thumbnail"
)

// takeScreenshot renders the current workspace buffer to a PNG file
// and copies it to the system clipboard. Uses the shared
// internal/thumbnail rendering primitive — same code path as the
// server's lifecycle thumbnail capture.
func takeScreenshot(state *clientState) {
	buf := state.prevBuffer
	if len(buf) == 0 {
		return
	}

	coreGrid := make([][]texelcore.Cell, len(buf))
	for y, row := range buf {
		coreGrid[y] = make([]texelcore.Cell, len(row))
		for x := range row {
			coreGrid[y][x] = texelcore.Cell{
				Ch:    row[x].Ch,
				Style: row[x].Style,
			}
		}
	}

	img, err := thumbnail.RenderGrid(coreGrid)
	if err != nil {
		log.Printf("[SCREENSHOT] Render failed: %v", err)
		return
	}

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".texelation", "screenshots")
	os.MkdirAll(dir, 0o755)
	filename := filepath.Join(dir, fmt.Sprintf("screenshot-%s.png", time.Now().Format("2006-01-02_15-04-05")))

	if err := thumbnail.WritePNGAtomic(filename, img); err != nil {
		log.Printf("[SCREENSHOT] Write failed: %v", err)
		return
	}

	log.Printf("[SCREENSHOT] Saved to %s", filename)
	copyImageToClipboard(img, filename)
}

// copyImageToClipboard copies a PNG image to the system clipboard.
// Unchanged from the previous implementation.
func copyImageToClipboard(img image.Image, filePath string) {
	if path, err := exec.LookPath("wl-copy"); err == nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			cmd := exec.Command(path, "-t", "image/png")
			cmd.Stdin = &buf
			if err := cmd.Run(); err == nil {
				return
			}
		}
	}
	if path, err := exec.LookPath("xclip"); err == nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			cmd := exec.Command(path, "-selection", "clipboard", "-t", "image/png")
			cmd.Stdin = &buf
			if err := cmd.Run(); err == nil {
				return
			}
		}
	}
	if path, err := exec.LookPath("osascript"); err == nil {
		script := fmt.Sprintf(`set the clipboard to (read (POSIX file %q) as «class PNGf»)`, filePath)
		exec.Command(path, "-e", script).Run()
	}
}
```

The `textrender` import is dropped from this file (the primitive owns it now). The `image/png` import stays for the clipboard helpers' re-encode step.

- [ ] **Step 3: Build and run existing client tests to confirm no regressions**

Run: `go build ./internal/runtime/client/... && go test ./internal/runtime/client/ -count=1`
Expected: PASS. If `screenshot.go` had no dedicated tests, the build passing is the gate.

- [ ] **Step 4: Manual verification**

```bash
make build
./bin/texelation
# Trigger the screenshot keybinding (default is the existing binding)
# Confirm: file lands in ~/.texelation/screenshots/, clipboard contains
# the PNG. Behavior should be identical to before the refactor.
```

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/client/screenshot.go
git commit -m "Client screenshot: use shared internal/thumbnail primitive"
```

---

## Final Steps

After all 19 tasks land cleanly:

- [ ] **Run race-detector across the whole repo**

Run: `go test -race ./... -count=1`
Expected: PASS, no races, no failures.

- [ ] **Open a PR**

```bash
git push -u origin feature/issue-199-plan-f-session-picker
gh pr create --title "Issue #199 Plan F.1: stored-session recovery picker" --body "$(cat <<'BODY'
## Summary
- Adds protocol v5 with six new picker messages (list, recover, rename, delete, fetch-thumbnail, op-response) and SessionSummary/LiveSummary wire types.
- New shared `internal/thumbnail/` rendering primitive used by both server-side lifecycle capture and client-side user screenshots.
- Server-side: manager helpers (StoredSummaries, LiveSummaries, RenameStored, DeleteStored), connection handlers (with thumbnail size cap), lifecycle thumbnail capture (graceful shutdown + last-disconnect), pane-buffer composition adapter on DesktopSink.
- Client-side: picker UI in `cmd/texelation/boot/` reusing the splash screen; ASCII fallback (n-way splits) + lazy Kitty thumbnail fetch with locked map access and deferred pending clear; in-picker error banners on Recover/Rename/Delete/RefreshCatalog failures.
- Trigger logic in `boot.Run` activates picker when client state is missing AND server has stored sessions, or when `--recover` is passed.
- Client `takeScreenshot` refactored to use the shared primitive (dedup).
- Spec: `docs/superpowers/specs/2026-05-03-issue-199-plan-f-session-picker-design.md`

## Test plan
- [ ] `go test -race ./... -count=1` — full suite green
- [ ] Manual: cold-start with empty client state + populated server sessions → picker appears
- [ ] Manual: `--recover` flag with intact client state → picker still appears
- [ ] Manual: pick a stored session → recovers cleanly, panes restored
- [ ] Manual: rename inline → label persists across daemon restart
- [ ] Manual: delete a stored session → JSON + PNG both removed
- [ ] Manual: Kitty terminal → thumbnails render; non-Kitty terminal → ASCII fallback
- [ ] Manual: client screenshot keybinding still works (uses shared primitive)
- [ ] Manual: simulate stored-session network failure → error banner displayed, picker stays open

## Follow-ups
- F.2 issue (multi-live-sessions) to be filed after this lands; the wire format and Live tab are already in place.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
BODY
)"
```

---

## Forward-compat for F.2 / F.3 (no work in F.1)

- `LiveSummary` slice already on the wire; F.2 wires `Manager.LiveSummaries` to actual session counts.
- `Live` tab renders empty in F.1 (count = 0); F.2 reuses the same render path with populated data.
- `MsgRecoverSession` semantics generalise: F.2 detects "session is live" and skips rehydration when picked.
- `MsgFetchThumbnail` works for any session whose PNG sidecar exists; F.2 may capture on Live → Stored transitions when sessions are evicted.
- F.3 (templates) adds a `Templates` tab and `MsgInstantiateTemplate`; SessionSummary picks up an `IsTemplate` flag.
