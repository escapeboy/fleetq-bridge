package executor

import "strings"

// RoutingRule selects an agent key for a request.
// Rules are evaluated in order; the first match wins.
type RoutingRule interface {
	// Name returns a human-readable name for logging.
	Name() string
	// Match reports whether this rule applies to req.
	Match(req *Request) bool
	// AgentKey returns the agent key to use when this rule matches.
	AgentKey() string
}

// Router evaluates an ordered chain of RoutingRules and resolves an Executor.
// If a matched agent key is not in the registry, evaluation continues to the next rule.
// If no rule matches (or no matched agent is available), nil is returned.
type Router struct {
	rules    []RoutingRule
	registry *Registry
}

// NewRouter creates a router with the given rule chain and registry.
func NewRouter(registry *Registry, rules []RoutingRule) *Router {
	return &Router{rules: rules, registry: registry}
}

// Resolve returns the first available executor whose rule matches req.
func (r *Router) Resolve(req *Request) (Executor, string) {
	for _, rule := range r.rules {
		if !rule.Match(req) {
			continue
		}
		key := rule.AgentKey()
		if ex := r.registry.Get(key); ex != nil {
			return ex, rule.Name()
		}
		// Rule matched but agent unavailable — try next rule.
	}
	return nil, ""
}

// --- built-in rules ---

// PurposeRule routes requests whose Purpose field equals a specific value.
type PurposeRule struct {
	purpose  string
	agentKey string
}

// NewPurposeRule returns a rule that matches req.Purpose == purpose.
func NewPurposeRule(purpose, agentKey string) *PurposeRule {
	return &PurposeRule{purpose: purpose, agentKey: agentKey}
}

func (r *PurposeRule) Name() string              { return "purpose:" + r.purpose }
func (r *PurposeRule) Match(req *Request) bool   { return req.Purpose == r.purpose }
func (r *PurposeRule) AgentKey() string          { return r.agentKey }

// ModelPrefixRule routes requests whose Model field has a specific prefix.
type ModelPrefixRule struct {
	prefix   string
	agentKey string
}

// NewModelPrefixRule returns a rule that matches strings.HasPrefix(req.Model, prefix).
func NewModelPrefixRule(prefix, agentKey string) *ModelPrefixRule {
	return &ModelPrefixRule{prefix: prefix, agentKey: agentKey}
}

func (r *ModelPrefixRule) Name() string            { return "model-prefix:" + r.prefix }
func (r *ModelPrefixRule) Match(req *Request) bool { return strings.HasPrefix(req.Model, r.prefix) }
func (r *ModelPrefixRule) AgentKey() string        { return r.agentKey }

// DefaultRules returns the standard rule chain.
// Evaluation order: purpose → model-prefix (gemini) → model-prefix (claude).
// Callers should fall back to reg.Get(req.AgentKey) when Resolve returns nil.
func DefaultRules() []RoutingRule {
	return []RoutingRule{
		NewPurposeRule("platform_assistant", "claude-code"),
		NewModelPrefixRule("gemini", "gemini"),
		NewModelPrefixRule("claude", "claude-code"),
	}
}
