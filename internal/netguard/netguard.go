// Package netguard provides HTTP clients for requests that content authors
// can trigger (HTTP macro, workflow webhooks): only URLs on an allow list,
// no connections to internal addresses (checked after DNS resolution, so DNS
// rebinding does not help) unless explicitly allowed, redirects re-checked.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Allowed reports whether u is below one of the allowed prefixes (same
// scheme and host, path prefix on a segment boundary or exact prefix match).
func Allowed(u *url.URL, allow []string) bool {
	if u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.Host == "" {
		return false
	}
	for _, a := range allow {
		pu, err := url.Parse(strings.TrimSpace(a))
		if err != nil || pu.Host == "" {
			continue
		}
		if !strings.EqualFold(pu.Scheme, u.Scheme) || !strings.EqualFold(pu.Host, u.Host) {
			continue
		}
		prefix := pu.EscapedPath()
		path := u.EscapedPath()
		if prefix == "" || prefix == "/" || path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") ||
			(strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix)) {
			if pu.RawQuery == "" || strings.HasPrefix(u.RawQuery, pu.RawQuery) {
				return true
			}
		}
	}
	return false
}

var cgnat = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// Internal reports whether ip is not a public unicast address.
func Internal(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip) ||
		ip.Equal(net.IPv4bcast)
}

// ErrBlocked is returned for connections to internal addresses.
var ErrBlocked = errors.New("address is internal (set plugins.macros.http_allow_private to allow)")

// Client builds an HTTP client enforcing the rules.
func Client(allow []string, allowPrivate bool, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("invalid address %q", host)
		}
		if !allowPrivate && Internal(ip) {
			return ErrBlocked
		}
		return nil
	}}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	// A configured proxy would bypass the address check; only use it when
	// internal addresses are allowed anyway.
	if !allowPrivate {
		tr.Proxy = nil
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if !Allowed(req.URL, allow) {
			return fmt.Errorf("redirect to %s is not on the allow list", req.URL.Redacted())
		}
		return nil
	}}
}
