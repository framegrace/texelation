// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/app.go
// Summary: Implements app capabilities for the remote client runtime.
// Usage: Embedded by client binaries to handle app as part of the render/event loop.
// Notes: Owns session management, rendering, and protocol interaction for remote front-ends.

package clientruntime

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/internal/debuglog"
	"github.com/framegrace/texelation/internal/keybind"
	"github.com/framegrace/texelation/protocol"
	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/graphics"
	"github.com/framegrace/texelui/theme"
)

const resizeDebounce = 10 * time.Millisecond

// Options configures the remote client runtime.
type Options struct {
	Socket                  string
	Reconnect               bool
	PanicLog                string
	ShowRestartNotification bool   // Show notification that server was restarted
	ClientName              string // --client-name slot for multi-client persistence (issue #199 Plan D)
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

	// Plan D: load persisted client state if any. Failures (missing,
	// parse error, mismatch) all yield (nil, nil) and we proceed as
	// fresh.
	statePath, statePathErr := ResolvePath(opts.Socket, opts.ClientName)
	if statePathErr != nil {
		log.Printf("persistence: path resolution failed (%v); running without persistence", statePathErr)
	}
	var loadedState *ClientState
	if statePath != "" {
		ls, err := Load(statePath, opts.Socket)
		if err != nil {
			log.Printf("persistence: load failed (%v); running fresh", err)
		} else {
			loadedState = ls
		}
	}

	var sessionID [16]byte
	if loadedState != nil {
		sessionID = loadedState.SessionID
	}

	accept, conn, err := simple.Connect(&sessionID)
	if err != nil && loadedState != nil {
		// We sent a non-zero sessionID from disk and Connect failed.
		// The dominant cause is a stale sessionID: the server has
		// evicted the session (or the daemon was restarted without
		// Plan D2 persistence). Wipe the stale state and retry fresh
		// with a zero sessionID.
		//
		// We don't try to disambiguate stale-session from transient
		// network failure — retrying once with zero ID is cheap and
		// the second failure (if there is one) surfaces below as the
		// terminal connect error.
		log.Printf("persistence: connect with persisted sessionID failed (%v); wiping state file and retrying fresh", err)
		if statePath != "" {
			if werr := Wipe(statePath); werr != nil {
				log.Printf("persistence: wipe failed (%v); next start may repeat this rejection", werr)
			}
		}
		loadedState = nil
		sessionID = [16]byte{}
		accept, conn, err = simple.Connect(&sessionID)
	}
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	// Closure form so the deferred Close picks up any subsequent
	// reassignment of conn (none today, but defends against future
	// re-connect logic). Drop-in replacement for the older
	// `defer conn.Close()` shape.
	defer func() { conn.Close() }()

	var writeMu sync.Mutex

	debuglog.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))

	state := &clientState{
		cache:                   client.NewBufferCache(),
		viewports:               newViewportTrackers(),
		themeValues:             make(map[string]map[string]interface{}),
		defaultStyle:            tcell.StyleDefault,
		defaultFg:               tcell.ColorDefault,
		defaultBg:               tcell.ColorDefault,
		desktopBg:               tcell.ColorDefault,
		selectionFg:             tcell.ColorBlack,
		selectionBg:             tcell.NewRGBColor(232, 217, 255),
		showRestartNotification: opts.ShowRestartNotification,
	}

	// Wire connection context for FlushFrame (set once, never mutated).
	state.conn = conn
	state.writeMu = &writeMu
	state.sessionID = accept.SessionID

	// Load keybindings from config file or use platform defaults.
	state.keybindings = loadKeybindings()

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
	var lastSequence atomic.Uint64
	var lastSeqStart uint64
	if loadedState != nil {
		lastSeqStart = loadedState.LastSequence
	}
	lastSequence.Store(lastSeqStart) // lastSequence is atomic.Uint64 from Task 10

	var pendingAck atomic.Uint64
	var lastAck atomic.Uint64
	ackSignal := make(chan struct{}, 1)

	// Decide whether to send a resume: explicit --reconnect OR we
	// loaded a non-zero sessionID from disk.
	//
	// Note: state.cache and state.viewports are intentionally empty at
	// this point in a fresh-process invocation (no MsgTreeSnapshot
	// received yet, no panes rendered). The persisted PaneViewports
	// from disk are what feed the resume; live trackers are only used
	// for the same-process --reconnect case where they may be populated.
	shouldResume := opts.Reconnect || loadedState != nil
	if shouldResume {
		// Prefer persisted PaneViewports (fresh process, trackers map
		// is empty); fall back to live trackers for the same-process
		// reconnect case.
		var viewports []protocol.PaneViewportState
		if loadedState != nil && len(loadedState.PaneViewports) > 0 {
			viewports = loadedState.PaneViewports
		} else {
			for _, e := range state.viewports.snapshotAll() {
				viewports = append(viewports, protocol.PaneViewportState{
					PaneID:         e.id,
					AltScreen:      e.vp.AltScreen,
					AutoFollow:     e.vp.AutoFollow,
					ViewBottomIdx:  e.vp.ViewBottomIdx,
					WrapSegmentIdx: e.vp.WrapSegmentIdx,
					ViewportRows:   e.vp.Rows,
					ViewportCols:   e.vp.Cols,
				})
			}
		}

		hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence.Load(), viewports)
		if err != nil {
			// Resume against a session that completed handshake should
			// not normally fail. If it does, surface the error rather
			// than retrying — the connection is in an indeterminate
			// state and recovery is the user's job.
			return fmt.Errorf("resume request failed: %w", err)
		}
		handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence, &writeMu, &pendingAck, ackSignal)
	}

	// Plan D: install debounced persistence Writer. nil-safe — if path
	// resolution failed, persistence is silently disabled.
	var persistWriter *Writer
	if statePath != "" {
		persistWriter = NewWriter(statePath, 250*time.Millisecond)
		defer persistWriter.Close() // flushes synchronously and waits for in-flight ticks
	}

	// persistSnapshot builds the current ClientState and hands it to
	// the debounced Writer. Called from flushFrame (rate-limited to
	// once per render iteration) and on exit.
	//
	// Note: lastSequence is atomic.Uint64 (Task 10), so .Load() is
	// race-safe even though readLoop mutates it from another goroutine.
	// sessionID is captured by reference — Task 11's retry path passes
	// &sessionID into simple.Connect, which writes the freshly-allocated
	// session ID back through the pointer (simple_client.go:91), so
	// persistSnapshot always reads the current value at invocation time.
	//
	// IMPORTANT: there is NO eager initial seed. A persistSnapshot call
	// here would write LastSequence=0 with empty PaneViewports (because
	// no panes have rendered yet), which would overwrite the previous
	// session's state on a fast crash before the first frame. Wait for
	// the first real flushFrame to trigger the first save instead.
	persistSnapshot := func() {
		if persistWriter == nil {
			return
		}
		persistWriter.Update(ClientState{
			SocketPath:    opts.Socket,
			SessionID:     sessionID,
			LastSequence:  lastSequence.Load(),
			WrittenAt:     time.Now().UTC(),
			PaneViewports: state.viewports.snapshotForPersistence(),
		})
	}
	state.persistSnapshot = persistSnapshot

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

	// Initialize Kitty graphics output if the terminal supports it.
	if graphics.DetectCapability() == texelcore.GraphicsKitty {
		state.kitty = newKittyOutput()
		if tty, ok := screen.Tty(); ok {
			state.ttyWriter = tty
		}
		defer func() {
			if state.ttyWriter != nil {
				// Clear all Kitty images from terminal on exit.
				fmt.Fprint(state.ttyWriter, "\x1b_Ga=d,d=a,q=2;\x1b\\")
			}
		}()
	}

	// Send ClientReady with our dimensions so server can send properly-sized snapshot
	sendClientReady(&writeMu, conn, sessionID, screen)

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

	const dt = 33 * time.Millisecond // ~30fps fixed timestep

	// Unified ticker: started when animations or effects are active, stopped when idle.
	var ticker *time.Ticker
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()

	for {
		// Start or stop the unified ticker.
		animating := state.dynAnimating
		if state.effects != nil && state.effects.HasActiveAnimations() {
			animating = true
		}
		if animating && ticker == nil {
			ticker = time.NewTicker(dt)
		} else if !animating && ticker != nil {
			ticker.Stop()
			ticker = nil
		}

		// Build channel ref — nil channel blocks forever in select.
		var tickCh <-chan time.Time
		if ticker != nil {
			tickCh = ticker.C
		}

		select {
		case <-tickCh:
			// Fixed-timestep tick: advance time, update effects, render.
			state.tickAccum += dt.Seconds()
			state.frameDT = float32(dt.Seconds())
			if state.effects != nil {
				state.effects.Update(dt)
			}
			// Skip render frames when the active effect requests it.
			// Effects update at 30fps for smooth timelines, but heavy
			// full-screen effects can render less often to reduce terminal
			// output (which dominates CPU in both client and host terminal).
			state.animFrameCount++
			if state.effects != nil {
				if skip := state.effects.ActiveFrameSkip(); skip > 1 && state.animFrameCount%uint64(skip) != 0 {
					continue
				}
			}
			render(state, screen)
			state.frameDT = 0

		case <-renderCh:
			// Data-driven render: delta/snapshot arrived. Render immediately, no time advance.
			state.frameDT = 0
			if state.effects != nil {
				state.effects.Update(0)
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
			debuglog.Printf("CLIPBOARD DEBUG: Setting system clipboard: len=%d", len(clip.Data))
			screen.SetClipboard(clip.Data)
		}
	}
}

func loadKeybindings() *keybind.Registry {
	preset := "auto"
	var extraPreset string
	var overrides map[string][]string

	home, err := os.UserHomeDir()
	if err == nil {
		data, err := os.ReadFile(filepath.Join(home, ".config", "texelation", "keybindings.json"))
		if err == nil {
			var cfg struct {
				Preset      string              `json:"preset"`
				ExtraPreset string              `json:"extraPreset"`
				Actions     map[string][]string `json:"actions"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				if cfg.Preset != "" {
					preset = cfg.Preset
				}
				extraPreset = cfg.ExtraPreset
				overrides = cfg.Actions
			}
		}
	}

	return keybind.NewRegistry(preset, extraPreset, overrides)
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
