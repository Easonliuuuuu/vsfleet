// Package transport builds the network path to a vCenter. Every context gets
// its own dialer, so one process can talk to a directly reachable vCenter and
// to another that only exists behind a SOCKS5 proxy at the same time.
package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/easonliuuuuu/vcfleet/internal/config"
	"github.com/easonliuuuuu/vcfleet/internal/credentials"
)

// DefaultDialTimeout bounds a single TCP connect.
const DefaultDialTimeout = 15 * time.Second

// ErrProxyAuth marks a dial failure as the proxy rejecting the credentials it
// was given — a SOCKS5 RFC 1929 refusal or an HTTP CONNECT 407 — as opposed to
// the proxy being unreachable or refusing to route to the destination for any
// other reason. Diagnostics use it to report a bad proxy password under its
// own name instead of the generic "could not connect" stage.
var ErrProxyAuth = errors.New("proxy rejected the configured credentials")

// Dialer opens connections towards a vCenter. It is the single seam that keeps
// routing decisions out of the vSphere client.
type Dialer interface {
	// DialContext connects to address, which is always host:port and may be a
	// hostname that only resolves on the far side of the route.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// Describe renders the route for status output.
	Describe() string
}

// Options tune dialer construction.
type Options struct {
	// Timeout bounds a single connect attempt. Zero means DefaultDialTimeout.
	Timeout time.Duration
	// Resolver looks up credentials for proxies that require authentication.
	Resolver *credentials.Resolver
	// ProxyCredential, when set, is used as-is for proxy authentication and
	// the resolver is not consulted. This is the same bootstrapping escape
	// hatch vsphere.ConnectOptions.Credential gives the vCenter's own
	// password: testing a proxy password before it has been stored anywhere
	// needs a way to supply it that isn't "read it back from where it was
	// just written."
	ProxyCredential *credentials.Credential
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultDialTimeout
}

// ResolveProxyCredential looks up a proxy's password, the shared logic every
// authenticated proxy dialer needs. An empty Username means the proxy wants
// no authentication at all, which is the common case and returns immediately.
//
// It is also exported for a caller that needs the same password more than
// once in a single pass — Diagnose resolves it here and threads it through
// Options.ProxyCredential to the dialer it builds for its own stage checks
// and, separately, the one Connect builds internally for the Authentication
// stage, so an operator using a prompt-scheme proxy credential is asked once
// per diagnosis, not once per dialer it happens to build along the way.
func ResolveProxyCredential(ctx context.Context, cfg config.TransportConfig, opts Options) (password string, err error) {
	if cfg.Username == "" {
		return "", nil
	}
	if opts.ProxyCredential != nil {
		return opts.ProxyCredential.Password, nil
	}
	if cfg.Credential.IsZero() {
		return "", nil
	}
	if opts.Resolver == nil {
		return "", fmt.Errorf("%s proxy credential %s configured but no credential resolver available", cfg.Type, cfg.Credential)
	}
	c, err := opts.Resolver.Get(ctx, cfg.Credential)
	if err != nil {
		return "", fmt.Errorf("resolve %s proxy credential %s: %w", cfg.Type, cfg.Credential, err)
	}
	return c.Password, nil
}

// New builds the dialer described by a transport configuration.
func New(ctx context.Context, cfg config.TransportConfig, opts Options) (Dialer, error) {
	switch cfg.Type {
	case config.TransportDirect, "":
		return NewDirect(opts.timeout()), nil
	case config.TransportSOCKS5:
		return NewSOCKS5(ctx, cfg, opts)
	case config.TransportHTTPProxy:
		return NewHTTPProxy(ctx, cfg, opts, false)
	case config.TransportHTTPSProxy:
		return NewHTTPProxy(ctx, cfg, opts, true)
	default:
		return nil, fmt.Errorf("unknown transport type %q", cfg.Type)
	}
}

// HTTPTransport returns an *http.Transport that routes through d. Everything
// else matches the standard library defaults.
func HTTPTransport(d Dialer) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = d.DialContext
	// A per-context proxy is configured explicitly; the ambient HTTP_PROXY
	// environment must not silently re-route a context.
	t.Proxy = nil
	return t
}
