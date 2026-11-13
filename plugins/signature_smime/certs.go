package signature_smime

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// MaxUpload is the largest certificate file accepted (PKCS#12 or PEM).
const MaxUpload = 256 << 10

// parsed is a certificate with its private key and chain, checked.
type parsed struct {
	cert    *x509.Certificate
	key     crypto.Signer
	parents []*x509.Certificate
	emails  []string
	trusted bool
}

var oidEmailAddress = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}

// emailsOf returns the lower-case e-mail addresses of a certificate
// (subjectAltName rfc822Name and the legacy emailAddress attribute).
func emailsOf(c *x509.Certificate) []string {
	var out []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if strings.Count(s, "@") == 1 && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, e := range c.EmailAddresses {
		add(e)
	}
	for _, n := range c.Subject.Names {
		if n.Type.Equal(oidEmailAddress) {
			if s, ok := n.Value.(string); ok {
				add(s)
			}
		}
	}
	return out
}

// parseUpload reads a PKCS#12 file (with password) or PEM (certificates and
// an unencrypted private key) and checks that the certificate may sign
// e-mail now.
func parseUpload(data []byte, password string, now time.Time) (*parsed, error) {
	if len(data) == 0 {
		return nil, errors.New("the file is empty")
	}
	if len(data) > MaxUpload {
		return nil, fmt.Errorf("the file is larger than %d kB", MaxUpload>>10)
	}
	var (
		key   any
		cert  *x509.Certificate
		chain []*x509.Certificate
		err   error
	)
	if bytes.Contains(data, []byte("-----BEGIN ")) {
		key, cert, chain, err = parsePEM(data)
	} else {
		key, cert, chain, err = pkcs12.DecodeChain(data, password)
		if err != nil {
			if errors.Is(err, pkcs12.ErrIncorrectPassword) {
				return nil, errors.New("the password is wrong")
			}
			return nil, errors.New("not a readable PKCS#12 file (.p12/.pfx): " + err.Error())
		}
	}
	if err != nil {
		return nil, err
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("the private key cannot sign")
	}
	if err := checkKey(signer, cert); err != nil {
		return nil, err
	}
	if err := checkUsage(cert, now); err != nil {
		return nil, err
	}
	emails := emailsOf(cert)
	if len(emails) == 0 {
		return nil, errors.New("the certificate contains no e-mail address")
	}
	// only the issuers of this certificate (a PKCS#12 file may hold others)
	parents := orderChain(cert, chain)
	pool, _ := x509.SystemCertPool()
	if pool == nil {
		pool = x509.NewCertPool()
	}
	inter := x509.NewCertPool()
	for _, p := range parents {
		inter.AddCert(p)
	}
	_, verr := cert.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}})
	return &parsed{cert: cert, key: signer, parents: parents, emails: emails, trusted: verr == nil}, nil
}

func parsePEM(data []byte) (any, *x509.Certificate, []*x509.Certificate, error) {
	var (
		certs []*x509.Certificate
		key   any
	)
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		switch b.Type {
		case "CERTIFICATE":
			c, err := x509.ParseCertificate(b.Bytes)
			if err != nil {
				return nil, nil, nil, errors.New("a certificate in the file cannot be read")
			}
			certs = append(certs, c)
		case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY":
			if key != nil {
				return nil, nil, nil, errors.New("the file contains more than one private key")
			}
			if _, enc := b.Headers["DEK-Info"]; enc {
				return nil, nil, nil, errors.New("encrypted PEM keys are not supported: use a PKCS#12 file with a password")
			}
			var err error
			switch b.Type {
			case "PRIVATE KEY":
				key, err = x509.ParsePKCS8PrivateKey(b.Bytes)
			case "RSA PRIVATE KEY":
				key, err = x509.ParsePKCS1PrivateKey(b.Bytes)
			default:
				key, err = x509.ParseECPrivateKey(b.Bytes)
			}
			if err != nil {
				return nil, nil, nil, errors.New("the private key cannot be read")
			}
		case "ENCRYPTED PRIVATE KEY":
			return nil, nil, nil, errors.New("encrypted PEM keys are not supported: use a PKCS#12 file with a password")
		}
	}
	if key == nil {
		return nil, nil, nil, errors.New("the file contains no private key")
	}
	if len(certs) == 0 {
		return nil, nil, nil, errors.New("the file contains no certificate")
	}
	// the certificate of the key, the others are the chain
	pub, ok := key.(crypto.Signer)
	if !ok {
		return nil, nil, nil, errors.New("the private key cannot sign")
	}
	for i, c := range certs {
		if samePublicKey(c.PublicKey, pub.Public()) {
			rest := append(append([]*x509.Certificate{}, certs[:i]...), certs[i+1:]...)
			return key, c, rest, nil
		}
	}
	return nil, nil, nil, errors.New("no certificate in the file belongs to the private key")
}

func samePublicKey(a, b crypto.PublicKey) bool {
	type eq interface{ Equal(crypto.PublicKey) bool }
	x, ok := a.(eq)
	return ok && x.Equal(b)
}

// checkKey accepts RSA from 2048 bits and ECDSA P-256/P-384/P-521 whose
// public key is the one of the certificate.
func checkKey(k crypto.Signer, c *x509.Certificate) error {
	if c == nil {
		return errors.New("the file contains no certificate")
	}
	if !samePublicKey(c.PublicKey, k.Public()) {
		return errors.New("the private key does not belong to the certificate")
	}
	switch pk := k.Public().(type) {
	case *rsa.PublicKey:
		if pk.N.BitLen() < 2048 {
			return errors.New("RSA keys need at least 2048 bits")
		}
	case *ecdsa.PublicKey:
		switch pk.Curve {
		case elliptic.P256(), elliptic.P384(), elliptic.P521():
		default:
			return errors.New("unsupported elliptic curve (P-256, P-384 or P-521)")
		}
	default:
		return errors.New("unsupported key type (RSA or ECDSA)")
	}
	return nil
}

// checkUsage: valid now, may sign (digitalSignature or nonRepudiation) and
// is meant for e-mail (extended key usage emailProtection or any).
func checkUsage(c *x509.Certificate, now time.Time) error {
	if now.Before(c.NotBefore) && c.NotBefore.Sub(now) > 400*24*time.Hour {
		return errors.New("the certificate becomes valid only in more than 400 days")
	}
	if !now.Before(c.NotAfter) {
		return fmt.Errorf("the certificate expired on %s", c.NotAfter.UTC().Format("2006-01-02"))
	}
	if c.KeyUsage != 0 && c.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		return errors.New("the certificate may not be used for signatures (key usage)")
	}
	if len(c.ExtKeyUsage) > 0 || len(c.UnknownExtKeyUsage) > 0 {
		if !slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageEmailProtection) && !slices.Contains(c.ExtKeyUsage, x509.ExtKeyUsageAny) {
			return errors.New("the certificate is not meant for e-mail (extended key usage emailProtection)")
		}
	}
	if c.IsCA {
		return errors.New("this is a certificate of a certification authority, not of a sender")
	}
	return nil
}

// orderChain returns the issuers of c from the given certificates, nearest
// first (at most 8; the root is included when present).
func orderChain(c *x509.Certificate, certs []*x509.Certificate) []*x509.Certificate {
	var out []*x509.Certificate
	cur := c
	for range 8 {
		var next *x509.Certificate
		for _, p := range certs {
			if p.Equal(cur) || slices.ContainsFunc(out, p.Equal) {
				continue
			}
			if cur.CheckSignatureFrom(p) == nil {
				next = p
				break
			}
		}
		if next == nil {
			break
		}
		out = append(out, next)
		if bytes.Equal(next.RawIssuer, next.RawSubject) {
			break
		}
		cur = next
	}
	return out
}

func fingerprintOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// chainPEM encodes the certificate and its issuers.
func chainPEM(c *x509.Certificate, parents []*x509.Certificate) string {
	var b strings.Builder
	for _, x := range append([]*x509.Certificate{c}, parents...) {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: x.Raw})
	}
	return b.String()
}

// parseChainPEM reads what chainPEM wrote.
func parseChainPEM(s string) (*x509.Certificate, []*x509.Certificate, error) {
	var certs []*x509.Certificate
	data := []byte(s)
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			break
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, nil, err
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, nil, errors.New("no certificate")
	}
	return certs[0], certs[1:], nil
}
