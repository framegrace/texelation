package texel

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Side defines the placement of a StatusPane.
type Side int

const (
	SideTop Side = iota
	SideBottom
	SideLeft
	SideRight
)

// StatusPane is a special pane with absolute sizing, placed on one side of the screen.
type StatusPane struct {
	app  App
	side Side
	size int // rows for Top/Bottom, cols for Left/Right
}

// Desktop manages a collection of workspaces (Screens).
type Desktop struct {
	tcellScreen       tcell.Screen
	workspaces        map[int]*Screen
	activeWorkspace   *Screen
	statusPanes       []*StatusPane
	quit              chan struct{}
	closeOnce         sync.Once
	ShellAppFactory   AppFactory
	WelcomeAppFactory AppFactory
	styleCache        map[styleKey]tcell.Style
	DefaultFgColor    tcell.Color
	DefaultBgColor    tcell.Color
	dispatcher        *EventDispatcher

	// Global state now lives on the Desktop
	inControlMode   bool
	subControlMode  rune
	resizeSelection *selectedBorder
}

// NewDesktop creates and initializes a new desktop environment.
func NewDesktop(shellFactory, welcomeFactory AppFactory) (*Desktop, error) {
	tcellScreen, err := tcell.NewScreen()
	if err != nil {
		return nil, err
	}
	if err := tcellScreen.Init(); err != nil {
		return nil, err
	}
	defStyle := tcell.StyleDefault.Background(tcell.ColorReset).Foreground(tcell.ColorReset)
	tcellScreen.SetStyle(defStyle)
	tcellScreen.HideCursor()

	defaultFg, defaultBg, _ := initDefaultColors()

	d := &Desktop{
		tcellScreen:       tcellScreen,
		workspaces:        make(map[int]*Screen),
		statusPanes:       make([]*StatusPane, 0),
		quit:              make(chan struct{}),
		ShellAppFactory:   shellFactory,
		WelcomeAppFactory: welcomeFactory,
		styleCache:        make(map[styleKey]tcell.Style),
		DefaultFgColor:    defaultFg,
		DefaultBgColor:    defaultBg,
		dispatcher:        NewEventDispatcher(),
	}

	d.SwitchToWorkspace(1)
	return d, nil
}

func (d *Desktop) Subscribe(listener Listener) {
	d.dispatcher.Subscribe(listener)
}

func (d *Desktop) Unsubscribe(listener Listener) {
	d.dispatcher.Unsubscribe(listener)
}

// AddStatusPane adds a new status pane to the desktop.
func (d *Desktop) AddStatusPane(app App, side Side, size int) {
	sp := &StatusPane{
		app:  app,
		side: side,
		size: size,
	}
	d.statusPanes = append(d.statusPanes, sp)

	if listener, ok := app.(Listener); ok {
		d.Subscribe(listener)
	}

	if d.activeWorkspace != nil {
		app.SetRefreshNotifier(d.activeWorkspace.refreshChan)
	}

	go func() {
		if err := app.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Status pane app exited with error: %v", err)
		}
	}()
	d.recalculateLayout()
}

func (d *Desktop) recalculateLayout() {
	w, h := d.tcellScreen.Size()
	mainX, mainY := 0, 0
	mainW, mainH := w, h

	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			sp.app.Resize(w, sp.size)
			topOffset += sp.size
		case SideBottom:
			sp.app.Resize(w, sp.size)
			bottomOffset += sp.size
		case SideLeft:
			sp.app.Resize(sp.size, h-topOffset-bottomOffset)
			leftOffset += sp.size
		case SideRight:
			sp.app.Resize(sp.size, h-topOffset-bottomOffset)
			rightOffset += sp.size
		}
	}

	mainX = leftOffset
	mainY = topOffset
	mainW = w - leftOffset - rightOffset
	mainH = h - topOffset - bottomOffset

	if d.activeWorkspace != nil {
		d.activeWorkspace.setArea(mainX, mainY, mainW, mainH)
	}
}

func (d *Desktop) Run() error {
	eventChan := make(chan tcell.Event, 10)
	go func() {
		for {
			select {
			case <-d.quit:
				return
			default:
				eventChan <- d.tcellScreen.PollEvent()
			}
		}
	}()

	d.recalculateLayout()

	if d.activeWorkspace != nil {
		go func() {
			d.activeWorkspace.drawChan <- true
		}()
	}

	for {
		if d.activeWorkspace == nil {
			<-time.After(100 * time.Millisecond)
			continue
		}

		refreshChan := d.activeWorkspace.refreshChan
		drawChan := d.activeWorkspace.drawChan

		select {
		case ev := <-eventChan:
			d.handleEvent(ev)

		case <-refreshChan:
			d.broadcastStateUpdate()
			d.draw()

		case <-drawChan:
			d.draw()

		case <-d.quit:
			return nil
		}
	}
}

func (d *Desktop) handleEvent(ev tcell.Event) {
	if _, ok := ev.(*tcell.EventResize); ok {
		d.tcellScreen.Clear()
		d.recalculateLayout()
		d.draw()
		return
	}

	key, ok := ev.(*tcell.EventKey)
	if !ok {
		return
	}

	if key.Key() == keyQuit {
		d.Close()
		return
	}

	if key.Key() == keyControlMode {
		d.toggleControlMode()
		return
	}

	if d.inControlMode {
		d.handleControlMode(key)
		return
	}

	if d.activeWorkspace != nil {
		d.activeWorkspace.handleEvent(key)
	}
}

func (d *Desktop) drawStatusPanes(tcs tcell.Screen) {
	w, h := tcs.Size()
	topOffset, bottomOffset, leftOffset, rightOffset := 0, 0, 0, 0

	for _, sp := range d.statusPanes {
		switch sp.side {
		case SideTop:
			buf := sp.app.Render()
			blit(tcs, leftOffset, topOffset, buf)
			topOffset += sp.size
		case SideBottom:
			buf := sp.app.Render()
			blit(tcs, leftOffset, h-bottomOffset-sp.size, buf)
			bottomOffset += sp.size
		case SideLeft:
			buf := sp.app.Render()
			blit(tcs, leftOffset, topOffset, buf)
			leftOffset += sp.size
		case SideRight:
			buf := sp.app.Render()
			blit(tcs, w-rightOffset-sp.size, topOffset, buf)
			rightOffset += sp.size
		}
	}
}

// toggleControlMode enters or exits control mode.
func (d *Desktop) toggleControlMode() {
	d.inControlMode = !d.inControlMode
	d.subControlMode = 0
	if !d.inControlMode && d.resizeSelection != nil {
		d.activeWorkspace.clearResizeSelection(d.resizeSelection)
		d.resizeSelection = nil
	}

	var eventType EventType
	if d.inControlMode {
		eventType = EventControlOn
	} else {
		eventType = EventControlOff
	}

	if d.activeWorkspace != nil {
		d.activeWorkspace.Broadcast(Event{Type: eventType})
	}
	d.broadcastStateUpdate()
}

// handleControlMode processes all commands when the Desktop is in control mode.
func (d *Desktop) handleControlMode(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyEsc {
		d.toggleControlMode()
		return
	}

	if d.subControlMode != 0 {
		switch d.subControlMode {
		case 'w':
			d.activeWorkspace.SwapActivePane(keyToDirection(ev))
		}
		d.toggleControlMode()
		return
	}

	if ev.Modifiers()&tcell.ModCtrl != 0 {
		d.resizeSelection = d.activeWorkspace.handleInteractiveResize(ev, d.resizeSelection)
		return
	}

	r := ev.Rune()
	if r >= '1' && r <= '9' {
		wsID, _ := strconv.Atoi(string(r))
		d.SwitchToWorkspace(wsID)
		d.toggleControlMode()
		return
	}

	switch r {
	case 'x':
		d.activeWorkspace.CloseActivePane()
	case '|':
		d.activeWorkspace.PerformSplit(Vertical)
	case '-':
		d.activeWorkspace.PerformSplit(Horizontal)
	case 'w':
		d.subControlMode = 'w'
		d.broadcastStateUpdate()
		return
	}

	d.toggleControlMode()
}

// broadcastStateUpdate now broadcasts on the Desktop's dispatcher
func (d *Desktop) broadcastStateUpdate() {
	if d.activeWorkspace == nil {
		return
	}
	title := d.activeWorkspace.tree.GetActiveTitle()

	allWsIDs := make([]int, 0, len(d.workspaces))
	for id := range d.workspaces {
		allWsIDs = append(allWsIDs, id)
	}
	sort.Ints(allWsIDs)

	d.dispatcher.Broadcast(Event{
		Type: EventStateUpdate,
		Payload: StatePayload{
			AllWorkspaces:  allWsIDs,
			WorkspaceID:    d.activeWorkspace.id,
			InControlMode:  d.inControlMode,
			SubMode:        d.subControlMode,
			ActiveTitle:    title,
			DesktopBgColor: d.DefaultBgColor, // Provide the desktop's default background color
		},
	})
	if d.activeWorkspace != nil {
		d.activeWorkspace.Refresh()
	}
}

func (d *Desktop) SwitchToWorkspace(id int) {
	if d.activeWorkspace != nil && d.activeWorkspace.id == id {
		return
	}

	if ws, exists := d.workspaces[id]; exists {
		d.activeWorkspace = ws
	} else {
		ws, err := newScreen(id, d.ShellAppFactory, d)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating workspace: %v\n", err)
			return
		}
		d.workspaces[id] = ws
		d.activeWorkspace = ws

		if d.WelcomeAppFactory != nil {
			welcomeApp := d.WelcomeAppFactory()
			ws.AddApp(welcomeApp)
		}
	}

	for _, sp := range d.statusPanes {
		sp.app.SetRefreshNotifier(d.activeWorkspace.refreshChan)
	}

	d.tcellScreen.Clear()
	d.recalculateLayout()
	d.broadcastStateUpdate()
}

func (d *Desktop) draw() {
	if d.activeWorkspace != nil {
		d.activeWorkspace.draw(d.tcellScreen)
	}
	d.drawStatusPanes(d.tcellScreen)
	d.tcellScreen.Show()
}

func (d *Desktop) Close() {
	d.closeOnce.Do(func() {
		close(d.quit)
		for _, ws := range d.workspaces {
			ws.Close()
		}
		for _, sp := range d.statusPanes {
			sp.app.Stop()
		}
		if d.tcellScreen != nil {
			d.tcellScreen.Fini()
		}
	})
}

func (d *Desktop) getStyle(fg, bg tcell.Color, bold, underline, reverse bool) tcell.Style {
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

func initDefaultColors() (tcell.Color, tcell.Color, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return tcell.ColorDefault, tcell.ColorDefault, fmt.Errorf("open /dev/tty: %w", err)
	}
	defer tty.Close()

	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return tcell.ColorDefault, tcell.ColorDefault, fmt.Errorf("MakeRaw: %w", err)
	}
	defer term.Restore(int(tty.Fd()), oldState)

	query := func(code int) (tcell.Color, error) {
		seq := fmt.Sprintf("\x1b]%d;?\a", code)
		if _, err := tty.WriteString(seq); err != nil {
			return tcell.ColorDefault, err
		}
		resp := make([]byte, 0, 64)
		buf := make([]byte, 1)
		deadline := time.Now().Add(500 * time.Millisecond)
		if err := tty.SetReadDeadline(deadline); err != nil {
			return tcell.ColorDefault, err
		}
		for {
			n, err := tty.Read(buf)
			if err != nil {
				return tcell.ColorDefault, fmt.Errorf("read reply: %w", err)
			}
			resp = append(resp, buf[:n]...)
			if buf[0] == '\a' {
				break
			}
		}
		pattern := fmt.Sprintf(`\x1b\]%d;rgb:([0-9A-Fa-f]{4})/([0-9A-Fa-f]{4})/([0-9A-Fa-f]{4})`, code)
		re := regexp.MustCompile(pattern)
		m := re.FindStringSubmatch(string(resp))
		if len(m) != 4 {
			return tcell.ColorDefault, fmt.Errorf("unexpected reply: %q", resp)
		}
		hex2int := func(s string) (int32, error) {
			v, err := strconv.ParseInt(s, 16, 32)
			return int32(v), err
		}
		r, _ := hex2int(m[1])
		g, _ := hex2int(m[2])
		b, _ := hex2int(m[3])
		return tcell.NewRGBColor(r, g, b), nil
	}

	fg, err := query(10)
	if err != nil {
		fg = tcell.ColorWhite
	}
	bg, err := query(11)
	if err != nil {
		bg = tcell.ColorBlack
	}
	return fg, bg, nil
}
