package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/discovery"
	"github.com/fleetq/fleetq-bridge/internal/executor"
	"github.com/fleetq/fleetq-bridge/internal/mcp"
	"github.com/fleetq/fleetq-bridge/internal/version"
)

// ExecuteRequest is the POST /execute body from FleetQ cloud.
type ExecuteRequest struct {
	RequestID        string            `json:"request_id"`
	AgentKey         string            `json:"agent_key"`
	Model            string            `json:"model"`
	Prompt           string            `json:"prompt"`
	SystemPrompt     string            `json:"system_prompt"`
	Purpose          string            `json:"purpose"`
	WorkingDirectory string            `json:"working_directory"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	Env              map[string]string `json:"env"`
	Stream           bool              `json:"stream"`
	// LLM fields (when agent_key is empty)
	Messages    []map[string]any `json:"messages"`
	MaxTokens   int              `json:"max_tokens"`
	Temperature float64          `json:"temperature"`
}

// sseChunk is one SSE data frame.
type sseChunk struct {
	Chunk    string    `json:"chunk,omitempty"`
	Done     bool      `json:"done"`
	ExitCode int       `json:"exit_code,omitempty"`
	Error    string    `json:"error,omitempty"`
	Usage    *sseUsage `json:"usage,omitempty"`
}

type sseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Server is the HTTP tunnel server.
type Server struct {
	cfg    *config.Config
	log    *zap.Logger
	secret string

	mu          sync.RWMutex
	agents      []discovery.Agent
	llms        []discovery.LLMEndpoint
	registry    *executor.Registry
	mcpRegistry *mcp.Registry
}

// New creates a new HTTP bridge server.
func New(cfg *config.Config, secret string, log *zap.Logger) *Server {
	return &Server{
		cfg:    cfg,
		log:    log,
		secret: secret,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	// Start MCP servers
	if len(s.cfg.MCPServers) > 0 {
		cfgs := make([]mcp.ServerConfig, len(s.cfg.MCPServers))
		for i, sc := range s.cfg.MCPServers {
			cfgs[i] = mcp.ServerConfig{Name: sc.Name, Command: sc.Command, Args: sc.Args, Env: sc.Env}
		}
		s.mcpRegistry = mcp.NewRegistry(cfgs, ctx, s.log)
		defer s.mcpRegistry.StopAll()
	}

	// Initial discovery
	s.discover(ctx)

	// Periodic re-discovery
	go s.discoveryLoop(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/discover", s.handleDiscover)
	mux.HandleFunc("/execute", s.handleExecute)

	srv := &http.Server{
		Addr:    addr,
		Handler: s.authMiddleware(mux),
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("bridge HTTP server listening", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("HTTP server: %w", err)
		} else {
			errCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.secret != "" {
			if r.Header.Get("Authorization") != "Bearer "+s.secret {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","version":%q}`+"\n", version.String())
}

func (s *Server) handleDiscover(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	agents := s.agents
	llms := s.llms
	s.mu.RUnlock()

	type agentInfo struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Found   bool   `json:"found"`
		Version string `json:"version,omitempty"`
	}
	type llmInfo struct {
		Name    string   `json:"name"`
		BaseURL string   `json:"base_url"`
		Online  bool     `json:"online"`
		Models  []string `json:"models,omitempty"`
	}

	agentInfos := make([]agentInfo, 0, len(agents))
	for _, a := range agents {
		agentInfos = append(agentInfos, agentInfo{Key: a.Key, Name: a.DisplayName, Found: a.Found, Version: a.Version})
	}
	llmInfos := make([]llmInfo, 0, len(llms))
	for _, l := range llms {
		llmInfos = append(llmInfos, llmInfo{Name: l.Name, BaseURL: l.BaseURL, Online: l.Online, Models: l.Models})
	}

	var mcpNames []string
	if s.mcpRegistry != nil {
		mcpNames = s.mcpRegistry.Names()
	}
	if mcpNames == nil {
		mcpNames = []string{}
	}

	resp := map[string]any{
		"agents":        agentInfos,
		"llm_endpoints": llmInfos,
		"mcp_servers":   mcpNames,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()

	// Keepalive loop — prevents Cloudflare's 100s read-timeout from closing the stream
	keepaliveDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			case <-keepaliveDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	defer close(keepaliveDone)

	sendChunk := func(chunk sseChunk) {
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	if req.AgentKey != "" {
		s.executeAgent(ctx, &req, sendChunk)
	} else {
		s.executeLLM(ctx, &req, sendChunk)
	}
}

func (s *Server) executeAgent(ctx context.Context, req *ExecuteRequest, sendChunk func(sseChunk)) {
	s.mu.RLock()
	reg := s.registry
	s.mu.RUnlock()

	if reg == nil {
		sendChunk(sseChunk{Error: "discovery not complete", Done: true})
		return
	}

	router := executor.NewRouter(reg, executor.DefaultRules())
	ex, _ := router.Resolve(&executor.Request{AgentKey: req.AgentKey, Model: req.Model, Purpose: req.Purpose})
	if ex == nil {
		ex = reg.Get(req.AgentKey)
	}
	if ex == nil {
		sendChunk(sseChunk{Error: fmt.Sprintf("agent %q not found or not installed", req.AgentKey), Done: true})
		return
	}

	exReq := &executor.Request{
		ID:               req.RequestID,
		AgentKey:         req.AgentKey,
		Prompt:           req.Prompt,
		SystemPrompt:     req.SystemPrompt,
		Model:            req.Model,
		Purpose:          req.Purpose,
		WorkingDirectory: req.WorkingDirectory,
		TimeoutSeconds:   req.TimeoutSeconds,
		Env:              req.Env,
		Stream:           true,
	}
	if exReq.WorkingDirectory == "" {
		exReq.WorkingDirectory = s.cfg.Agents.WorkingDirectory
	}

	pr, pw := newPipe()
	go func() {
		defer pw.close()
		if err := ex.Execute(ctx, exReq, pw); err != nil {
			s.log.Warn("agent executor error", zap.String("agent", req.AgentKey), zap.Error(err))
		}
	}()

	dec := json.NewDecoder(pr)
	for {
		var event executor.Event
		if err := dec.Decode(&event); err != nil {
			break
		}
		chunk := sseChunk{Chunk: event.Text}
		if event.Kind == "done" {
			chunk.Done = true
			chunk.ExitCode = event.ExitCode
		}
		if event.Error != "" {
			chunk.Error = event.Error
		}
		sendChunk(chunk)
		if event.Kind == "done" {
			break
		}
	}
}

func (s *Server) executeLLM(ctx context.Context, req *ExecuteRequest, sendChunk func(sseChunk)) {
	// Find first online LLM endpoint
	s.mu.RLock()
	var endpointURL string
	for _, l := range s.llms {
		if l.Online {
			endpointURL = l.BaseURL
			break
		}
	}
	s.mu.RUnlock()

	if endpointURL == "" {
		sendChunk(sseChunk{Error: "no local LLM endpoint online", Done: true})
		return
	}

	// Forward as OpenAI-compatible streaming request
	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	bodyJSON, _ := json.Marshal(body)
	url := endpointURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		sendChunk(sseChunk{Error: err.Error(), Done: true})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		sendChunk(sseChunk{Error: err.Error(), Done: true})
		return
	}
	defer resp.Body.Close()

	// Parse OpenAI SSE stream and re-emit as bridge SSE
	var totalPrompt, totalCompletion int
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if len(line) < 6 || line[:6] != "data: " {
			continue
		}
		data := line[6:]
		var obj map[string]any
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			continue
		}
		// Extract delta text
		delta := extractDelta(obj)
		if delta != "" {
			sendChunk(sseChunk{Chunk: delta})
		}
		// Extract usage if present
		if usage, ok := obj["usage"].(map[string]any); ok {
			totalPrompt = int(toFloat(usage["prompt_tokens"]))
			totalCompletion = int(toFloat(usage["completion_tokens"]))
		}
	}

	sendChunk(sseChunk{
		Done: true,
		Usage: &sseUsage{
			PromptTokens:     totalPrompt,
			CompletionTokens: totalCompletion,
		},
	})
}

func (s *Server) discover(ctx context.Context) {
	llms := discovery.DiscoverLLMs(ctx)
	agents := discovery.DiscoverAgents(ctx, s.cfg.Agents.Enabled, s.cfg.Agents.BinaryPaths)
	registry := executor.NewRegistry(agents, s.cfg.MCPServers)

	s.mu.Lock()
	s.llms = llms
	s.agents = agents
	s.registry = registry
	s.mu.Unlock()

	online, found := 0, 0
	for _, ep := range llms {
		if ep.Online {
			online++
		}
	}
	for _, a := range agents {
		if a.Found {
			found++
		}
	}
	s.log.Info("discovery complete", zap.Int("llms_online", online), zap.Int("agents_found", found))
}

func (s *Server) discoveryLoop(ctx context.Context) {
	interval := time.Duration(s.cfg.Discovery.IntervalSeconds) * time.Second
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.discover(ctx)
		}
	}
}

// --- pipe helpers ---

type pipeReader struct {
	ch  chan []byte
	buf []byte
}

func (r *pipeReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		chunk, ok := <-r.ch
		if !ok {
			return 0, io.EOF
		}
		r.buf = chunk
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

type pipeWriter struct {
	ch chan []byte
}

func (w *pipeWriter) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	w.ch <- cp
	return len(p), nil
}

func (w *pipeWriter) close() {
	close(w.ch)
}

func newPipe() (*pipeReader, *pipeWriter) {
	ch := make(chan []byte, 128)
	return &pipeReader{ch: ch}, &pipeWriter{ch: ch}
}

// --- SSE helpers for LLM forwarding ---

func extractDelta(obj map[string]any) string {
	choices, ok := obj["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	delta, ok := choice["delta"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := delta["content"].(string)
	return content
}

func toFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}
