package devshell

import (
	"fmt"
	"log"
	"strings"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/configeditor"
	"texelation/apps/help"
	"texelation/apps/texelterm"
	texeluidemo "texelation/apps/texelui-demo"
	"texelation/texel"
	"texelation/texel/theme"
)

// Builder constructs a texel.App, optionally using CLI args.
type Builder func(args []string) (texel.App, error)

var registry = map[string]Builder{
	"texelterm": func(args []string) (texel.App, error) {
		shell := "/bin/bash"
		if len(args) > 0 {
			shell = strings.Join(args, " ")
		}
		return texelterm.New("texelterm", shell), nil
	},
	"help": func(args []string) (texel.App, error) {
		return help.NewHelpApp(), nil
	},
	"texelui-demo": func(args []string) (texel.App, error) {
		return texeluidemo.New(), nil
	},
	"config-editor": func(args []string) (texel.App, error) {
		return configeditor.New(nil), nil
	},
}

var screenFactory = tcell.NewScreen

// SetScreenFactory overrides the screen factory used by Run. Passing nil restores the default.
func SetScreenFactory(factory func() (tcell.Screen, error)) {
	if factory == nil {
		screenFactory = tcell.NewScreen
		return
	}
	screenFactory = factory
}

// Run executes the provided builder inside a local tcell screen.
func Run(builder Builder, args []string) error {
	return RunWithName("", builder, args)
}

// RunWithName executes the provided builder inside a local tcell screen with an optional app name.
func RunWithName(name string, builder Builder, args []string) error {
	app, err := builder(args)
	if err != nil {
		return err
	}
	if name != "" && name != "config-editor" {
		app = newToggleApp(name, app)
	}

	screen, err := screenFactory()
	if err != nil {
		return fmt.Errorf("init screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("screen init: %w", err)
	}
	defer screen.Fini()
	screen.Clear()
	screen.EnableMouse(tcell.MouseMotionEvents)
	defer screen.DisableMouse()
	screen.EnablePaste() // Enable bracketed paste support

	// Check if theme loaded successfully
	_ = theme.Get() // Force theme initialization
	if err := theme.GetLoadError(); err != nil {
		screen.Fini()
		log.Fatalf("Fatal: Theme configuration error: %v\nPlease fix your theme file and try again.", err)
	}

	width, height := screen.Size()
	app.Resize(width, height)
	refreshCh := make(chan bool, 1)
	app.SetRefreshNotifier(refreshCh)

	draw := func() {
		screen.Clear()
		buffer := app.Render()
		if buffer != nil {
			for y := 0; y < len(buffer); y++ {
				row := buffer[y]
				for x := 0; x < len(row); x++ {
					cell := row[x]
					screen.SetContent(x, y, cell.Ch, nil, cell.Style)
				}
			}
		}
		screen.Show()
	}

	draw()

	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run()
	}()
	defer app.Stop()

	go func() {
		for range refreshCh {
			screen.PostEvent(tcell.NewEventInterrupt(nil))
		}
	}()

	draw()

	var pasteBuffer []byte
	var inPaste bool

	for {
		select {
		case err := <-runErr:
			return err
		default:
		}

		ev := screen.PollEvent()
		switch tev := ev.(type) {
		case *tcell.EventInterrupt:
			draw()
		case *tcell.EventResize:
			w, h := tev.Size()
			app.Resize(w, h)
			draw()
		case *tcell.EventPaste:
			// Bracketed paste event from tcell
			if tev.Start() {
				inPaste = true
				pasteBuffer = nil
			} else if tev.End() {
				inPaste = false
				// Send collected paste data to app
				if ph, ok := app.(interface{ HandlePaste([]byte) }); ok && len(pasteBuffer) > 0 {
					ph.HandlePaste(pasteBuffer)
					draw()
				}
				pasteBuffer = nil
			}
		case *tcell.EventKey:
			if tev.Key() == tcell.KeyCtrlC {
				return nil
			}
			if inPaste {
				// Collect paste data
				if tev.Key() == tcell.KeyRune {
					pasteBuffer = append(pasteBuffer, []byte(string(tev.Rune()))...)
				} else if tev.Key() == tcell.KeyEnter || tev.Key() == 10 { // KeyEnter (CR) or LF
					pasteBuffer = append(pasteBuffer, '\n')
				}
				// Don't draw during paste collection
			} else {
				// Normal key handling
				app.HandleKey(tev)
				draw()
			}
		case *tcell.EventMouse:
			if mh, ok := app.(interface{ HandleMouse(*tcell.EventMouse) }); ok {
				mh.HandleMouse(tev)
				draw()
			}
		}
	}
}

// RunApp finds a registered builder by name and runs it.
func RunApp(name string, args []string) error {
	buildApp, ok := registry[name]
	if !ok {
		return fmt.Errorf("unknown app %q", name)
	}
	return RunWithName(name, buildApp, args)
}

type toggleApp struct {
	name    string
	main    texel.App
	editor  texel.App
	active  texel.App
	refresh chan<- bool
}

func newToggleApp(name string, main texel.App) texel.App {
	editor := configeditor.NewWithTarget(nil, name)
	return &toggleApp{
		name:   name,
		main:   main,
		editor: editor,
		active: main,
	}
}

func (t *toggleApp) Run() error {
	errCh := make(chan error, 2)
	go func() { errCh <- t.main.Run() }()
	go func() { errCh <- t.editor.Run() }()
	return <-errCh
}

func (t *toggleApp) Stop() {
	t.main.Stop()
	t.editor.Stop()
}

func (t *toggleApp) Resize(cols, rows int) {
	t.main.Resize(cols, rows)
	t.editor.Resize(cols, rows)
}

func (t *toggleApp) Render() [][]texel.Cell {
	if t.active == nil {
		return nil
	}
	return t.active.Render()
}

func (t *toggleApp) GetTitle() string {
	if t.active == nil {
		return "App"
	}
	return t.active.GetTitle()
}

func (t *toggleApp) HandleKey(ev *tcell.EventKey) {
	if ev.Key() == tcell.KeyCtrlF {
		t.toggle()
		return
	}
	if t.active != nil {
		t.active.HandleKey(ev)
	}
}

func (t *toggleApp) HandleMouse(ev *tcell.EventMouse) {
	if t.active == nil {
		return
	}
	if mh, ok := t.active.(interface{ HandleMouse(*tcell.EventMouse) }); ok {
		mh.HandleMouse(ev)
	}
}

func (t *toggleApp) SetRefreshNotifier(ch chan<- bool) {
	t.refresh = ch
	t.main.SetRefreshNotifier(ch)
	t.editor.SetRefreshNotifier(ch)
}

func (t *toggleApp) toggle() {
	if t.active == t.main {
		t.active = t.editor
	} else {
		t.active = t.main
	}
	t.requestRefresh()
}

func (t *toggleApp) requestRefresh() {
	if t.refresh == nil {
		return
	}
	select {
	case t.refresh <- true:
	default:
	}
}
