package runtimeadapter

import (
	"fmt"
	"strings"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/runtime"
	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/apps/configeditor"
	"github.com/framegrace/texelation/apps/help"
	"github.com/framegrace/texelation/apps/texelterm"
	"github.com/framegrace/texelation/registry"
)

// Builder constructs a texelcore.App, optionally using CLI args.
type Builder func(args []string) (texelcore.App, error)

var builderRegistry = map[string]Builder{
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
		return fmt.Errorf("runtime adapter: nil builder")
	}
	wrapped := func(runArgs []string) (texelcore.App, error) {
		app, err := builder(runArgs)
		if err != nil {
			return nil, err
		}
		if name != "" && name != "config-editor" {
			app = newToggleApp(nil, name, app)
		}
		return app, nil
	}

	opts := runtime.Options{ExitKey: tcell.KeyCtrlC}
	return runtime.RunWithOptions(wrapped, opts, args...)
}

// RunApp finds a registered builder by name and runs it.
func RunApp(name string, args []string) error {
	buildApp, ok := builderRegistry[name]
	if !ok {
		return fmt.Errorf("unknown app %q", name)
	}
	return RunWithName(name, buildApp, args)
}

// WrapApp decorates a runtime app with Texelation-specific runtime behavior.
func WrapApp(name string, app texelcore.App) texelcore.App {
	return wrapApp(nil, name, app)
}

// WrapForRegistry returns a registry wrapper that decorates apps as they are created.
func WrapForRegistry(reg *registry.Registry) registry.AppWrapper {
	return func(name string, app interface{}) interface{} {
		typed, ok := app.(texelcore.App)
		if !ok {
			return app
		}
		return wrapApp(reg, name, typed)
	}
}

func wrapApp(reg *registry.Registry, name string, app texelcore.App) texelcore.App {
	if app == nil {
		return nil
	}
	if name == "" || name == "config-editor" || name == "launcher" {
		return app
	}
	return newToggleApp(reg, name, app)
}

type toggleApp struct {
	name    string
	main    texelcore.App
	editor  texelcore.App
	active  texelcore.App
	refresh chan<- bool
}

func newToggleApp(reg *registry.Registry, name string, main texelcore.App) texelcore.App {
	editor := configeditor.NewWithTarget(reg, name)
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

func (t *toggleApp) RegisterControl(id, description string, handler func(payload interface{}) error) error {
	if provider, ok := t.main.(texelcore.ControlBusProvider); ok {
		return provider.RegisterControl(id, description, handler)
	}
	if provider, ok := t.editor.(texelcore.ControlBusProvider); ok {
		return provider.RegisterControl(id, description, handler)
	}
	return fmt.Errorf("runtime adapter: no control bus available")
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

// SnapshotMetadata implements texelcore.SnapshotProvider by delegating to the main app.
// This is critical for persistence - without it, the app type is not saved to snapshots.
func (t *toggleApp) SnapshotMetadata() (appType string, config map[string]interface{}) {
	if provider, ok := t.main.(interface {
		SnapshotMetadata() (string, map[string]interface{})
	}); ok {
		return provider.SnapshotMetadata()
	}
	return "", nil
}

// SetPaneID implements texelcore.PaneIDSetter by delegating to the main app.
// This is critical for terminal history persistence - without it, texelterm can't find its history file.
func (t *toggleApp) SetPaneID(id [16]byte) {
	if setter, ok := t.main.(interface{ SetPaneID([16]byte) }); ok {
		setter.SetPaneID(id)
	}
}

// MouseWheelEnabled implements texelcore.MouseWheelDeclarer by delegating to the main app.
// This is critical for mouse wheel scrolling in terminals.
func (t *toggleApp) MouseWheelEnabled() bool {
	if declarer, ok := t.main.(interface{ MouseWheelEnabled() bool }); ok {
		return declarer.MouseWheelEnabled()
	}
	return false
}

// HandleMouseWheel implements texelcore.MouseWheelHandler by delegating to the active app.
// This enables mouse wheel scrolling in terminals wrapped by toggleApp.
func (t *toggleApp) HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask) {
	if t.active == nil {
		return
	}
	if handler, ok := t.active.(interface {
		HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask)
	}); ok {
		handler.HandleMouseWheel(x, y, deltaX, deltaY, modifiers)
	}
}
