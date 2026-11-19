package core_macros

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
	"github.com/mtsonline/enterprisewiki/internal/media"
)

// gallery shows images of the media library as a grid; a click opens them
// large (lightbox, keyboard operable). Either chosen media (media="1,2,3")
// or the images of a category the viewer may see.
type gallery struct{ svc *media.Service }

func (gallery) Name() string              { return "gallery" }
func (gallery) Labels() map[string]string { return lm("Image gallery", "Bildergalerie") }
func (gallery) Description() map[string]string {
	return lm("Shows images of the media library as a gallery: chosen ones or all of a category.",
		"Zeigt Bilder der Medienbibliothek als Galerie: ausgewählte oder alle einer Kategorie.")
}
func (gallery) HasBody() bool { return false }

var galleryLayouts = []string{"grid", "masonry"}

func (gallery) Params() []content.MacroParam {
	return []content.MacroParam{
		{Name: "media", Labels: lm("Media (IDs, comma-separated)", "Medien (IDs, durch Komma getrennt)"), Type: "text"},
		{Name: "category", Labels: lm("…or all images of category (ID)", "…oder alle Bilder der Kategorie (ID)"), Type: "number"},
		{Name: "columns", Labels: lm("Columns on the desktop", "Spalten auf dem Desktop"), Type: "select", Options: []string{"2", "3", "4", "5", "6"}, Default: "4"},
		{Name: "tablet", Labels: lm("Columns on the tablet", "Spalten auf dem Tablet"), Type: "select", Options: []string{"1", "2", "3", "4"}, Default: "3"},
		{Name: "mobile", Labels: lm("Columns on the phone", "Spalten auf dem Handy"), Type: "select", Options: []string{"1", "2"}, Default: "2"},
		{Name: "layout", Labels: lm("Arrangement", "Anordnung"), Type: "select", Options: galleryLayouts, Default: "grid"},
		{Name: "captions", Labels: lm("Show titles", "Titel anzeigen"), Type: "bool", Default: "true"},
		{Name: "limit", Labels: lm("At most (category)", "Höchstens (Kategorie)"), Type: "number", Default: "48"},
	}
}

func (g gallery) Render(ctx context.Context, mc content.MacroContext) (string, error) {
	if g.svc == nil {
		return "", nil
	}
	var list []*media.Medium
	if ids := strings.TrimSpace(mc.Params["media"]); ids != "" {
		for _, f := range strings.FieldsFunc(ids, func(r rune) bool { return r == ',' || r == ' ' }) {
			id, err := strconv.ParseInt(f, 10, 64)
			if err != nil || len(list) >= 200 {
				continue
			}
			if m, err := g.svc.Get(ctx, id); err == nil && isImg(m.Mime) {
				list = append(list, m)
			}
		}
	} else if cat := intParam(mc.Params, "category", 0, 0, 1<<62); cat > 0 {
		var err error
		list, err = g.svc.List(ctx, media.ListInput{CategoryID: int64(cat), Type: "image", Limit: intParam(mc.Params, "limit", 48, 1, 200)})
		if err != nil {
			return "", nil
		}
	}
	if len(list) == 0 {
		return "", nil
	}
	// columns per view; a picture is at least 8 rem wide (phone: at most 2)
	cols := intParam(mc.Params, "columns", 4, 2, 6)
	tab := intParam(mc.Params, "tablet", min(cols, 3), 1, 4)
	mob := intParam(mc.Params, "mobile", 2, 1, 2)
	layout := galleryLayouts[0]
	if mc.Params["layout"] == "masonry" {
		layout = "masonry"
	}
	captions := mc.Params["captions"] != "false"
	label := lm("Image gallery", "Bildergalerie")
	var b strings.Builder
	// masonry: the pictures keep their own height, in columns (the order of reading stays the order of the list)
	fmt.Fprintf(&b, `<ul class="ew-gallery ew-gallery--%s" style="--ew-gallery-cols:%d;--ew-gallery-cols-t:%d;--ew-gallery-cols-m:%d" aria-label="%s">`, layout, cols, tab, mob, esc(pick(label, mc.Lang)))
	for _, m := range list {
		alt := m.Alt
		cls := ""
		if m.ViewOnly {
			cls = ` ew-protected`
		}
		fmt.Fprintf(&b, `<li class="ew-gallery-item%s"><a href="/api/v1/media/%d/view" data-ew-lightbox data-title="%s"><img src="/api/v1/media/%d/view?w=480" alt="%s" loading="lazy"></a>`,
			cls, m.ID, esc(m.Title), m.ID, esc(alt))
		if captions && m.Title != "" {
			fmt.Fprintf(&b, `<span class="ew-gallery-caption">%s</span>`, esc(m.Title))
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String(), nil
}

func isImg(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}
