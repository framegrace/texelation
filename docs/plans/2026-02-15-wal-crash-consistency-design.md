# WAL Crash Consistency Hardening

**Date**: 2026-02-15
**Status**: Approved

## Problem

After exiting texelterm (especially quickly via Ctrl+D), restarting shows:
1. Blank lines at the bottom of history (near where cursor was)
2. New text invisible (black on black) — terminal color state not initialized

These symptoms also occur after SIGKILL, server stop, or machine shutdown. The WAL persistence system must produce consistent state after ANY interruption.

## Root Causes

### Blank tail lines
`flushPendingLocked()` writes content and metadata to the WAL file but never calls `fsync()`. Data sits in the OS page cache. On Linux, `Close()` does NOT guarantee sync. If the process exits before the OS flushes the page cache, the data is lost. On reload, metadata (liveEdgeBase) may point past actually-persisted content, producing blank lines.

### Black-on-black colors
On reload, VTerm's `currentFG` and `currentBG` start at their zero value (`Color{Mode: 0}`), which is not the same as `DefaultFG`/`DefaultBG` (sentinels that resolve to theme colors). New shell output uses these zero-value colors, rendering as black on black.

## Design

### Principle
The on-disk state must be self-consistent after ANY interruption, OR the reload path must detect and repair inconsistencies.

### Change 1: Sync after every flush (`adaptive_persistence.go`)

Add `wal.SyncWAL()` at the end of `flushPendingLocked()` when content was written. This ensures debounced flushes reach disk, not just checkpoints.

Cost: ~3-5 fsyncs/second in Debounced mode. Acceptable for terminal scrollback.

### Change 2: Sync before close (`adaptive_persistence.go`)

In `Close()`, after `flushPendingLocked()` and before releasing the lock, call `wal.SyncWAL()`. This ensures the final flush reaches disk even if the process is killed before `wal.Close()` completes its checkpoint.

### Change 3: Expose SyncWAL method (`write_ahead_log.go`)

New public `SyncWAL()` method that calls `walFile.Sync()` under the WAL mutex.

### Change 4: Self-healing reload (`vterm_memory_buffer.go`)

After restoring metadata in `loadHistoryFromDisk()`, scan backward from `liveEdgeBase` and trim blank tail lines. Clamp `liveEdgeBase` to the last non-empty line + 1.

"Non-empty" = line has at least one cell with a non-space rune or non-default colors.

This handles all crash scenarios: stale metadata, partial flushes, unsynced content.

### Change 5: Reset terminal colors on reload (`vterm_memory_buffer.go`)

After history restore, reset drawing state:
```go
v.currentFG = DefaultFG
v.currentBG = DefaultBG
v.currentAttr = 0
```
The shell will re-emit colors on its first prompt. No need to persist colors — simpler and more correct.

### Change 6: Reorder Stop() sequence (`term.go`)

Close PTY before CloseMemoryBuffer so no new data arrives after persistence shuts down.

## What This Does NOT Change

- WAL format (no new metadata fields)
- ViewportState struct (no color persistence)
- Checkpoint logic (already correct with sync)
- Timer race in scheduleFlushLocked (already safe via ap.stopped check)

## Crash Scenario Matrix

| Scenario | Before | After |
|----------|--------|-------|
| SIGKILL during debounced flush | Data in page cache, lost | Synced to disk at each flush |
| SIGKILL between flushes | Pending data lost, stale metadata | Self-healing trim on reload |
| Normal close, fast exit | Race between flush and wal.Close | Explicit sync before close |
| Reload with blank tail | Blank lines visible, wrong liveEdgeBase | Trimmed, liveEdgeBase clamped |
| Reload black-on-black | FG/BG zero value = black | Reset to DefaultFG/DefaultBG |

## Scope

~40-60 lines of new code across 4 files. No format changes, no new dependencies.
