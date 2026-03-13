package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
)

// IDESource defines an IDE's MCP config file location.
type IDESource struct {
	Label string
	Paths map[string]string // goos -> relative path from home; "all" means any OS
	Key   string            // JSON key that holds the servers map
}

// MCPServerConfig is a single MCP server discovered from an IDE config file.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Source  string            `json:"source"` // IDE label, e.g. "Claude Desktop"
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"` // for HTTP/SSE transports
}

var knownIDESources = []IDESource{
	{
		Label: "Claude Desktop",
		Paths: map[string]string{
			"darwin":  "Library/Application Support/Claude/claude_desktop_config.json",
			"linux":   ".config/Claude/claude_desktop_config.json",
			"windows": `AppData\Roaming\Claude\claude_desktop_config.json`,
		},
		Key: "mcpServers",
	},
	{
		Label: "Claude Code",
		Paths: map[string]string{"all": ".claude.json"},
		Key:   "mcpServers",
	},
	{
		Label: "Cursor",
		Paths: map[string]string{"all": ".cursor/mcp.json"},
		Key:   "mcpServers",
	},
	{
		Label: "Windsurf",
		Paths: map[string]string{"all": ".codeium/windsurf/mcp_config.json"},
		Key:   "mcpServers",
	},
	{
		Label: "Kiro",
		Paths: map[string]string{"all": ".kiro/settings/mcp.json"},
		Key:   "mcpServers",
	},
	{
		Label: "VS Code",
		Paths: map[string]string{"all": ".vscode/mcp.json"},
		Key:   "servers",
	},
}

// DiscoverIDEMCPConfigs scans all known IDE config locations and returns found MCP servers.
// Deduplicates by server name (first occurrence wins).
func DiscoverIDEMCPConfigs() []MCPServerConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	goos := runtime.GOOS
	var results []MCPServerConfig
	seen := make(map[string]bool)

	for _, src := range knownIDESources {
		relPath, ok := src.Paths[goos]
		if !ok {
			relPath, ok = src.Paths["all"]
			if !ok {
				continue
			}
		}

		data, err := os.ReadFile(filepath.Join(home, relPath))
		if err != nil {
			continue
		}

		for name, raw := range parseIDEConfig(data, src.Key) {
			if seen[name] {
				continue
			}
			seen[name] = true
			results = append(results, buildMCPServerConfig(name, src.Label, raw))
		}
	}

	return results
}

// parseIDEConfig extracts the named server map from a raw IDE config JSON blob.
func parseIDEConfig(data []byte, key string) map[string]map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}

	raw, ok := doc[key]
	if !ok {
		return nil
	}

	servers, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	result := make(map[string]map[string]any, len(servers))
	for name, v := range servers {
		if cfg, ok := v.(map[string]any); ok {
			result[name] = cfg
		}
	}

	return result
}

// buildMCPServerConfig converts a raw server map into a typed MCPServerConfig.
func buildMCPServerConfig(name, source string, raw map[string]any) MCPServerConfig {
	cfg := MCPServerConfig{Name: name, Source: source}

	if cmd, ok := raw["command"].(string); ok {
		cfg.Command = cmd
	}
	if url, ok := raw["url"].(string); ok {
		cfg.URL = url
	}
	if args, ok := raw["args"].([]any); ok {
		for _, a := range args {
			if s, ok := a.(string); ok {
				cfg.Args = append(cfg.Args, s)
			}
		}
	}
	if env, ok := raw["env"].(map[string]any); ok {
		cfg.Env = make(map[string]string, len(env))
		for k, v := range env {
			if s, ok := v.(string); ok {
				cfg.Env[k] = s
			}
		}
	}

	return cfg
}
