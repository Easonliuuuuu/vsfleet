package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vc-tui/internal/config"
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
