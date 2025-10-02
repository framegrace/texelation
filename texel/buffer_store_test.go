package texel

import "testing"

func TestInMemoryBufferStore(t *testing.T) {
	store := NewInMemoryBufferStore()
	if store.Snapshot() != nil {
		t.Fatalf("expected empty snapshot")
	}

	buf := [][]Cell{{{Ch: 'a'}}}
	store.Save(buf)

	if store.Snapshot() == nil {
		t.Fatalf("expected snapshot after save")
	}

	store.Clear()
	if store.Snapshot() != nil {
		t.Fatalf("expected snapshot to clear")
	}
}
