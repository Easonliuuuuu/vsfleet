package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vcfleet/internal/config"
	"github.com/easonliuuuuu/vcfleet/internal/credentials"
)

const sample = `
version = 1
current_context = "lab"

[[contexts]]
name = "lab"
endpoint = "https://vcsa.lab.local"
username = "administrator@vsphere.local"
credential = "keyring:lab"

[contexts.transport]
type = "direct"

[contexts.tls]
mode = "thumbprint"
thumbprint = "ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01"

[[contexts]]
name = "customer-a"
endpoint = "vcsa.customer-a.internal"
username = "operator@vsphere.local"
credential = "keyring:customer-a"

[contexts.transport]
type = "socks5"
address = "127.0.0.1:1080"
remote_dns = true

[contexts.tls]
mode = "system"
`

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoad(t *testing.T) {
	cfg, err := config.Load(write(t, sample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(cfg.Contexts))
	}

	lab, err := cfg.Context("lab")
	if err != nil {
		t.Fatalf("Context(lab): %v", err)
	}
	if lab.Credential.Scheme != credentials.SchemeKeyring || lab.Credential.Value != "lab" {
		t.Errorf("credential parsed as %+v", lab.Credential)
	}
	// A thumbprint copied in lower case has to compare equal to the same
	// fingerprint copied out of the vSphere UI.
	if !strings.HasPrefix(lab.TLS.Thumbprint, "AB:CD:EF:") {
		t.Errorf("thumbprint was not normalised: %q", lab.TLS.Thumbprint)
	}

	// An endpoint written without a scheme still yields an https SDK URL.
	customer, _ := cfg.Context("customer-a")
	if customer.Endpoint != "https://vcsa.customer-a.internal" {
		t.Errorf("endpoint normalised to %q", customer.Endpoint)
	}
	u, err := customer.URL()
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if u.Path != "/sdk" {
		t.Errorf("SDK path is %q", u.Path)
	}
	addr, err := customer.Address()
	if err != nil {
		t.Fatalf("Address: %v", err)
	}
	if addr != "vcsa.customer-a.internal:443" {
		t.Errorf("address is %q", addr)
	}
	if !customer.Transport.RemoteDNS {
		t.Error("remote_dns was not read")
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Contexts) != 0 {
		t.Fatalf("expected no contexts, got %d", len(cfg.Contexts))
	}
	if _, err := cfg.Context(""); err == nil {
		t.Error("expected an error naming the setup command")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cc := &config.Context{
		Name:       "prod",
		Endpoint:   "vcsa.prod.internal",
		Username:   "svc@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "prod"},
		Transport:  config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", RemoteDNS: true},
		TLS:        config.TLSConfig{Mode: config.TLSInsecure},
	}
	if err := cfg.Add(cc, false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cfg.CurrentContext = "prod"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// The file names credential references, but a mistake here would be
	// expensive, so the permissions are asserted. Windows has no POSIX mode
	// bits — Chmod there only toggles the read-only attribute and Stat reports
	// 0666 for any writable file — so the check runs where it means something.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("config permissions are %v, want 0600", perm)
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "password") {
		t.Errorf("configuration contains a password field:\n%s", body)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := reloaded.Context("prod")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if got.Transport.Address != "127.0.0.1:1080" || !got.Transport.RemoteDNS {
		t.Errorf("transport did not round-trip: %+v", got.Transport)
	}
	if got.Credential.String() != "keyring:prod" {
		t.Errorf("credential did not round-trip: %q", got.Credential.String())
	}
	if reloaded.CurrentContext != "prod" {
		t.Errorf("current context did not round-trip: %q", reloaded.CurrentContext)
	}
}

func TestAddRejectsDuplicates(t *testing.T) {
	cfg, _ := config.Load(filepath.Join(t.TempDir(), "c.toml"))
	cc := &config.Context{Name: "a", Endpoint: "vc.local", Username: "u"}
	if err := cfg.Add(cc, false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := cfg.Add(&config.Context{Name: "a", Endpoint: "other.local", Username: "u"}, false); err == nil {
		t.Fatal("expected a duplicate name to be rejected")
	}
	if err := cfg.Add(&config.Context{Name: "a", Endpoint: "other.local", Username: "u"}, true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, _ := cfg.Context("a")
	if got.Endpoint != "https://other.local" {
		t.Errorf("replace did not take effect: %q", got.Endpoint)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		ctx  config.Context
		want string
	}{
		{"no name", config.Context{Endpoint: "vc.local", Username: "u"}, "name is required"},
		{"no username", config.Context{Name: "a", Endpoint: "vc.local"}, "username is required"},
		{"bad scheme", config.Context{Name: "a", Endpoint: "ftp://vc.local", Username: "u"}, "must be http or https"},
		{"socks without address", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			Transport: config.TransportConfig{Type: config.TransportSOCKS5}}, "address is required"},
		{"socks without port", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			Transport: config.TransportConfig{Type: config.TransportSOCKS5, Address: "proxy"}}, "must be host:port"},
		{"http proxy without address", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			Transport: config.TransportConfig{Type: config.TransportHTTPProxy}}, "address is required"},
		{"https proxy without address", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			Transport: config.TransportConfig{Type: config.TransportHTTPSProxy}}, "address is required"},
		{"unknown transport", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			Transport: config.TransportConfig{Type: "wireguard"}}, "unknown transport"},
		{"thumbprint without value", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			TLS: config.TLSConfig{Mode: config.TLSThumbprint}}, "tls.thumbprint is required"},
		{"unknown tls mode", config.Context{Name: "a", Endpoint: "vc.local", Username: "u",
			TLS: config.TLSConfig{Mode: "maybe"}}, "unknown tls mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ctx.Validate()
			if err == nil {
				t.Fatalf("expected an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error is %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	cfg, err := config.Load(write(t, sample))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	all, err := cfg.Resolve(nil, true)
	if err != nil || len(all) != 2 {
		t.Fatalf("Resolve all: %v, %d contexts", err, len(all))
	}
	one, err := cfg.Resolve(nil, false)
	if err != nil || len(one) != 1 || one[0].Name != "lab" {
		t.Fatalf("Resolve default: %v, %+v", err, one)
	}
	named, err := cfg.Resolve([]string{"customer-a"}, false)
	if err != nil || len(named) != 1 || named[0].Name != "customer-a" {
		t.Fatalf("Resolve named: %v, %+v", err, named)
	}
	if _, err := cfg.Resolve([]string{"absent"}, false); err == nil {
		t.Error("expected an unknown context to be an error")
	}
}

func TestNormalizeThumbprint(t *testing.T) {
	cases := map[string]string{
		"ab:cd:ef":         "AB:CD:EF",
		"ABCDEF":           "AB:CD:EF",
		"ab cd ef":         "AB:CD:EF",
		"AB-CD-EF":         "AB:CD:EF",
		"AB:CD:E":          "",
		"not a thumbprint": "",
		"":                 "",
	}
	for in, want := range cases {
		if got := config.NormalizeThumbprint(in); got != want {
			t.Errorf("NormalizeThumbprint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRejectsFutureVersion(t *testing.T) {
	_, err := config.Load(write(t, "version = 99\n"))
	if err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("expected a version error, got %v", err)
	}
}
