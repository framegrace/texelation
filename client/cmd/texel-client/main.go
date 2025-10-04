package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
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

	state := &uiState{cache: client.NewBufferCache()}
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
	case protocol.MsgThemeAck:
		ack, err := protocol.DecodeThemeAck(payload)
		if err != nil {
			log.Printf("decode theme ack failed: %v", err)
			return
		}
		state.theme = ack
		state.hasTheme = true
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
	screen.Clear()
	for _, pane := range panes {
		if pane == nil || pane.Rect.Width <= 0 || pane.Rect.Height <= 0 {
			continue
		}
		titleText := pane.Title
		if titleText == "" {
			titleText = fmt.Sprintf("%x", pane.ID[:4])
		}
		title := fmt.Sprintf("[%s rev %d]", titleText, pane.Revision)
		drawClippedText(screen, pane.Rect.X, pane.Rect.Y, pane.Rect.Width, title, tcell.StyleDefault.Bold(true))
		maxContentRows := pane.Rect.Height - 1
		if maxContentRows <= 0 {
			continue
		}
		rows := pane.Rows()
		baseY := pane.Rect.Y + 1
		for rowIdx := 0; rowIdx < maxContentRows; rowIdx++ {
			line := ""
			if rowIdx < len(rows) {
				line = rows[rowIdx]
			}
			drawClippedText(screen, pane.Rect.X, baseY+rowIdx, pane.Rect.Width, line, tcell.StyleDefault)
		}
	}
	width, height := screen.Size()
	var statusLines []string
	if state.hasFocus {
		statusLines = append(statusLines, fmt.Sprintf("Focus: %s", formatPaneID(state.focus.PaneID)))
	}
	if state.hasClipboard {
		statusLines = append(statusLines, fmt.Sprintf("Clipboard [%s]: %s", state.clipboard.MimeType, truncateForStatus(string(state.clipboard.Data), width-len(state.clipboard.MimeType)-14)))
	}
	if state.hasTheme {
		statusLines = append(statusLines, fmt.Sprintf("Theme %s.%s = %s", state.theme.Section, state.theme.Key, state.theme.Value))
	}
	startY := height - len(statusLines)
	for i, text := range statusLines {
		y := startY + i
		if y < 0 {
			continue
		}
		drawClippedText(screen, 0, y, width, truncateForStatus(text, width), tcell.StyleDefault)
	}
	screen.Show()
}

func drawClippedText(screen tcell.Screen, x, y, width int, text string, style tcell.Style) {
	if y < 0 || width <= 0 {
		return
	}
	runes := []rune(text)
	for i := 0; i < width; i++ {
		if x+i < 0 {
			continue
		}
		ch := ' '
		if i < len(runes) {
			ch = runes[i]
		}
		screen.SetContent(x+i, y, ch, nil, style)
	}
}

func truncateForStatus(text string, max int) string {
	runes := []rune(text)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return text
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func formatPaneID(id [16]byte) string {
	return fmt.Sprintf("%x", id[:4])
}

func isNetworkClosed(err error) bool {
	if err == os.ErrClosed {
		return true
	}
	ne, ok := err.(net.Error)
	return ok && !ne.Timeout()
}
