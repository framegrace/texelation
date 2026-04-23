// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelterm

import (
	"testing"
)

func TestTerm_RestoreViewport_ForwardsToVTerm(t *testing.T) {
	tt := NewTestTerm(80, 24)
	if tt.term.vterm == nil {
		t.Skip("NewTestTerm did not produce a VTerm; revise helper")
	}
	// Write 100 lines.
	for i := 0; i < 100; i++ {
		tt.Write([]byte("line\r\n"))
	}
	if !tt.term.vterm.AtLiveEdge() {
		t.Fatalf("pre: AtLiveEdge want true")
	}
	tt.RestoreViewport(50, 0, false)
	if tt.term.vterm.AtLiveEdge() {
		t.Fatalf("post: AtLiveEdge want false after restore to scrollback")
	}
}
