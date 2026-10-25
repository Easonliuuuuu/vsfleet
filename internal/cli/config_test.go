package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const twoContexts = `version = 1
current_context = 'a'

[[contexts]]
name = 'a'
endpoint = 'https://vcsa-a.example.internal'
username = 'ro@vsphere.local'
credential = 'env:A_PASSWORD'

[contexts.transport]
type = 'socks5'
address = 'bastion.example.net:1080'
remote_dns = true

[contexts.tls]
mode = 'system'

[[contexts]]
name = 'b'
endpoint = 'https://vcsa-b.example.internal'
username = 'ro@vsphere.local'
credential = 'env:B_PASSWORD'

[contexts.transport]
type = 'direct'

[contexts.tls]
mode = 'system'
`

// withConfig writes body to a fresh configuration file and returns args that
// point vsfleet at it, followed by rest.
func withConfig(t *testing.T, body string, rest ...string) (string, []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, append([]string{"--config", path}, rest...)
}

func viewJSON(t *testing.T, path string, args ...string) configView {
	t.Helper()
	out, _, err := runArgs(t, append([]string{"--config", path, "config", "view", "-o", "json"}, args...)...)
	if err != nil {
		t.Fatalf("config view: %v", err)
	}
	var v configView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("decode config view: %v\n%s", err, out)
	}
	return v
}

// Switching an existing context to a direct route must drop the proxy it
// had: a leftover proxy address fails validation, so the update could
// never be saved.
func TestSetContextRouteSwitchDropsProxySettings(t *testing.T) {
	path, args := withConfig(t, twoContexts, "config", "set-context", "a", "--transport", "direct", "--no-test")
	if _, stderr, err := runArgs(t, args...); err != nil {
		t.Fatalf("set-context: %v\n%s", err, stderr)
	}
	v := viewJSON(t, path, "--minify", "-n", "a")
	c := v.Contexts[0]
	if c.Transport.Type != "direct" || c.Transport.Address != "" || c.Transport.RemoteDNS {
		t.Errorf("transport = %+v, want a plain direct route", c.Transport)
	}
	if got := c.Credential.String(); got != "env:A_PASSWORD" {
		t.Errorf("credential = %q, want the unchanged env:A_PASSWORD", got)
	}
}

// Without a terminal there is no wizard, so naming an existing context with
// nothing to change is a mistake to report, not a silent re-save.
func TestSetContextWithNothingToChangeFails(t *testing.T) {
	path, args := withConfig(t, twoContexts, "config", "set-context", "b", "--no-test")
	_, _, err := runArgs(t, args...)
	if err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Fatalf("err = %v, want a nothing-to-change error", err)
	}
	if b, _ := os.ReadFile(path); string(b) != twoContexts {
		t.Errorf("the configuration file was rewritten:\n%s", b)
	}
}

// --minify keeps the context a command would act on: -n when given, the
// current context otherwise.
func TestConfigViewMinifySelectsOneContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "current", args: []string{"--minify"}, want: "a"},
		{name: "-n", args: []string{"--minify", "-n", "b"}, want: "b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := withConfig(t, twoContexts)
			v := viewJSON(t, path, tc.args...)
			if len(v.Contexts) != 1 || v.Contexts[0].Name != tc.want || v.CurrentContext != tc.want {
				t.Errorf("view = %+v, want only context %q, current", v, tc.want)
			}
		})
	}
}

// The removed "context" noun answers with the command that replaced the
// subcommand typed, so a script gets a pointer instead of "unknown command".
func TestRemovedContextCommandsNameTheirReplacement(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"context", "test", "prod"}, want: "vsfleet doctor NAME"},
		{args: []string{"contexts"}, want: "vsfleet config get-contexts"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, _, err := runArgs(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
