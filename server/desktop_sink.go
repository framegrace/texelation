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

func (d *DesktopSink) HandleMouseEvent(session *Session, event protocol.MouseEvent) {
	if d.desktop == nil {
		return
	}
	d.desktop.InjectMouseEvent(int(event.X), int(event.Y), tcell.ButtonMask(event.ButtonMask), tcell.ModMask(event.Modifiers))
	if d.publisher != nil {
		_ = d.publisher.Publish()
	}
}

func (d *DesktopSink) HandleClipboardSet(session *Session, event protocol.ClipboardSet) {
	if d.desktop == nil {
		return
	}
	d.desktop.HandleClipboardSet(event.MimeType, event.Data)
}

func (d *DesktopSink) HandleClipboardGet(session *Session, event protocol.ClipboardGet) {
	if d.desktop == nil {
		return
	}
	d.desktop.HandleClipboardGet(event.MimeType)
}

func (d *DesktopSink) HandleThemeUpdate(session *Session, event protocol.ThemeUpdate) {
	if d.desktop == nil {
		return
	}
	d.desktop.HandleThemeUpdate(event.Section, event.Key, event.Value)
}

func (d *DesktopSink) Desktop() *texel.Desktop {
	return d.desktop
}

func (d *DesktopSink) SetPublisher(publisher *DesktopPublisher) {
	d.publisher = publisher
}

func (d *DesktopSink) Snapshot() (protocol.TreeSnapshot, error) {
	if d.desktop == nil {
		return protocol.TreeSnapshot{}, nil
	}
	panes := d.desktop.SnapshotBuffers()
	snapshot := protocol.TreeSnapshot{Panes: make([]protocol.PaneSnapshot, len(panes))}
	for i, pane := range panes {
		rows := make([]string, len(pane.Buffer))
		for y, row := range pane.Buffer {
			runes := make([]rune, len(row))
			for x, cell := range row {
				if cell.Ch == 0 {
					runes[x] = ' '
				} else {
					runes[x] = cell.Ch
				}
			}
			rows[y] = string(runes)
		}
        snapshot.Panes[i] = protocol.PaneSnapshot{
            PaneID:   pane.ID,
            Revision: 0,
            Title:    pane.Title,
            Rows:     rows,
        }
	}
	return snapshot, nil
}
