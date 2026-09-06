// This file proves the non-interactive credential schemes end to end: a
// context configured with env:, file: or exec: connects to a vCenter with
// nothing on standard input and no keyring, which is the whole point of them —
// cron, systemd, a container and CI have no terminal to prompt at.
package tests

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// addNonInteractiveContext registers a context whose password is resolved by
// ref, with empty standard input: nothing may be read interactively.
func (r *runner) addNonInteractiveContext(name string, vc *vcenter, ref string, extra ...string) {
	r.t.Helper()
	args := []string{
		"context", "add",
		"--name", name,
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", ref,
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
	}
	r.mustRun("", append(args, extra...)...)
}

func TestContextResolvesAnEnvironmentCredential(t *testing.T) {
	t.Setenv("VSFLEET_E2E_PASSWORD", testPassword)
	vc := startVCenter(t, nil)
	r := newRunner(t)

	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_PASSWORD")

	show := r.mustRun("", "context", "show", "lab")
	if !strings.Contains(show, "env:VSFLEET_E2E_PASSWORD") {
		t.Fatalf("the saved context does not reference the environment variable:\n%s", show)
	}
	// The reference names the variable and never the secret, so it is safe in
	// a configuration file. Prove the password itself was not written.
	config, err := os.ReadFile(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), testPassword) {
		t.Error("the password was written to config.toml")
	}

	// Empty standard input: a prompt would have nothing to read, so a
	// successful connection proves the variable answered.
	if out := r.mustRun("", "context", "test", "lab"); !strings.Contains(out, "Connection successful.") {
		t.Errorf("context test did not use the environment credential:\n%s", out)
	}
}

func TestContextResolvesAFileCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vcenter-password")
	if err := os.WriteFile(path, []byte(testPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	vc := startVCenter(t, nil)
	r := newRunner(t)

	r.addNonInteractiveContext("lab", vc, "file:"+path)

	if out := r.mustRun("", "context", "test", "lab"); !strings.Contains(out, "Connection successful.") {
		t.Errorf("context test did not use the file credential:\n%s", out)
	}
}

func TestContextResolvesAnExecCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the helper fixture is a shell script")
	}
	helper := filepath.Join(t.TempDir(), "helper")
	// The helper is told which context it is answering for, which is what lets
	// one wrapper script serve an entire estate.
	script := "#!/bin/sh\nif [ \"$VSFLEET_CONTEXT\" != \"lab\" ]; then echo \"wrong context: $VSFLEET_CONTEXT\" >&2; exit 1; fi\nprintf '%s\\n' " + testPassword + "\n"
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	vc := startVCenter(t, nil)
	r := newRunner(t)

	r.addNonInteractiveContext("lab", vc, "exec:"+helper)

	if out := r.mustRun("", "context", "test", "lab"); !strings.Contains(out, "Connection successful.") {
		t.Errorf("context test did not use the exec credential:\n%s", out)
	}
}

// This is the behaviour that makes the schemes safe to schedule. Prompt reads
// a line from standard input when there is no terminal, so a mistyped
// reference that fell back to the prompt would consume whatever a cron job had
// piped in and try it as a password. It must fail and say what is wrong, and
// it must leave standard input alone.
func TestNonInteractiveCredentialMissDoesNotConsumeStandardInput(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)

	// Saved without testing: the point of the test is what the *next*
	// command does when the variable is missing.
	r.addNonInteractiveContext("lab", vc, "env:VSFLEET_E2E_ABSENT", "--no-test")

	const piped = "this-is-not-a-password\n"
	stdout, stderr, err := r.run(piped, "context", "test", "lab")
	if err == nil {
		t.Fatalf("a missing environment variable should fail the connection:\nstdout:\n%s", stdout)
	}
	combined := stdout + stderr + err.Error()
	if !strings.Contains(combined, "VSFLEET_E2E_ABSENT") {
		t.Errorf("the failure does not name the variable that is missing:\n%s", combined)
	}
	if strings.Contains(combined, piped) {
		t.Errorf("the piped input was read as a password:\n%s", combined)
	}
}

// env, file and exec name where a password lives; they cannot store one. Taking
// a password on standard input and then dropping it silently would leave an
// operator believing a secret was saved that never was.
func TestPasswordStdinIsRejectedForReadOnlySchemes(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)

	_, stderr, err := r.run(testPassword+"\n", "context", "add",
		"--name", "lab",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "env:VSFLEET_E2E_PASSWORD",
		"--password-stdin",
		"--tls", "insecure",
		"--no-test",
	)
	if err == nil {
		t.Fatal("--password-stdin with a read-only credential scheme should be rejected")
	}
	if !strings.Contains(err.Error(), "--password-stdin") {
		t.Errorf("the error does not explain the conflict: %v\nstderr:\n%s", err, stderr)
	}
	// Rejected before anything was saved.
	if _, statErr := os.Stat(r.configPath); statErr == nil {
		t.Error("a rejected context add still wrote a configuration file")
	}
}

func TestUnknownCredentialSchemeNamesTheSupportedOnes(t *testing.T) {
	vc := startVCenter(t, nil)
	r := newRunner(t)

	_, _, err := r.run("", "context", "add",
		"--name", "lab",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "vault:secret/vcenter",
		"--tls", "insecure",
		"--no-test",
	)
	if err == nil {
		t.Fatal("an unknown credential scheme should be rejected")
	}
	for _, scheme := range []string{"keyring", "prompt", "env", "file", "exec"} {
		if !strings.Contains(err.Error(), scheme) {
			t.Errorf("the error does not mention the %s scheme: %v", scheme, err)
		}
	}
}
