package effects

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
)

func TestIsFakeBackgroundCellMatchesNeighborBackground(t *testing.T) {
	fakeColor := tcell.NewRGBColor(32, 48, 64)
	row := []client.Cell{
		{Ch: ' ', Style: tcell.StyleDefault.Background(fakeColor)},
		{Ch: '▶', Style: tcell.StyleDefault.Foreground(fakeColor)},
		{Ch: ' ', Style: tcell.StyleDefault},
	}

	if !isFakeBackgroundCell(row, 1) {
		t.Fatalf("expected fake background cell detection for %#v", row[1])
	}

	if isFakeBackgroundCell(row, 0) {
		t.Fatalf("did not expect fake background detection for neighbour %#v", row[0])
	}
}

func TestIsFakeBackgroundCellWithExplicitForegroundAndBackground(t *testing.T) {
	fakeColor := tcell.NewRGBColor(50, 60, 70)
	row := []client.Cell{
		{Ch: ' ', Style: tcell.StyleDefault.Background(fakeColor)},
		{Ch: '▶', Style: tcell.StyleDefault.Foreground(fakeColor).Background(fakeColor)},
		{Ch: ' ', Style: tcell.StyleDefault.Background(fakeColor)},
	}

	if !isFakeBackgroundCell(row, 1) {
		t.Fatalf("expected detection when fg and bg both %v", fakeColor)
	}
}

func TestKeyFlashTintsFakeBackgroundCellForeground(t *testing.T) {
	fakeColor := tcell.NewRGBColor(64, 96, 128)
	row := []client.Cell{
		{Ch: ' ', Style: tcell.StyleDefault.Background(fakeColor)},
		{Ch: '▶', Style: tcell.StyleDefault.Foreground(fakeColor)},
	}
	buffer := [][]client.Cell{row}

	flash := newKeyFlashEffect(tcell.ColorWhite, 50*time.Millisecond, []rune{'F'}, tcell.NewRGBColor(220, 220, 220), tcell.NewRGBColor(0, 0, 0), 1.0).(*keyFlashEffect)
	flash.timeline.AnimateTo("flash", 1.0, 0)

	origFg, origBg, _ := buffer[0][1].Style.Decompose()
	flash.ApplyWorkspace(buffer)

	newFg, newBg, _ := buffer[0][1].Style.Decompose()

	if newFg == origFg {
		t.Fatalf("expected foreground to change, still %v", newFg)
	}
	if newBg == origBg {
		t.Fatalf("expected background to change from %v", origBg)
	}
}
