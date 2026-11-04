package tui

import (
	"strings"
	"testing"

	"github.com/easonliuuuuu/vsfleet/internal/config"
)

func TestVsphereClientURLNeedsInstanceID(t *testing.T) {
	cc := ctx("prod", "https://vcsa.prod.internal")
	if _, ok := vsphereClientURL(cc, "VirtualMachine", "vm-1234", ""); ok {
		t.Fatal("a URL must not be built without the vCenter's server GUID")
	}
	url, ok := vsphereClientURL(cc, "VirtualMachine", "vm-1234", "52-aaaa-bbbb")
	if !ok {
		t.Fatal("expected a URL once the GUID is known")
	}
	for _, want := range []string{"vcsa.prod.internal", "vm-1234", "52-aaaa-bbbb", "VirtualMachine"} {
		if !strings.Contains(url, want) {
			t.Errorf("URL %q missing %q", url, want)
		}
	}
}

func TestVsphereClientURLUnknownMorefKind(t *testing.T) {
	cc := ctx("prod", "https://vcsa.prod.internal")
	if _, ok := vsphereClientURL(cc, "", "vm-1234", "52-aaaa-bbbb"); ok {
		t.Fatal("expected no URL for an unrecognised managed object type")
	}
}

func TestHostClientURL(t *testing.T) {
	if _, ok := hostClientURL(""); ok {
		t.Fatal("expected no URL for an empty address")
	}
	url, ok := hostClientURL("esx-tpe-04.corp.example")
	if !ok || url != "https://esx-tpe-04.corp.example/ui" {
		t.Fatalf("got %q, %v", url, ok)
	}
}

// A guest-reported name with shell metacharacters is quoted in the command
// offered for copying.
func TestSSHCommandQuotesTheAddress(t *testing.T) {
	if got, want := sshCommand("10.20.0.11"), "ssh 10.20.0.11"; got != want {
		t.Errorf("sshCommand() = %q, want %q", got, want)
	}
	if got, want := sshCommand("app;01"), "ssh 'app;01'"; got != want {
		t.Errorf("sshCommand() = %q, want %q", got, want)
	}
}

func TestTailBufferKeepsOnlyRecentOutput(t *testing.T) {
	w := &tailBuffer{max: 8}
	if _, err := w.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("7890")); err != nil {
		t.Fatal(err)
	}
	if got, want := w.String(), "34567890"; got != want {
		t.Fatalf("tailBuffer.String() = %q, want %q", got, want)
	}
}

func TestIsDirect(t *testing.T) {
	if !isDirect(config.TransportConfig{}) {
		t.Error("an empty transport type must count as direct")
	}
	if !isDirect(config.TransportConfig{Type: config.TransportDirect}) {
		t.Error("explicit direct must count as direct")
	}
	if isDirect(config.TransportConfig{Type: config.TransportSOCKS5, Address: "127.0.0.1:1080"}) {
		t.Error("a SOCKS5 route must not count as direct")
	}
}
