package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/apps/statusbar"
	"texelation/server"
	"texelation/texel"
)

type textApp struct {
	title   string
	message []rune
	notify  chan<- bool
}

func newTextApp(title, message string) *textApp {
	return &textApp{title: title, message: []rune(message)}
}

func (a *textApp) Run() error { return nil }
func (a *textApp) Stop()      {}

func (a *textApp) Resize(cols, rows int) {}

func (a *textApp) Render() [][]texel.Cell {
	line := string(a.message)
	buf := make([][]texel.Cell, 1)
	buf[0] = make([]texel.Cell, len(line))
	for i, ch := range line {
		buf[0][i] = texel.Cell{Ch: ch, Style: tcell.StyleDefault}
	}
	return buf
}

func (a *textApp) GetTitle() string { return a.title }

func (a *textApp) HandleKey(ev *tcell.EventKey) {
	if ev.Rune() != 0 {
		a.message = append(a.message, ev.Rune())
		if a.notify != nil {
			select {
			case a.notify <- true:
			default:
			}
		}
	}
}

func (a *textApp) SetRefreshNotifier(ch chan<- bool) { a.notify = ch }

func main() {
	socketPath := flag.String("socket", "/tmp/texelation.sock", "Unix socket path")
	title := flag.String("title", "Texel Server", "Title for the main pane")
	flag.Parse()

	manager := server.NewManager()

	simScreen := tcell.NewSimulationScreen("ansi")
	driver := texel.NewTcellScreenDriver(simScreen)
	lifecycle := &texel.LocalAppLifecycle{}

	mainApp := newTextApp(*title, "Welcome to the texel server harness. Type from the client to append text.")
	shellFactory := func() texel.App { return mainApp }
	welcomeFactory := func() texel.App { return newTextApp("welcome", "Remote desktop ready") }

	desktop, err := texel.NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create desktop: %v\n", err)
		os.Exit(1)
	}

	status := statusbar.New()
	desktop.AddStatusPane(status, texel.SideBottom, 1)

	srv := server.NewServer(*socketPath, manager)
	sink := server.NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	srv.SetPublisherFactory(func(sess *server.Session) *server.DesktopPublisher {
		publisher := server.NewDesktopPublisher(desktop, sess)
		sink.SetPublisher(publisher)
		return publisher
	})

	go func() {
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	fmt.Printf("Texel server harness listening on %s\n", *socketPath)
	fmt.Println("Use the integration test client or proto harness to connect and send key events.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Stop(ctx)
	desktop.Close()

	fmt.Println("Server stopped")
}
