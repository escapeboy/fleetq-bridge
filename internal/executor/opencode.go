package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// OpenCodeExecutor executes tasks via OpenCode's ACP protocol (JSON-RPC 2.0 over stdio).
type OpenCodeExecutor struct {
	binaryPath string
}

func NewOpenCodeExecutor(binaryPath string) *OpenCodeExecutor {
	return &OpenCodeExecutor{binaryPath: binaryPath}
}

func (e *OpenCodeExecutor) Key() string { return "opencode" }

// acpRequest is a minimal JSON-RPC 2.0 message for the ACP protocol.
type acpRequest struct {
	EventVersion string `json:"eventVersion"`
	SessionID    string `json:"sessionId"`
	RequestID    string `json:"requestId"`
	Seq          int    `json:"seq"`
	Stream       bool   `json:"stream"`
	Type         string `json:"type"`
	Content      string `json:"content,omitempty"`
}

type acpEvent struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (e *OpenCodeExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binaryPath, "acp")
	if req.WorkingDirectory != "" {
		cmd.Dir = req.WorkingDirectory
	}
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start opencode: %w", err)
	}

	// Send the task as an ACP message
	acpReq := acpRequest{
		EventVersion: "1",
		SessionID:    req.ID,
		RequestID:    req.ID,
		Seq:          1,
		Stream:       true,
		Type:         "message",
		Content:      req.Prompt,
	}
	if err := json.NewEncoder(stdin).Encode(acpReq); err != nil {
		return err
	}
	stdin.Close()

	enc := json.NewEncoder(out)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var evt acpEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "message", "content", "output":
			if evt.Content != "" {
				enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: evt.Content}) //nolint:errcheck
			}
		case "error":
			enc.Encode(&Event{RequestID: req.ID, Kind: "error", Error: evt.Error}) //nolint:errcheck
		case "done", "completed", "result":
			// will emit done below
		}
	}

	cmd.Wait() //nolint:errcheck
	return enc.Encode(&Event{RequestID: req.ID, Kind: "done"})
}
