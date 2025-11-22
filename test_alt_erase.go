package main

import (
	"fmt"
	"texelation/apps/texelterm/parser"
)

func main() {
	vterm := parser.NewVTerm(80, 24)
	p := parser.NewParser(vterm)

	// Simulate what happens in a real terminal session with codex

	// 1. User has a prompt
	for _, r := range "\x1b[34m❯ \x1b[0m" {
		p.Parse(r)
	}
	for _, r := range "codex\n" {
		p.Parse(r)
	}

	fmt.Println("=== MAIN SCREEN (after prompt) ===")
	printLine(vterm, 0, false)
	printLine(vterm, 1, false)

	// 2. Codex switches to alternate screen
	for _, r := range "\x1b[?1049h" {
		p.Parse(r)
	}

	fmt.Println("\n=== ALT SCREEN (after switch, should be clear) ===")
	printLine(vterm, 0, true)

	// 3. Codex draws menu
	for _, r := range "\x1b[H" { // Home
		p.Parse(r)
	}
	for _, r := range "\x1b[2J" { // Clear screen
		p.Parse(r)
	}
	for _, r := range "\x1b[1;1H" { // Position 1,1
		p.Parse(r)
	}
	for _, r := range "1. Update now" {
		p.Parse(r)
	}

	fmt.Println("\n=== ALT SCREEN (after drawing) ===")
	printLine(vterm, 0, true)

	// 4. Simulate redraw: CR, EL, write
	for _, r := range "\r" {
		p.Parse(r)
	}
	for _, r := range "\x1b[2K" { // Erase entire line
		p.Parse(r)
	}
	for _, r := range "2. Skip" {
		p.Parse(r)
	}

	fmt.Println("\n=== ALT SCREEN (after erase and rewrite) ===")
	printLine(vterm, 0, true)
}

func printLine(vterm *parser.VTerm, y int, altScreen bool) {
	fmt.Printf("Line %d: ", y)

	var line []parser.Cell
	if altScreen {
		// Access altBuffer through reflection or export it for testing
		fmt.Print("[ALT] ")
		// For now, just print what we can see
	} else {
		if y < vterm.HistoryLength() {
			line = vterm.HistoryLineCopy(y + vterm.GetTopHistoryLine())
		}
	}

	if line != nil {
		for i := 0; i < 20 && i < len(line); i++ {
			if line[i].Rune == 0 || line[i].Rune == ' ' {
				fmt.Print("·")
			} else {
				fmt.Printf("%c", line[i].Rune)
			}
		}
	}
	fmt.Println()
}
