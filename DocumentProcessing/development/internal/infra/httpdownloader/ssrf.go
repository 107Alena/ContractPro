package httpdownloader

import (
	"fmt"
	"net"
	"syscall"

	"contractpro/document-processing/internal/domain/port"
)

// blockedCIDRs lists private, loopback, and link-local CIDR ranges
// that must not be connected to when downloading files (SSRF defense-in-depth).
// This duplicates the list in engine/validator/ssrf.go intentionally:
// the engine layer must not import from infra, and the infra layer must not
// import from engine. The list is small and unlikely to diverge.
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

// ssrfControl is a net.Dialer Control function that inspects the resolved
// IP address before a TCP connection is established. If the address falls
// within a blocked CIDR range, the connection is refused with SSRF_BLOCKED.
// This provides defense-in-depth against DNS rebinding attacks where the
// hostname resolves to a public IP during pre-validation but to a private
// IP at actual connection time.
func ssrfControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return port.NewSSRFBlockedError(fmt.Sprintf("cannot parse address %q: %v", address, err))
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return port.NewSSRFBlockedError(fmt.Sprintf("cannot parse IP %q", host))
	}

	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return port.NewSSRFBlockedError(
				fmt.Sprintf("connection to blocked IP address %s denied", ip),
			)
		}
	}

	return nil
}
