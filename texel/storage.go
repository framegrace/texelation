// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/storage.go
// Summary: Defines interfaces for the app storage service.
// Usage: Apps can persist state between runs using these interfaces.

package texel

import "encoding/json"

// StorageService is the central storage service hosted by Desktop.
// It manages persistent key-value storage for apps with both app-level
// and per-pane scopes.
type StorageService interface {
	// AppStorage returns a scoped storage accessor for the given app type.
	// App-level storage is shared across all instances of the same app type.
	AppStorage(appType string) AppStorage

	// PaneStorage returns storage scoped to a specific pane ID.
	// Per-pane storage is isolated to individual pane instances.
	PaneStorage(appType string, paneID [16]byte) AppStorage

	// Flush forces all pending writes to disk.
	Flush() error

	// Close flushes and releases all resources.
	Close() error
}

// AppStorage provides key-value operations for a specific scope.
type AppStorage interface {
	// Get retrieves a value by key. Returns nil if key doesn't exist.
	Get(key string) (json.RawMessage, error)

	// Set stores a value for a key. Value must be JSON-serializable.
	Set(key string, value interface{}) error

	// Delete removes a key. No error if key doesn't exist.
	Delete(key string) error

	// List returns all keys in this scope.
	List() ([]string, error)

	// Clear removes all keys in this scope (reset functionality).
	Clear() error

	// Scope returns the scope identifier for debugging.
	Scope() string
}

// StorageSetter is an optional interface for apps that need per-pane storage.
// Detected at pane attachment time (like PaneIDSetter).
type StorageSetter interface {
	SetStorage(storage AppStorage)
}

// AppStorageSetter provides app-level storage (shared across all instances).
// Can be combined with StorageSetter for apps needing both scopes.
type AppStorageSetter interface {
	SetAppStorage(storage AppStorage)
}
