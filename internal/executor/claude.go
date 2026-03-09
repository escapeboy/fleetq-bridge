package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ClaudeExecutor executes tasks via the Claude Code CLI.
// Uses: claude -p --output-format stream-json --dangerously-skip-permissions
type ClaudeExecutor struct {
	binaryPath string
}

// NewClaudeExecutor creates a new executor for Claude Code.
func NewClaudeExecutor(binaryPath string) *ClaudeExecutor {
	return &ClaudeExecutor{binaryPath: binaryPath}
}

func (e *ClaudeExecutor) Key() string { return "claude-code" }

func (e *ClaudeExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--dangerously-skip-permissions",
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}

	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	cmd.Stdin = strings.NewReader(req.Prompt)

	if req.WorkingDirectory != "" {
		cmd.Dir = req.WorkingDirectory
	}

	// Propagate env overrides
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
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

		event := parseClaudeEvent(req.ID, raw)
		if event != nil {
			if err := enc.Encode(event); err != nil {
				return err
			}
		}
	}

	exitErr := cmd.Wait()
	exitCode := 0
	if exitErr != nil {
		if ee, ok := exitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}

	return enc.Encode(&Event{
		RequestID: req.ID,
		Kind:      "done",
		ExitCode:  exitCode,
	})
}

// parseClaudeEvent translates a Claude Code stream-json line into our Event type.
func parseClaudeEvent(requestID string, raw map[string]any) *Event {
	msgType, _ := raw["type"].(string)

	switch msgType {
	case "assistant":
		// Extract text from content blocks
		msg, _ := raw["message"].(map[string]any)
		if msg == nil {
			return nil
		}
		content, _ := msg["content"].([]any)
		var sb strings.Builder
		for _, block := range content {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if b["type"] == "text" {
				if t, ok := b["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		if sb.Len() == 0 {
			return nil
		}
		return &Event{RequestID: requestID, Kind: "output", Text: sb.String()}

	case "result":
		return &Event{RequestID: requestID, Kind: "done"}

	case "error":
		msg, _ := raw["message"].(string)
		return &Event{RequestID: requestID, Kind: "error", Error: msg}
	}

	return nil
}
