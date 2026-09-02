package tests

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
)

// socksRequest records what a client asked the proxy to reach. The address
// type is the point of the recording: it distinguishes a hostname handed to
// the proxy for remote resolution from an address the client resolved itself.
type socksRequest struct {
	AddressType byte // 1 = IPv4, 3 = domain name, 4 = IPv6
	Host        string
	Port        int
}

// Domain reports whether the client asked the proxy to resolve a name.
func (r socksRequest) Domain() bool { return r.AddressType == 3 }

// socksServer is a minimal SOCKS5 proxy used to prove that vsfleet routes
// per-context traffic through a proxy, including remote name resolution,
// without needing a real bastion in the test environment.
type socksServer struct {
	// Routes maps a hostname the proxy is asked for onto the address it
	// actually dials, standing in for names that only resolve remotely.
	Routes map[string]string
	// RequireAuth, when set before the first connection arrives, requires
	// RFC 1929 username/password authentication with exactly this pair.
	RequireAuth *socksAuth

	listener net.Listener
	mu       sync.Mutex
	requests []socksRequest
}

// socksAuth is one SOCKS5 username/password pair.
type socksAuth struct {
	Username, Password string
}

func startSOCKS(t *testing.T, routes map[string]string) *socksServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &socksServer{Routes: routes, listener: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// Address is the proxy's host:port.
func (s *socksServer) Address() string { return s.listener.Addr().String() }

// Requests returns everything the proxy was asked to connect to.
func (s *socksServer) Requests() []socksRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]socksRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *socksServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			if err := s.handle(conn); err != nil {
				return
			}
		}()
	}
}

// authenticate runs the RFC 1929 username/password sub-negotiation. offered
// is the method list from the client's greeting.
func (s *socksServer) authenticate(conn net.Conn, offered []byte) error {
	wants := false
	for _, m := range offered {
		if m == 2 {
			wants = true
		}
	}
	if !wants {
		_, _ = conn.Write([]byte{5, 0xFF}) // no acceptable methods
		return fmt.Errorf("client did not offer username/password authentication")
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return err
	}
	head := make([]byte, 2) // auth version, username length
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	uname := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, uname); err != nil {
		return err
	}
	pl := make([]byte, 1)
	if _, err := io.ReadFull(conn, pl); err != nil {
		return err
	}
	passwd := make([]byte, int(pl[0]))
	if _, err := io.ReadFull(conn, passwd); err != nil {
		return err
	}
	ok := string(uname) == s.RequireAuth.Username && string(passwd) == s.RequireAuth.Password
	status := byte(0)
	if !ok {
		status = 1
	}
	if _, err := conn.Write([]byte{1, status}); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("socks5 auth rejected for user %q", uname)
	}
	return nil
}

func (s *socksServer) handle(conn net.Conn) error {
	// Greeting: version, method count, methods.
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 5 {
		return fmt.Errorf("unsupported socks version %d", head[0])
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	if s.RequireAuth != nil {
		if err := s.authenticate(conn, methods); err != nil {
			return err
		}
	} else if _, err := conn.Write([]byte{5, 0}); err != nil {
		return err
	}

	// Request: version, command, reserved, address type.
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return err
	}
	if req[1] != 1 {
		return fmt.Errorf("unsupported socks command %d", req[1])
	}

	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = net.IP(b).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = string(b)
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = net.IP(b).String()
	default:
		return fmt.Errorf("unsupported address type %d", req[3])
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(conn, pb); err != nil {
		return err
	}
	port := int(binary.BigEndian.Uint16(pb))

	s.mu.Lock()
	s.requests = append(s.requests, socksRequest{AddressType: req[3], Host: host, Port: port})
	s.mu.Unlock()

	target := net.JoinHostPort(host, strconv.Itoa(port))
	if mapped, ok := s.Routes[host]; ok {
		target = mapped
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		// Reply: general failure.
		_, _ = conn.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer upstream.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(upstream, conn) }()
	go func() { defer wg.Done(); _, _ = io.Copy(conn, upstream) }()
	wg.Wait()
	return nil
}
