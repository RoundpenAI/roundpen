package httpx

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// BlockedDialAddr validates a dial address ("1.2.3.4:443" or "[::1]:443").
// DNS is resolved before the dial-control hook runs, so a name that rebinds
// to a blocked address is rejected too.
//
// Loopback, link-local (including the cloud metadata address 169.254.169.254),
// unspecified and multicast are always blocked (loopback only when
// allowLoopback is set, for tests). Private ranges (RFC1918/ULA) are blocked
// unless allowPrivate is set.
func BlockedDialAddr(address string, allowLoopback, allowPrivate bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = strings.Trim(address, "[]")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("address %q is not allowed", address)
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	if ip.IsLoopback() && !allowLoopback {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	if ip.IsPrivate() && !allowPrivate {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	return nil
}

// GuardedDialer returns a *net.Dialer wired to BlockedDialAddr.
func GuardedDialer(allowLoopback, allowPrivate bool) *net.Dialer {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		return BlockedDialAddr(address, allowLoopback, allowPrivate)
	}
	return dialer
}

// GuardedTransport returns an *http.Transport that only dials addresses
// allowed by BlockedDialAddr.
func GuardedTransport(allowLoopback, allowPrivate bool) *http.Transport {
	return &http.Transport{
		ForceAttemptHTTP2:     true,
		DialContext:           GuardedDialer(allowLoopback, allowPrivate).DialContext,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}
