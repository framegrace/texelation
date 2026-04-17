// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package parser_test

import (
	"testing"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	_ "github.com/framegrace/texelation/apps/texelterm/parser/sparse"
)

// feedBytes feeds raw bytes through the parser, one rune at a time.
func feedBytes(p *parser.Parser, data []byte) {
	for _, b := range string(data) {
		p.Parse(b)
	}
}

// TestVTerm_DECSTBM_MarksRowNoWrap verifies that cells written while DECSTBM
// has non-default margins are marked NoWrap in the sparse store, and that cells
// written with default margins are not.
func TestVTerm_DECSTBM_MarksRowNoWrap(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	// Write with default (full-screen) margins — should NOT be NoWrap.
	feedBytes(p, []byte("hello"))
	cursorGI, _ := v.CursorGlobalIdx()
	if v.MainScreenRowNoWrap(cursorGI) {
		t.Fatalf("with default margins, row should not be NoWrap")
	}

	// Set non-default scroll region: rows 2–5 (1-indexed), then write a cell.
	feedBytes(p, []byte("\x1b[2;5r")) // non-default scroll region
	feedBytes(p, []byte("\x1b[HX"))   // home within region, write X

	cursorGI, _ = v.CursorGlobalIdx()
	if !v.MainScreenRowNoWrap(cursorGI) {
		t.Errorf("after DECSTBM [2;5r, written row should be NoWrap")
	}
}

// TestVTerm_DECSTBM_Reset_ClearsActive verifies that resetting DECSTBM to
// full-screen margins causes subsequent writes to NOT be marked NoWrap.
func TestVTerm_DECSTBM_Reset_ClearsActive(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	feedBytes(p, []byte("\x1b[2;5r")) // activate non-default region
	feedBytes(p, []byte("\x1b[r"))    // reset to full-screen region
	feedBytes(p, []byte("\x1b[HY"))   // home, write Y
	cursorGI, _ := v.CursorGlobalIdx()
	if v.MainScreenRowNoWrap(cursorGI) {
		t.Errorf("after DECSTBM reset, subsequent writes should not be NoWrap")
	}
}

// TestVTerm_DECSTBM_Resize_ClearsActive verifies that Resize resets
// decstbmActive so writes after resize are not spuriously marked NoWrap.
func TestVTerm_DECSTBM_Resize_ClearsActive(t *testing.T) {
	v := parser.NewVTerm(80, 24)
	v.EnableMemoryBuffer()
	p := parser.NewParser(v)

	feedBytes(p, []byte("\x1b[2;5r")) // activate non-default region

	// Resize clears margins per VT spec.
	v.Resize(80, 25)

	feedBytes(p, []byte("\x1b[HZ")) // home, write Z
	cursorGI, _ := v.CursorGlobalIdx()
	if v.MainScreenRowNoWrap(cursorGI) {
		t.Errorf("after Resize, decstbmActive should be cleared; writes should not be NoWrap")
	}
}
