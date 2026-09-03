// This file covers "vsfleet context add" against the OS keyring itself
// (github.com/easonliuuuuu/vsfleet/tests/cli_test.go only ever exercises
// --credential prompt, since a real secret store is rarely available in CI).
// go-keyring's own in-memory mock stands in for the OS keyring here, so the
// CLI's --password-stdin/--credential keyring: wiring is proven end to end
// without ever touching a real keychain or Secret Service.
package tests

import (
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/easonliuuuuu/vsfleet/internal/credentials"
)

func TestContextAddPasswordStdinWithExplicitKeyringCredential(t *testing.T) {
	keyring.MockInit()

	vc := startVCenter(t, nil)
	r := newRunner(t)

	// The requested keyring key deliberately differs from the context name,
	// so a bug that stores under the context's default key instead of the
	// one actually requested would show up as a lookup miss below.
	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "lab",
		"--endpoint", vc.URL,
		"--username", "operator@vsphere.local",
		"--credential", "keyring:lab-secret",
		"--password-stdin",
		"--tls", "thumbprint",
		"--thumbprint", vc.Thumbprint,
	)

	list := r.mustRun("", "context", "show", "lab")
	if !strings.Contains(list, "keyring:lab-secret") {
		t.Fatalf("saved context does not reference keyring:lab-secret:\n%s", list)
	}

	secret, err := keyring.Get(credentials.KeyringService, "lab-secret")
	if err != nil {
		t.Fatalf("password was not stored under the requested keyring key: %v", err)
	}
	if secret != testPassword {
		t.Errorf("stored password = %q, want %q", secret, testPassword)
	}

	// A later command must resolve the password from the keyring on its own,
	// with no prompt: empty stdin proves nothing was read interactively.
	test := r.mustRun("", "context", "test", "lab")
	if !strings.Contains(test, "Connection successful.") {
		t.Errorf("context test did not succeed using the stored keyring credential:\n%s", test)
	}
}

func TestContextAddPasswordStdinWithExplicitKeyringCredentialNoTest(t *testing.T) {
	keyring.MockInit()

	r := newRunner(t)

	r.mustRun(testPassword+"\n", "context", "add",
		"--name", "lab",
		"--endpoint", "https://vcsa.example.internal",
		"--username", "operator@vsphere.local",
		"--credential", "keyring:lab-secret",
		"--password-stdin",
		"--tls", "insecure",
		"--no-test",
	)

	show := r.mustRun("", "context", "show", "lab")
	if !strings.Contains(show, "keyring:lab-secret") {
		t.Fatalf("saved context does not reference keyring:lab-secret:\n%s", show)
	}

	secret, err := keyring.Get(credentials.KeyringService, "lab-secret")
	if err != nil {
		t.Fatalf("password was not stored under the requested keyring key: %v", err)
	}
	if secret != testPassword {
		t.Errorf("stored password = %q, want %q", secret, testPassword)
	}
}
