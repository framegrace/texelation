package client

import (
	"testing"

	"texelation/protocol"
)

func setupDelta(size int) protocol.BufferDelta {
	rows := make([]protocol.RowDelta, size)
	spans := []protocol.CellSpan{{StartCol: 0, Text: "sample text", StyleIndex: 0}}
	for i := range rows {
		rows[i] = protocol.RowDelta{Row: uint16(i), Spans: spans}
	}
	return protocol.BufferDelta{
		PaneID:   [16]byte{1, 2, 3, 4},
		Revision: 1,
		Styles:   []protocol.StyleEntry{{AttrFlags: 0, FgModel: protocol.ColorModelDefault, BgModel: protocol.ColorModelDefault}},
		Rows:     rows,
	}
}

func BenchmarkBufferCacheApplyDelta(b *testing.B) {
	cache := NewBufferCache()
	delta := setupDelta(24)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.ApplyDelta(delta)
	}
}

func BenchmarkDecodeBufferDelta(b *testing.B) {
	delta := setupDelta(24)
	payload, err := protocol.EncodeBufferDelta(delta)
	if err != nil {
		b.Fatalf("encode error: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := protocol.DecodeBufferDelta(payload); err != nil {
			b.Fatalf("decode error: %v", err)
		}
	}
}
