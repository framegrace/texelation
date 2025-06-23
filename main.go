package main

import (
	"fmt"
	"log"
	"os"
	"texelation/apps/statusbar"
	"texelation/apps/texelterm"
	"texelation/apps/welcome"
	"texelation/texel"
)

func main() {
	// Initialize Texel screen
	screen, err := texel.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Texel screen: %v\n", err)
		os.Exit(1)
	}
	// Ensure resources are released
	defer func() {
		if screen.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing screen\n")
		}
	}()

	log.Println("Application starting...")
	logFile, err := os.OpenFile("ansiterm.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	} else {
		log.SetOutput(logFile)
	}
	defer logFile.Close()

	screen.ShellPaneFactory = func() *texel.Pane {
		shellApp := texelterm.New("shell", "/bin/bash")
		return texel.NewPane(shellApp)
	}

	// Add a clock as a status pane at the bottom
	statusBarApp := statusbar.New()
	screen.AddStatusPane(statusBarApp, texel.SideBottom, 1)

	// Start with a single fullscreen Welcome app (fractional positioning)
	welcome := welcome.NewWelcomeApp()
	pane := texel.NewPane(welcome)
	screen.AddPane(pane)

	// Force initial layout
	screen.ForceResize()

	// Enter main event loop
	if err := screen.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running screen: %v\n", err)
		os.Exit(1)
	}
}
