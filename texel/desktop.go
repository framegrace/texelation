package texel

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// Desktop manages a collection of workspaces (Screens).
type Desktop struct {
	tcellScreen       tcell.Screen
	workspaces        map[int]*Screen
	activeWorkspace   *Screen
	quit              chan struct{}
	closeOnce         sync.Once
	ShellAppFactory   AppFactory
	WelcomeAppFactory AppFactory
	styleCache        map[styleKey]tcell.Style
	DefaultFgColor    tcell.Color
	DefaultBgColor    tcell.Color
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
		quit:              make(chan struct{}),
		ShellAppFactory:   shellFactory,
		WelcomeAppFactory: welcomeFactory,
		styleCache:        make(map[styleKey]tcell.Style),
		DefaultFgColor:    defaultFg,
		DefaultBgColor:    defaultBg,
	}

	d.SwitchToWorkspace(1)
	return d, nil
}

// Run starts the main event loop for the entire desktop.
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

	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		if d.activeWorkspace == nil {
			<-time.After(100 * time.Millisecond)
			continue
		}
		refreshChan := d.activeWorkspace.refreshChan
		drawChan := d.activeWorkspace.drawChan

		if d.activeWorkspace.needsDraw {
			d.draw()
			d.activeWorkspace.needsDraw = false
		}

		select {
		case ev := <-eventChan:
			d.handleEvent(ev)
		case <-refreshChan:
			d.activeWorkspace.broadcastStateUpdate()
			d.draw()
		case <-drawChan:
			d.draw()
		case <-ticker.C:
			if d.activeWorkspace.needsContinuousUpdate() {
				d.draw()
			}
		case <-d.quit:
			return nil
		}
	}
}

func (d *Desktop) handleEvent(ev tcell.Event) {
	if resize, ok := ev.(*tcell.EventResize); ok {
		w, h := resize.Size()
		d.tcellScreen.Clear()
		if d.activeWorkspace != nil {
			d.activeWorkspace.recalculateLayout(w, h)
			d.draw()
		}
		return
	}

	if d.activeWorkspace != nil {
		d.activeWorkspace.handleEvent(ev)
	}

	if d.activeWorkspace.inControlMode && d.activeWorkspace.subControlMode == 0 {
		if key, ok := ev.(*tcell.EventKey); ok {
			wsID := -1
			if key.Key() >= tcell.KeyF1 && key.Key() <= tcell.KeyF9 {
				wsID = int(key.Key()-tcell.KeyF1) + 1
			} else if r := key.Rune(); r >= '1' && r <= '9' {
				wsID, _ = strconv.Atoi(string(r))
			}

			if wsID != -1 {
				d.SwitchToWorkspace(wsID)
			}
		}
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

	d.tcellScreen.Clear()
	w, h := d.tcellScreen.Size()
	d.activeWorkspace.recalculateLayout(w, h)
	d.activeWorkspace.needsDraw = true
}

func (d *Desktop) draw() {
	if d.activeWorkspace != nil {
		d.activeWorkspace.draw(d.tcellScreen)
	}
}

func (d *Desktop) Close() {
	d.closeOnce.Do(func() {
		close(d.quit)
		for _, ws := range d.workspaces {
			ws.Close()
		}
		if d.tcellScreen != nil {
			d.tcellScreen.Fini()
		}
	})
}

// getStyle centrally manages tcell.Style objects to avoid re-creation.
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

// initDefaultColors queries the terminal for its default colors.
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
