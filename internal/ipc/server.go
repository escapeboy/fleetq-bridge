package ipc

import (
	"encoding/json"
	"net"
	"os"
	"runtime"
	"sync"
)

// SocketPath returns the platform-specific IPC socket path.
func SocketPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\fleetq-bridge`
	}
	return "/tmp/fleetq-bridge.sock"
}

// Server is the IPC server run inside the daemon.
type Server struct {
	mu          sync.RWMutex
	listener    net.Listener
	subscribers []net.Conn
	statusFn    func() *StatusPayload
}

// NewServer creates a new IPC server.
func NewServer(statusFn func() *StatusPayload) *Server {
	return &Server{statusFn: statusFn}
}

// Start begins listening on the IPC socket.
func (s *Server) Start() error {
	path := SocketPath()
	os.Remove(path)

	var err error
	s.listener, err = net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
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
	os.Remove(SocketPath())
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
