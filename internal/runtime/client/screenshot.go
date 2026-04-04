package clientruntime

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	texelcore "github.com/framegrace/texelui/core"
	"github.com/framegrace/texelui/graphics/textrender"
)

// takeScreenshot renders the current workspace buffer to a PNG file
// and copies it to the system clipboard.
func takeScreenshot(state *clientState) {
	buf := state.prevBuffer
	if len(buf) == 0 {
		return
	}

	coreGrid := make([][]texelcore.Cell, len(buf))
	for y, row := range buf {
		coreGrid[y] = make([]texelcore.Cell, len(row))
		for x := range row {
			coreGrid[y][x] = texelcore.Cell{
				Ch:    row[x].Ch,
				Style: row[x].Style,
			}
		}
	}

	fontPath, err := textrender.DetectFont()
	if err != nil {
		log.Printf("[SCREENSHOT] Font detection failed: %v", err)
		return
	}

	renderer, err := textrender.New(textrender.Config{FontPath: fontPath})
	if err != nil {
		log.Printf("[SCREENSHOT] Renderer creation failed: %v", err)
		return
	}

	img := renderer.Render(coreGrid)

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".texelation", "screenshots")
	os.MkdirAll(dir, 0o755)
	filename := filepath.Join(dir, fmt.Sprintf("screenshot-%s.png", time.Now().Format("2006-01-02_15-04-05")))

	f, err := os.Create(filename)
	if err != nil {
		log.Printf("[SCREENSHOT] Failed to create file: %v", err)
		return
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		log.Printf("[SCREENSHOT] Failed to encode PNG: %v", err)
		return
	}

	log.Printf("[SCREENSHOT] Saved to %s", filename)
	copyImageToClipboard(img, filename)
}

// copyImageToClipboard copies a PNG image to the system clipboard.
func copyImageToClipboard(img image.Image, filePath string) {
	// Wayland
	if path, err := exec.LookPath("wl-copy"); err == nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			cmd := exec.Command(path, "-t", "image/png")
			cmd.Stdin = &buf
			if err := cmd.Run(); err == nil {
				return
			}
		}
	}

	// X11
	if path, err := exec.LookPath("xclip"); err == nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			cmd := exec.Command(path, "-selection", "clipboard", "-t", "image/png")
			cmd.Stdin = &buf
			if err := cmd.Run(); err == nil {
				return
			}
		}
	}

	// macOS
	if path, err := exec.LookPath("osascript"); err == nil {
		script := fmt.Sprintf(`set the clipboard to (read (POSIX file %q) as «class PNGf»)`, filePath)
		exec.Command(path, "-e", script).Run()
	}
}
