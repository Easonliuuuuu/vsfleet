package transport

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

// HTTPProxy routes connections through an HTTP CONNECT proxy — over TLS to
// the proxy itself when tls is set, which is what distinguishes an "https"
// route from an "http" one. Either way, the destination hostname is always
// handed to the proxy in the CONNECT request line: unlike SOCKS5, HTTP
// CONNECT has no concept of resolving locally versus remotely, so there is no
// remote-DNS toggle to make here — the proxy always does the resolution.
type HTTPProxy struct {
	address    string
	tls        bool
	authHeader string // "Basic ...", empty when the proxy needs no auth
	timeout    time.Duration
}

// NewHTTPProxy builds an HTTP CONNECT proxy dialer from a transport
// configuration. tls selects an "https" route: the connection to the proxy
// itself is TLS, verified against the system trust store, before the CONNECT
// request is sent over it.
func NewHTTPProxy(ctx context.Context, cfg config.TransportConfig, opts Options, tlsToProxy bool) (*HTTPProxy, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("%s transport requires an address", cfg.Type)
	}
	p := &HTTPProxy{address: cfg.Address, tls: tlsToProxy, timeout: opts.timeout()}
	if cfg.Username != "" {
		password, err := ResolveProxyCredential(ctx, cfg, opts)
		if err != nil {
			return nil, err
		}
		p.authHeader = "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.Username+":"+password))
	}
	return p, nil
}

func (p *HTTPProxy) scheme() string {
	if p.tls {
		return "https"
	}
	return "http"
}

// dial opens and, for an https route, TLS-wraps the connection to the proxy
// itself. It is shared by DialContext and Probe so both agree on exactly what
// "the proxy is reachable" means.
func (p *HTTPProxy) dial(ctx context.Context) (net.Conn, error) {
	base := &net.Dialer{Timeout: p.timeout, KeepAlive: 30 * time.Second}
	conn, err := base.DialContext(ctx, "tcp", p.address)
	if err != nil {
		return nil, fmt.Errorf("%s proxy %s unreachable: %w", p.scheme(), p.address, err)
	}
	if !p.tls {
		return conn, nil
	}
	host, _, splitErr := net.SplitHostPort(p.address)
	if splitErr != nil {
		host = p.address
	}
	tc := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("tls handshake with proxy %s: %w", p.address, err)
	}
	return tc, nil
}

// DialContext implements Dialer: connect to the proxy, ask it to CONNECT to
// address, and hand back a connection that reads and writes straight through
// the tunnel it opens.
func (p *HTTPProxy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := p.dial(ctx)
	if err != nil {
		return nil, err
	}
	tunnel, err := p.connect(ctx, conn, address)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return tunnel, nil
}

// connect performs the CONNECT handshake on an already-open connection to the
// proxy and returns the connection ready to use as the tunnel — wrapped so
// that any bytes buffered while reading the proxy's response headers, which
// on a fast local proxy can already include the first bytes of whatever rides
// the tunnel, are not silently dropped.
func (p *HTTPProxy) connect(ctx context.Context, conn net.Conn, address string) (net.Conn, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
		defer conn.SetDeadline(time.Time{})
	}
	req := "CONNECT " + address + " HTTP/1.1\r\nHost: " + address + "\r\n"
	if p.authHeader != "" {
		req += "Proxy-Authorization: " + p.authHeader + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, fmt.Errorf("send CONNECT to %s proxy %s: %w", p.scheme(), p.address, err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, fmt.Errorf("read CONNECT response from %s proxy %s: %w", p.scheme(), p.address, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusProxyAuthRequired {
		return nil, fmt.Errorf("%w: %s proxy %s: 407 Proxy Authentication Required", ErrProxyAuth, p.scheme(), p.address)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s proxy %s refused to connect to %s: %s", p.scheme(), p.address, address, resp.Status)
	}
	return &bufConn{Conn: conn, r: br}, nil
}

// bufConn reads through a bufio.Reader that already consumed some bytes off
// the underlying connection while parsing an HTTP response, so those bytes
// are not lost to whoever reads the connection next. Once the buffer is
// empty, bufio.Reader.Read falls through to the underlying connection on its
// own, so this needs no special-casing beyond overriding Read.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// Describe implements Dialer.
func (p *HTTPProxy) Describe() string {
	label := "HTTP proxy"
	if p.tls {
		label = "HTTPS proxy"
	}
	return label + " -> " + p.address
}

// Address returns the configured proxy address, for diagnostic detail.
func (p *HTTPProxy) Address() string { return p.address }

// RemoteDNS always reports true: HTTP CONNECT has no local-resolution mode,
// so the diagnosis and the config's remote_dns field never disagree with it.
func (p *HTTPProxy) RemoteDNS() bool { return true }

// Probe checks that the proxy itself is reachable — and, for an https route,
// that its own TLS handshake completes — separately from anything about the
// vCenter it is asked to tunnel to.
func (p *HTTPProxy) Probe(ctx context.Context) error {
	conn, err := p.dial(ctx)
	if err != nil {
		return err
	}
	return conn.Close()
}
