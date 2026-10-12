package tests

import (
	"context"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/tui"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// answeringPrompt types testPassword each time it is asked, and counts.
type answeringPrompt struct{ asked int }

func (p *answeringPrompt) Scheme() string { return credentials.SchemePrompt }
func (p *answeringPrompt) Get(context.Context, credentials.Ref) (credentials.Credential, error) {
	p.asked++
	return credentials.Credential{Password: testPassword}, nil
}
func (p *answeringPrompt) Store(context.Context, credentials.Ref, credentials.Credential) error {
	return nil
}
func (p *answeringPrompt) Delete(context.Context, credentials.Ref) error { return nil }

// TestDiagnosingAConnectedPromptContextDoesNotAskAgain pins that the password
// an operator typed to connect is not asked for a second time when they
// diagnose the same, still connected, context from the terminal interface.
func TestDiagnosingAConnectedPromptContextDoesNotAskAgain(t *testing.T) {
	vc := startVCenter(t, nil)
	prompt := &answeringPrompt{}
	resolver := credentials.NewResolver(prompt)
	mgr := session.New(resolver)
	t.Cleanup(func() { _ = mgr.Close(context.Background()) })
	backend := tui.NewBackend(newCfg(t), resolver, mgr, vsphere.ConnectOptions{Resolver: resolver})
	ctx := context.Background()

	cc := &config.Context{
		Name: "prod", Endpoint: vc.URL, Username: "operator@vsphere.local",
		Credential: credentials.Ref{Scheme: credentials.SchemePrompt},
		TLS:        config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: vc.Thumbprint},
	}
	cc.Normalize()
	if _, err := mgr.Connect(ctx, cc); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if prompt.asked != 1 {
		t.Fatalf("connecting asked for the password %d times, want 1", prompt.asked)
	}

	d := backend.Diagnose(ctx, cc)
	if !d.OK() {
		t.Fatalf("diagnosis failed: %v", d.Err())
	}
	if prompt.asked != 1 {
		t.Errorf("diagnosing asked for the password again: %d asks in total, want 1", prompt.asked)
	}
}
