package cli

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/apiresource"
)

// resourceRequest is one kind and the names asked for in it. No names means
// every object of the kind.
type resourceRequest struct {
	kind  apiresource.Kind
	names []string
}

// parseResourceArgs reads the kubectl argument forms: "KIND[,KIND...]
// [NAME...]" or "KIND/NAME [KIND/NAME...]". The two cannot be mixed, the way
// kubectl refuses "get vm vm/web-01", because a bare word after a KIND/NAME
// could be either and guessing would be wrong half the time.
func parseResourceArgs(args []string) ([]resourceRequest, error) {
	if len(args) == 0 {
		return nil, errors.New(`a KIND is required; run "vsfleet api-resources" for the list`)
	}
	if strings.Contains(args[0], "/") {
		var out []resourceRequest
		for _, arg := range args {
			if !strings.Contains(arg, "/") {
				return nil, fmt.Errorf("%q is not in KIND/NAME form; give every argument as KIND/NAME, or the KIND once followed by names", arg)
			}
			kindName, name, err := splitKindName(arg)
			if err != nil {
				return nil, err
			}
			kind, err := lookupKind(kindName)
			if err != nil {
				return nil, err
			}
			i := indexOfKind(out, kind)
			if i < 0 {
				out = append(out, resourceRequest{kind: kind})
				i = len(out) - 1
			}
			out[i].names = append(out[i].names, name)
		}
		return out, nil
	}
	var out []resourceRequest
	for _, kindName := range strings.Split(args[0], ",") {
		kind, err := lookupKind(kindName)
		if err != nil {
			return nil, err
		}
		if indexOfKind(out, kind) < 0 {
			out = append(out, resourceRequest{kind: kind})
		}
	}
	for _, name := range args[1:] {
		if strings.Contains(name, "/") {
			return nil, fmt.Errorf("%q: there is no need to give a KIND again when passing names after it", name)
		}
	}
	for i := range out {
		out[i].names = args[1:]
	}
	return out, nil
}

// parseSubject reads the one object an analysis command is about, as
// "KIND/NAME" or "KIND NAME", and returns the arguments that follow it.
func parseSubject(args []string) (kind, name string, rest []string, err error) {
	if len(args) == 0 {
		return "", "", nil, errors.New("a KIND and NAME are required")
	}
	if strings.Contains(args[0], "/") {
		kind, name, err := splitKindName(args[0])
		return kind, name, args[1:], err
	}
	if len(args) < 2 {
		return "", "", nil, fmt.Errorf("a NAME is required after %q (or give KIND/NAME)", args[0])
	}
	return args[0], args[1], args[2:], nil
}

// splitKindName splits "vm/web-01". Only the first slash separates, so a
// name that itself holds a slash survives.
func splitKindName(s string) (kind, name string, err error) {
	kind, name, ok := strings.Cut(s, "/")
	if !ok || kind == "" || name == "" {
		return "", "", fmt.Errorf("%q is not in KIND/NAME form", s)
	}
	return kind, name, nil
}

func lookupKind(name string) (apiresource.Kind, error) {
	k, ok := apiresource.Lookup(name)
	if !ok {
		return apiresource.Kind{}, fmt.Errorf(`unknown kind %q; run "vsfleet api-resources" for the list`, name)
	}
	return k, nil
}

func indexOfKind(reqs []resourceRequest, kind apiresource.Kind) int {
	for i, r := range reqs {
		if r.kind.Name == kind.Name {
			return i
		}
	}
	return -1
}

// namePatterns matches object names against what was asked for: an exact
// name, ignoring case, or a shell glob such as 'web*'. It remembers which
// exact names matched nothing, since asking for web-01 and getting silence
// should be an error while 'web*' matching nothing is just an empty answer.
type namePatterns struct {
	asked    []string
	patterns []string
	matched  []bool
}

func newNamePatterns(names []string) (*namePatterns, error) {
	p := &namePatterns{asked: names, matched: make([]bool, len(names))}
	for _, name := range names {
		pattern := strings.ToLower(name)
		if _, err := path.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("invalid name pattern %q: %w", name, err)
		}
		p.patterns = append(p.patterns, pattern)
	}
	return p, nil
}

// match reports whether any of the object's names (its name, and for a VM its
// UUIDs) matches a pattern. With no patterns everything matches.
func (p *namePatterns) match(names ...string) bool {
	if len(p.patterns) == 0 {
		return true
	}
	hit := false
	for i, pattern := range p.patterns {
		for _, name := range names {
			if name == "" {
				continue
			}
			if ok, _ := path.Match(pattern, strings.ToLower(name)); ok {
				p.matched[i] = true
				hit = true
				break
			}
		}
	}
	return hit
}

// missing returns the exact names that matched nothing. A glob is left out.
func (p *namePatterns) missing() []string {
	var out []string
	for i, pattern := range p.patterns {
		if !p.matched[i] && !isGlob(pattern) {
			out = append(out, p.asked[i])
		}
	}
	return out
}

func isGlob(s string) bool { return strings.ContainsAny(s, `*?[\`) }
