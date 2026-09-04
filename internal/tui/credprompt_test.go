package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
)

// getOutcome is what a background credentials.Resolve-style call to
// PromptCoordinator.Get eventually returns.
type getOutcome struct {
	cred credentials.Credential
	err  error
}

// askInBackground calls coord.Get the way session.Manager.Connect ->
// credentials.Resolve does today: from a goroutine racing the interface for
// the terminal, which is exactly the setup issue #26 reproduces.
func askInBackground(ctx context.Context, coord *PromptCoordinator, label string) <-chan getOutcome {
	out := make(chan getOutcome, 1)
	go func() {
		cred, err := coord.Get(ctx, credentials.Ref{Scheme: credentials.SchemePrompt, Value: label})
		out <- getOutcome{cred, err}
	}()
	return out
}

// TestCredPromptOwnsKeystrokes reproduces issue #26: while a background load
// is waiting on a "prompt" credential, "q" — vsfleet's quit shortcut
// everywhere else — must reach the password field instead of being handled
// as a global shortcut, and the answer typed there must be what the
// background caller receives.
func TestCredPromptOwnsKeystrokes(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab"}}}
	coord := NewPromptCoordinator()
	m := New(context.Background(), b, Options{Credentials: coord})
	m.width, m.height = 140, 30

	listenCmd := m.nextCredPromptCmd()
	if listenCmd == nil {
		t.Fatal("nextCredPromptCmd returned nil with a coordinator configured")
	}

	results := askInBackground(context.Background(), coord, "lab")

	msg := listenCmd()
	reqMsg, ok := msg.(credRequestMsg)
	if !ok {
		t.Fatalf("listenForCredRequest produced %T, want credRequestMsg", msg)
	}
	if _, cmd := m.Update(reqMsg); cmd != nil {
		t.Fatal("credRequestMsg should not itself produce a command")
	}
	if m.credPrompt == nil {
		t.Fatal("expected the credential overlay to be showing")
	}
	// A real half-second cursor-blink timer would otherwise make this test
	// synchronously wait on it, the same reason newTestModel disables it on
	// every other text field.
	m.credPrompt.input.Cursor.SetMode(cursor.CursorStatic)

	press := func(r rune) {
		t.Helper()
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd != nil {
			cmd()
		}
	}

	press('q')
	if m.quitting {
		t.Fatal(`"q" quit the program instead of reaching the credential field`)
	}
	if m.credPrompt == nil {
		t.Fatal(`"q" closed the overlay instead of being typed into it`)
	}
	if got := m.credPrompt.input.Value(); got != "q" {
		t.Fatalf("password field = %q, want %q", got, "q")
	}

	for _, r := range "secret" {
		press(r)
	}
	if got := m.credPrompt.input.Value(); got != "qsecret" {
		t.Fatalf("password field = %q, want %q", got, "qsecret")
	}

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("expected enter to re-arm the listener for the next request")
	}
	if m.credPrompt != nil {
		t.Fatal("expected the overlay to close once enter answers it")
	}

	select {
	case res := <-results:
		if res.err != nil {
			t.Fatalf("Get returned error %v", res.err)
		}
		if res.cred.Password != "qsecret" {
			t.Fatalf("Get returned password %q, want %q", res.cred.Password, "qsecret")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the background Get() call never unblocked")
	}
}

// TestCredPromptEscCancelsWithoutQuitting covers esc: it answers the pending
// request with cancellation, closes the overlay, and re-arms the listener,
// but — unlike ctrl+c — leaves the interface running.
func TestCredPromptEscCancelsWithoutQuitting(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab"}}}
	coord := NewPromptCoordinator()
	m := New(context.Background(), b, Options{Credentials: coord})

	results := askInBackground(context.Background(), coord, "lab")
	reqMsg := m.nextCredPromptCmd()().(credRequestMsg)
	m.Update(reqMsg)
	if m.credPrompt == nil {
		t.Fatal("expected the credential overlay to be showing")
	}

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil {
		t.Fatal("expected esc to re-arm the listener")
	}
	if m.credPrompt != nil {
		t.Fatal("expected esc to close the overlay")
	}
	if m.quitting {
		t.Fatal("esc must cancel the prompt without quitting the interface")
	}

	select {
	case res := <-results:
		if res.err == nil {
			t.Fatal("expected Get to report cancellation, got a credential")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the background Get() call never unblocked")
	}
}

// TestCredPromptCtrlCCancelsAndQuits covers ctrl+c: it retains its usual
// meaning of quitting the interface outright, but must still answer the
// stuck background caller rather than leaving it blocked forever.
func TestCredPromptCtrlCCancelsAndQuits(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab"}}}
	coord := NewPromptCoordinator()
	m := New(context.Background(), b, Options{Credentials: coord})

	results := askInBackground(context.Background(), coord, "lab")
	reqMsg := m.nextCredPromptCmd()().(credRequestMsg)
	m.Update(reqMsg)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected ctrl+c to return tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected ctrl+c to quit the program")
	}
	if !m.quitting {
		t.Fatal("expected ctrl+c to set quitting")
	}

	select {
	case res := <-results:
		if res.err == nil {
			t.Fatal("expected Get to report cancellation, got a credential")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctrl+c left the background Get() call blocked")
	}
}

// TestCredPromptSerializesConcurrentAskers covers a second context also
// using a prompt credential: its request must not be shown until the first
// is resolved, so the two overlays never race for the screen.
func TestCredPromptSerializesConcurrentAskers(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab-a"}, {Name: "lab-b"}}}
	coord := NewPromptCoordinator()
	m := New(context.Background(), b, Options{Credentials: coord})

	resultsA := askInBackground(context.Background(), coord, "lab-a")
	reqMsg := m.nextCredPromptCmd()().(credRequestMsg)
	m.Update(reqMsg)
	if got := m.credPrompt.label; got != "lab-a" {
		t.Fatalf("first overlay label = %q, want %q", got, "lab-a")
	}

	resultsB := askInBackground(context.Background(), coord, "lab-b")

	// The second asker has nobody listening yet — the model only rearms
	// after answering the first — so it must not have been able to hand off
	// its request.
	select {
	case <-resultsB:
		t.Fatal("second asker resolved before the first overlay was answered")
	case <-time.After(50 * time.Millisecond):
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter to re-arm the listener")
	}

	select {
	case res := <-resultsA:
		if res.err != nil {
			t.Fatalf("first asker returned error %v", res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first asker never unblocked")
	}

	reqMsg2, ok := cmd().(credRequestMsg)
	if !ok {
		t.Fatalf("expected the re-armed listener to pick up the queued request, got %T", reqMsg2)
	}
	m.Update(reqMsg2)
	if got := m.credPrompt.label; got != "lab-b" {
		t.Fatalf("second overlay label = %q, want %q", got, "lab-b")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case res := <-resultsB:
		if res.err != nil {
			t.Fatalf("second asker returned error %v", res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second asker never unblocked")
	}
}

func TestCredPromptMarksTheSelectedContextAsCredentialsRequired(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab"}}}
	m := New(context.Background(), b, Options{Credentials: NewPromptCoordinator()})
	st := m.byName["lab"]
	if m.beginLoad(st, false, false, true) == nil {
		t.Fatal("expected the selected context to begin loading")
	}
	request := credRequest{label: "lab", resp: make(chan credResult, 1)}
	m.Update(credRequestMsg{req: request})

	if st.phase != phaseCredentials {
		t.Fatalf("context phase = %q, want %q", st.phase, phaseCredentials)
	}
	if m.credPrompt == nil || !strings.Contains(strings.Join(m.viewCredPrompt(), "\n"), "Password for lab") {
		t.Fatal("the credential request did not open a context-labelled overlay")
	}
	m.resolveCredPrompt(credResult{err: errPromptCanceled})
}

// TestStartupCredentialRequestWaitsForExplicitLoad covers the startup policy:
// automatic keyring/session work is allowed, but crossing an interactive
// credential boundary leaves the normal pane usable. Reloading is the explicit
// action that may then open the masked password overlay.
func TestStartupCredentialRequestWaitsForExplicitLoad(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab"}}}
	m := New(context.Background(), b, Options{Credentials: NewPromptCoordinator()})
	m.width, m.height = 120, 30
	st := m.byName["lab"]

	if cmds := m.ensureSelectedLoadedAtStartup(); len(cmds) == 0 {
		t.Fatal("expected startup to try the selected context")
	}
	if st.allowCredentialPrompt {
		t.Fatal("startup load must not be allowed to open a password overlay")
	}

	request := credRequest{label: "lab", resp: make(chan credResult, 1)}
	m.Update(credRequestMsg{req: request})
	if m.credPrompt != nil {
		t.Fatal("startup credential request opened an interactive prompt")
	}
	res := <-request.resp
	if !errors.Is(res.err, errDeferredCredentialPrompt) {
		t.Fatalf("startup credential response = %v, want %v", res.err, errDeferredCredentialPrompt)
	}

	m.Update(beginInventoryMsg{
		context: "lab", cc: st.cc, generation: st.generation, err: res.err,
	})
	if st.loading || !st.credentialsRequired() {
		t.Fatalf("startup state = loading:%v credentials-required:%v", st.loading, st.credentialsRequired())
	}
	out := m.View()
	if !strings.Contains(out, "credentials required · press r to connect") {
		t.Fatalf("startup pane does not explain how to continue:\n%s", out)
	}
	if strings.Contains(out, "1 failed") {
		t.Fatalf("a deferred startup credential was counted as a failed vCenter:\n%s", out)
	}
	if cmd := m.enterScope(); cmd != nil {
		t.Fatal("a presentation-only scope change retried the deferred credential")
	}

	if cmds := m.ensureSelectedLoaded(false); len(cmds) == 0 {
		t.Fatal("explicit reload did not retry the waiting context")
	}
	if !st.allowCredentialPrompt {
		t.Fatal("explicit reload should permit the credential overlay")
	}
	request = credRequest{label: "lab", resp: make(chan credResult, 1)}
	m.Update(credRequestMsg{req: request})
	if m.credPrompt == nil {
		t.Fatal("explicit reload did not open the credential overlay")
	}
	m.resolveCredPrompt(credResult{err: errPromptCanceled})
}

func TestBackgroundCredentialRequestDoesNotOpenPrompt(t *testing.T) {
	b := &fakeBackend{contexts: []*config.Context{{Name: "lab"}}}
	m := New(context.Background(), b, Options{Credentials: NewPromptCoordinator()})
	st := m.byName["lab"]
	if m.beginLoad(st, true, true, false) == nil {
		t.Fatal("expected a quiet refresh to begin loading")
	}
	request := credRequest{label: "lab", resp: make(chan credResult, 1)}
	_, cmd := m.Update(credRequestMsg{req: request})

	if m.credPrompt != nil {
		t.Fatal("a background refresh opened an interactive credential prompt")
	}
	if !st.credentialPrompted || st.phase != phaseCredentials {
		t.Fatalf("background credential state = prompted:%v phase:%q, want true/%q", st.credentialPrompted, st.phase, phaseCredentials)
	}
	select {
	case res := <-request.resp:
		if !errors.Is(res.err, errDeferredCredentialPrompt) {
			t.Fatalf("background credential response = %v, want %v", res.err, errDeferredCredentialPrompt)
		}
	default:
		t.Fatal("background credential request was not released")
	}
	if cmd == nil {
		t.Fatal("credential listener was not re-armed after deferring the background request")
	}
}

func TestBackgroundRefreshDoesNotRepeatCredentialPrompt(t *testing.T) {
	b := twoHealthy()
	m := newTestModel(t, b, Options{Current: "prod"})
	m.refreshInterval = time.Minute
	st := m.byName["customer-a"]
	st.attempted = true
	st.credentialPrompted = true
	st.phase = phaseCredentials
	st.err = errPromptCanceled

	tickRefresh(t, m)
	if got := b.calls["customer-a"]; got != 0 {
		t.Fatalf("background refresh retried a context waiting for credentials %d times", got)
	}

	for i, candidate := range m.states {
		if candidate == st {
			m.ctxCursor = i
			break
		}
	}
	drive(t, m, m.useContext())
	if got := b.calls["customer-a"]; got != 1 {
		t.Fatalf("selecting the context did not retry it exactly once; calls = %d", got)
	}
	if st.credentialPrompted {
		t.Fatal("a successful explicit retry left the credential gate armed")
	}
}
