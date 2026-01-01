// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/registry/registry.go
// Summary: Implements the app registry system for discovering and managing apps.
// Usage: The Desktop scans and loads apps from ~/.config/texelation/apps/

package registry

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// AppFactory creates a new app instance.
// Returns interface{} which is expected to be a texel.App.
type AppFactory func() interface{}

// AppEntry represents a discovered application with its metadata and factory.
type AppEntry struct {
	Manifest *Manifest
	Dir      string
	Factory  AppFactory
}

// WrapperFactory creates an app instance from a manifest.
// This is used for wrapper apps that need custom creation logic.
// Returns interface{} which is expected to be a texel.App.
type WrapperFactory func(manifest *Manifest) interface{}

// Registry manages the collection of available applications.
type Registry struct {
	mu               sync.RWMutex
	apps             map[string]*AppEntry // name -> entry (external apps)
	builtIn          map[string]*AppEntry // name -> entry (built-in apps)
	wrapperFactories map[string]WrapperFactory // wraps -> factory
}

// New creates a new empty registry.
func New() *Registry {
	return &Registry{
		apps:             make(map[string]*AppEntry),
		builtIn:          make(map[string]*AppEntry),
		wrapperFactories: make(map[string]WrapperFactory),
	}
}

// RegisterWrapperFactory registers a custom factory for wrapper apps.
// For example, "texelterm" wrappers need special handling to pass the command.
func (r *Registry) RegisterWrapperFactory(wrapsType string, factory WrapperFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wrapperFactories[wrapsType] = factory
	log.Printf("Registry: Registered wrapper factory for '%s'", wrapsType)
}

// RegisterBuiltIn registers a built-in app that's compiled into the binary.
// Built-in apps have priority over external apps with the same name.
// The factory should return a texel.App instance.
func (r *Registry) RegisterBuiltIn(manifest *Manifest, factory AppFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if manifest.Type == "" {
		manifest.Type = AppTypeBuiltIn
	}
	r.builtIn[manifest.Name] = &AppEntry{
		Manifest: manifest,
		Factory:  factory,
	}
	log.Printf("Registry: Registered built-in app '%s'", manifest.Name)
}

// Scan searches for apps in the given directory.
// Each subdirectory should contain a manifest.json file.
func (r *Registry) Scan(baseDir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear external apps (keep built-ins)
	r.apps = make(map[string]*AppEntry)

	// Check if directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		log.Printf("Registry: App directory does not exist: %s", baseDir)
		return nil // Not an error - just no apps
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("read app directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appDir := filepath.Join(baseDir, entry.Name())
		if err := r.loadApp(appDir); err != nil {
			log.Printf("Registry: Failed to load app from %s: %v", appDir, err)
			// Continue loading other apps
		}
	}

	log.Printf("Registry: Loaded %d external apps, %d built-in apps", len(r.apps), len(r.builtIn))
	return nil
}

// loadApp attempts to load a single app from a directory.
func (r *Registry) loadApp(dir string) error {
	manifest, err := LoadManifest(dir)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	if err := manifest.Validate(dir); err != nil {
		return fmt.Errorf("validate manifest: %w", err)
	}

	// Create factory based on app type
	var factory AppFactory

	switch manifest.Type {
	case AppTypeWrapper:
		// Wrapper apps create an instance of the wrapped app with custom parameters
		factory = r.createWrapperFactory(manifest)

	case AppTypeExternal:
		// External apps are not supported yet - need external process protocol
		// TODO: Implement external app launching
		factory = func() interface{} {
			log.Printf("Registry: External app launch not yet implemented: %s", manifest.Name)
			return nil
		}

	default:
		return fmt.Errorf("unsupported app type: %s", manifest.Type)
	}

	r.apps[manifest.Name] = &AppEntry{
		Manifest: manifest,
		Dir:      dir,
		Factory:  factory,
	}

	log.Printf("Registry: Loaded %s app '%s' (%s) from %s",
		manifest.Type, manifest.Name, manifest.DisplayName, dir)
	return nil
}

// createWrapperFactory creates a factory function for wrapper apps.
func (r *Registry) createWrapperFactory(manifest *Manifest) AppFactory {
	return func() interface{} {
		// Check if we have a custom wrapper factory registered
		if wrapperFactory, ok := r.wrapperFactories[manifest.Wraps]; ok {
			return wrapperFactory(manifest)
		}

		// Fallback: try to use the built-in factory directly
		wrappedEntry, ok := r.builtIn[manifest.Wraps]
		if !ok || wrappedEntry.Factory == nil {
			log.Printf("Registry: Wrapped app not found: %s (for %s)",
				manifest.Wraps, manifest.Name)
			return nil
		}

		// Generic wrapper: just create the wrapped app
		// This works for apps that don't need parameters
		log.Printf("Registry: Using generic wrapper for %s -> %s", manifest.Name, manifest.Wraps)
		return wrappedEntry.Factory()
	}
}

// Get retrieves an app entry by name.
// Returns nil if the app doesn't exist.
func (r *Registry) Get(name string) *AppEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check built-ins first
	if entry, ok := r.builtIn[name]; ok {
		return entry
	}

	return r.apps[name]
}

// List returns all available apps sorted by display name.
func (r *Registry) List() []*AppEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var entries []*AppEntry

	// Add built-in apps
	for _, entry := range r.builtIn {
		entries = append(entries, entry)
	}

	// Add external apps
	for _, entry := range r.apps {
		entries = append(entries, entry)
	}

	// Sort by display name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Manifest.DisplayName < entries[j].Manifest.DisplayName
	})

	return entries
}

// ListByCategory returns apps grouped by category.
func (r *Registry) ListByCategory() map[string][]*AppEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categories := make(map[string][]*AppEntry)

	// Add built-in apps
	for _, entry := range r.builtIn {
		category := entry.Manifest.Category
		if category == "" {
			category = "other"
		}
		categories[category] = append(categories[category], entry)
	}

	// Add external apps
	for _, entry := range r.apps {
		category := entry.Manifest.Category
		if category == "" {
			category = "other"
		}
		categories[category] = append(categories[category], entry)
	}

	// Sort entries within each category
	for _, entries := range categories {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Manifest.DisplayName < entries[j].Manifest.DisplayName
		})
	}

	return categories
}

// CreateApp creates a new instance of the named app.
// Returns nil if the app doesn't exist.
// The returned interface{} is expected to be a texel.App.
func (r *Registry) CreateApp(name string, config map[string]interface{}) interface{} {
	entry := r.Get(name)
	if entry == nil {
		log.Printf("Registry: App not found: %s", name)
		return nil
	}

	if entry.Factory == nil {
		log.Printf("Registry: App '%s' has no factory", name)
		return nil
	}

	// TODO: Pass config to factory when we support app configuration
	return entry.Factory()
}

// Count returns the total number of registered apps.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.builtIn) + len(r.apps)
}
