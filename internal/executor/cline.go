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

// ClineExecutor executes tasks via the Cline CLI.
// Uses: cline --json --yolo "<prompt>"
type ClineExecutor struct {
	binaryPath string
}

func NewClineExecutor(binaryPath string) *ClineExecutor {
	return &ClineExecutor{binaryPath: binaryPath}
}

func (e *ClineExecutor) Key() string { return "cline" }

func (e *ClineExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"--json", "--yolo"}
	if req.Model != "" {
		args = append(args, "--modelid", req.Model)
	}
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
		return fmt.Errorf("failed to start cline: %w", err)
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
			// Plain text line
			enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: line}) //nolint:errcheck
			continue
		}
		if text, ok := raw["text"].(string); ok && text != "" {
			enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: text}) //nolint:errcheck
		}
	}

	cmd.Wait() //nolint:errcheck
	return enc.Encode(&Event{RequestID: req.ID, Kind: "done"})
}
