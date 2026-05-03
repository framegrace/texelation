# Issue #199 Plan F.1 — Stored-Session Recovery Picker — Design

**Date:** 2026-05-03
**Status:** Approved for plan
**Issue:** [#199](https://github.com/framegrace/texelation/issues/199) (Plan F.1 of the rollout)

## Problem

A texelation user with lost client state — wiped state file, fresh machine,
reinstall, or first launch with persisted server-side sessions — currently has
no way to discover and rejoin sessions the daemon has on disk. Plans D and D2
already persist session metadata (`LastActive`, `Label`, `PaneCount`,
`FirstPaneTitle`, `Pinned`) and `StoredSession` JSON sidecars. Plan F.1 adds
the discovery + recovery UX.

This spec covers F.1 only — recovery of *stored* (fossilized) sessions. F.2
(multi-live-sessions) and F.3 (templates) will be filed as follow-up issues
after F.1 lands; F.1's UI scaffolding is built so F.2 can populate the empty
"Live" tab without restructuring.

## Session-state taxonomy (F.1 / F.2 framing)

| State | Meaning | Reachable in F.1? |
|---|---|---|
| **Live-Attached** | In memory, ≥1 client attached | Through normal connect; not in picker |
| **Live-Detached** | In memory, no clients attached | Through normal connect (sessionID match); F.2 surfaces in picker |
| **Stored** | Fossilized to disk, not in memory | F.1 picker — primary target |
| **Template** | Named, persistent, multi-instance | F.3 — out of scope |

F.1's picker shows a `Live` tab (always empty in F.1, populated by F.2) and
a `Stored` tab (the F.1 target).

## Out of scope

- **F.2: multi-live-sessions** — listing live detached sessions, attaching to
  one from a second client, etc. The wire format reserves `LiveSummary` so
  F.2 doesn't require another protocol bump.
- **F.3: templates** — user-named persistent layouts that spawn fresh
  instances on pick.
- **In-app rename UI** — F.1 only allows renames from the picker. A texel
  command palette / status-bar action for renaming a live session is a later
  initiative.
- **Periodic thumbnail capture** — F.1 captures lifecycle-only (graceful
  shutdown + last client disconnect). Continuous or rate-limited capture is
  rejected for F.1 (cost vs. value: staleness is acceptable for a hint).
- **Cold-start hydration performance** ([#230](https://github.com/framegrace/texelation/issues/230)) — orthogonal investigation.
- **Compatibility with v4 servers** — beyond a clear "this server is too old"
  message and fall-through to the existing connect path. We don't backport.

## Trigger model

Picker activation logic in `cmd/texelation/boot`:

```
if clientStateFile exists AND has valid sessionID:
    skip picker, take normal connect path
else if --recover flag set:
    always show picker (even if 0 stored — just shows "no sessions")
else:
    request MsgListSessions
    if response has ≥1 stored OR ≥1 live:
        show picker
    else:
        skip picker, take fresh-session path
```

This avoids interrupting first-time users (no flag, no state, no stored
sessions = silent fast-path) while making recovery automatic when the disk
already has sessions.

## Architecture

```
┌────────────────── client (cmd/texelation) ──────────────────┐
│                                                             │
│   boot.Run(opts)                                            │
│      │                                                      │
│      ├── normal path (existing) ────────► splash ───► run   │
│      │                                                      │
│      └── picker path (NEW) ──┐                              │
│                              ▼                              │
│                       picker UI (tcell)                     │
│                       ├ Live tab (empty F.1)                │
│                       └ Stored tab (cards)                  │
│                              │                              │
│                              ▼                              │
│                  recover/new/rename/delete ─► protocol      │
└─────────────────────────────────┬───────────────────────────┘
                                  │ unix socket
┌─────────────────────────────────▼───────────────────────────┐
│                      server (texel-server)                  │
│                                                             │
│   connection.go handler:                                    │
│      MsgListSessions       → manager.list()                 │
│      MsgRecoverSession     → manager.hydrate() ──► normal   │
│                                                  connect    │
│      MsgRenameSession      → updates StoredSession JSON     │
│      MsgDeleteSession      → unlinks JSON + PNG sidecar     │
│      MsgFetchThumbnail     → reads <id>.png                 │
│                                                             │
│   thumbnail.go (NEW):                                       │
│      captureThumbnail(sess) — on graceful shutdown          │
│                              + on last client disconnect    │
└─────────────────────────────────────────────────────────────┘
```

## Protocol (v4 → v5)

### Wire types

```go
// SessionSummary describes a stored session on disk.
type SessionSummary struct {
    SessionID      [16]byte
    Label          string            // empty -> rendered as "Untitled"
    LastActive     int64             // unix seconds
    PaneCount      int
    FirstPaneTitle string
    Pinned         bool
    HasThumbnail   bool              // <sessionID>.png exists
    LayoutCapture  *TreeNodeCapture  // for ASCII fallback render
}

// LiveSummary describes a hydrated session.
// Empty in F.1; reserved for F.2.
type LiveSummary struct {
    SessionID       [16]byte
    Label           string
    AttachedClients int
    PaneCount       int
    LastInputAt     int64
}
```

### New messages

**`MsgListSessions`** (client → server):
```go
type ListSessionsRequest struct {
    // Reserved for F.2 filters.
}
```

**`MsgListSessionsResponse`** (server → client):
```go
type ListSessionsResponse struct {
    Live   []LiveSummary    // empty in F.1
    Stored []SessionSummary // sorted: pinned desc, then LastActive desc
}
```

**`MsgRecoverSession`** (client → server):
```go
type RecoverSessionRequest struct {
    SessionID [16]byte
    NewLabel  string  // optional inline rename
}
```
Response: ordinary `MsgConnectAccept` + `MsgTreeSnapshot` flow — picker hands
off to the existing connect path with no special-casing downstream.

**`MsgRenameSession`** (client → server):
```go
type RenameSessionRequest struct {
    SessionID [16]byte
    NewLabel  string
}
type RenameSessionResponse struct {
    OK    bool
    Error string
}
```

**`MsgDeleteSession`** (client → server):
```go
type DeleteSessionRequest struct {
    SessionID [16]byte
}
type DeleteSessionResponse struct {
    OK    bool
    Error string
}
```
Refuses with error if session is currently live (only relevant once F.2 lands;
F.1 has no live sessions through the picker).

**`MsgFetchThumbnail`** (client → server, lazy):
```go
type FetchThumbnailRequest struct {
    SessionID [16]byte
}
type FetchThumbnailResponse struct {
    OK    bool
    Error string
    PNG   []byte
}
```

### Version handshake

- v4 → v5 because messages are added.
- Old (v4) clients ignore the new types entirely; servers continue to serve
  v4 protocol on negotiated v4 connections.
- New (v5) clients on v4 servers: detect via `MsgWelcome.Version`, render a
  one-line "this server doesn't support session listing" message in the
  picker, and fall through to the existing connect path.

## Server-side implementation

### List handler

```go
// internal/runtime/server/connection_list_sessions.go (new)

func (c *connection) handleListSessions(req *protocol.ListSessionsRequest) {
    stored := c.manager.StoredSummaries()
    live := c.manager.LiveSummaries() // returns nil in F.1

    sort.Slice(stored, func(i, j int) bool {
        if stored[i].Pinned != stored[j].Pinned {
            return stored[i].Pinned
        }
        return stored[i].LastActive > stored[j].LastActive
    })

    c.send(&protocol.ListSessionsResponse{Live: live, Stored: stored})
}
```

Reads from the existing `manager.persistedSessions` map populated at boot
scan; no disk IO at request time. `LayoutCapture` is reconstructed from the
StoredSession JSON's tree on first call and cached on the in-memory record.

### Recover handler

```go
// internal/runtime/server/connection_recover_session.go (new)

func (c *connection) handleRecoverSession(req *protocol.RecoverSessionRequest) {
    if req.NewLabel != "" {
        if err := c.manager.RenameStored(req.SessionID, req.NewLabel); err != nil {
            c.sendError(err)
            return
        }
    }

    sess, err := c.manager.HydrateStored(req.SessionID)
    if err != nil {
        c.sendError(err)
        return
    }

    // Reuses existing connect path.
    c.attachToSession(sess)
    c.sendConnectAccept(sess)
    c.sendTreeSnapshot(sess)
}
```

`HydrateStored` is the existing rehydrate path used by `MsgResumeRequest`,
extracted into a function the picker can also call.

### Rename / delete handlers

Both update `manager.persistedSessions` under the existing manager mutex,
write through to disk via the existing snapshot store helpers, and remove
sidecar PNG on delete.

```go
// internal/runtime/server/manager_session_files.go (new)

func (m *Manager) DeleteStored(id [16]byte) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if _, live := m.live[id]; live {
        return fmt.Errorf("session is live; detach all clients first")
    }
    if _, ok := m.persistedSessions[id]; !ok {
        return fmt.Errorf("session not found")
    }
    delete(m.persistedSessions, id)
    _ = os.Remove(filepath.Join(m.sessionDir, idString(id)+".png"))
    return os.Remove(filepath.Join(m.sessionDir, idString(id)+".json"))
}
```

### Thumbnail capture

Two trigger points wired into existing manager hooks:

```go
// internal/runtime/server/thumbnail.go (new)

func (m *Manager) captureThumbnail(sess *Session) {
    if !sess.HasContent() {
        return // empty workspace, skip
    }
    grid := sess.workspace.RenderToGrid()
    img, err := textrender.Render(grid, textrender.DefaultOptions())
    if err != nil {
        log.Printf("thumbnail: render: %v", err)
        return
    }
    img = downscaleAspectFit(img, 480, 270)

    path := filepath.Join(m.sessionDir, idString(sess.ID)+".png")
    if err := writePNGAtomic(path, img); err != nil {
        log.Printf("thumbnail: write: %v", err)
    }
}
```

**Trigger 1 — graceful shutdown:** `manager.Shutdown()` walks live sessions
before flushing the snapshot store and calls `captureThumbnail` for each.

**Trigger 2 — last client disconnect:** `manager.detachClient()` checks
`attachedClients == 0` after detach and fires `captureThumbnail` async (fire-
and-forget — failure logs and is non-fatal).

`writePNGAtomic` writes to `path + ".tmp"`, `Sync`s, and `Rename`s, so a
crash mid-write doesn't leave a half-PNG.

### Boot scan extension

`manager.scanSessionsDir` already populates `persistedSessions` from `*.json`
files. Add a `Stat` for `<id>.png` to set `HasThumbnail` on the in-memory
record. Orphaned PNGs (no matching JSON) are removed during the scan.

### Fetch thumbnail handler

```go
func (c *connection) handleFetchThumbnail(req *protocol.FetchThumbnailRequest) {
    path := filepath.Join(c.manager.sessionDir, idString(req.SessionID)+".png")
    data, err := os.ReadFile(path)
    if err != nil {
        c.send(&protocol.FetchThumbnailResponse{OK: false, Error: err.Error()})
        return
    }
    c.send(&protocol.FetchThumbnailResponse{OK: true, PNG: data})
}
```

## Client-side picker UI

### Package layout

```
cmd/texelation/boot/
  picker.go             — Picker struct, event loop, dispatch
  picker_render.go      — Layout + drawing
  picker_thumbnail.go   — Kitty + ASCII fallback rendering
  picker_ascii.go       — TreeNodeCapture → box-drawing chars
  picker_input.go       — Key handling, navigation
  picker_test.go        — Render, navigation, fallback tests
  picker_ascii_test.go  — Pure-function ASCII algorithm tests
```

### State machine

```go
type Picker struct {
    screen      tcell.Screen
    conn        protocol.Conn
    hasGraphics bool

    response    *protocol.ListSessionsResponse
    activeTab   tab // tabLive | tabStored
    selectedIdx int

    mode       mode // modeBrowse | modeRename | modeDeleteConfirm
    renameBuf  []rune
    thumbCache map[[16]byte][]byte // PNG bytes by session ID
    pending    map[[16]byte]bool   // outstanding fetches
}
```

### Layout (single-screen)

```
┌─ texelation ────────────────────────────────────────────────────┐
│                                                                 │
│  [ Live (0) ]  [ Stored (3) ]                                   │
│                                                                 │
│  ╔═════════════════╗  Label:    work-laptop                     │
│  ║  thumbnail or   ║  Active:   2 hours ago                     │
│  ║  ASCII layout   ║  Panes:    4                               │
│  ║                 ║  Title:    nvim · texelation/cmd/...       │
│  ╚═════════════════╝  Pinned:   ★                               │
│                                                                 │
│  ╔═════════════════╗  Label:    debug-cluster                   │
│  ║                 ║  Active:   yesterday                       │
│  ║                 ║  Panes:    2                               │
│  ║                 ║  Title:    bash                            │
│  ╚═════════════════╝                                            │
│                                                                 │
│  [Enter] recover   [n] new   [r] rename   [d] delete   [q] quit │
└─────────────────────────────────────────────────────────────────┘
```

Card thumbnail rect: ~22×8 cells, fixed. Cards stack vertically; viewport
scrolls when overflowing.

### Hotkeys (browse mode)

| Key | Action |
|---|---|
| ↑ / k | Move selection up |
| ↓ / j | Move selection down |
| Tab | Switch tab |
| Enter | Recover selected |
| n | Start fresh session |
| r | Enter rename mode |
| d | Enter delete-confirm mode |
| q / Esc | Exit picker (defaults to fresh) |

### Inline rename

`r` overlays a single-line text editor on the selected card's metadata
column:
- Label is preselected; typing replaces.
- Enter sends `MsgRenameSession`; on OK, updates local card label.
- Esc cancels.

### Delete confirmation

`d` overlays `Delete '<label>'? [y/N]` prompt at the bottom of the card.
Pinned cards add `(this session is pinned)` before the prompt.

### Thumbnail rendering

```go
// picker_thumbnail.go

func (p *Picker) renderThumbnail(rect Rect, summary protocol.SessionSummary) {
    cached, ok := p.thumbCache[summary.SessionID]
    if p.hasGraphics && summary.HasThumbnail && ok {
        renderKittyThumbnail(p.screen, rect, cached)
        return
    }
    if p.hasGraphics && summary.HasThumbnail && !p.pending[summary.SessionID] {
        p.pending[summary.SessionID] = true
        go p.fetchThumbnail(summary.SessionID)
    }
    renderASCIILayout(p.screen, rect, summary.LayoutCapture)
}
```

ASCII fallback always renders first; the Kitty image upgrades the card on
fetch completion.

### ASCII layout algorithm

```go
// picker_ascii.go

func renderASCIILayout(s tcell.Screen, r Rect, root *protocol.TreeNodeCapture) {
    if r.W < 8 || r.H < 4 || root == nil {
        drawSingleBox(s, r)
        return
    }
    drawNode(s, r, root)
}

func drawNode(s tcell.Screen, r Rect, n *protocol.TreeNodeCapture) {
    if n.IsLeaf() {
        drawSingleBox(s, r)
        if n.Focused {
            highlightBorder(s, r)
        }
        return
    }
    a, b := splitRect(r, n.SplitRatio, n.IsHSplit)
    drawNode(s, a, n.LeftOrTop)
    drawNode(s, b, n.RightOrBottom)
    drawDivider(s, r, n.SplitRatio, n.IsHSplit)
}
```

Box-drawing chars: `┌─┐│└─┘├┤┬┴┼─│`. Round-off errors absorbed by clamping
the right/bottom child to fill remaining cells. Sub-minimum-size rects
collapse to a single bordered box.

### Handoff to splash

On recover/new selection:
1. Picker tears down its UI (clears the screen region, releases input handler).
2. Calls into the existing splash entry point with the same `tcell.Screen`.
3. Splash renders boot progress (existing flow); on `OnStatus(StageReady)`
   the runtime client takes over.

No screen flicker — tcell is held throughout.

## Testing

### Server-side unit tests (`internal/runtime/server/`)

1. `TestListSessions_EmptyOnFreshDaemon` — fresh daemon, no JSONs, empty response.
2. `TestListSessions_StoredOnly` — 3 fixtures, ordered pinned-then-LastActive desc.
3. `TestListSessions_HasThumbnailFlag` — 2 JSONs, 1 PNG → flag differentiates.
4. `TestListSessions_LayoutCaptureCached` — second call doesn't re-parse JSONs.
5. `TestRecoverSession_HappyPath` — pick stored, expect ConnectAccept + TreeSnapshot.
6. `TestRecoverSession_UnknownID` — random UUID → error response, no crash.
7. `TestRecoverSession_WithRename` — `NewLabel` updates JSON; subsequent list reflects.
8. `TestDeleteStored_RemovesBothFiles` — JSON + PNG both gone; map entry removed.
9. `TestDeleteStored_PNGMissing` — JSON-only delete still succeeds.
10. `TestDeleteStored_RefusesLive` — live session present (manual setup) → error.
11. `TestRenameStored_PersistsAcrossReload` — rename, re-scan dir, label survives.
12. `TestCaptureThumbnail_OnLastDisconnect` — attach + detach → PNG appears.
13. `TestCaptureThumbnail_OnGracefulShutdown` — manager.Shutdown() captures live sessions.
14. `TestCaptureThumbnail_AtomicWrite` — write failure → no `<id>.png` left, JSON intact.
15. `TestBootScan_OrphanPNGRemoved` — PNG without JSON cleaned during scan.
16. `TestFetchThumbnail_HappyPath` — PNG bytes returned correctly.
17. `TestFetchThumbnail_MissingFile` — error response, no crash.

### Client-side picker tests (`cmd/texelation/boot/`)

18. `TestPicker_RendersStoredCards` — 3 summaries → 3 card regions painted.
19. `TestPicker_NavigationKeys` — j/k/arrows move, Enter dispatches recover, n dispatches new.
20. `TestPicker_TabSwitch` — Tab toggles between Live and Stored tabs.
21. `TestPicker_LiveTabEmptyState` — Live tab shows "no live sessions" when empty (F.1 default).
22. `TestPicker_RenameInline` — r enters edit mode, Enter commits via MsgRenameSession.
23. `TestPicker_RenameEsc_Cancels` — Esc reverts to original label.
24. `TestPicker_DeleteConfirmation_Y` — d → y → MsgDeleteSession dispatched, card removed.
25. `TestPicker_DeleteConfirmation_N` — d → n → no message, card retained.
26. `TestPicker_FallsBackToASCII_OnNoGraphics` — graphics=none → no Kitty escapes, only box chars.
27. `TestPicker_FetchThumbnailUpgradesCard` — Kitty + async response → card re-renders with image.
28. `TestPicker_TooSmallScreen_CollapsesToList` — sub-40×15 → name-only list mode.

### ASCII layout pure-function tests (`cmd/texelation/boot/`)

29. `TestRenderASCIILayout_SinglePane` — leaf-only → single bordered box.
30. `TestRenderASCIILayout_HorizontalSplit` — h-split → `─` divider mid-rect.
31. `TestRenderASCIILayout_VerticalSplit` — v-split → `│` divider mid-rect.
32. `TestRenderASCIILayout_NestedSplits` — 4 leaves → correct T-junctions.
33. `TestRenderASCIILayout_FocusHighlight` — `Focused: true` leaf gets highlight color.
34. `TestRenderASCIILayout_RoundOff` — 7-cell width × 0.33 ratio → no gaps.
35. `TestRenderASCIILayout_BelowMinSize` — 6×3 rect → falls back to single box.

### Integration tests (`internal/runtime/server/`)

36. `TestPickerEndToEnd_RecoverFlow` — memconn loop: list → recover → ConnectAccept → buffers.
37. `TestPickerEndToEnd_RenameThenRecover` — rename via picker, verify label persisted post-recover.

## Risks

- **Stale thumbnails.** External edits to a stored session's tree won't
  reflect in the thumbnail until next live capture. Acceptable — thumbnail
  is a hint; metadata is authoritative.
- **Sidecar PNG bloat.** Hundreds of stored sessions × ~50 KB → tens of MB.
  Cleanup is wired through the existing StoredSession retention path; one
  line addition.
- **TreeNodeCapture wire size in list response.** Typical 1-5 panes ≈
  ~100 bytes per summary — negligible. Won't bother with a separate fetch.
- **Tiny terminal rendering.** Sub-40×15 cells auto-collapse to name-only
  list mode; no thumbnails, single-line cards.
- **Concurrent rename + delete races.** Two clients with picker open: one
  renames, the other deletes. Server serializes via manager mutex; second
  op gets a clear error and the picker re-fetches its list.
- **Shutdown delay from thumbnail capture.** 50 stored sessions × ~100 ms
  render = 5 s. Realistic case is 1-3 live sessions; F.1 accepts the worst-
  case delay. F.2 adds dirty-since-last-capture tracking if needed.
- **v4 server fallback UX.** Picker on a v5 client connecting to a v4 server
  shows a clear one-liner and falls through. Not silent, not crashing.

## Forward-compat for F.2 / F.3

- `LiveSummary` slice already on the wire — F.2 just populates it.
- `Live` tab renders empty in F.1 — F.2 just changes its content source.
- `MsgRecoverSession` semantics naturally extend: F.2 can branch on "is the
  picked session live?" and skip rehydration when it's already in memory.
- `MsgFetchThumbnail` works for live sessions too once F.2 wires capture
  on transitions from Live-Detached → Stored (planned daemon-shutdown path).
- F.3 (templates) adds a `Templates` tab and a `MsgInstantiateTemplate`
  message; SessionSummary picks up an `IsTemplate` flag.
