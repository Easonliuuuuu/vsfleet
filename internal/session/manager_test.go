package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/session"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// TestOperationBoundsTheWholeCall reproduces the core of issue #28: a single
// deadline must cover everything a caller does with a connection, not only
// Connect itself. Operation's context is what a caller — CLI inventory
// listing, estate search, the terminal interface — passes to both Connect
// and whatever comes after it, so that work started after a fast connection
// is still cut off at the configured timeout.
func TestOperationBoundsTheWholeCall(t *testing.T) {
	mgr := session.New(nil)
	mgr.ConnectTimeout = 20 * time.Millisecond

	ctx, cancel, tracker := mgr.Operation(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected Operation's context to carry a deadline")
	}
	if until := time.Until(deadline); until <= 0 || until > mgr.ConnectTimeout {
		t.Fatalf("deadline is %s out, want within (0, %s]", until, mgr.ConnectTimeout)
	}

	// Simulate work done after a successful Connect reporting its own
	// progress, the way ListInventory reports the resource kind it is
	// enumerating.
	tracker.Report(vsphere.StageLoadingHosts)

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Operation's context never reached its deadline")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("ctx.Err() = %v, want context.DeadlineExceeded", ctx.Err())
	}
}

// TestTimeoutErrorNamesDurationAndStage covers the error message a timed-out
// operation surfaces: the configured duration and the last stage reached,
// rather than the bare "context deadline exceeded" that would otherwise
// reach the operator from deep inside a govmomi call.
func TestTimeoutErrorNamesDurationAndStage(t *testing.T) {
	mgr := session.New(nil)
	mgr.ConnectTimeout = 30 * time.Second

	tracker := &vsphere.StageTracker{}
	tracker.Report(vsphere.StageLoadingNetworks)

	err := mgr.TimeoutError(context.DeadlineExceeded, tracker)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "timed out after 30s") {
		t.Errorf("error %q does not name the configured duration", err.Error())
	}
	if !strings.Contains(err.Error(), "loading networks") {
		t.Errorf("error %q does not name the last reported stage", err.Error())
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("expected the wrapped error to still satisfy errors.Is(err, context.DeadlineExceeded)")
	}
}

// TestTimeoutErrorWithoutAStage covers an operation that timed out before
// reporting anything — a slow credential prompt or proxy dial before Connect
// reaches its first stage, say.
func TestTimeoutErrorWithoutAStage(t *testing.T) {
	mgr := session.New(nil)
	mgr.ConnectTimeout = 5 * time.Second

	err := mgr.TimeoutError(context.DeadlineExceeded, &vsphere.StageTracker{})
	if !strings.Contains(err.Error(), "timed out after 5s") {
		t.Errorf("error %q does not name the configured duration", err.Error())
	}
	if strings.Contains(err.Error(), " while ") {
		t.Errorf("error %q names a stage that was never reported", err.Error())
	}
}

// TestTimeoutErrorLeavesOtherErrorsAlone covers the common case: most
// failures are not the operation's own deadline, and must reach the operator
// exactly as they came from Connect or ListInventory.
func TestTimeoutErrorLeavesOtherErrorsAlone(t *testing.T) {
	mgr := session.New(nil)
	original := errors.New("authenticate to vc.example.com as operator: EOF")

	if got := mgr.TimeoutError(original, &vsphere.StageTracker{}); got != original {
		t.Fatalf("TimeoutError altered a non-timeout error: got %v, want %v", got, original)
	}
	if got := mgr.TimeoutError(nil, &vsphere.StageTracker{}); got != nil {
		t.Fatalf("TimeoutError(nil, ...) = %v, want nil", got)
	}
}
