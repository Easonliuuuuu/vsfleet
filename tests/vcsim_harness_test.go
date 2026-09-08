//go:build integration

package tests

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

const vcsimUsername = "operator@vsphere.local"

var vcsimEnvironmentLine = regexp.MustCompile(`(?:^|\s)GOVC_URL=(\S+)`)

type vcsimSpec struct {
	Fixture string
	Name    string
	Flags   []string
}

// simEndpoint is the external-process equivalent of the in-process vcenter
// helper. The URL, address and thumbprint fields intentionally mirror the
// fields consumed by runner context helpers.
type simEndpoint struct {
	URL        string
	Host       string
	Port       string
	Address    string
	Thumbprint string

	cmd      *exec.Cmd
	wait     <-chan error
	exited   <-chan struct{}
	stopOnce sync.Once
}

func vcsimBinary(t *testing.T) string {
	t.Helper()
	required := os.Getenv("VSFLEET_VCSIM_REQUIRED") == "1"
	if configured := strings.TrimSpace(os.Getenv("VSFLEET_VCSIM_BIN")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured
		}
		t.Logf("VSFLEET_VCSIM_BIN=%q is not an executable file; trying vcsim on PATH", configured)
	}
	if binary, err := exec.LookPath("vcsim"); err == nil {
		return binary
	}
	message := "vcsim is unavailable; install cmd/vsfleet-vcsim or set VSFLEET_VCSIM_BIN"
	if required {
		t.Fatal(message)
	}
	t.Skip(message)
	return ""
}

func startEndpoint(t *testing.T, spec vcsimSpec) *simEndpoint {
	t.Helper()
	binary := vcsimBinary(t)
	logDir := strings.TrimSpace(os.Getenv("VSFLEET_VCSIM_LOG_DIR"))
	if logDir == "" {
		logDir = t.TempDir()
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("create vcsim log directory: %v", err)
	}
	logName := sanitizeVCSimName(spec.Fixture) + "-" + sanitizeVCSimName(spec.Name) + ".log"
	logFile, err := os.OpenFile(filepath.Join(logDir, logName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create vcsim log: %v", err)
	}

	args := []string{"-l", "127.0.0.1:0", "-username", vcsimUsername, "-password", testPassword}
	args = append(args, spec.Flags...)
	cmd := exec.Command(binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logFile.Close()
		t.Fatalf("capture vcsim stdout: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logFile.Close()
		t.Fatalf("capture vcsim stderr: %v", err)
	}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatalf("start vcsim %s: %v", spec.Name, err)
	}

	endpoint := &simEndpoint{cmd: cmd}
	wait := make(chan error, 1)
	exited := make(chan struct{})
	endpoint.wait = wait
	endpoint.exited = exited
	ready := make(chan string, 1)
	var logMu sync.Mutex
	writeLog := func(line string) {
		logMu.Lock()
		defer logMu.Unlock()
		_, _ = fmt.Fprintln(logFile, line)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			writeLog(line)
			if vcsimEnvironmentLine.MatchString(line) {
				select {
				case ready <- line:
				default:
				}
			}
		}
		if err := scanner.Err(); err != nil {
			writeLog("stdout: " + err.Error())
		}
	}()
	go func() {
		_, _ = io.Copy(writerFunc(writeLog), stderr)
	}()
	go func() {
		wait <- cmd.Wait()
		close(exited)
	}()

	// Cleanup is registered before readiness so a failed startup cannot leave a
	// child process behind.
	t.Cleanup(func() {
		endpoint.Kill()
		_ = logFile.Close()
	})

	var line string
	select {
	case line = <-ready:
	case <-exited:
		t.Fatalf("vcsim %s exited before readiness; inspect the simulator log", spec.Name)
	case <-time.After(30 * time.Second):
		t.Fatalf("vcsim %s did not print GOVC_URL within 30s", spec.Name)
	}
	match := vcsimEnvironmentLine.FindStringSubmatch(line)
	if len(match) != 2 {
		t.Fatalf("vcsim %s printed malformed readiness line %q", spec.Name, line)
	}
	parsed, err := url.Parse(match[1])
	if err != nil || parsed.Host == "" {
		t.Fatalf("vcsim %s printed invalid GOVC_URL %q: %v", spec.Name, match[1], err)
	}
	parsed.User = nil
	parsed.Path = ""
	parsed.RawPath = ""

	endpoint.URL = parsed.String()
	endpoint.Host = parsed.Hostname()
	endpoint.Port = parsed.Port()
	endpoint.Address = parsed.Host
	probe := &config.Context{Name: spec.Name, Endpoint: endpoint.URL, Username: vcsimUsername, TLS: config.TLSConfig{Mode: config.TLSInsecure}}
	probe.Normalize()
	deadline, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		sha256, _, _, _, probeErr := vsphere.FetchThumbprint(deadline, probe, vsphere.ConnectOptions{})
		if probeErr == nil {
			endpoint.Thumbprint = sha256
			return endpoint
		}
		if deadline.Err() != nil {
			t.Fatalf("vcsim %s readiness probe failed: %v", spec.Name, probeErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// writerFunc adapts the small line logger to io.Writer for stderr, retaining
// one complete log line per simulator message.
type writerFunc func(string)

func (w writerFunc) Write(p []byte) (int, error) {
	text := strings.TrimSuffix(string(p), "\n")
	if text != "" {
		w(text)
	}
	return len(p), nil
}

func (e *simEndpoint) asVCenter() *vcenter {
	return &vcenter{URL: e.URL, Host: e.Host, Port: e.Port, Address: e.Address, Thumbprint: e.Thumbprint}
}

// Kill stops the external simulator. It is exported so partial-coverage
// scenarios can take one endpoint down after a successful capture.
func (e *simEndpoint) Kill() {
	e.stopOnce.Do(func() {
		if e.cmd == nil || e.cmd.Process == nil {
			return
		}
		if err := e.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			_ = e.cmd.Process.Kill()
		}
		select {
		case <-e.wait:
		case <-time.After(5 * time.Second):
			_ = e.cmd.Process.Kill()
			<-e.wait
		}
	})
}

func sanitizeVCSimName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "endpoint"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
