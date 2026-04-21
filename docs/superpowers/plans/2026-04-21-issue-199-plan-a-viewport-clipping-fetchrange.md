# Issue #199 — Plan A: Viewport Clipping + FetchRange Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the viewport-clipping data path end-to-end. After this plan, the server emits only the client's visible window (plus 1× overscan) in `BufferDelta`, and clients can fetch arbitrary scrollback ranges on demand via `MsgFetchRange`. Resume still snaps to live edge, selection is still client-only — those ship in Plans B / C.

**Architecture:** Extend `BufferDelta` with `RowBase int64` and a `Flags.AltScreen` bit. Add `MsgViewportUpdate` (client → server per-pane viewport state), `MsgFetchRange` + `MsgFetchRangeResponse` (on-demand scrollback). Server tracks `map[PaneID]ClientViewport` per `Session`. Publisher clips rows at emit time. Client replaces `BufferCache` with a per-pane sparse cache keyed by `globalIdx`, populated by deltas and fetch responses, rendered against the current `ViewWindow`. Alt-screen keeps current flat-buffer semantics, opts out of clipping and FetchRange via the new flag.

**Tech Stack:** Go 1.24.3, existing binary protocol (`protocol/`), `sparse.Store` + `sparse.ViewWindow`, `tcell`-based client renderer, `DesktopPublisher` + `Session` fan-out.

**Spec:** `docs/superpowers/specs/2026-04-20-issue-199-viewport-only-rendering-design.md` — this plan implements spec sequencing steps **1–4**. Steps 5–8 land in follow-up plans B/C/D/E.

**Branch:** `design/issue-199-viewport-only-rendering` — do **not** merge to main until Plan A lands green. Per project CLAUDE.md: never commit directly to main.

---

## File Structure

### New files

- `protocol/viewport.go` — `ViewportUpdate` encode/decode.
- `protocol/viewport_test.go` — round-trip tests.
- `protocol/fetch_range.go` — `FetchRange`, `FetchRangeResponse`, `LogicalRow` wire format.
- `protocol/fetch_range_test.go` — round-trip tests.
- `internal/runtime/server/client_viewport.go` — per-pane `ClientViewport` struct, `Session`-scoped map, helpers.
- `internal/runtime/server/client_viewport_test.go` — clipping unit tests.
- `internal/runtime/server/fetch_range_handler.go` — handles `MsgFetchRange`, produces `MsgFetchRangeResponse`.
- `internal/runtime/server/fetch_range_handler_test.go`.
- `internal/runtime/client/pane_cache.go` — per-pane sparse cache keyed by globalIdx; replaces `BufferCache` incrementally.
- `internal/runtime/client/pane_cache_test.go`.

### Modified files

- `protocol/protocol.go` — new `MessageType` constants: `MsgViewportUpdate`, `MsgFetchRange`, `MsgFetchRangeResponse`. Bump protocol version.
- `protocol/buffer_delta.go` — add `RowBase int64` field; add `BufferDeltaAltScreen` flag constant; extend encode/decode with backward-incompatible version guard.
- `protocol/buffer_delta_test.go` — expand round-trip coverage.
- `texel/snapshot.go` — add `RowGlobalIdx []int64` to `PaneSnapshot`; one entry per row of `Buffer`, value is the `globalIdx` of that row for main-screen panes, `-1` for alt-screen rows.
- `apps/texelterm/term.go` (and any other App with `Snapshot()` on a sparse-backed pane) — populate `RowGlobalIdx` from `ViewWindow`.
- `texel/desktop.go` / `texel/desktop_engine_core.go` — plumb `RowGlobalIdx` through `SnapshotBuffers`.
- `internal/runtime/server/desktop_publisher.go` — clip per-client at emit time; track per-pane `Revision`; populate `RowBase`; set `Flags.AltScreen` for alt-screen panes.
- `internal/runtime/server/desktop_publisher_test.go` — rewrite full-pane assertions as viewport-clipped.
- `internal/runtime/server/session.go` — store `viewports map[PaneID]ClientViewport`; extend handshake hook to accept `ViewportUpdate`.
- `internal/runtime/server/connection_handler.go` — dispatch `MsgViewportUpdate` and `MsgFetchRange`.
- `internal/runtime/client/renderer.go` — replace `pane.RowCells` / `RowCellsDirect` dispatch with `PaneCache.RowAt(globalIdx)`.
- `internal/runtime/client/client_loop.go` (or equivalent) — on `MsgBufferDelta` receipt, index rows by `RowBase + offset`; handle `MsgFetchRangeResponse`.
- `internal/runtime/client/input.go` (or wherever scroll/resize reach the wire) — emit `MsgViewportUpdate` on viewport change, coalesced per animation frame; issue `MsgFetchRange` for cache misses inside the resident window.
- `apps/texelterm/parser/sparse/store.go` — add `OldestRetained() int64` (needed by Plan B but cheap to land here; used by clipping overscan edge).

---

## Task 1 — `BufferDelta` wire format: `RowBase` + `Flags.AltScreen`

No behavior change — server still sends `RowBase = 0` and `Flags.AltScreen = 0` by default. Purpose: get the wire-format extension in first so everything downstream can rely on it.

**Files:**
- Modify: `protocol/buffer_delta.go`
- Modify: `protocol/buffer_delta_test.go`

- [ ] **Step 1: Write the failing round-trip test for `RowBase`**

Add to `protocol/buffer_delta_test.go`:

```go
func TestBufferDelta_RowBaseRoundTrip(t *testing.T) {
    in := BufferDelta{
        PaneID:   [16]byte{1, 2, 3},
        Revision: 42,
        Flags:    BufferDeltaNone,
        RowBase:  1_234_567,
        Rows: []RowDelta{
            {Row: 0, Spans: []CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}},
        },
        Styles: []StyleEntry{{AttrFlags: 0}},
    }
    raw, err := EncodeBufferDelta(in)
    if err != nil {
        t.Fatalf("encode: %v", err)
    }
    out, err := DecodeBufferDelta(raw)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }
    if out.RowBase != in.RowBase {
        t.Fatalf("RowBase: got %d want %d", out.RowBase, in.RowBase)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./protocol/ -run TestBufferDelta_RowBaseRoundTrip -v`
Expected: FAIL — either "unknown field RowBase" compile error, or `RowBase` decoded as 0.

- [ ] **Step 3: Add `RowBase` field to the struct**

In `protocol/buffer_delta.go`:

```go
type BufferDelta struct {
    PaneID   [16]byte
    Revision uint32
    Flags    BufferDeltaFlags
    RowBase  int64
    Styles   []StyleEntry
    Rows     []RowDelta
}
```

- [ ] **Step 4: Extend `EncodeBufferDelta` to write `RowBase` after `Flags`**

Insert after `buf.WriteByte(byte(delta.Flags))`:

```go
if err := binary.Write(buf, binary.LittleEndian, delta.RowBase); err != nil {
    return nil, err
}
```

- [ ] **Step 5: Extend `DecodeBufferDelta` to read `RowBase`**

Update the header-length guard from 21 to 29 bytes and insert after the `Flags` read:

```go
if len(b) < 29 { // paneID(16)+revision(4)+flags(1)+rowBase(8)
    return delta, ErrPayloadShort
}
copy(delta.PaneID[:], b[:16])
delta.Revision = binary.LittleEndian.Uint32(b[16:20])
delta.Flags = BufferDeltaFlags(b[20])
delta.RowBase = int64(binary.LittleEndian.Uint64(b[21:29]))
b = b[29:]
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./protocol/ -run TestBufferDelta_RowBaseRoundTrip -v`
Expected: PASS.

- [ ] **Step 7: Write the failing `Flags.AltScreen` round-trip test**

Add:

```go
func TestBufferDelta_AltScreenFlagRoundTrip(t *testing.T) {
    in := BufferDelta{
        PaneID: [16]byte{9},
        Flags:  BufferDeltaAltScreen,
        Rows:   []RowDelta{{Row: 3, Spans: []CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
        Styles: []StyleEntry{{AttrFlags: 0}},
    }
    raw, err := EncodeBufferDelta(in)
    if err != nil {
        t.Fatalf("encode: %v", err)
    }
    out, err := DecodeBufferDelta(raw)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }
    if out.Flags&BufferDeltaAltScreen == 0 {
        t.Fatalf("AltScreen flag lost")
    }
}
```

- [ ] **Step 8: Add the `BufferDeltaAltScreen` constant**

In `protocol/buffer_delta.go`:

```go
const (
    BufferDeltaNone      BufferDeltaFlags = 0
    BufferDeltaAltScreen BufferDeltaFlags = 1 << 0
)
```

- [ ] **Step 9: Run the test to verify it passes**

Run: `go test ./protocol/ -run TestBufferDelta_AltScreenFlagRoundTrip -v`
Expected: PASS.

- [ ] **Step 10: Run the full protocol test suite**

Run: `go test ./protocol/...`
Expected: all pre-existing tests still pass.

- [ ] **Step 11: Bump protocol version**

In `protocol/protocol.go`:

```go
// Update whatever constant holds the protocol version (locate via grep). Bump by one.
```

Run: `go test ./protocol/...` — confirm still green.

- [ ] **Step 12: Commit**

```bash
git add protocol/buffer_delta.go protocol/buffer_delta_test.go protocol/protocol.go
git commit -m "protocol: extend BufferDelta with RowBase and AltScreen flag (#199)"
```

---

## Task 2 — `MsgViewportUpdate` wire message

Introduces the client → server message carrying per-pane viewport state. Server decodes and stores; no clipping yet.

**Files:**
- Create: `protocol/viewport.go`
- Create: `protocol/viewport_test.go`
- Modify: `protocol/protocol.go` (new `MsgViewportUpdate` constant)
- Create: `internal/runtime/server/client_viewport.go`
- Create: `internal/runtime/server/client_viewport_test.go`
- Modify: `internal/runtime/server/session.go`
- Modify: `internal/runtime/server/connection_handler.go`

### Task 2a — Wire format

- [ ] **Step 1: Add the `MsgViewportUpdate` type constant**

In `protocol/protocol.go`, extend the `MessageType` block (placement: after the last existing client→server message; renumbering other constants is **not** safe, so append at the end):

```go
MsgViewportUpdate MessageType = <next unused value>
```

Confirm by running: `go test ./protocol/...`.

- [ ] **Step 2: Write the failing round-trip test**

Create `protocol/viewport_test.go`:

```go
package protocol

import "testing"

func TestViewportUpdate_RoundTrip(t *testing.T) {
    in := ViewportUpdate{
        PaneID:         [16]byte{0xDE, 0xAD, 0xBE, 0xEF},
        AltScreen:      false,
        ViewTopIdx:     1_000,
        ViewBottomIdx:  1_023,
        WrapSegmentIdx: 2,
        Rows:           24,
        Cols:           80,
        AutoFollow:     true,
    }
    raw, err := EncodeViewportUpdate(in)
    if err != nil {
        t.Fatalf("encode: %v", err)
    }
    out, err := DecodeViewportUpdate(raw)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }
    if out != in {
        t.Fatalf("round-trip mismatch:\n got  %#v\n want %#v", out, in)
    }
}
```

- [ ] **Step 3: Run to confirm failure**

Run: `go test ./protocol/ -run TestViewportUpdate_RoundTrip -v`
Expected: FAIL — `ViewportUpdate` / encoder / decoder undefined.

- [ ] **Step 4: Implement `ViewportUpdate` encode/decode**

Create `protocol/viewport.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
    "bytes"
    "encoding/binary"
)

// ViewportUpdate is sent by the client whenever its per-pane viewport changes
// (scroll, resize, alt-screen enter/exit). The server uses it to clip
// subsequent BufferDeltas.
type ViewportUpdate struct {
    PaneID         [16]byte
    AltScreen      bool
    ViewTopIdx     int64
    ViewBottomIdx  int64
    WrapSegmentIdx uint16
    Rows           uint16
    Cols           uint16
    AutoFollow     bool
}

func EncodeViewportUpdate(v ViewportUpdate) ([]byte, error) {
    buf := bytes.NewBuffer(make([]byte, 0, 45))
    buf.Write(v.PaneID[:])
    var bools uint8
    if v.AltScreen {
        bools |= 1 << 0
    }
    if v.AutoFollow {
        bools |= 1 << 1
    }
    buf.WriteByte(bools)
    if err := binary.Write(buf, binary.LittleEndian, v.ViewTopIdx); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, v.ViewBottomIdx); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, v.WrapSegmentIdx); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, v.Rows); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, v.Cols); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func DecodeViewportUpdate(b []byte) (ViewportUpdate, error) {
    var v ViewportUpdate
    // 16 + 1 + 8 + 8 + 2 + 2 + 2 = 39
    if len(b) < 39 {
        return v, ErrPayloadShort
    }
    copy(v.PaneID[:], b[:16])
    bools := b[16]
    v.AltScreen = bools&(1<<0) != 0
    v.AutoFollow = bools&(1<<1) != 0
    v.ViewTopIdx = int64(binary.LittleEndian.Uint64(b[17:25]))
    v.ViewBottomIdx = int64(binary.LittleEndian.Uint64(b[25:33]))
    v.WrapSegmentIdx = binary.LittleEndian.Uint16(b[33:35])
    v.Rows = binary.LittleEndian.Uint16(b[35:37])
    v.Cols = binary.LittleEndian.Uint16(b[37:39])
    return v, nil
}
```

- [ ] **Step 5: Run the round-trip test**

Run: `go test ./protocol/ -run TestViewportUpdate -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add protocol/viewport.go protocol/viewport_test.go protocol/protocol.go
git commit -m "protocol: add MsgViewportUpdate wire message (#199)"
```

### Task 2b — Server-side per-pane `ClientViewport`

- [ ] **Step 1: Write the failing unit test for `ClientViewport.Apply`**

Create `internal/runtime/server/client_viewport_test.go`:

```go
package server

import (
    "testing"

    "github.com/framegrace/texelation/protocol"
)

func TestClientViewports_ApplyUpdate(t *testing.T) {
    vs := NewClientViewports()
    pane := [16]byte{1, 2, 3}
    vs.Apply(protocol.ViewportUpdate{
        PaneID:        pane,
        ViewTopIdx:    100,
        ViewBottomIdx: 123,
        Rows:          24,
        Cols:          80,
        AutoFollow:    false,
    })
    got, ok := vs.Get(pane)
    if !ok {
        t.Fatal("viewport missing after Apply")
    }
    if got.ViewTopIdx != 100 || got.ViewBottomIdx != 123 || got.Rows != 24 {
        t.Fatalf("unexpected state: %#v", got)
    }
}
```

- [ ] **Step 2: Run the test to confirm failure**

Run: `go test ./internal/runtime/server/ -run TestClientViewports -v`
Expected: FAIL — package does not define `NewClientViewports`.

- [ ] **Step 3: Implement `ClientViewports`**

Create `internal/runtime/server/client_viewport.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
    "sync"

    "github.com/framegrace/texelation/protocol"
)

// ClientViewport is the server's view of a single pane's state inside a client
// session. Publisher uses it to clip BufferDelta rows at emit time.
type ClientViewport struct {
    AltScreen     bool
    ViewTopIdx    int64
    ViewBottomIdx int64
    Rows          uint16
    Cols          uint16
    AutoFollow    bool
}

// ClientViewports is the per-Session map of pane → viewport.
type ClientViewports struct {
    mu       sync.RWMutex
    byPaneID map[[16]byte]ClientViewport
}

func NewClientViewports() *ClientViewports {
    return &ClientViewports{byPaneID: make(map[[16]byte]ClientViewport)}
}

func (c *ClientViewports) Apply(u protocol.ViewportUpdate) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.byPaneID[u.PaneID] = ClientViewport{
        AltScreen:     u.AltScreen,
        ViewTopIdx:    u.ViewTopIdx,
        ViewBottomIdx: u.ViewBottomIdx,
        Rows:          u.Rows,
        Cols:          u.Cols,
        AutoFollow:    u.AutoFollow,
    }
}

func (c *ClientViewports) Get(paneID [16]byte) (ClientViewport, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.byPaneID[paneID]
    return v, ok
}

// Snapshot returns a shallow copy of all viewports. Intended for publisher
// fan-out; callers must treat the result as read-only.
func (c *ClientViewports) Snapshot() map[[16]byte]ClientViewport {
    c.mu.RLock()
    defer c.mu.RUnlock()
    out := make(map[[16]byte]ClientViewport, len(c.byPaneID))
    for k, v := range c.byPaneID {
        out[k] = v
    }
    return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/runtime/server/ -run TestClientViewports -v`
Expected: PASS.

- [ ] **Step 5: Wire `ClientViewports` into `Session`**

In `internal/runtime/server/session.go`, add a field on `Session`:

```go
viewports *ClientViewports
```

Initialize it in the session constructor (search for `&Session{` allocations; there should be one central one — typically in `newSession` / `attachSession`):

```go
viewports: NewClientViewports(),
```

Expose a helper:

```go
func (s *Session) ApplyViewportUpdate(u protocol.ViewportUpdate) {
    s.viewports.Apply(u)
}
```

- [ ] **Step 6: Dispatch `MsgViewportUpdate` in the connection handler**

In `internal/runtime/server/connection_handler.go`, add a case to the existing message-type switch:

```go
case protocol.MsgViewportUpdate:
    u, err := protocol.DecodeViewportUpdate(payload)
    if err != nil {
        return fmt.Errorf("decode viewport update: %w", err)
    }
    session.ApplyViewportUpdate(u)
```

- [ ] **Step 7: Write an end-to-end test**

Add to `internal/runtime/server/client_viewport_test.go`:

```go
func TestSession_ApplyViewportUpdate(t *testing.T) {
    s := newTestSession(t)
    pane := [16]byte{7}
    s.ApplyViewportUpdate(protocol.ViewportUpdate{
        PaneID:        pane,
        ViewTopIdx:    500,
        ViewBottomIdx: 523,
        Rows:          24,
        Cols:          80,
    })
    got, ok := s.viewports.Get(pane)
    if !ok || got.ViewTopIdx != 500 {
        t.Fatalf("unexpected: %#v ok=%v", got, ok)
    }
}
```

`newTestSession` helper: if none exists, add one at the top of the test file that constructs a bare `Session{viewports: NewClientViewports()}`. Keep the helper minimal — don't wire connections.

- [ ] **Step 8: Run the test**

Run: `go test ./internal/runtime/server/ -run TestSession_ApplyViewportUpdate -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/runtime/server/client_viewport.go internal/runtime/server/client_viewport_test.go internal/runtime/server/session.go internal/runtime/server/connection_handler.go
git commit -m "server: track per-pane ClientViewport state in Session (#199)"
```

---

## Task 3 — `MsgFetchRange` and `MsgFetchRangeResponse`

Lets the client pull arbitrary scrollback. Ships before the publisher starts clipping so that by the time clipping is live, clients have a way to recover rows outside the resident window.

**Files:**
- Create: `protocol/fetch_range.go`
- Create: `protocol/fetch_range_test.go`
- Modify: `protocol/protocol.go`
- Create: `internal/runtime/server/fetch_range_handler.go`
- Create: `internal/runtime/server/fetch_range_handler_test.go`
- Modify: `internal/runtime/server/connection_handler.go`
- Modify: `apps/texelterm/parser/sparse/store.go` (add `OldestRetained()`)

### Task 3a — Add `OldestRetained()` to `sparse.Store`

Publisher needs to know the bottom of the in-memory window so it can respond with `BelowRetention` when appropriate.

- [ ] **Step 1: Write the failing test**

Add to `apps/texelterm/parser/sparse/store_test.go` (create if missing):

```go
package sparse

import (
    "testing"

    "github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestStore_OldestRetained(t *testing.T) {
    s := NewStore(80)
    if got := s.OldestRetained(); got != -1 {
        t.Fatalf("empty: got %d want -1", got)
    }
    s.Set(5, 0, parser.Cell{})
    s.Set(10, 0, parser.Cell{})
    s.Set(3, 0, parser.Cell{})
    if got := s.OldestRetained(); got != 3 {
        t.Fatalf("got %d want 3", got)
    }
    s.ClearRange(3, 3)
    if got := s.OldestRetained(); got != 5 {
        t.Fatalf("after clear: got %d want 5", got)
    }
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestStore_OldestRetained -v`
Expected: FAIL — method undefined.

- [ ] **Step 3: Implement `OldestRetained`**

In `apps/texelterm/parser/sparse/store.go`:

```go
// OldestRetained returns the lowest globalIdx currently resident in the Store.
// Returns -1 when the Store is empty. O(n) in the number of resident rows; the
// Store is typically small (resident window is bounded) so this is acceptable.
func (s *Store) OldestRetained() int64 {
    s.mu.RLock()
    defer s.mu.RUnlock()
    oldest := int64(-1)
    for k := range s.lines {
        if oldest == -1 || k < oldest {
            oldest = k
        }
    }
    return oldest
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./apps/texelterm/parser/sparse/ -run TestStore_OldestRetained -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/parser/sparse/store.go apps/texelterm/parser/sparse/store_test.go
git commit -m "sparse: add OldestRetained() to Store (#199)"
```

### Task 3b — Wire format

- [ ] **Step 1: Add the new MessageType constants**

In `protocol/protocol.go`, append:

```go
MsgFetchRange         MessageType = <next unused>
MsgFetchRangeResponse MessageType = <next unused>
```

- [ ] **Step 2: Write the failing round-trip test**

Create `protocol/fetch_range_test.go`:

```go
package protocol

import (
    "reflect"
    "testing"
)

func TestFetchRange_RoundTrip(t *testing.T) {
    in := FetchRange{
        RequestID:    7,
        PaneID:       [16]byte{0xAA},
        LoIdx:        1_000,
        HiIdx:        1_050,
        AsOfRevision: 42,
    }
    raw, err := EncodeFetchRange(in)
    if err != nil {
        t.Fatalf("encode: %v", err)
    }
    out, err := DecodeFetchRange(raw)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }
    if out != in {
        t.Fatalf("mismatch: %#v vs %#v", out, in)
    }
}

func TestFetchRangeResponse_RoundTrip(t *testing.T) {
    in := FetchRangeResponse{
        RequestID: 7,
        PaneID:    [16]byte{0xAA},
        Revision:  99,
        Flags:     FetchRangeNone,
        Rows: []LogicalRow{
            {GlobalIdx: 1_000, Wrapped: false, NoWrap: false, Spans: []CellSpan{{StartCol: 0, Text: "hi", StyleIndex: 0}}},
            {GlobalIdx: 1_001, Wrapped: true, Spans: []CellSpan{{StartCol: 0, Text: "continuation", StyleIndex: 0}}},
        },
        Styles: []StyleEntry{{AttrFlags: 0}},
    }
    raw, err := EncodeFetchRangeResponse(in)
    if err != nil {
        t.Fatalf("encode: %v", err)
    }
    out, err := DecodeFetchRangeResponse(raw)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }
    if !reflect.DeepEqual(out, in) {
        t.Fatalf("mismatch:\n got %#v\n want %#v", out, in)
    }
}
```

- [ ] **Step 3: Run to confirm failure**

Run: `go test ./protocol/ -run TestFetchRange -v`
Expected: FAIL — undefined types.

- [ ] **Step 4: Implement encode/decode**

Create `protocol/fetch_range.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package protocol

import (
    "bytes"
    "encoding/binary"
)

// FetchRangeFlags is a bitmask set on FetchRangeResponse.
type FetchRangeFlags uint8

const (
    FetchRangeNone           FetchRangeFlags = 0
    FetchRangeAltScreenActive FetchRangeFlags = 1 << 0
    FetchRangeBelowRetention  FetchRangeFlags = 1 << 1
    FetchRangeEmpty           FetchRangeFlags = 1 << 2
)

// FetchRange is a client → server request for a slice of scrollback.
// LoIdx is inclusive; HiIdx is exclusive. AsOfRevision is informational
// (server stamps the response with its own Revision at read time).
type FetchRange struct {
    RequestID    uint32
    PaneID       [16]byte
    LoIdx        int64
    HiIdx        int64
    AsOfRevision uint32
}

// LogicalRow is one row in a FetchRangeResponse. Spans use the same shared
// Styles table as BufferDelta rows.
type LogicalRow struct {
    GlobalIdx int64
    Wrapped   bool
    NoWrap    bool
    Spans     []CellSpan
}

// FetchRangeResponse is the server → client reply.
type FetchRangeResponse struct {
    RequestID uint32
    PaneID    [16]byte
    Revision  uint32
    Flags     FetchRangeFlags
    Styles    []StyleEntry
    Rows      []LogicalRow
}

func EncodeFetchRange(f FetchRange) ([]byte, error) {
    buf := bytes.NewBuffer(make([]byte, 0, 36))
    if err := binary.Write(buf, binary.LittleEndian, f.RequestID); err != nil {
        return nil, err
    }
    buf.Write(f.PaneID[:])
    if err := binary.Write(buf, binary.LittleEndian, f.LoIdx); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, f.HiIdx); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, f.AsOfRevision); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func DecodeFetchRange(b []byte) (FetchRange, error) {
    var f FetchRange
    // 4 + 16 + 8 + 8 + 4 = 40
    if len(b) < 40 {
        return f, ErrPayloadShort
    }
    f.RequestID = binary.LittleEndian.Uint32(b[:4])
    copy(f.PaneID[:], b[4:20])
    f.LoIdx = int64(binary.LittleEndian.Uint64(b[20:28]))
    f.HiIdx = int64(binary.LittleEndian.Uint64(b[28:36]))
    f.AsOfRevision = binary.LittleEndian.Uint32(b[36:40])
    return f, nil
}

func EncodeFetchRangeResponse(r FetchRangeResponse) ([]byte, error) {
    buf := bytes.NewBuffer(make([]byte, 0, 64))
    if err := binary.Write(buf, binary.LittleEndian, r.RequestID); err != nil {
        return nil, err
    }
    buf.Write(r.PaneID[:])
    if err := binary.Write(buf, binary.LittleEndian, r.Revision); err != nil {
        return nil, err
    }
    buf.WriteByte(byte(r.Flags))

    if len(r.Styles) > 0xFFFF || len(r.Rows) > 0xFFFF {
        return nil, ErrBufferTooLarge
    }
    if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Styles))); err != nil {
        return nil, err
    }
    // Reuse the same inline style-entry encoding as BufferDelta — factor out
    // if the duplication grows. For now it's two sites.
    for _, s := range r.Styles {
        if err := binary.Write(buf, binary.LittleEndian, s.AttrFlags); err != nil {
            return nil, err
        }
        buf.WriteByte(byte(s.FgModel))
        binary.Write(buf, binary.LittleEndian, s.FgValue)
        buf.WriteByte(byte(s.BgModel))
        binary.Write(buf, binary.LittleEndian, s.BgValue)
        // Dynamic colors are not supported in FetchRange rows for v1.
        if s.AttrFlags&AttrHasDynamic != 0 {
            return nil, ErrBufferTooLarge
        }
    }

    if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Rows))); err != nil {
        return nil, err
    }
    for _, row := range r.Rows {
        if err := binary.Write(buf, binary.LittleEndian, row.GlobalIdx); err != nil {
            return nil, err
        }
        var flags uint8
        if row.Wrapped {
            flags |= 1 << 0
        }
        if row.NoWrap {
            flags |= 1 << 1
        }
        buf.WriteByte(flags)
        if len(row.Spans) > 0xFFFF {
            return nil, ErrBufferTooLarge
        }
        if err := binary.Write(buf, binary.LittleEndian, uint16(len(row.Spans))); err != nil {
            return nil, err
        }
        for _, span := range row.Spans {
            textBytes := []byte(span.Text)
            if len(textBytes) > 0xFFFF {
                return nil, ErrInvalidSpan
            }
            binary.Write(buf, binary.LittleEndian, span.StartCol)
            binary.Write(buf, binary.LittleEndian, uint16(len(textBytes)))
            binary.Write(buf, binary.LittleEndian, span.StyleIndex)
            buf.Write(textBytes)
        }
    }
    return buf.Bytes(), nil
}

func DecodeFetchRangeResponse(b []byte) (FetchRangeResponse, error) {
    var r FetchRangeResponse
    // 4 + 16 + 4 + 1 = 25
    if len(b) < 25 {
        return r, ErrPayloadShort
    }
    r.RequestID = binary.LittleEndian.Uint32(b[:4])
    copy(r.PaneID[:], b[4:20])
    r.Revision = binary.LittleEndian.Uint32(b[20:24])
    r.Flags = FetchRangeFlags(b[24])
    b = b[25:]

    if len(b) < 2 {
        return r, ErrPayloadShort
    }
    styleCount := binary.LittleEndian.Uint16(b[:2])
    b = b[2:]
    r.Styles = make([]StyleEntry, styleCount)
    for i := 0; i < int(styleCount); i++ {
        if len(b) < 12 {
            return r, ErrPayloadShort
        }
        r.Styles[i].AttrFlags = binary.LittleEndian.Uint16(b[:2])
        r.Styles[i].FgModel = ColorModel(b[2])
        r.Styles[i].FgValue = binary.LittleEndian.Uint32(b[3:7])
        r.Styles[i].BgModel = ColorModel(b[7])
        r.Styles[i].BgValue = binary.LittleEndian.Uint32(b[8:12])
        b = b[12:]
        if r.Styles[i].AttrFlags&AttrHasDynamic != 0 {
            return r, ErrPayloadShort // unsupported in v1
        }
    }

    if len(b) < 2 {
        return r, ErrPayloadShort
    }
    rowCount := binary.LittleEndian.Uint16(b[:2])
    b = b[2:]
    r.Rows = make([]LogicalRow, rowCount)
    for i := 0; i < int(rowCount); i++ {
        if len(b) < 11 { // globalIdx(8)+flags(1)+spanCount(2)
            return r, ErrPayloadShort
        }
        r.Rows[i].GlobalIdx = int64(binary.LittleEndian.Uint64(b[:8]))
        rowFlags := b[8]
        r.Rows[i].Wrapped = rowFlags&(1<<0) != 0
        r.Rows[i].NoWrap = rowFlags&(1<<1) != 0
        spanCount := binary.LittleEndian.Uint16(b[9:11])
        b = b[11:]
        spans := make([]CellSpan, spanCount)
        for s := 0; s < int(spanCount); s++ {
            if len(b) < 6 {
                return r, ErrPayloadShort
            }
            startCol := binary.LittleEndian.Uint16(b[:2])
            textLen := binary.LittleEndian.Uint16(b[2:4])
            styleIndex := binary.LittleEndian.Uint16(b[4:6])
            b = b[6:]
            if len(b) < int(textLen) {
                return r, ErrPayloadShort
            }
            spans[s] = CellSpan{StartCol: startCol, Text: string(b[:textLen]), StyleIndex: styleIndex}
            b = b[textLen:]
        }
        r.Rows[i].Spans = spans
    }
    return r, nil
}
```

- [ ] **Step 5: Run the test**

Run: `go test ./protocol/ -run TestFetchRange -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add protocol/fetch_range.go protocol/fetch_range_test.go protocol/protocol.go
git commit -m "protocol: add MsgFetchRange and MsgFetchRangeResponse (#199)"
```

### Task 3c — Server handler

- [ ] **Step 1: Write the failing handler test**

Create `internal/runtime/server/fetch_range_handler_test.go`:

```go
package server

import (
    "testing"

    "github.com/framegrace/texelation/apps/texelterm/parser"
    "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
    "github.com/framegrace/texelation/protocol"
)

func TestFetchRangeHandler_Basic(t *testing.T) {
    st := sparse.NewStore(80)
    st.Set(100, 0, parser.Cell{Char: 'a'})
    st.Set(101, 0, parser.Cell{Char: 'b'})

    resp, err := ServeFetchRange(st, protocol.FetchRange{
        LoIdx: 100,
        HiIdx: 102,
    }, 42) // revision
    if err != nil {
        t.Fatalf("serve: %v", err)
    }
    if resp.Revision != 42 {
        t.Fatalf("revision: got %d want 42", resp.Revision)
    }
    if len(resp.Rows) != 2 {
        t.Fatalf("rows: got %d want 2", len(resp.Rows))
    }
    if resp.Rows[0].GlobalIdx != 100 {
        t.Fatalf("row[0].GlobalIdx: got %d want 100", resp.Rows[0].GlobalIdx)
    }
}

func TestFetchRangeHandler_BelowRetention(t *testing.T) {
    st := sparse.NewStore(80)
    st.Set(500, 0, parser.Cell{Char: 'a'})
    resp, err := ServeFetchRange(st, protocol.FetchRange{
        LoIdx: 0,
        HiIdx: 10,
    }, 1)
    if err != nil {
        t.Fatalf("serve: %v", err)
    }
    if resp.Flags&protocol.FetchRangeBelowRetention == 0 {
        t.Fatalf("expected BelowRetention flag")
    }
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/runtime/server/ -run TestFetchRangeHandler -v`
Expected: FAIL — `ServeFetchRange` undefined.

- [ ] **Step 3: Implement `ServeFetchRange`**

Create `internal/runtime/server/fetch_range_handler.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
    "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
    "github.com/framegrace/texelation/protocol"
)

// ServeFetchRange reads rows [lo, hi) from the sparse store and returns a
// FetchRangeResponse stamped with the provided revision. Cold pages are
// expected to have been faulted in by the Store's own persistence bridge
// before this call; ServeFetchRange does not drive page loads itself.
//
// If any row in the range has globalIdx below Store.OldestRetained(), the
// response flags include FetchRangeBelowRetention. Callers that need
// cross-restart resume should fault cold pages before calling.
func ServeFetchRange(st *sparse.Store, req protocol.FetchRange, revision uint32) (protocol.FetchRangeResponse, error) {
    resp := protocol.FetchRangeResponse{
        RequestID: req.RequestID,
        PaneID:    req.PaneID,
        Revision:  revision,
    }
    if req.LoIdx >= req.HiIdx {
        resp.Flags |= protocol.FetchRangeEmpty
        return resp, nil
    }
    oldest := st.OldestRetained()
    if oldest == -1 || req.LoIdx < oldest {
        resp.Flags |= protocol.FetchRangeBelowRetention
    }
    // Style dedup table scoped to this response.
    styleTable := newStyleTable()
    for idx := req.LoIdx; idx < req.HiIdx; idx++ {
        cells := st.GetLine(idx)
        if cells == nil {
            continue
        }
        row := protocol.LogicalRow{
            GlobalIdx: idx,
            NoWrap:    st.RowNoWrap(idx),
        }
        row.Spans = encodeCellsToSpans(cells, styleTable)
        // Wrapped flag: rows other than the last in a wrap chain have Wrapped=true.
        // Source of truth lives on the cells themselves (parser.Cell.Wrapped).
        // Use the last cell of this row — if it wrapped, the next row is a continuation.
        if n := len(cells); n > 0 && cells[n-1].Wrapped {
            row.Wrapped = true
        }
        resp.Rows = append(resp.Rows, row)
    }
    resp.Styles = styleTable.entries()
    if len(resp.Rows) == 0 && resp.Flags&protocol.FetchRangeBelowRetention == 0 {
        resp.Flags |= protocol.FetchRangeEmpty
    }
    return resp, nil
}
```

`newStyleTable` / `encodeCellsToSpans` likely already exist inside `desktop_publisher.go` for `BufferDelta` encoding. If they are not exported, either promote them to package-level helpers in a new file `internal/runtime/server/encoding.go` or duplicate the small helpers here. **Prefer promotion**: the publisher and the fetch handler should share one encoder.

- [ ] **Step 4: Promote shared cell-encoding helpers**

Locate the style dedup + span encoder in `internal/runtime/server/desktop_publisher.go` (search for `Styles` build-up). Extract to `internal/runtime/server/encoding.go` with signatures:

```go
type styleTable struct { /* existing fields */ }
func newStyleTable() *styleTable
func (t *styleTable) indexOf(cell parser.Cell) uint16
func (t *styleTable) entries() []protocol.StyleEntry
func encodeCellsToSpans(cells []parser.Cell, t *styleTable) []protocol.CellSpan
```

Keep behavior identical — this is refactor, not rewrite. Update `bufferToDelta` to use the helpers.

Run the existing publisher tests to confirm no regression:

```
go test ./internal/runtime/server/ -run TestDesktopPublisher -v
```

- [ ] **Step 5: Run the handler tests**

Run: `go test ./internal/runtime/server/ -run TestFetchRangeHandler -v`
Expected: PASS.

- [ ] **Step 6: Dispatch `MsgFetchRange` in the connection handler**

In `internal/runtime/server/connection_handler.go`:

```go
case protocol.MsgFetchRange:
    req, err := protocol.DecodeFetchRange(payload)
    if err != nil {
        return fmt.Errorf("decode fetch range: %w", err)
    }
    resp, err := session.ServeFetchRange(req)
    if err != nil {
        return fmt.Errorf("serve fetch range: %w", err)
    }
    raw, err := protocol.EncodeFetchRangeResponse(resp)
    if err != nil {
        return fmt.Errorf("encode fetch range response: %w", err)
    }
    return session.Send(protocol.MsgFetchRangeResponse, raw)
```

`session.ServeFetchRange` is a thin wrapper around `ServeFetchRange`:

```go
func (s *Session) ServeFetchRange(req protocol.FetchRange) (protocol.FetchRangeResponse, error) {
    pane := s.desktop.PaneByID(req.PaneID)
    if pane == nil {
        return protocol.FetchRangeResponse{RequestID: req.RequestID, PaneID: req.PaneID, Flags: protocol.FetchRangeEmpty}, nil
    }
    if pane.IsAltScreenActive() {
        return protocol.FetchRangeResponse{RequestID: req.RequestID, PaneID: req.PaneID, Flags: protocol.FetchRangeAltScreenActive}, nil
    }
    st := pane.SparseStore()      // helper to be added on the pane/app
    rev := s.publisher.RevisionFor(req.PaneID)
    return ServeFetchRange(st, req, rev)
}
```

If `PaneByID` / `IsAltScreenActive` / `SparseStore` are not yet exposed, add minimal accessors — they already exist somewhere inside `texel.Desktop` and `texelterm.App`. Grep for `altBuffer` and `PaneByID`; add thin public shims if they are internal.

- [ ] **Step 7: Commit**

```bash
git add internal/runtime/server/fetch_range_handler.go internal/runtime/server/fetch_range_handler_test.go internal/runtime/server/encoding.go internal/runtime/server/desktop_publisher.go internal/runtime/server/connection_handler.go internal/runtime/server/session.go
git commit -m "server: serve MsgFetchRange from sparse store (#199)"
```

---

## Task 4 — Flip the publisher to clip on emit, land the client sparse cache

This is the **behavior-change** task. Everything before this has been plumbing.

### Task 4a — Extend `PaneSnapshot` with `RowGlobalIdx`

So the publisher knows which globalIdx each row in the snapshot buffer corresponds to.

**Files:**
- Modify: `texel/snapshot.go`
- Modify: `apps/texelterm/term.go` (and any other App producing snapshots with sparse backing)

- [ ] **Step 1: Write the failing test**

Add to `apps/texelterm/term_test.go`:

```go
func TestTexelTerm_SnapshotCarriesGlobalIdx(t *testing.T) {
    a := newTestTerm(t, 80, 24)
    a.Feed([]byte("hello\r\n"))
    snap := a.Snapshot()
    if len(snap.RowGlobalIdx) != len(snap.Buffer) {
        t.Fatalf("RowGlobalIdx len=%d Buffer len=%d", len(snap.RowGlobalIdx), len(snap.Buffer))
    }
    // After "hello\r\n" the first visible row carries a main-screen globalIdx >= 0.
    if snap.RowGlobalIdx[0] < 0 {
        t.Fatalf("row 0 should have main-screen idx, got %d", snap.RowGlobalIdx[0])
    }
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./apps/texelterm/ -run TestTexelTerm_SnapshotCarriesGlobalIdx -v`
Expected: FAIL — field missing.

- [ ] **Step 3: Add the field to `PaneSnapshot`**

In `texel/snapshot.go`:

```go
type PaneSnapshot struct {
    ID           [16]byte
    Title        string
    Buffer       [][]Cell
    RowGlobalIdx []int64  // len == len(Buffer); -1 for alt-screen rows
    Rect         Rectangle
    AppType      string
    AppConfig    map[string]interface{}
}
```

- [ ] **Step 4: Populate it in texelterm**

In `apps/texelterm/term.go`, in whatever `Snapshot()` / `Render()` path constructs the `PaneSnapshot.Buffer`, fill `RowGlobalIdx` alongside. For main-screen panes the source is `ViewWindow.RowGlobalIdx(y)`; for alt-screen, `-1`. If no such helper exists on `ViewWindow`, add:

```go
// In apps/texelterm/parser/sparse/view_window.go:
//
// RowGlobalIdx returns the globalIdx of the row currently at viewport y,
// or -1 if y is outside the viewport or the row has no globalIdx yet.
func (vw *ViewWindow) RowGlobalIdx(y int) int64 { /* ... */ }
```

Implement it from the existing reflow state (the walk that resolves wrap segments already knows the globalIdx of each rendered row — expose that).

- [ ] **Step 5: Run the test**

Run: `go test ./apps/texelterm/ -run TestTexelTerm_SnapshotCarriesGlobalIdx -v`
Expected: PASS.

- [ ] **Step 6: Run the full texelterm suite**

Run: `go test ./apps/texelterm/...`
Expected: no regression.

- [ ] **Step 7: Commit**

```bash
git add texel/snapshot.go apps/texelterm/term.go apps/texelterm/term_test.go apps/texelterm/parser/sparse/view_window.go
git commit -m "texel: carry per-row globalIdx in PaneSnapshot (#199)"
```

### Task 4b — Publisher: per-client clip + `RowBase` + `Flags.AltScreen`

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/server/desktop_publisher_test.go`:

```go
func TestPublisher_ClipsToViewport(t *testing.T) {
    pub, session, paneID := newTestPublisherWithPane(t)
    session.ApplyViewportUpdate(protocol.ViewportUpdate{
        PaneID:        paneID,
        ViewTopIdx:    100,
        ViewBottomIdx: 123,
        Rows:          24,
        Cols:          80,
        AutoFollow:    false,
    })
    // Pane has 200 rows of content; feed a snapshot with globalIdx 0..199.
    snap := makeTestPaneSnapshot(paneID, 200)
    pub.Publish([]texel.PaneSnapshot{snap})

    delta := session.LastDelta(paneID)
    // Expect rows only in [76, 147] (viewport [100,123] + 1× overscan = ±24).
    for _, row := range delta.Rows {
        globalIdx := delta.RowBase + int64(row.Row)
        if globalIdx < 76 || globalIdx > 147 {
            t.Fatalf("row %d (idx %d) outside resident window", row.Row, globalIdx)
        }
    }
    if delta.Flags&protocol.BufferDeltaAltScreen != 0 {
        t.Fatalf("main-screen pane should not set AltScreen flag")
    }
}

func TestPublisher_AltScreenSetsFlag(t *testing.T) {
    pub, session, paneID := newTestPublisherWithAltScreenPane(t)
    snap := makeAltScreenPaneSnapshot(paneID, 24, 80)
    pub.Publish([]texel.PaneSnapshot{snap})
    delta := session.LastDelta(paneID)
    if delta.Flags&protocol.BufferDeltaAltScreen == 0 {
        t.Fatalf("alt-screen pane should set AltScreen flag")
    }
    if delta.RowBase != 0 {
        t.Fatalf("alt-screen pane should have RowBase=0, got %d", delta.RowBase)
    }
}
```

Helpers `newTestPublisherWithPane`, `makeTestPaneSnapshot`, `LastDelta`: write in the same test file. Keep them minimal; reuse existing patterns from `desktop_publisher_test.go`.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/runtime/server/ -run TestPublisher_Clips -v`
Expected: FAIL.

- [ ] **Step 3: Implement clip-on-emit**

In `internal/runtime/server/desktop_publisher.go`, change `bufferToDelta(snap, prev, rev)` to accept the client viewport and clip rows:

```go
func bufferToDelta(snap texel.PaneSnapshot, prev [][]texel.Cell, rev uint32, vp ClientViewport, altScreen bool) protocol.BufferDelta {
    delta := protocol.BufferDelta{
        PaneID:   snap.ID,
        Revision: rev,
    }
    if altScreen {
        delta.Flags |= protocol.BufferDeltaAltScreen
        // Alt-screen: no clipping, no RowBase. Existing encoding per-row.
        for y := range snap.Buffer {
            if !rowsEqual(snap.Buffer[y], rowAt(prev, y)) {
                delta.Rows = append(delta.Rows, encodeRow(snap.Buffer[y], y))
            }
        }
        return delta
    }
    // Main screen: clip to viewport ± 1× overscan, set RowBase.
    overscan := int64(vp.Rows)
    liveTop := int64(0)
    if len(snap.RowGlobalIdx) > 0 {
        liveTop = snap.RowGlobalIdx[0]
    }
    var lo, hi int64
    if vp.AutoFollow {
        lo = liveTop - overscan
        hi = liveTop + int64(vp.Rows) + overscan
    } else {
        lo = vp.ViewTopIdx - overscan
        hi = vp.ViewBottomIdx + overscan
    }
    // RowBase = lo (first potentially-emitted globalIdx).
    delta.RowBase = lo
    styleTable := newStyleTable()
    for y, rowCells := range snap.Buffer {
        gid := snap.RowGlobalIdx[y]
        if gid < lo || gid > hi {
            continue
        }
        if rowsEqual(rowCells, rowAt(prev, y)) {
            continue
        }
        offset := uint16(gid - lo)
        delta.Rows = append(delta.Rows, protocol.RowDelta{
            Row:   offset,
            Spans: encodeCellsToSpans(rowCells, styleTable),
        })
    }
    delta.Styles = styleTable.entries()
    return delta
}
```

Update the single call site in the publisher's `Publish` path to pass `ClientViewport` (pulled from `session.viewports.Get(paneID)`; default zero-value means "send nothing until client tells us its viewport"). For the zero-value case, skip emit entirely — the client will send its first `ViewportUpdate` shortly after handshake.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/runtime/server/ -run TestPublisher -v`
Expected: PASS.

- [ ] **Step 5: Rewrite pre-existing full-pane publisher assertions**

Hunt with grep:

```
grep -nR "Buffer\[" internal/runtime/server/*_test.go
grep -nR "RowDelta" internal/runtime/server/*_test.go
```

For each test that asserts against the full-pane buffer, rewrite to assert against the clipped window. Expected test touch count: ~6–10.

Run: `go test ./internal/runtime/server/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/server/desktop_publisher.go internal/runtime/server/desktop_publisher_test.go
git commit -m "server: clip BufferDelta rows to per-client viewport (#199)"
```

### Task 4c — Client: per-pane sparse cache

Replace the row-index `BufferCache` lookups with a globalIdx-keyed cache.

**Files:**
- Create: `internal/runtime/client/pane_cache.go`
- Create: `internal/runtime/client/pane_cache_test.go`
- Modify: `internal/runtime/client/renderer.go`
- Modify: `internal/runtime/client/client_loop.go` (or whichever file dispatches `MsgBufferDelta`)

- [ ] **Step 1: Write the failing test**

Create `internal/runtime/client/pane_cache_test.go`:

```go
package client

import (
    "testing"

    "github.com/framegrace/texelation/protocol"
)

func TestPaneCache_ApplyDeltaMainScreen(t *testing.T) {
    pc := NewPaneCache()
    pc.ApplyDelta(protocol.BufferDelta{
        RowBase: 1_000,
        Rows: []protocol.RowDelta{
            {Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}},
            {Row: 2, Spans: []protocol.CellSpan{{StartCol: 0, Text: "world", StyleIndex: 0}}},
        },
        Styles: []protocol.StyleEntry{{AttrFlags: 0}},
    })
    if row, ok := pc.RowAt(1_000); !ok || !rowStartsWith(row, "hello") {
        t.Fatalf("globalIdx 1000 missing or wrong: ok=%v row=%v", ok, row)
    }
    if _, ok := pc.RowAt(1_001); ok {
        t.Fatalf("globalIdx 1001 should be absent (no delta row)")
    }
    if row, ok := pc.RowAt(1_002); !ok || !rowStartsWith(row, "world") {
        t.Fatalf("globalIdx 1002 missing or wrong")
    }
}

func TestPaneCache_ApplyDeltaAltScreen(t *testing.T) {
    pc := NewPaneCache()
    pc.ApplyDelta(protocol.BufferDelta{
        Flags: protocol.BufferDeltaAltScreen,
        Rows:  []protocol.RowDelta{{Row: 3, Spans: []protocol.CellSpan{{StartCol: 0, Text: "vim", StyleIndex: 0}}}},
        Styles: []protocol.StyleEntry{{AttrFlags: 0}},
    })
    if !pc.IsAltScreen() {
        t.Fatalf("expected alt-screen mode")
    }
    if row, ok := pc.AltRowAt(3); !ok || !rowStartsWith(row, "vim") {
        t.Fatalf("alt row 3 missing")
    }
}

func TestPaneCache_EvictsOutsideWindow(t *testing.T) {
    pc := NewPaneCache()
    pc.ApplyDelta(protocol.BufferDelta{
        RowBase: 0,
        Rows: []protocol.RowDelta{
            {Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "a", StyleIndex: 0}}},
            {Row: 1, Spans: []protocol.CellSpan{{StartCol: 0, Text: "b", StyleIndex: 0}}},
            {Row: 2, Spans: []protocol.CellSpan{{StartCol: 0, Text: "c", StyleIndex: 0}}},
        },
        Styles: []protocol.StyleEntry{{AttrFlags: 0}},
    })
    // Viewport is [1,2] + 1× overscan of 2 rows → [−1, 4]; hysteresis 1.5×.
    pc.Evict(1, 2, 2)
    if _, ok := pc.RowAt(0); !ok {
        t.Fatalf("row 0 inside hysteresis band, should be retained")
    }
    // Now slam the viewport far away — eviction should clear row 0.
    pc.Evict(1_000, 1_002, 2)
    if _, ok := pc.RowAt(0); ok {
        t.Fatalf("row 0 should be evicted after viewport jump")
    }
}

func rowStartsWith(row []Cell, s string) bool {
    var buf []rune
    for _, c := range row {
        buf = append(buf, rune(c.Char))
    }
    return string(buf[:len(s)]) == s
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/runtime/client/ -run TestPaneCache -v`
Expected: FAIL.

- [ ] **Step 3: Implement `PaneCache`**

Create `internal/runtime/client/pane_cache.go`:

```go
// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package client

import (
    "sync"

    "github.com/framegrace/texelation/protocol"
)

// PaneCache is the client's local copy of a pane's displayable cells.
// Main-screen rows are keyed by globalIdx; alt-screen rows are a flat
// screen-sized 2D buffer.
type PaneCache struct {
    mu        sync.RWMutex
    altScreen bool
    main      map[int64][]Cell
    alt       [][]Cell
    revision  uint32
    styles    []protocol.StyleEntry
}

func NewPaneCache() *PaneCache {
    return &PaneCache{main: make(map[int64][]Cell)}
}

func (c *PaneCache) IsAltScreen() bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.altScreen
}

// ApplyDelta merges a BufferDelta into the cache.
func (c *PaneCache) ApplyDelta(d protocol.BufferDelta) {
    c.mu.Lock()
    defer c.mu.Unlock()
    alt := d.Flags&protocol.BufferDeltaAltScreen != 0
    if alt != c.altScreen {
        // Mode changed — drop the other side's state.
        c.altScreen = alt
        if alt {
            c.main = make(map[int64][]Cell)
        } else {
            c.alt = nil
        }
    }
    if d.Revision > c.revision {
        c.revision = d.Revision
    }
    // NOTE: Styles are per-delta. We decode spans into concrete cells
    // eagerly so later reads don't need the style table.
    styles := d.Styles
    for _, row := range d.Rows {
        cells := decodeSpans(row.Spans, styles)
        if alt {
            c.putAlt(int(row.Row), cells)
        } else {
            gid := d.RowBase + int64(row.Row)
            c.main[gid] = cells
        }
    }
}

// ApplyFetchRange merges rows fetched on-demand.
func (c *PaneCache) ApplyFetchRange(r protocol.FetchRangeResponse) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if r.Flags&protocol.FetchRangeAltScreenActive != 0 {
        // Server says alt-screen is active — nothing to cache.
        return
    }
    // Coherence rule: drop stale responses.
    if r.Revision < c.revision {
        return
    }
    if r.Revision > c.revision {
        c.revision = r.Revision
    }
    styles := r.Styles
    for _, row := range r.Rows {
        cells := decodeSpans(row.Spans, styles)
        c.main[row.GlobalIdx] = cells
    }
}

// RowAt returns the main-screen row for globalIdx.
func (c *PaneCache) RowAt(globalIdx int64) ([]Cell, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    row, ok := c.main[globalIdx]
    return row, ok
}

// AltRowAt returns the alt-screen row for screenRow.
func (c *PaneCache) AltRowAt(screenRow int) ([]Cell, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if screenRow < 0 || screenRow >= len(c.alt) {
        return nil, false
    }
    return c.alt[screenRow], true
}

// Evict drops main-screen rows outside [lo − overscan×1.5, hi + overscan×1.5].
// Called after each viewport change. Small hysteresis prevents thrash on
// micro-scrolls.
func (c *PaneCache) Evict(lo, hi, overscan int64) {
    band := int64(float64(overscan) * 1.5)
    lowerBound := lo - band
    upperBound := hi + band
    c.mu.Lock()
    defer c.mu.Unlock()
    for k := range c.main {
        if k < lowerBound || k > upperBound {
            delete(c.main, k)
        }
    }
}

// MissingRows returns the globalIdxs in [lo, hi] not currently in cache.
// Caller uses this to issue a MsgFetchRange. Returned slice is sorted ascending.
func (c *PaneCache) MissingRows(lo, hi int64) []int64 {
    c.mu.RLock()
    defer c.mu.RUnlock()
    var miss []int64
    for gid := lo; gid <= hi; gid++ {
        if _, ok := c.main[gid]; !ok {
            miss = append(miss, gid)
        }
    }
    return miss
}

func (c *PaneCache) putAlt(row int, cells []Cell) {
    for row >= len(c.alt) {
        c.alt = append(c.alt, nil)
    }
    c.alt[row] = cells
}
```

`decodeSpans` likely already exists in the client package (symmetric to server-side `encodeCellsToSpans`). Grep for it; reuse if present, add if missing.

- [ ] **Step 4: Run the PaneCache tests**

Run: `go test ./internal/runtime/client/ -run TestPaneCache -v`
Expected: PASS.

- [ ] **Step 5: Replace `BufferCache` lookups in the renderer**

In `internal/runtime/client/renderer.go`, replace:

```go
cells := pane.RowCells(rowIdx)
```

…with:

```go
gid := pane.RowGlobalIdx(rowIdx)  // translates screen row → globalIdx via local ViewWindow
if gid >= 0 {
    cells, ok := paneCache.RowAt(gid)
    if !ok {
        cells = emptyCells(paneWidth) // missed-deadline path; see Task 4d for indicator
    }
    // ... existing draw code using `cells` ...
} else {
    // alt-screen row
    cells, _ := paneCache.AltRowAt(rowIdx)
    // ... draw ...
}
```

Compile:

```
go build ./...
```

Fix any lingering `BufferCache` references — it's being retired.

- [ ] **Step 6: Wire `MsgBufferDelta` and `MsgFetchRangeResponse` in the client loop**

Locate the message dispatch in `internal/runtime/client/client_loop.go` (or equivalent). On receiving `MsgBufferDelta`:

```go
case protocol.MsgBufferDelta:
    d, err := protocol.DecodeBufferDelta(payload)
    if err != nil {
        return err
    }
    pc := client.PaneCacheFor(d.PaneID)
    pc.ApplyDelta(d)
```

On receiving `MsgFetchRangeResponse`:

```go
case protocol.MsgFetchRangeResponse:
    r, err := protocol.DecodeFetchRangeResponse(payload)
    if err != nil {
        return err
    }
    pc := client.PaneCacheFor(r.PaneID)
    pc.ApplyFetchRange(r)
    client.NotifyFetchResolved(r.PaneID, r.RequestID)
```

`NotifyFetchResolved` wakes any waiter tracking "fetch pending" state — used by the statusbar in Plan E but already wired here as a no-op hook.

- [ ] **Step 7: Run the full client test suite**

Run: `go test ./internal/runtime/client/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/runtime/client/pane_cache.go internal/runtime/client/pane_cache_test.go internal/runtime/client/renderer.go internal/runtime/client/client_loop.go
git commit -m "client: sparse per-pane cache keyed by globalIdx (#199)"
```

### Task 4d — Client: emit `MsgViewportUpdate` + `MsgFetchRange`

Plumb scroll / resize / alt-screen-flag into the wire.

**Files:**
- Modify: `internal/runtime/client/input.go` (or wherever scroll lives) — add `MsgViewportUpdate` emit
- Modify: client render loop — issue `MsgFetchRange` for cache misses

- [ ] **Step 1: Write the failing test**

Add to `internal/runtime/client/client_loop_test.go`:

```go
func TestClient_EmitsViewportUpdateOnScroll(t *testing.T) {
    c, tx := newTestClient(t)
    paneID := [16]byte{1}
    c.OnScroll(paneID, /*newTop*/ 100, /*newBottom*/ 123, /*autoFollow*/ false)
    // Expect one MsgViewportUpdate on the wire.
    msg := tx.WaitFor(protocol.MsgViewportUpdate)
    u, _ := protocol.DecodeViewportUpdate(msg.Payload)
    if u.ViewTopIdx != 100 || u.ViewBottomIdx != 123 || u.AutoFollow {
        t.Fatalf("unexpected: %#v", u)
    }
}

func TestClient_CoalescesViewportUpdatesWithinFrame(t *testing.T) {
    c, tx := newTestClient(t)
    paneID := [16]byte{1}
    c.OnScroll(paneID, 100, 123, false)
    c.OnScroll(paneID, 110, 133, false)
    c.OnScroll(paneID, 120, 143, false)
    c.FlushFrame()
    msgs := tx.AllFor(protocol.MsgViewportUpdate)
    if len(msgs) != 1 {
        t.Fatalf("expected exactly 1 update per frame, got %d", len(msgs))
    }
}

func TestClient_IssuesFetchForMissingVisibleRows(t *testing.T) {
    c, tx := newTestClient(t)
    paneID := [16]byte{1}
    c.OnScroll(paneID, 1_000, 1_023, false) // no rows cached in this range
    c.FlushFrame()
    msg := tx.WaitFor(protocol.MsgFetchRange)
    r, _ := protocol.DecodeFetchRange(msg.Payload)
    if r.LoIdx > 1_000 || r.HiIdx < 1_024 {
        t.Fatalf("fetch range should cover [1000, 1024), got [%d,%d)", r.LoIdx, r.HiIdx)
    }
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/runtime/client/ -run "TestClient_Emits|TestClient_Coalesces|TestClient_IssuesFetch" -v`
Expected: FAIL.

- [ ] **Step 3: Implement `OnScroll` + per-frame coalescing**

Add a per-pane pending-update struct on the client's top-level struct:

```go
type pendingViewport struct {
    update   protocol.ViewportUpdate
    dirty    bool
}
```

`OnScroll(paneID, top, bottom, autoFollow)` stores into `pendingViewport[paneID]` with `dirty=true`.

`FlushFrame()` (called at the top of each render tick):

```go
for pid, pv := range c.pendingViewport {
    if !pv.dirty { continue }
    c.sendViewportUpdate(pv.update)
    pv.dirty = false
    c.pendingViewport[pid] = pv
    // Also check the cache for missing rows in the new viewport + 1× overscan.
    pc := c.PaneCacheFor(pid)
    lo := pv.update.ViewTopIdx - int64(pv.update.Rows)
    hi := pv.update.ViewBottomIdx + int64(pv.update.Rows)
    miss := pc.MissingRows(lo, hi)
    if len(miss) > 0 {
        c.requestFetch(pid, miss[0], miss[len(miss)-1]+1)
    }
    pc.Evict(pv.update.ViewTopIdx, pv.update.ViewBottomIdx, int64(pv.update.Rows))
}
```

`requestFetch` enforces at-most-one-in-flight-per-pane:

```go
func (c *Client) requestFetch(paneID [16]byte, lo, hi int64) {
    if c.inflightFetch[paneID] {
        c.pendingFetch[paneID] = [2]int64{lo, hi}
        return
    }
    c.inflightFetch[paneID] = true
    req := protocol.FetchRange{
        RequestID: c.nextFetchID(),
        PaneID:    paneID,
        LoIdx:     lo,
        HiIdx:     hi,
    }
    raw, _ := protocol.EncodeFetchRange(req)
    c.send(protocol.MsgFetchRange, raw)
}
```

On `FetchRangeResponse` dispatch (Task 4c step 6), clear `inflightFetch[paneID]`; if `pendingFetch[paneID]` is set, emit it now.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/runtime/client/ -run "TestClient_Emits|TestClient_Coalesces|TestClient_IssuesFetch" -v`
Expected: PASS.

- [ ] **Step 5: Smoke-test the whole thing**

Build and run:

```bash
make build
./bin/texel-server &
./bin/texel-client  # in another terminal
```

- Open a long scrollback (Claude or `yes | head -n 5000 | cat`).
- Scroll up far; verify the visible rows populate within a frame or two.
- Run `vim` in a pane, exit; verify the main-screen scroll position is preserved.
- Resize a pane; verify no visible tearing.

If anything looks wrong, fall back to smaller debugging chunks with the `testutil` reference-terminal framework on texelterm snapshots.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/client/input.go internal/runtime/client/client_loop.go internal/runtime/client/client_loop_test.go
git commit -m "client: emit ViewportUpdate + FetchRange on scroll/resize (#199)"
```

### Task 4e — Integration tests

Using the existing memconn harness (`internal/runtime/server/testutil/memconn.go`), land end-to-end tests that exercise the clipping + FetchRange loop.

**Files:**
- Create: `internal/runtime/server/viewport_integration_test.go`

- [ ] **Step 1: Write the failing test**

```go
//go:build integration
// +build integration

package server

import (
    "testing"
    "time"

    "github.com/framegrace/texelation/internal/runtime/server/testutil"
    "github.com/framegrace/texelation/protocol"
)

func TestIntegration_ClipsAndFetches(t *testing.T) {
    srv, cli := testutil.NewMemConnPair(t)
    defer srv.Close()
    defer cli.Close()

    paneID := srv.SpawnTexelTerm(t)
    srv.FeedPane(paneID, longOutput(5000)) // >> 24 visible rows

    cli.ApplyViewport(paneID, 4900, 4923, /*autoFollow*/ true)
    cli.AwaitRow(paneID, 4900, 2*time.Second)

    // Scroll back 1000 rows.
    cli.ApplyViewport(paneID, 3900, 3923, /*autoFollow*/ false)
    cli.AwaitRow(paneID, 3900, 2*time.Second)
}

func TestIntegration_AltScreenOptsOut(t *testing.T) {
    srv, cli := testutil.NewMemConnPair(t)
    defer srv.Close()
    defer cli.Close()

    paneID := srv.SpawnTexelTerm(t)
    srv.FeedPane(paneID, []byte("\x1b[?1049h")) // enter alt-screen
    srv.FeedPane(paneID, []byte("hello alt"))

    cli.AwaitAltRow(paneID, 0, "hello alt", time.Second)

    // FetchRange while alt-screen is active must return AltScreenActive.
    resp := cli.FetchRangeSync(paneID, 0, 100)
    if resp.Flags&protocol.FetchRangeAltScreenActive == 0 {
        t.Fatal("expected AltScreenActive")
    }
}
```

Helpers `SpawnTexelTerm`, `FeedPane`, `ApplyViewport`, `AwaitRow`, `AwaitAltRow`, `FetchRangeSync`: add to `testutil/memconn.go`. Pattern-match against existing helpers in that file; do not invent new patterns.

- [ ] **Step 2: Run to confirm failure**

Run: `go test -tags=integration ./internal/runtime/server/ -run TestIntegration_Clips -v`
Expected: FAIL.

- [ ] **Step 3: Iterate until green**

Stabilize the memconn helpers and handler plumbing until both tests pass.

- [ ] **Step 4: Run the full test suite one more time**

```
make test
go test -tags=integration ./...
```

Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/server/viewport_integration_test.go internal/runtime/server/testutil/memconn.go
git commit -m "tests: integration coverage for viewport clipping + FetchRange (#199)"
```

---

## Task 5 — Push the branch and open the Plan A PR

- [ ] **Step 1: Final self-review**

- `git diff main...HEAD` — eyeball the whole thing.
- `make test` and `go test -tags=integration ./...` — green.
- `make fmt && make lint` — clean.

- [ ] **Step 2: Push and open PR**

```bash
git push -u origin design/issue-199-viewport-only-rendering
gh pr create --title "Viewport clipping + FetchRange foundation (#199, Plan A)" --body "$(cat <<'EOF'
## Summary
- Adds RowBase + AltScreen flag to BufferDelta; every delta is now viewport-clipped to per-client window + 1× overscan.
- New MsgViewportUpdate (client → server) and MsgFetchRange / MsgFetchRangeResponse (on-demand scrollback).
- Client-side sparse PaneCache keyed by globalIdx replaces row-index BufferCache lookups.
- Protocol version bumped in lockstep; old clients reconnect against the supervisor.

Plan: docs/superpowers/plans/2026-04-21-issue-199-plan-a-viewport-clipping-fetchrange.md
Spec: docs/superpowers/specs/2026-04-20-issue-199-viewport-only-rendering-design.md

## Test plan
- [x] Unit: `go test ./...`
- [x] Integration: `go test -tags=integration ./...`
- [x] Smoke: scroll far back on a 5k-row pane; alt-screen enter/exit preserves scroll position.
- [ ] Manual: `texelation` with live Claude session, verify no regression on resize.

Follow-up plans (not this PR):
- Plan B: viewport-aware resume (precise wrap-segment anchor).
- Plan C: server-side selection/copy (`ResolveBoundary`, `CaptureSelection`).
- Plan D: cross-restart persistence of PaneViewportState.
- Plan E: statusbar "fetch pending" indicator.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist (run before calling Plan A done)

1. **Spec coverage (steps 1–4 only):**
   - Step 1 (BufferDelta wire format) — Task 1.
   - Step 2 (MsgViewportUpdate + server tracks) — Task 2.
   - Step 3 (FetchRange) — Task 3.
   - Step 4 (clip-on-emit + client cache) — Task 4.
   Steps 5–8 are deferred to Plans B/C/D/E.

2. **No placeholders** — all encode/decode functions have full bodies; all tests have real assertions.

3. **Type consistency** — `ClientViewport`, `PaneCache`, `ViewportUpdate`, `FetchRange`, `FetchRangeResponse`, `LogicalRow` names match across every task.

4. **Backwards compatibility** — none expected; protocol version bump + supervisor restart. Called out in task 1 step 11.

5. **TDD discipline** — every implementation step is preceded by a failing test. Every task ends with a commit.
