//go:build !systray

// Package systray provides optional system tray integration.
// Build with -tags systray to enable (requires CGO_ENABLED=1).
package systray

import "context"

// Run is a no-op when built without the systray build tag.
func Run(_ context.Context, _ func() bool) {}
