package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client connects to a running daemon via IPC.
type Client struct {
	conn net.Conn
	dec  *json.Decoder
	enc  *json.Encoder
}

// Dial connects to the daemon IPC socket.
func Dial(ctx context.Context) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", SocketPath())
	if err != nil {
		return nil, fmt.Errorf("daemon not running (could not connect to %s): %w", SocketPath(), err)
	}
	return &Client{
		conn: conn,
		dec:  json.NewDecoder(conn),
		enc:  json.NewEncoder(conn),
	}, nil
}

// GetStatus requests and returns the daemon status.
func (c *Client) GetStatus() (*StatusPayload, error) {
	c.conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	if err := c.enc.Encode(Message{Type: MsgCommand, Payload: "status"}); err != nil {
		return nil, err
	}

	var msg Message
	if err := c.dec.Decode(&msg); err != nil {
		return nil, err
	}

	data, err := json.Marshal(msg.Payload)
	if err != nil {
		return nil, err
	}
	var s StatusPayload
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Close closes the IPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
