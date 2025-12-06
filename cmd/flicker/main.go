package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"texelation/apps/flicker"
	"texelation/texel"
)

func main() {
	targetFPS := 60
	if len(os.Args) > 1 {
		var fps int
		if _, err := fmt.Sscanf(os.Args[1], "%d", &fps); err == nil && fps > 0 {
			targetFPS = fps
		}
	}
	fmt.Printf("Targeting %d FPS\n", targetFPS)

	app := flicker.New()

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	frames, elapsed := runLoop(app, screen, targetFPS)
	
	screen.Fini()
	
	fmt.Fprintf(os.Stderr, "Rendered %d frames in %v (%.2f FPS)\n", frames, elapsed, float64(frames)/elapsed.Seconds())
}

func runLoop(app texel.App, screen tcell.Screen, targetFPS int) (int, time.Duration) {
	// Set up a simple event loop
	quit := make(chan struct{})
	refresh := make(chan bool, 1)

	// Wire up refresh notification (flicker app expects this)
	app.SetRefreshNotifier(refresh)

	// Initial resize
	w, h := screen.Size()
	app.Resize(w, h)

	// Start app
	go func() {
		if err := app.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "App error: %v\n", err)
			close(quit)
		}
	}()

	// Event loop
	go func() {
		for {
			ev := screen.PollEvent()
			switch ev := ev.(type) {
			case *tcell.EventResize:
				w, h := screen.Size()
				app.Resize(w, h)
				screen.Sync()
				refresh <- true
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyEsc || ev.Key() == tcell.KeyCtrlC {
					close(quit)
					return
				}
				app.HandleKey(ev)
			}
		}
	}()

	// Render loop
	ticker := time.NewTicker(time.Second / time.Duration(targetFPS))
	defer ticker.Stop()
	
	var frames int
	start := time.Now()

	for {
		select {
		case <-quit:
			return frames, time.Since(start)
		case <-refresh:
			// Drain multiple refresh signals
		default:
		}
		
		select {
		case <-ticker.C:
			// Render the app buffer
			buf := app.Render()
			
			screen.Clear()
			w, h := screen.Size()
			
			for y := 0; y < len(buf) && y < h; y++ {
				for x := 0; x < len(buf[y]) && x < w; x++ {
					cell := buf[y][x]
					screen.SetContent(x, y, cell.Ch, nil, cell.Style)
				}
			}
			screen.Show()
			frames++
		case <-quit:
			return frames, time.Since(start)
		}
	}
}
