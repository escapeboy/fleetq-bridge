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
	cmd.AddCommand(newMCPListCmd(), newMCPAddCmd(), newMCPRemoveCmd())
	return cmd
}

func newMCPAddCmd() *cobra.Command {
	var env []string

	cmd := &cobra.Command{
		Use:   "add NAME COMMAND [ARGS...]",
		Short: "Add an MCP server to the bridge config",
		Long: `Adds an MCP stdio server to the bridge config file so you do not have to
edit the YAML by hand. Restart the bridge afterwards for it to take effect.

Examples:
  fleetq-bridge mcp add computer-use open-computer-use mcp
  fleetq-bridge mcp add filesystem npx -y @modelcontextprotocol/server-filesystem ~/
  fleetq-bridge mcp add --env API_KEY=xyz weather weather-mcp serve`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			command := args[1]
			cmdArgs := args[2:]

			cfg, err := config.LoadFrom(configFile)
			if err != nil {
				return err
			}
			for _, s := range cfg.MCPServers {
				if s.Name == name {
					return fmt.Errorf("MCP server %q already exists — remove it first with `fleetq-bridge mcp remove %s`", name, name)
				}
			}

			cfg.MCPServers = append(cfg.MCPServers, config.MCPServer{
				Name:    name,
				Command: command,
				Args:    cmdArgs,
				Env:     env,
			})
			if err := config.SaveTo(configFile, cfg); err != nil {
				return err
			}

			fmt.Printf("Added MCP server %q to %s\n", name, config.Resolve(configFile))
			fmt.Println("Restart the bridge for it to take effect:  fleetq-bridge daemon")
			return nil
		},
	}
	// Treat flags after the first positional arg as part of the MCP command,
	// so `mcp add fs npx -y pkg` does not error on the MCP server's own flags.
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringArrayVar(&env, "env", nil, "environment variable KEY=VALUE for the MCP server (repeatable)")
	return cmd
}

func newMCPRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove NAME",
		Aliases: []string{"rm"},
		Short:   "Remove an MCP server from the bridge config",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.LoadFrom(configFile)
			if err != nil {
				return err
			}

			kept := make([]config.MCPServer, 0, len(cfg.MCPServers))
			found := false
			for _, s := range cfg.MCPServers {
				if s.Name == name {
					found = true
					continue
				}
				kept = append(kept, s)
			}
			if !found {
				return fmt.Errorf("MCP server %q not found — run `fleetq-bridge mcp list` to see configured servers", name)
			}

			cfg.MCPServers = kept
			if err := config.SaveTo(configFile, cfg); err != nil {
				return err
			}

			fmt.Printf("Removed MCP server %q from %s\n", name, config.Resolve(configFile))
			fmt.Println("Restart the bridge for it to take effect:  fleetq-bridge daemon")
			return nil
		},
	}
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
