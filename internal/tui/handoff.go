package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/muesli/termenv"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/sshalias"
)

// Handoff is everything the detail pane's field-cursor actions need from the
// operator's own workstation: a clipboard, a browser, and a terminal to hand
// off to. It exists for the same reason Backend does (see backend.go) — a
// feature that only launches real external processes is a feature nobody can
// exercise from a test, and "vsfleet demo dials nothing" has to keep meaning
// that once actions like these exist. Model.actionsFor checks Options.Demo
// before ever calling this interface, rather than routing demo mode through
// a disabled implementation of it — one flag check is simpler than a second
// Handoff that does nothing.
type Handoff interface {
	// Copy writes value to the operator's clipboard.
	Copy(value string) error
	// OpenURL launches the operator's default browser on url.
	OpenURL(url string) error
	// SSH builds the command an ssh session to spec would run. It does not
	// run it — the TUI execution adapter owns that, so the alternate screen Bubble Tea
	// set up (see run.go) is suspended and restored around it correctly —
	// which also means this method is the entire testable surface of SSH.
	SSH(spec SSHSpec) (*exec.Cmd, error)
	// DiscoverSSHIdentities returns existing private-key paths relevant to
	// address. It never reads key material; paths come from ssh -G and the
	// conventional top-level files in ~/.ssh.
	DiscoverSSHIdentities(address string) ([]SSHIdentity, error)
	// ResolveUser reports the remote user ssh(1) would pick for address
	// given the operator's own ~/.ssh/config — "" when it cannot tell. It is
	// for display only: the SSH command keeps an empty SSHSpec.User so
	// ssh(1) stays the authority on the answer, and this is just a way to
	// tell the operator what that answer will be before they connect.
	ResolveUser(address string) string
	// DiscoverSSHDestinations finds OpenSSH Host aliases that already reach
	// targetIP. sshalias.Discover produces literal candidates from
	// ~/.ssh/config and its Include files — text it never trusts on its
	// own — and each candidate is independently confirmed with a bounded
	// "ssh -G": only a candidate whose effective hostname equals targetIP is
	// returned. remembered, if non-empty, is always included in that
	// validation even when the parser did not surface it (the operator's
	// config may have changed since it was chosen), and staleRemembered
	// reports that it was checked and definitively no longer matches —
	// never that the check merely failed or timed out, which the caller
	// must not treat as proof of anything. err carries a non-fatal
	// discovery problem (an unreadable config file, a discovery limit
	// reached) alongside whatever candidates were still found.
	DiscoverSSHDestinations(targetIP, remembered string) (aliases []string, staleRemembered bool, err error)
}

// SSHSpec fully describes one SSH handoff before it becomes a command:
// everything actionsFor derived from the row, the context's route, and the
// configured default user, with nothing left for SSH itself to resolve
// beyond what it always resolves — ~/.ssh/config and the local username.
type SSHSpec struct {
	// Address is the VM's guest IP or the host's registered name.
	Address string
	// User is the configured default; empty leaves ssh(1) to pick one.
	User string
	// IdentityFile is an explicitly selected private-key path. Empty leaves
	// OpenSSH to use its normal config and agent resolution.
	IdentityFile string
	// ProxyArgs are extra ssh(1) arguments — "-o", "ProxyCommand=..." — that
	// route the connection through the same bastion vsfleet itself uses to
	// reach this context, or that force a per-VM route override. Empty means
	// a direct route. Always empty when Alias is set — see Alias.
	ProxyArgs []string
	// Alias marks Address as an OpenSSH Host alias rather than a guest DNS
	// name or IP: the alias already validated (see
	// Handoff.DiscoverSSHDestinations) as reaching this machine, and OpenSSH
	// itself — not vsfleet — owns its ProxyJump, ProxyCommand, Port and any
	// other route it applies. SSH refuses a spec that sets both Alias and
	// ProxyArgs, and sshArgs omits the ConnectTimeout option it otherwise
	// adds, so the generated command stays as close as possible to plainly
	// running "ssh <alias>".
	Alias bool
}

// SSHIdentity is a private-key path offered by the identity picker.
type SSHIdentity struct {
	Path string
}

// realHandoff is the production Handoff: a real clipboard, a real browser, a
// real ssh(1).
type realHandoff struct{}

// Copy writes value with the OSC 52 terminal escape and, as a courtesy,
// through the local clipboard command. OSC 52 is a request the terminal
// never acknowledges — there is no reply to wait for — so it is the one
// half of "copy" that still works when vsfleet is running over SSH or inside
// tmux, which is precisely where this feature is meant to be used, and it is
// written unconditionally rather than only as a fallback. The local command
// usually succeeds silently on a desktop session and fails silently (no
// xclip, no pbcopy) on a headless jump host; either way that is not this
// call's problem to report, so its error is discarded.
func (realHandoff) Copy(value string) error {
	return copyValue(os.Stdout, value)
}

// copyValue is Copy's body, taking the output stream explicitly so a test
// can capture the OSC 52 escape sequence instead of writing to the real
// terminal.
func copyValue(w io.Writer, value string) error {
	if w == nil {
		w = os.Stdout
	}
	termenv.NewOutput(w).Copy(value)
	_ = clipboard.WriteAll(value)
	return nil
}

// OpenURL launches the operator's default browser without blocking the
// interface on it; a goroutine reaps the process once the browser (which
// usually forks and returns immediately) exits, so it never becomes a
// zombie.
func (realHandoff) OpenURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// SSH builds the ssh(1) invocation for spec. The TUI execution adapter owns
// its stdio and terminal handoff.
func (realHandoff) SSH(spec SSHSpec) (*exec.Cmd, error) {
	if spec.Address == "" {
		return nil, errors.New("nothing to connect to")
	}
	if !validSSHHost(spec.Address) {
		return nil, fmt.Errorf("refusing to ssh to %q", spec.Address)
	}
	if spec.User != "" && !validSSHUser(spec.User) {
		return nil, fmt.Errorf("refusing to ssh as %q", spec.User)
	}
	if spec.Alias && len(spec.ProxyArgs) > 0 {
		return nil, fmt.Errorf("refusing to combine OpenSSH alias %q with a vsfleet-generated route", spec.Address)
	}
	if spec.IdentityFile != "" {
		path, err := expandSSHPath(spec.IdentityFile)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("SSH identity %q: %w", displaySSHPath(path), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("SSH identity %q is not a regular file", displaySSHPath(path))
		}
		spec.IdentityFile = path
	}
	if len(spec.ProxyArgs) > 0 {
		if err := checkNetcat(); err != nil {
			return nil, err
		}
	}
	return exec.Command("ssh", sshArgs(spec)...), nil
}

// DiscoverSSHIdentities asks ssh for target-specific IdentityFile entries and
// supplements them with conventional private-key names in ~/.ssh. A failure
// to resolve ssh configuration is returned alongside any local candidates so
// the picker still offers manual entry and the OpenSSH default.
func (realHandoff) DiscoverSSHIdentities(address string) ([]SSHIdentity, error) {
	if !validSSHHost(address) {
		return nil, fmt.Errorf("refusing to inspect SSH configuration for %q", address)
	}
	var identities []SSHIdentity
	var firstErr error
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-G", address).Output()
	if err != nil {
		firstErr = err
	} else {
		for _, path := range parseSSHIdentityFiles(string(out)) {
			if expanded, ok := existingSSHIdentity(path); ok {
				identities = appendUniqueSSHIdentity(identities, expanded)
			}
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		entries, readErr := os.ReadDir(filepath.Join(home, ".ssh"))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) && firstErr == nil {
			firstErr = readErr
		}
		var local []string
		for _, entry := range entries {
			if entry.IsDir() || !conventionalSSHIdentity(entry.Name()) {
				continue
			}
			local = append(local, filepath.Join(home, ".ssh", entry.Name()))
		}
		sort.Strings(local)
		for _, path := range local {
			if expanded, ok := existingSSHIdentity(path); ok {
				identities = appendUniqueSSHIdentity(identities, expanded)
			}
		}
	}
	return identities, firstErr
}

// ResolveUser asks ssh(1) itself, through "ssh -G", which prints the fully
// resolved configuration for a destination — every Host and Match block
// applied — without connecting. The timeout bounds a Match exec that hangs;
// any failure, including no ssh on PATH, simply yields "".
func (realHandoff) ResolveUser(address string) string {
	if !validSSHHost(address) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-G", address).Output()
	if err != nil {
		return ""
	}
	return parseSSHUser(string(out))
}

// parseSSHUser finds the "user" line in "ssh -G" output.
func parseSSHUser(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if user, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "user "); ok {
			return strings.TrimSpace(user)
		}
	}
	return ""
}

// parseSSHHostname finds the "hostname" line in "ssh -G" output — the
// effective destination address after every Host and Match block has been
// applied. This, not the config text a Host block quotes, is what
// DiscoverSSHDestinations trusts.
func parseSSHHostname(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if host, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "hostname "); ok {
			return strings.TrimSpace(host)
		}
	}
	return ""
}

// sshDiscoveryBudget bounds the total time DiscoverSSHDestinations spends
// running "ssh -G" across every candidate. A config with many candidates
// stops being validated once the budget is spent rather than running one
// more bounded call per candidate indefinitely — this is called
// synchronously while building the detail pane's action list, so a slow
// config must not make opening a VM feel unresponsive.
const sshDiscoveryBudget = 5 * time.Second

// DiscoverSSHDestinations implements Handoff.DiscoverSSHDestinations. See
// that doc comment for the contract; this is the "candidate enumeration,
// then bounded ssh -G" pipeline the issue's design section describes.
func (realHandoff) DiscoverSSHDestinations(targetIP, remembered string) ([]string, bool, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(targetIP))
	if err != nil {
		// Discovery only makes sense against a literal IP: a DNS name would
		// need to be resolved first, and that is exactly the kind of
		// network probe this feature promises never to add.
		return nil, false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, err
	}
	candidates, discErr := sshalias.Discover(home, ip, sshalias.DefaultLimits())
	ordered := candidates
	if remembered != "" {
		ordered = append([]string{remembered}, candidates...)
	}

	budget, cancel := context.WithTimeout(context.Background(), sshDiscoveryBudget)
	defer cancel()

	var aliases []string
	staleRemembered := false
	seen := map[string]bool{}
	for _, alias := range ordered {
		if seen[alias] || !validSSHHost(alias) {
			continue
		}
		seen[alias] = true
		if budget.Err() != nil {
			break
		}
		callCtx, callCancel := context.WithTimeout(budget, 2*time.Second)
		cmd := exec.CommandContext(callCtx, "ssh", "-G", alias)
		// A "Match exec" directive (see ssh_config(5)) can run a command
		// that backgrounds a grandchild holding ssh's own stdout open;
		// without WaitDelay, Output() would keep reading from that pipe
		// long after ssh itself was killed for exceeding callCtx, and the
		// timeout above would bound nothing. WaitDelay forces the pipes
		// closed shortly after the process is signaled, so this call is
		// bounded by callCtx regardless of what it runs.
		cmd.WaitDelay = 500 * time.Millisecond
		out, err := cmd.Output()
		callCancel()
		if err != nil {
			// Transient — a timeout, or ssh(1) itself failing — proves
			// nothing either way, so a remembered alias is not discarded
			// over it; only a confirmed mismatch does that.
			continue
		}
		hostAddr, parseErr := netip.ParseAddr(parseSSHHostname(string(out)))
		switch {
		case parseErr == nil && hostAddr.Unmap() == ip:
			aliases = append(aliases, alias)
		case alias == remembered:
			staleRemembered = true
		}
	}
	return aliases, staleRemembered, discErr
}

func parseSSHIdentityFiles(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if path, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "identityfile "); ok {
			path = strings.TrimSpace(path)
			if path != "" && !strings.Contains(path, "%") {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func appendUniqueSSHIdentity(items []SSHIdentity, path string) []SSHIdentity {
	path, err := expandSSHPath(path)
	if err != nil {
		return items
	}
	for _, item := range items {
		if item.Path == path {
			return items
		}
	}
	return append(items, SSHIdentity{Path: path})
}

func existingSSHIdentity(path string) (string, bool) {
	expanded, err := expandSSHPath(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(expanded)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return expanded, true
}

func conventionalSSHIdentity(name string) bool {
	if strings.HasSuffix(name, ".pub") || strings.HasSuffix(name, "-cert.pub") {
		return false
	}
	return strings.HasPrefix(name, "id_") || strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key")
}

func expandSSHPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("SSH identity path is empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("SSH identity path %q uses an unsupported home expansion", path)
	}
	return filepath.Clean(path), nil
}

func displaySSHPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return path
}

// validSSHHost and validSSHUser guard the two strings that reach ssh(1)'s
// argument list. Either can originate with someone other than the operator —
// a guest chooses its own reported name, and a remembered user is read back
// from a file — and ssh(1) reads a leading "-" as an option, which would make
// "-oProxyCommand=..." a way to run a command. Whitespace and control
// characters have no place in either.
func validSSHHost(s string) bool {
	return safeSSHToken(s) && !strings.ContainsRune(s, '@')
}

func validSSHUser(s string) bool { return safeSSHToken(s) }

func safeSSHToken(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// checkNetcat verifies the feature set required by the generated
// ProxyCommand. Different netcat implementations use different proxy flags;
// failing before SSH starts gives the operator a useful explanation instead
// of another opaque exit status 255.
func checkNetcat() error {
	path, err := exec.LookPath("nc")
	if err != nil {
		return errors.New("SSH proxy requires nc with -X and -x support")
	}
	out, _ := exec.Command(path, "-h").CombinedOutput()
	help := string(out)
	if !strings.Contains(help, "-X") || !strings.Contains(help, "-x") {
		return errors.New("SSH proxy requires a netcat implementation with -X and -x support")
	}
	return nil
}

// proxyArgs turns a context's network route into extra ssh(1) arguments, so
// SSH follows the same path vsfleet itself takes to reach whatever sits
// behind it — an ESXi host or a VM behind a bastion is usually only
// reachable that way. An authenticated proxy is declined outright rather
// than attempted: handing its keyring password to ssh's ProxyCommand would
// put it on this process's command line, visible to anyone who can run "ps"
// on this machine, and that trade needs its own deliberate decision rather
// than shipping as a side effect of this feature.
// directSSHArgs forces a direct connection regardless of the operator's own
// ~/.ssh/config, for the per-VM "Direct" route override — distinct from
// leaving spec.ProxyArgs empty, which merely adds nothing of vsfleet's own
// and still lets ~/.ssh/config apply its own ProxyJump or ProxyCommand.
func directSSHArgs() []string {
	return []string{"-o", "ProxyCommand=none", "-o", "ProxyJump=none"}
}

// routeOverrideArgs turns a per-VM remembered route override into extra
// ssh(1) arguments, the same way proxyArgs turns a context's transport into
// one. "openssh" reports no VSFleet-owned arguments at all — the deliberate
// difference from "direct" is documented on directSSHArgs.
func routeOverrideArgs(routeKind, proxyAddress string) (args []string, disabledReason string) {
	switch routeKind {
	case sshRouteOpenSSH, "":
		return nil, ""
	case sshRouteDirect:
		return directSSHArgs(), ""
	case config.TransportSOCKS5, config.TransportHTTPProxy:
		return proxyArgs(config.TransportConfig{Type: routeKind, Address: proxyAddress})
	default:
		return nil, ""
	}
}

func proxyArgs(t config.TransportConfig) (args []string, disabledReason string) {
	if t.Username != "" {
		return nil, "authenticated proxy — SSH does not support one yet"
	}
	switch t.Type {
	case config.TransportSOCKS5:
		return []string{"-o", "ProxyCommand=nc -X 5 -x " + t.Address + " %h %p"}, ""
	case config.TransportHTTPProxy:
		return []string{"-o", "ProxyCommand=nc -X connect -x " + t.Address + " %h %p"}, ""
	case config.TransportHTTPSProxy:
		return nil, "HTTPS proxy — SSH has no TLS-capable ProxyCommand configured"
	default:
		return nil, ""
	}
}

// tailBuffer keeps only the end of a subprocess diagnostic. SSH remains
// interactive because the command's stderr is also written to the terminal,
// while the bounded copy survives Bubble Tea restoring its alternate screen.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (w *tailBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) >= w.max {
		w.b = append(w.b[:0], p[len(p)-w.max:]...)
		return len(p), nil
	}
	if len(w.b)+len(p) > w.max {
		w.b = append(w.b[:0], w.b[len(w.b)+len(p)-w.max:]...)
	}
	w.b = append(w.b, p...)
	return len(p), nil
}

func (w *tailBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.b)
}

// isDirect reports whether t reaches its vCenter without a proxy — and so
// whether the operator's own browser has any route to an object behind it.
// A proxied context is exactly the case issue #84 did not account for: the
// browser cannot follow a SOCKS5 or HTTP CONNECT route vsfleet itself was
// configured to use, so an "open in browser" action has to decline instead
// of silently doing nothing.
func isDirect(t config.TransportConfig) bool {
	switch t.Type {
	case config.TransportDirect, "":
		return true
	default:
		return false
	}
}
