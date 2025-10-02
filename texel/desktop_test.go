package texel

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

type stubScreenDriver struct {
	width, height int
	initCalled    bool
	finiCalled    bool
	hideCursor    bool
	setStyle      bool
}

func (s *stubScreenDriver) Init() error {
	s.initCalled = true
	return nil
}

func (s *stubScreenDriver) Fini() {
	s.finiCalled = true
}

func (s *stubScreenDriver) Size() (int, int) {
	if s.width == 0 {
		s.width = 80
	}
	if s.height == 0 {
		s.height = 24
	}
	return s.width, s.height
}

func (s *stubScreenDriver) SetStyle(style tcell.Style) {
	s.setStyle = true
}

func (s *stubScreenDriver) HideCursor() {
	s.hideCursor = true
}

func (s *stubScreenDriver) Show() {}

func (s *stubScreenDriver) PollEvent() tcell.Event { return nil }

func (s *stubScreenDriver) SetContent(x, y int, mainc rune, combc []rune, style tcell.Style) {}

func (s *stubScreenDriver) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	return ' ', nil, tcell.StyleDefault, 1
}

type trackingLifecycle struct {
	started []App
	stopped []App
}

func (l *trackingLifecycle) StartApp(app App) {
	l.started = append(l.started, app)
}

func (l *trackingLifecycle) StopApp(app App) {
	l.stopped = append(l.stopped, app)
}

type fakeApp struct {
	title    string
	cols     int
	rows     int
	stopped  bool
	notifier chan<- bool
}

func newFakeApp(title string) *fakeApp {
	return &fakeApp{title: title}
}

func (f *fakeApp) Run() error { return nil }
func (f *fakeApp) Stop()      { f.stopped = true }
func (f *fakeApp) Resize(cols, rows int) {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	f.cols, f.rows = cols, rows
}
func (f *fakeApp) Render() [][]Cell {
	if f.rows == 0 {
		f.rows = 1
	}
	if f.cols == 0 {
		f.cols = 1
	}
	buf := make([][]Cell, f.rows)
	for r := range buf {
		buf[r] = make([]Cell, f.cols)
	}
	return buf
}
func (f *fakeApp) GetTitle() string                  { return f.title }
func (f *fakeApp) HandleKey(ev *tcell.EventKey)      {}
func (f *fakeApp) SetRefreshNotifier(ch chan<- bool) { f.notifier = ch }

func TestDesktopWithInjectedDriverAndLifecycle(t *testing.T) {
	driver := &stubScreenDriver{}
	lifecycle := &trackingLifecycle{}

	shellFactory := func() App { return newFakeApp("shell") }
	welcomeFactory := func() App { return newFakeApp("welcome") }

	desktop, err := NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		t.Fatalf("expected desktop, got error %v", err)
	}

	if !driver.initCalled || !driver.hideCursor || !driver.setStyle {
		t.Fatalf("driver was not initialised correctly: %+v", driver)
	}

	if len(lifecycle.started) != 1 {
		t.Fatalf("expected welcome app to start, got %d", len(lifecycle.started))
	}

	statusApp := newFakeApp("status")
	desktop.AddStatusPane(statusApp, SideTop, 1)

	if len(lifecycle.started) != 2 {
		t.Fatalf("expected status app to start, got %d", len(lifecycle.started))
	}

	desktop.Close()

	if len(lifecycle.stopped) != 2 {
		t.Fatalf("expected two apps to stop, got %d", len(lifecycle.stopped))
	}

	if !driver.finiCalled {
		t.Fatalf("driver was not closed")
	}
}
