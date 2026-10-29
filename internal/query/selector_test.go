package query

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

func TestLabelSelectorMatchesTags(t *testing.T) {
	prod := Subject{TagsAvailable: true, Tags: []vsphere.Tag{{Category: "env", Name: "prod"}, {Category: "tier", Name: "web"}}}
	untagged := Subject{TagsAvailable: true}
	unread := Subject{Tags: prod.Tags}
	for _, tc := range []struct {
		selector string
		prod     bool
		untagged bool
	}{
		{"env=prod", true, false},
		{"env==prod", true, false},
		{"env=dev", false, false},
		{"env!=dev", true, true},
		{"env!=prod", false, true},
		{"env in (dev, prod)", true, false},
		{"env notin (dev,prod)", false, true},
		{"env", true, false},
		{"!env", false, true},
		{"env=prod,tier=web", true, false},
		{"env=prod, tier=db", false, false},
		// A tag name is not a category: "prod" is no key here.
		{"prod", false, false},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			sel, err := ParseLabelSelector(tc.selector)
			if err != nil {
				t.Fatal(err)
			}
			if got := sel.Match(prod); got != tc.prod {
				t.Errorf("prod VM: Match = %v; want %v", got, tc.prod)
			}
			if got := sel.Match(untagged); got != tc.untagged {
				t.Errorf("untagged VM: Match = %v; want %v", got, tc.untagged)
			}
			// Unread tags never match, a negative requirement included.
			if sel.Match(unread) {
				t.Error("a subject whose tags could not be read matched")
			}
		})
	}
}

func TestLabelSelectorRejects(t *testing.T) {
	for _, s := range []string{"", "env=", "=prod", "env in prod", "env in ()", "!", "env prod", "a=b,,c=d"} {
		if _, err := ParseLabelSelector(s); err == nil {
			t.Errorf("ParseLabelSelector(%q) accepted", s)
		}
	}
}

func TestFieldSelector(t *testing.T) {
	kinds := []vsphere.Kind{vsphere.KindVM}
	f, err := ParseFieldSelector(`cpu>=8,power_state==poweredOn,custom.owner="a,b"`, kinds)
	if err != nil {
		t.Fatal(err)
	}
	vm := Subject{CustomAvailable: true, Fields: map[string]any{"cpu": float64(8), "power_state": "poweredOn"},
		CustomAttributes: []vsphere.CustomAttribute{{Name: "owner", Value: "a,b"}}}
	if !f.Match(vm) {
		t.Fatal("selector did not match")
	}
	for _, tc := range []struct{ selector, want string }{
		{"tag.env=prod", "-l env=prod"},
		{"tag=prod", "-l CATEGORY=prod"},
		{"nope=1", `unknown query field "nope"`},
		// The column reads STATE but the field is power_state: the error names it.
		{"state=poweredOn", "power_state"},
		{"cpu>=8,", "empty predicate"},
	} {
		if _, err := ParseFieldSelector(tc.selector, kinds); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseFieldSelector(%q) error = %v; want %q", tc.selector, err, tc.want)
		}
	}
}
