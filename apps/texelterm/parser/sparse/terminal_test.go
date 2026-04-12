// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestTerminal_NewInitialState(t *testing.T) {
	tm := NewTerminal(80, 24)
	if got := tm.Width(); got != 80 {
		t.Errorf("Width = %d, want 80", got)
	}
	if got := tm.Height(); got != 24 {
		t.Errorf("Height = %d, want 24", got)
	}
	if !tm.IsFollowing() {
		t.Error("new Terminal should follow writeBottom")
	}
	if got := tm.ContentEnd(); got != -1 {
		t.Errorf("fresh ContentEnd = %d, want -1", got)
	}
	_ = parser.Cell{}
}
