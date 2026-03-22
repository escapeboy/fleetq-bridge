package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
		"--verbose",
		"--dangerously-skip-permissions",
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.SystemPrompt != "" {
		args = append(args, "--system-prompt", req.SystemPrompt)
	}
	if req.Purpose == "platform_assistant" {
		// Disable built-in tools so the agent relies exclusively on FleetQ MCP tools.
		// The system prompt instructs it to use only mcp__fleetq__* tools.
		args = append(args, "--tools", "")
	}

	cmd := exec.CommandContext(ctx, e.binaryPath, args...)
	cmd.Stdin = strings.NewReader(req.Prompt)

	if req.WorkingDirectory != "" {
		cmd.Dir = req.WorkingDirectory
	}

	// Build environment: start from current env, drop CLAUDECODE (prevents nested-session
	// detection when the bridge itself runs inside a Claude Code session), then apply overrides.
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr // Ensure claude-code stderr goes to bridge log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start claude: %w", err)
	}
	log.Printf("[executor] claude started pid=%d request=%s", cmd.Process.Pid, req.ID)

	enc := json.NewEncoder(out)
	scanner := bufio.NewScanner(stdout)
	// Increase buffer to 1 MB — claude-code can emit large JSON lines (tool outputs, etc.).
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	outputEmitted := false
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lineCount++
		log.Printf("[executor] line %d len=%d request=%s", lineCount, len(line), req.ID)

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			log.Printf("[executor] json parse error: %v request=%s", err, req.ID)
			continue
		}

		event := parseClaudeEvent(req.ID, raw, outputEmitted)
		if event == nil {
			msgType, _ := raw["type"].(string)
			log.Printf("[executor] skipped type=%s request=%s", msgType, req.ID)
		}
		if event != nil {
			log.Printf("[executor] event kind=%s request=%s", event.Kind, req.ID)
			if event.Kind == "output" {
				outputEmitted = true
			}
			if err := enc.Encode(event); err != nil {
				log.Printf("[executor] encode error: %v request=%s", err, req.ID)
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[executor] scanner error: %v request=%s", err, req.ID)
	}
	log.Printf("[executor] scanner done lines=%d request=%s", lineCount, req.ID)

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
// outputEmitted indicates whether any output events have already been produced for this request,
// used to prevent duplicate content when both assistant messages and the result event carry text.
func parseClaudeEvent(requestID string, raw map[string]any, outputEmitted bool) *Event {
	msgType, _ := raw["type"].(string)

	switch msgType {
	case "assistant":
		// Extract text and tool_use info from content blocks.
		// Claude Code 2.1+ emits assistant messages with mixed content:
		// text blocks (narrative), tool_use blocks (tool invocations).
		msg, _ := raw["message"].(map[string]any)
		if msg == nil {
			return nil
		}
		content, _ := msg["content"].([]any)
		var sb strings.Builder
		var hasToolUse bool
		var lastToolName string
		for _, block := range content {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch b["type"] {
			case "text":
				if t, ok := b["text"].(string); ok {
					sb.WriteString(t)
				}
			case "tool_use":
				hasToolUse = true
				if name, ok := b["name"].(string); ok {
					lastToolName = name
				}
			}
		}
		if sb.Len() > 0 {
			return &Event{RequestID: requestID, Kind: "output", Text: sb.String()}
		}
		// No text but has tool_use — emit progress so upstream knows agent is working.
		if hasToolUse {
			return &Event{RequestID: requestID, Kind: "progress", Text: "tool: " + lastToolName}
		}
		return nil

	case "stream_event":
		// Claude Code 2.1+ wraps incremental events in a stream_event envelope.
		// Unwrap and extract text from content_block_delta events.
		inner, _ := raw["event"].(map[string]any)
		if inner == nil {
			return nil
		}
		if inner["type"] != "content_block_delta" {
			return nil
		}
		delta, _ := inner["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		if delta["type"] == "text_delta" {
			if t, ok := delta["text"].(string); ok && t != "" {
				return &Event{RequestID: requestID, Kind: "output", Text: t}
			}
		}
		return nil

	case "result":
		// claude-code emits {"type":"result","result":"full answer text",...} as the final event.
		// Always emit the result text as a "result" kind event (distinct from "output") so
		// that the consumer receives the complete final answer regardless of prior streaming.
		if result, ok := raw["result"].(string); ok && result != "" {
			return &Event{RequestID: requestID, Kind: "result", Text: result}
		}
		return nil

	case "system":
		// System init events — emit progress to keep the connection alive.
		return &Event{RequestID: requestID, Kind: "progress", Text: "initializing"}

	case "tool_use":
		// Claude Code is invoking a tool — emit progress to prevent upstream timeout.
		toolName, _ := raw["tool"].(string)
		if toolName == "" {
			toolName, _ = raw["name"].(string)
		}
		return &Event{RequestID: requestID, Kind: "progress", Text: "tool: " + toolName}

	case "tool_result":
		// Tool completed — emit progress.
		return &Event{RequestID: requestID, Kind: "progress", Text: "tool_result"}

	case "user":
		// Tool result feedback — emit progress to keep connection alive.
		return &Event{RequestID: requestID, Kind: "progress", Text: "tool_result"}

	case "rate_limit_event":
		// Rate limit info — ignore silently.
		return nil

	case "error":
		msg, _ := raw["message"].(string)
		return &Event{RequestID: requestID, Kind: "error", Error: msg}
	}

	return nil
}
