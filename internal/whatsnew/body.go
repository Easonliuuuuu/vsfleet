package whatsnew

import (
	"encoding/json"
	"strings"
	"unicode"
)

// The block the release workflow puts at the top of a GitHub release: the
// notes as Markdown for people, and as JSON inside an HTML comment, which
// GitHub does not show, for the upgrade prompt. json.Marshal escapes ">",
// so the JSON can never end the comment early.
const (
	blockStart = "<!-- vsfleet-whats-new "
	blockEnd   = "<!-- /vsfleet-whats-new -->"
)

// Markdown renders r as the release block.
func (r Release) Markdown() (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(blockStart + string(data) + " -->\n")
	b.WriteString("## What's new\n\n")
	for _, it := range r.Highlights {
		writeItem(&b, it)
	}
	if len(r.HeadsUp) > 0 {
		b.WriteString("\n### ⚠ Heads up before you upgrade\n\n")
		for _, it := range r.HeadsUp {
			writeItem(&b, it)
		}
	}
	if len(r.Fixes) > 0 {
		b.WriteString("\n### Notable fixes\n\n")
		for _, it := range r.Fixes {
			writeItem(&b, it)
		}
	}
	b.WriteString("\n" + blockEnd + "\n")
	return b.String(), nil
}

func writeItem(b *strings.Builder, it Item) {
	b.WriteString("- **" + it.Title + "**")
	if it.Detail != "" {
		b.WriteString(" — " + it.Detail)
	}
	if it.Try != "" {
		b.WriteString(" Try `" + it.Try + "`.")
	}
	b.WriteString("\n")
}

// WithBlock returns body with r's block at the top, replacing any block an
// earlier run put there, so running the release step twice changes nothing.
func WithBlock(body string, r Release) (string, error) {
	block, err := r.Markdown()
	if err != nil {
		return "", err
	}
	rest := strings.TrimLeft(withoutBlock(body), "\n")
	return block + "\n" + rest, nil
}

func withoutBlock(body string) string {
	start := strings.Index(body, blockStart)
	if start < 0 {
		return body
	}
	end := strings.Index(body[start:], blockEnd)
	if end < 0 {
		return body
	}
	return body[:start] + body[start+end+len(blockEnd):]
}

// FromBody reads the notes out of a release body, as the upgrade prompt
// does. The body comes from the network, so whatever it holds is cleaned to
// plain single-line text and cut to the limits before anything shows it.
func FromBody(body, version string) (Release, bool) {
	start := strings.Index(body, blockStart)
	if start < 0 {
		return Release{}, false
	}
	rest := body[start+len(blockStart):]
	end := strings.Index(rest, "-->")
	if end < 0 {
		return Release{}, false
	}
	var r Release
	if err := json.Unmarshal([]byte(strings.TrimSpace(rest[:end])), &r); err != nil {
		return Release{}, false
	}
	if r.Version != version {
		return Release{}, false
	}
	r = r.Clean()
	if len(r.Highlights) == 0 && len(r.HeadsUp) == 0 {
		return Release{}, false
	}
	return r, true
}

// Clean returns r with every text cut to the limits and stripped of control
// characters, such as the escape that starts a terminal sequence, and with
// at most the items that fit.
func (r Release) Clean() Release {
	return Release{
		Version:    r.Version,
		Highlights: cleanItems(r.Highlights, MaxHighlights),
		HeadsUp:    cleanItems(r.HeadsUp, MaxHeadsUp),
		Fixes:      cleanItems(r.Fixes, MaxFixes),
	}
}

func cleanItems(items []Item, limit int) []Item {
	var out []Item
	for _, it := range items {
		c := Item{Title: cleanText(it.Title, MaxLine), Try: cleanText(it.Try, MaxLine), Detail: cleanText(it.Detail, MaxDetail)}
		if c.Title == "" {
			continue
		}
		out = append(out, c)
		if len(out) == limit {
			break
		}
	}
	return out
}

// cleanText keeps printable characters, folds any run of spaces, tabs or
// line breaks into one space, and cuts the result to limit characters.
func cleanText(s string, limit int) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true
			continue
		case !unicode.IsPrint(r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteRune(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := []rune(b.String())
	if len(out) > limit {
		return string(out[:limit-1]) + "…"
	}
	return string(out)
}
