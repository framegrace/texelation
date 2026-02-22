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

	"github.com/chromedp/cdproto/cdp"
)

// rawAXValue mirrors accessibility.Value but uses plain strings for Type
// so unknown value types don't cause unmarshal errors.
type rawAXValue struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value,omitempty"`
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
	IgnoredReasons   []*rawAXProperty  `json:"ignoredReasons,omitempty"`
	Role             *rawAXValue       `json:"role,omitempty"`
	ChromeRole       *rawAXValue       `json:"chromeRole,omitempty"`
	Name             *rawAXValue       `json:"name,omitempty"`
	Description      *rawAXValue       `json:"description,omitempty"`
	Value            *rawAXValue       `json:"value,omitempty"`
	Properties       []*rawAXProperty  `json:"properties,omitempty"`
	ParentID         string            `json:"parentId,omitempty"`
	ChildIDs         []string          `json:"childIds,omitempty"`
	BackendDOMNodeID cdp.BackendNodeID `json:"backendDOMNodeId,omitempty"`
	FrameID          cdp.FrameID       `json:"frameId,omitempty"`
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
	var s string
	if err := json.Unmarshal(v.Value, &s); err == nil {
		return s
	}
	// For non-string JSON (numbers, booleans), return raw representation.
	return string(v.Value)
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
