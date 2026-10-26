package cli

import (
	"strings"
	"testing"
)

// Every one of these is a mistake in the command line itself, so it fails
// before configuration is read or a vCenter is asked, and says what to do.
func TestGetFlagMistakesFailFast(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"health", "-o", "yaml"}, `-o yaml is not supported by "vsfleet health", which takes table or json`},
		{[]string{"get", "vms", "-o", "xml"}, `unknown output format "xml"`},
		{[]string{"get", "vms", "-o", "jsonpath"}, "-o jsonpath needs a template"},
		{[]string{"get", "vms", "-o", "yaml=x"}, "-o yaml takes no argument"},
		{[]string{"get", "vms", "-o", "jsonpath={.items[*]"}, "unclosed {"},
		{[]string{"get", "vms", "-o", "custom-columns=NAME"}, "is not NAME:EXPRESSION"},
		{[]string{"get", "vms", "-l", "env in prod"}, "go in parentheses"},
		{[]string{"get", "vms", "--sort-by", "name"}, "must start with"},
		{[]string{"get", "vms", "--refresh", "1m"}, "--refresh applies with -w"},
		{[]string{"get", "vms", "--where", "cpu>=8"}, "--where was renamed"},
		{[]string{"get", "vms", "--wide"}, "use -o wide"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, _, err := runArgs(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %q", err, tc.want)
			}
		})
	}
}
