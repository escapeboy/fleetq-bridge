package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/discovery"
	"github.com/fleetq/fleetq-bridge/internal/executor"
	"github.com/fleetq/fleetq-bridge/internal/ipc"
	"github.com/fleetq/fleetq-bridge/internal/llm"
	"github.com/fleetq/fleetq-bridge/internal/mcp"
	"github.com/fleetq/fleetq-bridge/internal/tunnel"
)

// Runner is the core daemon logic.
type Runner struct {
	cfg    *config.Config
	apiKey string
	log    *zap.Logger

	mu             sync.RWMutex
	llmEndpoints   []discovery.LLMEndpoint
	agents         []discovery.Agent
	ideMCPConfigs  []discovery.MCPServerConfig
	registry       *executor.Registry
	startedAt      time.Time

	tunnelClient *tunnel.Client
	ipcServer    *ipc.Server
	llmProxy     *llm.Proxy
	mcpRegistry  *mcp.Registry
}

// NewRunner creates a daemon runner.
// socketPath is the IPC socket path; use ipc.SocketPathFor(configPath) to derive it.
func NewRunner(cfg *config.Config, apiKey string, log *zap.Logger, socketPath string) *Runner {
	r := &Runner{
		cfg:       cfg,
		apiKey:    apiKey,
		log:       log,
		startedAt: time.Now(),
		llmProxy:  llm.NewProxy(),
	}
	r.tunnelClient = tunnel.NewClient(cfg.RelayURL, apiKey, r, log)
	r.ipcServer = ipc.NewServer(r.buildStatusPayload, socketPath)
	return r
}

// Run starts the daemon. Blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	r.log.Info("fleetq-bridge starting",
		zap.String("relay", r.cfg.RelayURL),
	)

	// Start IPC server
	if err := r.ipcServer.Start(); err != nil {
		r.log.Warn("IPC server failed to start", zap.Error(err))
	} else {
		defer r.ipcServer.Stop()
	}

	// Start MCP servers
	if len(r.cfg.MCPServers) > 0 {
		cfgs := make([]mcp.ServerConfig, len(r.cfg.MCPServers))
		for i, s := range r.cfg.MCPServers {
			cfgs[i] = mcp.ServerConfig{Name: s.Name, Command: s.Command, Args: s.Args, Env: s.Env}
		}
		r.mcpRegistry = mcp.NewRegistry(cfgs, ctx, r.log)
		defer r.mcpRegistry.StopAll()
	}

	// Initial discovery
	r.discover(ctx)

	// Start periodic re-discovery
	go r.discoveryLoop(ctx)

	// Connect to relay (blocks, reconnects automatically)
	r.tunnelClient.Run(ctx)
	return nil
}

// discoveryLoop periodically re-probes local LLMs and agents.
func (r *Runner) discoveryLoop(ctx context.Context) {
	interval := time.Duration(r.cfg.Discovery.IntervalSeconds) * time.Second
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
			r.discover(ctx)
		}
	}
}

func (r *Runner) discover(ctx context.Context) {
	llms := discovery.DiscoverLLMs(ctx)
	agents := discovery.DiscoverAgents(ctx, r.cfg.Agents.Enabled)
	ideMCP := discovery.DiscoverIDEMCPConfigs()
	registry := executor.NewRegistry(agents)

	r.mu.Lock()
	r.llmEndpoints = llms
	r.agents = agents
	r.ideMCPConfigs = ideMCP
	r.registry = registry
	r.mu.Unlock()

	// Count online resources for log
	online := 0
	for _, ep := range llms {
		if ep.Online {
			online++
		}
	}
	found := 0
	for _, a := range agents {
		if a.Found {
			found++
		}
	}
	r.log.Info("discovery complete",
		zap.Int("llms_online", online),
		zap.Int("agents_found", found),
		zap.Int("ide_mcp_configs", len(ideMCP)),
	)
}

// --- tunnel.Handler interface ---

// GetManifest returns the current capability manifest.
func (r *Runner) GetManifest() *tunnel.DiscoverManifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m := &tunnel.DiscoverManifest{BridgeVersion: "1.0.0"}

	for _, ep := range r.llmEndpoints {
		m.LLMEndpoints = append(m.LLMEndpoints, tunnel.LLMEndpointInfo{
			Name:    ep.Name,
			BaseURL: ep.BaseURL,
			Online:  ep.Online,
			Models:  ep.Models,
		})
	}
	for _, a := range r.agents {
		m.Agents = append(m.Agents, tunnel.AgentInfo{
			Key:     a.Key,
			Name:    a.DisplayName,
			Found:   a.Found,
			Version: a.Version,
		})
	}
	if r.mcpRegistry != nil {
		m.MCPServers = r.mcpRegistry.Names()
	}
	for _, cfg := range r.ideMCPConfigs {
		m.IDEMCPConfigs = append(m.IDEMCPConfigs, tunnel.MCPServerInfo{
			Name:    cfg.Name,
			Source:  cfg.Source,
			Command: cfg.Command,
			Args:    cfg.Args,
			Env:     cfg.Env,
			URL:     cfg.URL,
		})
	}
	return m
}

// OnLLMRequest handles an LLM inference request from the relay.
func (r *Runner) OnLLMRequest(ctx context.Context, req *tunnel.LLMRequest, send func(*tunnel.Frame) error) error {
	r.log.Info("LLM request", zap.String("request_id", req.RequestID), zap.String("model", req.Model))

	sendChunk := func(chunk *tunnel.LLMResponseChunk) error {
		frameType := tunnel.FrameLLMResponseChunk
		if chunk.Done {
			frameType = tunnel.FrameLLMResponseEnd
		}
		f, err := tunnel.NewJSONFrame(req.RequestID, frameType, chunk)
		if err != nil {
			return err
		}
		return send(f)
	}

	if err := r.llmProxy.Forward(ctx, req, sendChunk); err != nil {
		r.log.Error("LLM proxy error", zap.Error(err))
		errFrame, _ := tunnel.NewJSONFrame(req.RequestID, tunnel.FrameError,
			&tunnel.ErrorPayload{Code: "llm_error", Message: err.Error()})
		return send(errFrame)
	}
	return nil
}

// OnAgentRequest handles an agent execution request from the relay.
func (r *Runner) OnAgentRequest(ctx context.Context, req *tunnel.AgentRequest, send func(*tunnel.Frame) error) error {
	r.log.Info("agent request",
		zap.String("request_id", req.RequestID),
		zap.String("agent", req.AgentKey),
	)

	r.mu.RLock()
	reg := r.registry
	r.mu.RUnlock()

	if reg == nil {
		return sendError(req.RequestID, "not_ready", "discovery not complete", send)
	}

	ex := reg.Get(req.AgentKey)
	if ex == nil {
		return sendError(req.RequestID, "agent_not_found",
			fmt.Sprintf("agent %q not found or not installed", req.AgentKey), send)
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
		Stream:           req.Stream,
	}
	if exReq.WorkingDirectory == "" {
		exReq.WorkingDirectory = r.cfg.Agents.WorkingDirectory
	}

	// Use a pipe to collect executor output and relay as frames
	pr, pw := newBytePipe()
	go func() {
		defer pw.Close()
		if err := ex.Execute(ctx, exReq, pw); err != nil {
			r.log.Warn("agent executor error",
				zap.String("agent", req.AgentKey),
				zap.Error(err))
		}
	}()

	// Stream events back to relay
	dec := json.NewDecoder(pr)
	for {
		var event executor.Event
		if err := dec.Decode(&event); err != nil {
			break
		}
		frameType := tunnel.FrameAgentEvent
		if event.Kind == "done" {
			frameType = tunnel.FrameAgentDone
		}
		agentEvent := tunnel.AgentEvent{
			RequestID: req.RequestID,
			Kind:      event.Kind,
			Text:      event.Text,
			ExitCode:  event.ExitCode,
			Error:     event.Error,
		}
		f, err := tunnel.NewJSONFrame(req.RequestID, frameType, &agentEvent)
		if err != nil {
			break
		}
		if err := send(f); err != nil {
			break
		}
		if event.Kind == "done" {
			break
		}
	}
	return nil
}

// OnMCPRequest routes an MCP JSON-RPC request to the right local MCP server.
// The requestID encodes the server name as a prefix: "serverName/uuid".
func (r *Runner) OnMCPRequest(_ context.Context, requestID string, payload []byte, send func(*tunnel.Frame) error) error {
	if r.mcpRegistry == nil {
		errFrame, _ := tunnel.NewJSONFrame(requestID, tunnel.FrameError,
			&tunnel.ErrorPayload{Code: "no_mcp_servers", Message: "no MCP servers configured"})
		return send(errFrame)
	}

	// Extract server name from requestID prefix (format: "name/uuid")
	serverName := requestID
	if i := len(requestID); i > 0 {
		for j, c := range requestID {
			if c == '/' {
				serverName = requestID[:j]
				break
			}
		}
	}

	resp, err := r.mcpRegistry.Handle(serverName, payload)
	if err != nil {
		errFrame, _ := tunnel.NewJSONFrame(requestID, tunnel.FrameError,
			&tunnel.ErrorPayload{Code: "mcp_error", Message: err.Error()})
		return send(errFrame)
	}

	f, err := tunnel.NewRawFrame(requestID, tunnel.FrameMCPResponse, resp)
	if err != nil {
		return err
	}
	return send(f)
}

// OnRotateKey handles an API key rotation from the relay.
func (r *Runner) OnRotateKey(_ context.Context, newKey string) {
	r.log.Info("rotating API key")
	r.mu.Lock()
	r.apiKey = newKey
	r.mu.Unlock()
	// Persist to keychain
	// Note: import cycle avoided — auth package called from CLI layer on restart
}

// IsConnected returns true if the tunnel is currently connected.
func (r *Runner) IsConnected() bool {
	return r.tunnelClient.IsConnected()
}

// --- IPC ---

func (r *Runner) buildStatusPayload() *ipc.StatusPayload {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := &ipc.StatusPayload{
		Connected: r.tunnelClient.IsConnected(),
		RelayURL:  r.cfg.RelayURL,
		StartedAt: r.startedAt,
		Latency:   r.tunnelClient.LatencyMs(),
	}
	if !r.startedAt.IsZero() {
		status.Uptime = time.Since(r.startedAt).Truncate(time.Second).String()
	}

	for _, ep := range r.llmEndpoints {
		status.LLMEndpoints = append(status.LLMEndpoints, ipc.LLMEndpointInfo{
			Name:   ep.Name,
			URL:    ep.BaseURL,
			Online: ep.Online,
			Models: ep.Models,
		})
	}
	for _, a := range r.agents {
		status.Agents = append(status.Agents, ipc.AgentInfo{
			Key:     a.Key,
			Name:    a.DisplayName,
			Binary:  a.Binary,
			Version: a.Version,
			Found:   a.Found,
		})
	}
	if r.mcpRegistry != nil {
		for _, s := range r.cfg.MCPServers {
			running := false
			for _, name := range r.mcpRegistry.Names() {
				if name == s.Name {
					running = true
					break
				}
			}
			status.MCPServers = append(status.MCPServers, ipc.MCPServerInfo{
				Name:    s.Name,
				Command: s.Command,
				Running: running,
			})
		}
	}
	return status
}

// --- helpers ---

func sendError(requestID, code, message string, send func(*tunnel.Frame) error) error {
	f, err := tunnel.NewJSONFrame(requestID, tunnel.FrameError,
		&tunnel.ErrorPayload{Code: code, Message: message})
	if err != nil {
		return err
	}
	return send(f)
}

// bytePipe is a simple in-process io.Reader/io.WriteCloser pair.
type bytePipe struct {
	pr *bytes.Buffer
	ch chan []byte
}

func newBytePipe() (*pipeReader, *pipeWriter) {
	ch := make(chan []byte, 128)
	return &pipeReader{ch: ch}, &pipeWriter{ch: ch}
}

type pipeReader struct {
	ch  chan []byte
	buf []byte
}

func (r *pipeReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		chunk, ok := <-r.ch
		if !ok {
			return 0, fmt.Errorf("EOF")
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

func (w *pipeWriter) Close() error {
	close(w.ch)
	return nil
}
