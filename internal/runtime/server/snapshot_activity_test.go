// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"testing"

	"github.com/framegrace/texelation/protocol"
)

func TestPaneActivityFromSnapshot_Empty(t *testing.T) {
	count, title := paneActivityFromSnapshot(protocol.TreeSnapshot{})
	if count != 0 || title != "" {
		t.Fatalf("empty snapshot: got count=%d title=%q", count, title)
	}
}

func TestPaneActivityFromSnapshot_PicksFirstTitle(t *testing.T) {
	snap := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{
			{Title: "bash"},
			{Title: "vim"},
			{Title: "logs"},
		},
	}
	count, title := paneActivityFromSnapshot(snap)
	if count != 3 {
		t.Fatalf("count: got %d want 3", count)
	}
	if title != "bash" {
		t.Fatalf("title: got %q want bash", title)
	}
}

func TestPaneActivityFromSnapshot_TitleMayBeEmpty(t *testing.T) {
	snap := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{{Title: ""}}}
	count, title := paneActivityFromSnapshot(snap)
	if count != 1 || title != "" {
		t.Fatalf("got count=%d title=%q want 1, empty", count, title)
	}
}
