package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.uber.org/zap"
)

// Handler is called when the client receives a frame from the server.
type Handler interface {
	OnLLMRequest(ctx context.Context, req *LLMRequest, send func(*Frame) error) error
	OnAgentRequest(ctx context.Context, req *AgentRequest, send func(*Frame) error) error
	OnMCPRequest(ctx context.Context, requestID string, payload []byte, send func(*Frame) error) error
	OnRotateKey(ctx context.Context, newKey string)
	GetManifest() *DiscoverManifest
}

const (
	// heartbeatInterval is how often we send heartbeat pings.
	// 10s is below typical NAT idle-table timeouts (often 30s for TCP),
	// ensuring probes arrive before the NAT entry is evicted.
	heartbeatInterval = 10 * time.Second

	// heartbeatAckTimeout: if no ack arrives within this window, the connection is dead.
	// 90s = 9 missed heartbeat cycles — tolerates transient relay backpressure.
	heartbeatAckTimeout = 90 * time.Second

	// readTimeout: maximum time to wait for any data on the WebSocket.
	// Heartbeats arrive every 10s and get ack'd, so 90s = ~9 missed cycles.
	readTimeout = 90 * time.Second

	// writeTimeout: maximum time for a single WebSocket write.
	writeTimeout = 10 * time.Second

	// connResetThreshold: if a connection lasted at least this long before dropping,
	// reset backoff to minimum — it was a stable session, not a boot-loop.
	connResetThreshold = 30 * time.Second
)

// Client manages the outbound WebSocket connection to the FleetQ relay.
type Client struct {
	relayURL string
	apiKey   string
	log      *zap.Logger
	handler  Handler

	mu   sync.Mutex
	conn *websocket.Conn // current active connection (nil when disconnected)

	connected        bool
	connectedAt      time.Time
	lastEventAt      time.Time
	latencyMs        int64
	lastHeartbeatAck time.Time
}

// NewClient creates a new tunnel client.
func NewClient(relayURL, apiKey string, handler Handler, log *zap.Logger) *Client {
	return &Client{
		relayURL: relayURL,
		apiKey:   apiKey,
		handler:  handler,
		log:      log,
	}
}

// Run connects to the relay and processes frames. It reconnects automatically.
// Blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	backoff := 1 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		start := time.Now()
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return // context cancelled
		}

		// If the connection was live for a healthy period, reset backoff —
		// this was a stable session that dropped, not a persistent boot-loop.
		if time.Since(start) >= connResetThreshold {
			backoff = 1 * time.Second
		}

		if err != nil {
			c.log.Warn("relay disconnected", zap.Error(err), zap.Duration("reconnect_in", backoff))
		}

		c.setConnected(false)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff with jitter: base 1s, max 60s, multiplier 1.5
		backoff = time.Duration(math.Min(
			float64(backoff)*1.5*(0.8+rand.Float64()*0.4),
			float64(60*time.Second),
		))
	}
}

// IsConnected reports whether the relay connection is active.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// LatencyMs returns the last measured round-trip latency to the relay.
func (c *Client) LatencyMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.latencyMs
}

// ConnectedAt returns when the connection was established.
func (c *Client) ConnectedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectedAt
}

// Send sends a frame via the current active connection.
// Safe to call from any goroutine — resolves conn dynamically so that
// long-running agent handlers survive WebSocket reconnections.
func (c *Client) Send(f *Frame) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	var buf bytes.Buffer
	if err := f.Encode(&buf); err != nil {
		return err
	}

	writeCtx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageBinary, buf.Bytes())
}

func (c *Client) connect(ctx context.Context) error {
	c.log.Info("connecting to relay", zap.String("url", c.relayURL))

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Use a custom dialer with TCP keepalive so the OS sends keepalive probes
	// every 15s. This prevents home routers and cloud NAT tables from silently
	// dropping the idle TCP connection. 15s is below the common 30s NAT idle
	// timeout, ensuring the entry stays alive between application heartbeats.
	netDialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 15 * time.Second,
	}
	conn, _, err := websocket.Dial(dialCtx, c.relayURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + c.apiKey},
			"User-Agent":    []string{"fleetq-bridge/1.1"},
		},
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: netDialer.DialContext,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	// Increase read limit to 10 MB — the default 32 KB is too small for
	// large agent request payloads (e.g. assistant system prompts with tool schemas).
	conn.SetReadLimit(10 * 1024 * 1024)

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.connectedAt = time.Now()
	c.lastHeartbeatAck = time.Now()
	c.mu.Unlock()

	c.log.Info("connected to relay")

	// Clean up: clear conn reference when this connection ends, but only
	// if it hasn't already been replaced by a newer connection.
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		conn.CloseNow()
	}()

	// Send initial capability manifest
	if err := c.sendManifest(ctx, conn); err != nil {
		return fmt.Errorf("failed to send manifest: %w", err)
	}

	// Start heartbeat goroutine (tied to THIS connection)
	heartbeatCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()
	go c.heartbeat(heartbeatCtx, conn)

	// Read loop with per-read timeout
	for {
		readCtx, readCancel := context.WithTimeout(ctx, readTimeout)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			return err
		}

		c.mu.Lock()
		c.lastEventAt = time.Now()
		c.mu.Unlock()

		frame, err := Decode(bytes.NewReader(data))
		if err != nil {
			c.log.Warn("failed to decode frame", zap.Error(err))
			continue
		}

		// Dispatch uses c.Send() (dynamic conn), NOT a captured conn reference.
		// The parent ctx (from Run) is passed so handlers survive reconnections.
		if err := c.dispatch(ctx, frame); err != nil {
			c.log.Warn("frame dispatch error", zap.Error(err),
				zap.String("request_id", frame.RequestID),
				zap.Uint16("frame_type", uint16(frame.Type)))
		}
	}
}

func (c *Client) dispatch(ctx context.Context, frame *Frame) error {
	// sendFn uses c.Send() which dynamically resolves the current connection.
	// This is the key fix: when a connection drops and reconnects, handlers
	// that are still running (e.g. agent executors) will automatically use
	// the new connection instead of writing to a dead one.
	sendFn := func(f *Frame) error {
		return c.Send(f)
	}

	switch frame.Type {
	case FrameLLMRequest:
		var req LLMRequest
		if err := json.Unmarshal(frame.Payload, &req); err != nil {
			return err
		}
		go c.handler.OnLLMRequest(ctx, &req, sendFn) //nolint:errcheck

	case FrameAgentRequest:
		var req AgentRequest
		if err := json.Unmarshal(frame.Payload, &req); err != nil {
			return err
		}
		go c.handler.OnAgentRequest(ctx, &req, sendFn) //nolint:errcheck

	case FrameMCPRequest:
		go c.handler.OnMCPRequest(ctx, frame.RequestID, frame.Payload, sendFn) //nolint:errcheck

	case FrameHeartbeatAck:
		start, _ := unmarshalTimestamp(frame.Payload)
		c.mu.Lock()
		c.lastHeartbeatAck = time.Now()
		if !start.IsZero() {
			c.latencyMs = time.Since(start).Milliseconds()
		}
		c.mu.Unlock()

	case FrameRotateKey:
		var rk RotateKeyPayload
		if err := json.Unmarshal(frame.Payload, &rk); err != nil {
			return err
		}
		c.handler.OnRotateKey(ctx, rk.NewKey)
	}
	return nil
}

func (c *Client) sendFrame(ctx context.Context, conn *websocket.Conn, frame *Frame) error {
	var buf bytes.Buffer
	if err := frame.Encode(&buf); err != nil {
		return err
	}

	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageBinary, buf.Bytes())
}

func (c *Client) sendManifest(ctx context.Context, conn *websocket.Conn) error {
	manifest := c.handler.GetManifest()
	frame, err := NewJSONFrame("", FrameDiscover, manifest)
	if err != nil {
		return err
	}
	return c.sendFrame(ctx, conn, frame)
}

func (c *Client) heartbeat(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			// Check application-level ack timeout first.
			c.mu.Lock()
			lastAck := c.lastHeartbeatAck
			c.mu.Unlock()

			if time.Since(lastAck) > heartbeatAckTimeout {
				c.log.Warn("heartbeat ack timeout, closing connection",
					zap.Duration("since_last_ack", time.Since(lastAck)))
				conn.Close(websocket.StatusGoingAway, "heartbeat timeout")
				return
			}

			// WebSocket protocol-level ping — detects dead TCP connections at the
			// transport layer, independent of application logic. conn.Ping blocks
			// until the peer's pong is processed by our read loop or the context
			// times out. This catches half-open TCP states that only surface on write.
			pingCtx, pingCancel := context.WithTimeout(ctx, writeTimeout)
			pingErr := conn.Ping(pingCtx)
			pingCancel()
			if pingErr != nil {
				c.log.Warn("websocket ping failed, closing connection", zap.Error(pingErr))
				conn.Close(websocket.StatusGoingAway, "ping failed")
				return
			}

			// Application-level heartbeat for latency measurement and relay-side
			// liveness tracking. Sent after the WS ping succeeds.
			payload, _ := json.Marshal(map[string]int64{"ts": t.UnixMilli()})
			frame := &Frame{Type: FrameHeartbeat, Payload: payload}
			if err := c.sendFrame(ctx, conn, frame); err != nil {
				c.log.Warn("heartbeat send failed, closing connection", zap.Error(err))
				conn.Close(websocket.StatusGoingAway, "heartbeat send failed")
				return
			}
		}
	}
}

func (c *Client) setConnected(v bool) {
	c.mu.Lock()
	c.connected = v
	c.mu.Unlock()
}

func unmarshalTimestamp(data []byte) (time.Time, error) {
	var m map[string]int64
	if err := json.Unmarshal(data, &m); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(m["ts"]), nil
}
