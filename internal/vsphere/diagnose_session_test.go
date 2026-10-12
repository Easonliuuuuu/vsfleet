package vsphere_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vmware/govmomi/simulator"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// countingPrompt stands in for the operator: it records each ask and
// declines it, so a test sees whether a diagnosis wanted a typed password.
type countingPrompt struct{ asked int }

var errDeclined = errors.New("declined")

func (p *countingPrompt) Scheme() string { return credentials.SchemePrompt }
func (p *countingPrompt) Get(context.Context, credentials.Ref) (credentials.Credential, error) {
	p.asked++
	return credentials.Credential{}, errDeclined
}
func (p *countingPrompt) Store(context.Context, credentials.Ref, credentials.Credential) error {
	return errDeclined
}
func (p *countingPrompt) Delete(context.Context, credentials.Ref) error { return errDeclined }

func TestDiagnoseSessionReusesLiveLoginForPromptPassword(t *testing.T) {
	tests := []struct {
		name      string
		live      bool
		loggedOut bool
		wantAsked int
		wantOK    bool
	}{
		{name: "live session", live: true, wantAsked: 0, wantOK: true},
		{name: "no session", wantAsked: 1},
		{name: "expired session", live: true, loggedOut: true, wantAsked: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := simulator.VPX()
			if err := model.Create(); err != nil {
				t.Fatalf("create simulator model: %v", err)
			}
			t.Cleanup(model.Remove)
			gc, endpoint := dialSimulator(t, model)

			cc := &config.Context{
				Name: "sim", Endpoint: endpoint, Username: "user",
				Credential: credentials.Ref{Scheme: credentials.SchemePrompt},
				TLS:        config.TLSConfig{Mode: config.TLSInsecure},
			}
			cc.Normalize()
			var live *vsphere.Client
			if tt.live {
				live = vsphere.NewClientForTest(cc, gc)
			}
			if tt.loggedOut {
				if err := gc.Logout(context.Background()); err != nil {
					t.Fatalf("logout: %v", err)
				}
			}
			prompt := &countingPrompt{}
			opts := vsphere.ConnectOptions{Resolver: credentials.NewResolver(prompt)}

			d, client := vsphere.DiagnoseSession(context.Background(), cc, opts, live)
			if client != nil {
				_ = client.Close(context.Background())
				t.Error("DiagnoseSession returned a client; borrowing the live session must not log in again")
			}
			if prompt.asked != tt.wantAsked {
				t.Errorf("password asked %d times, want %d", prompt.asked, tt.wantAsked)
			}
			if d.OK() != tt.wantOK {
				t.Errorf("diagnosis OK = %v, want %v (first error: %v)", d.OK(), tt.wantOK, d.Err())
			}
		})
	}
}
