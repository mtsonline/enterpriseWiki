// Package core_attachment provides the "core.attachment" element type: a file
// with description. Files are stored content-addressed (internal/storage);
// each upload creates a new revision, old files stay available in the history.
package core_attachment

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/storage"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed locales/*.json
var locales embed.FS

// Plugin is the attachment plugin.
type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

// FileRef points to a stored file.
type FileRef struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Body is the content of an attachment element.
type Body struct {
	File        *FileRef `json:"file"`
	Description string   `json:"description"`
	Text        string   `json:"text,omitempty"` // extracted text for the search index
}

// Setup registers the element type and the download route.
func (p *Plugin) Setup(host *sdk.Host) error {
	host.RegisterElementType(&Type{files: host.Files})
	// GET /api/v1/x/core.attachment/elements/{id}/file?hash=&inline=1
	// Delivers the file of an element (latest or a given version) after a
	// permission check on the element.
	host.Handle("GET", "/elements/{id}/file", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		lang := r.URL.Query().Get("lang")
		view, err := host.Content.GetElement(ctx, id, lang, false)
		if err != nil {
			return nil, err
		}
		if view.Type != "core.attachment" {
			return nil, apperr.NotFound("file")
		}
		// strictly confidential: no downloads without the permission to manage
		if _, noDL := host.ACL.FileProtection(ctx, view.CategoryID); noDL && !host.ACL.Can(ctx, identity.From(ctx), view.CategoryID, acl.Manage) {
			return nil, apperr.Forbidden("strictly confidential files cannot be downloaded")
		}
		hash := r.URL.Query().Get("hash")
		var raw json.RawMessage
		if hash == "" {
			err = host.DB.QueryRow(ctx, `SELECT body FROM element_revisions WHERE element_id=$1 ORDER BY (lang=$2) DESC, rev DESC LIMIT 1`, id, view.ShownLang).Scan(&raw)
		} else {
			err = host.DB.QueryRow(ctx, `SELECT body FROM element_revisions WHERE element_id=$1 AND body->'file'->>'hash'=$2 ORDER BY rev DESC LIMIT 1`, id, hash).Scan(&raw)
		}
		if err != nil {
			return nil, apperr.NotFound("file")
		}
		var b Body
		if json.Unmarshal(raw, &b) != nil || b.File == nil {
			return nil, apperr.NotFound("file")
		}
		f, err := host.Files.Get(ctx, b.File.Hash)
		if err != nil {
			return nil, err
		}
		host.Files.Serve(w, r, f, b.File.Name, r.URL.Query().Get("inline") == "1")
		return h.Handled{}, nil
	})
	return nil
}

// Type implements content.ElementType.
type Type struct {
	files *storage.Store
}

// TypeID implements content.ElementType.
func (t *Type) TypeID() string { return "core.attachment" }

// Labels implements content.ElementType.
func (t *Type) Labels() map[string]string { return map[string]string{"en": "File", "de": "Datei"} }

// EmptyBody implements content.ElementType.
func (t *Type) EmptyBody() json.RawMessage { return json.RawMessage(`{"file":null,"description":""}`) }

func decode(raw json.RawMessage) (*Body, error) {
	var b Body
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&b); err != nil {
		return nil, errors.New(`body must be {"file": {"hash": "..."}, "description": "..."}`)
	}
	return &b, nil
}

// Normalize checks that the file exists, takes type and size from the store
// (never from the client) and extracts text for the search index.
func (t *Type) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	b, err := decode(raw)
	if err != nil {
		return nil, err
	}
	if b.File == nil || !storage.ValidHash(b.File.Hash) {
		return nil, errors.New("please upload a file")
	}
	if utf8.RuneCountInString(b.Description) > 5000 {
		return nil, errors.New("description too long (max 5000 characters)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	f, err := t.files.Get(ctx, b.File.Hash)
	if err != nil {
		return nil, errors.New("the uploaded file does not exist")
	}
	name := storage.CleanName(b.File.Name)
	if b.File.Name == "" {
		name = f.Name
	}
	b.File = &FileRef{Hash: f.Hash, Name: name, Mime: f.Mime, Size: f.Size}
	b.Description = strings.TrimSpace(b.Description)
	b.Text = t.files.ExtractText(ctx, f)
	return json.Marshal(b)
}

// HumanSize formats a byte count.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Render shows a preview (PDF, image, video, audio), the description and a
// download link. All dynamic values are escaped.
func (t *Type) Render(_ context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	b, err := decode(raw)
	if err != nil {
		return "", err
	}
	if b.File == nil {
		return `<p class="ew-muted">–</p>`, nil
	}
	esc := html.EscapeString
	base := fmt.Sprintf("/api/v1/x/core.attachment/elements/%d/file?hash=%s", rc.ElementID, b.File.Hash)
	var sb strings.Builder
	sb.WriteString(`<figure class="ew-attachment">`)
	m := b.File.Mime
	switch {
	case m == "application/pdf":
		fmt.Fprintf(&sb, `<iframe class="ew-attachment-pdf" src="%s&amp;inline=1" title="%s"></iframe>`, esc(base), esc(b.File.Name))
	case strings.HasPrefix(m, "image/") && !strings.Contains(m, "svg"):
		alt := b.Description
		if alt == "" {
			alt = b.File.Name
		}
		fmt.Fprintf(&sb, `<img class="ew-attachment-img" src="/api/v1/files/%s" alt="%s">`, esc(b.File.Hash), esc(alt))
	case strings.HasPrefix(m, "video/"):
		fmt.Fprintf(&sb, `<video class="ew-attachment-media" controls preload="metadata" src="%s&amp;inline=1"></video>`, esc(base))
	case strings.HasPrefix(m, "audio/"):
		fmt.Fprintf(&sb, `<audio controls preload="metadata" src="%s&amp;inline=1"></audio>`, esc(base))
	}
	sb.WriteString(`<figcaption>`)
	if b.Description != "" {
		for _, para := range strings.Split(b.Description, "\n\n") {
			fmt.Fprintf(&sb, `<p>%s</p>`, strings.ReplaceAll(esc(para), "\n", "<br>"))
		}
	}
	fmt.Fprintf(&sb, `<p class="ew-attachment-meta"><a class="ew-btn ew-btn--secondary" href="%s" download>%s</a> <span>%s · %s</span></p>`,
		esc(base), esc(b.File.Name), esc(m), esc(HumanSize(b.File.Size)))
	sb.WriteString(`</figcaption></figure>`)
	return sb.String(), nil
}

// Text implements content.ElementType (file name, description, extracted text).
func (t *Type) Text(raw json.RawMessage) string {
	b, err := decode(raw)
	if err != nil {
		return ""
	}
	name := ""
	if b.File != nil {
		name = b.File.Name
	}
	return strings.TrimSpace(name + " " + b.Description + " " + b.Text)
}
