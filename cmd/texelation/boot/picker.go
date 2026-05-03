// Copyright © 2026 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: cmd/texelation/boot/picker.go
// Summary: Stored-session recovery picker UI (issue #199 Plan F.1).
// Owns the tcell screen for the duration of the user's selection;
// hands off to the splash + clientrt pipeline once a choice is made.

package boot

import (
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"

	"github.com/framegrace/texelation/protocol"
	core "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/widgets"
)

// PickerClient is the network surface the picker needs. The boot
// runner constructs a real implementation against the unix socket;
// tests inject a fake.
type PickerClient interface {
	ListSessions() (protocol.ListSessionsResponse, error)
	RecoverSession(id [16]byte, newLabel string) error
	RenameSession(id [16]byte, newLabel string) error
	DeleteSession(id [16]byte) error
	FetchThumbnail(id [16]byte) ([]byte, error)
	StartFreshSession()
}

type pickerMode int

const (
	modeBrowse pickerMode = iota
	modeRename
	modeDeleteConfirm
)

type pickerTab int

const (
	tabLive pickerTab = iota
	tabStored
)

type pickerChoice int

const (
	choiceNone pickerChoice = iota
	choiceRecover
	choiceFresh
	choiceQuit
)

// Public re-exports for the choice constants so main.go can
// reference them.
const (
	PickerChoiceNone    = choiceNone
	PickerChoiceRecover = choiceRecover
	PickerChoiceFresh   = choiceFresh
	PickerChoiceQuit    = choiceQuit
)

const maxFetchAttempts = 3

// Picker holds the picker's runtime state.
//
// mu guards thumbCache + pending + imgCache + failedFetches against
// concurrent access from the lazy fetch goroutines spawned in Task 17.
type Picker struct {
	screen tcell.Screen
	client PickerClient

	response    protocol.ListSessionsResponse
	activeTab   pickerTab
	selectedIdx int
	mode        pickerMode

	renameBuf []rune

	// errMsg, when non-empty, is rendered as a red banner above the
	// action bar. RefreshCatalog / Recover / Rename / Delete set it
	// when their underlying op fails; user dismisses by pressing any
	// navigation key. Sticky-until-dismissed because it's tied to a
	// discrete user action.
	errMsg string

	// flushErrMsg is the per-frame variant set when the graphics
	// provider's Flush fails during Render. Cleared on the next
	// successful Flush so a single hiccup doesn't pin a stale banner.
	flushErrMsg string

	// gp drives image rendering through the texelui widgets.Image
	// pipeline. nil = ASCII-only fallback.
	gp core.GraphicsProvider

	// gpFlush is where queued APC sequences (Kitty) are written.
	gpFlush io.Writer

	// cellBuf is the painter's destination, re-allocated on resize.
	cellBuf [][]core.Cell

	mu            sync.Mutex
	thumbCache    map[[16]byte][]byte
	pending       map[[16]byte]bool
	imgCache      map[[16]byte]*widgets.Image
	failedFetches map[[16]byte]int

	done   bool
	choice pickerChoice
}

// NewPicker returns a picker bound to screen + client.
func NewPicker(screen tcell.Screen, client PickerClient) *Picker {
	return &Picker{
		screen:        screen,
		client:        client,
		activeTab:     tabStored,
		mode:          modeBrowse,
		thumbCache:    make(map[[16]byte][]byte),
		pending:       make(map[[16]byte]bool),
		imgCache:      make(map[[16]byte]*widgets.Image),
		failedFetches: make(map[[16]byte]int),
	}
}

// SetGraphicsProvider wires the texelui GraphicsProvider used to render
// thumbnails. flushTo is where queued APC sequences (Kitty) get written
// after each Render — typically os.Stdout in production, io.Discard or
// nil in tests.
func (p *Picker) SetGraphicsProvider(gp core.GraphicsProvider, flushTo io.Writer) {
	p.gp = gp
	p.gpFlush = flushTo
}

// hasGraphics reports whether the wired provider can render images.
// Used to gate fetch dispatch + the widgets.Image draw branch.
func (p *Picker) hasGraphics() bool {
	return p.gp != nil && p.gp.Capability() >= core.GraphicsHalfBlock
}

// RefreshCatalog fetches the catalog from the server. On error the
// previous response is preserved (so a transient socket blip doesn't
// wipe a freshly-shown list) and errMsg is set so the user sees a
// banner rather than silently emptying.
func (p *Picker) RefreshCatalog() {
	resp, err := p.client.ListSessions()
	if err != nil {
		p.errMsg = "Could not load sessions: " + err.Error()
		return
	}
	p.errMsg = ""
	p.response = resp
	if p.selectedIdx >= len(p.response.Stored) {
		p.selectedIdx = 0
	}
}

// SelectedIdx returns the currently highlighted index. Exposed for tests.
func (p *Picker) SelectedIdx() int { return p.selectedIdx }

// Done reports whether the picker has chosen an action.
func (p *Picker) Done() bool { return p.done }

// Choice returns what the user picked once Done() is true.
func (p *Picker) Choice() pickerChoice { return p.choice }
