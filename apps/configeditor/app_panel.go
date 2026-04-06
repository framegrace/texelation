// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/app_panel.go
// Summary: Standalone app config panel builder for embedding in overlays.

package configeditor

import (
	"fmt"
	"sort"

	"github.com/framegrace/texelation/config"
	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// NewAppConfigPanel builds a standalone config panel widget for the given app.
// It loads the app's config, creates form sections with auto-save on change,
// and calls onStatus with status messages (save confirmations or errors).
// The returned widget is a targetContent (Pane with header + TabPanel).
func NewAppConfigPanel(appName string, onStatus func(msg string, isErr bool)) (core.Widget, error) {
	values := ensureConfig(config.Clone(config.App(appName)))
	if values == nil {
		values = make(config.Config)
	}

	target := &configTarget{
		kind:   targetApp,
		name:   appName,
		label:  humanLabel(appName),
		values: values,
	}

	onApply := func(kind applyKind) {
		config.SetApp(appName, target.values)
		if err := config.SaveApp(appName); err != nil {
			if onStatus != nil {
				onStatus(fmt.Sprintf("Save failed: %v", err), true)
			}
		} else {
			if onStatus != nil {
				onStatus("Saved.", false)
			}
		}
	}

	sections := buildAppSectionsStandalone(target, onApply)
	if sections == nil {
		return nil, fmt.Errorf("no config sections for app %q", appName)
	}

	target.sections = sections
	content := newTargetContent(target.label+" Configuration", sections)
	return content, nil
}

// NewAppConfigPanelWithStorage builds a config panel that reads/writes from
// a provided config map instead of the global config. Used for per-pane config.
// onSave is called with the updated config on each change.
// onSaveAsDefault is called when the user clicks "Save as Default" (may be nil to hide the button).
func NewAppConfigPanelWithStorage(appName string, values config.Config, onSave func(config.Config), onSaveAsDefault func(config.Config), onStatus func(msg string, isErr bool)) (core.Widget, error) {
	if values == nil {
		values = make(config.Config)
	}
	values = ensureConfig(config.Clone(values))

	target := &configTarget{
		kind:   targetApp,
		name:   appName,
		label:  humanLabel(appName),
		values: values,
	}

	onApply := func(kind applyKind) {
		if onSave != nil {
			onSave(target.values)
		}
		if onStatus != nil {
			onStatus("Saved.", false)
		}
	}

	sections := buildAppSectionsStandalone(target, onApply)
	if sections == nil {
		return nil, fmt.Errorf("no config sections for app %q", appName)
	}

	target.sections = sections
	content := newTargetContent(target.label+" Configuration", sections)

	// Add "Save as Default" button at the bottom if callback provided.
	if onSaveAsDefault != nil {
		btn := widgets.NewButton("💾 Save as Default")
		btn.OnClick = func() {
			onSaveAsDefault(target.values)
			if onStatus != nil {
				onStatus("Saved as default for new terminals.", false)
			}
		}
		content.footer = btn
		content.Pane.AddChild(btn)
	}

	return content, nil
}

// buildAppSectionsStandalone builds app config sections without a ConfigEditor.
func buildAppSectionsStandalone(target *configTarget, onApply func(applyKind)) *widgets.TabPanel {
	sections := splitSections(target.values)
	delete(sections, "theme_overrides")

	// Filter by defaults schema — only show known keys.
	if defaults := config.AppDefaults(target.name); defaults != nil {
		defaultSections := splitSections(defaults)
		filtered := make(map[string]map[string]interface{})
		for sectionKey, defKeys := range defaultSections {
			userKeys := sections[sectionKey]
			if userKeys == nil {
				userKeys = make(map[string]interface{})
			}
			filteredKeys := make(map[string]interface{})
			for key, defVal := range defKeys {
				if userVal, ok := userKeys[key]; ok {
					filteredKeys[key] = userVal
				} else {
					filteredKeys[key] = defVal
				}
			}
			filtered[sectionKey] = filteredKeys
		}
		sections = filtered
	}

	if len(sections) == 0 {
		sections[""] = map[string]interface{}{}
	}
	sectionKeys := make([]string, 0, len(sections))
	for key := range sections {
		sectionKeys = append(sectionKeys, key)
	}
	sort.Strings(sectionKeys)

	panel := widgets.NewTabPanel()
	for _, key := range sectionKeys {
		label := key
		if key == "" {
			label = "General"
		} else {
			label = humanLabel(key)
		}
		pane := buildSectionPaneStandalone(target, target.values, key, sections[key], onApply)
		panel.AddTab(label, pane)
	}
	return panel
}

// buildSectionPaneStandalone builds a single config section form.
func buildSectionPaneStandalone(target *configTarget, cfg config.Config, sectionKey string, values map[string]interface{}, onApply func(applyKind)) core.Widget {
	pane := widgets.NewForm()
	if values == nil {
		values = make(map[string]interface{})
	}
	keys := keysSorted(values)
	for _, key := range keys {
		value := values[key]
		fb := NewFieldBuilder(target, cfg, onApply)
		fb.Build(FieldConfig{
			Section:   sectionKey,
			Key:       key,
			Value:     value,
			ApplyKind: applyApp,
		}, pane)
	}
	return wrapInScrollPane(pane)
}
