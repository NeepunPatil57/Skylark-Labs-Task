package stream

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Resolver looks up a host's addresses. *net.Resolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// URLGuard stops a public deployment from being used to reach private
// networks (SSRF): it rejects stream URLs whose host is, or resolves to, a
// loopback, private, link-local (cloud metadata) or otherwise internal
// address. Hosts in the allow list skip the check, so the bundled test
// cameras on localhost keep working.
//
// The check runs when a stream is added. It does not stop DNS rebinding (a
// name that resolves differently by the time FFmpeg connects) or RTSP
// redirects to internal hosts.
type URLGuard struct {
	resolver Resolver
	allow    map[string]bool // "host:port"
	timeout  time.Duration
}

func NewURLGuard(allowHosts []string, r Resolver) *URLGuard {
	if r == nil {
		r = net.DefaultResolver
	}
	g := &URLGuard{resolver: r, allow: make(map[string]bool), timeout: 3 * time.Second}
	for _, h := range allowHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			g.allow[h] = true
		}
	}
	return g
}

// Check returns an ErrInvalidURL error if u points at an internal address.
func (g *URLGuard) Check(u *url.URL) error {
	host := strings.ToLower(u.Hostname())
	if g.allow[net.JoinHostPort(host, urlPort(u))] {
		return nil
	}

	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
		defer cancel()
		addrs, err = g.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addrs) == 0 {
			return fmt.Errorf("%w: could not resolve host %q", ErrInvalidURL, host)
		}
	}
	// Every address must be public: a name with one public and one private
	// address could still lead FFmpeg to the private one.
	for _, a := range addrs {
		if isInternal(a) {
			return fmt.Errorf("%w: host %q points to a private or internal address", ErrInvalidURL, host)
		}
	}
	return nil
}

func urlPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "rtsps" {
		return "322"
	}
	return "554"
}

// Ranges that net/netip's helpers don't cover.
var internalPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT, used for cloud internal networks
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64, can wrap a private IPv4
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	netip.MustParsePrefix("2001:db8::/32"),  // documentation
}

func isInternal(a netip.Addr) bool {
	a = a.Unmap() // ::ffff:10.0.0.1 is 10.0.0.1
	// IsGlobalUnicast is false for loopback, link-local (169.254/16, the cloud
	// metadata range), multicast, unspecified and broadcast. IsPrivate covers
	// 10/8, 172.16/12, 192.168/16 and fc00::/7.
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return true
	}
	for _, p := range internalPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
