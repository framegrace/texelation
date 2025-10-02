package texel

import "github.com/gdamore/tcell/v2"

// TcellScreenDriver adapts a tcell.Screen to the ScreenDriver interface.
type TcellScreenDriver struct {
	screen tcell.Screen
}

// NewTcellScreenDriver wraps the provided screen.
func NewTcellScreenDriver(screen tcell.Screen) *TcellScreenDriver {
	return &TcellScreenDriver{screen: screen}
}

func (d *TcellScreenDriver) Init() error {
	return d.screen.Init()
}

func (d *TcellScreenDriver) Fini() {
	d.screen.Fini()
}

func (d *TcellScreenDriver) Size() (int, int) {
	return d.screen.Size()
}

func (d *TcellScreenDriver) SetStyle(style tcell.Style) {
	d.screen.SetStyle(style)
}

func (d *TcellScreenDriver) HideCursor() {
	d.screen.HideCursor()
}

func (d *TcellScreenDriver) Show() {
	d.screen.Show()
}

func (d *TcellScreenDriver) PollEvent() tcell.Event {
	return d.screen.PollEvent()
}

func (d *TcellScreenDriver) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {
	d.screen.SetContent(x, y, mainc, combc, style)
}

func (d *TcellScreenDriver) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	return d.screen.GetContent(x, y)
}

// Underlying exposes the wrapped tcell.Screen for compatibility code paths
// that still need direct access.
func (d *TcellScreenDriver) Underlying() tcell.Screen {
	return d.screen
}
