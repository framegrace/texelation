// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Snapshot-derived pane activity metrics for cross-restart session
// metadata (Plan D2 / Plan F). Pure helpers — no I/O, no mutation.

package server

import "github.com/framegrace/texelation/protocol"

// paneActivityFromSnapshot extracts the session-level pane activity
// fields (PaneCount, FirstPaneTitle) for Plan F's session-discovery
// picker. Pure function over the protocol shape; safe to call from the
// connection handler's snapshot-emit hot path.
func paneActivityFromSnapshot(snap protocol.TreeSnapshot) (paneCount int, firstTitle string) {
	paneCount = len(snap.Panes)
	if paneCount > 0 {
		firstTitle = snap.Panes[0].Title
	}
	return
}
