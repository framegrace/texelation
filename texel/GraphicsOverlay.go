// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: texel/GraphicsOverlay.go
// Summary: Implements GraphicsOverlay capabilities for the core desktop engine.
// Usage: Used throughout the project to implement GraphicsOverlay inside the desktop and panes.
// Notes: Legacy desktop logic migrated from the monolithic application.

package texel

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"time"
)

// SimpleGraphicsOverlay - back to basics, no flashing
type SimpleGraphicsOverlay struct {
	currentImageID uint32
	lastUpdate     time.Time
}

// NewSimpleGraphicsOverlay creates a simple overlay manager
func NewSimpleGraphicsOverlay() *SimpleGraphicsOverlay {
	return &SimpleGraphicsOverlay{
		currentImageID: 0,
		lastUpdate:     time.Now(),
	}
}

// DrawPersistentGraphics draws graphics that persist until replaced
func (overlay *SimpleGraphicsOverlay) DrawPersistentGraphics(x, y, width, height int) {
	// Throttle updates
	if time.Since(overlay.lastUpdate) < 100*time.Millisecond {
		return
	}
	overlay.lastUpdate = time.Now()

	log.Printf("SimpleGraphicsOverlay: Drawing persistent graphics at (%d,%d) size %dx%d", x, y, width, height)

	// Create image
	pixelWidth := width * 8
	pixelHeight := height * 16

	// Keep it small
	if pixelWidth > 160 {
		pixelWidth = 160
	}
	if pixelHeight > 128 {
		pixelHeight = 128
	}

	img := overlay.createAnimatedImage(pixelWidth, pixelHeight)

	// Generate new image ID - this replaces the old image automatically
	overlay.currentImageID = overlay.generateImageID()

	// Create Kitty sequence
	sequence := overlay.imageToKittySequence(img, overlay.currentImageID)
	if sequence == "" {
		return
	}

	// Draw at position
	overlay.drawAt(sequence, x, y)

	log.Printf("SimpleGraphicsOverlay: Drew persistent image ID %d", overlay.currentImageID)
}

// ClearGraphics clears current graphics
func (overlay *SimpleGraphicsOverlay) ClearGraphics() {
	if overlay.currentImageID != 0 {
		clearSeq := fmt.Sprintf("\x1b_Ga=d,i=%d\x1b\\", overlay.currentImageID)
		os.Stdout.WriteString(clearSeq)
		os.Stdout.Sync()
		overlay.currentImageID = 0
		log.Printf("SimpleGraphicsOverlay: Cleared graphics")
	}
}

// createAnimatedImage creates a simple animated image
func (overlay *SimpleGraphicsOverlay) createAnimatedImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Simple animation based on time
	t := float64(time.Now().UnixNano()/1000000) * 0.001 // milliseconds to seconds

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Create a moving wave pattern
			wave := 0.5 + 0.5*(0.3*(1+float64(x)/float64(width))+
				0.3*(1+float64(y)/float64(height))+
				0.4*(1+float64(x+y)/float64(width+height))+
				0.2*(1+0.1*t)) // time component for animation

			// Convert to color
			r := uint8(wave * 255 * 0.8) // Red component
			g := uint8(wave * 255 * 0.6) // Green component
			b := uint8(wave * 255 * 1.0) // Blue component

			// Add border
			if x < 2 || x >= width-2 || y < 2 || y >= height-2 {
				r, g, b = 255, 255, 255 // White border
			}

			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}

	return img
}

// imageToKittySequence converts image to Kitty protocol
func (overlay *SimpleGraphicsOverlay) imageToKittySequence(img *image.RGBA, imageID uint32) string {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Printf("SimpleGraphicsOverlay: PNG encode failed: %v", err)
		return ""
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Use placement action with cursor control
	sequence := fmt.Sprintf("\x1b_Gi=%d,a=T,f=100,C=1;%s\x1b\\", imageID, encoded)

	return sequence
}

// drawAt draws graphics at specific position
func (overlay *SimpleGraphicsOverlay) drawAt(sequence string, x, y int) {
	fmt.Print("\x1b[s")                 // Save cursor
	fmt.Printf("\x1b[%d;%dH", y+1, x+1) // Move (1-based coords)
	os.Stdout.WriteString(sequence)     // Draw
	fmt.Print("\x1b[u")                 // Restore cursor
	os.Stdout.Sync()
}

// generateImageID creates unique image ID
func (overlay *SimpleGraphicsOverlay) generateImageID() uint32 {
	return uint32(time.Now().UnixNano() & 0xFFFFFFFF)
}

// SmoothGraphicsTestApp - simplified app with smooth graphics
type SmoothGraphicsTestApp struct {
	width, height int
	overlay       *SimpleGraphicsOverlay
	ticker        *time.Ticker
	stop          chan struct{}
	frameCount    int
}

// NewSmoothGraphicsTestApp creates the smooth test app
func NewSmoothGraphicsTestApp() *SmoothGraphicsTestApp {
	return &SmoothGraphicsTestApp{
		overlay: NewSimpleGraphicsOverlay(),
		stop:    make(chan struct{}),
	}
}

func (a *SmoothGraphicsTestApp) Run() error {
	// Update every 200ms for smooth animation without overwhelming
	a.ticker = time.NewTicker(200 * time.Millisecond)
	defer a.ticker.Stop()

	log.Printf("SmoothGraphicsTestApp: Starting smooth graphics")

	for {
		select {
		case <-a.ticker.C:
			// Draw persistent graphics that replace the previous ones
			a.overlay.DrawPersistentGraphics(2, 2, 12, 6)
			a.frameCount++
		case <-a.stop:
			a.overlay.ClearGraphics()
			return nil
		}
	}
}

func (a *SmoothGraphicsTestApp) Stop() {
	close(a.stop)
	if a.ticker != nil {
		a.ticker.Stop()
	}
	a.overlay.ClearGraphics()
}

func (a *SmoothGraphicsTestApp) Resize(cols, rows int) {
	a.width, a.height = cols, rows
}

func (a *SmoothGraphicsTestApp) Render() [][]Cell {
	if a.width <= 0 || a.height <= 0 {
		return [][]Cell{}
	}

	buffer := make([][]Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]Cell, a.width)
		for j := range buffer[i] {
			buffer[i][j] = Cell{Ch: ' ', Style: tcell.StyleDefault}
		}
	}

	messages := []string{
		"Smooth Graphics Test",
		"",
		"Graphics should appear below this text",
		"and animate smoothly without flashing.",
		"",
		"Frame: " + fmt.Sprintf("%d", a.frameCount),
		"",
		"Controls:",
		"  c/C - Clear graphics",
		"  r/R - Restart graphics",
		"",
		"The graphics overlay appears here:",
		"[GRAPHICS REGION]",
	}

	for i, msg := range messages {
		if i < a.height {
			for j, ch := range msg {
				if j < a.width {
					style := tcell.StyleDefault.Foreground(tcell.ColorWhite)
					if i == 11 { // Graphics region marker
						style = tcell.StyleDefault.Foreground(tcell.ColorGreen)
					}
					buffer[i][j] = Cell{Ch: ch, Style: style}
				}
			}
		}
	}

	return buffer
}

func (a *SmoothGraphicsTestApp) GetTitle() string {
	return "Smooth Graphics Test"
}

func (a *SmoothGraphicsTestApp) HandleKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'c', 'C':
			log.Printf("SmoothGraphicsTestApp: Clearing graphics")
			a.overlay.ClearGraphics()
		case 'r', 'R':
			log.Printf("SmoothGraphicsTestApp: Restarting graphics")
			a.overlay.ClearGraphics()
			time.Sleep(100 * time.Millisecond)
			a.overlay.DrawPersistentGraphics(2, 2, 12, 6)
		}
	}
}

func (a *SmoothGraphicsTestApp) SetRefreshNotifier(refreshChan chan<- bool) {
	// Not needed
}

// Factory function
func NewSmoothGraphicsApp() App {
	return NewSmoothGraphicsTestApp()
}
