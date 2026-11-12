// Package storage stores uploaded files content-addressed on the file system:
// the SHA-256 of the content is the file name (data/files/ab/cd/<hash>).
// Identical files are stored once; files are immutable, so versions of
// attachments simply reference different hashes.
package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // dimensions of uploads
	_ "image/jpeg" // dimensions of uploads
	_ "image/png"  // dimensions of uploads
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// File is the metadata of a stored file.
type File struct {
	Hash      string    `json:"hash"`
	Size      int64     `json:"size"`
	Mime      string    `json:"mime"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is the file store.
type Store struct {
	pool *pgxpool.Pool
	cfg  *config.Store
}

// New creates the store.
func New(pool *pgxpool.Pool, cfg *config.Store) *Store { return &Store{pool: pool, cfg: cfg} }

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidHash reports whether h looks like a SHA-256 hex digest.
func ValidHash(h string) bool { return hashRe.MatchString(h) }

func (s *Store) root() string {
	p := s.cfg.Get().Storage.Path
	if p == "" {
		p = "./data"
	}
	return filepath.Join(p, "files")
}

// Path returns the file system path of a hash.
func (s *Store) Path(hash string) string {
	return filepath.Join(s.root(), hash[:2], hash[2:4], hash)
}

// MaxBytes returns the configured upload limit.
func (s *Store) MaxBytes() int64 {
	mb := s.cfg.Get().Storage.MaxUploadMB
	if mb <= 0 {
		mb = 100
	}
	return int64(mb) << 20
}

// CleanName makes an uploaded file name safe for display and downloads.
func CleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		name = name[:200-len(ext)] + ext
	}
	return name
}

// DetectMime determines the content type from the content (not from the
// client) and refines generic results with the file extension.
func DetectMime(head []byte, name string) string {
	m := http.DetectContentType(head)
	if strings.HasPrefix(m, "text/plain") || m == "application/octet-stream" || m == "application/zip" {
		if ext := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); ext != "" {
			// never trust the extension for active content
			if !strings.Contains(ext, "html") && !strings.Contains(ext, "javascript") && !strings.Contains(ext, "svg") {
				m = ext
			}
		}
	}
	if i := strings.Index(m, ";"); i > 0 && !strings.HasPrefix(m, "text/") {
		m = m[:i]
	}
	return m
}

// Put stores the content of r (at most MaxBytes) and records it.
func (s *Store) Put(ctx context.Context, r io.Reader, name string) (*File, error) {
	if err := os.MkdirAll(s.root(), 0o750); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.root(), ".upload-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	head := make([]byte, 0, headSize) // type detection and image dimensions
	limit := s.MaxBytes()
	buf := make([]byte, 64<<10)
	var size int64
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > limit {
				_ = tmp.Close()
				return nil, apperr.BadRequest("file too large (max %d MB)", limit>>20)
			}
			if len(head) < headSize {
				head = append(head, buf[:min(n, headSize-len(head))]...)
			}
			h.Write(buf[:n])
			if _, err := tmp.Write(buf[:n]); err != nil {
				_ = tmp.Close()
				return nil, err
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			_ = tmp.Close()
			return nil, rerr
		}
	}
	if err := CheckPixels(head); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, apperr.BadRequest("the file is empty")
	}
	f := &File{Hash: hex.EncodeToString(h.Sum(nil)), Size: size, Name: CleanName(name)}
	f.Mime = DetectMime(head, f.Name)
	dst := s.Path(f.Hash)
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp.Name(), dst); err != nil {
			return nil, err
		}
		_ = os.Chmod(dst, 0o400)
	}
	var uid *int64
	if id := identity.From(ctx).ID(); id != 0 {
		uid = &id
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO files(hash, size, mime, name, created_by) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (hash) DO UPDATE SET hash=EXCLUDED.hash RETURNING created_at, name`, f.Hash, f.Size, f.Mime, f.Name, uid).Scan(&f.CreatedAt, new(string))
	return f, err
}

// Get returns the metadata of a file.
func (s *Store) Get(ctx context.Context, hash string) (*File, error) {
	if !ValidHash(hash) {
		return nil, apperr.NotFound("file")
	}
	f := &File{Hash: hash}
	err := s.pool.QueryRow(ctx, `SELECT size, mime, name, created_at FROM files WHERE hash=$1`, hash).Scan(&f.Size, &f.Mime, &f.Name, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("file")
	}
	return f, err
}

// Open opens the content of a file.
func (s *Store) Open(hash string) (*os.File, error) {
	if !ValidHash(hash) {
		return nil, apperr.NotFound("file")
	}
	return os.Open(s.Path(hash))
}

// Inline reports whether a type may be shown inline in the browser. Active
// content (HTML, SVG, scripts) is always downloaded.
func Inline(mimeType string) bool {
	switch {
	case mimeType == "application/pdf":
		return true
	case strings.HasPrefix(mimeType, "image/") && !strings.Contains(mimeType, "svg"):
		return true
	case strings.HasPrefix(mimeType, "video/"), strings.HasPrefix(mimeType, "audio/"):
		return true
	}
	return false
}

// Serve writes a file with safe headers. inline is honoured only for types
// that are safe to display.
func (s *Store) Serve(w http.ResponseWriter, r *http.Request, f *File, downloadName string, inline bool) {
	fh, err := s.Open(f.Hash)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer func() { _ = fh.Close() }()
	if downloadName == "" {
		downloadName = f.Name
	}
	disp := "attachment"
	if inline && Inline(f.Mime) {
		disp = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", f.Mime)
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": downloadName}))
	h.Set("X-Content-Type-Options", "nosniff")
	if disp == "inline" && f.Mime == "application/pdf" {
		// The browser's PDF viewer is embedded as a plugin object; a
		// default-src 'none' policy would block it.
		h.Set("Content-Security-Policy", "object-src 'self'; frame-ancestors 'self'")
	} else {
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox")
	}
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("ETag", `"`+f.Hash+`"`)
	http.ServeContent(w, r, "", f.CreatedAt, fh)
}

// GC deletes files that are older than a day and no longer referenced by any
// revision (attachments or images in rich text).
func (s *Store) GC(ctx context.Context) (int, error) {
	q := `SELECT hash FROM files f WHERE created_at < now() - interval '1 day'
		AND NOT EXISTS (SELECT 1 FROM element_revisions r WHERE strpos(r.body::text, f.hash) > 0)`
	refMu.Lock()
	for _, r := range extraRefs {
		q += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM %s x WHERE strpos(x.%s::text, f.hash) > 0)`, r[0], r[1])
	}
	refMu.Unlock()
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return 0, err
	}
	hashes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	keep := s.configHashes()
	n := 0
	for _, h := range hashes {
		if keep[h] {
			continue // e.g. logo or hero image of a template (plugin settings)
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM files WHERE hash=$1`, h); err != nil {
			return n, err
		}
		if err := os.Remove(s.Path(h)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, fmt.Errorf("remove %s: %w", h, err)
		}
		n++
	}
	return n, nil
}

// configHashes returns file hashes used in plugin settings (config.yaml).
func (s *Store) configHashes() map[string]bool {
	out := map[string]bool{}
	if s.cfg == nil {
		return out
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if hashRe.MatchString(x) {
				out[x] = true
			}
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	for _, m := range s.cfg.Get().Settings {
		for _, v := range m {
			walk(v)
		}
	}
	return out
}

var (
	refMu     sync.Mutex
	extraRefs [][2]string
	identRe   = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
)

// AddReference declares that column of table (text or jsonb) may contain
// file hashes, so the garbage collector keeps those files (used by plugins
// that store uploads outside of element bodies).
func (s *Store) AddReference(table, column string) {
	if !identRe.MatchString(table) || !identRe.MatchString(column) {
		panic("storage.AddReference: invalid identifier")
	}
	refMu.Lock()
	defer refMu.Unlock()
	for _, r := range extraRefs {
		if r == [2]string{table, column} {
			return
		}
	}
	extraRefs = append(extraRefs, [2]string{table, column})
}

// CacheGet returns a derived file (e.g. an image with a watermark) from the
// cache; key is any string (hashed). ok is false if it is missing.
func (s *Store) CacheGet(key string) ([]byte, bool) {
	b, err := os.ReadFile(s.cachePath(key)) // #nosec G304 -- path from a SHA-256 of the key
	return b, err == nil
}

// CachePut stores a derived file; failures only cost a recomputation.
func (s *Store) CachePut(key string, data []byte) {
	p := s.cachePath(key)
	if os.MkdirAll(filepath.Dir(p), 0o750) != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

func (s *Store) cachePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	h := hex.EncodeToString(sum[:])
	return filepath.Join(s.root(), ".cache", h[:2], h)
}

// PruneCache removes derived files older than maxAge. Works inside an
// os.Root, so symbolic links cannot lead outside the cache directory.
func (s *Store) PruneCache(maxAge time.Duration) int {
	root, err := os.OpenRoot(filepath.Join(s.root(), ".cache"))
	if err != nil {
		return 0
	}
	defer func() { _ = root.Close() }()
	n := 0
	_ = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // best effort
		}
		if info, err := d.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			if root.Remove(p) == nil {
				n++
			}
		}
		return nil
	})
	return n
}

// IsConfigAsset reports whether hash is used in the settings (logos,
// background images of plugins and themes): such files are public.
func (s *Store) IsConfigAsset(hash string) bool { return s.configHashes()[hash] }

// ServeBytes sends derived content (e.g. an image with a watermark) with
// the same safety headers as Serve; cacheable=false forbids any caching.
func ServeBytes(w http.ResponseWriter, r *http.Request, data []byte, mimeType, name string, inline, cacheable bool) {
	disp := "attachment"
	if inline && Inline(mimeType) {
		disp = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", mimeType)
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	h.Set("X-Content-Type-Options", "nosniff")
	if disp == "inline" && mimeType == "application/pdf" {
		h.Set("Content-Security-Policy", "object-src 'self'; frame-ancestors 'self'")
	} else {
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'self'; sandbox")
	}
	if cacheable {
		h.Set("Cache-Control", "private, max-age=300")
	} else {
		h.Set("Cache-Control", "private, no-store")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// RemoveUnreferenced deletes the given files at once (no grace period) if
// nothing references them any more (demo content). It returns the number
// of removed files.
func (s *Store) RemoveUnreferenced(ctx context.Context, hashes []string) (int, error) {
	if len(hashes) == 0 {
		return 0, nil
	}
	q := `SELECT hash FROM files f WHERE hash = ANY($1)
		AND NOT EXISTS (SELECT 1 FROM element_revisions r WHERE strpos(r.body::text, f.hash) > 0)`
	refMu.Lock()
	for _, r := range extraRefs {
		q += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM %s x WHERE strpos(x.%s::text, f.hash) > 0)`, r[0], r[1])
	}
	refMu.Unlock()
	rows, err := s.pool.Query(ctx, q, hashes)
	if err != nil {
		return 0, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	keep := s.configHashes()
	n := 0
	for _, h := range list {
		if keep[h] || !hashRe.MatchString(h) {
			continue
		}
		if _, err := s.pool.Exec(ctx, `DELETE FROM files WHERE hash=$1`, h); err != nil {
			return n, err
		}
		if err := os.Remove(s.Path(h)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, fmt.Errorf("remove %s: %w", h, err)
		}
		n++
	}
	if n > 0 {
		s.PruneCache(0) // derived files (watermarks, previews) of removed files
	}
	return n, nil
}

// Wipe removes all stored files and derived files of this site (site
// reset). The database rows must already be gone.
func (s *Store) Wipe() error {
	root := s.root()
	if filepath.Base(root) != "files" {
		return fmt.Errorf("refusing to wipe %s", root)
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	return os.MkdirAll(root, 0o750)
}

// headSize is the start of an upload kept for checks (JPEG dimensions
// can follow a long EXIF block).
const headSize = 256 << 10

// MaxPixels limits decoded images (decompression bombs).
const MaxPixels = 80_000_000

// CheckPixels refuses images with more pixels than can be processed
// (S-09: refused when uploaded, not when shown); other files pass. b may be
// the start of the file only.
func CheckPixels(b []byte) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil // not an image the wiki decodes (or dimensions not in b)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return apperr.New(http.StatusUnprocessableEntity, "image_too_large",
			fmt.Sprintf("image too large: %d × %d pixels (at most %d million)", cfg.Width, cfg.Height, MaxPixels/1_000_000))
	}
	return nil
}
