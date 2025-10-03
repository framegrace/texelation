package server

import (
	"context"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"texelation/protocol"
)

// Server listens on a Unix domain socket and manages sessions.
type Server struct {
	addr             string
	manager          *Manager
	listener         net.Listener
	quit             chan struct{}
	wg               sync.WaitGroup
	sink             EventSink
	publisherFactory func(*Session) *DesktopPublisher
	snapshotStore    *SnapshotStore
	snapshotInterval time.Duration
	snapshotQuit     chan struct{}
	desktopSink      *DesktopSink
	bootSnapshotMu   sync.RWMutex
	bootSnapshot     *protocol.TreeSnapshot
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
	if ds, ok := sink.(*DesktopSink); ok {
		s.desktopSink = ds
	}
}

func (s *Server) SetPublisherFactory(factory func(*Session) *DesktopPublisher) {
	s.publisherFactory = factory
}

func (s *Server) SetSnapshotStore(store *SnapshotStore, interval time.Duration) {
	s.snapshotStore = store
	if interval > 0 {
		s.snapshotInterval = interval
	}
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
	s.loadBootSnapshot()
	s.wg.Add(1)
	go s.acceptLoop()
	s.startSnapshotLoop()
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
			if s.desktopSink != nil {
				s.desktopSink.SetPublisher(publisher)
			}
			if publisher != nil {
				_ = publisher.Publish()
			}
			s.sendSnapshot(c, session)
			conn := newConnection(c, session, s.sink)
			_ = conn.serve()
		}(conn)
	}
}

func (s *Server) Stop(ctx context.Context) error {
	close(s.quit)
	if s.snapshotQuit != nil {
		close(s.snapshotQuit)
	}
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

func (s *Server) SetDiffRetentionLimit(limit int) {
	s.manager.SetDiffRetentionLimit(limit)
}

func (s *Server) loadBootSnapshot() {
	if s.snapshotStore == nil {
		return
	}
	stored, err := s.snapshotStore.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("snapshot load failed: %v", err)
		return
	}
	snapshot := stored.ToTreeSnapshot()
	if len(snapshot.Panes) == 0 {
		return
	}
	s.setBootSnapshot(snapshot)
}

func (s *Server) setBootSnapshot(snapshot protocol.TreeSnapshot) {
	copySnapshot := protocol.TreeSnapshot{Panes: make([]protocol.PaneSnapshot, len(snapshot.Panes))}
	copy(copySnapshot.Panes, snapshot.Panes)
	s.bootSnapshotMu.Lock()
	s.bootSnapshot = &copySnapshot
	s.bootSnapshotMu.Unlock()
}

func (s *Server) bootSnapshotCopy() (protocol.TreeSnapshot, bool) {
	s.bootSnapshotMu.RLock()
	defer s.bootSnapshotMu.RUnlock()
	if s.bootSnapshot == nil || len(s.bootSnapshot.Panes) == 0 {
		return protocol.TreeSnapshot{}, false
	}
	copySnapshot := protocol.TreeSnapshot{Panes: make([]protocol.PaneSnapshot, len(s.bootSnapshot.Panes))}
	copy(copySnapshot.Panes, s.bootSnapshot.Panes)
	return copySnapshot, true
}

func (s *Server) startSnapshotLoop() {
	if s.snapshotStore == nil || s.desktopSink == nil {
		return
	}
	interval := s.snapshotInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	s.snapshotQuit = make(chan struct{})
	ticker := time.NewTicker(interval)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.persistSnapshot()
			case <-s.snapshotQuit:
				return
			case <-s.quit:
				return
			}
		}
	}()
	s.persistSnapshot()
}

func (s *Server) persistSnapshot() {
	if s.snapshotStore == nil || s.desktopSink == nil {
		return
	}
	desktop := s.desktopSink.Desktop()
	if desktop == nil {
		return
	}
	panes := desktop.SnapshotBuffers()
	if len(panes) == 0 {
		return
	}
	if err := s.snapshotStore.Save(panes); err != nil {
		log.Printf("snapshot save failed: %v", err)
	}
	protoPanes := make([]protocol.PaneSnapshot, len(panes))
	for i, pane := range panes {
		rows := make([]string, len(pane.Buffer))
		for y, row := range pane.Buffer {
			runes := make([]rune, len(row))
			for x, cell := range row {
				if cell.Ch == 0 {
					runes[x] = ' '
				} else {
					runes[x] = cell.Ch
				}
			}
			rows[y] = string(runes)
		}
		protoPanes[i] = protocol.PaneSnapshot{
			PaneID: pane.ID,
			Title:  pane.Title,
			Rows:   rows,
			X:      int32(pane.Rect.X),
			Y:      int32(pane.Rect.Y),
			Width:  int32(pane.Rect.Width),
			Height: int32(pane.Rect.Height),
		}
	}
	s.setBootSnapshot(protocol.TreeSnapshot{Panes: protoPanes})
}

func (s *Server) sendSnapshot(conn net.Conn, session *Session) {
	provider, ok := s.sink.(SnapshotProvider)
	if !ok {
		return
	}
	snapshot, err := provider.Snapshot()
	if err != nil || len(snapshot.Panes) == 0 {
		if fallback, ok := s.bootSnapshotCopy(); ok {
			snapshot = fallback
		} else {
			if err != nil {
				log.Printf("snapshot capture failed: %v", err)
			}
			return
		}
	} else {
		s.setBootSnapshot(snapshot)
	}
	payload, err := protocol.EncodeTreeSnapshot(snapshot)
	if err != nil {
		log.Printf("encode snapshot failed: %v", err)
		return
	}
	header := protocol.Header{Version: protocol.Version, Type: protocol.MsgTreeSnapshot, Flags: protocol.FlagChecksum, SessionID: session.ID()}
	if err := protocol.WriteMessage(conn, header, payload); err != nil {
		log.Printf("send snapshot failed: %v", err)
	}
}
