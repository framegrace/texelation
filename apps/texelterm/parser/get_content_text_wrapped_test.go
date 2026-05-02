// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Regression: GetContentText must NOT insert \n between gids that are
// part of the same wrapped chain. A long logical line written when the
// terminal autowraps lands across multiple gids; the last cell of every
// non-tail gid carries Wrapped=true to mark the chain. Inserting \n
// between those gids splits one visual line into multiple clipboard
// lines, which is what the user sees as "wrong copy on wrapped lines."

package parser

import (
	"strings"
	"testing"
)

func TestGetContentText_WrappedChainJoinsWithoutNewline(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	// A logical line longer than 80 cols. The autowrap path puts the
	// first 80 chars on one gid (with Wrapped=true on the trailing
	// cell) and the rest on the next gid.
	p := NewParser(v)
	const long = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" + // 62
		"0123456789ABCDEFGHIJKLMNOPQRS" // 29 → 91 total → wraps at 80
	for _, r := range long {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')

	// Find the chain head gid by walking back from the cursor.
	cursorGI, _ := v.mainScreen.Cursor()
	tailGI := cursorGI - 1
	headGI := tailGI
	for headGI > 0 {
		prev := v.mainScreen.ReadLine(headGI - 1)
		if len(prev) == 0 || !prev[len(prev)-1].Wrapped {
			break
		}
		headGI--
	}
	if headGI == tailGI {
		t.Fatalf("expected a multi-gid wrapped chain; head=tail=%d", headGI)
	}

	// Capture from start of head to end of tail.
	tail := v.mainScreen.ReadLine(tailGI)
	got := v.GetContentText(headGI, 0, tailGI, len(tail))

	if strings.Contains(got, "\n") {
		t.Errorf("captured wrapped chain has spurious \\n in middle:\n%q", got)
	}
	if got != long {
		t.Errorf("captured text mismatch.\n got: %q\nwant: %q", got, long)
	}
}

// TestGetContentText_ChainBreakInsertsNewline guards the converse: two
// distinct logical lines (no Wrapped flag in between) must join with a
// single \n, exactly as a user would expect.
func TestGetContentText_ChainBreakInsertsNewline(t *testing.T) {
	v := NewVTerm(80, 24)
	v.EnableMemoryBuffer()

	p := NewParser(v)
	for _, r := range "first line" {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')
	for _, r := range "second line" {
		p.Parse(r)
	}
	p.Parse('\r')
	p.Parse('\n')

	cursorGI, _ := v.mainScreen.Cursor()
	tailGI := cursorGI - 1
	headGI := tailGI - 1

	got := v.GetContentText(headGI, 0, tailGI, len("second line"))
	want := "first line\nsecond line"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
