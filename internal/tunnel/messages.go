package tunnel

// LLMRequest is sent from FleetQ cloud to request local LLM inference.
type LLMRequest struct {
	RequestID    string   `json:"request_id"`
	EndpointURL  string   `json:"endpoint_url"`  // e.g. "http://localhost:11434"
	Model        string   `json:"model"`
	Messages     []ChatMessage `json:"messages"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
	Temperature  float64  `json:"temperature,omitempty"`
	Stream       bool     `json:"stream"`
}

// ChatMessage is an OpenAI-compatible chat message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponseChunk is a streaming token from the local LLM.
type LLMResponseChunk struct {
	RequestID string `json:"request_id"`
	Delta     string `json:"delta"`
	Done      bool   `json:"done"`
}

// AgentRequest is sent from FleetQ cloud to execute a local agent.
type AgentRequest struct {
	RequestID        string            `json:"request_id"`
	AgentKey         string            `json:"agent_key"`
	Prompt           string            `json:"prompt"`
	SystemPrompt     string            `json:"system_prompt,omitempty"`
	Model            string            `json:"model,omitempty"`
	WorkingDirectory string            `json:"working_directory,omitempty"`
	TimeoutSeconds   int               `json:"timeout_seconds,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	Stream           bool              `json:"stream"`
}

// AgentEvent is a streaming event from a local agent execution.
type AgentEvent struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"` // "output", "error", "done"
	Text      string `json:"text,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

// DiscoverManifest is sent by the bridge to announce its capabilities.
type DiscoverManifest struct {
	BridgeVersion  string            `json:"bridge_version"`
	LLMEndpoints   []LLMEndpointInfo `json:"llm_endpoints"`
	Agents         []AgentInfo       `json:"agents"`
	MCPServers     []string          `json:"mcp_servers,omitempty"`      // names of running (bridge-managed) MCP servers
	IDEMCPConfigs  []MCPServerInfo   `json:"ide_mcp_configs,omitempty"` // full configs from IDE config files
}

// MCPServerInfo describes a discovered MCP server from a host IDE config file.
type MCPServerInfo struct {
	Name    string            `json:"name"`
	Source  string            `json:"source"` // e.g. "Claude Desktop"
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// LLMEndpointInfo describes a local LLM endpoint.
type LLMEndpointInfo struct {
	Name    string   `json:"name"`
	BaseURL string   `json:"base_url"`
	Online  bool     `json:"online"`
	Models  []string `json:"models"`
}

// AgentInfo describes a detected local agent binary.
type AgentInfo struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Version string `json:"version,omitempty"`
}

// ErrorPayload describes a bridge-level error.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RotateKeyPayload is sent by the server to trigger API key rotation.
type RotateKeyPayload struct {
	NewKey string `json:"new_key"`
}
