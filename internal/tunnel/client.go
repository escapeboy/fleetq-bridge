package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
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

// Client manages the outbound WebSocket connection to the FleetQ relay.
type Client struct {
	relayURL string
	apiKey   string
	log      *zap.Logger
	handler  Handler

	mu   sync.Mutex
	conn *websocket.Conn

	connected    bool
	connectedAt  time.Time
	lastEventAt  time.Time
	latencyMs    int64
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

		err := c.connect(ctx)
		if ctx.Err() != nil {
			return // context cancelled
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

func (c *Client) connect(ctx context.Context) error {
	c.log.Info("connecting to relay", zap.String("url", c.relayURL))

	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(dialCtx, c.relayURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + c.apiKey},
			"User-Agent":    []string{"fleetq-bridge/1.0"},
		},
	})
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.CloseNow()

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.connectedAt = time.Now()
	c.mu.Unlock()

	c.log.Info("connected to relay")

	// Send initial capability manifest
	if err := c.sendManifest(ctx, conn); err != nil {
		return fmt.Errorf("failed to send manifest: %w", err)
	}

	// Start heartbeat goroutine
	heartbeatCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()
	go c.heartbeat(heartbeatCtx, conn)

	// Read loop
	for {
		_, data, err := conn.Read(ctx)
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

		sendFn := func(f *Frame) error {
			return c.sendFrame(ctx, conn, f)
		}

		if err := c.dispatch(ctx, frame, sendFn); err != nil {
			c.log.Warn("frame dispatch error", zap.Error(err),
				zap.String("request_id", frame.RequestID),
				zap.Uint16("frame_type", uint16(frame.Type)))
		}
	}
}

func (c *Client) dispatch(ctx context.Context, frame *Frame, send func(*Frame) error) error {
	switch frame.Type {
	case FrameLLMRequest:
		var req LLMRequest
		if err := json.Unmarshal(frame.Payload, &req); err != nil {
			return err
		}
		go c.handler.OnLLMRequest(ctx, &req, send) //nolint:errcheck

	case FrameAgentRequest:
		var req AgentRequest
		if err := json.Unmarshal(frame.Payload, &req); err != nil {
			return err
		}
		go c.handler.OnAgentRequest(ctx, &req, send) //nolint:errcheck

	case FrameMCPRequest:
		go c.handler.OnMCPRequest(ctx, frame.RequestID, frame.Payload, send) //nolint:errcheck

	case FrameHeartbeatAck:
		start, _ := unmarshalTimestamp(frame.Payload)
		if !start.IsZero() {
			c.mu.Lock()
			c.latencyMs = time.Since(start).Milliseconds()
			c.mu.Unlock()
		}

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
	return conn.Write(ctx, websocket.MessageBinary, buf.Bytes())
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			payload, _ := json.Marshal(map[string]int64{"ts": t.UnixMilli()})
			frame := &Frame{Type: FrameHeartbeat, Payload: payload}
			if err := c.sendFrame(ctx, conn, frame); err != nil {
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
