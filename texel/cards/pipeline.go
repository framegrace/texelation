package cards

import (
	texelcore "github.com/framegrace/texelui/core"
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/framegrace/texelation/texel"
)

// ControlFunc allows the pipeline to intercept key events before the cards see them.
// Returning true marks the event as consumed.
type ControlFunc func(*tcell.EventKey) bool

// ControllableCard allows cards to expose control capabilities on the pipeline bus.
type ControllableCard interface {
	Card
	RegisterControls(reg texel.ControlRegistry) error
}

// Pipeline composes multiple cards into a single texelcore.App implementation.
// Cards are executed in order; the output buffer of one card becomes the
// input buffer for the next card.
type Pipeline struct {
	mu      sync.RWMutex
	cards   []Card
	width   int
	height  int
	refresh chan<- bool
	control ControlFunc
	bus     texelcore.ControlBus

	runOnce  sync.Once
	stopOnce sync.Once
	wg       sync.WaitGroup
	err      error
	errMu    sync.Mutex
}

var _ texelcore.App = (*Pipeline)(nil)
var _ texelcore.MouseHandler = (*Pipeline)(nil)
var _ texelcore.ControlBusProvider = (*Pipeline)(nil)

// NewPipeline constructs a pipeline with the provided cards. The resulting
// Pipeline implements texelcore.App and can be launched like any other app.
func NewPipeline(control ControlFunc, cards ...Card) *Pipeline {
	p := &Pipeline{
		cards:   append([]Card(nil), cards...),
		control: control,
		bus:     texelcore.NewControlBus(),
	}
	for _, card := range p.cards {
		if controllable, ok := card.(ControllableCard); ok {
			if err := controllable.RegisterControls(p.bus); err != nil {
				panic(fmt.Sprintf("cards: register controls: %v", err))
			}
		}
	}
	return p
}

// Cards returns a snapshot of the current card list.
func (p *Pipeline) Cards() []Card {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cards == nil {
		return nil
	}
	return append([]Card(nil), p.cards...)
}

// AppendCard adds a card to the end of the pipeline. The card is resized and
// connected to the current refresh channel but Run is not started automatically.
// Call StartCard to launch it when appropriate.
func (p *Pipeline) AppendCard(card Card) {
	p.mu.Lock()
	p.cards = append(p.cards, card)
	width, height := p.width, p.height
	refresh := p.refresh
	p.mu.Unlock()

	card.Resize(width, height)
	card.SetRefreshNotifier(refresh)
}

// Run starts all cards (once) and blocks until they complete.
// If any card returns an error, the pipeline will attempt to stop all other cards
// and return the error.
func (p *Pipeline) Run() error {
	p.runOnce.Do(func() {
		cards := p.Cards()
		for _, card := range cards {
			p.StartCard(card)
		}
	})
	p.wg.Wait()
	return p.Error()
}

// StartCard launches Run() for the specified card in its own goroutine.
func (p *Pipeline) StartCard(card Card) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := card.Run(); err != nil {
			p.setError(err)
			// If one card fails, we should probably stop the whole pipeline?
			// For now, just logging it via error state.
			// In a robust system, we might want to cancel a context or call Stop().
			p.terminate() // Force stop all cards to unblock Run()
		}
	}()
}

func (p *Pipeline) setError(err error) {
	if err == nil {
		return
	}
	p.errMu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.errMu.Unlock()
}

// Error returns the first error reported by any card.
func (p *Pipeline) Error() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

// Stop stops all cards and waits for them to exit.
func (p *Pipeline) Stop() {
	p.terminate()
	p.wg.Wait()
}

// terminate signals all cards to stop but does not wait.
func (p *Pipeline) terminate() {
	p.stopOnce.Do(func() {
		cards := p.Cards()
		for _, card := range cards {
			card.Stop()
		}
	})
}

// Resize propagates dimensions to all cards.
func (p *Pipeline) Resize(cols, rows int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.width, p.height = cols, rows
	cards := append([]Card(nil), p.cards...)
	p.mu.Unlock()

	for _, card := range cards {
		card.Resize(cols, rows)
	}
}

// Render executes the pipeline and returns the final buffer.
func (p *Pipeline) Render() [][]texelcore.Cell {
	cards := p.Cards()
	var buffer [][]texelcore.Cell
	for _, card := range cards {
		buffer = card.Render(buffer)
	}
	return buffer
}

// GetTitle returns the title of the first card that supports texelcore.App semantics.
func (p *Pipeline) GetTitle() string {
	if p == nil {
		return ""
	}
	cards := p.Cards()
	if len(cards) == 0 {
		return ""
	}
	if cards[0] == nil {
		return ""
	}
	if adapter, ok := cards[0].(*appAdapter); ok {
		return adapter.app.GetTitle()
	}
	if titled, ok := cards[0].(interface{ GetTitle() string }); ok {
		return titled.GetTitle()
	}
	return ""
}

// HandleKey routes the event through the control function (if provided) and
// then forwards it to all cards until one consumes it.
func (p *Pipeline) HandleKey(ev *tcell.EventKey) {
	if p.control != nil && p.control(ev) {
		return
	}
	cards := p.Cards()
	for _, card := range cards {
		card.HandleKey(ev)
	}
}

// SetRefreshNotifier stores the refresh channel and forwards it to all cards.
func (p *Pipeline) SetRefreshNotifier(ch chan<- bool) {
	p.mu.Lock()
	p.refresh = ch
	cards := append([]Card(nil), p.cards...)
	p.mu.Unlock()

	for _, card := range cards {
		card.SetRefreshNotifier(ch)
	}
}

// mouseHandler finds the first card capable of handling mouse events.
func (p *Pipeline) mouseHandler() texelcore.MouseHandler {
	cards := p.Cards()
	for _, card := range cards {
		if handler, ok := card.(texelcore.MouseHandler); ok {
			return handler
		}
		if accessor, ok := card.(AppAccessor); ok {
			underlying := accessor.UnderlyingApp()
			if underlying == nil {
				continue
			}
			if handler, ok := underlying.(texelcore.MouseHandler); ok {
				return handler
			}
		}
	}
	return nil
}

// HandleMouse implements texelcore.MouseHandler for the pipeline.
// Forwards mouse events to the first card or underlying app that handles mouse.
func (p *Pipeline) HandleMouse(ev *tcell.EventMouse) {
	if handler := p.mouseHandler(); handler != nil {
		handler.HandleMouse(ev)
	}
}

// pasteHandler finds the first card capable of handling paste events.
func (p *Pipeline) pasteHandler() interface{ HandlePaste([]byte) } {
	cards := p.Cards()
	for _, card := range cards {
		if handler, ok := card.(interface{ HandlePaste([]byte) }); ok {
			return handler
		}
		if accessor, ok := card.(AppAccessor); ok {
			underlying := accessor.UnderlyingApp()
			if underlying == nil {
				continue
			}
			if handler, ok := underlying.(interface{ HandlePaste([]byte) }); ok {
				return handler
			}
		}
	}
	return nil
}

// HandlePaste forwards paste events to the first capable card.
func (p *Pipeline) HandlePaste(data []byte) {
	if handler := p.pasteHandler(); handler != nil {
		handler.HandlePaste(data)
	}
}

// ControlBus exposes the control bus associated with this pipeline.
func (p *Pipeline) ControlBus() texelcore.ControlBus {
	return p.bus
}

// RegisterControl implements texelcore.ControlBusProvider by forwarding to the pipeline's control bus.
// This allows apps wrapped in pipelines to register control handlers without importing the cards package.
func (p *Pipeline) RegisterControl(id, description string, handler func(payload interface{}) error) error {
	// Wrap the handler to match ControlHandler type
	wrappedHandler := texel.ControlHandler(handler)
	return p.bus.Register(id, description, wrappedHandler)
}
