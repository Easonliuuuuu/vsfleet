// Package tests drives vctui end to end against a simulated vCenter and a real
// SOCKS5 proxy, so that the parts of the design that are hard to reason about
// — per-context routing, remote DNS, certificate pinning and failure isolation
// — are proven without access to physical VMware infrastructure.
package tests

import (
	"bytes"
	"context"
	"crypto/tls"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vc-tui/internal/cli"
	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

const testPassword = "correct-horse"

// vcenter is a simulated vCenter with the details a context needs.
type vcenter struct {
	URL        string
	Host       string
	Port       string
	Address    string
	Thumbprint string
}

func startVCenter(t *testing.T, tune func(*simulator.Model)) *vcenter {
	t.Helper()
	model := simulator.VPX()
	if tune != nil {
		tune(model)
	}
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)
	// vcsim serves plain HTTP unless a TLS configuration is set. A real
	// vCenter is always TLS, and the certificate policy is what these tests
	// need to exercise.
	model.Service.TLS = new(tls.Config)
	server := model.Service.NewServer()
	t.Cleanup(server.Close)

	u := *server.URL
	u.User = nil
	u.Path = ""

	vc := &vcenter{URL: u.String(), Host: u.Hostname(), Port: u.Port(), Address: u.Host}
	cc := &config.Context{Name: "probe", Endpoint: vc.URL, Username: "u", TLS: config.TLSConfig{Mode: config.TLSInsecure}}
	cc.Normalize()
	sha256, _, _, _, err := vsphere.FetchThumbprint(context.Background(), cc, vsphere.ConnectOptions{})
	if err != nil {
		t.Fatalf("fetch simulator thumbprint: %v", err)
	}
	vc.Thumbprint = sha256
	return vc
}

// runner invokes vctui the way a shell would, against a throwaway config file.
type runner struct {
	t          *testing.T
	configPath string
}

func newRunner(t *testing.T) *runner {
	t.Helper()
	return &runner{t: t, configPath: filepath.Join(t.TempDir(), "config.toml")}
}

// run executes one command. stdin feeds interactive prompts, which is how the
// tests supply passwords without touching the operating system keyring.
func (r *runner) run(stdin string, args ...string) (stdout, stderr string, err error) {
	r.t.Helper()
	var out, errOut bytes.Buffer
	app := &cli.App{
		In:  strings.NewReader(stdin),
		Out: &out,
		Err: &errOut,
	}
	root := cli.NewRootCommand(app)
	root.SetArgs(append([]string{"--config", r.configPath}, args...))
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// mustRun fails the test if the command does not succeed.
func (r *runner) mustRun(stdin string, args ...string) string {
	r.t.Helper()
	stdout, stderr, err := r.run(stdin, args...)
	if err != nil {
		r.t.Fatalf("vctui %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

// addContext registers a direct, thumbprint-pinned context.
func (r *runner) addContext(name string, vc *vcenter, extra ...string) {
	r.t.Helper()
	args := []string{
		"context", "add",
		"--name", name,
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
	}
	r.mustRun(testPassword+"\n"+testPassword+"\n", append(args, extra...)...)
}

func TestContextLifecycleAndInventory(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Machine = 3
	})
	r := newRunner(t)
	r.addContext("lab", vc)

	list := r.mustRun("", "context", "list")
	if !strings.Contains(list, "lab") {
		t.Fatalf("context list did not show the context:\n%s", list)
	}
	if !strings.Contains(list, "Direct") {
		t.Errorf("context list did not show the route:\n%s", list)
	}

	test := r.mustRun(testPassword+"\n", "context", "test", "lab")
	for _, want := range []string{"Context: lab", "Connection successful."} {
		if !strings.Contains(test, want) {
			t.Errorf("context test output is missing %q:\n%s", want, test)
		}
	}

	vms := r.mustRun(testPassword+"\n", "vm", "list")
	if !strings.Contains(vms, "poweredOn") {
		t.Errorf("vm list did not show any running VM:\n%s", vms)
	}

	hosts := r.mustRun(testPassword+"\n", "host", "list")
	if !strings.Contains(hosts, "connected") {
		t.Errorf("host list did not show a connected host:\n%s", hosts)
	}

	stores := r.mustRun(testPassword+"\n", "datastore", "list")
	if !strings.Contains(stores, "LocalDS_0") {
		t.Errorf("datastore list did not show the simulated datastore:\n%s", stores)
	}

	doctor := r.mustRun(testPassword+"\n", "doctor", "lab")
	for _, want := range []string{"Configuration valid", "TLS certificate", "Authentication", "API available"} {
		if !strings.Contains(doctor, want) {
			t.Errorf("doctor output is missing the %q stage:\n%s", want, doctor)
		}
	}
}

func TestSearchAcrossVCenters(t *testing.T) {
	a := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 })
	b := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 2 })
	r := newRunner(t)
	r.addContext("alpha", a)
	r.addContext("beta", b)

	stdin := strings.Repeat(testPassword+"\n", 4)
	out := r.mustRun(stdin, "search", "DC0_H0_VM0")
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("search did not cover both vCenters:\n%s", out)
	}
	if !strings.Contains(out, "2 match(es) across 2 vCenter(s)") {
		t.Errorf("search summary is wrong:\n%s", out)
	}

	kinds := r.mustRun(stdin, "search", "DC0", "--kind", "datastore")
	if strings.Contains(kinds, "\tvm\t") || strings.Contains(kinds, " vm ") {
		t.Errorf("--kind datastore returned virtual machines:\n%s", kinds)
	}
}

// TestFailureIsolation is the behaviour that separates this tool from a set of
// per-vCenter scripts: one broken environment costs one line of output.
func TestFailureIsolation(t *testing.T) {
	good := startVCenter(t, nil)
	r := newRunner(t)
	r.addContext("healthy", good)

	// A context pointing at a port nothing is listening on.
	broken := &vcenter{URL: "https://127.0.0.1:1", Thumbprint: good.Thumbprint}
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "unreachable",
		"--endpoint", broken.URL,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "insecure",
		"--no-test",
	)

	stdin := strings.Repeat(testPassword+"\n", 4)
	stdout, stderr, err := r.run(stdin, "search", "DC0", "--all-contexts")
	if err != nil {
		t.Fatalf("search failed outright instead of isolating the broken context: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "healthy") {
		t.Errorf("results from the healthy vCenter were lost:\n%s", stdout)
	}
	if !strings.Contains(stderr, "unreachable") {
		t.Errorf("the broken context was not reported:\n%s", stderr)
	}

	stdout, stderr, _ = r.run(stdin, "status")
	if !strings.Contains(stdout, "connected") {
		t.Errorf("status did not show the healthy vCenter as connected:\n%s", stdout)
	}
	if !strings.Contains(stdout, "failed") {
		t.Errorf("status did not show the broken vCenter as failed:\n%s\n%s", stdout, stderr)
	}
}

// TestUIRefusesWithoutContexts checks the terminal interface is wired into the
// command tree and declines to open on an empty configuration. Anything past
// that point needs a terminal; the interface itself is tested in internal/tui,
// where its model is driven directly.
func TestUIRefusesWithoutContexts(t *testing.T) {
	r := newRunner(t)

	_, _, err := r.run("", "ui")
	if err == nil {
		t.Fatal("vctui ui on an empty configuration should fail")
	}
	if !strings.Contains(err.Error(), "context add") {
		t.Errorf("error should point at the fix, got: %v", err)
	}
}
