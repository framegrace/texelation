// internal/persistence/atomicjson/store_test.go
package atomicjson

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// TestSaveFailingRenameLeavesPriorFile exercises the REAL atomicjson.Save
// via the package-level renameFn hook. After a successful initial save,
// renameFn is swapped to one that always returns an error. A subsequent
// Save must:
//
//  1. Return the rename error (so callers know the save didn't land).
//  2. Leave the canonical file intact (atomic-rename guarantee).
//  3. Trigger the deferred os.Remove(tmpPath) cleanup so no orphan
//     tmps accumulate in the directory.
//
// This is the spec-promised "atomic-write semantics" property — testing
// it through Save (rather than a saver-injection bypass) means the
// production deferred-cleanup path is what's under test.
func TestSaveFailingRenameLeavesPriorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	// First save: succeeds, populates canonical file.
	good := &fakePayload{Name: "good", Count: 1}
	if err := Save(path, good); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Override the rename primitive to always fail. Restore on cleanup.
	origRename := renameFn
	renameFn = func(oldpath, newpath string) error {
		return errors.New("synthetic rename failure")
	}
	t.Cleanup(func() { renameFn = origRename })

	// Second save: encodes successfully into a tmp file, but renameFn
	// returns an error. Save's defer runs os.Remove(tmpPath).
	bad := &fakePayload{Name: "replacement", Count: 999}
	err := Save(path, bad)
	if err == nil {
		t.Fatalf("Save expected to return rename error, got nil")
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Fatalf("Save error should mention rename, got: %v", err)
	}

	// Canonical file still has the original payload.
	got, err := Load[fakePayload](path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil || got.Name != "good" || got.Count != 1 {
		t.Fatalf("canonical file modified by failing-rename path: got %+v", got)
	}

	// No orphan .atomicjson.tmp-* files leak.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".atomicjson.tmp-") {
			t.Errorf("orphan tmp file leaked after failing rename: %s", name)
		}
	}
}

// syncBuf wraps bytes.Buffer in a mutex so concurrent log writes
// (from the Store's tick goroutines) don't race against test reads.
type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestSaveErrRelogInterval_DeduplicatesIdenticalErrors: repeated
// identical errors are logged once, not on every retry. A different
// error string logs immediately. Recovery (success after failure)
// emits one "save recovered" line.
func TestSaveErrRelogInterval_DeduplicatesIdenticalErrors(t *testing.T) {
	buf := &syncBuf{}
	prev := log.Writer()
	log.SetOutput(buf)
	defer log.SetOutput(prev)

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	st := NewStore[fakePayload](path, 5*time.Millisecond)

	var mode atomic.Int32 // 0=err1, 1=err2, 2=ok
	st.SetSaverForTest(func(p string, v *fakePayload) error {
		switch mode.Load() {
		case 0:
			return errors.New("err A")
		case 1:
			return errors.New("err B")
		default:
			return Save(p, v)
		}
	})

	// 5 saves with the same error — exactly 1 log line.
	for i := 0; i < 5; i++ {
		st.Update(fakePayload{Count: i})
		st.Flush()
	}
	if c := strings.Count(buf.String(), "save failed"); c != 1 {
		t.Fatalf("identical errors must dedup: saw %d log lines\n%s", c, buf.String())
	}

	// Switch error string — must log a new line.
	mode.Store(1)
	st.Update(fakePayload{Count: 100})
	st.Flush()
	if c := strings.Count(buf.String(), "save failed"); c != 2 {
		t.Fatalf("distinct error strings must each log: saw %d\n%s", c, buf.String())
	}

	// Recovery — success after failure logs exactly one "recovered" line.
	mode.Store(2)
	st.Update(fakePayload{Count: 200})
	st.Flush()
	if c := strings.Count(buf.String(), "save recovered"); c != 1 {
		t.Fatalf("expected 1 recovered log, saw %d\n%s", c, buf.String())
	}
	st.Close()
}
