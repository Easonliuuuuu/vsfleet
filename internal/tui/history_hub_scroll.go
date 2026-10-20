package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// scrollHubLines windows a scrolling History pane's lines into h rows. It
// clamps hubOffset to the last full page and records that limit, so the
// next key starts from what is on screen rather than from an offset that ran
// past the end while nothing moved.
func (m *Model) scrollHubLines(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}
	m.hubScrollMax = max(0, len(lines)-h)
	m.hubOffset = clamp(m.hubOffset, 0, m.hubScrollMax)
	return scrollLines(lines, m.hubOffset, h)
}

// scrollHubPane moves the Health or Trends pane for the movement keys the
// browse table answers to, and reports whether the key was one of them.
func (m *Model) scrollHubPane(msg tea.KeyMsg) bool {
	page := max(1, m.bodyHeight()-1)
	switch {
	case key.Matches(msg, m.keys.Up):
		m.hubOffset--
	case key.Matches(msg, m.keys.Down):
		m.hubOffset++
	case key.Matches(msg, m.keys.PageUp):
		m.hubOffset -= page
	case key.Matches(msg, m.keys.PageDown):
		m.hubOffset += page
	case key.Matches(msg, m.keys.Home):
		m.hubOffset = 0
	case key.Matches(msg, m.keys.End):
		m.hubOffset = m.hubScrollMax
	default:
		return false
	}
	m.hubOffset = clamp(m.hubOffset, 0, m.hubScrollMax)
	return true
}
