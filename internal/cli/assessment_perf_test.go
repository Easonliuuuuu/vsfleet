package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/assessment"
	"github.com/easonliuuuuu/vsfleet/internal/perf"
)

func syntheticPerfCounter(t *testing.T, m perf.Metric) perf.Counter {
	t.Helper()
	c, ok := perf.CounterFor(m)
	if !ok {
		t.Fatalf("no counter %s", m)
	}
	return c
}

// seedPerformance stores a synthetic window for "prod": one VM that is idle
// in its latest sample but peaks briefly and periodically, one whose memory
// counter was denied (so it reads cpu-only), one with too little history, and
// one at statistics level 1 whose active memory comes from mem.usage, and one
// at that level that was powered off. "edge" gets none.
func seedPerformance(t *testing.T, dbPath string) {
	t.Helper()
	store, err := assessment.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const n = 288
	cpu, mem := make([]int64, n), make([]int64, n)
	for i := range cpu {
		cpu[i], mem[i] = 500, 400*1024
		if i%96 < 2 {
			cpu[i] = 9000
		}
	}
	summarize := func(m perf.Metric, raw []int64) perf.Summary {
		return perf.Summarize(perf.SummaryInput{Counter: syntheticPerfCounter(t, m), Raw: raw, Expected: n, IntervalSeconds: 300, VCPU: 4})
	}
	spiky := []perf.Summary{summarize(perf.CPUUsage, cpu), summarize(perf.MemActive, mem)}
	denied := []perf.Summary{summarize(perf.CPUUsage, cpu), perf.Unavailable(syntheticPerfCounter(t, perf.MemActive), 300, n, "permission denied")}
	sparse := []perf.Summary{summarize(perf.CPUUsage, cpu[:3]), summarize(perf.MemActive, mem[:3])}
	usage := make([]int64, n)
	for i := range usage {
		usage[i] = 1885
	}
	active := syntheticPerfCounter(t, perf.MemActive)
	belowLevel := perf.Unavailable(active, 300, n, perf.BelowLevelReason(active, 2, 1, 300))
	leveled := []perf.Summary{summarize(perf.CPUUsage, cpu), belowLevel, summarize(perf.MemUsage, usage)}
	off := []perf.Summary{summarize(perf.CPUUsage, nil), belowLevel, summarize(perf.MemUsage, nil)}
	end := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	w := perf.Window{
		Context: "prod", VCenterID: "vc-prod", Endpoint: "https://prod.example", Source: "synthetic test data",
		StartedAt: end, FinishedAt: end, WindowStart: end.Add(-24 * time.Hour), WindowEnd: end, IntervalSeconds: 300, ExpectedSamples: n,
		RequestsUsed: 2, VMsRequested: 5, VMsSampled: 5, Status: perf.WindowSuccess,
	}
	for _, vm := range []struct {
		name string
		sums []perf.Summary
	}{{"spiky", spiky}, {"denied", denied}, {"sparse", sparse}, {"leveled", leveled}, {"off", off}} {
		sig, reason := perf.Classify(perf.ClassifyInput{Summaries: vm.sums, MemoryMB: 4096})
		w.VMs = append(w.VMs, perf.VMResult{MoRef: "vm-" + vm.name, Name: vm.name, VCPU: 4, MemoryMB: 4096, Sampled: true, Signal: sig, SignalReason: reason, Summaries: vm.sums})
	}
	if _, err := store.SavePerfWindow(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

func mustTopVM(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	out, errOut, err := runCLI(t, dbPath, append([]string{"top", "vm"}, args...)...)
	if err != nil {
		t.Fatalf("top vm %v: %v\nstderr: %s", args, err, errOut)
	}
	return out
}

func mustAssessment(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	out, errOut, err := runCLI(t, dbPath, append([]string{"assessment"}, args...)...)
	if err != nil {
		t.Fatalf("assessment %v: %v\nstderr: %s", args, err, errOut)
	}
	return out
}

func TestTopDoesNotInferSizingFromOneSampleOrMissingData(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	seedPerformance(t, dbPath)
	out := mustTopVM(t, dbPath)
	lines := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 1 {
			lines[f[0]] = line
		}
	}
	if !strings.Contains(lines["spiky"], "peaks-observed") {
		t.Errorf("spiky VM must not read as idle from its last sample: %q", lines["spiky"])
	}
	if f := strings.Fields(lines["denied"]); f[1] != string(perf.SignalCPUOnly) || f[5] != "-" || f[6] != "-" {
		t.Errorf("denied memory counter must read cpu-only with memory unknown, not zero: %q", lines["denied"])
	}
	if !strings.Contains(lines["sparse"], "insufficient-data") {
		t.Errorf("three samples must not support a signal: %q", lines["sparse"])
	}
	for _, name := range []string{"spiky", "denied", "sparse"} {
		if strings.Contains(lines[name], "sustained-low") {
			t.Errorf("%s was called sustained-low: %q", name, lines[name])
		}
	}
}

func TestAssessmentReportStatesWhichContextsHavePerformanceHistory(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	seedPerformance(t, dbPath)
	out := mustAssessment(t, dbPath, "report")
	if !strings.Contains(out, "prod: window #1 success") || !strings.Contains(out, "edge: not collected") {
		t.Fatalf("report:\n%s", out)
	}
	var report struct {
		Performance []assessment.PerfSummary `json:"performance"`
	}
	if err := json.Unmarshal([]byte(mustAssessment(t, dbPath, "report", "-o", "json")), &report); err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, p := range report.Performance {
		status[p.Context] = p.Status
	}
	if status["prod"] != "success" || status["edge"] != "not collected" {
		t.Fatalf("performance = %+v", report.Performance)
	}
}

// At statistics level 1 the table fills active memory from mem.usage, marks
// it, and drops the hint to raise the level, which would change nothing.
func TestTopReadsActiveMemoryFromMemUsage(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	seedPerformance(t, dbPath)
	out := mustTopVM(t, dbPath)
	var row []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 1 && f[0] == "leveled" {
			row = f
		}
	}
	// 18.85% of 4096 MiB.
	if len(row) < 7 || row[1] != string(perf.SignalPeaksObserved) || row[5] != "772.1*" || row[6] != "772.1*" {
		t.Errorf("leveled row = %q; want active memory from mem.usage, marked", row)
	}
	for _, want := range []string{
		"memory:    active memory read from mem.usage for 1 of 5 sampled VMs, as mem.active is not kept",
		"* active memory from mem.usage × configured memory",
		"leveled: low typical usage but peaks of CPU 90%, active memory 19% (" + perf.FromMemUsage + ")",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hint:") {
		t.Errorf("no VM needs a higher level, so there is no hint:\n%s", out)
	}
}

func TestTopNarrowsAndRefuses(t *testing.T) {
	dbPath := newTopologyTestHistoryDB(t)
	seedPerformance(t, dbPath)
	t.Run("glob narrows the VMs", func(t *testing.T) {
		out := mustTopVM(t, dbPath, "sp*")
		if !strings.Contains(out, "spiky") || !strings.Contains(out, "sparse") || strings.Contains(out, "leveled") {
			t.Fatalf("top vm 'sp*':\n%s", out)
		}
	})
	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"unknown VM", []string{"top", "vm", "no-such-vm"}, `no vm matched "no-such-vm"`},
		{"hosts are not collected", []string{"top", "host"}, "sample VMs only"},
		{"missing window", []string{"top", "vm", "--window", "99"}, "no performance window 99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runCLI(t, dbPath, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("%v: error = %v; want %q", tc.args, err, tc.wantErr)
			}
		})
	}
}
