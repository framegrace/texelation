// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package theme

import "testing"

func TestWithOverridesMergesAndPreservesBase(t *testing.T) {
	base := Config{
		"ui": Section{
			"text.primary": "@text",
		},
		"pane": Section{
			"active_border_fg": "@red",
		},
	}
	overrides := Config{
		"ui": Section{
			"text.primary": "@mauve",
		},
		"selection": Section{
			"highlight_bg": "@blue",
		},
	}

	merged := WithOverrides(base, overrides)

	if got := merged["ui"]["text.primary"]; got != "@mauve" {
		t.Fatalf("expected override value, got %v", got)
	}
	if got := merged["pane"]["active_border_fg"]; got != "@red" {
		t.Fatalf("expected base value, got %v", got)
	}
	if got := merged["selection"]["highlight_bg"]; got != "@blue" {
		t.Fatalf("expected new section value, got %v", got)
	}

	if got := base["ui"]["text.primary"]; got != "@text" {
		t.Fatalf("expected base to remain unchanged, got %v", got)
	}
}

func TestParseOverridesFromMapAndJSON(t *testing.T) {
	raw := map[string]interface{}{
		"ui": map[string]interface{}{
			"text.primary": "@text",
		},
	}
	cfg := ParseOverrides(raw)
	if cfg == nil || cfg["ui"] == nil {
		t.Fatalf("expected parsed config from map")
	}

	jsonRaw := `{"ui":{"text.primary":"@text"}}`
	cfg = ParseOverrides(jsonRaw)
	if cfg == nil || cfg["ui"] == nil {
		t.Fatalf("expected parsed config from json")
	}
	if got := cfg["ui"]["text.primary"]; got != "@text" {
		t.Fatalf("expected text.primary, got %v", got)
	}
}
