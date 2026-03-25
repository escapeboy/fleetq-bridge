package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/fleetq/fleetq-bridge/internal/logutil"
	"github.com/fleetq/fleetq-bridge/internal/tunnel"
)

// Config holds relay server configuration.
type Config struct {
	APIURL      string // e.g. "https://fleetq.net"
	APIHost     string // optional Host header override for internal routing (e.g. "fleetq.net" when APIURL=http://nginx)
	RedisURL    string // e.g. "redis://redis:6379/0"
	RedisPrefix string // e.g. "fleetq-" — prepended to all Redis keys (must match Laravel's REDIS_PREFIX)
}

// Server is the HTTP/WebSocket relay server.
type Server struct {
	cfg    Config
	hub    *Hub
	rdb    *redis.Client
	log    *zap.Logger
	client *http.Client
}

// NewServer creates and initialises a relay Server.
func NewServer(cfg Config, log *zap.Logger) (*Server, error) {
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Server{
		cfg:    cfg,
		hub:    NewHub(),
		rdb:    rdb,
		log:    log,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ServeHTTP routes requests to the appropriate handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/bridge/health":
		s.handleHealth(w, r)
	case "/bridge/ws":
		s.handleWebSocket(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleHealth returns a simple JSON health response.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"relay":       "fleetq-relay/1.0",
		"connections": s.hub.Count(),
	})
}

// handleWebSocket upgrades the connection and runs the bridge relay loop.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	apiKey := extractBearer(r.Header.Get("Authorization"))
	if apiKey == "" {
		http.Error(w, "missing Authorization: Bearer <api_key>", http.StatusUnauthorized)
		return
	}

	teamID, err := s.resolveTeam(r.Context(), apiKey)
	if err != nil {
		s.log.Warn("auth failed", zap.Error(err), logutil.RedactedString("api_key", apiKey))
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	})
	if err != nil {
		s.log.Warn("websocket accept failed", zap.Error(err), zap.String("team_id", teamID))
		return
	}

	sessionID := fmt.Sprintf("relay-%s-%d", teamID[:8], time.Now().Unix())

	connCtx, cancel := context.WithCancel(context.Background())
	conn := &Conn{
		TeamID:    teamID,
		SessionID: sessionID,
		apiKey:    apiKey,
		ws:        ws,
		send:      make(chan []byte, 64),
		cancel:    cancel,
	}

	s.hub.Register(conn)
	s.log.Info("bridge connected", zap.String("team_id", teamID))

	// Notify Laravel of the new connection synchronously before starting readLoop.
	// This ensures the DB record exists before any FrameDiscover frames are processed,
	// avoiding a race where updateEndpoints cannot find the connection and falls through
	// to the Redis-pending path while registerConnection concurrently reads stale data.
	s.registerConnection(sessionID, apiKey)

	defer func() {
		cancel()
		s.hub.Unregister(conn)
		ws.CloseNow()
		s.log.Info("bridge disconnected", zap.String("team_id", teamID))
		go s.unregisterConnection(teamID, apiKey)
	}()

	// Three concurrent goroutines: read, write, and Redis pump
	type result struct {
		loop string
		err  error
	}
	resCh := make(chan result, 3)

	go func() { resCh <- result{"read", s.readLoop(connCtx, conn)} }()
	go func() { resCh <- result{"write", s.writeLoop(connCtx, conn)} }()
	go func() { resCh <- result{"redis", s.redisPump(connCtx, conn)} }()

	res := <-resCh // first loop to exit closes everything
	if res.err != nil && connCtx.Err() == nil {
		s.log.Warn("connection loop exited",
			zap.String("team_id", teamID),
			zap.String("loop", res.loop),
			zap.Error(res.err))
	}
}

// readLoop reads frames from the daemon and handles them.
func (s *Server) readLoop(ctx context.Context, conn *Conn) error {
	for {
		// Read timeout: 6 heartbeat cycles (5s each).
		// Bridge sends heartbeats every 5s; 30s = 6 missed cycles before
		// declaring the connection dead. Matches the bridge's own ack timeout
		// so both sides detect failures at roughly the same time.
		readCtx, readCancel := context.WithTimeout(ctx, 30*time.Second)
		_, data, err := conn.ws.Read(readCtx)
		readCancel()
		if err != nil {
			return err
		}

		frame, err := tunnel.Decode(bytes.NewReader(data))
		if err != nil {
			s.log.Warn("frame decode error", zap.Error(err), zap.String("team_id", conn.TeamID))
			continue
		}

		s.log.Debug("frame received",
			zap.String("team_id", conn.TeamID),
			zap.Uint16("type", uint16(frame.Type)),
			zap.Int("payload_bytes", len(frame.Payload)))

		if err := s.handleFrame(ctx, conn, frame); err != nil {
			s.log.Warn("frame handle error", zap.Error(err), zap.String("team_id", conn.TeamID))
		}
	}
}

// handleFrame dispatches an incoming frame from the daemon.
func (s *Server) handleFrame(ctx context.Context, conn *Conn, frame *tunnel.Frame) error {
	switch frame.Type {
	case tunnel.FrameHeartbeat:
		// Echo back as ack
		ack := &tunnel.Frame{Type: tunnel.FrameHeartbeatAck, RequestID: frame.RequestID, Payload: frame.Payload}
		return conn.SendFrame(ack)

	case tunnel.FrameDiscover:
		// Update endpoints in Laravel
		go s.updateEndpoints(conn, frame.Payload)
		// Send ack
		ack := &tunnel.Frame{Type: tunnel.FrameDiscoverAck, RequestID: frame.RequestID}
		return conn.SendFrame(ack)

	case tunnel.FrameLLMResponseChunk, tunnel.FrameLLMResponseEnd,
		tunnel.FrameAgentEvent, tunnel.FrameAgentDone,
		tunnel.FrameMCPResponse, tunnel.FrameError:
		// Push response to Redis for Laravel to consume
		return s.pushResponse(ctx, frame)
	}
	return nil
}

// writeLoop drains the send channel and writes frames to the WebSocket.
func (s *Server) writeLoop(ctx context.Context, conn *Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case data := <-conn.send:
			// Write timeout prevents blocking indefinitely on dead TCP sockets.
			writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.ws.Write(writeCtx, websocket.MessageBinary, data)
			writeCancel()
			if err != nil {
				return err
			}
		}
	}
}

// redisPump polls Redis for requests queued for this team and sends them to the daemon.
func (s *Server) redisPump(ctx context.Context, conn *Conn) error {
	key := fmt.Sprintf("%sbridge:req:%s", s.cfg.RedisPrefix, conn.TeamID)

	// Use a dedicated Redis connection for BLPOP to avoid pool exhaustion
	rdb := s.rdb.Conn()
	defer rdb.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// BLPOP with 3s timeout — loop so we check ctx regularly
		result, err := rdb.BLPop(ctx, 3*time.Second, key).Result()
		if err == redis.Nil {
			continue // timeout, no messages
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("redis blpop: %w", err)
		}

		if len(result) < 2 {
			continue
		}

		// Decode the envelope
		var env requestEnvelope
		if err := json.Unmarshal([]byte(result[1]), &env); err != nil {
			s.log.Warn("invalid bridge request envelope", zap.Error(err))
			continue
		}

		frame := &tunnel.Frame{
			RequestID: env.RequestID,
			Type:      tunnel.FrameType(env.FrameType),
			Payload:   env.Payload,
		}
		if err := conn.SendFrame(frame); err != nil {
			s.log.Warn("send frame failed", zap.Error(err), zap.String("team_id", conn.TeamID))
		}
	}
}

// pushResponse publishes a response frame to Redis for Laravel to consume.
func (s *Server) pushResponse(ctx context.Context, frame *tunnel.Frame) error {
	done := frame.Type == tunnel.FrameLLMResponseEnd ||
		frame.Type == tunnel.FrameAgentDone ||
		frame.Type == tunnel.FrameError

	env := responseEnvelope{
		FrameType: uint16(frame.Type),
		Payload:   frame.Payload,
		Done:      done,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%sbridge:stream:%s", s.cfg.RedisPrefix, frame.RequestID)
	pipe := s.rdb.Pipeline()
	pipe.RPush(ctx, key, data)
	pipe.Expire(ctx, key, 10*time.Minute)
	_, err = pipe.Exec(ctx)
	return err
}

// resolveTeam calls the FleetQ API to validate the API key and return the team_id.
func (s *Server) resolveTeam(ctx context.Context, apiKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.APIURL+"/api/v1/me", nil)
	if err != nil {
		return "", err
	}
	if s.cfg.APIHost != "" {
		req.Host = s.cfg.APIHost
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("invalid API key")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var body struct {
		Data struct {
			CurrentTeam struct {
				ID string `json:"id"`
			} `json:"current_team"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.Data.CurrentTeam.ID == "" {
		return "", fmt.Errorf("no current_team in /me response")
	}
	return body.Data.CurrentTeam.ID, nil
}

// registerConnection notifies Laravel that a bridge connected.
func (s *Server) registerConnection(sessionID, apiKey string) {
	body := map[string]any{
		"session_id":     sessionID,
		"bridge_version": "relay/1.0",
	}
	s.apiCall(apiKey, "POST", "/api/v1/bridge/register", body)
}

// unregisterConnection notifies Laravel that the bridge disconnected.
func (s *Server) unregisterConnection(teamID, apiKey string) {
	s.apiCall(apiKey, "DELETE", "/api/v1/bridge", nil)
}

// updateEndpoints sends the discover manifest to Laravel.
func (s *Server) updateEndpoints(conn *Conn, payload []byte) {
	var manifest tunnel.DiscoverManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return
	}
	s.log.Info("updateEndpoints", zap.Int("agents", len(manifest.Agents)), zap.Int("ide_mcp", len(manifest.IDEMCPConfigs)))

	body := map[string]any{
		"session_id": conn.SessionID,
		"endpoints": map[string]any{
			"llm_endpoints":   manifest.LLMEndpoints,
			"agents":          manifest.Agents,
			"mcp_servers":     manifest.MCPServers,
			"ide_mcp_configs": manifest.IDEMCPConfigs,
		},
	}
	s.apiCall(conn.apiKey, "POST", "/api/v1/bridge/endpoints", body)
}

// apiCall makes an authenticated POST/DELETE call to the FleetQ API.
func (s *Server) apiCall(apiKey, method, path string, body map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, s.cfg.APIURL+path, bodyReader)
	if err != nil {
		return
	}
	if s.cfg.APIHost != "" {
		req.Host = s.cfg.APIHost
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("api call failed", zap.String("path", path), zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		s.log.Warn("api call error", zap.String("path", path), zap.Int("status", resp.StatusCode))
	}
}

// extractBearer returns the token from "Bearer <token>".
func extractBearer(header string) string {
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	return ""
}

// requestEnvelope is the Redis message format for outbound requests (Laravel → relay).
type requestEnvelope struct {
	RequestID string          `json:"request_id"`
	FrameType uint16          `json:"frame_type"`
	Payload   json.RawMessage `json:"payload"`
}

// responseEnvelope is the Redis message format for inbound responses (relay → Laravel).
type responseEnvelope struct {
	FrameType uint16 `json:"frame_type"`
	Payload   []byte `json:"payload"`
	Done      bool   `json:"done"`
}
