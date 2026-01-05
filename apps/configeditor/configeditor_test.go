// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package configeditor

import (
	"testing"

	"texelation/config"
	"texelation/texel/theme"
	"texelation/texelui/widgets"
)

func TestBuildFieldKinds(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_ = theme.Reload()

	editor := &ConfigEditor{}
	target := &configTarget{
		kind:       targetSystem,
		appOptions: []string{"launcher", "texelterm"},
		values:     make(config.Config),
	}
	pane := widgets.NewForm()

	binding := editor.buildField(target.values, target, "", "enabled", true, pane, false, applySystem)
	if binding == nil || binding.kind != fieldBool {
		t.Fatalf("expected bool field, got %#v", binding)
	}

	binding = editor.buildField(target.values, target, "", "count", int(3), pane, false, applySystem)
	if binding == nil || binding.kind != fieldInt {
		t.Fatalf("expected int field, got %#v", binding)
	}

	binding = editor.buildField(target.values, target, "", "ratio", float64(1.5), pane, false, applySystem)
	if binding == nil || binding.kind != fieldFloat {
		t.Fatalf("expected float field, got %#v", binding)
	}

	binding = editor.buildField(target.values, target, "", "count_float", float64(2), pane, false, applySystem)
	if binding == nil || binding.kind != fieldInt {
		t.Fatalf("expected int field from float, got %#v", binding)
	}

	binding = editor.buildField(target.values, target, "", "defaultApp", "launcher", pane, false, applySystem)
	if binding == nil || binding.kind != fieldCombo {
		t.Fatalf("expected combo field, got %#v", binding)
	}

	binding = editor.buildField(target.values, target, "", "accent", "#aabbcc", pane, false, applySystem)
	if binding == nil || binding.kind != fieldColor {
		t.Fatalf("expected color field, got %#v", binding)
	}

	binding = editor.buildField(target.values, target, "", "payload", []interface{}{"a"}, pane, false, applySystem)
	if binding == nil || binding.kind != fieldJSON {
		t.Fatalf("expected json field, got %#v", binding)
	}
}
