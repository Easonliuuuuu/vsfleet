package assessment

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUnknownStoredContext reports a selector naming a vCenter that no run in
// the window ever recorded. It is a distinct error because a caller that
// derives its selectors from live configuration — the TUI, whose scope is
// whichever vCenter the operator has selected — is asking about a context
// that is simply not captured yet, which is an empty result to explain rather
// than a mistake to report.
var ErrUnknownStoredContext = errors.New("unknown assessment context")

// NormalizeContextSelectors canonicalizes stored-assessment selectors. Empty
// selectors are rejected so a typo cannot silently widen a query.
func NormalizeContextSelectors(selectors []string) ([]string, error) {
	out := make([]string, 0, len(selectors))
	seen := make(map[string]bool, len(selectors))
	for _, raw := range selectors {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, fmt.Errorf("assessment context selector must not be blank")
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// ValidateStoredContexts checks selectors against context rows recorded in
// one or more runs. It intentionally does not consult current configuration.
func ValidateStoredContexts(selectors []string, runs ...[]ContextRun) ([]string, error) {
	wanted, err := NormalizeContextSelectors(selectors)
	if err != nil || len(wanted) == 0 {
		return wanted, err
	}
	known := make(map[string]bool)
	for _, contexts := range runs {
		for _, c := range contexts {
			name := strings.ToLower(strings.TrimSpace(c.Name))
			if name != "" {
				known[name] = true
			}
		}
	}
	var unknown []string
	for _, name := range wanted {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%w(s): %s", ErrUnknownStoredContext, strings.Join(unknown, ", "))
	}
	return wanted, nil
}

func contextSelected(name string, selectors []string) bool {
	if len(selectors) == 0 {
		return true
	}
	name = strings.ToLower(strings.TrimSpace(name))
	for _, selector := range selectors {
		if selector == name {
			return true
		}
	}
	return false
}
