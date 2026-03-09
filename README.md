# FleetQ Bridge

Connects [FleetQ](https://fleetq.net) cloud agents to your local AI compute — LLMs, coding agents, MCP servers, and browser automation. No port-forwarding, no ngrok, no third-party tunnels.

## How it works

The bridge opens a persistent outbound WebSocket connection to FleetQ cloud. When your cloud agents need to run something locally, the request travels through this connection — your machine never needs to be publicly reachable.

```
FleetQ Cloud ──WSS──► FleetQ Bridge (your machine)
                              │
                   ┌──────────┼──────────┐
                   │          │          │
              Ollama/etc   claude -p   MCP servers
```

## Installation

**macOS (Homebrew)**
```bash
brew install fleetq/tap/fleetq-bridge
```

**macOS / Linux (binary)**
```bash
curl -sSL https://get.fleetq.net/bridge | sh
```

**Windows**
Download the installer from [Releases](https://github.com/fleetq/fleetq-bridge/releases).

## Quick Start

```bash
# 1. Authenticate
fleetq-bridge login --api-key flq_team_...

# 2. Install as auto-start service
fleetq-bridge install

# 3. Check status
fleetq-bridge status
```

## Supported Local Compute

### Local LLMs (auto-discovered)
| Software | Default Port |
|----------|-------------|
| Ollama | 11434 |
| LM Studio | 1234 |
| Jan.ai | 1337 |
| LocalAI | 8080 |
| GPT4All | 4891 |

### AI Coding Agents
| Agent | Binary |
|-------|--------|
| Claude Code | `claude` |
| Gemini CLI | `gemini` |
| OpenCode | `opencode` |
| Cline CLI | `cline` |
| Cursor CLI | `agent` |
| Kiro CLI | `kiro-cli` |
| Aider | `aider` |
| Codex CLI | `codex` |

### MCP Servers
Configure in `~/.config/fleetq/bridge.yaml`:
```yaml
mcp_servers:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "~/"]
  - name: playwright
    command: npx
    args: ["-y", "@playwright/mcp"]
```

## Commands

```
fleetq-bridge login --api-key <key>  Authenticate
fleetq-bridge daemon                 Run in foreground
fleetq-bridge install                Install as system service
fleetq-bridge uninstall              Remove system service
fleetq-bridge status                 Show connection status
fleetq-bridge endpoints list         List discovered endpoints
fleetq-bridge endpoints probe        Re-probe all endpoints
fleetq-bridge logs                   Show log file path
```

## Configuration

Config file: `~/.config/fleetq/bridge.yaml`

```yaml
relay_url: wss://relay.fleetq.net/bridge/ws

discovery:
  interval_seconds: 30

agents:
  enabled: [claude-code, gemini, opencode, cline, cursor, kiro, aider, codex]
  working_directory: ~/projects
  timeout_seconds: 300

log_level: info
```

## License

MIT
