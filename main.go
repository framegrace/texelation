package main

import (
	"log"
	"os"
	//	"texelation/apps/clock"
	"texelation/apps/texelterm"
	//	"texelation/apps/welcome"
	"texelation/texel"
)

func main() {
	screen, err := texel.NewScreen()
	if err != nil {
		panic(err)
	}
	defer screen.Close()

	log.Println("Application starting...")
	logFile, err := os.OpenFile("ansiterm.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	} else {
		log.SetOutput(logFile)
	}
	defer logFile.Close()

	log.Println("Application starting...")

	// This function now defines the desired layout proportionally
	setupPanes(screen)

	// Manually trigger a resize at the start to draw the initial layout
	screen.ForceResize()

	if err := screen.Run(); err != nil {
		log.Fatalf("Application exited with error: %v", err)
	}
	log.Println("Application stopped cleanly.")
}

// setupPanes defines the layout of the panes and the apps they contain.
func setupPanes(screen *texel.Screen) {
	// Create the applications that will run in the panes
	//	appHtop := texelterm.New("htop", "htop")
	appPTYShell := texelterm.New("shell", "/bin/bash")
	appPTYShell2 := texelterm.New("shell", "/bin/bash")

	// Define a simple 50/50 vertical split layout
	panes := []*texel.Pane{
		texel.NewPane(texel.Rect{X: 0.0, Y: 0.0, W: 0.5, H: 1.0}, appPTYShell),
		texel.NewPane(texel.Rect{X: 0.5, Y: 0.0, W: 0.5, H: 1.0}, appPTYShell2),
	}

	for _, p := range panes {
		screen.AddPane(p)
	}
}
