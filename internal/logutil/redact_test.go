package logutil

import (
	"testing"
)

func TestRedactKey(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Normal long keys — keep prefix 12 + "..." + last 4
		{"sk-ant-api03-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "sk-ant-api03...xxxx"},
		{"flq_team_abcdefghij1234567890", "flq_team_abc...7890"},
		{"AAABBBCCCDDDEEEFFFGGGHHHIII", "AAABBBCCCDDD...HIII"},
		// Exactly 16 chars — minimum to show partial
		{"1234567890abcdef", "1234567890ab...cdef"},
		// Short values — fully redacted
		{"short", "[REDACTED]"},
		{"", "[REDACTED]"},
		{"abc", "[REDACTED]"},
		{"123456789012345", "[REDACTED]"}, // 15 chars, just below threshold
	}
	for _, tc := range cases {
		got := RedactKey(tc.input)
		if got != tc.want {
			t.Errorf("RedactKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRedactEnv(t *testing.T) {
	env := map[string]string{
		"ANTHROPIC_API_KEY":  "sk-ant-api03-realkey12345678",
		"OPENAI_API_KEY":     "sk-openai-realkey1234567890",
		"GOOGLE_AI_API_KEY":  "google-ai-key-1234567890123",
		"CRED_GITHUB_TOKEN":  "ghp_realtoken1234567890xxxx",
		"MY_SERVICE_SECRET":  "supersecretvalue123456789",
		"DB_PASSWORD":        "correcthorsebatterystaple",
		"GEMINI_API_KEY":     "gemini-key-xxxxxxxxxxx12345",
		"HOME":               "/Users/katsarov",
		"PATH":               "/usr/bin:/bin",
		"WORKING_DIRECTORY":  "/Users/katsarov/projects",
		"AGENT_KEY":          "claude-code",
		"PORT":               "8080",
	}

	got := RedactEnv(env)

	sensitive := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"GOOGLE_AI_API_KEY",
		"CRED_GITHUB_TOKEN",
		"MY_SERVICE_SECRET",
		"DB_PASSWORD",
		"GEMINI_API_KEY",
	}
	for _, k := range sensitive {
		if got[k] == env[k] {
			t.Errorf("RedactEnv: key %q was NOT redacted (got %q)", k, got[k])
		}
		if got[k] == "" {
			t.Errorf("RedactEnv: key %q redacted to empty string", k)
		}
	}

	passthrough := []string{"HOME", "PATH", "WORKING_DIRECTORY", "AGENT_KEY", "PORT"}
	for _, k := range passthrough {
		if got[k] != env[k] {
			t.Errorf("RedactEnv: key %q should pass through unchanged, got %q", k, got[k])
		}
	}
}

func TestRedactEnvDoesNotMutateOriginal(t *testing.T) {
	orig := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-verysecretkey12345678"}
	origVal := orig["ANTHROPIC_API_KEY"]

	RedactEnv(orig)

	if orig["ANTHROPIC_API_KEY"] != origVal {
		t.Error("RedactEnv mutated the original map")
	}
}

func TestRedactEnvNil(t *testing.T) {
	got := RedactEnv(nil)
	if len(got) != 0 {
		t.Errorf("RedactEnv(nil) should return empty map, got %v", got)
	}
}

func TestIsSensitiveEnvKey(t *testing.T) {
	sensitive := []string{
		"ANTHROPIC_API_KEY", "anthropic_api_key",
		"OPENAI_API_KEY", "OPENAI_SECRET",
		"GOOGLE_AI_KEY",
		"GEMINI_API_KEY",
		"CRED_SOME_SERVICE",
		"MY_DB_SECRET", "MY_TOKEN", "MY_PASSWORD",
		"STRIPE_SECRET_KEY",
	}
	for _, k := range sensitive {
		if !isSensitiveEnvKey(k) {
			t.Errorf("isSensitiveEnvKey(%q) should be true", k)
		}
	}

	nonSensitive := []string{
		"HOME", "PATH", "USER", "SHELL",
		"WORKING_DIRECTORY", "AGENT_KEY", "PORT",
		"BRIDGE_VERSION", "LOG_LEVEL",
	}
	for _, k := range nonSensitive {
		if isSensitiveEnvKey(k) {
			t.Errorf("isSensitiveEnvKey(%q) should be false", k)
		}
	}
}
