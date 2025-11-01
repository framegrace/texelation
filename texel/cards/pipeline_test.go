package cards

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

type stubCard struct {
	runErr      error
	stopped     bool
	resized     [][2]int
	renders     [][]int
	refresh     chan<- bool
	handledKeys []*tcell.EventKey
}

func (s *stubCard) Run() error            { return s.runErr }
func (s *stubCard) Stop()                 { s.stopped = true }
func (s *stubCard) Resize(cols, rows int) { s.resized = append(s.resized, [2]int{cols, rows}) }
func (s *stubCard) Render(input [][]texel.Cell) [][]texel.Cell {
	s.renders = append(s.renders, []int{len(input)})
	if input == nil {
		buffer := make([][]texel.Cell, 1)
		buffer[0] = []texel.Cell{{Ch: 'A'}}
		return buffer
	}
	return input
}
func (s *stubCard) HandleKey(ev *tcell.EventKey)      { s.handledKeys = append(s.handledKeys, ev) }
func (s *stubCard) SetRefreshNotifier(ch chan<- bool) { s.refresh = ch }
func (s *stubCard) HandleMessage(msg texel.Message)   {}

type effectCard struct{}

func (effectCard) Run() error      { return nil }
func (effectCard) Stop()           {}
func (effectCard) Resize(int, int) {}
func (effectCard) Render(input [][]texel.Cell) [][]texel.Cell {
	if len(input) == 0 {
		return input
	}
	input[0][0].Ch = 'B'
	return input
}
func (effectCard) HandleKey(*tcell.EventKey)      {}
func (effectCard) SetRefreshNotifier(chan<- bool) {}
func (effectCard) HandleMessage(texel.Message)    {}

type busCard struct {
	triggered bool
}

func (b *busCard) Run() error                                 { return nil }
func (b *busCard) Stop()                                      {}
func (b *busCard) Resize(int, int)                            {}
func (b *busCard) Render(input [][]texel.Cell) [][]texel.Cell { return input }
func (b *busCard) HandleKey(*tcell.EventKey)                  {}
func (b *busCard) SetRefreshNotifier(chan<- bool)             {}
func (b *busCard) HandleMessage(texel.Message)                {}
func (b *busCard) RegisterControls(reg ControlRegistry) error {
	return reg.Register("card.trigger", "Bus test trigger", func(interface{}) error {
		b.triggered = true
		return nil
	})
}

func TestPipelineRenderOrder(t *testing.T) {
	base := &stubCard{}
	eff := effectCard{}
	p := NewPipeline(nil, base, eff)
	p.Resize(5, 3)
	buf := p.Render()
	if len(buf) != 1 || len(buf[0]) != 1 || buf[0][0].Ch != 'B' {
		t.Fatalf("unexpected render output: %+v", buf)
	}
}

func TestPipelineControlHook(t *testing.T) {
	consumed := false
	control := func(ev *tcell.EventKey) bool {
		consumed = true
		return true
	}
	base := &stubCard{}
	p := NewPipeline(control, base)
	ev := tcell.NewEventKey(tcell.KeyRune, 'x', 0)
	p.HandleKey(ev)
	if !consumed {
		t.Fatalf("expected control hook to run")
	}
	if len(base.handledKeys) != 0 {
		t.Fatalf("expected card to skip handling, got %d events", len(base.handledKeys))
	}
}

func TestPipelineResizeAndRefresh(t *testing.T) {
	base := &stubCard{}
	p := NewPipeline(nil, base)
	ch := make(chan bool)
	p.SetRefreshNotifier(ch)
	select {
	case <-ch:
		t.Fatalf("refresh channel should not receive immediately")
	default:
	}
	if base.refresh != ch {
		t.Fatalf("expected refresh channel forwarded")
	}
	p.Resize(80, 24)
	if len(base.resized) == 0 || base.resized[len(base.resized)-1] != [2]int{80, 24} {
		t.Fatalf("resize not forwarded: %+v", base.resized)
	}
}

func TestPipelineControlBusRegistersCards(t *testing.T) {
	base := &stubCard{}
	controlCard := &busCard{}
	p := NewPipeline(nil, base, controlCard)
	if p.ControlBus() == nil {
		t.Fatal("control bus not initialized")
	}
	caps := p.ControlBus().Capabilities()
	found := false
	for _, cap := range caps {
		if cap.ID == "card.trigger" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected capability registered, got %+v", caps)
	}
	if err := p.ControlBus().Trigger("card.trigger", nil); err != nil {
		t.Fatalf("triggering card control failed: %v", err)
	}
	if !controlCard.triggered {
		t.Fatal("expected card control handler to run")
	}
}

func TestPipelineControlFuncTriggersBus(t *testing.T) {
	base := &stubCard{}
	controlCard := &busCard{}
	var pipe *Pipeline
	control := func(ev *tcell.EventKey) bool {
		if ev.Key() == tcell.KeyF5 {
			if err := pipe.ControlBus().Trigger("card.trigger", nil); err != nil {
				t.Fatalf("control bus trigger failed: %v", err)
			}
			return true
		}
		return false
	}
	pipe = NewPipeline(control, base, controlCard)
	ev := tcell.NewEventKey(tcell.KeyF5, 0, 0)
	pipe.HandleKey(ev)
	if !controlCard.triggered {
		t.Fatal("expected control function to trigger bus")
	}
	if len(base.handledKeys) != 0 {
		t.Fatalf("expected base card not to receive handled key, got %d", len(base.handledKeys))
	}
}
