package main

import (
	"log"
	"os"
	"textmode-env/tui"

	"github.com/nsf/termbox-go"
)

func main() {
	// Setup logging
	logFile, err := os.OpenFile("ansiterm.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.Println("Application starting...")

	// Initialize the main screen
	screen, err := tui.NewScreen()
	if err != nil {
		panic(err)
	}
	defer screen.Close()

	// Define the layout and create apps for the panes
	setupPanes(screen)

	// Run the main application loop
	if err := screen.Run(); err != nil {
		log.Fatalf("Application exited with error: %v", err)
	}
	log.Println("Application stopped cleanly.")
}

// setupPanes defines the layout of the panes and the apps they contain.
func setupPanes(screen *tui.Screen) {
	w, h := termbox.Size()
	cellW := w / 2
	cellH := h / 2

	// Create the applications that will run in the panes
	appTop := tui.NewPTYApp("htop", "htop")
	appClock := tui.NewClockApp()
	appWelcome := tui.NewWelcomeApp()
	appPTYShell := tui.NewPTYApp("shell", "/bin/bash")

	// Create panes and add them to the screen
	panes := []*tui.Pane{
		tui.NewPane(0, 0, cellW, cellH, appTop),
		tui.NewPane(cellW, 0, w, cellH, appWelcome),
		tui.NewPane(0, cellH, cellW, h, appClock),
		tui.NewPane(cellW, cellH, w, h, appPTYShell),
	}

	for _, p := range panes {
		screen.AddPane(p)
	}
}
