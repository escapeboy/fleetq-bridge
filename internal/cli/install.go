package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/fleetq/fleetq-bridge/internal/auth"
	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/daemon"
)

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install FleetQ Bridge as a system service (auto-start on login)",
		Long: `Installs the bridge as:
  • macOS:   ~/Library/LaunchAgents/net.fleetq.bridge.plist
  • Linux:   ~/.config/systemd/user/fleetq-bridge.service
  • Windows: Windows Service Manager`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := buildService()
			if err != nil {
				return err
			}

			if err := svc.Install(); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}

			fmt.Println("FleetQ Bridge installed as a system service.")
			fmt.Println("Start it now with:")
			switch {
			case isWindows():
				fmt.Println("  sc start fleetq-bridge")
			case isMac():
				fmt.Println("  brew services start fleetq-bridge")
				fmt.Println("  OR: launchctl load ~/Library/LaunchAgents/net.fleetq.bridge.plist")
			default:
				fmt.Println("  systemctl --user enable --now fleetq-bridge")
			}
			return nil
		},
	}
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the FleetQ Bridge system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := buildService()
			if err != nil {
				return err
			}

			// Stop if running
			svc.Stop() //nolint:errcheck

			if err := svc.Uninstall(); err != nil {
				return fmt.Errorf("uninstall failed: %w", err)
			}

			fmt.Println("FleetQ Bridge service removed.")
			return nil
		},
	}
}

func buildService() (service.Service, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	apiKey, err := auth.Load(cfg.APIKey)
	if err != nil {
		return nil, err
	}
	log, _ := zap.NewProduction()
	runner := daemon.NewRunner(cfg, apiKey, log)
	_ = context.Background() // runner holds context internally when running as service
	return daemon.NewService(runner, log)
}

func isMac() bool {
	_, err := os.Stat("/Library")
	return err == nil
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
