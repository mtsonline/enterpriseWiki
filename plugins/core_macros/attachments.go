package core_macros

// Attachments list (plan of blocks and options, 3.2): the files of the media
// library that a page shows, or the files of a category, as a table with
// name, kind, size and date. Everybody sees only files they may see; a file
// that may only be viewed has no download link. People who may add files to
// the category get a button to upload one there.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/media"
)

type attachments struct{ svc *media.Service }

var (
	attachSorts = []string{"name", "updated", "size", "type"}
	attachTypes = []string{"all", "image", "video", "audio", "pdf", "document"}
)

func (attachments) Name() string              { return "attachments" }
func (attachments) Labels() map[string]string { return lm("Attachments", "Anhänge") }
func (attachments) Description() map[string]string {
	return lm("The files this page shows, or the files of a category, as a list with size and date. People who may add files there can upload one directly.",
		"Die Dateien, die diese Seite zeigt, oder die Dateien einer Kategorie, als Liste mit Größe und Datum. Wer dort Dateien ablegen darf, kann direkt eine hochladen.")
}
func (attachments) HasBody() bool { return false }
func (attachments) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "source", Labels: lm("Files", "Dateien"), Type: "select", Options: []string{"page", "category"}, Default: "page",
			OptionLabels: map[string]map[string]string{"page": lm("That this page shows", "Die diese Seite zeigt"), "category": lm("Of a category", "Einer Kategorie")}},
		{Name: "category", Labels: lm("Category (empty: this one)", "Kategorie (leer: diese)"), Type: "custom:category"},
		{Name: "sub", Labels: lm("With subcategories", "Mit Unterkategorien"), Type: "bool"},
		{Name: "type", Labels: lm("Kind", "Art"), Type: "select", Options: attachTypes, Default: "all",
			OptionLabels: map[string]map[string]string{"all": lm("All", "Alle"), "image": lm("Pictures", "Bilder"), "video": lm("Videos", "Videos"),
				"audio": lm("Audio", "Audio"), "pdf": lm("PDF", "PDF"), "document": lm("Documents", "Dokumente")}},
		{Name: "sorting", Labels: lm("Order", "Reihenfolge"), Type: "select", Options: attachSorts, Default: "name",
			OptionLabels: map[string]map[string]string{"name": lm("By name", "Nach Name"), "updated": lm("Newest first", "Neueste zuerst"),
				"size": lm("Largest first", "Größte zuerst"), "type": lm("By kind", "Nach Art")}},
		{Name: "limit", Labels: lm("At most", "Höchstens"), Type: "number", Default: "50"},
		{Name: "upload", Labels: lm("Upload button (category)", "Knopf zum Hochladen (Kategorie)"), Type: "bool", Default: "true"},
	}
}

func (a attachments) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if a.svc == nil || mc.Service == nil {
		return "", nil
	}
	in := media.ListInput{Sort: "name", Limit: intParam(mc.Params, "limit", 50, 1, 200)}
	if s := mc.Params["sorting"]; slices.Contains(attachSorts, s) {
		in.Sort = s
	}
	if t := mc.Params["type"]; t != "" && t != "all" {
		if !slices.Contains(attachTypes, t) {
			return "", apperr.BadRequest("unknown kind %q", t)
		}
		in.Type = t
	}
	cat := int64(0)
	if mc.Params["source"] == "category" {
		cat = mc.CategoryID
		if v := idParam(mc.Params["category"]); v > 0 {
			cat = v
		}
		if cat == 0 {
			return "", apperr.BadRequest("choose a category")
		}
		// the category itself must be visible (its files have their own rights)
		if _, err := mc.Service.Scope(ctx, cat, false); err != nil {
			return "", apperr.BadRequest("the category %d is not there or not visible to you", cat)
		}
		in.CategoryID, in.Recursive = cat, mc.Params["sub"] == "true"
	} else {
		if mc.ElementID == 0 {
			// a new page: it shows no files yet (they are counted when it is saved)
			return `<p class="ew-help">` + esc(pick(lm("The files of this page appear here once it is saved.", "Die Dateien dieser Seite erscheinen hier, sobald sie gespeichert ist."), mc.Lang)) + `</p>`, nil
		}
		in.UsedBy = mc.ElementID
	}
	list, err := a.svc.List(ctx, in)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`<div class="ew-attachments">`)
	if len(list) == 0 {
		fmt.Fprintf(&b, `<p class="ew-help">%s</p>`, esc(pick(lm("No files.", "Keine Dateien."), mc.Lang)))
	} else {
		fmt.Fprintf(&b, `<div class="ew-table-wrap"><table class="ew-table"><caption class="sr-only">%s</caption><thead><tr><th scope="col">%s</th><th scope="col">%s</th><th scope="col">%s</th><th scope="col">%s</th></tr></thead><tbody>`,
			esc(pick(lm("Attachments", "Anhänge"), mc.Lang)), esc(pick(lm("Name", "Name"), mc.Lang)), esc(pick(lm("Kind", "Art"), mc.Lang)),
			esc(pick(lm("Size", "Größe"), mc.Lang)), esc(pick(lm("Changed", "Geändert"), mc.Lang)))
		for _, m := range list {
			name := m.Title
			if name == "" {
				name = m.Name
			}
			// a file that may only be viewed opens in the viewer, never as a download
			link := fmt.Sprintf(`<a href="/api/v1/media/%d/view">%s</a>`, m.ID, esc(name))
			if !m.ViewOnly {
				link += fmt.Sprintf(` <a class="ew-attachments-dl" href="/api/v1/media/%d/download" download>%s<span class="sr-only">: %s</span></a>`, m.ID,
					esc(pick(lm("Download", "Herunterladen"), mc.Lang)), esc(name))
			}
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td data-sort="%d">%s</td><td><time datetime="%s">%s</time></td></tr>`,
				link, esc(kindLabel(m.Mime, mc.Lang)), m.Size, esc(humanSize(m.Size, mc.Lang)),
				m.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"), m.UpdatedAt.Format("2006-01-02"))
		}
		b.WriteString(`</tbody></table></div>`)
	}
	// uploading needs the right to add or change content in the category;
	// the media service checks it again on the upload
	if cat > 0 && mc.Params["upload"] != "false" {
		if c, err := mc.Service.GetCategory(ctx, cat); err == nil && (slices.Contains(c.Permissions, "create") || slices.Contains(c.Permissions, "edit")) {
			fmt.Fprintf(&b, `<div data-ew-widget="core.macros.upload" data-category="%d"></div>`, cat)
		}
	}
	b.WriteString(`</div>`)
	return b.String(), nil
}

func kindLabel(mime, lang string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return pick(lm("Picture", "Bild"), lang)
	case strings.HasPrefix(mime, "video/"):
		return pick(lm("Video", "Video"), lang)
	case strings.HasPrefix(mime, "audio/"):
		return pick(lm("Audio", "Audio"), lang)
	case mime == "application/pdf":
		return "PDF"
	case strings.Contains(mime, "spreadsheet") || strings.Contains(mime, "excel") || mime == "text/csv":
		return pick(lm("Spreadsheet", "Tabelle"), lang)
	case strings.Contains(mime, "presentation") || strings.Contains(mime, "powerpoint"):
		return pick(lm("Presentation", "Präsentation"), lang)
	case strings.Contains(mime, "word") || strings.Contains(mime, "opendocument.text") || mime == "application/rtf":
		return pick(lm("Text document", "Textdokument"), lang)
	case strings.HasPrefix(mime, "text/"):
		return pick(lm("Text", "Text"), lang)
	case strings.Contains(mime, "zip") || strings.Contains(mime, "compressed"):
		return pick(lm("Archive", "Archiv"), lang)
	}
	return pick(lm("File", "Datei"), lang)
}

// humanSize: 1 decimal from KB on, the decimal sign of the language.
func humanSize(n int64, lang string) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f, u := float64(n), 0
	for f >= 1024 && u < len(units)-1 {
		f /= 1024
		u++
	}
	s := fmt.Sprintf("%d %s", n, units[0])
	if u > 0 {
		s = fmt.Sprintf("%.1f %s", f, units[u])
	}
	if lang == "de" {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}
