// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package effects

import (
	"testing"
	"time"
)

func TestParseScreensaverConfig_Defaults(t *testing.T) {
	cfg := ParseScreensaverConfig(nil)
	if cfg.Enabled {
		t.Fatal("expected disabled by default")
	}
	if cfg.Timeout != 5*time.Minute {
		t.Fatalf("expected 5m timeout, got %v", cfg.Timeout)
	}
	if cfg.EffectID != "crypt" {
		t.Fatalf("expected crypt effect, got %q", cfg.EffectID)
	}
	if cfg.LockEnabled {
		t.Fatal("expected lock disabled by default")
	}
	if cfg.LockTimeout != 15*time.Minute {
		t.Fatalf("expected 15m lock timeout, got %v", cfg.LockTimeout)
	}
}

func TestParseScreensaverConfig_Custom(t *testing.T) {
	section := map[string]interface{}{
		"enabled":              true,
		"timeout_minutes":      float64(10),
		"effect":               "rainbow",
		"lock_enabled":         true,
		"lock_timeout_minutes": float64(30),
	}
	cfg := ParseScreensaverConfig(section)
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Timeout != 10*time.Minute {
		t.Fatalf("expected 10m, got %v", cfg.Timeout)
	}
	if cfg.EffectID != "rainbow" {
		t.Fatalf("expected rainbow, got %q", cfg.EffectID)
	}
	if !cfg.LockEnabled {
		t.Fatal("expected lock enabled")
	}
	if cfg.LockTimeout != 30*time.Minute {
		t.Fatalf("expected 30m, got %v", cfg.LockTimeout)
	}
}

func TestParseScreensaverConfig_FractionalMinutes(t *testing.T) {
	section := map[string]interface{}{
		"enabled":         true,
		"timeout_minutes": float64(0.5),
	}
	cfg := ParseScreensaverConfig(section)
	if cfg.Timeout != 30*time.Second {
		t.Fatalf("expected 30s for 0.5 minutes, got %v", cfg.Timeout)
	}
}
