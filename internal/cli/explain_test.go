package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
	"github.com/easonliuuuuu/vsfleet/internal/report"
)

func runExplain(t *testing.T, args ...string) (*App, string, string, error) {
	t.Helper()
	var in, out, errOut bytes.Buffer
	a := &App{In: &in, Out: &out, Err: &errOut}
	root := NewRootCommand(a)
	root.SetArgs(append([]string{"explain"}, args...))
	err := root.ExecuteContext(context.Background())
	return a, out.String(), errOut.String(), err
}

// Explain describes the export profile itself, so it must answer without a
// vCenter, without configuration and without a keyring — the same promise the
// demo makes, measured the same way: every side-effecting dependency on App is
// a lazy accessor, so each one still being nil proves none of it was touched.
func TestExplainLoadsNothingFromTheMachine(t *testing.T) {
	scratchPaths(t)
	keyring.MockInit()

	for _, arg := range []string{"rvtools", "vm"} {
		a, out, _, err := runExplain(t, arg)
		if err != nil {
			t.Fatalf("explain %s: %v", arg, err)
		}
		if out == "" {
			t.Fatalf("explain %s produced no output", arg)
		}
		assertNothingLoaded(t, a)
	}
}

func assertNothingLoaded(t *testing.T, a *App) {
	t.Helper()
	if a.cfg != nil {
		t.Error("the report loaded the configuration file")
	}
	if a.resolver != nil {
		t.Error("the report built the credential resolver")
	}
	if a.mgr != nil {
		t.Error("the report built the session manager")
	}
	if a.history != nil {
		t.Error("the report opened the history database")
	}
}

func TestExplainRVToolsCoversEveryWorksheet(t *testing.T) {
	_, out, _, err := runExplain(t, "rvtools")
	if err != nil {
		t.Fatalf("explain rvtools: %v", err)
	}
	for _, sheet := range []string{"vInfo", "vCPU", "vMemory", "vDisk", "vPartition", "vNetwork", "vCD", "vUSB", "vSnapshot", "vTools", "vRP", "vCluster", "vHost", "vHBA", "vNIC", "vSwitch", "vPort", "dvSwitch", "dvPort", "vSC_VMK", "vDatastore", "vMultiPath", "vFileInfo", "vHealth", "vsfleetCoverage"} {
		if !strings.Contains(out, sheet) {
			t.Errorf("report does not mention the %s worksheet", sheet)
		}
	}
}

func TestExplainRVToolsJSONIsMachineReadable(t *testing.T) {
	_, out, _, err := runExplain(t, "rvtools.vDatastore", "-o", "json")
	if err != nil {
		t.Fatalf("explain rvtools.vDatastore: %v", err)
	}
	var sheets []struct {
		Sheet       string `json:"sheet"`
		RVToolsName bool   `json:"rvtools_named"`
		DerivesFrom string `json:"derives_from"`
		Columns     []struct {
			Column string `json:"column"`
			Kind   string `json:"kind"`
			Unit   string `json:"unit"`
		} `json:"columns"`
	}
	if err := json.Unmarshal([]byte(out), &sheets); err != nil {
		t.Fatalf("decoding the report: %v\n%s", err, out)
	}
	if len(sheets) != 1 || sheets[0].Sheet != "vDatastore" {
		t.Fatalf("rvtools.vDatastore did not narrow the report: %+v", sheets)
	}
	if !sheets[0].RVToolsName || sheets[0].DerivesFrom == "" {
		t.Errorf("vDatastore is missing its provenance: %+v", sheets[0])
	}
	var capacity bool
	for _, col := range sheets[0].Columns {
		if col.Column == "Capacity MiB" {
			capacity = true
			if col.Kind == "" || col.Unit != "MiB" {
				t.Errorf("Capacity MiB is described as %+v", col)
			}
		}
	}
	if !capacity {
		t.Error("vDatastore does not describe its Capacity MiB column")
	}
}

// vsfleetCoverage is vsfleet's own worksheet. The report must say so rather
// than letting a reader assume every tab in the workbook is an RVTools layout.
func TestExplainRVToolsMarksTheVsfleetExtension(t *testing.T) {
	_, out, _, err := runExplain(t, "rvtools.vsfleetcoverage")
	if err != nil {
		t.Fatalf("explain rvtools.vsfleetcoverage: %v", err)
	}
	if !strings.Contains(out, "vsfleet extension") {
		t.Errorf("vsfleetCoverage is not marked as a vsfleet extension:\n%s", out)
	}
}

func TestExplainRVToolsRejectsAnUnknownWorksheet(t *testing.T) {
	_, _, _, err := runExplain(t, "rvtools.vMetaData")
	if err == nil {
		t.Fatal("expected an error for a worksheet this profile does not write")
	}
	// The message must name what is available, so the answer to "is vMetaData
	// in here?" is settled by the error itself.
	if !strings.Contains(err.Error(), "vInfo") {
		t.Errorf("the error does not list the worksheets that exist: %v", err)
	}
}

// Every kind explain maps to a worksheet must name one the export writes, or
// "explain KIND" silently drops that worksheet.
func TestKindSheetsExist(t *testing.T) {
	profile, err := report.Profile()
	if err != nil {
		t.Fatal(err)
	}
	for kind, sheets := range kindSheets {
		if _, ok := apiresource.Lookup(kind); !ok {
			t.Errorf("kindSheets names unknown kind %q", kind)
		}
		for _, sheet := range sheets {
			if !slices.ContainsFunc(profile, func(s report.SheetSpec) bool { return s.Name == sheet }) {
				t.Errorf("%s: worksheet %q is not in the export profile", kind, sheet)
			}
		}
	}
}

func TestExplainKind(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		want    []string
		wantErr string
	}{
		{name: "kind by short name", args: []string{"ds"}, want: []string{"KIND:", "datastore", "vsfleet get datastores", "vDatastore", "Capacity MiB", "vsfleet describe datastore NAME"}},
		{name: "subresource has no describe", args: []string{"hba"}, want: []string{"--host HOST", "vHBA"}},
		{name: "field across worksheets", args: []string{"vm.annotation"}, want: []string{"FIELD:", "RVTools vInfo:", "RVTools vDisk:"}},
		{name: "get column", args: []string{"host.cluster"}, want: []string{`A column of "vsfleet get hosts"`}},
		{name: "unknown field", args: []string{"vm.no-such-column"}, wantErr: `vm has no field "no-such-column"`},
		{name: "unknown kind", args: []string{"widget"}, wantErr: `unknown kind "widget"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, _, err := runExplain(t, tc.args...)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v; want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output does not mention %q:\n%s", want, out)
				}
			}
		})
	}
}
