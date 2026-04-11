// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sparse provides a globalIdx-keyed sparse cell store for texelterm.
// Subsequent types in this package add the write-window and view-window
// cursors that replace the dense MemoryBuffer.
//
// See docs/superpowers/specs/2026-04-11-sparse-viewport-write-window-split-design.md.
package sparse
