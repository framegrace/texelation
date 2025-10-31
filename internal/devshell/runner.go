package devshell

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/texelterm"
	"texelation/apps/welcome"
	"texelation/texel"
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
	"welcome": func(args []string) (texel.App, error) {
		return welcome.NewWelcomeApp(), nil
	},
}

// Run executes the provided builder inside a local tcell screen.
func Run(builder Builder, args []string) error {
	app, err := builder(args)
	if err != nil {
		return err
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("init screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("screen init: %w", err)
	}
	defer screen.Fini()
	screen.Clear()

	width, height := screen.Size()
	app.Resize(width, height)
	refreshCh := make(chan bool, 1)
	app.SetRefreshNotifier(refreshCh)

	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run()
	}()
	defer app.Stop()

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

	go func() {
		for range refreshCh {
			screen.PostEvent(tcell.NewEventInterrupt(nil))
		}
	}()

	draw()

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
		case *tcell.EventKey:
			if tev.Key() == tcell.KeyCtrlC {
				return nil
			}
			app.HandleKey(tev)
			draw()
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
