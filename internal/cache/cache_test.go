package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

func TestGetOnAnUnknownContextIsNotLoaded(t *testing.T) {
	c := New(0)
	e, ok := c.Get("prod")
	if ok {
		t.Errorf("Get on an unfetched context: ok = true, want false")
	}
	if e.Loaded() {
		t.Errorf("Loaded() = true, want false")
	}
}

func TestRefreshStoresASuccessfulFetch(t *testing.T) {
	c := New(0)
	want := &vsphere.Inventory{Context: "prod"}
	e := c.Refresh(context.Background(), "prod", func(context.Context) (*vsphere.Inventory, error) {
		return want, nil
	})
	if e.Inventory != want {
		t.Errorf("Inventory = %v, want %v", e.Inventory, want)
	}
	if e.Err != nil {
		t.Errorf("Err = %v, want nil", e.Err)
	}
	if e.LoadedAt.IsZero() {
		t.Error("LoadedAt is zero after a successful fetch")
	}

	got, ok := c.Get("prod")
	if !ok || got.Inventory != want {
		t.Errorf("Get after Refresh = %+v, %v, want the same entry", got, ok)
	}
}

func TestRefreshOnFailurePreservesThePreviousInventory(t *testing.T) {
	c := New(0)
	first := &vsphere.Inventory{Context: "prod"}
	c.Refresh(context.Background(), "prod", func(context.Context) (*vsphere.Inventory, error) {
		return first, nil
	})

	boom := errors.New("connection refused")
	e := c.Refresh(context.Background(), "prod", func(context.Context) (*vsphere.Inventory, error) {
		return nil, boom
	})

	if e.Inventory != first {
		t.Errorf("Inventory after a failed refresh = %v, want the stale one (%v)", e.Inventory, first)
	}
	if !errors.Is(e.Err, boom) {
		t.Errorf("Err = %v, want it to wrap %v", e.Err, boom)
	}
}

func TestRefreshFailureBeforeAnySuccessLeavesNoInventory(t *testing.T) {
	c := New(0)
	boom := errors.New("no route to host")
	e := c.Refresh(context.Background(), "prod", func(context.Context) (*vsphere.Inventory, error) {
		return nil, boom
	})
	if e.Loaded() {
		t.Errorf("Loaded() = true after a first fetch that failed, want false")
	}
	if !errors.Is(e.Err, boom) {
		t.Errorf("Err = %v, want it to wrap %v", e.Err, boom)
	}
}

func TestRefreshSuccessClearsAPreviousError(t *testing.T) {
	c := New(0)
	boom := errors.New("timeout")
	c.Refresh(context.Background(), "prod", func(context.Context) (*vsphere.Inventory, error) {
		return nil, boom
	})
	want := &vsphere.Inventory{Context: "prod"}
	e := c.Refresh(context.Background(), "prod", func(context.Context) (*vsphere.Inventory, error) {
		return want, nil
	})
	if e.Err != nil {
		t.Errorf("Err after a successful retry = %v, want nil", e.Err)
	}
	if e.Inventory != want {
		t.Errorf("Inventory = %v, want %v", e.Inventory, want)
	}
}

// TestRefreshBoundsConcurrency proves the semaphore actually limits how many
// fetches run at once: with a limit of 2, a third concurrent Refresh call
// must wait for one of the first two to finish before its fetch starts.
func TestRefreshBoundsConcurrency(t *testing.T) {
	const limit = 2
	c := New(limit)

	var running int32
	var maxSeen int32
	release := make(chan struct{})

	fetch := func(context.Context) (*vsphere.Inventory, error) {
		n := atomic.AddInt32(&running, 1)
		for {
			cur := atomic.LoadInt32(&maxSeen)
			if n <= cur || atomic.CompareAndSwapInt32(&maxSeen, cur, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&running, -1)
		return &vsphere.Inventory{}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		name := string(rune('a' + i))
		go func() {
			defer wg.Done()
			c.Refresh(context.Background(), name, fetch)
		}()
	}

	// Give every goroutine a chance to reach the semaphore before releasing
	// any of them; this is inherently a little timing-sensitive, but a
	// generous margin keeps it from flaking on a loaded machine.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&maxSeen); got > limit {
		t.Errorf("max concurrent fetches = %d, want <= %d", got, limit)
	}
}

func TestRefreshUnboundedRunsAllAtOnce(t *testing.T) {
	c := New(0) // maxConcurrent <= 0 means unbounded
	const n = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var inFlight int32
	reached := make(chan struct{}, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		name := string(rune('a' + i))
		go func() {
			defer wg.Done()
			c.Refresh(context.Background(), name, func(context.Context) (*vsphere.Inventory, error) {
				atomic.AddInt32(&inFlight, 1)
				reached <- struct{}{}
				<-start
				return &vsphere.Inventory{}, nil
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

func TestRefreshRespectsContextCancellationWhileWaitingForASlot(t *testing.T) {
	c := New(1)
	release := make(chan struct{})
	go c.Refresh(context.Background(), "holder", func(context.Context) (*vsphere.Inventory, error) {
		<-release
		return &vsphere.Inventory{}, nil
	})
	// Give the first Refresh time to actually take the only slot.
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	e := c.Refresh(ctx, "waiter", func(context.Context) (*vsphere.Inventory, error) {
		called = true
		return &vsphere.Inventory{}, nil
	})
	close(release)

	if called {
		t.Error("fetch ran despite the context being cancelled before a slot freed up")
	}
	if !errors.Is(e.Err, context.Canceled) {
		t.Errorf("Err = %v, want context.Canceled", e.Err)
	}
}
