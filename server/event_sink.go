package server

import "texelation/protocol"

// EventSink receives events associated with a session.
type EventSink interface {
	HandleKeyEvent(session *Session, event protocol.KeyEvent)
	HandleMouseEvent(session *Session, event protocol.MouseEvent)
	HandleClipboardSet(session *Session, event protocol.ClipboardSet)
	HandleClipboardGet(session *Session, event protocol.ClipboardGet)
	HandleThemeUpdate(session *Session, event protocol.ThemeUpdate)
}

// nopSink discards events when no sink is provided.
type nopSink struct{}

func (nopSink) HandleKeyEvent(*Session, protocol.KeyEvent)         {}
func (nopSink) HandleMouseEvent(*Session, protocol.MouseEvent)     {}
func (nopSink) HandleClipboardSet(*Session, protocol.ClipboardSet) {}
func (nopSink) HandleClipboardGet(*Session, protocol.ClipboardGet) {}
func (nopSink) HandleThemeUpdate(*Session, protocol.ThemeUpdate)   {}
