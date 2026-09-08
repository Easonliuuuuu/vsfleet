package vsphere

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// seedSimulatorTree writes a small directory tree into the simulator's
// datastore so the browser has something with a shape: a folder containing a
// file, plus a file at the root. The nesting is the point — it is what proves
// the single-directory listing does not descend.
func seedSimulatorTree(t *testing.T, c *Client) (id, name string) {
	t.Helper()
	finder := find.NewFinder(c.VIM(), false)
	datacenter, err := finder.DefaultDatacenter(context.Background())
	if err != nil {
		t.Fatalf("find simulator datacenter: %v", err)
	}
	finder.SetDatacenter(datacenter)
	datastore, err := finder.DefaultDatastore(context.Background())
	if err != nil {
		t.Fatalf("find simulator datastore: %v", err)
	}
	var props mo.Datastore
	if err := datastore.Properties(context.Background(), datastore.Reference(), []string{"summary", "name"}, &props); err != nil {
		t.Fatalf("read simulator datastore: %v", err)
	}
	nested := filepath.Join(props.Summary.Url, "database01", "deep")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("create simulator datastore tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "database01.vmdk"), []byte("synthetic vmdk"), 0o600); err != nil {
		t.Fatalf("write nested fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(props.Summary.Url, "README.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write root fixture: %v", err)
	}
	return datastore.Reference().Value, props.Name
}

func findEntry(entries []DatastoreEntry, name string) (DatastoreEntry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return DatastoreEntry{}, false
}

// A root listing must show the directory and the root file, and must not show
// anything from inside the directory. That absence is the whole lazy-loading
// guarantee: navigation costs one query per directory an operator opens, not
// one enumeration of the datastore.
func TestListDatastoreDirectoryDoesNotDescend(t *testing.T) {
	c, _ := browseSimulator(t)
	id, name := seedSimulatorTree(t, c)

	listing, err := c.ListDatastoreDirectory(context.Background(), id, name, "")
	if err != nil {
		t.Fatalf("list root: %v", err)
	}
	folder, ok := findEntry(listing.Entries, "database01")
	if !ok {
		t.Fatalf("root listing has no database01 folder: %+v", listing.Entries)
	}
	if folder.Type != DatastoreEntryFolder {
		t.Fatalf("database01 type=%q, want folder", folder.Type)
	}
	if _, ok := findEntry(listing.Entries, "README.txt"); !ok {
		t.Fatalf("root listing has no README.txt: %+v", listing.Entries)
	}
	if _, ok := findEntry(listing.Entries, "database01.vmdk"); ok {
		t.Fatal("root listing returned a file from inside a subdirectory; the listing is recursing")
	}
	if listing.Truncated {
		t.Fatal("a three-entry root reported truncation")
	}
}

func TestListDatastoreDirectoryReadsASubdirectory(t *testing.T) {
	c, _ := browseSimulator(t)
	id, name := seedSimulatorTree(t, c)

	listing, err := c.ListDatastoreDirectory(context.Background(), id, name, "database01/deep")
	if err != nil {
		t.Fatalf("list subdirectory: %v", err)
	}
	file, ok := findEntry(listing.Entries, "database01.vmdk")
	if !ok {
		t.Fatalf("subdirectory listing has no database01.vmdk: %+v", listing.Entries)
	}
	if file.Type != DatastoreEntryFile {
		t.Fatalf("database01.vmdk type=%q, want file", file.Type)
	}
	if file.Path != "["+name+"] database01/deep/database01.vmdk" {
		t.Fatalf("entry path=%q, want the canonical datastore path", file.Path)
	}
	if file.SizeBytes == 0 {
		t.Fatal("entry has no size; the query did not ask for file details")
	}
	if file.Modified.IsZero() {
		t.Fatal("entry has no modification time; the query did not ask for file details")
	}
}

// Find is the one recursive operation, and it has to reach a file the root
// listing deliberately cannot see.
func TestFindInDatastoreIsRecursive(t *testing.T) {
	c, _ := browseSimulator(t)
	id, name := seedSimulatorTree(t, c)

	listing, err := c.FindInDatastore(context.Background(), id, name, "*.vmdk")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	found := false
	for _, entry := range listing.Entries {
		if entry.Path == "["+name+"] database01/deep/database01.vmdk" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recursive find did not reach the nested vmdk: %+v", listing.Entries)
	}
}

func TestFindInDatastoreRejectsAnEmptyPattern(t *testing.T) {
	c, _ := browseSimulator(t)
	id, name := seedSimulatorTree(t, c)

	if _, err := c.FindInDatastore(context.Background(), id, name, "   "); err == nil {
		t.Fatal("an empty pattern was accepted; that is a whole-datastore enumeration nobody asked for")
	}
}

// A denied browse must reach the caller as an error carrying the server's own
// words. The interactive path deliberately differs from the assessment sweep
// here: an operator is waiting, so silence rendered as an empty directory is
// the wrong answer.
func TestListDatastoreDirectorySurfacesPermissionFailures(t *testing.T) {
	c, model := browseSimulator(t)
	id, name := seedSimulatorTree(t, c)
	model.Service.FaultInjector().AddRule(&simulator.FaultInjectionRule{
		MethodName:  "SearchDatastore_Task",
		ObjectType:  "*",
		ObjectName:  "*",
		Probability: 1,
		FaultType:   simulator.FaultTypeNoPermission,
		Message:     "Datastore.Browse denied",
		Enabled:     true,
	})

	listing, err := c.ListDatastoreDirectory(context.Background(), id, name, "")
	if err == nil {
		t.Fatal("a denied listing returned no error; it would render as an empty directory")
	}
	if len(listing.Entries) != 0 {
		t.Fatalf("a failed listing returned entries: %+v", listing.Entries)
	}
}

func TestListDatastoreDirectoryNeedsADatastoreReference(t *testing.T) {
	c, _ := browseSimulator(t)
	if _, err := c.ListDatastoreDirectory(context.Background(), "", "ds", ""); err == nil {
		t.Fatal("a listing without a datastore reference was accepted")
	}
}

func TestDatastoreListingCapIsHonored(t *testing.T) {
	const limit = 3
	files := make([]types.BaseFileInfo, 0, limit+2)
	for i := 0; i < limit+2; i++ {
		files = append(files, &types.FileInfo{Path: "file.txt"})
	}
	listing := datastoreListing("ds", types.ArrayOfHostDatastoreBrowserSearchResults{
		HostDatastoreBrowserSearchResults: []types.HostDatastoreBrowserSearchResults{{FolderPath: "[ds]", File: files}},
	}, limit)
	if len(listing.Entries) != limit {
		t.Fatalf("entry count=%d, want cap %d", len(listing.Entries), limit)
	}
	if !listing.Truncated {
		t.Fatal("the cap was reached without truncation provenance; a partial result would read as complete")
	}
}

func TestDatastoreListingPutsFoldersFirst(t *testing.T) {
	listing := datastoreListing("ds", types.ArrayOfHostDatastoreBrowserSearchResults{
		HostDatastoreBrowserSearchResults: []types.HostDatastoreBrowserSearchResults{{FolderPath: "[ds]", File: []types.BaseFileInfo{
			&types.FileInfo{Path: "a.txt"},
			&types.FolderFileInfo{FileInfo: types.FileInfo{Path: "zzz"}},
		}}},
	}, 10)
	if len(listing.Entries) != 2 {
		t.Fatalf("entries=%+v", listing.Entries)
	}
	if listing.Entries[0].Name != "zzz" || listing.Entries[0].Type != DatastoreEntryFolder {
		t.Fatalf("folders do not sort first: %+v", listing.Entries)
	}
}

func TestDatastoreEntrySkipsDotPaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "  "} {
		if _, ok := datastoreEntry("ds", "[ds] dir", &types.FolderFileInfo{FileInfo: types.FileInfo{Path: name}}); ok {
			t.Fatalf("path %q became a navigable entry", name)
		}
	}
}

func TestBrowsePathHelpers(t *testing.T) {
	dirs := []struct{ datastore, relative, want string }{
		{"ds", "", "[ds]"},
		{"ds", "/", "[ds]"},
		{"ds", "ISO/linux/", "[ds] ISO/linux"},
		{"ds", `ISO\linux`, "[ds] ISO/linux"},
		{"ds", "ISO//linux", "[ds] ISO/linux"},
		{"Mixed Case DS", "ISO", "[Mixed Case DS] ISO"},
	}
	for _, tc := range dirs {
		if got := datastoreDirPath(tc.datastore, tc.relative); got != tc.want {
			t.Fatalf("datastoreDirPath(%q, %q)=%q, want %q", tc.datastore, tc.relative, got, tc.want)
		}
	}

	parents := []struct{ in, want string }{
		{"", ""},
		{"ISO", ""},
		{"ISO/linux", "ISO"},
		{"ISO/linux/", "ISO"},
		{"a/b/c", "a/b"},
	}
	for _, tc := range parents {
		if got := ParentBrowsePath(tc.in); got != tc.want {
			t.Fatalf("ParentBrowsePath(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}

	children := []struct{ relative, name, want string }{
		{"", "ISO", "ISO"},
		{"ISO", "linux", "ISO/linux"},
		{"ISO/", "/linux/", "ISO/linux"},
		{"ISO", "", "ISO"},
	}
	for _, tc := range children {
		if got := ChildBrowsePath(tc.relative, tc.name); got != tc.want {
			t.Fatalf("ChildBrowsePath(%q, %q)=%q, want %q", tc.relative, tc.name, got, tc.want)
		}
	}
}

// A path that walks out of the datastore is refused rather than normalized
// away: read-only means reading what was asked for, not reading somewhere else
// quietly.
func TestNormalizeBrowsePathRefusesEscapes(t *testing.T) {
	for _, in := range []string{"..", "../..", "ISO/../../etc", `ISO\..\..`} {
		if _, err := normalizeBrowsePath(in); err == nil {
			t.Fatalf("normalizeBrowsePath(%q) accepted a path that leaves the datastore", in)
		}
	}
	got, err := normalizeBrowsePath("/ISO//linux/")
	if err != nil {
		t.Fatalf("normalizeBrowsePath: %v", err)
	}
	if got != "ISO/linux" {
		t.Fatalf("normalizeBrowsePath=%q, want %q", got, "ISO/linux")
	}
	if strings.Contains(got, "//") {
		t.Fatalf("normalizeBrowsePath left a doubled separator: %q", got)
	}
}

// SplitBrowsePath must not lowercase. SplitDatastorePath does, deliberately,
// because it builds join keys — but a browser sends its path back to a server
// that may be case-sensitive and shows it to someone who has to recognize it.
func TestSplitBrowsePathKeepsCase(t *testing.T) {
	datastore, relative, ok := SplitBrowsePath("[SAN-PROD-01] ISO/Linux/Ubuntu.iso")
	if !ok {
		t.Fatal("a canonical datastore path was rejected")
	}
	if datastore != "SAN-PROD-01" {
		t.Fatalf("datastore=%q, want the name as written", datastore)
	}
	if relative != "ISO/Linux/Ubuntu.iso" {
		t.Fatalf("relative=%q, want the path as written", relative)
	}
	if _, _, ok := SplitBrowsePath("ISO/Linux"); ok {
		t.Fatal("a path with no datastore prefix was accepted")
	}
	if _, relative, _ := SplitBrowsePath(`[ds] ISO\\Linux\\`); relative != "ISO/Linux" {
		t.Fatalf("relative=%q, want backslashes normalized and the trailing separator trimmed", relative)
	}
}
