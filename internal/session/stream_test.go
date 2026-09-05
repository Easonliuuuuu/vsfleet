package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// TestStreamOperationSurvivesWorkLongerThanTheIdleTimeout is the behaviour a
// fixed deadline could not express: an estate that legitimately takes longer
// than the timeout to read must finish, as long as it keeps arriving.
func TestStreamOperationSurvivesWorkLongerThanTheIdleTimeout(t *testing.T) {
	m := session.New(nil)
	m.IdleTimeout = 60 * time.Millisecond

	ctx, progress, cancel, _ := m.StreamOperation(context.Background())
	defer cancel()

	// Four times the idle timeout of steady progress.
	for i := 0; i < 12; i++ {
		time.Sleep(20 * time.Millisecond)
		progress()
		if err := ctx.Err(); err != nil {
			t.Fatalf("cancelled after %d progress reports, none more than the idle timeout apart: %v", i+1, err)
		}
	}
}

// TestStreamOperationGivesUpOnSilence is the other half: work that stops
// arriving still fails, and reports itself as the timeout it was rather than
// as a bare cancellation.
func TestStreamOperationGivesUpOnSilence(t *testing.T) {
	m := session.New(nil)
	m.IdleTimeout = 40 * time.Millisecond

	ctx, _, cancel, tracker := m.StreamOperation(context.Background())
	defer cancel()
	tracker.Report(vsphere.StageLoadingVMs)

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the watchdog never fired on a silent operation")
	}

	err := m.StreamError(ctx, ctx.Err(), tracker)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("StreamError = %v, want something that reads as a deadline", err)
	}
	if got := err.Error(); got == "" || !contains(got, "loading VMs") {
		t.Errorf("StreamError = %q, want it to name the stage the operation was in", got)
	}
}

// TestStreamErrorLeavesUnrelatedFailuresAlone keeps the watchdog from
// relabelling a real error — a refused login is not a timeout.
func TestStreamErrorLeavesUnrelatedFailuresAlone(t *testing.T) {
	m := session.New(nil)
	ctx, _, cancel, tracker := m.StreamOperation(context.Background())
	defer cancel()

	want := errors.New("authenticate to vcsa.example.com as svc: permission denied")
	if got := m.StreamError(ctx, want, tracker); !errors.Is(got, want) {
		t.Errorf("StreamError rewrote an unrelated failure: %v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
