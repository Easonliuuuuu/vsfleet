package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/whatsnew"
)

var sampleNotes = []whatsnew.Release{
	{
		Version: "0.11.0",
		Highlights: []whatsnew.Item{
			{Title: "Commands now work like kubectl", Try: "vsfleet get vms -A", Detail: "get, describe and the rest work the way kubectl does."},
			{Title: "Sort any tab", Try: "press s", Detail: "Each tab remembers its order."},
			{Title: "Third highlight", Try: "try three"},
			{Title: "Fourth highlight", Try: "try four"},
			{Title: "Fifth highlight", Try: "try five"},
		},
		HeadsUp: []whatsnew.Item{{Title: "--history-db is now --db"}},
		Fixes:   []whatsnew.Item{{Title: "Diffs no longer flag every disk"}},
	},
	{Version: "0.10.1", Highlights: []whatsnew.Item{{Title: "Older highlight"}}, HeadsUp: []whatsnew.Item{{Title: "Older heads-up"}}},
}

func newWhatsNewFixture(t *testing.T, width, height int) *whatsNewPanel {
	t.Helper()
	b := &fakeBackend{contexts: []*config.Context{ctx("prod", "https://vcsa.prod.internal")}}
	m := newTestModel(t, b, Options{Current: "prod"})
	p := newWhatsNewPanel(m, m, sampleNotes)
	p.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return p
}

func notesKey(k string) tea.KeyMsg {
	switch k {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestWhatsNewAnyKeyClosesWithoutActingOnTheKey(t *testing.T) {
	p := newWhatsNewFixture(t, 120, 40)
	next, cmd := p.Update(notesKey("q"))
	if next != p.inner {
		t.Fatalf("a key should close the notes and hand over the interface, got %T", next)
	}
	if cmd != nil || p.inner.quitting {
		t.Error("the key that closed the notes must not also reach the interface: q would have quit")
	}
}

func TestWhatsNewCtrlCStillQuits(t *testing.T) {
	p := newWhatsNewFixture(t, 120, 40)
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c over the notes should quit, got no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c over the notes should quit, got %T", cmd())
	}
}

func TestWhatsNewFullListOpensAndClosesBackToThePopup(t *testing.T) {
	p := newWhatsNewFixture(t, 120, 40)
	if next, _ := p.Update(notesKey("w")); next != p || p.list == nil {
		t.Fatal("w should open the full list over the popup")
	}
	view := ansi.Strip(p.View())
	for _, want := range []string{"get, describe and the rest", "Diffs no longer flag every disk", "vsfleet v0.10.1"} {
		if !strings.Contains(view, want) {
			t.Errorf("full list is missing %q:\n%s", want, view)
		}
	}
	if next, _ := p.Update(notesKey("esc")); next != p || p.list != nil {
		t.Fatal("esc should go back to the popup, not close the notes")
	}
}

func TestWhatsNewFullListScrollsWithinItsLines(t *testing.T) {
	p := newWhatsNewFixture(t, 120, 16)
	p.Update(notesKey("w"))
	p.View()
	l := p.list
	if l.total <= l.rows {
		t.Fatalf("fixture should need scrolling: %d lines in %d rows", l.total, l.rows)
	}
	for range l.total * 2 {
		p.Update(notesKey("down"))
		p.View()
	}
	if l.offset != l.total-l.rows {
		t.Errorf("offset = %d after scrolling past the end, want it held at %d", l.offset, l.total-l.rows)
	}
	if h := lipgloss.Height(l.render(p.inner.theme, 120, 16, "esc back")); h > 16 {
		t.Errorf("the list is %d rows in a 16-row terminal", h)
	}
}

// A shorter terminal drops the "try" lines, then highlights from the end,
// but keeps every heads-up item.
func TestWhatsNewPopupShrinksToTheTerminal(t *testing.T) {
	for _, height := range []int{40, 20, 16} {
		p := newWhatsNewFixture(t, 120, height)
		box := p.popup()
		if box == "" {
			t.Fatalf("height %d: popup did not fit", height)
		}
		if h := lipgloss.Height(box); h > height {
			t.Errorf("height %d: popup is %d rows", height, h)
		}
		plain := ansi.Strip(box)
		for _, want := range []string{"--history-db is now --db", "Older heads-up", "Commands now work like kubectl"} {
			if !strings.Contains(plain, want) {
				t.Errorf("height %d: popup lost %q:\n%s", height, want, plain)
			}
		}
	}
	if full := ansi.Strip(newWhatsNewFixture(t, 120, 40).popup()); !strings.Contains(full, "vsfleet get vms -A") {
		t.Errorf("a tall terminal should show the try lines:\n%s", full)
	}
}

func TestWhatsNewInATinyTerminalOpensTheInterfaceWithANote(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{ctx("prod", "https://vcsa.prod.internal")}}
	m := newTestModel(t, b, Options{Current: "prod"})
	p := newWhatsNewPanel(m, m, sampleNotes)
	next, _ := p.Update(tea.WindowSizeMsg{Width: whatsNewBoxWidth - 1, Height: 40})
	if next != m {
		t.Fatalf("a terminal narrower than the popup should open the interface at once, got %T", next)
	}
	if !strings.Contains(m.message, "Commands now work like kubectl") {
		t.Errorf("message line = %q, want the first highlight", m.message)
	}
}

func TestUpgradePromptShowsHeadsUpAndOpensTheNotes(t *testing.T) {
	offer := brewOffer
	notes := sampleNotes[0]
	offer.Notes = &notes
	p, chosen, _ := newPromptFixture(t, offer)
	view := ansi.Strip(p.View())
	for _, want := range []string{"⚠ --history-db is now --db", "★ 5 highlights · w what's new"} {
		if !strings.Contains(view, want) {
			t.Errorf("prompt is missing %q:\n%s", want, view)
		}
	}
	if next, _ := answerPrompt(p, "w"); next != p || p.list == nil {
		t.Fatal("w should open the release's notes over the prompt")
	}
	if next, _ := answerPrompt(p, "esc"); next != p || p.list != nil || len(*chosen) != 0 {
		t.Fatal("esc in the notes should return to the prompt without answering it")
	}
	if next, _ := answerPrompt(p, "n"); next != p.inner {
		t.Fatalf("the prompt should still answer after the notes close, got %T", next)
	}
}
