package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/fleetq/fleetq-bridge/internal/auth"
	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/daemon"
	"github.com/fleetq/fleetq-bridge/internal/ipc"
	"github.com/fleetq/fleetq-bridge/internal/systray"
)

func newDaemonCmd() *cobra.Command {
	var logLevel string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the bridge daemon (connects to FleetQ cloud relay)",
		Long: `Starts the FleetQ Bridge daemon.

When running in a terminal, logs are printed to stdout.
When installed as a system service, logs go to the configured log file.

The daemon reconnects automatically on network interruptions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadFrom(configFile)
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}

			if logLevel != "" {
				cfg.LogLevel = logLevel
			}

			apiKey, err := auth.Load(cfg.APIKey)
			if err != nil {
				return fmt.Errorf("%w\nRun: fleetq-bridge login --api-key <your-key>", err)
			}

			log := buildLogger(cfg.LogLevel)
			defer log.Sync() //nolint:errcheck

			runner := daemon.NewRunner(cfg, apiKey, log, ipc.SocketPathFor(configFile))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle OS signals for graceful shutdown
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				log.Info("shutting down")
				cancel()
			}()

			// Start system tray icon (no-op if built without -tags systray)
			go systray.Run(ctx, runner.IsConnected)

			return runner.Run(ctx)
		},
	}

	cmd.Flags().StringVar(&logLevel, "log-level", "", "log level (debug, info, warn, error)")
	return cmd
}

func buildLogger(level string) *zap.Logger {
	lvl := zapcore.InfoLevel
	switch level {
	case "debug":
		lvl = zapcore.DebugLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		zapcore.AddSync(os.Stdout),
		lvl,
	)
	return zap.New(core)
}
