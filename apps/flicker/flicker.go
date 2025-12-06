package flicker

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
	"texelation/texel/cards"
)

type ColorApp struct {
	mu      sync.Mutex
	color   tcell.Color
	text    string
	width   int
	height  int
	refresh chan<- bool
}

func NewColorApp(color tcell.Color, text string) *ColorApp {
	return &ColorApp{color: color, text: text}
}

func (a *ColorApp) Run() error { return nil }
func (a *ColorApp) Stop()      {}
func (a *ColorApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.width = cols
	a.height = rows
}
func (a *ColorApp) Render() [][]texel.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()
	buf := make([][]texel.Cell, a.height)
	for y := 0; y < a.height; y++ {
		buf[y] = make([]texel.Cell, a.width)
		for x := 0; x < a.width; x++ {
			style := tcell.StyleDefault.Background(a.color).Foreground(tcell.ColorWhite)
			ch := ' '
			if y == a.height/2 && x >= (a.width-len(a.text))/2 {
				idx := x - (a.width-len(a.text))/2
				if idx >= 0 && idx < len(a.text) && x < a.width {
					ch = rune(a.text[idx])
				}
			}
			buf[y][x] = texel.Cell{Ch: ch, Style: style}
		}
	}
	return buf
}
func (a *ColorApp) HandleKey(ev *tcell.EventKey)      {}
func (a *ColorApp) GetTitle() string                  { return "Flicker Test" }
func (a *ColorApp) SetRefreshNotifier(ch chan<- bool) { a.refresh = ch }

func New() texel.App {
	// Create two apps
	app1 := NewColorApp(tcell.ColorBlue, "BACKGROUND (BLUE)")
	app2 := NewColorApp(tcell.ColorRed, "FOREGROUND (RED)")

	// Wrap them in cards
	card1 := cards.WrapApp(app1)
	card2 := cards.WrapApp(app2)

	// Wrap in AlternatingCard
	// Period 2: 0, 1, 0, 1...
	// Card 1: Phase 0 (Even frames)
	// Card 2: Phase 1 (Odd frames)
	alt1 := cards.NewAlternatingCard(card1, 2, 0)
	alt2 := cards.NewAlternatingCard(card2, 2, 1)

	// Compose in a pipeline
	// Order matters: earlier cards are drawn first (bottom).
	// Here we draw alt1, then alt2.
	// On frame 0: alt1 draws, alt2 skips -> Result Blue
	// On frame 1: alt1 skips, alt2 draws -> Result Red (over transparent? no, skips means returns input)
	// Wait, pipeline passes input buffer.
	// If alt1 skips, it returns input (empty/black?).
	// If alt2 skips, it returns input (from alt1).
	// Frame 0: alt1 draws Blue. alt2 skips (returns Blue). Output: Blue.
	// Frame 1: alt1 skips (returns input/empty). alt2 draws Red. Output: Red.
	// Result: Blue / Red flickering.
	
	return cards.NewPipeline(nil, alt1, alt2)
}
