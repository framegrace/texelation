package welcome

import "testing"

func TestSimpleColoredWelcomeRender(t *testing.T) {
	app := NewSimpleColored()
	app.Resize(50, 4)
	if err := app.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	buf := app.Render()
	if len(buf) != 4 || len(buf[0]) != 50 {
		t.Fatalf("unexpected buffer dimensions: %dx%d", len(buf), len(buf[0]))
	}

	found := false
	for _, row := range buf {
		for _, cell := range row {
			if cell.Ch == 'W' {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("expected welcome text in buffer")
	}
	app.Stop()
}
