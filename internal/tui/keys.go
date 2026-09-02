package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is the whole keyboard surface. It is one struct rather than a switch
// on raw strings so that the help panel is generated from the bindings and
// cannot drift out of date with them.
type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	NextTab   key.Binding
	PrevTab   key.Binding
	NextPane  key.Binding
	Open      key.Binding
	Back      key.Binding
	Filter    key.Binding
	AllScope  key.Binding
	Reload    key.Binding
	ReloadAll key.Binding
	Doctor    key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
		PageDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		Home:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "first")),
		End:       key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "last")),
		NextTab:   key.NewBinding(key.WithKeys("right", "l", "]"), key.WithHelp("→/l", "next tab")),
		PrevTab:   key.NewBinding(key.WithKeys("left", "h", "["), key.WithHelp("←/h", "prev tab")),
		NextPane:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		Open:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		AllScope:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all vCenters")),
		Reload:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		ReloadAll: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reload all")),
		Doctor:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diagnose")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// helpSections groups the bindings for the help panel.
func (k keyMap) helpSections() []helpSection {
	return []helpSection{
		{"Move", []key.Binding{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End}},
		{"Navigate", []key.Binding{k.NextTab, k.PrevTab, k.NextPane, k.Open, k.Back}},
		{"Scope", []key.Binding{k.AllScope, k.Filter}},
		{"Connection", []key.Binding{k.Reload, k.ReloadAll, k.Doctor}},
		{"Other", []key.Binding{k.Help, k.Quit}},
	}
}

type helpSection struct {
	title    string
	bindings []key.Binding
}

// footerHints is the always-visible key line, kept short enough to survive an
// 80 column terminal.
func (k keyMap) footerHints(m *Model) []key.Binding {
	switch m.mode {
	case modeDetail:
		return []key.Binding{k.Up, k.Down, k.Back, k.Help, k.Quit}
	case modeDoctor:
		return []key.Binding{k.Reload, k.Back, k.Help, k.Quit}
	case modeHelp:
		return []key.Binding{k.Back, k.Quit}
	default:
		return []key.Binding{k.NextTab, k.NextPane, k.Open, k.Filter, k.AllScope, k.Reload, k.Doctor, k.Help, k.Quit}
	}
}
