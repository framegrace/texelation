// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texelui/examples/widget_demo.go
// Summary: Demonstrates all TexelUI widgets in a simple form layout.
// Usage: go run texelui/examples/widget_demo.go
// Notes: Shows Label, Input, Checkbox, Button widgets with VBox layout.

package main

import (
	"fmt"
	"log"
	"os"
	"texelation/texelui/core"
	"texelation/texelui/widgets"

	"github.com/gdamore/tcell/v2"
)

func main() {
	// Initialize screen
	screen, err := tcell.NewScreen()
	if err != nil {
		log.Fatal(err)
	}
	if err := screen.Init(); err != nil {
		log.Fatal(err)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
	screen.Clear()

	// Create UI manager
	ui := core.NewUIManager()
	width, height := screen.Size()
	ui.Resize(width, height)

	// Create widgets
	title := widgets.NewLabel(5, 2, 40, 1, "TexelUI Widget Demo")
	title.Align = widgets.AlignCenter

	nameLabel := widgets.NewLabel(5, 4, 10, 1, "Name:")
	nameInput := widgets.NewInput(16, 4, 25)
	nameInput.Placeholder = "Enter your name"

	emailLabel := widgets.NewLabel(5, 6, 10, 1, "Email:")
	emailInput := widgets.NewInput(16, 6, 25)
	emailInput.Placeholder = "user@example.com"

	termsCheckbox := widgets.NewCheckbox(5, 8, "I agree to the terms")

	var resultLabel *widgets.Label

	submitButton := widgets.NewButton(5, 10, 0, 1, "Submit")
	submitButton.OnClick = func() {
		msg := fmt.Sprintf("Name: %s, Email: %s, Terms: %v",
			nameInput.Text, emailInput.Text, termsCheckbox.Checked)
		if resultLabel == nil {
			resultLabel = widgets.NewLabel(5, 12, 50, 1, msg)
			ui.AddWidget(resultLabel)
		} else {
			resultLabel.Text = msg
		}
		ui.Invalidate(core.Rect{X: 0, Y: 0, W: width, H: height})
	}

	cancelButton := widgets.NewButton(20, 10, 0, 1, "Cancel")
	cancelButton.OnClick = func() {
		screen.Fini()
		fmt.Println("Demo cancelled")
		os.Exit(0)
	}

	helpLabel := widgets.NewLabel(5, height-2, width-10, 1, "Tab/Shift-Tab: navigate | Enter/Space: activate | Esc: quit")

	// Add widgets to UI manager
	ui.AddWidget(title)
	ui.AddWidget(nameLabel)
	ui.AddWidget(nameInput)
	ui.AddWidget(emailLabel)
	ui.AddWidget(emailInput)
	ui.AddWidget(termsCheckbox)
	ui.AddWidget(submitButton)
	ui.AddWidget(cancelButton)
	ui.AddWidget(helpLabel)

	// Focus first input
	ui.Focus(nameInput)

	// Main event loop
	running := true
	for running {
		// Render
		buf := ui.Render()
		for y := 0; y < len(buf); y++ {
			for x := 0; x < len(buf[y]); x++ {
				cell := buf[y][x]
				screen.SetContent(x, y, cell.Ch, nil, cell.Style)
			}
		}
		screen.Show()

		// Handle events
		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyEscape {
				running = false
			} else {
				// Tab/Shift-Tab handled internally by HandleKey
				ui.HandleKey(ev)
			}
		case *tcell.EventMouse:
			ui.HandleMouse(ev)
		case *tcell.EventResize:
			width, height = ev.Size()
			ui.Resize(width, height)
			helpLabel.Resize(width-10, 1)
			helpLabel.SetPosition(5, height-2)
			screen.Sync()
		}
	}

	screen.Fini()
	fmt.Println("Demo completed")
}
