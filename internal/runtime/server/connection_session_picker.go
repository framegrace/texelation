// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/connection_session_picker.go
// Summary: Picker request handlers (Plan F.1) — listing, recovery,
//   rename, delete, and lazy thumbnail fetch.

package server

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/framegrace/texelation/protocol"
)

// maxThumbnailBytes caps PNG sidecar reads to defend against a local
// attacker writing a giant file at the predictable on-disk path. A
// 480×270 PNG at high quality is well under 100 KiB; 1 MiB leaves
// generous headroom while keeping the worst-case server allocation
// bounded.
const maxThumbnailBytes int64 = 1 << 20

func (c *connection) handleListSessions(payload []byte) error {
	if _, err := protocol.DecodeListSessionsRequest(payload); err != nil {
		return fmt.Errorf("decode list-sessions: %w", err)
	}
	resp := protocol.ListSessionsResponse{
		Live:   c.manager.LiveSummaries(),
		Stored: c.manager.StoredSummaries(),
	}
	body, err := protocol.EncodeListSessionsResponse(resp)
	if err != nil {
		return fmt.Errorf("encode list-sessions: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgListSessionsResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}

func (c *connection) handleRecoverSession(payload []byte) error {
	req, err := protocol.DecodeRecoverSessionRequest(payload)
	if err != nil {
		return fmt.Errorf("decode recover-session: %w", err)
	}
	if req.NewLabel != "" {
		if rerr := c.manager.RenameStored(req.SessionID, req.NewLabel); rerr != nil && !errors.Is(rerr, ErrSessionNotFound) {
			return c.sendErrorFrame(rerr)
		}
	}
	sess, _, err := c.manager.LookupOrRehydrate(req.SessionID)
	if err != nil {
		return c.sendErrorFrame(err)
	}
	c.session = sess
	accept := protocol.ConnectAccept{
		SessionID:       sess.ID(),
		ResumeSupported: true,
	}
	body, err := protocol.EncodeConnectAccept(accept)
	if err != nil {
		return fmt.Errorf("encode accept: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgConnectAccept,
		Flags:     protocol.FlagChecksum,
		SessionID: sess.ID(),
	}
	return c.writeMessage(hdr, body)
}

// handleRenameSession applies an inline label edit. Replies with a
// MsgSessionOpResponse{OpType: OpRename, OK: ...} so the picker can
// correlate against the op it issued (no conflation with other
// responses on the connection).
func (c *connection) handleRenameSession(payload []byte) error {
	req, err := protocol.DecodeRenameSessionRequest(payload)
	if err != nil {
		return fmt.Errorf("decode rename: %w", err)
	}
	if err := c.manager.RenameStored(req.SessionID, req.NewLabel); err != nil {
		return c.sendOpResponse(protocol.OpRename, false, err.Error())
	}
	return c.sendOpResponse(protocol.OpRename, true, "")
}

func (c *connection) handleDeleteSession(payload []byte) error {
	req, err := protocol.DecodeDeleteSessionRequest(payload)
	if err != nil {
		return fmt.Errorf("decode delete: %w", err)
	}
	if err := c.manager.DeleteStored(req.SessionID); err != nil {
		return c.sendOpResponse(protocol.OpDelete, false, err.Error())
	}
	return c.sendOpResponse(protocol.OpDelete, true, "")
}

func (c *connection) handleFetchThumbnail(payload []byte) error {
	req, err := protocol.DecodeFetchThumbnailRequest(payload)
	if err != nil {
		return fmt.Errorf("decode fetch-thumb: %w", err)
	}
	pngPath := c.manager.sessionPNGPath(req.SessionID)
	if pngPath == "" {
		return c.sendThumbnailResponse(false, "thumbnail unavailable", nil)
	}
	info, err := os.Stat(pngPath)
	if err != nil {
		if os.IsNotExist(err) {
			return c.sendThumbnailResponse(false, "thumbnail not found", nil)
		}
		log.Printf("server: fetch thumbnail %x stat: %v", req.SessionID[:4], err)
		return c.sendThumbnailResponse(false, "thumbnail io error", nil)
	}
	if info.Size() > maxThumbnailBytes {
		log.Printf("server: fetch thumbnail %x: refusing %d bytes (cap %d)", req.SessionID[:4], info.Size(), maxThumbnailBytes)
		return c.sendThumbnailResponse(false, "thumbnail too large", nil)
	}
	data, err := os.ReadFile(pngPath)
	if err != nil {
		log.Printf("server: fetch thumbnail %x read: %v", req.SessionID[:4], err)
		return c.sendThumbnailResponse(false, "thumbnail io error", nil)
	}
	return c.sendThumbnailResponse(true, "", data)
}

func (c *connection) sendThumbnailResponse(ok bool, errMsg string, png []byte) error {
	body, err := protocol.EncodeFetchThumbnailResponse(protocol.FetchThumbnailResponse{
		OK:    ok,
		Error: errMsg,
		PNG:   png,
	})
	if err != nil {
		return fmt.Errorf("encode fetch-thumb response: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgFetchThumbnailResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}

// sendOpResponse confirms a rename/delete with the typed
// SessionOpResponse envelope. The picker correlates by OpType so it
// can ignore unrelated frames that arrive on the connection.
func (c *connection) sendOpResponse(op protocol.SessionOpKind, ok bool, errMsg string) error {
	body, err := protocol.EncodeSessionOpResponse(protocol.SessionOpResponse{
		OpType: op,
		OK:     ok,
		Error:  errMsg,
	})
	if err != nil {
		return fmt.Errorf("encode op response: %w", err)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgSessionOpResponse,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}

// sendErrorFrame surfaces an error to the client without aborting the
// connection. Used for picker handlers that don't have a dedicated
// response envelope (recover, list).
func (c *connection) sendErrorFrame(err error) error {
	body, encErr := protocol.EncodeErrorFrame(protocol.ErrorFrame{
		Code:    1,
		Message: err.Error(),
	})
	if encErr != nil {
		return fmt.Errorf("encode error frame: %w", encErr)
	}
	hdr := protocol.Header{
		Version:   protocol.Version,
		Type:      protocol.MsgError,
		Flags:     protocol.FlagChecksum,
		SessionID: c.session.ID(),
	}
	return c.writeMessage(hdr, body)
}
