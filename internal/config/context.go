package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/easonliuuuuu/vcfleet/internal/credentials"
)

// Transport types.
const (
	TransportDirect     = "direct"
	TransportSOCKS5     = "socks5"
	TransportHTTPProxy  = "http"
	TransportHTTPSProxy = "https"
)

// TLS verification modes.
const (
	TLSSystem     = "system"     // verify against the system trust store
	TLSThumbprint = "thumbprint" // pin one certificate fingerprint
	TLSInsecure   = "insecure"   // verification explicitly disabled
)

// TransportConfig describes how to reach a vCenter's network endpoint. It is
// per-context on purpose: one process routes some vCenters directly and others
// through a proxy — SOCKS5, HTTP CONNECT, or HTTP CONNECT over TLS — at the
// same time.
type TransportConfig struct {
	Type string `toml:"type" json:"type"`
	// Address is the proxy's own address, host:port. Unused for direct.
	Address string `toml:"address,omitempty" json:"address,omitempty"`
	// RemoteDNS resolves the vCenter hostname at the proxy rather than
	// locally. Only meaningful for socks5: an HTTP CONNECT proxy always
	// resolves the destination itself, since the protocol has no concept of
	// asking it to do otherwise.
	RemoteDNS bool `toml:"remote_dns,omitempty" json:"remote_dns,omitempty"`
	// Username and Credential authenticate to the proxy itself, if it asks.
	// An empty Username means the proxy needs no authentication.
	Username   string          `toml:"username,omitempty" json:"username,omitempty"`
	Credential credentials.Ref `toml:"credential,omitempty" json:"credential,omitempty"`
}

// Describe renders the route in one short line for status output.
func (t TransportConfig) Describe() string {
	switch t.Type {
	case TransportSOCKS5:
		s := "SOCKS5 -> " + t.Address
		if t.RemoteDNS {
			s += " (remote DNS)"
		}
		return s
	case TransportHTTPProxy:
		return "HTTP proxy -> " + t.Address
	case TransportHTTPSProxy:
		return "HTTPS proxy -> " + t.Address
	default:
		return "Direct"
	}
}

func (t TransportConfig) validate() error {
	switch t.Type {
	case TransportDirect, "":
		if t.Address != "" {
			return fmt.Errorf("transport.address is only meaningful for a proxy route")
		}
	case TransportSOCKS5, TransportHTTPProxy, TransportHTTPSProxy:
		if t.Address == "" {
			return fmt.Errorf("transport.address is required for %s, e.g. 127.0.0.1:1080", t.Type)
		}
		host, port, err := net.SplitHostPort(t.Address)
		if err != nil || host == "" || port == "" {
			return fmt.Errorf("transport.address %q must be host:port", t.Address)
		}
	default:
		return fmt.Errorf("unknown transport type %q (supported: direct, socks5, http, https)", t.Type)
	}
	return nil
}

// TLSConfig describes how the vCenter certificate is verified. There is
// deliberately no bare "insecure = true": disabling verification is a named
// mode an operator has to choose.
type TLSConfig struct {
	Mode string `toml:"mode" json:"mode"`
	// Thumbprint is the expected certificate fingerprint, SHA-256 or SHA-1,
	// colon separated (AB:CD:...). Only used in thumbprint mode.
	Thumbprint string `toml:"thumbprint,omitempty" json:"thumbprint,omitempty"`
}

// Describe renders the TLS policy for status output.
func (t TLSConfig) Describe() string {
	switch t.Mode {
	case TLSThumbprint:
		return "Pinned thumbprint"
	case TLSInsecure:
		return "Verification disabled"
	default:
		return "System trust store"
	}
}

func (t TLSConfig) validate() error {
	switch t.Mode {
	case TLSSystem, "":
	case TLSInsecure:
	case TLSThumbprint:
		if NormalizeThumbprint(t.Thumbprint) == "" {
			return fmt.Errorf("tls.thumbprint is required when tls.mode is thumbprint")
		}
	default:
		return fmt.Errorf("unknown tls mode %q (supported: system, thumbprint, insecure)", t.Mode)
	}
	return nil
}

// NormalizeThumbprint upper-cases a fingerprint and rewrites it in the
// colon-separated form vCenter itself uses, so that thumbprints copied from
// browsers, govc or the vSphere UI all compare equal.
func NormalizeThumbprint(s string) string {
	var hex strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'F':
			hex.WriteRune(r)
		case r >= 'a' && r <= 'f':
			hex.WriteRune(r - 'a' + 'A')
		case r == ':' || r == ' ' || r == '-':
		default:
			return ""
		}
	}
	h := hex.String()
	if len(h) == 0 || len(h)%2 != 0 {
		return ""
	}
	parts := make([]string, 0, len(h)/2)
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// Context is one vCenter and everything needed to reach it: where it is, who
// to log in as, how to route there, and how to verify its certificate.
type Context struct {
	Name       string          `toml:"name" json:"name"`
	Endpoint   string          `toml:"endpoint" json:"endpoint"`
	Username   string          `toml:"username" json:"username"`
	Credential credentials.Ref `toml:"credential,omitempty" json:"credential,omitempty"`
	// Datacenter is an optional default for inventory queries.
	Datacenter string          `toml:"datacenter,omitempty" json:"datacenter,omitempty"`
	Transport  TransportConfig `toml:"transport" json:"transport"`
	TLS        TLSConfig       `toml:"tls" json:"tls"`
}

// URL returns the vCenter SDK URL for the context.
func (c *Context) URL() (*url.URL, error) {
	raw := strings.TrimSpace(c.Endpoint)
	if raw == "" {
		return nil, fmt.Errorf("context %q: endpoint is empty", c.Name)
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("context %q: invalid endpoint %q: %w", c.Name, c.Endpoint, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("context %q: endpoint %q has no host", c.Name, c.Endpoint)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("context %q: endpoint scheme %q must be http or https", c.Name, u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/sdk"
	}
	// Credentials are never carried in the URL; they are attached at login.
	u.User = nil
	return u, nil
}

// Host returns the endpoint host without a port.
func (c *Context) Host() string {
	u, err := c.URL()
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Address returns the host:port to connect to, defaulting the port by scheme.
func (c *Context) Address() (string, error) {
	u, err := c.URL()
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

// Validate checks that a context is internally consistent. It does not touch
// the network.
func (c *Context) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("context name is required")
	}
	if strings.ContainsAny(c.Name, " \t/\\") {
		return fmt.Errorf("context name %q must not contain spaces or slashes", c.Name)
	}
	if _, err := c.URL(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Username) == "" {
		return fmt.Errorf("context %q: username is required", c.Name)
	}
	if err := c.Transport.validate(); err != nil {
		return fmt.Errorf("context %q: %w", c.Name, err)
	}
	if err := c.TLS.validate(); err != nil {
		return fmt.Errorf("context %q: %w", c.Name, err)
	}
	return nil
}

// Normalize fills in defaults so that a context written by hand behaves the
// same as one produced by "vcfleet context add".
func (c *Context) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Username = strings.TrimSpace(c.Username)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	if c.Transport.Type == "" {
		c.Transport.Type = TransportDirect
	}
	if c.TLS.Mode == "" {
		c.TLS.Mode = TLSSystem
	}
	if c.TLS.Mode == TLSThumbprint {
		c.TLS.Thumbprint = NormalizeThumbprint(c.TLS.Thumbprint)
	}
	if u, err := c.URL(); err == nil {
		e := *u
		e.Path = strings.TrimSuffix(e.Path, "/sdk")
		c.Endpoint = strings.TrimSuffix(e.String(), "/")
	}
}
