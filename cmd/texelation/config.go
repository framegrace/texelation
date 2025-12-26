// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/config.go
// Summary: Standard paths for texelation configuration and runtime files.

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Paths holds standard file paths for texelation
type Paths struct {
	ConfigDir     string // ~/.texelation
	PIDPath       string // ~/.texelation/texelation.pid
	SnapshotPath  string // ~/.texelation/snapshot.json
	ServerLogPath string // ~/.texelation/server.log
	SocketPath    string // /tmp/texelation.sock (default)
}

// GetPaths returns the standard paths for texelation files
func GetPaths() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}

	configDir := filepath.Join(home, ".texelation")

	return &Paths{
		ConfigDir:     configDir,
		PIDPath:       filepath.Join(configDir, "texelation.pid"),
		SnapshotPath:  filepath.Join(configDir, "snapshot.json"),
		ServerLogPath: filepath.Join(configDir, "server.log"),
		SocketPath:    "/tmp/texelation.sock",
	}, nil
}

// EnsureConfigDir creates the configuration directory if it doesn't exist
func (p *Paths) EnsureConfigDir() error {
	return os.MkdirAll(p.ConfigDir, 0755)
}
