// internal/persistence/atomicjson/store_test.go
package atomicjson

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.json")
	in := &fakePayload{Name: "alpha", Count: 7}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out == nil || out.Name != "alpha" || out.Count != 7 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestLoadMissingReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	out, err := Load[fakePayload](filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil for missing file, got %+v", out)
	}
}

func TestLoadCorruptReturnsNilNilAndDeletes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil on parse error, got %+v", out)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected file wiped after parse error, got stat=%v", err)
	}
}

func TestWipeIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := Wipe(path); err != nil {
		t.Fatalf("Wipe missing: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Wipe(path); err != nil {
		t.Fatalf("Wipe existing: %v", err)
	}
}

func TestStoreCoalescesUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 50*time.Millisecond)
	defer st.Close()

	for i := 0; i < 10; i++ {
		st.Update(fakePayload{Name: "x", Count: i})
	}
	time.Sleep(150 * time.Millisecond)

	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out == nil || out.Count != 9 {
		t.Fatalf("expected last-wins Count=9, got %+v", out)
	}
}

func TestStoreFlushOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 1*time.Hour)
	st.Update(fakePayload{Name: "y", Count: 42})
	st.Close()

	out, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out == nil || out.Count != 42 {
		t.Fatalf("Close did not flush: %+v", out)
	}
}

func TestStoreUpdateAfterCloseIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 50*time.Millisecond)
	st.Update(fakePayload{Name: "z", Count: 1})
	st.Close()
	st.Update(fakePayload{Name: "z", Count: 999})
	time.Sleep(100 * time.Millisecond)

	out, _ := Load[fakePayload](path)
	if out == nil || out.Count != 1 {
		t.Fatalf("post-Close Update leaked: %+v", out)
	}
}

func TestStoreSerializesSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	st := NewStore[fakePayload](path, 5*time.Millisecond)

	var inFlight atomic.Int32
	var maxParallel atomic.Int32
	st.SetSaverForTest(func(p string, v *fakePayload) error {
		n := inFlight.Add(1)
		if n > maxParallel.Load() {
			maxParallel.Store(n)
		}
		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)
		return Save(p, v)
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st.Update(fakePayload{Count: i})
		}(i)
	}
	wg.Wait()
	st.Close()

	if mp := maxParallel.Load(); mp > 1 {
		t.Fatalf("expected serialized saves, observed max parallel=%d", mp)
	}
}
