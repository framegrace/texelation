# Viewport-Only Client Rendering (Issue #199)

**Status**: Design
**Date**: 2026-04-20
**Issue**: [#199](https://github.com/framegrace/texelation/issues/199)
**Relates to**:
- `docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md`
- `docs/superpowers/specs/2026-04-16-sparse-resize-reflow-design.md`
- `docs/CLIENT_SERVER_ARCHITECTURE.md`

## Background

Today the server renders and broadcasts the full pane buffer for every pane, regardless of what the client is actually looking at. For terminals running long Claude / AI-agent sessions this produces a pathological pattern: the TUI's visible prompt may sit 1,000+ rows deep in scrollback, but the server still paints and ships the entire pane on every resize, draw, or reconnect. Scrolled-back browsing and aggressive repaints both pay for content nobody is rendering.

Issue #199 asks the server to render only the client's visible viewport (plus small overscan), while preserving everything that works today: selection, copy, search, scrollback persistence, resume, and multi-pane/multi-client fan-out.

A pre-design risk audit (2026-04-19, five parallel agents) identified three sub-problems that must be resolved in specific order before the protocol can be designed without traps:

1. Selection / copy across uncached scrollback.
2. Reconnect to a scrolled-back state.
3. Alt-screen semantics.

The protocol shape (`FetchRange`, clipped `BufferDelta`, sequencing) falls out naturally once those three are settled. This spec captures the decisions from the brainstorming session that followed.

## Goals

- Server-rendered data per pane is clipped to the client's visible viewport plus symmetric overscan.
- Clients can request arbitrary scrollback ranges on demand (`FetchRange`), shipping in the same PR as clipping — no half-state where a scrolled-back client is stuck with no data.
- Selection and copy work across the entire scrollback, including rows that are cold (on disk) or simply never cached on the client.
- Reconnect lands the user back at the exact scrolled-back position they left, across client disconnect *and* best-effort across server restart.
- Alt-screen remains ephemeral, viewport-sized, and cleanly opts out of the globalIdx machinery.
- Existing UX is preserved. Nothing that works today should regress.

## Non-goals

- Copy-stack UI, multi-entry clipboard operations, or post-copy transforms (colorize / reformat to modal card). Scaffolded via stable `entryID` in the copy-commit path; not shipped in this PR.
- Predictive prefetch of scrollback beyond the fixed 1× overscan window. User explicitly declined pre-optimization; revisit only if measurement shows need.
- Alt-screen reflow (already out of scope per sparse design).
- Reworking the statusbar app beyond adding a "scrollback fetch pending" indicator subscription.
- Investigating why current selection UX appears to be viewport-only rather than cross-scrollback (user suspects regression). Tracked separately; not this PR.

## Non-negotiables (from audit)

- `FetchRange` ships in the same PR as viewport clipping.
- Selection/copy is resolved server-side; client is a highlight renderer and event source.
- Resume carries viewport state (anchor + wrap-offset + autoFollow) per pane.
- Every `BufferDelta` on the wire carries a monotonic sequence number.
- Alt-screen explicitly opts out of `FetchRange`.
- Tests rewrite budget: existing viewport assertions that inspect full-pane buffers must be rewritten against viewport-clipped buffers; estimate is included in the plan.

## The Approach

### Architecture at a glance

```
┌─────────────────────────────────────────────────────────────────┐
│ Client                                                          │
│                                                                 │
│   Input ── MsgViewportUpdate ─┐        ┌─── MsgBufferDelta ──┐  │
│                               │        │   (Sequence,         │  │
│   ViewWindow (scroll state)   │        │    RowBase,          │  │
│   │                           │        │    Flags.AltScreen)  │  │
│   ▼                           │        │                      │  │
│   Per-pane sparse cache  ◄────┼────────┴──────────────────────┘  │
│   keyed by globalIdx          │                                 │
│                               │  ┌───► MsgFetchRangeResponse    │
│   MsgFetchRange  ◄────────────┼──┤     (revision stamp)         │
│                               │                                 │
│   MsgResolveBoundary  ◄───────┼─► resolved (idx,col)             │
│   MsgCaptureSelection ◄───────┼─► {entryID, plain, ansi}        │
│                               │                                 │
└───────────────────────────────┼─────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│ Server                                                          │
│                                                                 │
│   Per-session ClientViewport{paneID -> {viewTop, viewBottom,    │
│                                         overscan, altScreen,    │
│                                         autoFollow}}            │
│                                                                 │
│   Publisher clips per-client on emit                             │
│   Statusbar subscribes to "fetch pending" signal                │
│                                                                 │
│   sparse.Store / WriteWindow / ViewWindow (unchanged)            │
│   alt buffer (VTerm altBuffer, unchanged)                        │
│                                                                 │
│   Persistence: PageStore + WAL + NEW PaneViewportState          │
└─────────────────────────────────────────────────────────────────┘
```

### Sub-problem 1 — Selection / copy across uncached scrollback

**Decision.** Selection is a server-resolved concept. The client owns live selection *state* (so mouse drag and highlight rendering stay zero-RPC); the server owns the resolution of that state into text. This keeps the hot path local and makes mosh-style silent wrong-byte grabs structurally impossible — the client never reads bytes past what the server has shipped it.

**Coordinate space.**
- Main screen: `(globalIdx, col)`. Stable across scrollback growth because the sparse store is append-only by `globalIdx`.
- Alt screen: `(screenRow, col)`. Separate, ephemeral.

**Live selection (client-side).** The client's `selectionState` holds `{anchor, head, mode}` in the current pane's coordinate space. Mouse drag, keyboard extend, autoscroll-at-edge, and highlight rendering are all local. Screen-row ↔ `globalIdx` translation is done via `ViewWindow` (already tracks which `globalIdx` maps to each visible row).

**Mode resolution (server RPC).** Some selection modes require server-only knowledge. The client sends a `MsgResolveBoundary` to get a boundary position:

```
MsgResolveBoundary { paneID, pos: (globalIdx, col) | (screenRow, col),
                     direction: up | down, mode }
 →
MsgResolveBoundaryResponse { resolved: (globalIdx, col) | (screenRow, col)
                           | unsupported }
```

Modes: `word`, `line`, `paragraph`, `prompt`, `prompt-output`, `logical-line`.

- `word` / `line` / `paragraph` / `logical-line`: resolvable from the sparse store (main) or `altBuffer` (alt).
- `prompt` / `prompt-output`: uses OSC 133 anchors already tracked by the server. Main-screen only; on alt-screen, server returns `unsupported` and the client disables the binding.

Double-click / triple-click are two `ResolveBoundary` calls (one each direction) paired with `CaptureSelection` below.

**Commit / copy (server RPC).** On mouse-up or explicit copy command, the client sends:

```
MsgCaptureSelection {
    paneID,
    coordSpace: main | alt,
    anchor, head, mode,
    formats: [plain, ansi],        // 'b' supported from day one
}
 →
MsgCaptureResult {
    entryID,                         // stable server-side ID
    formats: { plain: string, ansi?: string },
}
```

Server walks the range (loading cold pages from `PageStore` synchronously if needed), respects `Wrapped` flags (soft-join within a wrapped logical line; CRLF on hard breaks), trims trailing blanks per row, and returns the requested formats. The server pushes `entryID` onto a session-scoped in-memory copy stack. System clipboard sync reuses the existing `MsgClipboardSet`.

**Copy-stack scaffold (out of scope to ship, in scope to design for).** The returned `entryID` is stable and addressable. Follow-up messages (`MsgListStack`, `MsgGetStackEntry`, `MsgTransformStackEntry`, `MsgDeleteStackEntry`) layer on without protocol churn. Transforms that produce a modal card reuse the existing `texel/cards/` pipeline.

**Scrollback regression investigation.** Current code routes selection strictly through the visible pane's `BufferCache`, meaning cross-scrollback selection is not available today. User believes this is a regression and plans to track it separately. Not a prerequisite for this PR.

### Sub-problem 2 — Reconnect to a scrolled-back state

**Decision.** Resume carries per-pane viewport state and the server honors it on first paint — no snap to live edge. Anchor is precise enough to land on the exact wrap-segment the user was viewing, and the state is persisted alongside the scrollback so it survives server restarts on a best-effort basis.

**Per-pane resume payload (new).**

```go
type PaneViewportState struct {
    PaneID          [16]byte
    AltScreen       bool     // if true, scroll fields are ignored
    ViewBottomIdx   int64    // globalIdx of the bottom-most logical line shown
    WrapSegmentIdx  uint16   // which wrap-segment of ViewBottomIdx was at the bottom row
    AutoFollow      bool
    ViewportRows    uint16
    ViewportCols    uint16
}
```

`MsgResumeRequest` is extended with `[]PaneViewportState`. The client builds this from its `ViewWindow` state on disconnect and from its per-pane selection of segment at render time.

**Server-side first-paint logic.**

For each pane in the request:
1. If `AltScreen=true`: send current `altBuffer` content; skip scroll resolution.
2. Else if `AutoFollow=true`: clamp `ViewBottomIdx` to `sparse.Store.Max()` at current server-side geometry.
3. Else: honor `ViewBottomIdx` exactly. Compute `ViewTopIdx` by walking backwards from `ViewBottomIdx` for `ViewportRows` screen rows at the current pane width, respecting `Wrapped` flags. Place `WrapSegmentIdx` at the bottommost physical row.
4. Send a viewport-clipped `TreeSnapshot` containing rows `[ViewTopIdx, ViewBottomIdx]` per pane, plus symmetric 1× overscan.

**Missing-anchor policy.** If `ViewBottomIdx < sparse.Store.OldestRetained()` (scrollback evicted while client was offline), server snaps `ViewBottomIdx` to the oldest retained row and forces `AutoFollow=false`. Preserves the user's intent of being scrolled back, and is indistinguishable from the UX of scrolling up and hitting the top of available history.

**Best-effort cross-restart resume.** Server persists `PaneViewportState` alongside the scrollback WAL / PageStore. On server restart, the next `MsgResumeRequest` for that session is matched against persisted viewport state; if found and `ViewBottomIdx` is still in the store, restore exactly. Otherwise fall back to missing-anchor policy above.

**Fresh connect (no SessionID).** No prior viewport state; server sends live-edge viewport with `AutoFollow=true`. Equivalent to current behavior minus the full-pane buffer.

`LastSequence` (existing) continues to drive delta replay and is orthogonal to `PaneViewportState`.

### Sub-problem 3 — Alt-screen semantics

**Decision.** Alt-screen stays a separate, flat, viewport-sized 2D buffer, outside the sparse store. It opts out of the globalIdx machinery wholesale. Entry/exit transitions preserve the main-screen `ViewWindow` state, matching current behavior.

**Clipping.** No-op. Alt-screen *is* the viewport. Server sends the full alt buffer on every delta. Row indices in the delta are screen-local (the existing `RowDelta.Row uint16` suffices unchanged).

**FetchRange.** Opt-out. Server responds with `flags = AltScreenActive` and empty payload. Client should not send `MsgFetchRange` while `altScreen=true` for that pane; if it does (race), the response above is expected — not an error.

**Delta flag.** `BufferDeltaFlags.AltScreen` bit. Set to 1 → row indices target `altBuffer`. Set to 0 → row indices are offsets from `RowBase` in the sparse store (main screen).

**Entry (DECSET 1049 / 47 / 1047).** Server switches the pane's publisher mode. Subsequent deltas carry `Flags.AltScreen=1`. Client's main-screen `ViewWindow` state is not touched; resume state preserves it.

**Exit.** Subsequent deltas carry `Flags.AltScreen=0`. Main-screen `ViewWindow` state resumes exactly where it was. Matches existing texelterm behavior — if the user was scrolled back before running `vim`, they are still scrolled back when `vim` exits.

**Selection on alt-screen.** Client-side, screen-local. `MsgCaptureSelection.coordSpace` discriminator routes to the alt-screen reader path on the server:

```
coordSpace=main → anchor/head are (globalIdx, col), server reads sparse store
coordSpace=alt  → anchor/head are (screenRow, col), server reads altBuffer
```

**Mid-drag alt-screen transition.** Client cancels any in-flight selection on entry or exit. The coordinate space changes out from under the user; any preservation attempt is wrong-byte territory. Matches universal terminal behavior.

**Mode resolution on alt-screen.** `word` / `line` / `paragraph` supported against `altBuffer`. `prompt` / `prompt-output` / `logical-line` return `unsupported`; client greys out the corresponding binding while in alt-screen.

### Sub-problem 4 — Protocol API

**New messages.**

Client → Server:
```
MsgViewportUpdate {
    paneID,
    altScreen       bool,
    viewTopIdx      int64,    // ignored if altScreen
    viewBottomIdx   int64,    // ignored if altScreen or autoFollow
    wrapSegmentIdx  uint16,
    rows, cols      uint16,
    autoFollow      bool,
}

MsgFetchRange {
    requestID     uint32,
    paneID        [16]byte,
    loIdx         int64,
    hiIdx         int64,      // exclusive
    asOfRevision  uint32,
}

MsgResolveBoundary { ... } // as in Sub-problem 1
MsgCaptureSelection { ... } // as in Sub-problem 1
```

Server → Client:
```
MsgFetchRangeResponse {
    requestID   uint32,
    paneID      [16]byte,
    revision    uint32,
    rows        []LogicalRow,   // one per globalIdx in [loIdx, hiIdx)
    flags       uint8,          // {AltScreenActive, BelowRetention, Empty}
}

MsgCaptureResult { ... }
```

**Extended messages.**

```go
type BufferDelta struct {
    PaneID    [16]byte
    Revision  uint32        // existing (per-pane)
    Flags     BufferDeltaFlags // NEW bit: AltScreen
    RowBase   int64         // NEW: main-screen globalIdx of row offset 0;
                            // zero and ignored when Flags.AltScreen=1
    Styles    []StyleEntry
    Rows      []RowDelta    // Row uint16 is now an offset from RowBase (main)
                            // or a screen-row index (alt)
}
```

**Note on monotonic sequencing.** The protocol frame header already carries a session-global monotonic `Sequence uint64` (`protocol.Header.Sequence`, filled by `Session.EnqueueDiff`). That field already satisfies the audit's "all BufferDelta carry monotonic seq" requirement — we do not add a second sequence to the payload. Per-pane `Revision` lives on the payload and drives FetchRange coherence separately (see below).

```go
type ResumeRequest struct {
    SessionID     SessionID
    LastSequence  uint64          // existing
    PaneViewports []PaneViewportState  // NEW
}
```

`TreeSnapshot` is unchanged in structure; its per-pane row payload is now the viewport-clipped slice rather than the full pane.

**Sequencing model.**

Two separate counters, each doing one job:

- `Header.Sequence uint64` (session-global, on every frame — already shipped): drives wire ordering and resume's `LastSequence`. Monotonically increasing across the session via `Session.nextSequence`.
- `BufferDelta.Revision uint32` (per-pane, already on the delta payload): drives `FetchRange` coherence — see below.

**FetchRange coherence rule.**

Invariant: by the time the client applies a `FetchRangeResponse` for revision R, it has already applied every `BufferDelta` with `Revision ≤ R` that it was going to apply. Achieved via:

1. Connection is FIFO. Server emits pending deltas for the pane before the fetch response.
2. Response stamps `revision = pane.Revision at read time`.
3. Client discard rule on receive: `if response.revision < pane.localRevision: discard; else: apply rows, set pane.localRevision = max(pane.localRevision, response.revision)`.

This handles the out-of-order case where an in-flight delta at `R+1` arrives after a fetch response at `R`. No cross-pane coordination required.

**Per-client viewport tracking (server).**

Each `Session` maintains:
```go
map[PaneID] ClientViewport {
    ViewTopIdx, ViewBottomIdx int64
    AltScreen, AutoFollow     bool
    ViewportRows, ViewportCols uint16
}
```

Overscan is derived from `ViewportRows` (1× top, 1× bottom) rather than stored; see "Overscan" below.

Publisher emit path, per pane update at `globalIdx X`:
```
for each client session with this pane subscribed:
    vp := session.viewports[paneID]
    if vp.AltScreen:
        emit full alt delta with Flags.AltScreen=1
        continue
    overscan := int64(vp.ViewportRows)
    liveTop := Store.Max() - int64(vp.ViewportRows)
    live    := Store.Max()
    lo := (if vp.AutoFollow then liveTop else vp.ViewTopIdx) - overscan
    hi := (if vp.AutoFollow then live    else vp.ViewBottomIdx) + overscan
    if X ∈ [lo, hi]:
        emit delta row with RowBase set for this client
    else:
        drop
```

Fan-out shape is unchanged — deltas are already per-session. This just clips what each session sees.

**Overscan.**

Fixed **1× viewport top and bottom**. With a 24-row viewport, the resident range is 72 rows (24 visible + 24 above + 24 below). Configurable via theme only when measurement shows need.

**FetchRange flow control.**

- Client coalesces viewport changes within one animation frame into a single `MsgViewportUpdate`.
- Client maintains **at most one in-flight `MsgFetchRange` per pane**. If a new request supersedes an outstanding one, client waits for the response and immediately sends the latest-needed range.
- Server-side: no explicit debounce. Requests are cheap reads against the sparse store (synchronous cold-page load if needed).

**Missed-deadline handling.**

When a visible row's `globalIdx` is not yet in the client cache and no fetch has resolved within ~50ms of the viewport update:

- Row renders as plain empty cells (normal bg, no inline marker).
- Statusbar app (`apps/statusbar`) subscribes to a "scrollback fetch pending" signal from the pane publisher and surfaces the loading indicator at the bottom of the screen.
- Signal clears when all outstanding fetches for visible rows resolve.

No inline "loading" text — keeps the visible content area calm during scroll-jumps. Uses a surface that is currently under-utilized.

**Client-side cache.**

`BufferCache` is replaced with a per-pane sparse structure:

```go
type PaneCache struct {
    altScreen bool
    main      map[int64]LogicalRow   // keyed by globalIdx
    alt       [][]Cell                // screen-sized 2D (unchanged semantics)
    revision  uint32
}
```

- Entries populated by deltas (`RowBase + offset` → globalIdx) and `FetchRangeResponse` rows.
- Trailing-edge eviction outside `[viewTop - overscan, viewBottom + overscan]`, with a small hysteresis band to prevent thrash during micro-scrolls.
- Render path asks the cache for the globalIdx of each visible row; missing rows render empty (see missed-deadline above).

**AutoFollow data path.**

- `AutoFollow=true`: client does not send a `ViewportUpdate` on every new live-edge row. Server derives `viewBottom = Store.Max()` dynamically for clipping. Client's `ViewWindow` continues tailing normally.
- `AutoFollow=false`: client's `ViewportUpdate` carries explicit `ViewBottomIdx`. Server uses it literally until the next update.
- Transitions (user scrolls away → `AutoFollow=false`; user hits bottom → `AutoFollow=true`) each produce exactly one `MsgViewportUpdate`.

**Delta queue retention while offline.**

Unattached-session delta queues shrink naturally under clipping: at most `overscan_total` rows worth of deltas matter for any given client viewport. On reconnect, we rely on the viewport-clipped `TreeSnapshot` plus `FetchRange` rather than replaying a long backlog of deltas.

### Statusbar integration

`apps/statusbar` gains a subscription to a server-side "scrollback fetch pending" event. Event is emitted when the pane publisher notes a client is waiting on rows for >50ms; cleared when all outstanding visible-row fetches resolve. Statusbar renders an unobtrusive indicator (spinner or text) while pending. No additional protocol on the wire — statusbar reads from the server-internal `EventDispatcher`.

### Selection-state regression (out of scope)

User observed that cross-scrollback selection, which they remember working previously, does not work in the current build. The exploration confirms selection sources strictly from the visible pane's `BufferCache`. Tracked as a separate investigation; this spec does not block on root-causing it, because the new design supersedes the client-side path entirely (selection coords migrate to `(globalIdx, col)` resolved server-side).

## Testing

### Unit tests

- `sparse.ViewWindow` → produce `PaneViewportState` round-trip.
- Server viewport clipper: emits rows only inside window + overscan, drops outside.
- `BufferDelta` encode/decode with `Sequence` + `RowBase` + `Flags.AltScreen`.
- `FetchRange` coherence: stale response is discarded; in-order response applies.
- Missing-anchor resume policy: snap-to-oldest + autoFollow=false.
- Alt-screen entry/exit preserves main-screen `ViewWindow`.
- Mid-drag alt-screen transition cancels selection.
- Mode resolution `ResolveBoundary`: word / line / paragraph / prompt against sparse store; prompt modes return `unsupported` in alt-screen.
- `CaptureSelection`: soft-join wrapped rows, CRLF on hard break, trailing-blank trim; cold-page selection loads correctly.

### Integration tests

- Viewport-aware resume: disconnect scrolled back → reconnect at same visual position.
- Cross-restart best-effort resume: server restart with persisted `PaneViewportState`; client lands at saved position.
- FetchRange flow: fast scroll past overscan → row arrives before 50ms deadline in happy path.
- AutoFollow transitions: live-edge follow → user scrolls back → autoFollow off → user hits bottom → autoFollow on.
- Alt-screen opt-out: `MsgFetchRange` returns `AltScreenActive` while `vim` is running; main-screen scroll state survives.

### Tests to rewrite (budget)

Existing tests that assert over the full-pane buffer:
- `internal/runtime/server/desktop_publisher_test.go` — rewrite to expect clipped deltas under a configured viewport.
- `internal/runtime/server/session_test.go` — resume tests assume full-pane snapshots; rewrite against viewport-clipped snapshots.
- `internal/runtime/client/*_test.go` — any `BufferCache` assertion against row-index addressing.

Rough estimate: ~15–20 tests touched across server and client runtime packages. Must be confirmed during implementation planning.

## Migration and rollout

- Protocol version bumps by one. Both sides are updated in lockstep in this PR — clipping has no meaningful fallback mode, so there is no mixed-version compatibility window.
- Old persisted scrollback loads normally; it simply lacks `PaneViewportState`, which is fine (treated as a fresh-connect resume for that pane).
- The `texelation` supervisor restarts the server daemon on version mismatch, so mixed versions do not co-exist in the wild for long.

## Open questions for implementation

These are design-settled but need a concrete call during the plan:

- Exact wire encoding for `LogicalRow` in `FetchRangeResponse` (reuse `RowDelta` run-length style, or a distinct row format).
- Hysteresis constant for cache eviction (start with overscan × 1.5).
- Missed-deadline threshold (50ms is a starting guess; tune on first profiling).
- Whether `PaneViewportState` is persisted per-session (simpler) or per-client-identity (survives multiple clients on the same session).

## Sequencing within the PR

Implementation order (refined by `writing-plans`):

1. Extend `BufferDelta` wire format (`RowBase`, `Flags.AltScreen`); no behavior change yet — `RowBase = 0`, alt-screen flag unset. Frame-header `Sequence` is already monotonic and needs no change.
2. Add `MsgViewportUpdate`; server tracks per-client viewport but does not yet clip.
3. Add `MsgFetchRange` / `MsgFetchRangeResponse`; server can serve ranges.
4. Flip publisher to clip-on-emit; client sparse per-pane cache lands; eviction behavior tested.
5. Extend `MsgResumeRequest` with `PaneViewportState`; server honors on first paint.
6. Add `MsgResolveBoundary`, `MsgCaptureSelection`, `MsgCaptureResult`; migrate client selection to globalIdx coords.
7. Persist `PaneViewportState` alongside scrollback for cross-restart resume.
8. Statusbar "scrollback fetch pending" integration.

Each step has an individually-mergeable boundary for local testing, though the public-facing behavior change lands at step 4.
