// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/axtree.go
// Summary: Converts a flat list of chromedp accessibility nodes into
//          the intermediate Document model used by the renderer.

package texelbrowse

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
)

// interactiveRoles are AX roles that represent user-interactive elements.
var interactiveRoles = map[string]bool{
	"link":        true,
	"button":      true,
	"textbox":     true,
	"searchbox":   true,
	"checkbox":    true,
	"radiobutton": true,
	"combobox":    true,
	"listbox":     true,
	"menuitem":    true,
	"tab":         true,
	"switch":      true,
	"slider":      true,
	"spinbutton":  true,
}

// BuildDocument converts a flat list of AX nodes into a Document model.
// Ignored nodes and nodes with neither role nor name are skipped.
func BuildDocument(axNodes []*accessibility.Node) *Document {
	doc := &Document{
		ByID: make(map[string]*DocNode),
	}

	for _, ax := range axNodes {
		if ax.Ignored {
			continue
		}

		role := extractValue(ax.Role)
		name := extractValue(ax.Name)

		// Skip nodes with empty role AND empty name.
		if role == "" && name == "" {
			continue
		}

		id := string(ax.NodeID)

		dn := &DocNode{
			ID:            id,
			Role:          role,
			Name:          name,
			Value:         extractValue(ax.Value),
			Description:   extractValue(ax.Description),
			ParentID:      string(ax.ParentID),
			Interactive:   interactiveRoles[role],
			Properties:    make(map[string]string),
			BackendNodeID: int64(ax.BackendDOMNodeID),
		}

		// Convert child IDs.
		for _, cid := range ax.ChildIDs {
			dn.Children = append(dn.Children, string(cid))
		}

		// Extract properties, looking for heading level.
		for _, prop := range ax.Properties {
			val := extractValue(prop.Value)
			dn.Properties[string(prop.Name)] = val

			if string(prop.Name) == "level" {
				if lvl, err := strconv.Atoi(val); err == nil {
					dn.Level = lvl
				}
			}
		}

		// Detect heading level from role name as fallback (e.g. "heading").
		// The level property is authoritative when present.
		if strings.HasPrefix(role, "heading") && dn.Level == 0 {
			dn.Level = 1 // default to h1 when role is heading but no level property
		}

		doc.Nodes = append(doc.Nodes, dn)
		doc.ByID[id] = dn
	}

	return doc
}

// extractValue gets the string content from an AX Value.
// The inner Value field is a jsontext.Value (raw JSON bytes); for strings
// it contains JSON like `"heading"` which must be unquoted.
func extractValue(v *accessibility.Value) string {
	if v == nil {
		return ""
	}
	raw := []byte(v.Value)
	if len(raw) == 0 {
		return ""
	}

	// Try to unmarshal as a JSON string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// For non-string JSON (numbers, booleans), trim whitespace and return
	// the raw representation.
	return strings.TrimSpace(string(raw))
}
