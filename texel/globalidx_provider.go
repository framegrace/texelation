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
