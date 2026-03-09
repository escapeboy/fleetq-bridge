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

	"github.com/creack/pty"
)

// CursorExecutor executes tasks via the Cursor CLI agent binary.
// Requires a PTY due to a known TTY bug in the Cursor CLI.
// See: https://forum.cursor.com/t/cursor-agent-p-print-headless-mode-hangs-indefinitely-and-never-returns/150246
type CursorExecutor struct {
	binaryPath string
}

func NewCursorExecutor(binaryPath string) *CursorExecutor {
	return &CursorExecutor{binaryPath: binaryPath}
}

func (e *CursorExecutor) Key() string { return "cursor" }

func (e *CursorExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--force",
		req.Prompt,
	}

	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	if req.WorkingDirectory != "" {
		cmd.Dir = req.WorkingDirectory
	}
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Allocate a PTY to work around the Cursor CLI TTY requirement.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start cursor with PTY: %w", err)
	}
	defer ptmx.Close()

	enc := json.NewEncoder(out)
	scanner := bufio.NewScanner(ptmx)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			// Non-JSON (e.g. PTY control sequences) — skip
			continue
		}

		event := parseCursorEvent(req.ID, raw)
		if event != nil {
			enc.Encode(event) //nolint:errcheck
		}
	}

	cmd.Wait() //nolint:errcheck
	return enc.Encode(&Event{RequestID: req.ID, Kind: "done"})
}

func parseCursorEvent(requestID string, raw map[string]any) *Event {
	msgType, _ := raw["type"].(string)
	switch msgType {
	case "assistant", "message":
		if text, ok := raw["text"].(string); ok && text != "" {
			return &Event{RequestID: requestID, Kind: "output", Text: text}
		}
	case "error":
		msg, _ := raw["message"].(string)
		return &Event{RequestID: requestID, Kind: "error", Error: msg}
	case "result", "done":
		return &Event{RequestID: requestID, Kind: "done"}
	}
	return nil
}
