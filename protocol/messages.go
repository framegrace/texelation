package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var (
	errStringTooLong = errors.New("protocol: string exceeds 64KB limit")
	errPayloadShort  = errors.New("protocol: payload too short")
)

// Hello initiates the handshake from client to server.
type Hello struct {
	ClientID     [16]byte
	ClientName   string
	Capabilities uint32
}

// Welcome is returned by the server acknowledging the handshake.
type Welcome struct {
	SessionID  [16]byte
	ServerName string
}

// ConnectRequest attaches or creates a session on the server.
type ConnectRequest struct {
	SessionID [16]byte
}

// ConnectAccept is returned once the session is ready.
type ConnectAccept struct {
	SessionID       [16]byte
	ResumeSupported bool
}

// ResumeRequest asks the server to replay buffered diffs from a sequence point.
type ResumeRequest struct {
	SessionID    [16]byte
	LastSequence uint64
}

// ResumeData carries any metadata needed to resume a session.
type ResumeData struct {
	SessionID    [16]byte
	FromSequence uint64
	SnapshotHash [16]byte
}

// DisconnectNotice informs the peer that the session is closing.
type DisconnectNotice struct {
	ReasonCode uint16
	Message    string
}

// Ping/Pong keep the connection alive.
type Ping struct {
	Timestamp int64
}

type Pong struct {
	Timestamp int64
}

// ErrorFrame communicates protocol-level errors.
type ErrorFrame struct {
	Code    uint16
	Message string
}

// BufferAck acknowledges receipt of buffer deltas up to the provided sequence.
type BufferAck struct {
    Sequence uint64
}

// KeyEvent carries keyboard input from client to server.
type KeyEvent struct {
    KeyCode   uint32
    RuneValue rune
    Modifiers uint16
}

// MouseEvent carries mouse position and button data.
type MouseEvent struct {
    X          int16
    Y          int16
    ButtonMask uint32
    WheelX     int16
    WheelY     int16
    Modifiers  uint16
}

// ClipboardSet transfers clipboard contents from client to server.
type ClipboardSet struct {
    MimeType string
    Data     []byte
}

// ClipboardGet requests clipboard contents for a specific MIME type.
type ClipboardGet struct {
    MimeType string
}

// ThemeUpdate notifies about runtime theme adjustments.
type ThemeUpdate struct {
    Section string
    Key     string
    Value   string
}

func encodeString(buf *bytes.Buffer, value string) error {
	if len(value) > 0xFFFF {
		return errStringTooLong
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(value))); err != nil {
		return err
	}
	if len(value) > 0 {
		if _, err := buf.WriteString(value); err != nil {
			return err
		}
	}
	return nil
}

func decodeString(b []byte) (string, []byte, error) {
	if len(b) < 2 {
		return "", nil, errPayloadShort
	}
	length := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	if uint16(len(b)) < length {
		return "", nil, errPayloadShort
	}
	return string(b[:length]), b[length:], nil
}

func EncodeHello(h Hello) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 32+len(h.ClientName)))
	buf.Write(h.ClientID[:])
	if err := encodeString(buf, h.ClientName); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, h.Capabilities); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeHello(b []byte) (Hello, error) {
	var h Hello
	if len(b) < 16 {
		return h, errPayloadShort
	}
	copy(h.ClientID[:], b[:16])
	b = b[16:]
	name, rest, err := decodeString(b)
	if err != nil {
		return h, err
	}
	h.ClientName = name
	if len(rest) < 4 {
		return h, errPayloadShort
	}
	h.Capabilities = binary.LittleEndian.Uint32(rest[:4])
	return h, nil
}

func EncodeWelcome(w Welcome) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 32+len(w.ServerName)))
	buf.Write(w.SessionID[:])
	if err := encodeString(buf, w.ServerName); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeWelcome(b []byte) (Welcome, error) {
	var w Welcome
	if len(b) < 16 {
		return w, errPayloadShort
	}
	copy(w.SessionID[:], b[:16])
	name, _, err := decodeString(b[16:])
	if err != nil {
		return w, err
	}
	w.ServerName = name
	return w, nil
}

func EncodeConnectRequest(c ConnectRequest) ([]byte, error) {
	return c.SessionID[:], nil
}

func DecodeConnectRequest(b []byte) (ConnectRequest, error) {
	var c ConnectRequest
	if len(b) < 16 {
		return c, errPayloadShort
	}
	copy(c.SessionID[:], b[:16])
	return c, nil
}

func EncodeConnectAccept(c ConnectAccept) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 17))
	buf.Write(c.SessionID[:])
	if c.ResumeSupported {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	return buf.Bytes(), nil
}

func DecodeConnectAccept(b []byte) (ConnectAccept, error) {
	var c ConnectAccept
	if len(b) < 17 {
		return c, errPayloadShort
	}
	copy(c.SessionID[:], b[:16])
	c.ResumeSupported = b[16] != 0
	return c, nil
}

func EncodeResumeRequest(r ResumeRequest) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 24))
	buf.Write(r.SessionID[:])
	if err := binary.Write(buf, binary.LittleEndian, r.LastSequence); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeResumeRequest(b []byte) (ResumeRequest, error) {
	var r ResumeRequest
	if len(b) < 24 {
		return r, errPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	r.LastSequence = binary.LittleEndian.Uint64(b[16:24])
	return r, nil
}

func EncodeResumeData(r ResumeData) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 40))
	buf.Write(r.SessionID[:])
	if err := binary.Write(buf, binary.LittleEndian, r.FromSequence); err != nil {
		return nil, err
	}
	buf.Write(r.SnapshotHash[:])
	return buf.Bytes(), nil
}

func DecodeResumeData(b []byte) (ResumeData, error) {
	var r ResumeData
	if len(b) < 40 {
		return r, errPayloadShort
	}
	copy(r.SessionID[:], b[:16])
	r.FromSequence = binary.LittleEndian.Uint64(b[16:24])
	copy(r.SnapshotHash[:], b[24:40])
	return r, nil
}

func EncodeDisconnectNotice(d DisconnectNotice) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 4+len(d.Message)))
	if err := binary.Write(buf, binary.LittleEndian, d.ReasonCode); err != nil {
		return nil, err
	}
	if err := encodeString(buf, d.Message); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeDisconnectNotice(b []byte) (DisconnectNotice, error) {
	var d DisconnectNotice
	if len(b) < 2 {
		return d, errPayloadShort
	}
	d.ReasonCode = binary.LittleEndian.Uint16(b[:2])
	msg, _, err := decodeString(b[2:])
	if err != nil {
		return d, err
	}
	d.Message = msg
	return d, nil
}

func EncodePing(p Ping) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	if err := binary.Write(buf, binary.LittleEndian, p.Timestamp); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodePing(b []byte) (Ping, error) {
	var p Ping
	if len(b) < 8 {
		return p, errPayloadShort
	}
	p.Timestamp = int64(binary.LittleEndian.Uint64(b[:8]))
	return p, nil
}

func EncodePong(p Pong) ([]byte, error) {
	return EncodePing(Ping{Timestamp: p.Timestamp})
}

func DecodePong(b []byte) (Pong, error) {
	ping, err := DecodePing(b)
	if err != nil {
		return Pong{}, err
	}
	return Pong{Timestamp: ping.Timestamp}, nil
}

func EncodeErrorFrame(e ErrorFrame) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 4+len(e.Message)))
	if err := binary.Write(buf, binary.LittleEndian, e.Code); err != nil {
		return nil, err
	}
	if err := encodeString(buf, e.Message); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeErrorFrame(b []byte) (ErrorFrame, error) {
	var e ErrorFrame
	if len(b) < 2 {
		return e, errPayloadShort
	}
	e.Code = binary.LittleEndian.Uint16(b[:2])
	msg, _, err := decodeString(b[2:])
	if err != nil {
		return e, err
	}
	e.Message = msg
	return e, nil
}

func EncodeBufferAck(a BufferAck) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	if err := binary.Write(buf, binary.LittleEndian, a.Sequence); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeBufferAck(b []byte) (BufferAck, error) {
    var ack BufferAck
    if len(b) < 8 {
        return ack, errPayloadShort
    }
    ack.Sequence = binary.LittleEndian.Uint64(b[:8])
    return ack, nil
}

func EncodeMouseEvent(ev MouseEvent) ([]byte, error) {
    buf := bytes.NewBuffer(make([]byte, 0, 16))
    if err := binary.Write(buf, binary.LittleEndian, ev.X); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, ev.Y); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, ev.ButtonMask); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, ev.WheelX); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, ev.WheelY); err != nil {
        return nil, err
    }
    if err := binary.Write(buf, binary.LittleEndian, ev.Modifiers); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func DecodeMouseEvent(b []byte) (MouseEvent, error) {
    var ev MouseEvent
    if len(b) < 14 {
        return ev, errPayloadShort
    }
    ev.X = int16(binary.LittleEndian.Uint16(b[0:2]))
    ev.Y = int16(binary.LittleEndian.Uint16(b[2:4]))
    ev.ButtonMask = binary.LittleEndian.Uint32(b[4:8])
    ev.WheelX = int16(binary.LittleEndian.Uint16(b[8:10]))
    ev.WheelY = int16(binary.LittleEndian.Uint16(b[10:12]))
    ev.Modifiers = binary.LittleEndian.Uint16(b[12:14])
    return ev, nil
}

func EncodeClipboardSet(msg ClipboardSet) ([]byte, error) {
    buf := bytes.NewBuffer(make([]byte, 0, 2+len(msg.MimeType)+len(msg.Data)))
    if err := encodeString(buf, msg.MimeType); err != nil {
        return nil, err
    }
    if len(msg.Data) > 0xFFFF {
        return nil, errStringTooLong
    }
    if err := binary.Write(buf, binary.LittleEndian, uint16(len(msg.Data))); err != nil {
        return nil, err
    }
    if len(msg.Data) > 0 {
        if _, err := buf.Write(msg.Data); err != nil {
            return nil, err
        }
    }
    return buf.Bytes(), nil
}

func DecodeClipboardSet(b []byte) (ClipboardSet, error) {
    var msg ClipboardSet
    mime, rest, err := decodeString(b)
    if err != nil {
        return msg, err
    }
    if len(rest) < 2 {
        return msg, errPayloadShort
    }
    dataLen := binary.LittleEndian.Uint16(rest[:2])
    rest = rest[2:]
    if len(rest) < int(dataLen) {
        return msg, errPayloadShort
    }
    msg.MimeType = mime
    msg.Data = append([]byte(nil), rest[:dataLen]...)
    return msg, nil
}

func EncodeClipboardGet(req ClipboardGet) ([]byte, error) {
    buf := bytes.NewBuffer(nil)
    if err := encodeString(buf, req.MimeType); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func DecodeClipboardGet(b []byte) (ClipboardGet, error) {
    var req ClipboardGet
    mime, _, err := decodeString(b)
    if err != nil {
        return req, err
    }
    req.MimeType = mime
    return req, nil
}

func EncodeThemeUpdate(update ThemeUpdate) ([]byte, error) {
    buf := bytes.NewBuffer(nil)
    if err := encodeString(buf, update.Section); err != nil {
        return nil, err
    }
    if err := encodeString(buf, update.Key); err != nil {
        return nil, err
    }
    if err := encodeString(buf, update.Value); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func DecodeThemeUpdate(b []byte) (ThemeUpdate, error) {
    var update ThemeUpdate
    section, rest, err := decodeString(b)
    if err != nil {
        return update, err
    }
    key, rest, err := decodeString(rest)
    if err != nil {
        return update, err
    }
    value, _, err := decodeString(rest)
    if err != nil {
        return update, err
    }
    update.Section = section
    update.Key = key
    update.Value = value
    return update, nil
}

func EncodeTreeSnapshot(snapshot BufferSnapshot) ([]byte, error) {
    buf := bytes.NewBuffer(nil)
    if err := binary.Write(buf, binary.LittleEndian, uint16(len(snapshot.Panes))); err != nil {
        return nil, err
    }
    for _, pane := range snapshot.Panes {
        if err := binary.Write(buf, binary.LittleEndian, pane.ID); err != nil {
            return nil, err
        }
        if err := encodeString(buf, pane.Title); err != nil {
            return nil, err
        }
    }
    return buf.Bytes(), nil
}

func EncodeKeyEvent(ev KeyEvent) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	if err := binary.Write(buf, binary.LittleEndian, ev.KeyCode); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(ev.RuneValue)); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, ev.Modifiers); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodeKeyEvent(b []byte) (KeyEvent, error) {
	var ev KeyEvent
	if len(b) < 10 {
		return ev, errPayloadShort
	}
	ev.KeyCode = binary.LittleEndian.Uint32(b[:4])
	r := binary.LittleEndian.Uint32(b[4:8])
	ev.RuneValue = rune(r)
	ev.Modifiers = binary.LittleEndian.Uint16(b[8:10])
	return ev, nil
}
