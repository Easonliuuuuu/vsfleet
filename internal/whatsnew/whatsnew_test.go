package whatsnew

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// Every shipped notes file must fit the upgrade box; this is what stops a
// long title from overflowing it.
func TestShippedNotesFitThePopup(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no notes shipped")
	}
	for _, r := range all {
		if err := r.Check(); err != nil {
			t.Error(err)
		}
	}
}

func TestCheckRejectsWhatDoesNotFit(t *testing.T) {
	long := strings.Repeat("x", MaxLine+1)
	cases := []struct {
		name string
		r    Release
	}{
		{"no highlights", Release{Version: "1.0.0"}},
		{"long title", Release{Version: "1.0.0", Highlights: []Item{{Title: long}}}},
		{"long try", Release{Version: "1.0.0", Highlights: []Item{{Title: "ok", Try: long}}}},
		{"too many highlights", Release{Version: "1.0.0", Highlights: make([]Item, MaxHighlights+1)}},
		{"untitled heads-up", Release{Version: "1.0.0", Highlights: []Item{{Title: "ok"}}, HeadsUp: []Item{{Detail: "x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.r.Check() == nil {
				t.Fatal("Check passed, want an error")
			}
		})
	}
}

func TestLoadRejectsAMisnamedFile(t *testing.T) {
	fsys := fstest.MapFS{"notes/1.0.0.toml": {Data: []byte(`version = "1.0.1"`)}}
	if _, err := load(fsys, "notes"); err == nil {
		t.Fatal("load passed, want the version/file name mismatch")
	}
}

func TestSince(t *testing.T) {
	all := []Release{{Version: "0.9.0"}, {Version: "0.8.1"}, {Version: "0.8.0"}, {Version: "0.7.0"}}
	cases := []struct {
		name              string
		previous, current string
		want              []string
	}{
		{"one release", "0.8.1", "0.9.0", []string{"0.9.0"}},
		{"skipped releases", "0.7.0", "0.9.0", []string{"0.9.0", "0.8.1", "0.8.0"}},
		{"notes for a later release are not shown", "0.7.0", "0.8.0", []string{"0.8.0"}},
		{"previous predates the notes", "", "0.8.1", []string{"0.8.1"}},
		{"no notes for this release", "0.9.0", "0.9.1", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, r := range since(all, tc.previous, tc.current) {
				got = append(got, r.Version)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("since = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReleaseBlockRoundTrips(t *testing.T) {
	r := Release{
		Version:    "1.2.0",
		Highlights: []Item{{Title: "Sort --> anything", Try: "vsfleet get vms", Detail: "Detail."}},
		HeadsUp:    []Item{{Title: "A flag moved"}},
	}
	body, err := WithBlock("## [1.2.0](link)\n\n### Features\n", r)
	if err != nil {
		t.Fatalf("WithBlock: %v", err)
	}
	again, err := WithBlock(body, r)
	if err != nil || again != body {
		t.Fatalf("a second run changed the body:\n%s\nwant\n%s", again, body)
	}
	if !strings.HasSuffix(body, "### Features\n") {
		t.Fatalf("the generated notes were not kept after the block:\n%s", body)
	}
	got, ok := FromBody(body, "1.2.0")
	if !ok || !reflect.DeepEqual(got, r) {
		t.Fatalf("FromBody = %+v, %v; want %+v", got, ok, r)
	}
	if _, ok := FromBody(body, "1.3.0"); ok {
		t.Fatal("FromBody accepted notes for another version")
	}
}

func TestFromBodyCleansWhatItReads(t *testing.T) {
	body := `<!-- vsfleet-whats-new {"version":"1.0.0","highlights":[` +
		`{"title":"\u001b[2Jclear\nscreen\u202e"},{"title":"` + strings.Repeat("y", 80) + `"},` +
		`{"title":"3"},{"title":"4"},{"title":"5"},{"title":"6"}]} -->`
	r, ok := FromBody(body, "1.0.0")
	if !ok {
		t.Fatal("FromBody refused the block")
	}
	if got := r.Highlights[0].Title; got != "[2Jclear screen" {
		t.Errorf("title = %q, want control characters dropped and the line break folded", got)
	}
	if got := []rune(r.Highlights[1].Title); len(got) != MaxLine || got[len(got)-1] != '…' {
		t.Errorf("long title = %q, want it cut to %d characters", string(got), MaxLine)
	}
	if len(r.Highlights) != MaxHighlights {
		t.Errorf("%d highlights, want at most %d", len(r.Highlights), MaxHighlights)
	}
}
