# Sparse Test Migration Plan

Date: 2026-04-14
Branch: `chore/sparse-cleanup`

## Context

After the sparse cutover (PR #179, 2026-04-14), two `//go:build ignore` test files were
retained in `apps/texelterm/parser/` as migration candidates:

- `burst_recovery_test.go` (~1261 lines) — **done** — migrated in this branch
- `vterm_memory_buffer_test.go` (5944 lines, 69 tests) — audit below

## Current Status (2026-04-14)

- `burst_recovery_test.go` is migrated and un-ignored. All tests pass.
- Migration exposed and fixed two production bugs in `vterm_main_screen.go` recovery path:
  1. VTerm's `cursorX`/`cursorY` were not synced from restored `MainScreenState`,
     causing subsequent writes to overwrite the top row.
  2. Stale metadata (WriteTop beyond PageStore end) was blindly restored.
     Now validated against `PageStore.LineCount()` and discarded if inconsistent.
- **All five migration batches complete; `vterm_memory_buffer_test.go` deleted.**
  Surviving sparse-native suites:
  - `scrollback_history_test.go` (Batch 1 — 5 tests; `TestLoadHistory_TrimsBlankTailLines` dropped — sparse restores WriteTop verbatim)
  - `scroll_region_test.go` (Batch 2 — 9 tests)
  - `overlay_insert_test.go` (Batch 3 — 3 tests; dropped 3 pre-sparse tests tied to the Cells/Overlay toggle and viewport cache)
  - `resize_cursor_sync_test.go` (Batch 4 — 5 tests; dropped 2 width-wrap-reflow tests, fundamentally incompatible)
  - `reverse_search_test.go`, `osc7_test.go` (Batch 5 — 2 tests)
- Production regression fixes landed in `RequestLineInsert` (commit baadfc9 semantics
  restored): cursor-follow on insert at/before cursor, and `PromptStartGlobalLine`
  shift on insert at/before prompt. Pre-sparse's "cursor-at-prompt-row on reload +
  erase-from-prompt-down" is a deliberately dropped semantic, documented inline in
  the reload tests rather than restored.

## Audit of `vterm_memory_buffer_test.go`

Classification of the 69 tests:

- **COVERED** (20 tests, 29%) — already tested by sparse-native suites; delete
- **PORTABLE** (26 tests, 38%) — valid sparse scenarios worth porting
- **INCOMPATIBLE** (22 tests, 32%) — tied to legacy reflow model; delete
- **OBSOLETE** (1 test, 1%) — legacy API toggle; delete

Net actionable work: port 26 tests, delete the other 43.

### Portable tests by theme

1. **Scrollback / History / Metadata**
   - `TestVTerm_MemoryBufferUserScroll` — user scrollback navigation via view window
   - `TestVTerm_MemoryBufferTotalLines` — count total lines in store
   - `TestLoadHistory_TrimsBlankTailLines` — blank tail trimming on reload
   - `TestLoadHistory_ResetsTerminalColors` — terminal color reset on history load
   - `TestPromptPositionOnReload` — PromptStartGlobalLine persisted + restored
   - `TestPromptPositionAfterTransformerInsert` — PromptStartGlobalLine adjusted when
     synthetic lines inserted

2. **Scroll Region (non-reflow)**
   - `TestVTerm_ScrollRegionNoHeader` — region at row 0
   - `TestVTerm_ScrollRegionNoFooter` — region ending at last row
   - `TestVTerm_ScrollRegionMultipleScrollN` — ESC[nS with n>1
   - `TestVTerm_ScrollRegionFullScreenUnchanged` — scroll after region reset
   - `TestVTerm_ScrollRegionScrollDownUnchanged` — ESC[nT within region
   - `TestVTerm_ScrollRegionReloadCorruption` — TUI region + reload content integrity
   - `TestVTerm_ScrollRegionReloadMultipleTUISessions` — multi-session reload
   - `TestVTerm_ScrollRegionReloadLongSession` — large-volume region test
   - `TestVTerm_ScrollRegionMultiCycleReload` — repeated open/close of TUI with region

3. **Overlay / Synthetic lines**
   - `TestGetContentText_PrefersOverlayWhenEnabled`
   - `TestGetContentText_SyntheticLineWithOverlay`
   - `TestRequestLineInsert_CursorPositionNotFullViewport`
   - `TestLineHasContent_SyntheticAndOverlay`

4. **Resize (non-reflow, viewport projection only)**
   - `TestVTerm_MemoryBufferResize`
   - `TestVTerm_ResizeBeforeContentCursorSync`
   - `TestVTerm_ResizeShrinkBeforeContentCursorSync`
   - `TestResize_HeightChangeCursorDesync`
   - `TestResizeWidth_FullViewport_CursorGridConsistency`
   - `TestVTerm_ResizeWidthWrapDiagnostic`

5. **Special sequences**
   - `TestOSC7_WorkingDirectory` — OSC 7 parsing
   - `TestReverseSearch_RealReadlineSequences` — readline emulation

### Tests to delete (incompatible with sparse)

All reflow-based width/wrap tests are fundamentally incompatible — sparse stores at
fixed width and does not reflow on resize. These can be deleted without sparse-native
replacements:

- `TestAutoWrap_ResizeReflow`
- `TestVTerm_ScrollRegionReloadCorruptionScaling`
- `TestVTerm_ResizeWidthWrapBeforeContentCursorSync`
- `TestResize_SpringAnimationCursorSync`
- `TestResize_WidthDecreaseIncreaseWrapChainDesync`
- `TestResize_WithShellRedrawBetweenResizes`
- `TestResize_CursorPastWrapChain_PhysicalCursor`
- `TestResize_SpringAnimation_PhysicalCursorSync`
- `TestResize_DirtyTrackingAfterWrapChain`
- `TestSmallTerminal_ResizeDownCursorDrift`
- `TestResizeWidth_TransformerContent_DebugPhysicalLines`
- `TestResizeWidth_TransformerContent_CursorDrift`
- `TestResizeWidth_NewPromptAtSmallWidth_ExpandDrift`
- `TestResizeWidth_TwoLinePrompt_CharByCharExpand`
- `TestResizeSplit_FlagSetOnChunks`
- `TestResizeSplit_NaturalWrapNotRejoined`
- `TestResizeSplit_CursorPastChain`
- `TestRequestLineOverlay_MarksDirtyAndInvalidatesCache` (memBufState cache model)
- `TestRequestLineInsert_MarksDirtyAndInvalidatesCache` (memBufState cache model)

## Migration Strategy

Approach: port portable tests in theme batches, deleting the legacy file when the last
portable test is covered.

**Suggested batch order:**
1. Scrollback / History / Metadata (6 tests) — low-risk, plumbing exists
2. Scroll Region reload scenarios (9 tests) — highest coverage value
3. Overlay / Synthetic lines (4 tests) — needs GetContentText sparse equivalent
4. Resize non-reflow (6 tests) — exercises Rule 5/6 variants
5. Special sequences (2 tests) — OSC 7, reverse search

At the end of each batch, the corresponding tests can be removed from the ignored file.
When all 26 are ported, the whole file can be deleted.

## Helper APIs Available

- `newTestVTerm(t, cols, rows, dir, id)` already exists and sets up sparse persistence
- `captureState(v)`, `captureViewport(v)`, `readSparseLine(v, gi)` in burst_recovery_test.go
- `sparseCellsToString`, `logicalLineToString`, `trimLogicalLine` in test_helpers_test.go

## Not in Scope

- Porting legacy reflow tests — sparse does not reflow by design.
- Re-creating the `memoryBufferScrollRegion` / `ViewportWindow` helpers — they no longer exist.
