// Package whatsnew holds the hand-written "What's new" notes for each
// release: a few highlights, the changes that need attention before
// upgrading, and notable fixes, written for the people who run vsfleet
// rather than taken from commit titles. The notes ship inside the binary, so
// the interface can show what an upgrade brought without going online, and
// the release workflow copies them to the top of the GitHub release, where
// the upgrade prompt reads them before the upgrade.
package whatsnew

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Limits that keep the notes inside the upgrade box: a title or a "try"
// line is one line of its 54 columns, and the popup has room for so many
// items. The tests hold every shipped notes file to them; text read from a
// release is cut to them.
const (
	MaxLine       = 50
	MaxDetail     = 240
	MaxHighlights = 5
	MaxHeadsUp    = 3
	MaxFixes      = 8
)

// Item is one note.
type Item struct {
	// Title is the note in a few words, such as "Commands now work like
	// kubectl".
	Title string `toml:"title" json:"title"`
	// Try is an optional way to see it, such as "vsfleet get vms -A".
	Try string `toml:"try,omitempty" json:"try,omitempty"`
	// Detail is one or two sentences for the full list.
	Detail string `toml:"detail,omitempty" json:"detail,omitempty"`
}

// Release is one release's notes.
type Release struct {
	Version    string `toml:"version" json:"version"`
	Highlights []Item `toml:"highlight" json:"highlights,omitempty"`
	// HeadsUp are the changes someone upgrading must act on, such as a
	// renamed command.
	HeadsUp []Item `toml:"heads_up" json:"heads_up,omitempty"`
	Fixes   []Item `toml:"fix" json:"fixes,omitempty"`
}

//go:embed notes/*.toml
var notesFS embed.FS

// All returns every shipped release's notes, newest first.
func All() ([]Release, error) {
	return load(notesFS, "notes")
}

func load(fsys fs.FS, dir string) ([]Release, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []Release
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".toml" {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var r Release
		if err := toml.Unmarshal(b, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if r.Version+".toml" != e.Name() {
			return nil, fmt.Errorf("%s: version %q does not match the file name", e.Name(), r.Version)
		}
		if _, ok := parseVersion(r.Version); !ok {
			return nil, fmt.Errorf("%s: version %q is not X.Y.Z", e.Name(), r.Version)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return Newer(out[i].Version, out[j].Version) })
	return out, nil
}

// Since returns the notes of the releases after previous up to and
// including current, newest first: what someone who last ran previous has
// not seen. An empty previous, a release from before the notes existed,
// gives current's notes alone.
func Since(previous, current string) []Release {
	all, err := All()
	if err != nil {
		return nil
	}
	return since(all, previous, current)
}

func since(all []Release, previous, current string) []Release {
	var out []Release
	for _, r := range all {
		if Newer(r.Version, current) {
			continue
		}
		if previous == "" {
			if r.Version == current {
				out = append(out, r)
			}
			continue
		}
		if Newer(r.Version, previous) {
			out = append(out, r)
		}
	}
	return out
}

// Get returns version's notes.
func Get(version string) (Release, bool) {
	all, err := All()
	if err != nil {
		return Release{}, false
	}
	for _, r := range all {
		if r.Version == version {
			return r, true
		}
	}
	return Release{}, false
}

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func parseVersion(v string) ([3]int, bool) {
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether a is a later X.Y.Z release than b. Anything else is
// never newer.
func Newer(a, b string) bool {
	x, ok := parseVersion(a)
	if !ok {
		return false
	}
	y, ok := parseVersion(b)
	if !ok {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// Check reports the first way r breaks the limits, or nil.
func (r Release) Check() error {
	if len(r.Highlights) == 0 {
		return fmt.Errorf("%s: no highlights", r.Version)
	}
	if len(r.Highlights) > MaxHighlights {
		return fmt.Errorf("%s: %d highlights, at most %d fit", r.Version, len(r.Highlights), MaxHighlights)
	}
	if len(r.HeadsUp) > MaxHeadsUp {
		return fmt.Errorf("%s: %d heads-up items, at most %d fit", r.Version, len(r.HeadsUp), MaxHeadsUp)
	}
	if len(r.Fixes) > MaxFixes {
		return fmt.Errorf("%s: %d fixes, at most %d", r.Version, len(r.Fixes), MaxFixes)
	}
	for _, list := range [][]Item{r.Highlights, r.HeadsUp, r.Fixes} {
		for _, it := range list {
			switch {
			case strings.TrimSpace(it.Title) == "":
				return fmt.Errorf("%s: an item has no title", r.Version)
			case width(it.Title) > MaxLine:
				return fmt.Errorf("%s: title %q is %d characters, at most %d fit", r.Version, it.Title, width(it.Title), MaxLine)
			case width(it.Try) > MaxLine:
				return fmt.Errorf("%s: try %q is %d characters, at most %d fit", r.Version, it.Try, width(it.Try), MaxLine)
			case width(it.Detail) > MaxDetail:
				return fmt.Errorf("%s: detail of %q is %d characters, at most %d", r.Version, it.Title, width(it.Detail), MaxDetail)
			}
		}
	}
	return nil
}

func width(s string) int { return len([]rune(s)) }
