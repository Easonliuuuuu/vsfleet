package tui

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/atotto/clipboard"
	"github.com/muesli/termenv"

	"github.com/easonliuuuuu/vsfleet/internal/config"
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
	// run it — tea.ExecProcess owns that, so the alternate screen Bubble Tea
	// set up (see run.go) is suspended and restored around it correctly —
	// which also means this method is the entire testable surface of SSH.
	SSH(spec SSHSpec) (*exec.Cmd, error)
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
	// ProxyArgs are extra ssh(1) arguments — "-o", "ProxyCommand=..." — that
	// route the connection through the same bastion vsfleet itself uses to
	// reach this context. Empty means a direct route.
	ProxyArgs []string
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

// SSH builds the ssh(1) invocation for spec, inheriting the program's own
// stdio so tea.ExecProcess can hand the whole terminal to it.
func (realHandoff) SSH(spec SSHSpec) (*exec.Cmd, error) {
	if spec.Address == "" {
		return nil, errors.New("nothing to connect to")
	}
	target := spec.Address
	if spec.User != "" {
		target = spec.User + "@" + spec.Address
	}
	args := append([]string{}, spec.ProxyArgs...)
	args = append(args, target)
	cmd := exec.Command("ssh", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd, nil
}

// proxyArgs turns a context's network route into extra ssh(1) arguments, so
// SSH follows the same path vsfleet itself takes to reach whatever sits
// behind it — an ESXi host or a VM behind a bastion is usually only
// reachable that way. An authenticated proxy is declined outright rather
// than attempted: handing its keyring password to ssh's ProxyCommand would
// put it on this process's command line, visible to anyone who can run "ps"
// on this machine, and that trade needs its own deliberate decision rather
// than shipping as a side effect of this feature.
func proxyArgs(t config.TransportConfig) (args []string, disabledReason string) {
	if t.Username != "" {
		return nil, "authenticated proxy — SSH does not support one yet"
	}
	switch t.Type {
	case config.TransportSOCKS5:
		return []string{"-o", "ProxyCommand=nc -X 5 -x " + t.Address + " %h %p"}, ""
	case config.TransportHTTPProxy, config.TransportHTTPSProxy:
		return []string{"-o", "ProxyCommand=nc -X connect -x " + t.Address + " %h %p"}, ""
	default:
		return nil, ""
	}
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
