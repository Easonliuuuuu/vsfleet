package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the interface and blocks until the operator quits. The alternate
// screen is used so the terminal is left exactly as it was found.
func Run(ctx context.Context, b Backend, opts Options) error {
	p := tea.NewProgram(
		New(ctx, b, opts),
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	)
	_, err := p.Run()
	return err
}
