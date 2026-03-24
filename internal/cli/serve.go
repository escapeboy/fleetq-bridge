package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/server"
)

func newServeCmd() *cobra.Command {
	var addr string
	var secret string
	var logLevel string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run bridge as HTTP server for Cloudflare Tunnel / Tailscale / ngrok",
		Long: `Starts FleetQ Bridge as an HTTP server.

Expose this server via any tunnel provider:

  # Cloudflare Tunnel (recommended, free):
  cloudflared tunnel --url http://localhost:8765

  # Tailscale Funnel:
  tailscale funnel 8765

  # ngrok:
  ngrok http 8765

Then register the tunnel URL in FleetQ → Settings → Bridge → "Connect via URL".

Endpoints:
  GET  /health    — health check
  GET  /discover  — capability manifest (agents, LLMs, MCP servers)
  POST /execute   — execute agent or LLM request (SSE streaming)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadFrom(configFile)
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}

			if logLevel != "" {
				cfg.LogLevel = logLevel
			}

			log := buildLogger(cfg.LogLevel)
			defer log.Sync() //nolint:errcheck

			srv := server.New(cfg, secret, log)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				log.Info("shutting down")
				cancel()
			}()

			return srv.Run(ctx, addr)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":8765", "HTTP listen address (host:port)")
	cmd.Flags().StringVar(&secret, "secret", "", "Bearer token secret for authentication (recommended)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level (debug, info, warn, error)")

	return cmd
}
