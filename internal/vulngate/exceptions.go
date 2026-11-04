package vulngate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// MaxExceptionTerm is the longest an exception may run from the day the
// gate checks it, so every exception is reviewed at least that often.
const MaxExceptionTerm = 90 * 24 * time.Hour

// Exception accepts one advisory in named versions of one artifact until it
// expires.
type Exception struct {
	ID            string         `toml:"id"`
	Aliases       []string       `toml:"aliases"`
	Artifact      string         `toml:"artifact"`
	Versions      []string       `toml:"versions"`
	Justification string         `toml:"justification"`
	Owner         string         `toml:"owner"`
	Expires       toml.LocalDate `toml:"expires"`
}

type exceptionFile struct {
	Exceptions []Exception `toml:"exception"`
}

// LoadExceptions reads the exception file. Unknown keys are errors, so a
// misspelled field cannot silently widen or void an exception.
func LoadExceptions(r io.Reader) ([]Exception, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read exceptions: %w", err)
	}
	var file exceptionFile
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
			return nil, fmt.Errorf("parse exceptions: unknown keys:\n%s", strict.String())
		}
		return nil, fmt.Errorf("parse exceptions: %w", err)
	}
	return file.Exceptions, nil
}

// expiresAt is the first instant the exception no longer applies: the end
// of its expiry day in UTC.
func (e Exception) expiresAt() time.Time {
	d := e.Expires
	return time.Date(d.Year, time.Month(d.Month), d.Day+1, 0, 0, 0, 0, time.UTC)
}

// problems lists why the exception cannot be used at now, if any.
func (e Exception) problems(now time.Time) []string {
	var out []string
	for _, f := range []struct{ name, value string }{
		{"id", e.ID}, {"artifact", e.Artifact}, {"justification", e.Justification}, {"owner", e.Owner},
	} {
		if f.value == "" {
			out = append(out, "missing "+f.name)
		}
	}
	if len(e.Versions) == 0 || slices.Contains(e.Versions, "") {
		out = append(out, "missing versions")
	}
	if e.Expires == (toml.LocalDate{}) {
		out = append(out, "missing expires")
		return out
	}
	if end := e.expiresAt(); !now.Before(end) {
		out = append(out, "expired on "+e.Expires.String())
	} else if end.Sub(now) > MaxExceptionTerm+24*time.Hour {
		out = append(out, fmt.Sprintf("expires %s, more than %d days ahead", e.Expires, int(MaxExceptionTerm.Hours()/24)))
	}
	return out
}

func (e Exception) covers(f Finding) bool {
	if e.Artifact != f.Artifact || !slices.Contains(e.Versions, f.Version) {
		return false
	}
	ids := f.IDs()
	for _, id := range append([]string{e.ID}, e.Aliases...) {
		if slices.Contains(ids, id) {
			return true
		}
	}
	return false
}

func (e Exception) label() string {
	if e.ID == "" {
		return "exception for " + orUnknown(e.Artifact)
	}
	return e.ID + " in " + orUnknown(e.Artifact)
}
