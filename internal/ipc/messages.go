package ipc

import "time"

// MessageType identifies the IPC message kind.
type MessageType string

const (
	MsgStatus    MessageType = "status"
	MsgEvent     MessageType = "event"
	MsgSubscribe MessageType = "subscribe"
	MsgCommand   MessageType = "command"
	MsgError     MessageType = "error"
)

// Message is the wire envelope for all IPC messages.
type Message struct {
	Type    MessageType `json:"type"`
	Payload any         `json:"payload"`
}

// StatusPayload is sent in response to a "status" command.
type StatusPayload struct {
	Connected    bool      `json:"connected"`
	RelayURL     string    `json:"relay_url"`
	Uptime       string    `json:"uptime"`
	StartedAt    time.Time `json:"started_at"`
	LastEventAt  time.Time `json:"last_event_at,omitempty"`
	Latency      int64     `json:"latency_ms"`
	LLMEndpoints []LLMEndpointInfo   `json:"llm_endpoints"`
	Agents       []AgentInfo         `json:"agents"`
}

// LLMEndpointInfo describes a discovered local LLM.
type LLMEndpointInfo struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Online  bool     `json:"online"`
	Models  []string `json:"models"`
}

// AgentInfo describes a detected local agent binary.
type AgentInfo struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Binary  string `json:"binary"`
	Version string `json:"version"`
	Found   bool   `json:"found"`
}

// EventPayload is a push notification from the daemon.
type EventPayload struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
