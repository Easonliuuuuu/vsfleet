package tests

import (
	"net"
	"strings"
	"testing"
)

// remoteOnlyHost is a name that deliberately does not resolve on the machine
// running the tests. It stands for the internal DNS name of a customer
// vCenter, which is only meaningful on the far side of the proxy.
const remoteOnlyHost = "vcsa.customer-a.internal"

func TestSOCKS5WithRemoteDNS(t *testing.T) {
	vc := startVCenter(t, nil)
	proxy := startSOCKS(t, map[string]string{remoteOnlyHost: vc.Address})

	if _, err := net.LookupHost(remoteOnlyHost); err == nil {
		t.Skipf("%s resolves locally in this environment, so the test cannot prove remote resolution", remoteOnlyHost)
	}

	r := newRunner(t)
	r.mustRun(testPassword+"\n"+testPassword+"\n", "context", "add",
		"--name", "customer-a",
		"--endpoint", "https://"+net.JoinHostPort(remoteOnlyHost, vc.Port),
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "socks5",
		"--proxy-address", proxy.Address(),
		"--remote-dns",
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
	)

	out := r.mustRun(testPassword+"\n", "context", "test", "customer-a")
	if !strings.Contains(out, "Connection successful.") {
		t.Fatalf("connection through the proxy failed:\n%s", out)
	}
	if !strings.Contains(out, "Remote") {
		t.Errorf("context test did not report remote DNS:\n%s", out)
	}
	if !strings.Contains(out, "SOCKS5") {
		t.Errorf("context test did not report the proxy route:\n%s", out)
	}

	// The proxy must have been asked for the hostname, not for an address the
	// client resolved. That difference is the whole reason remote DNS exists.
	var sawDomain bool
	for _, req := range proxy.Requests() {
		if req.Domain() && req.Host == remoteOnlyHost {
			sawDomain = true
		}
	}
	if !sawDomain {
		t.Errorf("the proxy was never asked to resolve %s: %+v", remoteOnlyHost, proxy.Requests())
	}

	// Inventory has to work over the proxy too, not just the handshake.
	vms := r.mustRun(testPassword+"\n", "vm", "list")
	if !strings.Contains(vms, "DC0") {
		t.Errorf("vm list over the proxy returned nothing:\n%s", vms)
	}
}

func TestSOCKS5WithoutRemoteDNSCannotResolve(t *testing.T) {
	vc := startVCenter(t, nil)
	proxy := startSOCKS(t, map[string]string{remoteOnlyHost: vc.Address})

	if _, err := net.LookupHost(remoteOnlyHost); err == nil {
		t.Skipf("%s resolves locally in this environment", remoteOnlyHost)
	}

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "customer-a",
		"--endpoint", "https://"+net.JoinHostPort(remoteOnlyHost, vc.Port),
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "socks5",
		"--proxy-address", proxy.Address(),
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "context", "test", "customer-a")
	if err == nil {
		t.Fatalf("expected local resolution of %s to fail:\n%s", remoteOnlyHost, stdout)
	}
	if !strings.Contains(stdout, "DNS resolution") {
		t.Errorf("the failure was not attributed to DNS:\n%s", stdout)
	}
}

func TestSOCKS5ProxyOffline(t *testing.T) {
	vc := startVCenter(t, nil)
	proxy := startSOCKS(t, nil)
	address := proxy.Address()
	proxy.listener.Close()

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "customer-b",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--transport", "socks5",
		"--proxy-address", address,
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "doctor", "customer-b")
	if err == nil {
		t.Fatalf("expected the offline proxy to fail the diagnosis:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Proxy reachable") || !strings.Contains(stdout, "unreachable") {
		t.Errorf("doctor did not name the proxy as the fault:\n%s", stdout)
	}
	// The stages after the proxy must be reported as not reached rather than
	// as further failures, so the first line of the report is the real cause.
	if !strings.Contains(stdout, "not reached") {
		t.Errorf("later stages were not marked as skipped:\n%s", stdout)
	}
}

func TestTLSThumbprintMismatch(t *testing.T) {
	vc := startVCenter(t, nil)
	wrong := strings.Repeat("AB:", 31) + "AB"

	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "rotated",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "thumbprint",
		"--thumbprint", wrong,
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "context", "test", "rotated")
	if err == nil {
		t.Fatalf("a pinned certificate that does not match must not connect:\n%s", stdout)
	}
	for _, want := range []string{"certificate mismatch", "expected", "received", vc.Thumbprint} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the mismatch report is missing %q:\n%s", want, stdout)
		}
	}
}

func TestTLSSystemRejectsSelfSignedCertificate(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "unverified",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "system",
		"--no-test",
	)

	stdout, _, err := r.run(testPassword+"\n", "context", "test", "unverified")
	if err == nil {
		t.Fatalf("system trust must reject the simulator's self-signed certificate:\n%s", stdout)
	}
	if !strings.Contains(stdout, "TLS certificate") {
		t.Errorf("the failure was not attributed to the certificate:\n%s", stdout)
	}
}

// TestTLSThumbprintDiscovery covers the unattended path: asking for pinning
// without supplying a fingerprint records the one the server presents.
func TestTLSThumbprintDiscovery(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.mustRun(testPassword+"\n"+testPassword+"\n", "context", "add",
		"--name", "pinned",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "thumbprint",
	)

	show := r.mustRun("", "context", "show", "pinned")
	if !strings.Contains(show, vc.Thumbprint) {
		t.Errorf("the discovered thumbprint was not stored:\n%s", show)
	}
}
