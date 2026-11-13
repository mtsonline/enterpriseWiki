package mail

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Signer signs outgoing messages with S/MIME (RFC 8551), e.g. the plugin
// signature.smime. The mail service builds the MIME structure; the signer
// only creates the detached CMS signature.
type Signer interface {
	// SignDetached returns a detached CMS SignedData (DER, digest SHA-256)
	// over content for a message sent from the address from, or nil to send
	// the message unsigned. An error keeps the message in the outbox (e.g. an
	// expired certificate with the setting "hold"): a message is never sent
	// unsigned because signing failed unexpectedly. msgID is the outbox id
	// (0 for messages sent immediately).
	SignDetached(ctx context.Context, from string, msgID int64, content []byte) ([]byte, error)
}

// ErrHold is returned by signers that keep a message on purpose (wrapped
// with the reason).
var ErrHold = errors.New("message held")

// SetSigner sets the signer of outgoing messages (nil: none).
func (s *Service) SetSigner(x Signer) {
	s.signerMu.Lock()
	s.signer = x
	s.signerMu.Unlock()
}

func (s *Service) currentSigner() Signer {
	s.signerMu.RLock()
	defer s.signerMu.RUnlock()
	return s.signer
}

// render builds the message and signs it when a signer wants to.
func (s *Service) render(ctx context.Context, in BuildInput, msgID int64) ([]byte, error) {
	head, entity := buildParts(in)
	sg := s.currentSigner()
	if sg == nil {
		return append([]byte(head), entity...), nil
	}
	sig, err := sg.SignDetached(ctx, in.From, msgID, entity)
	if err != nil {
		return nil, err
	}
	if sig == nil {
		return append([]byte(head), entity...), nil
	}
	return []byte(head + signedEntity(entity, sig)), nil
}

// signedEntity wraps a MIME entity and its detached signature into
// multipart/signed (RFC 1847, RFC 8551 section 3.5.3). The signed content
// is exactly entity (with its last line break); the line break before the
// next boundary belongs to the boundary.
func signedEntity(entity, sig []byte) string {
	rnd := make([]byte, 12)
	_, _ = rand.Read(rnd)
	boundary := "ewiki-signed-" + hex.EncodeToString(rnd)
	var b strings.Builder
	b.WriteString(`Content-Type: multipart/signed; protocol="application/pkcs7-signature"; micalg=sha-256; boundary="` + boundary + "\"\r\n\r\n")
	b.WriteString("This is a cryptographically signed message in MIME format.\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.Write(entity)
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/pkcs7-signature; name=\"smime.p7s\"\r\nContent-Transfer-Encoding: base64\r\n" +
		"Content-Disposition: attachment; filename=\"smime.p7s\"\r\nContent-Description: S/MIME Cryptographic Signature\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString(sig)
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}

// holdMessage keeps a message queued because it may not be sent yet
// (signing not possible); it is tried again in 15 minutes.
func (s *Service) holdMessage(ctx context.Context, id int64, reason string) {
	_, _ = s.pool.Exec(ctx, `UPDATE mail_recipients SET last_error=$2, last_code=0,
		next_attempt_at=now() + interval '15 minutes' WHERE message_id=$1 AND status='pending'`, id, truncate(reason))
	_ = s.finalize(ctx, id)
	s.log.Warn("e-mail held", "message", id, "reason", reason)
}
