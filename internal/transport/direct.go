package transport

import (
	"context"
	"net"
	"time"
)

// Direct connects straight from this host, resolving names locally.
type Direct struct {
	dialer *net.Dialer
}

// NewDirect returns a Dialer that connects without any intermediary.
func NewDirect(timeout time.Duration) *Direct {
	return &Direct{dialer: &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}}
}

// DialContext implements Dialer.
func (d *Direct) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, address)
}

// Describe implements Dialer.
func (d *Direct) Describe() string { return "Direct" }
