package credentials

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Prompt asks the operator for a password on the terminal. Nothing is
// persisted: the secret lives only for the current process.
type Prompt struct {
	In  io.Reader
	Out io.Writer
	// Label describes what is being asked for when the reference carries no
	// value of its own.
	Label string

	// buf is shared by every read so that a buffered line read cannot swallow
	// input meant for the next question.
	buf *bufio.Reader
}

// NewPrompt returns a Prompt bound to the process terminal.
func NewPrompt() *Prompt { return &Prompt{In: os.Stdin, Out: os.Stderr} }

func (p *Prompt) Scheme() string { return SchemePrompt }

func (p *Prompt) in() io.Reader {
	if p.In != nil {
		return p.In
	}
	return os.Stdin
}

func (p *Prompt) out() io.Writer {
	if p.Out != nil {
		return p.Out
	}
	return os.Stderr
}

func (p *Prompt) reader() *bufio.Reader {
	if p.buf == nil {
		p.buf = bufio.NewReader(p.in())
	}
	return p.buf
}

// ReadLine writes prompt to the output and reads one echoed line.
func (p *Prompt) ReadLine(prompt string) (string, error) {
	fmt.Fprint(p.out(), prompt)
	line, err := p.reader().ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Interactive reports whether a password can actually be read from the user.
func (p *Prompt) Interactive() bool {
	f, ok := p.in().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (p *Prompt) Get(_ context.Context, ref Ref) (Credential, error) {
	label := ref.Value
	if label == "" {
		label = p.Label
	}
	var prompt string
	if label == "" {
		prompt = "Password: "
	} else {
		prompt = fmt.Sprintf("Password for %s: ", label)
	}
	secret, err := p.ReadSecret(prompt)
	if err != nil {
		return Credential{}, err
	}
	return Credential{Password: secret}, nil
}

// ReadSecret writes prompt to the output and reads one line without echoing it.
func (p *Prompt) ReadSecret(prompt string) (string, error) {
	fmt.Fprint(p.out(), prompt)
	f, ok := p.in().(*os.File)
	if ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(p.out())
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(b), nil
	}
	// Not a terminal: read a line so the tool stays scriptable, but the input
	// is echoed by whatever is feeding it, which is the caller's choice.
	line, err := p.reader().ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (p *Prompt) Store(context.Context, Ref, Credential) error {
	return fmt.Errorf("%w: prompt", ErrUnsupported)
}

func (p *Prompt) Delete(context.Context, Ref) error {
	return fmt.Errorf("%w: prompt", ErrUnsupported)
}

// Resolve fetches a credential for ref, falling back to an interactive prompt
// when the reference resolves to nothing and a terminal is available. label
// names the thing being unlocked, typically the context name.
//
// It reports whether the credential came from the prompt, so callers can offer
// to persist it.
func Resolve(ctx context.Context, r *Resolver, ref Ref, label string) (c Credential, prompted bool, err error) {
	c, err = r.Get(ctx, ref)
	if err == nil {
		return c, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Credential{}, false, err
	}
	p, ok := r.Provider(SchemePrompt)
	if !ok {
		return Credential{}, false, err
	}
	pr, ok := p.(*Prompt)
	if ok && !pr.Interactive() {
		return Credential{}, false, err
	}
	c, perr := p.Get(ctx, Ref{Scheme: SchemePrompt, Value: label})
	if perr != nil {
		return Credential{}, false, perr
	}
	return c, true, nil
}
