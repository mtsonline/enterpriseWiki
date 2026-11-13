package signature_smime

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"

	"github.com/smallstep/pkcs7"

	"github.com/mtsonline/enterprisewiki/internal/signature"
)

// signDetached creates a detached CMS SignedData (RFC 5652) over content
// with SHA-256, the signed attributes content type, signing time and
// message digest, and the certificate chain.
func signDetached(content []byte, cert *x509.Certificate, key crypto.Signer, parents []*x509.Certificate) ([]byte, error) {
	sd, err := pkcs7.NewSignedData(content)
	if err != nil {
		return nil, err
	}
	sd.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	// the issuers without the root (receivers have their own roots)
	var chain []*x509.Certificate
	for _, p := range parents {
		if string(p.RawIssuer) != string(p.RawSubject) {
			chain = append(chain, p)
		}
	}
	if err := sd.AddSignerChain(cert, key, chain, pkcs7.SignerInfoConfig{}); err != nil {
		return nil, err
	}
	sd.Detach()
	return sd.Finish()
}

// verifyDetached checks a detached CMS signature over content and returns
// the signing certificate. With roots it also checks the chain.
func verifyDetached(content, der []byte, roots *x509.CertPool) (*x509.Certificate, error) {
	p7, err := pkcs7.Parse(der)
	if err != nil {
		return nil, fmt.Errorf("not a CMS signature: %w", err)
	}
	p7.Content = content
	if roots != nil {
		err = p7.VerifyWithChain(roots)
	} else {
		err = p7.Verify()
	}
	if err != nil {
		return nil, err
	}
	c := p7.GetOnlySigner()
	if c == nil {
		return nil, errors.New("no single signer")
	}
	return c, nil
}

func init() {
	// offline check of the additional seal of signature evidence (also used
	// by "ewiki signature verify --offline"): the signature over the sealed
	// bytes; who the certificate belongs to is shown, trust is the reader's
	// decision (as with any S/MIME signature)
	signature.RegisterCMSVerifier(func(msg, der []byte) signature.Check {
		c, err := verifyDetached(msg, der, nil)
		if err != nil {
			return signature.Check{Name: "seal.cms", OK: false, Detail: err.Error()}
		}
		return signature.Check{Name: "seal.cms", OK: true, Detail: describe(c)}
	})
}

// describe names the holder of a certificate for checks and lists.
func describe(c *x509.Certificate) string {
	parts := []string{c.Subject.CommonName}
	if e := emailsOf(c); len(e) > 0 {
		parts = append(parts, "<"+strings.Join(e, ", ")+">")
	}
	parts = append(parts, "issued by "+c.Issuer.CommonName, "SHA-256 "+fingerprintOf(c))
	return strings.Join(parts, " ")
}
