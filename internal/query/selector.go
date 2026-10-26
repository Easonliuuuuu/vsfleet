package query

import (
	"fmt"
	"slices"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// LabelSelector is kubectl's -l over vSphere tags: a tag category is the
// label key and the tag's name is its value. Requirements are ANDed:
//
//	env=prod, env==prod   a tag named prod in category env
//	env!=prod             no tag named prod in category env
//	env in (prod,dev)     a tag in env named prod or dev
//	env notin (prod,dev)  no tag in env named prod or dev
//	env                   any tag in category env
//	!env                  no tag in category env
//
// Keys and values are matched exactly, as tags are. A subject whose tags
// could not be read matches nothing, negative requirements included,
// because an unknown answer is not a "no".
type LabelSelector struct{ requirements []requirement }

type requirement struct {
	key    string
	op     string // "in", "notin", "exists", "!exists"
	values []string
}

// ParseLabelSelector parses a comma-separated label selector.
func ParseLabelSelector(s string) (LabelSelector, error) {
	var sel LabelSelector
	for _, part := range splitTopLevel(s, true) {
		part = strings.TrimSpace(part)
		if part == "" {
			return LabelSelector{}, fmt.Errorf("label selector %q has an empty requirement", s)
		}
		r, err := parseRequirement(part)
		if err != nil {
			return LabelSelector{}, fmt.Errorf("label selector %q: %w", s, err)
		}
		sel.requirements = append(sel.requirements, r)
	}
	return sel, nil
}

func parseRequirement(s string) (requirement, error) {
	for _, op := range []string{"!=", "==", "="} {
		if key, value, ok := strings.Cut(s, op); ok {
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if key == "" || value == "" {
				return requirement{}, fmt.Errorf("%q needs a key and a value", s)
			}
			r := requirement{key: key, op: "in", values: []string{value}}
			if op == "!=" {
				r.op = "notin"
			}
			return r, nil
		}
	}
	fields := strings.Fields(s)
	switch {
	case len(fields) == 1 && strings.HasPrefix(fields[0], "!"):
		key := strings.TrimSpace(fields[0][1:])
		if key == "" {
			return requirement{}, fmt.Errorf("%q needs a key", s)
		}
		return requirement{key: key, op: "!exists"}, nil
	case len(fields) == 1:
		return requirement{key: fields[0], op: "exists"}, nil
	case len(fields) >= 2 && (fields[1] == "in" || fields[1] == "notin"):
		key := fields[0]
		set := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s[len(key):]), fields[1]))
		if !strings.HasPrefix(set, "(") || !strings.HasSuffix(set, ")") {
			return requirement{}, fmt.Errorf("%q: the values after %s go in parentheses", s, fields[1])
		}
		var values []string
		for _, v := range strings.Split(set[1:len(set)-1], ",") {
			if v = strings.TrimSpace(v); v != "" {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			return requirement{}, fmt.Errorf("%q lists no values", s)
		}
		return requirement{key: key, op: fields[1], values: values}, nil
	}
	return requirement{}, fmt.Errorf("%q is not key=value, key!=value, key in (...), key notin (...), key or !key", s)
}

// Empty reports whether the selector has no requirements.
func (l LabelSelector) Empty() bool { return len(l.requirements) == 0 }

// Match reports whether the subject's tags satisfy every requirement.
func (l LabelSelector) Match(s Subject) bool {
	if l.Empty() {
		return true
	}
	if !s.TagsAvailable {
		return false
	}
	for _, r := range l.requirements {
		if !r.match(s.Tags) {
			return false
		}
	}
	return true
}

func (r requirement) match(tags []vsphere.Tag) bool {
	inCategory := func(pred func(vsphere.Tag) bool) bool {
		return slices.ContainsFunc(tags, func(t vsphere.Tag) bool { return t.Category == r.key && pred(t) })
	}
	named := func(t vsphere.Tag) bool { return slices.Contains(r.values, t.Name) }
	switch r.op {
	case "in":
		return inCategory(named)
	case "notin":
		return !inCategory(named)
	case "exists":
		return inCategory(func(vsphere.Tag) bool { return true })
	case "!exists":
		return !inCategory(func(vsphere.Tag) bool { return true })
	}
	return false
}

// ParseFieldSelector parses kubectl's --field-selector: predicates in the
// --where language, separated by commas and ANDed. "==" is accepted for "=",
// as kubectl does. Tag predicates are refused here because -l owns them.
func ParseFieldSelector(s string, kinds []vsphere.Kind) (Filter, error) {
	var expressions []string
	for _, part := range splitTopLevel(s, false) {
		part = strings.TrimSpace(part)
		if part == "" {
			return Filter{}, fmt.Errorf("field selector %q has an empty predicate", s)
		}
		expressions = append(expressions, strings.Replace(part, "==", "=", 1))
	}
	f, err := Parse(expressions, kinds)
	if err != nil {
		return Filter{}, err
	}
	for _, p := range f.predicates {
		if p.Field == "tag" {
			return Filter{}, fmt.Errorf("%q: select tags with -l, e.g. -l %s", p.String(), labelHint(p))
		}
	}
	return f, nil
}

func labelHint(p Predicate) string {
	key := p.Qualifier
	if key == "" {
		key = "CATEGORY"
	}
	return key + p.Operator + p.Value
}

// splitTopLevel splits s at commas outside quotes and, for label selectors,
// outside parentheses.
func splitTopLevel(s string, parens bool) []string {
	var (
		out   []string
		start int
		depth int
		quote rune
	)
	for i, c := range s {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case parens && c == '(':
			depth++
		case parens && c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
