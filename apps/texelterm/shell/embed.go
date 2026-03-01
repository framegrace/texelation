// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package shell embeds canonical shell integration scripts and installs/updates
// them in the user's config directory. Each script carries a version comment;
// on every shell launch the installed version is compared against the embedded
// version and overwritten if outdated or missing.
package shell

import (
	"bufio"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed bash.sh zsh.sh fish.fish
var scripts embed.FS

// CurrentVersion is the version stamped into the embedded scripts.
// Bump this (and the comment in each .sh/.fish file) whenever the
// scripts change in a way that requires re-installation.
const CurrentVersion = 10

// scriptFiles maps installed filenames to their embedded source names.
var scriptFiles = []string{"bash.sh", "zsh.sh", "fish.fish"}

// EnsureInstalled checks each script in configDir, writes or updates if
// missing or version < CurrentVersion. Generates bash-wrapper.sh dynamically.
func EnsureInstalled(configDir string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("shell: create config dir: %w", err)
	}

	for _, name := range scriptFiles {
		dst := filepath.Join(configDir, name)
		if needsUpdate(dst) {
			data, err := scripts.ReadFile(name)
			if err != nil {
				return fmt.Errorf("shell: read embedded %s: %w", name, err)
			}
			if err := os.WriteFile(dst, data, 0644); err != nil {
				return fmt.Errorf("shell: write %s: %w", dst, err)
			}
			log.Printf("[SHELL] Installed %s (version %d)", dst, CurrentVersion)
		}
	}

	// Generate bash-wrapper.sh dynamically (contains home-dir path).
	// Only regenerated when missing or version is outdated.
	wrapperPath := filepath.Join(configDir, "bash-wrapper.sh")
	if needsUpdate(wrapperPath) {
		if err := generateBashWrapper(configDir, wrapperPath); err != nil {
			return err
		}
	}

	return nil
}

// needsUpdate returns true if the installed script is missing or has a
// version older than CurrentVersion.
func needsUpdate(path string) bool {
	v, err := installedVersion(path)
	if err != nil {
		return true // missing or unreadable
	}
	return v < CurrentVersion
}

// installedVersion reads the first line of path and extracts the
// TEXEL_SHELL_INTEGRATION_VERSION value. Returns 0 and an error if
// the file is missing, unreadable, or lacks a version comment.
func installedVersion(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0, fmt.Errorf("empty file")
	}
	return parseVersionLine(scanner.Text())
}

// parseVersionLine extracts the version number from a line like:
//
//	# TEXEL_SHELL_INTEGRATION_VERSION=9
func parseVersionLine(line string) (int, error) {
	const prefix = "TEXEL_SHELL_INTEGRATION_VERSION="
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return 0, fmt.Errorf("no version marker")
	}
	numStr := strings.TrimSpace(line[idx+len(prefix):])
	v, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("bad version number %q: %w", numStr, err)
	}
	return v, nil
}

// generateBashWrapper writes bash-wrapper.sh which sources the integration
// script followed by the user's .bashrc. The path is derived from configDir
// rather than hardcoded to a specific home directory.
//
// Includes a version stamp so needsUpdate() can skip regeneration on
// subsequent terminal spawns. Uses atomic write (temp file + rename) so
// concurrent spawns during a version bump never see a truncated wrapper.
func generateBashWrapper(configDir, wrapperPath string) error {
	integrationPath := filepath.Join(configDir, "bash.sh")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("shell: detect home dir: %w", err)
	}
	bashrc := filepath.Join(homeDir, ".bashrc")

	content := fmt.Sprintf(`#!/bin/bash
# TEXEL_SHELL_INTEGRATION_VERSION=%d
# Texelterm shell integration wrapper (auto-generated, do not edit)
# Loads integration first, then user's bashrc
[[ -f %s ]] && source %s
[[ -f %s ]] && source %s
`, CurrentVersion, integrationPath, integrationPath, bashrc, bashrc)

	// Atomic write: write to temp file then rename, so concurrent bash
	// processes never see a truncated/partial wrapper.
	tmp, err := os.CreateTemp(configDir, "bash-wrapper-*.tmp")
	if err != nil {
		return fmt.Errorf("shell: create temp wrapper: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("shell: write temp wrapper: %w", err)
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("shell: chmod temp wrapper: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("shell: close temp wrapper: %w", err)
	}
	if err := os.Rename(tmpPath, wrapperPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("shell: rename wrapper: %w", err)
	}
	log.Printf("[SHELL] Installed %s (version %d)", wrapperPath, CurrentVersion)
	return nil
}
