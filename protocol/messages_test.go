package protocol

import "testing"

func TestHelloRoundTrip(t *testing.T) {
	var id [16]byte
	copy(id[:], []byte("client-abcdefghi"))
	hello := Hello{ClientID: id, ClientName: "texel-client", Capabilities: 0xdeadbeef}
	payload, err := EncodeHello(hello)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeHello(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.ClientName != hello.ClientName || decoded.Capabilities != hello.Capabilities {
		t.Fatalf("mismatch: %#v vs %#v", decoded, hello)
	}
}

func TestDisconnectNoticeRoundTrip(t *testing.T) {
	notice := DisconnectNotice{ReasonCode: 3, Message: "server shutdown"}
	payload, err := EncodeDisconnectNotice(notice)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeDisconnectNotice(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.ReasonCode != notice.ReasonCode || decoded.Message != notice.Message {
		t.Fatalf("mismatch: %#v vs %#v", decoded, notice)
	}
}

func TestErrorFrameRoundTrip(t *testing.T) {
	frame := ErrorFrame{Code: 500, Message: "bad things"}
	payload, err := EncodeErrorFrame(frame)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	decoded, err := DecodeErrorFrame(payload)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded.Code != frame.Code || decoded.Message != frame.Message {
		t.Fatalf("mismatch: %#v vs %#v", decoded, frame)
	}
}
