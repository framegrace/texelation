// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package texelbrowse

import (
	"testing"

	"github.com/framegrace/texelui/widgets"
)

func TestMapper_HeadingToLabel(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:    "n1",
				Role:  "heading",
				Name:  "Welcome",
				Level: 1,
			},
		},
	}

	m := NewMapper(nil)
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	lbl, ok := ws[0].(*widgets.Label)
	if !ok {
		t.Fatalf("expected *widgets.Label, got %T", ws[0])
	}
	if lbl.Text != "# Welcome" {
		t.Errorf("expected label text %q, got %q", "# Welcome", lbl.Text)
	}
	if lbl.Focusable() {
		t.Error("heading label should not be focusable")
	}
}

func TestMapper_HeadingLevel3(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:    "n1",
				Role:  "heading",
				Name:  "Sub-section",
				Level: 3,
			},
		},
	}

	m := NewMapper(nil)
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	lbl := ws[0].(*widgets.Label)
	if lbl.Text != "### Sub-section" {
		t.Errorf("expected label text %q, got %q", "### Sub-section", lbl.Text)
	}
}

func TestMapper_LinkToLinkWidget(t *testing.T) {
	var clickedID int64
	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:            "n1",
				Role:          "link",
				Name:          "About Us",
				BackendNodeID: 42,
			},
		},
	}

	m := NewMapper(func(id int64) {
		clickedID = id
	})
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	lnk, ok := ws[0].(*widgets.Link)
	if !ok {
		t.Fatalf("expected *widgets.Link, got %T", ws[0])
	}
	if lnk.Text != "About Us" {
		t.Errorf("expected link text %q, got %q", "About Us", lnk.Text)
	}

	// Trigger the OnClick callback.
	if lnk.OnClick == nil {
		t.Fatal("expected OnClick to be set")
	}
	lnk.OnClick()
	if clickedID != 42 {
		t.Errorf("expected clickedID 42, got %d", clickedID)
	}
}

func TestMapper_TextboxToInput(t *testing.T) {
	var typedID int64
	var typedText string

	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:            "n1",
				Role:          "textbox",
				Name:          "Search",
				Value:         "hello",
				BackendNodeID: 99,
			},
		},
	}

	m := NewMapper(nil)
	m.SetOnType(func(id int64, text string) {
		typedID = id
		typedText = text
	})
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	inp, ok := ws[0].(*widgets.Input)
	if !ok {
		t.Fatalf("expected *widgets.Input, got %T", ws[0])
	}
	if inp.Placeholder != "Search" {
		t.Errorf("expected placeholder %q, got %q", "Search", inp.Placeholder)
	}
	if inp.Text != "hello" {
		t.Errorf("expected text %q, got %q", "hello", inp.Text)
	}

	// Trigger submit callback.
	if inp.OnSubmit == nil {
		t.Fatal("expected OnSubmit to be set")
	}
	inp.OnSubmit("world")
	if typedID != 99 {
		t.Errorf("expected typedID 99, got %d", typedID)
	}
	if typedText != "world" {
		t.Errorf("expected typedText %q, got %q", "world", typedText)
	}
}

func TestMapper_MixedDocument(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "n1", Role: "heading", Name: "Title", Level: 1},
			{ID: "n2", Role: "StaticText", Name: "Some text here"},
			{ID: "n3", Role: "textbox", Name: "Username", BackendNodeID: 10},
			{ID: "n4", Role: "textbox", Name: "Password", BackendNodeID: 11},
			{ID: "n5", Role: "button", Name: "Submit", BackendNodeID: 12},
		},
	}

	m := NewMapper(func(_ int64) {})
	m.SetOnType(func(_ int64, _ string) {})
	ws := m.MapDocument(doc)

	if len(ws) != 5 {
		t.Fatalf("expected 5 widgets, got %d", len(ws))
	}

	// Verify types in order.
	expected := []string{
		"*widgets.Label",
		"*widgets.Label",
		"*widgets.Input",
		"*widgets.Input",
		"*widgets.Button",
	}
	for i, w := range ws {
		var got string
		switch w.(type) {
		case *widgets.Label:
			got = "*widgets.Label"
		case *widgets.Link:
			got = "*widgets.Link"
		case *widgets.Input:
			got = "*widgets.Input"
		case *widgets.Button:
			got = "*widgets.Button"
		case *widgets.Checkbox:
			got = "*widgets.Checkbox"
		default:
			got = "unknown"
		}
		if got != expected[i] {
			t.Errorf("widget[%d]: expected %s, got %s", i, expected[i], got)
		}
	}
}

func TestMapper_SkipStructuralNodes(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "n1", Role: "RootWebArea", Name: "Page"},
			{ID: "n2", Role: "generic"},
			{ID: "n3", Role: "none"},
			{ID: "n4", Role: "group"},
			{ID: "n5", Role: "document"},
		},
	}

	m := NewMapper(nil)
	ws := m.MapDocument(doc)

	if len(ws) != 0 {
		t.Errorf("expected 0 widgets for structural nodes, got %d", len(ws))
	}
}

func TestMapper_CheckboxFromProperties(t *testing.T) {
	var clickedID int64
	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:            "n1",
				Role:          "checkbox",
				Name:          "Accept terms",
				BackendNodeID: 77,
				Properties:    map[string]string{"checked": "true"},
			},
		},
	}

	m := NewMapper(func(id int64) {
		clickedID = id
	})
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	cb, ok := ws[0].(*widgets.Checkbox)
	if !ok {
		t.Fatalf("expected *widgets.Checkbox, got %T", ws[0])
	}
	if cb.Label != "Accept terms" {
		t.Errorf("expected label %q, got %q", "Accept terms", cb.Label)
	}
	if !cb.Checked {
		t.Error("expected checkbox to be checked")
	}

	// Trigger OnChange callback.
	if cb.OnChange == nil {
		t.Fatal("expected OnChange to be set")
	}
	cb.OnChange(false)
	if clickedID != 77 {
		t.Errorf("expected clickedID 77, got %d", clickedID)
	}
}

func TestMapper_SeparatorToDashLabel(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "n1", Role: "separator"},
		},
	}

	m := NewMapper(nil)
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	lbl, ok := ws[0].(*widgets.Label)
	if !ok {
		t.Fatalf("expected *widgets.Label, got %T", ws[0])
	}
	if len(lbl.Text) == 0 {
		t.Error("expected non-empty separator text")
	}
}

func TestMapper_DefaultFallbackWithName(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{ID: "n1", Role: "unknown-role", Name: "Something"},
			{ID: "n2", Role: "unknown-role", Name: ""},
		},
	}

	m := NewMapper(nil)
	ws := m.MapDocument(doc)

	// Only the first node has a Name, so only one widget.
	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	lbl, ok := ws[0].(*widgets.Label)
	if !ok {
		t.Fatalf("expected *widgets.Label, got %T", ws[0])
	}
	if lbl.Text != "Something" {
		t.Errorf("expected label text %q, got %q", "Something", lbl.Text)
	}
}

func TestMapper_ButtonOnClick(t *testing.T) {
	var clickedID int64
	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:            "n1",
				Role:          "button",
				Name:          "Login",
				BackendNodeID: 55,
			},
		},
	}

	m := NewMapper(func(id int64) {
		clickedID = id
	})
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	btn, ok := ws[0].(*widgets.Button)
	if !ok {
		t.Fatalf("expected *widgets.Button, got %T", ws[0])
	}
	if btn.Text != "Login" {
		t.Errorf("expected button text %q, got %q", "Login", btn.Text)
	}
	btn.OnClick()
	if clickedID != 55 {
		t.Errorf("expected clickedID 55, got %d", clickedID)
	}
}

func TestMapper_SearchboxToInput(t *testing.T) {
	doc := &Document{
		Nodes: []*DocNode{
			{
				ID:            "n1",
				Role:          "searchbox",
				Name:          "Find",
				Value:         "query",
				BackendNodeID: 33,
			},
		},
	}

	m := NewMapper(nil)
	ws := m.MapDocument(doc)

	if len(ws) != 1 {
		t.Fatalf("expected 1 widget, got %d", len(ws))
	}
	inp, ok := ws[0].(*widgets.Input)
	if !ok {
		t.Fatalf("expected *widgets.Input, got %T", ws[0])
	}
	if inp.Placeholder != "Find" {
		t.Errorf("expected placeholder %q, got %q", "Find", inp.Placeholder)
	}
	if inp.Text != "query" {
		t.Errorf("expected text %q, got %q", "query", inp.Text)
	}
}
