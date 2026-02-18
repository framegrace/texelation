// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_sync.go
// Summary: Implements state synchronization and snapshot methods for connections.
// Usage: Used by texel-server to push state updates, pane states, and tree snapshots to clients.
// Notes: Split from connection.go for clarity; methods remain on *connection.

package server

import (
	"log"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
)

func (c *connection) sendPending() error {
	if c.awaitResume {
		return nil
	}
	pending := c.session.Pending(c.lastAcked)
	for _, diff := range pending {
		if diff.Sequence <= c.lastSent {
			continue
		}
		header := diff.Message
		header.Sequence = diff.Sequence
		header.SessionID = c.session.ID()
		if err := c.writeMessage(header, diff.Payload); err != nil {
			return err
		}
		c.lastSent = diff.Sequence
	}
	return nil
}

func (c *connection) sendStateUpdate(state texel.StatePayload) {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)

	all := make([]int32, 0, len(state.AllWorkspaces))
	for _, id := range state.AllWorkspaces {
		if id < minInt32 || id > maxInt32 {
			log.Printf("connection: workspace id %d out of int32 range; skipping", id)
			continue
		}
		all = append(all, int32(id))
	}

	workspaceID := int32(0)
	if state.WorkspaceID < minInt32 || state.WorkspaceID > maxInt32 {
		log.Printf("connection: active workspace id %d out of int32 range; defaulting to 0", state.WorkspaceID)
	} else {
		workspaceID = int32(state.WorkspaceID)
	}

	r, g, b := state.DesktopBgColor.RGB()
	update := protocol.StateUpdate{
		WorkspaceID:   workspaceID,
		AllWorkspaces: all,
		InControlMode: state.InControlMode,
		SubMode:       state.SubMode,
		ActiveTitle:   state.ActiveTitle,
		DesktopBgRGB:  colorToRGB(r, g, b),
	}
	payload, err := protocol.EncodeStateUpdate(update)
	if err != nil {
		return
	}
	if err := c.writeControlMessage(protocol.MsgStateUpdate, payload); err != nil {
		// Ignore write failures; connection loop will surface them.
	}
}

func colorToRGB(r, g, b int32) uint32 {
	return ((uint32(r) & 0xFF) << 16) | ((uint32(g) & 0xFF) << 8) | (uint32(b) & 0xFF)
}

func (c *connection) sendPaneState(id [16]byte, active, resizing bool, z int, handlesSelection bool) {
	var flags protocol.PaneStateFlags
	if active {
		flags |= protocol.PaneStateActive
	}
	if resizing {
		flags |= protocol.PaneStateResizing
	}
	if handlesSelection {
		flags |= protocol.PaneStateSelectionDelegated
	}
	payload, err := protocol.EncodePaneState(protocol.PaneState{PaneID: id, Flags: flags, ZOrder: int32(z)})
	if err != nil {
		return
	}
	_ = c.writeControlMessage(protocol.MsgPaneState, payload)
}

func (c *connection) sendPaneStateSnapshots(states []texel.PaneStateSnapshot) {
	for _, state := range states {
		c.sendPaneState(state.ID, state.Active, state.Resizing, state.ZOrder, state.HandlesMouse)
	}
}

func (c *connection) sendTreeSnapshot() {
	sink, ok := c.sink.(*DesktopSink)
	if !ok {
		return
	}
	snapshot, err := sink.Snapshot()
	if err != nil {
		return
	}
	// Always send snapshot, even if empty - client needs to clear old panes during workspace switches
	sink.Publish()
	geometrySnapshot := geometryOnlySnapshot(snapshot)

	payload, err := protocol.EncodeTreeSnapshot(geometrySnapshot)
	if err != nil {
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: c.session.ID()}
	_ = c.writeMessage(header, payload)
	states := snapshotMergedPaneStates(snapshot, sink.Desktop())
	for _, pane := range states {
		c.sendPaneState(pane.ID, pane.Active, pane.Resizing, pane.ZOrder, pane.HandlesMouse)
	}
}

func (c *connection) PaneStateChanged(id [16]byte, active bool, resizing bool, z int, handlesSelection bool) {
	c.sendPaneState(id, active, resizing, z, handlesSelection)
}

func (c *connection) PaneFocused(paneID [16]byte) {
	payload, err := protocol.EncodePaneFocus(protocol.PaneFocus{PaneID: paneID})
	if err != nil {
		return
	}
	if err := c.writeControlMessage(protocol.MsgPaneFocus, payload); err != nil {
		// Ignore errors when the connection is closing.
	}
}

func (c *connection) OnEvent(event texel.Event) {
	switch event.Type {
	case texel.EventStateUpdate:
		payload, ok := event.Payload.(texel.StatePayload)
		if !ok {
			return
		}
		c.sendStateUpdate(payload)
	case texel.EventTreeChanged:
		c.sendTreeSnapshot()
	}
}

func snapshotMergedPaneStates(snapshot protocol.TreeSnapshot, desktop *texel.DesktopEngine) []texel.PaneStateSnapshot {
	if desktop == nil {
		return nil
	}
	byID := make(map[[16]byte]texel.PaneStateSnapshot)
	for _, state := range desktop.PaneStates() {
		byID[state.ID] = state
	}
	merged := make([]texel.PaneStateSnapshot, 0, len(snapshot.Panes))
	for _, pane := range snapshot.Panes {
		if state, ok := byID[pane.PaneID]; ok {
			merged = append(merged, state)
		} else {
			merged = append(merged, texel.PaneStateSnapshot{ID: pane.PaneID})
		}
	}
	return merged
}

func geometryOnlySnapshot(snapshot protocol.TreeSnapshot) protocol.TreeSnapshot {
	out := protocol.TreeSnapshot{
		Panes: make([]protocol.PaneSnapshot, len(snapshot.Panes)),
		Root:  snapshot.Root,
	}
	for i, pane := range snapshot.Panes {
		cloned := pane
		cloned.Rows = nil
		out.Panes[i] = cloned
	}
	return out
}
