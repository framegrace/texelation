// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/palette.go
// Summary: Defines standard color palettes for the theme subsystem.
// Usage: Used by the theme engine to resolve named color references (e.g. "@blue").

package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"os"
	"path/filepath"
	"sync"
)

// Palette represents a collection of named colors.
type Palette map[string]tcell.Color

// PaletteConfig is the JSON structure for a palette file.
type PaletteConfig map[string]string

// Embed the default palettes so they are always available.
//
//go:embed palettes/*.json
var embeddedPalettes embed.FS

// CurrentPalette holds the currently active palette.
// In the future this could be dynamic, but we start with Mocha.
var (
	CurrentPalette = make(Palette)
	paletteMu      sync.RWMutex
)

// LoadPalette loads a palette by name.
// It searches in the user config directory first, then falls back to embedded defaults.
func LoadPalette(name string) error {
	// 1. Try loading from user config dir
	configDir, err := os.UserConfigDir()
	var data []byte
	
	if err == nil {
		path := filepath.Join(configDir, "texelation", "palettes", name+".json")
		if d, err := os.ReadFile(path); err == nil {
			data = d
		}
	}

	// 2. Try loading from embedded defaults if not found
	if data == nil {
		path := fmt.Sprintf("palettes/%s.json", name)
		if d, err := embeddedPalettes.ReadFile(path); err == nil {
			data = d
		}
	}

	if data == nil {
		return fmt.Errorf("palette '%s' not found", name)
	}

	return loadPaletteData(data)
}

func loadPaletteData(data []byte) error {
	var cfg PaletteConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	newPalette := make(Palette)
	for name, hex := range cfg {
		newPalette[name] = HexColor(hex).ToTcell()
	}
	
	paletteMu.Lock()
	CurrentPalette = newPalette
	paletteMu.Unlock()
	
	return nil
}

// ResolveColorName looks up a color name in the current palette.
// Returns tcell.ColorDefault if not found.
func ResolveColorName(name string) tcell.Color {
	paletteMu.RLock()
	defer paletteMu.RUnlock()
	if c, ok := CurrentPalette[name]; ok {
		return c
	}
	return tcell.ColorDefault
}
