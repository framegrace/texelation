// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelbrowse

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/go-json-experiment/json/jsontext"
)

// makeAXValue builds an accessibility.Value whose inner Value field
// contains the JSON encoding of s.
func makeAXValue(s string) *accessibility.Value {
	raw, _ := json.Marshal(s)
	return &accessibility.Value{
		Value: jsontext.Value(raw),
	}
}

// makeAXNode constructs a synthetic accessibility.Node for testing.
func makeAXNode(id, parentID, role, name string, ignored bool, childIDs ...string) *accessibility.Node {
	n := &accessibility.Node{
		NodeID:  accessibility.NodeID(id),
		Ignored: ignored,
	}
	if parentID != "" {
		n.ParentID = accessibility.NodeID(parentID)
	}
	if role != "" {
		n.Role = makeAXValue(role)
	}
	if name != "" {
		n.Name = makeAXValue(name)
	}
	for _, cid := range childIDs {
		n.ChildIDs = append(n.ChildIDs, accessibility.NodeID(cid))
	}
	return n
}

// withBackendNodeID sets BackendDOMNodeID on a node and returns it for chaining.
func withBackendNodeID(n *accessibility.Node, id int64) *accessibility.Node {
	n.BackendDOMNodeID = cdp.BackendNodeID(id)
	return n
}

// withProperty adds a named property to a node and returns it for chaining.
func withProperty(n *accessibility.Node, name, value string) *accessibility.Node {
	raw, _ := json.Marshal(value)
	n.Properties = append(n.Properties, &accessibility.Property{
		Name:  accessibility.PropertyName(name),
		Value: &accessibility.Value{Value: jsontext.Value(raw)},
	})
	return n
}

// withValue sets the Value field on a node and returns it for chaining.
func withValue(n *accessibility.Node, val string) *accessibility.Node {
	n.Value = makeAXValue(val)
	return n
}

// withDescription sets the Description field and returns the node for chaining.
func withDescription(n *accessibility.Node, desc string) *accessibility.Node {
	n.Description = makeAXValue(desc)
	return n
}

func TestDocumentModel_FromAXNodes(t *testing.T) {
	nodes := []*accessibility.Node{
		makeAXNode("root", "", "WebArea", "Test Page", false, "h1", "link1"),
		withProperty(
			makeAXNode("h1", "root", "heading", "Welcome", false),
			"level", "1",
		),
		withBackendNodeID(
			makeAXNode("link1", "root", "link", "Click me", false),
			42,
		),
	}

	doc := BuildDocument(nodes)

	if len(doc.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(doc.Nodes))
	}

	// Verify root node.
	root := doc.ByID["root"]
	if root == nil {
		t.Fatal("root node not found in ByID")
	}
	if root.Role != "WebArea" {
		t.Errorf("root role = %q, want %q", root.Role, "WebArea")
	}
	if root.Name != "Test Page" {
		t.Errorf("root name = %q, want %q", root.Name, "Test Page")
	}
	if root.Interactive {
		t.Error("root should not be interactive")
	}
	if len(root.Children) != 2 {
		t.Errorf("root children = %d, want 2", len(root.Children))
	}

	// Verify heading.
	h1 := doc.ByID["h1"]
	if h1 == nil {
		t.Fatal("h1 node not found in ByID")
	}
	if h1.Role != "heading" {
		t.Errorf("h1 role = %q, want %q", h1.Role, "heading")
	}
	if h1.Name != "Welcome" {
		t.Errorf("h1 name = %q, want %q", h1.Name, "Welcome")
	}
	if h1.Level != 1 {
		t.Errorf("h1 level = %d, want 1", h1.Level)
	}
	if h1.Interactive {
		t.Error("heading should not be interactive")
	}
	if h1.ParentID != "root" {
		t.Errorf("h1 parent = %q, want %q", h1.ParentID, "root")
	}

	// Verify link.
	link := doc.ByID["link1"]
	if link == nil {
		t.Fatal("link1 node not found in ByID")
	}
	if !link.Interactive {
		t.Error("link should be interactive")
	}
	if link.BackendNodeID != 42 {
		t.Errorf("link BackendNodeID = %d, want 42", link.BackendNodeID)
	}
}

func TestDocumentModel_ModeDetection(t *testing.T) {
	t.Run("reading mode (mostly text)", func(t *testing.T) {
		// 10 nodes: 2 interactive (20%) -> reading mode.
		nodes := []*accessibility.Node{
			makeAXNode("1", "", "WebArea", "Page", false),
			makeAXNode("2", "1", "heading", "Title", false),
			makeAXNode("3", "1", "paragraph", "Text 1", false),
			makeAXNode("4", "1", "paragraph", "Text 2", false),
			makeAXNode("5", "1", "paragraph", "Text 3", false),
			makeAXNode("6", "1", "paragraph", "Text 4", false),
			makeAXNode("7", "1", "paragraph", "Text 5", false),
			makeAXNode("8", "1", "paragraph", "Text 6", false),
			makeAXNode("9", "1", "link", "A link", false),
			makeAXNode("10", "1", "button", "Submit", false),
		}
		doc := BuildDocument(nodes)
		if doc.SuggestedMode() != ModeReading {
			t.Errorf("expected ModeReading, got ModeForm")
		}
	})

	t.Run("form mode (mostly interactive)", func(t *testing.T) {
		// 10 nodes: 5 interactive (50%) -> form mode.
		nodes := []*accessibility.Node{
			makeAXNode("1", "", "WebArea", "Form", false),
			makeAXNode("2", "1", "heading", "Login", false),
			makeAXNode("3", "1", "textbox", "Username", false),
			makeAXNode("4", "1", "textbox", "Password", false),
			makeAXNode("5", "1", "checkbox", "Remember me", false),
			makeAXNode("6", "1", "button", "Submit", false),
			makeAXNode("7", "1", "link", "Forgot password?", false),
			makeAXNode("8", "1", "paragraph", "Enter credentials", false),
			makeAXNode("9", "1", "paragraph", "Footer text", false),
			makeAXNode("10", "1", "paragraph", "Help", false),
		}
		doc := BuildDocument(nodes)
		if doc.SuggestedMode() != ModeForm {
			t.Errorf("expected ModeForm, got ModeReading")
		}
	})

	t.Run("boundary at 30%", func(t *testing.T) {
		// 10 nodes: 3 interactive (exactly 30%) -> reading mode (not >30%).
		nodes := []*accessibility.Node{
			makeAXNode("1", "", "WebArea", "Page", false),
			makeAXNode("2", "1", "heading", "Title", false),
			makeAXNode("3", "1", "paragraph", "Text 1", false),
			makeAXNode("4", "1", "paragraph", "Text 2", false),
			makeAXNode("5", "1", "paragraph", "Text 3", false),
			makeAXNode("6", "1", "paragraph", "Text 4", false),
			makeAXNode("7", "1", "paragraph", "Text 5", false),
			makeAXNode("8", "1", "link", "Link 1", false),
			makeAXNode("9", "1", "button", "Button", false),
			makeAXNode("10", "1", "textbox", "Input", false),
		}
		doc := BuildDocument(nodes)
		if doc.SuggestedMode() != ModeReading {
			t.Errorf("expected ModeReading at exactly 30%%, got ModeForm")
		}
	})

	t.Run("empty document", func(t *testing.T) {
		doc := BuildDocument(nil)
		if doc.SuggestedMode() != ModeReading {
			t.Errorf("expected ModeReading for empty doc, got ModeForm")
		}
	})
}

func TestDocumentModel_IgnoredNodes(t *testing.T) {
	nodes := []*accessibility.Node{
		makeAXNode("1", "", "WebArea", "Page", false, "2", "3", "4"),
		makeAXNode("2", "1", "heading", "Visible", false),
		makeAXNode("3", "1", "paragraph", "Hidden", true),  // ignored
		makeAXNode("4", "1", "link", "Also visible", false),
	}

	doc := BuildDocument(nodes)

	if len(doc.Nodes) != 3 {
		t.Fatalf("expected 3 nodes (1 ignored), got %d", len(doc.Nodes))
	}
	if doc.ByID["3"] != nil {
		t.Error("ignored node should not appear in ByID")
	}

	// Verify the non-ignored nodes are present.
	for _, id := range []string{"1", "2", "4"} {
		if doc.ByID[id] == nil {
			t.Errorf("expected node %q in ByID", id)
		}
	}
}

func TestDocumentModel_HeadingLevels(t *testing.T) {
	nodes := []*accessibility.Node{
		makeAXNode("root", "", "WebArea", "Page", false),
		withProperty(makeAXNode("h1", "root", "heading", "H1", false), "level", "1"),
		withProperty(makeAXNode("h2", "root", "heading", "H2", false), "level", "2"),
		withProperty(makeAXNode("h3", "root", "heading", "H3", false), "level", "3"),
		withProperty(makeAXNode("h6", "root", "heading", "H6", false), "level", "6"),
		// Heading with no level property defaults to 1.
		makeAXNode("h-no-level", "root", "heading", "NoLevel", false),
	}

	doc := BuildDocument(nodes)

	tests := []struct {
		id    string
		level int
	}{
		{"h1", 1},
		{"h2", 2},
		{"h3", 3},
		{"h6", 6},
		{"h-no-level", 1},
	}

	for _, tt := range tests {
		n := doc.ByID[tt.id]
		if n == nil {
			t.Errorf("node %q not found", tt.id)
			continue
		}
		if n.Level != tt.level {
			t.Errorf("node %q: level = %d, want %d", tt.id, n.Level, tt.level)
		}
	}
}

func TestDocumentModel_ValueAndDescription(t *testing.T) {
	node := withDescription(
		withValue(
			makeAXNode("tb", "", "textbox", "Email", false),
			"user@example.com",
		),
		"Enter your email address",
	)

	doc := BuildDocument([]*accessibility.Node{node})

	if len(doc.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(doc.Nodes))
	}
	dn := doc.Nodes[0]
	if dn.Value != "user@example.com" {
		t.Errorf("value = %q, want %q", dn.Value, "user@example.com")
	}
	if dn.Description != "Enter your email address" {
		t.Errorf("description = %q, want %q", dn.Description, "Enter your email address")
	}
}

func TestDocumentModel_InteractiveNodes(t *testing.T) {
	nodes := []*accessibility.Node{
		makeAXNode("1", "", "WebArea", "Page", false),
		makeAXNode("2", "1", "paragraph", "Text", false),
		makeAXNode("3", "1", "link", "Link A", false),
		makeAXNode("4", "1", "paragraph", "More text", false),
		makeAXNode("5", "1", "button", "Click", false),
		makeAXNode("6", "1", "textbox", "Input", false),
	}

	doc := BuildDocument(nodes)
	interactive := doc.InteractiveNodes()

	if len(interactive) != 3 {
		t.Fatalf("expected 3 interactive nodes, got %d", len(interactive))
	}

	// Verify document order is preserved.
	wantIDs := []string{"3", "5", "6"}
	for i, n := range interactive {
		if n.ID != wantIDs[i] {
			t.Errorf("interactive[%d].ID = %q, want %q", i, n.ID, wantIDs[i])
		}
	}
}

func TestDocumentModel_SkipEmptyRoleAndName(t *testing.T) {
	nodes := []*accessibility.Node{
		makeAXNode("1", "", "WebArea", "Page", false),
		makeAXNode("2", "1", "", "", false), // no role, no name -> skip
		makeAXNode("3", "1", "link", "", false), // has role -> keep
		makeAXNode("4", "1", "", "Some text", false), // has name -> keep
	}

	doc := BuildDocument(nodes)

	if len(doc.Nodes) != 3 {
		t.Fatalf("expected 3 nodes (1 skipped), got %d", len(doc.Nodes))
	}
	if doc.ByID["2"] != nil {
		t.Error("node with empty role and name should be skipped")
	}
}

func TestDocumentModel_Properties(t *testing.T) {
	node := withProperty(
		withProperty(
			makeAXNode("cb", "", "checkbox", "Agree", false),
			"checked", "true",
		),
		"disabled", "false",
	)

	doc := BuildDocument([]*accessibility.Node{node})
	dn := doc.ByID["cb"]
	if dn == nil {
		t.Fatal("node not found")
	}

	if dn.Properties["checked"] != "true" {
		t.Errorf("checked = %q, want %q", dn.Properties["checked"], "true")
	}
	if dn.Properties["disabled"] != "false" {
		t.Errorf("disabled = %q, want %q", dn.Properties["disabled"], "false")
	}
}
