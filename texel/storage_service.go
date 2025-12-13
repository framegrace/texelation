// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/storage_service.go
// Summary: Implements the file-backed app storage service.
// Usage: Created by Desktop to provide persistent storage to apps.

package texel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileStorageService implements StorageService with file-backed JSON persistence.
type fileStorageService struct {
	baseDir string
	mu      sync.RWMutex

	// In-memory cache: scope -> key -> value
	cache map[string]map[string]json.RawMessage

	// Track dirty scopes for lazy flushing
	dirty map[string]bool

	// Map scope to file path
	scopePaths map[string]string

	// Flush configuration
	flushDebounce time.Duration
	flushTimer    *time.Timer
	flushMu       sync.Mutex

	closed bool
}

// NewStorageService creates a new file-backed storage service.
// baseDir is typically ~/.texelation
func NewStorageService(baseDir string) (StorageService, error) {
	storageDir := filepath.Join(baseDir, "storage")

	// Create directory structure
	for _, subdir := range []string{"app", "pane"} {
		dir := filepath.Join(storageDir, subdir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create storage dir %s: %w", dir, err)
		}
	}

	return &fileStorageService{
		baseDir:       storageDir,
		cache:         make(map[string]map[string]json.RawMessage),
		dirty:         make(map[string]bool),
		scopePaths:    make(map[string]string),
		flushDebounce: 2 * time.Second,
	}, nil
}

func (s *fileStorageService) AppStorage(appType string) AppStorage {
	scope := fmt.Sprintf("app/%s", appType)
	filePath := filepath.Join(s.baseDir, "app", appType+".json")

	s.mu.Lock()
	s.scopePaths[scope] = filePath
	s.mu.Unlock()

	return &scopedStorage{
		service:  s,
		scope:    scope,
		filePath: filePath,
	}
}

func (s *fileStorageService) PaneStorage(appType string, paneID [16]byte) AppStorage {
	paneIDHex := fmt.Sprintf("%x", paneID)
	scope := fmt.Sprintf("pane/%s/%s", paneIDHex, appType)

	// Ensure pane directory exists
	paneDir := filepath.Join(s.baseDir, "pane", paneIDHex)
	os.MkdirAll(paneDir, 0755)

	filePath := filepath.Join(paneDir, appType+".json")

	s.mu.Lock()
	s.scopePaths[scope] = filePath
	s.mu.Unlock()

	return &scopedStorage{
		service:  s,
		scope:    scope,
		filePath: filePath,
	}
}

func (s *fileStorageService) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for scope, isDirty := range s.dirty {
		if !isDirty {
			continue
		}

		filePath, ok := s.scopePaths[scope]
		if !ok {
			continue
		}

		scopeData := s.cache[scope]
		if scopeData == nil {
			// Empty scope, remove file if exists
			os.Remove(filePath)
			delete(s.dirty, scope)
			continue
		}

		data, err := json.MarshalIndent(scopeData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal scope %s: %w", scope, err)
		}

		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return fmt.Errorf("failed to write scope %s: %w", scope, err)
		}

		delete(s.dirty, scope)
	}
	return nil
}

func (s *fileStorageService) Close() error {
	s.flushMu.Lock()
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}
	s.flushMu.Unlock()

	err := s.Flush()

	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	return err
}

func (s *fileStorageService) markDirty(scope string) {
	s.dirty[scope] = true
	s.scheduleFlush()
}

func (s *fileStorageService) scheduleFlush() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	if s.flushTimer != nil {
		s.flushTimer.Stop()
	}
	s.flushTimer = time.AfterFunc(s.flushDebounce, func() {
		s.Flush()
	})
}

// ensureLoaded loads the scope from disk if not already cached.
// Must be called with s.mu held (at least read lock).
func (s *fileStorageService) ensureLoaded(scope, filePath string) error {
	if _, ok := s.cache[scope]; ok {
		return nil // Already loaded
	}

	// Try to load from disk
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No file yet, initialize empty
			s.cache[scope] = make(map[string]json.RawMessage)
			return nil
		}
		return fmt.Errorf("failed to read storage file: %w", err)
	}

	var scopeData map[string]json.RawMessage
	if err := json.Unmarshal(data, &scopeData); err != nil {
		// Corrupted file, start fresh
		s.cache[scope] = make(map[string]json.RawMessage)
		return nil
	}

	s.cache[scope] = scopeData
	return nil
}

// scopedStorage provides storage operations for a specific scope.
type scopedStorage struct {
	service  *fileStorageService
	scope    string
	filePath string
}

func (ss *scopedStorage) Get(key string) (json.RawMessage, error) {
	ss.service.mu.Lock()
	defer ss.service.mu.Unlock()

	if err := ss.service.ensureLoaded(ss.scope, ss.filePath); err != nil {
		return nil, err
	}

	if scopeData, ok := ss.service.cache[ss.scope]; ok {
		return scopeData[key], nil
	}
	return nil, nil
}

func (ss *scopedStorage) Set(key string, value interface{}) error {
	ss.service.mu.Lock()
	defer ss.service.mu.Unlock()

	if err := ss.service.ensureLoaded(ss.scope, ss.filePath); err != nil {
		return err
	}

	// Marshal value to JSON
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	// Initialize scope if needed
	if ss.service.cache[ss.scope] == nil {
		ss.service.cache[ss.scope] = make(map[string]json.RawMessage)
	}

	ss.service.cache[ss.scope][key] = data
	ss.service.markDirty(ss.scope)
	return nil
}

func (ss *scopedStorage) Delete(key string) error {
	ss.service.mu.Lock()
	defer ss.service.mu.Unlock()

	if err := ss.service.ensureLoaded(ss.scope, ss.filePath); err != nil {
		return err
	}

	if scopeData, ok := ss.service.cache[ss.scope]; ok {
		delete(scopeData, key)
		ss.service.markDirty(ss.scope)
	}
	return nil
}

func (ss *scopedStorage) List() ([]string, error) {
	ss.service.mu.Lock()
	defer ss.service.mu.Unlock()

	if err := ss.service.ensureLoaded(ss.scope, ss.filePath); err != nil {
		return nil, err
	}

	var keys []string
	if scopeData, ok := ss.service.cache[ss.scope]; ok {
		for key := range scopeData {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (ss *scopedStorage) Clear() error {
	ss.service.mu.Lock()
	defer ss.service.mu.Unlock()

	ss.service.cache[ss.scope] = make(map[string]json.RawMessage)
	ss.service.markDirty(ss.scope)

	// Also delete the file
	os.Remove(ss.filePath)
	return nil
}

func (ss *scopedStorage) Scope() string {
	return ss.scope
}
