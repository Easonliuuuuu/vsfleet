package vsphere

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"

	"github.com/easonliuuuuu/vcfleet/internal/config"
)

// ThumbprintMismatchError reports that the certificate presented by a vCenter
// is not the one that was pinned. It carries both fingerprints so the operator
// can see exactly what changed rather than a generic handshake failure.
type ThumbprintMismatchError struct {
	Host     string
	Expected string
	Received string
}

func (e *ThumbprintMismatchError) Error() string {
	return fmt.Sprintf("certificate mismatch for %s\n  expected: %s\n  received: %s\nconnection aborted", e.Host, e.Expected, e.Received)
}

// ThumbprintSHA256 renders a certificate fingerprint the way vCenter does.
func ThumbprintSHA256(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hexColon(sum[:])
}

// ThumbprintSHA1 renders the legacy SHA-1 fingerprint, which is still what the
// vSphere UI and several VMware APIs display.
func ThumbprintSHA1(cert *x509.Certificate) string {
	sum := sha1.Sum(cert.Raw)
	return hexColon(sum[:])
}

func hexColon(raw []byte) string {
	parts := make([]string, len(raw))
	for i, c := range raw {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, ":")
}

// TLSConfig builds the crypto/tls configuration for a context's TLS policy.
//
// The three modes are deliberately explicit. "system" is ordinary CA
// validation; "thumbprint" pins one certificate, which is what most private
// vCenters actually need; "insecure" disables verification and is never a
// side effect of another setting.
func TLSConfig(c *config.Context) (*tls.Config, error) {
	host := c.Host()
	base := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	switch c.TLS.Mode {
	case config.TLSSystem, "":
		return base, nil
	case config.TLSInsecure:
		base.InsecureSkipVerify = true
		return base, nil
	case config.TLSThumbprint:
		expected := config.NormalizeThumbprint(c.TLS.Thumbprint)
		if expected == "" {
			return nil, fmt.Errorf("context %q: tls.thumbprint is not a valid fingerprint", c.Name)
		}
		// Verification is done by hand below, so the standard chain check is
		// turned off rather than merely relaxed.
		base.InsecureSkipVerify = true
		base.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no certificate presented by %s", host)
			}
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("parse certificate from %s: %w", host, err)
			}
			got256 := ThumbprintSHA256(cert)
			got1 := ThumbprintSHA1(cert)
			if expected == got256 || expected == got1 {
				return nil
			}
			received := got256
			if len(expected) == len(got1) {
				received = got1
			}
			return &ThumbprintMismatchError{Host: host, Expected: expected, Received: received}
		}
		return base, nil
	default:
		return nil, fmt.Errorf("context %q: unknown tls mode %q", c.Name, c.TLS.Mode)
	}
}
