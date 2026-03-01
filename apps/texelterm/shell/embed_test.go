package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVersionLine(t *testing.T) {
	tests := []struct {
		line    string
		want    int
		wantErr bool
	}{
		{"# TEXEL_SHELL_INTEGRATION_VERSION=9", 9, false},
		{"# TEXEL_SHELL_INTEGRATION_VERSION=1", 1, false},
		{"# TEXEL_SHELL_INTEGRATION_VERSION=42", 42, false},
		{"# Texelterm Shell Integration for Bash", 0, true},
		{"", 0, true},
		{"# TEXEL_SHELL_INTEGRATION_VERSION=abc", 0, true},
	}
	for _, tt := range tests {
		got, err := parseVersionLine(tt.line)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseVersionLine(%q) error = %v, wantErr %v", tt.line, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseVersionLine(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestEnsureInstalled_FreshDir(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "shell-integration")

	t.Setenv("HOME", dir) // bash-wrapper.sh uses UserHomeDir

	if err := EnsureInstalled(configDir); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}

	// All scripts should exist
	for _, name := range []string{"bash.sh", "zsh.sh", "fish.fish", "bash-wrapper.sh"} {
		path := filepath.Join(configDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// Verify version in each script
	for _, name := range scriptFiles {
		path := filepath.Join(configDir, name)
		v, err := installedVersion(path)
		if err != nil {
			t.Errorf("installedVersion(%s): %v", name, err)
			continue
		}
		if v != CurrentVersion {
			t.Errorf("%s version = %d, want %d", name, v, CurrentVersion)
		}
	}
}

func TestEnsureInstalled_UpdatesOldVersion(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "shell-integration")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	// Write an old-version bash.sh
	old := "# TEXEL_SHELL_INTEGRATION_VERSION=7\n# old content\n"
	if err := os.WriteFile(filepath.Join(configDir, "bash.sh"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureInstalled(configDir); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}

	// bash.sh should be updated to CurrentVersion
	v, err := installedVersion(filepath.Join(configDir, "bash.sh"))
	if err != nil {
		t.Fatalf("installedVersion: %v", err)
	}
	if v != CurrentVersion {
		t.Errorf("bash.sh version = %d after update, want %d", v, CurrentVersion)
	}

	// Content should contain OSC 7
	data, _ := os.ReadFile(filepath.Join(configDir, "bash.sh"))
	if !strings.Contains(string(data), "OSC 7") {
		t.Error("updated bash.sh missing OSC 7 comment")
	}
}

func TestEnsureInstalled_NoDowngrade(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "shell-integration")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	// Write a future-version bash.sh
	future := "# TEXEL_SHELL_INTEGRATION_VERSION=99\n# future content\n"
	futurePath := filepath.Join(configDir, "bash.sh")
	if err := os.WriteFile(futurePath, []byte(future), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureInstalled(configDir); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}

	// bash.sh should NOT be overwritten
	data, _ := os.ReadFile(futurePath)
	if !strings.Contains(string(data), "future content") {
		t.Error("bash.sh was downgraded from future version")
	}
}

func TestEnsureInstalled_BashWrapperUsesHomeDir(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "shell-integration")

	t.Setenv("HOME", dir)

	if err := EnsureInstalled(configDir); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "bash-wrapper.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Should reference the correct config dir, not a hardcoded path
	if !strings.Contains(content, configDir) {
		t.Errorf("bash-wrapper.sh doesn't reference configDir %s", configDir)
	}

	// Should reference bashrc in the home dir
	bashrc := filepath.Join(dir, ".bashrc")
	if !strings.Contains(content, bashrc) {
		t.Errorf("bash-wrapper.sh doesn't reference %s", bashrc)
	}

	// Should be executable
	info, _ := os.Stat(filepath.Join(configDir, "bash-wrapper.sh"))
	if info.Mode()&0111 == 0 {
		t.Error("bash-wrapper.sh is not executable")
	}
}

func TestNeedsUpdate_MissingFile(t *testing.T) {
	if !needsUpdate("/nonexistent/path/script.sh") {
		t.Error("needsUpdate should return true for missing file")
	}
}

func TestNeedsUpdate_NoVersionMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sh")
	os.WriteFile(path, []byte("#!/bin/bash\necho hello\n"), 0644)

	if !needsUpdate(path) {
		t.Error("needsUpdate should return true for file without version marker")
	}
}

func TestEmbeddedScriptsHaveVersionMarker(t *testing.T) {
	for _, name := range scriptFiles {
		data, err := scripts.ReadFile(name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		lines := strings.SplitN(string(data), "\n", 2)
		v, err := parseVersionLine(lines[0])
		if err != nil {
			t.Errorf("embedded %s: no version on line 1: %v", name, err)
			continue
		}
		if v != CurrentVersion {
			t.Errorf("embedded %s version = %d, want %d", name, v, CurrentVersion)
		}
	}
}

func TestEnsureInstalled_Idempotent(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "shell-integration")
	t.Setenv("HOME", dir)

	// First install
	if err := EnsureInstalled(configDir); err != nil {
		t.Fatalf("first EnsureInstalled: %v", err)
	}

	// Record mod times
	modTimes := make(map[string]int64)
	for _, name := range scriptFiles {
		info, _ := os.Stat(filepath.Join(configDir, name))
		modTimes[name] = info.ModTime().UnixNano()
	}

	// Second install — scripts already at CurrentVersion, should not rewrite
	if err := EnsureInstalled(configDir); err != nil {
		t.Fatalf("second EnsureInstalled: %v", err)
	}

	for _, name := range scriptFiles {
		info, _ := os.Stat(filepath.Join(configDir, name))
		if info.ModTime().UnixNano() != modTimes[name] {
			t.Errorf("%s was rewritten on idempotent call", name)
		}
	}
}
