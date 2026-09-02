// Package transport builds the network path to a vCenter. Every context gets
// its own dialer, so one process can talk to a directly reachable vCenter and
// to another that only exists behind a SOCKS5 proxy at the same time.
package transport

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
)

// DefaultDialTimeout bounds a single TCP connect.
const DefaultDialTimeout = 15 * time.Second

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
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return DefaultDialTimeout
}

// New builds the dialer described by a transport configuration.
func New(ctx context.Context, cfg config.TransportConfig, opts Options) (Dialer, error) {
	switch cfg.Type {
	case config.TransportDirect, "":
		return NewDirect(opts.timeout()), nil
	case config.TransportSOCKS5:
		return NewSOCKS5(ctx, cfg, opts)
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
