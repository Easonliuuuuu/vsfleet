package tui

import (
	"context"
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/credentials"
)

// errPromptCanceled is what a background load gets back when the operator
// dismisses the credential overlay instead of answering it.
var errPromptCanceled = errors.New("credential entry canceled")

// credRequest is one pending ask for a password, raised by a background
// inventory or connection load that hit a "prompt" credential reference.
// resp is buffered by one so the model can always answer it without
// blocking, even if the asker already gave up (its own context canceled
// first): the send just lands on a channel nobody reads again.
type credRequest struct {
	label string
	resp  chan credResult
}

// credResult is what a credRequest resolves to: either a password or the
// reason none was given.
type credResult struct {
	password string
	err      error
}

// credRequestMsg carries a pending credRequest into Update, where it becomes
// the credential overlay.
type credRequestMsg struct{ req credRequest }

// PromptCoordinator is the "prompt" credentials.Provider used by the
// terminal interface in place of credentials.Prompt.
//
// A background tea.Cmd cannot read a password from os.Stdin the way the
// command line does: Bubble Tea already owns the terminal, and a second
// reader racing it for the same input is exactly what let a keystroke meant
// for a password field reach a global shortcut instead (or vice versa).
// PromptCoordinator instead hands the request to the Model as an ordinary
// message and blocks until the operator answers it through a normal text
// field, the same way every other input in the interface works.
//
// reqCh is unbuffered, which is what serializes concurrent askers: the
// Model only receives another request after it has finished showing the
// previous one, so a second background load asking for a password simply
// waits its turn instead of racing the first for the screen.
type PromptCoordinator struct {
	reqCh chan credRequest
}

// NewPromptCoordinator returns a PromptCoordinator ready to register on a
// credentials.Resolver via SetProvider, and to pass to tui.Run as
// Options.Credentials so the Model can answer what it asks.
func NewPromptCoordinator() *PromptCoordinator {
	return &PromptCoordinator{reqCh: make(chan credRequest)}
}

// Scheme implements credentials.Provider.
func (p *PromptCoordinator) Scheme() string { return credentials.SchemePrompt }

// Get implements credentials.Provider by asking the Model for a password and
// waiting for either an answer or the caller's context to end.
func (p *PromptCoordinator) Get(ctx context.Context, ref credentials.Ref) (credentials.Credential, error) {
	req := credRequest{label: ref.Value, resp: make(chan credResult, 1)}
	select {
	case p.reqCh <- req:
	case <-ctx.Done():
		return credentials.Credential{}, ctx.Err()
	}
	select {
	case res := <-req.resp:
		if res.err != nil {
			return credentials.Credential{}, res.err
		}
		return credentials.Credential{Password: res.password}, nil
	case <-ctx.Done():
		return credentials.Credential{}, ctx.Err()
	}
}

// Store implements credentials.Provider. Persisting a password typed into
// the overlay is out of scope here — saving a context already goes through
// the ordinary form, which offers to store it explicitly.
func (p *PromptCoordinator) Store(context.Context, credentials.Ref, credentials.Credential) error {
	return errors.New("operation not supported by credential provider: prompt")
}

// Delete implements credentials.Provider.
func (p *PromptCoordinator) Delete(context.Context, credentials.Ref) error {
	return errors.New("operation not supported by credential provider: prompt")
}

// listenForCredRequest is the tea.Cmd that waits for the next password ask.
// The model reissues it every time it finishes with one request, which is
// what makes reqCh's blocking receive serialize prompts one at a time
// instead of several overlays racing to be shown.
func listenForCredRequest(reqCh <-chan credRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-reqCh
		if !ok {
			return nil
		}
		return credRequestMsg{req: req}
	}
}

// credPromptState is the credential overlay's own state while it owns the
// keyboard.
type credPromptState struct {
	label string
	input textinput.Model
	resp  chan credResult
}

func newCredPromptState(req credRequest) *credPromptState {
	ti := newFormSecret(40)
	ti.Placeholder = "password"
	ti.Focus()
	return &credPromptState{label: req.label, input: ti, resp: req.resp}
}
