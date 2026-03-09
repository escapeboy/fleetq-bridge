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

// AiderExecutor executes coding tasks via Aider.
// Output is plain text; artifacts are git commits in the working directory.
type AiderExecutor struct {
	binaryPath string
}

func NewAiderExecutor(binaryPath string) *AiderExecutor {
	return &AiderExecutor{binaryPath: binaryPath}
}

func (e *AiderExecutor) Key() string { return "aider" }

func (e *AiderExecutor) Execute(ctx context.Context, req *Request, out io.Writer) error {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = defaultTimeout * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"--message", req.Prompt,
		"--yes",         // auto-confirm all prompts
		"--no-git",      // don't require git; caller controls version control
	}
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

	output, err := cmd.Output()
	enc := json.NewEncoder(out)

	if err != nil {
		exitCode := 1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
			// Aider writes output to stderr on error
			combined := strings.TrimSpace(string(output) + "\n" + string(ee.Stderr))
			enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: combined}) //nolint:errcheck
		}
		return enc.Encode(&Event{
			RequestID: req.ID,
			Kind:      "error",
			Error:     fmt.Sprintf("aider exited with code %d", exitCode),
			ExitCode:  exitCode,
		})
	}

	text := strings.TrimSpace(string(output))
	if text != "" {
		enc.Encode(&Event{RequestID: req.ID, Kind: "output", Text: text}) //nolint:errcheck
	}
	return enc.Encode(&Event{RequestID: req.ID, Kind: "done"})
}
