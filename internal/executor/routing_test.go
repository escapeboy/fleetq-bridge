package executor

import (
	"context"
	"io"
	"testing"
)

// mockExecutor is a minimal Executor for test use.
type mockExecutor struct{ key string }

func (m *mockExecutor) Key() string { return m.key }
func (m *mockExecutor) Execute(_ context.Context, _ *Request, _ io.Writer) error {
	return nil
}

// buildRegistry builds a Registry with the given keys pre-populated.
func buildRegistry(keys ...string) *Registry {
	r := &Registry{executors: make(map[string]Executor, len(keys))}
	for _, k := range keys {
		r.executors[k] = &mockExecutor{key: k}
	}
	return r
}

func TestPurposeRule(t *testing.T) {
	rule := NewPurposeRule("platform_assistant", "claude-code")

	if rule.Name() != "purpose:platform_assistant" {
		t.Errorf("unexpected name: %s", rule.Name())
	}
	if !rule.Match(&Request{Purpose: "platform_assistant"}) {
		t.Error("should match purpose=platform_assistant")
	}
	if rule.Match(&Request{Purpose: "other"}) {
		t.Error("should not match purpose=other")
	}
	if rule.Match(&Request{}) {
		t.Error("should not match empty purpose")
	}
	if rule.AgentKey() != "claude-code" {
		t.Errorf("unexpected agent key: %s", rule.AgentKey())
	}
}

func TestModelPrefixRule(t *testing.T) {
	rule := NewModelPrefixRule("gemini", "gemini")

	if rule.Name() != "model-prefix:gemini" {
		t.Errorf("unexpected name: %s", rule.Name())
	}
	if !rule.Match(&Request{Model: "gemini-2.0-flash"}) {
		t.Error("should match model with gemini prefix")
	}
	if !rule.Match(&Request{Model: "gemini"}) {
		t.Error("should match exact prefix")
	}
	if rule.Match(&Request{Model: "claude-3"}) {
		t.Error("should not match claude model")
	}
	if rule.Match(&Request{Model: ""}) {
		t.Error("should not match empty model")
	}
}

func TestRouterResolve(t *testing.T) {
	reg := buildRegistry("claude-code", "gemini")
	router := NewRouter(reg, DefaultRules())

	cases := []struct {
		name        string
		req         *Request
		wantKey     string
		wantNilRule bool
	}{
		{
			name:    "purpose=platform_assistant routes to claude-code",
			req:     &Request{AgentKey: "gemini", Purpose: "platform_assistant"},
			wantKey: "claude-code",
		},
		{
			name:    "model prefix gemini routes to gemini",
			req:     &Request{AgentKey: "claude-code", Model: "gemini-2.5-pro"},
			wantKey: "gemini",
		},
		{
			name:    "model prefix claude routes to claude-code",
			req:     &Request{AgentKey: "gemini", Model: "claude-sonnet-4-5"},
			wantKey: "claude-code",
		},
		{
			name:        "no rule match returns nil",
			req:         &Request{AgentKey: "kiro", Model: "gpt-4o"},
			wantNilRule: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ex, _ := router.Resolve(tc.req)
			if tc.wantNilRule {
				if ex != nil {
					t.Errorf("expected nil executor, got %s", ex.Key())
				}
				return
			}
			if ex == nil {
				t.Fatal("expected non-nil executor")
			}
			if ex.Key() != tc.wantKey {
				t.Errorf("got key=%s, want %s", ex.Key(), tc.wantKey)
			}
		})
	}
}

func TestRouterSkipsUnavailableAgent(t *testing.T) {
	// Only gemini registered, not claude-code.
	reg := buildRegistry("gemini")
	router := NewRouter(reg, DefaultRules())

	// Purpose rule matches claude-code but it's not in registry.
	// Model prefix "claude" also maps to claude-code (unavailable).
	// No further rules → nil.
	ex, _ := router.Resolve(&Request{Purpose: "platform_assistant", Model: "claude-sonnet"})
	if ex != nil {
		t.Errorf("expected nil (claude-code unavailable), got %s", ex.Key())
	}
}

func TestRouterFallsThrough(t *testing.T) {
	// Purpose rule matched but agent unavailable; model prefix matches gemini.
	reg := buildRegistry("gemini") // claude-code NOT available
	router := NewRouter(reg, DefaultRules())

	// Purpose=platform_assistant → claude-code (unavailable) → skip
	// Model=gemini-2.5-flash → gemini (available) → use it
	ex, ruleName := router.Resolve(&Request{Purpose: "platform_assistant", Model: "gemini-2.5-flash"})
	if ex == nil {
		t.Fatal("expected gemini executor after fallthrough")
	}
	if ex.Key() != "gemini" {
		t.Errorf("expected gemini, got %s", ex.Key())
	}
	if ruleName != "model-prefix:gemini" {
		t.Errorf("expected model-prefix:gemini rule name, got %s", ruleName)
	}
}

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()
	if len(rules) != 3 {
		t.Fatalf("expected 3 default rules, got %d", len(rules))
	}
	names := []string{"purpose:platform_assistant", "model-prefix:gemini", "model-prefix:claude"}
	for i, want := range names {
		if rules[i].Name() != want {
			t.Errorf("rule[%d]: got %s, want %s", i, rules[i].Name(), want)
		}
	}
}
