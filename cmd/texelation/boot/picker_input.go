// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package boot

import "github.com/gdamore/tcell/v2"

// activeListLen returns the entry count of whichever tab is currently
// active. Used to clamp navigation indices.
func (p *Picker) activeListLen() int {
	if p.activeTab == tabLive {
		return len(p.response.Live)
	}
	return len(p.response.Stored)
}

// activeSelectedID returns the session ID at p.selectedIdx in the
// currently-active tab. Returns ([16]byte{}, false) if the active list
// is empty.
func (p *Picker) activeSelectedID() ([16]byte, bool) {
	if p.activeTab == tabLive {
		if p.selectedIdx >= len(p.response.Live) {
			return [16]byte{}, false
		}
		return p.response.Live[p.selectedIdx].SessionID, true
	}
	if p.selectedIdx >= len(p.response.Stored) {
		return [16]byte{}, false
	}
	return p.response.Stored[p.selectedIdx].SessionID, true
}

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
		if p.selectedIdx < p.activeListLen()-1 {
			p.selectedIdx++
		}
	case tcell.KeyEnter:
		if id, ok := p.activeSelectedID(); ok {
			if err := p.client.RecoverSession(id, ""); err != nil {
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
		// Clamp selection so the new tab's smaller list doesn't
		// leave selectedIdx pointing past the end.
		if p.selectedIdx >= p.activeListLen() {
			p.selectedIdx = 0
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
		if p.selectedIdx < p.activeListLen()-1 {
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
		// Rename only operates on stored sessions. Live sessions
		// don't have a sensible rename target in F.1.
		if p.activeTab == tabStored && len(p.response.Stored) > 0 {
			p.mode = modeRename
			p.renameBuf = []rune(p.response.Stored[p.selectedIdx].Label)
		}
	case 'd':
		// Delete only operates on stored sessions; the server refuses
		// to delete a live session anyway.
		if p.activeTab == tabStored && len(p.response.Stored) > 0 {
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
