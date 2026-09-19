package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func FuzzTUIKeyResizeNeverPanics(f *testing.F) {
	f.Add([]byte("hjkl?q"), uint8(60), uint8(20))
	f.Add([]byte{0, 1, 2, 3, 13, 27, 127}, uint8(140), uint8(40))
	f.Fuzz(func(t *testing.T, keys []byte, width, height uint8) {
		if len(keys) > 256 {
			keys = keys[:256]
		}
		m := New(context.Background(), twoHealthy(), Options{Current: "prod", RefreshInterval: -1})
		m.width, m.height = 140, 40
		drive(t, m, m.Init())
		for _, key := range keys {
			w := int(width)
			h := int(height)
			if w == 0 {
				w = 1
			}
			if h == 0 {
				h = 1
			}
			// The fuzz target is a state-machine safety check. Do not execute
			// returned timer commands here: a filter cursor blink is deliberately
			// asynchronous and would turn arbitrary input into a real-time test.
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(key)}})
			_ = m.View()
		}
	})
}

func FuzzStaleMessagesCannotReplaceNewerState(f *testing.F) {
	f.Add(uint64(1), uint64(2))
	f.Add(uint64(99), uint64(1))
	f.Fuzz(func(t *testing.T, oldGeneration, newGeneration uint64) {
		m := New(context.Background(), twoHealthy(), Options{Current: "prod", RefreshInterval: -1})
		drive(t, m, m.Init())
		st := m.current()
		if st == nil {
			t.Fatal("model has no current context")
		}
		if oldGeneration == 0 {
			oldGeneration = 1
		}
		if newGeneration == oldGeneration {
			newGeneration++
		}
		st.outstanding = 2 // one stale reply must not finish the newer load
		st.loading = true
		st.generation = newGeneration
		stale := &vsphere.Inventory{Context: "prod", VMs: []vsphere.VM{{Name: "stale-fuzz-row"}}}
		m.Update(groupMsg{context: "prod", cc: st.cc, generation: oldGeneration, group: vsphere.GroupVMs, inv: stale})
		if strings.Contains(m.View(), "stale-fuzz-row") {
			t.Fatalf("stale generation painted stale data")
		}
	})
}

func FuzzRenderIsBoundedAndDeterministic(f *testing.F) {
	f.Add(uint8(60), uint8(20))
	f.Add(uint8(1), uint8(1))
	f.Fuzz(func(t *testing.T, width, height uint8) {
		w, h := int(width), int(height)
		if w < 60 {
			w = 60
		}
		if h < 5 {
			h = 5
		}
		m := New(context.Background(), twoHealthy(), Options{Current: "prod", RefreshInterval: -1})
		drive(t, m, m.Init())
		drive(t, m, discard(m.Update(tea.WindowSizeMsg{Width: w, Height: h})))
		first, second := m.View(), m.View()
		if first != second {
			t.Fatal("render is not deterministic")
		}
		for _, line := range strings.Split(first, "\n") {
			if ansi.StringWidth(line) > w {
				t.Fatalf("line width %d exceeds terminal width %d", ansi.StringWidth(line), w)
			}
		}
	})
}
