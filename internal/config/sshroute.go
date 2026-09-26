package config

import (
	"fmt"
	"net/netip"
	"strings"
	"unicode"
)

// SSHRoute sends the TUI's SSH handoff for one destination network through a
// chosen route. It describes how the operator's workstation reaches a VM or
// host — deliberately separate from Context.Transport, which describes how
// vsfleet reaches the vCenter itself. A vCenter can be direct while the guests
// it manages sit in a subnet only a proxy can see.
//
// It has no username or credential: the generated ssh(1) ProxyCommand cannot
// carry one without exposing it on the command line, so authenticated proxies
// stay unsupported rather than being half-supported here.
type SSHRoute struct {
	// Context names the vCenter context the route applies to. A moref's
	// address is only meaningful inside one estate, so a route never spans
	// contexts.
	Context string `toml:"context" json:"context"`
	// CIDR is the destination network, e.g. 172.31.7.0/24.
	CIDR string `toml:"cidr" json:"cidr"`
	// Type is direct, socks5 or http.
	Type string `toml:"type" json:"type"`
	// ProxyAddress is the proxy's host:port. It must be empty for direct.
	ProxyAddress string `toml:"proxy_address,omitempty" json:"proxy_address,omitempty"`
}

// transport reports the route as the TransportConfig the existing ssh
// argument generation already understands, so its safety checks apply
// unchanged.
func (r SSHRoute) transport() TransportConfig {
	return TransportConfig{Type: r.Type, Address: r.ProxyAddress}
}

// Normalize trims the route and rewrites its CIDR to the masked form, so
// 172.31.7.5/24 and 172.31.7.0/24 name the same rule. An unparseable CIDR is
// left as written for validate to report.
func (r *SSHRoute) Normalize() {
	r.Context = strings.TrimSpace(r.Context)
	r.CIDR = strings.TrimSpace(r.CIDR)
	r.Type = strings.ToLower(strings.TrimSpace(r.Type))
	r.ProxyAddress = strings.TrimSpace(r.ProxyAddress)
	if p, err := netip.ParsePrefix(r.CIDR); err == nil {
		r.CIDR = p.Masked().String()
	}
}

// ValidSSHProxyAddress reports whether addr is safe to interpolate into a
// generated ProxyCommand: a plain host:port, never something ssh(1) or nc(1)
// could read as an option, and never split into extra words. It is exported
// so callers that accept a proxy address outside of [[ssh.routes]] — such as
// the TUI's per-VM route override — can apply the same rule.
func ValidSSHProxyAddress(addr string) bool {
	if addr == "" || strings.HasPrefix(addr, "-") {
		return false
	}
	if strings.IndexFunc(addr, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
		return false
	}
	return TransportConfig{Type: TransportSOCKS5, Address: addr}.validate() == nil
}

func (r SSHRoute) validate(i int) error {
	prefix := fmt.Sprintf("ssh.routes[%d]", i)
	if r.Context == "" {
		return fmt.Errorf("%s: context is required", prefix)
	}
	if _, err := netip.ParsePrefix(r.CIDR); err != nil {
		return fmt.Errorf("%s: cidr %q is not a valid CIDR, e.g. 172.31.7.0/24", prefix, r.CIDR)
	}
	switch r.Type {
	case TransportDirect:
		if r.ProxyAddress != "" {
			return fmt.Errorf("%s: proxy_address is only meaningful for a proxy route", prefix)
		}
	case TransportSOCKS5, TransportHTTPProxy:
		if r.ProxyAddress == "" {
			return fmt.Errorf("%s: proxy_address is required for %s, e.g. 127.0.0.1:1080", prefix, r.Type)
		}
		if !ValidSSHProxyAddress(r.ProxyAddress) {
			return fmt.Errorf("%s: proxy_address %q must be host:port", prefix, r.ProxyAddress)
		}
		if err := r.transport().validate(); err != nil {
			return fmt.Errorf("%s: proxy_address %q must be host:port", prefix, r.ProxyAddress)
		}
	case TransportHTTPSProxy:
		return fmt.Errorf("%s: type %q is not supported for SSH: the generated ProxyCommand has no TLS-capable proxy client (supported: direct, socks5, http)", prefix, r.Type)
	default:
		return fmt.Errorf("%s: unknown type %q (supported: direct, socks5, http)", prefix, r.Type)
	}
	return nil
}

// validate checks every route against the set of configured contexts and
// rejects two rules for the same context and network that disagree.
func (s SSHConfig) validate(contexts map[string]bool) error {
	type key struct{ context, cidr string }
	seen := make(map[key]SSHRoute, len(s.Routes))
	for i, r := range s.Routes {
		r.Normalize()
		if err := r.validate(i); err != nil {
			return err
		}
		if !contexts[r.Context] {
			return fmt.Errorf("ssh.routes[%d]: context %q does not name a configured context", i, r.Context)
		}
		k := key{r.Context, r.CIDR}
		if prev, ok := seen[k]; ok && prev != r {
			return fmt.Errorf("ssh.routes[%d]: conflicts with an earlier route for context %q and %s", i, r.Context, r.CIDR)
		}
		seen[k] = r
	}
	return nil
}

// ResolveSSHRoute picks the route for an SSH target in one context: among the
// routes naming that context whose CIDR contains addr, the longest prefix
// wins, and a tie goes to the earlier route. ok is false when nothing
// matches — including when addr is not an IP literal. Only parsed addresses
// are matched: a hostname is never resolved just to choose a route, so
// selection stays deterministic and free of network probes.
//
// The result is a TransportConfig so the caller can reuse the existing ssh
// argument generation and its refusals. A "direct" route yields a direct
// transport, overriding whatever route the context itself uses.
func ResolveSSHRoute(routes []SSHRoute, context, addr string) (TransportConfig, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(addr))
	if err != nil {
		return TransportConfig{}, false
	}
	ip = ip.Unmap()
	best, bestBits := -1, -1
	for i, r := range routes {
		if strings.TrimSpace(r.Context) != context {
			continue
		}
		p, err := netip.ParsePrefix(strings.TrimSpace(r.CIDR))
		if err != nil || !p.Contains(ip) {
			continue
		}
		if p.Bits() > bestBits {
			best, bestBits = i, p.Bits()
		}
	}
	if best < 0 {
		return TransportConfig{}, false
	}
	r := routes[best]
	r.Normalize()
	return r.transport(), true
}
