package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
)

type uiState struct {
	cache        *client.BufferCache
	clipboard    protocol.ClipboardData
	hasClipboard bool
	theme        protocol.ThemeAck
	hasTheme     bool
	focus        protocol.PaneFocus
	hasFocus     bool
	themeValues  map[string]map[string]string
	defaultStyle tcell.Style
}

func main() {
	socket := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := flag.Bool("reconnect", false, "Attempt to resume previous session")
	flag.Parse()

	simple := client.NewSimpleClient(*socket)
	var sessionID [16]byte
	if !*reconnect {
		sessionID = [16]byte{}
	}

	accept, conn, err := simple.Connect(&sessionID)
	if err != nil {
		log.Fatalf("connect failed: %v", err)
	}
	defer conn.Close()

	fmt.Printf("Connected to session %s\n", client.FormatUUID(accept.SessionID))

	state := &uiState{cache: client.NewBufferCache(), themeValues: make(map[string]map[string]string), defaultStyle: tcell.StyleDefault}
	lastSequence := uint64(0)

	if *reconnect {
		if hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence); err != nil {
			log.Fatalf("resume request failed: %v", err)
		} else {
			handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence)
		}
	}

	inbound := make(chan protocol.Header, 16)
	go readLoop(conn, inbound, state, sessionID, &lastSequence)

	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("create screen failed: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("init screen failed: %v", err)
	}
	screen.HideCursor()
	defer screen.Fini()

	render(state, screen)

	for {
		select {
		case hdr, ok := <-inbound:
			if !ok {
				fmt.Println("Connection closed")
				return
			}
			switch hdr.Type {
			case protocol.MsgTreeSnapshot, protocol.MsgBufferDelta, protocol.MsgClipboardData, protocol.MsgThemeAck, protocol.MsgClipboardSet, protocol.MsgThemeUpdate:
				render(state, screen)
			}
		default:
			ev := screen.PollEvent()
			switch ev := ev.(type) {
			case *tcell.EventKey:
				key := protocol.KeyEvent{KeyCode: uint32(ev.Key()), RuneValue: ev.Rune(), Modifiers: uint16(ev.Modifiers())}
				payload, _ := protocol.EncodeKeyEvent(key)
				if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgKeyEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
					log.Printf("send key failed: %v", err)
				}
			case *tcell.EventMouse:
				x, y := ev.Position()
				mouse := protocol.MouseEvent{X: int16(x), Y: int16(y), ButtonMask: uint32(ev.Buttons()), Modifiers: uint16(ev.Modifiers())}
				payload, _ := protocol.EncodeMouseEvent(mouse)
				if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgMouseEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
					log.Printf("send mouse failed: %v", err)
				}
			case *tcell.EventResize:
				render(state, screen)
			}
		}
	}
}

func readLoop(conn net.Conn, headers chan<- protocol.Header, state *uiState, sessionID [16]byte, lastSequence *uint64) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if !isNetworkClosed(err) {
				log.Printf("read failed: %v", err)
			}
			close(headers)
			return
		}
		handleControlMessage(state, conn, hdr, payload, sessionID, lastSequence)
		headers <- hdr
	}
}

func handleControlMessage(state *uiState, conn net.Conn, hdr protocol.Header, payload []byte, sessionID [16]byte, lastSequence *uint64) {
	cache := state.cache
	switch hdr.Type {
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			log.Printf("decode delta failed: %v", err)
			return
		}
		state := cache.ApplyDelta(delta)
		ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
		if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: sessionID}, ackPayload); err != nil {
			log.Printf("ack failed: %v", err)
		}
		if state != nil {
			fmt.Printf("Delta: pane=%x rev=%d rows=%d\n", delta.PaneID, delta.Revision, len(state.Rows()))
		}
		if lastSequence != nil && hdr.Sequence > *lastSequence {
			*lastSequence = hdr.Sequence
		}
	case protocol.MsgTreeSnapshot:
		snap, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			log.Printf("decode snapshot failed: %v", err)
			return
		}
		cache.ApplySnapshot(snap)
	case protocol.MsgPing:
		pong, _ := protocol.EncodePong(protocol.Pong{Timestamp: time.Now().UnixNano()})
		_ = protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgPong, Flags: protocol.FlagChecksum, SessionID: sessionID}, pong)
	case protocol.MsgClipboardSet:
		clip, err := protocol.DecodeClipboardSet(payload)
		if err != nil {
			log.Printf("decode clipboard failed: %v", err)
			return
		}
		state.clipboard = protocol.ClipboardData{MimeType: clip.MimeType, Data: clip.Data}
		state.hasClipboard = true
	case protocol.MsgClipboardData:
		clip, err := protocol.DecodeClipboardData(payload)
		if err != nil {
			log.Printf("decode clipboard data failed: %v", err)
			return
		}
		state.clipboard = clip
		state.hasClipboard = true
	case protocol.MsgThemeUpdate:
		themeUpdate, err := protocol.DecodeThemeUpdate(payload)
		if err != nil {
			log.Printf("decode theme update failed: %v", err)
			return
		}
		state.theme = protocol.ThemeAck(themeUpdate)
		state.hasTheme = true
		state.updateTheme(themeUpdate.Section, themeUpdate.Key, themeUpdate.Value)
	case protocol.MsgThemeAck:
		ack, err := protocol.DecodeThemeAck(payload)
		if err != nil {
			log.Printf("decode theme ack failed: %v", err)
			return
		}
		state.theme = ack
		state.hasTheme = true
		state.updateTheme(ack.Section, ack.Key, ack.Value)
	case protocol.MsgPaneFocus:
		focus, err := protocol.DecodePaneFocus(payload)
		if err != nil {
			log.Printf("decode pane focus failed: %v", err)
			return
		}
		state.focus = focus
		state.hasFocus = true
	}
}

func render(state *uiState, screen tcell.Screen) {
	panes := state.cache.LayoutPanes()
	if len(panes) == 0 {
		return
	}
	width, height := screen.Size()
	screen.SetStyle(state.defaultStyle)
	screen.Clear()
	for _, pane := range panes {
		if pane == nil || pane.Rect.Width <= 0 || pane.Rect.Height <= 0 {
			continue
		}
		for rowIdx := 0; rowIdx < pane.Rect.Height; rowIdx++ {
			targetY := pane.Rect.Y + rowIdx
			if targetY < 0 || targetY >= height {
				continue
			}
			row := pane.RowCells(rowIdx)
			for col := 0; col < pane.Rect.Width; col++ {
				targetX := pane.Rect.X + col
				if targetX < 0 || targetX >= width {
					continue
				}
				ch := ' '
				style := tcell.StyleDefault
				if row != nil && col < len(row) {
					cell := row[col]
					if cell.Ch != 0 {
						ch = cell.Ch
					}
					style = cell.Style
				}
				screen.SetContent(targetX, targetY, ch, nil, style)
			}
		}
	}
	screen.Show()
}

func isNetworkClosed(err error) bool {
	if err == os.ErrClosed {
		return true
	}
	ne, ok := err.(net.Error)
	return ok && !ne.Timeout()
}

func (s *uiState) updateTheme(section, key, value string) {
	if section == "" || key == "" {
		return
	}
	if s.themeValues == nil {
		s.themeValues = make(map[string]map[string]string)
	}
	sec := s.themeValues[section]
	if sec == nil {
		sec = make(map[string]string)
		s.themeValues[section] = sec
	}
	sec[key] = value
	s.defaultStyle = computeDefaultStyle(s.themeValues)
}

func computeDefaultStyle(values map[string]map[string]string) tcell.Style {
	style := tcell.StyleDefault
	if values == nil {
		return style
	}
	if desktop, ok := values["desktop"]; ok {
		if fgStr, ok := desktop["default_fg"]; ok {
			if fg, ok := parseHexColor(fgStr); ok {
				style = style.Foreground(fg)
			}
		}
		if bgStr, ok := desktop["default_bg"]; ok {
			if bg, ok := parseHexColor(bgStr); ok {
				style = style.Background(bg)
			}
		}
	}
	return style
}

func parseHexColor(value string) (tcell.Color, bool) {
	if len(value) == 0 {
		return tcell.ColorDefault, false
	}
	if len(value) == 7 && value[0] == '#' {
		if fg, err := strconv.ParseInt(value[1:], 16, 32); err == nil {
			r := int32((fg >> 16) & 0xFF)
			g := int32((fg >> 8) & 0xFF)
			b := int32(fg & 0xFF)
			return tcell.NewRGBColor(r, g, b), true
		}
	}
	return tcell.ColorDefault, false
}
