// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/snapshot_store.go
// Summary: Implements snapshot store capabilities for the server runtime.
// Usage: Used by texel-server to coordinate snapshot store when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
	"os"
	"path/filepath"
	"sync"
	"time"

	"texelation/protocol"
	"texelation/texel"
)

// SnapshotStore persists pane snapshots to disk with a content hash for integrity checks.
type SnapshotStore struct {
	path string
	mu   sync.Mutex
}

// StoredSnapshot is the serialized representation written to disk.
type StoredSnapshot struct {
	Timestamp time.Time    `json:"timestamp"`
	Hash      string       `json:"hash"`
	Panes     []StoredPane `json:"panes"`
	Tree      StoredNode   `json:"tree"`
}

// StoredNode captures the persisted tree layout.
type StoredNode struct {
	PaneIndex int          `json:"pane_index"`
	Split     string       `json:"split"`
	Ratios    []float64    `json:"ratios,omitempty"`
	Children  []StoredNode `json:"children,omitempty"`
}

// StoredPane represents a single pane's textual content.
type StoredPane struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Rows      []string               `json:"rows"`
	X         int                    `json:"x"`
	Y         int                    `json:"y"`
	Width     int                    `json:"width"`
	Height    int                    `json:"height"`
	AppType   string                 `json:"app_type,omitempty"`
	AppConfig map[string]interface{} `json:"app_config,omitempty"`
}

func NewSnapshotStore(path string) *SnapshotStore {
	return &SnapshotStore{path: path}
}

// Save writes the current snapshots to disk, computing a SHA-1 hash for integrity.
func (s *SnapshotStore) Save(capture texel.TreeCapture) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := StoredSnapshot{
		Timestamp: time.Now().UTC(),
		Panes:     make([]StoredPane, len(capture.Panes)),
	}

	hasher := sha1.New()

	for i, pane := range capture.Panes {
		id := hex.EncodeToString(pane.ID[:])
		rows := make([]string, len(pane.Buffer))
		for y, row := range pane.Buffer {
			runes := make([]rune, len(row))
			for x, cell := range row {
				if cell.Ch == 0 {
					runes[x] = ' '
				} else {
					runes[x] = cell.Ch
				}
			}
			rows[y] = string(runes)
			hasher.Write([]byte(rows[y]))
		}

		hasher.Write(pane.ID[:])
		hasher.Write([]byte(pane.Title))

		stored.Panes[i] = StoredPane{
			ID:        id,
			Title:     pane.Title,
			Rows:      rows,
			X:         pane.Rect.X,
			Y:         pane.Rect.Y,
			Width:     pane.Rect.Width,
			Height:    pane.Rect.Height,
			AppType:   pane.AppType,
			AppConfig: cloneAppConfig(pane.AppConfig),
		}
	}
	for _, pane := range capture.Panes {
		hasher.Write([]byte(pane.AppType))
		if pane.AppConfig != nil {
			if data, err := json.Marshal(pane.AppConfig); err == nil {
				hasher.Write(data)
			}
		}
	}
	hashTreeCapture(capture.Root, hasher)
	stored.Tree = storeTreeNode(capture.Root)

	stored.Hash = hex.EncodeToString(hasher.Sum(nil))

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o644)
}

// Load retrieves the most recent stored snapshot from disk.
func (s *SnapshotStore) Load() (StoredSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var stored StoredSnapshot
	data, err := os.ReadFile(s.path)
	if err != nil {
		return stored, err
	}

	if err := json.Unmarshal(data, &stored); err != nil {
		return stored, err
	}
	return stored, nil
}

func (s StoredSnapshot) ToTreeSnapshot() protocol.TreeSnapshot {
	panes := make([]protocol.PaneSnapshot, len(s.Panes))
	for i, pane := range s.Panes {
		panes[i] = pane.toProtocolPane()
	}
	snapshot := protocol.TreeSnapshot{Panes: panes}
	snapshot.Root = s.Tree.toProtocolNode()
	return snapshot
}

func (sp StoredPane) ToPaneSnapshot() texel.PaneSnapshot {
	var id [16]byte
	decoded, err := hex.DecodeString(sp.ID)
	if err == nil && len(decoded) >= 16 {
		copy(id[:], decoded[:16])
	}

	buffer := make([][]texel.Cell, len(sp.Rows))
	for i, row := range sp.Rows {
		runes := []rune(row)
		buffer[i] = make([]texel.Cell, len(runes))
		for j, ch := range runes {
			buffer[i][j] = texel.Cell{Ch: ch}
		}
	}

	return texel.PaneSnapshot{
		ID:        id,
		Title:     sp.Title,
		Buffer:    buffer,
		Rect:      texel.Rectangle{X: sp.X, Y: sp.Y, Width: sp.Width, Height: sp.Height},
		AppType:   sp.AppType,
		AppConfig: cloneAppConfig(sp.AppConfig),
	}
}

func (sp StoredPane) toProtocolPane() protocol.PaneSnapshot {
	var id [16]byte
	decoded, err := hex.DecodeString(sp.ID)
	if err == nil && len(decoded) >= 16 {
		copy(id[:], decoded[:16])
	}
	rows := make([]string, len(sp.Rows))
	copy(rows, sp.Rows)
	return protocol.PaneSnapshot{
		PaneID:    id,
		Title:     sp.Title,
		Rows:      rows,
		X:         int32(sp.X),
		Y:         int32(sp.Y),
		Width:     int32(sp.Width),
		Height:    int32(sp.Height),
		AppType:   sp.AppType,
		AppConfig: encodeStoredConfig(sp.AppConfig),
	}
}

func hashTreeCapture(node *texel.TreeNodeCapture, hasher hash.Hash) {
	if node == nil {
		hasher.Write([]byte{0xFF})
		return
	}
	_ = binary.Write(hasher, binary.LittleEndian, int32(node.PaneIndex))
	hasher.Write([]byte{byte(node.Split)})
	childCount := uint16(len(node.Children))
	_ = binary.Write(hasher, binary.LittleEndian, childCount)
	for _, ratio := range node.SplitRatios {
		_ = binary.Write(hasher, binary.LittleEndian, ratio)
	}
	for _, child := range node.Children {
		hashTreeCapture(child, hasher)
	}
}

func cloneAppConfig(cfg map[string]interface{}) map[string]interface{} {
	if cfg == nil {
		return nil
	}
	clone := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		clone[k] = v
	}
	return clone
}

func encodeStoredConfig(cfg map[string]interface{}) string {
	if cfg == nil {
		return ""
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(data)
}

func storeTreeNode(node *texel.TreeNodeCapture) StoredNode {
	if node == nil {
		return StoredNode{PaneIndex: -1, Split: "none"}
	}
	stored := StoredNode{PaneIndex: node.PaneIndex}
	if len(node.Children) == 0 {
		stored.Split = "none"
		return stored
	}
	switch node.Split {
	case texel.Vertical:
		stored.Split = "vertical"
	case texel.Horizontal:
		stored.Split = "horizontal"
	default:
		stored.Split = "none"
	}
	stored.Ratios = make([]float64, len(node.SplitRatios))
	copy(stored.Ratios, node.SplitRatios)
	stored.Children = make([]StoredNode, len(node.Children))
	for i, child := range node.Children {
		stored.Children[i] = storeTreeNode(child)
	}
	return stored
}

func (sn StoredNode) toProtocolNode() protocol.TreeNodeSnapshot {
	node := protocol.TreeNodeSnapshot{PaneIndex: int32(sn.PaneIndex)}
	switch sn.Split {
	case "vertical":
		node.Split = protocol.SplitVertical
	case "horizontal":
		node.Split = protocol.SplitHorizontal
	default:
		node.Split = protocol.SplitNone
	}
	node.SplitRatios = make([]float32, len(sn.Ratios))
	for i, ratio := range sn.Ratios {
		node.SplitRatios[i] = float32(ratio)
	}
	node.Children = make([]protocol.TreeNodeSnapshot, len(sn.Children))
	for i, child := range sn.Children {
		node.Children[i] = child.toProtocolNode()
	}
	return node
}
