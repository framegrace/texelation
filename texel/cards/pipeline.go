package cards

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
	"texelation/texel"
)

// ControlFunc allows the pipeline to intercept key events before the cards see them.
// Returning true marks the event as consumed.
type ControlFunc func(*tcell.EventKey) bool

// ControllableCard allows cards to expose control capabilities on the pipeline bus.
type ControllableCard interface {
	Card
	RegisterControls(reg ControlRegistry) error
}

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
	bus     *controlBus

	runOnce  sync.Once
	stopOnce sync.Once
	wg       sync.WaitGroup
	err      error
	errMu    sync.Mutex
}

var _ texel.App = (*Pipeline)(nil)
var _ texel.SelectionHandler = (*Pipeline)(nil)
var _ texel.SelectionDeclarer = (*Pipeline)(nil)
var _ texel.MouseWheelHandler = (*Pipeline)(nil)
var _ texel.MouseWheelDeclarer = (*Pipeline)(nil)
var _ texel.ReplacerReceiver = (*Pipeline)(nil)

// NewPipeline constructs a pipeline with the provided cards. The resulting
// Pipeline implements texel.App and can be launched like any other app.
func NewPipeline(control ControlFunc, cards ...Card) *Pipeline {
	p := &Pipeline{
		cards:   append([]Card(nil), cards...),
		control: control,
		bus:     newControlBus(),
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

// Run starts all cards (once) and blocks until they complete.
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

// selectionHandler finds the first card capable of handling selections.
func (p *Pipeline) selectionHandler() texel.SelectionHandler {
	cards := p.Cards()
	for _, card := range cards {
		if decl, ok := card.(texel.SelectionDeclarer); ok && !decl.SelectionEnabled() {
			continue
		}
		if handler, ok := card.(texel.SelectionHandler); ok {
			return handler
		}
		if accessor, ok := card.(AppAccessor); ok {
			underlying := accessor.UnderlyingApp()
			if underlying == nil {
				continue
			}
			if decl, ok := underlying.(texel.SelectionDeclarer); ok && !decl.SelectionEnabled() {
				continue
			}
			if handler, ok := underlying.(texel.SelectionHandler); ok {
				return handler
			}
		}
	}
	return nil
}

func (p *Pipeline) SelectionStart(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) bool {
	if handler := p.selectionHandler(); handler != nil {
		return handler.SelectionStart(x, y, buttons, modifiers)
	}
	return false
}

func (p *Pipeline) SelectionUpdate(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) {
	if handler := p.selectionHandler(); handler != nil {
		handler.SelectionUpdate(x, y, buttons, modifiers)
	}
}

func (p *Pipeline) SelectionFinish(x, y int, buttons tcell.ButtonMask, modifiers tcell.ModMask) (string, []byte, bool) {
	if handler := p.selectionHandler(); handler != nil {
		return handler.SelectionFinish(x, y, buttons, modifiers)
	}
	return "", nil, false
}

func (p *Pipeline) SelectionCancel() {
	if handler := p.selectionHandler(); handler != nil {
		handler.SelectionCancel()
	}
}

func (p *Pipeline) SelectionEnabled() bool {
	return p.selectionHandler() != nil
}

func (p *Pipeline) wheelHandler() texel.MouseWheelHandler {
	cards := p.Cards()
	for _, card := range cards {
		if decl, ok := card.(texel.MouseWheelDeclarer); ok && !decl.MouseWheelEnabled() {
			continue
		}
		if handler, ok := card.(texel.MouseWheelHandler); ok {
			return handler
		}
		if accessor, ok := card.(AppAccessor); ok {
			underlying := accessor.UnderlyingApp()
			if underlying == nil {
				continue
			}
			if decl, ok := underlying.(texel.MouseWheelDeclarer); ok && !decl.MouseWheelEnabled() {
				continue
			}
			if handler, ok := underlying.(texel.MouseWheelHandler); ok {
				return handler
			}
		}
	}
	return nil
}

func (p *Pipeline) HandleMouseWheel(x, y, deltaX, deltaY int, modifiers tcell.ModMask) {
	if handler := p.wheelHandler(); handler != nil {
		handler.HandleMouseWheel(x, y, deltaX, deltaY, modifiers)
	}
}

func (p *Pipeline) MouseWheelEnabled() bool {
	return p.wheelHandler() != nil
}

// SetReplacer implements ReplacerReceiver by forwarding to the first card that wants it.
func (p *Pipeline) SetReplacer(replacer texel.AppReplacer) {
	cards := p.Cards()
	for _, card := range cards {
		if receiver, ok := card.(texel.ReplacerReceiver); ok {
			receiver.SetReplacer(replacer)
			return
		}
		if accessor, ok := card.(AppAccessor); ok {
			underlying := accessor.UnderlyingApp()
			if underlying == nil {
				continue
			}
			if receiver, ok := underlying.(texel.ReplacerReceiver); ok {
				receiver.SetReplacer(replacer)
				return
			}
		}
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
func (p *Pipeline) ControlBus() ControlBus {
	return p.bus
}

// OnEvent implements texel.Listener to forward events to all cards.
func (p *Pipeline) OnEvent(event texel.Event) {
	cards := p.Cards()
	for _, card := range cards {
		// Try the card directly
		if listener, ok := card.(texel.Listener); ok {
			listener.OnEvent(event)
			continue
		}
		// Try the underlying app if this is an appAdapter
		if accessor, ok := card.(AppAccessor); ok {
			underlying := accessor.UnderlyingApp()
			if underlying != nil {
				if listener, ok := underlying.(texel.Listener); ok {
					listener.OnEvent(event)
				}
			}
		}
	}
}
