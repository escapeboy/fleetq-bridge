package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/fleetq/fleetq-bridge/internal/config"
)

func newLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Show the bridge log file path",
		Long:  "Prints the log file path. Use 'tail -f <path>' to follow logs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadFrom(configFile)
			if err != nil {
				return err
			}

			logPath := cfg.LogFile
			if logPath == "" {
				home, _ := os.UserHomeDir()
				logPath = filepath.Join(home, ".local", "share", "fleetq", "bridge.log")
			}

			if _, err := os.Stat(logPath); os.IsNotExist(err) {
				fmt.Printf("Log file not found: %s\n", logPath)
				fmt.Println("(The daemon may not have run yet.)")
				return nil
			}

			fmt.Println(logPath)
			fmt.Printf("\nTo follow logs:\n  tail -f %s\n", logPath)
			return nil
		},
	}
}
