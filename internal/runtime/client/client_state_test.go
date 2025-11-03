// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package clientruntime

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
)

func TestClientStateSetRenderChannel(t *testing.T) {
	state := &clientState{}
	ch := make(chan struct{}, 1)

	state.setRenderChannel(ch)

	if state.renderCh != ch {
		t.Error("setRenderChannel did not set the channel")
	}
}

func TestClientStateSetThemeValue(t *testing.T) {
	state := &clientState{}

	// Set a value
	state.setThemeValue("pane", "fg", "#ffffff")

	if state.themeValues == nil {
		t.Fatal("themeValues map was not initialized")
	}

	section, ok := state.themeValues["pane"]
	if !ok {
		t.Fatal("pane section was not created")
	}

	value, ok := section["fg"]
	if !ok {
		t.Fatal("fg key was not set")
	}

	if value != "#ffffff" {
		t.Errorf("value = %v, want %v", value, "#ffffff")
	}

	// Update existing value
	state.setThemeValue("pane", "fg", "#000000")
	if state.themeValues["pane"]["fg"] != "#000000" {
		t.Error("value was not updated")
	}

	// Add different section
	state.setThemeValue("desktop", "bg", "#ff0000")
	if state.themeValues["desktop"]["bg"] != "#ff0000" {
		t.Error("new section was not created")
	}
}

func TestClientStateUpdateTheme(t *testing.T) {
	state := &clientState{
		defaultFg: tcell.ColorWhite,
		defaultBg: tcell.ColorBlack,
	}

	// Update foreground color
	state.updateTheme("desktop", "default_fg", "#ff0000")

	if state.defaultFg == tcell.ColorWhite {
		t.Error("default fg color was not updated")
	}

	r, g, b := state.defaultFg.RGB()
	if r != 255 || g != 0 || b != 0 {
		t.Errorf("default fg RGB = (%d, %d, %d), want (255, 0, 0)", r, g, b)
	}

	// Update background color
	state.updateTheme("desktop", "default_bg", "#0000ff")

	if state.defaultBg == tcell.ColorBlack {
		t.Error("default bg color was not updated")
	}

	r, g, b = state.defaultBg.RGB()
	if r != 0 || g != 0 || b != 255 {
		t.Errorf("default bg RGB = (%d, %d, %d), want (0, 0, 255)", r, g, b)
	}

	// Verify desktopBg was also updated
	if state.desktopBg != state.defaultBg {
		t.Error("desktopBg was not updated with defaultBg")
	}
}

func TestClientStateRecomputeDefaultStyle(t *testing.T) {
	state := &clientState{
		defaultFg: tcell.NewRGBColor(255, 0, 0),
		defaultBg: tcell.NewRGBColor(0, 0, 255),
	}

	state.recomputeDefaultStyle()

	fg, bg, _ := state.defaultStyle.Decompose()

	fgR, fgG, fgB := fg.RGB()
	if fgR != 255 || fgG != 0 || fgB != 0 {
		t.Errorf("style fg RGB = (%d, %d, %d), want (255, 0, 0)", fgR, fgG, fgB)
	}

	bgR, bgG, bgB := bg.RGB()
	if bgR != 0 || bgG != 0 || bgB != 255 {
		t.Errorf("style bg RGB = (%d, %d, %d), want (0, 0, 255)", bgR, bgG, bgB)
	}
}

func TestClientStateApplyStateUpdate(t *testing.T) {
	state := &clientState{
		cache: client.NewBufferCache(),
	}

	update := protocol.StateUpdate{
		WorkspaceID:   2,
		AllWorkspaces: []int32{1, 2, 3},
		InControlMode: true,
		SubMode:       'h',
		ActiveTitle:   "test-title",
		DesktopBgRGB:  0xff5733, // Orange
		Zoomed:        true,
		ZoomedPaneID:  [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}

	state.applyStateUpdate(update)

	if state.workspaceID != 2 {
		t.Errorf("workspaceID = %d, want 2", state.workspaceID)
	}

	if len(state.workspaces) != 3 {
		t.Errorf("len(workspaces) = %d, want 3", len(state.workspaces))
	}

	if state.workspaces[0] != 1 || state.workspaces[1] != 2 || state.workspaces[2] != 3 {
		t.Errorf("workspaces = %v, want [1 2 3]", state.workspaces)
	}

	if !state.controlMode {
		t.Error("controlMode should be true")
	}

	if state.subMode != 'h' {
		t.Errorf("subMode = %q, want 'h'", state.subMode)
	}

	if state.activeTitle != "test-title" {
		t.Errorf("activeTitle = %q, want %q", state.activeTitle, "test-title")
	}

	r, g, b := state.desktopBg.RGB()
	if r != 255 || g != 87 || b != 51 {
		t.Errorf("desktopBg RGB = (%d, %d, %d), want (255, 87, 51)", r, g, b)
	}

	if !state.zoomed {
		t.Error("zoomed should be true")
	}

	if state.zoomedPane != update.ZoomedPaneID {
		t.Error("zoomedPane was not set correctly")
	}
}

func TestClientStateApplyStateUpdatePreservesCapacity(t *testing.T) {
	state := &clientState{
		cache:      client.NewBufferCache(),
		workspaces: make([]int, 0, 10), // Pre-allocated capacity
	}

	update := protocol.StateUpdate{
		WorkspaceID:   1,
		AllWorkspaces: []int32{1, 2},
	}

	state.applyStateUpdate(update)

	if cap(state.workspaces) < 10 {
		t.Errorf("workspace capacity was not preserved: got %d, want >= 10", cap(state.workspaces))
	}

	if len(state.workspaces) != 2 {
		t.Errorf("len(workspaces) = %d, want 2", len(state.workspaces))
	}
}

func TestClientStateApplyStateUpdateUnzoom(t *testing.T) {
	state := &clientState{
		cache:      client.NewBufferCache(),
		zoomed:     true,
		zoomedPane: [16]byte{1, 2, 3},
	}

	update := protocol.StateUpdate{
		WorkspaceID:   1,
		AllWorkspaces: []int32{1},
		Zoomed:        false, // Unzoom
	}

	state.applyStateUpdate(update)

	if state.zoomed {
		t.Error("zoomed should be false")
	}

	if state.zoomedPane != [16]byte{} {
		t.Error("zoomedPane should be zeroed when unzooming")
	}
}
