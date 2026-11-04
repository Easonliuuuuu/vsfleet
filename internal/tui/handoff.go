package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"unicode"

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
	// SSH builds "ssh <address>". It adds no options of its own: the
	// operator's ~/.ssh/config decides the user, key, port and route. It
	// does not run the command — the TUI execution adapter owns that, so the
	// alternate screen Bubble Tea set up (see run.go) is suspended and
	// restored around it correctly.
	SSH(address string) (*exec.Cmd, error)
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

// SSH builds the ssh(1) invocation for address.
func (realHandoff) SSH(address string) (*exec.Cmd, error) {
	if address == "" {
		return nil, errors.New("nothing to connect to")
	}
	if !validSSHHost(address) {
		return nil, fmt.Errorf("refusing to ssh to %q", address)
	}
	return exec.Command("ssh", address), nil
}

// validSSHHost guards the one string that reaches ssh(1)'s argument list. A
// guest chooses its own reported name, and ssh(1) reads a leading "-" as an
// option, which would make "-oProxyCommand=..." a way to run a command. "@"
// would be read as a user, and whitespace and control characters have no
// place in a host.
func validSSHHost(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsRune(s, '@') {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
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
