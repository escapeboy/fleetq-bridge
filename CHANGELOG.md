# Changelog

All notable changes to FleetQ Bridge are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

---

## [0.5.2] — 2026-03-22

### Fixed

- **TCP keepalive** — bridge client now uses a custom `net.Dialer` with `KeepAlive: 30s` when dialing the relay WebSocket. This causes the OS to send TCP keepalive probes every 30 seconds, preventing home routers and cloud NAT tables from silently dropping idle connections between heartbeat frames.

---

## [0.5.1] — 2026-03-22

### Fixed

- **Claude Code event parser** — `assistant` messages with only `tool_use` content blocks now emit `progress` events instead of being silently dropped. Previously 90%+ of claude-code stream-json lines were discarded, making agent tool usage invisible to the platform.
- **Result event always emitted** — the final `result` event from claude-code is now always forwarded (as kind `"result"`) regardless of whether prior `output` events were captured. Previously the complete agent answer was lost when streaming had already produced partial output.
- **New event type handlers** — added handlers for `user` (tool result feedback) and `rate_limit_event` (silently ignored) event types to eliminate "skipped" log noise.

---

## [0.5.0] — 2026-03-22

### Added

- **Redis key prefix support** — relay server now reads `REDIS_PREFIX` env var and prepends it to all Redis keys (`bridge:req:*`, `bridge:stream:*`), ensuring correct key resolution when Laravel uses a prefix on its bridge Redis connection.
- **Executor diagnostic logging** — Claude Code executor logs process PID, line counts, event kinds, and scanner errors to stderr for production debugging.
- **Debug-level event tracing** — runner logs streaming events at DEBUG level for troubleshooting without noise in production.
- **stderr passthrough** — Claude Code subprocess stderr is forwarded to the bridge daemon log for visibility into agent errors.

### Fixed

- **Claude Code authentication in LaunchAgent context** — bridge daemon running under macOS LaunchAgent now has keychain access, enabling Claude Code OAuth session tokens to work correctly. Previously, running via SSH `nohup` lacked keychain context and claude-code exited with "Not logged in".

---

## [0.4.0] — 2026-03-22

### Added

- **IDE MCP config discovery** — automatically detects MCP server configurations from Claude Code (`~/.claude.json`), Cursor (`.cursor/mcp.json`), and Windsurf (`~/.codeium/windsurf/mcp_config.json`). Discovered configs are sent to FleetQ cloud alongside local MCP servers.
- **Purpose-aware agent execution** — agents receive a `purpose` field from the relay. When purpose is `platform_assistant`, Claude Code's built-in tools are disabled so it relies exclusively on FleetQ MCP tools.

### Fixed

- **WebSocket read limit** — increased from 32 KB to 10 MB to handle large agent request payloads (e.g. assistant system prompts with full tool schemas).
- **Claude Code stream-json parsing** — updated for Claude Code 2.1.74+ which changed the streaming event format.
- **Gemini/Codex CLI output format** — executor now handles updated JSON output structure from recent CLI versions.

### Changed

- **Race-free connection registration** — relay server now registers the connection synchronously before starting the read loop, preventing a race where `updateEndpoints` could not find the connection record.

---

## [0.3.0] — 2026-03-13

### Added

- **`--config` flag** — all commands accept `--config <path>` to support multiple bridge instances with separate config files.
- **`--api-url` login flag** — specify the FleetQ server URL directly during login.
- **Relay server Dockerfile** — containerized relay server with CI workflow for building images.
- **Install script** — `curl -sSL https://get.fleetq.net | sh` for quick installation.
- **Cloudflare Worker** — serves the install script at `get.fleetq.net/bridge`.

### Fixed

- **Session ID race** — fixed race condition where endpoints were sent before the session was registered, causing them to be lost.
- **Endpoints format** — corrected the JSON structure to match Laravel's expected nested format (`endpoints.agents`, `endpoints.llm_endpoints`, `endpoints.mcp_servers`).
- **Team ID parsing** — relay now correctly reads `current_team.id` from the `/me` API response.

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
