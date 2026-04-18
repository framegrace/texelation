// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparse

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
)

func TestStore_RowNoWrap_DefaultFalse(t *testing.T) {
	s := NewStore(80)
	s.Set(5, 0, parser.Cell{Rune: 'x'})
	if s.RowNoWrap(5) {
		t.Errorf("new row should default NoWrap=false")
	}
	if s.RowNoWrap(99) {
		t.Errorf("missing row should report NoWrap=false")
	}
}

func TestStore_SetRowNoWrap_StickyOR(t *testing.T) {
	s := NewStore(80)
	s.Set(5, 0, parser.Cell{Rune: 'x'})
	s.SetRowNoWrap(5, true)
	if !s.RowNoWrap(5) {
		t.Errorf("after SetRowNoWrap(true), expected true")
	}
	// Sticky: setting false does not clear
	s.SetRowNoWrap(5, false)
	if !s.RowNoWrap(5) {
		t.Errorf("SetRowNoWrap(false) must not clear sticky flag")
	}
}

func TestStore_SetRowNoWrap_AutoCreateRow(t *testing.T) {
	s := NewStore(80)
	s.SetRowNoWrap(7, true)
	if !s.RowNoWrap(7) {
		t.Errorf("SetRowNoWrap on missing row should create row + set flag")
	}
	got := s.GetLine(7)
	if got == nil {
		t.Errorf("row should be created by SetRowNoWrap")
	}
}

func TestStore_SetLine_CarriesNoWrap(t *testing.T) {
	s := NewStore(80)
	s.Set(5, 0, parser.Cell{Rune: 'a'})
	s.SetRowNoWrap(5, true)
	s.SetLine(5, []parser.Cell{{Rune: 'b'}})
	if !s.RowNoWrap(5) {
		t.Errorf("SetLine must preserve NoWrap flag")
	}
}
