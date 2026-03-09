package cli

import (
	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/version"
)

// NewRootCmd builds the top-level cobra command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "fleetq-bridge",
		Short:   "FleetQ Bridge — local compute gateway for FleetQ cloud",
		Long:    "FleetQ Bridge connects FleetQ cloud agents to your local LLMs, AI coding agents, and MCP servers.",
		Version: version.String(),
	}

	root.AddCommand(
		newLoginCmd(),
		newDaemonCmd(),
		newStatusCmd(),
		newInstallCmd(),
		newUninstallCmd(),
		newLogsCmd(),
		newEndpointsCmd(),
	)

	return root
}
