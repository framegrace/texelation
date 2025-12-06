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
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/internal/effects"
	"texelation/protocol"
)

type clientState struct {
	cache                *client.BufferCache
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
	layoutTransition     *LayoutTransitionAnimator
	resizeMu             sync.Mutex
	pendingResize        protocol.Resize
	resizeSeq            uint64
	selection            selectionState
}

func (s *clientState) setRenderChannel(ch chan<- struct{}) {
	s.renderCh = ch
	if s.effects != nil {
		s.effects.AttachRenderChannel(ch)
	}
	if s.layoutTransition != nil {
		s.layoutTransition.AttachRenderSignal(ch)
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

func (s *clientState) applyLayoutTransitionConfig() {
	cfg := DefaultLayoutTransitionConfig()

	// Parse configuration from theme
	if section, ok := s.themeValues["layout_transitions"]; ok {
		if enabled, ok := section["enabled"].(bool); ok {
			cfg.Enabled = enabled
		}
		if durationMs, ok := section["duration_ms"].(float64); ok {
			cfg.Duration = time.Duration(durationMs) * time.Millisecond
		}
		if easing, ok := section["easing"].(string); ok {
			cfg.EasingFunc = easing
		}
		if threshold, ok := section["min_threshold"].(float64); ok {
			cfg.MinThreshold = int(threshold)
		}
	}

	// Create or update animator
	if s.layoutTransition == nil {
		s.layoutTransition = NewLayoutTransitionAnimator()
		if s.renderCh != nil {
			s.layoutTransition.AttachRenderSignal(s.renderCh)
		}
	}
	s.layoutTransition.SetConfig(cfg)
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
	s.applyLayoutTransitionConfig()
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
			Type:      effects.TriggerWorkspaceControl,
			Active:    s.controlMode,
			Timestamp: time.Now(),
		})
	}
}

func (s *clientState) handleSelectionMouse(ev *tcell.EventMouse) bool {
	if s == nil || ev == nil || s.cache == nil {
		return false
	}
	buttons := ev.Buttons()

	// Ignore wheel events for selection state to prevent false releases
	// (wheel events often don't report held buttons correctly)
	if buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0 {
		return false
	}

	x, y := ev.Position()
	sel := &s.selection
	changed := false

	startPressed := buttons&tcell.Button1 != 0 && sel.lastButtons&tcell.Button1 == 0
	stillPressed := buttons&tcell.Button1 != 0 && sel.dragging
	released := buttons&tcell.Button1 == 0 && sel.lastButtons&tcell.Button1 != 0

	if startPressed {
		pane := s.cache.PaneAt(x, y)
		if pane == nil {
			changed = sel.clear() || changed
		} else if pane.HandlesSelection {
			changed = sel.clear() || changed
		} else {
			changed = sel.begin(pane, x, y) || changed
		}
	} else if stillPressed {
		pane := s.cache.PaneByID(sel.paneID)
		changed = sel.updateCurrent(pane, x, y) || changed
	} else if released {
		pane := s.cache.PaneByID(sel.paneID)
		changed = sel.finish(pane, x, y) || changed
		if pane == nil || !pane.HandlesSelection {
			changed = sel.clear() || changed
		}
	}

	sel.lastButtons = buttons
	return changed
}

func (s *clientState) clearSelection() bool {
	if s == nil {
		return false
	}
	return s.selection.clear()
}

func (s *clientState) selectionBounds() (pane *client.PaneState, minX, maxX, minY, maxY int, ok bool) {
	if s == nil {
		return nil, 0, 0, 0, 0, false
	}
	sel := &s.selection
	if !sel.isVisible() {
		return nil, 0, 0, 0, 0, false
	}
	pane = s.cache.PaneByID(sel.paneID)
	if pane == nil {
		return nil, 0, 0, 0, 0, false
	}
	minX, maxX, minY, maxY, okBounds := sel.bounds()
	if !okBounds {
		return nil, 0, 0, 0, 0, false
	}
	return pane, minX, maxX, minY, maxY, true
}

func (s *clientState) selectionClipboardData() ([]byte, string, bool) {
	pane, minX, maxX, minY, maxY, ok := s.selectionBounds()
	if !ok || pane == nil {
		return nil, "", false
	}
	if minX >= maxX || minY >= maxY {
		return nil, "", false
	}
	lines := make([]string, 0, maxY-minY)
	for y := minY; y < maxY; y++ {
		localY := y - pane.Rect.Y
		if localY < 0 || localY >= pane.Rect.Height {
			continue
		}
		rowCells := pane.RowCells(localY)
		start := minX - pane.Rect.X
		end := maxX - pane.Rect.X
		if start < 0 {
			start = 0
		}
		if end > pane.Rect.Width {
			end = pane.Rect.Width
		}
		if end <= start {
			continue
		}
		width := end - start
		runes := make([]rune, width)
		for i := 0; i < width; i++ {
			ch := ' '
			idx := start + i
			if rowCells != nil && idx < len(rowCells) {
				if rowCells[idx].Ch != 0 {
					ch = rowCells[idx].Ch
				}
			}
			runes[i] = ch
		}
		lines = append(lines, string(runes))
	}
	if len(lines) == 0 {
		return nil, "", false
	}
	text := strings.Join(lines, "\n")
	return []byte(text), "text/plain", true
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

type selectionRect struct {
	x, y, width, height int
}

func (r selectionRect) clamp(x, y int) (int, int, bool) {
	if r.width <= 0 || r.height <= 0 {
		return 0, 0, false
	}
	maxX := r.x + r.width - 1
	maxY := r.y + r.height - 1
	if x < r.x {
		x = r.x
	} else if x > maxX {
		x = maxX
	}
	if y < r.y {
		y = r.y
	} else if y > maxY {
		y = maxY
	}
	return x, y, true
}

type selectionState struct {
	active      bool
	dragging    bool
	moved       bool
	hasPane     bool
	paneID      [16]byte
	paneRect    selectionRect
	anchorX     int
	anchorY     int
	currentX    int
	currentY    int
	lastButtons tcell.ButtonMask
	pendingCopy bool
}

func (s *selectionState) clear() bool {
	changed := s.active || s.dragging || s.hasPane || s.pendingCopy
	s.active = false
	s.dragging = false
	s.moved = false
	s.hasPane = false
	s.pendingCopy = false
	s.paneID = [16]byte{}
	s.paneRect = selectionRect{}
	return changed
}

func (s *selectionState) begin(pane *client.PaneState, x, y int) bool {
	if pane == nil {
		return s.clear()
	}
	s.dragging = true
	s.moved = false
	s.pendingCopy = false
	s.active = false
	s.hasPane = true
	s.paneID = pane.ID
	s.paneRect = selectionRect{
		x:      pane.Rect.X,
		y:      pane.Rect.Y,
		width:  pane.Rect.Width,
		height: pane.Rect.Height,
	}
	x, y, ok := s.paneRect.clamp(x, y)
	if !ok {
		return s.clear()
	}
	s.anchorX = x
	s.anchorY = y
	s.currentX = x
	s.currentY = y
	return true
}

func (s *selectionState) updateCurrent(pane *client.PaneState, x, y int) bool {
	if !s.dragging || !s.hasPane {
		return false
	}
	if pane != nil {
		s.paneRect = selectionRect{
			x:      pane.Rect.X,
			y:      pane.Rect.Y,
			width:  pane.Rect.Width,
			height: pane.Rect.Height,
		}
	}
	x, y, ok := s.paneRect.clamp(x, y)
	if !ok {
		return false
	}
	if x == s.currentX && y == s.currentY {
		return false
	}
	s.currentX = x
	s.currentY = y
	if x != s.anchorX || y != s.anchorY {
		s.moved = true
	}
	return true
}

func (s *selectionState) finish(pane *client.PaneState, x, y int) bool {
	if !s.dragging {
		return false
	}
	s.dragging = false
	if pane != nil {
		s.paneRect = selectionRect{
			x:      pane.Rect.X,
			y:      pane.Rect.Y,
			width:  pane.Rect.Width,
			height: pane.Rect.Height,
		}
	}
	x, y, ok := s.paneRect.clamp(x, y)
	if ok {
		s.currentX = x
		s.currentY = y
	}
	if s.moved {
		s.active = true
		s.pendingCopy = true
	} else {
		s.active = false
		s.pendingCopy = false
		s.hasPane = false
	}
	return true
}

func (s *selectionState) isVisible() bool {
	return (s.active || s.dragging) && s.hasPane
}

func (s *selectionState) bounds() (minX, maxX, minY, maxY int, ok bool) {
	if !s.isVisible() {
		return 0, 0, 0, 0, false
	}
	x0, x1 := s.anchorX, s.currentX
	y0, y1 := s.anchorY, s.currentY
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	// Expand to make the range inclusive of the end cell.
	return x0, x1 + 1, y0, y1 + 1, true
}

func (s *selectionState) consumePendingCopy() bool {
	if !s.pendingCopy {
		return false
	}
	s.pendingCopy = false
	return true
}

func colorToHex(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return ""
	}
	r, g, b := c.RGB()
	return fmt.Sprintf("#%02X%02X%02X", r&0xFF, g&0xFF, b&0xFF)
}
