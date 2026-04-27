# Pane Border / Decoration Row Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make pane top/bottom borders and app decoration rows (texterm internal statusbar) actually render on the client, fixing the pre-existing `bufferToDelta gid<0` filter bug surfaced by Plan D2's daemon-restart rehydrate path.

**Architecture:** Server stays authoritative for all visible cells. Wire format gains `protocol.PaneSnapshot.{ContentTopRow, NumContentRows}` (zero-content panes use `NumContentRows == 0` — no overloaded sentinel) and `protocol.BufferDelta.DecorRows` of new distinct type `DecorRowDelta` (with a `RowIdx` field carrying absolute rowIdx, so the compiler refuses to mix it with content `RowDelta`). Client renders via two-layer composite: gid-keyed `PaneCache` for content rowIdx in `[ContentTopRow, ContentTopRow+NumContentRows-1]`, positional decoration cache (under `rowsMu`) otherwise. Protocol bumps v2 → v3, no v2 fallback in the decoder.

**Tech Stack:** Go 1.24, custom binary protocol (`protocol/`), `internal/runtime/server`, `internal/runtime/client`, `client` package, `texel` package. Tests use `go test`; integration tests use `internal/runtime/server/testutil/memconn.go`.

**Spec:** `docs/superpowers/specs/2026-04-27-issue-199-pane-decoration-rendering-design.md`

**Branch:** `feature/issue-199-pane-decoration-rendering` (already created off main).

---

## File Structure

| File | Responsibility | Lines (current) |
|------|---------------|-----------------|
| `protocol/protocol.go` | Protocol version constant | bump 2→3 |
| `protocol/buffer_delta.go` | `BufferDelta` type + new `DecorRowDelta` type + encode/decode | ~352 |
| `protocol/messages.go` | `PaneSnapshot.{ContentTopRow, NumContentRows}` + tree snapshot encode/decode | ~1050 |
| `texel/snapshot.go` | `texel.PaneSnapshot.{ContentTopRow, NumContentRows}` + `capturePaneSnapshot` populates them | ~600 |
| `internal/runtime/server/tree_convert.go` | Bridge `texel.PaneSnapshot` ↔ `protocol.PaneSnapshot` | ~150 |
| `internal/runtime/server/desktop_publisher.go` | `bufferToDelta`: route gid<0 rows into `DecorRows` (typed `DecorRowDelta`) | ~330 |
| `client/buffercache.go` | `PaneState.{ContentTopRow, NumContentRows, decorRows}` (decorRows unexported, guarded by `rowsMu`); `DecorRowAt` accessor; `ApplyDelta` populates; `ResetRevisions` clears | ~600 |
| `internal/runtime/client/viewport_tracker.go` | `onBufferDelta`: `top` calc uses `pane.NumContentRows`; logs + skips on `pane==nil` | ~280 |
| `internal/runtime/client/renderer.go` | `rowSourceForPane`: two-layer lookup; logs once per (paneID, rowIdx) on decoration miss | ~550 |
| `internal/runtime/server/viewport_integration_test.go` | Extend `memHarness` with multi-pane + cross-restart support (Task 11.5) | ~870 |

---

## Task 1: Protocol — `DecorRowDelta` type + `BufferDelta.DecorRows` + encode/decode

**Files:**
- Modify: `protocol/buffer_delta.go` (struct definition around line 92, encoder around line 109, decoder around line 247)
- Test: `protocol/buffer_delta_test.go`

**Goal:** Round-trip an empty and non-empty `DecorRows` slice using the new `DecorRowDelta` type, with `ErrPayloadShort` on truncated payloads.

- [ ] **Step 1: Add the failing tests (red).**

Append to `protocol/buffer_delta_test.go`:

```go
func TestEncodeDecodeBufferDelta_DecorRoundTrip(t *testing.T) {
	original := protocol.BufferDelta{
		PaneID:   [16]byte{0xab, 0xcd},
		Revision: 7,
		Flags:    protocol.BufferDeltaNone,
		RowBase:  100,
		Styles: []protocol.StyleEntry{
			{AttrFlags: 0, FgModel: protocol.ColorModelDefault, BgModel: protocol.ColorModelDefault},
		},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hi", StyleIndex: 0}}},
		},
		DecorRows: []protocol.DecorRowDelta{
			{RowIdx: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+", StyleIndex: 0}}},
			{RowIdx: 22, Spans: []protocol.CellSpan{{StartCol: 0, Text: "-", StyleIndex: 0}}},
		},
	}
	encoded, err := protocol.EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := protocol.DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("round-trip mismatch:\n  want %+v\n  got  %+v", original, decoded)
	}
}

func TestEncodeDecodeBufferDelta_EmptyDecorRoundTrip(t *testing.T) {
	original := protocol.BufferDelta{
		PaneID:   [16]byte{0xff},
		Revision: 1,
		Flags:    protocol.BufferDeltaAltScreen,
		RowBase:  0,
		Styles:   []protocol.StyleEntry{{AttrFlags: 0, FgModel: protocol.ColorModelDefault, BgModel: protocol.ColorModelDefault}},
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
		// DecorRows intentionally nil
	}
	encoded, err := protocol.EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := protocol.DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.DecorRows) != 0 {
		t.Fatalf("expected empty DecorRows, got %+v", decoded.DecorRows)
	}
}

func TestDecodeBufferDelta_TruncatedDecorTailErrPayloadShort(t *testing.T) {
	// Build a valid v3 payload then chop the trailing 2-byte decor count.
	original := protocol.BufferDelta{
		PaneID:   [16]byte{0x01},
		Revision: 1,
		Styles:   []protocol.StyleEntry{{AttrFlags: 0, FgModel: protocol.ColorModelDefault, BgModel: protocol.ColorModelDefault}},
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "x", StyleIndex: 0}}}},
	}
	encoded, err := protocol.EncodeBufferDelta(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	truncated := encoded[:len(encoded)-2]
	if _, err := protocol.DecodeBufferDelta(truncated); !errors.Is(err, protocol.ErrPayloadShort) {
		t.Fatalf("expected ErrPayloadShort on truncated v3, got %v", err)
	}
}
```

If `reflect`, `errors`, and `protocol` aren't imported in this file already, add them.

- [ ] **Step 2: Run the test — verify it fails.**

```bash
go test ./protocol/ -run TestEncodeDecodeBufferDelta_DecorRoundTrip -v
```

Expected: FAIL with `unknown field DecorRows in struct literal`.

- [ ] **Step 3: Add `DecorRowDelta` type + `BufferDelta.DecorRows` field.**

In `protocol/buffer_delta.go`, just before the existing `BufferDelta` struct, add:

```go
// DecorRowDelta carries a single positional decoration row (border, app
// statusbar). RowIdx is the absolute rowIdx in the pane buffer — distinct
// from RowDelta.Row, which is gid - RowBase. Wire byte layout matches
// RowDelta exactly; the type is separate to prevent accidental mixing.
type DecorRowDelta struct {
	RowIdx uint16
	Spans  []CellSpan
}
```

Modify the `BufferDelta` struct to:

```go
type BufferDelta struct {
	PaneID    [16]byte
	Revision  uint32
	Flags     BufferDeltaFlags
	RowBase   int64
	Styles    []StyleEntry
	Rows      []RowDelta
	DecorRows []DecorRowDelta // rows keyed by absolute rowIdx (borders + app decoration)
}
```

- [ ] **Step 4: Extend the encoder.**

In `EncodeBufferDelta` (`protocol/buffer_delta.go` ~line 244, just before `return buf.Bytes(), nil`), append:

```go
	if len(delta.DecorRows) > 0xFFFF {
		return nil, ErrBufferTooLarge
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(delta.DecorRows))); err != nil {
		return nil, err
	}
	for _, row := range delta.DecorRows {
		if len(row.Spans) > 0xFFFF {
			return nil, ErrBufferTooLarge
		}
		if err := binary.Write(buf, binary.LittleEndian, row.RowIdx); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(row.Spans))); err != nil {
			return nil, err
		}
		for _, span := range row.Spans {
			textBytes := []byte(span.Text)
			if len(textBytes) > 0xFFFF {
				return nil, ErrInvalidSpan
			}
			if int(span.StyleIndex) >= len(delta.Styles) {
				return nil, ErrStyleIndexOutOfRange
			}
			if err := binary.Write(buf, binary.LittleEndian, span.StartCol); err != nil {
				return nil, err
			}
			if err := binary.Write(buf, binary.LittleEndian, uint16(len(textBytes))); err != nil {
				return nil, err
			}
			if err := binary.Write(buf, binary.LittleEndian, span.StyleIndex); err != nil {
				return nil, err
			}
			if len(textBytes) > 0 {
				if _, err := buf.Write(textBytes); err != nil {
					return nil, err
				}
			}
		}
	}
```

The encoder always emits the 2-byte `DecorRows` count (zero when empty), so a v3 payload always has a tail. The decoder will reject any payload missing it.

- [ ] **Step 5: Extend the decoder.**

In `DecodeBufferDelta`, **replace** the existing `return delta, nil` at the end (~line 350) with:

```go
	// v3 tail: DecorRows. The 2-byte count is mandatory — no v2 fallback.
	if len(b) < 2 {
		return delta, ErrPayloadShort
	}
	decorCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if decorCount > 0 {
		delta.DecorRows = make([]DecorRowDelta, decorCount)
		for i := 0; i < int(decorCount); i++ {
			if len(b) < 4 {
				return delta, ErrPayloadShort
			}
			rowIdx := binary.LittleEndian.Uint16(b[:2])
			spanCount := binary.LittleEndian.Uint16(b[2:4])
			b = b[4:]
			spans := make([]CellSpan, spanCount)
			for s := 0; s < int(spanCount); s++ {
				if len(b) < 6 {
					return delta, ErrPayloadShort
				}
				startCol := binary.LittleEndian.Uint16(b[:2])
				textLen := binary.LittleEndian.Uint16(b[2:4])
				styleIndex := binary.LittleEndian.Uint16(b[4:6])
				b = b[6:]
				if len(b) < int(textLen) {
					return delta, ErrPayloadShort
				}
				text := string(b[:textLen])
				b = b[textLen:]
				if int(styleIndex) >= int(styleCount) {
					return delta, ErrStyleIndexOutOfRange
				}
				spans[s] = CellSpan{StartCol: startCol, Text: text, StyleIndex: styleIndex}
			}
			delta.DecorRows[i] = DecorRowDelta{RowIdx: rowIdx, Spans: spans}
		}
	}
	if len(b) != 0 {
		return delta, ErrPayloadShort
	}
	return delta, nil
```

Two correctness points:

1. The block ends with an explicit `return delta, nil` for the success path — no falling off the end of the function.
2. A truncated v3 payload (missing the 2-byte count, or short during per-row decode) returns `ErrPayloadShort`. There is no v2 silent-accept path. Per project policy, no backward compat — bumping `Version` from 2 to 3 already refuses old clients at handshake.

- [ ] **Step 6: Run the test — verify it passes.**

```bash
go test ./protocol/ -run TestEncodeDecodeBufferDelta -v
```

Expected: PASS.

- [ ] **Step 7: Run the entire protocol test suite to confirm no regressions.**

```bash
go test ./protocol/ -v
```

Expected: ALL PASS.

- [ ] **Step 8: Commit.**

```bash
git add protocol/buffer_delta.go protocol/buffer_delta_test.go
git commit -m "$(cat <<'EOF'
protocol: add BufferDelta.DecorRows field with codec round-trip

Decoration rows ride alongside content rows but are keyed by absolute
rowIdx, not (gid - RowBase). Used for borders and app decoration rows
that have no main-screen globalIdx.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Protocol — `PaneSnapshot.ContentTopRow` / `NumContentRows` + encode/decode

**Files:**
- Modify: `protocol/messages.go` (struct ~line 193, `EncodeTreeSnapshot` ~line 961, `DecodeTreeSnapshot` ~line 1013)
- Test: `protocol/messages_test.go`

**Goal:** Round-trip the new fields through `EncodeTreeSnapshot` / `DecodeTreeSnapshot`.

- [ ] **Step 1: Add the failing test.**

Append to `protocol/messages_test.go`:

```go
func TestEncodeDecodeTreeSnapshot_ContentBoundsRoundTrip(t *testing.T) {
	original := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{
				PaneID:         [16]byte{0xaa},
				Revision:       3,
				Title:          "term",
				Rows:           nil,
				X:              0, Y: 0, Width: 80, Height: 24,
				AppType:        "texelterm",
				AppConfig:      "",
				ContentTopRow:  1,
				NumContentRows: 21,
			},
		},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}
	encoded, err := protocol.EncodeTreeSnapshot(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := protocol.DecodeTreeSnapshot(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Panes[0].ContentTopRow != 1 || decoded.Panes[0].NumContentRows != 21 {
		t.Fatalf("content bounds mismatch: got top=%d num=%d", decoded.Panes[0].ContentTopRow, decoded.Panes[0].NumContentRows)
	}
}

func TestEncodeDecodeTreeSnapshot_ZeroContent(t *testing.T) {
	original := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID:         [16]byte{0xbb},
			Title:          "all-decor",
			ContentTopRow:  0,
			NumContentRows: 0, // unambiguous: no content rows
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}
	encoded, err := protocol.EncodeTreeSnapshot(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := protocol.DecodeTreeSnapshot(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Panes[0].ContentTopRow != 0 || decoded.Panes[0].NumContentRows != 0 {
		t.Fatalf("zero-content mismatch: got top=%d num=%d", decoded.Panes[0].ContentTopRow, decoded.Panes[0].NumContentRows)
	}
}
```

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./protocol/ -run TestEncodeDecodeTreeSnapshot_ContentBoundsRoundTrip -v
```

Expected: FAIL with `unknown field ContentTopRow in struct literal`.

- [ ] **Step 3: Add fields to `PaneSnapshot`.**

In `protocol/messages.go` (~line 193), modify struct to:

```go
type PaneSnapshot struct {
	PaneID         [16]byte
	Revision       uint32
	Title          string
	Rows           []string
	X              int32
	Y              int32
	Width          int32
	Height         int32
	AppType        string
	AppConfig      string
	ContentTopRow  uint16 // first content rowIdx (ignored when NumContentRows == 0)
	NumContentRows uint16 // count of content rows; 0 means the pane is all-decoration
}
```

- [ ] **Step 4: Extend `EncodeTreeSnapshot`.**

In `protocol/messages.go` (~line 1003), inside the per-pane loop, after `if err := encodeString(buf, pane.AppConfig); err != nil { return nil, err }`, append:

```go
		if err := binary.Write(buf, binary.LittleEndian, pane.ContentTopRow); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.NumContentRows); err != nil {
			return nil, err
		}
```

- [ ] **Step 5: Extend `DecodeTreeSnapshot`.**

In `protocol/messages.go` (~line 1056), after the existing block:

```go
		config, remaining, err := decodeString(remaining)
		if err != nil {
			return snapshot, err
		}
		pane.AppType = appType
		pane.AppConfig = config
```

Add:

```go
		if len(remaining) < 4 {
			return snapshot, ErrPayloadShort
		}
		pane.ContentTopRow = binary.LittleEndian.Uint16(remaining[0:2])
		pane.NumContentRows = binary.LittleEndian.Uint16(remaining[2:4])
		remaining = remaining[4:]
```

- [ ] **Step 6: Run — verify pass.**

```bash
go test ./protocol/ -run TestEncodeDecodeTreeSnapshot -v
```

Expected: PASS for both new tests.

- [ ] **Step 7: Run all protocol tests.**

```bash
go test ./protocol/ -v
```

Expected: ALL PASS.

- [ ] **Step 8: Commit.**

```bash
git add protocol/messages.go protocol/messages_test.go
git commit -m "$(cat <<'EOF'
protocol: add PaneSnapshot.ContentTopRow/NumContentRows

Tells the client which rowIdx range maps to gids vs decoration.
NumContentRows == 0 is the unambiguous zero-content state for
all-decoration panes — no overloaded sentinel.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Protocol — bump `Version` to 3

**Files:**
- Modify: `protocol/protocol.go` (line 35)
- Test: `protocol/protocol_test.go`

- [ ] **Step 1: Add the failing test.**

Append to `protocol/protocol_test.go`:

```go
func TestProtocolVersionIs3(t *testing.T) {
	if protocol.Version != 3 {
		t.Fatalf("expected Version=3, got %d", protocol.Version)
	}
}
```

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./protocol/ -run TestProtocolVersionIs3 -v
```

Expected: FAIL with `expected Version=3, got 2`.

- [ ] **Step 3: Bump version.**

In `protocol/protocol.go` line 35, change `const Version uint8 = 2` to `const Version uint8 = 3`.

- [ ] **Step 4: Run — verify pass.**

```bash
go test ./protocol/ -run TestProtocolVersionIs3 -v
```

Expected: PASS.

- [ ] **Step 5: Run full protocol suite.**

```bash
go test ./protocol/ -v
```

Expected: ALL PASS. If any other test asserts `Version == 2`, update it to 3.

- [ ] **Step 6: Commit.**

```bash
git add protocol/protocol.go protocol/protocol_test.go
git commit -m "$(cat <<'EOF'
protocol: bump Version 2 -> 3 for decoration rows + content bounds

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `texel.PaneSnapshot` content bounds + `capturePaneSnapshot` populates them

**Files:**
- Modify: `texel/snapshot.go` (struct around line 24, `capturePaneSnapshot` around line 304)
- Test: `texel/snapshot_test.go` (create if missing)

- [ ] **Step 1: Add the failing test.**

Append to (or create) `texel/snapshot_test.go`:

```go
package texel

import (
	"testing"

	texelcore "github.com/framegrace/texelui/core"
)

func TestCapturePaneSnapshot_ContentBoundsComputed(t *testing.T) {
	// 6-row pane: [0]=-1 (top border), [1..3]=content, [4]=-1 (app statusbar), [5]=-1 (bottom border)
	rowIdx := []int64{-1, 100, 101, 102, -1, -1}
	top, num := computeContentBounds(rowIdx)
	if top != 1 || num != 3 {
		t.Fatalf("expected top=1 num=3, got top=%d num=%d", top, num)
	}
}

func TestCapturePaneSnapshot_ContentBoundsAllDecoration(t *testing.T) {
	// All -1 rows: zero content, top=0 num=0.
	rowIdx := []int64{-1, -1, -1}
	top, num := computeContentBounds(rowIdx)
	if top != 0 || num != 0 {
		t.Fatalf("expected top=0 num=0, got top=%d num=%d", top, num)
	}
}

func TestCapturePaneSnapshot_ContentBoundsEmpty(t *testing.T) {
	top, num := computeContentBounds(nil)
	if top != 0 || num != 0 {
		t.Fatalf("expected top=0 num=0, got top=%d num=%d", top, num)
	}
}

// Suppress unused import on platforms where texelcore isn't used in this file.
var _ texelcore.Cell
```

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./texel/ -run TestCapturePaneSnapshot_ContentBounds -v
```

Expected: FAIL with `undefined: computeContentBounds`.

- [ ] **Step 3: Add fields to `texel.PaneSnapshot`.**

In `texel/snapshot.go` (~line 24), update struct to:

```go
type PaneSnapshot struct {
	ID           [16]byte
	Title        string
	Buffer       [][]Cell
	RowGlobalIdx []int64
	AltScreen    bool
	Rect         Rectangle
	AppType      string
	AppConfig    map[string]interface{}
	// ContentTopRow is the first rowIdx in Buffer with RowGlobalIdx[y] >= 0.
	// NumContentRows is the count of indices with RowGlobalIdx[y] >= 0.
	// NumContentRows == 0 means zero content rows (status panes, all-decoration apps);
	// in that case ContentTopRow is meaningless. For alt-screen panes the fields
	// are populated but unused — clients render alt-screen positionally regardless.
	ContentTopRow  uint16
	NumContentRows uint16
}
```

- [ ] **Step 4: Add `computeContentBounds` helper.**

In `texel/snapshot.go` near `allMinusOne` (~line 360), add:

```go
// computeContentBounds returns (ContentTopRow, NumContentRows) for the
// given RowGlobalIdx slice. If no row has gid>=0, returns (0, 0).
// Assumes contiguity: callers must ensure all gid>=0 rows form a single
// contiguous block (enforced by capturePaneSnapshot construction).
func computeContentBounds(rowIdx []int64) (uint16, uint16) {
	top := -1
	last := -1
	for y, gid := range rowIdx {
		if gid < 0 {
			continue
		}
		if top < 0 {
			top = y
		}
		last = y
	}
	if top < 0 {
		return 0, 0
	}
	return uint16(top), uint16(last - top + 1)
}
```

- [ ] **Step 5: Run — verify the unit tests pass.**

```bash
go test ./texel/ -run TestCapturePaneSnapshot_ContentBounds -v
```

Expected: PASS.

- [ ] **Step 6: Wire `computeContentBounds` into `capturePaneSnapshot`.**

In `texel/snapshot.go` `capturePaneSnapshot` (~line 351, just before `return snap` at the end), add:

```go
	snap.ContentTopRow, snap.NumContentRows = computeContentBounds(snap.RowGlobalIdx)
```

- [ ] **Step 7: Add an integration-shape test for capturePaneSnapshot via the existing texterm path.**

If a test already exists that builds a real `*pane` with a `RowGlobalIdxProvider`, extend it to assert ContentBounds. Otherwise add a fresh test using a minimal fake provider:

Append to `texel/snapshot_test.go`:

```go
type fakeRowProvider struct {
	gids []int64
}

func (f *fakeRowProvider) RowGlobalIdx() []int64 { return f.gids }

// (No-op other texel.App methods required to satisfy the App interface in
// this test would go in a fixture file. For pure computeContentBounds
// coverage, the unit tests above are sufficient.)
```

This step is informational — `computeContentBounds` already has direct unit coverage. Skip if the file structure makes adding a real-pane fixture intrusive.

- [ ] **Step 8: Run all texel tests.**

```bash
go test ./texel/ -v
```

Expected: ALL PASS.

- [ ] **Step 9: Commit.**

```bash
git add texel/snapshot.go texel/snapshot_test.go
git commit -m "$(cat <<'EOF'
texel: capturePaneSnapshot computes ContentTopRow + NumContentRows

NumContentRows == 0 signals zero content rows (status panes, all-
decoration apps). Used by tree_convert to populate the protocol
PaneSnapshot.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `treeCaptureToProtocol` passes through content bounds

**Files:**
- Modify: `internal/runtime/server/tree_convert.go` (lines 19-37, the forward translator; lines 39+, the reverse translator)
- Test: `internal/runtime/server/tree_convert_test.go` (create if missing)

- [ ] **Step 1: Add the failing test.**

Create `internal/runtime/server/tree_convert_test.go` (or append if it exists):

```go
package server

import (
	"testing"

	"github.com/framegrace/texelation/texel"
)

func TestTreeCaptureToProtocol_PassesContentBounds(t *testing.T) {
	capture := texel.TreeCapture{
		Panes: []texel.PaneSnapshot{{
			ID:             [16]byte{0xab},
			Title:          "t",
			ContentTopRow:  2,
			NumContentRows: 16,
		}},
	}
	snap := treeCaptureToProtocol(capture)
	if len(snap.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(snap.Panes))
	}
	if snap.Panes[0].ContentTopRow != 2 || snap.Panes[0].NumContentRows != 16 {
		t.Fatalf("content bounds not passed through: top=%d num=%d",
			snap.Panes[0].ContentTopRow, snap.Panes[0].NumContentRows)
	}
}
```

(Note: `texel.TreeCapture` is the existing intermediate type used by `treeCaptureToProtocol` — verify that `texel.PaneSnapshot` exposed via `texel.TreeCapture.Panes` matches the type modified in Task 4. Inspect `texel/snapshot.go` for the `TreeCapture` definition if needed.)

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./internal/runtime/server/ -run TestTreeCaptureToProtocol_PassesContentBounds -v
```

Expected: FAIL — values are zero (default) because the bridge doesn't copy them.

- [ ] **Step 3: Update `treeCaptureToProtocol`.**

In `internal/runtime/server/tree_convert.go` (line 22), modify the per-pane construction to:

```go
	for i, pane := range capture.Panes {
		snapshot.Panes[i] = protocol.PaneSnapshot{
			PaneID:         pane.ID,
			Revision:       0,
			Title:          pane.Title,
			Rows:           nil,
			X:              int32(pane.Rect.X),
			Y:              int32(pane.Rect.Y),
			Width:          int32(pane.Rect.Width),
			Height:         int32(pane.Rect.Height),
			AppType:        pane.AppType,
			AppConfig:      encodeAppConfig(pane.AppConfig),
			ContentTopRow:  pane.ContentTopRow,
			NumContentRows: pane.NumContentRows,
		}
	}
```

- [ ] **Step 4: Update `protocolToTreeCapture` (reverse direction).**

In `internal/runtime/server/tree_convert.go` (~line 39), modify the per-pane construction:

```go
	capture.Panes[i] = texel.PaneSnapshot{
		ID:             pane.PaneID,
		Title:          pane.Title,
		Buffer:         buffer,
		RowGlobalIdx:   rowGlobalIdxAllMinusOne(len(buffer)),
		Rect:           texel.Rectangle{X: int(pane.X), Y: int(pane.Y), Width: int(pane.Width), Height: int(pane.Height)},
		AppType:        pane.AppType,
		AppConfig:      decodeAppConfig(pane.AppConfig),
		ContentTopRow:  pane.ContentTopRow,
		NumContentRows: pane.NumContentRows,
	}
```

- [ ] **Step 5: Run — verify pass.**

```bash
go test ./internal/runtime/server/ -run TestTreeCaptureToProtocol_PassesContentBounds -v
```

Expected: PASS.

- [ ] **Step 6: Run package tests.**

```bash
go test ./internal/runtime/server/ -count=1
```

Expected: ALL PASS (server-side tests are extensive; expect this to take ~30s).

- [ ] **Step 7: Commit.**

```bash
git add internal/runtime/server/tree_convert.go internal/runtime/server/tree_convert_test.go
git commit -m "$(cat <<'EOF'
server: tree_convert passes ContentTopRow + NumContentRows through both directions

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `bufferToDelta` routes gid<0 rows into `DecorRows`

**Files:**
- Modify: `internal/runtime/server/desktop_publisher.go` (the `for y, row := range snap.Buffer` loop ~line 296-313)
- Test: `internal/runtime/server/desktop_publisher_test.go` (the file may be named differently — check existing tests for `bufferToDelta`)

- [ ] **Step 1: Locate existing `bufferToDelta` tests.**

```bash
grep -rln "bufferToDelta" internal/runtime/server/*_test.go
```

Use the file that surfaces. If none exists, create `internal/runtime/server/desktop_publisher_test.go`.

- [ ] **Step 2: Add the failing test.**

Append (or create with appropriate package boilerplate):

```go
func TestBufferToDelta_DecorationRowsIncluded(t *testing.T) {
	// 5-row buffer: rowIdx 0 = top border (-1), rowIdx 1..3 = content gids,
	// rowIdx 4 = bottom border (-1).
	rows := [][]texel.Cell{
		{{Ch: '+'}, {Ch: '-'}, {Ch: '+'}}, // border
		{{Ch: 'a'}, {Ch: 'b'}, {Ch: 'c'}}, // content
		{{Ch: 'd'}, {Ch: 'e'}, {Ch: 'f'}}, // content
		{{Ch: 'g'}, {Ch: 'h'}, {Ch: 'i'}}, // content
		{{Ch: '+'}, {Ch: '-'}, {Ch: '+'}}, // border
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, 101, 102, -1},
		ContentTopRow:  1,
		NumContentRows: 3,
	}
	vp := ClientViewport{Rows: 3, AutoFollow: true}
	prev := [][]texel.Cell(nil)

	delta := bufferToDelta(snap, vp, prev, 1)

	if len(delta.DecorRows) != 2 {
		t.Fatalf("expected 2 DecorRows, got %d: %+v", len(delta.DecorRows), delta.DecorRows)
	}
	gotIdx := map[uint16]bool{delta.DecorRows[0].RowIdx: true, delta.DecorRows[1].RowIdx: true}
	if !gotIdx[0] || !gotIdx[4] {
		t.Fatalf("expected decoration rows at rowIdx 0 and 4, got %v", gotIdx)
	}
	if len(delta.Rows) != 3 {
		t.Fatalf("expected 3 content Rows, got %d", len(delta.Rows))
	}
}

func TestBufferToDelta_DecorationRowsDiffed(t *testing.T) {
	rows := [][]texel.Cell{
		{{Ch: '+'}}, // border
		{{Ch: 'a'}}, // content
		{{Ch: '+'}}, // border
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, -1},
		ContentTopRow:  1,
		NumContentRows: 1,
	}
	vp := ClientViewport{Rows: 1, AutoFollow: true}
	prev := [][]texel.Cell{
		{{Ch: '+'}},
		{{Ch: 'a'}},
		{{Ch: '+'}},
	}
	delta := bufferToDelta(snap, vp, prev, 1)
	if len(delta.DecorRows) != 0 {
		t.Fatalf("expected 0 DecorRows when borders unchanged, got %d", len(delta.DecorRows))
	}
	if len(delta.Rows) != 0 {
		t.Fatalf("expected 0 content Rows when content unchanged, got %d", len(delta.Rows))
	}
}

func TestBufferToDelta_DecorationRowsDiffPartial(t *testing.T) {
	rows := [][]texel.Cell{
		{{Ch: '+'}}, // border (will change)
		{{Ch: 'a'}}, // content
		{{Ch: '+'}}, // border (unchanged)
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, -1},
		ContentTopRow:  1,
		NumContentRows: 1,
	}
	vp := ClientViewport{Rows: 1, AutoFollow: true}
	prev := [][]texel.Cell{
		{{Ch: '#'}}, // different
		{{Ch: 'a'}}, // same
		{{Ch: '+'}}, // same
	}
	delta := bufferToDelta(snap, vp, prev, 1)
	if len(delta.DecorRows) != 1 || delta.DecorRows[0].RowIdx != 0 {
		t.Fatalf("expected 1 DecorRows entry at rowIdx 0, got %+v", delta.DecorRows)
	}
}

func TestBufferToDelta_TexelTermInternalStatusbar(t *testing.T) {
	// 6-row layout: rowIdx 0 = top border, [1..3] = content, rowIdx 4 = app
	// internal statusbar (gid=-1), rowIdx 5 = bottom border.
	rows := [][]texel.Cell{
		{{Ch: '+'}},
		{{Ch: 'a'}},
		{{Ch: 'b'}},
		{{Ch: 'c'}},
		{{Ch: 'S'}},
		{{Ch: '+'}},
	}
	snap := texel.PaneSnapshot{
		ID:             [16]byte{0xab},
		Buffer:         rows,
		RowGlobalIdx:   []int64{-1, 100, 101, 102, -1, -1},
		ContentTopRow:  1,
		NumContentRows: 3,
	}
	vp := ClientViewport{Rows: 3, AutoFollow: true}
	delta := bufferToDelta(snap, vp, nil, 1)
	got := map[uint16]bool{}
	for _, r := range delta.DecorRows {
		got[r.RowIdx] = true
	}
	if !got[0] || !got[4] || !got[5] {
		t.Fatalf("expected DecorRows at rowIdx 0, 4, 5 (top + statusbar + bottom), got %v", got)
	}
}

func TestBufferToDelta_AltScreenLeavesDecorRowsEmpty(t *testing.T) {
	rows := [][]texel.Cell{{{Ch: 'x'}}}
	snap := texel.PaneSnapshot{
		ID:           [16]byte{0xab},
		Buffer:       rows,
		RowGlobalIdx: []int64{-1},
		AltScreen:    true,
	}
	vp := ClientViewport{Rows: 1, AltScreen: true}
	delta := bufferToDelta(snap, vp, nil, 1)
	if len(delta.DecorRows) != 0 {
		t.Fatalf("alt-screen must not emit DecorRows, got %d", len(delta.DecorRows))
	}
}
```

(The exact `ClientViewport` field names and `bufferToDelta` signature must match existing code — read the function signature at `internal/runtime/server/desktop_publisher.go:206` (or near there) and adapt the test calls. The test author should preserve the public-API arguments the function actually takes.)

- [ ] **Step 3: Run — verify failure.**

```bash
go test ./internal/runtime/server/ -run TestBufferToDelta_Decoration -v
```

Expected: FAIL — `DecorRows` is empty because the publisher still drops gid<0 rows.

- [ ] **Step 4: Modify `bufferToDelta`.**

In `internal/runtime/server/desktop_publisher.go` find the row-iteration loop (around line 296-313) which currently is:

```go
for y, row := range snap.Buffer {
	if len(row) == 0 {
		continue
	}
	if y >= len(snap.RowGlobalIdx) {
		continue
	}
	gid := snap.RowGlobalIdx[y]
	if gid < 0 {
		continue
	}
	if gid < lo || gid > hi {
		continue
	}
	if y < len(prev) && rowsEqual(row, prev[y]) {
		continue
	}
	rows = append(rows, protocol.RowDelta{Row: uint16(gid - lo), Spans: encodeRow(row)})
}
delta.Styles = styles
delta.Rows = rows
return delta
```

Replace with:

```go
var decorRows []protocol.DecorRowDelta
for y, row := range snap.Buffer {
	if len(row) == 0 {
		continue
	}
	if y >= len(snap.RowGlobalIdx) {
		continue
	}
	gid := snap.RowGlobalIdx[y]
	// Alt-screen panes have RowGlobalIdx all -1; skip decoration emission
	// before the rowsEqual cost. The existing alt-screen positional path
	// (BufferDeltaAltScreen flag) handles them.
	if snap.AltScreen && gid < 0 {
		continue
	}
	if y < len(prev) && rowsEqual(row, prev[y]) {
		continue
	}
	if gid < 0 {
		// Decoration row (border or app statusbar) — positional.
		decorRows = append(decorRows, protocol.DecorRowDelta{
			RowIdx: uint16(y),
			Spans:  encodeRow(row),
		})
		continue
	}
	if gid < lo || gid > hi {
		continue
	}
	rows = append(rows, protocol.RowDelta{Row: uint16(gid - lo), Spans: encodeRow(row)})
}
delta.Styles = styles
delta.Rows = rows
delta.DecorRows = decorRows
return delta
```

Two ordering points:

1. The `snap.AltScreen && gid < 0` short-circuit fires *before* `rowsEqual` so alt-screen panes never pay the diff comparison for decoration rows. This avoids the per-row regression a reviewer flagged.
2. For non-altScreen panes, the `rowsEqual` diff fires for both content and decoration paths (they share the positional `prev[y]` storage), so unchanged decoration rows don't re-ship.

- [ ] **Step 5: Run — verify pass.**

```bash
go test ./internal/runtime/server/ -run TestBufferToDelta_Decoration -v
```

Expected: PASS for all three new tests.

- [ ] **Step 6: Run server package tests.**

```bash
go test ./internal/runtime/server/ -count=1
```

Expected: ALL PASS. If a Plan A integration test like `TestIntegration_ClipsAndFetches` regresses (because it now sees `DecorRows`), inspect it and either add a `len(delta.DecorRows) == 0` assertion or update the assertion to account for them.

- [ ] **Step 7: Commit.**

```bash
git add internal/runtime/server/desktop_publisher.go internal/runtime/server/desktop_publisher_test.go
git commit -m "$(cat <<'EOF'
server: bufferToDelta routes gid<0 rows into DecorRows

Borders and app decoration rows now reach the client as positional
RowDeltas alongside content. Diff is positional and applies to both
layers. Alt-screen panes opt out (existing positional flag path
already covers them).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: `client.PaneState` gains content-bound + decoration-cache fields

**Files:**
- Modify: `client/buffercache.go` (struct ~line 22, `ApplySnapshot` ~line 222, add `DecorRowAt` accessor)
- Test: `client/buffercache_test.go`

- [ ] **Step 1: Add the failing test.**

Append to `client/buffercache_test.go`:

```go
func TestApplySnapshot_PopulatesContentBounds(t *testing.T) {
	cache := client.NewBufferCache()
	id := [16]byte{0xab}
	snapshot := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID:         id,
			Title:          "t",
			Width:          10, Height: 6,
			ContentTopRow:  1,
			NumContentRows: 4,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}
	cache.ApplySnapshot(snapshot)
	pane := cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane not registered")
	}
	if pane.ContentTopRow != 1 || pane.NumContentRows != 4 {
		t.Fatalf("content bounds not applied: top=%d num=%d", pane.ContentTopRow, pane.NumContentRows)
	}
}
```

If `cache.Pane(id)` accessor doesn't exist, use whatever inspector the existing tests use (e.g., reading via `cache.AllPanes()`).

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./client/ -run TestApplySnapshot_PopulatesContentBounds -v
```

Expected: FAIL with `pane.ContentTopRow undefined`.

- [ ] **Step 3: Add fields + `DecorRowAt` accessor to `PaneState`.**

In `client/buffercache.go` (~line 22), update struct:

```go
type PaneState struct {
	ID               [16]byte
	Revision         uint32
	UpdatedAt        time.Time
	rowsMu           sync.RWMutex
	rows             map[int][]Cell
	decorRows        map[uint16][]Cell // unexported; guarded by rowsMu (decoration: borders + app statusbar)
	Title            string
	Rect             clientRect
	Active           bool
	Resizing         bool
	ZOrder           int
	HandlesSelection bool

	// Content bounds (populated from PaneSnapshot). For non-altScreen panes,
	// rowIdx in [ContentTopRow, ContentTopRow + NumContentRows - 1] maps to
	// gid via the viewport tracker; rowIdx outside that range reads from
	// decorRows via DecorRowAt. NumContentRows == 0 means the pane has zero
	// content rows (status panes, all-decoration apps).
	ContentTopRow  uint16
	NumContentRows uint16

	// Dirty tracking for incremental rendering.
	Dirty       bool
	DirtyRows   map[int]bool
	HasAnimated bool
}

// DecorRowAt returns the cells for an absolute decoration rowIdx, or
// (nil, false) if no decoration has been applied to that row. Read under
// rowsMu.RLock(). The returned slice is a direct reference to internal
// state; callers must not retain or modify it across frame boundaries.
func (p *PaneState) DecorRowAt(rowIdx uint16) ([]Cell, bool) {
	if p == nil {
		return nil, false
	}
	p.rowsMu.RLock()
	defer p.rowsMu.RUnlock()
	cells, ok := p.decorRows[rowIdx]
	return cells, ok
}
```

- [ ] **Step 4: Update `ApplySnapshot` to copy content bounds.**

In `client/buffercache.go` `ApplySnapshot` (~line 235), after `pane.Title = paneSnap.Title`, add:

```go
		pane.ContentTopRow = paneSnap.ContentTopRow
		pane.NumContentRows = paneSnap.NumContentRows
```

- [ ] **Step 5: Run — verify pass.**

```bash
go test ./client/ -run TestApplySnapshot_PopulatesContentBounds -v
```

Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add client/buffercache.go client/buffercache_test.go
git commit -m "$(cat <<'EOF'
client: PaneState carries ContentTopRow + NumContentRows + decorRows

decorRows is unexported and guarded by rowsMu; access via DecorRowAt.
ApplySnapshot copies content bounds from the protocol pane snapshot.
ApplyDelta populates decorRows (next commit).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: `BufferCache.ApplyDelta` populates `DecorRows`

**Files:**
- Modify: `client/buffercache.go` `ApplyDelta` (~line 149)
- Test: `client/buffercache_test.go`

- [ ] **Step 1: Add the failing test.**

Append to `client/buffercache_test.go`:

```go
func TestApplyDelta_PopulatesDecorRows(t *testing.T) {
	cache := client.NewBufferCache()
	id := [16]byte{0xab}
	delta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 1,
		Styles: []protocol.StyleEntry{
			{AttrFlags: 0, FgModel: protocol.ColorModelDefault, BgModel: protocol.ColorModelDefault},
		},
		DecorRows: []protocol.DecorRowDelta{
			{RowIdx: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+--+", StyleIndex: 0}}},
			{RowIdx: 9, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+--+", StyleIndex: 0}}},
		},
	}
	cache.ApplyDelta(delta)
	pane := cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane not registered")
	}
	row0, ok0 := pane.DecorRowAt(0)
	row9, ok9 := pane.DecorRowAt(9)
	if !ok0 || !ok9 {
		t.Fatalf("expected DecorRowAt(0) and DecorRowAt(9) to be present")
	}
	if len(row0) != 4 || row0[0].Ch != '+' {
		t.Fatalf("rowIdx 0 content wrong: %+v", row0)
	}
	if len(row9) != 4 || row9[3].Ch != '+' {
		t.Fatalf("rowIdx 9 content wrong: %+v", row9)
	}
}
```

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./client/ -run TestApplyDelta_PopulatesDecorRows -v
```

Expected: FAIL — DecorRowAt returns (nil, false).

- [ ] **Step 3: Update `ApplyDelta`.**

In `client/buffercache.go` `ApplyDelta` (~line 149), the existing code acquires `pane.rowsMu.Lock()` for the content-row write and releases it before the dirty-tracking block. **Extend that critical section** to also cover decoration rows: keep `pane.rowsMu` locked through the new `decorRows` write. Concretely, after the content-row apply loop and *before* `pane.rowsMu.Unlock()`, add:

```go
	if len(delta.DecorRows) > 0 {
		if pane.decorRows == nil {
			pane.decorRows = make(map[uint16][]Cell, len(delta.DecorRows))
		}
		for _, rowDelta := range delta.DecorRows {
			row := pane.decorRows[rowDelta.RowIdx]
			for _, span := range rowDelta.Spans {
				start := int(span.StartCol)
				textRunes := []rune(span.Text)
				needed := start + len(textRunes)
				row = ensureRowLength(row, needed)
				style := tcell.StyleDefault
				var dynFG, dynBG protocol.DynColorDesc
				if int(span.StyleIndex) < len(styles) {
					style = styles[span.StyleIndex]
				}
				if int(span.StyleIndex) < len(delta.Styles) {
					entry := delta.Styles[span.StyleIndex]
					if entry.AttrFlags&protocol.AttrHasDynamic != 0 {
						dynFG = entry.DynFG
						dynBG = entry.DynBG
					}
				}
				for i, r := range textRunes {
					row[start+i] = Cell{Ch: r, Style: style, DynFG: dynFG, DynBG: dynBG}
				}
			}
			pane.decorRows[rowDelta.RowIdx] = row
		}
	}
```

After `pane.rowsMu.Unlock()`, in the dirty-flag block, add:

```go
	if len(delta.DecorRows) > 0 {
		pane.Dirty = true
		// Decoration changes invalidate row-level dirty tracking; force a
		// full re-render of this pane.
		pane.DirtyRows = nil
	}
```

**Lock discipline:** all writes to `pane.decorRows` happen under `pane.rowsMu.Lock()`; all reads go through `DecorRowAt` which takes `pane.rowsMu.RLock()`. The renderer (Task 11) must not access `pane.decorRows` directly.

- [ ] **Step 4: Run — verify pass.**

```bash
go test ./client/ -run TestApplyDelta_PopulatesDecorRows -v
```

Expected: PASS.

- [ ] **Step 5: Run client package tests.**

```bash
go test ./client/ -count=1
```

Expected: ALL PASS.

- [ ] **Step 6: Commit.**

```bash
git add client/buffercache.go client/buffercache_test.go
git commit -m "$(cat <<'EOF'
client: ApplyDelta populates PaneState.DecorRows

DecorRows is keyed by absolute rowIdx, mirrors the content-row apply
loop. Changes force pane.DirtyRows=nil (full re-render) since rowIdx-
keyed decoration writes don't fit the existing gid-based row dirty
tracking.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `ResetRevisions` clears `DecorRows`

**Files:**
- Modify: `client/buffercache.go` `ResetRevisions` (~line 585)
- Test: `client/buffercache_test.go`

- [ ] **Step 1: Add the failing test.**

Append to `client/buffercache_test.go`:

```go
func TestResetRevisions_ClearsDecorRows(t *testing.T) {
	cache := client.NewBufferCache()
	id := [16]byte{0xab}
	delta := protocol.BufferDelta{
		PaneID:   id,
		Revision: 7,
		Styles:   []protocol.StyleEntry{{AttrFlags: 0, FgModel: protocol.ColorModelDefault, BgModel: protocol.ColorModelDefault}},
		DecorRows: []protocol.DecorRowDelta{
			{RowIdx: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+", StyleIndex: 0}}},
		},
	}
	cache.ApplyDelta(delta)
	if pane := cache.Pane(id); pane == nil {
		t.Fatalf("pane not registered")
	}
	if _, ok := cache.Pane(id).DecorRowAt(0); !ok {
		t.Fatalf("pre-reset: expected DecorRowAt(0) populated")
	}
	cache.ResetRevisions()
	pane := cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane gone after reset")
	}
	if _, ok := pane.DecorRowAt(0); ok {
		t.Fatalf("expected decoration cache cleared after ResetRevisions")
	}
	if pane.Revision != 0 {
		t.Fatalf("expected Revision=0, got %d", pane.Revision)
	}
}
```

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./client/ -run TestResetRevisions_ClearsDecorRows -v
```

Expected: FAIL — decoration row still present after reset.

- [ ] **Step 3: Update `ResetRevisions`.**

In `client/buffercache.go` (~line 585), modify to:

```go
func (c *BufferCache) ResetRevisions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pane := range c.panes {
		pane.Revision = 0
		pane.rowsMu.Lock()
		pane.decorRows = nil
		pane.rowsMu.Unlock()
	}
}
```

The per-pane `rowsMu.Lock()` ensures the renderer doesn't observe a torn map mid-clear. Holding both `c.mu` and `pane.rowsMu` is safe — `c.mu` is always acquired first throughout the package.

- [ ] **Step 4: Run — verify pass.**

```bash
go test ./client/ -run TestResetRevisions_ClearsDecorRows -v
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add client/buffercache.go client/buffercache_test.go
git commit -m "$(cat <<'EOF'
client: ResetRevisions clears PaneState.DecorRows

On session reuse / new publisher (Plan D2 path), the next delta will
republish all decoration rows. Stale chrome would otherwise survive
across publishers.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: `viewport_tracker.onBufferDelta` uses `numContentRows`

**Files:**
- Modify: `internal/runtime/client/viewport_tracker.go` `onBufferDelta` (~line 250-275)
- Test: `internal/runtime/client/viewport_tracker_test.go` (or extend the existing tracker tests)

- [ ] **Step 1: Locate the existing tracker test file.**

```bash
ls internal/runtime/client/ | grep viewport
```

Use `viewport_tracker_test.go` if present; otherwise the existing test file pattern in this directory.

- [ ] **Step 2: Add the failing tests.**

Append:

```go
func TestOnBufferDelta_TopUsesContentRowCount(t *testing.T) {
	state := newClientStateForTest(t) // helper from existing tests
	id := [16]byte{0xab}
	// Pane has H=10 rows; ContentTopRow=1, NumContentRows=8 → 8 content rows.
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 5, Height: 10,
			ContentTopRow: 1, NumContentRows: 8,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})
	state.viewports.get(id).Rows = 10
	state.viewports.get(id).Cols = 5
	state.viewports.get(id).AutoFollow = true

	delta := protocol.BufferDelta{
		PaneID:  id,
		RowBase: 0,
		Styles:  []protocol.StyleEntry{{}},
		Rows:    []protocol.RowDelta{{Row: 100}}, // maxGid = 100; old math: 100-9=91, new math: 100-7=93
	}
	state.onBufferDelta(delta, false)
	if vp := state.viewports.get(id); vp.ViewTopIdx != 93 {
		t.Fatalf("expected ViewTopIdx=93 (maxGid=100, 8 content rows), got %d", vp.ViewTopIdx)
	}
}

func TestOnBufferDelta_PaneNilSkipsAndLogs(t *testing.T) {
	state := newClientStateForTest(t)
	id := [16]byte{0xab}
	// Do NOT call ApplySnapshot; pane is absent from the cache.
	state.viewports.get(id).Rows = 10
	state.viewports.get(id).AutoFollow = true
	delta := protocol.BufferDelta{
		PaneID:  id,
		RowBase: 0,
		Styles:  []protocol.StyleEntry{{}},
		Rows:    []protocol.RowDelta{{Row: 100}},
	}
	state.onBufferDelta(delta, false)
	// Viewport must NOT advance silently using vp.Rows fallback.
	if vp := state.viewports.get(id); vp.ViewTopIdx != 0 {
		t.Fatalf("expected ViewTopIdx unchanged (=0) when pane is absent, got %d", vp.ViewTopIdx)
	}
}

func TestOnBufferDelta_ZeroContentRowsReturnsEarly(t *testing.T) {
	state := newClientStateForTest(t)
	id := [16]byte{0xab}
	// Status pane: NumContentRows == 0
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 5, Height: 1,
			ContentTopRow: 0, NumContentRows: 0,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})
	state.viewports.get(id).Rows = 1
	state.viewports.get(id).AutoFollow = true
	delta := protocol.BufferDelta{
		PaneID: id, RowBase: 0,
		Styles: []protocol.StyleEntry{{}},
		Rows:   []protocol.RowDelta{{Row: 0}},
	}
	state.onBufferDelta(delta, false)
	if vp := state.viewports.get(id); vp.ViewTopIdx != 0 {
		t.Fatalf("expected ViewTopIdx unchanged (=0) for zero-content pane, got %d", vp.ViewTopIdx)
	}
}
```

If `newClientStateForTest` helper doesn't exist, build the minimum state inline. Inspect existing tests in the file for their setup pattern.

- [ ] **Step 3: Run — verify failure.**

```bash
go test ./internal/runtime/client/ -run TestOnBufferDelta -v
```

Expected: FAIL with `expected ViewTopIdx=93, got 91` for the first test.

- [ ] **Step 4: Update `onBufferDelta`.**

In `internal/runtime/client/viewport_tracker.go` (~line 263), change:

```go
	top := maxGid - int64(vp.Rows-1)
```

to:

```go
	pane := s.cache.Pane(delta.PaneID)
	if pane == nil {
		// Delta arrived before the snapshot populated the cache. Silent
		// fallback to vp.Rows would reintroduce Issue #199 misalignment;
		// skip and log loudly instead.
		log.Printf("client: onBufferDelta: pane %x not in cache; skipping viewport advance", delta.PaneID)
		return
	}
	if pane.NumContentRows == 0 {
		// Zero-content pane (status panes, all-decoration apps) — no
		// viewport to advance.
		return
	}
	numContentRows := int64(pane.NumContentRows)
	top := maxGid - (numContentRows - 1)
```

(Verify `s.cache` is the access path to `BufferCache` from `clientState`. Add `import "log"` at the top of the file if not already imported. If `s.cache.Pane` doesn't exist, use whichever inspector the existing tests use.)

- [ ] **Step 5: Run — verify pass.**

```bash
go test ./internal/runtime/client/ -run TestOnBufferDelta_TopUsesContentRowCount -v
```

Expected: PASS.

- [ ] **Step 6: Run client runtime tests.**

```bash
go test ./internal/runtime/client/ -count=1
```

Expected: ALL PASS.

- [ ] **Step 7: Commit.**

```bash
git add internal/runtime/client/viewport_tracker.go internal/runtime/client/viewport_tracker_test.go
git commit -m "$(cat <<'EOF'
client: onBufferDelta computes top from numContentRows, not vp.Rows

vp.Rows includes border/decoration rows; numContentRows excludes them.
This stops the viewport top from being offset by the count of non-
content rows. Falls back to vp.Rows if the pane has no ContentBottom/
Top yet (pre-snapshot delta — should not happen in production).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: `renderer.rowSourceForPane` two-layer lookup

**Files:**
- Modify: `internal/runtime/client/renderer.go` `rowSourceForPane` (~line 165-189)
- Test: `internal/runtime/client/renderer_test.go` (or wherever renderer tests live)

- [ ] **Step 1: Locate renderer tests.**

```bash
ls internal/runtime/client/ | grep -E "render(er)?_test"
```

Use the surfaced file or create `renderer_test.go`.

- [ ] **Step 2: Add the failing tests.**

Append:

```go
func TestRowSourceForPane_DecorationLayer(t *testing.T) {
	state := newClientStateForTest(t)
	id := [16]byte{0xab}
	// Pane H=5, ContentTopRow=1, NumContentRows=3 (rowIdx 1..3 are content).
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, NumContentRows: 3,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})

	// Seed the viewport tracker via onBufferDelta so the gid math lines up.
	// onBufferDelta calls cache.ApplyDelta internally — do NOT also call
	// state.cache.ApplyDelta or the delta is applied twice.
	delta := protocol.BufferDelta{
		PaneID:  id,
		RowBase: 10,
		Styles:  []protocol.StyleEntry{{}},
		Rows: []protocol.RowDelta{
			// Content row: gid 10 (= RowBase + Row). With NumContentRows=3,
			// onBufferDelta will set ViewTopIdx = 10 - 2 = 8.
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "C", StyleIndex: 0}}},
		},
		DecorRows: []protocol.DecorRowDelta{
			{RowIdx: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "T", StyleIndex: 0}}},
			{RowIdx: 4, Spans: []protocol.CellSpan{{StartCol: 0, Text: "B", StyleIndex: 0}}},
		},
	}
	state.onBufferDelta(delta, false)

	pane := state.cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane missing")
	}

	// rowIdx 0 → decoration "T"
	if src := rowSourceForPane(state, pane, 0); len(src) == 0 || src[0].Ch != 'T' {
		t.Fatalf("rowIdx 0 expected decoration 'T', got %+v", src)
	}
	// rowIdx 4 → decoration "B"
	if src := rowSourceForPane(state, pane, 4); len(src) == 0 || src[0].Ch != 'B' {
		t.Fatalf("rowIdx 4 expected decoration 'B', got %+v", src)
	}
	// rowIdx 1 → content row (gid = ViewTopIdx + (rowIdx - ContentTopRow) = 8 + 0 = 8)
	// PaneCache should have gid 10 from the delta; gid 8 is a miss → nil.
	// To exercise the content-layer path with a hit, also test rowIdx 3 → gid 10:
	if src := rowSourceForPane(state, pane, 3); len(src) == 0 || src[0].Ch != 'C' {
		t.Fatalf("rowIdx 3 expected content 'C' (gid 10), got %+v", src)
	}
}

func TestRowSourceForPane_DecorationCacheMiss(t *testing.T) {
	state := newClientStateForTest(t)
	id := [16]byte{0xab}
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, NumContentRows: 3,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})
	pane := state.cache.Pane(id)
	src := rowSourceForPane(state, pane, 0)
	if src != nil {
		t.Fatalf("expected nil for decoration miss, got %+v", src)
	}
}
```

- [ ] **Step 3: Run — verify failure.**

```bash
go test ./internal/runtime/client/ -run TestRowSourceForPane_Decoration -v
```

Expected: FAIL — `rowIdx 0` returns content (or nil), not decoration.

- [ ] **Step 4: Update `rowSourceForPane`.**

In `internal/runtime/client/renderer.go` (~line 165), replace function body:

```go
func rowSourceForPane(state *clientState, pane *client.PaneState, rowIdx int) []client.Cell {
	if state.viewports == nil {
		return pane.RowCellsDirect(rowIdx)
	}
	vc, ok := state.paneViewportFor(pane.ID)
	if !ok {
		return pane.RowCellsDirect(rowIdx)
	}
	pc := state.paneCacheFor(pane.ID)
	if vc.AltScreen {
		row, found := pc.AltRowAt(rowIdx)
		if !found {
			return pane.RowCellsDirect(rowIdx)
		}
		return row
	}

	// Decoration layer: rowIdx outside the content range reads from the
	// pane's decoration cache (positional).
	contentEnd := int(pane.ContentTopRow) + int(pane.NumContentRows) // exclusive
	if pane.NumContentRows == 0 || rowIdx < int(pane.ContentTopRow) || rowIdx >= contentEnd {
		if row, ok := pane.DecorRowAt(uint16(rowIdx)); ok {
			return row
		}
		state.logDecorationMissOnce(pane.ID, uint16(rowIdx))
		return nil
	}

	// Content layer: rowIdx mapped via gid lookup.
	contentRowIdx := rowIdx - int(pane.ContentTopRow)
	gid := vc.ViewTopIdx + int64(contentRowIdx)
	row, found := pc.RowAt(gid)
	if !found {
		// Row not yet in cache (fetch is en route). Render blank rather than
		// showing stale BufferCache content at a mismatched globalIdx.
		// No log here — content-layer misses are normal during FetchRange.
		return nil
	}
	return row
}
```

Add a one-time-per-(paneID, rowIdx) logger to `clientState` (in `internal/runtime/client/state.go` or wherever `clientState` is defined):

```go
// decorationMissKey is the dedup key for once-per-pane-row decoration miss logging.
type decorationMissKey struct {
	paneID [16]byte
	rowIdx uint16
}

// logDecorationMissOnce emits a log line the first time the renderer
// observes a decoration cache miss for a given (paneID, rowIdx). Subsequent
// misses for the same key are silent.
func (s *clientState) logDecorationMissOnce(paneID [16]byte, rowIdx uint16) {
	s.decorMissMu.Lock()
	defer s.decorMissMu.Unlock()
	if s.decorMissSeen == nil {
		s.decorMissSeen = make(map[decorationMissKey]struct{})
	}
	key := decorationMissKey{paneID: paneID, rowIdx: rowIdx}
	if _, seen := s.decorMissSeen[key]; seen {
		return
	}
	s.decorMissSeen[key] = struct{}{}
	log.Printf("client: decoration cache miss for pane %x rowIdx %d (rendering blank); subsequent misses suppressed", paneID, rowIdx)
}
```

Add the corresponding fields to `clientState`:

```go
type clientState struct {
	// ... existing fields ...
	decorMissMu   sync.Mutex
	decorMissSeen map[decorationMissKey]struct{}
}
```

- [ ] **Step 5: Run — verify pass.**

```bash
go test ./internal/runtime/client/ -run TestRowSourceForPane_Decoration -v
```

Expected: PASS for both tests.

- [ ] **Step 6: Run client runtime tests.**

```bash
go test ./internal/runtime/client/ -count=1
```

Expected: ALL PASS.

- [ ] **Step 7: Commit.**

```bash
git add internal/runtime/client/renderer.go internal/runtime/client/renderer_test.go
git commit -m "$(cat <<'EOF'
client: rowSourceForPane two-layer lookup (content gid / decoration positional)

rowIdx in [ContentTopRow, ContentTopRow+NumContentRows-1] reads from PaneCache via
gid; outside the range reads from PaneState.DecorRows positionally.
A content-layer miss returns nil (preserves Plan A's no-stale-content
behavior); a decoration-layer miss also returns nil (renders blank;
should hit after the first delta).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11.5: Extend `memHarness` with the helpers Tasks 12–15 need

**Why this task exists.** The existing `memHarness` in `internal/runtime/server/viewport_integration_test.go` (around line 280) is single-pane and single-`sparseFakeApp`. The plan's integration tests need: a way to inspect the client's `BufferCache` for content bounds + decoration cache, a way to wait for the next or latest delta for a given pane, a way to switch focus between two panes, and a way to drive a daemon restart for the rehydrate test. Rather than fabricate these helpers ad hoc inside each test, add them once on `memHarness` so the tests stay readable.

Read the existing harness end-to-end before adding fields. Match its naming (`Publish`, `AwaitRow`, `ApplyViewport`) and its locking discipline (`h.mu` guards `rowsByGID` etc.).

**Files:**
- Modify: `internal/runtime/server/viewport_integration_test.go` (add fields and methods to `memHarness`; add an option struct or constructor variant for two-pane setup)

- [ ] **Step 1: Add a client-side `BufferCache` to the harness.**

The harness's existing `clientReadLoop` decodes `MsgBufferDelta` and `MsgTreeSnapshot` directly into `rowsByGID` / `altRowsByIdx`. To exercise the client-side `PaneState.{ContentTopRow, NumContentRows, decorRows}` path, also feed each decoded message into a real `client.BufferCache`:

```go
type memHarness struct {
	// ... existing fields ...
	clientCache *client.BufferCache
}
```

Initialize in `newMemHarness`:

```go
h.clientCache = client.NewBufferCache()
```

In `clientReadLoop`'s `MsgBufferDelta` case, after decoding, call `h.clientCache.ApplyDelta(delta)`. In the `MsgTreeSnapshot` case, after decoding, call `h.clientCache.ApplySnapshot(snap)`.

Add a method:

```go
func (h *memHarness) ClientPane(id [16]byte) *client.PaneState {
	return h.clientCache.Pane(id)
}
```

- [ ] **Step 2: Add `LatestDeltaForPane` / `WaitForDelta` helpers.**

The existing `AwaitRow` waits for a specific gid. For decoration assertions we need the most-recent full delta (so we can iterate `DecorRows`). Add:

```go
type lastDeltaTracker struct {
	mu       sync.Mutex
	byPane   map[[16]byte]protocol.BufferDelta
	cond     *sync.Cond
}

// (initialize in newMemHarness; populate in clientReadLoop's MsgBufferDelta
// case after the cache.ApplyDelta call.)

func (h *memHarness) WaitForDelta(t *testing.T, paneID [16]byte, timeout time.Duration) protocol.BufferDelta {
	t.Helper()
	deadline := time.Now().Add(timeout)
	h.lastDelta.mu.Lock()
	defer h.lastDelta.mu.Unlock()
	for {
		if d, ok := h.lastDelta.byPane[paneID]; ok {
			return d
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("WaitForDelta: timeout waiting for pane %x", paneID)
		}
		// Cond.Wait drops the mutex; reacquires before returning.
		// Use a Cond bound to h.lastDelta.mu.
		// (If sync.Cond seems heavy here, a simple per-pane channel works too.)
	}
}
```

Pick whichever shape (Cond, channel, polling with `time.Sleep`) fits the existing patterns in the file.

- [ ] **Step 3: Add daemon-restart helper.**

Look at `TestD2_FullCrossRestartCycle` in `internal/runtime/server/d2_cross_restart_integration_test.go` for the existing pattern. Extract the common boot sequence into a helper on the harness:

```go
// Restart shuts the in-process server down, recreates the Manager+Server
// pointing at the same persistence dir, and reconnects the client.
func (h *memHarness) Restart(t *testing.T) {
	t.Helper()
	// 1. Close existing serverConn / readerDone.
	// 2. Construct a new Manager with the same persistBasedir.
	// 3. Wire a new DesktopPublisher + Server.
	// 4. Open a new memconn pair and rebind h.serverConn / h.clientConn.
	// 5. Replay handshake (MsgHello / MsgWelcome / MsgResumeRequest with persisted sessionID).
}
```

Implementation lives next to `newMemHarness`; reuse the bits from `TestD2_FullCrossRestartCycle`.

- [ ] **Step 4: Add two-pane variant.**

Either a separate `newMemHarnessTwoPanes(t, cols, rows)` that wires two `sparseFakeApp` instances under a horizontal split, or an option struct passed to `newMemHarness`:

```go
type memHarnessOpts struct {
	cols, rows int
	twoPanes   bool
}
func newMemHarnessOpts(t *testing.T, opts memHarnessOpts) *memHarness { ... }
```

For the two-pane case, after `desktop.SwitchToWorkspace(1)`, split the workspace via the desktop's split API (look at `desktop_engine_core.go` for the actual call) and attach a second fake app. Track both pane IDs:

```go
type memHarness struct {
	// ... existing fields ...
	paneIDs []  [16]byte // [0] = original; [1] = right after split (when twoPanes=true)
}
```

Add `FocusPane(paneID [16]byte)` that drives the desktop's focus change.

- [ ] **Step 5: Verify the harness compiles + existing tests still pass.**

```bash
go test ./internal/runtime/server/ -run TestIntegration_ -count=1 -v
```

Expected: PASS for all existing viewport integration tests. The new helpers haven't been wired into any test yet — this step just confirms no regressions.

- [ ] **Step 6: Commit.**

```bash
git add internal/runtime/server/viewport_integration_test.go
git commit -m "$(cat <<'EOF'
test(server): extend memHarness for decoration + multi-pane + restart

Adds client.BufferCache, WaitForDelta, Restart, and two-pane support
to memHarness so subsequent integration tests can exercise the
decoration rendering paths without bespoke fixtures.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Integration — borders render on clean start

**Files:**
- Modify: `internal/runtime/server/viewport_integration_test.go`

- [ ] **Step 1: Add the failing test.**

Append to `internal/runtime/server/viewport_integration_test.go`:

```go
func TestPaneRenders_AllFourBorders_CleanStart(t *testing.T) {
	h := newMemHarness(t, 10, 6)
	defer h.serverConn.Close()
	// First publish primes the buffer + ships the initial deltas.
	h.Publish()

	delta := h.WaitForDelta(t, h.paneID, 2*time.Second)
	if len(delta.DecorRows) == 0 {
		t.Fatal("expected DecorRows for at least the top + bottom borders")
	}
	rowIdxs := map[uint16]bool{}
	for _, r := range delta.DecorRows {
		rowIdxs[r.RowIdx] = true
	}
	if !rowIdxs[0] || !rowIdxs[5] {
		t.Fatalf("expected decoration rows at rowIdx 0 and 5, got %v", rowIdxs)
	}

	// Verify content bounds reached the client cache.
	pane := h.ClientPane(h.paneID)
	if pane == nil {
		t.Fatalf("client cache missing pane %x", h.paneID)
	}
	if pane.ContentTopRow != 1 || pane.NumContentRows != 4 {
		t.Fatalf("client content bounds wrong: top=%d num=%d", pane.ContentTopRow, pane.NumContentRows)
	}

	// And the cache has decoration rows for both borders.
	if _, ok := pane.DecorRowAt(0); !ok {
		t.Fatalf("client decoration cache missing rowIdx 0")
	}
	if _, ok := pane.DecorRowAt(5); !ok {
		t.Fatalf("client decoration cache missing rowIdx 5")
	}
}
```

If the `sparseFakeApp` doesn't naturally produce `RowGlobalIdx` matching `(top=1, num=4)` for a 6-row pane, adjust its `RowGlobalIdx()` method (search the file for `func (a *sparseFakeApp) RowGlobalIdx`) so the bottom row reports `-1` (statusbar / bottom border slot).

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./internal/runtime/server/ -run TestPaneRenders_AllFourBorders_CleanStart -v
```

Expected: FAIL initially.

- [ ] **Step 3: Make the test green.**

Tasks 1-11 + 11.5 should make this pass without further publisher changes. If the assertions fail because `sparseFakeApp` doesn't model the bottom statusbar, adjust the fake.

- [ ] **Step 4: Run — verify pass.**

```bash
go test ./internal/runtime/server/ -run TestPaneRenders_AllFourBorders_CleanStart -v
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/runtime/server/viewport_integration_test.go
git commit -m "$(cat <<'EOF'
test(server): integration — pane renders all four borders on clean start

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Integration — borders render after daemon-restart rehydrate

**Files:**
- Modify: `internal/runtime/server/viewport_integration_test.go`

- [ ] **Step 1: Add the failing test.**

```go
func TestPaneRenders_AllFourBorders_AfterRehydrate(t *testing.T) {
	h := newMemHarness(t, 10, 6)
	defer h.serverConn.Close()
	h.Publish()
	h.WaitForDelta(t, h.paneID, 2*time.Second)
	sessionID := h.sessionID()

	// Daemon restart: rebuild the server in place and reconnect.
	h.Restart(t)
	// (Restart internally replays MsgHello + MsgResumeRequest using sessionID.)
	_ = sessionID

	h.Publish()
	delta := h.WaitForDelta(t, h.paneID, 2*time.Second)

	rowIdxs := map[uint16]bool{}
	for _, r := range delta.DecorRows {
		rowIdxs[r.RowIdx] = true
	}
	if !rowIdxs[0] || !rowIdxs[5] {
		t.Fatalf("post-rehydrate delta missing border DecorRows, got %v", rowIdxs)
	}

	pane := h.ClientPane(h.paneID)
	if pane == nil {
		t.Fatalf("client cache missing pane after rehydrate")
	}
	if _, ok := pane.DecorRowAt(0); !ok {
		t.Fatalf("client decoration cache missing rowIdx 0 after rehydrate")
	}
	if _, ok := pane.DecorRowAt(5); !ok {
		t.Fatalf("client decoration cache missing rowIdx 5 after rehydrate")
	}
	// Statusbar at rowIdx 4 (H-2) should also be a decoration row.
	if _, ok := pane.DecorRowAt(4); !ok {
		t.Fatalf("client decoration cache missing texterm-style internal statusbar at rowIdx 4")
	}
}
```

- [ ] **Step 2-4: Red, green, run.**

```bash
go test ./internal/runtime/server/ -run TestPaneRenders_AllFourBorders_AfterRehydrate -v
```

Expected: FAIL initially; PASS after Task 11.5's `Restart` is implemented.

- [ ] **Step 5: Commit.**

```bash
git commit -m "$(cat <<'EOF'
test(server): integration — pane renders all four borders + statusbar after daemon restart

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Integration — borders + decoration render when scrolled mid-history

**Files:**
- Modify: `internal/runtime/server/viewport_integration_test.go`

- [ ] **Step 1: Add the failing test.**

```go
func TestPaneRenders_AllFourBorders_ScrolledMidHistory(t *testing.T) {
	h := newMemHarness(t, 10, 6)
	defer h.serverConn.Close()
	h.Publish()
	h.WaitForDelta(t, h.paneID, 2*time.Second)

	// Append several screens of content via the fake app's writer.
	for i := 0; i < 50; i++ {
		h.fakeApp.AppendLine(fmt.Sprintf("line-%d", i))
		h.Publish()
	}
	h.WaitForDelta(t, h.paneID, 2*time.Second)

	// Scroll off the live edge: autoFollow=false, viewBottom mid-history.
	h.ApplyViewport(h.paneID, 10, 15, false, false)
	h.Publish()
	// Optionally wait for a fetch-range round-trip if the harness has one.

	pane := h.ClientPane(h.paneID)
	if pane == nil {
		t.Fatalf("client cache missing pane")
	}
	if pane.ContentTopRow != 1 || pane.NumContentRows != 4 {
		t.Fatalf("scrolled state lost content bounds: top=%d num=%d", pane.ContentTopRow, pane.NumContentRows)
	}
	if _, ok := pane.DecorRowAt(0); !ok {
		t.Fatalf("scrolled state lost top border decoration")
	}
	if _, ok := pane.DecorRowAt(5); !ok {
		t.Fatalf("scrolled state lost bottom border decoration")
	}
}
```

If `sparseFakeApp` lacks `AppendLine`, add it as a thin wrapper over the existing append API. Match the existing fake-app patterns in the file.

- [ ] **Step 2-5: Red, green, run, commit.**

```bash
git commit -m "$(cat <<'EOF'
test(server): integration — borders + bounds survive mid-history scroll

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Integration — focus change repaints borders via DecorRows

**Files:**
- Modify: `internal/runtime/server/viewport_integration_test.go`

- [ ] **Step 1: Add the failing test.**

```go
func TestPaneRenders_FocusChangeRepaintsBorders(t *testing.T) {
	h := newMemHarnessOpts(t, memHarnessOpts{cols: 10, rows: 6, twoPanes: true})
	defer h.serverConn.Close()
	h.Publish()
	h.WaitForDelta(t, h.paneIDs[0], 2*time.Second)
	h.WaitForDelta(t, h.paneIDs[1], 2*time.Second)

	// Snapshot what we've seen so subsequent WaitForDelta returns only NEW deltas.
	h.ResetDeltaTracker(h.paneIDs[0])
	h.ResetDeltaTracker(h.paneIDs[1])

	h.FocusPane(h.paneIDs[1])
	h.Publish()

	deltaA := h.WaitForDelta(t, h.paneIDs[0], 2*time.Second)
	deltaB := h.WaitForDelta(t, h.paneIDs[1], 2*time.Second)
	if len(deltaA.DecorRows) == 0 {
		t.Fatalf("pane A focus loss did not emit DecorRows")
	}
	if len(deltaB.DecorRows) == 0 {
		t.Fatalf("pane B focus gain did not emit DecorRows")
	}
}
```

`ResetDeltaTracker` is a small helper on the harness that clears the entry for a pane in the `lastDelta` map so the next `WaitForDelta` blocks until a fresh delta arrives. Add it as part of the Task 11.5 helpers or here, whichever lands first.

- [ ] **Step 2-5: Red, green, run, commit.**

```bash
git commit -m "$(cat <<'EOF'
test(server): integration — focus change ships border DecorRows for both panes

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Manual e2e + effects-compatibility check

**Files:** none modified — verification only.

- [ ] **Step 1: Build the canonical binary.**

```bash
make build
```

Expected: `./bin/texelation`, `./bin/texel-server`, `./bin/texel-client` all build cleanly.

- [ ] **Step 2: Reset session state.**

```bash
./bin/texelation --reset-state
# Confirm "yes" when prompted.
```

- [ ] **Step 3: Start fresh, exercise the texterm pane.**

```bash
./bin/texelation
```

Then in the running session:

1. Confirm a single texterm pane is visible with all 4 borders rendered (top, bottom, left, right).
2. Confirm content sits inside the borders (no offset; top row of typed text appears immediately under the top border, not 3 rows down).
3. Confirm the texterm internal statusbar renders content (typically just above the bottom border).
4. Type a few commands; scroll back; scroll forward — borders remain visible throughout.

- [ ] **Step 4: Daemon-restart rehydrate.**

While `texelation` is running:

```bash
ps aux | grep texel-server  # find the daemon PID
kill -9 <pid>                # crash the daemon (texelation supervisor restarts it)
```

After the supervisor brings the daemon back:

1. Confirm all 4 borders are visible from the first paint.
2. Confirm content is positioned correctly inside borders.
3. Confirm the texterm internal statusbar renders.

- [ ] **Step 5: Focus change check.**

Split the pane (Ctrl-A then `s` or `v` per the keybinding) → focus the new pane via Ctrl-A then arrow.

1. Confirm the previously-focused pane's border style transitions to inactive (per theme `pane.active` binding).
2. Confirm the newly-focused pane's border style transitions to active.
3. If the theme has a `fadeTint` effect bound to `pane.active`, confirm the transition is animated, not snap. (A failure here would suggest decoration deltas aren't reaching the client per animation tick — debug at server `bufferToDelta` instrumentation.)

- [ ] **Step 6: Resize check.**

Resize the terminal window to a dramatically different size. Borders must remain visible and content must reflow correctly inside them. ContentTopRow / NumContentRows recompute should keep the rowIdx ↔ gid map consistent.

- [ ] **Step 7: Document the verification.**

Write the verification observations into the PR description when opening it. Note any deviations from expected behavior.

No commit for this task — verification only.

---

## Self-Review Checklist (run before declaring the plan done)

- [ ] **Spec coverage:** every test the spec lists maps to a task (Tasks 1, 2, 6, 7, 8, 9, 10, 11, 12-15). Every type/field the spec introduces (`ContentTopRow`, `NumContentRows`, `DecorRowDelta`, `DecorRows`, `decorRows`, `DecorRowAt`, `computeContentBounds`, `logDecorationMissOnce`) is defined exactly once and referenced consistently.
- [ ] **Type consistency:** field types (`uint16`), method names (`ApplyDelta`, `ApplySnapshot`, `ResetRevisions`), and protocol names (`RowDelta`, `BufferDelta`, `PaneSnapshot`) match between earlier and later tasks.
- [ ] **No placeholders:** every code block is complete; no "TBD" or "fill in" steps.
- [ ] **Decoration contiguity invariant** is documented in spec; tests rely on it.
- [ ] **Zero-content state** is consistently treated: server emits `NumContentRows == 0`; client `onBufferDelta` returns early; client `rowSourceForPane` reads decoration for every rowIdx; no overloaded sentinel.
- [ ] **`decorRows` lock discipline:** all writes under `pane.rowsMu.Lock()`; all reads via `DecorRowAt` (which takes `pane.rowsMu.RLock()`); `ResetRevisions` takes per-pane `rowsMu.Lock()` before nilling; renderer never touches `pane.decorRows` directly.
- [ ] **Decoder rejects truncated v3:** `len(b) < 2` for the `DecorRows` count returns `ErrPayloadShort`; no v2 silent-accept fallback (project policy: no backward compat).
- [ ] **Alt-screen path** is unchanged for non-decoration cells; `bufferToDelta` short-circuits decoration emission for `snap.AltScreen=true`.
- [ ] **Race-detector run** before opening PR: `go test -race -count=1 ./protocol/ ./client/ ./internal/runtime/...`. Address any new races.
- [ ] **Manual e2e Task 16** completed; observations noted in PR description.
