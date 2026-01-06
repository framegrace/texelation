package cards

import (
	texelcore "github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

// Card represents a rendering stage within a Texel application pipeline.
// Implementations may generate a base buffer or post-process the input buffer
// from a previous stage. The final card's output becomes the displayed frame.
type Card interface {
	Run() error
	Stop()
	Resize(cols, rows int)
	Render(input [][]texelcore.Cell) [][]texelcore.Cell
	HandleKey(ev *tcell.EventKey)
	SetRefreshNotifier(refreshChan chan<- bool)
}
