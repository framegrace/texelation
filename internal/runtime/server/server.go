// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// File: internal/runtime/server/server.go
// Summary: Implements server capabilities for the server runtime.
// Usage: Used by texel-server to coordinate server when hosting apps and sessions.
// Notes: This package bridges the legacy desktop code with the client/server protocol implementation.

package server

import (
	"context"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/framegrace/texelation/protocol"
	"github.com/framegrace/texelation/texel"
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
	focusMetrics     *FocusMetrics
	bootSnapshotMu   sync.RWMutex
	bootSnapshot     *protocol.TreeSnapshot
}

func NewServer(addr string, manager *Manager) *Server {
	if manager == nil {
		manager = NewManager()
	}
	return &Server{addr: addr, manager: manager, quit: make(chan struct{}), sink: nopSink{}}
}

// OnEvent implements texel.Listener to react to desktop events.
func (s *Server) OnEvent(event texel.Event) {
	if event.Type == texel.EventTreeChanged || event.Type == texel.EventAppAttached {
		s.persistSnapshot()
	}
}

func (s *Server) SetEventSink(sink EventSink) {
	if sink == nil {
		sink = nopSink{}
	}
	s.sink = sink
	if ds, ok := sink.(*DesktopSink); ok {
		s.desktopSink = ds
		if s.focusMetrics != nil {
			if desktop := ds.Desktop(); desktop != nil {
				s.focusMetrics.Attach(desktop)
			}
		}

		// Subscribe to desktop events for snapshot triggers
		if desktop := ds.Desktop(); desktop != nil {
			desktop.Subscribe(s)
		}

		s.applyBootSnapshot()
	}
}

func (s *Server) SetFocusMetrics(metrics *FocusMetrics) {
	s.focusMetrics = metrics
	if metrics == nil {
		return
	}
	if s.desktopSink != nil {
		if desktop := s.desktopSink.Desktop(); desktop != nil {
			metrics.Attach(desktop)
		}
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
	// Load snapshot immediately so it's available for applyBootSnapshot
	s.loadBootSnapshot()
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
	// Note: loadBootSnapshot is called from SetSnapshotStore, not here
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
			session, resuming, err := handleHandshake(c, s.manager)
			if err != nil {
				return
			}
			conn := newConnection(c, session, s.sink, resuming)
			publisher := (*DesktopPublisher)(nil)
			if s.publisherFactory != nil {
				publisher = s.publisherFactory(session)
			}
			if publisher != nil {
				publisher.SetNotifier(conn.nudge)
			}
			if s.desktopSink != nil {
				s.desktopSink.SetPublisher(publisher)
			}
			if publisher != nil {
				_ = publisher.Publish()
				conn.nudge()
			}
			s.sendSnapshot(c, session)
			_ = conn.serve()
		}(conn)
	}
}

func (s *Server) Stop(ctx context.Context) error {
	select {
	case <-s.quit:
		// Already stopped
		return nil
	default:
		close(s.quit)
	}

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

	if ctx == nil {
		<-done
		return nil
	}

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
	log.Printf("[BOOT] loadBootSnapshot called, snapshotStore=%v", s.snapshotStore != nil)
	if s.snapshotStore == nil {
		return
	}
	stored, err := s.snapshotStore.Load()
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[BOOT] snapshot file does not exist")
			return
		}
		log.Printf("snapshot load failed: %v", err)
		return
	}
	
	// Convert to TreeCapture which supports multiple workspaces
	capture := stored.ToTreeCapture()
	
	log.Printf("[BOOT] Loaded snapshot with %d panes, workspaces=%d", len(capture.Panes), len(capture.WorkspaceRoots))
	if len(capture.Panes) == 0 {
		log.Printf("[BOOT] No panes in snapshot, skipping")
		return
	}
	
	// Convert back to protocol format for initial client handshake (active workspace only)
	// We do this so setBootSnapshot still works for clients
	snapshot := stored.ToTreeSnapshot()
	s.setBootSnapshot(snapshot)
	
	// Apply the full capture
	s.applyBootCapture(capture)
}

func (s *Server) setBootSnapshot(snapshot protocol.TreeSnapshot) {
	copySnapshot := protocol.TreeSnapshot{Panes: make([]protocol.PaneSnapshot, len(snapshot.Panes))}
	copy(copySnapshot.Panes, snapshot.Panes)
	copySnapshot.Root = cloneProtocolTree(snapshot.Root)
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
	copySnapshot.Root = cloneProtocolTree(s.bootSnapshot.Root)
	return copySnapshot, true
}

// applyBootCapture applies the full multi-workspace capture
func (s *Server) applyBootCapture(capture texel.TreeCapture) {
	log.Printf("[BOOT] applyBootCapture called, desktopSink=%v", s.desktopSink != nil)
	if s.desktopSink == nil {
		return
	}
	desktop := s.desktopSink.Desktop()
	if desktop == nil {
		log.Printf("[BOOT] desktop is nil, cannot apply snapshot")
		return
	}
	log.Printf("[BOOT] Applying tree capture with %d panes", len(capture.Panes))
	if err := desktop.ApplyTreeCapture(capture); err != nil {
		log.Printf("apply boot snapshot failed: %v", err)
	} else {
		log.Printf("[BOOT] Successfully applied boot snapshot")
	}
}

// Deprecated: use applyBootCapture
func (s *Server) applyBootSnapshot() {
	// Re-load snapshot to get full capture if possible, or fallback to saved protocol snapshot
	if s.snapshotStore != nil {
		s.loadBootSnapshot()
	}
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
		// Ensure we save one last time when the loop exits
		defer s.persistSnapshot()

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
	// Do NOT save immediately on startup - we might be restoring!
	// s.persistSnapshot() 
}

func (s *Server) persistSnapshot() {
	if s.snapshotStore == nil || s.desktopSink == nil {
		return
	}
	desktop := s.desktopSink.Desktop()
	if desktop == nil {
		return
	}
	capture := desktop.CaptureTree()
	if len(capture.Panes) == 0 {
		return
	}
	if err := s.snapshotStore.Save(&capture); err != nil {
		log.Printf("snapshot save failed: %v", err)
	} else {
		log.Printf("Snapshot saved with %d panes", len(capture.Panes))
	}
	s.setBootSnapshot(treeCaptureToProtocol(capture))
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
