package claude

import (
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
)

func TestClaudeConfig_Validate_HappyPath(t *testing.T) {
	cfg := ClaudeConfig{
		APIKey:  "sk-ant-test",
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-sonnet-4-6",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestClaudeConfig_Validate_MissingFieldsAggregate(t *testing.T) {
	cfg := ClaudeConfig{}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() = nil, want error")
	}
	msg := err.Error()
	for _, want := range []string{"APIKey", "BaseURL", "Model"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing token %q", msg, want)
		}
	}
}

func TestClaudeConfig_Validate_InvalidScheme(t *testing.T) {
	cfg := ClaudeConfig{
		APIKey:  "sk-ant-test",
		BaseURL: "ftp://api.anthropic.com",
		Model:   "claude-sonnet-4-6",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("Validate() = %v, want error mentioning scheme", err)
	}
}

func TestClaudeConfig_Validate_MalformedURL(t *testing.T) {
	cfg := ClaudeConfig{
		APIKey:  "sk-ant-test",
		BaseURL: "://not-a-url",
		Model:   "claude-sonnet-4-6",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "BaseURL") {
		t.Fatalf("Validate() = %v, want BaseURL error", err)
	}
}

func TestClaudeConfig_Validate_RejectsUserinfoInURL(t *testing.T) {
	cfg := ClaudeConfig{
		APIKey:  "sk-ant-test",
		BaseURL: "https://user:secret@api.anthropic.com",
		Model:   "claude-sonnet-4-6",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "userinfo") {
		t.Fatalf("Validate() = %v, want error rejecting userinfo", err)
	}
}

func TestClaudeConfig_Validate_WhitespaceOnlyFields(t *testing.T) {
	cfg := ClaudeConfig{
		APIKey:  "   ",
		BaseURL: " ",
		Model:   "\t\n",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() = nil, want error for whitespace-only fields")
	}
	for _, tok := range []string{"APIKey", "BaseURL", "Model"} {
		if !strings.Contains(err.Error(), tok) {
			t.Errorf("error %q missing %q", err.Error(), tok)
		}
	}
}

func TestDefaultHTTPClient_EnforcesTLS12(t *testing.T) {
	c := defaultHTTPClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", c.Transport)
	}
	if tr.TLSClientConfig == nil {
		t.Fatalf("Transport.TLSClientConfig is nil; TLS 1.2 floor not enforced")
	}
	if got := tr.TLSClientConfig.MinVersion; got != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want %x (TLS 1.2)", got, tls.VersionTLS12)
	}
	// No client-level Timeout — caller's context.WithTimeout is authoritative
	// (error-handling.md §7.3). A non-zero value would create a double-budget
	// race and is forbidden by the adapter contract.
	if c.Timeout != 0 {
		t.Fatalf("Client.Timeout = %v, want 0 (caller's context owns the budget)", c.Timeout)
	}
}
