package credentials_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/credentials"
)

// Fixture values, not credentials — see the note in credentials_test.go.
const (
	envFixture  = "env-credential-fixture"
	fileFixture = "file-credential-fixture"
	execFixture = "exec-credential-fixture"
)

func TestEnvReadsTheNamedVariable(t *testing.T) {
	t.Setenv("VSFLEET_TEST_PASSWORD", envFixture)
	ref, err := credentials.ParseRef("env:VSFLEET_TEST_PASSWORD")
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	got, err := credentials.NewEnv().Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Password != envFixture {
		t.Errorf("password = %q, want %q", got.Password, envFixture)
	}
}

// An unset variable and an empty one are different mistakes and must read as
// different mistakes; neither may quietly become an empty password.
func TestEnvDistinguishesUnsetFromEmpty(t *testing.T) {
	unset, _ := credentials.ParseRef("env:VSFLEET_TEST_ABSENT")
	_, err := credentials.NewEnv().Get(context.Background(), unset)
	if err == nil || !strings.Contains(err.Error(), "not set") {
		t.Errorf("an unset variable should say so, got %v", err)
	}

	t.Setenv("VSFLEET_TEST_EMPTY", "")
	empty, _ := credentials.ParseRef("env:VSFLEET_TEST_EMPTY")
	_, err = credentials.NewEnv().Get(context.Background(), empty)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("an empty variable should say so, got %v", err)
	}
}

func TestFileReadsTheSecretAndTrimsOneNewline(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "bare", content: fileFixture, want: fileFixture},
		{name: "newline", content: fileFixture + "\n", want: fileFixture},
		{name: "crlf", content: fileFixture + "\r\n", want: fileFixture},
		// Only one newline goes. A password ending in a space is legal, and
		// trimming it would produce an authentication failure nobody can
		// explain from the outside.
		{name: "trailing space", content: fileFixture + " \n", want: fileFixture + " "},
		{name: "two newlines", content: fileFixture + "\n\n", want: fileFixture + "\n"},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		ref, err := credentials.ParseRef("file:" + path)
		if err != nil {
			t.Fatalf("%s: ParseRef: %v", tc.name, err)
		}
		got, err := credentials.NewFile().Get(context.Background(), ref)
		if err != nil {
			t.Fatalf("%s: Get: %v", tc.name, err)
		}
		if got.Password != tc.want {
			t.Errorf("%s: password = %q, want %q", tc.name, got.Password, tc.want)
		}
	}
}

func TestFileReportsAMissingOrEmptyFile(t *testing.T) {
	missing, _ := credentials.ParseRef("file:" + filepath.Join(t.TempDir(), "absent"))
	if _, err := credentials.NewFile().Get(context.Background(), missing); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a missing file should say so, got %v", err)
	}

	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	empty, _ := credentials.ParseRef("file:" + path)
	if _, err := credentials.NewFile().Get(context.Background(), empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("an empty file should say so, got %v", err)
	}
}

// helperScript writes an executable shell script and returns its path.
func helperScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the helper fixtures are shell scripts")
	}
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecTakesStandardOutputAsTheSecret(t *testing.T) {
	path := helperScript(t, "printf '%s\\n' "+execFixture)
	ref, err := credentials.ParseRef("exec:" + path)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	got, err := credentials.NewExec().Get(context.Background(), ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Password != execFixture {
		t.Errorf("password = %q, want %q", got.Password, execFixture)
	}
}

// One wrapper script has to serve an estate, so the helper is told which
// context it is answering for.
func TestExecTellsTheHelperWhichContextItIsAnsweringFor(t *testing.T) {
	path := helperScript(t, `printf '%s\n' "$VSFLEET_CONTEXT/$VSFLEET_CREDENTIAL_REF"`)
	ref, err := credentials.ParseRef("exec:" + path)
	if err != nil {
		t.Fatalf("ParseRef: %v", err)
	}
	ctx := credentials.WithLabel(context.Background(), "customer-a")
	got, err := credentials.NewExec().Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := "customer-a/exec:" + path
	if got.Password != want {
		t.Errorf("helper environment = %q, want %q", got.Password, want)
	}
}

func TestExecReportsAFailingHelper(t *testing.T) {
	path := helperScript(t, "echo 'vault is sealed' >&2\nexit 1")
	ref, _ := credentials.ParseRef("exec:" + path)
	_, err := credentials.NewExec().Get(context.Background(), ref)
	if err == nil {
		t.Fatal("a failing helper should be an error")
	}
	// The helper's own explanation is the useful half of the message.
	if !strings.Contains(err.Error(), "vault is sealed") {
		t.Errorf("the error drops what the helper said: %v", err)
	}
}

func TestExecReportsAHelperThatWritesNothing(t *testing.T) {
	path := helperScript(t, "exit 0")
	ref, _ := credentials.ParseRef("exec:" + path)
	if _, err := credentials.NewExec().Get(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "wrote nothing") {
		t.Errorf("a silent helper should say so, got %v", err)
	}
}

func TestExecReportsAMissingHelper(t *testing.T) {
	ref, _ := credentials.ParseRef("exec:vsfleet-helper-that-does-not-exist")
	if _, err := credentials.NewExec().Get(context.Background(), ref); err == nil {
		t.Fatal("a helper that is not on PATH should be an error")
	}
}

// A helper that hangs must fail its own context's timeout rather than the
// whole run.
func TestExecHonoursTheContextDeadline(t *testing.T) {
	path := helperScript(t, "sleep 30")
	ref, _ := credentials.ParseRef("exec:" + path)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := credentials.NewExec().Get(ctx, ref); err == nil {
		t.Fatal("a hanging helper should fail the deadline")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the helper was not cancelled: waited %s", elapsed)
	}
}

// Every non-interactive source names where a password lives; none of them
// stores one.
func TestNonInteractiveProvidersAreReadOnly(t *testing.T) {
	for _, p := range []credentials.Provider{credentials.NewEnv(), credentials.NewFile(), credentials.NewExec()} {
		ref := credentials.Ref{Scheme: p.Scheme(), Value: "x"}
		if err := p.Store(context.Background(), ref, credentials.Credential{Password: envFixture}); !errors.Is(err, credentials.ErrUnsupported) {
			t.Errorf("%s Store: %v, want ErrUnsupported", p.Scheme(), err)
		}
		if err := p.Delete(context.Background(), ref); !errors.Is(err, credentials.ErrUnsupported) {
			t.Errorf("%s Delete: %v, want ErrUnsupported", p.Scheme(), err)
		}
	}
}

func TestNonInteractiveRefsAreMarkedAsSuch(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		want bool
	}{
		{ref: "env:X", want: true},
		{ref: "file:/x", want: true},
		{ref: "exec:/x", want: true},
		{ref: "keyring:x", want: false},
		{ref: "prompt", want: false},
		{ref: "", want: false},
	} {
		ref, err := credentials.ParseRef(tc.ref)
		if err != nil {
			t.Fatalf("ParseRef(%q): %v", tc.ref, err)
		}
		if got := ref.NonInteractive(); got != tc.want {
			t.Errorf("ParseRef(%q).NonInteractive() = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

// This is the security-relevant behaviour of the whole feature. Prompt.ReadSecret
// on a non-terminal reads a line from standard input instead of failing, so a
// mistyped reference that fell back to the prompt would silently consume
// whatever a scheduled job had piped in and try it as a password. A
// non-interactive reference must fail instead, and must not touch stdin.
func TestNonInteractiveMissNeverFallsBackToThePrompt(t *testing.T) {
	const pipedIn = "this-is-not-a-password\n"
	for _, refText := range []string{
		"env:VSFLEET_TEST_ABSENT",
		"file:/nonexistent/vsfleet-test-secret",
	} {
		stdin := strings.NewReader(pipedIn)
		prompt := &credentials.Prompt{In: stdin, Out: io.Discard}
		r := credentials.NewResolver(credentials.NewEnv(), credentials.NewFile(), credentials.NewExec(), prompt)
		ref, err := credentials.ParseRef(refText)
		if err != nil {
			t.Fatalf("ParseRef(%q): %v", refText, err)
		}
		_, prompted, err := credentials.Resolve(context.Background(), r, ref, "prod")
		if err == nil {
			t.Errorf("%s: a miss should be an error, not a prompt", refText)
		}
		if prompted {
			t.Errorf("%s: fell back to the interactive prompt", refText)
		}
		if n := stdin.Len(); n != len(pipedIn) {
			t.Errorf("%s: read %d bytes of standard input; it must not be touched", refText, len(pipedIn)-n)
		}
	}
}
