package tests

import (
	"bytes"
	"encoding/xml"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/vmware/govmomi/simulator"
)

// This file is the standing proof that vsfleet is read-only against a real
// vCenter. Reading the source says the same thing — nothing imports
// govmomi/object, where every mutating operation lives — but source reading
// is not evidence anyone can re-run before pointing this at production. This
// records every SOAP operation the tool actually puts on the wire during a
// full exercise of every command that talks to vCenter, and fails on
// anything outside a small, explicitly justified allowlist.
//
// The allowlist is deliberately exhaustive rather than a deny-list of known
// dangerous verbs: a new call has to be justified here before it can ship,
// which is the property that actually protects an operator's estate.

// readOnlyMethods are the vSphere operations vsfleet is allowed to invoke.
//
// Four of these are not literal reads, and each is here for a stated reason:
//
//   - Login/Logout create and release a session. Nothing else can be read
//     without one, and Logout is what keeps abandoned sessions from
//     lingering for half an hour on someone else's vCenter.
//   - CreateContainerView/DestroyView create and destroy a transient,
//     session-scoped view object — the standard govmomi way to enumerate
//     inventory. A view holds references to objects; creating one changes
//     nothing about the objects themselves, and destroying it is the cleanup
//     of the thing this tool just created, never of anything in inventory.
//
// Everything else is a pure read.
//
// The first seven are what a full run actually sends today. The last three
// are the same paged read against a vCenter large enough to split the
// results, which vcsim's dataset is not; they are approved in advance
// because a real estate will reach them and nothing about them differs from
// the single-page read.
//
// Nothing else belongs here without a specific reason. In particular
// RetrieveManagedMethodExecuter is deliberately absent: it is a mechanism
// for invoking arbitrary operations, so approving it would defeat the point
// of the list.
var readOnlyMethods = map[string]string{
	"RetrieveServiceContent":       "read: the service's own capability document, on connect",
	"Login":                        "session: nothing is readable without one",
	"Logout":                       "session: release it rather than leaving it to time out",
	"SessionIsActive":              "read: is this session still valid (the doctor/status ping)",
	"CreateContainerView":          "transient view object this tool creates for its own enumeration",
	"DestroyView":                  "cleanup of that same view; never touches inventory",
	"RetrievePropertiesEx":         "read: the property collector, how all inventory is enumerated",
	"ContinueRetrievePropertiesEx": "read: the next page of that same enumeration",
	"CancelRetrievePropertiesEx":   "read path: abandons a paged enumeration",
	"RetrieveProperties":           "read: the pre-6.0 property collector call",
}

// soapRecorder collects the operation name of every SOAP request that
// reaches the simulator.
type soapRecorder struct {
	mu      sync.Mutex
	methods []string
}

func (r *soapRecorder) record(method string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = append(r.methods, method)
}

// distinct returns the set of operations seen, sorted, for a stable report.
func (r *soapRecorder) distinct() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for _, m := range r.methods {
		seen[m] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// soapMethod pulls the operation name out of a SOAP envelope: the first
// element inside <Body>. Reading it off the wire rather than instrumenting
// govmomi is the point — this sees exactly what a real vCenter would.
func soapMethod(body []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	inBody := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !inBody {
			if se.Name.Local == "Body" {
				inBody = true
			}
			continue
		}
		return se.Name.Local
	}
}

// startRecordingVCenter starts a plain-HTTP vcsim with a recording reverse
// proxy in front of it, and returns the endpoint to point a context at.
// Plain HTTP keeps the recorder to a few lines; the TLS paths are covered by
// the other tests in this package.
func startRecordingVCenter(t *testing.T) (string, *soapRecorder) {
	t.Helper()
	model := simulator.VPX()
	model.Datacenter = 1
	model.Cluster = 1
	model.ClusterHost = 2
	model.App = 1
	model.Machine = 2
	model.Datastore = 2
	if err := model.Create(); err != nil {
		t.Fatalf("create simulator model: %v", err)
	}
	t.Cleanup(model.Remove)

	server := model.Service.NewServer()
	t.Cleanup(server.Close)

	target := &url.URL{Scheme: "http", Host: server.URL.Host}
	proxy := httputil.NewSingleHostReverseProxy(target)
	rec := &soapRecorder{}

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		if m := soapMethod(body); m != "" {
			rec.record(m)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)

	return front.URL, rec
}

// TestEveryCommandIsReadOnly drives every command that talks to a vCenter and
// asserts that nothing outside readOnlyMethods was ever sent.
func TestEveryCommandIsReadOnly(t *testing.T) {
	endpoint, rec := startRecordingVCenter(t)

	r := newRunner(t)
	r.mustRun(testPassword+"\n"+testPassword+"\n", "context", "add",
		"--name", "audit",
		"--endpoint", endpoint,
		"--username", "operator@vsphere.local",
		"--credential", "prompt",
		"--password-stdin",
		"--tls", "insecure",
	)

	// Every command that reaches a vCenter, including the ones that connect
	// as a side effect of doing something else.
	for _, args := range [][]string{
		{"context", "test", "audit"},
		{"doctor", "audit"},
		{"status"},
		{"vm", "list"},
		{"template", "list"},
		{"host", "list"},
		{"cluster", "list"},
		{"vapp", "list"},
		{"datastore", "list"},
		{"network", "list"},
		{"vm", "list", "--all-contexts"},
		{"search", "DC0"},
		{"search", "LocalDS", "--kind", "datastore"},
	} {
		stdout, stderr, err := r.run(testPassword+"\n", args...)
		if err != nil {
			t.Fatalf("vsfleet %s failed: %v\nstdout:\n%s\nstderr:\n%s",
				strings.Join(args, " "), err, stdout, stderr)
		}
	}

	seen := rec.distinct()
	if len(seen) == 0 {
		t.Fatal("no SOAP traffic recorded; the recording proxy is not in the path")
	}
	t.Logf("operations sent to vCenter across every command:\n  %s", strings.Join(seen, "\n  "))

	for _, m := range seen {
		if _, ok := readOnlyMethods[m]; !ok {
			t.Errorf("vsfleet sent %q, which is not an approved read-only operation.\n"+
				"If this is genuinely safe, add it to readOnlyMethods with the reason. "+
				"If it modifies a vCenter, it must not ship.", m)
		}
	}
}

// mutatingPackages are the govmomi packages through which a vSphere object
// can be changed. object is the big one: VirtualMachine.PowerOff,
// Destroy_Task, Reconfigure and every other mutation hangs off the wrappers
// it defines. The rest reach the same surface by other routes.
//
// Checking imports rather than the built binary is deliberate. Every
// operation name in the vSphere schema is present as a string in any binary
// that so much as names a govmomi type, because vim25/types registers the
// whole schema in an init() the linker cannot eliminate — so searching the
// binary for "Destroy_Task" says nothing about whether it can be called.
// What the code imports does say something: a mutation has to come from one
// of these packages, and shipping one of them is a reviewable event.
var mutatingPackages = []string{
	"github.com/vmware/govmomi/object",
	"github.com/vmware/govmomi/task",
	"github.com/vmware/govmomi/vapi/rest",
	"github.com/vmware/govmomi/vim25/methods",
	"github.com/vmware/govmomi/guest",
	"github.com/vmware/govmomi/nfc",
	"github.com/vmware/govmomi/ovf",
}

// TestNoMutationCapablePackageIsImported is the standing guard on the
// property the wire-level test measures: the shipped code has no access to
// the API surface a mutation would have to come from. Test files are exempt
// — they run against vcsim, never a real vCenter, and never ship.
func TestNoMutationCapablePackageIsImported(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			got := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range mutatingPackages {
				if got == bad || strings.HasPrefix(got, bad+"/") {
					t.Errorf("%s imports %s, which can modify a vCenter; "+
						"vsfleet is read-only and must not reach that surface", path, got)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
