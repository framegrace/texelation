// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/mapper.go
// Summary: Converts DocNode instances to texelui widgets based on their
//          accessibility role.

package texelbrowse

import (
	"strings"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// Mapper converts a Document into a list of texelui widgets by examining
// each DocNode's accessibility role.
type Mapper struct {
	onClickNode func(backendNodeID int64)
	onTypeNode  func(backendNodeID int64, text string)
}

// NewMapper creates a Mapper with the given click callback.
func NewMapper(onClickNode func(backendNodeID int64)) *Mapper {
	return &Mapper{
		onClickNode: onClickNode,
	}
}

// SetOnType registers a callback for text input submission.
func (m *Mapper) SetOnType(fn func(backendNodeID int64, text string)) {
	m.onTypeNode = fn
}

// MapDocument converts all nodes in the document into widgets. Structural
// nodes and nodes that produce no widget are omitted from the result.
func (m *Mapper) MapDocument(doc *Document) []core.Widget {
	var out []core.Widget
	for _, node := range doc.Nodes {
		w := m.mapNode(node)
		if w != nil {
			out = append(out, w)
		}
	}
	return out
}

// mapNode converts a single DocNode into a widget (or nil if skipped).
func (m *Mapper) mapNode(node *DocNode) core.Widget {
	switch node.Role {
	case "heading":
		prefix := strings.Repeat("#", node.Level)
		if prefix == "" {
			prefix = "#"
		}
		text := prefix + " " + node.Name
		lbl := widgets.NewLabel(text)
		return lbl

	case "link":
		lnk := widgets.NewLink(node.Name)
		nodeID := node.BackendNodeID
		lnk.OnClick = func() {
			if m.onClickNode != nil {
				m.onClickNode(nodeID)
			}
		}
		return lnk

	case "button":
		btn := widgets.NewButton(node.Name)
		nodeID := node.BackendNodeID
		btn.OnClick = func() {
			if m.onClickNode != nil {
				m.onClickNode(nodeID)
			}
		}
		return btn

	case "textbox", "searchbox":
		inp := widgets.NewInput()
		inp.Placeholder = node.Name
		inp.Text = node.Value
		nodeID := node.BackendNodeID
		syncToChrome := func(text string) {
			if m.onTypeNode != nil {
				m.onTypeNode(nodeID, text)
			}
		}
		// Sync value to Chrome on both Enter and when focus leaves,
		// so form fields have the correct values when a submit button
		// is clicked.
		inp.OnSubmit = syncToChrome
		inp.OnBlur = syncToChrome
		return inp

	case "checkbox":
		cb := widgets.NewCheckbox(node.Name)
		if node.Properties != nil && node.Properties["checked"] == "true" {
			cb.Checked = true
		}
		nodeID := node.BackendNodeID
		cb.OnChange = func(_ bool) {
			if m.onClickNode != nil {
				m.onClickNode(nodeID)
			}
		}
		return cb

	case "separator":
		lbl := widgets.NewLabel(strings.Repeat("\u2500", 40))
		return lbl

	case "StaticText", "paragraph", "text":
		lbl := widgets.NewLabel(node.Name)
		return lbl

	case "RootWebArea", "generic", "none", "group", "document":
		return nil

	default:
		if node.Name != "" {
			lbl := widgets.NewLabel(node.Name)
			return lbl
		}
		return nil
	}
}
