package transport

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"github.com/easonliuuuuu/vcfleet/internal/config"
)

// SOCKS5 routes connections through a SOCKS5 proxy, optionally resolving the
// destination hostname at the proxy so that names which only exist inside the
// remote network still work.
type SOCKS5 struct {
	address   string
	remoteDNS bool
	timeout   time.Duration
	dialer    proxy.ContextDialer
	resolver  *net.Resolver
}

// NewSOCKS5 builds a SOCKS5 dialer from a transport configuration.
func NewSOCKS5(ctx context.Context, cfg config.TransportConfig, opts Options) (*SOCKS5, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("socks5 transport requires an address")
	}
	var auth *proxy.Auth
	if cfg.Username != "" {
		password, err := ResolveProxyCredential(ctx, cfg, opts)
		if err != nil {
			return nil, err
		}
		auth = &proxy.Auth{User: cfg.Username, Password: password}
	}
	base := &net.Dialer{Timeout: opts.timeout(), KeepAlive: 30 * time.Second}
	d, err := proxy.SOCKS5("tcp", cfg.Address, auth, base)
	if err != nil {
		return nil, fmt.Errorf("configure socks5 proxy %s: %w", cfg.Address, err)
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("socks5 proxy dialer does not support cancellation")
	}
	return &SOCKS5{
		address:   cfg.Address,
		remoteDNS: cfg.RemoteDNS,
		timeout:   opts.timeout(),
		dialer:    cd,
		resolver:  net.DefaultResolver,
	}, nil
}

// Address returns the configured proxy address.
func (s *SOCKS5) Address() string { return s.address }

// RemoteDNS reports whether names are resolved at the proxy.
func (s *SOCKS5) RemoteDNS() bool { return s.remoteDNS }

// DialContext implements Dialer. With remote DNS the hostname is handed to the
// proxy verbatim; otherwise it is resolved here first and an address literal is
// sent, which makes the difference visible on the wire.
func (s *SOCKS5) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	target := address
	if !s.remoteDNS {
		resolved, err := s.resolveLocally(ctx, address)
		if err != nil {
			return nil, err
		}
		target = resolved
	}
	conn, err := s.dialer.DialContext(ctx, network, target)
	if err != nil {
		// golang.org/x/net/proxy has no typed error for a rejected RFC 1929
		// login; this substring is the exact text its internal socks package
		// returns, so it is the only way to tell "the proxy said no to this
		// username/password" apart from every other reason the dial failed.
		if strings.Contains(err.Error(), "authentication failed") {
			return nil, fmt.Errorf("%w: socks5 proxy %s: %v", ErrProxyAuth, s.address, err)
		}
		return nil, fmt.Errorf("connect to %s via socks5 proxy %s: %w", address, s.address, err)
	}
	return conn, nil
}

func (s *SOCKS5) resolveLocally(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid address %q: %w", address, err)
	}
	if net.ParseIP(host) != nil {
		return address, nil
	}
	ips, err := s.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve %s locally (set remote_dns = true to resolve at the proxy): %w", host, err)
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("resolve %s locally: no addresses returned", host)
	}
	return net.JoinHostPort(ips[0].IP.String(), port), nil
}

// Describe implements Dialer.
func (s *SOCKS5) Describe() string {
	if s.remoteDNS {
		return fmt.Sprintf("SOCKS5 -> %s (remote DNS)", s.address)
	}
	return fmt.Sprintf("SOCKS5 -> %s", s.address)
}

// Probe checks that the proxy itself accepts TCP connections. Diagnostics use
// it to separate "the proxy is down" from "the vCenter is down".
func (s *SOCKS5) Probe(ctx context.Context) error {
	d := &net.Dialer{Timeout: s.timeout}
	conn, err := d.DialContext(ctx, "tcp", s.address)
	if err != nil {
		return fmt.Errorf("socks5 proxy %s unreachable: %w", s.address, err)
	}
	return conn.Close()
}
