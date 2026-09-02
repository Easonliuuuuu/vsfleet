package credentials_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/credentials"
)

// Fixture values, not credentials. These tests never reach a real secret
// store, and naming the values keeps a password-shaped literal out of a
// Password field, where it reads as a real secret to both a human skimming
// the file and a secret scanner.
const (
	storedFixture   = "stored-credential-fixture"
	promptedFixture = "prompted-credential-fixture"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		in     string
		scheme string
		value  string
		bad    bool
	}{
		{in: "keyring:customer-a", scheme: "keyring", value: "customer-a"},
		{in: "prompt", scheme: "prompt"},
		{in: "prompt:lab", scheme: "prompt", value: "lab"},
		{in: "  keyring:lab  ", scheme: "keyring", value: "lab"},
		{in: ""},
		{in: "keyring", bad: true},
		{in: "vault:secret/vcenter", bad: true},
		{in: "hunter2", bad: true},
	}
	for _, tc := range cases {
		ref, err := credentials.ParseRef(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("ParseRef(%q) should have failed", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRef(%q): %v", tc.in, err)
			continue
		}
		if ref.Scheme != tc.scheme || ref.Value != tc.value {
			t.Errorf("ParseRef(%q) = %+v, want scheme %q value %q", tc.in, ref, tc.scheme, tc.value)
		}
	}
}

func TestRefRoundTrip(t *testing.T) {
	for _, s := range []string{"keyring:lab", "prompt", ""} {
		ref, err := credentials.ParseRef(s)
		if err != nil {
			t.Fatalf("ParseRef(%q): %v", s, err)
		}
		if got := ref.String(); got != s {
			t.Errorf("round trip of %q produced %q", s, got)
		}
		var back credentials.Ref
		if err := back.UnmarshalText([]byte(s)); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", s, err)
		}
		if back != ref {
			t.Errorf("UnmarshalText(%q) = %+v, want %+v", s, back, ref)
		}
	}
}

func TestResolverDispatch(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewStatic(credentials.SchemeKeyring, map[string]credentials.Credential{
		"lab": {Password: storedFixture},
	})
	prompt := credentials.NewStatic(credentials.SchemePrompt, map[string]credentials.Credential{
		"": {Password: promptedFixture},
	})
	r := credentials.NewResolver(store, prompt)

	ref, _ := credentials.ParseRef("keyring:lab")
	got, err := r.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Password != storedFixture {
		t.Errorf("resolved %q, want the stored value", got.Password)
	}

	missing, _ := credentials.ParseRef("keyring:absent")
	if _, err := r.Get(ctx, missing); !errors.Is(err, credentials.ErrNotFound) {
		t.Errorf("an absent key should report ErrNotFound, got %v", err)
	}

	unknown := credentials.Ref{Scheme: "vault", Value: "x"}
	if _, err := r.Get(ctx, unknown); err == nil || !strings.Contains(err.Error(), "vault") {
		t.Errorf("an unregistered scheme should be named in the error, got %v", err)
	}
}

// TestPromptFallback covers the path an operator hits on a new machine: the
// keyring holds nothing yet, so the password is asked for instead.
func TestPromptFallback(t *testing.T) {
	ctx := context.Background()
	store := credentials.NewStatic(credentials.SchemeKeyring, nil)
	prompt := &credentials.Prompt{In: strings.NewReader(promptedFixture + "\n"), Out: &strings.Builder{}}
	r := credentials.NewResolver(store, prompt)

	ref, _ := credentials.ParseRef("keyring:lab")
	cred, prompted, err := credentials.Resolve(ctx, r, ref, "lab")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !prompted {
		t.Error("expected the credential to come from the prompt")
	}
	if cred.Password != promptedFixture {
		t.Errorf("read %q from the prompt", cred.Password)
	}
}

// TestPromptSharesReader guards against a buffered read swallowing the input
// meant for the next question, which the interactive wizard depends on.
func TestPromptSharesReader(t *testing.T) {
	p := &credentials.Prompt{In: strings.NewReader("customer-a\nvcsa.internal\nsecret\n"), Out: &strings.Builder{}}
	name, err := p.ReadLine("Name: ")
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	endpoint, err := p.ReadLine("Endpoint: ")
	if err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	secret, err := p.ReadSecret("Password: ")
	if err != nil {
		t.Fatalf("ReadSecret: %v", err)
	}
	if name != "customer-a" || endpoint != "vcsa.internal" || secret != "secret" {
		t.Errorf("read %q, %q, %q", name, endpoint, secret)
	}
}

func TestPromptCannotStore(t *testing.T) {
	p := credentials.NewPrompt()
	err := p.Store(context.Background(), credentials.Ref{Scheme: credentials.SchemePrompt}, credentials.Credential{})
	if !errors.Is(err, credentials.ErrUnsupported) {
		t.Errorf("storing into a prompt should be unsupported, got %v", err)
	}
}
