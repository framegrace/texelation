package main

import (
	"fmt"
	"os"
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

	// Start with a single fullscreen Welcome app (fractional positioning)
	welcome := welcome.NewWelcomeApp()
	pane := texel.NewPane(texel.Rect{X: 0.0, Y: 0.0, W: 1.0, H: 1.0}, welcome)
	screen.AddPane(pane)

	// Force initial layout
	screen.ForceResize()

	// Enter main event loop
	if err := screen.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running screen: %v\n", err)
		os.Exit(1)
	}
}
