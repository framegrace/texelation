// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/document.go
// Summary: Intermediate document model representing a parsed accessibility
//          tree as a flat list of semantically typed nodes.

package texelbrowse

// DisplayMode controls how the document is presented.
type DisplayMode int

const (
	// ModeReading renders the page as a linear text document.
	ModeReading DisplayMode = iota
	// ModeForm emphasises interactive controls for form-heavy pages.
	ModeForm
)

// DocNode is a single element in the document model, converted from an
// accessibility tree node.
type DocNode struct {
	ID            string
	Role          string
	Name          string
	Value         string
	Description   string
	Level         int               // Heading level (1-6), zero if not a heading.
	Interactive   bool              // True for clickable/typeable elements.
	Children      []string          // Child node IDs in document order.
	ParentID      string
	Properties    map[string]string // Remaining AX properties as key/value pairs.
	BackendNodeID int64             // CDP BackendDOMNodeID for dispatching actions.
}

// Document is the intermediate model between the raw accessibility tree
// and the rendered widget layout.
type Document struct {
	Nodes []*DocNode
	ByID  map[string]*DocNode
	URL   string
	Title string
}

// SuggestedMode returns ModeForm if more than 30% of meaningful nodes
// are interactive, otherwise ModeReading.
func (d *Document) SuggestedMode() DisplayMode {
	if len(d.Nodes) == 0 {
		return ModeReading
	}
	interactive := 0
	for _, n := range d.Nodes {
		if n.Interactive {
			interactive++
		}
	}
	ratio := float64(interactive) / float64(len(d.Nodes))
	if ratio > 0.3 {
		return ModeForm
	}
	return ModeReading
}

// InteractiveNodes returns all interactive nodes in document order.
func (d *Document) InteractiveNodes() []*DocNode {
	var out []*DocNode
	for _, n := range d.Nodes {
		if n.Interactive {
			out = append(out, n)
		}
	}
	return out
}
