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

type unavailableKeyring struct{}

func (unavailableKeyring) Scheme() string { return credentials.SchemeKeyring }
func (unavailableKeyring) Get(context.Context, credentials.Ref) (credentials.Credential, error) {
	return credentials.Credential{}, errors.New("secret service is locked")
}
func (unavailableKeyring) Store(context.Context, credentials.Ref, credentials.Credential) error {
	return errors.New("secret service is locked")
}
func (unavailableKeyring) Delete(context.Context, credentials.Ref) error {
	return errors.New("secret service is locked")
}

func TestPromptFallbackWhenKeyringIsUnavailable(t *testing.T) {
	prompt := credentials.NewStatic(credentials.SchemePrompt, map[string]credentials.Credential{
		"lab": {Password: promptedFixture},
	})
	r := credentials.NewResolver(unavailableKeyring{}, prompt)
	ref := credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "lab"}

	cred, prompted, err := credentials.Resolve(context.Background(), r, ref, "lab")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !prompted || cred.Password != promptedFixture {
		t.Fatalf("Resolve returned prompted=%v credential=%q, want prompt fallback", prompted, cred.Password)
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

// TestResolverPrimeShortCircuitsGet checks the mechanism issue #27's start-up
// flow relies on: a reference answered once through Prime resolves from that
// cache afterwards, without consulting the provider registered for its
// scheme at all — proven here by registering no provider whatsoever for
// "keyring" and getting an answer anyway.
func TestResolverPrimeShortCircuitsGet(t *testing.T) {
	ctx := context.Background()
	r := credentials.NewResolver() // no providers registered for any scheme
	ref := credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "lab"}

	if _, err := r.Get(ctx, ref); err == nil {
		t.Fatal("Get should fail before priming: no provider is registered")
	}

	r.Prime(ref, credentials.Credential{Password: storedFixture})
	cred, err := r.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get after Prime: %v", err)
	}
	if cred.Password != storedFixture {
		t.Errorf("primed Get returned %q, want %q", cred.Password, storedFixture)
	}

	// Priming is per reference: a different value under the same scheme, or
	// the same value under a different scheme, must still miss.
	other := credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "other"}
	if _, err := r.Get(ctx, other); err == nil {
		t.Error("priming one reference must not answer for another")
	}
}

func TestRefWithDefaultLabel(t *testing.T) {
	cases := []struct {
		name  string
		in    credentials.Ref
		label string
		want  credentials.Ref
	}{
		{
			name:  "bare prompt takes the label",
			in:    credentials.Ref{Scheme: credentials.SchemePrompt},
			label: "lab",
			want:  credentials.Ref{Scheme: credentials.SchemePrompt, Value: "lab"},
		},
		{
			name:  "a labelled prompt is left alone",
			in:    credentials.Ref{Scheme: credentials.SchemePrompt, Value: "custom"},
			label: "lab",
			want:  credentials.Ref{Scheme: credentials.SchemePrompt, Value: "custom"},
		},
		{
			name:  "a keyring reference is never relabelled",
			in:    credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "lab"},
			label: "other",
			want:  credentials.Ref{Scheme: credentials.SchemeKeyring, Value: "lab"},
		},
	}
	for _, tc := range cases {
		if got := tc.in.WithDefaultLabel(tc.label); got != tc.want {
			t.Errorf("%s: WithDefaultLabel(%q) = %+v, want %+v", tc.name, tc.label, got, tc.want)
		}
	}
}
