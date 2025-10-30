package devshell

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/texelterm"
	"texelation/apps/welcome"
	"texelation/texel"
)

var registry = map[string]func(args []string) (texel.App, error){
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

// RunApp locates the named app, runs it inside a local tcell screen, and
// applies a minimal event loop. It is intended for development only.
func RunApp(name string, args []string) error {
	buildApp, ok := registry[name]
	if !ok {
		return fmt.Errorf("unknown app %q", name)
	}

	app, err := buildApp(args)
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
