package testbed

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

// socksProxy is a deliberately small SOCKS5 CONNECT proxy. It only accepts
// loopback targets or names present in routes, preventing the lab from
// becoming an accidental open proxy.
type socksProxy struct {
	listener net.Listener
	routes   map[string]string
	username string
	password string
	serveWG  sync.WaitGroup
	connWG   sync.WaitGroup
	mu       sync.Mutex
	active   map[net.Conn]struct{}
}

func newSOCKSProxy(address string, routes map[string]string, username, password string) (*socksProxy, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	p := &socksProxy{listener: ln, routes: routes, username: username, password: password, active: map[net.Conn]struct{}{}}
	p.serveWG.Add(1)
	go p.serve()
	return p, nil
}

func (p *socksProxy) Address() string { return p.listener.Addr().String() }
func (p *socksProxy) Close() error {
	err := p.listener.Close()
	p.serveWG.Wait()
	p.mu.Lock()
	for conn := range p.active {
		_ = conn.Close()
	}
	p.mu.Unlock()
	p.connWG.Wait()
	return err
}

func (p *socksProxy) serve() {
	defer p.serveWG.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.track(conn)
		p.connWG.Add(1)
		go func() { defer p.connWG.Done(); defer p.untrack(conn); defer conn.Close(); _ = p.handle(conn) }()
	}
}

func (p *socksProxy) handle(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 {
		return fmt.Errorf("invalid SOCKS5 greeting")
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return err
	}
	if err := p.auth(conn); err != nil {
		return err
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return err
	}
	if req[0] != 5 || req[1] != 1 {
		return fmt.Errorf("unsupported SOCKS5 command")
	}
	host, err := readSOCKSHost(conn, req[3])
	if err != nil {
		return err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return err
	}
	port := int(binary.BigEndian.Uint16(portBytes))
	target := p.routes[host]
	if target == "" && isLoopbackHost(host) {
		target = net.JoinHostPort(host, fmt.Sprint(port))
	}
	if target == "" {
		_, _ = conn.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("destination %q is not in the testbed route table", host)
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer upstream.Close()
	p.track(upstream)
	defer p.untrack(upstream)
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	return tunnel(conn, conn, upstream)
}

func (p *socksProxy) track(conn net.Conn)   { p.mu.Lock(); p.active[conn] = struct{}{}; p.mu.Unlock() }
func (p *socksProxy) untrack(conn net.Conn) { p.mu.Lock(); delete(p.active, conn); p.mu.Unlock() }

func (p *socksProxy) auth(conn net.Conn) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 1 {
		return fmt.Errorf("unsupported SOCKS5 auth version")
	}
	user := make([]byte, head[1])
	if _, err := io.ReadFull(conn, user); err != nil {
		return err
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(conn, plen); err != nil {
		return err
	}
	pass := make([]byte, plen[0])
	if _, err := io.ReadFull(conn, pass); err != nil {
		return err
	}
	status := byte(0)
	if string(user) != p.username || string(pass) != p.password {
		status = 1
	}
	if _, err := conn.Write([]byte{1, status}); err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("SOCKS5 proxy authentication rejected")
	}
	return nil
}

func readSOCKSHost(conn net.Conn, typ byte) (string, error) {
	switch typ {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	case 3:
		b := make([]byte, 1)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		name := make([]byte, b[0])
		if _, err := io.ReadFull(conn, name); err != nil {
			return "", err
		}
		return string(name), nil
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		return net.IP(b).String(), nil
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type %d", typ)
	}
}

type httpProxy struct {
	listener net.Listener
	routes   map[string]string
	auth     string
	serveWG  sync.WaitGroup
	connWG   sync.WaitGroup
	mu       sync.Mutex
	active   map[net.Conn]struct{}
}

func newHTTPProxy(address string, routes map[string]string, username, password string) (*httpProxy, error) {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	p := &httpProxy{listener: ln, routes: routes, active: map[net.Conn]struct{}{}}
	if username != "" {
		p.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	}
	p.serveWG.Add(1)
	go p.serve()
	return p, nil
}

func (p *httpProxy) Address() string { return p.listener.Addr().String() }
func (p *httpProxy) Close() error {
	err := p.listener.Close()
	p.serveWG.Wait()
	p.mu.Lock()
	for conn := range p.active {
		_ = conn.Close()
	}
	p.mu.Unlock()
	p.connWG.Wait()
	return err
}

func (p *httpProxy) serve() {
	defer p.serveWG.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.track(conn)
		p.connWG.Add(1)
		go func() { defer p.connWG.Done(); defer p.untrack(conn); defer conn.Close(); _ = p.handle(conn) }()
	}
}

func (p *httpProxy) handle(conn net.Conn) error {
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return err
	}
	if req.Method != http.MethodConnect {
		return fmt.Errorf("proxy only supports CONNECT")
	}
	if p.auth != "" && req.Header.Get("Proxy-Authorization") != p.auth {
		_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
		return fmt.Errorf("HTTP proxy authentication rejected")
	}
	host, port, err := net.SplitHostPort(req.Host)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return err
	}
	target := p.routes[host]
	if target == "" && isLoopbackHost(host) {
		target = net.JoinHostPort(host, port)
	}
	if target == "" {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\n\r\n")
		return fmt.Errorf("destination %q is not in the testbed route table", req.Host)
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return err
	}
	defer upstream.Close()
	p.track(upstream)
	defer p.untrack(upstream)
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return err
	}
	return tunnel(br, conn, upstream)
}

func (p *httpProxy) track(conn net.Conn)   { p.mu.Lock(); p.active[conn] = struct{}{}; p.mu.Unlock() }
func (p *httpProxy) untrack(conn net.Conn) { p.mu.Lock(); delete(p.active, conn); p.mu.Unlock() }

func tunnel(left io.Reader, leftOut io.Writer, right io.ReadWriter) error {
	var wg sync.WaitGroup
	var first error
	var mu sync.Mutex
	copyOne := func(dst io.Writer, src io.Reader) {
		defer wg.Done()
		if _, err := io.Copy(dst, src); err != nil {
			mu.Lock()
			if first == nil {
				first = err
			}
			mu.Unlock()
		}
	}
	wg.Add(2)
	go copyOne(right, left)
	go copyOne(leftOut, right)
	wg.Wait()
	return first
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
