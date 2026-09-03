package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRequiresTerminal(t *testing.T) {
	b := &fakeBackend{}
	var in, out bytes.Buffer

	_, err := Run(context.Background(), b, Options{
		In:  &in,
		Out: &out,
	})
	if err == nil {
		t.Fatal("Run should have failed when In and Out are not terminals")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should mention terminal, got: %v", err)
	}
}
