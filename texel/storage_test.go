// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/storage_test.go
// Summary: Tests for the app storage service.

package texel

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestStorageService_AppStorage_SetGet(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Test Set
	err = storage.Set("recentApps", []string{"texelterm", "help"})
	if err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	// Test Get
	data, err := storage.Get("recentApps")
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	var result []string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if len(result) != 2 || result[0] != "texelterm" || result[1] != "help" {
		t.Errorf("Expected [texelterm, help], got %v", result)
	}
}

func TestStorageService_AppStorage_GetNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Get non-existent key should return nil, no error
	data, err := storage.Get("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error getting non-existent key: %v", err)
	}
	if data != nil {
		t.Errorf("Expected nil for non-existent key, got %v", data)
	}
}

func TestStorageService_AppStorage_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Set a value
	storage.Set("key", "value")

	// Delete it
	err = storage.Delete("key")
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify it's gone
	data, _ := storage.Get("key")
	if data != nil {
		t.Errorf("Key should be deleted, got %v", data)
	}
}

func TestStorageService_AppStorage_List(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Set multiple keys
	storage.Set("key1", "value1")
	storage.Set("key2", "value2")
	storage.Set("key3", "value3")

	// List keys
	keys, err := storage.List()
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}

	// Verify all keys present (order may vary)
	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	for _, expected := range []string{"key1", "key2", "key3"} {
		if !keySet[expected] {
			t.Errorf("Expected key %s not found", expected)
		}
	}
}

func TestStorageService_AppStorage_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Set multiple keys
	storage.Set("key1", "value1")
	storage.Set("key2", "value2")

	// Clear
	err = storage.Clear()
	if err != nil {
		t.Fatalf("Failed to clear: %v", err)
	}

	// Verify empty
	keys, _ := storage.List()
	if len(keys) != 0 {
		t.Errorf("Expected empty after clear, got %d keys", len(keys))
	}
}

func TestStorageService_AppStorage_Scope(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")
	scope := storage.Scope()

	if scope != "app/launcher" {
		t.Errorf("Expected scope 'app/launcher', got '%s'", scope)
	}
}

func TestStorageService_PaneStorage_Isolation(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	paneID1 := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	paneID2 := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}

	storage1 := service.PaneStorage("launcher", paneID1)
	storage2 := service.PaneStorage("launcher", paneID2)

	// Set different values in each pane's storage
	storage1.Set("selection", 5)
	storage2.Set("selection", 10)

	// Verify isolation
	data1, _ := storage1.Get("selection")
	data2, _ := storage2.Get("selection")

	var val1, val2 int
	json.Unmarshal(data1, &val1)
	json.Unmarshal(data2, &val2)

	if val1 != 5 {
		t.Errorf("Pane 1 should have 5, got %d", val1)
	}
	if val2 != 10 {
		t.Errorf("Pane 2 should have 10, got %d", val2)
	}
}

func TestStorageService_AppVsPaneIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	paneID := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	appStorage := service.AppStorage("launcher")
	paneStorage := service.PaneStorage("launcher", paneID)

	// Set same key in both scopes
	appStorage.Set("key", "app-value")
	paneStorage.Set("key", "pane-value")

	// Verify they're isolated
	appData, _ := appStorage.Get("key")
	paneData, _ := paneStorage.Get("key")

	var appVal, paneVal string
	json.Unmarshal(appData, &appVal)
	json.Unmarshal(paneData, &paneVal)

	if appVal != "app-value" {
		t.Errorf("App storage should have 'app-value', got '%s'", appVal)
	}
	if paneVal != "pane-value" {
		t.Errorf("Pane storage should have 'pane-value', got '%s'", paneVal)
	}
}

func TestStorageService_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// First session: write data
	service1, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}

	storage := service1.AppStorage("launcher")
	storage.Set("persistent", "data")

	// Force flush and close
	service1.Flush()
	service1.Close()

	// Second session: read data
	service2, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create second storage service: %v", err)
	}
	defer service2.Close()

	storage = service2.AppStorage("launcher")
	data, err := storage.Get("persistent")
	if err != nil {
		t.Fatalf("Failed to get persistent data: %v", err)
	}

	var val string
	json.Unmarshal(data, &val)

	if val != "data" {
		t.Errorf("Expected 'data', got '%s'", val)
	}
}

func TestStorageService_PanePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	paneID := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

	// First session
	service1, _ := NewStorageService(tmpDir)
	storage := service1.PaneStorage("texelterm", paneID)
	storage.Set("scrollPos", 42)
	service1.Flush()
	service1.Close()

	// Second session
	service2, _ := NewStorageService(tmpDir)
	defer service2.Close()
	storage = service2.PaneStorage("texelterm", paneID)
	data, _ := storage.Get("scrollPos")

	var val int
	json.Unmarshal(data, &val)

	if val != 42 {
		t.Errorf("Expected 42, got %d", val)
	}
}

func TestStorageService_ThreadSafety(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			storage.Set("counter", n)
		}(i)
	}
	wg.Wait()

	// Verify we can still read (no corruption)
	data, err := storage.Get("counter")
	if err != nil {
		t.Fatalf("Failed to get after concurrent writes: %v", err)
	}
	if data == nil {
		t.Error("Data should not be nil after concurrent writes")
	}
}

func TestStorageService_ComplexTypes(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Test map
	usage := map[string]int{
		"texelterm": 15,
		"help":      3,
		"launcher":  1,
	}
	storage.Set("usageCounts", usage)

	data, _ := storage.Get("usageCounts")
	var result map[string]int
	json.Unmarshal(data, &result)

	if result["texelterm"] != 15 {
		t.Errorf("Expected texelterm=15, got %d", result["texelterm"])
	}
}

func TestStorageService_DeleteNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Delete non-existent key should not error
	err = storage.Delete("nonexistent")
	if err != nil {
		t.Errorf("Delete non-existent should not error, got: %v", err)
	}
}

func TestStorageService_ClearEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	service, err := NewStorageService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}
	defer service.Close()

	storage := service.AppStorage("launcher")

	// Clear empty storage should not error
	err = storage.Clear()
	if err != nil {
		t.Errorf("Clear empty should not error, got: %v", err)
	}
}
