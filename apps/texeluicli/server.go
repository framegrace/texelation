package texeluicli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/gdamore/tcell/v2"
)

type Server struct {
	socketPath string
	ln         net.Listener
	mu         sync.Mutex
	session    *Session
	runner     *uiRunner
	stopOnce   sync.Once
}

func RunServer(socketPath string) error {
	if socketPath == "" {
		var err error
		socketPath, err = SocketPath("")
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return err
	}
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}()
	if err := os.Chmod(socketPath, 0600); err != nil {
		return err
	}

	server := &Server{socketPath: socketPath, runner: newUIRunner(), ln: ln}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		server.shutdown()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go server.handle(conn)
	}
}

func (s *Server) shutdown() {
	s.stopOnce.Do(func() {
		s.runner.Stop()
		s.runner.Wait()
		if s.ln != nil {
			_ = s.ln.Close()
		}
		s.mu.Lock()
		session := s.session
		s.session = nil
		s.mu.Unlock()
		if session != nil {
			session.Close()
		}
	})
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	var req Request
	if err := dec.Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: err.Error()})
		return
	}
	resp := s.dispatch(req)
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Server) dispatch(req Request) Response {
	switch req.Cmd {
	case "open":
		return s.open(req)
	case "wait":
		return s.wait(req)
	case "get":
		return s.get(req)
	case "set":
		return s.set(req)
	case "append":
		return s.append(req)
	case "run":
		return s.run(req)
	case "close":
		return s.close(req)
	default:
		return Response{OK: false, Error: fmt.Sprintf("unknown command %q", req.Cmd)}
	}
}

func (s *Server) open(req Request) Response {
	if req.Spec == nil {
		return Response{OK: false, Error: "spec is required"}
	}
	s.mu.Lock()
	if s.session != nil {
		s.mu.Unlock()
		return Response{OK: false, Error: "session already active"}
	}
	s.mu.Unlock()

	session, err := BuildSession(*req.Spec)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if err := s.runner.Start(session, func() {
		s.clearSession(session.ID)
		s.shutdown()
	}); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	s.mu.Lock()
	s.session = session
	s.mu.Unlock()
	return Response{OK: true, Session: session.ID}
}

func (s *Server) wait(req Request) Response {
	session, err := s.getSession(req.Session)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	ev, err := session.Wait(req.Events)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	values := map[string]string{}
	if len(req.Values) > 0 {
		values, err = session.Values(req.Values)
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
	}
	return Response{OK: true, Event: fmt.Sprintf("%s:%s", ev.Type, ev.ID), Values: values}
}

func (s *Server) get(req Request) Response {
	session, err := s.getSession(req.Session)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	values, err := session.Values(req.IDs)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true, Values: values}
}

func (s *Server) set(req Request) Response {
	session, err := s.getSession(req.Session)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	b, ok := session.Binding(req.ID)
	if !ok {
		return Response{OK: false, Error: fmt.Sprintf("unknown widget %q", req.ID)}
	}
	val := req.Value
	if req.Text != "" {
		val = req.Text
	}
	action := func() error {
		switch {
		case req.Checked != nil:
			if b.setChecked == nil {
				return fmt.Errorf("widget %q does not support checked", req.ID)
			}
			if err := b.setChecked(*req.Checked); err != nil {
				return err
			}
		case b.set != nil:
			if err := b.set(val); err != nil {
				return err
			}
		default:
			return fmt.Errorf("widget %q is not writable", req.ID)
		}
		invalidateWidget(session.UI, b.widget)
		return nil
	}
	if err := s.runner.Post(action); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (s *Server) append(req Request) Response {
	session, err := s.getSession(req.Session)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	b, ok := session.Binding(req.ID)
	if !ok {
		return Response{OK: false, Error: fmt.Sprintf("unknown widget %q", req.ID)}
	}
	if b.append == nil {
		return Response{OK: false, Error: fmt.Sprintf("widget %q does not support append", req.ID)}
	}
	action := func() error {
		b.append(req.Text)
		invalidateWidget(session.UI, b.widget)
		return nil
	}
	if err := s.runner.Post(action); err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	return Response{OK: true}
}

func (s *Server) run(req Request) Response {
	session, err := s.getSession(req.Session)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	if req.Run == nil {
		return Response{OK: false, Error: "run request missing"}
	}
	argv := req.Run.Argv
	if len(argv) == 0 && req.Run.Cmd != "" {
		argv = []string{req.Run.Cmd}
	}
	if len(argv) == 0 {
		return Response{OK: false, Error: "command required"}
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	if req.Run.Cwd != "" {
		cmd.Dir = req.Run.Cwd
	}

	var stdout io.ReadCloser
	var stderr io.ReadCloser
	if req.Run.Stdout != "" {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
	} else {
		cmd.Stdout = io.Discard
	}
	if req.Run.Stderr != "" {
		stderr, err = cmd.StderrPipe()
		if err != nil {
			return Response{OK: false, Error: err.Error()}
		}
	} else {
		cmd.Stderr = io.Discard
	}

	if req.Run.Clear != "" {
		_ = s.runner.Post(func() error {
			if b, ok := session.Binding(req.Run.Clear); ok && b.set != nil {
				_ = b.set("")
				invalidateWidget(session.UI, b.widget)
			}
			return nil
		})
	}

	if err := cmd.Start(); err != nil {
		return Response{OK: false, Error: err.Error()}
	}

	var wg sync.WaitGroup
	stream := func(r io.Reader, target string) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			_ = s.runner.Post(func() error {
				b, ok := session.Binding(target)
				if ok && b.append != nil {
					b.append(line)
					invalidateWidget(session.UI, b.widget)
				}
				return nil
			})
		}
	}

	if stdout != nil {
		wg.Add(1)
		go stream(stdout, req.Run.Stdout)
	}
	if stderr != nil {
		wg.Add(1)
		go stream(stderr, req.Run.Stderr)
	}

	waitErr := cmd.Wait()
	wg.Wait()

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return Response{OK: false, Error: waitErr.Error()}
		}
	} else if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	return Response{OK: true, ExitCode: &exitCode}
}

func (s *Server) close(req Request) Response {
	session, err := s.getSession(req.Session)
	if err != nil {
		return Response{OK: false, Error: err.Error()}
	}
	session.Close()
	s.clearSession(session.ID)
	s.shutdown()
	return Response{OK: true}
}

func (s *Server) getSession(id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil, errors.New("no active session")
	}
	if id == "" || id == s.session.ID {
		return s.session, nil
	}
	return nil, fmt.Errorf("session %q not found", id)
}

func (s *Server) clearSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil && s.session.ID == id {
		s.session = nil
	}
}

type uiRunner struct {
	mu        sync.Mutex
	screen    tcell.Screen
	session   *Session
	refreshCh chan bool
	actions   chan func() error
	stopCh    chan struct{}
	doneCh    chan struct{}
	onClosed  func()
}

func newUIRunner() *uiRunner {
	return &uiRunner{
		actions: make(chan func() error, 128),
	}
}

func (r *uiRunner) Start(session *Session, onClosed func()) error {
	r.mu.Lock()
	if r.screen != nil {
		r.mu.Unlock()
		return errors.New("ui already running")
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if err := screen.Init(); err != nil {
		r.mu.Unlock()
		return err
	}
	screen.EnableMouse(tcell.MouseMotionEvents)
	screen.EnablePaste()

	r.screen = screen
	r.session = session
	r.refreshCh = make(chan bool, 1)
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.onClosed = onClosed
	r.mu.Unlock()

	session.UI.SetRefreshNotifier(r.refreshCh)
	w, h := screen.Size()
	session.UI.Resize(w, h)
	r.draw()

	go r.refreshLoop()
	go r.eventLoop()
	return nil
}

func (r *uiRunner) Stop() {
	r.mu.Lock()
	screen := r.screen
	stopCh := r.stopCh
	r.mu.Unlock()
	if screen == nil || stopCh == nil {
		return
	}
	select {
	case <-stopCh:
		return
	default:
		close(stopCh)
	}
	_ = screen.PostEvent(tcell.NewEventInterrupt(nil))
}

func (r *uiRunner) Wait() {
	r.mu.Lock()
	doneCh := r.doneCh
	r.mu.Unlock()
	if doneCh == nil {
		return
	}
	<-doneCh
}

func (r *uiRunner) Post(action func() error) error {
	r.mu.Lock()
	screen := r.screen
	r.mu.Unlock()
	if screen == nil {
		return errors.New("ui not running")
	}
	r.actions <- action
	_ = screen.PostEvent(tcell.NewEventInterrupt(nil))
	return nil
}

func (r *uiRunner) refreshLoop() {
	for {
		select {
		case <-r.stopCh:
			return
		case <-r.refreshCh:
			r.mu.Lock()
			screen := r.screen
			r.mu.Unlock()
			if screen != nil {
				_ = screen.PostEvent(tcell.NewEventInterrupt(nil))
			}
		}
	}
}

func (r *uiRunner) eventLoop() {
	defer func() {
		r.mu.Lock()
		screen := r.screen
		session := r.session
		doneCh := r.doneCh
		onClosed := r.onClosed
		r.screen = nil
		r.session = nil
		r.doneCh = nil
		r.mu.Unlock()
		if session != nil {
			session.Close()
		}
		if screen != nil {
			screen.Fini()
		}
		if doneCh != nil {
			close(doneCh)
		}
		if onClosed != nil {
			onClosed()
		}
	}()

	for {
		r.mu.Lock()
		screen := r.screen
		session := r.session
		stopCh := r.stopCh
		r.mu.Unlock()

		if screen == nil || session == nil {
			return
		}
		if stopCh != nil {
			select {
			case <-stopCh:
				return
			default:
			}
		}

		ev := screen.PollEvent()
		switch tev := ev.(type) {
		case *tcell.EventInterrupt:
			r.drainActions()
			r.draw()
		case *tcell.EventResize:
			w, h := tev.Size()
			session.UI.Resize(w, h)
			r.draw()
		case *tcell.EventKey:
			if tev.Key() == tcell.KeyCtrlC || tev.Key() == tcell.KeyEsc {
				return
			}
			session.UI.HandleKey(tev)
			r.draw()
		case *tcell.EventMouse:
			session.UI.HandleMouse(tev)
			r.draw()
		}
	}
}

func (r *uiRunner) drainActions() {
	for {
		select {
		case action := <-r.actions:
			if action != nil {
				_ = action()
			}
		default:
			return
		}
	}
}

func (r *uiRunner) draw() {
	r.mu.Lock()
	screen := r.screen
	session := r.session
	r.mu.Unlock()
	if screen == nil || session == nil {
		return
	}
	screen.Clear()
	buffer := session.UI.Render()
	if buffer != nil {
		for y := 0; y < len(buffer); y++ {
			row := buffer[y]
			for x := 0; x < len(row); x++ {
				cell := row[x]
				screen.SetContent(x, y, cell.Ch, nil, cell.Style)
			}
		}
	}
	screen.Show()
}
