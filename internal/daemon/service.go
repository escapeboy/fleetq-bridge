package daemon

import (
	"context"

	"github.com/kardianos/service"
	"go.uber.org/zap"
)

// program implements kardianos/service.Interface for launchd/systemd/WinSvc.
type program struct {
	runner *Runner
	log    *zap.Logger
	cancel context.CancelFunc
}

// NewService wraps a Runner in a kardianos service.
func NewService(runner *Runner, log *zap.Logger) (service.Service, error) {
	svcConfig := &service.Config{
		Name:        "fleetq-bridge",
		DisplayName: "FleetQ Bridge",
		Description: "FleetQ local compute gateway — connects FleetQ cloud to local LLMs and AI agents",
		Option: service.KeyValue{
			// macOS: install as user-level LaunchAgent (no sudo required)
			"UserService": true,
			"KeepAlive":   true,
			"RunAtLoad":   true,
			// Linux: systemd restart policy
			"Restart": "on-failure",
			// All: restart on failure with 5s delay
			"OnFailureDelayDuration": "5s",
		},
	}

	prg := &program{runner: runner, log: log}
	return service.New(prg, svcConfig)
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
