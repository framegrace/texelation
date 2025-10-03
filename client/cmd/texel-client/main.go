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

	cache := client.NewBufferCache()
	lastSequence := uint64(0)

	if *reconnect {
		if hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence); err != nil {
			log.Fatalf("resume request failed: %v", err)
		} else {
			handleControlMessage(conn, hdr, payload, cache, sessionID, &lastSequence)
		}
	}

	inbound := make(chan protocol.Header, 16)
	go readLoop(conn, inbound, cache, sessionID, &lastSequence)

	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("create screen failed: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("init screen failed: %v", err)
	}
	defer screen.Fini()

	render(cache, screen)

	for {
		select {
		case hdr, ok := <-inbound:
			if !ok {
				fmt.Println("Connection closed")
				return
			}
			if hdr.Type == protocol.MsgTreeSnapshot || hdr.Type == protocol.MsgBufferDelta {
				render(cache, screen)
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
				render(cache, screen)
			}
		}
	}
}

func readLoop(conn net.Conn, headers chan<- protocol.Header, cache *client.BufferCache, sessionID [16]byte, lastSequence *uint64) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if !isNetworkClosed(err) {
				log.Printf("read failed: %v", err)
			}
			close(headers)
			return
		}
		handleControlMessage(conn, hdr, payload, cache, sessionID, lastSequence)
		headers <- hdr
	}
}

func handleControlMessage(conn net.Conn, hdr protocol.Header, payload []byte, cache *client.BufferCache, sessionID [16]byte, lastSequence *uint64) {
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
	}
}

func render(cache *client.BufferCache, screen tcell.Screen) {
	panes := cache.AllPanes()
	if len(panes) == 0 {
		return
	}
	screen.Clear()
	y := 0
	for _, pane := range panes {
		titleText := pane.Title
		if titleText == "" {
			titleText = fmt.Sprintf("%x", pane.ID[:4])
		}
		title := fmt.Sprintf("[%s rev %d]", titleText, pane.Revision)
		for x, ch := range []rune(title) {
			screen.SetContent(x, y, ch, nil, tcell.StyleDefault.Bold(true))
		}
		y++
		for _, line := range pane.Rows() {
			for x, ch := range []rune(line) {
				screen.SetContent(x, y, ch, nil, tcell.StyleDefault)
			}
			y++
		}
		y++
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
