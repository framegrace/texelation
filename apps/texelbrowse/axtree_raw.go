// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: apps/texelbrowse/axtree_raw.go
// Summary: Raw CDP accessibility tree fetcher that bypasses cdproto's strict
//          PropertyName/ValueType enums. Chrome frequently adds new AX
//          property names that cdproto does not know about, causing
//          UnmarshalJSON errors. This file defines lenient types that accept
//          any string value for property names and value types.

package texelbrowse

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/go-json-experiment/json/jsontext"
)

// rawAXValue mirrors accessibility.Value but uses plain strings for Type
// so unknown value types don't cause unmarshal errors.
// Uses jsontext.Value (not json.RawMessage) because chromedp uses jsonv2
// for unmarshalling CDP responses.
type rawAXValue struct {
	Type  string          `json:"type"`
	Value jsontext.Value  `json:"value,omitempty,omitzero"`
}

// rawAXProperty mirrors accessibility.Property but uses plain string for Name.
type rawAXProperty struct {
	Name  string      `json:"name"`
	Value *rawAXValue `json:"value"`
}

// rawAXNode mirrors accessibility.Node with lenient property types.
type rawAXNode struct {
	NodeID           string            `json:"nodeId"`
	Ignored          bool              `json:"ignored"`
	IgnoredReasons   []*rawAXProperty  `json:"ignoredReasons,omitempty,omitzero"`
	Role             *rawAXValue       `json:"role,omitempty,omitzero"`
	ChromeRole       *rawAXValue       `json:"chromeRole,omitempty,omitzero"`
	Name             *rawAXValue       `json:"name,omitempty,omitzero"`
	Description      *rawAXValue       `json:"description,omitempty,omitzero"`
	Value            *rawAXValue       `json:"value,omitempty,omitzero"`
	Properties       []*rawAXProperty  `json:"properties,omitempty,omitzero"`
	ParentID         string            `json:"parentId,omitempty,omitzero"`
	ChildIDs         []string          `json:"childIds,omitempty,omitzero"`
	BackendDOMNodeID cdp.BackendNodeID `json:"backendDOMNodeId,omitempty,omitzero"`
	FrameID          cdp.FrameID       `json:"frameId,omitempty,omitzero"`
}

// rawGetFullAXTreeReturns is the result struct for Accessibility.getFullAXTree.
type rawGetFullAXTreeReturns struct {
	Nodes []*rawAXNode `json:"nodes"`
}

// fetchRawAXTree calls Accessibility.getFullAXTree using lenient types
// that tolerate unknown property names and value types.
func fetchRawAXTree(ctx context.Context) ([]*rawAXNode, error) {
	var res rawGetFullAXTreeReturns
	if err := cdp.Execute(ctx, "Accessibility.getFullAXTree", nil, &res); err != nil {
		return nil, err
	}
	return res.Nodes, nil
}

// extractRawValue gets the string content from a rawAXValue.
func extractRawValue(v *rawAXValue) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	raw := []byte(v.Value)
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// For non-string JSON (numbers, booleans), return raw representation.
	return strings.TrimSpace(string(raw))
}

// buildDocumentFromRaw converts raw AX nodes into a Document model.
func buildDocumentFromRaw(nodes []*rawAXNode) *Document {
	doc := &Document{
		ByID: make(map[string]*DocNode),
	}

	for _, ax := range nodes {
		if ax.Ignored {
			continue
		}

		role := extractRawValue(ax.Role)
		name := extractRawValue(ax.Name)

		if role == "" && name == "" {
			continue
		}

		dn := &DocNode{
			ID:            ax.NodeID,
			Role:          role,
			Name:          name,
			Value:         extractRawValue(ax.Value),
			Description:   extractRawValue(ax.Description),
			ParentID:      ax.ParentID,
			Interactive:   interactiveRoles[role],
			Properties:    make(map[string]string),
			BackendNodeID: int64(ax.BackendDOMNodeID),
		}

		for _, cid := range ax.ChildIDs {
			dn.Children = append(dn.Children, cid)
		}

		for _, prop := range ax.Properties {
			val := extractRawValue(prop.Value)
			dn.Properties[prop.Name] = val

			if prop.Name == "level" {
				if lvl, err := strconv.Atoi(val); err == nil {
					dn.Level = lvl
				}
			}
		}

		if role == "heading" && dn.Level == 0 {
			dn.Level = 1
		}

		doc.Nodes = append(doc.Nodes, dn)
		doc.ByID[dn.ID] = dn
	}

	return doc
}
