package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/auth"
	"github.com/fleetq/fleetq-bridge/internal/config"
)

func newLoginCmd() *cobra.Command {
	var apiKey string
	var apiURL string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with FleetQ and store API key",
		Example: `  # FleetQ cloud (default)
  fleetq-bridge login --api-key flq_team_abc123

  # Self-hosted Docker instance
  fleetq-bridge login --api-key flq_team_abc123 --api-url http://localhost:8080`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if apiKey == "" {
				return fmt.Errorf("--api-key is required")
			}

			if err := auth.Validate(apiKey); err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			// Override api_url and derive relay_url when --api-url is provided
			if apiURL != "" {
				cfg.APIURL = strings.TrimRight(apiURL, "/")
				cfg.RelayURL = deriveRelayURL(cfg.APIURL)
				fmt.Printf("Using FleetQ instance: %s\n", cfg.APIURL)
				fmt.Printf("Relay URL:             %s\n", cfg.RelayURL)
			}

			// Test connectivity to the relay
			fmt.Print("Testing connection to FleetQ relay... ")
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
				// Persist api_url / relay_url changes even when key is in keychain
				if apiURL != "" {
					if saveErr := config.Save(cfg); saveErr != nil {
						return fmt.Errorf("failed to save config: %w", saveErr)
					}
				}
			}

			fmt.Println("\nNext steps:")
			fmt.Println("  fleetq-bridge install   # install as auto-start service")
			fmt.Println("  fleetq-bridge daemon    # run in foreground")
			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "FleetQ team API key (flq_team_...)")
	cmd.Flags().StringVar(&apiURL, "api-url", "", "Base URL of your FleetQ instance (default: https://fleetq.net)")
	return cmd
}

// deriveRelayURL derives the WebSocket relay URL from the FleetQ instance base URL.
//
//	https://myfleetq.example.com  →  wss://myfleetq.example.com/bridge/ws
//	http://localhost:8080          →  ws://localhost:8070/bridge/ws  (separate relay port)
func deriveRelayURL(apiURL string) string {
	isLocal := strings.HasPrefix(apiURL, "http://")
	if isLocal {
		// For local self-hosted, relay listens on a dedicated port (8070 by default)
		host := strings.TrimPrefix(apiURL, "http://")
		if idx := strings.LastIndex(host, ":"); idx >= 0 {
			host = host[:idx]
		}
		return "ws://" + host + ":8070/bridge/ws"
	}
	// For HTTPS, relay is behind the same reverse proxy at /bridge/ws
	host := strings.TrimPrefix(apiURL, "https://")
	return "wss://" + host + "/bridge/ws"
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
