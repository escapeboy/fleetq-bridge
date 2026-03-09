package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/auth"
	"github.com/fleetq/fleetq-bridge/internal/config"
)

func newLoginCmd() *cobra.Command {
	var apiKey string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with FleetQ cloud and store API key",
		Example: `  fleetq-bridge login --api-key flq_team_abc123
  FLEETQ_API_KEY=flq_team_abc123 fleetq-bridge daemon`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if apiKey == "" {
				return fmt.Errorf("--api-key is required")
			}

			if err := auth.Validate(apiKey); err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}

			// Test connectivity to the relay
			fmt.Print("Testing connection to FleetQ relay... ")
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := testRelayHealth(cfg.RelayURL, apiKey); err != nil {
				fmt.Println("⚠ Could not reach relay (will retry when daemon starts)")
				fmt.Printf("  Error: %v\n", err)
			} else {
				fmt.Println("✓")
			}

			// Store in OS keychain
			if err := auth.Store(apiKey); err != nil {
				// Fall back to config file
				cfg.APIKey = apiKey
				if saveErr := config.Save(cfg); saveErr != nil {
					return fmt.Errorf("failed to store API key: keychain error: %v; config error: %v", err, saveErr)
				}
				fmt.Println("Note: stored API key in config file (keychain unavailable)")
			} else {
				fmt.Println("API key stored in OS keychain.")
			}

			fmt.Println("\nNext steps:")
			fmt.Println("  fleetq-bridge install   # install as auto-start service")
			fmt.Println("  fleetq-bridge daemon    # run in foreground")
			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "FleetQ team API key (flq_team_...)")
	return cmd
}

// testRelayHealth does a lightweight HTTP probe to check relay reachability.
// It does NOT try to open a WebSocket — just tests HTTPS connectivity.
func testRelayHealth(relayURL, apiKey string) error {
	// Convert wss:// to https:// for health check
	healthURL := relayURL
	if len(healthURL) > 6 && healthURL[:6] == "wss://" {
		healthURL = "https://" + healthURL[6:]
	} else if len(healthURL) > 5 && healthURL[:5] == "ws://" {
		healthURL = "http://" + healthURL[5:]
	}
	// Replace /bridge/ws with /bridge/health
	if len(healthURL) > 10 {
		for _, suffix := range []string{"/bridge/ws", "/ws"} {
			if idx := len(healthURL) - len(suffix); idx >= 0 && healthURL[idx:] == suffix {
				healthURL = healthURL[:idx] + "/bridge/health"
				break
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed — check your API key")
	}
	return nil
}
