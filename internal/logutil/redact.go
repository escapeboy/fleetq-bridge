package logutil

import (
	"bytes"
	"io"
	"strings"

	"go.uber.org/zap"
)

// RedactKey masks a sensitive string: keeps the first 12 characters, "...", then the
// last 4 characters. Values shorter than 16 characters become "[REDACTED]" entirely.
//
//	"sk-ant-api03-xxxxxxxxxxx...xxxx"  → "sk-ant-api03-x...xxxx"
//	"flq_team_abc...7890"
func RedactKey(s string) string {
	if len(s) < 16 {
		return "[REDACTED]"
	}
	return s[:12] + "..." + s[len(s)-4:]
}

// RedactEnv returns a shallow copy of env with sensitive values masked.
// The original map is not modified.
func RedactEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if isSensitiveEnvKey(k) {
			out[k] = RedactKey(v)
		} else {
			out[k] = v
		}
	}
	return out
}

// RedactedString returns a zap.Field whose value is the redacted form of s.
func RedactedString(key, value string) zap.Field {
	return zap.String(key, RedactKey(value))
}

// RedactingWriter wraps an io.Writer, masking lines that contain sensitive tokens
// (e.g. "sk-", "Bearer ") before forwarding to the underlying writer.
// It buffers partial lines so that each write boundary is respected.
type RedactingWriter struct {
	w   io.Writer
	buf []byte
}

// NewRedactingWriter returns a writer that redacts sensitive lines before forwarding to w.
func NewRedactingWriter(w io.Writer) *RedactingWriter {
	return &RedactingWriter{w: w}
}

// Write implements io.Writer.
func (r *RedactingWriter) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	for {
		idx := bytes.IndexByte(r.buf, '\n')
		if idx < 0 {
			break
		}
		line := r.buf[:idx+1]
		if _, err := r.w.Write(redactLine(line)); err != nil {
			return 0, err
		}
		r.buf = r.buf[idx+1:]
	}
	return len(p), nil
}

// redactLine returns a redacted version of line if it contains a sensitive token.
func redactLine(line []byte) []byte {
	s := string(line)
	if strings.Contains(s, "sk-") || strings.Contains(s, "Bearer ") {
		return []byte("[REDACTED]\n")
	}
	return line
}

// isSensitiveEnvKey reports whether the environment variable named k contains
// a secret that should not appear in logs.
func isSensitiveEnvKey(k string) bool {
	u := strings.ToUpper(k)
	return strings.HasPrefix(u, "CRED_") ||
		strings.HasSuffix(u, "_API_KEY") ||
		strings.Contains(u, "_SECRET") ||
		strings.HasSuffix(u, "_TOKEN") ||
		strings.HasSuffix(u, "_PASSWORD") ||
		strings.HasPrefix(u, "ANTHROPIC_") ||
		strings.HasPrefix(u, "OPENAI_") ||
		strings.HasPrefix(u, "GOOGLE_AI") ||
		strings.HasPrefix(u, "GEMINI_")
}
