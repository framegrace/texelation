# ED-Clear Tombstones for Persistent Sparse Store

**Status**: Design
**Date**: 2026-04-19
**Relates to**: `docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md`, `docs/superpowers/specs/2026-04-07-sparse-pagestore-design.md`
**Implements fix for**: phantom Claude text bleeding through on reload after OSC 133 anchor persistence (PR #190, follow-up to #189)

## Background

The sparse persistence stack (`sparse.Store` + WAL + `PageStore`) treats the on-disk store as append-only. `sparse.Store.ClearRange(lo, hi)` (`apps/texelterm/parser/sparse/store.go:201`) deletes entries from the in-memory `lines` map only — no notification reaches `AdaptivePersistence`, the WAL, or the `PageStore` index. On restart, `LoadStore` iterates every persisted position via `StoredLineCount` + `GlobalIdxAtStoredPosition` and re-materializes every cell that was ever written, including cells that an in-session ED 2 (or other erase op) had already cleared.

User-visible symptom: after an in-session `/compact` or similar TUI reset, Claude issues ED 2 (`ESC[2J`), which our VTerm resolves via anchor-rewind to `CommandStart` / `InputStart+1` / `PromptStart+1` and calls `sparse.Terminal.ClearRange` on the stale range. The in-session grid looks correct. But on the next restart the stale range returns verbatim from disk and shows through any cells the respawned shell does not overwrite.

A prior attempt (b75073c on `fix/clear-viewport-on-restore`) patched this at restore time by gating on `CommandStartGlobalLine >= 0` and clearing cells after `LoadStore`. It was fragile (depended on a command being in-flight at crash time) and was reverted. This spec addresses the problem at its origin: the clear needs to propagate to disk.

## Goals

- ED erase ops (`ED 0`, `ED 1`, `ED 2`, single-line invalidate) that clear a range on the sparse store also remove that range from the on-disk `PageStore` index, so `LoadStore` never re-emits the cleared cells.
- One WAL entry per range clear, not one per line.
- Ordering: a clear that follows queued writes in the same session must beat those writes in the end state — no zombies.
- Crash safety: if the process dies between emitting the WAL delete op and applying it to `pageIndex`, WAL replay re-applies the delete.
- Downgrade safety: an older binary opening a new-format WAL must fail loudly, not silently truncate.

## Non-goals

- Tombstones for `WriteWindow` scroll / newline / scroll-region ops. Those clears are always immediately overwritten by subsequent writes, so tombstoning them produces churn with no correctness gain.
- Page-file byte reclamation / compaction. Tombstoned bytes remain in page files until natural rotation. Correctness depends on `pageIndex` absence, not byte-level erasure.
- Multi-version WAL replay (skip-unknown op codes). WAL stays strict: any unknown op triggers recovery-truncate as today.
- Automatic v1 → v2 WAL migration. On first launch with v2 code, a pre-existing v1 WAL will be rejected and history will appear empty. Documented in the PR description so the upgrade path is explicit.

## The Approach

### Architecture

```
vterm_main_screen.go / vterm_erase.go  (ED 0/1/2 + MainScreenInvalidateLine)
    │ call sparse.Terminal.ClearRangePersistent(lo, hi)
    ▼
sparse.Terminal.ClearRangePersistent  (new)
    • calls store.ClearRange(lo, hi)            — in-memory delete (existing)
    • calls persistence.NotifyClearRange(lo, hi) — new persistence hook
    ▼
AdaptivePersistence.NotifyClearRange  (new)
    • sweeps pendingLines for gi in [lo, hi]    — drops queued writes
    • enqueues a Delete op in the FIFO ops queue
    • WriteThrough mode: flushes immediately
    • Debounced / BestEffort: flushes in FIFO order at the next flush
    ▼
WAL.AppendDelete(lo, hi, timestamp)  (new)       — one entry per range
PageStore.DeleteRange(lo, hi)        (new)       — pageIndex slice-out
```

Scroll, newline, and scroll-region clears in `write_window.go` keep calling `sparse.Store.ClearRange` directly and stay in-memory only.

### WAL format

**Version bump:** header `Version uint32` goes from 1 to 2. Magic stays `TXWAL001`. `readHeader` already rejects mismatched versions with an error; an old binary opening a v2 WAL fails cleanly.

**New op code:** `EntryTypeLineDelete = 0x06`. Current ops 0x01–0x05 unchanged.

**Entry framing:** reuses the standard 25-byte entry header.

| Field | Size | Value for delete ops |
|-------|------|----------------------|
| Type | 1 byte | `0x06` |
| GlobalLineIdx | 8 bytes | `lo` (int64, inclusive) |
| Timestamp | 8 bytes | wall-clock at the time of the ED op |
| DataLen | 4 bytes | `8` |
| Data | 8 bytes | `hi` (int64, inclusive, big-endian) |
| CRC32 | 4 bytes | over all prior fields |

**Validity:** `lo ≥ 0`, `lo ≤ hi`. Malformed entries fall through to the existing corruption path (WAL truncated to the last valid entry, execution continues).

**Checkpointing:** unchanged. A tombstone applied before a checkpoint is reflected in the post-checkpoint `pageIndex` state, so the existing checkpoint-truncation logic works without modification.

### PageStore

**New method:**

```go
// DeleteRange removes all pageIndex entries in the closed interval [lo, hi].
// Emits one WAL entry (EntryTypeLineDelete), then mutates pageIndex and
// totalLineCount. Page-file bytes are not reclaimed. Safe no-op if the
// range contains no stored entries (the WAL entry is still emitted so
// on-disk ordering matches caller order; this keeps replay deterministic).
func (ps *PageStore) DeleteRange(lo, hi int64) error
```

**Implementation sketch:**
1. Validate `lo ≤ hi` and both non-negative.
2. Emit `wal.AppendDelete(lo, hi, time.Now())`.
3. Binary-search `pageIndex` for the first entry with `globalIdx ≥ lo`.
4. Scan forward while `globalIdx ≤ hi`; record the end index.
5. `pageIndex = append(pageIndex[:start], pageIndex[end:]...)`.
6. `totalLineCount -= (end - start)`.
7. If the current unflushed page falls entirely inside the range, discard it (reset `currentPage` to `nil`).

**WAL replay:** the recovery path at `write_ahead_log.go:885` dispatches by op code; a new case for `EntryTypeLineDelete` calls `PageStore.DeleteRange` directly (bypassing the WAL-emit step — replay is idempotent).

### AdaptivePersistence

**New method:**

```go
// NotifyClearRange records a tombstone for [lo, hi]. Drops any queued
// writes in that range from pendingLines, then enqueues a Delete op in
// the FIFO ops queue. Flush semantics match the current mode:
//
//   WriteThrough: flush synchronously.
//   Debounced:    arm debounce timer (or flush if already armed); ordering
//                 preserved by the FIFO queue.
//   BestEffort:   queue only; flush on idle or Close.
func (ap *AdaptivePersistence) NotifyClearRange(lo, hi int64)
```

**FIFO ops queue.** Replaces (or wraps) the current `pendingLines` map with an ordered sequence of operations so writes and deletes flush in call order. Shape:

```go
type pendingOp struct {
    kind    opKind       // opWrite | opDelete
    lo, hi  int64        // for deletes; for writes, lo == hi == lineIdx
    ts      time.Time
    isCmd   bool         // writes only
}

type AdaptivePersistence struct {
    // ...existing fields...
    pendingOps []pendingOp     // ordered list
    pendingSet map[int64]int   // lineIdx -> index into pendingOps (for dedup)
}
```

`NotifyWrite` always appends a new `opWrite` entry and sets `pendingSet[gi]` to the new entry's index. `NotifyClearRange` sweeps `pendingSet` for any `gi` in `[lo, hi]`, marks those `pendingOps` entries as dropped (or removes them), clears the matching `pendingSet` keys, then appends an `opDelete`. Flush drains `pendingOps` in order: for each write, skip if `pendingSet[gi]` points to a later entry (superseded by a newer write); otherwise call `PageStore.AppendLineWithGlobalIdx`. For each delete, call `PageStore.DeleteRange`. This preserves call order between writes and deletes while deduping successive writes to the same line.

**Pre-evict callback:** unchanged. Eviction still reads the current in-memory state; if a cell has been tombstoned before eviction triggers, the eviction simply sees nothing to flush for that `gi`.

### sparse.Terminal

**New method:**

```go
// ClearRangePersistent removes lines [lo, hi] from the in-memory store
// and tombstones the range on disk. Used by VTerm ED ops and single-line
// invalidation; not used by WriteWindow scroll / newline / scroll-region
// (those stay transient).
func (t *Terminal) ClearRangePersistent(lo, hi int64)
```

Implementation:
1. `t.store.ClearRange(lo, hi)`.
2. If `t.persistence != nil`, `t.persistence.NotifyClearRange(lo, hi)`.

### Call-site changes

Switch these sites from `ClearRange` to `ClearRangePersistent`:

- `apps/texelterm/parser/vterm_main_screen.go:351` (ED 0 cursor → screen bottom)
- `apps/texelterm/parser/vterm_main_screen.go:355` (ED 1 screen top → cursor)
- `apps/texelterm/parser/vterm_main_screen.go:394` (ED 2 anchor-rewind)
- `apps/texelterm/parser/vterm_main_screen.go:410` (ED 2 overflow-past-viewport)
- `apps/texelterm/parser/vterm_main_screen.go:416` (ED 2 no-scrollback)
- `apps/texelterm/parser/vterm_main_screen.go:507` (`MainScreenInvalidateLine`)
- `apps/texelterm/parser/vterm_erase.go:68` (ED 2 alt-screen / no-scrollback path)

Leave unchanged (in-memory only):

- `apps/texelterm/parser/sparse/write_window.go:272, 280, 328, 358, 383`

## Error handling

- `WAL.AppendDelete` failure: propagate up from `PageStore.DeleteRange`. Caller (`AdaptivePersistence` flush path) logs and continues; this matches the existing policy for `AppendLineWithGlobalIdx` errors.
- Malformed WAL delete entry on replay: falls into the existing corruption path (`write_ahead_log.go:913-997`) — WAL truncates to last valid entry.
- `DeleteRange` called with `lo > hi` or negative values: return an error without emitting a WAL entry.
- Downgrade (v2 WAL + v1 binary): `readHeader` returns `fmt.Errorf("unsupported WAL version %d")`; the terminal starts with empty history.

## Testing

1. **WAL round-trip** (`write_ahead_log_test.go`). Append a mix of writes and deletes; recover; verify entry order and payloads. Corrupt a delete entry's CRC; verify truncation at the entry boundary.
2. **PageStore range delete** (`page_store_test.go`). Seed 10 lines at `gi=0..9`. `DeleteRange(3,7)`; assert `StoredLineCount == 5`, `GlobalIdxAtStoredPosition(3) == 8`, `ReadLine(4)` returns not-found. Edges: empty range (no entries in `[lo,hi]`), single-line range, whole-store range, range straddling the current unflushed page.
3. **AdaptivePersistence FIFO ordering** (`adaptive_persistence_test.go`). WriteThrough: `NotifyWrite(5); NotifyClearRange(0,10); NotifyWrite(6)` → WAL records write-delete-write; `pendingOps` for gi=5 dropped at the delete. Debounced: same sequence queued, single flush emits in FIFO order.
4. **Sparse reload round-trip** (`sparse/persistence_test.go`). Persist 20 lines, `ClearRangePersistent(5, 10)`, close, reopen, verify `LoadStore` skips the cleared range and the in-memory store after load has no entries in `[5,10]`.
5. **End-to-end ED 2 regression** (`osc133_anchor_persistence_test.go`). Drive VTerm through a simulated TUI session that issues ED 2 with anchor rewind; close; reopen; verify no stale cells in the rewound range. This is the direct regression test for the bug motivating the whole feature.

## Rollout

- Branch: `fix/clear-viewport-on-restore` (current). Rename to `feat/persistent-clear-range` is optional — cosmetic.
- Squash history on the branch to drop the reverted b75073c restore-time approach; keep the ED-in-session anchor clear commit (1c86537) plus the new tombstone commits.
- PR #190 description rewritten around tombstones.
- First launch after merge will report "unsupported WAL version" for pre-existing per-terminal WAL files and start with empty history. Acceptable given the single-user situation; documented in the PR body.
