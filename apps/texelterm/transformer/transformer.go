// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Package transformer provides a plugin registry and pipeline for
// inline output transformers in texelterm. Transformers self-register
// at init time (like effects do) and are composed into a pipeline
// from the texelterm app config.
package transformer

import (
	"log"
	"sync"

	"github.com/framegrace/texelation/apps/texelterm/parser"
	"github.com/framegrace/texelation/config"
)

// Transformer processes committed lines in-place.
type Transformer interface {
	// HandleLine is called for each committed line.
	HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool)
	// NotifyPromptStart signals that shell integration is active.
	NotifyPromptStart()
}

// LineInserter is an optional interface that transformers can implement
// to receive a callback for inserting synthetic lines into the buffer.
type LineInserter interface {
	SetInsertFunc(fn func(beforeIdx int64, cells []parser.Cell))
}

// LineOverlayer is an optional interface that transformers can implement
// to receive a callback for setting overlay content on existing lines.
type LineOverlayer interface {
	SetOverlayFunc(fn func(lineIdx int64, cells []parser.Cell))
}

// LineSuppressor is an optional interface that transformers can implement
// to consume a line, preventing further pipeline processing and scrollback
// persistence. Used by buffering transformers like tablefmt.
type LineSuppressor interface {
	ShouldSuppress(lineIdx int64) bool
}

// LinePersistNotifier is an optional interface that transformers can implement
// to receive a callback for notifying that lines are ready for persistence.
// Used after setting overlay content on previously suppressed lines.
type LinePersistNotifier interface {
	SetPersistNotifyFunc(fn func(lineIdx int64))
}

// Config holds per-transformer configuration.
type Config map[string]interface{}

// Factory creates a Transformer from config.
type Factory func(Config) (Transformer, error)

// --- Registry ---

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register adds a transformer factory to the global registry.
// Panics on duplicate registration.
func Register(id string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[id]; exists {
		panic("transformer: duplicate registration for " + id)
	}
	registry[id] = factory
}

// Lookup returns the factory for a given transformer ID.
func Lookup(id string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[id]
	return f, ok
}

// --- Pipeline ---

// Pipeline is an ordered chain of transformers.
type Pipeline struct {
	transformers      []Transformer
	insertFunc        func(beforeIdx int64, cells []parser.Cell)
	overlayFunc       func(lineIdx int64, cells []parser.Cell)
	persistNotifyFunc func(lineIdx int64)
}

// SetInsertFunc sets the line insertion callback. The pipeline forwards
// it to any transformer that implements LineInserter.
func (p *Pipeline) SetInsertFunc(fn func(beforeIdx int64, cells []parser.Cell)) {
	p.insertFunc = fn
	for _, t := range p.transformers {
		if li, ok := t.(LineInserter); ok {
			li.SetInsertFunc(fn)
		}
	}
}

// SetOverlayFunc sets the line overlay callback. The pipeline forwards
// it to any transformer that implements LineOverlayer.
func (p *Pipeline) SetOverlayFunc(fn func(lineIdx int64, cells []parser.Cell)) {
	p.overlayFunc = fn
	for _, t := range p.transformers {
		if lo, ok := t.(LineOverlayer); ok {
			lo.SetOverlayFunc(fn)
		}
	}
}

// SetPersistNotifyFunc sets the persistence notification callback.
// The pipeline forwards it to any transformer that implements LinePersistNotifier.
func (p *Pipeline) SetPersistNotifyFunc(fn func(lineIdx int64)) {
	p.persistNotifyFunc = fn
	for _, t := range p.transformers {
		if pn, ok := t.(LinePersistNotifier); ok {
			pn.SetPersistNotifyFunc(fn)
		}
	}
}

// HandleLine dispatches to each transformer in order. Returns true if a
// transformer suppressed the line (via LineSuppressor), which signals
// the caller to skip scrollback persistence for this line.
func (p *Pipeline) HandleLine(lineIdx int64, line *parser.LogicalLine, isCommand bool) bool {
	for _, t := range p.transformers {
		t.HandleLine(lineIdx, line, isCommand)
		if sup, ok := t.(LineSuppressor); ok && sup.ShouldSuppress(lineIdx) {
			return true
		}
	}
	return false
}

// NotifyPromptStart dispatches to each transformer in order.
func (p *Pipeline) NotifyPromptStart() {
	for _, t := range p.transformers {
		t.NotifyPromptStart()
	}
}

// BuildPipeline reads the "transformers" config section and creates an
// ordered pipeline. Returns nil (no-op) if transformers are disabled or
// the section is missing.
func BuildPipeline(cfg config.Config) *Pipeline {
	if !cfg.GetBool("transformers", "enabled", true) {
		return nil
	}
	section := cfg.Section("transformers")
	if section == nil {
		return nil
	}
	rawPipeline, ok := section["pipeline"]
	if !ok {
		return nil
	}
	entries, ok := rawPipeline.([]interface{})
	if !ok {
		log.Printf("[TRANSFORMER] Invalid pipeline config: expected array, got %T", rawPipeline)
		return nil
	}

	var transformers []Transformer
	for _, entry := range entries {
		m, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		if enabled, ok := m["enabled"].(bool); ok && !enabled {
			continue
		}

		factory, found := Lookup(id)
		if !found {
			log.Printf("[TRANSFORMER] Unknown transformer %q, skipping", id)
			continue
		}
		tcfg := make(Config)
		for k, v := range m {
			if k != "id" && k != "enabled" {
				tcfg[k] = v
			}
		}
		t, err := factory(tcfg)
		if err != nil {
			log.Printf("[TRANSFORMER] Failed to create %q: %v", id, err)
			continue
		}
		transformers = append(transformers, t)
	}

	if len(transformers) == 0 {
		return nil
	}
	return &Pipeline{transformers: transformers}
}
