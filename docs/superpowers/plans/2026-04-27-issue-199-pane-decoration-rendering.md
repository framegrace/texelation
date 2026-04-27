# Pane Border / Decoration Row Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make pane top/bottom borders and app decoration rows (texterm internal statusbar) actually render on the client, fixing the pre-existing `bufferToDelta gid<0` filter bug surfaced by Plan D2's daemon-restart rehydrate path.

**Architecture:** Server stays authoritative for all visible cells. Wire format gains `protocol.PaneSnapshot.{ContentTopRow, ContentBottomRow}` so the client can map rowIdx ↔ gid for content rows, and `protocol.BufferDelta.DecorRows` so border + app-decoration cells (anything where `RowGlobalIdx[y] < 0`) ride alongside content. Client renders via two-layer composite: gid-keyed `PaneCache` for content rowIdx in `[ContentTopRow, ContentBottomRow]`, positional decoration cache otherwise. Protocol bumps v2 → v3.

**Tech Stack:** Go 1.24, custom binary protocol (`protocol/`), `internal/runtime/server`, `internal/runtime/client`, `client` package, `texel` package. Tests use `go test`; integration tests use `internal/runtime/server/testutil/memconn.go`.

**Spec:** `docs/superpowers/specs/2026-04-27-issue-199-pane-decoration-rendering-design.md`

**Branch:** `feature/issue-199-pane-decoration-rendering` (already created off main).

---

## File Structure

| File | Responsibility | Lines (current) |
|------|---------------|-----------------|
| `protocol/protocol.go` | Protocol version constant | bump 2→3 |
| `protocol/buffer_delta.go` | `BufferDelta` type + encode/decode + `DecorRows` field | ~352 |
| `protocol/messages.go` | `PaneSnapshot` type + tree snapshot encode/decode + content bound fields | ~1050 |
| `texel/snapshot.go` | `texel.PaneSnapshot` type + `capturePaneSnapshot` content bound computation | ~600 |
| `internal/runtime/server/tree_convert.go` | Bridge `texel.PaneSnapshot` ↔ `protocol.PaneSnapshot` | ~150 |
| `internal/runtime/server/desktop_publisher.go` | `bufferToDelta`: route gid<0 rows into `DecorRows` | ~330 |
| `client/buffercache.go` | `PaneState.{ContentTopRow, ContentBottomRow, DecorRows}`; `ApplySnapshot` populates content bounds; `ApplyDelta` populates `DecorRows`; `ResetRevisions` clears it | ~600 |
| `internal/runtime/client/viewport_tracker.go` | `onBufferDelta`: `top` calc uses `numContentRows` from PaneState | ~280 |
| `internal/runtime/client/renderer.go` | `rowSourceForPane`: two-layer lookup (content gid / decoration positional) | ~550 |

---

## Task 1: Protocol — `BufferDelta.DecorRows` field + encode/decode

**Files:**
- Modify: `protocol/buffer_delta.go` (struct definition around line 92, encoder around line 109, decoder around line 247)
- Test: `protocol/buffer_delta_test.go`

**Goal:** Round-trip an empty and non-empty `DecorRows` slice.

- [ ] **Step 1: Add the failing test (red).**

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
		DecorRows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+", StyleIndex: 0}}},
			{Row: 22, Spans: []protocol.CellSpan{{StartCol: 0, Text: "-", StyleIndex: 0}}},
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
```

If `reflect` and `protocol` aren't imported in this file already, add them.

- [ ] **Step 2: Run the test — verify it fails.**

```bash
go test ./protocol/ -run TestEncodeDecodeBufferDelta_DecorRoundTrip -v
```

Expected: FAIL with `unknown field DecorRows in struct literal`.

- [ ] **Step 3: Add `DecorRows` field to `BufferDelta`.**

In `protocol/buffer_delta.go`, modify the `BufferDelta` struct to:

```go
type BufferDelta struct {
	PaneID    [16]byte
	Revision  uint32
	Flags     BufferDeltaFlags
	RowBase   int64
	Styles    []StyleEntry
	Rows      []RowDelta
	DecorRows []RowDelta // rows keyed by absolute rowIdx (not gid - RowBase). Borders + app decoration.
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
		if err := binary.Write(buf, binary.LittleEndian, row.Row); err != nil {
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

- [ ] **Step 5: Extend the decoder.**

In `DecodeBufferDelta`, just before `return delta, nil` at the end (~line 350), append:

```go
	if len(b) < 2 {
		// Pre-v3 wire (no DecorRows tail). Treat as empty.
		return delta, nil
	}
	decorCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if decorCount == 0 {
		if len(b) != 0 {
			return delta, ErrPayloadShort
		}
		return delta, nil
	}
	delta.DecorRows = make([]RowDelta, decorCount)
	for i := 0; i < int(decorCount); i++ {
		if len(b) < 4 {
			return delta, ErrPayloadShort
		}
		row := binary.LittleEndian.Uint16(b[:2])
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
		delta.DecorRows[i] = RowDelta{Row: row, Spans: spans}
	}
	if len(b) != 0 {
		return delta, ErrPayloadShort
	}
```

Note: the "pre-v3 wire" comment is intentional — the decoder treats a missing `DecorRows` tail as empty. We are NOT supporting v2 clients; the `Version=3` handshake will refuse them. This branch is for hand-crafted test fixtures that don't include the tail.

Also: `delta, nil` at the very end of the function should now happen only after the new tail is fully consumed. Replace the existing `return delta, nil` at end-of-func with the appended block above (which itself returns at the end).

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

## Task 2: Protocol — `PaneSnapshot.ContentTopRow` / `ContentBottomRow` + encode/decode

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
				PaneID:           [16]byte{0xaa},
				Revision:         3,
				Title:            "term",
				Rows:             nil,
				X:                0, Y: 0, Width: 80, Height: 24,
				AppType:          "texelterm",
				AppConfig:        "",
				ContentTopRow:    1,
				ContentBottomRow: 21,
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
	if decoded.Panes[0].ContentTopRow != 1 || decoded.Panes[0].ContentBottomRow != 21 {
		t.Fatalf("content bounds mismatch: got top=%d bottom=%d", decoded.Panes[0].ContentTopRow, decoded.Panes[0].ContentBottomRow)
	}
}

func TestEncodeDecodeTreeSnapshot_ZeroContentSentinel(t *testing.T) {
	original := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID:           [16]byte{0xbb},
			Title:            "all-decor",
			ContentTopRow:    1,
			ContentBottomRow: 0, // sentinel: no content rows
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
	if decoded.Panes[0].ContentTopRow != 1 || decoded.Panes[0].ContentBottomRow != 0 {
		t.Fatalf("sentinel mismatch: got top=%d bottom=%d", decoded.Panes[0].ContentTopRow, decoded.Panes[0].ContentBottomRow)
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
	PaneID           [16]byte
	Revision         uint32
	Title            string
	Rows             []string
	X                int32
	Y                int32
	Width            int32
	Height           int32
	AppType          string
	AppConfig        string
	ContentTopRow    uint16 // first content rowIdx; for the zero-content sentinel see ContentBottomRow
	ContentBottomRow uint16 // last content rowIdx; if < ContentTopRow, pane has no content rows
}
```

- [ ] **Step 4: Extend `EncodeTreeSnapshot`.**

In `protocol/messages.go` (~line 1003), inside the per-pane loop, after `if err := encodeString(buf, pane.AppConfig); err != nil { return nil, err }`, append:

```go
		if err := binary.Write(buf, binary.LittleEndian, pane.ContentTopRow); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.ContentBottomRow); err != nil {
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
		pane.ContentBottomRow = binary.LittleEndian.Uint16(remaining[2:4])
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
protocol: add PaneSnapshot.ContentTopRow/ContentBottomRow

Tells the client which rowIdx range maps to gids vs decoration. Bottom
< Top is the zero-content sentinel for all-decoration panes.

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
	// Simulate a snapshot's RowGlobalIdx layout for a 6-row pane:
	// [0]=-1 (top border), [1..3]=content gids, [4]=-1 (app statusbar), [5]=-1 (bottom border)
	rowIdx := []int64{-1, 100, 101, 102, -1, -1}
	top, bottom := computeContentBounds(rowIdx)
	if top != 1 || bottom != 3 {
		t.Fatalf("expected top=1 bottom=3, got top=%d bottom=%d", top, bottom)
	}
}

func TestCapturePaneSnapshot_ContentBoundsAllDecoration(t *testing.T) {
	// All -1 rows: zero content, sentinel top=1 bottom=0.
	rowIdx := []int64{-1, -1, -1}
	top, bottom := computeContentBounds(rowIdx)
	if top != 1 || bottom != 0 {
		t.Fatalf("expected sentinel top=1 bottom=0, got top=%d bottom=%d", top, bottom)
	}
}

func TestCapturePaneSnapshot_ContentBoundsEmpty(t *testing.T) {
	// Empty slice: sentinel top=1 bottom=0 (degenerate but well-defined).
	top, bottom := computeContentBounds(nil)
	if top != 1 || bottom != 0 {
		t.Fatalf("expected sentinel top=1 bottom=0, got top=%d bottom=%d", top, bottom)
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
	// ContentBottomRow is the last such rowIdx. If ContentBottomRow < ContentTopRow,
	// the pane has zero content rows (e.g., status panes, all-decoration apps).
	// For alt-screen panes these fields are populated but unused — clients
	// render alt-screen positionally regardless.
	ContentTopRow    uint16
	ContentBottomRow uint16
}
```

- [ ] **Step 4: Add `computeContentBounds` helper.**

In `texel/snapshot.go` near `allMinusOne` (~line 360), add:

```go
// computeContentBounds returns (top, bottom) where rowIdx is the
// inclusive range with RowGlobalIdx[y] >= 0. If no row has gid>=0, returns
// (1, 0) as the zero-content sentinel.
func computeContentBounds(rowIdx []int64) (uint16, uint16) {
	top := -1
	bottom := -1
	for y, gid := range rowIdx {
		if gid < 0 {
			continue
		}
		if top < 0 {
			top = y
		}
		bottom = y
	}
	if top < 0 {
		return 1, 0 // sentinel: zero content rows
	}
	return uint16(top), uint16(bottom)
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
	snap.ContentTopRow, snap.ContentBottomRow = computeContentBounds(snap.RowGlobalIdx)
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
texel: capturePaneSnapshot computes Content{Top,Bottom}Row from RowGlobalIdx

Bottom < Top sentinel signals zero content rows (status panes, all-
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
			ID:               [16]byte{0xab},
			Title:            "t",
			ContentTopRow:    2,
			ContentBottomRow: 17,
		}},
	}
	snap := treeCaptureToProtocol(capture)
	if len(snap.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(snap.Panes))
	}
	if snap.Panes[0].ContentTopRow != 2 || snap.Panes[0].ContentBottomRow != 17 {
		t.Fatalf("content bounds not passed through: top=%d bottom=%d",
			snap.Panes[0].ContentTopRow, snap.Panes[0].ContentBottomRow)
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
			PaneID:           pane.ID,
			Revision:         0,
			Title:            pane.Title,
			Rows:             nil,
			X:                int32(pane.Rect.X),
			Y:                int32(pane.Rect.Y),
			Width:            int32(pane.Rect.Width),
			Height:           int32(pane.Rect.Height),
			AppType:          pane.AppType,
			AppConfig:        encodeAppConfig(pane.AppConfig),
			ContentTopRow:    pane.ContentTopRow,
			ContentBottomRow: pane.ContentBottomRow,
		}
	}
```

- [ ] **Step 4: Update `protocolToTreeCapture` (reverse direction).**

In `internal/runtime/server/tree_convert.go` (~line 39), modify the per-pane construction:

```go
	capture.Panes[i] = texel.PaneSnapshot{
		ID:               pane.PaneID,
		Title:            pane.Title,
		Buffer:           buffer,
		RowGlobalIdx:     rowGlobalIdxAllMinusOne(len(buffer)),
		Rect:             texel.Rectangle{X: int(pane.X), Y: int(pane.Y), Width: int(pane.Width), Height: int(pane.Height)},
		AppType:          pane.AppType,
		AppConfig:        decodeAppConfig(pane.AppConfig),
		ContentTopRow:    pane.ContentTopRow,
		ContentBottomRow: pane.ContentBottomRow,
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
server: tree_convert passes Content{Top,Bottom}Row through both directions

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
		ID:               [16]byte{0xab},
		Buffer:           rows,
		RowGlobalIdx:     []int64{-1, 100, 101, 102, -1},
		ContentTopRow:    1,
		ContentBottomRow: 3,
	}
	vp := ClientViewport{Rows: 3, AutoFollow: true}
	prev := [][]texel.Cell(nil)

	delta := bufferToDelta(snap, vp, prev, 1)

	// Expect 2 decoration rows: rowIdx 0 (top border) and rowIdx 4 (bottom border).
	if len(delta.DecorRows) != 2 {
		t.Fatalf("expected 2 DecorRows, got %d: %+v", len(delta.DecorRows), delta.DecorRows)
	}
	gotIdx := map[uint16]bool{delta.DecorRows[0].Row: true, delta.DecorRows[1].Row: true}
	if !gotIdx[0] || !gotIdx[4] {
		t.Fatalf("expected decoration rows at rowIdx 0 and 4, got %v", gotIdx)
	}
	// Content rows still present.
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
		ID:               [16]byte{0xab},
		Buffer:           rows,
		RowGlobalIdx:     []int64{-1, 100, -1},
		ContentTopRow:    1,
		ContentBottomRow: 1,
	}
	vp := ClientViewport{Rows: 1, AutoFollow: true}
	// prev identical => no rows or DecorRows should ship.
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
		ID:               [16]byte{0xab},
		Buffer:           rows,
		RowGlobalIdx:     []int64{-1, 100, -1},
		ContentTopRow:    1,
		ContentBottomRow: 1,
	}
	vp := ClientViewport{Rows: 1, AutoFollow: true}
	prev := [][]texel.Cell{
		{{Ch: '#'}}, // different from current — should ship
		{{Ch: 'a'}}, // same — skip
		{{Ch: '+'}}, // same — skip
	}
	delta := bufferToDelta(snap, vp, prev, 1)
	if len(delta.DecorRows) != 1 || delta.DecorRows[0].Row != 0 {
		t.Fatalf("expected 1 DecorRows entry at rowIdx 0, got %+v", delta.DecorRows)
	}
}

func TestBufferToDelta_TexelTermInternalStatusbar(t *testing.T) {
	// 6-row layout: rowIdx 0 = top border, [1..3] = content, rowIdx 4 = app
	// internal statusbar (gid=-1), rowIdx 5 = bottom border. Mirrors the real
	// texelterm pane shape that surfaced the bug.
	rows := [][]texel.Cell{
		{{Ch: '+'}},
		{{Ch: 'a'}},
		{{Ch: 'b'}},
		{{Ch: 'c'}},
		{{Ch: 'S'}}, // app statusbar
		{{Ch: '+'}},
	}
	snap := texel.PaneSnapshot{
		ID:               [16]byte{0xab},
		Buffer:           rows,
		RowGlobalIdx:     []int64{-1, 100, 101, 102, -1, -1},
		ContentTopRow:    1,
		ContentBottomRow: 3,
	}
	vp := ClientViewport{Rows: 3, AutoFollow: true}
	delta := bufferToDelta(snap, vp, nil, 1)
	got := map[uint16]bool{}
	for _, r := range delta.DecorRows {
		got[r.Row] = true
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
var decorRows []protocol.RowDelta
for y, row := range snap.Buffer {
	if len(row) == 0 {
		continue
	}
	if y >= len(snap.RowGlobalIdx) {
		continue
	}
	if y < len(prev) && rowsEqual(row, prev[y]) {
		continue
	}
	gid := snap.RowGlobalIdx[y]
	if gid < 0 {
		// Decoration row: borders, app statusbars. Alt-screen panes
		// emit nothing here because RowGlobalIdx is all -1 AND we
		// take the existing alt-screen positional path elsewhere.
		if snap.AltScreen {
			continue
		}
		decorRows = append(decorRows, protocol.RowDelta{
			Row:   uint16(y),
			Spans: encodeRow(row),
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

Note the diff check moves *before* the gid check so it fires for both content and decoration rows. The `snap.AltScreen` guard prevents alt-screen panes (whose RowGlobalIdx is all -1) from spamming decoration rows; the existing alt-screen path handles them positionally via `delta.Flags & BufferDeltaAltScreen`.

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
- Modify: `client/buffercache.go` (struct ~line 22)
- Test: `client/buffercache_test.go`

- [ ] **Step 1: Add the failing test.**

Append to `client/buffercache_test.go`:

```go
func TestApplySnapshot_PopulatesContentBounds(t *testing.T) {
	cache := client.NewBufferCache()
	id := [16]byte{0xab}
	snapshot := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID:           id,
			Title:            "t",
			Width:            10, Height: 6,
			ContentTopRow:    1,
			ContentBottomRow: 4,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}
	cache.ApplySnapshot(snapshot)
	pane := cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane not registered")
	}
	if pane.ContentTopRow != 1 || pane.ContentBottomRow != 4 {
		t.Fatalf("content bounds not applied: top=%d bottom=%d", pane.ContentTopRow, pane.ContentBottomRow)
	}
}
```

If `cache.Pane(id)` accessor doesn't exist, use whatever inspector the existing tests use (e.g., reading via `cache.AllPanes()`).

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./client/ -run TestApplySnapshot_PopulatesContentBounds -v
```

Expected: FAIL with `pane.ContentTopRow undefined`.

- [ ] **Step 3: Add fields to `PaneState`.**

In `client/buffercache.go` (~line 22), update struct:

```go
type PaneState struct {
	ID               [16]byte
	Revision         uint32
	UpdatedAt        time.Time
	rowsMu           sync.RWMutex
	rows             map[int][]Cell
	Title            string
	Rect             clientRect
	Active           bool
	Resizing         bool
	ZOrder           int
	HandlesSelection bool

	// Content bounds (populated from PaneSnapshot). For non-altScreen panes,
	// rowIdx in [ContentTopRow, ContentBottomRow] maps to gid via the
	// viewport tracker; rowIdx outside reads from DecorRows. If
	// ContentBottomRow < ContentTopRow, the pane has zero content rows.
	ContentTopRow    uint16
	ContentBottomRow uint16

	// DecorRows holds positional decoration cells (borders + app statusbars).
	// Keyed by absolute rowIdx. Populated from BufferDelta.DecorRows.
	DecorRows map[uint16][]Cell

	// Dirty tracking for incremental rendering.
	Dirty       bool
	DirtyRows   map[int]bool
	HasAnimated bool
}
```

- [ ] **Step 4: Update `ApplySnapshot` to copy content bounds.**

In `client/buffercache.go` `ApplySnapshot` (~line 235), after `pane.Title = paneSnap.Title`, add:

```go
		pane.ContentTopRow = paneSnap.ContentTopRow
		pane.ContentBottomRow = paneSnap.ContentBottomRow
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
client: PaneState carries Content{Top,Bottom}Row + DecorRows fields

ApplySnapshot copies content bounds from the protocol pane snapshot.
DecorRows is populated by ApplyDelta (next commit).

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
		DecorRows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+--+", StyleIndex: 0}}},
			{Row: 9, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+--+", StyleIndex: 0}}},
		},
	}
	cache.ApplyDelta(delta)
	pane := cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane not registered")
	}
	if len(pane.DecorRows) != 2 {
		t.Fatalf("expected 2 decor rows, got %d: %+v", len(pane.DecorRows), pane.DecorRows)
	}
	if len(pane.DecorRows[0]) != 4 || pane.DecorRows[0][0].Ch != '+' {
		t.Fatalf("rowIdx 0 content wrong: %+v", pane.DecorRows[0])
	}
	if len(pane.DecorRows[9]) != 4 || pane.DecorRows[9][3].Ch != '+' {
		t.Fatalf("rowIdx 9 content wrong: %+v", pane.DecorRows[9])
	}
}
```

- [ ] **Step 2: Run — verify failure.**

```bash
go test ./client/ -run TestApplyDelta_PopulatesDecorRows -v
```

Expected: FAIL — `pane.DecorRows` is nil.

- [ ] **Step 3: Update `ApplyDelta`.**

In `client/buffercache.go` `ApplyDelta` (~line 149), within the function, just before `pane.Revision = delta.Revision` near the bottom, insert:

```go
	if len(delta.DecorRows) > 0 {
		if pane.DecorRows == nil {
			pane.DecorRows = make(map[uint16][]Cell, len(delta.DecorRows))
		}
		for _, rowDelta := range delta.DecorRows {
			row := pane.DecorRows[rowDelta.Row]
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
			pane.DecorRows[rowDelta.Row] = row
		}
		pane.Dirty = true
		// DecorRow changes invalidate row-level dirty tracking; force a
		// full re-render of this pane.
		pane.DirtyRows = nil
	}
```

This mirrors the content-row apply loop above it but writes into `pane.DecorRows` instead of `pane.rows`.

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
		DecorRows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "+", StyleIndex: 0}}},
		},
	}
	cache.ApplyDelta(delta)
	if pane := cache.Pane(id); pane == nil || len(pane.DecorRows) != 1 {
		t.Fatalf("pre-reset: expected 1 DecorRows entry, got %+v", pane)
	}
	cache.ResetRevisions()
	pane := cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane gone after reset")
	}
	if len(pane.DecorRows) != 0 {
		t.Fatalf("expected DecorRows cleared, got %d entries", len(pane.DecorRows))
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

Expected: FAIL — DecorRows still has 1 entry after reset.

- [ ] **Step 3: Update `ResetRevisions`.**

In `client/buffercache.go` (~line 585), modify to:

```go
func (c *BufferCache) ResetRevisions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pane := range c.panes {
		pane.Revision = 0
		pane.DecorRows = nil
	}
}
```

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

- [ ] **Step 2: Add the failing test.**

Append:

```go
func TestOnBufferDelta_TopUsesContentRowCount(t *testing.T) {
	state := newClientStateForTest(t) // helper from existing tests
	id := [16]byte{0xab}
	// Pane has H=10 rows; ContentTopRow=1, ContentBottomRow=8 → 8 content rows.
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 5, Height: 10,
			ContentTopRow: 1, ContentBottomRow: 8,
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
		Rows: []protocol.RowDelta{
			{Row: 7}, // gid = RowBase + 7 = 7 (maxGid)
		},
	}
	state.onBufferDelta(delta, false)
	vp := state.viewports.get(id)
	// numContentRows = 8 → top = 7 - (8-1) = 0
	if vp.ViewTopIdx != 0 {
		t.Fatalf("expected ViewTopIdx=0 (8 content rows), got %d", vp.ViewTopIdx)
	}
}
```

If `newClientStateForTest` helper doesn't exist, build the minimum state inline. Inspect existing tests in the file for their setup pattern.

- [ ] **Step 3: Run — verify failure.**

```bash
go test ./internal/runtime/client/ -run TestOnBufferDelta_TopUsesContentRowCount -v
```

Expected: FAIL — `vp.ViewTopIdx` will be `7 - (10-1) = -2` clamped to 0, but the failure is in the math derivation, not the clamp. To make the failure crisp, change the test to use a larger maxGid where the bug shows up: set `Rows: []protocol.RowDelta{{Row: 100}}` and `RowBase: 0`. With old code: top = 100 - 9 = 91. With new code: top = 100 - 7 = 93.

Replace the `Rows: []protocol.RowDelta{{Row: 7}}` line and the assertion to:

```go
		Rows: []protocol.RowDelta{{Row: 100}},
```

```go
	if vp.ViewTopIdx != 93 {
		t.Fatalf("expected ViewTopIdx=93 (maxGid=100, 8 content rows), got %d", vp.ViewTopIdx)
	}
```

Re-run; expected: FAIL with `expected ViewTopIdx=93, got 91`.

- [ ] **Step 4: Update `onBufferDelta`.**

In `internal/runtime/client/viewport_tracker.go` (~line 263), change:

```go
	top := maxGid - int64(vp.Rows-1)
```

to:

```go
	pane := s.cache.Pane(delta.PaneID)
	numContentRows := int64(vp.Rows)
	if pane != nil && pane.ContentBottomRow >= pane.ContentTopRow {
		numContentRows = int64(pane.ContentBottomRow) - int64(pane.ContentTopRow) + 1
	}
	if numContentRows <= 0 {
		return
	}
	top := maxGid - (numContentRows - 1)
```

(Verify `s.cache` is the access path to `BufferCache` from `clientState`; if it's named differently, adjust. Inspect `internal/runtime/client/state.go` or the surrounding file for the field name.)

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

- [ ] **Step 2: Add the failing test.**

Append:

```go
func TestRowSourceForPane_DecorationLayer(t *testing.T) {
	state := newClientStateForTest(t)
	id := [16]byte{0xab}
	// Pane H=5, ContentTopRow=1, ContentBottomRow=3.
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, ContentBottomRow: 3,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})
	// Apply a delta with 1 content row (gid=10) and 2 decoration rows (rowIdx 0 + 4).
	delta := protocol.BufferDelta{
		PaneID:  id,
		RowBase: 10,
		Styles:  []protocol.StyleEntry{{}},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "C", StyleIndex: 0}}},
		},
		DecorRows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "T", StyleIndex: 0}}},
			{Row: 4, Spans: []protocol.CellSpan{{StartCol: 0, Text: "B", StyleIndex: 0}}},
		},
	}
	state.cache.ApplyDelta(delta)
	state.onBufferDelta(delta, false)

	pane := state.cache.Pane(id)
	if pane == nil {
		t.Fatalf("pane missing")
	}

	// rowIdx 0 → decoration "T"
	src := rowSourceForPane(state, pane, 0)
	if len(src) == 0 || src[0].Ch != 'T' {
		t.Fatalf("rowIdx 0 expected decoration 'T', got %+v", src)
	}
	// rowIdx 1 → content "C" (gid 10)
	src = rowSourceForPane(state, pane, 1)
	if len(src) == 0 || src[0].Ch != 'C' {
		t.Fatalf("rowIdx 1 expected content 'C', got %+v", src)
	}
	// rowIdx 4 → decoration "B"
	src = rowSourceForPane(state, pane, 4)
	if len(src) == 0 || src[0].Ch != 'B' {
		t.Fatalf("rowIdx 4 expected decoration 'B', got %+v", src)
	}
}

func TestRowSourceForPane_DecorationCacheMiss(t *testing.T) {
	state := newClientStateForTest(t)
	id := [16]byte{0xab}
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, ContentBottomRow: 3,
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

	// Decoration layer: rowIdx outside the content-bound range reads from
	// PaneState.DecorRows (positional).
	if rowIdx < int(pane.ContentTopRow) || rowIdx > int(pane.ContentBottomRow) {
		if pane.DecorRows != nil {
			if row, ok := pane.DecorRows[uint16(rowIdx)]; ok {
				return row
			}
		}
		return nil
	}

	// Content layer: rowIdx mapped via gid lookup.
	contentRowIdx := rowIdx - int(pane.ContentTopRow)
	gid := vc.ViewTopIdx + int64(contentRowIdx)
	row, found := pc.RowAt(gid)
	if !found {
		// Row not yet in cache (fetch is en route). Render blank rather than
		// showing stale BufferCache content at a mismatched globalIdx.
		return nil
	}
	return row
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

rowIdx in [ContentTopRow, ContentBottomRow] reads from PaneCache via
gid; outside the range reads from PaneState.DecorRows positionally.
A content-layer miss returns nil (preserves Plan A's no-stale-content
behavior); a decoration-layer miss also returns nil (renders blank;
should hit after the first delta).

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Integration — borders render on clean start

**Files:**
- Modify: `internal/runtime/server/viewport_integration_test.go` (extend with new test cases)
- Helper: existing memconn harness

- [ ] **Step 1: Add the failing test.**

Append to `internal/runtime/server/viewport_integration_test.go` (or create a sibling `decoration_integration_test.go`):

```go
func TestIntegration_PaneRendersAllFourBorders_CleanStart(t *testing.T) {
	// Build a minimal harness with a single texterm-shaped pane.
	// Use the existing helper that produces a sparseFakeApp + memconn
	// pair (search the file for sparseFakeApp + buildHarness).
	h := buildIntegrationHarness(t, harnessOpts{
		paneRows: 6,
		paneCols: 10,
		// fake provides RowGlobalIdx so capturePaneSnapshot computes
		// ContentTopRow=1, ContentBottomRow=4 (top border + 4 content + bottom border).
	})
	defer h.Close()
	h.WaitForFirstDelta(t)

	// Inspect what the client sees:
	delta := h.LastDelta(t)
	if len(delta.DecorRows) == 0 {
		t.Fatal("expected DecorRows with at least 2 entries (top + bottom borders)")
	}
	rowIdxs := make(map[uint16]bool, len(delta.DecorRows))
	for _, r := range delta.DecorRows {
		rowIdxs[r.Row] = true
	}
	if !rowIdxs[0] || !rowIdxs[5] {
		t.Fatalf("expected border decoration rows at rowIdx 0 and 5, got %v", rowIdxs)
	}

	// Verify content bounds reached the client cache.
	pane := h.ClientCache().Pane(h.PaneID())
	if pane.ContentTopRow != 1 || pane.ContentBottomRow != 4 {
		t.Fatalf("client content bounds wrong: top=%d bottom=%d", pane.ContentTopRow, pane.ContentBottomRow)
	}
}
```

If `buildIntegrationHarness`, `WaitForFirstDelta`, `LastDelta`, `ClientCache`, `PaneID` don't exist as helpers, look at how existing tests like `TestIntegration_ClipsAndFetches` set up their fixtures and reuse the same pattern. The test author may need to extract a small helper from an existing test if no shared one exists yet.

- [ ] **Step 2: Run — verify failure.**

```bash
go test -tags=integration ./internal/runtime/server/ -run TestIntegration_PaneRendersAllFourBorders_CleanStart -v
```

Expected: FAIL — initially the harness may not exist or the assertions fail.

- [ ] **Step 3: Make the test green.**

Most of the wiring should already work after Tasks 1-11. If the test fails for harness-shape reasons, build the missing helpers as the smallest possible addition — do NOT introduce a generalised harness; copy the inline shape used by existing tests.

If the assertions fail (e.g., border rowIdx don't match), inspect why — likely the fake app's `RowGlobalIdx` doesn't put -1 at the expected slots. Adjust the fake to mirror what a real texterm produces.

- [ ] **Step 4: Run — verify pass.**

```bash
go test -tags=integration ./internal/runtime/server/ -run TestIntegration_PaneRendersAllFourBorders_CleanStart -v
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
- Modify: `internal/runtime/server/viewport_integration_test.go` (or the same file used in Task 12)

- [ ] **Step 1: Add the failing test.**

This test is the Plan D2 cross-restart cycle adapted to assert decoration rows. Find the existing Plan D2 cross-restart integration test (search for `TestD2_FullCrossRestartCycle` or `TestIntegration_D2`) and extend it, OR create a sibling that exercises the same cycle:

```go
func TestIntegration_PaneRendersAllFourBorders_AfterRehydrate(t *testing.T) {
	// 1. Boot fresh daemon, attach client, get initial render.
	// 2. Simulate daemon restart: tear down + boot a new server backed by
	//    the same persistence dir (mirrors Plan D2 harness).
	// 3. Reconnect with the persisted sessionID via MsgResumeRequest.
	// 4. Assert the post-resume BufferDelta carries border DecorRows AND
	//    the client renders them at rowIdx 0 and H-1.
	// (See TestD2_FullCrossRestartCycle for the harness pattern.)

	h := buildD2Harness(t, harnessOpts{paneRows: 6, paneCols: 10})
	defer h.Close()
	h.WaitForFirstDelta(t)
	sessionID := h.SessionID()
	h.RestartDaemon(t)
	h.Reconnect(t, sessionID)

	delta := h.WaitForPostResumeDelta(t)
	rowIdxs := map[uint16]bool{}
	for _, r := range delta.DecorRows {
		rowIdxs[r.Row] = true
	}
	if !rowIdxs[0] || !rowIdxs[5] {
		t.Fatalf("post-rehydrate delta missing border DecorRows, got %v", rowIdxs)
	}
}
```

The exact helper names must mirror the existing Plan D2 test harness. If the existing harness uses different names, adapt.

- [ ] **Step 2-4: Verify red, green, run.**

```bash
go test -tags=integration ./internal/runtime/server/ -run TestIntegration_PaneRendersAllFourBorders_AfterRehydrate -v
```

Expect FAIL initially (harness setup or missing assertions); make it green.

- [ ] **Step 5: Commit.**

```bash
git add internal/runtime/server/viewport_integration_test.go
git commit -m "$(cat <<'EOF'
test(server): integration — pane renders all four borders after daemon restart

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
func TestIntegration_PaneRendersAllFourBorders_ScrolledMidHistory(t *testing.T) {
	h := buildIntegrationHarness(t, harnessOpts{paneRows: 6, paneCols: 10})
	defer h.Close()
	h.WaitForFirstDelta(t)

	// Push several pages of content so the client can scroll back.
	for i := 0; i < 50; i++ {
		h.AppendContentRow(t, fmt.Sprintf("line-%d", i))
	}
	h.WaitForLatestDelta(t)
	// Scroll the client off the live edge (autoFollow=false, viewBottom mid-history).
	h.SetClientViewport(t, ClientViewportOpts{
		AutoFollow:    false,
		ViewBottomIdx: 10,
	})
	h.WaitForFetchRangeRoundTrip(t)

	pane := h.ClientCache().Pane(h.PaneID())
	if pane.ContentTopRow != 1 || pane.ContentBottomRow != 4 {
		t.Fatalf("scrolled state lost content bounds: top=%d bottom=%d", pane.ContentTopRow, pane.ContentBottomRow)
	}
	if len(pane.DecorRows) < 2 {
		t.Fatalf("scrolled state lost decoration rows: %+v", pane.DecorRows)
	}
}
```

- [ ] **Step 2-5: Red, green, run, commit.**

Same shape as Tasks 12-13.

```bash
git commit -m "$(cat <<'EOF'
test(server): integration — borders survive mid-history scroll

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
func TestIntegration_FocusChangeRepaintsBorders(t *testing.T) {
	h := buildIntegrationHarness(t, harnessOpts{
		paneRows: 6, paneCols: 10,
		twoPanes: true, // harness must support a 2-pane setup
	})
	defer h.Close()
	h.WaitForFirstDelta(t)
	h.DrainDeltas(t) // ignore initial deltas

	// Toggle focus from pane A to pane B.
	h.FocusPane(t, h.PaneB())

	// Both panes should emit DecorRows for their border style change.
	deltaA := h.NextDeltaFor(t, h.PaneA())
	deltaB := h.NextDeltaFor(t, h.PaneB())
	if len(deltaA.DecorRows) == 0 {
		t.Fatalf("pane A focus loss did not emit DecorRows")
	}
	if len(deltaB.DecorRows) == 0 {
		t.Fatalf("pane B focus gain did not emit DecorRows")
	}
}
```

- [ ] **Step 2-5: Red, green, run, commit.**

```bash
git commit -m "$(cat <<'EOF'
test(server): integration — focus change ships border DecorRows

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

Resize the terminal window to a dramatically different size. Borders must remain visible and content must reflow correctly inside them. ContentTopRow / ContentBottomRow recompute should keep the rowIdx ↔ gid map consistent.

- [ ] **Step 7: Document the verification.**

Write the verification observations into the PR description when opening it. Note any deviations from expected behavior.

No commit for this task — verification only.

---

## Self-Review Checklist (run before declaring the plan done)

- [ ] **Spec coverage:** every test the spec lists maps to a task (Tasks 1, 2, 6, 7, 8, 9, 10, 11, 12-15). Every type/field the spec introduces (`ContentTopRow`, `ContentBottomRow`, `DecorRows`, `numContentRows`, `computeContentBounds`) is defined exactly once and referenced consistently.
- [ ] **Type consistency:** field types (`uint16`), method names (`ApplyDelta`, `ApplySnapshot`, `ResetRevisions`), and protocol names (`RowDelta`, `BufferDelta`, `PaneSnapshot`) match between earlier and later tasks.
- [ ] **No placeholders:** every code block is complete; no "TBD" or "fill in" steps.
- [ ] **Decoration contiguity invariant** is documented in spec; tests rely on it.
- [ ] **`Bottom < Top` zero-content sentinel** is consistently treated: server emits `(1, 0)` for empty; client checks `pane.ContentBottomRow >= pane.ContentTopRow` before doing the math.
- [ ] **Alt-screen path** is unchanged for non-decoration cells; `bufferToDelta` short-circuits decoration emission for `snap.AltScreen=true`.
- [ ] **Race-detector run** before opening PR: `go test -race -count=1 ./protocol/ ./client/ ./internal/runtime/...`. Address any new races.
- [ ] **Manual e2e Task 16** completed; observations noted in PR description.
