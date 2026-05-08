package executor

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/fleetq/fleetq-bridge/internal/config"
)

func TestWriteTempMCPConfig_PerRequestHttpEntry(t *testing.T) {
	perRequest := map[string]MCPServerEntry{
		"agent_fleet": {
			Type:    "http",
			URL:     "https://chat.sandbox.example.test/mcp",
			Headers: map[string]string{"Authorization": "Bearer test-token"},
		},
	}

	path, err := writeTempMCPConfig(nil, perRequest)
	if err != nil {
		t.Fatalf("writeTempMCPConfig returned error: %v", err)
	}
	defer os.Remove(path)

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(contents, &cfg); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, string(contents))
	}

	got, ok := cfg.MCPServers["agent_fleet"]
	if !ok {
		t.Fatalf("expected agent_fleet entry, got: %s", string(contents))
	}
	if got["type"] != "http" {
		t.Errorf("type=%v, want http", got["type"])
	}
	if got["url"] != "https://chat.sandbox.example.test/mcp" {
		t.Errorf("url=%v", got["url"])
	}
	headers, _ := got["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer test-token" {
		t.Errorf("Authorization header missing or wrong: %v", headers)
	}
}

func TestWriteTempMCPConfig_PerRequestShadowsStatic(t *testing.T) {
	static := []config.MCPServer{
		{Name: "filesystem", Command: "/usr/bin/fs-mcp"},
		{Name: "agent_fleet", Command: "/old/binary"},
	}
	perRequest := map[string]MCPServerEntry{
		"agent_fleet": {
			Type: "http",
			URL:  "https://new.example.test/mcp",
		},
	}

	path, err := writeTempMCPConfig(static, perRequest)
	if err != nil {
		t.Fatalf("writeTempMCPConfig returned error: %v", err)
	}
	defer os.Remove(path)

	contents, _ := os.ReadFile(path)
	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(contents, &cfg)

	if _, ok := cfg.MCPServers["filesystem"]; !ok {
		t.Errorf("static-only filesystem entry was dropped")
	}

	af := cfg.MCPServers["agent_fleet"]
	if af["url"] != "https://new.example.test/mcp" {
		t.Errorf("agent_fleet url=%v, want per-request to win", af["url"])
	}
	if af["command"] != nil {
		t.Errorf("agent_fleet command=%v, want empty (per-request shadows static)", af["command"])
	}
}

func TestWriteTempMCPConfig_DropsMalformedPerRequest(t *testing.T) {
	perRequest := map[string]MCPServerEntry{
		"valid":     {Type: "http", URL: "https://ok.test/mcp"},
		"malformed": {Headers: map[string]string{"X": "Y"}},
	}

	path, err := writeTempMCPConfig(nil, perRequest)
	if err != nil {
		t.Fatalf("writeTempMCPConfig returned error: %v", err)
	}
	defer os.Remove(path)

	contents, _ := os.ReadFile(path)
	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	_ = json.Unmarshal(contents, &cfg)

	if _, ok := cfg.MCPServers["valid"]; !ok {
		t.Errorf("valid entry dropped")
	}
	if _, ok := cfg.MCPServers["malformed"]; ok {
		t.Errorf("malformed entry should have been dropped")
	}
}

func TestWriteTempMCPConfig_EmptyReturnsError(t *testing.T) {
	if _, err := writeTempMCPConfig(nil, nil); err == nil {
		t.Errorf("expected error when no servers provided")
	}
}
