package server

import (
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
)

// DesktopPublisher captures desktop pane buffers and enqueues them as buffer
// deltas on the associated session.
type DesktopPublisher struct {
	desktop   *texel.Desktop
	session   *Session
	revisions map[[16]byte]uint32
	observer  PublishObserver
}

// PublishObserver records desktop publish metrics for instrumentation.
type PublishObserver interface {
	ObservePublish(session *Session, paneCount int, duration time.Duration)
}

func NewDesktopPublisher(desktop *texel.Desktop, session *Session) *DesktopPublisher {
	return &DesktopPublisher{
		desktop:   desktop,
		session:   session,
		revisions: make(map[[16]byte]uint32),
	}
}

// SetObserver registers an optional metrics observer invoked after each publish.
func (p *DesktopPublisher) SetObserver(observer PublishObserver) {
	p.observer = observer
}

func (p *DesktopPublisher) Publish() error {
	if p.desktop == nil || p.session == nil {
		return nil
	}
	start := time.Now()
	snapshots := p.desktop.SnapshotBuffers()
	for _, snap := range snapshots {
		rev := p.revisions[snap.ID] + 1
		p.revisions[snap.ID] = rev
		delta := bufferToDelta(snap, rev)
		if err := p.session.EnqueueDiff(delta); err != nil {
			return err
		}
	}
	if p.observer != nil {
		p.observer.ObservePublish(p.session, len(snapshots), time.Since(start))
	}
	return nil
}

func bufferToDelta(snap texel.PaneSnapshot, revision uint32) protocol.BufferDelta {
	styleMap := make(map[styleKey]uint16)
	styles := make([]protocol.StyleEntry, 0)

	rows := make([]protocol.RowDelta, 0, len(snap.Buffer))
	for y, row := range snap.Buffer {
		if len(row) == 0 {
			continue
		}
		spans := make([]protocol.CellSpan, 0)
		builders := make([]*strings.Builder, 0)

		for x, cell := range row {
			key, entry := convertStyle(cell.Style)
			index, ok := styleMap[key]
			if !ok {
				styles = append(styles, entry)
				index = uint16(len(styles) - 1)
				styleMap[key] = index
			}

			if len(spans) == 0 || spans[len(spans)-1].StyleIndex != index {
				spans = append(spans, protocol.CellSpan{StartCol: uint16(x), StyleIndex: index})
				builders = append(builders, &strings.Builder{})
			}
			builders[len(builders)-1].WriteRune(cell.Ch)
		}

		for i := range spans {
			spans[i].Text = builders[i].String()
		}

		rows = append(rows, protocol.RowDelta{Row: uint16(y), Spans: spans})
	}

	return protocol.BufferDelta{
		PaneID:   snap.ID,
		Revision: revision,
		Flags:    protocol.BufferDeltaNone,
		Styles:   styles,
		Rows:     rows,
	}
}

type styleKey struct {
	attrFlags uint16
	fgModel   protocol.ColorModel
	fgValue   uint32
	bgModel   protocol.ColorModel
	bgValue   uint32
}

func convertStyle(style tcell.Style) (styleKey, protocol.StyleEntry) {
	fg, bg, attrs := style.Decompose()

	attrFlags := uint16(0)
	if attrs&tcell.AttrBold != 0 {
		attrFlags |= protocol.AttrBold
	}
	if attrs&tcell.AttrUnderline != 0 {
		attrFlags |= protocol.AttrUnderline
	}
	if attrs&tcell.AttrReverse != 0 {
		attrFlags |= protocol.AttrReverse
	}
	if attrs&tcell.AttrBlink != 0 {
		attrFlags |= protocol.AttrBlink
	}
	if attrs&tcell.AttrDim != 0 {
		attrFlags |= protocol.AttrDim
	}
	if attrs&tcell.AttrItalic != 0 {
		attrFlags |= protocol.AttrItalic
	}

	fgModel, fgValue := convertColor(fg)
	bgModel, bgValue := convertColor(bg)

	key := styleKey{attrFlags: attrFlags, fgModel: fgModel, fgValue: fgValue, bgModel: bgModel, bgValue: bgValue}
	entry := protocol.StyleEntry{
		AttrFlags: attrFlags,
		FgModel:   fgModel,
		FgValue:   fgValue,
		BgModel:   bgModel,
		BgValue:   bgValue,
	}
	return key, entry
}

func convertColor(color tcell.Color) (protocol.ColorModel, uint32) {
	if color == tcell.ColorDefault {
		return protocol.ColorModelDefault, 0
	}
	r, g, b := color.RGB()
	return protocol.ColorModelRGB, (uint32(r)&0xff)<<16 | (uint32(g)&0xff)<<8 | (uint32(b) & 0xff)
}
