package limiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRunBoundsConcurrency proves the semaphore actually limits how many
// calls execute at once: with a limit of 2, a third concurrent Run call must
// wait for one of the first two to finish before its fn starts.
func TestRunBoundsConcurrency(t *testing.T) {
	const limit = 2
	l := New(limit)

	var running int32
	var maxSeen int32
	release := make(chan struct{})

	fn := func() {
		n := atomic.AddInt32(&running, 1)
		for {
			cur := atomic.LoadInt32(&maxSeen)
			if n <= cur || atomic.CompareAndSwapInt32(&maxSeen, cur, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&running, -1)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Run(context.Background(), fn)
		}()
	}

	// Give every goroutine a chance to reach the semaphore before releasing
	// any of them; this is inherently a little timing-sensitive, but a
	// generous margin keeps it from flaking on a loaded machine.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&maxSeen); got > limit {
		t.Errorf("max concurrent calls = %d, want <= %d", got, limit)
	}
}

func TestRunUnboundedRunsAllAtOnce(t *testing.T) {
	l := New(0) // maxConcurrent <= 0 means unbounded
	const n = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var inFlight int32
	reached := make(chan struct{}, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Run(context.Background(), func() {
				atomic.AddInt32(&inFlight, 1)
				reached <- struct{}{}
				<-start
			})
		}()
	}
	for i := 0; i < n; i++ {
		<-reached
	}
	if got := atomic.LoadInt32(&inFlight); got != n {
		t.Errorf("in flight = %d, want all %d to have started unbounded", got, n)
	}
	close(start)
	wg.Wait()
}

func TestRunRespectsContextCancellationWhileWaitingForASlot(t *testing.T) {
	l := New(1)
	release := make(chan struct{})
	go func() { _ = l.Run(context.Background(), func() { <-release }) }()
	// Give the first Run time to actually take the only slot.
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := l.Run(ctx, func() { called = true })
	close(release)

	if called {
		t.Error("fn ran despite the context being cancelled before a slot freed up")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
