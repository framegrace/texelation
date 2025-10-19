// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/theme/color.go
// Summary: Implements color capabilities for the theme subsystem.
// Usage: Accessed by both server and client when reading color from theme configurations.
// Notes: Ensures user theme files always contain required defaults.

package theme

import (
	"encoding/json"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"strconv"
	"strings"
)

// HexColor is a string type for hex color codes, e.g., "#RRGGBB".
type HexColor string

// ToTcell converts a HexColor string to a tcell.Color.
// It returns tcell.ColorDefault if the hex string is invalid.
func (hc HexColor) ToTcell() tcell.Color {
	s := strings.TrimPrefix(string(hc), "#")
	if len(s) != 6 {
		return tcell.ColorDefault
	}
	r, errR := strconv.ParseInt(s[0:2], 16, 32)
	g, errG := strconv.ParseInt(s[2:4], 16, 32)
	b, errB := strconv.ParseInt(s[4:6], 16, 32)
	if errR != nil || errG != nil || errB != nil {
		return tcell.ColorDefault
	}
	return tcell.NewRGBColor(int32(r), int32(g), int32(b))
}

// FromTcell converts a tcell.Color to a HexColor string.
func FromTcell(c tcell.Color) HexColor {
	r, g, b := c.RGB()
	return HexColor(fmt.Sprintf("#%02x%02x%02x", r, g, b))
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (hc *HexColor) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*hc = HexColor(s)
	return nil
}

// MarshalJSON implements the json.Marshaler interface.
func (hc HexColor) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(hc))
}
