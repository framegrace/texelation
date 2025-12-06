package cards

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/internal/effects"
	"texelation/texel"
)

const FlashTriggerID = "effects.flash"

// EffectCard wraps any registered effect from the desktop effect registry and adapts it
// to the Card interface so we can reuse the same animation/easing implementations.
type EffectCard struct {
	mu            sync.Mutex
	effect        effects.Effect
	effectID      string
	refresh       chan<- bool
	width         int
	height        int
	updateTicker  *time.Ticker
	stopUpdate    chan struct{}
	triggerType   effects.EffectTriggerType
	triggerActive bool
	triggerKey    rune
}

// NewEffectCard creates a card that wraps an effect registered via effects.Register.
// The supplied config mirrors the JSON configuration used by texelation themes.
// The following optional keys are intercepted by the card (and removed before the
// effect is constructed):
//   - "trigger_type": string ("workspace.control" default)
//   - "trigger_active": bool (default true)
//   - "trigger_key": string (first rune) or numeric code
func NewEffectCard(effectID string, config effects.EffectConfig) (*EffectCard, error) {
	if config == nil {
		config = make(effects.EffectConfig)
	}

	triggerType := effects.TriggerWorkspaceControl
	if raw, ok := config["trigger_type"].(string); ok && raw != "" {
		if parsed, ok := effects.ParseTrigger(raw); ok {
			triggerType = parsed
		}
	}

	triggerActive := true
	if raw, ok := config["trigger_active"].(bool); ok {
		triggerActive = raw
	}

	var triggerKey rune
	if raw, ok := config["trigger_key"]; ok {
		switch v := raw.(type) {
		case string:
			if r := []rune(v); len(r) > 0 {
				triggerKey = r[0]
			}
		case float64:
			triggerKey = rune(int(v))
		case int:
			triggerKey = rune(v)
		case int64:
			triggerKey = rune(v)
		}
	}

	// Remove card-only keys before instantiating the effect.
	effectCfg := make(effects.EffectConfig, len(config))
	for k, v := range config {
		switch k {
		case "trigger_type", "trigger_active", "trigger_key":
			// Skip
		default:
			effectCfg[k] = v
		}
	}

	eff, err := effects.CreateEffect(effectID, effectCfg)
	if err != nil {
		return nil, err
	}

	return &EffectCard{
		effect:        eff,
		effectID:      effectID,
		triggerType:   triggerType,
		triggerActive: triggerActive,
		triggerKey:    triggerKey,
	}, nil
}

func (c *EffectCard) Run() error {
	c.mu.Lock()
	if c.updateTicker != nil {
		c.mu.Unlock()
		return nil
	}
	c.updateTicker = time.NewTicker(16 * time.Millisecond) // ~60fps
	c.stopUpdate = make(chan struct{})
	ticker := c.updateTicker
	stop := c.stopUpdate
	c.mu.Unlock()

	go func() {
		for {
			select {
			case <-ticker.C:
				c.mu.Lock()
				eff := c.effect
				c.mu.Unlock()

				eff.Update(time.Now())
				if eff.Active() {
					c.requestRefresh()
				}
			case <-stop:
				return
			}
		}
	}()
	return nil
}

func (c *EffectCard) Stop() {
	c.mu.Lock()
	if c.updateTicker != nil {
		c.updateTicker.Stop()
		c.updateTicker = nil
	}
	if c.stopUpdate != nil {
		close(c.stopUpdate)
		c.stopUpdate = nil
	}
	c.mu.Unlock()
}

func (c *EffectCard) Resize(cols, rows int) {
	c.mu.Lock()
	c.width = cols
	c.height = rows
	c.mu.Unlock()
}

func (c *EffectCard) HandleKey(*tcell.EventKey) {}

func (c *EffectCard) SetRefreshNotifier(ch chan<- bool) {
	c.mu.Lock()
	c.refresh = ch
	c.mu.Unlock()
}

// Render applies the wrapped effect to the input buffer. Buffers are cloned
// so the underlying app output remains untouched.
func (c *EffectCard) Render(input [][]texel.Cell) [][]texel.Cell {
	if len(input) == 0 {
		return input
	}

	c.mu.Lock()
	eff := c.effect
	c.mu.Unlock()

	if !eff.Active() {
		return input
	}

	clientBuffer := convertToClientCells(input)
	eff.ApplyWorkspace(clientBuffer)
	return convertToTexelCells(clientBuffer)
}

// Effect exposes the underlying effect instance (useful for tests).
func (c *EffectCard) Effect() effects.Effect {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.effect
}

// RegisterControls wires the effect onto the card control bus.
func (c *EffectCard) RegisterControls(reg texel.ControlRegistry) error {
	triggerID := "effects." + c.effectID
	description := "Trigger " + c.effectID + " effect"

	return reg.Register(triggerID, description, func(payload interface{}) error {
		trigger := effects.EffectTrigger{
			Type:      c.triggerType,
			Active:    c.triggerActive,
			Timestamp: time.Now(),
		}

		switch v := payload.(type) {
		case effects.EffectTrigger:
			trigger = v
		case bool:
			trigger.Active = v
		case rune:
			trigger.Key = v
		case string:
			if r := []rune(v); len(r) > 0 {
				trigger.Key = r[0]
			}
		}

		if trigger.Key == 0 && c.triggerKey != 0 {
			trigger.Key = c.triggerKey
		}

		c.effect.HandleTrigger(trigger)
		c.requestRefresh()
		return nil
	})
}

// ControllableCard implementation check.
var _ ControllableCard = (*EffectCard)(nil)

func (c *EffectCard) requestRefresh() {
	c.mu.Lock()
	refresh := c.refresh
	c.mu.Unlock()

	if refresh != nil {
		select {
		case refresh <- true:
		default:
		}
	}
}

// convertToClientCells clones a texel buffer into the client.Cell equivalent.
func convertToClientCells(buffer [][]texel.Cell) [][]client.Cell {
	if buffer == nil {
		return nil
	}
	result := make([][]client.Cell, len(buffer))
	for y, row := range buffer {
		result[y] = make([]client.Cell, len(row))
		for x, cell := range row {
			result[y][x] = client.Cell{
				Ch:    cell.Ch,
				Style: cell.Style,
			}
		}
	}
	return result
}

func convertToTexelCells(buffer [][]client.Cell) [][]texel.Cell {
	if buffer == nil {
		return nil
	}
	result := make([][]texel.Cell, len(buffer))
	for y, row := range buffer {
		result[y] = make([]texel.Cell, len(row))
		for x, cell := range row {
			result[y][x] = texel.Cell{
				Ch:    cell.Ch,
				Style: cell.Style,
			}
		}
	}
	return result
}
