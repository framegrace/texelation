// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Replay the captured Claude session from /tmp/claude.{bytes,events} to
// reproduce the horizontal-resize duplicate-banner bug (issue #48).
// Skipped unless the capture files are present so CI stays green.

package parser

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	captureBytesPath  = "/tmp/claude.bytes"
	captureEventsPath = "/tmp/claude.events"
)

type captureEvent struct {
	kind   string // "START" or "RESIZE"
	offset int64
	cols   int
	rows   int
}

func loadCaptureEvents(t *testing.T, path string) []captureEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("no capture events file %q: %v", path, err)
	}
	defer f.Close()
	var events []captureEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		ev := captureEvent{}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		ev.kind = fields[0]
		for _, f := range fields[1:] {
			kv := strings.SplitN(f, "=", 2)
			if len(kv) != 2 {
				continue
			}
			n, err := strconv.Atoi(kv[1])
			if err != nil {
				continue
			}
			switch kv[0] {
			case "offset":
				ev.offset = int64(n)
			case "cols":
				ev.cols = n
			case "rows":
				ev.rows = n
			}
		}
		events = append(events, ev)
	}
	return events
}

// TestClaudeReplay_HorizontalResizeDuplicates replays the captured Claude
// session and checks whether the "Claude" banner row appears in the store
// at more globalIdxes than expected. On real horizontal-drag repro the user
// reports "first 9 rows repeat per resize step"; a per-row count much greater
// than 1 is the mechanical signature.
func TestClaudeReplay_HorizontalResizeDuplicates(t *testing.T) {
	events := loadCaptureEvents(t, captureEventsPath)
	if len(events) == 0 {
		t.Skip("no events recorded")
	}
	data, err := os.ReadFile(captureBytesPath)
	if err != nil {
		t.Skipf("no capture bytes file: %v", err)
	}
	if events[0].kind != "START" {
		t.Fatalf("first event must be START, got %+v", events[0])
	}

	cols := events[0].cols
	rows := events[0].rows
	v := NewVTerm(cols, rows)
	p := NewParser(v)

	// Feed bytes up to each RESIZE offset, applying the resize, then continue.
	var fed int64
	for _, ev := range events[1:] {
		if ev.kind != "RESIZE" {
			continue
		}
		if ev.offset > int64(len(data)) {
			break
		}
		if ev.offset > fed {
			chunk := data[fed:ev.offset]
			parseString(p, string(chunk))
			fed = ev.offset
		}
		v.Resize(ev.cols, ev.rows)
	}
	if fed < int64(len(data)) {
		parseString(p, string(data[fed:]))
	}

	// Inspect the full sparse store for duplicate banner rows. The small
	// banner in the capture contains the literal "Claude" followed by
	// (via ESC[1C advance) "Code". After stripping styles, the rendered
	// row contains "Claude" and "Code" on the same line.
	lines := readAllSparseLines(v)

	claudeLines := 0
	type hit struct {
		gi   int
		text string
	}
	var hits []hit
	for gi, ln := range lines {
		if strings.Contains(ln, "Claude") && strings.Contains(ln, "Code") {
			claudeLines++
			hits = append(hits, hit{gi, ln})
		}
	}
	t.Logf("total rows in store: %d", len(lines))
	t.Logf("rows containing both 'Claude' and 'Code': %d", claudeLines)
	for i, h := range hits {
		if i >= 20 {
			t.Logf("... (%d more)", len(hits)-20)
			break
		}
		t.Logf("  [gi=%d] %q", h.gi, h.text)
	}

	// Also dump the rendered view grid.
	grid := v.Grid()
	t.Logf("rendered grid (%dx%d) after replay:", len(grid), cols)
	for i, row := range grid {
		t.Logf("[%02d] %s", i, trimRight(cellsToString(row)))
	}

	// The live banner is expected to appear on at most ~3 rows (banner
	// line + maybe a redraw-in-progress). More than that indicates
	// per-resize pollution in scrollback.
	if claudeLines > 3 {
		t.Errorf("banner 'Claude...Code' appears on %d store rows — duplicates in scrollback", claudeLines)
	}

	// Second check: count banner-marker rows per segment to see if they
	// cluster at regular globalIdx intervals (one cluster per resize).
	if len(hits) > 1 {
		gaps := []int{}
		for i := 1; i < len(hits); i++ {
			gaps = append(gaps, hits[i].gi-hits[i-1].gi)
		}
		t.Logf("gaps between banner rows: %v", gaps)
	}

	// Dump a sample of the broader scrollback context around each hit
	// so the log captures what the user actually sees when scrolling.
	for _, h := range hits[:min(5, len(hits))] {
		lo := h.gi - 2
		if lo < 0 {
			lo = 0
		}
		hi := h.gi + 8
		if hi > len(lines) {
			hi = len(lines)
		}
		t.Logf("-- context around gi=%d --", h.gi)
		for gi := lo; gi < hi; gi++ {
			t.Logf("  [gi=%d] %s", gi, lines[gi])
		}
	}

	_ = fmt.Sprint // keep import
}

// TestClaudeReplay_NoResizesBaseline feeds the exact same capture bytes
// but SKIPS all resize events. If the banner still appears on many rows,
// the duplication is caused by Claude's own per-frame repaints overflowing
// the window — not by resize. If it appears ~1×, the resize path is the
// mechanism.
func TestClaudeReplay_NoResizesBaseline(t *testing.T) {
	events := loadCaptureEvents(t, captureEventsPath)
	if len(events) == 0 {
		t.Skip("no events recorded")
	}
	data, err := os.ReadFile(captureBytesPath)
	if err != nil {
		t.Skipf("no capture bytes: %v", err)
	}
	cols := events[0].cols
	rows := events[0].rows
	v := NewVTerm(cols, rows)
	p := NewParser(v)
	parseString(p, string(data))

	lines := readAllSparseLines(v)
	claudeLines := 0
	for _, ln := range lines {
		if strings.Contains(ln, "Claude") && strings.Contains(ln, "Code") {
			claudeLines++
		}
	}
	t.Logf("no-resize baseline: rows=%d claude-banners=%d", len(lines), claudeLines)
}

// TestClaudeReplay_DebouncedResize models the ghostty-style behavior:
// coalesce rapid resize events and only apply the final size. If the bug
// is "52 SIGWINCHs flood causes 52 full repaints that each overflow",
// this should show near-zero scrollback duplicates.
func TestClaudeReplay_DebouncedResize(t *testing.T) {
	events := loadCaptureEvents(t, captureEventsPath)
	if len(events) == 0 {
		t.Skip("no events recorded")
	}
	data, err := os.ReadFile(captureBytesPath)
	if err != nil {
		t.Skipf("no capture bytes: %v", err)
	}
	cols := events[0].cols
	rows := events[0].rows
	v := NewVTerm(cols, rows)
	p := NewParser(v)
	// Feed all bytes first (Claude's own emits), then apply ONLY final resize.
	parseString(p, string(data))
	final := events[len(events)-1]
	if final.kind == "RESIZE" {
		v.Resize(final.cols, final.rows)
	}
	lines := readAllSparseLines(v)
	claudeLines := 0
	for _, ln := range lines {
		if strings.Contains(ln, "Claude") && strings.Contains(ln, "Code") {
			claudeLines++
		}
	}
	t.Logf("debounced-resize: rows=%d claude-banners=%d", len(lines), claudeLines)
}

// TestClaudeReplay_FirstFrameOnly feeds only the bytes before the very
// first resize — i.e. Claude's initial output with zero resize-triggered
// repaints. Shows how many banner rows a SINGLE repaint-free session
// produces, isolating overflow from repaint count.
func TestClaudeReplay_FirstFrameOnly(t *testing.T) {
	events := loadCaptureEvents(t, captureEventsPath)
	if len(events) < 2 {
		t.Skip("need at least START + one RESIZE")
	}
	data, err := os.ReadFile(captureBytesPath)
	if err != nil {
		t.Skipf("no capture bytes: %v", err)
	}
	firstResize := events[1]
	cols := events[0].cols
	rows := events[0].rows
	v := NewVTerm(cols, rows)
	p := NewParser(v)
	parseString(p, string(data[:firstResize.offset]))
	lines := readAllSparseLines(v)
	claudeLines := 0
	for _, ln := range lines {
		if strings.Contains(ln, "Claude") && strings.Contains(ln, "Code") {
			claudeLines++
		}
	}
	t.Logf("first-frame-only (bytes 0..%d): rows=%d claude-banners=%d",
		firstResize.offset, len(lines), claudeLines)
}

// TestClaudeReplay_TallWindow replays with the same resize events but
// forces rows=100 so Claude's per-frame content cannot overflow. If the
// bug is pure overflow, banner duplicates should not appear in scrollback.
func TestClaudeReplay_TallWindow(t *testing.T) {
	events := loadCaptureEvents(t, captureEventsPath)
	if len(events) == 0 {
		t.Skip("no events recorded")
	}
	data, err := os.ReadFile(captureBytesPath)
	if err != nil {
		t.Skipf("no capture bytes: %v", err)
	}
	const forcedRows = 100
	cols := events[0].cols
	v := NewVTerm(cols, forcedRows)
	p := NewParser(v)
	var fed int64
	for _, ev := range events[1:] {
		if ev.kind != "RESIZE" {
			continue
		}
		if ev.offset > int64(len(data)) {
			break
		}
		if ev.offset > fed {
			parseString(p, string(data[fed:ev.offset]))
			fed = ev.offset
		}
		v.Resize(ev.cols, forcedRows)
	}
	if fed < int64(len(data)) {
		parseString(p, string(data[fed:]))
	}
	lines := readAllSparseLines(v)
	claudeLines := 0
	for _, ln := range lines {
		if strings.Contains(ln, "Claude") && strings.Contains(ln, "Code") {
			claudeLines++
		}
	}
	t.Logf("tall-window(rows=%d): rows=%d claude-banners=%d", forcedRows, len(lines), claudeLines)
}
