// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/input_handler.go
// Summary: Event processing and input handling for client runtime.
// Usage: Handles keyboard, mouse, resize, and paste events, routing them to the server.

package clientruntime

import (
	"log"
	"net"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/internal/effects"
	"github.com/framegrace/texelation/protocol"
)

func handleScreenEvent(ev tcell.Event, state *clientState, screen tcell.Screen, conn net.Conn, sessionID [16]byte, writeMu *sync.Mutex) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		// Dismiss restart notification on any key press
		if state.showRestartNotification && !state.restartNotificationDismissed {
			state.restartNotificationDismissed = true
			render(state, screen)
			return true // Consume the event
		}
		if state.pasting {
			consumePasteKey(state, ev)
			return true
		}
		if ev.Key() == tcell.KeyCtrlR {
			if state.effects != nil {
				state.effects.HandleTrigger(effects.EffectTrigger{
					Type:      effects.TriggerCryptToggle,
					Timestamp: time.Now(),
				})
			}
			render(state, screen)
			return true
		}
		if state.controlMode && ev.Modifiers() == 0 {
			r := ev.Rune()
			if r == 'q' || r == 'Q' {
				if err := sendKeyEvent(writeMu, conn, sessionID, tcell.KeyEsc, 0, 0); err != nil {
					log.Printf("control reset failed: %v", err)
				}
				state.controlMode = false
				state.subMode = 0
				if state.effects != nil {
					state.effects.HandleTrigger(effects.EffectTrigger{
						Type:      effects.TriggerWorkspaceControl,
						Active:    state.controlMode,
						Timestamp: time.Now(),
					})
				}
				log.Printf("control quit requested; closing client")
				return false
			}
		}
		if ev.Key() == tcell.KeyCtrlA {
			state.controlMode = !state.controlMode
			state.subMode = 0
			if state.effects != nil {
				state.effects.HandleTrigger(effects.EffectTrigger{
					Type:      effects.TriggerWorkspaceControl,
					Active:    state.controlMode,
					Timestamp: time.Now(),
				})
			}
			render(state, screen)
		}
		if ev.Key() == tcell.KeyEsc && ev.Modifiers() == 0 && state.controlMode {
			state.controlMode = false
			state.subMode = 0
			if state.effects != nil {
				state.effects.HandleTrigger(effects.EffectTrigger{
					Type:      effects.TriggerWorkspaceControl,
					Active:    state.controlMode,
					Timestamp: time.Now(),
				})
			}
			render(state, screen)
		}
		if err := sendKeyEvent(writeMu, conn, sessionID, ev.Key(), ev.Rune(), ev.Modifiers()); err != nil {
			log.Printf("send key failed: %v", err)
		} else if state.effects != nil {
			now := time.Now()
			r := ev.Rune()
			mod := uint16(ev.Modifiers())
			if state.hasFocus {
				state.effects.HandleTrigger(effects.EffectTrigger{
					Type:      effects.TriggerPaneKey,
					PaneID:    state.focus.PaneID,
					Key:       r,
					Modifiers: mod,
					Timestamp: now,
				})
			}
			state.effects.HandleTrigger(effects.EffectTrigger{
				Type:        effects.TriggerWorkspaceKey,
				WorkspaceID: state.workspaceID,
				Key:         r,
				Modifiers:   mod,
				Timestamp:   now,
			})
		}
	case *tcell.EventMouse:
		selectionChanged := state.handleSelectionMouse(ev)
		x, y := ev.Position()
		mouse := protocol.MouseEvent{X: int16(x), Y: int16(y), ButtonMask: uint32(ev.Buttons()), Modifiers: uint16(ev.Modifiers())}
		payload, _ := protocol.EncodeMouseEvent(mouse)
		if err := writeMessage(writeMu, conn, protocol.Header{Version: protocol.Version, Type: protocol.MsgMouseEvent, Flags: protocol.FlagChecksum, SessionID: sessionID}, payload); err != nil {
			log.Printf("send mouse failed: %v", err)
		}
		if selectionChanged {
			render(state, screen)
		}
		if state.selection.consumePendingCopy() {
			if data, mime, ok := state.selectionClipboardData(); ok {
				screen.SetClipboard(data)
				sendClipboardSet(writeMu, conn, sessionID, mime, data)
			}
		}
	case *tcell.EventResize:
		cols, rows := screen.Size()
		state.scheduleResize(writeMu, conn, sessionID, protocol.Resize{Cols: uint16(cols), Rows: uint16(rows)})
		// Trigger render instead of expensive Sync() - tcell handles internal resize sync automatically
		state.triggerRender()
	case *tcell.EventInterrupt:
		// Ignore; used to wake PollEvent for shutdown.
	case *tcell.EventPaste:
		if ev.Start() {
			state.pasting = true
			state.pasteBuf = state.pasteBuf[:0]
		} else {
			state.pasting = false
			if len(state.pasteBuf) > 0 {
				data := append([]byte(nil), state.pasteBuf...)
				if err := sendPaste(writeMu, conn, sessionID, data); err != nil {
					log.Printf("send paste failed: %v", err)
				}
				state.pasteBuf = state.pasteBuf[:0]
			}
		}
	}
	return true
}

func consumePasteKey(state *clientState, ev *tcell.EventKey) {
	var b byte
	switch ev.Key() {
	case tcell.KeyRune:
		r := ev.Rune()
		if r == '\n' {
			state.pasteBuf = append(state.pasteBuf, '\r')
		} else {
			state.pasteBuf = utf8.AppendRune(state.pasteBuf, r)
		}
		return
	case tcell.KeyEnter:
		state.pasteBuf = append(state.pasteBuf, '\r')
		return
	case tcell.KeyCtrlJ:
		// Ctrl+J is newline (0x0A = '\n')
		// In tcell v2.13.x, newlines might be reported as KeyCtrlJ
		state.pasteBuf = append(state.pasteBuf, '\r')
		return
	case tcell.KeyTab:
		state.pasteBuf = append(state.pasteBuf, '\t')
		return
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		state.pasteBuf = append(state.pasteBuf, 0x7F)
		return
	case tcell.KeyEsc:
		state.pasteBuf = append(state.pasteBuf, 0x1b)
		return
	default:
		if ev.Rune() != 0 {
			state.pasteBuf = utf8.AppendRune(state.pasteBuf, ev.Rune())
			return
		}
	}
	b = byte(ev.Rune())
	if b != 0 {
		state.pasteBuf = append(state.pasteBuf, b)
	}
}

func isNetworkClosed(err error) bool {
	if err == os.ErrClosed {
		return true
	}
	ne, ok := err.(net.Error)
	return ok && !ne.Timeout()
}
