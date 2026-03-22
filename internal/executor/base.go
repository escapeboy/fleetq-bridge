package executor

import (
	"context"
	"io"
)

// Request is an agent execution request from FleetQ cloud.
type Request struct {
	ID               string            `json:"id"`
	AgentKey         string            `json:"agent_key"`
	Prompt           string            `json:"prompt"`
	SystemPrompt     string            `json:"system_prompt,omitempty"`
	Model            string            `json:"model,omitempty"`
	Purpose          string            `json:"purpose,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	TimeoutSeconds   int               `json:"timeout_seconds,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	Stream           bool              `json:"stream"`
}

// Event is a streaming response event from an agent.
type Event struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"` // "output", "error", "done"
	Text      string `json:"text,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Executor can execute an agent task.
type Executor interface {
	// Key returns the agent key this executor handles.
	Key() string
	// Execute runs the task and streams events to the provided writer.
	Execute(ctx context.Context, req *Request, out io.Writer) error
}

// defaultTimeout is used when req.TimeoutSeconds is 0.
const defaultTimeout = 300
