// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/mapper.go
// Summary: Converts DocNode instances to texelui widgets based on their
//          accessibility role.

package texelbrowse

import (
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// contentOnlyRoles lists roles that produce a Label from their Name and
// nothing more. When such a child's Name duplicates the parent's consumed
// name, it is skipped to avoid text duplication (e.g. link > StaticText).
var contentOnlyRoles = map[string]bool{
	"StaticText": true,
	"text":       true,
}

// sectionTier classifies landmark and content grouping roles.
type sectionTier int

const (
	tierNone    sectionTier = iota
	tierLandmark
	tierContent
)

// sectionRoles maps accessibility roles to their grouping tier.
var sectionRoles = map[string]sectionTier{
	"navigation": tierLandmark, "main": tierLandmark, "banner": tierLandmark,
	"contentinfo": tierLandmark, "complementary": tierLandmark, "form": tierLandmark,
	"article": tierContent, "section": tierContent, "region": tierContent, "list": tierContent,
}

// sectionDefaultTitles provides fallback titles when a section node has no Name.
var sectionDefaultTitles = map[string]string{
	"navigation": "Navigation", "main": "Main", "banner": "Banner",
	"contentinfo": "Footer", "complementary": "Sidebar", "form": "Form",
	"article": "Article", "section": "Section", "region": "Region", "list": "List",
}

// sectionTitle returns the display title for a section node.
func sectionTitle(node *DocNode) string {
	if node.Name != "" {
		return node.Name
	}
	if t, ok := sectionDefaultTitles[node.Role]; ok {
		return t
	}
	return node.Role
}

// Border color constants: muted blue for landmarks, muted green for content sections.
var (
	landmarkBorderColor = tcell.NewRGBColor(100, 140, 180)
	contentBorderColor  = tcell.NewRGBColor(100, 170, 120)
)

// makeSectionBorder creates a Border with a colored title wrapping a VBox child.
// Returns both the border (for adding to a parent) and the inner VBox (for adding children).
func makeSectionBorder(title string, tier sectionTier) (*widgets.Border, *widgets.VBox) {
	color := contentBorderColor
	if tier == tierLandmark {
		color = landmarkBorderColor
	}
	style := tcell.StyleDefault.Foreground(color)
	border := widgets.NewBorderWithStyle(style)
	border.Title = title
	inner := widgets.NewVBox()
	border.SetChild(inner)
	return border, inner
}

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

// MapDocument converts all nodes in the document into widgets using a DFS
// walk of the reconstructed tree. This ensures widgets appear in visual
// (document) order rather than whatever order Chrome returned nodes in.
// Structural nodes and nodes that produce no widget are omitted from the
// result.
func (m *Mapper) MapDocument(doc *Document) []core.Widget {
	if len(doc.Nodes) == 0 {
		return nil
	}
	root := doc.Nodes[0]
	visited := make(map[string]bool, len(doc.Nodes))
	var out []core.Widget
	m.walkDFS(doc, root, visited, &out)
	// Append any unvisited nodes (disconnected subtrees / safety net).
	for _, node := range doc.Nodes {
		if !visited[node.ID] {
			if w := m.mapNode(node); w != nil {
				out = append(out, w)
			}
		}
	}
	return out
}

// walkDFS performs a depth-first walk of the document tree, appending
// mapped widgets in document order. Content-only children (StaticText,
// text) whose Name duplicates the parent's are skipped to avoid the
// common AX tree pattern where a link/heading wraps a StaticText child
// carrying the same label.
func (m *Mapper) walkDFS(doc *Document, node *DocNode, visited map[string]bool, out *[]core.Widget) {
	visited[node.ID] = true
	w := m.mapNode(node)
	if w != nil {
		*out = append(*out, w)
	}
	parentConsumedName := w != nil && node.Name != ""
	for _, childID := range node.Children {
		child, ok := doc.ByID[childID]
		if !ok || visited[child.ID] {
			continue
		}
		if parentConsumedName && contentOnlyRoles[child.Role] && child.Name == node.Name {
			visited[child.ID] = true
			continue
		}
		m.walkDFS(doc, child, visited, out)
	}
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

// MapDocumentGrouped converts all nodes in the document into a nested widget
// tree where landmark and content section roles are wrapped in bordered VBox
// containers. Returns a root VBox, or nil for empty documents.
func (m *Mapper) MapDocumentGrouped(doc *Document) core.Widget {
	if len(doc.Nodes) == 0 {
		return nil
	}
	root := doc.Nodes[0]
	visited := make(map[string]bool, len(doc.Nodes))
	rootBox := widgets.NewVBox()
	m.walkDFSGrouped(doc, root, visited, rootBox)
	// Safety net: append any unvisited nodes (disconnected subtrees).
	for _, node := range doc.Nodes {
		if !visited[node.ID] {
			if w := m.mapNode(node); w != nil {
				rootBox.AddChild(w)
			}
		}
	}
	return rootBox
}

// walkDFSGrouped performs a depth-first walk building a nested VBox tree.
// Section roles create Border>VBox wrappers; other nodes are mapped with
// content-only dedup (same as walkDFS).
func (m *Mapper) walkDFSGrouped(doc *Document, node *DocNode, visited map[string]bool, target *widgets.VBox) {
	visited[node.ID] = true

	if tier, ok := sectionRoles[node.Role]; ok {
		// Section node: create a bordered container. The section's Name
		// becomes the border title — it is NOT consumed content, so children
		// with the same name are NOT deduped against it.
		title := sectionTitle(node)
		border, inner := makeSectionBorder(title, tier)
		target.AddChild(border)
		for _, childID := range node.Children {
			child, ok := doc.ByID[childID]
			if !ok || visited[child.ID] {
				continue
			}
			m.walkDFSGrouped(doc, child, visited, inner)
		}
		return
	}

	// Non-section node: map it and apply content-only dedup to children.
	w := m.mapNode(node)
	if w != nil {
		target.AddChild(w)
	}
	parentConsumedName := w != nil && node.Name != ""
	for _, childID := range node.Children {
		child, ok := doc.ByID[childID]
		if !ok || visited[child.ID] {
			continue
		}
		if parentConsumedName && contentOnlyRoles[child.Role] && child.Name == node.Name {
			visited[child.ID] = true
			continue
		}
		m.walkDFSGrouped(doc, child, visited, target)
	}
}
