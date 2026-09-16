package tests

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/mo"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// datastoreTestClient opens a second, direct connection to the same
// in-process simulator a CLI test already started, so the test can inspect
// and seed inventory the CLI command under test will then read back. It is
// deliberately independent of the runner: it exercises the same read-only
// vsphere.Client the CLI itself uses, never a parallel implementation.
func datastoreTestClient(t *testing.T, vc *vcenter) *vsphere.Client {
	t.Helper()
	u, err := url.Parse(vc.URL)
	if err != nil {
		t.Fatalf("parse simulator URL: %v", err)
	}
	u.Path = "/sdk"
	u.User = url.UserPassword("prober", testPassword)
	gc, err := govmomi.NewClient(context.Background(), u, true)
	if err != nil {
		t.Fatalf("connect to simulator: %v", err)
	}
	t.Cleanup(func() { _ = gc.Logout(context.Background()) })
	cc := &config.Context{Name: "probe", Endpoint: vc.URL, Username: "prober", TLS: config.TLSConfig{Mode: config.TLSInsecure}}
	cc.Normalize()
	return vsphere.NewClientForTest(cc, gc)
}

// datastoreDirectory resolves the physical directory backing the simulator's
// default datastore, so a test can write a file exactly where the browser
// will find it.
func datastoreDirectory(t *testing.T, client *vsphere.Client) string {
	t.Helper()
	ctx := context.Background()
	finder := find.NewFinder(client.VIM(), false)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatalf("find simulator datacenter: %v", err)
	}
	finder.SetDatacenter(dc)
	ds, err := finder.DefaultDatastore(ctx)
	if err != nil {
		t.Fatalf("find simulator datastore: %v", err)
	}
	var props mo.Datastore
	if err := ds.Properties(ctx, ds.Reference(), []string{"summary"}, &props); err != nil {
		t.Fatalf("read simulator datastore path: %v", err)
	}
	return props.Summary.Url
}

// seedDatastoreFile writes a real file at a canonical "[datastore] relative"
// path, using the case-preserving split so the file lands exactly where the
// backing path an existing VM's disk names.
func seedDatastoreFile(t *testing.T, baseDir, canonicalPath string) {
	t.Helper()
	_, relative, ok := vsphere.SplitBrowsePath(canonicalPath)
	if !ok {
		t.Fatalf("not a canonical datastore path: %q", canonicalPath)
	}
	full := filepath.Join(baseDir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("create datastore fixture directory: %v", err)
	}
	if err := os.WriteFile(full, []byte("synthetic vmdk"), 0o600); err != nil {
		t.Fatalf("write datastore fixture: %v", err)
	}
}

func TestDatastoreFilesListRootAndSubdirectory(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 1
		m.Machine = 1
		m.Datastore = 1
	})
	probe := datastoreTestClient(t, vc)
	stores, err := probe.ListDatastores(context.Background())
	if err != nil || len(stores) == 0 {
		t.Fatalf("probe ListDatastores: %v (%d stores)", err, len(stores))
	}
	ds := stores[0]
	baseDir := datastoreDirectory(t, probe)
	if err := os.MkdirAll(filepath.Join(baseDir, "notes"), 0o700); err != nil {
		t.Fatalf("seed subdirectory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "notes", "readme.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	r := newRunner(t)
	r.addContext("lab", vc)

	root := r.mustRun(testPassword+"\n", "datastore", "files", "list", ds.Name)
	if !strings.Contains(root, "notes") {
		t.Fatalf("root listing did not show the seeded directory:\n%s", root)
	}
	if strings.Contains(root, "readme.txt") {
		t.Fatalf("root listing recursed into a subdirectory:\n%s", root)
	}

	sub := r.mustRun(testPassword+"\n", "datastore", "files", "list", ds.Name, "notes")
	if !strings.Contains(sub, "readme.txt") {
		t.Fatalf("subdirectory listing did not show the seeded file:\n%s", sub)
	}

	rootJSON := r.mustRun(testPassword+"\n", "datastore", "files", "list", ds.Name, "-o", "json")
	for _, want := range []string{`"context": "lab"`, `"datastore": "` + ds.Name + `"`, `"path": ""`, `"truncated": false`} {
		if !strings.Contains(rootJSON, want) {
			t.Errorf("JSON listing is missing %q:\n%s", want, rootJSON)
		}
	}
}

func TestDatastoreFilesFindIsRecursiveAndReportsLiveReferences(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 1
		m.Machine = 1
		m.Datastore = 1
	})
	probe := datastoreTestClient(t, vc)
	ctx := context.Background()
	stores, err := probe.ListDatastores(ctx)
	if err != nil || len(stores) == 0 {
		t.Fatalf("probe ListDatastores: %v (%d stores)", err, len(stores))
	}
	ds := stores[0]
	refs, err := probe.ListDatastoreVMReferences(ctx, ds.ID)
	if err != nil || len(refs.References) == 0 {
		t.Fatalf("probe ListDatastoreVMReferences: %v (%d refs)", err, len(refs.References))
	}
	ref := refs.References[0]
	baseDir := datastoreDirectory(t, probe)
	seedDatastoreFile(t, baseDir, ref.BackingPath)

	r := newRunner(t)
	r.addContext("lab", vc)

	found := r.mustRun(testPassword+"\n", "datastore", "files", "find", ds.Name, "*.vmdk")
	if !strings.Contains(found, "referenced by "+ref.VMName) {
		t.Fatalf("find did not report the live VM reference for %q:\n%s", ref.VMName, found)
	}

	foundJSON := r.mustRun(testPassword+"\n", "datastore", "files", "find", ds.Name, "*.vmdk", "-o", "json")
	for _, want := range []string{`"vm": "` + ref.VMName + `"`, `"backing_path": "` + ref.BackingPath + `"`} {
		if !strings.Contains(foundJSON, want) {
			t.Errorf("JSON find output is missing %q:\n%s", want, foundJSON)
		}
	}
}

func TestDatastoreFilesFindLimitAndTypeFilter(t *testing.T) {
	vc := startVCenter(t, func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 1
		m.Machine = 1
		m.Datastore = 1
	})
	probe := datastoreTestClient(t, vc)
	stores, err := probe.ListDatastores(context.Background())
	if err != nil || len(stores) == 0 {
		t.Fatalf("probe ListDatastores: %v (%d stores)", err, len(stores))
	}
	ds := stores[0]
	baseDir := datastoreDirectory(t, probe)
	for _, name := range []string{"one.log", "two.log", "three.log"} {
		if err := os.WriteFile(filepath.Join(baseDir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "some.log-dir"), 0o700); err != nil {
		t.Fatalf("seed directory: %v", err)
	}

	r := newRunner(t)
	r.addContext("lab", vc)

	limited := r.mustRun(testPassword+"\n", "datastore", "files", "find", ds.Name, "*.log*", "--limit", "1")
	if !strings.Contains(limited, "showing the first 1 match") {
		t.Errorf("limited find did not report the limit:\n%s", limited)
	}

	filesOnly := r.mustRun(testPassword+"\n", "datastore", "files", "find", ds.Name, "*.log*", "--type", "file")
	if strings.Contains(filesOnly, "some.log-dir") {
		t.Errorf("--type file returned a directory:\n%s", filesOnly)
	}

	dirsOnly := r.mustRun(testPassword+"\n", "datastore", "files", "find", ds.Name, "*.log*", "--type", "directory")
	if strings.Contains(dirsOnly, "one.log") {
		t.Errorf("--type directory returned a file:\n%s", dirsOnly)
	}
	if !strings.Contains(dirsOnly, "some.log-dir") {
		t.Errorf("--type directory did not return the seeded directory:\n%s", dirsOnly)
	}

	_, _, err = r.run(testPassword+"\n", "datastore", "files", "find", ds.Name, "*", "--type", "bogus")
	if err == nil {
		t.Fatal("an unknown --type value should be rejected")
	}
}

func TestDatastoreFilesListMissingDatastoreFails(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)
	r.addContext("lab", vc)

	_, _, err := r.run(testPassword+"\n", "datastore", "files", "list", "does-not-exist")
	if err == nil {
		t.Fatal("listing a nonexistent datastore should fail")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error does not name the missing datastore: %v", err)
	}
}

func TestDatastoreFilesListPermissionDenied(t *testing.T) {
	vc := startVCenter(t, nil)
	vc.Service.FaultInjector().AddRule(&simulator.FaultInjectionRule{
		MethodName:  "SearchDatastore_Task",
		ObjectType:  "*",
		ObjectName:  "*",
		Probability: 1,
		FaultType:   simulator.FaultTypeNoPermission,
		Message:     "Datastore.Browse denied",
		Enabled:     true,
	})
	probe := datastoreTestClient(t, vc)
	stores, err := probe.ListDatastores(context.Background())
	if err != nil || len(stores) == 0 {
		t.Fatalf("probe ListDatastores: %v (%d stores)", err, len(stores))
	}

	r := newRunner(t)
	r.addContext("lab", vc)
	_, stderr, err := r.run(testPassword+"\n", "datastore", "files", "list", stores[0].Name)
	if err == nil {
		t.Fatal("a denied browse should fail the command")
	}
	if !strings.Contains(err.Error(), "denied") && !strings.Contains(stderr, "denied") {
		t.Errorf("permission failure was not surfaced: err=%v stderr=%s", err, stderr)
	}
}

// Two vCenters that both use the simulator's default datastore name collide
// on purpose: the resolver must refuse to guess which one an operator meant
// rather than silently browsing whichever it found first.
func TestDatastoreFilesAmbiguousNameAcrossContextsIsRefused(t *testing.T) {
	a := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 1 })
	b := startVCenter(t, func(m *simulator.Model) { m.Datacenter = 1; m.Machine = 1 })
	r := newRunner(t)
	r.addContext("alpha", a)
	r.addContext("beta", b)

	probe := datastoreTestClient(t, a)
	stores, err := probe.ListDatastores(context.Background())
	if err != nil || len(stores) == 0 {
		t.Fatalf("probe ListDatastores: %v (%d stores)", err, len(stores))
	}

	stdin := strings.Repeat(testPassword+"\n", 4)
	stdout, stderr, err := r.run(stdin, "datastore", "files", "list", stores[0].Name, "--all-contexts")
	if err == nil {
		t.Fatalf("an ambiguous datastore name should be refused, got:\n%s", stdout)
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error does not say the name is ambiguous: %v", err)
	}
	if !strings.Contains(stderr, "alpha") || !strings.Contains(stderr, "beta") {
		t.Errorf("candidate list did not name both contexts:\n%s", stderr)
	}
}
