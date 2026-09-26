package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseSSHHostname(t *testing.T) {
	out := "host app-prod\r\nuser devops\r\nhostname 172.31.7.122\nport 22\n"
	if got := parseSSHHostname(out); got != "172.31.7.122" {
		t.Errorf("got %q", got)
	}
	if got := parseSSHHostname("port 22\n"); got != "" {
		t.Errorf("got %q for output with no hostname line", got)
	}
}

// An OpenSSH alias must produce exactly "ssh <alias>" plus only what the
// operator explicitly layered on top — never vsfleet's own ConnectTimeout or
// a route it generated — so ~/.ssh/config stays fully in control.
func TestSSHArgsForAlias(t *testing.T) {
	got := sshArgs(SSHSpec{Address: "app-prod", Alias: true})
	want := []string{"app-prod"}
	if !equalArgs(got, want) {
		t.Errorf("alias args = %v, want %v", got, want)
	}

	got = sshArgs(SSHSpec{Address: "app-prod", Alias: true, User: "otheruser"})
	want = []string{"otheruser@app-prod"}
	if !equalArgs(got, want) {
		t.Errorf("alias+user args = %v, want %v", got, want)
	}

	got = sshArgs(SSHSpec{Address: "app-prod", Alias: true, IdentityFile: "/home/eason/.ssh/other_key"})
	if !strings.Contains(strings.Join(got, " "), "-i /home/eason/.ssh/other_key") || got[len(got)-1] != "app-prod" {
		t.Errorf("alias+identity args = %v", got)
	}
	if strings.Contains(strings.Join(got, " "), "ConnectTimeout") {
		t.Errorf("alias args must never carry vsfleet's own ConnectTimeout: %v", got)
	}

	// A plain native address still gets it — nothing else pins one.
	got = sshArgs(SSHSpec{Address: "10.20.0.11"})
	if !strings.Contains(strings.Join(got, " "), "ConnectTimeout=15") {
		t.Errorf("native args should keep ConnectTimeout: %v", got)
	}
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// SSH refuses to combine an alias with vsfleet-generated proxy arguments —
// OpenSSH must own the alias's entire route, or not be used at all.
func TestSSHRefusesAliasWithProxyArgs(t *testing.T) {
	h := realHandoff{}
	_, err := h.SSH(SSHSpec{Address: "app-prod", Alias: true, ProxyArgs: []string{"-o", "ProxyCommand=nc -X connect -x proxy:8080 %h %p"}})
	if err == nil {
		t.Fatal("want a refusal combining an alias with a generated route")
	}
}

// directSSHArgs forces a route distinct from simply adding nothing:
// ~/.ssh/config's own ProxyJump/ProxyCommand for the address must be
// overridden, not merely left unaugmented.
func TestDirectRouteOverridesTheOperatorsOwnConfig(t *testing.T) {
	args, reason := routeOverrideArgs(sshRouteDirect, "")
	if reason != "" {
		t.Fatalf("reason = %q", reason)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "ProxyCommand=none") || !strings.Contains(joined, "ProxyJump=none") {
		t.Errorf("direct route args = %v", args)
	}
	// OpenSSH default is the deliberate opposite: it adds nothing at all.
	if args, _ := routeOverrideArgs(sshRouteOpenSSH, ""); len(args) != 0 {
		t.Errorf("openssh-default route args = %v, want none", args)
	}
}

// writeSSHShim installs a fake "ssh" on PATH (ahead of the real one) that
// answers "-G <alias>" the way OpenSSH would after resolving its config,
// without actually reading one — the parser (internal/sshalias) already has
// its own tests against real config text; this exercises only the
// validation half of DiscoverSSHDestinations. "app-hang" never returns,
// proving the per-call timeout — not just the overall budget — bounds it.
func writeSSHShim(t *testing.T, dir string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "-G" ]; then
  case "$2" in
    app-prod)
      echo "hostname 172.31.7.122"
      echo "user devops"
      ;;
    app-stale)
      echo "hostname 172.31.7.99"
      echo "user devops"
      ;;
    app-hang)
      sleep 30
      ;;
    *)
      echo "hostname $2"
      ;;
  esac
  exit 0
fi
exit 1
`
	path := filepath.Join(dir, "ssh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write ssh shim: %v", err)
	}
}

func withSSHShimAndHome(t *testing.T, home string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell shim not supported on windows")
	}
	shimDir := t.TempDir()
	writeSSHShim(t, shimDir)
	t.Setenv("HOME", home)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeUserSSHConfig(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A candidate the parser finds is only trusted once the shimmed "ssh -G"
// confirms its effective hostname, and a remembered alias that no longer
// matches is reported stale rather than silently kept.
func TestRealHandoffDiscoverSSHDestinationsValidatesCandidates(t *testing.T) {
	home := t.TempDir()
	withSSHShimAndHome(t, home)
	writeUserSSHConfig(t, home, `
Host app-prod
    HostName 172.31.7.122

Host app-stale
    HostName 172.31.7.122
`)
	// app-stale is a literal candidate the parser would surface too, but the
	// shim reports its effective hostname as .99 — proving the config text
	// alone is never trusted; only the shimmed "ssh -G" is.
	h := realHandoff{}
	aliases, stale, err := h.DiscoverSSHDestinations("172.31.7.122", "app-stale")
	if err != nil {
		t.Fatalf("DiscoverSSHDestinations: %v", err)
	}
	if !stale {
		t.Error("a remembered alias whose effective hostname no longer matches must be reported stale")
	}
	if len(aliases) != 1 || aliases[0] != "app-prod" {
		t.Errorf("aliases = %v, want only [app-prod]", aliases)
	}
}

func TestRealHandoffDiscoverSSHDestinationsNoMatch(t *testing.T) {
	home := t.TempDir()
	withSSHShimAndHome(t, home)
	writeUserSSHConfig(t, home, `
Host app-prod
    HostName 172.31.7.122
`)
	h := realHandoff{}
	aliases, stale, err := h.DiscoverSSHDestinations("10.0.0.1", "")
	if err != nil {
		t.Fatalf("DiscoverSSHDestinations: %v", err)
	}
	if stale {
		t.Error("nothing was remembered, so nothing can be stale")
	}
	if len(aliases) != 0 {
		t.Errorf("aliases = %v, want none", aliases)
	}
}

// A candidate whose "ssh -G" call hangs must not block discovery beyond the
// per-call timeout — proven here in isolation from the overall budget by
// giving the hang a longer sleep than either bound.
func TestRealHandoffDiscoverSSHDestinationsBoundsAHangingCandidate(t *testing.T) {
	home := t.TempDir()
	withSSHShimAndHome(t, home)
	writeUserSSHConfig(t, home, `
Host app-hang
    HostName 172.31.7.122

Host app-prod
    HostName 172.31.7.122
`)
	h := realHandoff{}
	start := time.Now()
	aliases, _, err := h.DiscoverSSHDestinations("172.31.7.122", "")
	if err != nil {
		t.Fatalf("DiscoverSSHDestinations: %v", err)
	}
	if elapsed := time.Since(start); elapsed > sshDiscoveryBudget+3*time.Second {
		t.Errorf("discovery took %s, want it bounded near the discovery budget", elapsed)
	}
	if len(aliases) != 1 || aliases[0] != "app-prod" {
		t.Errorf("aliases = %v, want the hanging candidate skipped and app-prod still found", aliases)
	}
}

func TestRealHandoffDiscoverSSHDestinationsRejectsANonIPTarget(t *testing.T) {
	h := realHandoff{}
	aliases, stale, err := h.DiscoverSSHDestinations("app01.internal", "")
	if aliases != nil || stale || err != nil {
		t.Errorf("a DNS name target must skip discovery entirely rather than resolve it: aliases=%v stale=%v err=%v", aliases, stale, err)
	}
}
