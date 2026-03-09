//go:build systray

package systray

import (
	"context"
	"os/exec"
	"runtime"

	"fyne.io/systray"
)

// icon is a minimal 1×1 pixel PNG (placeholder — replace with real icon).
var icon = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0x7c, 0x3a, 0xf8, 0x01,
	0x00, 0x00, 0x85, 0x00, 0x41, 0x7c, 0x0a, 0x87,
	0x67, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

// Run starts the system tray icon and runs until ctx is cancelled.
// isConnected is called periodically to update the tray tooltip.
func Run(ctx context.Context, isConnected func() bool) {
	onReady := func() {
		systray.SetIcon(icon)
		systray.SetTitle("FleetQ Bridge")
		systray.SetTooltip("FleetQ Bridge — local compute gateway")

		mStatus := systray.AddMenuItem("Connecting...", "Bridge connection status")
		mStatus.Disable()
		systray.AddSeparator()
		mTUI := systray.AddMenuItem("Open Dashboard (TUI)", "Open terminal dashboard")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Stop FleetQ Bridge")

		// Status update goroutine
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
					if isConnected() {
						mStatus.SetTitle("● Connected to FleetQ")
					} else {
						mStatus.SetTitle("○ Disconnected")
					}
				}
			}
		}()

		// Event loop
		go func() {
			for {
				select {
				case <-ctx.Done():
					systray.Quit()
					return
				case <-mTUI.ClickedCh:
					openTerminal("fleetq-bridge tui")
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}

	onExit := func() {}
	systray.Run(onReady, onExit)
}

// openTerminal launches a new terminal window running the given command.
func openTerminal(cmd string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e",
			`tell app "Terminal" to do script "`+cmd+`"`).Start()
	case "linux":
		// Try common terminals in order of preference
		for _, term := range []string{"gnome-terminal", "xterm", "konsole"} {
			if path, err := exec.LookPath(term); err == nil {
				_ = exec.Command(path, "-e", cmd).Start()
				return
			}
		}
	case "windows":
		_ = exec.Command("cmd", "/C", "start", "cmd", "/K", cmd).Start()
	}
}
