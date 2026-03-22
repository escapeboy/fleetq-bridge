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
	RelayURL string `yaml:"relay_url"`
	APIURL   string `yaml:"api_url"`  // base URL of the FleetQ instance (e.g. https://myfleetq.example.com)
	APIKey   string `yaml:"api_key"`  // fallback if keychain unavailable

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
	Enabled          []string          `yaml:"enabled"`
	WorkingDirectory string            `yaml:"working_directory"`
	TimeoutSeconds   int               `yaml:"timeout_seconds"`
	BinaryPaths      map[string]string `yaml:"binary_paths"` // explicit binary paths — overrides PATH lookup
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

// Path returns the default config file path.
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "fleetq", "bridge.yaml")
}

// Resolve returns path if non-empty, otherwise the default path.
func Resolve(path string) string {
	if path != "" {
		return path
	}
	return Path()
}

// Load reads the default config file, returning defaults if not found.
func Load() (*Config, error) {
	return LoadFrom("")
}

// LoadFrom reads the config file at path (empty = default), returning defaults if not found.
func LoadFrom(path string) (*Config, error) {
	cfg := Default()
	p := Resolve(path)

	data, err := os.ReadFile(p)
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

// Save writes the config to the default path.
func Save(cfg *Config) error {
	return SaveTo("", cfg)
}

// SaveTo writes the config to path (empty = default).
func SaveTo(path string, cfg *Config) error {
	p := Resolve(path)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}
