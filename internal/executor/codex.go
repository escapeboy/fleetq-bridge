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

// CodexExecutor executes tasks via the OpenAI Codex CLI.
// Uses: codex exec --json --full-auto "<prompt>"
// Note: Codex CLI is being deprecated by OpenAI in favour of the API.
type CodexExecutor struct {
	binaryPath string
}

func NewCodexExecutor(binaryPath string) *CodexExecutor {
	return &CodexExecutor{binaryPath: binaryPath}
}

func (e *CodexExecutor) Key() string { return "codex" }

func (e *CodexExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"exec", "--json", "--full-auto", req.Prompt}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	if req.WorkingDirectory != "" {
		cmd.Dir = req.WorkingDirectory
	}
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start codex: %w", err)
	}

	enc := json.NewEncoder(out)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		event := parseCodexEvent(req.ID, raw)
		if event != nil {
			enc.Encode(event) //nolint:errcheck
		}
	}

	cmd.Wait() //nolint:errcheck
	return enc.Encode(&Event{RequestID: req.ID, Kind: "done"})
}

func parseCodexEvent(requestID string, raw map[string]any) *Event {
	eventType, _ := raw["type"].(string)
	switch eventType {
	case "item.completed", "turn.completed":
		// Extract text output from items
		if item, ok := raw["item"].(map[string]any); ok {
			if content, ok := item["content"].(string); ok && content != "" {
				return &Event{RequestID: requestID, Kind: "output", Text: content}
			}
		}
	case "error":
		msg, _ := raw["message"].(string)
		return &Event{RequestID: requestID, Kind: "error", Error: msg}
	}
	return nil
}
