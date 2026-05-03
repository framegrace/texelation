// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: protocol/protocol.go
// Summary: Implements protocol capabilities for the protocol definitions.
// Usage: Shared by clients and servers to encode protocol messages over the wire.
// Notes: Keep changes backward-compatible; any additions require coordinated version bumps.

package protocol

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

const (
	magic      uint32 = 0x54584c01 // "TXL\x01"
	headerSize        = 40
)

// Flag bits for the header Flags byte.
const (
	FlagChecksum uint8 = 0x01
)

// Version is the negotiated protocol version implemented by this package.
//
// v2 (issue #199 Plan B): ResumeRequest grew a PaneViewports payload
// (minimum size 24 -> 26 bytes even for an empty list, plus per-entry
// PaneViewportState records). Bumping the version lets pre-Plan-B clients
// receive an explicit handshake rejection instead of a mysterious
// ErrPayloadShort on the first resume attempt.
//
// v3 (PR #206): PaneSnapshot grew ContentTopRow / NumContentRows;
// BufferDelta grew DecorRows.
//
// v4: MsgBootProgress added — server emits unsolicited string-payload
// progress messages during expensive resume processing so the boot
// splash can render fine-grained text instead of "Loading session…"
// for the full WAL-replay window.
//
// v5 (issue #199 Plan F.1): adds MsgListSessions, MsgListSessionsResponse,
// MsgRecoverSession, MsgRenameSession, MsgDeleteSession, MsgFetchThumbnail,
// MsgFetchThumbnailResponse, MsgSessionOpResponse plus SessionSummary /
// LiveSummary wire types. Strict version equality means old (v4) clients
// fail at the header check — there is no in-band fallback path; users on
// the old client see a generic connect error and must upgrade. Single-binary
// deployment makes this acceptable; revisit if a multi-version client
// population emerges.
const Version uint8 = 5

// MessageType enumerates the canonical message categories exchanged between
// client and server.
type MessageType uint8

const (
	MsgHello MessageType = iota
	MsgWelcome
	MsgConnectRequest
	MsgConnectAccept
	MsgResumeRequest
	MsgResumeData
	MsgDisconnectNotice
	MsgPing
	MsgPong
	MsgTreeSnapshot
	_ // was MsgTreeDelta (unused, preserves iota)
	MsgBufferDelta
	MsgBufferAck
	MsgKeyEvent
	MsgMouseEvent
	MsgClipboardSet
	MsgClipboardGet
	MsgThemeUpdate
	MsgError
	_ // was MsgMetricUpdate (unused, preserves iota)
	MsgClipboardData
	MsgThemeAck
	MsgPaneFocus
	MsgStateUpdate
	MsgPaneState
	MsgResize
	MsgPaste
	MsgClientReady
	MsgImageUpload
	MsgImagePlace
	MsgImageDelete
	MsgImageReset
	MsgViewportUpdate
	MsgFetchRange
	MsgFetchRangeResponse
	// MsgBootProgress carries a single human-readable progress string
	// emitted by the server during expensive resume operations
	// (per-pane WAL hydration etc). Clients display it in the boot
	// splash; missing this message is harmless — the splash falls back
	// to the static "Loading session" stage.
	MsgBootProgress
	// MsgListSessions / MsgListSessionsResponse drive the F.1 stored-
	// session recovery picker. Request has no payload; response carries
	// Live and Stored slices.
	MsgListSessions
	MsgListSessionsResponse
	// MsgRecoverSession: client picks a stored session to hydrate.
	// Server replies with the ordinary MsgConnectAccept + MsgTreeSnapshot
	// flow; picker hands off to the existing connect path.
	MsgRecoverSession
	// MsgRenameSession: edit a stored session's label without recovering.
	MsgRenameSession
	// MsgDeleteSession: remove a stored session's JSON + PNG sidecar.
	// Refused with error if the session is currently live.
	MsgDeleteSession
	// MsgFetchThumbnail / MsgFetchThumbnailResponse: lazy pull of a
	// stored session's PNG thumbnail. Only fires for graphics-capable
	// terminals; non-graphics clients render an ASCII fallback instead.
	MsgFetchThumbnail
	MsgFetchThumbnailResponse
	// MsgSessionOpResponse: dedicated ack for rename/delete (and any
	// future session-mutating op). Carries OK + Error + OpType so the
	// picker can correlate without conflating with MsgListSessionsResponse.
	MsgSessionOpResponse
)

// Header describes the fixed portion of every frame exchanged over the wire.
type Header struct {
	Version    uint8
	Type       MessageType
	Flags      uint8
	Reserved   uint8
	SessionID  [16]byte
	Sequence   uint64
	PayloadLen uint32
	Checksum   uint32
}

var (
	ErrInvalidMagic     = errors.New("protocol: invalid magic")
	ErrUnsupportedVer   = errors.New("protocol: unsupported version")
	ErrShortPayload     = errors.New("protocol: payload shorter than declared length")
	ErrChecksumMismatch = errors.New("protocol: checksum mismatch")
	ErrPayloadTooLarge  = errors.New("protocol: payload exceeds MaxPayloadLen")
)

// MaxPayloadLen caps a single message's payload size to defend against
// malformed or hostile headers that would otherwise allocate up to 4GB
// (the uint32 limit of Header.PayloadLen). 16MiB comfortably exceeds
// any legitimate texelation message; revisit only if a real protocol
// addition needs more.
const MaxPayloadLen uint32 = 16 * 1024 * 1024

// WriteMessage serialises the header and payload to the provided writer. The
// payload slice is written as-is; callers retain ownership of the buffer.
func WriteMessage(w io.Writer, hdr Header, payload []byte) error {
	hdr.PayloadLen = uint32(len(payload))

	buf := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(buf[0:], magic)
	buf[4] = hdr.Version
	buf[5] = byte(hdr.Type)
	buf[6] = hdr.Flags
	buf[7] = hdr.Reserved
	copy(buf[8:24], hdr.SessionID[:])
	binary.LittleEndian.PutUint64(buf[24:32], hdr.Sequence)
	binary.LittleEndian.PutUint32(buf[32:36], hdr.PayloadLen)

	checksum := hdr.Checksum
	if hdr.Flags&FlagChecksum != 0 {
		crc := crc32.NewIEEE()
		_, _ = crc.Write(buf[4:36])
		if len(payload) > 0 {
			_, _ = crc.Write(payload)
		}
		checksum = crc.Sum32()
	}
	binary.LittleEndian.PutUint32(buf[36:40], checksum)

	if _, err := w.Write(buf); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// ReadMessage reads a header and payload from r. The returned payload points to
// a freshly allocated slice sized to the declared payload length.
func ReadMessage(r io.Reader) (Header, []byte, error) {
	var hdr Header
	buf := make([]byte, headerSize)
	if _, err := io.ReadFull(r, buf); err != nil {
		return hdr, nil, err
	}

	if binary.LittleEndian.Uint32(buf[0:4]) != magic {
		return hdr, nil, ErrInvalidMagic
	}

	hdr.Version = buf[4]
	hdr.Type = MessageType(buf[5])
	hdr.Flags = buf[6]
	hdr.Reserved = buf[7]
	copy(hdr.SessionID[:], buf[8:24])
	hdr.Sequence = binary.LittleEndian.Uint64(buf[24:32])
	hdr.PayloadLen = binary.LittleEndian.Uint32(buf[32:36])
	hdr.Checksum = binary.LittleEndian.Uint32(buf[36:40])

	if hdr.Version != Version {
		return hdr, nil, ErrUnsupportedVer
	}

	if hdr.PayloadLen > MaxPayloadLen {
		return hdr, nil, ErrPayloadTooLarge
	}

	payload := make([]byte, hdr.PayloadLen)
	if hdr.PayloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return hdr, nil, ErrShortPayload
			}
			return hdr, nil, err
		}
	}

	if hdr.Flags&FlagChecksum != 0 {
		crc := crc32.NewIEEE()
		_, _ = crc.Write(buf[4:36])
		if len(payload) > 0 {
			_, _ = crc.Write(payload)
		}
		computed := crc.Sum32()
		if computed != hdr.Checksum {
			return hdr, nil, ErrChecksumMismatch
		}
	}

	return hdr, payload, nil
}
