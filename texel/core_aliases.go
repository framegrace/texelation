// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/core_aliases.go
// Summary: Re-exports TexelUI core types for Texelation internals.

package texel

import texelcore "github.com/framegrace/texelui/core"

// Core app types.
type App = texelcore.App
type Cell = texelcore.Cell
type PasteHandler = texelcore.PasteHandler
type SnapshotProvider = texelcore.SnapshotProvider
type SnapshotFactory = texelcore.SnapshotFactory
type MouseHandler = texelcore.MouseHandler
type ClipboardService = texelcore.ClipboardService
type ClipboardAware = texelcore.ClipboardAware
type CloseRequester = texelcore.CloseRequester
type CloseCallbackRequester = texelcore.CloseCallbackRequester
type ControlBusProvider = texelcore.ControlBusProvider
type PaneIDSetter = texelcore.PaneIDSetter
type RenderPipeline = texelcore.RenderPipeline
type PipelineProvider = texelcore.PipelineProvider

// Control bus types.
type ControlHandler = texelcore.ControlHandler
type ControlCapability = texelcore.ControlCapability
type ControlBus = texelcore.ControlBus
type ControlRegistry = texelcore.ControlRegistry

// Storage types.
type StorageService = texelcore.StorageService
type AppStorage = texelcore.AppStorage
type StorageSetter = texelcore.StorageSetter
type AppStorageSetter = texelcore.AppStorageSetter

// NewControlBus provides a local helper for legacy call sites.
func NewControlBus() ControlBus {
	return texelcore.NewControlBus()
}
