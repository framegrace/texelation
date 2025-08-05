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

	// Register the default theme settings for all core components and apps.
	// If the user's theme.json is missing a section or a key, these defaults will be used
	// and then saved back to the file, allowing users to discover and customize new options.
	registerDefaultThemes(tm)

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

func registerDefaultThemes(tm theme.Config) {
	tm.RegisterDefaults("desktop", theme.Section{
		"default_fg": "#f8f8f2",
		"default_bg": "#282a36",
	})
	tm.RegisterDefaults("pane", theme.Section{
		"inactive_border_fg": "#6272a4",
		"active_border_fg":   "#50fa7b",
		"resizing_border_fg": "#ffb86c",
	})
	tm.RegisterDefaults("statusbar", theme.Section{
		"base_fg":         "#f8f8f2",
		"base_bg":         "#21222C",
		"inactive_tab_fg": "#6272a4",
		"inactive_tab_bg": "#383a46",
		"active_tab_fg":   "#f8f8f2",
		"active_tab_bg":   "#44475a",
		"control_mode_fg": "#f8f8f2",
		"control_mode_bg": "#ff5555",
		"title_fg":        "#8be9fd",
		"clock_fg":        "#f1fa8c",
	})
	tm.RegisterDefaults("welcome", theme.Section{
		"text_fg": "#bd93f9",
	})
	tm.RegisterDefaults("clock", theme.Section{
		"text_fg": "#f1fa8c",
	})

	// After registering all defaults, save the config.
	// This writes new defaults to the user's file without overwriting their changes.
	if err := tm.Save(); err != nil {
		log.Printf("Theme error: failed to save updated theme file: %v", err)
	}
}
