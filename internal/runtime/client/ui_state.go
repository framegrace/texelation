// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/ui_state.go
// Summary: UI state management and theme application for client runtime.
// Usage: Manages client-side state including theme, focus, clipboard, and effects configuration.

package clientruntime

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/internal/effects"
	"texelation/protocol"
)

type uiState struct {
	cache         *client.BufferCache
	clipboard     protocol.ClipboardData
	hasClipboard  bool
	theme         protocol.ThemeAck
	hasTheme      bool
	focus         protocol.PaneFocus
	hasFocus      bool
	themeValues   map[string]map[string]interface{}
	defaultStyle  tcell.Style
	defaultFg     tcell.Color
	defaultBg     tcell.Color
	workspaces    []int
	workspaceID   int
	activeTitle   string
	controlMode   bool
	subMode       rune
	desktopBg     tcell.Color
	zoomed        bool
	zoomedPane    [16]byte
	pasting       bool
	pasteBuf      []byte
	renderCh      chan<- struct{}
	effects       *effects.Manager
	resizeMu      sync.Mutex
	pendingResize protocol.Resize
	resizeSeq     uint64
}

func (s *uiState) setRenderChannel(ch chan<- struct{}) {
	s.renderCh = ch
	if s.effects != nil {
		s.effects.AttachRenderChannel(ch)
	}
}

func (s *uiState) setThemeValue(section, key string, value interface{}) {
	if s.themeValues == nil {
		s.themeValues = make(map[string]map[string]interface{})
	}
	sec := s.themeValues[section]
	if sec == nil {
		sec = make(map[string]interface{})
		s.themeValues[section] = sec
	}
	sec[key] = value
}

func (s *uiState) scheduleResize(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, resize protocol.Resize) {
	if s == nil {
		return
	}
	s.resizeMu.Lock()
	s.pendingResize = resize
	s.resizeSeq++
	seq := s.resizeSeq
	s.resizeMu.Unlock()

	go func() {
		time.Sleep(resizeDebounce)
		s.resizeMu.Lock()
		if seq != s.resizeSeq {
			s.resizeMu.Unlock()
			return
		}
		res := s.pendingResize
		s.resizeMu.Unlock()
		sendResizeMessage(writeMu, conn, sessionID, res)
	}()
}

func (s *uiState) applyEffectConfig() {
	var rawBindings interface{}
	if section, ok := s.themeValues["effects"]; ok {
		rawBindings = section["bindings"]
	}
	bindings, err := effects.ParseBindings(rawBindings)
	if err != nil {
		log.Printf("effect bindings parse failed: %v", err)
	}
	if len(bindings) == 0 {
		bindings = effects.DefaultBindings()
	}

	manager := effects.NewManager()
	for _, binding := range bindings {
		eff, err := effects.CreateEffect(binding.Effect, binding.Config)
		if err != nil {
			log.Printf("effect %s creation failed: %v", binding.Effect, err)
			continue
		}
		manager.RegisterBinding(effects.Binding{Effect: eff, Target: binding.Target, Event: binding.Event})
	}
	if s.renderCh != nil {
		manager.AttachRenderChannel(s.renderCh)
	}
	s.effects = manager
	if s.cache != nil {
		s.effects.ResetPaneStates(s.cache.SortedPanes())
	}
	s.effects.HandleTrigger(effects.EffectTrigger{
		Type:      effects.TriggerWorkspaceControl,
		Active:    s.controlMode,
		Timestamp: time.Now(),
	})
}

func (s *uiState) updateTheme(section, key, value string) {
	if section == "" || key == "" {
		return
	}
	var stored interface{} = value
	if section == "effects" && key == "bindings" {
		var decoded interface{}
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			stored = decoded
		}
	}
	s.setThemeValue(section, key, stored)
	if section == "desktop" {
		switch key {
		case "default_fg":
			if fg, ok := parseHexColor(value); ok {
				s.defaultFg = fg
			}
		case "default_bg":
			if bg, ok := parseHexColor(value); ok {
				s.defaultBg = bg
				s.desktopBg = bg
			}
		}
	}
	s.recomputeDefaultStyle()
	s.applyEffectConfig()
}

func (s *uiState) recomputeDefaultStyle() {
	style := tcell.StyleDefault
	if s.defaultFg != tcell.ColorDefault {
		style = style.Foreground(s.defaultFg)
	}
	if s.defaultBg != tcell.ColorDefault {
		style = style.Background(s.defaultBg)
	}
	s.defaultStyle = style
}

func (s *uiState) applyStateUpdate(update protocol.StateUpdate) {
	s.workspaceID = int(update.WorkspaceID)
	if cap(s.workspaces) < len(update.AllWorkspaces) {
		s.workspaces = make([]int, 0, len(update.AllWorkspaces))
	} else {
		s.workspaces = s.workspaces[:0]
	}
	for _, id := range update.AllWorkspaces {
		s.workspaces = append(s.workspaces, int(id))
	}
	prevControl := s.controlMode
	s.controlMode = update.InControlMode
	s.subMode = update.SubMode
	s.activeTitle = update.ActiveTitle
	bg := colorFromRGB(update.DesktopBgRGB)
	if bg != tcell.ColorDefault {
		s.desktopBg = bg
		s.defaultBg = bg
	}
	s.zoomed = update.Zoomed
	if update.Zoomed {
		s.zoomedPane = update.ZoomedPaneID
	} else {
		s.zoomedPane = [16]byte{}
	}
	s.recomputeDefaultStyle()
	if s.effects != nil && prevControl != s.controlMode {
		s.effects.HandleTrigger(effects.EffectTrigger{
			Type:      effects.TriggerWorkspaceControl,
			Active:    s.controlMode,
			Timestamp: time.Now(),
		})
	}
}
