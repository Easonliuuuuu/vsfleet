package tui

import (
	"slices"
	"testing"
)

// SSH runs "ssh <address>" and nothing else: the user, key, port and route
// are ~/.ssh/config's to decide.
func TestRealHandoffSSHAddsNoOptions(t *testing.T) {
	cmd, err := realHandoff{}.SSH("10.20.0.11")
	if err != nil {
		t.Fatalf("SSH: %v", err)
	}
	if want := []string{"ssh", "10.20.0.11"}; !slices.Equal(cmd.Args, want) {
		t.Errorf("args = %q, want %q", cmd.Args, want)
	}
}

// A guest picks its own reported name, so anything ssh(1) would read as an
// option, a user or more than one argument is refused.
func TestRealHandoffSSHRefusesUnsafeAddresses(t *testing.T) {
	for _, address := range []string{"", "-oProxyCommand=touch /tmp/x", "root@app-01", "app 01", "app\n01"} {
		t.Run(address, func(t *testing.T) {
			if _, err := (realHandoff{}).SSH(address); err == nil {
				t.Errorf("SSH(%q) was accepted", address)
			}
		})
	}
}
