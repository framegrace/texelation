package main

import (
	"log"
	"os"
	"texelation/apps/clock"
	"texelation/apps/texelterm"
	"texelation/apps/welcome"
	"texelation/texel"

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
	screen, err := texel.NewScreen()
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
func setupPanes(screen *texel.Screen) {
	w, h := termbox.Size()
	cellW := w / 2
	cellH := h / 2

	// Create the applications that will run in the panes
	appTop := texelterm.New("htop", "htop")
	appClock := clock.NewClockApp()
	appWelcome := welcome.NewWelcomeApp()
	appPTYShell := texelterm.New("shell", "/bin/bash")

	// Create panes and add them to the screen
	panes := []*texel.Pane{
		texel.NewPane(0, 0, cellW, cellH, appTop),
		texel.NewPane(cellW, 0, w, cellH, appWelcome),
		texel.NewPane(0, cellH, cellW, h, appClock),
		texel.NewPane(cellW, cellH, w, h, appPTYShell),
	}

	for _, p := range panes {
		screen.AddPane(p)
	}
}
