package validator

import (
	"context"
	"net"
	"testing"

	"contractpro/document-processing/internal/domain/port"
)

// mockResolver implements Resolver with a fixed map of host → IPs.
type mockResolver struct {
	hosts map[string][]string
}

func (m *mockResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if addrs, ok := m.hosts[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"zero_network", "0.0.0.0", true},
		{"zero_network_1", "0.0.0.1", true},
		{"loopback_v4", "127.0.0.1", true},
		{"loopback_v4_other", "127.0.0.2", true},
		{"private_10", "10.0.0.1", true},
		{"private_10_deep", "10.255.255.255", true},
		{"private_172_16", "172.16.0.1", true},
		{"private_172_31", "172.31.255.255", true},
		{"private_192_168", "192.168.1.1", true},
		{"link_local_v4", "169.254.1.1", true},
		{"loopback_v6", "::1", true},
		{"unique_local_v6", "fc00::1", true},
		{"unique_local_v6_fd", "fd12:3456::1", true},
		{"link_local_v6", "fe80::1", true},
		{"public_v4", "93.184.216.34", false},
		{"public_v4_8888", "8.8.8.8", false},
		{"public_v6", "2001:db8::1", false},
		{"not_private_172_32", "172.32.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test IP: %s", tt.ip)
			}
			got := isBlockedIP(ip)
			if got != tt.blocked {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestValidateURLSecurity(t *testing.T) {
	resolver := &mockResolver{
		hosts: map[string][]string{
			"public.example.com":  {"93.184.216.34"},
			"private.example.com": {"10.0.0.1"},
			"mixed.example.com":   {"93.184.216.34", "10.0.0.1"},
			"multi-public.com":    {"93.184.216.34", "8.8.8.8"},
		},
	}
	ctx := context.Background()

	tests := []struct {
		name    string
		url     string
		wantErr bool
		errCode string
	}{
		// Valid URLs.
		{
			name: "https_public_domain",
			url:  "https://public.example.com/file.pdf",
		},
		{
			name: "http_public_domain",
			url:  "http://public.example.com/file.pdf",
		},
		{
			name: "https_public_ip",
			url:  "https://93.184.216.34/file.pdf",
		},
		{
			name: "multi_public_ips",
			url:  "https://multi-public.com/file.pdf",
		},

		// Blocked schemes.
		{
			name:    "ftp_scheme",
			url:     "ftp://example.com/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "file_scheme",
			url:     "file:///etc/passwd",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "javascript_scheme",
			url:     "javascript:alert(1)",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "data_scheme",
			url:     "data:text/html,<h1>test</h1>",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "empty_scheme",
			url:     "://example.com/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},

		// Blocked IPs (literal).
		{
			name:    "loopback_ip",
			url:     "https://127.0.0.1/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "private_10_ip",
			url:     "https://10.0.0.1/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "private_172_ip",
			url:     "https://172.16.0.1:8080/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "private_192_ip",
			url:     "https://192.168.1.1/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "link_local_ip",
			url:     "http://169.254.169.254/latest/meta-data/",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "loopback_v6",
			url:     "https://[::1]/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "unique_local_v6",
			url:     "https://[fc00::1]/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "link_local_v6",
			url:     "https://[fe80::1]/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},

		// Blocked IPs (DNS resolution).
		{
			name:    "dns_resolves_to_private",
			url:     "https://private.example.com/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name:    "dns_resolves_mixed_has_private",
			url:     "https://mixed.example.com/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},

		// DNS resolution failure — block to prevent DNS rebinding attacks.
		{
			name:    "dns_failure_blocked",
			url:     "https://unresolvable.example.com/file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},

		// Edge cases.
		{
			name:    "no_host",
			url:     "https:///file.pdf",
			wantErr: true,
			errCode: port.ErrCodeSSRFBlocked,
		},
		{
			name: "url_with_port_public",
			url:  "https://93.184.216.34:8443/file.pdf",
		},
		{
			name: "url_with_path_traversal_but_valid_host",
			url:  "https://public.example.com/../../../etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURLSecurity(ctx, tt.url, resolver)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				code := port.ErrorCode(err)
				if code != tt.errCode {
					t.Errorf("error code = %q, want %q (error: %v)", code, tt.errCode, err)
				}
				if port.IsRetryable(err) {
					t.Errorf("SSRF error should not be retryable")
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
