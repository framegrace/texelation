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
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelui/theme"
)

const resizeDebounce = 10 * time.Millisecond

// Options configures the remote client runtime.
type Options struct {
	Socket                  string
	Reconnect               bool
	PanicLog                string
	ShowRestartNotification bool // Show notification that server was restarted
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

	state := &clientState{
		cache:                   client.NewBufferCache(),
		themeValues:             make(map[string]map[string]interface{}),
		defaultStyle:            tcell.StyleDefault,
		defaultFg:               tcell.ColorDefault,
		defaultBg:               tcell.ColorDefault,
		desktopBg:               tcell.ColorDefault,
		selectionFg:             tcell.ColorBlack,
		selectionBg:             tcell.NewRGBColor(232, 217, 255),
		showRestartNotification: opts.ShowRestartNotification,
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

	renderCh := make(chan struct{}, 64) // Larger buffer for smooth animations
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
	screen.EnableMouse()
	defer screen.DisableMouse()
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
			// Drain any additional pending render signals to avoid rendering stale frames
			drainLoop:
			for {
				select {
				case <-renderCh:
					// Drained one more signal
				default:
					break drainLoop
				}
			}
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
		if clip, ok := state.consumeClipboardSync(); ok && len(clip.Data) > 0 {
			screen.SetClipboard(clip.Data)
		}
	}
}

func formatPaneID(id [16]byte) string {
	return fmt.Sprintf("%x", id[:4])
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
