package health

import "testing"

// TestStateWordsReadAsWords checks that finding messages name vSphere states
// in words rather than as API enums (issue #476).
func TestStateWordsReadAsWords(t *testing.T) {
	for _, tc := range []struct{ state, want string }{
		{"notResponding", "not responding"},
		{"disconnected", "disconnected"},
		{"guestToolsNeedUpgrade", "an upgrade is available"},
		{"someNewState", "some new state"},
		{"", "in an unknown state"},
	} {
		if got := stateWords(tc.state); got != tc.want {
			t.Errorf("stateWords(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
	for _, tc := range []struct{ state, want string }{
		{"guestToolsNotRunning", "VMware Tools are not running"},
		{"guestToolsExecutingScripts", "VMware Tools are not running (starting)"},
	} {
		if got := toolsNotRunningMessage(tc.state); got != tc.want {
			t.Errorf("toolsNotRunningMessage(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}
