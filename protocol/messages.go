package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

var (
	errStringTooLong = errors.New("protocol: string exceeds 64KB limit")
	errPayloadShort  = errors.New("protocol: payload too short")
	errExtraBytes    = errors.New("protocol: payload has trailing data")
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

// ClipboardData delivers clipboard contents from server to client.
type ClipboardData struct {
	MimeType string
	Data     []byte
}

// ThemeUpdate notifies about runtime theme adjustments.
type ThemeUpdate struct {
	Section string
	Key     string
	Value   string
}

// ThemeAck confirms that a theme update has been applied server-side.
type ThemeAck struct {
	Section string
	Key     string
	Value   string
}

// PaneFocus identifies the pane that is currently active/focused.
type PaneFocus struct {
	PaneID [16]byte
}

// StateUpdate mirrors texel.StatePayload for remote clients.
type StateUpdate struct {
	WorkspaceID   int32
	AllWorkspaces []int32
	InControlMode bool
	SubMode       rune
	ActiveTitle   string
	DesktopBgRGB  uint32
}

// PaneStateFlags indicate pane state bits.
type PaneStateFlags uint8

const (
	PaneStateActive PaneStateFlags = 1 << iota
	PaneStateResizing
)

// PaneState reports transient pane flags (active, resizing, etc.).
type PaneState struct {
	PaneID [16]byte
	Flags  PaneStateFlags
}

// PaneSnapshot describes the full buffer content for a single pane.
type PaneSnapshot struct {
	PaneID    [16]byte
	Revision  uint32
	Title     string
	Rows      []string
	X         int32
	Y         int32
	Width     int32
	Height    int32
	AppType   string
	AppConfig string
}

// SplitKind describes how an internal node divides space among children.
type SplitKind uint8

const (
	SplitNone SplitKind = iota
	SplitHorizontal
	SplitVertical
)

// TreeNodeSnapshot captures either a leaf pane index or an internal split node.
type TreeNodeSnapshot struct {
	PaneIndex   int32
	Split       SplitKind
	SplitRatios []float32
	Children    []TreeNodeSnapshot
}

// TreeSnapshot aggregates pane snapshots along with the layout tree so peers can rebuild geometry.
type TreeSnapshot struct {
	Panes []PaneSnapshot
	Root  TreeNodeSnapshot
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

func EncodeClipboardData(msg ClipboardData) ([]byte, error) {
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

func DecodeClipboardData(b []byte) (ClipboardData, error) {
	var msg ClipboardData
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

func EncodeThemeAck(msg ThemeAck) ([]byte, error) {
	return EncodeThemeUpdate(ThemeUpdate(msg))
}

func DecodeThemeAck(b []byte) (ThemeAck, error) {
	update, err := DecodeThemeUpdate(b)
	return ThemeAck(update), err
}

func EncodePaneFocus(focus PaneFocus) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if err := binary.Write(buf, binary.LittleEndian, focus.PaneID); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DecodePaneFocus(b []byte) (PaneFocus, error) {
	var focus PaneFocus
	if len(b) < len(focus.PaneID) {
		return focus, errPayloadShort
	}
	copy(focus.PaneID[:], b[:len(focus.PaneID)])
	return focus, nil
}

// EncodeStateUpdate serialises a state update for transport.
func EncodeStateUpdate(update StateUpdate) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if err := binary.Write(buf, binary.LittleEndian, update.WorkspaceID); err != nil {
		return nil, err
	}
	if update.InControlMode {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(update.SubMode)); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.LittleEndian, update.DesktopBgRGB); err != nil {
		return nil, err
	}
	if len(update.AllWorkspaces) > 0xFFFF {
		return nil, errStringTooLong
	}
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(update.AllWorkspaces))); err != nil {
		return nil, err
	}
	for _, ws := range update.AllWorkspaces {
		if err := binary.Write(buf, binary.LittleEndian, ws); err != nil {
			return nil, err
		}
	}
	if err := encodeString(buf, update.ActiveTitle); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeStateUpdate deserialises the state update payload.
func DecodeStateUpdate(b []byte) (StateUpdate, error) {
	var update StateUpdate
	if len(b) < 4 {
		return update, errPayloadShort
	}
	update.WorkspaceID = int32(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	if len(b) < 1 {
		return update, errPayloadShort
	}
	update.InControlMode = b[0] != 0
	b = b[1:]
	if len(b) < 4 {
		return update, errPayloadShort
	}
	update.SubMode = rune(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	if len(b) < 4 {
		return update, errPayloadShort
	}
	update.DesktopBgRGB = binary.LittleEndian.Uint32(b[:4])
	b = b[4:]
	if len(b) < 2 {
		return update, errPayloadShort
	}
	count := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	update.AllWorkspaces = make([]int32, count)
	for i := 0; i < int(count); i++ {
		if len(b) < 4 {
			return update, errPayloadShort
		}
		update.AllWorkspaces[i] = int32(binary.LittleEndian.Uint32(b[:4]))
		b = b[4:]
	}
	var err error
	update.ActiveTitle, b, err = decodeString(b)
	if err != nil {
		return update, err
	}
	if len(b) != 0 {
		return update, errExtraBytes
	}
	return update, nil
}

// EncodePaneState serialises pane state flags.
func EncodePaneState(state PaneState) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 18))
	if err := binary.Write(buf, binary.LittleEndian, state.PaneID); err != nil {
		return nil, err
	}
	buf.WriteByte(byte(state.Flags))
	return buf.Bytes(), nil
}

// DecodePaneState deserialises pane state flags.
func DecodePaneState(b []byte) (PaneState, error) {
	var state PaneState
	if len(b) < len(state.PaneID)+1 {
		return state, errPayloadShort
	}
	copy(state.PaneID[:], b[:len(state.PaneID)])
	state.Flags = PaneStateFlags(b[len(state.PaneID)])
	return state, nil
}

// EncodeTreeSnapshot serialises the tree snapshot for transport.
func EncodeTreeSnapshot(snapshot TreeSnapshot) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if err := binary.Write(buf, binary.LittleEndian, uint16(len(snapshot.Panes))); err != nil {
		return nil, err
	}
	for _, pane := range snapshot.Panes {
		if err := binary.Write(buf, binary.LittleEndian, pane.PaneID); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.Revision); err != nil {
			return nil, err
		}
		if err := encodeString(buf, pane.Title); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.X); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.Y); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.Width); err != nil {
			return nil, err
		}
		if err := binary.Write(buf, binary.LittleEndian, pane.Height); err != nil {
			return nil, err
		}
		if len(pane.Rows) > 0xFFFF {
			return nil, errStringTooLong
		}
		if err := binary.Write(buf, binary.LittleEndian, uint16(len(pane.Rows))); err != nil {
			return nil, err
		}
		for _, row := range pane.Rows {
			if err := encodeString(buf, row); err != nil {
				return nil, err
			}
		}
		if err := encodeString(buf, pane.AppType); err != nil {
			return nil, err
		}
		if err := encodeString(buf, pane.AppConfig); err != nil {
			return nil, err
		}
	}
	if err := encodeTreeNode(buf, snapshot.Root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeTreeSnapshot deserialises the tree snapshot payload.
func DecodeTreeSnapshot(b []byte) (TreeSnapshot, error) {
	var snapshot TreeSnapshot
	if len(b) < 2 {
		return snapshot, errPayloadShort
	}
	count := binary.LittleEndian.Uint16(b[:2])
	b = b[2:]
	snapshot.Panes = make([]PaneSnapshot, count)
	for i := 0; i < int(count); i++ {
		if len(b) < 20 {
			return snapshot, errPayloadShort
		}
		var pane PaneSnapshot
		copy(pane.PaneID[:], b[:16])
		pane.Revision = binary.LittleEndian.Uint32(b[16:20])
		b = b[20:]
		title, rest, err := decodeString(b)
		if err != nil {
			return snapshot, err
		}
		pane.Title = title
		if len(rest) < 18 {
			return snapshot, errPayloadShort
		}
		pane.X = int32(binary.LittleEndian.Uint32(rest[0:4]))
		pane.Y = int32(binary.LittleEndian.Uint32(rest[4:8]))
		pane.Width = int32(binary.LittleEndian.Uint32(rest[8:12]))
		pane.Height = int32(binary.LittleEndian.Uint32(rest[12:16]))
		rowCount := binary.LittleEndian.Uint16(rest[16:18])
		rest = rest[18:]
		pane.Rows = make([]string, rowCount)
		for r := 0; r < int(rowCount); r++ {
			row, remaining, err := decodeString(rest)
			if err != nil {
				return snapshot, err
			}
			pane.Rows[r] = row
			rest = remaining
		}
		appType, remaining, err := decodeString(rest)
		if err != nil {
			return snapshot, err
		}
		config, remaining, err := decodeString(remaining)
		if err != nil {
			return snapshot, err
		}
		pane.AppType = appType
		pane.AppConfig = config
		snapshot.Panes[i] = pane
		b = remaining
	}
	node, remaining, err := decodeTreeNode(b)
	if err != nil {
		return snapshot, err
	}
	snapshot.Root = node
	if len(remaining) != 0 {
		return snapshot, errPayloadShort
	}
	return snapshot, nil
}

func encodeTreeNode(buf *bytes.Buffer, node TreeNodeSnapshot) error {
	if err := binary.Write(buf, binary.LittleEndian, node.PaneIndex); err != nil {
		return err
	}
	if err := buf.WriteByte(byte(node.Split)); err != nil {
		return err
	}
	childCount := uint16(len(node.Children))
	if err := binary.Write(buf, binary.LittleEndian, childCount); err != nil {
		return err
	}
	if childCount > 0 {
		if len(node.SplitRatios) != int(childCount) {
			return errPayloadShort
		}
		for _, ratio := range node.SplitRatios {
			if err := binary.Write(buf, binary.LittleEndian, ratio); err != nil {
				return err
			}
		}
		for _, child := range node.Children {
			if err := encodeTreeNode(buf, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeTreeNode(b []byte) (TreeNodeSnapshot, []byte, error) {
	var node TreeNodeSnapshot
	if len(b) < 7 {
		return node, nil, errPayloadShort
	}
	node.PaneIndex = int32(binary.LittleEndian.Uint32(b[:4]))
	node.Split = SplitKind(b[4])
	childCount := binary.LittleEndian.Uint16(b[5:7])
	b = b[7:]
	if childCount > 0 {
		req := int(childCount) * 4
		if len(b) < req {
			return node, nil, errPayloadShort
		}
		node.SplitRatios = make([]float32, childCount)
		for i := 0; i < int(childCount); i++ {
			node.SplitRatios[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4 : (i+1)*4]))
		}
		b = b[req:]
		node.Children = make([]TreeNodeSnapshot, 0, childCount)
		for i := 0; i < int(childCount); i++ {
			child, rest, err := decodeTreeNode(b)
			if err != nil {
				return node, nil, err
			}
			node.Children = append(node.Children, child)
			b = rest
		}
	}
	return node, b, nil
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
