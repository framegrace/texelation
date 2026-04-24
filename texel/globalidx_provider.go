// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/globalidx_provider.go
// Summary: Optional App interface for per-row sparse-store globalIdx mapping.
// Usage: Used by capturePaneSnapshot to populate PaneSnapshot.RowGlobalIdx.
// Notes: Only terminal-like apps backed by a sparse store need to implement
//   this; everything else defaults to "no globalIdx" (-1).

package texel

// RowGlobalIdxProvider is optionally implemented by apps whose rendered
// content rows map 1:1 onto a main-screen sparse-store globalIdx. Returns
// a slice of length == len(app.Render()) where entry [y] is the globalIdx
// of row y of the app's last-rendered buffer, or -1 if that row has no
// main-screen globalIdx (e.g. alt-screen, status/scrollbar decorations).
type RowGlobalIdxProvider interface {
	RowGlobalIdx() []int64
}

// AltScreenProvider is optionally implemented by apps whose underlying
// terminal may switch into an alt-screen buffer (e.g. vim, less). Returns
// true when the app's rendered content does not correspond to main-screen
// scrollback. Publisher uses this to stamp BufferDelta.Flags with
// BufferDeltaAltScreen and to skip viewport clipping for such panes.
type AltScreenProvider interface {
	InAltScreen() bool
}

// ViewportRestorer is implemented by pane apps (notably texelterm) that
// support viewport-aware resume. Called by DesktopEngine.RestorePaneViewport
// on MsgResumeRequest to re-seat the app's scrollback view before the first
// post-resume render. Apps that don't implement this interface (statusbar,
// launcher, etc.) are skipped — they don't have scrollback.
type ViewportRestorer interface {
	RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool)
}
