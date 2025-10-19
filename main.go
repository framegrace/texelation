package main

import (
	"fmt"
	"log"
	"os"
	"texelation/apps/statusbar"
	"texelation/apps/texelterm"
	"texelation/apps/welcome"
	"texelation/texel"
	"texelation/texel/theme"
)

func main() {
	logFile, err := os.OpenFile("ansiterm.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	log.Println("Application starting...")

	// Get the theme manager singleton. It will load the user's theme.json if it exists.
	tm := theme.Get()

	// Ensure the default theme exists on disk so users can customize it.
	theme.ApplyDefaults(tm)

	// Define factories for the apps we want to use.
	shellFactory := func() texel.App {
		return texelterm.New("shell", "/bin/bash")
	}
	welcomeFactory := func() texel.App {
		return welcome.NewWelcomeApp()
	}

	// Initialize the Desktop Environment with the factories.
	desktop, err := texel.NewDesktop(shellFactory, welcomeFactory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating desktop: %v\n", err)
		os.Exit(1)
	}
	defer desktop.Close()

	statusBarApp := statusbar.New()
	desktop.AddStatusPane(statusBarApp, texel.SideTop, 1)

	// Enter main event loop.
	if err := desktop.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running desktop: %v\n", err)
		os.Exit(1)
	}
}
