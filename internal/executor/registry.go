package executor

import "github.com/fleetq/fleetq-bridge/internal/discovery"

// Registry maps agent keys to their executors.
type Registry struct {
	executors map[string]Executor
}

// NewRegistry builds an executor registry from the discovered agents.
func NewRegistry(agents []discovery.Agent) *Registry {
	r := &Registry{executors: make(map[string]Executor)}
	for _, a := range agents {
		if !a.Found {
			continue
		}
		var ex Executor
		switch a.Key {
		case "claude-code":
			ex = NewClaudeExecutor(a.Path)
		case "gemini":
			ex = NewGeminiExecutor(a.Path)
		case "opencode":
			ex = NewOpenCodeExecutor(a.Path)
		case "cline":
			ex = NewClineExecutor(a.Path)
		case "cursor":
			ex = NewCursorExecutor(a.Path)
		case "kiro":
			ex = NewKiroExecutor(a.Path)
		case "aider":
			ex = NewAiderExecutor(a.Path)
		case "codex":
			ex = NewCodexExecutor(a.Path)
		}
		if ex != nil {
			r.executors[a.Key] = ex
		}
	}
	return r
}

// Get returns the executor for the given agent key, or nil.
func (r *Registry) Get(key string) Executor {
	return r.executors[key]
}

// Keys returns all registered agent keys.
func (r *Registry) Keys() []string {
	keys := make([]string, 0, len(r.executors))
	for k := range r.executors {
		keys = append(keys, k)
	}
	return keys
}
