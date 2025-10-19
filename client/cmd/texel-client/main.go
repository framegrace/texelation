package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
	"texelation/texel/theme"
)

type uiState struct {
	cache          *client.BufferCache
	clipboard      protocol.ClipboardData
	hasClipboard   bool
	theme          protocol.ThemeAck
	hasTheme       bool
	focus          protocol.PaneFocus
	hasFocus       bool
	themeValues    map[string]map[string]interface{}
	defaultStyle   tcell.Style
	defaultFg      tcell.Color
	defaultBg      tcell.Color
	workspaces     []int
	workspaceID    int
	activeTitle    string
	controlMode    bool
	subMode        rune
	desktopBg      tcell.Color
	zoomed         bool
	zoomedPane     [16]byte
	pasting        bool
	pasteBuf       []byte
	effectRegistry *effectRegistry
	renderCh       chan<- struct{}
	effects        *effectManager
	geometry       *geometryManager
	lastPaneRects  map[[16]byte]PaneRect
	lastPaneBuffer map[[16]byte][][]client.Cell
	workspaceCols  int
	workspaceRows  int
	geometryCfg    geometryConfig
}

func (s *uiState) setRenderChannel(ch chan<- struct{}) {
	s.renderCh = ch
	if s.effects != nil {
		s.effects.attachRenderChannel(ch)
	}
	if s.geometry != nil {
		s.geometry.attachRenderChannel(ch)
	}
}

func (s *uiState) setThemeValue(section, key string, value interface{}) {
	if s.themeValues == nil {
		s.themeValues = make(map[string]map[string]interface{})
	}
	sec := s.themeValues[section]
	if sec == nil {
		sec = make(map[string]interface{})
		s.themeValues[section] = sec
	}
	sec[key] = value
}

func (s *uiState) applyEffectConfig(reg *effectRegistry) {
	if reg == nil {
		if s.effectRegistry != nil {
			reg = s.effectRegistry
		} else {
			reg = newEffectRegistry()
		}
	}
	s.effectRegistry = reg

	paneSpecs := []paneEffectSpec{{ID: "inactive-overlay"}}
	if section, ok := s.themeValues["pane"]; ok {
		if raw, ok := section["effects"]; ok && raw != "" {
			if specs, err := parsePaneEffectSpecs(raw); err == nil && len(specs) > 0 {
				paneSpecs = specs
			}
		}
	}

	workspaceSpecs := []workspaceEffectSpec{{ID: "rainbow"}, {ID: "flash"}}
	if section, ok := s.themeValues["workspace"]; ok {
		if raw, ok := section["effects"]; ok && raw != "" {
			if specs, err := parseWorkspaceEffectSpecs(raw); err == nil && len(specs) > 0 {
				workspaceSpecs = specs
			}
		}
	}

	manager := newEffectManager()
	for _, spec := range paneSpecs {
		if eff := reg.createPaneEffect(spec); eff != nil {
			manager.registerPaneEffect(eff)
		}
	}
	for _, spec := range workspaceSpecs {
		if eff := reg.createWorkspaceEffect(spec); eff != nil {
			manager.registerWorkspaceEffect(eff)
		}
	}
	if s.renderCh != nil {
		manager.attachRenderChannel(s.renderCh)
	}
	s.effects = manager
	if s.cache != nil {
		s.effects.ResetPaneStates(s.cache.SortedPanes())
	}
	s.effects.HandleTrigger(EffectTrigger{
		Type:      TriggerWorkspaceControl,
		Active:    s.controlMode,
		Timestamp: time.Now(),
	})

	if sec, ok := s.themeValues["geometry"]; ok {
		s.geometryCfg = parseGeometryConfig(sec)
	} else {
		s.geometryCfg = parseGeometryConfig(nil)
	}

	geom := newGeometryManager()
	if s.renderCh != nil {
		geom.attachRenderChannel(s.renderCh)
	}
	geom.registerEffect(newGeometryTransitionEffect(s.geometryCfg))
	s.geometry = geom
	if s.lastPaneRects == nil {
		s.lastPaneRects = make(map[[16]byte]PaneRect)
	}
	s.refreshPaneGeometry(false)
}

type panicLogger struct {
	path string
	mu   sync.Mutex
}

func newPanicLogger(path string) *panicLogger {
	return &panicLogger{path: path}
}

func (p *panicLogger) Recover(context string) {
	if r := recover(); r != nil {
		p.logPanic(context, r)
		os.Exit(2)
	}
}

func (p *panicLogger) Go(context string, fn func()) {
	go func() {
		defer p.Recover(context)
		fn()
	}()
}

func (p *panicLogger) logPanic(context string, r interface{}) {
	buf := make([]byte, 1<<16)
	n := runtime.Stack(buf, true)
	stack := buf[:n]
	log.Printf("panic in %s: %v\n%s", context, r, stack)
	if p.path == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("panic: unable to write panic log: %v", err)
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC3339Nano)
	fmt.Fprintf(f, "[%s] panic in %s: %v\n%s\n", ts, context, r, stack)
}

func main() {
	socket := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := flag.Bool("reconnect", false, "Attempt to resume previous session")
	panicLogPath := flag.String("panic-log", "", "File to append panic stack traces")
	flag.Parse()

	panicLogger := newPanicLogger(*panicLogPath)
	defer panicLogger.Recover("main")

	logFile, err := setupLogging()
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging disabled: %v\n", err)
	} else {
		defer logFile.Close()
	}

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
	var writeMu sync.Mutex

	log.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))

	state := &uiState{
		cache:          client.NewBufferCache(),
		themeValues:    make(map[string]map[string]interface{}),
		defaultStyle:   tcell.StyleDefault,
		defaultFg:      tcell.ColorDefault,
		defaultBg:      tcell.ColorDefault,
		desktopBg:      tcell.ColorDefault,
		lastPaneRects:  make(map[[16]byte]PaneRect),
		lastPaneBuffer: make(map[[16]byte][][]client.Cell),
	}

	cfg := theme.Get()
	if err := theme.Err(); err != nil {
		log.Fatalf("failed to load theme: %v", err)
	}
	theme.ApplyDefaults(cfg)
	for sectionName, section := range cfg {
		for key, value := range section {
			state.setThemeValue(sectionName, key, value)
		}
	}

	registry := newEffectRegistry()
	state.effectRegistry = registry
	state.applyEffectConfig(nil)
	lastSequence := uint64(0)

	var pendingAck atomic.Uint64
	var lastAck atomic.Uint64
	ackSignal := make(chan struct{}, 1)

	if *reconnect {
		if hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence); err != nil {
			log.Fatalf("resume request failed: %v", err)
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
		log.Fatalf("create screen failed: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("init screen failed: %v", err)
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
				return
			}
			if !handleScreenEvent(ev, state, screen, conn, sessionID, &writeMu) {
				return
			}
		case <-doneCh:
			fmt.Println("Connection closed")
			return
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
		cache.ApplySnapshot(snap)
		if state.effects != nil {
			state.effects.ResetPaneStates(cache.SortedPanes())
		}
		emitGeometry := state.geometry != nil && len(state.lastPaneRects) > 0
		state.refreshPaneGeometry(emitGeometry)
		return true
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			log.Printf("decode delta failed: %v", err)
			return false
		}
		cache.ApplyDelta(delta)
		state.refreshPaneGeometry(true)
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
		state.applyEffectConfig(nil)
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
		state.applyEffectConfig(nil)
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
			state.effects.HandleTrigger(EffectTrigger{Type: TriggerPaneActive, PaneID: paneFlags.PaneID, Active: active, Timestamp: ts})
			state.effects.HandleTrigger(EffectTrigger{Type: TriggerPaneResizing, PaneID: paneFlags.PaneID, Resizing: resizing, Timestamp: ts})
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

func render(state *uiState, screen tcell.Screen) {
	width, height := screen.Size()
	state.workspaceCols = width
	state.workspaceRows = height
	screen.SetStyle(state.defaultStyle)
	screen.Clear()

	now := time.Now()
	if state.effects != nil {
		state.effects.Update(now)
	}
	if state.geometry != nil {
		state.geometry.Update(now)
	}

	workspaceBuffer := make([][]client.Cell, height)
	for y := 0; y < height; y++ {
		row := make([]client.Cell, width)
		for x := range row {
			row[x] = client.Cell{Ch: ' ', Style: state.defaultStyle}
		}
		workspaceBuffer[y] = row
	}

	panes := state.cache.SortedPanes()
	geomStates := make(map[[16]byte]*geometryPaneState, len(panes))
	orderIndex := make(map[*geometryPaneState]int, len(panes))
	for idx, pane := range panes {
		if pane == nil {
			continue
		}
		rect := PaneRect{X: pane.Rect.X, Y: pane.Rect.Y, Width: pane.Rect.Width, Height: pane.Rect.Height}
		geom := &geometryPaneState{Pane: pane, Base: rect, Rect: rect}
		geomStates[pane.ID] = geom
		orderIndex[geom] = idx
	}

	workspaceGeom := geometryWorkspaceState{
		Width:      width,
		Height:     height,
		Zoomed:     state.zoomed,
		ZoomedPane: state.zoomedPane,
	}
	if state.geometry != nil {
		state.geometry.Apply(geomStates, &workspaceGeom)
	}

	drawStates := make([]*geometryPaneState, 0, len(geomStates))
	for _, pane := range panes {
		if geom := geomStates[pane.ID]; geom != nil {
			drawStates = append(drawStates, geom)
		}
	}
	for _, geom := range geomStates {
		if _, ok := orderIndex[geom]; !ok {
			orderIndex[geom] = len(orderIndex) + 1000
			drawStates = append(drawStates, geom)
		}
	}

	sort.SliceStable(drawStates, func(i, j int) bool {
		if drawStates[i].ZIndex == drawStates[j].ZIndex {
			return orderIndex[drawStates[i]] < orderIndex[drawStates[j]]
		}
		return drawStates[i].ZIndex < drawStates[j].ZIndex
	})

	for _, geom := range drawStates {
		rect := geom.Rect
		if rect.Width <= 0 || rect.Height <= 0 {
			continue
		}
		pane := geom.Pane

		paneBuffer := make([][]client.Cell, rect.Height)
		for rowIdx := 0; rowIdx < rect.Height; rowIdx++ {
			row := make([]client.Cell, rect.Width)
			var source []client.Cell
			if geom.Buffer != nil {
				if rowIdx < len(geom.Buffer) {
					source = geom.Buffer[rowIdx]
				}
			} else if pane != nil {
				source = pane.RowCells(rowIdx)
			}
			for col := 0; col < rect.Width; col++ {
				cell := client.Cell{Ch: ' ', Style: state.defaultStyle}
				if source != nil && col < len(source) {
					cell = source[col]
					if cell.Ch == 0 {
						cell.Ch = ' '
					}
					if cell.Style == (tcell.Style{}) {
						cell.Style = state.defaultStyle
					}
				}
				row[col] = cell
			}
			paneBuffer[rowIdx] = row
		}

		if pane != nil && state.effects != nil {
			state.effects.ApplyPaneEffects(pane, paneBuffer)
		}

		for rowIdx := 0; rowIdx < rect.Height; rowIdx++ {
			targetY := rect.Y + rowIdx
			if targetY < 0 || targetY >= height {
				continue
			}
			row := paneBuffer[rowIdx]
			for col := 0; col < rect.Width; col++ {
				targetX := rect.X + col
				if targetX < 0 || targetX >= width {
					continue
				}
				cell := client.Cell{Ch: ' ', Style: state.defaultStyle}
				if row != nil && col < len(row) {
					cell = row[col]
				}
				style := cell.Style
				if pane != nil && pane.Resizing {
					style = applyResizingOverlay(style, 0.2, state)
				}
				if pane != nil && state.zoomed && pane.ID == state.zoomedPane {
					style = applyZoomOverlay(style, 0.2, state)
				}
				workspaceBuffer[targetY][targetX] = client.Cell{Ch: cell.Ch, Style: style}
			}
		}
	}

	if state.effects != nil {
		state.effects.ApplyWorkspaceEffects(workspaceBuffer)
	}

	for y, row := range workspaceBuffer {
		for x, cell := range row {
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			style := cell.Style
			if style == (tcell.Style{}) {
				style = state.defaultStyle
			}
			screen.SetContent(x, y, ch, nil, style)
		}
	}

	if state.controlMode {
		applyControlOverlay(state, screen)
	}
	screen.Show()
}

func handleScreenEvent(ev tcell.Event, state *uiState, screen tcell.Screen, conn net.Conn, sessionID [16]byte, writeMu *sync.Mutex) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if state.pasting {
			consumePasteKey(state, ev)
			return true
		}
		if state.controlMode && ev.Modifiers() == 0 {
			r := ev.Rune()
			if r == 'q' || r == 'Q' {
				if err := sendKeyEvent(writeMu, conn, sessionID, tcell.KeyEsc, 0, 0); err != nil {
					log.Printf("control reset failed: %v", err)
				}
				state.controlMode = false
				state.subMode = 0
				if state.effects != nil {
					state.effects.HandleTrigger(EffectTrigger{
						Type:      TriggerWorkspaceControl,
						Active:    state.controlMode,
						Timestamp: time.Now(),
					})
				}
				log.Printf("control quit requested; closing client")
				return false
			}
		}
		if ev.Key() == tcell.KeyCtrlA {
			state.controlMode = !state.controlMode
			state.subMode = 0
			if state.effects != nil {
				state.effects.HandleTrigger(EffectTrigger{
					Type:      TriggerWorkspaceControl,
					Active:    state.controlMode,
					Timestamp: time.Now(),
				})
			}
			render(state, screen)
		}
		if ev.Key() == tcell.KeyEsc && ev.Modifiers() == 0 && state.controlMode {
			state.controlMode = false
			state.subMode = 0
			if state.effects != nil {
				state.effects.HandleTrigger(EffectTrigger{
					Type:      TriggerWorkspaceControl,
					Active:    state.controlMode,
					Timestamp: time.Now(),
				})
			}
			render(state, screen)
		}
		if err := sendKeyEvent(writeMu, conn, sessionID, ev.Key(), ev.Rune(), ev.Modifiers()); err != nil {
			log.Printf("send key failed: %v", err)
		} else if state.effects != nil {
			now := time.Now()
			r := ev.Rune()
			mod := uint16(ev.Modifiers())
			if state.hasFocus {
				state.effects.HandleTrigger(EffectTrigger{
					Type:      TriggerPaneKey,
					PaneID:    state.focus.PaneID,
					Key:       r,
					Modifiers: mod,
					Timestamp: now,
				})
			}
			state.effects.HandleTrigger(EffectTrigger{
				Type:        TriggerWorkspaceKey,
				WorkspaceID: state.workspaceID,
				Key:         r,
				Modifiers:   mod,
				Timestamp:   now,
			})
		}
	case *tcell.EventMouse:
		x, y := ev.Position()
		mouse := protocol.MouseEvent{X: int16(x), Y: int16(y), ButtonMask: uint32(ev.Buttons()), Modifiers: uint16(ev.Modifiers())}
		payload, _ := protocol.EncodeMouseEvent(mouse)
		if err := writeMessage(writeMu, conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgMouseEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
			log.Printf("send mouse failed: %v", err)
		}
	case *tcell.EventResize:
		sendResize(writeMu, conn, sessionID, screen)
		render(state, screen)
	case *tcell.EventInterrupt:
		// Ignore; used to wake PollEvent for shutdown.
	case *tcell.EventPaste:
		if ev.Start() {
			state.pasting = true
			state.pasteBuf = state.pasteBuf[:0]
		} else {
			state.pasting = false
			if len(state.pasteBuf) > 0 {
				data := append([]byte(nil), state.pasteBuf...)
				if err := sendPaste(writeMu, conn, sessionID, data); err != nil {
					log.Printf("send paste failed: %v", err)
				}
				state.pasteBuf = state.pasteBuf[:0]
			}
		}
	}
	return true
}

func isNetworkClosed(err error) bool {
	if err == os.ErrClosed {
		return true
	}
	ne, ok := err.(net.Error)
	return ok && !ne.Timeout()
}

func (s *uiState) refreshPaneGeometry(emit bool) {
	if s == nil || s.cache == nil {
		return
	}
	panes := s.cache.SortedPanes()
	current := make(map[[16]byte]PaneRect, len(panes))
	now := time.Now()
	previous := s.lastPaneRects
	for _, pane := range panes {
		if pane == nil {
			continue
		}
		rect := PaneRect{X: pane.Rect.X, Y: pane.Rect.Y, Width: pane.Rect.Width, Height: pane.Rect.Height}
		current[pane.ID] = rect
		if emit && s.geometry != nil {
			if old, ok := previous[pane.ID]; ok {
				if old != rect {
					s.geometry.HandleTrigger(EffectTrigger{Type: TriggerPaneGeometry, PaneID: pane.ID, OldRect: old, NewRect: rect, Timestamp: now})
				}
			} else {
				relatedID, relatedRect := findRelatedPane(rect, previous)
				startRect := rect
				if s.geometryCfg.SplitMode == splitModeGhost {
					startRect = expandRectFromLine(rect, relatedRect)
				} else if relatedRect != (PaneRect{}) {
					startRect = alignRectToEdge(rect, relatedRect)
				}
				s.geometry.HandleTrigger(EffectTrigger{Type: TriggerPaneCreated, PaneID: pane.ID, RelatedPaneID: relatedID, OldRect: startRect, NewRect: rect, Timestamp: now})
			}
		}
		if s.lastPaneBuffer != nil {
			s.lastPaneBuffer[pane.ID] = clonePaneBuffer(pane)
		}
	}
	if emit && s.geometry != nil {
		for id, oldRect := range previous {
			if _, ok := current[id]; !ok {
				relatedID, relatedRect := findRelatedPane(oldRect, current)
				targetRect := collapseRectTowards(oldRect, relatedRect)
				ghost := s.geometryCfg.RemoveMode == removeModeGhost
				var buffer [][]client.Cell
				if s.lastPaneBuffer != nil {
					buffer = s.lastPaneBuffer[id]
					delete(s.lastPaneBuffer, id)
				}
				s.geometry.HandleTrigger(EffectTrigger{Type: TriggerPaneRemoved, PaneID: id, RelatedPaneID: relatedID, OldRect: oldRect, NewRect: targetRect, PaneBuffer: buffer, Ghost: ghost, Timestamp: now})
			}
		}
	}
	s.lastPaneRects = current
}

func clonePaneBuffer(pane *client.PaneState) [][]client.Cell {
	if pane == nil {
		return nil
	}
	height := pane.Rect.Height
	buffer := make([][]client.Cell, height)
	for rowIdx := 0; rowIdx < height; rowIdx++ {
		src := pane.RowCells(rowIdx)
		if len(src) == 0 {
			buffer[rowIdx] = nil
			continue
		}
		row := make([]client.Cell, len(src))
		copy(row, src)
		buffer[rowIdx] = row
	}
	return buffer
}

func findRelatedPane(rect PaneRect, candidates map[[16]byte]PaneRect) (PaneID, PaneRect) {
	var bestID PaneID
	bestRect := PaneRect{}
	bestArea := -1
	for id, candidate := range candidates {
		area := rectOverlapArea(rect, candidate)
		if area > bestArea {
			bestArea = area
			bestID = id
			bestRect = candidate
		}
	}
	return bestID, bestRect
}

func rectOverlapArea(a, b PaneRect) int {
	x0 := maxInt(a.X, b.X)
	y0 := maxInt(a.Y, b.Y)
	x1 := minInt(a.X+a.Width, b.X+b.Width)
	y1 := minInt(a.Y+a.Height, b.Y+b.Height)
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	return (x1 - x0) * (y1 - y0)
}

func expandRectFromLine(target, reference PaneRect) PaneRect {
	start := target
	if reference.Width == 0 && reference.Height == 0 {
		start.Width = 0
		start.Height = 0
		return start
	}
	if reference.Height == target.Height && reference.Y == target.Y {
		start.Width = 0
		if target.X >= reference.X+reference.Width {
			start.X = target.X
		} else if target.X+target.Width <= reference.X {
			start.X = target.X + target.Width
		}
		return start
	}
	if reference.Width == target.Width && reference.X == target.X {
		start.Height = 0
		if target.Y >= reference.Y+reference.Height {
			start.Y = target.Y
		} else if target.Y+target.Height <= reference.Y {
			start.Y = target.Y + target.Height
		}
		return start
	}
	start.Width = 0
	start.Height = 0
	return start
}

func collapseRectTowards(source, reference PaneRect) PaneRect {
	target := source
	if reference == (PaneRect{}) {
		target.Width = 0
		target.Height = 0
		return target
	}
	if source.Y == reference.Y && source.Height == reference.Height {
		target.Y = source.Y
		target.Height = source.Height
		if reference.X >= source.X+source.Width {
			target.X = source.X + source.Width
		} else if reference.X+reference.Width <= source.X {
			target.X = source.X
		} else {
			target.X = source.X
		}
		target.Width = 0
		return target
	}
	if source.X == reference.X && source.Width == reference.Width {
		target.X = source.X
		target.Width = source.Width
		if reference.Y >= source.Y+source.Height {
			target.Y = source.Y + source.Height
		} else if reference.Y+reference.Height <= source.Y {
			target.Y = source.Y
		} else {
			target.Y = source.Y
		}
		target.Height = 0
		return target
	}
	target.Width = 0
	target.Height = 0
	return target
}

func alignRectToEdge(target, reference PaneRect) PaneRect {
	start := target
	if reference == (PaneRect{}) {
		start.Y = target.Y
		start.Height = target.Height
		start.X = target.X + target.Width
		start.Width = 0
		return start
	}
	if target.Y == reference.Y && target.Height == reference.Height {
		start.Y = target.Y
		start.Height = target.Height
		if target.X >= reference.X+reference.Width {
			start.X = target.X + target.Width
		} else if target.X+target.Width <= reference.X {
			start.X = target.X
		} else {
			start.X = target.X + target.Width
		}
		start.Width = 0
		return start
	}
	if target.X == reference.X && target.Width == reference.Width {
		start.X = target.X
		start.Width = target.Width
		if target.Y >= reference.Y+reference.Height {
			start.Y = target.Y + target.Height
		} else if target.Y+target.Height <= reference.Y {
			start.Y = target.Y
		} else {
			start.Y = target.Y + target.Height
		}
		start.Height = 0
		return start
	}
	start.X = target.X + target.Width
	start.Width = 0
	start.Y = target.Y
	start.Height = target.Height
	return start
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *uiState) updateTheme(section, key, value string) {
	if section == "" || key == "" {
		return
	}
	var stored interface{} = value
	if key == "effects" {
		var decoded interface{}
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			stored = decoded
		}
	}
	s.setThemeValue(section, key, stored)
	if section == "desktop" {
		switch key {
		case "default_fg":
			if fg, ok := parseHexColor(value); ok {
				s.defaultFg = fg
			}
		case "default_bg":
			if bg, ok := parseHexColor(value); ok {
				s.defaultBg = bg
				s.desktopBg = bg
			}
		}
	}
	s.recomputeDefaultStyle()
	s.applyEffectConfig(nil)
}

func (s *uiState) recomputeDefaultStyle() {
	style := tcell.StyleDefault
	if s.defaultFg != tcell.ColorDefault {
		style = style.Foreground(s.defaultFg)
	}
	if s.defaultBg != tcell.ColorDefault {
		style = style.Background(s.defaultBg)
	}
	s.defaultStyle = style
}

func (s *uiState) applyStateUpdate(update protocol.StateUpdate) {
	s.workspaceID = int(update.WorkspaceID)
	if cap(s.workspaces) < len(update.AllWorkspaces) {
		s.workspaces = make([]int, 0, len(update.AllWorkspaces))
	} else {
		s.workspaces = s.workspaces[:0]
	}
	for _, id := range update.AllWorkspaces {
		s.workspaces = append(s.workspaces, int(id))
	}
	prevControl := s.controlMode
	prevZoomed := s.zoomed
	prevZoomPane := s.zoomedPane
	s.controlMode = update.InControlMode
	s.subMode = update.SubMode
	s.activeTitle = update.ActiveTitle
	bg := colorFromRGB(update.DesktopBgRGB)
	if bg != tcell.ColorDefault {
		s.desktopBg = bg
		s.defaultBg = bg
	}
	s.zoomed = update.Zoomed
	if update.Zoomed {
		s.zoomedPane = update.ZoomedPaneID
	} else {
		s.zoomedPane = [16]byte{}
	}
	s.recomputeDefaultStyle()
	if s.geometry != nil {
		paneID := s.zoomedPane
		if !s.zoomed {
			paneID = prevZoomPane
		}
		if paneID != ([16]byte{}) {
			now := time.Now()
			startRect := s.lastPaneRects[paneID]
			workspaceTarget := PaneRect{X: 0, Y: 0, Width: s.workspaceCols, Height: s.workspaceRows}
			if s.zoomed && (!prevZoomed || paneID != prevZoomPane) {
				if startRect == (PaneRect{}) {
					startRect = workspaceTarget
				}
				s.geometry.HandleTrigger(EffectTrigger{Type: TriggerWorkspaceZoom, PaneID: paneID, OldRect: startRect, NewRect: workspaceTarget, Active: true, Timestamp: now})
			} else if !s.zoomed && prevZoomed {
				targetRect := s.lastPaneRects[paneID]
				if targetRect == (PaneRect{}) {
					targetRect = startRect
				}
				s.geometry.HandleTrigger(EffectTrigger{Type: TriggerWorkspaceZoom, PaneID: paneID, OldRect: workspaceTarget, NewRect: targetRect, Active: false, Timestamp: now})
			}
		}
	}
	if s.effects != nil && prevControl != s.controlMode {
		s.effects.HandleTrigger(EffectTrigger{
			Type:      TriggerWorkspaceControl,
			Active:    s.controlMode,
			Timestamp: time.Now(),
		})
	}
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

func colorFromRGB(rgb uint32) tcell.Color {
	r := int32((rgb >> 16) & 0xFF)
	g := int32((rgb >> 8) & 0xFF)
	b := int32(rgb & 0xFF)
	return tcell.NewRGBColor(r, g, b)
}

func applyControlOverlay(state *uiState, screen tcell.Screen) {
	width, height := screen.Size()
	accent := tcell.NewRGBColor(90, 200, 255)
	intensity := float32(0.35)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ch, comb, style, cellW := screen.GetContent(x, y)
			if cellW <= 0 {
				cellW = 1
			}
			fg, bg, attrs := style.Decompose()
			if !fg.Valid() {
				fg = state.defaultFg
				if !fg.Valid() {
					fg = tcell.ColorWhite
				}
			}
			if !bg.Valid() {
				bg = state.defaultBg
				if !bg.Valid() {
					bg = state.desktopBg
					if !bg.Valid() {
						bg = tcell.ColorBlack
					}
				}
			}
			blendedFg := blendColor(fg, accent, intensity)
			styled := tcell.StyleDefault.Foreground(blendedFg).Background(bg)
			styled = styled.Bold(attrs&tcell.AttrBold != 0).
				Underline(attrs&tcell.AttrUnderline != 0).
				Reverse(attrs&tcell.AttrReverse != 0).
				Blink(attrs&tcell.AttrBlink != 0).
				Dim(attrs&tcell.AttrDim != 0).
				Italic(attrs&tcell.AttrItalic != 0)
			screen.SetContent(x, y, ch, comb, styled)
			x += cellW - 1
		}
	}
}

func formatPaneID(id [16]byte) string {
	return fmt.Sprintf("%x", id[:4])
}

func sendResize(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, screen tcell.Screen) {
	cols, rows := screen.Size()
	payload, err := protocol.EncodeResize(protocol.Resize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		log.Printf("encode resize failed: %v", err)
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgResize, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := writeMessage(writeMu, conn, header, payload); err != nil {
		log.Printf("send resize failed: %v", err)
	}
}

func sendKeyEvent(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, key tcell.Key, r rune, mods tcell.ModMask) error {
	event := protocol.KeyEvent{KeyCode: uint32(key), RuneValue: r, Modifiers: uint16(mods)}
	// log.Printf("send key: key=%v rune=%q mods=%v", key, r, mods)
	payload, err := protocol.EncodeKeyEvent(event)
	if err != nil {
		return err
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgKeyEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}
	return writeMessage(writeMu, conn, header, payload)
}

func sendPaste(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, data []byte) error {
	payload, err := protocol.EncodePaste(protocol.Paste{Data: data})
	if err != nil {
		return err
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgPaste, Flags: protocol.FlagChecksum, SessionID: sessionID}
	return writeMessage(writeMu, conn, header, payload)
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

func writeMessage(mu *sync.Mutex, conn net.Conn, header protocol.Header, payload []byte) error {
	mu.Lock()
	defer mu.Unlock()
	log.Printf("client tx type=%d seq=%d len=%d", header.Type, header.Sequence, len(payload))
	return protocol.WriteMessage(conn, header, payload)
}

func consumePasteKey(state *uiState, ev *tcell.EventKey) {
	var b byte
	switch ev.Key() {
	case tcell.KeyRune:
		r := ev.Rune()
		if r == '\n' {
			state.pasteBuf = append(state.pasteBuf, '\r')
		} else {
			state.pasteBuf = utf8.AppendRune(state.pasteBuf, r)
		}
		return
	case tcell.KeyEnter:
		state.pasteBuf = append(state.pasteBuf, '\r')
		return
	case tcell.KeyTab:
		state.pasteBuf = append(state.pasteBuf, '\t')
		return
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		state.pasteBuf = append(state.pasteBuf, '')
		return
	case tcell.KeyEsc:
		state.pasteBuf = append(state.pasteBuf, 0x1b)
		return
	default:
		if ev.Rune() != 0 {
			state.pasteBuf = utf8.AppendRune(state.pasteBuf, ev.Rune())
			return
		}
	}
	b = byte(ev.Rune())
	if b != 0 {
		state.pasteBuf = append(state.pasteBuf, b)
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

func applyResizingOverlay(style tcell.Style, intensity float32, state *uiState) tcell.Style {
	if intensity <= 0 {
		return style
	}
	fg, bg, attrs := style.Decompose()
	if !fg.Valid() {
		fg = state.defaultFg
		if !fg.Valid() {
			fg = tcell.ColorWhite
		}
	}
	if !bg.Valid() {
		bg = state.defaultBg
		if !bg.Valid() {
			bg = state.desktopBg
			if !bg.Valid() {
				bg = tcell.ColorBlack
			}
		}
	}
	resizingTint := tcell.NewRGBColor(255, 184, 108)
	blendedFg := blendColor(fg, resizingTint, intensity/1.5)
	blendedBg := blendColor(bg, resizingTint, intensity)
	return tcell.StyleDefault.Foreground(blendedFg).
		Background(blendedBg).
		Bold(attrs&tcell.AttrBold != 0).
		Underline(attrs&tcell.AttrUnderline != 0).
		Reverse(attrs&tcell.AttrReverse != 0).
		Blink(attrs&tcell.AttrBlink != 0).
		Dim(attrs&tcell.AttrDim != 0).
		Italic(attrs&tcell.AttrItalic != 0)
}

func applyZoomOverlay(style tcell.Style, intensity float32, state *uiState) tcell.Style {
	if intensity <= 0 {
		return style
	}
	fg, bg, attrs := style.Decompose()
	if !fg.Valid() {
		fg = state.defaultFg
		if !fg.Valid() {
			fg = tcell.ColorWhite
		}
	}
	if !bg.Valid() {
		bg = state.defaultBg
		if !bg.Valid() {
			bg = state.desktopBg
			if !bg.Valid() {
				bg = tcell.ColorBlack
			}
		}
	}
	outline := tcell.NewRGBColor(120, 200, 255)
	blendedFg := blendColor(fg, outline, intensity/2)
	blendedBg := blendColor(bg, outline, intensity/1.5)
	return tcell.StyleDefault.Foreground(blendedFg).
		Background(blendedBg).
		Bold(true).
		Underline(attrs&tcell.AttrUnderline != 0).
		Reverse(attrs&tcell.AttrReverse != 0).
		Blink(attrs&tcell.AttrBlink != 0).
		Dim(attrs&tcell.AttrDim != 0).
		Italic(attrs&tcell.AttrItalic != 0)
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
