package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
)

func TestRenderRespectsPaneGeometry(t *testing.T) {
	cache := client.NewBufferCache()
	var leftID, rightID [16]byte
	leftID[0] = 1
	rightID[0] = 2

	snapshot := protocol.TreeSnapshot{Panes: []protocol.PaneSnapshot{
		{
			PaneID:   leftID,
			Title:    "left",
			Revision: 3,
			Rows:     []string{"ls -l", "tail -f"},
			X:        0,
			Y:        0,
			Width:    12,
			Height:   4,
		},
		{
			PaneID:   rightID,
			Title:    "right",
			Revision: 5,
			Rows:     []string{"htop", "logs"},
			X:        16,
			Y:        2,
			Width:    10,
			Height:   4,
		},
	}}
	cache.ApplySnapshot(snapshot)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init screen: %v", err)
	}
	defer screen.Fini()

	render(cache, screen)

	leftTitle := readScreenLine(screen, 0, 0, 12)
	if leftTitle != "[left rev 3]" {
		t.Fatalf("expected left title, got %q", leftTitle)
	}
	leftContent := readScreenLine(screen, 0, 1, 12)
	if leftContent != "ls -l" {
		t.Fatalf("expected left content row, got %q", leftContent)
	}

	rightTitle := readScreenLine(screen, 16, 2, 10)
	if rightTitle != "[right rev" { // clipped to width 10
		t.Fatalf("expected right title prefix, got %q", rightTitle)
	}
	rightContent := readScreenLine(screen, 16, 3, 10)
	if rightContent != "htop" {
		t.Fatalf("expected right content row, got %q", rightContent)
	}

	belowLeft := readScreenLine(screen, 0, 5, 12)
	if belowLeft != "" {
		t.Fatalf("expected empty area, got %q", belowLeft)
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
