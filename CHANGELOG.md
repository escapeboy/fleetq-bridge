# Changelog

All notable changes to FleetQ Bridge are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [0.2.0] — 2026-03-09

### Added

- **TUI dashboard** (`fleetq-bridge tui`) — live terminal UI with three tabs:
  - Status tab: relay connection state, uptime, LLM/agent counts
  - Endpoints tab: per-endpoint online status with model list
  - Logs tab: streaming daemon events
  - Keyboard navigation: `Tab`/`1`–`3` to switch tabs, `j`/`k` to scroll, `q` to quit
- **MCP proxy** — routes MCP tool calls from FleetQ cloud to locally configured MCP servers
  - Configure servers in `~/.config/fleetq/bridge.yaml` under `mcp_servers`
  - `fleetq-bridge mcp list` command lists configured servers
  - Servers start automatically alongside the daemon
- **System tray icon** — menubar icon on macOS/Linux shows connection status at a glance
  - Green dot = connected, grey = disconnected
  - Menu items: Open TUI, Show Logs, Quit
- **Outbound WebSocket relay** — persistent WSS connection to FleetQ cloud
  - Heartbeat keepalive with automatic reconnect on disconnect
  - Session registration via `POST /api/v1/bridge/register`
  - Endpoint manifest pushed to cloud via `POST /api/v1/bridge/endpoints`
  - Periodic heartbeat via `POST /api/v1/bridge/heartbeat`

### Changed

- Agent executors now support streaming output via PTY (pseudo-terminal)
- Discovery probe interval is configurable (`discovery.interval_seconds` in config)

---

## [0.1.0] — 2026-03-09

### Added

- **Daemon** (`fleetq-bridge daemon`) — persistent background process
- **Auto-discovery** — probes local ports for LLM inference servers:
  - Ollama (`:11434`), LM Studio (`:1234`), Jan.ai (`:1337`), LocalAI (`:8080`), GPT4All (`:4891`)
  - Reports discovered models with online/offline status
- **Agent executors** — detects and spawns local coding agents:
  - Claude Code (`claude`), Gemini CLI (`gemini`), OpenCode (`opencode`),
    Cline CLI (`cline`), Cursor (`agent`), Kiro CLI (`kiro-cli`), Aider (`aider`), Codex (`codex`)
- **Service management** — install/uninstall as a system service:
  - macOS: launchd plist (`~/Library/LaunchAgents/net.fleetq.bridge.plist`)
  - Linux: systemd user unit (`~/.config/systemd/user/fleetq-bridge.service`)
  - Windows: SCM service (via `golang.org/x/sys/windows/svc`)
- **CLI commands**: `login`, `daemon`, `install`, `uninstall`, `status`, `endpoints list/probe`, `logs`
- **Config file** at `~/.config/fleetq/bridge.yaml` with relay URL, discovery interval, agent list, log level
- Cross-platform builds: macOS (arm64/amd64), Linux (amd64), Windows (amd64)

---

## Upcoming

- `.dmg` installer for macOS with auto-update
- Windows MSI installer
- API key rotation command
- Dashboard KPI integration in FleetQ web UI
- WebSocket relay activation (requires FleetQ Cloud relay server)
