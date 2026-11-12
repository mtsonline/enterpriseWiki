// Package secrets protects secrets stored in the database (signing keys of
// webhooks, secrets of authenticator apps, secrets of workflows and forms).
//
// Modes (config "secrets.mode"):
//
//   - plain (default): stored as they are; protected by the access to the
//     database (as before).
//   - aes: encrypted with AES-256-GCM. The key lives in a key file on the
//     file system of the wiki (not in the database, not in backups of the
//     database). AES-256 is considered safe against quantum computers too
//     (Grover's algorithm leaves 128 bits).
//   - pq: hybrid post-quantum encryption: each secret gets its own key,
//     agreed with ML-KEM-1024 (FIPS 203) and X25519 and derived with
//     HKDF-SHA256, then AES-256-GCM. Secure as long as one of the two
//     methods holds; the public part could later encrypt without being able
//     to decrypt (write-only nodes).
//
// The key file can itself be encrypted with a password from the environment
// (Argon2id): then the file alone does not reveal anything either. With the
// database on another host an attacker needs the database, the key file
// and – if set – the password.
//
// Every encrypted value is bound to its place (table and row) with
// additional data: it cannot be copied into another row. Values without
// the prefix are plain (old values, mode plain); "ewiki secrets rewrap"
// (or POST /api/v1/admin/secrets/rewrap) brings all registered columns to
// the current mode.
package secrets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"

	"github.com/mtsonline/enterprisewiki/internal/db"
)

// Modes.
const (
	ModePlain = "plain"
	ModeAES   = "aes"
	ModePQ    = "pq"
)

// Prefixes of encrypted values.
const (
	prefixAES = "ews1:a:"
	prefixPQ  = "ews1:q:"
)

// ErrNoKey: an encrypted value cannot be read without the key file.
var ErrNoKey = errors.New("the value is encrypted, but the key file of the secrets is not loaded")

// Box seals and opens secrets.
type Box struct {
	mode string
	aes  []byte // 32 bytes
	kem  *mlkem.DecapsulationKey1024
	x    *ecdh.PrivateKey
}

// keyFile is the content of the key file.
type keyFile struct {
	Version int    `json:"version"`
	AES     string `json:"aes,omitempty"`    // base64, 32 bytes
	KEM     string `json:"mlkem,omitempty"`  // base64, 64 byte seed of ML-KEM-1024
	X25519  string `json:"x25519,omitempty"` // base64, 32 bytes
	// wrapped with a password
	KDF    string `json:"kdf,omitempty"` // argon2id
	Salt   string `json:"salt,omitempty"`
	Time   uint32 `json:"t,omitempty"`
	Memory uint32 `json:"m,omitempty"` // KiB
	Lanes  uint8  `json:"p,omitempty"`
	Nonce  string `json:"nonce,omitempty"`
	Data   string `json:"data,omitempty"` // the inner key file, encrypted
}

// Plain returns a box of the mode plain (no key; encrypted values cannot be opened).
func Plain() *Box { return &Box{mode: ModePlain} }

// Mode returns the mode new values are stored in.
func (b *Box) Mode() string {
	if b == nil {
		return ModePlain
	}
	return b.mode
}

// HasKey reports whether a key file is loaded.
func (b *Box) HasKey() bool { return b != nil && len(b.aes) == 32 && b.kem != nil && b.x != nil }

// Load opens the key file path for mode. password ("" = none) opens a
// wrapped key file. create makes a new key file when there is none (at the
// installation or when switching from plain).
func Load(mode, path, password string, create bool) (*Box, error) {
	switch mode {
	case "", ModePlain:
		b := &Box{mode: ModePlain}
		// a key file that exists is still loaded: encrypted values stay readable
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				k, err := readKey(path, password)
				if err != nil {
					return nil, err
				}
				if err := b.setKeys(k); err != nil {
					return nil, err
				}
			}
		}
		return b, nil
	case ModeAES, ModePQ:
	default:
		return nil, fmt.Errorf("secrets.mode must be plain, aes or pq (not %q)", mode)
	}
	if path == "" {
		return nil, errors.New("secrets.key_file is required for the modes aes and pq")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if !create {
			return nil, fmt.Errorf("the key file %s of the secrets does not exist", path)
		}
		if err := NewKeyFile(path, password); err != nil {
			return nil, err
		}
	}
	k, err := readKey(path, password)
	if err != nil {
		return nil, err
	}
	b := &Box{mode: mode}
	return b, b.setKeys(k)
}

func (b *Box) setKeys(k *keyFile) error {
	dec := func(s string, n int, what string) ([]byte, error) {
		v, err := base64.StdEncoding.DecodeString(s)
		if err != nil || len(v) != n {
			return nil, fmt.Errorf("the key file is damaged (%s)", what)
		}
		return v, nil
	}
	var err error
	if b.aes, err = dec(k.AES, 32, "aes"); err != nil {
		return err
	}
	seed, err := dec(k.KEM, 64, "mlkem")
	if err != nil {
		return err
	}
	if b.kem, err = mlkem.NewDecapsulationKey1024(seed); err != nil {
		return err
	}
	xs, err := dec(k.X25519, 32, "x25519")
	if err != nil {
		return err
	}
	b.x, err = ecdh.X25519().NewPrivateKey(xs)
	return err
}

// argon2id parameters of wrapped key files.
const (
	kdfTime   = 3
	kdfMemory = 64 * 1024
	kdfLanes  = 4
)

// NewKeyFile writes a new key file (mode 0600); with a password it is
// wrapped. It never replaces an existing file.
func NewKeyFile(path, password string) error {
	a := make([]byte, 32)
	if _, err := rand.Read(a); err != nil {
		return err
	}
	dk, err := mlkem.GenerateKey1024()
	if err != nil {
		return err
	}
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	inner := keyFile{Version: 1, AES: b64(a), KEM: b64(dk.Bytes()), X25519: b64(x.Bytes())}
	out := inner
	if password != "" {
		raw, err := json.Marshal(inner)
		if err != nil {
			return err
		}
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return err
		}
		key := argon2.IDKey([]byte(password), salt, kdfTime, kdfMemory, kdfLanes, 32)
		nonce, ct, err := gcmSeal(key, raw, []byte("ewiki-secrets-keyfile"))
		if err != nil {
			return err
		}
		out = keyFile{Version: 1, KDF: "argon2id", Salt: b64(salt), Time: kdfTime, Memory: kdfMemory, Lanes: kdfLanes, Nonce: b64(nonce), Data: b64(ct)}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- path from the configuration
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func readKey(path, password string) (*keyFile, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("the key file %s may be read by others (chmod 600)", path)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path from the configuration
	if err != nil {
		return nil, err
	}
	var k keyFile
	if err := json.Unmarshal(raw, &k); err != nil || k.Version != 1 {
		return nil, fmt.Errorf("the key file %s is damaged", path)
	}
	if k.KDF == "" {
		return &k, nil
	}
	if k.KDF != "argon2id" {
		return nil, fmt.Errorf("unknown key derivation %q", k.KDF)
	}
	if password == "" {
		return nil, errors.New("the key file of the secrets is protected by a password: set the variable named in secrets.password_env")
	}
	salt, e1 := base64.StdEncoding.DecodeString(k.Salt)
	nonce, e2 := base64.StdEncoding.DecodeString(k.Nonce)
	ct, e3 := base64.StdEncoding.DecodeString(k.Data)
	if e1 != nil || e2 != nil || e3 != nil || k.Time == 0 || k.Time > 20 || k.Memory < 8*1024 || k.Memory > 4*1024*1024 || k.Lanes == 0 {
		return nil, fmt.Errorf("the key file %s is damaged", path)
	}
	key := argon2.IDKey([]byte(password), salt, k.Time, k.Memory, k.Lanes, 32)
	inner, err := gcmOpen(key, nonce, ct, []byte("ewiki-secrets-keyfile"))
	if err != nil {
		return nil, errors.New("wrong password for the key file of the secrets")
	}
	var in keyFile
	if err := json.Unmarshal(inner, &in); err != nil {
		return nil, fmt.Errorf("the key file %s is damaged", path)
	}
	return &in, nil
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func gcmSeal(key, plain, aad []byte) (nonce, ct []byte, err error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, g.Seal(nil, nonce, plain, aad), nil
}

func gcmOpen(key, nonce, ct, aad []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(nonce) != g.NonceSize() {
		return nil, errors.New("invalid nonce")
	}
	return g.Open(nil, nonce, ct, aad)
}

// Sealed reports whether v is an encrypted value.
func Sealed(v []byte) bool {
	return bytes.HasPrefix(v, []byte(prefixAES)) || bytes.HasPrefix(v, []byte(prefixPQ))
}

const pqInfo = "ewiki-secrets-pq-v1"

// Seal protects plain for the place aad (e.g. "webhooks:12") in the
// current mode. Empty values stay empty.
func (b *Box) Seal(plain []byte, aad string) ([]byte, error) {
	if len(plain) == 0 || b.Mode() == ModePlain {
		return plain, nil
	}
	if !b.HasKey() {
		return nil, ErrNoKey
	}
	switch b.mode {
	case ModeAES:
		nonce, ct, err := gcmSeal(b.aes, plain, []byte(aad))
		if err != nil {
			return nil, err
		}
		return []byte(prefixAES + b64(append(nonce, ct...))), nil
	default: // pq
		ss1, kemCT := b.kem.EncapsulationKey().Encapsulate()
		eph, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		ss2, err := eph.ECDH(b.x.PublicKey())
		if err != nil {
			return nil, err
		}
		key, err := pqKey(ss1, ss2, kemCT, eph.PublicKey().Bytes())
		if err != nil {
			return nil, err
		}
		nonce, ct, err := gcmSeal(key, plain, []byte(aad))
		if err != nil {
			return nil, err
		}
		out := append(append(append(kemCT, eph.PublicKey().Bytes()...), nonce...), ct...)
		return []byte(prefixPQ + b64(out)), nil
	}
}

// pqKey combines both shared secrets (and what was sent) into the key.
func pqKey(ss1, ss2, kemCT, epk []byte) ([]byte, error) {
	ikm := append(append([]byte{}, ss1...), ss2...)
	salt := sha256.Sum256(append(append([]byte{}, kemCT...), epk...))
	return hkdf.Key(sha256.New, ikm, salt[:], pqInfo, 32)
}

// Open returns the plain value of v (stored for aad); plain values are
// returned as they are.
func (b *Box) Open(v []byte, aad string) ([]byte, error) {
	switch {
	case bytes.HasPrefix(v, []byte(prefixAES)):
		if !b.HasKey() {
			return nil, ErrNoKey
		}
		raw, err := base64.StdEncoding.DecodeString(string(v[len(prefixAES):]))
		if err != nil || len(raw) < 12+16 {
			return nil, errors.New("damaged secret")
		}
		out, err := gcmOpen(b.aes, raw[:12], raw[12:], []byte(aad))
		if err != nil {
			return nil, errors.New("the secret cannot be decrypted here (wrong key or moved value)")
		}
		return out, nil
	case bytes.HasPrefix(v, []byte(prefixPQ)):
		if !b.HasKey() {
			return nil, ErrNoKey
		}
		raw, err := base64.StdEncoding.DecodeString(string(v[len(prefixPQ):]))
		if err != nil || len(raw) < mlkem.CiphertextSize1024+32+12+16 {
			return nil, errors.New("damaged secret")
		}
		kemCT, epk, rest := raw[:mlkem.CiphertextSize1024], raw[mlkem.CiphertextSize1024:mlkem.CiphertextSize1024+32], raw[mlkem.CiphertextSize1024+32:]
		ss1, err := b.kem.Decapsulate(kemCT)
		if err != nil {
			return nil, errors.New("damaged secret")
		}
		pub, err := ecdh.X25519().NewPublicKey(epk)
		if err != nil {
			return nil, errors.New("damaged secret")
		}
		ss2, err := b.x.ECDH(pub)
		if err != nil {
			return nil, errors.New("damaged secret")
		}
		key, err := pqKey(ss1, ss2, kemCT, epk)
		if err != nil {
			return nil, err
		}
		out, err := gcmOpen(key, rest[:12], rest[12:], []byte(aad))
		if err != nil {
			return nil, errors.New("the secret cannot be decrypted here (wrong key or moved value)")
		}
		return out, nil
	}
	return v, nil
}

// SealString and OpenString are Seal and Open for text columns.
func (b *Box) SealString(plain, aad string) (string, error) {
	v, err := b.Seal([]byte(plain), aad)
	return string(v), err
}

// OpenString opens a text value.
func (b *Box) OpenString(v, aad string) (string, error) {
	out, err := b.Open([]byte(v), aad)
	return string(out), err
}

// ---------------------------------------------------------------- registered columns

// Column is a column with secrets (for rewrap and status).
type Column struct {
	Table string // e.g. "webhooks"
	// Key is an SQL expression of the row (text) that is also the place
	// of the values: aad = Table + ":" + key.
	Key   string
	Value string
	Text  bool // text column (otherwise bytea)
}

var (
	colMu   sync.Mutex
	columns = map[string]Column{}
)

// Register declares a column with secrets (core and plugins).
func Register(c Column) {
	colMu.Lock()
	columns[c.Table+"."+c.Value] = c
	colMu.Unlock()
}

// Columns returns the registered columns.
func Columns() []Column {
	colMu.Lock()
	defer colMu.Unlock()
	out := make([]Column, 0, len(columns))
	for _, c := range columns {
		out = append(out, c)
	}
	return out
}

// AAD returns the place of a value.
func AAD(table, key string) string { return table + ":" + key }

// DB is what Rewrap and Count need (a pool or a transaction).
type DB = db.Querier

// Stats counts the values of the registered columns by kind.
type Stats struct {
	Plain int `json:"plain"`
	AES   int `json:"aes"`
	PQ    int `json:"pq"`
}

func (s *Stats) add(v []byte) {
	switch {
	case bytes.HasPrefix(v, []byte(prefixAES)):
		s.AES++
	case bytes.HasPrefix(v, []byte(prefixPQ)):
		s.PQ++
	case len(v) > 0:
		s.Plain++
	}
}

// tableExists guards plugins that are not installed (their tables are missing).
func tableExists(ctx context.Context, q DB, table string) bool {
	ok := false
	_ = q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&ok)
	return ok
}

// Count returns the statistics of all registered columns.
func Count(ctx context.Context, q DB) (Stats, error) {
	var st Stats
	for _, c := range Columns() {
		if !tableExists(ctx, q, c.Table) {
			continue
		}
		rows, err := q.Query(ctx, fmt.Sprintf(`SELECT %s::bytea FROM %s`, quoteCol(c), quoteIdent(c.Table)))
		if err != nil {
			return st, err
		}
		for rows.Next() {
			var v []byte
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return st, err
			}
			st.add(v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return st, err
		}
	}
	return st, nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func quoteCol(c Column) string {
	if c.Text {
		return "convert_to(" + quoteIdent(c.Value) + ", 'UTF8')"
	}
	return quoteIdent(c.Value)
}

// Rewrap stores all values of the registered columns in the current mode
// (encrypting plain values, decrypting for plain, re-encrypting others).
func Rewrap(ctx context.Context, q DB, b *Box) (int, error) {
	n := 0
	for _, c := range Columns() {
		if !tableExists(ctx, q, c.Table) {
			continue
		}
		// #nosec G201 -- identifiers of registered columns (code, not input)
		rows, err := q.Query(ctx, fmt.Sprintf(`SELECT (%s)::text, %s::bytea FROM %s`, c.Key, quoteCol(c), quoteIdent(c.Table)))
		if err != nil {
			return n, err
		}
		type item struct {
			key string
			v   []byte
		}
		var list []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.key, &it.v); err != nil {
				rows.Close()
				return n, err
			}
			list = append(list, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return n, err
		}
		for _, it := range list {
			if len(it.v) == 0 {
				continue
			}
			aad := AAD(c.Table, it.key)
			plain, err := b.Open(it.v, aad)
			if err != nil {
				return n, fmt.Errorf("%s %s: %w", c.Table, it.key, err)
			}
			out, err := b.Seal(plain, aad)
			if err != nil {
				return n, err
			}
			if bytes.Equal(out, it.v) {
				continue
			}
			var val any = out
			if c.Text {
				val = string(out)
			}
			// #nosec G201 -- identifiers of registered columns (code, not input)
			if _, err := q.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s=$1 WHERE (%s)::text=$2`, quoteIdent(c.Table), quoteIdent(c.Value), c.Key), val, it.key); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
