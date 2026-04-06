// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/keybinding_editor.go
// Summary: Keybinding editor tab for the system config editor.

package configeditor

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/framegrace/texelation/internal/keybind"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// keybindingConfig is the JSON structure of keybindings.json.
type keybindingConfig struct {
	Preset      string              `json:"preset"`
	ExtraPreset string              `json:"extraPreset"`
	Actions     map[string][]string `json:"actions"`
}

// modifierOptions for the combo box. <Control Mode> is dynamic.
var modifierLabels = []string{"(none)", "<Ctrl>", "<Alt>", "<Shift>", "<Control Mode>"}

// buildKeybindingsTab creates the Keybindings tab with sub-tabs per category.
func buildKeybindingsTab(onApply func()) core.Widget {
	// Load current keybinding config
	kbCfg := loadKeybindingConfig()
	registry := keybind.NewRegistry(kbCfg.Preset, kbCfg.ExtraPreset, kbCfg.Actions)
	if kbCfg.Actions == nil {
		kbCfg.Actions = make(map[string][]string)
	}

	// Group actions by category
	entries := registry.AllActions()
	categories := make(map[string][]keybind.ActionEntry)
	for _, e := range entries {
		categories[e.Category] = append(categories[e.Category], e)
	}

	// Sort category names
	catNames := make([]string, 0, len(categories))
	for cat := range categories {
		catNames = append(catNames, cat)
	}
	sort.Strings(catNames)

	panel := widgets.NewTabPanel()
	for _, cat := range catNames {
		actions := categories[cat]
		pane := buildKeybindingCategory(actions, registry, kbCfg, onApply)
		panel.AddTab(cat, pane)
	}

	return panel
}

// buildKeybindingCategory creates a scrollable form for one category of actions.
func buildKeybindingCategory(actions []keybind.ActionEntry, registry *keybind.Registry, kbCfg *keybindingConfig, onApply func()) core.Widget {
	form := widgets.NewForm()

	for _, entry := range actions {
		action := entry.Action
		desc := entry.Description

		// Get current binding
		keys := registry.KeysForAction(action)
		currentModifier := "(none)"
		currentKey := ""
		if len(keys) > 0 {
			currentModifier, currentKey = decomposeKeyCombo(keys[0], entry.Category)
		}

		// Build the modifier combo
		modCombo := widgets.NewComboBox(modifierLabels, false)
		modCombo.SetValue(currentModifier)

		// Build the key input
		keyInput := widgets.NewInput()
		keyInput.Text = currentKey

		// Capture for closure
		act := action
		cat := entry.Category

		// On change handler for either widget
		onChange := func() {
			mod := modCombo.Value()
			key := keyInput.Text
			if key == "" {
				return
			}
			keyStr := composeKeyString(mod, key, cat)
			if keyStr == "" {
				return
			}
			kbCfg.Actions[string(act)] = []string{keyStr}
			saveKeybindingConfig(kbCfg)
			if onApply != nil {
				onApply()
			}
		}

		modCombo.OnChange = func(_ string) { onChange() }
		keyInput.OnSubmit = func(_ string) { onChange() }

		// Create a row: [description] [modifier combo] [key input]
		descLabel := widgets.NewLabel(desc)
		form.AddRow(widgets.FormRow{
			Label:  descLabel,
			Field:  modCombo,
			Height: 1,
		})
		form.AddRow(widgets.FormRow{
			Label:  widgets.NewLabel("  Key:"),
			Field:  keyInput,
			Height: 1,
		})
		form.AddSpacer(1)
	}

	return wrapInScrollPane(form)
}

// decomposeKeyCombo splits a KeyCombo into modifier label and key name.
func decomposeKeyCombo(kc keybind.KeyCombo, category string) (modifier, key string) {
	// Control mode actions are always prefixed by the control key
	if category == "Control" {
		formatted := keybind.FormatKeyCombo(kc)
		return "<Control Mode>", strings.ToLower(formatted)
	}

	formatted := keybind.FormatKeyCombo(kc)
	lower := strings.ToLower(formatted)

	// Check for modifiers
	if strings.HasPrefix(lower, "ctrl+") {
		return "<Ctrl>", strings.TrimPrefix(lower, "ctrl+")
	}
	if strings.HasPrefix(lower, "alt+") {
		return "<Alt>", strings.TrimPrefix(lower, "alt+")
	}
	if strings.HasPrefix(lower, "shift+") {
		return "<Shift>", strings.TrimPrefix(lower, "shift+")
	}

	return "(none)", lower
}

// composeKeyString builds a key string like "shift+up" from modifier label and key name.
func composeKeyString(modifier, key, category string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return ""
	}

	// Control mode actions: the key is bare (the control prefix is implicit)
	if category == "Control" || modifier == "<Control Mode>" {
		return key
	}

	switch modifier {
	case "<Ctrl>":
		return "ctrl+" + key
	case "<Alt>":
		return "alt+" + key
	case "<Shift>":
		return "shift+" + key
	default:
		return key
	}
}

// loadKeybindingConfig reads the keybindings.json file.
func loadKeybindingConfig() *keybindingConfig {
	cfg := &keybindingConfig{Preset: "auto"}
	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "texelation", "keybindings.json"))
	if err != nil {
		return cfg
	}
	if json.Unmarshal(data, cfg) != nil {
		return &keybindingConfig{Preset: "auto"}
	}
	return cfg
}

// saveKeybindingConfig writes the keybindings.json file.
func saveKeybindingConfig(cfg *keybindingConfig) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[KEYBIND] Failed to get home dir: %v", err)
		return
	}
	path := filepath.Join(home, ".config", "texelation", "keybindings.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("[KEYBIND] Failed to marshal: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[KEYBIND] Failed to write: %v", err)
	}
}
