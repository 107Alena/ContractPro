package validator

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"contractpro/document-processing/internal/domain/port"
)

// Resolver abstracts DNS hostname resolution for testability.
// In production, net.DefaultResolver is used.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// blockedCIDRs lists private, loopback, and link-local CIDR ranges
// that must not be targeted by file_url (SSRF protection).
var blockedCIDRs []*net.IPNet

func init() {
	cidrs := []string{
		"0.0.0.0/8",      // "this network" (RFC 1122) — 0.0.0.0 routes to loopback on Linux
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC 1918 private
		"172.16.0.0/12",  // RFC 1918 private
		"192.168.0.0/16", // RFC 1918 private
		"169.254.0.0/16", // IPv4 link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid CIDR %q: %v", cidr, err))
		}
		blockedCIDRs = append(blockedCIDRs, network)
	}
}

// isBlockedIP checks whether the given IP falls within any blocked CIDR range.
func isBlockedIP(ip net.IP) bool {
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// allowedSchemes lists the URL schemes allowed for file downloads.
var allowedSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// ValidateURLSecurity checks that rawURL uses an allowed scheme (http/https)
// and does not resolve to a blocked IP address (SSRF protection).
// The resolver parameter is used for DNS lookups; pass nil to use net.DefaultResolver.
func ValidateURLSecurity(ctx context.Context, rawURL string, resolver Resolver) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return port.NewSSRFBlockedError(fmt.Sprintf("invalid URL: %v", err))
	}

	// Check scheme.
	scheme := strings.ToLower(parsed.Scheme)
	if !allowedSchemes[scheme] {
		return port.NewSSRFBlockedError(
			fmt.Sprintf("URL scheme %q is not allowed, only http/https permitted", parsed.Scheme),
		)
	}

	// Extract hostname (without port).
	host := parsed.Hostname()
	if host == "" {
		return port.NewSSRFBlockedError("URL has no host")
	}

	// If host is a literal IP, check directly.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return port.NewSSRFBlockedError(
				fmt.Sprintf("URL targets blocked IP address %s", ip),
			)
		}
		return nil
	}

	// Resolve hostname and check all returned IPs.
	// DNS resolution failure is treated as a block: an attacker-controlled DNS
	// server could return SERVFAIL on pre-validation and then resolve to a
	// private IP on the actual connection (DNS rebinding). The DialContext
	// ssrfControl provides a second check, but we err on the side of security.
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil {
		return port.NewSSRFBlockedError(
			fmt.Sprintf("DNS resolution failed for host %q: %v", host, err),
		)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip != nil && isBlockedIP(ip) {
			return port.NewSSRFBlockedError(
				fmt.Sprintf("URL host %q resolves to blocked IP address %s", host, ip),
			)
		}
	}

	return nil
}
