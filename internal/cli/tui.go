package cli

import (
	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open interactive terminal dashboard",
		Long:  "Opens a full-screen terminal UI showing live status, endpoints, and logs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run()
		},
	}
}
