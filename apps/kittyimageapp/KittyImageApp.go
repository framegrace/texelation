package apps

import (
	"bytes"
	"color"
	"encoding/base64"
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"image"
	"image/draw"
	"image/png"
	"log"
	"math"
	"sync"
	"texelation/texel"
	"time"
)

// KittyImageApp renders real-time graphics using the Kitty image protocol
type KittyImageApp struct {
	width, height int
	mu            sync.RWMutex
	refreshChan   chan<- bool
	stop          chan struct{}

	// Image buffer and rendering
	canvas  *image.RGBA
	imageID uint32

	// Animation state
	frameCount uint64
	lastFrame  time.Time
	fps        float64

	// Font rendering (for clocks, text, etc.)
	font     *truetype.Font
	fontFace font.Face

	// Animation mode
	mode string // "clock", "demo", "video", etc.
}

// NewKittyImageApp creates a new Kitty image protocol app
func NewKittyImageApp(mode string) texel.App {
	app := &KittyImageApp{
		stop:       make(chan struct{}),
		imageID:    uint32(time.Now().UnixNano() & 0xFFFFFFFF), // Unique image ID
		mode:       mode,
		frameCount: 0,
		lastFrame:  time.Now(),
	}

	// Load a default font (you'd need to embed or load a TTF font)
	// For now, we'll use a placeholder
	app.loadFont()

	return app
}

func (a *KittyImageApp) loadFont() {
	// This would load a TTF font - for demo purposes, we'll skip this
	// You'd typically embed a font file or load from system fonts
	log.Printf("KittyImageApp: Font loading placeholder")
}

func (a *KittyImageApp) HandleKey(ev *tcell.EventKey) {
	// Could handle key presses to change animation modes, etc.
}

func (a *KittyImageApp) SetRefreshNotifier(refreshChan chan<- bool) {
	a.refreshChan = refreshChan
}

func (a *KittyImageApp) Run() error {
	// Animation loop - run at 60fps
	ticker := time.NewTicker(16 * time.Millisecond) // ~60fps
	defer ticker.Stop()

	log.Printf("KittyImageApp: Starting animation loop for mode '%s'", a.mode)

	for {
		select {
		case <-ticker.C:
			a.updateFrame()
			if a.refreshChan != nil {
				select {
				case a.refreshChan <- true:
				default:
				}
			}
		case <-a.stop:
			return nil
		}
	}
}

func (a *KittyImageApp) Stop() {
	close(a.stop)
}

func (a *KittyImageApp) Resize(cols, rows int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Each character cell is roughly 8x16 pixels in most terminals
	// Adjust these based on your terminal's cell size
	pixelWidth := cols * 8
	pixelHeight := rows * 16

	a.width, a.height = cols, rows

	log.Printf("KittyImageApp: Resized to %dx%d chars (%dx%d pixels)",
		cols, rows, pixelWidth, pixelHeight)

	// Create new canvas
	a.canvas = image.NewRGBA(image.Rect(0, 0, pixelWidth, pixelHeight))
}

func (a *KittyImageApp) updateFrame() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.canvas == nil {
		return
	}

	// Update FPS calculation
	now := time.Now()
	if !a.lastFrame.IsZero() {
		a.fps = 1.0 / now.Sub(a.lastFrame).Seconds()
	}
	a.lastFrame = now
	a.frameCount++

	// Clear canvas
	draw.Draw(a.canvas, a.canvas.Bounds(), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)

	// Render based on mode
	switch a.mode {
	case "clock":
		a.renderClock()
	case "demo":
		a.renderDemo()
	case "video":
		a.renderVideo()
	default:
		a.renderDemo()
	}
}

func (a *KittyImageApp) renderClock() {
	if a.canvas == nil {
		return
	}

	bounds := a.canvas.Bounds()
	now := time.Now()

	// Simple digital clock rendering
	timeStr := now.Format("15:04:05")

	// If we had font rendering set up:
	// Draw time string in the center
	// For now, we'll draw colored rectangles as a placeholder

	// Create animated background based on seconds
	seconds := float64(now.Second())
	hue := (seconds / 60.0) * 360.0

	// Simple gradient effect
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Create a simple animated pattern
			wave := math.Sin(float64(x)*0.1 + float64(a.frameCount)*0.1)
			intensity := uint8((wave + 1.0) * 127.5)

			r := uint8(float64(intensity) * math.Sin(hue*math.Pi/180.0))
			g := uint8(float64(intensity) * math.Sin((hue+120)*math.Pi/180.0))
			b := uint8(float64(intensity) * math.Sin((hue+240)*math.Pi/180.0))

			a.canvas.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}

	log.Printf("KittyImageApp: Rendered clock frame %d (%.1f fps)", a.frameCount, a.fps)
}

func (a *KittyImageApp) renderDemo() {
	if a.canvas == nil {
		return
	}

	bounds := a.canvas.Bounds()

	// Animated rainbow plasma effect
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// Create plasma effect
			t := float64(a.frameCount) * 0.1

			plasma := math.Sin(float64(x)*0.1+t) +
				math.Sin(float64(y)*0.08+t*1.2) +
				math.Sin((float64(x)+float64(y))*0.05+t*0.8) +
				math.Sin(math.Sqrt(float64(x*x+y*y))*0.1+t*1.5)

			// Convert plasma to RGB
			normalized := (plasma + 4.0) / 8.0 // Normalize to 0-1

			r := uint8(255 * (math.Sin(normalized*math.Pi*2.0+0) + 1) / 2)
			g := uint8(255 * (math.Sin(normalized*math.Pi*2.0+2.094) + 1) / 2)
			b := uint8(255 * (math.Sin(normalized*math.Pi*2.0+4.188) + 1) / 2)

			a.canvas.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
}

func (a *KittyImageApp) renderVideo() {
	// This would render video frames
	// You could integrate with FFmpeg bindings or load video files
	a.renderDemo() // Placeholder
}

func (a *KittyImageApp) Render() [][]texel.Cell {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.width <= 0 || a.height <= 0 || a.canvas == nil {
		return [][]texel.Cell{}
	}

	// Create cell buffer
	buffer := make([][]texel.Cell, a.height)
	for i := range buffer {
		buffer[i] = make([]texel.Cell, a.width)
	}

	// Convert image to Kitty protocol escape sequence
	kittySequence := a.generateKittySequence()

	// For now, put the Kitty sequence in the first cell
	// In a real implementation, you'd need to handle the escape sequence properly
	if len(buffer) > 0 && len(buffer[0]) > 0 {
		// The Kitty protocol sequence would be written to the terminal
		// For demonstration, we'll show some info about the frame
		infoStr := fmt.Sprintf("Frame %d (%.1ffps)", a.frameCount, a.fps)
		for i, ch := range infoStr {
			if i < a.width {
				buffer[0][i] = texel.Cell{
					Ch:    ch,
					Style: tcell.StyleDefault.Foreground(tcell.ColorWhite),
				}
			}
		}

		// The actual image would be displayed via the Kitty protocol
		// This requires writing the escape sequence directly to the terminal
		log.Printf("KittyImageApp: Generated Kitty sequence (%d bytes)", len(kittySequence))
	}

	return buffer
}

func (a *KittyImageApp) generateKittySequence() string {
	if a.canvas == nil {
		return ""
	}

	// Convert image to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, a.canvas); err != nil {
		log.Printf("KittyImageApp: Failed to encode PNG: %v", err)
		return ""
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	// Generate Kitty protocol sequence
	// Format: \x1b_G<control_data>;<payload>\x1b\
	// For real-time updates, we'd use the same image ID and update in place
	sequence := fmt.Sprintf("\x1b_Gi=%d,f=100,a=T,q=2;%s\x1b\\", a.imageID, encoded)

	return sequence
}

func (a *KittyImageApp) GetTitle() string {
	return fmt.Sprintf("Kitty Graphics (%s)", a.mode)
}

// Add missing imports at the top:

// Factory functions for different modes
func NewKittyClockApp() texel.App {
	return NewKittyImageApp("clock")
}

func NewKittyDemoApp() texel.App {
	return NewKittyImageApp("demo")
}

func NewKittyVideoApp() texel.App {
	return NewKittyImageApp("video")
}
