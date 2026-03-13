package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/fleetq/fleetq-bridge/internal/auth"
	"github.com/fleetq/fleetq-bridge/internal/config"
	"github.com/fleetq/fleetq-bridge/internal/daemon"
	"github.com/fleetq/fleetq-bridge/internal/ipc"
)

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install FleetQ Bridge as a system service (auto-start on login)",
		Long: `Installs the bridge as:
  • macOS:   ~/Library/LaunchAgents/net.fleetq.<name>.plist
  • Linux:   ~/.config/systemd/user/fleetq-<name>.service
  • Windows: Windows Service Manager`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, name, err := buildService(configFile)
			if err != nil {
				return err
			}

			if err := svc.Install(); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}

			fmt.Printf("FleetQ Bridge (%s) installed as a system service.\n", name)
			fmt.Println("Start it now with:")
			switch {
			case isWindows():
				fmt.Printf("  sc start %s\n", name)
			case isMac():
				fmt.Printf("  launchctl load ~/Library/LaunchAgents/net.fleetq.%s.plist\n", name)
			default:
				fmt.Printf("  systemctl --user enable --now %s\n", name)
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
			svc, _, err := buildService(configFile)
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

// buildService creates a kardianos service for the given config file path.
// Returns the service, its name, and any error.
func buildService(configPath string) (service.Service, string, error) {
	cfg, err := config.LoadFrom(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("config error: %w", err)
	}
	apiKey, err := auth.Load(cfg.APIKey)
	if err != nil {
		return nil, "", err
	}

	// Derive a stable service name from the config file stem.
	// bridge.yaml → "fleetq-bridge", bridge-local.yaml → "fleetq-bridge-local"
	name := serviceNameFor(configPath)

	log, _ := zap.NewProduction()
	socketPath := ipc.SocketPathFor(configPath)
	runner := daemon.NewRunner(cfg, apiKey, log, socketPath)
	_ = context.Background()

	svcConfig := &service.Config{
		Name:        name,
		DisplayName: "FleetQ Bridge (" + name + ")",
		Description: "FleetQ local compute gateway — connects FleetQ to local LLMs and AI agents",
		Option: service.KeyValue{
			"UserService": true,
			"KeepAlive":   true,
			"RunAtLoad":   true,
			"Restart":     "on-failure",
			"OnFailureDelayDuration": "5s",
		},
	}

	svc, err := service.New(serviceProgram(runner, log), svcConfig)
	return svc, name, err
}

// serviceNameFor derives a system service name from the config file path.
// Default config → "fleetq-bridge". Custom → "fleetq-<stem>".
func serviceNameFor(configPath string) string {
	if configPath == "" {
		return "fleetq-bridge"
	}
	base := filepath.Base(configPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return "fleetq-" + stem
}

// serviceProgram wraps a Runner to implement kardianos/service.Interface.
type program struct {
	runner *daemon.Runner
	log    *zap.Logger
	cancel context.CancelFunc
}

func serviceProgram(runner *daemon.Runner, log *zap.Logger) service.Interface {
	return &program{runner: runner, log: log}
}

func (p *program) Start(s service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go func() {
		if err := p.runner.Run(ctx); err != nil {
			p.log.Error("daemon exited with error", zap.Error(err))
		}
	}()
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}

func isMac() bool {
	_, err := os.Stat("/Library")
	return err == nil
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
