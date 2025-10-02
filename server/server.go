package server

import (
	"context"
	"net"
	"os"
	"sync"
)

// Server listens on a Unix domain socket and manages sessions.
type Server struct {
    addr     string
    manager  *Manager
    listener net.Listener
    quit     chan struct{}
    wg       sync.WaitGroup
    sink     EventSink
    publisherFactory func(*Session) *DesktopPublisher
}

func NewServer(addr string, manager *Manager) *Server {
    if manager == nil {
        manager = NewManager()
	}
	return &Server{addr: addr, manager: manager, quit: make(chan struct{}), sink: nopSink{}}
}

func (s *Server) SetEventSink(sink EventSink) {
    if sink == nil {
        sink = nopSink{}
    }
    s.sink = sink
}

func (s *Server) SetPublisherFactory(factory func(*Session) *DesktopPublisher) {
    s.publisherFactory = factory
}

func (s *Server) Start() error {
	if err := os.RemoveAll(s.addr); err != nil {
		return err
	}
	l, err := net.Listen("unix", s.addr)
	if err != nil {
		return err
	}
	s.listener = l
	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
			}
			continue
		}

		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			session, err := handleHandshake(c, s.manager)
			if err != nil {
				return
			}
            publisher := (*DesktopPublisher)(nil)
            if s.publisherFactory != nil {
                publisher = s.publisherFactory(session)
            }
            if desktopSink, ok := s.sink.(*DesktopSink); ok {
                desktopSink.SetPublisher(publisher)
            }
            if publisher != nil {
                _ = publisher.Publish()
            }
            conn := newConnection(c, session, s.sink)
            _ = conn.serve()
        }(conn)
    }
}

func (s *Server) Stop(ctx context.Context) error {
	close(s.quit)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *Server) Manager() *Manager {
	return s.manager
}

func (s *Server) EventSink() EventSink {
	return s.sink
}
