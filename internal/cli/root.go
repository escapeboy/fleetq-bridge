package cli

import (
	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/version"
)

// configFile holds the --config flag value (empty = use default path).
var configFile string

// NewRootCmd builds the top-level cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "fleetq-bridge",
		Short:   "FleetQ Bridge — local compute gateway for FleetQ cloud",
		Long:    "FleetQ Bridge connects FleetQ cloud agents to your local LLMs, AI coding agents, and MCP servers.",
		Version: version.String(),
	}

	root.PersistentFlags().StringVar(&configFile, "config", "",
		"config file (default: ~/.config/fleetq/bridge.yaml)\n"+
			"Use a different file to run multiple independent bridge instances, e.g.:\n"+
			"  fleetq-bridge --config ~/.config/fleetq/bridge-local.yaml login --api-url http://localhost:8080 --api-key TOKEN\n"+
			"  fleetq-bridge --config ~/.config/fleetq/bridge-local.yaml daemon")

	root.AddCommand(
		newLoginCmd(),
		newDaemonCmd(),
		newStatusCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newLogsCmd(),
		newEndpointsCmd(),
		newTUICmd(),
		newMCPCmd(),
	)

	return root
}
