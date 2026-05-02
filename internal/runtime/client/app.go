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

// Stage names a milestone in client startup that an external splash
// (texelation's boot package) renders to the user. Stage strings are
// stable identifiers — the splash maps them to its own visual state.
type Stage string

const (
	// StageConnecting fires before the protocol handshake.
	StageConnecting Stage = "connecting"
	// StageResuming fires before the resume request is sent. Skipped
	// when no resume is needed (fresh start).
	StageResuming Stage = "resuming"
	// StageReady fires once the renderer is about to enter its main
	// event loop. The splash should stop after seeing this.
	StageReady Stage = "ready"
)

// StatusFn receives stage transitions during startup. The detail
// string is optional context (e.g. "retrying after stale session").
// The callback is invoked synchronously from Run's goroutine; keep
// it cheap — defer expensive work to the splash's own paint loop.
type StatusFn func(stage Stage, detail string)

// Options configures the remote client runtime.
type Options struct {
	Socket                  string
	Reconnect               bool
	PanicLog                string
	ShowRestartNotification bool   // Show notification that server was restarted
	ClientName              string // --client-name slot for multi-client persistence (issue #199 Plan D)

	// Screen, when set, is used in place of a freshly-created tcell
	// screen. The caller owns its lifecycle: Init must already have
	// been called and Fini is the caller's responsibility. This is
	// how texelation hands off a screen that already showed a boot
	// splash without forcing a Fini/Init flicker. When nil, Run
	// creates and tears down a screen itself (the standalone path).
	Screen tcell.Screen

	// OnStatus, when set, is called at startup milestones (see Stage
	// constants) so an external splash can update its display. nil-safe.
	OnStatus StatusFn
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

	emitStatus := func(stage Stage, detail string) {
		if opts.OnStatus != nil {
			opts.OnStatus(stage, detail)
		}
	}

	emitStatus(StageConnecting, "")
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
		emitStatus(StageConnecting, "retrying with fresh session")
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

	writer := newMessageWriter(conn, 256)
	defer writer.Close()

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
	state.writer = writer
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

		state.resetOnNextSnapshot.Store(true)
		emitStatus(StageResuming, "")
		hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence.Load(), viewports, func(msg string) {
			emitStatus(StageResuming, msg)
		})
		if err != nil {
			// Plan D2: a failed resume must NOT leave the flag armed — a later
			// resume against a different sessionID (after Plan D's wipe-and-retry
			// fallback) would otherwise consume the stale flag and reset against
			// the wrong synchronization barrier.
			state.resetOnNextSnapshot.Store(false)
			// Resume against a session that completed handshake should
			// not normally fail. If it does, surface the error rather
			// than retrying — the connection is in an indeterminate
			// state and recovery is the user's job.
			return fmt.Errorf("resume request failed: %w", err)
		}
		handleControlMessage(state, hdr, payload, sessionID, &lastSequence, writer, &pendingAck, ackSignal)
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
	// Wire MsgBootProgress messages received via readLoop into the
	// splash. RequestResume drains progress messages until its first
	// non-progress response, so anything emitted by the server later
	// (handleClientReady's per-pane progress on rehydrated cold
	// starts) only arrives via the read pump.
	state.setBootProgressFn(func(msg string) {
		emitStatus(StageResuming, msg)
	})
	doneCh := make(chan struct{})
	panicLogger.Go("readLoop", func() {
		readLoop(conn, state, sessionID, &lastSequence, renderCh, doneCh, writer, &pendingAck, ackSignal)
	})
	pingStop := make(chan struct{})
	panicLogger.Go("pingLoop", func() {
		pingLoop(sessionID, doneCh, pingStop, writer)
	})
	panicLogger.Go("ackLoop", func() {
		ackLoop(sessionID, writer, doneCh, &pendingAck, &lastAck, ackSignal)
	})

	// Screen ownership: if the caller passed in a pre-initialized
	// screen via opts.Screen, use it as-is and let the caller Fini.
	// This is the texelation boot-splash handoff path — the screen
	// already has the splash painted on it, and we want the first
	// production frame to land on the same surface without a
	// Fini → Init flicker. Standalone callers (no opts.Screen) get
	// the legacy behaviour where Run owns the lifecycle.
	screen := opts.Screen
	ownsScreen := screen == nil
	if ownsScreen {
		var err error
		screen, err = tcell.NewScreen()
		if err != nil {
			return fmt.Errorf("create screen failed: %w", err)
		}
		if err := screen.Init(); err != nil {
			return fmt.Errorf("init screen failed: %w", err)
		}
	}
	screen.EnablePaste()
	screen.EnableMouse()
	defer screen.DisableMouse()
	screen.HideCursor()
	if ownsScreen {
		defer screen.Fini()
	}
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
	sendClientReady(writer, sessionID, screen)

	// First render before any server content has arrived produces a
	// blank frame — clearing it now would expose that blankness if a
	// boot splash is still on the screen. Skip the empty render and
	// let the first event-loop render (driven by the server's first
	// snapshot/delta on renderCh) be what the splash hands off to.
	// firstContentRendered tracks that handoff: it stays false until
	// the first data-driven render lands actual cells, at which point
	// we emit StageReady so the splash runner can stop without
	// leaving blank cells between itself and live content.
	firstContentRendered := false

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
			// Splash handoff: stop only once a real TreeSnapshot has
			// been applied AND a content-bearing delta has landed.
			//
			// On rehydrated cold starts the server defers
			// TreeSnapshot to handleClientReady (so it lands at the
			// client's real dimensions), and that handler runs the
			// slow SetViewportSize → Snapshot chain. Before that,
			// the publisher emits decor-only BufferDeltas that fire
			// renderCh while the panes are still hydrating.
			//
			// Without the treeSnapshotApplied gate the splash hands
			// off to a workspace that has no real content yet. The
			// race: on the first renderCh tick, ensureBuffers
			// returns resized=true (so fullRender runs and
			// fullRenderHappened flips) and a decor-only BufferDelta
			// already in flight can flip firstContentDelta during
			// render(). Both flags would pass, the splash would
			// stop, and the user would see only borders while
			// handleClientReady's slow SetViewportSize → Snapshot
			// chain finished.
			if !firstContentRendered && state.bootHandoffReady() {
				firstContentRendered = true
				emitStatus(StageReady, "")
			}

		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if !handleScreenEvent(ev, state, screen, sessionID, writer) {
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

// coalesceRenderCh non-blocks-drains every queued tick on ch. The
// client's main event loop calls this after consuming a single
// renderCh tick from its select so a burst of N signals (heavy
// server traffic flooding readLoop's signalRender path) collapses
// into one render call. With render frequency tracking incoming
// delta rate, the loop spent most of its time re-rendering against
// transient intermediate state; coalescing makes render frequency
// bounded by render duration itself.
//
// The function logs (at debuglog level) when it coalesces 2+ ticks
// in one call. Pre-coalesce, the renderCh channel filling its 64
// buffer was the de-facto canary that render() was lagging behind
// readLoop. Coalescing collapses bursts before the buffer can fill,
// so this log line preserves the same diagnostic signal — a tail
// of the debug log shows post-hoc whether bursts were arriving.
func coalesceRenderCh(ch <-chan struct{}) {
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			if count > 1 {
				debuglog.Printf("event loop: coalesced %d renderCh ticks", count)
			}
			return
		}
	}
}

// drainScreenEvents pulls every queued tcell event from events and
// dispatches each via handle. Returns the count drained and ok=false
// if either the channel closed (run-loop exit signal) or the handle
// returned false (also a run-loop exit signal). Non-blocking: empty
// channel returns (0, true) immediately and the caller's select can
// block on the next signal.
//
// The handle callback is the call site's closure over
// handleScreenEvent + state/screen/sessionID/writer; passing it as
// a parameter keeps this helper pure and unit-testable without a
// full clientState fixture.
func drainScreenEvents(events <-chan tcell.Event, handle func(tcell.Event) bool) (drained int, ok bool) {
	for {
		select {
		case ev, chOK := <-events:
			if !chOK {
				return drained, false
			}
			if !handle(ev) {
				return drained, false
			}
			drained++
		default:
			return drained, true
		}
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
