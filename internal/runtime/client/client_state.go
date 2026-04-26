// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/client_state.go
// Summary: UI state management and theme application for client runtime.
// Usage: Manages client-side state including theme, focus, clipboard, and effects configuration.

package clientruntime

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/client"
	"github.com/framegrace/texelation/config"
	"github.com/framegrace/texelation/internal/effects"
	"github.com/framegrace/texelation/internal/keybind"
	"github.com/framegrace/texelation/protocol"
)

// paneCacheFor returns the PaneCache for id, creating it on first access.
func (s *clientState) paneCacheFor(id [16]byte) *client.PaneCache {
	s.paneCachesMu.Lock()
	defer s.paneCachesMu.Unlock()
	if s.paneCaches == nil {
		s.paneCaches = make(map[[16]byte]*client.PaneCache)
	}
	pc, ok := s.paneCaches[id]
	if !ok {
		pc = client.NewPaneCache()
		s.paneCaches[id] = pc
	}
	return pc
}

// dropPaneCache removes the PaneCache for id, if present.
func (s *clientState) dropPaneCache(id [16]byte) {
	s.paneCachesMu.Lock()
	defer s.paneCachesMu.Unlock()
	delete(s.paneCaches, id)
}

type clientState struct {
	cache        *client.BufferCache
	paneCaches   map[[16]byte]*client.PaneCache
	paneCachesMu sync.RWMutex
	viewports    *viewportTrackers

	// Wire for FlushFrame — set once after connect, never mutated thereafter.
	conn      net.Conn
	writeMu   *sync.Mutex
	sessionID [16]byte

	clipboardMu          sync.Mutex
	clipboard            protocol.ClipboardData
	hasClipboard         bool
	clipboardSyncPending bool
	theme                protocol.ThemeAck
	hasTheme             bool
	focus                protocol.PaneFocus
	hasFocus             bool
	themeValues          map[string]map[string]interface{}
	defaultStyle         tcell.Style
	defaultFg            tcell.Color
	defaultBg            tcell.Color
	selectionFg          tcell.Color
	selectionBg          tcell.Color
	workspaces           []int
	workspaceID          int
	activeTitle          string
	controlMode          bool
	subMode              rune
	desktopBg            tcell.Color
	zoomed               bool
	zoomedPane           [16]byte
	pasting              bool
	pasteBuf             []byte
	renderCh             chan<- struct{}
	effects              *effects.Manager
	resizeMu             sync.Mutex
	pendingResize        protocol.Resize
	resizeSeq            uint64
	selection            selectionState
	idleWatcher          *effects.IdleWatcher

	// Double-buffered rendering
	prevBuffer       [][]client.Cell
	renderBuffer     [][]client.Cell
	fullRenderNeeded bool

	// Pooled pane buffer for compositeInto (avoids per-frame allocations)
	paneBuffer [][]client.Cell

	// Fixed-timestep animation state
	tickAccum      float64 // accumulated animation time in seconds (high precision)
	frameDT        float32 // delta time for current frame (0 for data-driven renders)
	dynAnimating   bool    // true when dynamic cells need continuous rendering
	animFrameCount uint64  // tick counter for frame skipping

	// Restart notification state
	showRestartNotification      bool
	restartNotificationDismissed bool

	// Kitty graphics output (nil when terminal doesn't support it)
	kitty     *kittyOutput
	ttyWriter io.Writer

	keybindings *keybind.Registry

	// persistSnapshot, if non-nil, is invoked once per flushFrame
	// iteration to schedule a debounced persist. Set by app.go when
	// Plan D persistence is active. Plan D / issue #199.
	persistSnapshot func()
}

func (s *clientState) setRenderChannel(ch chan<- struct{}) {
	s.renderCh = ch
	if s.effects != nil {
		s.effects.SetWakeChannel(ch)
	}
}

// triggerRender requests a render on the render channel (non-blocking).
func (s *clientState) triggerRender() {
	if s.renderCh != nil {
		select {
		case s.renderCh <- struct{}{}:
		default:
		}
	}
}

func (s *clientState) setThemeValue(section, key string, value interface{}) {
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

func (s *clientState) scheduleResize(writeMu *sync.Mutex, conn net.Conn, sessionID [16]byte, resize protocol.Resize) {
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

func (s *clientState) applyEffectConfig() {
	var rawBindings interface{}
	if cfg := config.System(); cfg != nil {
		if section := cfg.Section("effects"); section != nil {
			rawBindings = section["bindings"]
		}
	}
	if rawBindings == nil {
		if section, ok := s.themeValues["effects"]; ok {
			rawBindings = section["bindings"]
		}
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
		cfg := make(effects.EffectConfig)
		for k, v := range binding.Config {
			cfg[k] = v
		}
		if s.defaultFg != tcell.ColorDefault {
			cfg["default_fg"] = colorToHex(s.defaultFg)
		}
		if s.defaultBg != tcell.ColorDefault {
			cfg["default_bg"] = colorToHex(s.defaultBg)
		}

		eff, err := effects.CreateEffect(binding.Effect, cfg)
		if err != nil {
			log.Printf("effect %s creation failed: %v", binding.Effect, err)
			continue
		}
		manager.RegisterBinding(effects.Binding{Effect: eff, Target: binding.Target, Event: binding.Event})
	}
	if s.renderCh != nil {
		manager.SetWakeChannel(s.renderCh)
	}
	s.effects = manager

	// Stop previous idle watcher if any.
	if s.idleWatcher != nil {
		s.idleWatcher.Stop()
		s.idleWatcher = nil
	}

	// Set up screensaver idle watcher from system config.
	var screensaverSection map[string]interface{}
	if cfg := config.System(); cfg != nil {
		if section := cfg.Section("screensaver"); section != nil {
			screensaverSection = section
		}
	}
	ssCfg := effects.ParseScreensaverConfig(screensaverSection)
	if ssCfg.Enabled {
		var ssWrapper effects.Effect
		if ssCfg.EffectID == "random" {
			ssWrapper = effects.NewScreensaverFadeRandom(effects.ScreensaverEffectIDs(), ssCfg.FadeStyle)
		} else if ssEff, err := effects.CreateEffect(ssCfg.EffectID, nil); err == nil {
			ssWrapper = effects.NewScreensaverFade(ssEff, ssCfg.FadeStyle)
		}
		if ssWrapper != nil {
			manager.RegisterBinding(effects.Binding{
				Effect: ssWrapper,
				Target: effects.TargetWorkspace,
				Event:  effects.TriggerScreensaver,
			})
		}
		mgr := s.effects
		fadeIn := ssCfg.FadeIn
		fadeOut := ssCfg.FadeOut
		s.idleWatcher = effects.NewIdleWatcher(effects.IdleWatcherConfig{
			Timeout:     ssCfg.Timeout,
			EffectID:    ssCfg.EffectID,
			LockEnabled: ssCfg.LockEnabled,
			LockTimeout: ssCfg.LockTimeout,
			OnActivate: func() {
				mgr.HandleTrigger(effects.EffectTrigger{
					Type:    effects.TriggerScreensaver,
					Active:  true,
					FadeIn:  fadeIn,
					FadeOut: fadeOut,
				})
			},
			OnDeactivate: func() {
				mgr.HandleTrigger(effects.EffectTrigger{
					Type:    effects.TriggerScreensaver,
					Active:  false,
					FadeIn:  fadeIn,
					FadeOut: fadeOut,
				})
			},
		})
	}
	if s.cache != nil {
		s.effects.ResetPaneStates(s.cache.SortedPanes())
	}
	s.effects.HandleTrigger(effects.EffectTrigger{
		Type:   effects.TriggerWorkspaceControl,
		Active: s.controlMode,
	})
}

func (s *clientState) updateTheme(section, key, value string) {
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
	} else if section == "selection" {
		switch key {
		case "highlight_fg":
			if fg, ok := parseHexColor(value); ok {
				s.selectionFg = fg
			}
		case "highlight_bg":
			if bg, ok := parseHexColor(value); ok {
				s.selectionBg = bg
			}
		}
	}
	s.recomputeDefaultStyle()
	s.applyEffectConfig()
}

func (s *clientState) recomputeDefaultStyle() {
	style := tcell.StyleDefault
	if s.defaultFg != tcell.ColorDefault {
		style = style.Foreground(s.defaultFg)
	}
	if s.defaultBg != tcell.ColorDefault {
		style = style.Background(s.defaultBg)
	}
	s.defaultStyle = style
}

func (s *clientState) applyStateUpdate(update protocol.StateUpdate) {
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
			Type:   effects.TriggerWorkspaceControl,
			Active: s.controlMode,
		})
	}
}

func (s *clientState) setClipboard(data protocol.ClipboardData) {
	if s == nil {
		return
	}
	s.clipboardMu.Lock()
	s.clipboard = data
	s.hasClipboard = true
	s.clipboardSyncPending = true
	s.clipboardMu.Unlock()
}

func (s *clientState) consumeClipboardSync() (protocol.ClipboardData, bool) {
	if s == nil {
		return protocol.ClipboardData{}, false
	}
	s.clipboardMu.Lock()
	defer s.clipboardMu.Unlock()
	if !s.clipboardSyncPending {
		return protocol.ClipboardData{}, false
	}
	s.clipboardSyncPending = false
	return s.clipboard, true
}

func colorToHex(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return ""
	}
	r, g, b := c.RGB()
	return fmt.Sprintf("#%02X%02X%02X", r&0xFF, g&0xFF, b&0xFF)
}
