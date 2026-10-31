package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// A push subscription's endpoint is a URL the browser hands us, and every
// completed task makes the server POST to it. Left unchecked, that lets any
// logged-in user point the server at addresses only it can reach — cluster
// services, the cloud metadata endpoint, the router. The checks below keep
// those requests on the public internet, which is where every real push
// service (FCM, Mozilla autopush, Apple, WNS) lives.

// pushSendTimeout bounds one delivery. Push services answer in well under a
// second; without a bound, an endpoint that accepts the connection and
// never replies would pin a notifyTaskCompleted goroutine for good.
const pushSendTimeout = 10 * time.Second

// validatePushEndpoint is the up-front check at subscribe time. It cannot
// resolve hostnames meaningfully (DNS may answer differently at send time),
// so it only rejects what is wrong on its face; pushDialControl is the
// check that actually holds.
func validatePushEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("endpoint is not a valid URL")
	}
	if u.Scheme != "https" {
		return errors.New("endpoint must use https")
	}
	if u.Host == "" || u.Hostname() == "" {
		return errors.New("endpoint has no host")
	}
	if u.User != nil {
		return errors.New("endpoint must not carry credentials")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !isPublicAddr(ip) {
		return errors.New("endpoint must be a public address")
	}
	return nil
}

// isPublicAddr reports whether ip is an ordinary internet unicast address,
// i.e. not loopback, private (RFC 1918 / ULA), link-local (which includes
// the 169.254.169.254 metadata service), CGNAT, multicast or unspecified.
func isPublicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// Ranges the netip predicates above don't cover but that are never a push
// service: shared CGNAT space (Tailscale lives here too), the IETF
// protocol-assignment block, and benchmarking space.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 — could translate to a private v4
}

// pushDialControl runs after DNS resolution, on the exact address about to
// be connected to, so a hostname that resolves to something public at
// subscribe time and to 10.0.0.1 later is still refused.
func pushDialControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("push: unexpected dial address %q", address)
	}
	if !isPublicAddr(ap.Addr()) {
		return fmt.Errorf("push: refusing to connect to non-public address %s", ap.Addr())
	}
	return nil
}

// newPushHTTPClient is the client every push delivery goes through.
// Proxy is nil on purpose: an environment proxy would do the dialing
// itself, out of reach of pushDialControl.
func newPushHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: pushDialControl}
	return &http.Client{
		Timeout: pushSendTimeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
		},
		// The push protocol never redirects; following one would be a
		// second, unchecked request to wherever the endpoint points.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
