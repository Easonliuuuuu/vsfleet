package tui

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

func TestVsphereClientURLNeedsInstanceID(t *testing.T) {
	cc := ctx("prod", "https://vcsa.prod.internal")
	if _, ok := vsphereClientURL(cc, "VirtualMachine", "vm-1234", ""); ok {
		t.Fatal("a URL must not be built without the vCenter's server GUID")
	}
	url, ok := vsphereClientURL(cc, "VirtualMachine", "vm-1234", "52-aaaa-bbbb")
	if !ok {
		t.Fatal("expected a URL once the GUID is known")
	}
	for _, want := range []string{"vcsa.prod.internal", "vm-1234", "52-aaaa-bbbb", "VirtualMachine"} {
		if !strings.Contains(url, want) {
			t.Errorf("URL %q missing %q", url, want)
		}
	}
}

func TestVsphereClientURLUnknownMorefKind(t *testing.T) {
	cc := ctx("prod", "https://vcsa.prod.internal")
	if _, ok := vsphereClientURL(cc, "", "vm-1234", "52-aaaa-bbbb"); ok {
		t.Fatal("expected no URL for an unrecognised managed object type")
	}
}

func TestHostClientURL(t *testing.T) {
	if _, ok := hostClientURL(""); ok {
		t.Fatal("expected no URL for an empty address")
	}
	url, ok := hostClientURL("esx-tpe-04.corp.example")
	if !ok || url != "https://esx-tpe-04.corp.example/ui" {
		t.Fatalf("got %q, %v", url, ok)
	}
}

func TestProxyArgsDeclinesAuthenticatedProxy(t *testing.T) {
	tc := config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", Username: "bastion-user"}
	args, reason := proxyArgs(tc)
	if args != nil || reason == "" {
		t.Fatalf("expected an authenticated proxy declined, got args=%v reason=%q", args, reason)
	}
}

func TestProxyArgsSOCKS5(t *testing.T) {
	tc := config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080"}
	args, reason := proxyArgs(tc)
	if reason != "" {
		t.Fatalf("unexpected decline: %q", reason)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-X 5") || !strings.Contains(joined, "127.0.0.1:1080") {
		t.Errorf("expected a SOCKS5 ProxyCommand, got %v", args)
	}
}

func TestProxyArgsHTTPConnect(t *testing.T) {
	tc := config.TransportConfig{Type: config.TransportHTTPProxy, Address: "proxy.internal:3128"}
	args, reason := proxyArgs(tc)
	if reason != "" {
		t.Fatalf("unexpected decline: %q", reason)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-X connect") || !strings.Contains(joined, "proxy.internal:3128") {
		t.Errorf("expected an HTTP CONNECT ProxyCommand, got %v", args)
	}
}

func TestIsDirect(t *testing.T) {
	if !isDirect(config.TransportConfig{}) {
		t.Error("an empty transport type must count as direct")
	}
	if !isDirect(config.TransportConfig{Type: config.TransportDirect}) {
		t.Error("explicit direct must count as direct")
	}
	if isDirect(config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080"}) {
		t.Error("a SOCKS5 route must not count as direct")
	}
}
