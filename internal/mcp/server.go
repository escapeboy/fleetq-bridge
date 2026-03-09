// Package mcp implements a proxy for local MCP stdio servers.
// It spawns configured MCP server processes, forwards JSON-RPC 2.0
// requests from the tunnel to the right server's stdin, and sends
// responses back via the tunnel.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"go.uber.org/zap"
)

// ServerConfig is a single MCP stdio server definition.
type ServerConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env,omitempty"`
}

// Server manages one MCP stdio subprocess.
type Server struct {
	cfg    ServerConfig
	log    *zap.Logger
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	ready  bool
}

// NewServer creates but does not start a Server.
func NewServer(cfg ServerConfig, log *zap.Logger) *Server {
	return &Server{cfg: cfg, log: log.With(zap.String("mcp_server", cfg.Name))}
}

// Start spawns the subprocess.
func (s *Server) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.cfg.Command, s.cfg.Args...) //nolint:gosec
	if len(s.cfg.Env) > 0 {
		cmd.Env = s.cfg.Env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %q: %w", s.cfg.Command, err)
	}

	s.cmd = cmd
	s.stdin = stdin
	s.stdout = bufio.NewScanner(stdout)
	s.ready = true
	s.log.Info("MCP server started", zap.Int("pid", cmd.Process.Pid))
	return nil
}

// Call sends a JSON-RPC request and returns the raw response line.
func (s *Server) Call(request []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.ready {
		return nil, fmt.Errorf("server %q not running", s.cfg.Name)
	}

	// Append newline delimiter (Content-Length framing is optional; most MCP
	// stdio servers also accept newline-delimited JSON-RPC).
	line := append(bytes.TrimRight(request, "\n"), '\n')
	if _, err := s.stdin.Write(line); err != nil {
		return nil, fmt.Errorf("write to %q: %w", s.cfg.Name, err)
	}

	if !s.stdout.Scan() {
		if err := s.stdout.Err(); err != nil {
			return nil, fmt.Errorf("read from %q: %w", s.cfg.Name, err)
		}
		return nil, io.EOF
	}
	return s.stdout.Bytes(), nil
}

// Stop terminates the subprocess.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	s.log.Info("MCP server stopped")
}

// Registry manages multiple MCP servers, keyed by name.
type Registry struct {
	servers map[string]*Server
	log     *zap.Logger
}

// NewRegistry builds a Registry from config and starts all servers.
func NewRegistry(cfgs []ServerConfig, ctx context.Context, log *zap.Logger) *Registry {
	r := &Registry{servers: make(map[string]*Server), log: log}
	for _, cfg := range cfgs {
		srv := NewServer(cfg, log)
		if err := srv.Start(ctx); err != nil {
			log.Warn("failed to start MCP server", zap.String("name", cfg.Name), zap.Error(err))
			continue
		}
		r.servers[cfg.Name] = srv
	}
	return r
}

// Handle routes an incoming MCP JSON-RPC request (identified by serverName
// embedded in the requestID or the payload) to the right subprocess.
// The payload is raw JSON-RPC 2.0.
func (r *Registry) Handle(serverName string, payload []byte) ([]byte, error) {
	srv, ok := r.servers[serverName]
	if !ok {
		return errorResponse(payload, -32001, fmt.Sprintf("MCP server %q not found", serverName))
	}
	resp, err := srv.Call(payload)
	if err != nil {
		return errorResponse(payload, -32002, err.Error())
	}
	return resp, nil
}

// Names returns the list of active MCP server names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.servers))
	for name := range r.servers {
		names = append(names, name)
	}
	return names
}

// StopAll terminates all servers.
func (r *Registry) StopAll() {
	for _, srv := range r.servers {
		srv.Stop()
	}
}

// errorResponse builds a JSON-RPC error response, reusing id from request if possible.
func errorResponse(request []byte, code int, message string) ([]byte, error) {
	var req struct {
		ID interface{} `json:"id"`
	}
	_ = json.Unmarshal(request, &req)

	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	return json.Marshal(resp)
}
