// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package statusbar

import (
	"strings"
	"testing"
	"time"

	"github.com/framegrace/texelation/texel"
	"github.com/framegrace/texelui/core"
	"github.com/gdamore/tcell/v2"
)

// newTestPainter allocates a 1-row cell buffer of the given width and returns
// a Painter clipped to the full buffer.
func newTestPainter(width int) (*core.Painter, [][]core.Cell) {
	buf := make([][]core.Cell, 1)
	buf[0] = make([]core.Cell, width)
	clip := core.Rect{X: 0, Y: 0, W: width, H: 1}
	return core.NewPainter(buf, clip), buf
}

// cellsToString converts the rune content of a cell row to a string.
func cellsToString(row []core.Cell) string {
	var sb strings.Builder
	for _, c := range row {
		if c.Ch == 0 {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(c.Ch)
		}
	}
	return sb.String()
}

// TestBlendInfoLine_Render verifies that basic rendering does not panic and
// produces a non-empty (non-all-space) output when title and clock are set.
func TestBlendInfoLine_Render(t *testing.T) {
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(60, 1)
	bil.SetMode(false, 0)
	bil.SetTitle("myterm")
	bil.SetClock("12:34")
	bil.SetAccentColor(tcell.NewRGBColor(100, 120, 200))

	painter, buf := newTestPainter(60)
	painter.SetWidgetRect(core.Rect{X: 0, Y: 0, W: 60, H: 1})

	// Must not panic.
	bil.Draw(painter)

	row := buf[0]
	if len(row) != 60 {
		t.Fatalf("expected 60 cells, got %d", len(row))
	}

	// At least some cells must have a non-default background (gradient fills them).
	nonDefault := 0
	for _, c := range row {
		_, bg, _ := c.Style.Decompose()
		if bg != tcell.ColorDefault {
			nonDefault++
		}
	}
	if nonDefault == 0 {
		t.Error("expected gradient to set background colors, but all cells have default background")
	}
}

// TestBlendInfoLine_ToastMode verifies ShowToast activates toast state and
// DismissToast deactivates it.
func TestBlendInfoLine_ToastMode(t *testing.T) {
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(60, 1)

	// Initially not active.
	bil.mu.RLock()
	active := bil.toastActive
	bil.mu.RUnlock()
	if active {
		t.Fatal("toast should not be active initially")
	}

	// Activate toast.
	bil.ShowToast("hello world", texel.ToastInfo, 5*time.Second)
	bil.mu.RLock()
	active = bil.toastActive
	bil.mu.RUnlock()
	if !active {
		t.Fatal("toast should be active after ShowToast")
	}

	// Dismiss.
	bil.DismissToast()
	bil.mu.RLock()
	active = bil.toastActive
	bil.mu.RUnlock()
	if active {
		t.Fatal("toast should not be active after DismissToast")
	}
}

// TestBlendInfoLine_ToastExpiry verifies that an expired toast is auto-dismissed
// during Draw.
func TestBlendInfoLine_ToastExpiry(t *testing.T) {
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(60, 1)

	// Set a very short expiry (already expired).
	bil.ShowToast("bye", texel.ToastWarning, 1*time.Nanosecond)
	time.Sleep(2 * time.Millisecond) // ensure expiry

	painter, _ := newTestPainter(60)
	painter.SetWidgetRect(core.Rect{X: 0, Y: 0, W: 60, H: 1})
	bil.Draw(painter)

	bil.mu.RLock()
	active := bil.toastActive
	bil.mu.RUnlock()
	if active {
		t.Error("expired toast should have been dismissed during Draw")
	}
}

// TestBlendInfoLine_ToastCentered verifies that in toast mode the
// rendered row contains the toast message centered, with title and clock visible.
func TestBlendInfoLine_ToastCentered(t *testing.T) {
	const width = 80
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(width, 1)
	bil.SetMode(false, 0)
	bil.SetTitle("somepane")
	bil.SetClock("09:00")

	toastMsg := "Deployment complete"
	bil.ShowToast(toastMsg, texel.ToastSuccess, 10*time.Second)

	painter, buf := newTestPainter(width)
	painter.SetWidgetRect(core.Rect{X: 0, Y: 0, W: width, H: 1})
	bil.Draw(painter)

	rendered := cellsToString(buf[0])

	// The toast message must appear somewhere in the rendered row.
	if !strings.Contains(rendered, "Deployment") {
		t.Errorf("expected toast message %q in rendered output %q", toastMsg, rendered)
	}

	// The title should still be visible (toast is centered, not replacing).
	if !strings.Contains(rendered, "somepane") {
		t.Errorf("expected title %q to still be visible, got %q", "somepane", rendered)
	}
}

// TestBlendInfoLine_NormalContent verifies that title and clock appear in normal
// (non-toast) mode.
func TestBlendInfoLine_NormalContent(t *testing.T) {
	const width = 80
	bil := NewBlendInfoLine()
	bil.SetPosition(0, 0)
	bil.Resize(width, 1)
	bil.SetMode(false, 0)
	bil.SetTitle("myshell")
	bil.SetClock("15:30")

	painter, buf := newTestPainter(width)
	painter.SetWidgetRect(core.Rect{X: 0, Y: 0, W: width, H: 1})
	bil.Draw(painter)

	rendered := cellsToString(buf[0])

	if !strings.Contains(rendered, "myshell") {
		t.Errorf("expected title %q in rendered output %q", "myshell", rendered)
	}
	if !strings.Contains(rendered, "15:30") {
		t.Errorf("expected clock %q in rendered output %q", "15:30", rendered)
	}
}
