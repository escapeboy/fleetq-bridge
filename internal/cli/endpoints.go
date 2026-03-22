package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/discovery"
	"github.com/fleetq/fleetq-bridge/internal/ipc"
)

func newEndpointsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoints",
		Short: "Manage local LLM endpoints",
	}

	cmd.AddCommand(newEndpointsListCmd())
	cmd.AddCommand(newEndpointsProbeCmd())
	return cmd
}

func newEndpointsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List discovered local LLM and agent endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Try daemon IPC first
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			client, err := ipc.Dial(ctx)
			if err == nil {
				defer client.Close()
				status, err := client.GetStatus()
				if err == nil {
					printStatus(status)
					return nil
				}
			}

			// Daemon not running — probe directly
			fmt.Println("(Daemon not running — probing directly)")
			ctx2 := context.Background()
			llms := discovery.DiscoverLLMs(ctx2)
			agents := discovery.DiscoverAgents(ctx2, nil, nil)

			fmt.Println("\nLocal LLMs:")
			for _, ep := range llms {
				status := "offline"
				if ep.Online {
					status = fmt.Sprintf("online (%d models)", len(ep.Models))
				}
				fmt.Printf("  %-12s %s  %s\n", ep.Name, ep.BaseURL, status)
			}

			fmt.Println("\nLocal Agents:")
			for _, a := range agents {
				status := "not found"
				if a.Found {
					status = "found " + a.Version
				}
				fmt.Printf("  %-14s %-20s %s\n", a.Key, a.Binary, status)
			}
			return nil
		},
	}
}

func newEndpointsProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "probe",
		Short: "Re-probe all local endpoints now",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Probing local endpoints...")
			ctx := context.Background()

			llms := discovery.DiscoverLLMs(ctx)
			onlineCount := 0
			for _, ep := range llms {
				if ep.Online {
					onlineCount++
					fmt.Printf("  ✓ %-12s %s (%d models)\n", ep.Name, ep.BaseURL, len(ep.Models))
				} else {
					fmt.Printf("  ○ %-12s %s (offline)\n", ep.Name, ep.BaseURL)
				}
			}

			agents := discovery.DiscoverAgents(ctx, nil, nil)
			foundCount := 0
			for _, a := range agents {
				if a.Found {
					foundCount++
					fmt.Printf("  ✓ %-14s %s\n", a.Key, a.Version)
				} else {
					fmt.Printf("  ○ %-14s (not found)\n", a.Key)
				}
			}

			fmt.Printf("\n%d LLM(s) online, %d agent(s) found\n", onlineCount, foundCount)
			return nil
		},
	}
}
