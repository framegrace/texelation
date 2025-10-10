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
const Version uint8 = 0

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
	MsgTreeDelta
	MsgBufferDelta
	MsgBufferAck
	MsgKeyEvent
	MsgMouseEvent
	MsgClipboardSet
	MsgClipboardGet
	MsgThemeUpdate
	MsgError
	MsgMetricUpdate
	MsgClipboardData
	MsgThemeAck
	MsgPaneFocus
	MsgStateUpdate
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
)

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
