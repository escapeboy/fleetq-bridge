package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// KiroExecutor executes tasks via the Kiro CLI.
// Output is plain text (no structured JSON in chat mode as of 2026).
type KiroExecutor struct {
	binaryPath string
}

func NewKiroExecutor(binaryPath string) *KiroExecutor {
	return &KiroExecutor{binaryPath: binaryPath}
}

func (e *KiroExecutor) Key() string { return "kiro" }

func (e *KiroExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"chat",
		"--no-interactive",
		"--trust-all-tools",
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

	output, err := cmd.Output()
	if err != nil {
		exitCode := 1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		enc := json.NewEncoder(out)
		enc.Encode(&Event{RequestID: req.ID, Kind: "error", Error: fmt.Sprintf("kiro exited %d: %s", exitCode, string(output))}) //nolint:errcheck
		return enc.Encode(&Event{RequestID: req.ID, Kind: "done", ExitCode: exitCode})
	}

	enc := json.NewEncoder(out)
	text := strings.TrimSpace(string(output))
	if text != "" {
		enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: text}) //nolint:errcheck
	}
	return enc.Encode(&Event{RequestID: req.ID, Kind: "done"})
}
