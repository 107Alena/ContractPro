package gemini

import (
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
)

func TestGeminiConfig_Validate_HappyPath(t *testing.T) {
	cfg := GeminiConfig{
		APIKey:  "AIza-test",
		BaseURL: "https://generativelanguage.googleapis.com",
		Model:   "gemini-2.5-pro",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestGeminiConfig_Validate_MissingFieldsAggregate(t *testing.T) {
	cfg := GeminiConfig{}
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

func TestGeminiConfig_Validate_InvalidScheme(t *testing.T) {
	cfg := GeminiConfig{APIKey: "k", BaseURL: "ftp://host", Model: "gemini-2.5-pro"}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("Validate() = %v, want scheme error", err)
	}
}

func TestGeminiConfig_Validate_MalformedURL(t *testing.T) {
	cfg := GeminiConfig{APIKey: "k", BaseURL: "://nope", Model: "gemini-2.5-pro"}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "BaseURL") {
		t.Fatalf("Validate() = %v, want BaseURL error", err)
	}
}

func TestGeminiConfig_Validate_RejectsUserinfoInURL(t *testing.T) {
	cfg := GeminiConfig{
		APIKey:  "k",
		BaseURL: "https://user:secret@generativelanguage.googleapis.com",
		Model:   "gemini-2.5-pro",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "userinfo") {
		t.Fatalf("Validate() = %v, want userinfo rejection", err)
	}
}

func TestGeminiConfig_Validate_WhitespaceOnlyFields(t *testing.T) {
	cfg := GeminiConfig{APIKey: "  ", BaseURL: " ", Model: "\t\n"}
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

// MUST-FIX #2: the model id is interpolated into the URL path; a "/" or ":"
// or whitespace must be rejected at config time.
func TestGeminiConfig_Validate_RejectsPathBreakingModelID(t *testing.T) {
	for _, bad := range []string{
		"gemini-2.5-pro:generateContent",
		"../../v1beta/models/x",
		"gemini 2.5 pro",
		"gemini/pro",
		"gemini\tpro",
	} {
		cfg := GeminiConfig{
			APIKey:  "k",
			BaseURL: "https://generativelanguage.googleapis.com",
			Model:   bad,
		}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "path-safe") {
			t.Errorf("Validate(model=%q) = %v, want path-safe rejection", bad, err)
		}
	}
}

func TestGeminiConfig_Validate_AcceptsRealisticModelIDs(t *testing.T) {
	for _, ok := range []string{"gemini-2.5-pro", "gemini-2.0-flash-001", "gemini-1.5-pro-002"} {
		cfg := GeminiConfig{
			APIKey:  "k",
			BaseURL: "https://generativelanguage.googleapis.com",
			Model:   ok,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate(model=%q) = %v, want nil", ok, err)
		}
	}
}

func TestIsValidModelID(t *testing.T) {
	cases := map[string]bool{
		"":                  false,
		"gemini-2.5-pro":    true,
		"a.b_c-D9":          true,
		"x:y":               false,
		"x/y":               false,
		"x y":               false,
		"x%2e":              false,
	}
	for in, want := range cases {
		if got := isValidModelID(in); got != want {
			t.Errorf("isValidModelID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDefaultHTTPClient_EnforcesTLS12_NoTimeout(t *testing.T) {
	c := defaultHTTPClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", c.Transport)
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS 1.2 floor not enforced: %+v", tr.TLSClientConfig)
	}
	if c.Timeout != 0 {
		t.Fatalf("Client.Timeout = %v, want 0 (caller's context owns the budget)", c.Timeout)
	}
}
