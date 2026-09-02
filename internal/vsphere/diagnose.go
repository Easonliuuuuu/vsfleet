package vsphere

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
	"github.com/easonliuuuuu/vc-tui/internal/transport"
)

// CheckStatus is the outcome of one diagnostic stage.
type CheckStatus string

// Diagnostic outcomes.
const (
	CheckPass CheckStatus = "pass"
	CheckFail CheckStatus = "fail"
	CheckSkip CheckStatus = "skip"
)

// Check is one stage of connecting to a vCenter. Splitting the connection into
// named stages is the point: "cannot connect" is useless when the cause could
// be a dead proxy, an expired password or a rotated certificate.
type Check struct {
	Name     string
	Status   CheckStatus
	Detail   string
	Err      error
	Duration time.Duration
}

// Diagnosis is the result of walking every stage for one context.
type Diagnosis struct {
	Context  string
	Endpoint string
	Route    string
	TLS      string
	Checks   []Check
	About    About
	// Thumbprint is the fingerprint the server actually presented.
	Thumbprint string
	// Latency is the login round trip, populated once authentication passes.
	Latency time.Duration
}

// OK reports whether every stage that ran passed.
func (d *Diagnosis) OK() bool {
	for _, c := range d.Checks {
		if c.Status == CheckFail {
			return false
		}
	}
	return true
}

// Err returns the first failure, or nil.
func (d *Diagnosis) Err() error {
	for _, c := range d.Checks {
		if c.Status == CheckFail {
			if c.Err != nil {
				return c.Err
			}
			return fmt.Errorf("%s failed", c.Name)
		}
	}
	return nil
}

// diagRunner walks the stages in order and marks everything after the first
// failure as skipped, so a report shows how far the connection actually got.
type diagRunner struct {
	d      *Diagnosis
	failed bool
}

func (r *diagRunner) run(name string, fn func() (string, error)) {
	if r.failed {
		r.d.Checks = append(r.d.Checks, Check{Name: name, Status: CheckSkip})
		return
	}
	start := time.Now()
	detail, err := fn()
	c := Check{Name: name, Detail: detail, Duration: time.Since(start)}
	if err != nil {
		c.Status = CheckFail
		c.Err = err
		r.failed = true
	} else {
		c.Status = CheckPass
	}
	r.d.Checks = append(r.d.Checks, c)
}

func (r *diagRunner) skip(name, detail string) {
	r.d.Checks = append(r.d.Checks, Check{Name: name, Status: CheckSkip, Detail: detail})
}

// Diagnose walks the whole path to a vCenter one stage at a time and returns
// both the report and, on success, a live client the caller must close.
func Diagnose(ctx context.Context, cc *config.Context, opts ConnectOptions) (*Diagnosis, *Client) {
	d := &Diagnosis{Context: cc.Name, Endpoint: cc.Endpoint, Route: cc.Transport.Describe(), TLS: cc.TLS.Describe()}
	r := &diagRunner{d: d}

	var (
		dialer    transport.Dialer
		cred      credentials.Credential
		proxyCred *credentials.Credential
		addr      string
		client    *Client
	)

	r.run("Configuration valid", func() (string, error) {
		if err := cc.Validate(); err != nil {
			return "", err
		}
		a, err := cc.Address()
		if err != nil {
			return "", err
		}
		addr = a
		return cc.Endpoint, nil
	})

	r.run("Credential available", func() (string, error) {
		c, err := resolveCredential(ctx, cc, opts)
		if err != nil {
			return "", err
		}
		cred = c
		src := cc.Credential.String()
		if src == "" {
			src = "prompt"
		}
		return src, nil
	})

	r.run("Route configured", func() (string, error) {
		// Resolved here, once, and threaded through as an override from this
		// point on — including into the dialer Connect builds internally for
		// the Authentication stage below — so a prompt-scheme proxy
		// credential is asked for once per diagnosis, not once per dialer.
		proxyCred = opts.ProxyCredential
		if proxyCred == nil && cc.Transport.Username != "" {
			pw, err := transport.ResolveProxyCredential(ctx, cc.Transport, transport.Options{Resolver: opts.Resolver})
			if err != nil {
				return "", err
			}
			proxyCred = &credentials.Credential{Password: pw}
		}
		dl, err := transport.New(ctx, cc.Transport, transport.Options{
			Timeout:         opts.DialTimeout,
			Resolver:        opts.Resolver,
			ProxyCredential: proxyCred,
		})
		if err != nil {
			return "", err
		}
		dialer = dl
		d.Route = dl.Describe()
		return dl.Describe(), nil
	})

	// proxied and remoteResolver are satisfied by every proxy dialer —
	// SOCKS5, HTTP and HTTPS alike — so this stage neither knows nor cares
	// which kind of proxy it is looking at.
	type proxied interface {
		Probe(ctx context.Context) error
		Address() string
	}
	if px, ok := dialer.(proxied); ok {
		r.run("Proxy reachable", func() (string, error) {
			if err := px.Probe(ctx); err != nil {
				return "", err
			}
			return px.Address(), nil
		})
	} else {
		r.skip("Proxy reachable", "not using a proxy")
	}

	type remoteResolver interface {
		RemoteDNS() bool
	}
	if rr, ok := dialer.(remoteResolver); ok && rr.RemoteDNS() {
		r.skip("DNS resolution", "resolved at the proxy")
	} else {
		r.run("DNS resolution", func() (string, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return "", err
			}
			if ip := net.ParseIP(host); ip != nil {
				return host + " (address literal)", nil
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return "", fmt.Errorf("resolve %s: %w", host, err)
			}
			return fmt.Sprintf("%s -> %s", host, ips[0].IP), nil
		})
	}

	r.run("TCP connection", func() (string, error) {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return "", err
		}
		defer conn.Close()
		return addr, nil
	})

	if u, _ := cc.URL(); u != nil && u.Scheme == "http" {
		r.skip("TLS certificate", "endpoint is plain http")
	} else {
		r.run("TLS certificate", func() (string, error) {
			cert, err := fetchCertificate(ctx, cc, dialer, addr, false)
			if err != nil {
				return "", err
			}
			d.Thumbprint = ThumbprintSHA256(cert)
			expiry := "valid until " + cert.NotAfter.Format(time.DateOnly)
			if cn := cert.Subject.CommonName; cn != "" {
				return cn + ", " + expiry, nil
			}
			return expiry, nil
		})
	}

	r.run("Authentication", func() (string, error) {
		co := opts
		co.Credential = &cred
		co.ProxyCredential = proxyCred
		c, err := Connect(ctx, cc, co)
		if err != nil {
			return "", err
		}
		client = c
		d.Latency = c.Latency
		d.About = c.About
		return cc.Username, nil
	})

	r.run("API available", func() (string, error) {
		latency, err := client.Ping(ctx)
		if err != nil {
			return "", err
		}
		d.Latency = latency
		return d.About.FullVersion(), nil
	})

	if !d.OK() && client != nil {
		_ = client.Close(context.WithoutCancel(ctx))
		client = nil
	}
	return d, client
}

// fetchCertificate performs a TLS handshake and returns the leaf certificate.
// When trustAny is set the context's TLS policy is bypassed, which is how
// "vctui context add" discovers a thumbprint to pin.
func fetchCertificate(ctx context.Context, cc *config.Context, dialer transport.Dialer, addr string, trustAny bool) (*x509.Certificate, error) {
	var cfg *tls.Config
	if trustAny {
		cfg = &tls.Config{ServerName: cc.Host(), InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}
	} else {
		c, err := TLSConfig(cc)
		if err != nil {
			return nil, err
		}
		cfg = c
	}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	state := tc.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("%s presented no certificate", cc.Host())
	}
	return state.PeerCertificates[0], nil
}

// FetchThumbprint connects to a context's endpoint without verifying anything
// and reports the certificate it presents, so an operator can inspect a
// fingerprint before choosing to pin it.
func FetchThumbprint(ctx context.Context, cc *config.Context, opts ConnectOptions) (sha256, sha1, subject string, notAfter time.Time, err error) {
	dialer, err := transport.New(ctx, cc.Transport, transport.Options{
		Timeout:         opts.DialTimeout,
		Resolver:        opts.Resolver,
		ProxyCredential: opts.ProxyCredential,
	})
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	addr, err := cc.Address()
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	cert, err := fetchCertificate(ctx, cc, dialer, addr, true)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	return ThumbprintSHA256(cert), ThumbprintSHA1(cert), cert.Subject.CommonName, cert.NotAfter, nil
}
