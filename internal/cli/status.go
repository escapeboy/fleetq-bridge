package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/ipc"
)

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show bridge connection status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			client, err := ipc.Dial(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "daemon not running:", err)
				os.Exit(1)
			}
			defer client.Close()

			status, err := client.GetStatus()
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			if jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(status)
			}

			printStatus(status)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func printStatus(s *ipc.StatusPayload) {
	connStr := "● CONNECTED"
	if !s.Connected {
		connStr = "○ DISCONNECTED"
	}

	fmt.Printf("Status:   %s\n", connStr)
	fmt.Printf("Relay:    %s\n", s.RelayURL)
	fmt.Printf("Uptime:   %s\n", s.Uptime)
	if s.Latency > 0 {
		fmt.Printf("Latency:  %dms\n", s.Latency)
	}

	// LLMs
	fmt.Println("\nLocal LLMs:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tURL\tSTATUS\tMODELS")
	for _, ep := range s.LLMEndpoints {
		status := "offline"
		if ep.Online {
			status = "online"
		}
		models := "-"
		if len(ep.Models) > 0 {
			models = fmt.Sprintf("%d available", len(ep.Models))
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", ep.Name, ep.URL, status, models)
	}
	w.Flush()

	// Agents
	fmt.Println("\nLocal Agents:")
	w2 := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w2, "  KEY\tNAME\tSTATUS\tVERSION")
	for _, a := range s.Agents {
		status := "not found"
		if a.Found {
			status = "ready"
		}
		ver := "-"
		if a.Version != "" {
			ver = a.Version
		}
		fmt.Fprintf(w2, "  %s\t%s\t%s\t%s\n", a.Key, a.Name, status, ver)
	}
	w2.Flush()
}
