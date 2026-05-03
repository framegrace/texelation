// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import "github.com/gdamore/tcell/v2"

// HandleKey routes a tcell key event through the picker's mode-aware
// state machine. Tests call this directly; the run loop wraps real
// EventKey events.
func (p *Picker) HandleKey(key tcell.Key, ch rune, mods tcell.ModMask) {
	if p.mode == modeRename {
		p.handleRenameKey(key, ch)
		return
	}
	if p.mode == modeDeleteConfirm {
		p.handleDeleteConfirmKey(key, ch)
		return
	}
	// Any key dismisses a sticky error banner, even if it doesn't
	// otherwise navigate.
	p.errMsg = ""
	switch key {
	case tcell.KeyUp:
		if p.selectedIdx > 0 {
			p.selectedIdx--
		}
	case tcell.KeyDown:
		if p.selectedIdx < len(p.response.Stored)-1 {
			p.selectedIdx++
		}
	case tcell.KeyEnter:
		if len(p.response.Stored) > 0 {
			id := p.response.Stored[p.selectedIdx].SessionID
			if err := p.client.RecoverSession(id, ""); err != nil {
				// Keep the picker open and surface the error so the
				// user can pick a different session or retry. Don't
				// signal Done — recovery hasn't actually happened.
				p.errMsg = "Recover failed: " + err.Error()
				return
			}
			p.done = true
			p.choice = choiceRecover
		}
		return
	case tcell.KeyTab:
		if p.activeTab == tabStored {
			p.activeTab = tabLive
		} else {
			p.activeTab = tabStored
		}
		return
	case tcell.KeyEsc:
		p.done = true
		p.choice = choiceQuit
		return
	default:
	}
	switch ch {
	case 'j':
		if p.selectedIdx < len(p.response.Stored)-1 {
			p.selectedIdx++
		}
	case 'k':
		if p.selectedIdx > 0 {
			p.selectedIdx--
		}
	case 'n':
		p.client.StartFreshSession()
		p.done = true
		p.choice = choiceFresh
	case 'r':
		if len(p.response.Stored) > 0 {
			p.mode = modeRename
			p.renameBuf = []rune(p.response.Stored[p.selectedIdx].Label)
		}
	case 'd':
		if len(p.response.Stored) > 0 {
			p.mode = modeDeleteConfirm
		}
	case 'q':
		p.done = true
		p.choice = choiceQuit
	}
}

func (p *Picker) handleRenameKey(key tcell.Key, ch rune) {
	switch key {
	case tcell.KeyEsc:
		p.mode = modeBrowse
		p.renameBuf = nil
		return
	case tcell.KeyEnter:
		if len(p.response.Stored) > 0 {
			id := p.response.Stored[p.selectedIdx].SessionID
			newLabel := string(p.renameBuf)
			if err := p.client.RenameSession(id, newLabel); err != nil {
				p.errMsg = "Rename failed: " + err.Error()
				p.mode = modeBrowse
				p.renameBuf = nil
				return
			}
			p.response.Stored[p.selectedIdx].Label = newLabel
		}
		p.mode = modeBrowse
		p.renameBuf = nil
		return
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(p.renameBuf) > 0 {
			p.renameBuf = p.renameBuf[:len(p.renameBuf)-1]
		}
		return
	}
	if ch != 0 {
		p.renameBuf = append(p.renameBuf, ch)
	}
}

func (p *Picker) handleDeleteConfirmKey(key tcell.Key, ch rune) {
	switch ch {
	case 'y', 'Y':
		if len(p.response.Stored) > 0 {
			id := p.response.Stored[p.selectedIdx].SessionID
			if err := p.client.DeleteSession(id); err != nil {
				p.errMsg = "Delete failed: " + err.Error()
			} else {
				// Drop the entry locally so the cursor position
				// stays meaningful even if we don't refresh.
				p.response.Stored = append(p.response.Stored[:p.selectedIdx], p.response.Stored[p.selectedIdx+1:]...)
				if p.selectedIdx >= len(p.response.Stored) && p.selectedIdx > 0 {
					p.selectedIdx--
				}
			}
		}
		p.mode = modeBrowse
	case 'n', 'N':
		p.mode = modeBrowse
	}
	if key == tcell.KeyEsc {
		p.mode = modeBrowse
	}
}
