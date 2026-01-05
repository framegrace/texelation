// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/configeditor/field_builder.go
// Summary: Encapsulates widget construction for config editor fields.
// Usage: Used by ConfigEditor to build form fields for different value types.

package configeditor

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"texelation/config"
	"texelation/texelui/widgets"
)

// FieldConfig holds configuration for building a single field.
type FieldConfig struct {
	Section    string
	Key        string
	Value      interface{}
	Label      string
	Options    []string
	ForceColor bool
	ApplyKind  applyKind
}

// FieldBuilder encapsulates widget construction for config fields.
type FieldBuilder struct {
	target  *configTarget
	cfg     config.Config
	onApply func(applyKind)
}

// NewFieldBuilder creates a new FieldBuilder.
func NewFieldBuilder(target *configTarget, cfg config.Config, onApply func(applyKind)) *FieldBuilder {
	return &FieldBuilder{
		target:  target,
		cfg:     cfg,
		onApply: onApply,
	}
}

// Build creates the appropriate widget for the given field configuration.
// Returns the widget and a fieldBinding for tracking.
func (fb *FieldBuilder) Build(fc FieldConfig, pane *formPane) *fieldBinding {
	if fc.Label == "" {
		fc.Label = humanLabel(fc.Key)
	}
	if fc.Options == nil {
		fc.Options = ComboOptionsFor(fb.target, fc.Section, fc.Key)
	}

	switch v := fc.Value.(type) {
	case bool:
		return fb.buildCheckbox(fc, v, pane)
	case float64:
		if v == math.Trunc(v) {
			fc.Value = int(v)
			return fb.Build(fc, pane)
		}
		if fc.Options != nil {
			return fb.buildComboBox(fc, fmt.Sprintf("%v", v), pane)
		}
		return fb.buildNumericInput(fc, v, pane)
	case int:
		return fb.buildIntInput(fc, v, pane)
	case string:
		if fc.Options != nil {
			return fb.buildComboBox(fc, v, pane)
		}
		if fc.ForceColor || looksLikeColor(v) {
			return fb.buildColorPicker(fc, v, pane)
		}
		return fb.buildStringInput(fc, v, pane)
	case []interface{}, map[string]interface{}, []string:
		return fb.buildTextArea(fc, v, pane)
	default:
		return fb.buildStringInput(fc, fmt.Sprintf("%v", v), pane)
	}
}

func (fb *FieldBuilder) buildCheckbox(fc FieldConfig, value bool, pane *formPane) *fieldBinding {
	checkbox := widgets.NewCheckbox(0, 0, fc.Label)
	checkbox.Checked = value
	checkbox.OnChange = func(checked bool) {
		updateConfigValue(fb.cfg, fc.Section, fc.Key, checked)
		fb.onApply(fc.ApplyKind)
	}
	pane.AddRow(formRow{field: checkbox, height: 1, fullWidth: true})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldBool, widget: checkbox}
}

func (fb *FieldBuilder) buildComboBox(fc FieldConfig, value string, pane *formPane) *fieldBinding {
	combo := widgets.NewComboBox(0, 0, 0, fc.Options, false)
	combo.SetValue(value)
	combo.OnChange = func(val string) {
		updateConfigValue(fb.cfg, fc.Section, fc.Key, val)
		fb.onApply(fc.ApplyKind)
	}
	pane.AddRow(formRow{label: widgets.NewLabel(fc.Label), field: combo, height: 1})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldCombo, widget: combo}
}

func (fb *FieldBuilder) buildNumericInput(fc FieldConfig, value float64, pane *formPane) *fieldBinding {
	input := widgets.NewInput()
	input.Text = formatNumber(value)
	dirty := false
	input.OnChange = func(text string) {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil {
			updateConfigValue(fb.cfg, fc.Section, fc.Key, parsed)
			dirty = true
		}
	}
	input.OnBlur = func(text string) {
		if dirty {
			fb.onApply(fc.ApplyKind)
			dirty = false
		}
	}
	pane.AddRow(formRow{label: widgets.NewLabel(fc.Label), field: input, height: 1})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldFloat, widget: input}
}

func (fb *FieldBuilder) buildIntInput(fc FieldConfig, value int, pane *formPane) *fieldBinding {
	input := widgets.NewInput()
	input.Text = strconv.Itoa(value)
	dirty := false
	input.OnChange = func(text string) {
		if parsed, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
			updateConfigValue(fb.cfg, fc.Section, fc.Key, parsed)
			dirty = true
		}
	}
	input.OnBlur = func(text string) {
		if dirty {
			fb.onApply(fc.ApplyKind)
			dirty = false
		}
	}
	pane.AddRow(formRow{label: widgets.NewLabel(fc.Label), field: input, height: 1})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldInt, widget: input}
}

func (fb *FieldBuilder) buildColorPicker(fc FieldConfig, value string, pane *formPane) *fieldBinding {
	colorPicker := widgets.NewColorPicker(0, 0, widgets.ColorPickerConfig{
		EnableSemantic: true,
		EnablePalette:  true,
		EnableOKLCH:    true,
		Label:          fc.Label,
	})
	colorPicker.SetValue(value)
	colorPicker.OnChange = func(result widgets.ColorPickerResult) {
		updateConfigValue(fb.cfg, fc.Section, fc.Key, result.Source)
		fb.onApply(fc.ApplyKind)
	}
	pane.AddRow(formRow{label: widgets.NewLabel(fc.Label), field: colorPicker, height: 1})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldColor, widget: colorPicker}
}

func (fb *FieldBuilder) buildStringInput(fc FieldConfig, value string, pane *formPane) *fieldBinding {
	input := widgets.NewInput()
	input.Text = value
	dirty := false
	input.OnChange = func(text string) {
		updateConfigValue(fb.cfg, fc.Section, fc.Key, text)
		dirty = true
	}
	input.OnBlur = func(text string) {
		if dirty {
			fb.onApply(fc.ApplyKind)
			dirty = false
		}
	}
	pane.AddRow(formRow{label: widgets.NewLabel(fc.Label), field: input, height: 1})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldString, widget: input}
}

func (fb *FieldBuilder) buildTextArea(fc FieldConfig, value interface{}, pane *formPane) *fieldBinding {
	textarea := widgets.NewTextArea()
	textarea.Resize(0, 4)
	textarea.SetText(formatJSON(value))
	dirty := false
	textarea.OnChange = func(text string) {
		var decoded interface{}
		if err := json.Unmarshal([]byte(text), &decoded); err == nil {
			updateConfigValue(fb.cfg, fc.Section, fc.Key, decoded)
			dirty = true
		}
	}
	textarea.OnBlur = func(text string) {
		if dirty {
			fb.onApply(fc.ApplyKind)
			dirty = false
		}
	}
	pane.AddRow(formRow{label: widgets.NewLabel(fc.Label), height: 1})
	pane.AddRow(formRow{field: textarea, height: 4, fullWidth: true})
	return &fieldBinding{section: fc.Section, key: fc.Key, kind: fieldJSON, widget: textarea}
}
