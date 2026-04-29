// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestWipeResetStatePaths_WipesD2Sessions is the Plan D2 17.E
// regression: --reset-state must wipe ~/.texelation/sessions/ (the
// Plan D2 atomicjson session-state directory) so a future refactor
// that drops this from the wipe list cannot silently leave cross-
// restart viewport state on disk.
func TestWipeResetStatePaths_WipesD2Sessions(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	sessionsDir := filepath.Join(configDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-populate a fake stored session.
	if err := os.WriteFile(filepath.Join(sessionsDir, "deadbeefdeadbeefdeadbeefdeadbeef.json"), []byte(`{"schemaVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate Plan D client state dir.
	clientStateDir := filepath.Join(root, "client")
	if err := os.MkdirAll(clientStateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clientStateDir, "session.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate a fake socket file.
	socketPath := filepath.Join(root, "test.sock")
	if err := os.WriteFile(socketPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	removed := wipeResetStatePaths(&stderr, &stdout, configDir, clientStateDir, socketPath, nil)

	// Verify ConfigDir is gone (which includes sessions/).
	if _, err := os.Stat(configDir); !os.IsNotExist(err) {
		t.Fatalf("configDir still exists after wipe: stat err=%v", err)
	}
	if _, err := os.Stat(sessionsDir); !os.IsNotExist(err) {
		t.Fatalf("sessionsDir still exists after wipe: stat err=%v", err)
	}

	// Verify client state dir is gone.
	if _, err := os.Stat(clientStateDir); !os.IsNotExist(err) {
		t.Fatalf("clientStateDir still exists after wipe: stat err=%v", err)
	}

	// Verify socket is gone.
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after wipe: stat err=%v", err)
	}

	if removed < 3 {
		t.Errorf("expected >=3 items removed, got %d (stdout: %q)", removed, stdout.String())
	}
}

// TestWipeResetStatePaths_HandlesMissingPaths confirms the wipe
// tolerates each input path being absent (idempotent re-run case).
func TestWipeResetStatePaths_HandlesMissingPaths(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "absent-config")
	clientStateDir := filepath.Join(root, "absent-client")
	socketPath := filepath.Join(root, "absent.sock")

	var stdout, stderr bytes.Buffer
	removed := wipeResetStatePaths(&stderr, &stdout, configDir, clientStateDir, socketPath, nil)

	// RemoveAll on non-existent dir returns nil (so configDir's
	// removal counts), but Remove on non-existent socket returns
	// an error (so socket doesn't count). Just assert no panic
	// and removed >= 0.
	if removed < 0 {
		t.Fatalf("removed < 0: %d", removed)
	}
}

// TestWipeResetStatePaths_RemovesLegacyFiles verifies legacy per-pane
// files are removed.
func TestWipeResetStatePaths_RemovesLegacyFiles(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	legacy1 := filepath.Join(root, ".texel-env-pane1")
	legacy2 := filepath.Join(root, ".texel-history-pane1")
	for _, f := range []string{legacy1, legacy2} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	wipeResetStatePaths(&stderr, &stdout, configDir, "", "", []string{legacy1, legacy2})

	for _, f := range []string{legacy1, legacy2} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("legacy file %s still exists after wipe: %v", f, err)
		}
	}
}
