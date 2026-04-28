// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"testing"

	"github.com/framegrace/texelui/color"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/protocol"
)

func TestBlendColor(t *testing.T) {
	tests := []struct {
		name      string
		base      tcell.Color
		overlay   tcell.Color
		intensity float32
		wantR     int32
		wantG     int32
		wantB     int32
	}{
		{
			name:      "zero intensity returns base",
			base:      tcell.NewRGBColor(255, 0, 0), // Red
			overlay:   tcell.NewRGBColor(0, 0, 255), // Blue
			intensity: 0.0,
			wantR:     255,
			wantG:     0,
			wantB:     0,
		},
		{
			name:      "full intensity returns overlay",
			base:      tcell.NewRGBColor(255, 0, 0), // Red
			overlay:   tcell.NewRGBColor(0, 0, 255), // Blue
			intensity: 1.0,
			wantR:     0,
			wantG:     0,
			wantB:     255,
		},
		{
			name:      "half intensity blends colors",
			base:      tcell.NewRGBColor(255, 0, 0), // Red
			overlay:   tcell.NewRGBColor(0, 0, 255), // Blue
			intensity: 0.5,
			wantR:     127, // (255*0.5 + 0*0.5)
			wantG:     0,
			wantB:     127, // (0*0.5 + 255*0.5)
		},
		{
			name:      "blend white and black at 25%",
			base:      tcell.NewRGBColor(0, 0, 0),       // Black
			overlay:   tcell.NewRGBColor(255, 255, 255), // White
			intensity: 0.25,
			wantR:     63, // (0*0.75 + 255*0.25)
			wantG:     63,
			wantB:     63,
		},
		{
			name:      "blend gray shades",
			base:      tcell.NewRGBColor(100, 100, 100),
			overlay:   tcell.NewRGBColor(200, 200, 200),
			intensity: 0.5,
			wantR:     150, // (100*0.5 + 200*0.5)
			wantG:     150,
			wantB:     150,
		},
		{
			name:      "invalid overlay returns base",
			base:      tcell.NewRGBColor(255, 128, 64),
			overlay:   tcell.ColorDefault, // Invalid
			intensity: 0.5,
			wantR:     255,
			wantG:     128,
			wantB:     64,
		},
		{
			name:      "invalid base returns overlay",
			base:      tcell.ColorDefault, // Invalid
			overlay:   tcell.NewRGBColor(100, 150, 200),
			intensity: 0.5,
			wantR:     100,
			wantG:     150,
			wantB:     200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := blendColor(tt.base, tt.overlay, tt.intensity)
			r, g, b := result.RGB()

			if r != tt.wantR || g != tt.wantG || b != tt.wantB {
				t.Errorf("blendColor() RGB = (%d, %d, %d), want (%d, %d, %d)",
					r, g, b, tt.wantR, tt.wantG, tt.wantB)
			}
		})
	}
}

func TestApplyZoomOverlay(t *testing.T) {
	state := &clientState{
		defaultFg: tcell.NewRGBColor(255, 255, 255), // White
		defaultBg: tcell.NewRGBColor(0, 0, 0),       // Black
		desktopBg: tcell.NewRGBColor(32, 32, 32),    // Dark gray
	}

	t.Run("zero intensity returns original style", func(t *testing.T) {
		original := tcell.StyleDefault.
			Foreground(tcell.NewRGBColor(200, 100, 50)).
			Background(tcell.NewRGBColor(10, 20, 30))

		result := applyZoomOverlay(original, 0.0, state)

		if result != original {
			t.Error("zero intensity should return original style unchanged")
		}
	})

	t.Run("applies zoom overlay with intensity", func(t *testing.T) {
		original := tcell.StyleDefault.
			Foreground(tcell.NewRGBColor(200, 100, 50)).
			Background(tcell.NewRGBColor(10, 20, 30))

		result := applyZoomOverlay(original, 0.2, state)

		fg, bg, attrs := result.Decompose()

		// Should blend with outline color (120, 200, 255)
		fgR, fgG, fgB := fg.RGB()
		if fgR == 200 && fgG == 100 && fgB == 50 {
			t.Error("foreground should be blended, but appears unchanged")
		}

		bgR, bgG, bgB := bg.RGB()
		if bgR == 10 && bgG == 20 && bgB == 30 {
			t.Error("background should be blended, but appears unchanged")
		}

		// Should set bold
		if attrs&tcell.AttrBold == 0 {
			t.Error("zoom overlay should set bold attribute")
		}
	})

	t.Run("uses default colors when style has invalid colors", func(t *testing.T) {
		original := tcell.StyleDefault

		result := applyZoomOverlay(original, 0.2, state)

		fg, _, _ := result.Decompose()

		// Should use state.defaultFg (white)
		fgR, fgG, fgB := fg.RGB()
		if fgR == 255 && fgG == 255 && fgB == 255 {
			// Should be blended with outline, not pure white
			t.Error("foreground should be blended with outline")
		}
	})

	t.Run("preserves underline attribute", func(t *testing.T) {
		original := tcell.StyleDefault.Underline(true)

		result := applyZoomOverlay(original, 0.2, state)

		_, _, attrs := result.Decompose()

		if attrs&tcell.AttrUnderline == 0 {
			t.Error("underline attribute should be preserved")
		}
	})

	t.Run("preserves italic attribute", func(t *testing.T) {
		original := tcell.StyleDefault.Italic(true)

		result := applyZoomOverlay(original, 0.2, state)

		_, _, attrs := result.Decompose()

		if attrs&tcell.AttrItalic == 0 {
			t.Error("italic attribute should be preserved")
		}
	})
}

// TestDynamicColorFullPipeline exercises the complete path:
// server-side Pulse → Cell with DynBG → protocol encode/decode → client BufferCache → compositeInto resolution.
func TestDynamicColorFullPipeline(t *testing.T) {
	baseColor := tcell.NewRGBColor(137, 180, 250) // Catppuccin blue
	pulse := color.Pulse(baseColor, 0.7, 1.0, 6)

	// Step 1: Simulate what the Painter does — resolve static + store descriptor
	ctx := color.ColorContext{}
	staticBG := pulse.Resolve(ctx)
	desc := pulse.Describe()
	if desc.Type != color.DescTypePulse {
		t.Fatalf("expected DescTypePulse, got %d", desc.Type)
	}

	// Step 2: Simulate what the Publisher does — build a StyleEntry with dynamic desc
	_, bgVal := convertColorForTest(staticBG)
	styleEntry := protocol.StyleEntry{
		AttrFlags: protocol.AttrHasDynamic,
		FgModel:   protocol.ColorModelRGB,
		FgValue:   0xCDD6F4, // white-ish fg
		BgModel:   protocol.ColorModelRGB,
		BgValue:   bgVal,
		DynBG: protocol.DynColorDesc{
			Type: desc.Type, Base: desc.Base, Target: desc.Target,
			Easing: desc.Easing, Speed: desc.Speed, Min: desc.Min, Max: desc.Max,
		},
	}

	// Step 3: Protocol round-trip
	delta := protocol.BufferDelta{
		PaneID:   [16]byte{1, 2, 3},
		Revision: 1,
		Styles:   []protocol.StyleEntry{styleEntry},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "Hello", StyleIndex: 0}}},
		},
	}
	encoded, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := protocol.DecodeBufferDelta(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify dynamic descriptor survived the round-trip
	if decoded.Styles[0].DynBG.Type != color.DescTypePulse {
		t.Fatalf("DynBG type lost in round-trip: got %d", decoded.Styles[0].DynBG.Type)
	}
	if decoded.Styles[0].DynBG.Speed != 6 {
		t.Fatalf("DynBG speed lost: got %f", decoded.Styles[0].DynBG.Speed)
	}

	// Step 4: Apply to BufferCache (simulates client receiving delta)
	cache := client.NewBufferCache()
	// Set up pane geometry via snapshot
	cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: decoded.PaneID,
			X:      0, Y: 0, Width: 10, Height: 1,
		}},
	})
	cache.ApplyDelta(decoded)
	pane := cache.PaneByID(decoded.PaneID)

	cells := pane.RowCells(0)
	if len(cells) < 5 {
		t.Fatalf("expected at least 5 cells, got %d", len(cells))
	}
	if cells[0].DynBG.Type != color.DescTypePulse {
		t.Fatalf("DynBG not stored on client cell: type=%d", cells[0].DynBG.Type)
	}

	// Step 5: compositeInto resolves dynamic colors
	state := &clientState{
		defaultStyle: tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack),
		defaultFg:    tcell.ColorWhite,
		defaultBg:    tcell.ColorBlack,
		tickAccum:    0.5, // pretend 500ms has passed
	}

	workspaceBuf := make([][]client.Cell, 1)
	workspaceBuf[0] = make([]client.Cell, 10)
	for i := range workspaceBuf[0] {
		workspaceBuf[0][i] = client.Cell{Ch: ' ', Style: state.defaultStyle}
	}

	hasDynamic := compositeInto(workspaceBuf, []*client.PaneState{pane}, state, 10, 1)
	if !hasDynamic {
		t.Error("compositeInto should report hasDynamic=true")
	}

	// The resolved BG should differ from the static base color
	// because the pulse modulates brightness over time.
	_, resolvedBG, _ := workspaceBuf[0][0].Style.Decompose()
	resolvedR, resolvedG, resolvedB := resolvedBG.RGB()
	baseR, baseG, baseB := baseColor.RGB()

	t.Logf("Base: (%d,%d,%d), Resolved: (%d,%d,%d)", baseR, baseG, baseB, resolvedR, resolvedG, resolvedB)

	// The pulse oscillates brightness 0.7–1.0. At t=0.5s with speed=6 Hz,
	// sin(0.5*6) ≈ sin(3) ≈ 0.14, so factor ≈ 0.85 + 0.15*0.14 ≈ 0.87.
	// The resolved color should be dimmer than the base.
	if resolvedR == baseR && resolvedG == baseG && resolvedB == baseB {
		t.Error("resolved BG should differ from base due to pulse modulation")
	}

	// Verify it's a valid dimmed version (not black or garbage)
	if resolvedR < 0 || resolvedG < 0 || resolvedB < 0 {
		t.Error("resolved color has negative components")
	}
	if resolvedR > 255 || resolvedG > 255 || resolvedB > 255 {
		t.Error("resolved color exceeds 255")
	}
}

// TestDynamicColorStaticCellsUnaffected verifies that cells without dynamic
// descriptors pass through compositeInto without modification.
func TestDynamicColorStaticCellsUnaffected(t *testing.T) {
	paneID := [16]byte{5}
	cache := client.NewBufferCache()
	cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: paneID,
			X:      0, Y: 0, Width: 5, Height: 1,
		}},
	})
	delta := protocol.BufferDelta{
		PaneID:   paneID,
		Revision: 1,
		Styles: []protocol.StyleEntry{
			{FgModel: protocol.ColorModelRGB, FgValue: 0xFF0000, BgModel: protocol.ColorModelRGB, BgValue: 0x00FF00},
		},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "AB", StyleIndex: 0}}},
		},
	}
	cache.ApplyDelta(delta)
	pane := cache.PaneByID(delta.PaneID)

	state := &clientState{
		defaultStyle: tcell.StyleDefault,
	}

	workspaceBuf := make([][]client.Cell, 1)
	workspaceBuf[0] = make([]client.Cell, 5)
	for i := range workspaceBuf[0] {
		workspaceBuf[0][i] = client.Cell{Ch: ' ', Style: state.defaultStyle}
	}

	hasDynamic := compositeInto(workspaceBuf, []*client.PaneState{pane}, state, 5, 1)
	if hasDynamic {
		t.Error("static cells should not report hasDynamic")
	}

	// Verify the cell style is exactly what we sent
	_, bg, _ := workspaceBuf[0][0].Style.Decompose()
	r, g, b := bg.RGB()
	if r != 0 || g != 255 || b != 0 {
		t.Errorf("static BG should be green: got (%d,%d,%d)", r, g, b)
	}
}

func convertColorForTest(c tcell.Color) (protocol.ColorModel, uint32) {
	r, g, b := c.RGB()
	return protocol.ColorModelRGB, (uint32(r)&0xFF)<<16 | (uint32(g)&0xFF)<<8 | uint32(b)&0xFF
}

func TestIncrementalComposite_SkipsCleanPanes(t *testing.T) {
	paneID := [16]byte{1}
	cache := client.NewBufferCache()
	cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: paneID, X: 0, Y: 0, Width: 10, Height: 2,
		}},
	})
	cache.ApplyDelta(protocol.BufferDelta{
		PaneID:   paneID,
		Revision: 1,
		Styles:   []protocol.StyleEntry{{FgModel: protocol.ColorModelRGB, FgValue: 0xFF0000}},
		Rows:     []protocol.RowDelta{{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "hello", StyleIndex: 0}}}},
	})

	state := &clientState{
		cache:        cache,
		defaultStyle: tcell.StyleDefault,
	}

	// Allocate buffers
	ensureBuffers(state, 10, 2)

	// Pane is dirty from delta — incremental composite should update it.
	pane := cache.PaneByID(paneID)
	if !pane.Dirty {
		t.Fatal("pane should be dirty after delta")
	}

	hasDyn := incrementalComposite(state, 10, 2)
	if hasDyn {
		t.Error("no animated cells, should not report hasDynamic")
	}

	// After composite, pane should be clean.
	if pane.Dirty {
		t.Error("pane should be clean after incrementalComposite")
	}

	// Verify cell was written to prevBuffer.
	if state.prevBuffer[0][0].Ch != 'h' {
		t.Errorf("expected 'h' at (0,0), got '%c'", state.prevBuffer[0][0].Ch)
	}

	// Now apply no new delta — pane stays clean.
	hasDyn = incrementalComposite(state, 10, 2)
	// prevBuffer should still have the old content (not cleared).
	if state.prevBuffer[0][0].Ch != 'h' {
		t.Errorf("clean pane should preserve prevBuffer content, got '%c'", state.prevBuffer[0][0].Ch)
	}
}

func TestBlendColorSymmetry(t *testing.T) {
	// Test that blending is consistent
	red := tcell.NewRGBColor(255, 0, 0)
	blue := tcell.NewRGBColor(0, 0, 255)

	// Blending red->blue at 0.3 should give same result as blue->red at 0.7
	blend1 := blendColor(red, blue, 0.3)
	blend2 := blendColor(blue, red, 0.7)

	r1, g1, b1 := blend1.RGB()
	r2, g2, b2 := blend2.RGB()

	if r1 != r2 || g1 != g2 || b1 != b2 {
		t.Errorf("Blending should be symmetric: (%d,%d,%d) != (%d,%d,%d)",
			r1, g1, b1, r2, g2, b2)
	}
}

// TestRowSourceForPane_DecorationLayer verifies the two-layer lookup:
// rowIdx outside [ContentTopRow, ContentTopRow+NumContentRows) reads
// from the decoration cache; rowIdx inside reads via gid from PaneCache.
// Issue #199 Task 11.
func TestRowSourceForPane_DecorationLayer(t *testing.T) {
	state := makeStateWithViewports()
	id := paneID(0xab)
	// Pane H=5, ContentTopRow=1, NumContentRows=3 (rowIdx 1..3 are content).
	state.cache.ApplySnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, NumContentRows: 3,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})
	// Initialise viewport tracker from the snapshot so paneViewportFor returns ok.
	state.onTreeSnapshot(protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, NumContentRows: 3,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	})

	// Apply a delta with one content row (gid 10) and two decoration rows.
	// onBufferDelta will set ViewTopIdx = 10 - (NumContentRows-1) = 10 - 2 = 8.
	delta := protocol.BufferDelta{
		PaneID:  id,
		RowBase: 10,
		Styles:  []protocol.StyleEntry{{}},
		Rows: []protocol.RowDelta{
			{Row: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "C", StyleIndex: 0}}},
		},
		DecorRows: []protocol.DecorRowDelta{
			{RowIdx: 0, Spans: []protocol.CellSpan{{StartCol: 0, Text: "T", StyleIndex: 0}}},
			{RowIdx: 4, Spans: []protocol.CellSpan{{StartCol: 0, Text: "B", StyleIndex: 0}}},
		},
	}
	state.cache.ApplyDelta(delta)
	state.onBufferDelta(delta)

	// Also feed the row into the PaneCache so the content lookup at gid 10 hits.
	state.paneCacheFor(id).ApplyDelta(delta)

	pane := state.cache.PaneByID(id)
	if pane == nil {
		t.Fatalf("pane missing")
	}

	// rowIdx 0 → decoration "T"
	if src := rowSourceForPane(state, pane, 0); len(src) == 0 || src[0].Ch != 'T' {
		t.Fatalf("rowIdx 0 expected decoration 'T', got %+v", src)
	}
	// rowIdx 4 → decoration "B"
	if src := rowSourceForPane(state, pane, 4); len(src) == 0 || src[0].Ch != 'B' {
		t.Fatalf("rowIdx 4 expected decoration 'B', got %+v", src)
	}
	// rowIdx 1 → gid = 8 + 0 = 8; PaneCache only has gid 10 → miss → nil
	if src := rowSourceForPane(state, pane, 1); src != nil {
		t.Fatalf("rowIdx 1 expected nil (content-layer miss for gid 8), got %+v", src)
	}
	// rowIdx 3 → gid = 8 + 2 = 10, present in PaneCache → "C"
	if src := rowSourceForPane(state, pane, 3); len(src) == 0 || src[0].Ch != 'C' {
		t.Fatalf("rowIdx 3 expected content 'C' (gid 10), got %+v", src)
	}
}

// TestRowSourceForPane_DecorationCacheMiss verifies that an empty
// decoration cache returns nil for a decoration-row lookup.
func TestRowSourceForPane_DecorationCacheMiss(t *testing.T) {
	state := makeStateWithViewports()
	id := paneID(0xab)
	snap := protocol.TreeSnapshot{
		Panes: []protocol.PaneSnapshot{{
			PaneID: id, Width: 4, Height: 5,
			ContentTopRow: 1, NumContentRows: 3,
		}},
		Root: protocol.TreeNodeSnapshot{PaneIndex: 0, Split: protocol.SplitNone},
	}
	state.cache.ApplySnapshot(snap)
	state.onTreeSnapshot(snap)
	pane := state.cache.PaneByID(id)
	if pane == nil {
		t.Fatalf("pane missing")
	}
	src := rowSourceForPane(state, pane, 0)
	if src != nil {
		t.Fatalf("expected nil for decoration miss, got %+v", src)
	}
}
