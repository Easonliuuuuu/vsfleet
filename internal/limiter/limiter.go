// Package limiter bounds how many inventory fetches run at once, across
// every context and every resource-kind fetch group, so an estate with many
// vCenters — or one context loading all five of its kinds concurrently —
// does not open unbounded connections at the same moment.
package limiter

import "context"

// Limiter bounds how many calls to Run execute at once.
type Limiter struct {
	sem chan struct{}
}

// New returns a Limiter that runs at most maxConcurrent calls to Run at
// once. maxConcurrent <= 0 means unbounded.
func New(maxConcurrent int) *Limiter {
	l := &Limiter{}
	if maxConcurrent > 0 {
		l.sem = make(chan struct{}, maxConcurrent)
	}
	return l
}

// Run calls fn once a concurrency slot is free, releasing it before
// returning. It blocks the calling goroutine until a slot is free, or until
// ctx is done — in which case it returns ctx.Err() without ever calling fn.
func (l *Limiter) Run(ctx context.Context, fn func()) error {
	if l.sem != nil {
		select {
		case l.sem <- struct{}{}:
			defer func() { <-l.sem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	fn()
	return nil
}
