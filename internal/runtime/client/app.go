// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/app.go
// Summary: Implements app capabilities for the remote client runtime.
// Usage: Embedded by client binaries to handle app as part of the render/event loop.
// Notes: Owns session management, rendering, and protocol interaction for remote front-ends.

package clientruntime

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/internal/effects"
	"texelation/protocol"
	"texelation/texel/theme"
)

const resizeDebounce = 45 * time.Millisecond

// Options configures the remote client runtime.
type Options struct {
	Socket    string
	Reconnect bool
	PanicLog  string
}

func Run(opts Options) error {
	panicLogger := NewPanicLogger(opts.PanicLog)
	defer panicLogger.Recover("run")

	logFile, err := setupLogging()
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging disabled: %v\n", err)
	} else {
		defer logFile.Close()
	}

	simple := client.NewSimpleClient(opts.Socket)
	var sessionID [16]byte
	if !opts.Reconnect {
		sessionID = [16]byte{}
	}

	accept, conn, err := simple.Connect(&sessionID)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close()
	var writeMu sync.Mutex

	log.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))

	state := &uiState{
		cache:        client.NewBufferCache(),
		themeValues:  make(map[string]map[string]interface{}),
		defaultStyle: tcell.StyleDefault,
		defaultFg:    tcell.ColorDefault,
		defaultBg:    tcell.ColorDefault,
		desktopBg:    tcell.ColorDefault,
	}

	cfg := theme.Get()
	if err := theme.Err(); err != nil {
		return fmt.Errorf("failed to load theme: %w", err)
	}
	theme.ApplyDefaults(cfg)
	for sectionName, section := range cfg {
		for key, value := range section {
			state.setThemeValue(sectionName, key, value)
		}
	}

	state.applyEffectConfig()
	lastSequence := uint64(0)

	var pendingAck atomic.Uint64
	var lastAck atomic.Uint64
	ackSignal := make(chan struct{}, 1)

	if opts.Reconnect {
		if hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence); err != nil {
			return fmt.Errorf("resume request failed: %w", err)
		} else {
			handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence, &writeMu, &pendingAck, ackSignal)
		}
	}

	renderCh := make(chan struct{}, 1)
	state.setRenderChannel(renderCh)
	doneCh := make(chan struct{})
	panicLogger.Go("readLoop", func() {
		readLoop(conn, state, sessionID, &lastSequence, renderCh, doneCh, &writeMu, &pendingAck, ackSignal)
	})
	pingStop := make(chan struct{})
	panicLogger.Go("pingLoop", func() {
		pingLoop(conn, sessionID, doneCh, pingStop, &writeMu)
	})
	panicLogger.Go("ackLoop", func() {
		ackLoop(conn, sessionID, &writeMu, doneCh, &pendingAck, &lastAck, ackSignal)
	})

	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("create screen failed: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("init screen failed: %w", err)
	}
	screen.EnablePaste()
	screen.HideCursor()
	defer screen.Fini()
	defer close(pingStop)
	sendResize(&writeMu, conn, sessionID, screen)

	render(state, screen)

	events := make(chan tcell.Event, 32)
	stopEvents := make(chan struct{})
	panicLogger.Go("eventPoll", func() {
		for {
			select {
			case <-stopEvents:
				close(events)
				return
			default:
				ev := screen.PollEvent()
				if ev == nil {
					close(events)
					return
				}
				select {
				case events <- ev:
				case <-stopEvents:
					close(events)
					return
				}
			}
		}
	})
	defer func() {
		close(stopEvents)
		screen.PostEventWait(tcell.NewEventInterrupt(nil))
	}()

	for {
		select {
		case <-renderCh:
			render(state, screen)
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if !handleScreenEvent(ev, state, screen, conn, sessionID, &writeMu) {
				return nil
			}
		case <-doneCh:
			fmt.Println("Connection closed")
			return nil
		}
	}
}

func readLoop(conn net.Conn, state *uiState, sessionID [16]byte, lastSequence *uint64, renderCh chan<- struct{}, doneCh chan<- struct{}, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if !isNetworkClosed(err) {
				log.Printf("read failed: %v", err)
			}
			close(doneCh)
			return
		}
		if handleControlMessage(state, conn, hdr, payload, sessionID, lastSequence, writeMu, pendingAck, ackSignal) {
			select {
			case renderCh <- struct{}{}:
			default:
			}
		}
	}
}

func handleControlMessage(state *uiState, conn net.Conn, hdr protocol.Header, payload []byte, sessionID [16]byte, lastSequence *uint64, writeMu *sync.Mutex, pendingAck *atomic.Uint64, ackSignal chan<- struct{}) bool {
	cache := state.cache
	switch hdr.Type {
	case protocol.MsgTreeSnapshot:
		snap, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			log.Printf("decode snapshot failed: %v", err)
			return false
		}
		if len(snap.Panes) == 0 {
			if existing := cache.SortedPanes(); len(existing) > 0 {
				log.Printf("ignoring empty snapshot; retaining %d cached panes", len(existing))
				return false
			}
		}
		cache.ApplySnapshot(snap)
		if state.effects != nil {
			state.effects.ResetPaneStates(cache.SortedPanes())
		}
		return true
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			log.Printf("decode delta failed: %v", err)
			return false
		}
		cache.ApplyDelta(delta)
		scheduleAck(pendingAck, ackSignal, hdr.Sequence)
		if lastSequence != nil && hdr.Sequence > *lastSequence {
			*lastSequence = hdr.Sequence
		}
		return true
	case protocol.MsgPing:
		pong, _ := protocol.EncodePong(protocol.Pong{Timestamp: time.Now().UnixNano()})
		if err := writeMessage(writeMu, conn, protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgPong,
			Flags:     protocol.FlagChecksum,
			SessionID: sessionID,
		}, pong); err != nil {
			log.Printf("send pong failed: %v", err)
		}
		return false
	case protocol.MsgClipboardSet:
		clip, err := protocol.DecodeClipboardSet(payload)
		if err != nil {
			log.Printf("decode clipboard failed: %v", err)
			return false
		}
		state.clipboard = protocol.ClipboardData{MimeType: clip.MimeType, Data: clip.Data}
		state.hasClipboard = true
		return true
	case protocol.MsgClipboardData:
		clip, err := protocol.DecodeClipboardData(payload)
		if err != nil {
			log.Printf("decode clipboard data failed: %v", err)
			return false
		}
		state.clipboard = clip
		state.hasClipboard = true
		return true
	case protocol.MsgThemeUpdate:
		themeUpdate, err := protocol.DecodeThemeUpdate(payload)
		if err != nil {
			log.Printf("decode theme update failed: %v", err)
			return false
		}
		state.theme = protocol.ThemeAck(themeUpdate)
		state.hasTheme = true
		state.updateTheme(themeUpdate.Section, themeUpdate.Key, themeUpdate.Value)
		state.applyEffectConfig()
		return true
	case protocol.MsgThemeAck:
		ack, err := protocol.DecodeThemeAck(payload)
		if err != nil {
			log.Printf("decode theme ack failed: %v", err)
			return false
		}
		state.theme = ack
		state.hasTheme = true
		state.updateTheme(ack.Section, ack.Key, ack.Value)
		state.applyEffectConfig()
		return true
	case protocol.MsgPaneFocus:
		focus, err := protocol.DecodePaneFocus(payload)
		if err != nil {
			log.Printf("decode pane focus failed: %v", err)
			return false
		}
		state.focus = focus
		state.hasFocus = true
		return true
	case protocol.MsgPaneState:
		paneFlags, err := protocol.DecodePaneState(payload)
		if err != nil {
			log.Printf("decode pane state failed: %v", err)
			return false
		}
		active := paneFlags.Flags&protocol.PaneStateActive != 0
		resizing := paneFlags.Flags&protocol.PaneStateResizing != 0
		state.cache.SetPaneFlags(paneFlags.PaneID, active, resizing, paneFlags.ZOrder)
		if state.effects != nil {
			ts := time.Now()
			state.effects.HandleTrigger(effects.EffectTrigger{Type: effects.TriggerPaneActive, PaneID: paneFlags.PaneID, Active: active, Timestamp: ts})
			state.effects.HandleTrigger(effects.EffectTrigger{Type: effects.TriggerPaneResizing, PaneID: paneFlags.PaneID, Resizing: resizing, Timestamp: ts})
		}
		return true
	case protocol.MsgStateUpdate:
		update, err := protocol.DecodeStateUpdate(payload)
		if err != nil {
			log.Printf("decode state update failed: %v", err)
			return false
		}
		log.Printf("state update: control=%v sub=%q zoom=%v", update.InControlMode, update.SubMode, update.Zoomed)
		state.applyStateUpdate(update)
		return true
	}
	return false
}

func formatPaneID(id [16]byte) string {
	return fmt.Sprintf("%x", id[:4])
}

func pingLoop(conn net.Conn, sessionID [16]byte, done <-chan struct{}, stop <-chan struct{}, writeMu *sync.Mutex) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-stop:
			return
		case <-ticker.C:
			ping := protocol.Ping{Timestamp: time.Now().UnixNano()}
			payload, err := protocol.EncodePing(ping)
			if err != nil {
				log.Printf("encode ping failed: %v", err)
				continue
			}
			header := protocol.Header{Version: protocol.Version, Type: protocol.MsgPing, Flags: protocol.FlagChecksum, SessionID: sessionID}
			if err := writeMessage(writeMu, conn, header, payload); err != nil {
				log.Printf("send ping failed: %v", err)
				return
			}
		}
	}
}

func scheduleAck(pending *atomic.Uint64, signal chan<- struct{}, seq uint64) {
	for {
		current := pending.Load()
		if seq <= current {
			break
		}
		if pending.CompareAndSwap(current, seq) {
			break
		}
	}
	select {
	case signal <- struct{}{}:
	default:
	}
}

func ackLoop(conn net.Conn, sessionID [16]byte, writeMu *sync.Mutex, done <-chan struct{}, pending *atomic.Uint64, lastAck *atomic.Uint64, signal <-chan struct{}) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-signal:
		case <-ticker.C:
		}
		target := pending.Load()
		if target == 0 || target == lastAck.Load() {
			continue
		}
		payload, err := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: target})
		if err != nil {
			log.Printf("ack encode failed: %v", err)
			continue
		}
		header := protocol.Header{
			Version:   protocol.Version,
			Type:      protocol.MsgBufferAck,
			Flags:     protocol.FlagChecksum,
			SessionID: sessionID,
		}
		if err := writeMessage(writeMu, conn, header, payload); err != nil {
			log.Printf("ack send failed: %v", err)
			return
		}
		lastAck.Store(target)
	}
}

func setupLogging() (*os.File, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	logDir := filepath.Join(configDir, "texelation", "logs")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, err
	}
	logPath := filepath.Join(logDir, "remote-client.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, err
	}
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return file, nil
}
