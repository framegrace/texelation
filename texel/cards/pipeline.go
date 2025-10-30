package cards

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// ControlFunc allows the pipeline to intercept key events before the cards see them.
// Returning true marks the event as consumed.
type ControlFunc func(*tcell.EventKey) bool

// Pipeline composes multiple cards into a single texel.App implementation.
// Cards are executed in order; the output buffer of one card becomes the
// input buffer for the next card.
type Pipeline struct {
	mu      sync.RWMutex
	cards   []Card
	width   int
	height  int
	refresh chan<- bool
	control ControlFunc

	runOnce  sync.Once
	stopOnce sync.Once
	wg       sync.WaitGroup
	err      error
	errMu    sync.Mutex
}

var _ texel.App = (*Pipeline)(nil)

// NewPipeline constructs a pipeline with the provided cards. The resulting
// Pipeline implements texel.App and can be launched like any other app.
func NewPipeline(control ControlFunc, cards ...Card) *Pipeline {
	return &Pipeline{cards: append([]Card(nil), cards...), control: control}
}

// Cards returns a snapshot of the current card list.
func (p *Pipeline) Cards() []Card {
	p.mu.RLock()
	defer p.mu.RUnlock()
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

// StartCard launches Run() for the specified card in its own goroutine.
func (p *Pipeline) StartCard(card Card) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := card.Run(); err != nil {
			p.setError(err)
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

// Run starts all cards concurrently.
func (p *Pipeline) Run() error {
	cards := p.Cards()
	p.runOnce.Do(func() {
		for _, card := range cards {
			p.StartCard(card)
		}
	})
	return nil
}

// Stop stops all cards and waits for them to exit.
func (p *Pipeline) Stop() {
	p.stopOnce.Do(func() {
		cards := p.Cards()
		for _, card := range cards {
			card.Stop()
		}
	})
	p.wg.Wait()
}

// Resize propagates dimensions to all cards.
func (p *Pipeline) Resize(cols, rows int) {
	p.mu.Lock()
	p.width, p.height = cols, rows
	cards := append([]Card(nil), p.cards...)
	p.mu.Unlock()

	for _, card := range cards {
		card.Resize(cols, rows)
	}
}

// Render executes the pipeline and returns the final buffer.
func (p *Pipeline) Render() [][]texel.Cell {
	cards := p.Cards()
	var buffer [][]texel.Cell
	for _, card := range cards {
		buffer = card.Render(buffer)
	}
	return buffer
}

// GetTitle returns the title of the first card that supports texel.App semantics.
func (p *Pipeline) GetTitle() string {
	cards := p.Cards()
	if len(cards) == 0 {
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

// HandleMessage broadcasts messages to all cards.
func (p *Pipeline) HandleMessage(msg texel.Message) {
	cards := p.Cards()
	for _, card := range cards {
		card.HandleMessage(msg)
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
