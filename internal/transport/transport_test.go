package transport_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vcfleet/internal/config"
	"github.com/easonliuuuuu/vcfleet/internal/transport"
)

func TestNewDirect(t *testing.T) {
	d, err := transport.New(context.Background(), config.TransportConfig{Type: config.TransportDirect}, transport.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.Describe() != "Direct" {
		t.Errorf("Describe() = %q", d.Describe())
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	conn.Close()
}

func TestNewSOCKS5Describe(t *testing.T) {
	cfg := config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080", RemoteDNS: true}
	d, err := transport.New(context.Background(), cfg, transport.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !strings.Contains(d.Describe(), "127.0.0.1:1080") || !strings.Contains(d.Describe(), "remote DNS") {
		t.Errorf("Describe() = %q", d.Describe())
	}
}

func TestUnknownTransport(t *testing.T) {
	_, err := transport.New(context.Background(), config.TransportConfig{Type: "tailscale"}, transport.Options{})
	if err == nil || !strings.Contains(err.Error(), "tailscale") {
		t.Fatalf("expected the unknown type to be named, got %v", err)
	}
}

func TestSOCKS5ProbeReportsOfflineProxy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := ln.Addr().String()
	ln.Close()

	cfg := config.TransportConfig{Type: config.TransportSOCKS5, Address: address}
	d, err := transport.NewSOCKS5(context.Background(), cfg, transport.Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewSOCKS5: %v", err)
	}
	if err := d.Probe(context.Background()); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected an unreachable proxy, got %v", err)
	}
}

// TestHTTPTransportIgnoresAmbientProxy is the reason routing is per context:
// an environment variable must not silently re-route a vCenter that is
// configured to be reached directly.
func TestHTTPTransportIgnoresAmbientProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	ht := transport.HTTPTransport(transport.NewDirect(time.Second))
	if ht.Proxy != nil {
		t.Fatal("the HTTP transport should not consult the environment for a proxy")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://vcsa.example.internal/sdk", nil)
	_ = req
}
