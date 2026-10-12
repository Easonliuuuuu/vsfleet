package demo

import (
	"strings"

	"github.com/easonliuuuuu/vsfleet/internal/tui"
)

// Launch builds what both demo entry points, "vsfleet demo" and
// cmd/vsfleet-demo, hand to tui.Run: the synthetic backend, its seeded
// in-memory assessment service, and options that mark the run as a demo. The
// startup animation plays unless VSFLEET_NO_WELCOME or CI is set. The returned
// cleanup closes the store and must run even when tui.Run fails.
func Launch(getenv func(string) string) (*Backend, tui.Options, func(), error) {
	backend := NewBackend()
	service, closeHistory, err := backend.AssessmentService()
	if err != nil {
		return nil, tui.Options{}, nil, err
	}
	opts := tui.Options{Current: "prod-vc", Demo: true, Assessment: service}
	if strings.TrimSpace(getenv("VSFLEET_NO_WELCOME")) == "" && strings.TrimSpace(getenv("CI")) == "" {
		opts.Welcome = tui.Welcome{Kind: tui.WelcomeDemo}
	}
	return backend, opts, closeHistory, nil
}
