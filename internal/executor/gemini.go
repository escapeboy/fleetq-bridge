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

// GeminiExecutor executes tasks via the Gemini CLI.
// Uses: gemini -p --output-format stream-json --yolo
type GeminiExecutor struct {
	binaryPath string
}

func NewGeminiExecutor(binaryPath string) *GeminiExecutor {
	return &GeminiExecutor{binaryPath: binaryPath}
}

func (e *GeminiExecutor) Key() string { return "gemini" }

func (e *GeminiExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-p", "--yolo"}

	// Gemini stream-json is experimental; fall back to --output-format json
	// if stream-json is not available. We try stream-json first.
	args = append(args, "--output-format", "stream-json")

	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	// Gemini takes prompt as positional arg, not stdin
	args = append(args, req.Prompt)

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
		return fmt.Errorf("failed to start gemini: %w", err)
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
			// Non-JSON line (e.g. when stream-json not available, falls back to text)
			enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: line}) //nolint:errcheck
			continue
		}

		event := parseGeminiEvent(req.ID, raw)
		if event != nil {
			enc.Encode(event) //nolint:errcheck
		}
	}

	exitErr := cmd.Wait()
	exitCode := 0
	if exitErr != nil {
		if ee, ok := exitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}

	return enc.Encode(&Event{RequestID: req.ID, Kind: "done", ExitCode: exitCode})
}

func parseGeminiEvent(requestID string, raw map[string]any) *Event {
	eventType, _ := raw["type"].(string)

	switch eventType {
	case "message":
		// Only emit assistant messages
		role, _ := raw["role"].(string)
		if role != "assistant" {
			return nil
		}

		// Find text in content — gemini CLI emits content as a plain string,
		// but handle the Anthropic array format too for forward compatibility.
		var sb strings.Builder
		switch c := raw["content"].(type) {
		case string:
			sb.WriteString(c)
		case []any:
			for _, item := range c {
				if m, ok := item.(map[string]any); ok {
					if m["type"] == "text" {
						if t, ok := m["text"].(string); ok {
							sb.WriteString(t)
						}
					}
				}
			}
		}
		// Direct "text" field fallback
		if sb.Len() == 0 {
			if t, ok := raw["text"].(string); ok {
				sb.WriteString(t)
			}
		}
		if sb.Len() > 0 {
			return &Event{RequestID: requestID, Kind: "output", Text: sb.String()}
		}

	case "result":
		// Gemini CLI stream-json: {"type":"result","status":"success",...}
		// Text has already been emitted via message events; just signal done.
		// Also handle --output-format json mode with response.text.
		if resp, ok := raw["response"].(map[string]any); ok {
			if text, ok := resp["text"].(string); ok && text != "" {
				return &Event{RequestID: requestID, Kind: "output", Text: text}
			}
		}
		return &Event{RequestID: requestID, Kind: "done"}

	case "error":
		msg, _ := raw["message"].(string)
		return &Event{RequestID: requestID, Kind: "error", Error: msg}
	}

	return nil
}
