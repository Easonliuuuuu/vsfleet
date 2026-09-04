package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is the whole keyboard surface. It is one struct rather than a switch
// on raw strings so that the help panel is generated from the bindings and
// cannot drift out of date with them.
//
// The browse screen deliberately keeps only the keys an operator uses hourly.
// Everything about a vCenter itself — switching, adding, editing, removing —
// lives behind Contexts, so the always-visible key line fits an 80 column
// terminal without truncating.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Home     key.Binding
	End      key.Binding

	// Kind jumps straight to a resource tab by its number. Cycling with
	// NextTab and PrevTab still works, but five presses of "l" to reach
	// Networks is not a way to move around an estate.
	Kind    key.Binding
	NextTab key.Binding
	PrevTab key.Binding

	Open   key.Binding
	Back   key.Binding
	Filter key.Binding
	// Search widens the filter into every vCenter and every kind. It shares
	// "tab" with nothing: removing the two-pane layout freed the key, and
	// widening a search is the closest thing left to changing pane.
	Search   key.Binding
	Contexts key.Binding
	AllScope key.Binding
	// AllScopeBrief is the same key with a shorter label. The browse key line
	// is the one place that has to fit eight hints into 80 columns, and a
	// truncated key line is the problem this screen exists to fix.
	AllScopeBrief key.Binding

	Reload    key.Binding
	ReloadAll key.Binding
	Doctor    key.Binding
	History   key.Binding
	Capture   key.Binding
	Base      key.Binding
	Target    key.Binding
	// Swap exchanges baseline and target on the Changes pane. It is "s"
	// rather than sharing anything with Sort — Sort belongs to the browse
	// table, which the history hub never shows, so the two never collide.
	Swap        key.Binding
	Timeline    key.Binding
	TimelineAll key.Binding

	// PrevPane and NextPane move between the history hub's Changes, Trends and
	// Runs panes. They exist so the history footer stops borrowing NextTab and
	// PrevTab, whose "next kind"/"prev kind" labels describe the browse screen
	// and are wrong here. They are also arrow-only: the hub handles the arrow
	// keys alone, and "h" is already the timeline on that screen.
	PrevPane key.Binding
	NextPane key.Binding

	Sort key.Binding
	Help key.Binding
	Quit key.Binding

	// The next four belong to the contexts screen.
	UseContext    key.Binding
	NewContext    key.Binding
	EditContext   key.Binding
	DeleteContext key.Binding

	// The next three describe the form's own dispatch (up/down move the row,
	// left/right change a select or toggle, enter activates a button) —
	// display only, since the form reads raw key types rather than matching
	// these bindings.
	FormMove     key.Binding
	FormChange   key.Binding
	FormActivate key.Binding

	// Confirm and ToggleKeep belong to the delete confirmation screen.
	Confirm    key.Binding
	ToggleKeep key.Binding
	EditRun    key.Binding
	NoteRun    key.Binding
	PinRun     key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
		Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "first")),
		End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "last")),

		Kind:    key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7"), key.WithHelp("1-7", "kind")),
		NextTab: key.NewBinding(key.WithKeys("right", "l", "]"), key.WithHelp("→/l", "next kind")),
		PrevTab: key.NewBinding(key.WithKeys("left", "h", "["), key.WithHelp("←/h", "prev kind")),

		Open:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter:        key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Search:        key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "search all")),
		Contexts:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "contexts")),
		AllScope:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all vCenters")),
		AllScopeBrief: key.NewBinding(key.WithHelp("a", "all")),

		Reload:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		ReloadAll:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "reload all")),
		Doctor:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diagnose")),
		History:     key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "history")),
		Capture:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "capture")),
		Base:        key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "baseline")),
		Target:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "target")),
		Swap:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "swap")),
		Timeline:    key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "timeline")),
		TimelineAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all observations")),
		PrevPane:    key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev pane")),
		NextPane:    key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next pane")),

		Sort: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort: name/status")),
		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		UseContext:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "use")),
		NewContext:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		EditContext:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		DeleteContext: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),

		FormMove:     key.NewBinding(key.WithHelp("↑/↓", "move")),
		FormChange:   key.NewBinding(key.WithHelp("←/→", "change")),
		FormActivate: key.NewBinding(key.WithHelp("enter", "activate")),

		Confirm:    key.NewBinding(key.WithHelp("y", "delete")),
		ToggleKeep: key.NewBinding(key.WithHelp("c", "keep password")),
		EditRun:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit label")),
		// "n" captures a new assessment everywhere in the history hub, so the
		// note editor takes "N". The two used to share "n", which did whichever
		// the focused pane happened to mean.
		NoteRun: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "edit note")),
		PinRun:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "toggle pin")),
	}
}

// helpSections groups the bindings for the help panel.
func (k keyMap) helpSections() []helpSection {
	return []helpSection{
		{"Move", []key.Binding{k.Up, k.Down, k.PageUp, k.PageDown, k.Home, k.End}},
		{"Resource kinds", []key.Binding{k.Kind, k.NextTab, k.PrevTab, k.Open, k.Back}},
		{"Scope", []key.Binding{k.Contexts, k.AllScope, k.Filter, k.Search}},
		{"Connection", []key.Binding{k.Reload, k.ReloadAll, k.Doctor}},
		// The changes screen puts its run-picker and capture bindings in the
		// footer; keeping this section to one line preserves the compact help
		// overlay at the minimum supported terminal height.
		{"History", []key.Binding{k.History}},
		{"Table", []key.Binding{k.Sort}},
		{"Contexts screen (c)", []key.Binding{k.UseContext, k.NewContext, k.EditContext, k.DeleteContext}},
		{"Other", []key.Binding{k.Help, k.Quit}},
	}
}

type helpSection struct {
	title    string
	bindings []key.Binding
}

// footerHints is the always-visible key line, kept short enough to survive an
// 80 column terminal without an ellipsis.
func (k keyMap) footerHints(m *Model) []key.Binding {
	switch m.mode {
	case modeDetail:
		return []key.Binding{k.Up, k.Down, k.Timeline, k.Back, k.Help, k.Quit}
	case modeVAppDetail:
		return []key.Binding{k.Up, k.Down, k.Open, k.Back, k.Help, k.Quit}
	case modeVAppVMDetail:
		return []key.Binding{k.Up, k.Down, k.Timeline, k.Back, k.Help, k.Quit}
	case modeDoctor:
		return []key.Binding{k.Reload, k.Back, k.Help, k.Quit}
	case modeHelp:
		return []key.Binding{k.Up, k.Down, k.Back, k.Quit}
	case modeForm:
		return []key.Binding{k.FormMove, k.FormChange, k.FormActivate, k.Back}
	case modeConfirmDelete:
		return []key.Binding{k.Confirm, k.ToggleKeep, k.Back}
	case modeContexts:
		return []key.Binding{k.UseContext, k.AllScope, k.NewContext, k.EditContext, k.DeleteContext, k.Doctor, k.Back}
	case modeSearch:
		return []key.Binding{k.Open, k.Filter, k.Sort, k.Reload, k.Back, k.Help, k.Quit}
	case modeChanges:
		return []key.Binding{k.Up, k.Down, k.PrevPane, k.NextPane, k.Base, k.Target, k.Swap, k.Capture, k.Back, k.Help, k.Quit}
	case modeChangeDetail:
		return []key.Binding{k.Timeline, k.Back, k.Help, k.Quit}
	case modeHistoryRuns:
		return []key.Binding{k.Up, k.Down, k.Open, k.EditRun, k.NoteRun, k.PinRun, k.Back, k.Help, k.Quit}
	case modeHistoryRunEdit:
		return []key.Binding{k.FormActivate, k.Back, k.Help, k.Quit}
	case modeHistoryTimeline:
		return []key.Binding{k.Up, k.Down, k.Open, k.TimelineAll, k.Back, k.Help, k.Quit}
	case modeHistoryTimelineDetail:
		return []key.Binding{k.Back, k.Help, k.Quit}
	default:
		// History comes before lower-priority browse hints so it remains
		// discoverable even when a narrow terminal truncates the footer. Enter
		// opening the selected row is conventional and remains in the help view.
		return []key.Binding{k.Kind, k.History, k.Contexts, k.AllScopeBrief, k.Filter, k.Reload, k.Help, k.Quit}
	}
}
