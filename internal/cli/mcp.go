package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/config"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage local MCP servers",
	}
	cmd.AddCommand(newMCPListCmd())
	return cmd
}

func newMCPListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured MCP servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.MCPServers) == 0 {
				fmt.Println("No MCP servers configured.")
				fmt.Printf("\nAdd servers to %s under 'mcp_servers:'\n", config.Path())
				fmt.Println(`
Example:
  mcp_servers:
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "~/"]
    - name: playwright
      command: npx
      args: ["-y", "@playwright/mcp"]`)
				return nil
			}
			fmt.Printf("%-20s %s\n", "NAME", "COMMAND")
			fmt.Printf("%-20s %s\n", "----", "-------")
			for _, s := range cfg.MCPServers {
				cmd := s.Command
				if len(s.Args) > 0 {
					cmd += " " + s.Args[0]
					if len(s.Args) > 1 {
						cmd += " ..."
					}
				}
				fmt.Printf("%-20s %s\n", s.Name, cmd)
			}
			return nil
		},
	}
}
