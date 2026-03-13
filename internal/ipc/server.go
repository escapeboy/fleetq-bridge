package ipc

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// SocketPathFor returns the platform-specific IPC socket path derived from
// the config file path. An empty configPath uses the default socket.
//
//	~/.config/fleetq/bridge.yaml      →  /tmp/fleetq-bridge.sock      (default)
//	~/.config/fleetq/bridge-local.yaml →  /tmp/fleetq-bridge-local.sock
func SocketPathFor(configPath string) string {
	stem := "bridge"
	if configPath != "" {
		base := filepath.Base(configPath)
		stem = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if runtime.GOOS == "windows" {
		return `\\.\pipe\fleetq-` + stem
	}
	return "/tmp/fleetq-" + stem + ".sock"
}

// Server is the IPC server run inside the daemon.
type Server struct {
	mu          sync.RWMutex
	listener    net.Listener
	subscribers []net.Conn
	statusFn    func() *StatusPayload
	socketPath  string
}

// NewServer creates a new IPC server.
// socketPath is the Unix socket path; use SocketPathFor(configPath) to derive it.
func NewServer(statusFn func() *StatusPayload, socketPath string) *Server {
	return &Server{statusFn: statusFn, socketPath: socketPath}
}

// Start begins listening on the IPC socket.
func (s *Server) Start() error {
	os.Remove(s.socketPath)

	var err error
	s.listener, err = net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		return err
	}

	go s.accept()
	return nil
}

// Stop closes the listener.
func (s *Server) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
}

// Push sends an event to all subscribers.
func (s *Server) Push(event *EventPayload) {
	msg := Message{Type: MsgEvent, Payload: event}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	alive := s.subscribers[:0]
	for _, conn := range s.subscribers {
		if _, err := conn.Write(data); err == nil {
			alive = append(alive, conn)
		} else {
			conn.Close()
		}
	}
	s.subscribers = alive
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var msg Message
		if err := dec.Decode(&msg); err != nil {
			return
		}

		switch msg.Type {
		case MsgSubscribe:
			s.mu.Lock()
			s.subscribers = append(s.subscribers, conn)
			s.mu.Unlock()
			// send current status immediately
			enc.Encode(Message{Type: MsgStatus, Payload: s.statusFn()}) //nolint:errcheck
			// keep connection alive for pushes
			return // ownership transferred to push loop

		case MsgCommand:
			var cmd string
			if raw, ok := msg.Payload.(string); ok {
				cmd = raw
			}
			if cmd == "status" {
				enc.Encode(Message{Type: MsgStatus, Payload: s.statusFn()}) //nolint:errcheck
			}
		}
	}
}
