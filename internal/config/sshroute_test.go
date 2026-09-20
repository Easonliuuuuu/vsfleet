package config_test

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

const routeBlock = `
[ssh]
vm_user = "devops"

[[ssh.routes]]
context = "lab"
cidr = "172.31.7.5/24"
type = "HTTP"
proxy_address = "100.109.21.17:8080"
`

func TestLoadSSHRoutesNormalizes(t *testing.T) {
	cfg, err := config.Load(write(t, sample+routeBlock))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SSH.Routes) != 1 {
		t.Fatalf("routes = %+v", cfg.SSH.Routes)
	}
	want := config.SSHRoute{Context: "lab", CIDR: "172.31.7.0/24", Type: "http", ProxyAddress: "100.109.21.17:8080"}
	if got := cfg.SSH.Routes[0]; got != want {
		t.Errorf("route = %+v, want %+v", got, want)
	}
}

func TestSSHRoutesSurviveSave(t *testing.T) {
	cfg, err := config.Load(write(t, sample+routeBlock))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	path := t.TempDir() + "/config.toml"
	cfg.SetPath(path)
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(again.SSH.Routes) != 1 || again.SSH.Routes[0] != cfg.SSH.Routes[0] {
		t.Errorf("routes after round trip = %+v", again.SSH.Routes)
	}
}

func TestSSHRouteValidation(t *testing.T) {
	route := func(body string) string { return sample + "\n[[ssh.routes]]\n" + body }
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"empty context":       {route("cidr = \"10.0.0.0/8\"\ntype = \"direct\"\n"), "context is required"},
		"unknown context":     {route("context = \"nope\"\ncidr = \"10.0.0.0/8\"\ntype = \"direct\"\n"), "does not name a configured context"},
		"malformed cidr":      {route("context = \"lab\"\ncidr = \"10.0.0.0/33\"\ntype = \"direct\"\n"), "not a valid CIDR"},
		"bare address":        {route("context = \"lab\"\ncidr = \"10.0.0.1\"\ntype = \"direct\"\n"), "not a valid CIDR"},
		"unknown type":        {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"ftp\"\n"), "unknown type"},
		"https unsupported":   {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"https\"\nproxy_address = \"p:1\"\n"), "not supported for SSH"},
		"proxy without addr":  {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"socks5\"\n"), "proxy_address is required"},
		"direct with addr":    {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"direct\"\nproxy_address = \"p:1\"\n"), "only meaningful for a proxy"},
		"malformed host:port": {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"http\"\nproxy_address = \"proxy\"\n"), "must be host:port"},
		"option-shaped addr":  {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"http\"\nproxy_address = \"-x evil:22\"\n"), "must be host:port"},
		"whitespace addr":     {route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"http\"\nproxy_address = \"a b:22\"\n"), "must be host:port"},
		"conflicting duplicates": {
			route("context = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"direct\"\n") +
				"\n[[ssh.routes]]\ncontext = \"lab\"\ncidr = \"10.1.2.3/8\"\ntype = \"http\"\nproxy_address = \"p:1\"\n",
			"conflicts with an earlier route",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(write(t, tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestSSHRouteAllowsAgreeingDuplicates(t *testing.T) {
	body := sample + strings.Repeat("\n[[ssh.routes]]\ncontext = \"lab\"\ncidr = \"10.0.0.0/8\"\ntype = \"direct\"\n", 2)
	if _, err := config.Load(write(t, body)); err != nil {
		t.Fatalf("identical duplicate routes should be accepted: %v", err)
	}
}

func TestResolveSSHRoute(t *testing.T) {
	http := config.SSHRoute{Context: "a", CIDR: "172.31.0.0/16", Type: "http", ProxyAddress: "wide:1"}
	narrow := config.SSHRoute{Context: "a", CIDR: "172.31.7.0/24", Type: "socks5", ProxyAddress: "narrow:1"}
	direct := config.SSHRoute{Context: "a", CIDR: "172.31.7.128/25", Type: "direct"}
	other := config.SSHRoute{Context: "b", CIDR: "172.31.7.0/24", Type: "http", ProxyAddress: "b:1"}
	routes := []config.SSHRoute{http, narrow, direct, other}

	for name, tc := range map[string]struct {
		context, addr string
		ok            bool
		typ, proxy    string
	}{
		"longest prefix beats shorter":  {"a", "172.31.7.5", true, "socks5", "narrow:1"},
		"longer direct beats proxy":     {"a", "172.31.7.200", true, "direct", ""},
		"shorter matches elsewhere":     {"a", "172.31.9.9", true, "http", "wide:1"},
		"isolated by context":           {"b", "172.31.7.5", true, "http", "b:1"},
		"context without rules":         {"c", "172.31.7.5", false, "", ""},
		"outside every cidr":            {"a", "10.20.0.11", false, "", ""},
		"hostname is never resolved":    {"a", "vm01.corp.local", false, "", ""},
		"empty target":                  {"a", "", false, "", ""},
		"ipv4-mapped ipv6 still routes": {"a", "::ffff:172.31.7.5", true, "socks5", "narrow:1"},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := config.ResolveSSHRoute(routes, tc.context, tc.addr)
			if ok != tc.ok || got.Type != tc.typ || got.Address != tc.proxy {
				t.Errorf("ResolveSSHRoute(%q, %q) = %+v, %v", tc.context, tc.addr, got, ok)
			}
			if got.Username != "" || got.Credential != (config.TransportConfig{}).Credential {
				t.Errorf("a route must never carry credentials: %+v", got)
			}
		})
	}
}

func TestResolveSSHRouteIPv6AndTies(t *testing.T) {
	routes := []config.SSHRoute{
		{Context: "a", CIDR: "fd00::/8", Type: "socks5", ProxyAddress: "first:1"},
		{Context: "a", CIDR: "fd00::/8", Type: "socks5", ProxyAddress: "second:1"},
	}
	got, ok := config.ResolveSSHRoute(routes, "a", "fd00::5")
	if !ok || got.Address != "first:1" {
		t.Errorf("tie should go to the earlier route, got %+v, %v", got, ok)
	}
}
