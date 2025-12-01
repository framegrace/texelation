package devshell

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/texelterm"
	"texelation/apps/help"
	"texelation/texel"
	"texelation/texelui/adapter"
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
        return adapter.NewTextEditorApp("TexelUI Demo"), nil
    },
    "texelui-demo2": func(args []string) (texel.App, error) {
        return adapter.NewDualTextEditorApp("TexelUI Dual Demo"), nil
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
	app, err := builder(args)
	if err != nil {
		return err
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
	screen.EnableMouse()
	defer screen.DisableMouse()
	screen.EnablePaste() // Enable bracketed paste support

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
	return Run(buildApp, args)
}
