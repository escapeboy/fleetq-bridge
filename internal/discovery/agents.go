package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AgentDef defines how to detect a local AI agent binary.
type AgentDef struct {
	Key          string // internal key, e.g. "claude-code"
	DisplayName  string // human-readable name
	Binary       string // the binary to look up in PATH
	VersionArgs  []string
	OutputFormat string // "stream-json", "json", "text", "acp"
	RequiresPTY  bool   // Cursor CLI TTY bug
}

// Agent is a detected agent binary with its resolved version.
type Agent struct {
	AgentDef
	Found   bool
	Path    string
	Version string
}

// KnownAgents is the list of all supported local AI agents.
var KnownAgents = []AgentDef{
	{
		Key:          "claude-code",
		DisplayName:  "Claude Code",
		Binary:       "claude",
		VersionArgs:  []string{"--version"},
		OutputFormat: "stream-json",
	},
	{
		Key:          "gemini",
		DisplayName:  "Gemini CLI",
		Binary:       "gemini",
		VersionArgs:  []string{"--version"},
		OutputFormat: "stream-json",
	},
	{
		Key:          "opencode",
		DisplayName:  "OpenCode",
		Binary:       "opencode",
		VersionArgs:  []string{"--version"},
		OutputFormat: "acp",
	},
	{
		Key:          "cline",
		DisplayName:  "Cline CLI",
		Binary:       "cline",
		VersionArgs:  []string{"--version"},
		OutputFormat: "json",
	},
	{
		Key:          "cursor",
		DisplayName:  "Cursor CLI",
		Binary:       "agent",
		VersionArgs:  []string{"--version"},
		OutputFormat: "stream-json",
		RequiresPTY:  true,
	},
	{
		Key:          "kiro",
		DisplayName:  "Kiro CLI",
		Binary:       "kiro-cli",
		VersionArgs:  []string{"--version"},
		OutputFormat: "text",
	},
	{
		Key:          "aider",
		DisplayName:  "Aider",
		Binary:       "aider",
		VersionArgs:  []string{"--version"},
		OutputFormat: "text",
	},
	{
		Key:          "codex",
		DisplayName:  "Codex CLI",
		Binary:       "codex",
		VersionArgs:  []string{"--version"},
		OutputFormat: "json",
	},
}

// commonBinDirs are searched when exec.LookPath fails due to a minimal process PATH.
// This handles the case where bridge is started via SSH or other mechanism with a restricted PATH.
var commonBinDirs = []string{
	"/opt/homebrew/bin",   // macOS Homebrew (Apple Silicon)
	"/usr/local/bin",      // macOS Homebrew (Intel) / Linux
	"/usr/local/homebrew/bin",
}

func init() {
	// Also include ~/.local/bin from the actual user home.
	if home, err := os.UserHomeDir(); err == nil {
		commonBinDirs = append(commonBinDirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
}

// DiscoverAgents probes all known agent binaries and returns their detection results.
// binaryPaths is an optional map of agent key → explicit binary path that takes priority
// over PATH-based discovery (e.g. from bridge.yaml agents.binary_paths config).
func DiscoverAgents(ctx context.Context, enabled []string, binaryPaths map[string]string) []Agent {
	enabledSet := make(map[string]bool, len(enabled))
	for _, k := range enabled {
		enabledSet[k] = true
	}

	results := make([]Agent, 0, len(KnownAgents))
	for _, def := range KnownAgents {
		if len(enabled) > 0 && !enabledSet[def.Key] {
			results = append(results, Agent{AgentDef: def})
			continue
		}
		results = append(results, probeAgent(ctx, def, binaryPaths[def.Key]))
	}
	return results
}

func probeAgent(ctx context.Context, def AgentDef, explicitPath string) Agent {
	a := Agent{AgentDef: def}

	var binPath string

	if explicitPath != "" {
		// Explicit config path — use if file exists and is executable.
		if _, err := os.Stat(explicitPath); err == nil {
			binPath = explicitPath
		}
	}

	if binPath == "" {
		// Try PATH lookup first.
		if p, err := exec.LookPath(def.Binary); err == nil {
			binPath = p
		}
	}

	if binPath == "" {
		// PATH lookup failed — probe common install directories directly.
		// This handles bridge started via SSH/nohup with a minimal PATH.
		for _, dir := range commonBinDirs {
			candidate := filepath.Join(dir, def.Binary)
			if _, err := os.Stat(candidate); err == nil {
				binPath = candidate
				break
			}
		}
	}

	if binPath == "" {
		return a
	}

	a.Path = binPath
	a.Found = true

	// Get version with a short timeout.
	vCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	args := def.VersionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	out, err := exec.CommandContext(vCtx, binPath, args...).Output()
	if err == nil {
		a.Version = strings.TrimSpace(firstLine(string(out)))
	}

	// Special case: "agent" binary must mention "cursor" in version output
	// to avoid false positives from other tools named "agent".
	if def.Binary == "agent" && !strings.Contains(strings.ToLower(a.Version), "cursor") {
		a.Found = false
		a.Path = ""
		a.Version = ""
	}

	return a
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
