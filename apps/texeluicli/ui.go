package texeluicli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"texelation/texelui/core"
	"texelation/texelui/widgets"
)

type Event struct {
	Type string
	ID   string
}

type binding struct {
	id         string
	kind       string
	widget     core.Widget
	get        func() string
	set        func(string) error
	setChecked func(bool) error
	append     func(string)
}

type Session struct {
	ID       string
	Title    string
	UI       *core.UIManager
	Root     core.Widget
	bindings map[string]*binding
	events   chan Event
	closed   bool
	closedCh chan struct{}
}

func BuildSession(spec Spec) (*Session, error) {
	ui := core.NewUIManager()
	events := make(chan Event, 64)
	root, bindings, err := buildRoot(spec, events)
	if err != nil {
		return nil, err
	}
	if root != nil {
		ui.SetRootWidget(root)
		focusTarget := root
		if padded, ok := root.(*paddedContainer); ok {
			if padded.child != nil {
				focusTarget = padded.child
			}
		}
		ui.Focus(focusTarget)
	}
	return &Session{
		ID:       newSessionID(),
		Title:    spec.Title,
		UI:       ui,
		Root:     root,
		bindings: bindings,
		events:   events,
		closedCh: make(chan struct{}),
	}, nil
}

func (s *Session) Binding(id string) (*binding, bool) {
	b, ok := s.bindings[id]
	return b, ok
}

func (s *Session) Values(ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		b, ok := s.bindings[id]
		if !ok || b.get == nil {
			return nil, fmt.Errorf("unknown widget %q", id)
		}
		out[id] = b.get()
	}
	return out, nil
}

func (s *Session) Emit(ev Event) {
	if s.closed {
		return
	}
	select {
	case s.events <- ev:
	default:
	}
}

func (s *Session) Close() {
	if s.closed {
		return
	}
	s.Emit(Event{Type: "close", ID: "session"})
	s.closed = true
	close(s.closedCh)
}

func (s *Session) Wait(filters []string) (Event, error) {
	for {
		select {
		case ev := <-s.events:
			if matchesEvent(filters, ev) {
				return ev, nil
			}
		case <-s.closedCh:
			return Event{}, errors.New("session closed")
		}
	}
}

func buildRoot(spec Spec, events chan Event) (core.Widget, map[string]*binding, error) {
	layoutType := strings.ToLower(spec.LayoutType())
	switch layoutType {
	case "form":
		return buildForm(spec, events)
	case "vbox":
		root, bindings, err := buildVBox(spec, events)
		if err != nil {
			return nil, nil, err
		}
		if spec.Layout.Padding > 0 {
			return newPaddedContainer(root, spec.Layout.Padding), bindings, nil
		}
		return root, bindings, nil
	default:
		return nil, nil, fmt.Errorf("unknown layout type %q", layoutType)
	}
}

func buildForm(spec Spec, events chan Event) (core.Widget, map[string]*binding, error) {
	cfg := widgets.DefaultFormConfig()
	if spec.Layout.Padding > 0 {
		cfg.PaddingX = spec.Layout.Padding
		cfg.PaddingY = spec.Layout.Padding
	}
	if spec.Layout.Gap > 0 {
		cfg.RowSpacing = spec.Layout.Gap
	}
	if spec.Layout.LabelWidth > 0 {
		cfg.LabelWidth = spec.Layout.LabelWidth
	}
	form := widgets.NewFormWithConfig(cfg)
	bindings := make(map[string]*binding, len(spec.Widgets))

	for _, ws := range spec.Widgets {
		w, b, err := newWidget(ws, events)
		if err != nil {
			return nil, nil, err
		}
		if err := registerBinding(bindings, ws.ID, b); err != nil {
			return nil, nil, err
		}

		switch ws.Type {
		case "textarea", "log":
			if ws.Label != "" {
				form.AddRow(widgets.FormRow{Label: widgets.NewLabel(ws.Label), Height: 1})
			}
			height := ws.Height
			if height <= 0 {
				height = 4
			}
			form.AddFullWidthField(w, height)
		case "checkbox", "button", "label":
			height := ws.Height
			if height <= 0 {
				height = 1
			}
			form.AddFullWidthField(w, height)
		default:
			height := ws.Height
			if height <= 0 {
				height = 1
			}
			if ws.Label != "" {
				form.AddRow(widgets.FormRow{
					Label:  widgets.NewLabel(ws.Label),
					Field:  w,
					Height: height,
				})
			} else {
				form.AddFullWidthField(w, height)
			}
		}
	}

	return form, bindings, nil
}

func buildVBox(spec Spec, events chan Event) (core.Widget, map[string]*binding, error) {
	vbox := widgets.NewVBox()
	if spec.Layout.Gap > 0 {
		vbox.Spacing = spec.Layout.Gap
	}
	bindings := make(map[string]*binding, len(spec.Widgets))
	labelWidth := spec.Layout.LabelWidth
	if labelWidth <= 0 {
		for _, ws := range spec.Widgets {
			if usesInlineLabel(ws.Type) || ws.Label == "" {
				continue
			}
			if len(ws.Label) > labelWidth {
				labelWidth = len(ws.Label)
			}
		}
		if labelWidth == 0 {
			labelWidth = 12
		}
	}

	for _, ws := range spec.Widgets {
		w, b, err := newWidget(ws, events)
		if err != nil {
			return nil, nil, err
		}
		if err := registerBinding(bindings, ws.ID, b); err != nil {
			return nil, nil, err
		}

		var child core.Widget = w
		if ws.Label != "" && !usesInlineLabel(ws.Type) && ws.Type != "label" {
			row := widgets.NewHBox()
			row.Spacing = 1
			label := widgets.NewLabel(ws.Label)
			label.Resize(labelWidth, 1)
			row.AddChildWithSize(label, labelWidth)
			row.AddFlexChild(w)
			child = row
		}

		if ws.Flex {
			vbox.AddFlexChild(child)
		} else {
			vbox.AddChild(child)
		}
	}

	return vbox, bindings, nil
}

func newWidget(ws WidgetSpec, events chan Event) (core.Widget, *binding, error) {
	if ws.ID == "" {
		return nil, nil, errors.New("widget id is required")
	}
	switch strings.ToLower(ws.Type) {
	case "input", "number":
		input := widgets.NewInput()
		value := ws.ValueString()
		if value != "" {
			input.Text = value
			input.CaretPos = len([]rune(value))
		}
		if ws.Placeholder != "" {
			input.Placeholder = ws.Placeholder
		}
		if ws.Width > 0 {
			input.Resize(ws.Width, 1)
		}
		input.OnChange = func(text string) {
			emitEvent(events, Event{Type: "change", ID: ws.ID})
		}
		b := &binding{
			id:     ws.ID,
			kind:   "input",
			widget: input,
			get:    func() string { return input.Text },
			set: func(val string) error {
				input.Text = val
				input.CaretPos = len([]rune(val))
				input.OffX = 0
				return nil
			},
		}
		return input, b, nil

	case "combobox":
		combo := widgets.NewComboBox(ws.Options, ws.Editable)
		value := ws.ValueString()
		if value == "" && len(ws.Options) > 0 {
			value = ws.Options[0]
		}
		if value != "" {
			combo.SetValue(value)
		}
		if ws.Width > 0 {
			combo.Resize(ws.Width, 1)
		}
		combo.OnChange = func(text string) {
			emitEvent(events, Event{Type: "change", ID: ws.ID})
		}
		b := &binding{
			id:     ws.ID,
			kind:   "combobox",
			widget: combo,
			get:    combo.Value,
			set: func(val string) error {
				combo.SetValue(val)
				return nil
			},
		}
		return combo, b, nil

	case "checkbox":
		label := ws.Label
		checkbox := widgets.NewCheckbox(label)
		checkbox.Checked = ws.ValueBool()
		checkbox.OnChange = func(checked bool) {
			emitEvent(events, Event{Type: "change", ID: ws.ID})
		}
		b := &binding{
			id:     ws.ID,
			kind:   "checkbox",
			widget: checkbox,
			get:    func() string { return strconv.FormatBool(checkbox.Checked) },
			set: func(val string) error {
				checkbox.Checked = parseBool(val)
				return nil
			},
			setChecked: func(v bool) error {
				checkbox.Checked = v
				return nil
			},
		}
		return checkbox, b, nil

	case "button":
		text := ws.Text
		if text == "" {
			text = ws.Label
		}
		button := widgets.NewButton(text)
		if ws.Width > 0 {
			button.Resize(ws.Width, 1)
		}
		button.OnClick = func() {
			emitEvent(events, Event{Type: "click", ID: ws.ID})
		}
		b := &binding{
			id:     ws.ID,
			kind:   "button",
			widget: button,
			get:    func() string { return "" },
		}
		return button, b, nil

	case "label":
		text := ws.Text
		if text == "" {
			text = ws.Label
		}
		label := widgets.NewLabel(text)
		if ws.Width > 0 {
			label.Resize(ws.Width, 1)
		}
		b := &binding{
			id:     ws.ID,
			kind:   "label",
			widget: label,
			get:    func() string { return label.Text },
			set: func(val string) error {
				label.Text = val
				if ws.Width <= 0 {
					label.Resize(len(val), 1)
				}
				return nil
			},
		}
		return label, b, nil

	case "textarea", "log":
		ta := widgets.NewTextArea()
		width := ws.Width
		height := ws.Height
		if height <= 0 {
			height = 4
		}
		if width > 0 || height > 0 {
			if width <= 0 {
				width = 20
			}
			ta.Resize(width, height)
		}
		if ws.ReadOnly {
			ta.SetFocusable(false)
		}
		if value := ws.ValueString(); value != "" {
			ta.SetText(value)
		}
		if !ws.ReadOnly && strings.ToLower(ws.Type) != "log" {
			ta.OnChange = func(text string) {
				emitEvent(events, Event{Type: "change", ID: ws.ID})
			}
		}
		b := &binding{
			id:     ws.ID,
			kind:   "textarea",
			widget: ta,
			get:    ta.Text,
			set: func(val string) error {
				ta.SetText(val)
				return nil
			},
			append: func(val string) {
				ta.SetText(ta.Text() + val)
			},
		}
		return ta, b, nil
	default:
		return nil, nil, fmt.Errorf("unknown widget type %q", ws.Type)
	}
}

func registerBinding(bindings map[string]*binding, id string, b *binding) error {
	if id == "" {
		return errors.New("widget id is required")
	}
	if _, exists := bindings[id]; exists {
		return fmt.Errorf("duplicate widget id %q", id)
	}
	bindings[id] = b
	return nil
}

func usesInlineLabel(kind string) bool {
	switch kind {
	case "checkbox", "button":
		return true
	default:
		return false
	}
}

func matchesEvent(filters []string, ev Event) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if f == "*" {
			return true
		}
		parts := strings.SplitN(f, ":", 2)
		switch len(parts) {
		case 1:
			if parts[0] == ev.Type {
				return true
			}
		case 2:
			typeMatch := parts[0] == "*" || parts[0] == ev.Type
			idMatch := parts[1] == "*" || parts[1] == ev.ID
			if typeMatch && idMatch {
				return true
			}
		}
	}
	return false
}

func parseBool(val string) bool {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func emitEvent(events chan Event, ev Event) {
	select {
	case events <- ev:
		return
	default:
	}
	if !isHighPriorityEvent(ev.Type) {
		return
	}
	select {
	case <-events:
	default:
	}
	select {
	case events <- ev:
	default:
	}
}

func isHighPriorityEvent(eventType string) bool {
	switch eventType {
	case "click", "submit", "close":
		return true
	default:
		return false
	}
}

func invalidateWidget(ui *core.UIManager, w core.Widget) {
	if ui == nil || w == nil {
		return
	}
	x, y := w.Position()
	wW, wH := w.Size()
	ui.Invalidate(core.Rect{X: x, Y: y, W: wW, H: wH})
}

type paddedContainer struct {
	core.BaseWidget
	child core.Widget
	pad   int
	inv   func(core.Rect)
}

func newPaddedContainer(child core.Widget, pad int) *paddedContainer {
	p := &paddedContainer{child: child, pad: pad}
	p.Resize(1, 1)
	p.SetFocusable(false)
	return p
}

func (p *paddedContainer) SetInvalidator(fn func(core.Rect)) {
	p.inv = fn
	if ia, ok := p.child.(core.InvalidationAware); ok {
		ia.SetInvalidator(fn)
	}
}

func (p *paddedContainer) Draw(pr *core.Painter) {
	if p.child != nil {
		p.child.Draw(pr)
	}
}

func (p *paddedContainer) Resize(w, h int) {
	p.BaseWidget.Resize(w, h)
	p.layout()
}

func (p *paddedContainer) SetPosition(x, y int) {
	p.BaseWidget.SetPosition(x, y)
	p.layout()
}

func (p *paddedContainer) HandleKey(ev *tcell.EventKey) bool {
	if p.child == nil {
		return false
	}
	return p.child.HandleKey(ev)
}

func (p *paddedContainer) HitTest(x, y int) bool {
	if p.child == nil {
		return false
	}
	return p.child.HitTest(x, y)
}

func (p *paddedContainer) VisitChildren(f func(core.Widget)) {
	if p.child != nil {
		f(p.child)
	}
}

func (p *paddedContainer) WidgetAt(x, y int) core.Widget {
	if p.child == nil {
		return nil
	}
	if ht, ok := p.child.(core.HitTester); ok {
		if hit := ht.WidgetAt(x, y); hit != nil {
			return hit
		}
	}
	if p.child.HitTest(x, y) {
		return p.child
	}
	return nil
}

func (p *paddedContainer) HandleMouse(ev *tcell.EventMouse) bool {
	if p.child == nil {
		return false
	}
	if mw, ok := p.child.(core.MouseAware); ok {
		return mw.HandleMouse(ev)
	}
	return false
}

func (p *paddedContainer) layout() {
	if p.child == nil {
		return
	}
	pad := p.pad
	if pad < 0 {
		pad = 0
	}
	childW := p.Rect.W - (pad * 2)
	childH := p.Rect.H - (pad * 2)
	if childW < 0 {
		childW = 0
	}
	if childH < 0 {
		childH = 0
	}
	p.child.SetPosition(p.Rect.X+pad, p.Rect.Y+pad)
	p.child.Resize(childW, childH)
}
