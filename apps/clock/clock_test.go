package clock

import "testing"

func TestClockRenderDimensions(t *testing.T) {
	app := NewClockApp().(*clockApp)
	app.Resize(20, 3)
	buf := app.Render()
	if len(buf) != 3 || len(buf[0]) != 20 {
		t.Fatalf("unexpected buffer dimensions: %dx%d", len(buf), len(buf[0]))
	}
	app.Stop()
}
