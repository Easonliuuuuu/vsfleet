package tests

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// connectRequest records one CONNECT an httpProxyServer received.
type connectRequest struct {
	Target string
	Auth   string // the raw Proxy-Authorization header value, empty if none was sent
}

// httpProxyServer is a minimal HTTP CONNECT proxy, with optional Basic auth
// and optional TLS on the listener itself, used to prove vcfleet's http and
// https proxy routes without a real bastion in the test environment.
type httpProxyServer struct {
	// RequireAuth, when non-empty, is the exact "Basic ..." value a client
	// must present; anything else gets a 407.
	RequireAuth string

	listener net.Listener
	mu       sync.Mutex
	requests []connectRequest
}

func startHTTPProxy(t *testing.T, requireAuth string, tlsConfig *tls.Config) *httpProxyServer {
	t.Helper()
	var ln net.Listener
	var err error
	if tlsConfig != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &httpProxyServer{RequireAuth: requireAuth, listener: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// Address is the proxy's host:port.
func (s *httpProxyServer) Address() string { return s.listener.Addr().String() }

// Requests returns every CONNECT the proxy handled.
func (s *httpProxyServer) Requests() []connectRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]connectRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *httpProxyServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			_ = s.handle(conn)
		}()
	}
}

func (s *httpProxyServer) handle(conn net.Conn) error {
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return err
	}
	auth := req.Header.Get("Proxy-Authorization")
	s.mu.Lock()
	s.requests = append(s.requests, connectRequest{Target: req.Host, Auth: auth})
	s.mu.Unlock()

	if s.RequireAuth != "" && auth != s.RequireAuth {
		_, err := conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
		return err
	}
	upstream, err := net.Dial("tcp", req.Host)
	if err != nil {
		_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return err
	}
	defer upstream.Close()
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	// br, not conn, on this side: anything the client already sent that
	// landed in the same read as this handler's own request parsing must
	// still reach the tunnel, the same correctness concern the client-side
	// dialer's own bufConn exists for.
	go func() { defer wg.Done(); _, _ = io.Copy(upstream, br) }()
	go func() { defer wg.Done(); _, _ = io.Copy(conn, upstream) }()
	wg.Wait()
	return nil
}

// selfSignedCert builds an ephemeral certificate for 127.0.0.1 that is valid
// but signed by nobody the system trust store recognises — standing in for
// an https proxy whose certificate has not been vetted, so a test can prove
// it gets rejected rather than silently accepted.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("load key pair: %v", err)
	}
	return cert
}

func TestHTTPProxyUnauthenticated(t *testing.T) {
	vc := startVCenter(t, nil)
	proxy := startHTTPProxy(t, "", nil)

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "via-http-proxy",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "http",
		"--proxy-address", proxy.Address(),
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
	)

	out := r.mustRun(testPassword+"\n", "context", "test", "via-http-proxy")
	if !strings.Contains(out, "Connection successful.") {
		t.Fatalf("connection through the http proxy failed:\n%s", out)
	}
	if !strings.Contains(out, "HTTP proxy") {
		t.Errorf("context test did not report the proxy route:\n%s", out)
	}

	reqs := proxy.Requests()
	if len(reqs) == 0 {
		t.Fatal("the proxy never saw a CONNECT request")
	}
	if reqs[0].Auth != "" {
		t.Errorf("an unauthenticated route should not send Proxy-Authorization, got %q", reqs[0].Auth)
	}

	// Inventory has to work over the tunnel too, not just the handshake.
	vms := r.mustRun(testPassword+"\n", "vm", "list")
	if !strings.Contains(vms, "DC0") {
		t.Errorf("vm list over the http proxy returned nothing:\n%s", vms)
	}
}

func TestHTTPProxyBasicAuth(t *testing.T) {
	vc := startVCenter(t, nil)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("svc-proxy:proxy-secret"))
	proxy := startHTTPProxy(t, want, nil)

	r := newRunner(t)
	r.mustRun(testPassword+"\nproxy-secret\n", "context", "add",
		"--name", "via-authed-proxy",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "http",
		"--proxy-address", proxy.Address(),
		"--proxy-username", "svc-proxy",
		"--proxy-credential", "prompt",
		"--proxy-password-stdin",
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
	)

	out := r.mustRun(testPassword+"\nproxy-secret\n", "context", "test", "via-authed-proxy")
	if !strings.Contains(out, "Connection successful.") {
		t.Fatalf("connection through the authenticated proxy failed:\n%s", out)
	}

	reqs := proxy.Requests()
	if len(reqs) == 0 || reqs[len(reqs)-1].Auth != want {
		t.Errorf("the proxy did not see the expected Proxy-Authorization header: %+v", reqs)
	}
}

func TestHTTPProxyWrongCredentialFails(t *testing.T) {
	vc := startVCenter(t, nil)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("svc-proxy:proxy-secret"))
	proxy := startHTTPProxy(t, want, nil)

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "via-wrong-proxy-password",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "http",
		"--proxy-address", proxy.Address(),
		"--proxy-username", "svc-proxy",
		"--proxy-credential", "prompt",
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\nnot-the-right-password\n", "doctor", "via-wrong-proxy-password")
	if err == nil {
		t.Fatalf("a wrong proxy password must not connect:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Proxy authentication") {
		t.Errorf("a 407 during CONNECT should be named as a proxy authentication failure, not a generic connection failure:\n%s", stdout)
	}
	if !strings.Contains(stdout, "407") {
		t.Errorf("the failure should mention the proxy's 407, got:\n%s", stdout)
	}
}

func TestHTTPProxyOffline(t *testing.T) {
	vc := startVCenter(t, nil)
	proxy := startHTTPProxy(t, "", nil)
	address := proxy.Address()
	proxy.listener.Close()

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "via-offline-http-proxy",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "http",
		"--proxy-address", address,
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "doctor", "via-offline-http-proxy")
	if err == nil {
		t.Fatalf("expected the offline proxy to fail the diagnosis:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Proxy reachable") || !strings.Contains(stdout, "unreachable") {
		t.Errorf("doctor did not name the proxy as the fault:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not reached") {
		t.Errorf("later stages were not marked as skipped:\n%s", stdout)
	}
}

// TestHTTPSProxyOffline covers the https route's other failure mode: no
// certificate is ever presented because nothing answers the TCP connection
// at all. The address is reused from a listener that has already been
// closed, so this needs no TLS configuration on the (nonexistent) far end.
func TestHTTPSProxyOffline(t *testing.T) {
	vc := startVCenter(t, nil)
	proxy := startHTTPProxy(t, "", nil)
	address := proxy.Address()
	proxy.listener.Close()

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "via-offline-https-proxy",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "https",
		"--proxy-address", address,
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "doctor", "via-offline-https-proxy")
	if err == nil {
		t.Fatalf("expected the offline https proxy to fail the diagnosis:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Proxy reachable") || !strings.Contains(stdout, "unreachable") {
		t.Errorf("doctor did not name the proxy as the fault:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not reached") {
		t.Errorf("later stages were not marked as skipped:\n%s", stdout)
	}
}

// TestHTTPSProxyRejectsUntrustedCertificate proves the security property
// that matters most for an https route: vcfleet does not silently accept
// whatever certificate a proxy happens to present. Pinning a proxy's own
// certificate, the way a vCenter's can be pinned, is not implemented yet —
// this is the same "reject what the system trust store does not vouch for"
// behaviour a plain https:// endpoint gets, applied to the proxy hop.
func TestHTTPSProxyRejectsUntrustedCertificate(t *testing.T) {
	vc := startVCenter(t, nil)
	cert := selfSignedCert(t)
	proxy := startHTTPProxy(t, "", &tls.Config{Certificates: []tls.Certificate{cert}})

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "via-https-proxy",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "https",
		"--proxy-address", proxy.Address(),
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "doctor", "via-https-proxy")
	if err == nil {
		t.Fatalf("an untrusted proxy certificate must not be accepted silently:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Proxy reachable") {
		t.Errorf("the failure should be attributed to reaching the proxy:\n%s", stdout)
	}
	if !strings.Contains(stdout, "HTTPS proxy") {
		t.Errorf("the route should be reported as HTTPS proxy:\n%s", stdout)
	}
}
