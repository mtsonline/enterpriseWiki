// Package tlsutil builds TLS client configurations for connections to mail
// and directory servers: verified by default, optionally against an own CA
// (internal certificate authorities) instead of switching verification off.
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Client returns a TLS configuration for serverName. ca is a PEM certificate
// bundle or the path of a PEM file; its certificates are trusted in addition
// to the system ones. skipVerify switches the certificate check off (only
// for tests; logged as a warning).
func Client(serverName, ca string, skipVerify bool) (*tls.Config, error) {
	c := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(ca) != "" {
		pool, err := Pool(ca)
		if err != nil {
			return nil, err
		}
		c.RootCAs = pool
	}
	if skipVerify {
		slog.Warn("TLS certificate verification is switched off (skip_verify) - use a CA certificate instead", "server", serverName)
		c.InsecureSkipVerify = true // #nosec G402 -- explicit admin opt-in, warned about in the UI and the log
	}
	return c, nil
}

// Pool returns the system certificates plus those of ca (PEM or file path).
func Pool(ca string) (*x509.CertPool, error) {
	pem := []byte(ca)
	if !strings.Contains(ca, "-----BEGIN") {
		b, err := os.ReadFile(strings.TrimSpace(ca)) // #nosec G304 -- path set by an administrator
		if err != nil {
			return nil, fmt.Errorf("CA certificate: %w", err)
		}
		pem = b
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("CA certificate: no PEM certificate found")
	}
	return pool, nil
}

// Validate checks a CA setting (empty is fine).
func Validate(ca string) error {
	if strings.TrimSpace(ca) == "" {
		return nil
	}
	_, err := Pool(ca)
	return err
}
