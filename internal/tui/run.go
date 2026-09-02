package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the interface and blocks until the operator quits. The alternate
// screen is used so the terminal is left exactly as it was found.
//
// The returned Snapshot reflects wherever the interface was left — even on
// error, since a canceled context still exits through the same path and the
// caller decides for itself whether a partial run is worth remembering.
func Run(ctx context.Context, b Backend, opts Options) (Snapshot, error) {
	m := New(ctx, b, opts)
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)
	final, err := p.Run()
	if fm, ok := final.(*Model); ok {
		return fm.Snapshot(), err
	}
	return Snapshot{}, err
}
