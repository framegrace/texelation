// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/session_picker.go
// Summary: Wire types and encoders for the F.1 session-recovery picker.
// Usage: Used by the server's connection handler to advertise stored
//   sessions and by the client's boot-package picker to consume them.

package protocol

import (
	"bytes"
	"encoding/binary"
)

// SessionSummary describes a stored (fossilized) session on disk.
// HasThumbnail indicates whether <sessionID>.png exists alongside the
// JSON sidecar; the client fetches the PNG lazily via MsgFetchThumbnail.
// Layout is optional — nil when the StoredSession was written before
// Plan F.1 added the Layout field, in which case the picker falls back
// to a single-box ASCII placeholder.
type SessionSummary struct {
	SessionID      [16]byte
	Label          string
	LastActive     int64 // unix seconds
	PaneCount      int32
	FirstPaneTitle string
	Pinned         bool
	HasThumbnail   bool
	Layout         *TreeNodeSnapshot
}

// LiveSummary describes a hydrated (in-memory) session. Populated only
// by F.2; F.1 always sends an empty slice.
type LiveSummary struct {
	SessionID       [16]byte
	Label           string
	AttachedClients int32
	PaneCount       int32
	LastInputAt     int64
}

// EncodeSessionSummary writes a SessionSummary in the picker wire format.
// Layout is preceded by a uint8 flag (1 = present, 0 = nil) to handle
// the optional case without needing a sentinel TreeNodeSnapshot.
func EncodeSessionSummary(s SessionSummary) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(s.SessionID[:])
	if err := encodeString(buf, s.Label); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.LastActive); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.PaneCount); err != nil {
		return nil, err
	}
	if err := encodeString(buf, s.FirstPaneTitle); err != nil {
		return nil, err
	}
	flags := byte(0)
	if s.Pinned {
		flags |= 0x01
	}
	if s.HasThumbnail {
		flags |= 0x02
	}
	buf.WriteByte(flags)
	if s.Layout == nil {
		buf.WriteByte(0)
	} else {
		buf.WriteByte(1)
		if err := encodeTreeNode(buf, *s.Layout); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// DecodeSessionSummary reads a SessionSummary and returns the number of
// bytes consumed so callers parsing slices of summaries can advance.
func DecodeSessionSummary(b []byte) (SessionSummary, int, error) {
	var s SessionSummary
	start := len(b)
	if len(b) < 16 {
		return s, 0, ErrPayloadShort
	}
	copy(s.SessionID[:], b[:16])
	b = b[16:]
	label, rest, err := decodeString(b)
	if err != nil {
		return s, 0, err
	}
	s.Label = label
	b = rest
	if len(b) < 8 {
		return s, 0, ErrPayloadShort
	}
	s.LastActive = int64(binary.LittleEndian.Uint64(b[:8]))
	b = b[8:]
	if len(b) < 4 {
		return s, 0, ErrPayloadShort
	}
	s.PaneCount = int32(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	title, rest, err := decodeString(b)
	if err != nil {
		return s, 0, err
	}
	s.FirstPaneTitle = title
	b = rest
	if len(b) < 1 {
		return s, 0, ErrPayloadShort
	}
	flags := b[0]
	s.Pinned = flags&0x01 != 0
	s.HasThumbnail = flags&0x02 != 0
	b = b[1:]
	if len(b) < 1 {
		return s, 0, ErrPayloadShort
	}
	hasLayout := b[0]
	b = b[1:]
	if hasLayout != 0 {
		node, rest, err := decodeTreeNode(b)
		if err != nil {
			return s, 0, err
		}
		s.Layout = &node
		b = rest
	}
	return s, start - len(b), nil
}

// EncodeLiveSummary writes a LiveSummary in the picker wire format.
func EncodeLiveSummary(s LiveSummary) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(s.SessionID[:])
	if err := encodeString(buf, s.Label); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.AttachedClients); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.PaneCount); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, s.LastInputAt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeLiveSummary reads a LiveSummary and returns bytes consumed.
func DecodeLiveSummary(b []byte) (LiveSummary, int, error) {
	var s LiveSummary
	start := len(b)
	if len(b) < 16 {
		return s, 0, ErrPayloadShort
	}
	copy(s.SessionID[:], b[:16])
	b = b[16:]
	label, rest, err := decodeString(b)
	if err != nil {
		return s, 0, err
	}
	s.Label = label
	b = rest
	if len(b) < 16 {
		return s, 0, ErrPayloadShort
	}
	s.AttachedClients = int32(binary.LittleEndian.Uint32(b[:4]))
	s.PaneCount = int32(binary.LittleEndian.Uint32(b[4:8]))
	s.LastInputAt = int64(binary.LittleEndian.Uint64(b[8:16]))
	b = b[16:]
	return s, start - len(b), nil
}

// ListSessionsRequest is the client-side trigger for the recovery
// picker's catalog. Empty payload in v5; reserved for filters in F.2.
type ListSessionsRequest struct{}

// EncodeListSessionsRequest returns an empty payload (no fields in v5).
func EncodeListSessionsRequest(r ListSessionsRequest) ([]byte, error) {
	return nil, nil
}

// DecodeListSessionsRequest tolerates trailing bytes for forward-compat
// with future filter additions.
func DecodeListSessionsRequest(b []byte) (ListSessionsRequest, error) {
	return ListSessionsRequest{}, nil
}

// ListSessionsResponse carries the catalog of recoverable sessions.
// Live is empty in F.1; populated by F.2.
type ListSessionsResponse struct {
	Live   []LiveSummary
	Stored []SessionSummary
}

// EncodeListSessionsResponse writes Live and Stored slices.
func EncodeListSessionsResponse(r ListSessionsResponse) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if len(r.Live) > 0xFFFF {
		return nil, ErrStringTooLong
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Live))); err != nil {
		return nil, err
	}
	for _, l := range r.Live {
		raw, err := EncodeLiveSummary(l)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	if len(r.Stored) > 0xFFFF {
		return nil, ErrStringTooLong
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(r.Stored))); err != nil {
		return nil, err
	}
	for _, s := range r.Stored {
		raw, err := EncodeSessionSummary(s)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	return buf.Bytes(), nil
}

// DecodeListSessionsResponse parses Live then Stored slices in order.
func DecodeListSessionsResponse(b []byte) (ListSessionsResponse, error) {
	var r ListSessionsResponse
	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	liveCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if liveCount > 0 {
		r.Live = make([]LiveSummary, 0, liveCount)
		for i := uint16(0); i < liveCount; i++ {
			l, consumed, err := DecodeLiveSummary(b)
			if err != nil {
				return ListSessionsResponse{}, err
			}
			r.Live = append(r.Live, l)
			b = b[consumed:]
		}
	}
	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	storedCount := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if storedCount > 0 {
		r.Stored = make([]SessionSummary, 0, storedCount)
		for i := uint16(0); i < storedCount; i++ {
			s, consumed, err := DecodeSessionSummary(b)
			if err != nil {
				return ListSessionsResponse{}, err
			}
			r.Stored = append(r.Stored, s)
			b = b[consumed:]
		}
	}
	return r, nil
}

// RecoverSessionRequest hydrates a stored session and connects to it.
// NewLabel is optional — empty string means leave the label unchanged.
type RecoverSessionRequest struct {
	SessionID [16]byte
	NewLabel  string
}

// EncodeRecoverSessionRequest writes the recovery request payload.
func EncodeRecoverSessionRequest(r RecoverSessionRequest) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(r.SessionID[:])
	if err := encodeString(buf, r.NewLabel); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeRecoverSessionRequest reads the recovery request payload.
func DecodeRecoverSessionRequest(b []byte) (RecoverSessionRequest, error) {
	var r RecoverSessionRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	label, _, err := decodeString(b[16:])
	if err != nil {
		return r, err
	}
	r.NewLabel = label
	return r, nil
}

// RenameSessionRequest edits a stored session's label without recovering.
type RenameSessionRequest struct {
	SessionID [16]byte
	NewLabel  string
}

// EncodeRenameSessionRequest writes the rename request payload.
func EncodeRenameSessionRequest(r RenameSessionRequest) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.Write(r.SessionID[:])
	if err := encodeString(buf, r.NewLabel); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeRenameSessionRequest reads the rename request payload.
func DecodeRenameSessionRequest(b []byte) (RenameSessionRequest, error) {
	var r RenameSessionRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	label, _, err := decodeString(b[16:])
	if err != nil {
		return r, err
	}
	r.NewLabel = label
	return r, nil
}

// DeleteSessionRequest removes a stored session's JSON + PNG sidecar.
type DeleteSessionRequest struct {
	SessionID [16]byte
}

// EncodeDeleteSessionRequest writes the delete request payload.
func EncodeDeleteSessionRequest(r DeleteSessionRequest) ([]byte, error) {
	out := make([]byte, 16)
	copy(out, r.SessionID[:])
	return out, nil
}

// DecodeDeleteSessionRequest reads the delete request payload.
func DecodeDeleteSessionRequest(b []byte) (DeleteSessionRequest, error) {
	var r DeleteSessionRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	return r, nil
}

// SessionOpKind identifies which op a SessionOpResponse acks. The
// picker uses this to correlate without needing in-flight request IDs.
type SessionOpKind uint8

const (
	OpRename SessionOpKind = iota
	OpDelete
)

// SessionOpResponse is the dedicated ack envelope for rename + delete
// (and any future session-mutating op). OK=false implies a non-empty
// Error explaining the refusal (live session, not found, IO failure).
// OpType distinguishes which op this acks so the picker doesn't
// conflate it with a coincident MsgListSessionsResponse.
type SessionOpResponse struct {
	OpType SessionOpKind
	OK     bool
	Error  string
}

// EncodeSessionOpResponse writes the op-response payload.
func EncodeSessionOpResponse(r SessionOpResponse) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(byte(r.OpType))
	if r.OK {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if err := encodeString(buf, r.Error); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeSessionOpResponse reads the op-response payload.
func DecodeSessionOpResponse(b []byte) (SessionOpResponse, error) {
	var r SessionOpResponse
	if len(b) < 2 {
		return r, ErrPayloadShort
	}
	r.OpType = SessionOpKind(b[0])
	r.OK = b[1] != 0
	msg, _, err := decodeString(b[2:])
	if err != nil {
		return r, err
	}
	r.Error = msg
	return r, nil
}

// FetchThumbnailRequest pulls a stored session's PNG sidecar.
type FetchThumbnailRequest struct {
	SessionID [16]byte
}

// EncodeFetchThumbnailRequest writes the fetch-thumbnail request payload.
func EncodeFetchThumbnailRequest(r FetchThumbnailRequest) ([]byte, error) {
	out := make([]byte, 16)
	copy(out, r.SessionID[:])
	return out, nil
}

// DecodeFetchThumbnailRequest reads the fetch-thumbnail request payload.
func DecodeFetchThumbnailRequest(b []byte) (FetchThumbnailRequest, error) {
	var r FetchThumbnailRequest
	if len(b) < 16 {
		return r, ErrPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	return r, nil
}

// FetchThumbnailResponse carries the PNG bytes (or an error explanation
// if OK=false). PNGs over 16MiB are rejected at the framing layer
// (MaxPayloadLen); typical sidecars are tens of KB.
type FetchThumbnailResponse struct {
	OK    bool
	Error string
	PNG   []byte
}

// EncodeFetchThumbnailResponse writes the fetch-thumbnail response payload.
func EncodeFetchThumbnailResponse(r FetchThumbnailResponse) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if r.OK {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if err := encodeString(buf, r.Error); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(r.PNG))); err != nil {
		return nil, err
	}
	if len(r.PNG) > 0 {
		if _, err := buf.Write(r.PNG); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// DecodeFetchThumbnailResponse reads the fetch-thumbnail response payload.
func DecodeFetchThumbnailResponse(b []byte) (FetchThumbnailResponse, error) {
	var r FetchThumbnailResponse
	if len(b) < 1 {
		return r, ErrPayloadShort
	}
	r.OK = b[0] != 0
	b = b[1:]
	msg, rest, err := decodeString(b)
	if err != nil {
		return r, err
	}
	r.Error = msg
	if len(rest) < 4 {
		return r, ErrPayloadShort
	}
	pngLen := binary.LittleEndian.Uint32(rest[:4])
	rest = rest[4:]
	if uint32(len(rest)) < pngLen {
		return r, ErrPayloadShort
	}
	if pngLen > 0 {
		r.PNG = append([]byte(nil), rest[:pngLen]...)
	}
	return r, nil
}
