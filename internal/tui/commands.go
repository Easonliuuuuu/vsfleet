package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/contextops"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// inventoryMsg carries the outcome of reading one vCenter. The error travels
// in the message rather than being returned: a command that fails must land on
// its own context's row, never take the program down with it.
type inventoryMsg struct {
	context   string
	inventory *vsphere.Inventory
	err       error
	elapsed   time.Duration
}

// diagnosisMsg carries a completed connection diagnosis.
type diagnosisMsg struct {
	context   string
	diagnosis *vsphere.Diagnosis
}

// loadInventory reads one vCenter in the background. Every context gets its
// own command, so several are in flight at once and a slow one only ever
// delays its own row.
func loadInventory(ctx context.Context, b Backend, cc *config.Context) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		inv, err := b.Inventory(ctx, cc)
		return inventoryMsg{context: cc.Name, inventory: inv, err: err, elapsed: time.Since(start)}
	}
}

// diagnose walks the connection stages for one context in the background.
func diagnose(ctx context.Context, b Backend, cc *config.Context) tea.Cmd {
	return func() tea.Msg {
		return diagnosisMsg{context: cc.Name, diagnosis: b.Diagnose(ctx, cc)}
	}
}

// formTestMsg carries the outcome of testing an as-yet-unsaved context.
type formTestMsg struct {
	context   *config.Context
	diagnosis *vsphere.Diagnosis
}

// formSaveMsg carries the outcome of saving the form. result is non-nil even
// on failure, so the form can show what was attempted.
type formSaveMsg struct {
	result *contextops.Result
	err    error
}

// formDeleteMsg carries the outcome of removing a context.
type formDeleteMsg struct {
	name    string
	context *config.Context
	err     error
}

// formDiscoverMsg carries the certificate fetched for a not-yet-pinned
// endpoint.
type formDiscoverMsg struct {
	sha256, sha1, subject string
	notAfter              time.Time
	err                   error
}

func testFormContext(ctx context.Context, b Backend, in contextops.Input) tea.Cmd {
	return func() tea.Msg {
		cc, d := b.TestContext(ctx, in)
		return formTestMsg{context: cc, diagnosis: d}
	}
}

func saveFormContext(ctx context.Context, b Backend, in contextops.Input) tea.Cmd {
	return func() tea.Msg {
		res, err := b.SaveContext(ctx, in, true)
		return formSaveMsg{result: res, err: err}
	}
}

// removeContext deletes a context in the background. Removal is not gated on
// a connection, so unlike the other form commands it needs no test stage.
func removeContext(ctx context.Context, b Backend, name string, alsoCredential bool) tea.Cmd {
	return func() tea.Msg {
		cc, err := b.RemoveContext(ctx, name, alsoCredential)
		return formDeleteMsg{name: name, context: cc, err: err}
	}
}

func discoverThumbprint(ctx context.Context, b Backend, cc *config.Context) tea.Cmd {
	return func() tea.Msg {
		sha256, sha1, subject, notAfter, err := b.DiscoverThumbprint(ctx, cc)
		return formDiscoverMsg{sha256: sha256, sha1: sha1, subject: subject, notAfter: notAfter, err: err}
	}
}
