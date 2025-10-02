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
	inbound := make(chan protocol.Header, 16)
	go readLoop(conn, inbound, cache, sessionID)

	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("create screen failed: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("init screen failed: %v", err)
	}
	defer screen.Fini()

	for {
		select {
		case hdr := <-inbound:
			fmt.Printf("Received %v seq=%d\n", hdr.Type, hdr.Sequence)
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

func readLoop(conn net.Conn, headers chan<- protocol.Header, cache *client.BufferCache, sessionID [16]byte) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if !isNetworkClosed(err) {
				log.Printf("read failed: %v", err)
			}
			close(headers)
			return
		}
		switch hdr.Type {
		case protocol.MsgBufferDelta:
			delta, err := protocol.DecodeBufferDelta(payload)
			if err != nil {
				log.Printf("decode delta failed: %v", err)
				continue
			}
			state := cache.ApplyDelta(delta)
			ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
			if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: sessionID}, ackPayload); err != nil {
				log.Printf("ack failed: %v", err)
			}
			if state != nil {
				fmt.Printf("Delta: pane=%x rev=%d rows=%d\n", delta.PaneID, delta.Revision, len(state.Rows()))
			}
		case protocol.MsgPing:
			pong, _ := protocol.EncodePong(protocol.Pong{Timestamp: time.Now().UnixNano()})
			_ = protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgPong, Flags: protocol.FlagChecksum}, pong)
		}
		headers <- hdr
	}
}

func render(cache *client.BufferCache, screen tcell.Screen) {
	state := cache.LatestPane()
	if state == nil {
		return
	}
	screen.Clear()
	rows := state.Rows()
	for y, line := range rows {
		for x, ch := range []rune(line) {
			screen.SetContent(x, y, ch, nil, tcell.StyleDefault)
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
