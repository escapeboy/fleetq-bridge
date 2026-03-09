package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRelayURL = "wss://relay.fleetq.net/bridge/ws"
	DefaultBridgePort = 8065
)

// Config holds all bridge configuration.
type Config struct {
	RelayURL string    `yaml:"relay_url"`
	APIKey   string    `yaml:"api_key"` // fallback if keychain unavailable

	Discovery DiscoveryConfig `yaml:"discovery"`
	Agents    AgentConfig     `yaml:"agents"`
	MCPServers []MCPServer    `yaml:"mcp_servers"`

	LogLevel string `yaml:"log_level"`
	LogFile  string `yaml:"log_file"`
}

type DiscoveryConfig struct {
	IntervalSeconds int   `yaml:"interval_seconds"`
	LLMPorts        []int `yaml:"llm_ports"`
}

type AgentConfig struct {
	Enabled          []string `yaml:"enabled"`
	WorkingDirectory string   `yaml:"working_directory"`
	TimeoutSeconds   int      `yaml:"timeout_seconds"`
}

type MCPServer struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Env     []string `yaml:"env"`
}

// Default returns a config with sensible defaults.
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		RelayURL: DefaultRelayURL,
		Discovery: DiscoveryConfig{
			IntervalSeconds: 30,
			LLMPorts:        []int{11434, 1234, 1337, 8080, 4891},
		},
		Agents: AgentConfig{
			Enabled: []string{
				"claude-code", "gemini", "opencode", "cline",
				"cursor", "kiro", "aider", "codex",
			},
			WorkingDirectory: filepath.Join(home, "projects"),
			TimeoutSeconds:   300,
		},
		MCPServers: []MCPServer{
			{
				Name:    "filesystem",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", home},
			},
		},
		LogLevel: "info",
		LogFile:  filepath.Join(home, ".local", "share", "fleetq", "bridge.log"),
	}
}

// Path returns the config file path.
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "fleetq", "bridge.yaml")
}

// Load reads the config file, returning defaults if not found.
func Load() (*Config, error) {
	cfg := Default()
	path := Path()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
