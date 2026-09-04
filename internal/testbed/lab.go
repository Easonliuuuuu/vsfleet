// Package testbed owns the connectable, loopback-only VSFleet lab.
//
// This package is deliberately separate from internal/demo: demo is an
// in-memory presentation backend, while Lab starts real govmomi simulator
// endpoints and real proxy listeners so the production session, transport,
// credential, and assessment paths can be exercised safely.
package testbed

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const (
	FixtureUsername      = "operator@vsphere.test"
	FixturePassword      = "vsfleet-lab"
	FixtureProxyUser     = "svc-edge"
	FixtureProxyPassword = "proxy-lab"

	defaultRoot = ".vsfleet-testbed"
	marker      = "vsfleet-connected-testbed-v1\n"
)

// Options controls the local lab. Every listener is bound to loopback.
type Options struct {
	Root     string
	PortBase int
}

// Lab is a running set of local vCenter simulators and route proxies.
type Lab struct {
	Root         string
	ConfigPath   string
	HistoryPath  string
	ManifestPath string

	contexts []*config.Context
	models   []*simulator.Model
	servers  []*simulator.Server
	proxies  []closer

	mu     sync.Mutex
	closed bool
}

type closer interface{ Close() error }

type endpoint struct {
	Name       string `json:"name"`
	Endpoint   string `json:"endpoint"`
	Route      string `json:"route"`
	Proxy      string `json:"proxy,omitempty"`
	Thumbprint string `json:"thumbprint"`
	Configured bool   `json:"configured"`
}

// Start creates the connected lab and its isolated configuration. Existing
// user-added contexts and history are preserved when Root is reused.
func Start(ctx context.Context, opts Options) (*Lab, error) {
	if opts.Root == "" {
		if root := strings.TrimSpace(os.Getenv("VSFLEET_TESTBED_ROOT")); root != "" {
			opts.Root = root
		} else {
			opts.Root = defaultRoot
		}
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve testbed root: %w", err)
	}
	if opts.PortBase == 0 {
		opts.PortBase = 18443
	}
	if opts.PortBase < 1024 || opts.PortBase > 60000 {
		return nil, fmt.Errorf("port base %d must be between 1024 and 60000", opts.PortBase)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create testbed root: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "MARKER"), []byte(marker), 0o600); err != nil {
		return nil, fmt.Errorf("write testbed marker: %w", err)
	}

	lab := &Lab{
		Root:         root,
		ConfigPath:   filepath.Join(root, "config.toml"),
		HistoryPath:  filepath.Join(root, "history.db"),
		ManifestPath: filepath.Join(root, "endpoints.json"),
	}
	cleanup := func(err error) (*Lab, error) {
		_ = lab.Close(context.Background())
		return nil, err
	}

	prod, err := lab.startVCenter("prod-vc", opts.PortBase, modelConfig{Datacenter: 2, Cluster: 2, ClusterHost: 4, Pool: 2, Machine: 12, Datastore: 3, Portgroup: 3})
	if err != nil {
		return cleanup(err)
	}
	edge, err := lab.startVCenter("edge-vc", opts.PortBase+1, modelConfig{Datacenter: 1, Cluster: 2, ClusterHost: 2, Pool: 2, Machine: 12, Datastore: 2, Portgroup: 3})
	if err != nil {
		return cleanup(err)
	}
	branch, err := lab.startVCenter("branch-vc", opts.PortBase+2, modelConfig{Datacenter: 1, Cluster: 1, ClusterHost: 3, Pool: 2, Machine: 12, Datastore: 2, Portgroup: 2})
	if err != nil {
		return cleanup(err)
	}
	qa, err := lab.startVCenter("qa-vc", opts.PortBase+3, modelConfig{Datacenter: 1, Cluster: 2, ClusterHost: 1, Pool: 1, Machine: 10, Datastore: 2, Portgroup: 2})
	if err != nil {
		return cleanup(err)
	}
	archive, err := lab.startVCenter("archive-vc", opts.PortBase+4, modelConfig{Datacenter: 1, Cluster: 1, ClusterHost: 1, Pool: 1, Machine: 12, Datastore: 1, Portgroup: 1})
	if err != nil {
		return cleanup(err)
	}

	socks, err := newSOCKSProxy(net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.PortBase+100)), map[string]string{
		"edge.vsfleet.test": edge.address,
	}, FixtureProxyUser, FixtureProxyPassword)
	if err != nil {
		return cleanup(fmt.Errorf("start SOCKS5 proxy: %w", err))
	}
	lab.proxies = append(lab.proxies, socks)
	httpProxy, err := newHTTPProxy(net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.PortBase+101)), map[string]string{
		"branch.vsfleet.test": branch.address,
	}, FixtureProxyUser, FixtureProxyPassword)
	if err != nil {
		return cleanup(fmt.Errorf("start HTTP proxy: %w", err))
	}
	lab.proxies = append(lab.proxies, httpProxy)

	lab.contexts = []*config.Context{
		newContext("prod-vc", prod.endpoint, prod.thumbprint, config.TransportConfig{Type: config.TransportDirect}),
		newContext("edge-vc", "https://edge.vsfleet.test:"+strconv.Itoa(opts.PortBase+1), edge.thumbprint, config.TransportConfig{Type: config.TransportSOCKS5, Address: socks.Address(), RemoteDNS: true, Username: FixtureProxyUser, Credential: credentials.Ref{Scheme: credentials.SchemePrompt}}),
		newContext("branch-vc", "https://branch.vsfleet.test:"+strconv.Itoa(opts.PortBase+2), branch.thumbprint, config.TransportConfig{Type: config.TransportHTTPProxy, Address: httpProxy.Address(), Username: FixtureProxyUser, Credential: credentials.Ref{Scheme: credentials.SchemePrompt}}),
		newContext("dr-site", "https://dr.vsfleet.test:"+strconv.Itoa(opts.PortBase+5), "", config.TransportConfig{Type: config.TransportHTTPProxy, Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.PortBase+102))}),
	}
	addable := []endpoint{
		{Name: "prod-vc", Endpoint: prod.endpoint, Route: "Direct", Thumbprint: prod.thumbprint, Configured: true},
		{Name: "edge-vc", Endpoint: "https://edge.vsfleet.test:" + strconv.Itoa(opts.PortBase+1), Route: "SOCKS5 (remote DNS)", Proxy: socks.Address(), Thumbprint: edge.thumbprint, Configured: true},
		{Name: "branch-vc", Endpoint: "https://branch.vsfleet.test:" + strconv.Itoa(opts.PortBase+2), Route: "HTTP CONNECT", Proxy: httpProxy.Address(), Thumbprint: branch.thumbprint, Configured: true},
		{Name: "dr-site", Endpoint: "https://dr.vsfleet.test:" + strconv.Itoa(opts.PortBase+5), Route: "HTTP CONNECT (offline)", Proxy: net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.PortBase+102)), Configured: true},
		{Name: "qa-vc", Endpoint: qa.endpoint, Route: "Direct", Thumbprint: qa.thumbprint},
		{Name: "archive-vc", Endpoint: archive.endpoint, Route: "Direct", Thumbprint: archive.thumbprint},
	}
	if err := lab.writeManifest(addable); err != nil {
		return cleanup(err)
	}
	if err := lab.ensureConfig(); err != nil {
		return cleanup(err)
	}
	if err := lab.seedHistory(ctx); err != nil {
		return cleanup(err)
	}
	return lab, nil
}

type modelConfig struct{ Datacenter, Cluster, ClusterHost, Pool, Machine, Datastore, Portgroup int }
type runningVCenter struct{ endpoint, address, thumbprint string }

func (l *Lab) startVCenter(name string, port int, cfg modelConfig) (runningVCenter, error) {
	m := simulator.VPX()
	m.Datacenter, m.Cluster, m.ClusterHost = cfg.Datacenter, cfg.Cluster, cfg.ClusterHost
	m.Pool, m.Machine, m.Datastore, m.Portgroup = cfg.Pool, cfg.Machine, cfg.Datastore, cfg.Portgroup
	if err := m.Create(); err != nil {
		return runningVCenter{}, fmt.Errorf("create %s simulator: %w", name, err)
	}
	l.models = append(l.models, m)
	m.Service.TLS = new(tls.Config)
	m.Service.Listen = &url.URL{Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), User: url.UserPassword(FixtureUsername, FixturePassword)}
	s := m.Service.NewServer()
	l.servers = append(l.servers, s)
	cert := s.Certificate()
	return runningVCenter{endpoint: "https://" + s.URL.Host, address: s.URL.Host, thumbprint: vsphere.ThumbprintSHA256(cert)}, nil
}

func newContext(name, endpoint, thumbprint string, route config.TransportConfig) *config.Context {
	cc := &config.Context{Name: name, Endpoint: endpoint, Username: FixtureUsername, Credential: credentials.Ref{Scheme: credentials.SchemePrompt}, Transport: route, TLS: config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: thumbprint}}
	if thumbprint == "" {
		cc.TLS = config.TLSConfig{Mode: config.TLSInsecure}
	}
	cc.Normalize()
	return cc
}

func (l *Lab) ensureConfig() error {
	cfg, err := config.Load(l.ConfigPath)
	if err != nil {
		return err
	}
	cfg.SetPath(l.ConfigPath)
	changed := false
	for _, want := range l.contexts {
		if _, err := cfg.Context(want.Name); errors.Is(err, config.ErrNotFound) {
			if err := cfg.Add(want, false); err != nil {
				return err
			}
			changed = true
		} else if err != nil && !strings.Contains(err.Error(), "no contexts configured") {
			return err
		}
	}
	if cfg.CurrentContext == "" {
		cfg.CurrentContext = "prod-vc"
		changed = true
	}
	if changed {
		return cfg.Save()
	}
	return nil
}

func (l *Lab) writeManifest(endpoints []endpoint) error {
	b, err := json.MarshalIndent(struct {
		Username      string     `json:"username"`
		ProxyUsername string     `json:"proxy_username"`
		Endpoints     []endpoint `json:"endpoints"`
	}{FixtureUsername, FixtureProxyUser, endpoints}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.ManifestPath, append(b, '\n'), 0o600)
}

// Close stops proxies, simulator HTTP servers, and simulator models. It is
// safe to call more than once.
func (l *Lab) Close(_ context.Context) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	proxies, servers, models := l.proxies, l.servers, l.models
	l.mu.Unlock()
	for _, p := range proxies {
		_ = p.Close()
	}
	for _, s := range servers {
		s.Close()
	}
	for _, m := range models {
		m.Remove()
	}
	return nil
}

// Contexts returns the built-in contexts in display order.
func (l *Lab) Contexts() []*config.Context { return l.contexts }

// Paths returns the isolated files used by the lab.
func (l *Lab) Paths() (configPath, historyPath, manifestPath string) {
	return l.ConfigPath, l.HistoryPath, l.ManifestPath
}

// SeededHistory reports whether the lab has an assessment database with data.
func (l *Lab) SeededHistory(ctx context.Context) (bool, error) {
	s, err := assessment.Open(l.HistoryPath)
	if err != nil {
		return false, err
	}
	defer s.Close()
	runs, err := s.Runs(ctx)
	return len(runs) > 0, err
}

// NewSeedManager returns a manager that can authenticate to the fixture
// endpoints without opening the TUI prompt. It is intended only for seeding.
func NewSeedManager() *session.Manager {
	keyring := credentials.NewStatic(credentials.SchemeKeyring, map[string]credentials.Credential{})
	prompt := credentials.NewStatic(credentials.SchemePrompt, map[string]credentials.Credential{})
	resolver := credentials.NewResolver(keyring, prompt)
	m := session.New(resolver)
	m.ConnectOptions.Credential = &credentials.Credential{Username: FixtureUsername, Password: FixturePassword}
	return m
}
