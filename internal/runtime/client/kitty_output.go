// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/client/kitty_output.go
// Summary: Kitty graphics protocol output for the client renderer.
// Usage: Transmits and places images using Kitty APC sequences when the
//        terminal supports it, falling back to half-block art otherwise.

package clientruntime

import (
	"encoding/base64"
	"fmt"
	"io"

	"github.com/framegrace/texelation/client"
)

const kittyMaxChunk = 4096 // max base64 bytes per APC sequence

// kittyOutput manages Kitty graphics protocol output for the client.
type kittyOutput struct {
	transmitted map[uint32]bool // surfaceIDs already sent to terminal
	pending     []string        // queued APC sequences for this frame
}

func newKittyOutput() *kittyOutput {
	return &kittyOutput{
		transmitted: make(map[uint32]bool),
	}
}

// prepareFrame queues Kitty commands for all image placements visible
// in the current frame. It clears previous placements, ensures images
// are transmitted, and queues put commands at screen-space positions.
func (ko *kittyOutput) prepareFrame(cache *client.ImageCache, panes []*client.PaneState) {
	ko.pending = ko.pending[:0]
	// Clear all placements from previous frame (keep cached data).
	ko.pending = append(ko.pending, "\x1b_Ga=d,d=A,q=2;\x1b\\")

	for _, pane := range panes {
		if pane == nil {
			continue
		}
		placements := cache.Placements(pane.ID)
		for _, pl := range placements {
			img := cache.Get(pl.SurfaceID)
			if img == nil || len(img.Data) == 0 {
				continue
			}
			ko.ensureTransmitted(pl.SurfaceID, img.Data)
			// Content-space → screen-space: +1 for pane border.
			screenX := pane.Rect.X + 1 + pl.X
			screenY := pane.Rect.Y + 1 + pl.Y
			ko.queuePut(pl.SurfaceID, screenX, screenY, pl.W, pl.H, pl.ZIndex)
		}
	}
}

func (ko *kittyOutput) ensureTransmitted(surfaceID uint32, pngData []byte) {
	if ko.transmitted[surfaceID] {
		return
	}
	ko.transmitted[surfaceID] = true
	encoded := base64.StdEncoding.EncodeToString(pngData)
	chunks := kittyChunk(encoded, kittyMaxChunk)
	for i, chunk := range chunks {
		more := 1
		if i == len(chunks)-1 {
			more = 0
		}
		if i == 0 {
			ko.pending = append(ko.pending,
				fmt.Sprintf("\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\",
					surfaceID, more, chunk))
		} else {
			ko.pending = append(ko.pending,
				fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", more, chunk))
		}
	}
}

func (ko *kittyOutput) queuePut(surfaceID uint32, x, y, w, h, zIndex int) {
	// Move cursor to position (1-based), then place image.
	ko.pending = append(ko.pending,
		fmt.Sprintf("\x1b[%d;%dH\x1b_Ga=p,i=%d,c=%d,r=%d,z=%d,q=2;\x1b\\",
			y+1, x+1, surfaceID, w, h, zIndex))
}

// deleteImage removes a cached image from the terminal.
func (ko *kittyOutput) deleteImage(surfaceID uint32) {
	delete(ko.transmitted, surfaceID)
	ko.pending = append(ko.pending,
		fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", surfaceID))
}

// flush writes all queued APC sequences to the writer (typically the TTY).
func (ko *kittyOutput) flush(w io.Writer) error {
	for _, cmd := range ko.pending {
		if _, err := io.WriteString(w, cmd); err != nil {
			return err
		}
	}
	ko.pending = ko.pending[:0]
	return nil
}

func kittyChunk(s string, n int) []string {
	if len(s) <= n {
		return []string{s}
	}
	var chunks []string
	for len(s) > n {
		chunks = append(chunks, s[:n])
		s = s[n:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}
