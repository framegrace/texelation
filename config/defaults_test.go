package config

import (
	"sort"
	"testing"
)

func TestValidateAgainstDefaults(t *testing.T) {
	defaults := Config{
		"texelterm": Section{
			"visual_bell_enabled": false,
		},
		"texelterm.scroll": Section{
			"debounce_ms": 50,
		},
	}

	cfg := Config{
		"texelterm": Section{
			"visual_bell_enabled": true,
			"old_dead_key":        "stale",
		},
		"texelterm.scroll": Section{
			"debounce_ms": 100,
		},
		"unknown_section": Section{
			"foo": "bar",
		},
	}

	unknown := ValidateAgainstDefaults(defaults, cfg)
	sort.Strings(unknown)

	if len(unknown) != 2 {
		t.Fatalf("expected 2 unknown keys, got %d: %v", len(unknown), unknown)
	}
	if unknown[0] != "texelterm.old_dead_key" {
		t.Errorf("expected texelterm.old_dead_key, got %s", unknown[0])
	}
	if unknown[1] != "unknown_section" {
		t.Errorf("expected unknown_section, got %s", unknown[1])
	}
}

func TestValidateAgainstDefaults_Clean(t *testing.T) {
	defaults := Config{
		"texelterm": Section{
			"visual_bell_enabled": false,
		},
	}
	cfg := Config{
		"texelterm": Section{
			"visual_bell_enabled": true,
		},
	}

	unknown := ValidateAgainstDefaults(defaults, cfg)
	if len(unknown) != 0 {
		t.Fatalf("expected no unknown keys, got %v", unknown)
	}
}
