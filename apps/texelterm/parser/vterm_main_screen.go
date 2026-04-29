// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// vterm_main_screen.go: main-screen implementation backed by sparse.Terminal.
// Replaces vterm_memory_buffer.go after Step 6.6 cutover.

package parser

import (
	"log"
	"strings"
	"time"
	"unicode"
)

// IsMemoryBufferEnabled returns true if the sparse main screen is active.
// Kept for API compatibility with term.go callers.
func (v *VTerm) IsMemoryBufferEnabled() bool {
	return v.mainScreen != nil
}

// EnableMemoryBuffer activates the sparse main screen without disk persistence.
func (v *VTerm) EnableMemoryBuffer() {
	if v.mainScreen != nil {
		return
	}
	if MainScreenFactory != nil {
		v.mainScreen = MainScreenFactory(v.width, v.height)
	}
}

// EnableMemoryBufferWithDisk activates the sparse main screen with WAL persistence.
func (v *VTerm) EnableMemoryBufferWithDisk(diskPath string, opts MemoryBufferOptions) error {
	if opts.TerminalID == "" {
		opts.TerminalID = "sparse-term"
	}
	if MainScreenFactory != nil {
		v.mainScreen = MainScreenFactory(v.width, v.height)
	}
	if v.mainScreen == nil {
		return nil
	}

	walConfig := DefaultWALConfig(diskPath, opts.TerminalID)

	// Open WAL (which owns PageStore).
	wal, err := OpenWriteAheadLog(walConfig)
	if err != nil {
		log.Printf("[MAIN_SCREEN] WAL init failed: %v, running without persistence", err)
		return nil
	}
	pageStore := wal.PageStore()
	v.mainScreenPageStore = pageStore

	// Recover metadata from WAL to restore write position.
	// Validate against the PageStore's logical end: metadata may have been
	// written just before a crash without the referenced lines reaching disk.
	// Restoring a WriteTop past the available content would leave the write
	// window anchored in empty space. Discard such stale metadata rather
	// than propagating it into a new session.
	recoveredMeta := wal.RecoveredMainScreenState()
	pageStoreLineCount := pageStore.LineCount()
	if recoveredMeta != nil && recoveredMeta.WriteTop <= pageStoreLineCount && recoveredMeta.CursorGlobalIdx <= pageStoreLineCount+int64(v.height) {
		v.mainScreen.RestoreState(recoveredMeta.WriteTop, recoveredMeta.CursorGlobalIdx, recoveredMeta.CursorCol, recoveredMeta.WriteBottomHWM)
		// Discard a stale PromptStartLine that points past the last persisted
		// line. The prompt position is only meaningful if the referenced line
		// exists; otherwise prompt-aware operations (scroll-to-prompt,
		// erase-to-prompt) would target non-existent rows. -1 means "unknown".
		// An anchor exactly at pageStoreLineCount is the first unwritten line
		// — legitimate when the last write ended with \r\n and the anchor is
		// the starting point for the next input/command. Only anchors strictly
		// past the stored content are considered dangling.
		if recoveredMeta.PromptStartLine >= 0 && recoveredMeta.PromptStartLine > pageStoreLineCount {
			log.Printf("[MAIN_SCREEN] Discarded stale PromptStartLine %d (PageStore end=%d)",
				recoveredMeta.PromptStartLine, pageStoreLineCount)
			v.PromptStartGlobalLine = -1
		} else {
			v.PromptStartGlobalLine = recoveredMeta.PromptStartLine
		}
		// Same stale-discard rule for the OSC 133 input/command anchors: if
		// they point strictly past the last persisted line the reference is
		// dangling and any ED-2 rewind driven by them would land on
		// non-existent rows.
		if recoveredMeta.InputStartLine >= 0 && recoveredMeta.InputStartLine > pageStoreLineCount {
			log.Printf("[MAIN_SCREEN] Discarded stale InputStartLine %d (PageStore end=%d)",
				recoveredMeta.InputStartLine, pageStoreLineCount)
			v.InputStartGlobalLine = -1
		} else {
			v.InputStartGlobalLine = recoveredMeta.InputStartLine
		}
		if recoveredMeta.CommandStartLine >= 0 && recoveredMeta.CommandStartLine > pageStoreLineCount {
			log.Printf("[MAIN_SCREEN] Discarded stale CommandStartLine %d (PageStore end=%d)",
				recoveredMeta.CommandStartLine, pageStoreLineCount)
			v.CommandStartGlobalLine = -1
		} else {
			v.CommandStartGlobalLine = recoveredMeta.CommandStartLine
		}
		v.CurrentWorkingDir = recoveredMeta.WorkingDir
		// Sync VTerm's cursor to the restored state so the next write
		// lands at the correct row in the viewport. Without this, VTerm's
		// cursorY stays 0 and subsequent writes overwrite the top row.
		v.cursorX = recoveredMeta.CursorCol
		if cursorY := int(recoveredMeta.CursorGlobalIdx - recoveredMeta.WriteTop); cursorY >= 0 && cursorY < v.height {
			v.cursorY = cursorY
		}
		log.Printf("[MAIN_SCREEN] Restored: writeTop=%d cursor=%d", recoveredMeta.WriteTop, recoveredMeta.CursorGlobalIdx)
	} else if recoveredMeta != nil {
		log.Printf("[MAIN_SCREEN] Discarded stale metadata: writeTop=%d cursorGI=%d exceed PageStore end=%d",
			recoveredMeta.WriteTop, recoveredMeta.CursorGlobalIdx, pageStoreLineCount)
	}

	// Load historical lines from PageStore into sparse store.
	if err := v.mainScreen.LoadFromPageStore(pageStore); err != nil {
		log.Printf("[MAIN_SCREEN] LoadFromPageStore failed: %v", err)
	}

	// Create persistence adapter.
	adapter := &sparseLineStoreAdapter{tm: v.mainScreen}
	apConfig := DefaultAdaptivePersistenceConfig()
	persistence, err := newAdaptivePersistenceWithWAL(apConfig, adapter, wal, time.Now)
	if err != nil {
		log.Printf("[MAIN_SCREEN] AdaptivePersistence init failed: %v", err)
		wal.Close()
		return nil
	}
	v.mainScreenPersistence = persistence
	v.mainScreen.SetClearNotifier(persistence)

	log.Printf("[MAIN_SCREEN] Persistence enabled, history lines=%d", pageStore.LineCount())
	return nil
}

// CloseMemoryBuffer flushes persistence and closes.
func (v *VTerm) CloseMemoryBuffer() error {
	if v.mainScreen == nil {
		return nil
	}
	if v.mainScreenPersistence != nil {
		// Flush current viewport lines before closing.
		writeTop := v.mainScreen.WriteTop()
		for y := 0; y < v.height; y++ {
			gi := writeTop + int64(y)
			cells := v.mainScreen.ReadLine(gi)
			if cells != nil && lineHasSparseContent(cells) {
				v.mainScreenPersistence.NotifyWrite(gi)
			}
		}
		// Write final metadata.
		state := v.snapshotMainScreenState()
		v.mainScreenPersistence.NotifyMetadataChange(&state)
		if err := v.mainScreenPersistence.Flush(); err != nil {
			log.Printf("[MAIN_SCREEN] Close flush failed: %v", err)
		}
		v.mainScreenPersistence.Close()
		v.mainScreenPersistence = nil
	}
	return nil
}

// snapshotMainScreenState builds a MainScreenState from current sparse terminal.
func (v *VTerm) snapshotMainScreenState() MainScreenState {
	gi, col := v.mainScreen.Cursor()
	return MainScreenState{
		WriteTop:         v.mainScreen.WriteTop(),
		ContentEnd:       v.mainScreen.ContentEnd(),
		CursorGlobalIdx:  gi,
		CursorCol:        col,
		PromptStartLine:  v.PromptStartGlobalLine,
		InputStartLine:   v.InputStartGlobalLine,
		CommandStartLine: v.CommandStartGlobalLine,
		WorkingDir:       v.CurrentWorkingDir,
		WriteBottomHWM:   v.mainScreen.WriteBottomHWM(),
		SavedAt:          time.Now(),
	}
}

// cursorGlobalIdx returns the absolute globalIdx of the cursor's current row,
// computed as WriteTop + cursorY. Callers MUST ensure v.mainScreen != nil
// before calling; the helper does not defensive-check so that misuse surfaces
// as a nil-deref rather than a silent 0.
func (v *VTerm) cursorGlobalIdx() int64 {
	return v.mainScreen.WriteTop() + int64(v.cursorY)
}

// CursorGlobalIdx returns the cursor's (globalIdx, col) for tests and external
// callers. Returns (0, 0) if no main screen is active.
func (v *VTerm) CursorGlobalIdx() (int64, int) {
	if v.mainScreen == nil {
		return 0, 0
	}
	return v.mainScreen.Cursor()
}

// MainScreenRowNoWrap reports whether the sparse store row at globalIdx is
// marked NoWrap. Returns false if no main screen is active.
func (v *VTerm) MainScreenRowNoWrap(globalIdx int64) bool {
	if v.mainScreen == nil {
		return false
	}
	return v.mainScreen.RowNoWrap(globalIdx)
}

// mainScreenPlaceChar writes a rune to the sparse terminal at the current cursor.
func (v *VTerm) mainScreenPlaceChar(r rune, isWide bool) {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.SetCursor(v.cursorY, v.cursorX)
	gi, _ := v.mainScreen.Cursor()
	v.mainScreen.WriteCell(Cell{
		Rune: r,
		FG:   v.currentFG,
		BG:   v.currentBG,
		Attr: v.currentAttr,
		Wide: isWide,
	})
	if v.decstbmActive {
		v.mainScreen.SetRowNoWrap(gi, true)
	}
	if v.mainScreenPersistence != nil {
		v.mainScreenPersistence.NotifyWrite(gi)
	}
}

// mainScreenLineFeed handles an explicit LF at full-screen margins.
// Advances the write window and fires OnLineCommit for the committed line.
func (v *VTerm) mainScreenLineFeed() {
	if v.mainScreen == nil {
		return
	}
	// Sync sparse cursor to VTerm cursor before Newline so that WriteWindow
	// uses the correct position to decide whether to advance writeTop.
	// SetCursorPos does NOT sync the sparse cursor, so it can be stale after
	// any escape-code-driven cursor movement.
	v.mainScreen.SetCursor(v.cursorY, v.cursorX)
	oldWriteTop := v.mainScreen.WriteTop()
	v.mainScreen.Newline()
	newWriteTop := v.mainScreen.WriteTop()

	if newWriteTop > oldWriteTop {
		committedGlobal := newWriteTop - 1
		if v.OnLineCommit != nil {
			v.commitInsertOffset = 0
			line := v.mainScreen.ReadLine(committedGlobal)
			var ll *LogicalLine
			if line != nil {
				ll = &LogicalLine{Cells: line}
			} else {
				ll = &LogicalLine{}
			}
			ll.NoWrap = v.mainScreen.RowNoWrap(committedGlobal)
			if v.OnLineCommit(committedGlobal, ll, v.CommandActive) {
				// Transformer is buffering this line; skip persistence for now.
				return
			}
			committedGlobal += v.commitInsertOffset
		}
		if v.mainScreenPersistence != nil {
			v.mainScreenPersistence.NotifyWriteWithMeta(committedGlobal, time.Now(), v.CommandActive)
			v.notifyMainScreenMetadata()
		}
	}
}

// mainScreenLineFeedForWrap handles auto-wrap LF (no OnLineCommit).
func (v *VTerm) mainScreenLineFeedForWrap() {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.SetCursor(v.cursorY, v.cursorX)
	v.mainScreen.Newline()
}

// mainScreenScrollRegion shifts content in [top, bottom] by n lines.
// Positive n = scroll up (content shifts up, bottom cleared).
// Negative n = scroll down (content shifts down, top cleared).
// For full-screen scroll, advances writeTop. For partial, uses NewlineInRegion.
func (v *VTerm) mainScreenScrollRegion(n, top, bottom int) {
	if v.mainScreen == nil {
		return
	}
	isFullScreen := top == 0 && bottom == v.height-1
	if n > 0 {
		for i := 0; i < n; i++ {
			if isFullScreen {
				v.mainScreenLineFeedInternal()
			} else {
				v.mainScreen.NewlineInRegion(top, bottom)
			}
		}
	} else if n < 0 {
		// Scroll down: insert blank lines at top, shift content down.
		for i := 0; i < -n; i++ {
			v.mainScreen.InsertLines(1, top, top, bottom)
		}
	}
}

// mainScreenLineFeedInternal is a full-screen Newline that may fire OnLineCommit.
// Used for CSI S (Scroll Up) where cursor may not be at the bottom — we force
// the cursor to the write-window bottom so Newline() always advances writeTop.
func (v *VTerm) mainScreenLineFeedInternal() {
	v.mainScreen.SetCursor(v.height-1, v.cursorX)
	oldWriteTop := v.mainScreen.WriteTop()
	v.mainScreen.Newline()
	newWriteTop := v.mainScreen.WriteTop()
	if newWriteTop > oldWriteTop && v.OnLineCommit != nil {
		committedGlobal := newWriteTop - 1
		v.commitInsertOffset = 0
		line := v.mainScreen.ReadLine(committedGlobal)
		ll := &LogicalLine{}
		if line != nil {
			ll.Cells = line
		}
		ll.NoWrap = v.mainScreen.RowNoWrap(committedGlobal)
		if v.OnLineCommit(committedGlobal, ll, v.CommandActive) {
			return
		}
		committedGlobal += v.commitInsertOffset
		if v.mainScreenPersistence != nil {
			v.mainScreenPersistence.NotifyWriteWithMeta(committedGlobal, time.Now(), v.CommandActive)
		}
	}
	if v.mainScreenPersistence != nil && newWriteTop > oldWriteTop {
		v.notifyMainScreenMetadata()
	}
}

// mainScreenResize handles resize for the sparse terminal.
// Rules 5+6 are fully implemented in WriteWindow.Resize and ViewWindow.Resize.
func (v *VTerm) mainScreenResize(width, height int) {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.Resize(width, height)
	if v.mainScreenPersistence != nil {
		state := v.snapshotMainScreenState()
		v.mainScreenPersistence.NotifyMetadataChange(&state)
	}
}

// mainScreenEraseScreen handles ED (Erase Display) modes.
func (v *VTerm) mainScreenEraseScreen(mode int) {
	if v.mainScreen == nil {
		return
	}
	writeTop := v.mainScreen.WriteTop()
	switch mode {
	case 0: // cursor to end of screen
		v.mainScreen.EraseToEndOfLine(v.cursorX)
		if v.cursorY < v.height-1 {
			v.mainScreen.ClearRangePersistent(writeTop+int64(v.cursorY+1), writeTop+int64(v.height-1))
		}
	case 1: // start of screen to cursor
		if v.cursorY > 0 {
			v.mainScreen.ClearRangePersistent(writeTop, writeTop+int64(v.cursorY-1))
		}
		v.mainScreen.EraseFromStartOfLine(v.cursorX)
	case 2: // entire screen
		// If the last shell prompt is known and writeTop has drifted past
		// it (TUI repaint scrolled content), rewind writeTop to anchor at
		// the prompt. This turns "clear + repaint" into an overwrite of a
		// stable region instead of accumulating per-repaint overflow in
		// scrollback. Non-alt-screen TUIs like Claude Code are the
		// motivating case.
		if !v.inAltScreen {
			// Pick the tightest available "top of current foreground output"
			// anchor, in descending order of preference:
			//   1. CommandStartGlobalLine: writeTop when the currently-running
			//      command started (OSC 133;C, cleared on 133;D). Tracks
			//      long-running TUIs like Claude correctly — the shell's
			//      prompt may be many scrollback rows above the current frame.
			//   2. InputStartGlobalLine+1: one row past OSC 133;B (input
			//      start). Precise for bash-in-place repaints with multi-line
			//      prompts.
			//   3. PromptStartGlobalLine+1: fallback for shells that emit
			//      only 133;A. Preserves a 1-row prompt; an extra row of a
			//      taller prompt may be overwritten.
			anchor := int64(-1)
			switch {
			case v.CommandStartGlobalLine >= 0:
				anchor = v.CommandStartGlobalLine
			case v.InputStartGlobalLine >= 0:
				anchor = v.InputStartGlobalLine + 1
			case v.PromptStartGlobalLine >= 0:
				anchor = v.PromptStartGlobalLine + 1
			}
			if anchor >= 0 {
				curTop := v.mainScreen.WriteTop()
				if curTop > anchor {
					v.mainScreen.RewindWriteTop(anchor)
					// Clear [anchor, HWM] so leftover rows from the previous
					// repaint's overflow don't linger in scrollback once the
					// user scrolls up through the just-rewound area.
					v.mainScreen.ClearRangePersistent(anchor, v.mainScreen.WriteBottomHWM())
				}
			}
			// ED 2 from a non-alt-screen TUI is the canonical "homing"
			// signal: the caller is about to redraw a full frame from
			// scratch. Blank any overflow rows past the current viewport
			// up to HWM — they hold cells drawn by an earlier TUI or
			// command in this session, and if they survive they'll bleed
			// through once the new frame scrolls or the user scrolls down.
			// EraseDisplay below only covers [writeTop, writeTop+height-1];
			// this line extends the clean-slate guarantee past the viewport
			// so whatever the new TUI doesn't repaint ends up as empty
			// scrollback rather than a stale frame.
			wt := v.mainScreen.WriteTop()
			vpBot := wt + int64(v.height) - 1
			if hwm := v.mainScreen.WriteBottomHWM(); hwm > vpBot {
				v.mainScreen.ClearRangePersistent(vpBot+1, hwm)
			}
		}
		v.mainScreen.EraseDisplay()
	case 3: // clear scrollback
		if writeTop > 0 {
			v.mainScreen.ClearRangePersistent(0, writeTop-1)
		}
	}
}

// mainScreenEraseLine handles EL (Erase Line) modes.
func (v *VTerm) mainScreenEraseLine(mode int) {
	if v.mainScreen == nil {
		return
	}
	switch mode {
	case 0:
		v.mainScreen.EraseToEndOfLine(v.cursorX)
	case 1:
		v.mainScreen.EraseFromStartOfLine(v.cursorX)
	case 2:
		v.mainScreen.EraseLine()
	}
}

// mainScreenGrid returns the current sparse grid.
func (v *VTerm) mainScreenGrid() [][]Cell {
	grid, _ := v.mainScreenGridWithRowIdx()
	return grid
}

// mainScreenGridWithRowIdx returns both the current sparse grid and the
// parallel per-row globalIdx slice from the same view walk. Space-padding
// and search highlighting are applied on the rendered grid just as in
// mainScreenGrid; the rowIdx slice is passed through unchanged.
func (v *VTerm) mainScreenGridWithRowIdx() ([][]Cell, []int64) {
	if v.mainScreen == nil {
		return nil, nil
	}
	grid, rowIdx := v.mainScreen.RenderReflowWithRowIdx()
	// Sparse Grid() returns Cell{} (Rune=0) for unwritten/erased cells.
	// Convert to space so callers see consistent blank cells.
	for _, row := range grid {
		for i, c := range row {
			if c.Rune == 0 {
				row[i].Rune = ' '
			}
		}
	}
	// Apply search highlighting if set.
	if v.searchHighlight != "" && len(grid) > 0 {
		v.applySearchHighlight(grid)
	}
	return grid, rowIdx
}

// mainScreenScroll scrolls the user's view.
func (v *VTerm) mainScreenScroll(delta int) {
	if v.mainScreen == nil {
		return
	}
	if delta > 0 {
		v.mainScreen.ScrollDown(delta)
	} else if delta < 0 {
		v.mainScreen.ScrollUp(-delta)
	}
}

// mainScreenScrollToBottom snaps the view to the live edge.
func (v *VTerm) mainScreenScrollToBottom() {
	if v.mainScreen != nil {
		v.mainScreen.ScrollToBottom()
	}
}

// notifyMainScreenMetadata queues a metadata write to the WAL.
func (v *VTerm) notifyMainScreenMetadata() {
	if v.mainScreenPersistence == nil {
		return
	}
	state := v.snapshotMainScreenState()
	v.mainScreenPersistence.NotifyMetadataChange(&state)
}

// mainScreenGetHistoryLine returns the cells at globalIdx from the sparse store.
func (v *VTerm) mainScreenGetHistoryLine(globalIdx int) []Cell {
	if v.mainScreen == nil {
		return nil
	}
	return v.mainScreen.ReadLine(int64(globalIdx))
}

// mainScreenSetHistoryLine writes cells to a globalIdx in the sparse store.
func (v *VTerm) mainScreenSetHistoryLine(globalIdx int, cells []Cell) {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.SetLine(int64(globalIdx), cells)
}

// mainScreenEraseHistoryLine clears a line in the sparse store.
func (v *VTerm) mainScreenEraseHistoryLine(globalIdx int) {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.ClearRangePersistent(int64(globalIdx), int64(globalIdx))
}

// mainScreenGetTopHistoryLine returns the globalIdx at the top of the write window.
func (v *VTerm) mainScreenGetTopHistoryLine() int {
	if v.mainScreen == nil {
		return 0
	}
	return int(v.mainScreen.WriteTop())
}

// RequestLineInsert inserts a blank line before beforeIdx in the sparse store.
// Called by OnLineCommit handlers (transformers) to insert formatted output.
func (v *VTerm) RequestLineInsert(beforeIdx int64, cells []Cell) {
	if v.mainScreen == nil {
		return
	}
	// In the sparse model, insert a blank line by shifting content at the
	// write window boundary.
	writeTop := v.mainScreen.WriteTop()
	cursorRow := int(beforeIdx - writeTop)
	if cursorRow < 0 {
		cursorRow = 0
	}
	v.mainScreen.InsertLines(1, cursorRow, 0, v.height-1)
	if cells != nil {
		v.mainScreen.SetLine(beforeIdx, cells)
	}
	// The insert shifted the cursor's content down. Follow it by advancing
	// cursorY so subsequent writes land at the new logical row. Without
	// this, a multi-line prompt written after transformer inserts would
	// overwrite the inserted lines. The cursor is at cursorGlobalIdx();
	// if the insert happened at or before that, the row moved down.
	cursorGlobal := v.cursorGlobalIdx()
	if beforeIdx <= cursorGlobal && v.cursorY < v.height-1 {
		v.cursorY++
	}
	// Keep PromptStartGlobalLine pointing at the actual prompt line.
	// Transformer inserts shift content down; without this adjustment
	// the saved prompt position becomes stale and reload erases wrong lines.
	if v.PromptStartGlobalLine >= 0 && beforeIdx <= v.PromptStartGlobalLine {
		v.PromptStartGlobalLine++
	}
	if v.InputStartGlobalLine >= 0 && beforeIdx <= v.InputStartGlobalLine {
		v.InputStartGlobalLine++
	}
	if v.CommandStartGlobalLine >= 0 && beforeIdx <= v.CommandStartGlobalLine {
		v.CommandStartGlobalLine++
	}
	v.commitInsertOffset++
	v.MarkAllDirty()
}

// SetOverlay sets formatted overlay cells on a line in the sparse store.
// Called by transformers to provide a formatted view of a committed line.
func (v *VTerm) SetOverlay(lineIdx int64, cells []Cell) {
	if v.mainScreen == nil {
		return
	}
	if cells == nil {
		return
	}
	cloned := make([]Cell, len(cells))
	copy(cloned, cells)
	v.mainScreen.SetLine(lineIdx, cloned)
	if v.mainScreenPersistence != nil {
		v.mainScreenPersistence.NotifyWrite(lineIdx)
	}
	v.MarkAllDirty()
}

// lineHasSparseContent returns true if any cell in the slice has non-zero content.
func lineHasSparseContent(cells []Cell) bool {
	for _, c := range cells {
		if c.Rune != 0 {
			return true
		}
	}
	return false
}

// ScrollToLiveEdge scrolls the viewport to show the most recent content.
func (v *VTerm) ScrollToLiveEdge() {
	v.mainScreenScrollToBottom()
	v.MarkAllDirty()
}

// EnsureLiveEdge scrolls to live edge if not already there.
// Used when user performs an action (typing, pasting) to ensure they see the result.
func (v *VTerm) EnsureLiveEdge() {
	if !v.AtLiveEdge() {
		v.ScrollToLiveEdge()
	}
}

// AtLiveEdge returns whether the viewport is at the live edge (showing most recent content).
func (v *VTerm) AtLiveEdge() bool {
	if v.mainScreen == nil {
		return true
	}
	return v.mainScreen.IsFollowing()
}

// RestoreViewport is the VTerm-level entry point for Plan B viewport-aware
// resume. No-op when mainScreen is nil (parser-only tests) or when currently
// in alt-screen mode (alt-screen preserves its own state; callers MUST NOT
// invoke this for alt-screen panes — see PaneViewportState.AltScreen).
func (v *VTerm) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	if v.mainScreen == nil {
		return
	}
	v.mainScreen.RestoreViewport(viewBottom, wrapSeg, autoFollow)
}

// ScrollOffset returns the number of lines scrolled back from the live edge.
// 0 means at live edge, positive means scrolled back into history.
func (v *VTerm) ScrollOffset() int64 {
	if v.mainScreen == nil {
		return 0
	}
	_, viewBottom := v.mainScreen.VisibleRange()
	writeBottom := v.mainScreen.WriteBottom()
	offset := writeBottom - viewBottom
	if offset < 0 {
		return 0
	}
	return offset
}

// SetScrollOffset sets the scroll offset from the live edge.
// 0 means at live edge, positive means scrolled back into history.
func (v *VTerm) SetScrollOffset(offset int64) {
	if v.mainScreen == nil {
		return
	}
	if offset <= 0 {
		v.mainScreen.ScrollToBottom()
	} else {
		// Compute the current offset and adjust.
		current := v.ScrollOffset()
		diff := offset - current
		if diff > 0 {
			v.mainScreen.ScrollUp(int(diff))
		} else if diff < 0 {
			v.mainScreen.ScrollDown(int(-diff))
		}
	}
	v.MarkAllDirty()
	v.notifyMainScreenMetadata()
}

// ScrollToGlobalLine scrolls the viewport to show the specified global line index
// at approximately the center of the viewport.
// Returns false if the line is out of range.
func (v *VTerm) ScrollToGlobalLine(globalLineIdx int64) bool {
	if v.mainScreen == nil {
		return false
	}
	contentEnd := v.mainScreen.ContentEnd()
	writeTop := v.mainScreen.WriteTop()
	if globalLineIdx < 0 || globalLineIdx >= contentEnd {
		return false
	}
	// Compute how far from the bottom this line is.
	// writeBottom is the last writable row (writeTop + height - 1).
	writeBottom := v.mainScreen.WriteBottom()
	linesFromBottom := writeBottom - globalLineIdx
	if linesFromBottom < 0 {
		linesFromBottom = 0
	}
	// We want the target line centered in the viewport.
	halfH := int64(v.height / 2)
	targetOffset := linesFromBottom - halfH
	if targetOffset < 0 {
		targetOffset = 0
	}
	// Also clamp so we don't scroll past the top of history.
	maxOffset := writeBottom - writeTop
	if targetOffset > maxOffset {
		targetOffset = maxOffset
	}
	current := v.ScrollOffset()
	diff := targetOffset - current
	if diff > 0 {
		v.mainScreen.ScrollUp(int(diff))
	} else if diff < 0 {
		v.mainScreen.ScrollDown(int(-diff))
	}
	v.MarkAllDirty()
	return true
}

// mainScreenEraseCharacters erases n characters starting at the cursor position.
func (v *VTerm) mainScreenEraseCharacters(n int) {
	if v.mainScreen == nil {
		return
	}
	globalLine := v.cursorGlobalIdx()
	endCol := v.cursorX + n
	if endCol > v.width {
		endCol = v.width
	}
	cells := v.mainScreen.ReadLine(globalLine)
	if cells == nil {
		cells = make([]Cell, v.width)
	}
	blankCell := Cell{Rune: ' ', FG: v.currentFG, BG: v.currentBG}
	for col := v.cursorX; col < endCol; col++ {
		for len(cells) <= col {
			cells = append(cells, Cell{Rune: ' '})
		}
		cells[col] = blankCell
	}
	v.mainScreen.SetLine(globalLine, cells)
	if v.mainScreenPersistence != nil {
		v.mainScreenPersistence.NotifyWrite(globalLine)
	}
}

// mainScreenScrollColumnsUp scrolls content up within column margins using sparse main screen.
func (v *VTerm) mainScreenScrollColumnsUp(top, bottom, left, right, n int, fg, bg Color) {
	if v.mainScreen == nil {
		return
	}
	writeTop := v.mainScreen.WriteTop()
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}

	// Shift content up within the specified column range.
	for y := top; y <= bottom-n; y++ {
		srcY := y + n
		if srcY <= bottom {
			srcLine := v.mainScreen.ReadLine(writeTop + int64(srcY))
			dstCells := v.mainScreen.ReadLine(writeTop + int64(y))
			if dstCells == nil {
				dstCells = make([]Cell, v.width)
			}
			// Copy column range from src into dst.
			if srcLine != nil {
				for x := left; x <= right && x < len(srcLine); x++ {
					for len(dstCells) <= x {
						dstCells = append(dstCells, Cell{Rune: ' '})
					}
					dstCells[x] = srcLine[x]
				}
			} else {
				for x := left; x <= right; x++ {
					for len(dstCells) <= x {
						dstCells = append(dstCells, Cell{Rune: ' '})
					}
					dstCells[x] = blankCell
				}
			}
			v.mainScreen.SetLine(writeTop+int64(y), dstCells)
		}
	}

	// Clear the bottom n lines' margin regions.
	clearStart := bottom - n + 1
	if clearStart < top {
		clearStart = top
	}
	for y := clearStart; y <= bottom; y++ {
		dstCells := v.mainScreen.ReadLine(writeTop + int64(y))
		if dstCells == nil {
			dstCells = make([]Cell, v.width)
		}
		for x := left; x <= right; x++ {
			for len(dstCells) <= x {
				dstCells = append(dstCells, Cell{Rune: ' '})
			}
			dstCells[x] = blankCell
		}
		v.mainScreen.SetLine(writeTop+int64(y), dstCells)
	}
}

// mainScreenScrollColumnsDown scrolls content down within column margins using sparse main screen.
func (v *VTerm) mainScreenScrollColumnsDown(top, bottom, left, right, n int, fg, bg Color) {
	if v.mainScreen == nil {
		return
	}
	writeTop := v.mainScreen.WriteTop()
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}

	// Shift content down within the specified column range.
	for y := bottom; y >= top+n; y-- {
		srcY := y - n
		if srcY >= top {
			srcLine := v.mainScreen.ReadLine(writeTop + int64(srcY))
			dstCells := v.mainScreen.ReadLine(writeTop + int64(y))
			if dstCells == nil {
				dstCells = make([]Cell, v.width)
			}
			if srcLine != nil {
				for x := left; x <= right && x < len(srcLine); x++ {
					for len(dstCells) <= x {
						dstCells = append(dstCells, Cell{Rune: ' '})
					}
					dstCells[x] = srcLine[x]
				}
			} else {
				for x := left; x <= right; x++ {
					for len(dstCells) <= x {
						dstCells = append(dstCells, Cell{Rune: ' '})
					}
					dstCells[x] = blankCell
				}
			}
			v.mainScreen.SetLine(writeTop+int64(y), dstCells)
		}
	}

	// Clear the top n lines' margin regions.
	clearEnd := top + n - 1
	if clearEnd > bottom {
		clearEnd = bottom
	}
	for y := top; y <= clearEnd; y++ {
		dstCells := v.mainScreen.ReadLine(writeTop + int64(y))
		if dstCells == nil {
			dstCells = make([]Cell, v.width)
		}
		for x := left; x <= right; x++ {
			for len(dstCells) <= x {
				dstCells = append(dstCells, Cell{Rune: ' '})
			}
			dstCells[x] = blankCell
		}
		v.mainScreen.SetLine(writeTop+int64(y), dstCells)
	}
}

// mainScreenScrollColumnsHorizontal scrolls content horizontally within margins using sparse main screen.
func (v *VTerm) mainScreenScrollColumnsHorizontal(top, bottom, left, right, n int, fg, bg Color) {
	if v.mainScreen == nil {
		return
	}
	writeTop := v.mainScreen.WriteTop()
	blankCell := Cell{Rune: ' ', FG: fg, BG: bg}

	for y := top; y <= bottom; y++ {
		cells := v.mainScreen.ReadLine(writeTop + int64(y))
		if cells == nil {
			cells = make([]Cell, v.width)
		}
		// Ensure capacity for right+1 cells.
		for len(cells) <= right {
			cells = append(cells, Cell{Rune: ' '})
		}

		if n > 0 {
			// Scroll right: shift content right, insert blanks at left.
			for x := right; x >= left+n; x-- {
				srcX := x - n
				if srcX >= left {
					cells[x] = cells[srcX]
				}
			}
			for x := left; x < left+n && x <= right; x++ {
				cells[x] = blankCell
			}
		} else if n < 0 {
			// Scroll left: shift content left, insert blanks at right.
			absN := -n
			for x := left; x <= right-absN; x++ {
				srcX := x + absN
				if srcX <= right {
					cells[x] = cells[srcX]
				}
			}
			for x := right - absN + 1; x <= right; x++ {
				if x >= left {
					cells[x] = blankCell
				}
			}
		}
		v.mainScreen.SetLine(writeTop+int64(y), cells)
	}
}

// applySearchHighlight modifies the grid cells to highlight search matches.
func (v *VTerm) applySearchHighlight(grid [][]Cell) {
	termRunes := []rune(strings.ToLower(v.searchHighlight))
	termLen := len(termRunes)
	if termLen == 0 {
		return
	}

	hasStyledHighlight := v.searchSelectionColor.Mode != 0 || v.searchAccentColor.Mode != 0
	hasLineTint := v.searchLineTintColor.Mode != 0 && v.searchLineTintIntensity > 0

	type cellPos struct {
		y, x int
	}
	var allRunes []rune
	var positions []cellPos

	for y, row := range grid {
		for x, cell := range row {
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			allRunes = append(allRunes, unicode.ToLower(r))
			positions = append(positions, cellPos{y, x})
		}
	}

	type match struct {
		start      int
		isSelected bool
	}
	var matches []match
	selectedRows := make(map[int]bool)

	// Compute visible top for viewport-to-global mapping.
	var visibleTop int64
	if v.mainScreen != nil {
		visibleTop, _ = v.mainScreen.VisibleRange()
	}

	for i := 0; i <= len(allRunes)-termLen; i++ {
		found := true
		for j := range termLen {
			if allRunes[i+j] != termRunes[j] {
				found = false
				break
			}
		}
		if found {
			isSelected := false
			if hasStyledHighlight && v.searchHighlightLine >= 0 {
				pos := positions[i]
				globalLine := visibleTop + int64(pos.y)
				if globalLine == v.searchHighlightLine {
					isSelected = true
					if hasLineTint {
						for j := range termLen {
							selectedRows[positions[i+j].y] = true
						}
					}
				}
			}
			matches = append(matches, match{start: i, isSelected: isSelected})
		}
	}

	if hasLineTint && len(selectedRows) > 0 {
		for y := range selectedRows {
			if y >= 0 && y < len(grid) {
				for x := range grid[y] {
					cell := &grid[y][x]
					cell.BG = BlendColor(cell.BG, v.searchLineTintColor, v.searchLineTintIntensity, v.searchDefaultBG)
				}
			}
		}
	}

	for _, m := range matches {
		for j := range termLen {
			pos := positions[m.start+j]
			cell := &grid[pos.y][pos.x]
			if hasStyledHighlight {
				if m.isSelected {
					cell.FG = v.searchSelectionColor
				} else {
					cell.FG = v.searchAccentColor
				}
				cell.Attr |= AttrReverse
			} else {
				cell.Attr ^= AttrReverse
			}
		}
	}
}

// SetOnLineIndexed is a no-op in the sparse path. The sparse model does not
// maintain a separate per-line indexing callback. Callers should use
// OnLineCommit instead.
func (v *VTerm) SetOnLineIndexed(_ func(lineIdx int64, line *LogicalLine, timestamp time.Time, isCommand bool)) {
	// No-op: sparse model uses OnLineCommit for line-commit callbacks.
}

// CurrentLineCells returns the cells of the current cursor line.
func (v *VTerm) CurrentLineCells() []Cell {
	if v.inAltScreen {
		if v.cursorY >= 0 && v.cursorY < len(v.altBuffer) {
			return v.altBuffer[v.cursorY]
		}
		return nil
	}
	if v.mainScreen == nil {
		return nil
	}
	return v.mainScreen.ReadLine(v.cursorGlobalIdx())
}

// markLineWrapped sets the Wrapped flag on the last cell of the current cursor
// line in the sparse terminal.  This allows the viewport builder to join wrapped
// physical lines back into their logical counterpart on resize.
func (v *VTerm) markLineWrapped() {
	if v.mainScreen == nil {
		return
	}
	globalIdx := v.cursorGlobalIdx()
	cells := v.mainScreen.ReadLine(globalIdx)
	if len(cells) == 0 {
		return
	}
	cells[len(cells)-1].Wrapped = true
	v.mainScreen.SetLine(globalIdx, cells)
}

// SetSearchHighlight sets the search term to highlight with reversed colors.
func (v *VTerm) SetSearchHighlight(term string) {
	v.searchHighlight = term
	v.searchHighlightLine = -1
	v.MarkAllDirty()
}

// SetSearchHighlightStyled sets up styled search highlighting.
func (v *VTerm) SetSearchHighlightStyled(term string, currentLine int64, selectionColor, accentColor, lineTintColor Color, lineTintIntensity float32, defaultBG Color) {
	v.searchHighlight = term
	v.searchHighlightLine = currentLine
	v.searchSelectionColor = selectionColor
	v.searchAccentColor = accentColor
	v.searchLineTintColor = lineTintColor
	v.searchLineTintIntensity = lineTintIntensity
	v.searchDefaultBG = defaultBG
	v.MarkAllDirty()
}

// UpdateSearchHighlightLine updates just the current line for styled highlighting.
func (v *VTerm) UpdateSearchHighlightLine(currentLine int64) {
	v.searchHighlightLine = currentLine
	v.MarkAllDirty()
}

// ClearSearchHighlight removes search term highlighting.
func (v *VTerm) ClearSearchHighlight() {
	v.searchHighlight = ""
	v.searchHighlightLine = -1
	v.searchLineTintColor = Color{}
	v.searchLineTintIntensity = 0
	v.searchDefaultBG = Color{}
	v.MarkAllDirty()
}

// GlobalOffset returns the global index of the oldest available line.
func (v *VTerm) GlobalOffset() int64 {
	if v.mainScreen == nil {
		return 0
	}
	// The oldest line is at the beginning of history (global index 0),
	// but for range purposes return WriteTop so callers see live content.
	return 0
}

// GlobalEnd returns the global index just past the last written line.
func (v *VTerm) GlobalEnd() int64 {
	if v.mainScreen == nil {
		return 0
	}
	return v.mainScreen.ContentEnd()
}

// LastPromptLine returns the global line index of the last shell prompt,
// tracked via OSC 133;A. Returns -1 if unknown.
func (v *VTerm) LastPromptLine() int64 {
	return v.PromptStartGlobalLine
}

// LastPromptHeight returns the number of rows spanned by the last prompt,
// derived from OSC 133 anchors: InputStart - PromptStart + 1. Defaults to 1
// when anchors are unknown or inconsistent.
func (v *VTerm) LastPromptHeight() int {
	if v.PromptStartGlobalLine >= 0 && v.InputStartGlobalLine >= v.PromptStartGlobalLine {
		return int(v.InputStartGlobalLine-v.PromptStartGlobalLine) + 1
	}
	return 1
}

// RepositionForPromptOverwrite positions the cursor at `promptLine` column 0
// so a freshly-spawned shell's first prompt overwrites the stored prompt 1:1
// instead of rendering on the row where the previous session left off. When
// `promptLine` is within the current viewport, the cursor is moved in place.
// When it's outside the viewport (either above due to history-cap, OR below
// because the vterm hasn't yet been resized to the pane's full height at
// recovery time), writeTop is repositioned so the prompt line lands at row
// 0 of the visible area. No-op if the main screen is absent or `promptLine`
// is unknown (<0).
func (v *VTerm) RepositionForPromptOverwrite(promptLine int64) {
	if v.mainScreen == nil || promptLine < 0 {
		return
	}
	curTop := v.mainScreen.WriteTop()
	var targetRow int
	switch {
	case promptLine < curTop:
		v.mainScreen.RewindWriteTop(promptLine)
		targetRow = 0
	case promptLine >= curTop+int64(v.height):
		// Prompt sits past the viewport bottom — common when the vterm
		// is still at its pre-resize default size at recovery time.
		// Rewind so the new shell's first prompt lands at row 0 instead
		// of wherever the cursor happened to be (which would create the
		// "duplicated prompt" visual on first start).
		v.mainScreen.RewindWriteTop(promptLine)
		targetRow = 0
	default:
		targetRow = int(promptLine - curTop)
	}
	v.cursorX = 0
	v.cursorY = targetRow
	v.wrapNext = false
	v.MarkAllDirty()
}

// sparseLineStoreAdapter implements LineStore using MainScreen.
type sparseLineStoreAdapter struct {
	tm MainScreen
}

func (a *sparseLineStoreAdapter) GetLine(globalIdx int64) *LogicalLine {
	cells := a.tm.ReadLine(globalIdx)
	if cells == nil {
		return nil
	}
	return &LogicalLine{Cells: cells, NoWrap: a.tm.RowNoWrap(globalIdx)}
}

func (a *sparseLineStoreAdapter) ClearDirty(globalIdx int64) {
	// sparse store has no dirty tracking; no-op
}

func (a *sparseLineStoreAdapter) SetPreEvictCallback(cb func([]EvictedLine)) {
	// sparse store does not evict; no-op
}
