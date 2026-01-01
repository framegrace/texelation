// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/registry/manifest.go
// Summary: Defines app manifest structure for the registry system.
// Usage: Apps provide a manifest.json file describing their metadata.

package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppType specifies how the app should be launched.
type AppType string

const (
	// AppTypeBuiltIn uses a factory registered in the server
	AppTypeBuiltIn AppType = "built-in"

	// AppTypeWrapper wraps another app with custom parameters
	// Example: htop = texelterm with "htop" command
	AppTypeWrapper AppType = "wrapper"

	// AppTypeExternal launches an external binary (future)
	AppTypeExternal AppType = "external"
)

// Manifest describes an application's metadata and capabilities.
type Manifest struct {
	// Name is the unique identifier for this app (e.g., "htop", "mycalc")
	Name string `json:"name"`

	// DisplayName is the human-readable name shown in the launcher
	DisplayName string `json:"displayName"`

	// Description provides a brief explanation of what the app does
	Description string `json:"description"`

	// Version follows semantic versioning (e.g., "1.0.0")
	Version string `json:"version"`

	// Type specifies how to launch this app (built-in, wrapper, external)
	// Defaults to "external" for backward compatibility
	Type AppType `json:"type,omitempty"`

	// --- For wrapper apps ---

	// Wraps specifies which built-in app to wrap (e.g., "texelterm")
	// Only used when Type is "wrapper"
	Wraps string `json:"wraps,omitempty"`

	// Command is the command to run (for texelterm wrappers)
	// Example: "htop", "/usr/bin/vim", "python3 script.py"
	Command string `json:"command,omitempty"`

	// Args are additional arguments to pass
	Args []string `json:"args,omitempty"`

	// Env are environment variables to set
	Env map[string]string `json:"env,omitempty"`

	// --- For external apps ---

	// Binary is the path to the executable relative to the manifest directory
	// Only used when Type is "external"
	Binary string `json:"binary,omitempty"`

	// --- Common metadata ---

	// Icon is a single emoji or short string for visual identification
	Icon string `json:"icon"`

	// Category groups apps in the launcher (e.g., "system", "utility", "dev")
	Category string `json:"category"`

	// Author is the creator's name or organization
	Author string `json:"author,omitempty"`

	// Tags are searchable keywords
	Tags []string `json:"tags,omitempty"`

	// Homepage is a URL for more information
	Homepage string `json:"homepage,omitempty"`

	// ThemeSchema defines which theme sections/keys this app can override.
	// Keys are section names (e.g., "ui", "selection"), values are lists of
	// field names within that section (e.g., ["bg.surface", "text.primary"]).
	// Used by the config editor to show theme override fields.
	ThemeSchema ThemeSchema `json:"theme_schema,omitempty"`
}

// ThemeSchema maps section names to lists of overridable field names.
type ThemeSchema map[string][]string

// LoadManifest reads and parses a manifest.json file from the given directory.
func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Validate required fields
	if m.Name == "" {
		return nil, fmt.Errorf("manifest missing required field: name")
	}
	if m.DisplayName == "" {
		return nil, fmt.Errorf("manifest missing required field: displayName")
	}

	// Default type to external if not specified
	if m.Type == "" {
		m.Type = AppTypeExternal
	}

	return &m, nil
}

// Validate checks that the manifest is well-formed.
func (m *Manifest) Validate(dir string) error {
	if m.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if m.DisplayName == "" {
		return fmt.Errorf("displayName cannot be empty")
	}

	// Type-specific validation
	switch m.Type {
	case AppTypeWrapper:
		if m.Wraps == "" {
			return fmt.Errorf("wrapper app must specify 'wraps' field")
		}
		if m.Command == "" {
			return fmt.Errorf("wrapper app must specify 'command' field")
		}

	case AppTypeExternal:
		if m.Binary == "" {
			return fmt.Errorf("external app must specify 'binary' field")
		}
		// Check if binary exists
		binaryPath := filepath.Join(dir, m.Binary)
		if _, err := os.Stat(binaryPath); err != nil {
			return fmt.Errorf("binary not found: %s (%w)", m.Binary, err)
		}

	case AppTypeBuiltIn:
		// Built-ins don't need validation as they're registered in code
		// This type is only used internally by the registry

	default:
		return fmt.Errorf("unknown app type: %s", m.Type)
	}

	return nil
}

// BinaryPath returns the absolute path to the app's binary.
func (m *Manifest) BinaryPath(dir string) string {
	return filepath.Join(dir, m.Binary)
}
