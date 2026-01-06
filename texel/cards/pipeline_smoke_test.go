package cards

import (
	texelcore "github.com/framegrace/texelui/core"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// dummyCard wraps a texelcore.App-like behaviour for smoke tests.
type dummyCard struct {
	title       string
	width       int
	height      int
	renderCount int
}

func (d *dummyCard) Run() error            { return nil }
func (d *dummyCard) Stop()                 {}
func (d *dummyCard) Resize(cols, rows int) { d.width, d.height = cols, rows }
func (d *dummyCard) Render(input [][]texelcore.Cell) [][]texelcore.Cell {
	d.renderCount++
	buf := make([][]texelcore.Cell, d.height)
	for i := range buf {
		buf[i] = make([]texelcore.Cell, d.width)
	}
	if d.height > 0 && d.width > 0 {
		buf[0][0] = texelcore.Cell{Ch: 'X', Style: tcell.StyleDefault.Foreground(tcell.ColorRed)}
	}
	return buf
}
func (d *dummyCard) HandleKey(ev *tcell.EventKey)   {}
func (d *dummyCard) SetRefreshNotifier(chan<- bool) {}

func TestPipelineRenderSmoke(t *testing.T) {
	card := &dummyCard{}
	pipeline := NewPipeline(nil, card)
	pipeline.Resize(10, 5)
	buf := pipeline.Render()
	if len(buf) != 5 || len(buf[0]) != 10 || buf[0][0].Ch != 'X' {
		t.Fatalf("unexpected buffer: %v", buf)
	}
	if card.renderCount != 1 {
		t.Fatalf("unexpected render count: %d", card.renderCount)
	}
}
