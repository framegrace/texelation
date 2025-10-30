package cards

import (
	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// Card represents a rendering stage within a Texel application pipeline.
// Implementations may generate a base buffer or post-process the input buffer
// from a previous stage. The final card's output becomes the displayed frame.
type Card interface {
	Run() error
	Stop()
	Resize(cols, rows int)
	Render(input [][]texel.Cell) [][]texel.Cell
	HandleKey(ev *tcell.EventKey)
	SetRefreshNotifier(refreshChan chan<- bool)
	HandleMessage(msg texel.Message)
}
