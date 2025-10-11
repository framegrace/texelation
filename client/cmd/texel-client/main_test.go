package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
)

func TestRenderRespectsPaneGeometry(t *testing.T) {
	state := &uiState{cache: client.NewBufferCache()}
	var leftID, rightID [16]byte
	leftID[0] = 1
	rightID[0] = 2

	snapshot := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{
		{
			PaneID:   leftID,
			Title:    "left",
			Revision: 3,
			Rows: []string{
				"+----------+",
				"|ls -l    |",
				"|tail -f  |",
				"+----------+",
			},
			X:      0,
			Y:      0,
			Width:  12,
			Height: 4,
		},
		{
			PaneID:   rightID,
			Title:    "right",
			Revision: 5,
			Rows: []string{
				"+--------+",
				"|htop    |",
				"|logs    |",
				"+--------+",
			},
			X:      16,
			Y:      2,
			Width:  10,
			Height: 4,
		},
	}}
	state.cache.ApplySnapshot(snapshot)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init screen: %v", err)
	}
	defer screen.Fini()

	render(state, screen)

	leftTop := readScreenLine(screen, 0, 0, 12)
	if leftTop != "+----------+" {
		t.Fatalf("expected left top border, got %q", leftTop)
	}
	leftBody := readScreenLine(screen, 0, 1, 12)
	if leftBody != "|ls -l    |" {
		t.Fatalf("expected left body row, got %q", leftBody)
	}

	rightTop := readScreenLine(screen, 16, 2, 10)
	if rightTop != "+--------+" {
		t.Fatalf("expected right top border, got %q", rightTop)
	}
	rightBody := readScreenLine(screen, 16, 3, 10)
	if rightBody != "|htop    |" {
		t.Fatalf("expected right body row, got %q", rightBody)
	}

	belowLeft := readScreenLine(screen, 0, 5, 12)
	if belowLeft != "" {
		t.Fatalf("expected empty area, got %q", belowLeft)
	}
}

func TestRenderSkipsStatusWhenBottomOccupied(t *testing.T) {
	state := &uiState{cache: client.NewBufferCache(), defaultStyle: tcell.StyleDefault}
	var paneID [16]byte
	paneID[0] = 3
	snapshot := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{
		{
			PaneID:   paneID,
			Title:    "main",
			Revision: 1,
			Rows: []string{
				"+---------+",
				"|shell    |",
				"|prompt   |",
				"+---------+",
			},
			X:      0,
			Y:      0,
			Width:  11,
			Height: 4,
		},
	}}
	state.cache.ApplySnapshot(snapshot)
	state.applyStateUpdate(protocol.StateUpdate{
		WorkspaceID:   1,
		AllWorkspaces: []int32{1, 2},
		InControlMode: true,
		SubMode:       'w',
		ActiveTitle:   "shell",
	})

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(20, 6)

	render(state, screen)

	bottom := readScreenLine(screen, 0, 3, 11)
	if bottom != "+---------+" {
		t.Fatalf("expected bottom border to remain, got %q", bottom)
	}
	if screenHasSubstring(screen, "Workspaces") {
		t.Fatalf("expected status overlay to skip occupied row")
	}
}

func TestRenderShowsStatusWhenSpaceAvailable(t *testing.T) {
	state := &uiState{cache: client.NewBufferCache(), defaultStyle: tcell.StyleDefault}
	var paneID [16]byte
	paneID[0] = 4
	snapshot := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{
		{
			PaneID:   paneID,
			Title:    "main",
			Revision: 1,
			Rows: []string{
				"+------+",
				"|shell |",
				"+------+",
			},
			X:      0,
			Y:      0,
			Width:  8,
			Height: 3,
		},
	}}
	state.cache.ApplySnapshot(snapshot)
	state.applyStateUpdate(protocol.StateUpdate{
		WorkspaceID:   1,
		AllWorkspaces: []int32{1, 2},
		ActiveTitle:   "shell",
	})

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(20, 6)

	render(state, screen)

	if !screenHasSubstring(screen, "Workspaces") {
		t.Fatalf("expected status overlay to render when space is available")
	}
}

func screenHasSubstring(screen tcell.Screen, substr string) bool {
	width, height := screen.Size()
	for y := 0; y < height; y++ {
		line := readScreenLine(screen, 0, y, width)
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func TestStateUpdateFeedsStatusLines(t *testing.T) {
	state := &uiState{defaultStyle: tcell.StyleDefault}
	update := protocol.StateUpdate{
		WorkspaceID:   2,
		AllWorkspaces: []int32{1, 2, 3},
		InControlMode: true,
		SubMode:       'w',
		ActiveTitle:   "shell",
		DesktopBgRGB:  0x112233,
		Zoomed:        true,
		ZoomedPaneID:  [16]byte{9, 9, 9, 9},
	}
	state.applyStateUpdate(update)
	if !state.controlMode || state.workspaceID != 2 || state.activeTitle != "shell" {
		t.Fatalf("state not applied: %#v", state)
	}
	lines := state.buildStatusLines(80)
	if len(lines) == 0 {
		t.Fatalf("expected status lines")
	}
	if len(lines) < 3 {
		t.Fatalf("expected multiple status lines, got %v", lines)
	}
	if !strings.Contains(lines[0], "Workspaces") {
		t.Fatalf("expected workspace status, got %q", lines[0])
	}
	if !strings.Contains(strings.Join(lines, " "), "Zoom") {
		t.Fatalf("expected zoom status, got %v", lines)
	}
}

func readScreenLine(screen tcell.Screen, x, y, width int) string {
	runes := make([]rune, width)
	for i := 0; i < width; i++ {
		ch, _, _, _ := screen.GetContent(x+i, y)
		if ch == 0 {
			ch = ' '
		}
		runes[i] = ch
	}
	return stringTrimRightSpaces(string(runes))
}

func stringTrimRightSpaces(s string) string {
	runes := []rune(s)
	end := len(runes)
	for end > 0 && runes[end-1] == ' ' {
		end--
	}
	return string(runes[:end])
}
