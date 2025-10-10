package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	"texelation/client"
	"texelation/protocol"
	"texelation/server"
	"texelation/texel"
)

func main() {
	socketPath := flag.String("socket", "/tmp/texel-stress.sock", "Unix socket path for stress server")
	sessions := flag.Int("sessions", 1, "number of concurrent sessions")
	duration := flag.Duration("duration", 10*time.Second, "total duration of the stress run")
	publishInterval := flag.Duration("publish", 100*time.Millisecond, "interval between publish ticks")
	messagesPerCycle := flag.Int("messages", 10, "messages to process before forcing a resume")
	flag.Parse()

	if err := os.RemoveAll(*socketPath); err != nil {
		log.Fatalf("cleanup socket: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	desktop, mainApp := buildDesktop()
	manager := server.NewManager()
	srv := server.NewServer(*socketPath, manager)
	server.SetSessionStatsObserver(server.NewSessionStatsLogger(log.Default()))

	publishers := make([]*server.DesktopPublisher, 0)
	var pubMu sync.Mutex

	sink := server.NewDesktopSink(desktop)
	srv.SetEventSink(sink)
	srv.SetPublisherFactory(func(sess *server.Session) *server.DesktopPublisher {
		pub := server.NewDesktopPublisher(desktop, sess)
		pubMu.Lock()
		publishers = append(publishers, pub)
		pubMu.Unlock()
		pub.SetObserver(server.NewPublishLogger(log.Default()))
		return pub
	})

	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("server start failed: %v", err)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < *sessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runSession(ctx, *socketPath, *messagesPerCycle)
		}(i)
	}

	publishTicker := time.NewTicker(*publishInterval)
	defer publishTicker.Stop()
	counter := 0
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			timeoutCtx, cancelStop := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelStop()
			_ = srv.Stop(timeoutCtx)
			desktop.Close()
			fmt.Println("stress run complete")
			return
		case <-publishTicker.C:
			counter++
			mainApp.SetMessage(fmt.Sprintf("session tick %d", counter))
			pubMu.Lock()
			for _, pub := range publishers {
				_ = pub.Publish()
			}
			pubMu.Unlock()
		}
	}
}

func buildDesktop() (*texel.Desktop, *stressApp) {
	screen := tcell.NewSimulationScreen("ansi")
	driver := texel.NewTcellScreenDriver(screen)
	lifecycle := &texel.LocalAppLifecycle{}

	app := newStressApp("stress", "starting")
	shellFactory := func() texel.App { return app }
	welcomeFactory := func() texel.App { return newStressApp("welcome", "loaded") }

	desktop, err := texel.NewDesktopWithDriver(driver, shellFactory, welcomeFactory, lifecycle)
	if err != nil {
		log.Fatalf("desktop init failed: %v", err)
	}
	return desktop, app
}

func runSession(ctx context.Context, socket string, messagesPerCycle int) {
	simple := client.NewSimpleClient(socket)
	var sessionID [16]byte
	var lastSeq uint64

	for {
		if ctx.Err() != nil {
			return
		}
		accept, conn, err := simple.Connect(&sessionID)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		_ = accept
		if err := consumeMessages(ctx, conn, sessionID, &lastSeq, messagesPerCycle); err != nil && !errors.Is(err, context.Canceled) {
			// ignore transient errors
		}
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
		accept, conn, err = simple.Connect(&sessionID)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		hdr, payload, err := simple.RequestResume(conn, sessionID, lastSeq)
		if err != nil {
			_ = conn.Close()
			continue
		}
		if hdr.Type == protocol.MsgTreeSnapshot {
			if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
				_ = conn.Close()
				continue
			}
		}
		if err := consumeMessages(ctx, conn, sessionID, &lastSeq, messagesPerCycle); err != nil && !errors.Is(err, context.Canceled) {
		}
		_ = conn.Close()
	}
}

func consumeMessages(ctx context.Context, conn net.Conn, sessionID [16]byte, lastSeq *uint64, target int) error {
	ackHeader := protocol.Header{Version: protocol.Version, Type: protocol.MsgBufferAck, Flags: protocol.FlagChecksum, SessionID: sessionID}
	received := 0
	for received < target {
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return err
		}
		hdr, payload, err := protocol.ReadMessage(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			return err
		}
		switch hdr.Type {
		case protocol.MsgTreeSnapshot:
			if _, err := protocol.DecodeTreeSnapshot(payload); err != nil {
				return err
			}
			received++
		case protocol.MsgBufferDelta:
			if _, err := protocol.DecodeBufferDelta(payload); err != nil {
				return err
			}
			ackPayload, _ := protocol.EncodeBufferAck(protocol.BufferAck{Sequence: hdr.Sequence})
			if err := protocol.WriteMessage(conn, ackHeader, ackPayload); err != nil {
				return err
			}
			*lastSeq = hdr.Sequence
			received++
		default:
			received++
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

type stressApp struct {
	mu     sync.Mutex
	title  string
	runes  []rune
	notify chan<- bool
}

func newStressApp(title, msg string) *stressApp {
	return &stressApp{title: title, runes: []rune(msg)}
}

func (a *stressApp) Run() error      { return nil }
func (a *stressApp) Stop()           {}
func (a *stressApp) Resize(int, int) {}

func (a *stressApp) Render() [][]texel.Cell {
	a.mu.Lock()
	defer a.mu.Unlock()
	row := make([]texel.Cell, len(a.runes))
	for i, ch := range a.runes {
		row[i] = texel.Cell{Ch: ch, Style: tcell.StyleDefault}
	}
	return [][]texel.Cell{row}
}

func (a *stressApp) GetTitle() string          { return a.title }
func (a *stressApp) HandleKey(*tcell.EventKey) {}

func (a *stressApp) SetRefreshNotifier(ch chan<- bool) { a.notify = ch }

func (a *stressApp) SetMessage(msg string) {
	a.mu.Lock()
	a.runes = []rune(msg)
	notify := a.notify
	a.mu.Unlock()
	if notify != nil {
		select {
		case notify <- true:
		default:
		}
	}
}
