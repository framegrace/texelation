// internal/persistence/atomicjson/store_test.go
package atomicjson

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
