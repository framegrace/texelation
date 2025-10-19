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
}

func (s *uiState) setRenderChannel(ch chan<- struct{}) {
	s.renderCh = ch
	if s.effects != nil {
		s.effects.attachRenderChannel(ch)
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
		cache:        client.NewBufferCache(),
		themeValues:  make(map[string]map[string]interface{}),
		defaultStyle: tcell.StyleDefault,
		defaultFg:    tcell.ColorDefault,
		defaultBg:    tcell.ColorDefault,
		desktopBg:    tcell.ColorDefault,
	}

	cfg := theme.Get()
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
	screen.SetStyle(state.defaultStyle)
	screen.Clear()

	if state.effects != nil {
		state.effects.Update(time.Now())
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
	for _, pane := range panes {
		if pane == nil || pane.Rect.Width <= 0 || pane.Rect.Height <= 0 {
			continue
		}

		paneBuffer := make([][]client.Cell, pane.Rect.Height)
		for rowIdx := 0; rowIdx < pane.Rect.Height; rowIdx++ {
			row := make([]client.Cell, pane.Rect.Width)
			source := pane.RowCells(rowIdx)
			for col := 0; col < pane.Rect.Width; col++ {
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

		if state.effects != nil {
			state.effects.ApplyPaneEffects(pane, paneBuffer)
		}

		resizingOverlay := pane.Resizing
		zoomOverlay := state.zoomed && pane.ID == state.zoomedPane
		for rowIdx := 0; rowIdx < pane.Rect.Height; rowIdx++ {
			targetY := pane.Rect.Y + rowIdx
			if targetY < 0 || targetY >= height {
				continue
			}
			row := paneBuffer[rowIdx]
			for col := 0; col < pane.Rect.Width; col++ {
				targetX := pane.Rect.X + col
				if targetX < 0 || targetX >= width {
					continue
				}
				cell := row[col]
				style := cell.Style
				if resizingOverlay {
					style = applyResizingOverlay(style, 0.2, state)
				}
				if zoomOverlay {
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
