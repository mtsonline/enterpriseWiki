// Package netlist checks addresses against lists of allowed senders as
// administrators enter them: single addresses (192.0.2.7, 2001:db8::1),
// networks (192.0.2.0/24) and host names (proxy.example.org), whose
// addresses are looked up and kept for a few minutes.
package netlist

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"time"
)

var hostRe = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// MaxEntries limits a list.
const MaxEntries = 100

// Parse validates and normalizes a list (empty entries are dropped).
func Parse(list []string) ([]string, error) {
	out := []string{}
	for _, raw := range list {
		n := strings.TrimSpace(raw)
		if n == "" {
			continue
		}
		switch {
		case strings.Contains(n, "/"):
			p, err := netip.ParsePrefix(n)
			if err != nil {
				return nil, fmt.Errorf("%q is not a valid network", n)
			}
			n = p.Masked().String()
		default:
			if a, err := netip.ParseAddr(n); err == nil {
				n = a.Unmap().String()
			} else if len(n) <= 253 && hostRe.MatchString(n) {
				n = strings.ToLower(n)
			} else {
				return nil, fmt.Errorf("%q is neither an address, a network nor a host name", n)
			}
		}
		out = append(out, n)
	}
	if len(out) > MaxEntries {
		return nil, fmt.Errorf("at most %d entries", MaxEntries)
	}
	return out, nil
}

type cached struct {
	addrs []netip.Addr
	at    time.Time
}

var (
	mu    sync.Mutex
	hosts = map[string]cached{}
	// Lookup resolves host names (tests replace it).
	Lookup = func(ctx context.Context, host string) ([]string, error) {
		return net.DefaultResolver.LookupHost(ctx, host)
	}
	ttl = 5 * time.Minute
)

func resolve(ctx context.Context, host string) []netip.Addr {
	mu.Lock()
	c, ok := hosts[host]
	mu.Unlock()
	if ok && time.Since(c.at) < ttl {
		return c.addrs
	}
	lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	list, err := Lookup(lctx, host)
	var addrs []netip.Addr
	for _, s := range list {
		if a, err := netip.ParseAddr(s); err == nil {
			addrs = append(addrs, a.Unmap())
		}
	}
	if err != nil && ok {
		return c.addrs // keep the last answer while the resolver is away
	}
	mu.Lock()
	hosts[host] = cached{addrs, time.Now()}
	mu.Unlock()
	return addrs
}

// Allowed reports whether ip is in the list. An empty list allows nobody;
// callers decide whether "no list" means "everybody".
func Allowed(ctx context.Context, list []string, ip string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, n := range list {
		switch {
		case strings.Contains(n, "/"):
			if p, err := netip.ParsePrefix(n); err == nil && p.Contains(a) {
				return true
			}
		default:
			if b, err := netip.ParseAddr(n); err == nil {
				if b.Unmap() == a {
					return true
				}
				continue
			}
			for _, b := range resolve(ctx, n) {
				if b == a {
					return true
				}
			}
		}
	}
	return false
}
