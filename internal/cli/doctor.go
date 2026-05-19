package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/auth"
	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/ipc"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the bridge setup and report only what needs attention",
		Long: `Runs a series of health checks against the bridge configuration and
running daemon. Passing checks are shown with ✓; anything that needs attention
is shown with ✗ followed by a → hint on how to fix it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			problems := 0
			pass := func(label string) { fmt.Printf("  ✓ %s\n", label) }
			info := func(label string) { fmt.Printf("  · %s\n", label) }
			fail := func(label, hint string) {
				problems++
				fmt.Printf("  ✗ %s\n      → %s\n", label, hint)
			}

			// 1. Config file
			cfg, err := config.LoadFrom(configFile)
			if err != nil {
				fail("config file", fmt.Sprintf("%s is invalid: %v", config.Resolve(configFile), err))
				fmt.Println("\nFix the config file before continuing.")
				os.Exit(1)
			}
			pass("config file " + config.Resolve(configFile))

			// 2. API key
			if _, kerr := auth.Load(cfg.APIKey); kerr != nil {
				fail("API key", "run `fleetq-bridge login --api-url <url> --api-key <token>`")
			} else {
				pass("API key resolved")
			}

			// 3. Relay URL
			if cfg.RelayURL == "" {
				fail("relay URL", "relay_url is missing from the config file")
			} else {
				pass("relay URL " + cfg.RelayURL)
			}

			// 4. Daemon + relay connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			client, derr := ipc.DialAt(ctx, ipc.SocketPathFor(configFile))
			if derr != nil {
				fail("daemon running",
					"start it with `fleetq-bridge daemon` (or `fleetq-bridge install` for auto-start)")
			} else {
				defer client.Close()
				status, serr := client.GetStatus()
				switch {
				case serr != nil:
					fail("daemon status", "daemon is up but did not return status: "+serr.Error())
				default:
					pass("daemon running")
					if status.Connected {
						pass("relay connected")
					} else {
						fail("relay connected",
							"daemon is up but not connected — check network and API key")
					}

					online := 0
					for _, ep := range status.LLMEndpoints {
						if ep.Online {
							online++
						}
					}
					info(fmt.Sprintf("local LLMs: %d online / %d discovered", online, len(status.LLMEndpoints)))

					ready := 0
					for _, a := range status.Agents {
						if a.Found {
							ready++
						}
					}
					if ready == 0 {
						fail("local agents",
							"no AI coding agents found on PATH — install at least one (claude-code, codex, gemini, …)")
					} else {
						pass(fmt.Sprintf("local agents: %d ready", ready))
					}
				}
			}

			// 5. MCP servers (informational)
			info(fmt.Sprintf("MCP servers configured: %d", len(cfg.MCPServers)))

			fmt.Println()
			if problems == 0 {
				fmt.Println("All checks passed. Bridge is healthy.")
				return nil
			}
			fmt.Printf("%d issue(s) need attention — see the → hints above.\n", problems)
			os.Exit(1)
			return nil
		},
	}
}
