// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetStore() {
	once = sync.Once{}
	system = nil
	apps = nil
	loadErr = nil
}

func TestSystemDefaultsWritten(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetStore()

	cfg := System()
	if cfg.GetString("", "defaultApp", "") == "" {
		t.Fatalf("expected defaultApp to be set")
	}

	path, err := systemConfigPath()
	if err != nil {
		t.Fatalf("systemConfigPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read system config: %v", err)
	}

	var disk Config
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("unmarshal system config: %v", err)
	}
	if disk.Section("layout_transitions") == nil {
		t.Fatalf("expected layout_transitions section to be present")
	}
}

func TestSaveSystemWritesUpdates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetStore()

	cfg := Config{
		"defaultApp": "texelterm",
	}
	SetSystem(cfg)
	if err := SaveSystem(); err != nil {
		t.Fatalf("SaveSystem: %v", err)
	}

	path, err := systemConfigPath()
	if err != nil {
		t.Fatalf("systemConfigPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read system config: %v", err)
	}

	var disk Config
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("unmarshal system config: %v", err)
	}
	if got := disk.GetString("", "defaultApp", ""); got != "texelterm" {
		t.Fatalf("expected defaultApp to be texelterm, got %q", got)
	}
}

func TestAppDefaultsWritten(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetStore()

	cfg := App("texelterm")
	if cfg.Section("texelterm") == nil {
		t.Fatalf("expected texelterm section to be present")
	}

	path, err := appConfigPath("texelterm")
	if err != nil {
		t.Fatalf("appConfigPath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected app config to be written: %v", err)
	}
}

func TestSaveAppWritesUpdates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	resetStore()

	cfg := Config{
		"texelterm": map[string]interface{}{
			"wrap_enabled": false,
		},
	}
	SetApp("texelterm", cfg)
	if err := SaveApp("texelterm"); err != nil {
		t.Fatalf("SaveApp: %v", err)
	}

	path, err := appConfigPath("texelterm")
	if err != nil {
		t.Fatalf("appConfigPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read app config: %v", err)
	}

	var disk Config
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("unmarshal app config: %v", err)
	}
	section := disk.Section("texelterm")
	if section == nil {
		t.Fatalf("expected texelterm section")
	}
	if got, _ := section["wrap_enabled"].(bool); got {
		t.Fatalf("expected wrap_enabled false")
	}
}

func TestSystemMigrationFromLegacy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	resetStore()

	cfgRoot := filepath.Join(root, "texelation")
	if err := os.MkdirAll(cfgRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeConfig(filepath.Join(cfgRoot, "config.json"), Config{
		"defaultApp": "texelterm",
	}); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	if err := writeConfig(filepath.Join(cfgRoot, "theme.json"), Config{
		"layout_transitions": map[string]interface{}{
			"enabled": false,
		},
		"effects": map[string]interface{}{
			"bindings": []interface{}{},
		},
		"meta": map[string]interface{}{
			"palette": "frappe",
		},
	}); err != nil {
		t.Fatalf("write legacy theme: %v", err)
	}

	cfg := System()
	if got := cfg.GetString("", "defaultApp", ""); got != "texelterm" {
		t.Fatalf("expected defaultApp migration, got %q", got)
	}
	if got := cfg.GetString("", "activeTheme", ""); got != "frappe" {
		t.Fatalf("expected activeTheme migration, got %q", got)
	}
	if cfg.Section("layout_transitions") == nil {
		t.Fatalf("expected layout_transitions migration")
	}
	if cfg.Section("effects") == nil {
		t.Fatalf("expected effects migration")
	}
}

func TestAppMigrationFromLegacy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	resetStore()

	cfgRoot := filepath.Join(root, "texelation")
	if err := os.MkdirAll(cfgRoot, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeConfig(filepath.Join(cfgRoot, "theme.json"), Config{
		"texelterm": map[string]interface{}{
			"wrap_enabled": false,
		},
	}); err != nil {
		t.Fatalf("write legacy theme: %v", err)
	}

	cfg := App("texelterm")
	section := cfg.Section("texelterm")
	if section == nil {
		t.Fatalf("expected texelterm section after migration")
	}
	if got, _ := section["wrap_enabled"].(bool); got {
		t.Fatalf("expected wrap_enabled false after migration")
	}
}
