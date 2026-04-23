// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelterm

import (
	"github.com/framegrace/texelation/apps/texelterm/parser"
)

// testTerm holds a TexelTerm with a working VTerm (including the sparse main
// screen) and a Parser for feeding bytes, but no PTY. Intended for unit tests
// only — do not use from production code.
type testTerm struct {
	term   *TexelTerm
	parser *parser.Parser
}

// NewTestTerm constructs a TexelTerm with a working VTerm (including the
// sparse main screen) but no PTY. Intended for unit tests only — do not use
// from production code.
func NewTestTerm(width, height int) *testTerm {
	a := &TexelTerm{
		width:        width,
		height:       height,
		colorPalette: newDefaultPalette(),
	}
	a.vterm = parser.NewVTerm(width, height)
	a.vterm.EnableMemoryBuffer()
	p := parser.NewParser(a.vterm)
	return &testTerm{term: a, parser: p}
}

// Write feeds bytes to the VTerm through the Parser, simulating PTY input.
func (tt *testTerm) Write(b []byte) {
	for _, c := range string(b) {
		tt.parser.Parse(c)
	}
}

// RestoreViewport delegates to the underlying TexelTerm.
func (tt *testTerm) RestoreViewport(viewBottom int64, wrapSeg uint16, autoFollow bool) {
	tt.term.RestoreViewport(viewBottom, wrapSeg, autoFollow)
}
