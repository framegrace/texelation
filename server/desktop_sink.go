package server

import (
	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
)

// DesktopSink forwards key events to a local Desktop instance.
type DesktopSink struct {
	desktop   *texel.Desktop
	publisher *DesktopPublisher
}

func NewDesktopSink(desktop *texel.Desktop) *DesktopSink {
	return &DesktopSink{desktop: desktop}
}

func (d *DesktopSink) HandleKeyEvent(session *Session, event protocol.KeyEvent) {
	if d.desktop == nil {
		return
	}
	key := tcell.Key(event.KeyCode)
	mod := tcell.ModMask(event.Modifiers)
	d.desktop.InjectKeyEvent(key, event.RuneValue, mod)
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

func (d *DesktopSink) Desktop() *texel.Desktop {
	return d.desktop
}

func (d *DesktopSink) SetPublisher(publisher *DesktopPublisher) {
	d.publisher = publisher
}
