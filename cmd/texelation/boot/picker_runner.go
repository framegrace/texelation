// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker_runner.go
// Summary: Drives the picker's event loop until the user makes a
// selection. Returns a PickerOutcome the caller translates into a
// connect path.

package boot

import (
	"io"

	"github.com/gdamore/tcell/v2"

	core "github.com/framegrace/texelui/core"
)

// PickerOutcome captures what the user picked.
type PickerOutcome struct {
	Choice    pickerChoice
	SessionID [16]byte // populated when Choice == choiceRecover
}

// RunPicker drives the picker against client until the user makes a
// selection. The screen is shared with the splash and clientrt; this
// function does not call Init/Fini.
//
// gp is the texelui graphics provider used for thumbnail rendering.
// Pass nil for ASCII-only mode. flushTo is where Kitty APC sequences
// are written after each Render — typically os.Stdout in production.
func RunPicker(screen tcell.Screen, client PickerClient, gp core.GraphicsProvider, flushTo io.Writer) (PickerOutcome, error) {
	p := NewPicker(screen, client)
	p.SetGraphicsProvider(gp, flushTo)
	p.RefreshCatalog()

	for !p.Done() {
		p.Render()
		ev := screen.PollEvent()
		switch e := ev.(type) {
		case *tcell.EventKey:
			p.HandleKey(e.Key(), e.Rune(), e.Modifiers())
		case *tcell.EventResize:
			screen.Sync()
		case *tcell.EventInterrupt:
			return PickerOutcome{Choice: choiceQuit}, nil
		}
	}

	out := PickerOutcome{Choice: p.choice}
	if p.choice == choiceRecover && len(p.response.Stored) > 0 {
		out.SessionID = p.response.Stored[p.selectedIdx].SessionID
	}
	return out, nil
}
