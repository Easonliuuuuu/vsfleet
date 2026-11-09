package cli

import (
	"reflect"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/tui"
)

func TestWelcomeFor(t *testing.T) {
	none := func(string) string { return "" }
	cases := []struct {
		name       string
		welcomed   string
		current    string
		configured bool
		env        map[string]string
		want       tui.Welcome
	}{
		{"brand new operator", "", "0.10.0", false, nil, tui.Welcome{Kind: tui.WelcomeFirstRun, Version: "0.10.0"}},
		{"upgraded from before the welcome existed", "", "0.10.0", true, nil, tui.Welcome{Kind: tui.WelcomeUpdated, Version: "0.10.0"}},
		{"upgraded", "0.9.0", "0.10.0", true, nil, tui.Welcome{Kind: tui.WelcomeUpdated, Version: "0.10.0", Previous: "0.9.0"}},
		{"already welcomed", "0.10.0", "0.10.0", true, nil, tui.Welcome{}},
		{"rebuilt from a checkout", "dev", "dev", true, nil, tui.Welcome{}},
		{"turned off", "0.9.0", "0.10.0", true, map[string]string{EnvNoWelcome: "1"}, tui.Welcome{}},
		{"under CI", "", "0.10.0", false, map[string]string{"CI": "true"}, tui.Welcome{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := none
			if tc.env != nil {
				getenv = func(k string) string { return tc.env[k] }
			}
			if got := welcomeFor(tc.welcomed, tc.current, tc.configured, getenv); got != tc.want {
				t.Errorf("welcomeFor(%q, %q, %v) = %+v, want %+v", tc.welcomed, tc.current, tc.configured, got, tc.want)
			}
		})
	}
}

func TestWhatsNewFollowsAnUpgradeOnly(t *testing.T) {
	cases := []struct {
		name string
		w    tui.Welcome
		want []string
	}{
		{"upgrade", tui.Welcome{Kind: tui.WelcomeUpdated, Version: "0.7.0", Previous: "0.6.4"}, []string{"0.7.0"}},
		{"upgrade from a development build", tui.Welcome{Kind: tui.WelcomeUpdated, Version: "0.7.0", Previous: "dev"}, []string{"0.7.0"}},
		{"upgrade to a release without notes", tui.Welcome{Kind: tui.WelcomeUpdated, Version: "0.6.4", Previous: "0.6.3"}, nil},
		{"first run", tui.Welcome{Kind: tui.WelcomeFirstRun, Version: "0.7.0"}, nil},
		{"no welcome", tui.Welcome{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, r := range whatsNewFor(tc.w) {
				got = append(got, r.Version)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("whatsNewFor = %v, want %v", got, tc.want)
			}
		})
	}
}
