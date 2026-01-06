package devshell

import (
	"fmt"
	"strings"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/apps/configeditor"
	"github.com/framegrace/texelation/apps/help"
	"github.com/framegrace/texelation/apps/texelterm"
)

// Builder constructs a texelcore.App, optionally using CLI args.
type Builder func(args []string) (texelcore.App, error)

var registry = map[string]Builder{
	"texelterm": func(args []string) (texelcore.App, error) {
		shell := "/bin/bash"
		if len(args) > 0 {
			shell = strings.Join(args, " ")
		}
		return texelterm.New("texelterm", shell), nil
	},
	"help": func(args []string) (texelcore.App, error) {
		return help.NewHelpApp(), nil
	},
	"config-editor": func(args []string) (texelcore.App, error) {
		return configeditor.New(nil), nil
	},
}

// SetScreenFactory overrides the screen factory used by Run. Passing nil restores the default.
func SetScreenFactory(factory func() (tcell.Screen, error)) {
	runtime.SetScreenFactory(factory)
}

// Run executes the provided builder inside a local tcell screen.
func Run(builder Builder, args []string) error {
	return RunWithName("", builder, args)
}

// RunWithName executes the provided builder inside a local tcell screen with an optional app name.
func RunWithName(name string, builder Builder, args []string) error {
	if builder == nil {
		return fmt.Errorf("devshell: nil builder")
	}
	wrapped := func(runArgs []string) (texelcore.App, error) {
		app, err := builder(runArgs)
		if err != nil {
			return nil, err
		}
		if name != "" && name != "config-editor" {
			app = newToggleApp(name, app)
		}
		return app, nil
	}

	opts := runtime.Options{ExitKey: tcell.KeyCtrlC}
	return runtime.RunWithOptions(wrapped, opts, args...)
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
	main    texelcore.App
	editor  texelcore.App
	active  texelcore.App
	refresh chan<- bool
}

func newToggleApp(name string, main texelcore.App) texelcore.App {
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

func (t *toggleApp) Render() [][]texelcore.Cell {
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
