package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/whatsnew"
)

func TestRun(t *testing.T) {
	all, err := whatsnew.All()
	if err != nil || len(all) == 0 {
		t.Fatalf("All = %v, %v", all, err)
	}
	shipped := all[0].Version
	cases := []struct {
		name    string
		args    []string
		code    int
		written bool
	}{
		{"shipped release", []string{"v" + shipped}, 0, true},
		{"release without notes", []string{"0.0.1"}, exitNoNotes, false},
		{"no version", nil, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tc.args, strings.NewReader("### Features\n"), &out, &errOut)
			if code != tc.code {
				t.Fatalf("exit %d, want %d (%s)", code, tc.code, errOut.String())
			}
			if tc.written != (out.Len() > 0) {
				t.Fatalf("wrote %q, want output %v", out.String(), tc.written)
			}
			if tc.written {
				if _, ok := whatsnew.FromBody(out.String(), shipped); !ok || !strings.HasSuffix(out.String(), "### Features\n") {
					t.Errorf("body does not carry the notes and the original text:\n%s", out.String())
				}
			}
		})
	}
}
