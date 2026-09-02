// Package session keeps one connection per vCenter context and tracks its
// state, so that a broken customer environment is visible as a broken row
// rather than as a failure of the whole tool.
package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/easonliuuuuu/vc-tui/internal/config"
	"github.com/easonliuuuuu/vc-tui/internal/credentials"
	"github.com/easonliuuuuu/vc-tui/internal/vsphere"
)

// ConnectionState is where a session is in its lifecycle.
type ConnectionState int

// Connection states.
const (
	Disconnected ConnectionState = iota
	Connecting
	Connected
	Failed
)

// String renders the state for status output.
func (s ConnectionState) String() string {
	switch s {
	case Connecting:
		return "connecting"
	case Connected:
		return "connected"
	case Failed:
		return "failed"
	default:
		return "disconnected"
	}
}

// DefaultConnectTimeout bounds how long one vCenter may take to connect before
// it is reported as failed. One unreachable environment must never hold up the
// others.
const DefaultConnectTimeout = 30 * time.Second

// DefaultConcurrency bounds how many vCenters are contacted at once.
const DefaultConcurrency = 8

// Session is one vCenter context and its connection.
type Session struct {
	Context *config.Context

	mu       sync.Mutex
	state    ConnectionState
	client   *vsphere.Client
	err      error
	latency  time.Duration
	lastOK   time.Time
	lastTry  time.Time
	attempts int
}

// Status is an immutable snapshot of a session, safe to render.
type Status struct {
	Name       string
	Endpoint   string
	Route      string
	State      ConnectionState
	Latency    time.Duration
	Version    string
	Err        error
	LastOK     time.Time
	LastTry    time.Time
	Attempts   int
	Datacenter string
}

// Snapshot returns the current status of the session.
func (s *Session) Snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		Name:       s.Context.Name,
		Endpoint:   s.Context.Endpoint,
		Route:      s.Context.Transport.Describe(),
		State:      s.state,
		Latency:    s.latency,
		Err:        s.err,
		LastOK:     s.lastOK,
		LastTry:    s.lastTry,
		Attempts:   s.attempts,
		Datacenter: s.Context.Datacenter,
	}
	if s.client != nil {
		st.Version = s.client.About.FullVersion()
		st.Route = s.client.Route()
	}
	return st
}

// Client returns the live client, or nil when the session is not connected.
func (s *Session) Client() *vsphere.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

// Manager owns the sessions for every configured context.
type Manager struct {
	// ConnectTimeout bounds a single connection attempt.
	ConnectTimeout time.Duration
	// Concurrency bounds parallel connections. Zero means DefaultConcurrency.
	Concurrency int
	// ConnectOptions are passed to every connection.
	ConnectOptions vsphere.ConnectOptions

	mu       sync.Mutex
	sessions map[string]*Session
}

// New returns a Manager resolving credentials through r.
func New(r *credentials.Resolver) *Manager {
	return &Manager{
		ConnectTimeout: DefaultConnectTimeout,
		Concurrency:    DefaultConcurrency,
		ConnectOptions: vsphere.ConnectOptions{Resolver: r},
		sessions:       map[string]*Session{},
	}
}

func (m *Manager) session(cc *config.Context) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions == nil {
		m.sessions = map[string]*Session{}
	}
	s, ok := m.sessions[cc.Name]
	if !ok {
		s = &Session{Context: cc}
		m.sessions[cc.Name] = s
	}
	return s
}

func (m *Manager) timeout() time.Duration {
	if m.ConnectTimeout > 0 {
		return m.ConnectTimeout
	}
	return DefaultConnectTimeout
}

func (m *Manager) concurrency() int {
	if m.Concurrency > 0 {
		return m.Concurrency
	}
	return DefaultConcurrency
}

// Connect returns a connected session for a context, reusing an existing
// connection when one is live.
func (m *Manager) Connect(ctx context.Context, cc *config.Context) (*Session, error) {
	s := m.session(cc)
	s.mu.Lock()
	if s.state == Connected && s.client != nil {
		s.mu.Unlock()
		return s, nil
	}
	s.state = Connecting
	s.err = nil
	s.lastTry = time.Now()
	s.attempts++
	s.mu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, m.timeout())
	defer cancel()

	client, err := vsphere.Connect(cctx, cc, m.ConnectOptions)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.state = Failed
		s.err = err
		s.client = nil
		return s, err
	}
	s.state = Connected
	s.client = client
	s.latency = client.Latency
	s.lastOK = time.Now()
	return s, nil
}

// ConnectAll connects to every given context concurrently. Failures are
// recorded on the individual sessions and never abort the others: the returned
// error is nil unless the caller's context was cancelled.
func (m *Manager) ConnectAll(ctx context.Context, contexts []*config.Context) []*Session {
	sessions := make([]*Session, len(contexts))
	var g errgroup.Group
	g.SetLimit(m.concurrency())
	for i, cc := range contexts {
		g.Go(func() error {
			s, _ := m.Connect(ctx, cc)
			sessions[i] = s
			// Connection errors live on the session. Returning them here
			// would cancel every other vCenter, which is precisely the
			// coupling this tool exists to avoid.
			return nil
		})
	}
	_ = g.Wait()
	return sessions
}

// Statuses returns a snapshot of every known session, ordered by name.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()

	out := make([]Status, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.Snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Close logs out of every session. Errors are joined so that one vCenter that
// will not answer does not hide the rest.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()

	var errs []error
	for _, s := range sessions {
		s.mu.Lock()
		client := s.client
		s.client = nil
		s.state = Disconnected
		s.mu.Unlock()
		if client == nil {
			continue
		}
		if err := client.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Context.Name, err))
		}
	}
	return errors.Join(errs...)
}
