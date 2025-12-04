// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/desktop_publisher_test.go
// Summary: Exercises desktop publisher behaviour to ensure the server runtime remains reliable.
// Usage: Executed during `go test` to guard against regressions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"texelation/protocol"
	"texelation/texel"
)

type publisherScreenDriver struct{}

func (publisherScreenDriver) Init() error                                    { return nil }
func (publisherScreenDriver) Fini()                                          {}
func (publisherScreenDriver) Size() (int, int)                               { return 80, 24 }
func (publisherScreenDriver) SetStyle(tcell.Style)                           {}
func (publisherScreenDriver) HideCursor()                                    {}
func (publisherScreenDriver) Show()                                          {}
func (publisherScreenDriver) PollEvent() tcell.Event                         { return nil }
func (publisherScreenDriver) SetContent(int, int, rune, []rune, tcell.Style) {}
func (publisherScreenDriver) GetContent(int, int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type simpleApp struct {
	title string
}

func (s *simpleApp) Run() error            { return nil }
func (s *simpleApp) Stop()                 {}
func (s *simpleApp) Resize(cols, rows int) {}
func (s *simpleApp) Render() [][]texel.Cell {
	return [][]texel.Cell{{{Ch: 'a', Style: tcell.StyleDefault}}}
}
func (s *simpleApp) GetTitle() string               { return s.title }
func (s *simpleApp) HandleKey(ev *tcell.EventKey)   {}
func (s *simpleApp) SetRefreshNotifier(chan<- bool) {}

func TestDesktopPublisherProducesDiffs(t *testing.T) {
	driver := publisherScreenDriver{}
	lifecycle := texel.NoopAppLifecycle{}

	shellFactory := func() texel.App { return &simpleApp{title: "shell"} }
	welcomeFactory := func() texel.App { return &simpleApp{title: "welcome"} }

	desktop, err := texel.NewDesktopEngineWithDriver(driver, shellFactory, "", lifecycle)
	if err != nil {
		t.Fatalf("desktop init failed: %v", err)
	}

	session := NewSession([16]byte{1}, 512)
	publisher := NewDesktopPublisher(desktop, session)
	if err := publisher.Publish(); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	diffs := session.Pending(0)
	if len(diffs) == 0 {
		t.Fatalf("expected at least one diff")
	}

	for _, diff := range diffs {
		if diff.Message.Type != protocol.MsgBufferDelta {
			t.Fatalf("unexpected message type %v", diff.Message.Type)
		}
		delta, err := protocol.DecodeBufferDelta(diff.Payload)
		if err != nil {
			t.Fatalf("decode delta failed: %v", err)
		}
		if len(delta.Rows) == 0 {
			t.Fatalf("expected rows in delta")
		}
	}
}
