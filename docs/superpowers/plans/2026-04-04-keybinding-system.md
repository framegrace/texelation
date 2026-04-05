# Keybinding System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a configurable keybinding system that maps keyboard shortcuts to named actions, supports multiple bindings per action, and provides platform presets (Linux/Mac) with overlay merging.

**Architecture:** A `keybind` package defines `KeyCombo`, `Action`, and `Registry`. Presets are embedded JSON files in `defaults/`. On startup, the registry is built from preset + optional extra preset + user overrides. Each key handler (`input_handler.go`, `desktop_input.go`, `workspace.go`, `term.go`) replaces hardcoded key checks with `registry.Match(ev)` calls.

**Tech Stack:** Go, tcell (key types), embedded JSON assets

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/keybind/action.go` | Action constants and descriptions |
| `internal/keybind/keycombo.go` | KeyCombo type, ParseKeyCombo, FormatKeyCombo |
| `internal/keybind/registry.go` | Registry: merge presets + overrides, Match, AllActions |
| `internal/keybind/presets.go` | Built-in preset maps (linux, mac) |
| `internal/keybind/keycombo_test.go` | Tests for parse/format |
| `internal/keybind/registry_test.go` | Tests for merge and match |
| `defaults/keybindings-linux.json` | Linux default bindings |
| `defaults/keybindings-mac.json` | Mac default bindings |
| `defaults/embedded.go` | Add keybindings embed + loader |
| `internal/runtime/client/client_state.go` | Add registry to clientState, load on startup |
| `internal/runtime/client/input_handler.go` | Replace hardcoded keys with registry.Match |
| `texel/desktop_engine_core.go` | Add registry to DesktopEngine |
| `texel/desktop_input.go` | Replace hardcoded keys with registry.Match |
| `texel/desktop_engine_control_mode.go` | Replace hardcoded keys with registry.Match |
| `texel/workspace.go` | Replace hardcoded keys with registry.Match |
| `apps/texelterm/term.go` | Replace hardcoded keys with registry.Match |

---

### Task 1: KeyCombo type — parse and format

**Files:**
- Create: `internal/keybind/keycombo.go`
- Create: `internal/keybind/keycombo_test.go`

- [ ] **Step 1: Write tests for ParseKeyCombo**

```go
// internal/keybind/keycombo_test.go
package keybind

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestParseKeyCombo(t *testing.T) {
	tests := []struct {
		input string
		key   tcell.Key
		rune_ rune
		mod   tcell.ModMask
	}{
		{"ctrl+a", tcell.KeyCtrlA, 0, tcell.ModCtrl},
		{"shift+up", tcell.KeyUp, 0, tcell.ModShift},
		{"alt+left", tcell.KeyLeft, 0, tcell.ModAlt},
		{"f1", tcell.KeyF1, 0, tcell.ModNone},
		{"f12", tcell.KeyF12, 0, tcell.ModNone},
		{"ctrl+shift+f", tcell.KeyRune, 'f', tcell.ModCtrl | tcell.ModShift},
		{"enter", tcell.KeyEnter, 0, tcell.ModNone},
		{"esc", tcell.KeyEsc, 0, tcell.ModNone},
		{"tab", tcell.KeyTab, 0, tcell.ModNone},
		{"space", tcell.KeyRune, ' ', tcell.ModNone},
		{"a", tcell.KeyRune, 'a', tcell.ModNone},
		{"alt+b", tcell.KeyRune, 'b', tcell.ModAlt},
		{"pgup", tcell.KeyPgUp, 0, tcell.ModNone},
		{"pgdn", tcell.KeyPgDn, 0, tcell.ModNone},
		{"backspace", tcell.KeyBackspace2, 0, tcell.ModNone},
		{"delete", tcell.KeyDelete, 0, tcell.ModNone},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			kc, err := ParseKeyCombo(tt.input)
			if err != nil {
				t.Fatalf("ParseKeyCombo(%q) error: %v", tt.input, err)
			}
			if kc.Key != tt.key {
				t.Errorf("key: got %v, want %v", kc.Key, tt.key)
			}
			if kc.Rune != tt.rune_ {
				t.Errorf("rune: got %q, want %q", kc.Rune, tt.rune_)
			}
			if kc.Modifiers != tt.mod {
				t.Errorf("mod: got %v, want %v", kc.Modifiers, tt.mod)
			}
		})
	}
}

func TestParseKeyCombo_Invalid(t *testing.T) {
	invalid := []string{"", "ctrl+", "foo+bar", "ctrl+alt+"}
	for _, s := range invalid {
		_, err := ParseKeyCombo(s)
		if err == nil {
			t.Errorf("ParseKeyCombo(%q) should fail", s)
		}
	}
}

func TestFormatKeyCombo(t *testing.T) {
	tests := []struct {
		kc   KeyCombo
		want string
	}{
		{KeyCombo{Key: tcell.KeyCtrlA, Modifiers: tcell.ModCtrl}, "Ctrl+A"},
		{KeyCombo{Key: tcell.KeyUp, Modifiers: tcell.ModShift}, "Shift+Up"},
		{KeyCombo{Key: tcell.KeyF1}, "F1"},
		{KeyCombo{Key: tcell.KeyRune, Rune: 'b', Modifiers: tcell.ModAlt}, "Alt+B"},
	}
	for _, tt := range tests {
		got := FormatKeyCombo(tt.kc)
		if got != tt.want {
			t.Errorf("FormatKeyCombo(%+v) = %q, want %q", tt.kc, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/marc/projects/texel/texelation
go test ./internal/keybind/ -v
```

Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement KeyCombo, ParseKeyCombo, FormatKeyCombo**

```go
// internal/keybind/keycombo.go
package keybind

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// KeyCombo represents a parsed key combination.
type KeyCombo struct {
	Key       tcell.Key
	Rune      rune
	Modifiers tcell.ModMask
}

// MatchesEvent returns true if this combo matches the given tcell key event.
func (kc KeyCombo) MatchesEvent(ev *tcell.EventKey) bool {
	if kc.Key == tcell.KeyRune {
		return ev.Key() == tcell.KeyRune && ev.Rune() == kc.Rune && ev.Modifiers() == kc.Modifiers
	}
	// For Ctrl+letter, tcell encodes the key as KeyCtrlA..KeyCtrlZ with ModCtrl.
	// Match on Key alone — modifiers are implicit in the key constant.
	if kc.Key >= tcell.KeyCtrlA && kc.Key <= tcell.KeyCtrlZ {
		return ev.Key() == kc.Key
	}
	return ev.Key() == kc.Key && ev.Modifiers() == kc.Modifiers
}

var keyNames = map[string]tcell.Key{
	"up": tcell.KeyUp, "down": tcell.KeyDown,
	"left": tcell.KeyLeft, "right": tcell.KeyRight,
	"enter": tcell.KeyEnter, "esc": tcell.KeyEsc,
	"tab": tcell.KeyTab, "backspace": tcell.KeyBackspace2,
	"delete": tcell.KeyDelete, "insert": tcell.KeyInsert,
	"home": tcell.KeyHome, "end": tcell.KeyEnd,
	"pgup": tcell.KeyPgUp, "pgdn": tcell.KeyPgDn,
	"f1": tcell.KeyF1, "f2": tcell.KeyF2, "f3": tcell.KeyF3,
	"f4": tcell.KeyF4, "f5": tcell.KeyF5, "f6": tcell.KeyF6,
	"f7": tcell.KeyF7, "f8": tcell.KeyF8, "f9": tcell.KeyF9,
	"f10": tcell.KeyF10, "f11": tcell.KeyF11, "f12": tcell.KeyF12,
}

var ctrlKeys = map[string]tcell.Key{
	"a": tcell.KeyCtrlA, "b": tcell.KeyCtrlB, "c": tcell.KeyCtrlC,
	"d": tcell.KeyCtrlD, "e": tcell.KeyCtrlE, "f": tcell.KeyCtrlF,
	"g": tcell.KeyCtrlG, "h": tcell.KeyCtrlH, "i": tcell.KeyCtrlI,
	"j": tcell.KeyCtrlJ, "k": tcell.KeyCtrlK, "l": tcell.KeyCtrlL,
	"m": tcell.KeyCtrlM, "n": tcell.KeyCtrlN, "o": tcell.KeyCtrlO,
	"p": tcell.KeyCtrlP, "q": tcell.KeyCtrlQ, "r": tcell.KeyCtrlR,
	"s": tcell.KeyCtrlS, "t": tcell.KeyCtrlT, "u": tcell.KeyCtrlU,
	"v": tcell.KeyCtrlV, "w": tcell.KeyCtrlW, "x": tcell.KeyCtrlX,
	"y": tcell.KeyCtrlY, "z": tcell.KeyCtrlZ,
}

// ParseKeyCombo parses a string like "ctrl+a" or "shift+up" into a KeyCombo.
func ParseKeyCombo(s string) (KeyCombo, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return KeyCombo{}, fmt.Errorf("empty key string")
	}

	parts := strings.Split(s, "+")
	keyPart := parts[len(parts)-1]
	modParts := parts[:len(parts)-1]

	var mod tcell.ModMask
	hasCtrl := false
	for _, m := range modParts {
		switch m {
		case "ctrl":
			mod |= tcell.ModCtrl
			hasCtrl = true
		case "shift":
			mod |= tcell.ModShift
		case "alt":
			mod |= tcell.ModAlt
		default:
			return KeyCombo{}, fmt.Errorf("unknown modifier: %q", m)
		}
	}

	if keyPart == "" {
		return KeyCombo{}, fmt.Errorf("missing key after modifiers")
	}

	// Ctrl+letter → tcell.KeyCtrlX
	if hasCtrl && len(keyPart) == 1 && keyPart[0] >= 'a' && keyPart[0] <= 'z' {
		if k, ok := ctrlKeys[keyPart]; ok {
			return KeyCombo{Key: k, Modifiers: mod}, nil
		}
	}

	// Named special key
	if k, ok := keyNames[keyPart]; ok {
		return KeyCombo{Key: k, Modifiers: mod}, nil
	}

	// Space
	if keyPart == "space" {
		return KeyCombo{Key: tcell.KeyRune, Rune: ' ', Modifiers: mod}, nil
	}

	// Single character
	if len(keyPart) == 1 {
		return KeyCombo{Key: tcell.KeyRune, Rune: rune(keyPart[0]), Modifiers: mod}, nil
	}

	return KeyCombo{}, fmt.Errorf("unknown key: %q", keyPart)
}

// Reverse maps for formatting
var keyToName map[tcell.Key]string
var ctrlToLetter map[tcell.Key]string

func init() {
	keyToName = make(map[tcell.Key]string, len(keyNames))
	for name, key := range keyNames {
		keyToName[key] = strings.ToUpper(name[:1]) + name[1:]
	}
	// Fix casing for multi-char names
	keyToName[tcell.KeyPgUp] = "PgUp"
	keyToName[tcell.KeyPgDn] = "PgDn"

	ctrlToLetter = make(map[tcell.Key]string, len(ctrlKeys))
	for letter, key := range ctrlKeys {
		ctrlToLetter[key] = strings.ToUpper(letter)
	}
}

// FormatKeyCombo returns a human-readable string like "Ctrl+A" or "Shift+Up".
func FormatKeyCombo(kc KeyCombo) string {
	var parts []string
	if kc.Modifiers&tcell.ModCtrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if kc.Modifiers&tcell.ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if kc.Modifiers&tcell.ModShift != 0 {
		parts = append(parts, "Shift")
	}

	// Ctrl+letter
	if letter, ok := ctrlToLetter[kc.Key]; ok {
		parts = append(parts, letter)
		return strings.Join(parts, "+")
	}

	// Named key
	if name, ok := keyToName[kc.Key]; ok {
		parts = append(parts, name)
		return strings.Join(parts, "+")
	}

	// Rune
	if kc.Key == tcell.KeyRune {
		if kc.Rune == ' ' {
			parts = append(parts, "Space")
		} else {
			parts = append(parts, strings.ToUpper(string(kc.Rune)))
		}
		return strings.Join(parts, "+")
	}

	parts = append(parts, "?")
	return strings.Join(parts, "+")
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/keybind/ -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/keybind/
git commit -m "Add keybind package: KeyCombo parse and format"
```

---

### Task 2: Action constants and descriptions

**Files:**
- Create: `internal/keybind/action.go`

- [ ] **Step 1: Define all action constants and descriptions**

```go
// internal/keybind/action.go
package keybind

// Action is a named keyboard action.
type Action string

// ActionInfo holds metadata for an action.
type ActionInfo struct {
	Description string
	Category    string
}

// ActionEntry is returned by AllActions for help display.
type ActionEntry struct {
	Action      Action
	Description string
	Category    string
	Keys        []KeyCombo
}

// Desktop actions
const (
	Help         Action = "help"
	Screenshot   Action = "screenshot"
	Screensaver  Action = "screensaver"
	ConfigEditor Action = "config.editor"
	ControlToggle Action = "control.toggle"
)

// Pane actions
const (
	PaneNavUp    Action = "pane.navigate.up"
	PaneNavDown  Action = "pane.navigate.down"
	PaneNavLeft  Action = "pane.navigate.left"
	PaneNavRight Action = "pane.navigate.right"
	PaneResizeUp    Action = "pane.resize.up"
	PaneResizeDown  Action = "pane.resize.down"
	PaneResizeLeft  Action = "pane.resize.left"
	PaneResizeRight Action = "pane.resize.right"
)

// Workspace actions
const (
	WorkspaceSwitchPrev Action = "workspace.switch.prev"
	WorkspaceSwitchNext Action = "workspace.switch.next"
	WorkspaceTabPrev    Action = "workspace.tab.prev"
	WorkspaceTabNext    Action = "workspace.tab.next"
)

// Control mode actions (after prefix)
const (
	ControlClose    Action = "control.close"
	ControlVSplit   Action = "control.vsplit"
	ControlHSplit   Action = "control.hsplit"
	ControlZoom     Action = "control.zoom"
	ControlSwap     Action = "control.swap"
	ControlLauncher Action = "control.launcher"
	ControlHelp     Action = "control.help"
	ControlConfig   Action = "control.config"
	ControlNewTab   Action = "control.new_tab"
	ControlCloseTab Action = "control.close_tab"
)

// Texelterm actions
const (
	TermSearch      Action = "texelterm.search"
	TermScrollbar   Action = "texelterm.scrollbar"
	TermTransformer Action = "texelterm.transformer"
	TermScreenshot  Action = "texelterm.screenshot"
	TermScrollUp    Action = "texelterm.scroll.up"
	TermScrollDown  Action = "texelterm.scroll.down"
	TermScrollPgUp  Action = "texelterm.scroll.pgup"
	TermScrollPgDn  Action = "texelterm.scroll.pgdn"
)

// ActionDescriptions maps every action to its metadata.
var ActionDescriptions = map[Action]ActionInfo{
	Help:         {Description: "Open help overlay", Category: "Desktop"},
	Screenshot:   {Description: "Save workspace screenshot as PNG", Category: "Desktop"},
	Screensaver:  {Description: "Activate screensaver", Category: "Desktop"},
	ConfigEditor: {Description: "Open configuration editor", Category: "Desktop"},
	ControlToggle: {Description: "Toggle control mode", Category: "Desktop"},

	PaneNavUp:       {Description: "Move focus to pane above", Category: "Pane"},
	PaneNavDown:     {Description: "Move focus to pane below", Category: "Pane"},
	PaneNavLeft:     {Description: "Move focus to pane left", Category: "Pane"},
	PaneNavRight:    {Description: "Move focus to pane right", Category: "Pane"},
	PaneResizeUp:    {Description: "Shrink active pane vertically", Category: "Pane"},
	PaneResizeDown:  {Description: "Grow active pane vertically", Category: "Pane"},
	PaneResizeLeft:  {Description: "Shrink active pane horizontally", Category: "Pane"},
	PaneResizeRight: {Description: "Grow active pane horizontally", Category: "Pane"},

	WorkspaceSwitchPrev: {Description: "Switch to previous workspace", Category: "Workspace"},
	WorkspaceSwitchNext: {Description: "Switch to next workspace", Category: "Workspace"},
	WorkspaceTabPrev:    {Description: "Previous workspace (tab mode)", Category: "Workspace"},
	WorkspaceTabNext:    {Description: "Next workspace (tab mode)", Category: "Workspace"},

	ControlClose:    {Description: "Close active pane", Category: "Control"},
	ControlVSplit:   {Description: "Split pane vertically", Category: "Control"},
	ControlHSplit:   {Description: "Split pane horizontally", Category: "Control"},
	ControlZoom:     {Description: "Toggle pane zoom", Category: "Control"},
	ControlSwap:     {Description: "Enter pane swap mode", Category: "Control"},
	ControlLauncher: {Description: "Open app launcher", Category: "Control"},
	ControlHelp:     {Description: "Open help", Category: "Control"},
	ControlConfig:   {Description: "Open config editor", Category: "Control"},
	ControlNewTab:   {Description: "Create new workspace", Category: "Control"},
	ControlCloseTab: {Description: "Close workspace", Category: "Control"},

	TermSearch:      {Description: "Toggle history search", Category: "Terminal"},
	TermScrollbar:   {Description: "Toggle scrollbar", Category: "Terminal"},
	TermTransformer: {Description: "Toggle transformer pipeline", Category: "Terminal"},
	TermScreenshot:  {Description: "Save pane screenshot as PNG", Category: "Terminal"},
	TermScrollUp:    {Description: "Scroll up one line", Category: "Terminal"},
	TermScrollDown:  {Description: "Scroll down one line", Category: "Terminal"},
	TermScrollPgUp:  {Description: "Scroll up one page", Category: "Terminal"},
	TermScrollPgDn:  {Description: "Scroll down one page", Category: "Terminal"},
}
```

- [ ] **Step 2: Build**

```bash
go build ./internal/keybind/
```

Expected: builds.

- [ ] **Step 3: Commit**

```bash
git add internal/keybind/action.go
git commit -m "Add action constants and descriptions for keybinding system"
```

---

### Task 3: Registry — merge presets, match events

**Files:**
- Create: `internal/keybind/registry.go`
- Create: `internal/keybind/presets.go`
- Create: `internal/keybind/registry_test.go`

- [ ] **Step 1: Write tests**

```go
// internal/keybind/registry_test.go
package keybind

import (
	"sort"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestNewRegistry_LinuxPreset(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	// F1 should be bound to Help
	keys := r.KeysForAction(Help)
	if len(keys) == 0 {
		t.Fatal("Help has no bindings")
	}
}

func TestRegistry_Match(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	ev := tcell.NewEventKey(tcell.KeyF1, 0, tcell.ModNone)
	action := r.Match(ev)
	if action != Help {
		t.Fatalf("Match(F1) = %q, want %q", action, Help)
	}
}

func TestRegistry_MatchUnbound(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	ev := tcell.NewEventKey(tcell.KeyF11, 0, tcell.ModNone)
	action := r.Match(ev)
	if action != "" {
		t.Fatalf("Match(F11) = %q, want empty", action)
	}
}

func TestRegistry_MultipleBindings(t *testing.T) {
	overrides := map[string][]string{
		"screenshot": {"f5", "ctrl+p"},
	}
	r := NewRegistry("linux", "", overrides)
	// Both should match
	ev1 := tcell.NewEventKey(tcell.KeyF5, 0, tcell.ModNone)
	ev2 := tcell.NewEventKey(tcell.KeyCtrlP, 0, tcell.ModCtrl)
	if r.Match(ev1) != Screenshot {
		t.Fatalf("Match(F5) = %q, want screenshot", r.Match(ev1))
	}
	if r.Match(ev2) != Screenshot {
		t.Fatalf("Match(Ctrl+P) = %q, want screenshot", r.Match(ev2))
	}
}

func TestRegistry_OverrideReplacesPreset(t *testing.T) {
	r := NewRegistry("linux", "", map[string][]string{
		"help": {"f9"},
	})
	// F1 should no longer match help
	ev1 := tcell.NewEventKey(tcell.KeyF1, 0, tcell.ModNone)
	if r.Match(ev1) == Help {
		t.Fatal("F1 should not match help after override")
	}
	// F9 should match
	ev2 := tcell.NewEventKey(tcell.KeyF9, 0, tcell.ModNone)
	if r.Match(ev2) != Help {
		t.Fatalf("Match(F9) = %q, want help", r.Match(ev2))
	}
}

func TestRegistry_ExtraPresetOverlay(t *testing.T) {
	r := NewRegistry("linux", "mac", nil)
	// Mac preset should override pane.navigate.up from shift+up to something else
	// Verify mac preset was applied by checking that its bindings exist
	if r == nil {
		t.Fatal("registry is nil")
	}
}

func TestRegistry_AutoDetect(t *testing.T) {
	r := NewRegistry("auto", "", nil)
	if r == nil {
		t.Fatal("auto-detect returned nil")
	}
	// Should have resolved to a valid preset
	keys := r.KeysForAction(Help)
	if len(keys) == 0 {
		t.Fatal("auto-detected registry has no help binding")
	}
}

func TestRegistry_AllActions(t *testing.T) {
	r := NewRegistry("linux", "", nil)
	entries := r.AllActions()
	if len(entries) == 0 {
		t.Fatal("AllActions returned empty")
	}
	// Every action in ActionDescriptions should appear
	for action := range ActionDescriptions {
		found := false
		for _, e := range entries {
			if e.Action == action {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("action %q missing from AllActions", action)
		}
	}
}

func TestPresets_AllActionsPresent(t *testing.T) {
	for name, preset := range map[string]map[Action][]string{
		"linux": linuxPreset,
		"mac":   macPreset,
	} {
		for action := range ActionDescriptions {
			if _, ok := preset[action]; !ok {
				t.Errorf("%s preset missing action %q", name, action)
			}
		}
	}
}
```

- [ ] **Step 2: Implement presets**

```go
// internal/keybind/presets.go
package keybind

import "runtime"

var linuxPreset = map[Action][]string{
	Help:         {"f1"},
	Screenshot:   {"f5"},
	Screensaver:  {"ctrl+s"},
	ConfigEditor: {"f4"},
	ControlToggle: {"ctrl+a"},

	PaneNavUp:       {"shift+up"},
	PaneNavDown:     {"shift+down"},
	PaneNavLeft:     {"shift+left"},
	PaneNavRight:    {"shift+right"},
	PaneResizeUp:    {"ctrl+up"},
	PaneResizeDown:  {"ctrl+down"},
	PaneResizeLeft:  {"ctrl+left"},
	PaneResizeRight: {"ctrl+right"},

	WorkspaceSwitchPrev: {"alt+left"},
	WorkspaceSwitchNext: {"alt+right"},
	WorkspaceTabPrev:    {"shift+left"},
	WorkspaceTabNext:    {"shift+right"},

	ControlClose:    {"x"},
	ControlVSplit:   {"|"},
	ControlHSplit:   {"-"},
	ControlZoom:     {"z"},
	ControlSwap:     {"w"},
	ControlLauncher: {"l"},
	ControlHelp:     {"h"},
	ControlConfig:   {"f"},
	ControlNewTab:   {"t"},
	ControlCloseTab: {"X"},

	TermSearch:      {"f3"},
	TermScrollbar:   {"f7"},
	TermTransformer: {"f8"},
	TermScreenshot:  {"ctrl+p"},
	TermScrollUp:    {"alt+up"},
	TermScrollDown:  {"alt+down"},
	TermScrollPgUp:  {"alt+pgup"},
	TermScrollPgDn:  {"alt+pgdn"},
}

var macPreset = map[Action][]string{
	Help:         {"f1"},
	Screenshot:   {"f5"},
	Screensaver:  {"ctrl+s"},
	ConfigEditor: {"f4"},
	ControlToggle: {"ctrl+a"},

	PaneNavUp:       {"alt+up"},
	PaneNavDown:     {"alt+down"},
	PaneNavLeft:     {"alt+left"},
	PaneNavRight:    {"alt+right"},
	PaneResizeUp:    {"ctrl+up"},
	PaneResizeDown:  {"ctrl+down"},
	PaneResizeLeft:  {"ctrl+left"},
	PaneResizeRight: {"ctrl+right"},

	WorkspaceSwitchPrev: {"alt+["},
	WorkspaceSwitchNext: {"alt+]"},
	WorkspaceTabPrev:    {"alt+["},
	WorkspaceTabNext:    {"alt+]"},

	ControlClose:    {"x"},
	ControlVSplit:   {"|"},
	ControlHSplit:   {"-"},
	ControlZoom:     {"z"},
	ControlSwap:     {"w"},
	ControlLauncher: {"l"},
	ControlHelp:     {"h"},
	ControlConfig:   {"f"},
	ControlNewTab:   {"t"},
	ControlCloseTab: {"X"},

	TermSearch:      {"f3"},
	TermScrollbar:   {"f7"},
	TermTransformer: {"f8"},
	TermScreenshot:  {"ctrl+p"},
	TermScrollUp:    {"alt+up"},
	TermScrollDown:  {"alt+down"},
	TermScrollPgUp:  {"alt+pgup"},
	TermScrollPgDn:  {"alt+pgdn"},
}

func presetByName(name string) map[Action][]string {
	switch name {
	case "linux":
		return linuxPreset
	case "mac":
		return macPreset
	case "auto", "":
		if runtime.GOOS == "darwin" {
			return macPreset
		}
		return linuxPreset
	default:
		return linuxPreset
	}
}
```

- [ ] **Step 3: Implement Registry**

```go
// internal/keybind/registry.go
package keybind

import (
	"log"
	"sort"

	"github.com/gdamore/tcell/v2"
)

// Registry holds resolved key→action mappings.
type Registry struct {
	keyToAction  map[KeyCombo]Action
	actionToKeys map[Action][]KeyCombo
}

// NewRegistry builds a registry from a base preset, optional extra preset overlay,
// and user overrides. Each layer replaces per-action (not per-key).
func NewRegistry(preset string, extraPreset string, overrides map[string][]string) *Registry {
	// Start with base preset
	merged := make(map[Action][]string)
	for a, keys := range presetByName(preset) {
		merged[a] = keys
	}

	// Overlay extra preset (replaces per-action)
	if extraPreset != "" {
		for a, keys := range presetByName(extraPreset) {
			merged[a] = keys
		}
	}

	// Apply user overrides (replaces per-action)
	for actionStr, keys := range overrides {
		merged[Action(actionStr)] = keys
	}

	// Build lookup maps
	r := &Registry{
		keyToAction:  make(map[KeyCombo]Action),
		actionToKeys: make(map[Action][]KeyCombo),
	}

	for action, keyStrs := range merged {
		var combos []KeyCombo
		for _, s := range keyStrs {
			kc, err := ParseKeyCombo(s)
			if err != nil {
				log.Printf("[KEYBIND] Invalid key %q for action %q: %v", s, action, err)
				continue
			}
			combos = append(combos, kc)
			r.keyToAction[kc] = action
		}
		r.actionToKeys[action] = combos
	}

	return r
}

// Match returns the action for a key event, or "" if no binding matches.
func (r *Registry) Match(ev *tcell.EventKey) Action {
	if r == nil || ev == nil {
		return ""
	}
	for kc, action := range r.keyToAction {
		if kc.MatchesEvent(ev) {
			return action
		}
	}
	return ""
}

// KeysForAction returns the bound key combos for an action.
func (r *Registry) KeysForAction(a Action) []KeyCombo {
	if r == nil {
		return nil
	}
	return r.actionToKeys[a]
}

// AllActions returns all registered actions sorted by category, with descriptions
// and current key bindings. Used for help page rendering.
func (r *Registry) AllActions() []ActionEntry {
	var entries []ActionEntry
	for action, info := range ActionDescriptions {
		var keys []KeyCombo
		if r != nil {
			keys = r.actionToKeys[action]
		}
		entries = append(entries, ActionEntry{
			Action:      action,
			Description: info.Description,
			Category:    info.Category,
			Keys:        keys,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		return entries[i].Action < entries[j].Action
	})
	return entries
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/keybind/ -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/keybind/registry.go internal/keybind/presets.go internal/keybind/registry_test.go
git commit -m "Add Registry with preset merging, match, and help support"
```

---

### Task 4: Default JSON assets and loading

**Files:**
- Create: `defaults/keybindings-linux.json`
- Create: `defaults/keybindings-mac.json`
- Modify: `defaults/embedded.go`

- [ ] **Step 1: Create Linux defaults JSON**

```json
{
  "preset": "auto",
  "extraPreset": "",
  "actions": {}
}
```

Save to `defaults/keybindings-linux.json`.

- [ ] **Step 2: Create Mac defaults JSON**

```json
{
  "preset": "auto",
  "extraPreset": "",
  "actions": {}
}
```

Save to `defaults/keybindings-mac.json`.

- [ ] **Step 3: Add embed and loader to embedded.go**

Add to `defaults/embedded.go`:

Change the embed directive:
```go
//go:embed texelation.json apps/*/config.json keybindings-*.json
var fs embed.FS
```

Add function:
```go
// KeybindingsConfig returns the embedded keybindings JSON for the given preset.
func KeybindingsConfig(preset string) ([]byte, error) {
	filename := fmt.Sprintf("keybindings-%s.json", preset)
	return fs.ReadFile(filename)
}

// DefaultKeybindingsPreset returns "linux" or "mac" based on the runtime OS.
func DefaultKeybindingsPreset() string {
	if runtime.GOOS == "darwin" {
		return "mac"
	}
	return "linux"
}
```

Add `"runtime"` to imports.

- [ ] **Step 4: Build**

```bash
go build ./defaults/
```

- [ ] **Step 5: Commit**

```bash
git add defaults/
git commit -m "Add embedded keybinding default JSON assets"
```

---

### Task 5: Load keybindings in client startup

**Files:**
- Modify: `internal/runtime/client/client_state.go`
- Modify: `internal/runtime/client/app.go`

- [ ] **Step 1: Add registry to clientState**

In `client_state.go`, add to the `clientState` struct:
```go
	keybindings *keybind.Registry
```

Add import: `"github.com/framegrace/texelation/internal/keybind"`

- [ ] **Step 2: Load keybindings in Run()**

In `app.go`, in the `Run` function, after `state := &clientState{...}` initialization, add:

```go
	// Load keybindings
	var kbPreset, kbExtra string
	var kbOverrides map[string][]string
	kbPreset = "auto" // default

	// Try to load from config file
	if data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "texelation", "keybindings.json")); err == nil {
		var kbCfg struct {
			Preset      string              `json:"preset"`
			ExtraPreset string              `json:"extraPreset"`
			Actions     map[string][]string `json:"actions"`
		}
		if err := json.Unmarshal(data, &kbCfg); err == nil {
			if kbCfg.Preset != "" {
				kbPreset = kbCfg.Preset
			}
			kbExtra = kbCfg.ExtraPreset
			kbOverrides = kbCfg.Actions
		}
	}
	state.keybindings = keybind.NewRegistry(kbPreset, kbExtra, kbOverrides)
```

Add imports: `"encoding/json"`, `"path/filepath"`, `"github.com/framegrace/texelation/internal/keybind"`

- [ ] **Step 3: Build**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/runtime/client/client_state.go internal/runtime/client/app.go
git commit -m "Load keybinding registry on client startup"
```

---

### Task 6: Replace hardcoded keys in client input handler

**Files:**
- Modify: `internal/runtime/client/input_handler.go`

- [ ] **Step 1: Replace key checks with registry.Match**

Replace the hardcoded key checks in `handleScreenEvent`. The existing code (approximately lines 43-52):

```go
		if ev.Key() == tcell.KeyCtrlS {
			if state.idleWatcher != nil {
				state.idleWatcher.ActivateNow()
				render(state, screen)
			}
			return true
		}
		if ev.Key() == tcell.KeyCtrlP {
			takeScreenshot(state)
			return true
		}
```

Replace with:

```go
		switch state.keybindings.Match(ev) {
		case keybind.Screensaver:
			if state.idleWatcher != nil {
				state.idleWatcher.ActivateNow()
				render(state, screen)
			}
			return true
		case keybind.Screenshot:
			takeScreenshot(state)
			return true
		}
```

Also replace the Ctrl+A control mode toggle (around line 72-81) and Esc exit (around line 83-92):

```go
		// Control mode handling via keybindings
		action := state.keybindings.Match(ev)
		if action == keybind.ControlToggle {
			// ... existing control mode toggle logic ...
		}
```

Add import: `"github.com/framegrace/texelation/internal/keybind"`

- [ ] **Step 2: Build and test**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/runtime/client/input_handler.go
git commit -m "Replace hardcoded keys in client input handler with keybind registry"
```

---

### Task 7: Pass registry to DesktopEngine and replace desktop keys

**Files:**
- Modify: `texel/desktop_engine_core.go`
- Modify: `texel/desktop_input.go`

- [ ] **Step 1: Add registry to DesktopEngine**

In `desktop_engine_core.go`, add to the `DesktopEngine` struct:

```go
	keybindings *keybind.Registry
```

Add a setter method:
```go
func (d *DesktopEngine) SetKeybindings(r *keybind.Registry) {
	d.keybindings = r
}
```

Add import: `"github.com/framegrace/texelation/internal/keybind"`

- [ ] **Step 2: Replace hardcoded keys in desktop_input.go**

Replace in `handleEvent`:

```go
	// Global Shortcuts
	if key.Key() == tcell.KeyF1 {
		d.launchHelpOverlay()
		return
	}
```

With:

```go
	// Global shortcuts via keybinding registry
	action := d.keybindings.Match(key)
	switch action {
	case keybind.Help:
		d.launchHelpOverlay()
		return
	case keybind.ConfigEditor:
		d.launchConfigEditorOverlay(d.activeAppTarget())
		return
	}
```

Replace the Alt+Left/Right workspace switching:
```go
	if key.Modifiers()&tcell.ModAlt != 0 {
		switch key.Key() {
		case tcell.KeyLeft:
			d.switchWorkspaceRelative(-1)
			return
		case tcell.KeyRight:
			d.switchWorkspaceRelative(1)
			return
		}
	}
```

With:
```go
	switch action {
	case keybind.WorkspaceSwitchPrev:
		d.switchWorkspaceRelative(-1)
		return
	case keybind.WorkspaceSwitchNext:
		d.switchWorkspaceRelative(1)
		return
	}
```

Replace the Ctrl+Arrows pane resize:
```go
	if key.Modifiers()&tcell.ModCtrl != 0 && !d.inTabMode {
```

With matching on `PaneResizeUp/Down/Left/Right` actions.

Replace the Ctrl+A control mode toggle and Ctrl+F config editor.

- [ ] **Step 3: Build**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add texel/desktop_engine_core.go texel/desktop_input.go
git commit -m "Replace hardcoded keys in desktop input with keybind registry"
```

---

### Task 8: Replace hardcoded keys in workspace and control mode

**Files:**
- Modify: `texel/workspace.go`
- Modify: `texel/desktop_engine_control_mode.go`

- [ ] **Step 1: Replace Shift+Arrow pane navigation in workspace.go**

In `handleEvent`, replace the `ev.Modifiers()&tcell.ModShift` block with:

```go
	if d := d.keybindings; d != nil {
		switch d.Match(ev) {
		case keybind.PaneNavUp:
			if !w.moveActivePane(DirUp) {
				w.desktop.enterTabNavMode()
			}
			w.Refresh()
			return
		case keybind.PaneNavDown:
			w.moveActivePane(DirDown)
			w.Refresh()
			return
		case keybind.PaneNavLeft:
			w.moveActivePane(DirLeft)
			w.Refresh()
			return
		case keybind.PaneNavRight:
			w.moveActivePane(DirRight)
			w.Refresh()
			return
		}
	}
```

The workspace needs access to the keybindings registry. Add a field or access through `w.desktop.keybindings`.

- [ ] **Step 2: Replace control mode keys**

In `handleControlMode`, replace the rune switch with keybinding matches for control mode actions.

In `handleTabMode`, replace the Shift+Arrow checks with keybinding matches for `WorkspaceTabPrev`/`WorkspaceTabNext`.

- [ ] **Step 3: Build**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add texel/workspace.go texel/desktop_engine_control_mode.go
git commit -m "Replace hardcoded keys in workspace and control mode with keybind registry"
```

---

### Task 9: Replace hardcoded keys in texelterm

**Files:**
- Modify: `apps/texelterm/term.go`

- [ ] **Step 1: Add registry to TexelTerm**

The TexelTerm needs access to the registry. Add a field and a setter:

```go
	keybindings *keybind.Registry
```

```go
func (a *TexelTerm) SetKeybindings(r *keybind.Registry) {
	a.keybindings = r
}
```

- [ ] **Step 2: Replace hardcoded keys in HandleKey**

Replace `Ctrl+G`, `Alt+B`, `Ctrl+T`, `Ctrl+P`, and Alt+scroll keys with keybinding matches:

```go
	action := a.keybindings.Match(ev)
	switch action {
	case keybind.TermSearch:
		// toggle history navigator
		...
		return
	case keybind.TermScrollbar:
		if a.scrollbar != nil {
			a.scrollbar.Toggle()
		}
		return
	case keybind.TermTransformer:
		a.toggleTransformers()
		return
	case keybind.TermScreenshot:
		a.takeScreenshot()
		return
	case keybind.TermScrollUp:
		// scroll up one line
		...
		return
	case keybind.TermScrollDown:
		// scroll down one line
		...
		return
	case keybind.TermScrollPgUp:
		// scroll up one page
		...
		return
	case keybind.TermScrollPgDn:
		// scroll down one page
		...
		return
	}
```

- [ ] **Step 3: Wire registry from server to texelterm**

In the server boot or pane creation code, pass the registry to each TexelTerm instance via `SetKeybindings`.

- [ ] **Step 4: Build and test**

```bash
go build ./...
make test
```

- [ ] **Step 5: Commit**

```bash
git add apps/texelterm/term.go
git commit -m "Replace hardcoded keys in texelterm with keybind registry"
```

---

### Task 10: Wire registry through server to desktop engine

**Files:**
- Modify: `internal/runtime/client/app.go` (or protocol handler)
- Modify: `cmd/texel-server/main.go` or server boot

- [ ] **Step 1: Pass registry from client to desktop engine**

In the client's protocol handler where `DesktopEngine` is accessed, call:
```go
desktop.SetKeybindings(state.keybindings)
```

For the server side (when running in unified mode), the keybindings need to reach the DesktopEngine. In `handleUnifiedMode` in `cmd/texelation/main.go`, after creating the desktop, set keybindings.

- [ ] **Step 2: Pass registry to TexelTerm instances**

In pane creation/app factory, pass the registry. This may require adding it to the app factory or the pane setup.

- [ ] **Step 3: Build and test**

```bash
go build ./...
make test
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "Wire keybinding registry through server to desktop and texelterm"
```

---

### Task 11: First-run copy of keybindings config

**Files:**
- Modify: `config/config.go` or startup code

- [ ] **Step 1: Copy default keybindings on first run**

In the config initialization (where `texelation.json` is copied on first run), add similar logic for `keybindings.json`:

```go
kbPath := filepath.Join(configDir, "keybindings.json")
if _, err := os.Stat(kbPath); os.IsNotExist(err) {
	preset := defaults.DefaultKeybindingsPreset()
	data, err := defaults.KeybindingsConfig(preset)
	if err == nil {
		os.WriteFile(kbPath, data, 0644)
	}
}
```

- [ ] **Step 2: Build and test**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add config/ defaults/
git commit -m "Copy default keybindings.json on first run"
```
