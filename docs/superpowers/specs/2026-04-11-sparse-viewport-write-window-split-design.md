# Sparse Viewport + Write-Window Split

**Status**: Design
**Date**: 2026-04-11
**Supersedes**: `docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md` (the "Three-Level Architecture" section), all TUI-detection / pendingRollback / push-on-clear machinery currently on branch `fix/no-scrollback-from-partial-scroll-regions`.

## Background

Texelterm's scrollback/viewport model currently conflates three distinct concepts into one variable, `liveEdgeBase`:

1. The *top of the live write area* (where the TUI's cursor-relative writes land).
2. The *top of the user's view* (what's rendered to the screen).
3. The *boundary between viewport and scrollback* (which rows become history).

This conflation drives a large and ever-growing family of bugs around SIGWINCH, TUI redraws (claude, codex, Ink-based React apps), ESC[2J, and scroll regions. The current branch has 18+ consecutive `fix:` commits attempting to patch the symptoms: `suppressNextScrollbackPush`, `pendingRollbackActive`, `hasPartialScrollRegion`, `restoredFromDisk`, and assorted TUI-vs-shell detection heuristics. Every heuristic has found a corner case.

The root cause is that the model tries to decide, from structural hints (is this ESC[2J? is there a partial DECSTBM? is the app in alt screen?), *when to save viewport content to scrollback*. That decision is fundamentally unsound because TUIs and shells issue the same escape sequences for different intents, and the resize path has to guess intent from after-the-fact signals.

**The foundation already exists.** PR #167 ("Sparse PageStore") landed a sparse, globalIdx-keyed on-disk store that supports gaps. This spec extends that idea to the in-memory layer and to the write/view semantics, eliminating the dense ring buffer and the push-on-clear model together.

## Goals

- Delete all TUI-vs-shell detection from the scrollback/resize path.
- Delete all "push to scrollback" events (ESC[2J, partial scroll region, SetMargins rollback, etc.).
- Make scrolling genuinely independent of where any app is writing — a user scrolled back 100 lines reviewing history sees stable content no matter what a live TUI is doing.
- Make resize deterministic and eliminate the "claude jumps to top + text-content replicas in scrollback" bug class. Bounded cursor-anchor smearing of a TUI's topmost row during a shrink drag is acceptable; unbounded replication of the whole viewport is not.
- Align in-memory and on-disk storage on the same sparse globalIdx addressing, so reload is a direct repopulation rather than a reconstruction from dense snapshots.
- Keep alt-screen handling exactly as it is today; the redesign targets the main-screen path only.

## Non-goals

- Alt-screen reflow or storage semantics. The dense alt grid stays; only the main-screen path is being rewritten.
- Scroll region semantics on alt screen (unchanged).
- Disk format changes beyond whatever PR #167 already introduced. The on-disk `PageStore` is the long-lived foundation.
- Performance tuning. The first cut aims for correctness and clean invariants; profiling comes later.

## The semantic model

### State

```
contentEnd   int64   // high-water mark: highest globalIdx ever written. Only advances.
cursor       struct { globalIdx int64; col int }
writeTop     int64   // top of the TUI-addressable write window
viewBottom   int64   // bottom of the user-visible view window
autoFollow   bool    // whether view tracks write window
width        int
height       int
```

Derived (not stored):

- `writeBottom = writeTop + height - 1`
- `viewTop     = viewBottom - height + 1`

These four stored values — `contentEnd`, `cursor`, `writeTop`, `viewBottom` — plus `autoFollow` replace `liveEdgeBase`, `pendingRollbackActive`, `pendingRollbackPreEdge`, `pendingRollbackPreGlobalEnd`, `pendingRollbackSavedLines`, `pendingRollbackSavedBase`, `suppressNextScrollbackPush`, `hasPartialScrollRegion`, and `restoredFromDisk` entirely.

### Rule 1: storage

A single sparse globalIdx-keyed cell store. No distinction between "viewport cells" and "scrollback cells" at the storage layer. A cell at globalIdx X is just a cell at globalIdx X. Writes land at `(cursor.globalIdx, col)`; the cursor can sit anywhere, inside or outside any window. Reads return blank for unwritten globalIdxs.

This mirrors the on-disk `PageStore` from PR #167. The same addressing and the same page layout are used in memory and on disk.

### Rule 2: what the TUI sees

The TUI is told `(width, height)` via TIOCSWINSZ. `ESC[row;colH` resolves to `(writeTop + row - 1, col - 1)`. The TUI's addressable area is exactly `[writeTop, writeBottom]`. The TUI has no idea where the user is looking, and cannot observe or affect `viewBottom` / `autoFollow`.

### Rule 3: what the user sees

The client renders the range `[viewTop, viewBottom]` read from the sparse store. Unwritten globalIdxs render as blank cells. That is the entire rendering contract.

### Rule 4: auto-follow

- When `autoFollow == true`: every event that moves `writeBottom` — a write advancing the cursor past the current `writeBottom`, a LF at the bottom advancing `writeTop`, a resize of the write window — also sets `viewBottom := writeBottom`. View and write coincide.
- When `autoFollow == false`: `viewBottom` is frozen wherever the user left it. Writes, scrolls, and write-window resize do not move it. Only explicit scroll commands and view-window resize anchor rules move it.

Transitions:

- User scrolls up (PgUp, wheel, etc.): `autoFollow := false`; `viewBottom -= n` (clamped).
- User scrolls to bottom OR user types a key OR user clicks in the pane: `autoFollow := true`; `viewBottom := writeBottom`.

The "type a key re-engages follow" behavior matches tmux and is familiar to users.

### Rule 5: resize — write window

On SIGWINCH (or equivalent internal resize), the write window responds as follows:

**Shrink** (`newHeight < height`) — cursor-minimum-advance rule:

1. Let `advance = max(0, cursor.globalIdx - (writeTop + newHeight - 1))`.
2. `writeTop += advance`. (If the cursor already fits in the new window, `advance` is 0 and `writeTop` does not move.)
3. `newWriteBottom = writeTop + newHeight - 1`.
4. Cells in `[newWriteBottom + 1, oldWriteBottom]` are eagerly cleared — they were TUI scratch space below the new write window and have no historical meaning.
5. Cells in `[oldWriteTop, writeTop)` (only non-empty if `advance > 0`) become part of history: they remain in the Store, unchanged, now outside the write window from above. This is not a "push" event — the cells never moved. The window moved, and they are now on the scrollback side of the boundary.

**Grow** (`newHeight > height`) — writeBottom-anchor rule:

1. `writeTop -= (newHeight - height)`.
2. If `writeTop < 0`, clamp to 0. In that case `writeBottom` ends up at `newHeight - 1`, advancing past `oldWriteBottom` — the "shallow scrollback" case, where the write window extends into territory beyond `contentEnd`.
3. No cells are cleared. New top rows of the write window (`[newWriteTop, oldWriteTop)`) expose whatever is already stored there (old scrollback, if any). The TUI's SIGWINCH handler will typically clear and redraw, overwriting those rows.

**Why this rule set:**

- **Shell case** (cursor at bottom row): cursor is at `oldWriteBottom`, so `cursor.globalIdx - (writeTop + newHeight - 1) = oldHeight - newHeight`. `writeTop` advances by exactly the shrink delta. Top rows slide into scrollback. The shell prompt stays anchored at the new bottom. This is precisely "scroll up to keep the bottom anchored."
- **Full-screen TUI with cursor near top** (unusual): cursor fits in the new window, `advance = 0`, `writeTop` unchanged. Bottom rows are eagerly cleared. The TUI redraws in place with no pollution.
- **TUI with cursor in the bottom half** (the claude case): cursor forces a partial advance. Top rows of the old TUI become history on each drag step. A 20-step drag from h=40 to h=20 produces on the order of 15 rows of old TUI content in scrollback — O(N), not O(N²). See "Known accumulation trade-off" below.

**Known accumulation trade-off:** When a TUI has its cursor in the bottom half of the viewport (common — input boxes at the bottom), a rapid shrink drag will advance `writeTop` once per step that the cursor would otherwise fall off. Each advance leaves one row of old TUI content in scrollback. The accumulated rows are whichever content lived at the previous `writeTop` at the moment each step happened — typically the top edge of the TUI's UI (banner borders, frame characters). This is a large improvement over the current O(N²) per-drag pollution but is not zero. A follow-up optimization is to debounce SIGWINCH bursts so a drag collapses into a single resize event at steady state; this is out of scope for the initial redesign and can be added later without semantic changes.

### Rule 6: resize — view window

- **If `autoFollow == true`**: after the write window resizes, `viewBottom := writeBottom`. The view snaps to the (possibly moved) write window. The user continues to see whatever the TUI is drawing, at the new size.
- **If `autoFollow == false`**: `viewBottom` is anchored; `height` changes; `viewTop` is derived. The scrolled-back content the user was looking at stays anchored at the bottom of their view. The write window resizes independently in the background and the user does not see it.

### Rule 7: scroll

- `viewBottom` is clamped to `[height - 1, writeBottom]`. The user cannot scroll above the first line of history (globalIdx 0 at the top) and cannot scroll past the bottom of live content (viewing unwritten "future" cells makes no sense).
- Nothing else moves `viewBottom`. Not writes (unless following), not TUI scroll (unless following), not SIGWINCH (unless following).

### Rule 8: alt-screen interaction

Alt-screen is out of scope for this redesign, but the contact surface with the new main-screen model is:

- `VTerm.Parse()` dispatches each character to `altPath` or `mainPath` based on `inAltScreen`, exactly as today.
- Shared terminal-level state (`currentFG`, `currentBG`, attribute flags, charset, tab stops) is read by whichever path is active.
- `parser.Cell` is shared.
- Rendering dispatch is one line: `if inAltScreen { altGrid } else { sparseTerminal.ViewGrid() }`.
- On ESC[?1049h (enter alt): main-screen state (`Store`, `writeTop`, `viewBottom`, `contentEnd`) stays untouched. No save is needed — nothing is writing to it. Alt's cursor is saved/swapped exactly as today.
- On ESC[?1049l (exit alt): alt state is discarded, main-screen state is still where we left it. No restore is needed.
- **SIGWINCH while alt is active**: applied to both screens. Alt reflows its dense grid. Main runs the write-window and view-window anchor rules. The main-screen resize is invisible (nothing rendering from it), but the rules execute identically to the non-alt case. This removes the current "`memoryBufferResize` called during alt screen" bug.

No other coupling exists between the two screens.

## Architecture

### New package: `apps/texelterm/parser/sparse/`

Four new types, each testable in isolation with fast unit tests (no PTY, no real TUI):

#### `sparse.Store`

Globalidx-keyed sparse cell storage. In-memory layout chunked to match the on-disk `PageStore` page size so that flush/reload is a direct copy.

API:
- `Get(globalIdx int64, col int) parser.Cell`
- `Set(globalIdx int64, col int, cell parser.Cell)`
- `GetLine(globalIdx int64) []parser.Cell`
- `SetLine(globalIdx int64, cells []parser.Cell)`
- `ClearRange(lo, hi int64)` — zero out cells in `[lo, hi]`
- `Max() int64` — returns `contentEnd`
- `Width() int`

No viewport concept, no cursor concept, no resize concept. Pure CRUD.

Invariants (enforced by unit tests):
- A cell written at `(X, C)` is returned by `Get(X, C)` until overwritten or cleared.
- `Max()` never decreases.
- `ClearRange(lo, hi)` leaves cells outside `[lo, hi]` untouched.
- `SetLine` overwrites exactly one globalIdx's row; adjacent globalIdxs are not affected.

The concrete data structure (btree, ordered map, chunked segment list, etc.) is an implementation detail for the plan phase. The interface above is what the spec pins.

#### `sparse.WriteWindow`

Owns `writeTop`, `height`, `width`, `cursor`. Writes through to a `Store`.

API:
- `WriteCell(cell parser.Cell)` — writes at cursor, advances cursor column
- `WriteWide(cell parser.Cell)` — writes a 2-column cell
- `Newline()` — CR+LF. LF at bottom advances `writeTop` (classical scroll up)
- `CarriageReturn()` — cursor.col := 0
- `SetCursor(row, col int)` — `cursor.globalIdx := writeTop + row`; `cursor.col := col`
- `CursorRow() int` — derived: `cursor.globalIdx - writeTop`
- `Resize(newWidth, newHeight int)` — applies Rule 5
- `EraseInLine`, `EraseInDisplay`, `InsertLine`, `DeleteLine`, `ScrollUp(n)`, `ScrollDown(n)` — all operating on `[writeTop, writeBottom]` via the Store
- `SetScrollRegion(top, bottom int)` — DECSTBM; region is expressed as rows 0..height-1 relative to `writeTop`
- `WriteTop() int64`, `WriteBottom() int64`, `ContentEnd() int64` (reads from Store)

Invariants:
- `writeTop` advances on `Newline()` at bottom (classical scroll up).
- `writeTop` retreats on `Resize(grow)` (writeBottom-anchor rule, clamped at 0).
- `writeTop` advances on `Resize(shrink)` by exactly `max(0, cursor.globalIdx - (writeTop + newHeight - 1))` — the minimum amount needed to keep the cursor inside the new write window. If the cursor already fits, `writeTop` does not move.
- `writeTop` never decreases except via `Resize(grow)`.
- Every write goes through `Store.Set` or `Store.SetLine`; nothing is buffered outside the Store.

#### `sparse.ViewWindow`

Owns `viewBottom`, `height`, `width`, `autoFollow`. Observes a `WriteWindow`.

API:
- `Resize(newWidth, newHeight int)` — applies Rule 6
- `ScrollUp(n int)` — sets `autoFollow := false`; `viewBottom -= n` (clamped)
- `ScrollDown(n int)`
- `ScrollToBottom()` — `viewBottom := writeBottom`; `autoFollow := true`
- `OnInput()` — called when the user types/clicks; `ScrollToBottom()` equivalent
- `OnWriteBottomChanged(newBottom int64)` — called by `WriteWindow` when it advances; if `autoFollow`, `viewBottom := newBottom`
- `OnWriteTopChanged(newTop int64)` — called by `WriteWindow` after grow-retreat; if `autoFollow`, `viewBottom := writeBottom`
- `VisibleRange() (top, bottom int64)`
- `IsFollowing() bool`

Invariants:
- `viewBottom >= height - 1` (can't scroll above the first possible row).
- `viewBottom <= writeBottom` (can't scroll past the bottom of live content).
- `autoFollow == true` ⟹ `viewBottom == writeBottom` (after every state-changing operation).

#### `sparse.Terminal`

Thin composition of the three. Exposes the API that `VTerm` currently calls into the main-screen path.

API:
- `NewTerminal(width, height int) *Terminal`
- `WriteCell`, `WriteWide`, `Newline`, `CR`, etc. — delegate to `WriteWindow`
- `Grid() [][]parser.Cell` — build a dense `height × width` grid from `[viewTop, viewBottom]` via `Store.GetLine`, blank-filling gaps
- `Resize(w, h int)` — calls `WriteWindow.Resize`, then `ViewWindow.Resize`
- `ScrollUp(n)`, `ScrollDown(n)`, `ScrollToBottom()`, `OnInput()` — delegate to `ViewWindow`
- `ContentEnd() int64`, `Cursor() (globalIdx int64, col int)`, `CursorRow() int`, `WriteTop() int64`, `ViewBottom() int64`, `IsFollowing() bool` — reads

The `Grid()` method is what the existing `VTerm.Grid()` main-screen branch will call after integration.

### Persistence integration: `sparse.Persistence`

An adapter between `sparse.Store` / `sparse.Terminal` and the existing `AdaptivePersistence` / `PageStore` on-disk layer.

Responsibilities:
- On write: forward new/updated lines to `PageStore.AppendLineWithGlobalIdx` (or equivalent) using the same globalIdx as the in-memory Store. The addressing is identical across the boundary.
- Metadata sync: persist `(contentEnd, cursor.globalIdx, cursor.col, writeTop, width, height)` after every state change that affects these. `viewBottom` and `autoFollow` are **not** persisted — they are session-transient UI state.
- On reload: read `PageStore` contents into `sparse.Store`, restore `(contentEnd, cursor, writeTop)` from metadata, set `viewBottom := writeBottom`, `autoFollow := true`.

The existing WAL-based AdaptivePersistence code is reused as-is; only the field names and what they represent change. The WAL metadata record replaces `liveEdgeBase` with `writeTop` and `contentEnd`. This is a single-record structural change, not a format change.

### Integration point: `VTerm`

`VTerm` currently holds the main-screen state inline (`liveEdgeBase`, the `MemoryBuffer` ring, the pendingRollback flags, etc.). After integration:

- `VTerm.mainScreen *sparse.Terminal` replaces `VTerm.memoryBuffer *MemoryBuffer` and the associated flag soup.
- All the `memoryBuffer*` methods in `vterm_memory_buffer.go` are either deleted or rewritten to delegate to `mainScreen`.
- `clampCursorToHeight`, `memoryBufferPushViewportToScrollback`, `memoryBufferEraseScreen`'s case-2 push, and `memoryBufferScrollRegion`'s scrollback-advance path all go away. The new model has no equivalent.
- `SetMargins` still sets the DECSTBM region on the main-screen `WriteWindow`, but the rollback-on-partial-region and commit-on-fullscreen logic is deleted. A scroll region is just a scroll region now; it does not interact with scrollback promotion.
- `memoryBufferResize` becomes `mainScreen.Resize(w, h)`. The entire rollback-on-resize block is deleted.
- `memoryBufferEraseScreen` case 2 (ESC[2J) becomes a simple clear of `[writeTop, writeBottom]`. No pushing anything anywhere.

### Deleted concepts (the "delete-list")

The integration PR removes all of the following from `parser/vterm.go` and `parser/vterm_memory_buffer.go`:

- Fields: `pendingRollbackActive`, `pendingRollbackPreEdge`, `pendingRollbackPreGlobalEnd`, `pendingRollbackSavedLines`, `pendingRollbackSavedBase`, `suppressNextScrollbackPush`, `hasPartialScrollRegion`, `restoredFromDisk`.
- Methods / helpers: `memoryBufferPushViewportToScrollback`, the rollback branches in `SetMargins`, the cursor-clamp scrollback-advance in `clampCursorToHeight`, the resize-time rollback block in `memoryBufferResize`, any helper referenced only by the above.
- `MemoryBuffer` itself (the dense ring), once `VTerm`'s main-screen path is fully on `sparse.Terminal`. Files `memory_buffer.go`, `vterm_memory_buffer.go` shrink to a thin delegation layer during transition, then the delegation layer is also deleted.
- Any `inAltScreen`-gated rollback logic (alt screen's own state is preserved; only the main-screen rollback interaction is deleted).

### Client / server touch points

- `internal/runtime/server/desktop_publisher.go` calls `VTerm.Grid()` to snapshot panes. No change to that call site — `Grid()` returns the same type.
- `ViewportWindow` (the client-side projection type in `parser/viewport_window.go`) is renamed to `ViewWindow` and its innards are replaced by `sparse.ViewWindow`. Its external API changes minimally: the concurrency contract (mutex usage) stays the same so the publisher/client callers don't need changes beyond the type rename.
- Snapshot/delta protocol is unchanged. The `Rows` field of `PaneSnapshot` (flagged for removal in existing notes) can be cleaned up in a follow-up if desired, but is not required by this redesign.

## Build sequence

Each step is a PR. Steps 1–5 are pure additions with no risk to existing behavior. Step 6 is the cutover and is the only step that can break things. Step 7 is cleanup.

1. **`parser/sparse/store.go`** + unit tests. Pure data structure. No dependency on existing parser internals.
2. **`parser/sparse/write_window.go`** + unit tests. All resize/scroll/cursor rules. Depends on `Store`.
3. **`parser/sparse/view_window.go`** + unit tests. Scroll, autoFollow, resize. Depends on `Store` (read-only) and observes `WriteWindow`.
4. **`parser/sparse/terminal.go`** + unit tests (still no PTY). Glue, `Grid()` projection, character-feed entry point compatible with `VTerm`'s main-screen calls.
5. **`parser/sparse/persistence.go`** + tests. WAL metadata schema update (`liveEdgeBase` → `writeTop` + `contentEnd`). PageStore reload repopulates `Store` directly.
6. **Integration PR**: replace `VTerm`'s main-screen path with `sparse.Terminal`. Delete `MemoryBuffer`. Delete every field on the delete-list. Update `claude_code_shrink_test.go`, `codex_*` tests, and any other main-screen regression test to match the corrected semantics (many of the current assertions encode the bug). This is the commit that makes the bug go away.
7. **Cleanup PR**: rename `ViewportWindow` → `ViewWindow`, update callers in `internal/runtime/server`, client, and tests. Update / delete stale sections of `docs/TERMINAL_PERSISTENCE_ARCHITECTURE.md`. Update `CLAUDE.md` memory entries.

The branch `fix/no-scrollback-from-partial-scroll-regions` and its 18+ patch commits should **not** be merged. This redesign supersedes it. That branch can be closed or rebased to drop its fix commits after the integration PR lands.

## Open questions for the plan phase

These are design-adjacent choices that do not affect the semantic model and can be decided when writing the implementation plan:

1. **`sparse.Store` internal data structure.** Btree, ordered map, chunked segment list, `map[pageID]*page`. Plan should prototype and benchmark if it matters; correctness does not depend on the choice.
2. **Exact wire format for the updated WAL metadata record.** Whether to bump a format version or coexist with the old record during one release cycle. Leans toward "bump and break" — the old state has no clean translation to the new state anyway.
3. **Concurrency boundaries.** `sparse.Store` is accessed from the parser thread (writes) and the render thread (reads via `sparse.Terminal.Grid()`). The plan needs to specify lock granularity. Likely one RWMutex on `Store`, with `WriteWindow` and `ViewWindow` each having their own small mutex for their own state. The existing `ViewportWindow` race bug (fixed 2026-02-15, RLock→Lock) informs the choice: any "read" method that calls lazy-init code must take a write lock, or lazy-init must be done eagerly.
4. **Test migration strategy.** Which existing parser tests assert old-model behavior (push-on-clear, pendingRollback commit, etc.) and need their assertions flipped, vs which tests are model-agnostic and should pass as-is. A one-time audit during step 6.
5. **Client-side viewport state.** Does `autoFollow` / `viewBottom` live on the server (`sparse.ViewWindow` in `VTerm`) or on the client? This spec assumes server-side — the server owns the whole terminal state and the client renders snapshots. If multiple clients attach to one pane, they all share the same view. Per-client views would be a larger change (each client runs its own `ViewWindow` observing a server-side `WriteWindow`). Flagging for the plan to decide; default is server-side.

## Success criteria

- `TestClaudeCodeShrinkDragPollutesScrollback` passes with the `Claude Code` text marker appearing exactly once across scrollback + viewport after a 40→20 shrink drag. (Top-border rows of the claude banner may smear into scrollback under the cursor-minimum-advance rule, but the textual content of the banner does not duplicate because each of those smeared rows was claude's row 0 at that point in time — the banner's text rows sit at row 1+ and never become the "oldest row" of the write window.)
- A new regression test covering the user's real-world scenario (30-50 lines of prior scrollback, grow to the pane's maximum height, then shrink one row at a time) shows that the viewport-bottom content remains stable — prior scrollback is not corrupted, and the number of `"Claude Code"` text occurrences does not grow across repeated grow/shrink cycles. (A small number of banner-border rows may accumulate under cursor-minimum-advance; the test should assert against textual-content duplication specifically, not against raw line count.)
- The existing codex scroll-region tests still pass (content written inside DECSTBM regions still ends up in scrollback via normal LF-at-bottom scrolling, not via a rollback/commit event).
- `grep -r pendingRollback apps/texelterm/parser/` returns no matches.
- `grep -r suppressNextScrollback apps/texelterm/parser/` returns no matches.
- A user who scrolls back 100 lines while claude is redrawing sees their scrolled-back content completely stable, regardless of what claude is doing in the (invisible) write window.
- Reload from disk after a clean shutdown restores `(contentEnd, cursor, writeTop)` correctly and defaults to `autoFollow = true`.
