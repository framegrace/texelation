// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/desktop.go
// Summary: Implements desktop capabilities for the core desktop engine.
// Usage: Used throughout the project to implement desktop inside the desktop and panes.

package texel

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"github.com/framegrace/texelation/config"
	"github.com/framegrace/texelation/registry"
	"github.com/framegrace/texelui/theme"
	"time"
)

// AppRegistry is an alias for the registry.Registry type.
type AppRegistry = registry.Registry

type PaneRect struct {
	x, y, w, h int
}

func newStatusPaneID(app App) [16]byte {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return id
	}
	fingerprint := fmt.Sprintf("status:%p:%d", app, time.Now().UnixNano())
	sum := sha1.Sum([]byte(fingerprint))
	copy(id[:], sum[:])
	return id
}

// eventKind identifies the type of desktop event.
type eventKind int

const (
	keyEventKind eventKind = iota
	mouseEventKind
	pasteEventKind
	resizeEventKind
	syncEventKind
)

// desktopEvent is a tagged union for all events processed by the desktop event loop.
type desktopEvent struct {
	kind    eventKind
	key     tcell.Key
	ch      rune
	mod     tcell.ModMask
	mx, my  int
	buttons tcell.ButtonMask
	paste   []byte
	width   int
	height  int
	done    chan struct{} // used by syncEventKind
}

// animationFrame carries interpolated ratios from the animation ticker to the event loop.
type animationFrame struct {
	node       *Node
	ratios     []float64
	done       bool
	onComplete func()
}

// Desktop manages a collection of workspaces (Screens).
type DesktopEngine struct {
	display           ScreenDriver
	workspaces        map[int]*Workspace
	activeWorkspace   *Workspace
	statusPanes       []*StatusPane
	floatingPanels    []*FloatingPanel
	quit              chan struct{}
	eventCh           chan desktopEvent   // Key/mouse/paste/resize from connection goroutine
	animCh            chan animationFrame // Animation ratio updates from ticker
	refreshCh         chan struct{}       // Pane dirty signals
	closeOnce         sync.Once
	ShellAppFactory   AppFactory
	InitAppName       string
	styleCache        map[styleKey]tcell.Style
	DefaultFgColor    tcell.Color
	DefaultBgColor    tcell.Color
	dispatcher        *EventDispatcher
	statusBuffer      BufferStore
	appLifecycle      AppLifecycleManager
	registry          *AppRegistry
	storage           StorageService
	layoutTransitions *LayoutTransitionManager

	// Global state now lives on the Desktop
	inControlMode   bool
	subControlMode  rune
	resizeSelection *selectedBorder
	zoomedPane      *Node

	mouseMu            sync.Mutex
	lastMouseX         int
	lastMouseY         int
	lastMouseButtons   tcell.ButtonMask
	lastMouseModifier  tcell.ModMask
	clipboardMu        sync.Mutex
	clipboard          map[string][]byte
	clipboardMime      string
	clipboardPending   bool // True when clipboard has changed and needs to be sent to client
	focusMu              sync.RWMutex
	focusListeners       []DesktopFocusListener
	paneStateMu          sync.RWMutex
	paneStateListeners   []PaneStateListener
	snapshotFactories    map[string]SnapshotFactory
	viewportMu           sync.RWMutex
	viewportWidth        int
	viewportHeight       int
	hasViewport          bool

	// pendingAppStarts tracks panes from snapshot restore that need to be started
	// once we receive actual viewport dimensions from the client.
	pendingAppStartsMu sync.Mutex
	pendingAppStarts   []*pane

	stateMu      sync.Mutex
	hasLastState bool
	lastState    StatePayload

	refreshMu      sync.RWMutex
	refreshHandler func()

	lastPublishNanos atomic.Int64
}

// FloatingPanel represents an app floating above the workspace.
type FloatingPanel struct {
	app      App
	pipeline RenderPipeline // For events and rendering (from PipelineProvider)
	x, y     int
	width    int
	height   int
	modal    bool
	id       [16]byte
}

func newFloatingPanelID(app App) [16]byte {
	var id [16]byte
	if _, err := rand.Read(id[:]); err == nil {
		return id
	}
	fingerprint := fmt.Sprintf("float:%p:%d", app, time.Now().UnixNano())
	sum := sha1.Sum([]byte(fingerprint))
	copy(id[:], sum[:])
	return id
}

// PaneStateSnapshot captures dynamic pane flags for external consumers.
type PaneStateSnapshot struct {
	ID               [16]byte
	Active           bool
	Resizing         bool
	ZOrder           int
	HandlesMouse bool
}

// NewDesktopEngine creates and initializes a new desktop engine.
func NewDesktopEngine(shellFactory AppFactory, initAppName string) (*DesktopEngine, error) {
	tcellScreen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}

	driver := NewTcellScreenDriver(tcellScreen)
	lifecycle := &LocalAppLifecycle{}
	return NewDesktopEngineWithDriver(driver, shellFactory, initAppName, lifecycle)
}

// NewDesktopEngineWithDriver wires a DesktopEngine using the provided screen driver and
// lifecycle manager. It exists primarily to support tests and future remote
// runtimes.
func NewDesktopEngineWithDriver(driver ScreenDriver, shellFactory AppFactory, initAppName string, lifecycle AppLifecycleManager) (*DesktopEngine, error) {
	if driver == nil {
		return nil, fmt.Errorf("screen driver is required")
	}
	if lifecycle == nil {
		lifecycle = &LocalAppLifecycle{}
	}

	if err := driver.Init(); err != nil {
		return nil, err
	}

	tm := theme.Get()
	defbg := tm.GetSemanticColor("bg.base").TrueColor()
	deffg := tm.GetSemanticColor("text.primary").TrueColor()
	defStyle := tcell.StyleDefault.Background(defbg).Foreground(deffg)
	driver.SetStyle(defStyle)
	driver.HideCursor()

	// Initialize app registry
	reg := registry.New()

	// Register built-in shell app (wrap texel.AppFactory to registry.AppFactory)
	reg.RegisterBuiltIn(&registry.Manifest{
		Name:        "texelterm",
		DisplayName: "Terminal",
		Description: "Terminal emulator",
		Icon:        "💻",
		Category:    "system",
		ThemeSchema: registry.ThemeSchema{
			"selection": {"highlight_bg", "highlight_fg"},
			"ui":        {"text.primary", "bg.base"},
		},
	}, func() interface{} { return shellFactory() })

	// Note: Other apps (launcher, welcome, etc.) are registered in main.go
	// after Desktop is created, since launcher needs access to the registry

	// Parse layout transitions config from system config.
	layoutTransitionsConfig := loadLayoutTransitionsConfig()

	d := &DesktopEngine{
		display:            driver,
		workspaces:         make(map[int]*Workspace),
		statusPanes:        make([]*StatusPane, 0),
		floatingPanels:     make([]*FloatingPanel, 0),
		quit:               make(chan struct{}),
		eventCh:            make(chan desktopEvent, 64),
		animCh:             make(chan animationFrame, 16),
		refreshCh:          make(chan struct{}, 16),
		ShellAppFactory:    shellFactory,
		InitAppName:        initAppName,
		styleCache:         make(map[styleKey]tcell.Style),
		DefaultFgColor:     deffg,
		DefaultBgColor:     defbg,
		dispatcher:         NewEventDispatcher(),
		statusBuffer:       NewInMemoryBufferStore(),
		appLifecycle:       lifecycle,
		registry:           reg,
		inControlMode:      false,
		subControlMode:     0,
		clipboard:          make(map[string][]byte),
		focusListeners:     make([]DesktopFocusListener, 0),
		paneStateListeners: make([]PaneStateListener, 0),
		snapshotFactories:  make(map[string]SnapshotFactory),
	}

	// Initialize layout transitions manager (needs desktop pointer, so created after struct)
	d.layoutTransitions = NewLayoutTransitionManager(layoutTransitionsConfig, d)

	// Initialize storage service
	homeDir, err := os.UserHomeDir()
	if err == nil {
		storageBaseDir := filepath.Join(homeDir, ".texelation")
		if storage, err := NewStorageService(storageBaseDir); err == nil {
			d.storage = storage
		} else {
			log.Printf("Warning: Failed to initialize storage service: %v", err)
		}
	}

	// Scan for external apps
	d.loadApps()

	log.Printf("NewDesktop: Created with inControlMode=%v, %d apps registered, InitAppName=%s", d.inControlMode, d.registry.Count(), d.InitAppName)

	return d, nil
}

// loadApps scans the user's app directory and loads available apps.
func (d *DesktopEngine) loadApps() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Printf("Desktop: Failed to get user config dir: %v", err)
		return
	}

	appsDir := filepath.Join(configDir, "texelation", "apps")
	if err := d.registry.Scan(appsDir); err != nil {
		log.Printf("Desktop: Failed to scan apps: %v", err)
	}
}

// ForceRefresh clears caches and triggers a full repaint.
// When triggered by SIGHUP, this also reloads theme and apps.
func (d *DesktopEngine) ForceRefresh() {
	log.Println("Desktop: ForceRefresh triggered")

	// Reload apps
	d.loadApps()

	// Reload layout transitions configuration from system config.
	d.reloadLayoutTransitions()

	// Clear style cache to force re-evaluation of colors
	d.styleCache = make(map[styleKey]tcell.Style)

	// Trigger a full layout and state broadcast
	d.recalculateLayout()
	d.broadcastStateUpdate()
	d.broadcastTreeChanged()

	log.Println("Desktop: Broadcasting EventThemeChanged")
	d.dispatcher.Broadcast(Event{Type: EventThemeChanged})

	// Notify refresh handler if one is set (to wake up the loop)
	if d.refreshHandler != nil {
		d.refreshHandler()
	}
}

// reloadLayoutTransitions reads layout transition config from system config and updates the manager.
func (d *DesktopEngine) reloadLayoutTransitions() {
	if d.layoutTransitions == nil {
		return
	}

	// Parse layout transitions config from system config (same as initialization).
	config := loadLayoutTransitionsConfig()

	log.Printf("Desktop: Reloading layout transitions config: enabled=%v, duration=%dms, easing=%s",
		config.Enabled, config.DurationMs, config.Easing)

	d.layoutTransitions.UpdateConfig(config)
}

func loadLayoutTransitionsConfig() LayoutTransitionConfig {
	cfg := config.System()
	return LayoutTransitionConfig{
		Enabled:      cfg.GetBool("layout_transitions", "enabled", true),
		DurationMs:   cfg.GetInt("layout_transitions", "duration_ms", 300),
		Easing:       cfg.GetString("layout_transitions", "easing", "smoothstep"),
		MinThreshold: cfg.GetInt("layout_transitions", "min_threshold", 3),
	}
}

func (d *DesktopEngine) Subscribe(listener Listener) {
	d.dispatcher.Subscribe(listener)
}

func (d *DesktopEngine) Unsubscribe(listener Listener) {
	d.dispatcher.Unsubscribe(listener)
}

// Registry returns the app registry for this desktop.
func (d *DesktopEngine) Registry() *AppRegistry {
	return d.registry
}

// Storage returns the storage service for this desktop.
func (d *DesktopEngine) Storage() StorageService {
	return d.storage
}

// ActiveWorkspace returns the currently active workspace.
func (d *DesktopEngine) ActiveWorkspace() *Workspace {
	return d.activeWorkspace
}

// RegisterSnapshotFactory registers a factory used to restore apps from snapshot metadata.
func (d *DesktopEngine) RegisterSnapshotFactory(appType string, factory SnapshotFactory) {
	if appType == "" || factory == nil {
		return
	}
	d.snapshotFactories[appType] = factory
}

func (d *DesktopEngine) viewportSize() (int, int) {
	d.viewportMu.RLock()
	defer d.viewportMu.RUnlock()
	if d.hasViewport && d.viewportWidth > 0 && d.viewportHeight > 0 {
		return d.viewportWidth, d.viewportHeight
	}
	return d.display.Size()
}

func (d *DesktopEngine) applyThemeChange() {
	d.styleCache = make(map[styleKey]tcell.Style)
	d.recalculateLayout()
	d.broadcastStateUpdate()
	d.broadcastTreeChanged()
	d.dispatcher.Broadcast(Event{Type: EventThemeChanged})
	if d.refreshHandler != nil {
		d.refreshHandler()
	}
}

// HandleThemeUpdate applies runtime theme overrides.
func (d *DesktopEngine) HandleThemeUpdate(section, key, value string) {
	config := theme.Get()
	if _, ok := config[section]; !ok {
		config[section] = theme.Section{}
	}
	config[section][key] = value
}

// LastMousePosition returns the most recently recorded mouse coordinates.
func (d *DesktopEngine) LastMousePosition() (int, int) {
	d.mouseMu.Lock()
	defer d.mouseMu.Unlock()
	return d.lastMouseX, d.lastMouseY
}

// LastMouseButtons exposes the last recorded button mask.
func (d *DesktopEngine) LastMouseButtons() tcell.ButtonMask {
	d.mouseMu.Lock()
	defer d.mouseMu.Unlock()
	return d.lastMouseButtons
}

// LastMouseModifiers exposes the last recorded modifier mask.
func (d *DesktopEngine) LastMouseModifiers() tcell.ModMask {
	d.mouseMu.Lock()
	defer d.mouseMu.Unlock()
	return d.lastMouseModifier
}

// broadcastStateUpdate now broadcasts on the Desktop's dispatcher
func (d *DesktopEngine) broadcastStateUpdate() {
	if d.activeWorkspace == nil {
		return
	}
	var title string
	if d.zoomedPane != nil {
		if d.zoomedPane.Pane != nil {
			title = d.zoomedPane.Pane.getTitle()
		}
	} else {
		title = d.activeWorkspace.tree.ActiveTitle()
	}

	allWsIDs := make([]int, 0, len(d.workspaces))
	for id := range d.workspaces {
		allWsIDs = append(allWsIDs, id)
	}
	sort.Ints(allWsIDs)

	payload := d.currentStatePayload(allWsIDs, title)

	if !d.shouldBroadcastState(payload) {
		return
	}

	d.dispatcher.Broadcast(Event{
		Type:    EventStateUpdate,
		Payload: payload,
	})
	//	if d.activeWorkspace != nil {
	//		d.activeWorkspace.Refresh()
	//	}
}

func (d *DesktopEngine) SetRefreshHandler(handler func()) {
	d.refreshMu.Lock()
	d.refreshHandler = func() {
		d.broadcastStateUpdate()
		if handler != nil {
			handler()
		}
	}
	d.refreshMu.Unlock()
}

// SetLastPublishDuration records the duration of the most recent publish cycle.
func (d *DesktopEngine) SetLastPublishDuration(dur time.Duration) {
	d.lastPublishNanos.Store(dur.Nanoseconds())
}

func (d *DesktopEngine) refreshHandlerFunc() func() {
	d.refreshMu.RLock()
	defer d.refreshMu.RUnlock()
	return d.refreshHandler
}

func (d *DesktopEngine) broadcastTreeChanged() {
	d.dispatcher.Broadcast(Event{Type: EventTreeChanged})
}

func (d *DesktopEngine) shouldBroadcastState(payload StatePayload) bool {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	if !d.hasLastState {
		d.storeLastState(payload)
		return true
	}
	if d.lastState.equal(payload) {
		return false
	}
	d.storeLastState(payload)
	return true
}

func (d *DesktopEngine) storeLastState(payload StatePayload) {
	d.lastState = payload
	if payload.AllWorkspaces != nil {
		d.lastState.AllWorkspaces = append([]int(nil), payload.AllWorkspaces...)
	}
	d.hasLastState = true
}

func (d *DesktopEngine) currentStatePayload(allWsIDs []int, title string) StatePayload {
	if allWsIDs == nil {
		allWsIDs = make([]int, 0, len(d.workspaces))
		for id := range d.workspaces {
			allWsIDs = append(allWsIDs, id)
		}
		sort.Ints(allWsIDs)
	}
	if title == "" {
		if d.inControlMode && d.zoomedPane != nil {
			if d.zoomedPane.Pane != nil {
				title = d.zoomedPane.Pane.getTitle()
			}
		} else if d.activeWorkspace != nil {
			title = d.activeWorkspace.tree.ActiveTitle()
		}
	}
	workspaceID := 0
	if d.activeWorkspace != nil {
		workspaceID = d.activeWorkspace.id
	}
	var zoomID [16]byte
	zoomed := false
	if d.zoomedPane != nil && d.zoomedPane.Pane != nil {
		zoomed = true
		zoomID = d.zoomedPane.Pane.ID()
	}
	return StatePayload{
		AllWorkspaces:       allWsIDs,
		WorkspaceID:         workspaceID,
		InControlMode:       d.inControlMode,
		SubMode:             d.subControlMode,
		ActiveTitle:         title,
		DesktopBgColor:      d.DefaultBgColor,
		Zoomed:              zoomed,
		ZoomedPaneID:        zoomID,
		LastPublishDuration: time.Duration(d.lastPublishNanos.Load()),
	}
}

// CurrentStatePayload exposes the latest desktop state snapshot.
func (d *DesktopEngine) CurrentStatePayload() StatePayload {
	return d.currentStatePayload(nil, "")
}

func (d *DesktopEngine) SwitchToWorkspace(id int) {
	if d.activeWorkspace != nil && d.activeWorkspace.id == id {
		return
	}

	if d.zoomedPane != nil {
		d.zoomedPane = nil
	}

	if ws, exists := d.workspaces[id]; exists {
		d.activeWorkspace = ws
	} else {
		ws, err := newWorkspace(id, d.ShellAppFactory, d.appLifecycle, d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating workspace: %v\n", err)
			return
		}
		d.workspaces[id] = ws
		d.activeWorkspace = ws

		if d.InitAppName != "" {
			if appInstance := d.registry.CreateApp(d.InitAppName, nil); appInstance != nil {
				if app, ok := appInstance.(App); ok {
					ws.AddApp(app)
				} else {
					log.Printf("SwitchToWorkspace: Created app '%s' but it does not implement App interface", d.InitAppName)
				}
			} else {
				log.Printf("SwitchToWorkspace: Failed to create init app '%s'", d.InitAppName)
			}
		}
	}

	// Apply current control mode state to the new workspace
	if d.activeWorkspace != nil {
		d.activeWorkspace.SetControlMode(d.inControlMode)
	}

	for _, sp := range d.statusPanes {
		sp.app.SetRefreshNotifier(d.makeRefreshNotifier())
	}
	d.recalculateLayout()
	d.broadcastStateUpdate()
	d.notifyFocusActive()
	d.broadcastTreeChanged()
}

func (d *DesktopEngine) forEachPane(fn func(*pane)) {
	if fn == nil {
		return
	}
	for _, ws := range d.workspaces {
		if ws == nil || ws.tree == nil {
			continue
		}
		var traverse func(*Node)
		traverse = func(node *Node) {
			if node == nil {
				return
			}
			if node.Pane != nil {
				fn(node.Pane)
			}
			for _, child := range node.Children {
				traverse(child)
			}
		}
		traverse(ws.tree.Root)
	}
}

// PaneStates returns the current pane flags across all workspaces.
func (d *DesktopEngine) PaneStates() []PaneStateSnapshot {
	states := make([]PaneStateSnapshot, 0)
	d.forEachPane(func(p *pane) {
		states = append(states, PaneStateSnapshot{
			ID:               p.ID(),
			Active:           p.IsActive,
			Resizing:         p.IsResizing,
			ZOrder:           p.ZOrder,
			HandlesMouse: p.handlesMouseEvents(),
		})
	})
	for _, fp := range d.floatingPanels {
		states = append(states, PaneStateSnapshot{
			ID:               fp.id,
			Active:           true,
			Resizing:         false,
			ZOrder:           ZOrderFloating,
			HandlesMouse: false,
		})
	}
	return states
}

// SetViewportSize overrides the desktop viewport dimensions, typically used by
// remote clients to dictate layout size.
func (d *DesktopEngine) SetViewportSize(cols, rows int) {
	d.viewportMu.Lock()
	d.viewportWidth = cols
	d.viewportHeight = rows
	d.hasViewport = cols > 0 && rows > 0
	d.viewportMu.Unlock()
	d.recalculateLayout()

	// Start any apps that were waiting for actual viewport dimensions.
	// This handles the case where snapshot restore prepared apps before
	// the client sent its viewport size.
	d.startPendingApps()

	// NOTE: We intentionally do NOT call d.activeWorkspace.Refresh() here.
	// Both callers (handleResize, handleClientReady) do explicit publishes
	// after this method returns.  A background refresh would race with the
	// explicit publish, potentially enqueuing buffer deltas after the
	// caller's sendPending(), causing the client to render new content at
	// stale pane positions (border flicker).
}

// startPendingApps starts any apps that were deferred during snapshot restore.
// Called when we receive actual viewport dimensions from the client.
func (d *DesktopEngine) startPendingApps() {
	d.pendingAppStartsMu.Lock()
	pending := d.pendingAppStarts
	d.pendingAppStarts = nil
	d.pendingAppStartsMu.Unlock()

	if len(pending) == 0 {
		return
	}

	log.Printf("[RESTORE] Starting %d apps that were waiting for viewport dimensions", len(pending))
	for _, p := range pending {
		p.StartPreparedApp()
	}

	// Schedule a follow-up refresh after a short delay. Apps start in
	// goroutines, so the shell prompt may arrive after handleClientReady
	// has already published its initial snapshot. This delayed refresh
	// ensures any late-arriving content (like the prompt) gets published.
	time.AfterFunc(250*time.Millisecond, func() {
		d.SendRefresh()
	})
}

func (d *DesktopEngine) appFromSnapshot(snap PaneSnapshot) App {
	if snap.AppType != "" {
		if factory, ok := d.snapshotFactories[snap.AppType]; ok {
			cfg := cloneAppConfig(snap.AppConfig)
			if app := factory(snap.Title, cfg); app != nil {
				return app
			}
		}
	}
	return NewSnapshotApp(snap.Title, snap.Buffer)
}

func (d *DesktopEngine) Close() {
	d.closeOnce.Do(func() {
		close(d.quit)
		for _, ws := range d.workspaces {
			ws.Close()
		}
		for _, sp := range d.statusPanes {
			d.appLifecycle.StopApp(sp.app)
		}
		// Flush and close storage service
		if d.storage != nil {
			if err := d.storage.Close(); err != nil {
				log.Printf("Error closing storage: %v", err)
			}
		}
		if d.display != nil {
			d.display.Fini()
		}
	})
}

func (d *DesktopEngine) getStyle(fg, bg tcell.Color, bold, underline, reverse bool) tcell.Style {
	key := styleKey{fg: fg, bg: bg, bold: bold, underline: underline, reverse: reverse}
	if st, ok := d.styleCache[key]; ok {
		return st
	}
	st := tcell.StyleDefault.Foreground(fg).Background(bg)
	if bold {
		st = st.Bold(true)
	}
	if underline {
		st = st.Underline(true)
	}
	if reverse {
		st = st.Reverse(true)
	}
	d.styleCache[key] = st
	return st
}

// queryTerminalColors attempts to query the terminal for its default colors with a timeout.
func queryTerminalColors(ctx context.Context) (fg tcell.Color, bg tcell.Color, err error) {
	// Default to standard colors in case of any error
	fg = tcell.ColorWhite
	bg = tcell.ColorBlack

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fg, bg, fmt.Errorf("could not open /dev/tty: %w", err)
	}
	defer tty.Close()

	// Use a context to make sure we don't block forever on raw mode or reads
	done := make(chan struct{})
	go func() {
		defer close(done)

		var state *term.State
		state, err = term.MakeRaw(int(tty.Fd()))
		if err != nil {
			err = fmt.Errorf("failed to make raw terminal: %w", err)
			return
		}
		defer term.Restore(int(tty.Fd()), state)

		query := func(code int) (tcell.Color, error) {
			seq := fmt.Sprintf("\x1b]%d;?\a", code)
			if _, writeErr := tty.WriteString(seq); writeErr != nil {
				return tcell.ColorDefault, writeErr
			}

			resp := make([]byte, 0, 64)
			buf := make([]byte, 1)

			// Loop with context cancellation check
			for {
				select {
				case <-ctx.Done():
					return tcell.ColorDefault, ctx.Err()
				default:
				}

				// Set a very short deadline on each read to make the loop responsive to cancellation
				readDeadline := time.Now().Add(10 * time.Millisecond)
				if deadline, ok := ctx.Deadline(); ok && deadline.Before(readDeadline) {
					readDeadline = deadline
				}
				tty.SetReadDeadline(readDeadline)

				n, readErr := tty.Read(buf)
				if readErr != nil {
					if os.IsTimeout(readErr) {
						continue // Expected timeout, check context and loop again
					}
					return tcell.ColorDefault, fmt.Errorf("failed to read from tty: %w", readErr)
				}
				resp = append(resp, buf[:n]...)
				// BEL or ST terminates the response
				if buf[0] == '\a' || (len(resp) > 1 && resp[len(resp)-2] == '\x1b' && resp[len(resp)-1] == '\\') {
					break
				}
			}

			pattern := fmt.Sprintf(`\x1b\]%d;rgb:([0-9A-Fa-f]{1,4})/([0-9A-Fa-f]{1,4})/([0-9A-Fa-f]{1,4})`, code)
			re := regexp.MustCompile(pattern)
			m := re.FindStringSubmatch(string(resp))
			if len(m) != 4 {
				return tcell.ColorDefault, fmt.Errorf("unexpected response format: %q", resp)
			}

			hex2int := func(s string) (int32, error) {
				// Pad to 4 characters for consistent parsing (e.g., "ff" -> "00ff")
				if len(s) < 4 {
					s = "00" + s
					s = s[len(s)-4:]
				}
				v, err := strconv.ParseInt(s, 16, 32)
				// Scale 16-bit color down to 8-bit for tcell
				return int32(v / 257), err
			}
			r, _ := hex2int(m[1])
			g, _ := hex2int(m[2])
			b, _ := hex2int(m[3])

			return tcell.NewRGBColor(r, g, b), nil
		}

		var queryErr error
		fg, queryErr = query(10)
		if queryErr != nil {
			err = fmt.Errorf("failed to query foreground color: %w", queryErr)
			// Don't return yet, try to get the background color
		}

		bg, queryErr = query(11)
		if queryErr != nil {
			err = fmt.Errorf("failed to query background color: %w", queryErr)
		}
	}()

	select {
	case <-ctx.Done():
		return tcell.ColorWhite, tcell.ColorBlack, ctx.Err()
	case <-done:
		// The goroutine finished, return its results.
		// If 'err' is not nil, the default fg/bg will be used.
		return fg, bg, err
	}
}

func initDefaultColors() (tcell.Color, tcell.Color, error) {
	// Set a timeout for the entire color query operation.
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	fg, bg, err := queryTerminalColors(ctx)
	if err != nil {
		// If there's any error (including timeout), log it and return safe defaults.
		// log.Printf("Could not query terminal colors, using defaults: %v", err)
		return tcell.ColorWhite, tcell.ColorBlack, nil
	}
	return fg, bg, nil
}

// Run starts the desktop event loop. This goroutine is the sole accessor of
// tree state (Root, ActiveLeaf, SplitRatios, pane dimensions). Must be called
// after NewDesktopEngine and before events are injected.
func (d *DesktopEngine) Run() {
	for {
		// Block until at least one event arrives.
		select {
		case ev := <-d.eventCh:
			d.processDesktopEvent(ev)
		case frame := <-d.animCh:
			d.applyAnimationFrame(frame)
		case <-d.refreshCh:
			// App signalled dirty — just break to drain+publish below.
		case <-d.quit:
			return
		}

		// Drain: process all already-queued events before publishing.
		// Input events have priority over refresh signals so keystrokes
		// are never delayed by a flood of app refreshes.
		d.drainPending()

		// Single publish for the entire batch.
		d.publishIfDirty()
	}
}

// drainPending processes all events already sitting in channels without blocking.
// Events are drained in priority order: input first, then animations, then refreshes.
func (d *DesktopEngine) drainPending() {
	for {
		// Priority 1: user input — always process before refreshes.
		select {
		case ev := <-d.eventCh:
			d.processDesktopEvent(ev)
			continue
		default:
		}
		// Priority 2: animation frames.
		select {
		case frame := <-d.animCh:
			d.applyAnimationFrame(frame)
			continue
		default:
		}
		// Priority 3: coalesce refresh signals (just drain, don't publish each).
		select {
		case <-d.refreshCh:
			continue
		default:
		}
		// All channels empty — done draining.
		return
	}
}

// processDesktopEvent dispatches a desktopEvent to the appropriate handler.
// Returns true if the caller should publish immediately (e.g., barrier sync).
func (d *DesktopEngine) processDesktopEvent(ev desktopEvent) bool {
	switch ev.kind {
	case keyEventKind:
		tcellEvent := tcell.NewEventKey(ev.key, ev.ch, ev.mod)
		d.handleEvent(tcellEvent)
	case mouseEventKind:
		d.processMouseEvent(ev.mx, ev.my, ev.buttons, ev.mod)
	case pasteEventKind:
		d.handlePasteInternal(ev.paste)
	case resizeEventKind:
		d.handleResizeInternal()
	case syncEventKind:
		// Barrier: publish everything accumulated so far, then unblock caller.
		d.publishIfDirty()
		close(ev.done)
		return true
	}
	return false
}

// applyAnimationFrame applies interpolated ratios from the animation ticker.
func (d *DesktopEngine) applyAnimationFrame(frame animationFrame) {
	if frame.node != nil {
		frame.node.SplitRatios = frame.ratios
	}
	d.recalculateLayout()
	d.broadcastTreeChanged()
	if frame.done && frame.onComplete != nil {
		frame.onComplete()
	}
}

// publishIfDirty calls the refresh handler if one is set.
func (d *DesktopEngine) publishIfDirty() {
	if handler := d.refreshHandlerFunc(); handler != nil {
		handler()
	}
}

// SendEvent enqueues an event for the event loop to process.
// Non-blocking: drops if the event channel is full (unlikely at capacity 64).
func (d *DesktopEngine) SendEvent(ev desktopEvent) {
	select {
	case d.eventCh <- ev:
	default:
		log.Printf("[DESKTOP] Event channel full, dropping event kind=%d", ev.kind)
	}
}

// SendRefresh signals the event loop that a pane has dirty content.
func (d *DesktopEngine) SendRefresh() {
	select {
	case d.refreshCh <- struct{}{}:
	default:
	}
}

// makeRefreshNotifier creates a channel suitable for App.SetRefreshNotifier
// that forwards to the desktop event loop. Use this for status panes and
// floating panels that don't have a per-pane refresh forwarder.
func (d *DesktopEngine) makeRefreshNotifier() chan<- bool {
	ch := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-d.quit:
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				d.SendRefresh()
			}
		}
	}()
	return ch
}

// Barrier blocks until the event loop has processed all previously-queued events.
// Useful in tests to ensure event-loop state is settled before assertions.
func (d *DesktopEngine) Barrier() {
	done := make(chan struct{})
	d.eventCh <- desktopEvent{kind: syncEventKind, done: done}
	<-done
}

// SendAnimationFrame sends an animation frame to the event loop for tree-safe application.
func (d *DesktopEngine) SendAnimationFrame(frame animationFrame) {
	select {
	case d.animCh <- frame:
	default:
		log.Printf("[DESKTOP] Animation channel full, dropping frame")
	}
}

// handleResizeInternal processes resize events (called from event loop).
func (d *DesktopEngine) handleResizeInternal() {
	d.recalculateLayout()
}
