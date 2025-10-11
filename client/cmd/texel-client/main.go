package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	themeValues  map[string]map[string]string
	defaultStyle tcell.Style
	defaultFg    tcell.Color
	defaultBg    tcell.Color
	workspaces   []int
	workspaceID  int
	activeTitle  string
	controlMode  bool
	subMode      rune
	desktopBg    tcell.Color
	zoomed       bool
	zoomedPane   [16]byte
}

func main() {
	socket := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	reconnect := flag.Bool("reconnect", false, "Attempt to resume previous session")
	flag.Parse()

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

	log.Printf("Connected to session %s", client.FormatUUID(accept.SessionID))

	state := &uiState{
		cache:        client.NewBufferCache(),
		themeValues:  make(map[string]map[string]string),
		defaultStyle: tcell.StyleDefault,
		defaultFg:    tcell.ColorDefault,
		defaultBg:    tcell.ColorDefault,
		desktopBg:    tcell.ColorDefault,
	}
	lastSequence := uint64(0)

	if *reconnect {
		if hdr, payload, err := simple.RequestResume(conn, sessionID, lastSequence); err != nil {
			log.Fatalf("resume request failed: %v", err)
		} else {
			handleControlMessage(state, conn, hdr, payload, sessionID, &lastSequence)
		}
	}

	renderCh := make(chan struct{}, 1)
	doneCh := make(chan struct{})
	go readLoop(conn, state, sessionID, &lastSequence, renderCh, doneCh)

	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatalf("create screen failed: %v", err)
	}
	if err := screen.Init(); err != nil {
		log.Fatalf("init screen failed: %v", err)
	}
	screen.HideCursor()
	defer screen.Fini()
	sendResize(conn, sessionID, screen)

	render(state, screen)

	events := make(chan tcell.Event, 32)
	stopEvents := make(chan struct{})
	go func() {
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
	}()
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
			if !handleScreenEvent(ev, state, screen, conn, sessionID) {
				return
			}
		case <-doneCh:
			fmt.Println("Connection closed")
			return
		}
	}
}

func readLoop(conn net.Conn, state *uiState, sessionID [16]byte, lastSequence *uint64, renderCh chan<- struct{}, doneCh chan<- struct{}) {
	for {
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if !isNetworkClosed(err) {
				log.Printf("read failed: %v", err)
			}
			close(doneCh)
			return
		}
		if handleControlMessage(state, conn, hdr, payload, sessionID, lastSequence) {
			select {
			case renderCh <- struct{}{}:
			default:
			}
		}
	}
}

	func handleControlMessage(state *uiState, conn net.Conn, hdr protocol.Header, payload []byte, sessionID [16]byte, lastSequence *uint64) bool {
	cache := state.cache
	switch hdr.Type {
	case protocol.MsgTreeSnapshot:
		snap, err := protocol.DecodeTreeSnapshot(payload)
		if err != nil {
			log.Printf("decode snapshot failed: %v", err)
			return false
		}
		cache.ApplySnapshot(snap)
		return true
	case protocol.MsgBufferDelta:
		delta, err := protocol.DecodeBufferDelta(payload)
		if err != nil {
			log.Printf("decode delta failed: %v", err)
			return false
		}
		cache.ApplyDelta(delta)
		ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
		if err := protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: sessionID}, ackPayload); err != nil {
			log.Printf("ack failed: %v", err)
		}
		log.Printf("delta applied: pane=%x rev=%d spans=%d", delta.PaneID, delta.Revision, len(delta.Rows))
		if lastSequence != nil && hdr.Sequence > *lastSequence {
			*lastSequence = hdr.Sequence
		}
		return true
	case protocol.MsgPing:
		pong, _ := protocol.EncodePong(protocol.Pong{Timestamp: time.Now().UnixNano()})
		_ = protocol.WriteMessage(conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgPong, Flags: protocol.FlagChecksum, SessionID: sessionID}, pong)
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
		state.cache.SetPaneFlags(paneFlags.PaneID, active, resizing)
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
	state.cache.ForEachPaneSorted(func(pane *client.PaneState) {
		if pane == nil || pane.Rect.Width <= 0 || pane.Rect.Height <= 0 {
			return
		}
		inactiveIntensity := float32(0)
		if !pane.Active {
			inactiveIntensity = 0.3
		} else if state.hasFocus && pane.ID != state.focus.PaneID {
			inactiveIntensity = 0.3
		}
		for rowIdx := 0; rowIdx < pane.Rect.Height; rowIdx++ {
			targetY := pane.Rect.Y + rowIdx
			if targetY < 0 || targetY >= height {
				continue
			}
			row := pane.RowCells(rowIdx)
			for col := 0; col < pane.Rect.Width; col++ {
				targetX := pane.Rect.X + col
				if targetX < 0 || targetX >= width {
					continue
				}
				ch := ' '
				style := state.defaultStyle
				if row != nil && col < len(row) {
					cell := row[col]
					if cell.Ch != 0 {
						ch = cell.Ch
					}
					if cell.Style != (tcell.Style{}) {
						style = cell.Style
					}
				}
				if inactiveIntensity > 0 {
					style = applyInactiveOverlay(style, inactiveIntensity, state)
				}
				if pane.Resizing {
					style = applyResizingOverlay(style, 0.2, state)
				}
				if state.zoomed && pane.ID == state.zoomedPane {
					style = applyZoomOverlay(style, 0.2, state)
				}
				screen.SetContent(targetX, targetY, ch, nil, style)
			}
		}
	})
	if state.controlMode {
		applyControlOverlay(state, screen)
	}
	screen.Show()
}

func handleScreenEvent(ev tcell.Event, state *uiState, screen tcell.Screen, conn net.Conn, sessionID [16]byte) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if state.controlMode && ev.Modifiers() == 0 {
			r := ev.Rune()
			if r == 'q' || r == 'Q' {
				if err := sendKeyEvent(conn, sessionID, tcell.KeyEsc, 0, 0); err != nil {
					log.Printf("control reset failed: %v", err)
				}
				state.controlMode = false
				state.subMode = 0
				log.Printf("control quit requested; closing client")
				return false
			}
		}
		if ev.Key() == tcell.KeyCtrlA {
			state.controlMode = !state.controlMode
			state.subMode = 0
			render(state, screen)
		}
		if ev.Key() == tcell.KeyEsc && ev.Modifiers() == 0 && state.controlMode {
			state.controlMode = false
			state.subMode = 0
			render(state, screen)
		}
		if err := sendKeyEvent(conn, sessionID, ev.Key(), ev.Rune(), ev.Modifiers()); err != nil {
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
		sendResize(conn, sessionID, screen)
		render(state, screen)
	case *tcell.EventInterrupt:
		// Ignore; used to wake PollEvent for shutdown.
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
	if s.themeValues == nil {
		s.themeValues = make(map[string]map[string]string)
	}
	sec := s.themeValues[section]
	if sec == nil {
		sec = make(map[string]string)
		s.themeValues[section] = sec
	}
	sec[key] = value
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
	}
	s.recomputeDefaultStyle()
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

func blendColor(base, overlay tcell.Color, intensity float32) tcell.Color {
	if intensity <= 0 {
		return base
	}
	if intensity > 1 {
		intensity = 1
	}
	r1, g1, b1 := base.RGB()
	r2, g2, b2 := overlay.RGB()
	blend := func(a, b int32) int32 {
		return int32(float32(a)*(1-intensity) + float32(b)*intensity)
	}
	return tcell.NewRGBColor(blend(r1, r2), blend(g1, g2), blend(b1, b2))
}

func hsvToRGB(h, s, v float32) tcell.Color {
	h = float32(math.Mod(float64(h), 2*math.Pi)) / (2 * math.Pi) * 360
	c := v * s
	x := c * (1 - float32(math.Abs(math.Mod(float64(h/60), 2)-1)))
	m := v - c
	var r, g, b float32
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	r, g, b = (r+m)*255, (g+m)*255, (b+m)*255
	return tcell.NewRGBColor(int32(r), int32(g), int32(b))
}

func formatPaneID(id [16]byte) string {
	return fmt.Sprintf("%x", id[:4])
}

func sendResize(conn net.Conn, sessionID [16]byte, screen tcell.Screen) {
	cols, rows := screen.Size()
	payload, err := protocol.EncodeResize(protocol.Resize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		log.Printf("encode resize failed: %v", err)
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgResize, Flags: protocol.FlagChecksum, SessionID: sessionID}
	if err := protocol.WriteMessage(conn, header, payload); err != nil {
		log.Printf("send resize failed: %v", err)
	}
}

func sendKeyEvent(conn net.Conn, sessionID [16]byte, key tcell.Key, r rune, mods tcell.ModMask) error {
	event := protocol.KeyEvent{KeyCode: uint32(key), RuneValue: r, Modifiers: uint16(mods)}
	log.Printf("send key: key=%v rune=%q mods=%v", key, r, mods)
	payload, err := protocol.EncodeKeyEvent(event)
	if err != nil {
		return err
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgKeyEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}
	return protocol.WriteMessage(conn, header, payload)
}

func applyInactiveOverlay(style tcell.Style, intensity float32, state *uiState) tcell.Style {
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
	overlay := blendColor(bg, state.desktopBg, 0.5)
	blendedFg := blendColor(fg, overlay, intensity)
	blendedBg := blendColor(bg, state.desktopBg, intensity)
	return tcell.StyleDefault.Foreground(blendedFg).
		Background(blendedBg).
		Bold(attrs&tcell.AttrBold != 0).
		Underline(attrs&tcell.AttrUnderline != 0).
		Reverse(attrs&tcell.AttrReverse != 0).
		Blink(attrs&tcell.AttrBlink != 0).
		Dim(attrs&tcell.AttrDim != 0).
		Italic(attrs&tcell.AttrItalic != 0)
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
