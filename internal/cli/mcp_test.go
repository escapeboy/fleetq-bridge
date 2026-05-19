package cli

import (
	"path/filepath"
	"testing"

	"github.com/fleetq/fleetq-bridge/internal/config"
)

func findServer(cfg *config.Config, name string) *config.MCPServer {
	for i := range cfg.MCPServers {
		if cfg.MCPServers[i].Name == name {
			return &cfg.MCPServers[i]
		}
	}
	return nil
}

func TestMCPAddRemove(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "bridge.yaml")
	configFile = tmp
	t.Cleanup(func() { configFile = "" })

	add := newMCPAddCmd()
	add.SetArgs([]string{"computer-use", "open-computer-use", "mcp"})
	if err := add.Execute(); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	cfg, err := config.LoadFrom(tmp)
	if err != nil {
		t.Fatalf("load after add: %v", err)
	}
	got := findServer(cfg, "computer-use")
	if got == nil {
		t.Fatal("server not added")
	}
	if got.Command != "open-computer-use" || len(got.Args) != 1 || got.Args[0] != "mcp" {
		t.Fatalf("unexpected server entry: %+v", got)
	}

	dup := newMCPAddCmd()
	dup.SetArgs([]string{"computer-use", "other", "cmd"})
	if err := dup.Execute(); err == nil {
		t.Fatal("expected duplicate add to be rejected")
	}

	rm := newMCPRemoveCmd()
	rm.SetArgs([]string{"computer-use"})
	if err := rm.Execute(); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	cfg, err = config.LoadFrom(tmp)
	if err != nil {
		t.Fatalf("load after remove: %v", err)
	}
	if findServer(cfg, "computer-use") != nil {
		t.Fatal("server still present after remove")
	}

	rmMissing := newMCPRemoveCmd()
	rmMissing.SetArgs([]string{"does-not-exist"})
	if err := rmMissing.Execute(); err == nil {
		t.Fatal("expected remove of missing server to be rejected")
	}
}
