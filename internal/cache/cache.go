// Package cache holds the most recently fetched inventory for each
// configured context, outside the interface that displays it. A reader never
// touches the network: Get always returns immediately with whatever is on
// hand, stale or not, while Refresh does the actual fetch — bounded, so a
// vCenter estate with many contexts does not open them all at once, and
// stale-preserving, so a refresh that fails does not erase the last
// inventory that worked. The interface is the only caller today, but nothing
// here depends on it.
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/easonliuuuuu/vcfleet/internal/vsphere"
)

// Fetch retrieves one context's inventory. It is supplied by the caller —
// this package knows nothing about how a context connects.
type Fetch func(ctx context.Context) (*vsphere.Inventory, error)

// Entry is what the cache knows about one context. Inventory and LoadedAt
// describe the last successful fetch; Err, when set, is the reason the most
// recent attempt after that failed — the two are independent, so a reader can
// show last-known-good data alongside a note that refreshing it just failed.
type Entry struct {
	Inventory *vsphere.Inventory
	Err       error
	LoadedAt  time.Time
}

// Loaded reports whether any successful fetch has ever landed.
func (e Entry) Loaded() bool { return e.Inventory != nil }

// Cache maps context names to their most recent Entry.
type Cache struct {
	sem chan struct{}

	mu      sync.Mutex
	entries map[string]Entry
}

// New builds a cache that runs at most maxConcurrent fetches at once across
// every context, regardless of how many Refresh calls are outstanding.
// maxConcurrent <= 0 means unbounded.
func New(maxConcurrent int) *Cache {
	c := &Cache{entries: make(map[string]Entry)}
	if maxConcurrent > 0 {
		c.sem = make(chan struct{}, maxConcurrent)
	}
	return c
}

// Get returns whatever is cached for name without touching the network. The
// second result is false only when name has never been fetched at all.
func (c *Cache) Get(name string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[name]
	return e, ok
}

// Refresh runs fetch for name, bounded by the cache's concurrency limit, and
// stores the result before returning it. On success the new inventory and
// LoadedAt replace the old ones and Err clears; on failure the previous
// inventory and LoadedAt are kept exactly as they were and only Err changes,
// so a caller reading the entry mid-refresh or after a failed one always sees
// the newest data that ever loaded successfully.
//
// Refresh blocks the calling goroutine until a concurrency slot is free and
// the fetch completes. It does not deduplicate concurrent calls for the same
// name — a caller that must not run two fetches for one context at once (the
// interface, tracking a per-context "loading" flag to drive its spinner) is
// expected to arrange that itself, the same way it already decides when a
// context needs fetching at all.
func (c *Cache) Refresh(ctx context.Context, name string, fetch Fetch) Entry {
	if c.sem != nil {
		select {
		case c.sem <- struct{}{}:
			defer func() { <-c.sem }()
		case <-ctx.Done():
			// Never got as far as calling fetch — the context was cancelled
			// waiting for a concurrency slot. Report why, but leave whatever
			// inventory and LoadedAt are already cached untouched.
			return c.store(name, ctx.Err())
		}
	}
	inv, err := fetch(ctx)
	if err != nil {
		return c.store(name, err)
	}
	return c.storeSuccess(name, inv)
}

func (c *Cache) store(name string, err error) Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[name]
	e.Err = err
	c.entries[name] = e
	return e
}

func (c *Cache) storeSuccess(name string, inv *vsphere.Inventory) Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := Entry{Inventory: inv, LoadedAt: time.Now()}
	c.entries[name] = e
	return e
}
