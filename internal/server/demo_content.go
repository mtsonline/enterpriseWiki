package server

// The content of the demo: a category tree that shows the features of the
// wiki with texts about the wiki itself (German and English, other active
// languages get the English text).

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// tr returns a text for every active language (German for de, else English).
func (d *demoRun) tr(en, de string) map[string]string {
	out := map[string]string{}
	for _, l := range d.langs {
		if l == "de" {
			out[l] = de
		} else {
			out[l] = en
		}
	}
	return out
}

func (d *demoRun) lang() string { return d.langs[0] }

func htmlBodyOf(src string) map[string]string {
	return map[string]string{"format": "html", "source": src}
}

func macro(name string, params map[string]string, body string) string {
	p := ""
	if len(params) > 0 {
		var parts []string
		for k, v := range params {
			parts = append(parts, strconv.Quote(k)+":"+strconv.Quote(v))
		}
		p = ` data-params='{` + strings.Join(parts, ",") + `}'`
	}
	return `<div data-macro="` + name + `"` + p + `>` + body + `</div>`
}

// category creates a category below parent (0 = top level).
func (d *demoRun) category(parent int64, en, de, descEN, descDE string, extra map[string]any) int64 {
	body := map[string]any{"titles": d.tr(en, de), "descriptions": d.tr(descEN, descDE), "tags": []string{"demo"}}
	if parent > 0 {
		body["parent_id"] = parent
	}
	for k, v := range extra {
		body[k] = v
	}
	return d.create("category", "/api/v1/categories", body)
}

// element creates an element and adds the translations of the other languages.
func (d *demoRun) element(cat int64, typ string, titles map[string]string, bodies map[string]any, extra map[string]any) int64 {
	l := d.lang()
	in := map[string]any{"category_id": cat, "type": typ, "lang": l, "title": titles[l], "body": bodies[l], "tags": []string{"demo"}}
	for k, v := range extra {
		in[k] = v
	}
	id := d.create("element", "/api/v1/elements", in)
	for _, o := range d.langs[1:] {
		if d.err != nil || id == 0 {
			break
		}
		if b, ok := bodies[o]; ok {
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/elements/%d", id), map[string]any{"lang": o, "title": titles[o], "body": b, "comment": "Demo"}, nil)
		}
	}
	return id
}

// page creates an HTML page in all languages.
func (d *demoRun) page(cat int64, en, de, srcEN, srcDE string, extra map[string]any) int64 {
	bodies := map[string]any{}
	for l, src := range d.tr(srcEN, srcDE) {
		bodies[l] = htmlBodyOf(src)
	}
	return d.element(cat, "core.html", d.tr(en, de), bodies, extra)
}

// same returns the same body for all languages.
func (d *demoRun) same(b any) map[string]any {
	out := map[string]any{}
	for _, l := range d.langs {
		out[l] = b
	}
	return out
}

// ---------------------------------------------------------------- generated files

// demoPicture draws a landscape (sky, sun, hills) in the given colours.
func demoPicture(sky, hill color.RGBA, sunX int) []byte {
	const w, hgt = 960, 540
	img := image.NewRGBA(image.Rect(0, 0, w, hgt))
	for y := 0; y < hgt; y++ {
		f := float64(y) / hgt
		row := color.RGBA{uint8(float64(sky.R) * (1 - f*0.4)), uint8(float64(sky.G) * (1 - f*0.3)), uint8(min(255, float64(sky.B)*(1-f*0.1)+40*f)), 255}
		for x := 0; x < w; x++ {
			c := row
			dx, dy := x-sunX, y-150
			if dx*dx+dy*dy < 60*60 {
				c = color.RGBA{255, 214, 102, 255}
			}
			// two hills (sine waves)
			h1 := 380 + int(40*sinApprox(float64(x)/150))
			h2 := 430 + int(30*sinApprox(float64(x)/90+2))
			switch {
			case y > h2:
				c = color.RGBA{hill.R / 2, hill.G / 2, hill.B / 2, 255}
			case y > h1:
				c = hill
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// sinApprox is a cheap sine (Bhaskara) for the hills.
func sinApprox(x float64) float64 {
	const pi = 3.141592653589793
	for x > pi {
		x -= 2 * pi
	}
	for x < -pi {
		x += 2 * pi
	}
	s := 1.0
	if x < 0 {
		x, s = -x, -1
	}
	return s * 16 * x * (pi - x) / (5*pi*pi - 4*x*(pi-x))
}

// demoPDF is a one-page PDF with a few lines of text.
func demoPDF(lines []string) []byte {
	var text strings.Builder
	text.WriteString("BT /F1 18 Tf 72 760 Td 24 TL ")
	for _, l := range lines {
		l = strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(l)
		text.WriteString("(" + l + ") Tj T* ")
	}
	text.WriteString("ET")
	stream := text.String()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// demoDocx is a minimal Word document with paragraphs.
func demoDocx(paras []string) []byte {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	add := func(name, content string) {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	add("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`)
	add("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`)
	var body strings.Builder
	for _, p := range paras {
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">` + strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(p) + `</w:t></w:r></w:p>`)
	}
	add("word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`+body.String()+`</w:body></w:document>`)
	_ = zw.Close()
	return b.Bytes()
}

// ---------------------------------------------------------------- the demo

func buildDemo(d *demoRun) {
	site := d.s.app.Site
	suffix := ""
	if site != "main" {
		suffix = "-" + site // accounts and groups may be shared by all sites
	}
	inherit := true
	// what is placed where outside the pages (menu, blocks, carousel …): the
	// page "How the demo is built" lists it at the end
	var where []whereNote

	// ---- the demo area
	root := d.category(0, "Demo: Enterprise Wiki", "Demo: Enterprise Wiki",
		"A tour through the features – created by Administration → Demo, removed there without a trace.",
		"Ein Rundgang durch die Funktionen – angelegt unter Administration → Demo und dort spurlos wieder entfernt.", nil)
	d.call(http.MethodPost, fmt.Sprintf("/api/v1/categories/%d/acl", root), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": inherit}, nil)

	// ---- people: demo accounts (no sign-in possible) in groups
	editorsGroup := d.create("group", "/api/v1/admin/groups", map[string]any{"name": "Demo: Redaktion" + suffix, "description": "Demo"})
	readersGroup := d.create("group", "/api/v1/admin/groups", map[string]any{"name": "Demo: Fachbereich" + suffix, "description": "Demo"})
	people := []struct {
		user, name, job, dept string
		group                 int64
	}{
		{"anna", "Anna Berger", "Chefredakteurin", "Kommunikation", editorsGroup},
		{"ben", "Ben Huber", "Technischer Redakteur", "IT", editorsGroup},
		{"clara", "Clara Novak", "Projektleiterin", "Organisation", readersGroup},
		{"david", "David Steiner", "Datenschutzbeauftragter", "Recht", readersGroup},
	}
	handles := map[string]string{}
	userIDs := map[string]int64{}
	for _, pp := range people {
		if d.err != nil {
			break
		}
		uname := "demo-" + pp.user + suffix
		var uid int64
		// no e-mail address: the demo never sends mails
		if err := d.s.app.DB.QueryRow(d.ctx, `INSERT INTO users(username, provider, subject, display_name)
			VALUES ($1, 'demo', $1, $2) RETURNING id`, uname, pp.name).Scan(&uid); err != nil {
			d.err = fmt.Errorf("demo user: %w", err)
			break
		}
		d.track("user", uid)
		if _, err := d.s.app.DB.Exec(d.ctx, `INSERT INTO user_groups(user_id, group_id) VALUES ($1,$2)`, uid, pp.group); err != nil {
			d.err = err
			break
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d/profile", uid), map[string]string{
			"job_title": pp.job, "department": pp.dept, "phone": "+43 1 234 56 7" + strconv.Itoa(len(handles)), "office": "Raum " + strconv.Itoa(101+len(handles)),
			"about": "Demo-Konto (keine Anmeldung möglich).",
		}, nil)
		handles[pp.user] = uname
		userIDs[pp.user] = uid
	}
	// the editors may edit the demo area
	d.call(http.MethodPost, fmt.Sprintf("/api/v1/categories/%d/acl", root), map[string]any{"subject_type": "group", "subject_id": editorsGroup, "permissions": 1 | 2 | 4 | 8, "inherit": inherit}, nil)

	// ---- media: a hidden category as folder with pictures and documents
	mediaCat := d.category(root, "Media", "Medien", "Pictures and documents of the demo (hidden category = folder).",
		"Bilder und Dokumente der Demo (unsichtbare Kategorie = Ordner).", map[string]any{"hidden": true})
	pics := []struct {
		name, en, de string
		sky, hill    color.RGBA
		sun          int
	}{
		{"morgen.png", "Morning in the hills", "Morgen in den Hügeln", color.RGBA{120, 170, 230, 255}, color.RGBA{70, 140, 70, 255}, 200},
		{"mittag.png", "Noon over the valley", "Mittag über dem Tal", color.RGBA{60, 140, 240, 255}, color.RGBA{90, 160, 60, 255}, 480},
		{"abend.png", "Evening light", "Abendlicht", color.RGBA{240, 140, 90, 255}, color.RGBA{60, 90, 60, 255}, 760},
	}
	var picIDs []string
	for _, pc := range pics {
		var m idResp
		d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": mediaCat, "title": d.tr(pc.en, pc.de)[d.lang()],
			"alt": d.tr(pc.en, pc.de)[d.lang()]}, pc.name, demoPicture(pc.sky, pc.hill, pc.sun)), &m)
		d.track("medium", m.ID)
		picIDs = append(picIDs, strconv.FormatInt(m.ID, 10))
	}
	var guide, confidential idResp
	d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": mediaCat, "title": d.tr("Quick guide", "Kurzanleitung")[d.lang()]},
		"kurzanleitung.pdf", demoPDF([]string{"Enterprise Wiki - Kurzanleitung", "", "1. Kategorien ordnen Inhalte.", "2. Seiten haben Versionen.", "3. Rechte vererben sich."})), &guide)
	d.track("medium", guide.ID)
	d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": mediaCat, "title": d.tr("Confidential minutes (view only, watermark)", "Vertrauliches Protokoll (nur ansehen, Wasserzeichen)")[d.lang()],
		"download": false, "watermark": map[string]any{"mode": "viewer_view", "user": true, "date": true, "comment": "Demo"}},
		"protokoll.pdf", demoPDF([]string{"Protokoll der Demo-Sitzung", "", "Nur zur Ansicht - mit dem Namen des Betrachters."})), &confidential)
	d.track("medium", confidential.ID)
	var letter idResp
	d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": mediaCat, "title": d.tr("Letter template (office)", "Briefvorlage (Office)")[d.lang()]},
		"briefvorlage.docx", demoDocx([]string{"Briefvorlage", "Sehr geehrte Damen und Herren,", "dieses Dokument lässt sich im Office-Programm des Wikis bearbeiten."})), &letter)
	d.track("medium", letter.ID)
	if d.err != nil {
		return
	}
	img := func(i int, alt string) string {
		return `<figure><img src="/api/v1/media/` + picIDs[i] + `/view" alt="` + alt + `"><figcaption>` + alt + `</figcaption></figure>`
	}

	// ---- 1. getting started: a page with (almost) every macro
	start := d.category(root, "Getting started", "Erste Schritte", "What a wiki page can do.", "Was eine Wiki-Seite kann.", nil)
	overviewEN := `<h2>Welcome to the tour</h2><p>This demo shows the features of Enterprise Wiki with example content. Everything here can be edited – try the <strong>Edit</strong> mode. Under <em>Administration → Demo</em> the whole demo disappears again with one click.</p>` +
		macro("toc", map[string]string{"levels": "3"}, "") +
		macro("panel", map[string]string{"type": "info", "title": "Tip"}, `<p>Every change creates a version. Under <em>History</em> you compare and restore versions.</p>`) +
		`<h2>Status and properties</h2><p>Status labels: ` + macro("status", map[string]string{"text": "DONE", "color": "green"}, "") + ` ` + macro("status", map[string]string{"text": "IN PROGRESS", "color": "yellow"}, "") + ` ` + macro("status", map[string]string{"text": "BLOCKED", "color": "red"}, "") + `</p>` +
		`<p>The table above the content shows the <strong>page properties</strong> (owner, status, review date).</p>` +
		`<h2>Pictures</h2>` + img(0, "Morning in the hills") +
		`<h3>Gallery of the media category</h3>` + macro("gallery", map[string]string{"category": strconv.FormatInt(mediaCat, 10)}, "") +
		`<h2>Columns and expandable sections</h2>` + macro("columns", nil, `<p><strong>Left column:</strong> categories organise content in a tree with inherited permissions.</p><hr><p><strong>Right column:</strong> elements are pages, blogs, FAQs, calendars, team pages, files and more.</p>`) +
		macro("expand", map[string]string{"title": "Show more details"}, `<p>This text appears after a click. Screen readers announce the state.</p>`) +
		`<h2>Pages of this area</h2>` + macro("children", map[string]string{"category": strconv.FormatInt(root, 10), "show": "all"}, "") +
		`<h2>Recently changed</h2>` + macro("recent", map[string]string{"count": "5", "scope": "category"}, "") +
		`<h2>Upcoming events</h2>` + macro("events", map[string]string{"limit": "3", "days": "60"}, "") +
		`<p>Mention people with @: @` + handles["anna"] + ` gets a notification.</p>`
	overviewDE := `<h2>Willkommen zum Rundgang</h2><p>Diese Demo zeigt die Funktionen des Enterprise Wiki mit Beispielinhalten. Alles hier lässt sich bearbeiten – probiere den <strong>Bearbeitungsmodus</strong>. Unter <em>Administration → Demo</em> verschwindet die ganze Demo mit einem Klick wieder.</p>` +
		macro("toc", map[string]string{"levels": "3"}, "") +
		macro("panel", map[string]string{"type": "info", "title": "Tipp"}, `<p>Jede Änderung ist eine Version. Unter <em>Verlauf</em> vergleichst du Versionen und stellst sie wieder her.</p>`) +
		`<h2>Status und Eigenschaften</h2><p>Status-Labels: ` + macro("status", map[string]string{"text": "ERLEDIGT", "color": "green"}, "") + ` ` + macro("status", map[string]string{"text": "IN ARBEIT", "color": "yellow"}, "") + ` ` + macro("status", map[string]string{"text": "BLOCKIERT", "color": "red"}, "") + `</p>` +
		`<p>Die Tabelle über dem Inhalt zeigt die <strong>Seiteneigenschaften</strong> (Verantwortlich, Status, Prüfdatum).</p>` +
		`<h2>Bilder</h2>` + img(0, "Morgen in den Hügeln") +
		`<h3>Galerie der Medien-Kategorie</h3>` + macro("gallery", map[string]string{"category": strconv.FormatInt(mediaCat, 10)}, "") +
		`<h2>Spalten und Aufklappbereiche</h2>` + macro("columns", nil, `<p><strong>Linke Spalte:</strong> Kategorien ordnen Inhalte in einem Baum mit vererbten Rechten.</p><hr><p><strong>Rechte Spalte:</strong> Elemente sind Seiten, Blogs, FAQs, Kalender, Teamseiten, Dateien und mehr.</p>`) +
		macro("expand", map[string]string{"title": "Mehr Details anzeigen"}, `<p>Dieser Text erscheint nach einem Klick. Screenreader sagen den Zustand an.</p>`) +
		`<h2>Seiten dieses Bereichs</h2>` + macro("children", map[string]string{"category": strconv.FormatInt(root, 10), "show": "all"}, "") +
		`<h2>Zuletzt geändert</h2>` + macro("recent", map[string]string{"count": "5", "scope": "category"}, "") +
		`<h2>Nächste Termine</h2>` + macro("events", map[string]string{"limit": "3", "days": "60"}, "") +
		`<p>Personen erwähnst du mit @: @` + handles["anna"] + ` bekommt eine Benachrichtigung.</p>`
	overview := d.page(start, "Tour of the features", "Rundgang durch die Funktionen", overviewEN, overviewDE, map[string]any{"short_url": "demo" + suffix})
	d.optional("properties", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/x/core.properties/elements/%d", overview), map[string]any{
			"values": map[string]any{"responsible": "Anna Berger", "state": "Aktiv", "review": time.Now().AddDate(0, 6, 0).Format("2006-01-02")},
			"free": map[string]any{
				"responsible": map[string]string{"type": "text", "label": "Verantwortlich"},
				"state":       map[string]string{"type": "status", "label": "Status"},
				"review":      map[string]string{"type": "date", "label": "Prüfdatum"},
			}}, nil)
	})
	// comments with a mention, reactions and a favorite of the administrator
	d.optional("comments", func() {
		d.create("comment", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", overview), map[string]any{"body": "Great overview! @" + handles["ben"] + " could you add the calendar?"})
	})
	d.optional("reactions", func() {
		d.call(http.MethodPost, fmt.Sprintf("/api/v1/x/core.reactions/element/%d", overview), map[string]any{"emoji": "👍"}, nil)
	})
	d.optional("favorites", func() {
		d.call(http.MethodPost, "/api/v1/x/core.favorites/", map[string]any{"type": "element", "id": overview}, nil)
	})

	d.element(start, "core.html", d.tr("Markdown page", "Markdown-Seite"), d.same(map[string]string{"format": "markdown", "source": "## Markdown\n\nPages can also be written in **Markdown**.\n\n| Feature | Status |\n|---|---|\n| Tables | ✓ |\n| Code | ✓ |\n\n```go lines\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, wiki!\")\n}\n```\n\n- [x] Task done\n- [ ] Task open\n\n> A quote.\n"}), nil)

	d.page(start, "Website building blocks", "Website-Bausteine",
		macro("hero", map[string]string{"subtitle": "Sections, hero banners, cards and buttons for website-like pages", "button": "Back to the tour", "link": "/demo" + suffix, "background": "accent"}, "<p>Building blocks</p>")+
			macro("section", map[string]string{"background": "light", "width": "normal", "align": "center"}, "<h2>A section</h2><p>With a background and centred text.</p>")+
			macro("cards", nil, "<h3>Fast</h3><p>One single program.</p><hr><h3>Secure</h3><p>Permissions on every level.</p><hr><h3>Accessible</h3><p>WCAG 2.2 AA.</p>")+
			macro("spacer", nil, "")+macro("button", map[string]string{"text": "Open the FAQ", "link": "/wiki", "style": "primary"}, ""),
		macro("hero", map[string]string{"subtitle": "Abschnitte, Hero-Banner, Karten und Buttons für Website-Seiten", "button": "Zurück zum Rundgang", "link": "/demo" + suffix, "background": "accent"}, "<p>Bausteine</p>")+
			macro("section", map[string]string{"background": "light", "width": "normal", "align": "center"}, "<h2>Ein Abschnitt</h2><p>Mit Hintergrund und zentriertem Text.</p>")+
			macro("cards", nil, "<h3>Schnell</h3><p>Ein einziges Programm.</p><hr><h3>Sicher</h3><p>Rechte auf jeder Ebene.</p><hr><h3>Barrierefrei</h3><p>WCAG 2.2 AA.</p>")+
			macro("spacer", nil, "")+macro("button", map[string]string{"text": "Zu den FAQ", "link": "/wiki", "style": "primary"}, ""), nil)

	// ---- 2. news: a blog with posts
	news := d.category(root, "News", "Neuigkeiten", "A blog: the pages of this category are the posts.", "Ein Blog: die Seiten dieser Kategorie sind die Beiträge.", nil)
	posts := [][4]string{
		{"Office files open in the browser", "Office-Dateien im Browser öffnen", "<p>Documents, spreadsheets and presentations open in Collabora, OnlyOffice or Microsoft 365 – with the permissions of the file.</p>", "<p>Texte, Tabellen und Präsentationen öffnen sich in Collabora, OnlyOffice oder Microsoft 365 – mit den Rechten der Datei.</p>"},
		{"New: media library with watermarks", "Neu: Medienbibliothek mit Wasserzeichen", "<p>Files can be view-only and show the viewer's name.</p>" + img(1, "Noon over the valley"), "<p>Dateien können nur zur Ansicht freigegeben werden und zeigen den Namen des Betrachters.</p>" + img(1, "Mittag über dem Tal")},
		{"Several sites in one wiki", "Mehrere Sites in einem Wiki", "<p>Each site has its own address, template and sign-in.</p>", "<p>Jede Site hat eigene Adresse, Vorlage und Anmeldung.</p>"},
	}
	for _, p := range posts {
		d.page(news, p[0], p[1], p[2], p[3], nil)
	}
	d.optional("blog", func() {
		blog := d.element(news, "content.blog", d.tr("Wiki news", "Wiki-Neuigkeiten"), d.same(map[string]any{"category_id": news, "recursive": false,
			"intro": htmlBodyOf("<p>Demo-Blog</p>"), "per_page": 10, "layout": "cards", "show_author": true, "show_date": true, "summary_length": 200}), nil)
		// labels of this blog (instead of the plugin's "New post")
		if blog != 0 {
			texts := map[string]string{"en": "Write news", "de": "Neuigkeit schreiben"}
			values := map[string]any{}
			for _, l := range d.s.app.Config.Get().I18n.Languages {
				if v, ok := texts[l]; ok {
					values[l] = map[string]string{"content.blog.new_post": v}
				}
			}
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/elements/%d/labels", blog), map[string]any{"values": values}, nil)
		}
	})

	// ---- 3. FAQ: sections are subcategories, questions are pages
	faq := d.category(root, "Questions", "Häufige Fragen", "An FAQ: subcategories are sections, pages are questions.", "Eine FAQ: Unterkategorien sind Abschnitte, Seiten sind Fragen.", nil)
	secs := []struct {
		en, de string
		qs     [][4]string
	}{
		{"Content", "Inhalte", [][4]string{
			{"How do I restore an old version?", "Wie stelle ich eine alte Version wieder her?", "<p>Open <em>History</em>, choose the version and click <em>Restore</em>.</p>", "<p>Öffne den <em>Verlauf</em>, wähle die Version und klicke auf <em>Wiederherstellen</em>.</p>"},
			{"Can I translate pages?", "Kann ich Seiten übersetzen?", "<p>Yes, into every active language; outdated translations are marked.</p>", "<p>Ja, in jede aktive Sprache; veraltete Übersetzungen werden markiert.</p>"},
		}},
		{"Permissions", "Rechte", [][4]string{
			{"Who sees a page?", "Wer sieht eine Seite?", "<p>Everybody with the right <em>view</em> on its category (inherited from above).</p>", "<p>Alle mit dem Recht <em>Ansehen</em> auf ihre Kategorie (von oben vererbt).</p>"},
		}},
	}
	for _, sec := range secs {
		sc := d.category(faq, sec.en, sec.de, "", "", nil)
		for _, q := range sec.qs {
			d.page(sc, q[0], q[1], q[2], q[3], nil)
		}
	}
	d.optional("faq", func() {
		d.element(faq, "content.faq", d.tr("Frequently asked questions", "Häufige Fragen"), d.same(map[string]any{"category_id": faq, "recursive": true,
			"intro": htmlBodyOf(""), "search": true, "numbering": true}), nil)
	})

	// ---- 4. events
	evCat := d.category(root, "Events", "Termine", "An event calendar with recurring events and registration.", "Ein Veranstaltungskalender mit Serien und Anmeldung.", nil)
	var calID int64
	d.optional("events", func() {
		cal := d.element(evCat, "content.events", d.tr("Event calendar", "Veranstaltungskalender"), d.same(map[string]any{"intro": htmlBodyOf(""), "view": "month", "week_start": 1,
			"categories": []map[string]string{{"id": "training", "label": "Schulung", "color": "#2e7d32"}, {"id": "meeting", "label": "Besprechung", "color": "#1565c0"}},
			"ical":       true, "list_days": 90}), nil)
		calID = cal
		if d.err == nil {
			day := time.Now().AddDate(0, 0, 7).Truncate(24 * time.Hour).Add(9 * time.Hour)
			d.create("event", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", cal), map[string]any{"title": "Wiki-Schulung für Einsteiger", "description": "Grundlagen: Seiten, Kategorien, Rechte.",
				"location": "Schulungsraum 1", "category": "training", "starts_at": day, "ends_at": day.Add(2 * time.Hour), "tz": "Europe/Vienna",
				"registration": true, "capacity": 12, "waitlist": true})
			d.create("event", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", cal), map[string]any{"title": "Redaktionsrunde", "description": "Jeden Montag.",
				"location": "Online", "category": "meeting", "starts_at": day.AddDate(0, 0, -int(day.Weekday())+1).Add(time.Hour), "ends_at": day.AddDate(0, 0, -int(day.Weekday())+1).Add(90 * time.Minute),
				"tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;BYDAY=MO;COUNT=10"})
		}
	})

	// ---- 4b. several sources together: news, blog and FAQ macros, a calendar with further calendars
	srcCat := d.category(root, "Sources combined", "Quellen kombiniert", "Macros and elements that show several categories, tags and calendars together.",
		"Makros und Elemente, die mehrere Kategorien, Schlagwörter und Kalender gemeinsam zeigen.", nil)
	both := fmt.Sprintf("category:%d; category:%d+sub", news, faq)
	d.page(srcCat, "Everything new", "Alles Neue",
		"<p>The newest pages of <em>News</em> and <em>Questions</em> together (macro <code>news</code>):</p>"+macro("news", map[string]string{"source": both, "layout": "cards", "limit": "6"}, "")+
			"<h2>As blog</h2>"+macro("blog", map[string]string{"source": both, "layout": "compact", "limit": "4"}, ""),
		"<p>Die neuesten Seiten aus <em>Neuigkeiten</em> und <em>Häufige Fragen</em> gemeinsam (Makro <code>news</code>):</p>"+macro("news", map[string]string{"source": both, "layout": "cards", "limit": "6"}, "")+
			"<h2>Als Blog</h2>"+macro("blog", map[string]string{"source": both, "layout": "compact", "limit": "4"}, ""), nil)
	d.page(srcCat, "Help from all sections", "Hilfe aus allen Bereichen",
		"<p>Questions of several categories as one FAQ (macro <code>faq</code>):</p>"+macro("faq", map[string]string{"source": fmt.Sprintf("category:%d+sub; category:%d", faq, news), "open": "no", "numbering": "yes"}, ""),
		"<p>Fragen mehrerer Kategorien als eine FAQ (Makro <code>faq</code>):</p>"+macro("faq", map[string]string{"source": fmt.Sprintf("category:%d+sub; category:%d", faq, news), "open": "no", "numbering": "yes"}, ""), nil)
	// the source builder: exclusions, requirements and pages
	all := fmt.Sprintf("category:%d+sub; category:%d+sub; -type:content.faq; -type:content.blog", news, faq)
	d.page(srcCat, "Page by page", "Seitenweise",
		"<p>All pages of <em>News</em> and <em>Questions</em> except overviews, three per page – the page changes without reloading (macro <code>news</code> with <code>paginate</code>):</p>"+
			macro("news", map[string]string{"source": all, "layout": "list", "limit": "30", "paginate": "yes", "per_page": "3", "title": "All pages"}, "")+
			"<p>Only what is about holidays (search source) and nothing from the questions:</p>"+
			macro("blog", map[string]string{"source": fmt.Sprintf("search:holiday; -category:%d+sub", faq), "layout": "compact", "limit": "10"}, ""),
		"<p>Alle Seiten aus <em>Neuigkeiten</em> und <em>Häufige Fragen</em> ohne Übersichten, drei pro Seite – geblättert wird ohne Neuladen (Makro <code>news</code> mit <code>paginate</code>):</p>"+
			macro("news", map[string]string{"source": all, "layout": "list", "limit": "30", "paginate": "yes", "per_page": "3", "title": "Alle Seiten"}, "")+
			"<p>Nur was mit Urlaub zu tun hat (Quelle Suche) und nichts aus den Fragen:</p>"+
			macro("blog", map[string]string{"source": fmt.Sprintf("search:Urlaub; -category:%d+sub", faq), "layout": "compact", "limit": "10"}, ""), nil)
	// the demo is a space: key DEMO (/s/DEMO), own navigation with shortcuts
	d.optional("space", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/space", root), map[string]any{"key": "DEMO", "own_tree": true, "accent": "#5f2aa3",
			"shortcuts": []map[string]any{
				{"label": d.tr("All news", "Alles Neue"), "url": "/c/" + strconv.FormatInt(srcCat, 10)},
				{"label": d.tr("Search", "Suche"), "url": "/search"},
			}}, nil)
	})
	// a page template of the space (offered for new pages in the demo only)
	var tplID int64
	d.optional("space-template", func() {
		var r idResp
		defer func() { tplID = r.ID }()
		d.call(http.MethodPost, "/api/v1/templates", map[string]any{"name": d.tr("Meeting minutes", "Besprechungsprotokoll")[d.lang()], "type": "core.html", "lang": d.lang(),
			"title": d.tr("Minutes", "Protokoll")[d.lang()], "space_id": root,
			"body": htmlBodyOf(d.tr("<h2>Participants</h2><p>…</p><h2>Decisions</h2><ul><li>…</li></ul><h2>To-dos</h2><ul><li>…</li></ul>",
				"<h2>Teilnehmende</h2><p>…</p><h2>Beschlüsse</h2><ul><li>…</li></ul><h2>Aufgaben</h2><ul><li>…</li></ul>")[d.lang()])}, &r)
	})
	// wiki blocks: a page with excerpts, a page that includes it, an anchor,
	// the page tree, a search box and a button for new minutes
	d.optional("wiki-blocks", func() {
		rules := d.page(start, "House rules", "Hausordnung",
			`<p>The full rules for the office.</p>`+macro("excerpt", nil, `<p><strong>In short:</strong> the last one out switches off the lights and locks the door.</p>`)+`<h2>Details</h2><p>Keys are at the reception.</p>`,
			`<p>Die ganze Ordnung für das Büro.</p>`+macro("excerpt", nil, `<p><strong>Kurz gesagt:</strong> Wer zuletzt geht, schaltet das Licht aus und schließt ab.</p>`)+`<h2>Einzelheiten</h2><p>Schlüssel gibt es am Empfang.</p>`, nil)
		id := strconv.FormatInt(rules, 10)
		in := strconv.FormatInt(start, 10)
		newpage := func(text string) string {
			if tplID == 0 {
				return ""
			}
			return macro("newpage", map[string]string{"template": strconv.FormatInt(tplID, 10), "text": text}, "")
		}
		d.page(start, "Wiki building blocks", "Wiki-Bausteine",
			`<p>Jump to the <a href="#tree">page tree</a>.</p><h2>Included from another page</h2><p>Only the excerpt of the house rules, always as it is now:</p>`+
				macro("include", map[string]string{"page": id, "excerpt": "true"}, "")+
				`<h2>Search in this area</h2>`+macro("searchbox", map[string]string{"category": in, "label": "Search in Getting started"}, "")+
				newpage("New meeting minutes")+
				`<h2>Pages of this area <span data-macro="anchor" data-params='{"name":"tree"}'></span></h2>`+macro("pagetree", map[string]string{"category": in, "depth": "2"}, "")+
				`<h2>Files of the media folder</h2>`+macro("attachments", map[string]string{"source": "category", "category": strconv.FormatInt(mediaCat, 10)}, ""),
			`<p>Zum <a href="#tree">Seitenbaum</a> springen.</p><h2>Aus einer anderen Seite eingebunden</h2><p>Nur der Auszug der Hausordnung, immer im aktuellen Stand:</p>`+
				macro("include", map[string]string{"page": id, "excerpt": "true"}, "")+
				`<h2>In diesem Bereich suchen</h2>`+macro("searchbox", map[string]string{"category": in, "label": "In Erste Schritte suchen"}, "")+
				newpage("Neues Besprechungsprotokoll")+
				`<h2>Seiten dieses Bereichs <span data-macro="anchor" data-params='{"name":"tree"}'></span></h2>`+macro("pagetree", map[string]string{"category": in, "depth": "2"}, "")+
				`<h2>Dateien des Medien-Ordners</h2>`+macro("attachments", map[string]string{"source": "category", "category": strconv.FormatInt(mediaCat, 10)}, ""), nil)
	})
	// blocks B, part 1: progress, counter, timeline, dialog, tooltip
	d.optional("blocks-b1", func() {
		page := func(lang string) string {
			tr := func(en, de string) string {
				if lang == "de" {
					return de
				}
				return en
			}
			return `<p>` + tr("Figures, steps and explanations on a page.", "Zahlen, Schritte und Erklärungen auf einer Seite.") + `</p>` +
				macro("progress", map[string]string{"label": tr("Renovation of the office", "Umbau des Büros"), "value": "65", "color": "success"}, "") +
				macro("counter", map[string]string{"value": "1250", "label": tr("Pages in the wiki", "Seiten im Wiki"), "suffix": "+"}, "") +
				macro("timeline", map[string]string{"label": tr("History", "Geschichte"), "direction": "horizontal"},
					`<h3>2024</h3><p>`+tr("The team starts.", "Das Team startet.")+`</p><hr><h3>2025</h3><p>`+tr("The second office opens.", "Das zweite Büro öffnet.")+`</p><hr><h3>2026</h3><p>`+tr("The wiki replaces the old intranet.", "Das Wiki ersetzt das alte Intranet.")+`</p>`) +
				macro("dialog", map[string]string{"button": tr("Opening hours", "Öffnungszeiten"), "title": tr("When the reception is open", "Wann der Empfang besetzt ist")},
					`<p>`+tr("Monday to Friday, 8:00 to 17:00.", "Montag bis Freitag, 8:00 bis 17:00 Uhr.")+`</p>`) +
				`<p>` + tr("Questions about the ", "Fragen zur ") + `<span data-macro="tooltip" data-params='{"text":"VPN","tip":"` + tr("Virtual private network: the secure way into the company network from outside.", "Virtuelles privates Netz: der sichere Weg ins Firmennetz von außen.") + `"}'></span>` + tr(" go to the IT desk.", " beantwortet die IT.") + `</p>`
		}
		d.page(start, "Figures and steps", "Zahlen und Schritte", page("en"), page("de"), nil)
		// blocks B, part 2: a quote, the tags of the wiki, who wrote a page and its changes
		people := func(lang string) string {
			tr := func(en, de string) string {
				if lang == "de" {
					return de
				}
				return en
			}
			return macro("testimonial", map[string]string{"name": "Mira Example", "role": tr("Team lead, support", "Teamleitung Support")},
				`<p>`+tr("Since we keep our answers in the wiki, new colleagues find their way in a few days.", "Seit unsere Antworten im Wiki stehen, finden sich neue Kolleginnen und Kollegen in wenigen Tagen zurecht.")+`</p>`) +
				`<h2>` + tr("Tags of the wiki", "Schlagwörter des Wikis") + `</h2>` + macro("tagcloud", map[string]string{"max": "30"}, "") +
				`<h2>` + tr("Who wrote this page", "Wer an dieser Seite geschrieben hat") + `</h2>` + macro("contributors", nil, "") +
				`<h2>` + tr("Latest changes", "Letzte Änderungen") + `</h2>` + macro("pagechanges", map[string]string{"max": "5"}, "") +
				macro("personcard", map[string]string{"name": "Mira Example", "role": tr("Support", "Support"), "email": "mira@example.com"}, `<p>`+tr("Answers questions about the wiki.", "Beantwortet Fragen zum Wiki.")+`</p>`) +
				macro("slider", map[string]string{"label": tr("First steps", "Erste Schritte")},
					`<p>`+tr("1. Sign in.", "1. Anmelden.")+`</p><hr><p>`+tr("2. Open your area.", "2. Den eigenen Bereich öffnen.")+`</p><hr><p>`+tr("3. Write your first page.", "3. Die erste Seite schreiben.")+`</p>`)
		}
		d.page(start, "People and tags", "Personen und Schlagwörter", people("en"), people("de"), nil)
	})
	// a category with its own website template (the modern website look)
	d.optional("category-template", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", srcCat), map[string]any{"template": "theme.cms.modern"}, nil)
	})
	d.optional("events-sources", func() {
		if calID == 0 {
			return
		}
		d.element(srcCat, "content.events", d.tr("All dates", "Alle Termine"), d.same(map[string]any{"intro": htmlBodyOf(""), "view": "list", "week_start": 1, "list_days": 90,
			"more_sources": fmt.Sprintf("calendar:%d", calID)}), nil)
	})

	// ---- 5. team
	team := d.category(root, "Team", "Team", "A team page from groups and business cards.", "Eine Teamseite aus Gruppen und Visitenkarten.", nil)
	d.optional("team", func() {
		d.element(team, "content.team", d.tr("Our team", "Unser Team"), d.same(map[string]any{"description": htmlBodyOf("<p>Demo-Konten – eine Anmeldung ist nicht möglich.</p>"),
			"groups": []int64{editorsGroup, readersGroup}, "fields": []string{"photo", "job_title", "department", "phone", "office"}, "layout": "cards", "by_group": true, "sort": "name"}), nil)
	})

	// ---- 5b. projects with properties: definitions, a database and reports
	projCat := d.category(root, "Projects (properties)", "Projekte (Eigenschaften)", "Properties of every type, a property database and reports as table, board, charts, calendar and timeline.",
		"Eigenschaften aller Art, eine Eigenschaften-Datenbank und Berichte als Tabelle, Board, Diagramme, Kalender und Zeitleiste.", nil)
	d.optional("properties", func() {
		def := func(body map[string]any) {
			body["category_id"] = projCat
			d.create("property", "/api/v1/x/core.properties/defs", body)
		}
		ch := func(vals ...[3]string) []map[string]any {
			out := []map[string]any{}
			for _, v := range vals {
				out = append(out, map[string]any{"value": v[0], "labels": map[string]string{"en": v[0], "de": v[1]}, "color": v[2]})
			}
			return out
		}
		def(map[string]any{"key": "ref", "type": "autonumber", "labels": d.tr("Number", "Nummer"), "options": map[string]any{"prefix": "PRJ-", "digits": 3}, "position": 0})
		def(map[string]any{"key": "status", "type": "status", "labels": d.tr("Status", "Status"), "required": true, "default": "Planned", "position": 1,
			"options": map[string]any{"choices": ch([3]string{"Planned", "Geplant", "grey"}, [3]string{"Active", "Aktiv", "blue"}, [3]string{"At risk", "Gefährdet", "orange"}, [3]string{"Done", "Erledigt", "green"})}})
		def(map[string]any{"key": "priority", "type": "select", "labels": d.tr("Priority", "Priorität"), "position": 2,
			"options": map[string]any{"choices": ch([3]string{"High", "Hoch", ""}, [3]string{"Medium", "Mittel", ""}, [3]string{"Low", "Niedrig", ""})}})
		def(map[string]any{"key": "owner", "type": "user", "labels": d.tr("Owner", "Verantwortlich"), "position": 3})
		def(map[string]any{"key": "period", "type": "daterange", "labels": d.tr("Period", "Zeitraum"), "position": 4})
		def(map[string]any{"key": "due", "type": "date", "labels": d.tr("Next milestone", "Nächster Meilenstein"), "position": 5})
		def(map[string]any{"key": "budget", "type": "currency", "labels": d.tr("Budget", "Budget"), "options": map[string]any{"currency": "EUR", "min": 0}, "group": "Finanzen", "position": 6})
		def(map[string]any{"key": "progress", "type": "progress", "labels": d.tr("Progress", "Fortschritt"), "group": "Finanzen", "position": 7})
		def(map[string]any{"key": "spent", "type": "formula", "labels": d.tr("Used so far (budget × progress)", "Bisher verbraucht (Budget × Fortschritt)"), "group": "Finanzen", "position": 8,
			"options": map[string]any{"formula": "round(budget * progress / 100)", "decimals": 0}})
		def(map[string]any{"key": "rating", "type": "rating", "labels": d.tr("Satisfaction", "Zufriedenheit"), "options": map[string]any{"max": 5}, "position": 9})
		def(map[string]any{"key": "site", "type": "location", "labels": d.tr("Location", "Standort"), "position": 10})
		def(map[string]any{"key": "areas", "type": "multiselect", "labels": d.tr("Areas", "Bereiche"), "position": 11,
			"options": map[string]any{"choices": ch([3]string{"IT", "IT", ""}, [3]string{"HR", "Personal", ""}, [3]string{"Finance", "Finanzen", ""}, [3]string{"Facilities", "Gebäude", ""})}})
		day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
		lat := func(f float64) *float64 { return &f }
		projects := []struct {
			en, de, status, prio, owner string
			from, to, due               int
			budget, progress, rating    float64
			city                        string
			la, lo                      float64
			areas                       []string
		}{
			{"New intranet", "Neues Intranet", "Active", "High", "anna", -60, 90, 12, 120000, 45, 4, "Wien", 48.2082, 16.3738, []string{"IT", "HR"}},
			{"Office relocation", "Büro-Umzug", "At risk", "High", "ben", -30, 45, -3, 80000, 70, 2, "Graz", 47.0707, 15.4395, []string{"Facilities"}},
			{"Onboarding programme", "Onboarding-Programm", "Planned", "Medium", "clara", 20, 120, 25, 15000, 0, 0, "Linz", 48.3069, 14.2858, []string{"HR"}},
			{"Annual accounts", "Jahresabschluss", "Done", "Medium", "anna", -120, -10, -10, 25000, 100, 5, "Wien", 48.2082, 16.3738, []string{"Finance"}},
			{"Security audit", "Sicherheitsaudit", "Active", "High", "ben", -10, 40, 5, 40000, 30, 3, "Salzburg", 47.8095, 13.0550, []string{"IT"}},
			{"Energy saving", "Energiesparen", "Planned", "Low", "clara", 40, 200, 60, 60000, 0, 0, "Innsbruck", 47.2692, 11.4041, []string{"Facilities", "Finance"}},
		}
		for _, x := range projects {
			if d.err != nil {
				break
			}
			values := map[string]any{"status": x.status, "priority": x.prio, "period": map[string]string{"from": day(x.from), "to": day(x.to)}, "due": day(x.due),
				"budget": x.budget, "progress": x.progress, "site": map[string]any{"address": x.city, "lat": lat(x.la), "lng": lat(x.lo)}, "areas": x.areas}
			if uid := userIDs[x.owner]; uid != 0 {
				values["owner"] = []int64{uid}
			}
			if x.rating > 0 {
				values["rating"] = x.rating
			}
			d.page(projCat, x.en, x.de, "<p>Project description, goals and contacts.</p>", "<p>Projektbeschreibung, Ziele und Ansprechpersonen.</p>",
				map[string]any{"fields": map[string]any{"core.properties": map[string]any{"values": values}}, "tags": []string{"demo", "project"}})
		}
		src := "category:" + strconv.FormatInt(projCat, 10)
		d.optional("propdb", func() {
			d.element(projCat, "content.propdb", d.tr("Project database", "Projekt-Datenbank"), d.same(map[string]any{
				"intro":  map[string]string{"format": "markdown", "source": "Alle Projekte mit ihren Eigenschaften: suchen, filtern, gruppieren, als Board oder Kalender ansehen und Werte direkt ändern."},
				"source": src, "filter": "tags has project", "columns_text": "ref,status,priority,owner,due,budget,progress", "sort_text": "ref", "view": "table", "group": "status", "date": "period",
				"new_type": "core.html", "new_category": 0, "per_page": 50}), map[string]any{"position": 0})
		})
		rep := func(params map[string]string) string {
			params["source"] = src
			if params["filter"] != "" {
				params["filter"] += "; "
			}
			params["filter"] += "tags has project"
			return macro("properties", params, "")
		}
		de := "<p>Berichte aus den Eigenschaften der Projekte – jeweils mit den Rechten der Lesenden.</p>" +
			"<h2>Tabelle nach Status mit Summen</h2>" + rep(map[string]string{"view": "table", "columns": "ref,priority,owner,budget,spent,progress", "group": "status", "totals": "true", "sort": "-budget", "live_filters": "priority,owner"}) +
			"<h2>Board</h2>" + rep(map[string]string{"view": "kanban", "group": "status", "columns": "priority,owner,due"}) +
			"<h2>Budget nach Status</h2>" + rep(map[string]string{"view": "chart", "chart": "bar", "group": "status", "agg": "sum", "value": "budget", "title": "Budget nach Status"}) +
			"<h2>Projekte nach Bereich und Priorität</h2>" + rep(map[string]string{"view": "chart", "chart": "stacked", "group": "areas", "group2": "priority", "title": "Projekte nach Bereich und Priorität"}) +
			"<h2>Anteil nach Priorität</h2>" + rep(map[string]string{"view": "chart", "chart": "donut", "group": "priority", "title": "Anteil nach Priorität"}) +
			"<h2>Kennzahlen</h2>" + rep(map[string]string{"view": "kpi", "group": "status", "agg": "sum", "value": "budget"}) +
			"<h2>Pivot: Status × Priorität</h2>" + rep(map[string]string{"view": "pivot", "group": "status", "group2": "priority"}) +
			"<h2>Zeitleiste</h2>" + rep(map[string]string{"view": "timeline", "date": "period"}) +
			"<h2>Kalender der Meilensteine</h2>" + rep(map[string]string{"view": "calendar", "date": "due"}) +
			"<h2>Überfällig und bald fällig</h2>" + rep(map[string]string{"view": "overdue", "date": "due", "columns": "owner,status", "filter": "status != Done"}) +
			"<h2>Vollständigkeit</h2>" + rep(map[string]string{"view": "completeness", "columns": "owner,due,budget,rating"}) +
			"<h2>Standorte</h2>" + rep(map[string]string{"view": "map", "columns": "site"})
		en := strings.NewReplacer("Berichte aus den Eigenschaften der Projekte – jeweils mit den Rechten der Lesenden.", "Reports from the properties of the projects – always with the permissions of the reader.",
			"Tabelle nach Status mit Summen", "Table by status with totals", "Budget nach Status", "Budget by status", "Projekte nach Bereich und Priorität", "Projects by area and priority",
			"Anteil nach Priorität", "Share by priority", "Kennzahlen", "Key figures", "Zeitleiste", "Timeline", "Kalender der Meilensteine", "Calendar of milestones",
			"Überfällig und bald fällig", "Overdue and due soon", "Vollständigkeit", "Completeness", "Standorte", "Locations").Replace(de)
		d.page(projCat, "Reports: projects", "Berichte: Projekte", en, de, nil)
	})

	// ---- 6. documents and processes
	docs := d.category(root, "Documents and processes", "Dokumente und Abläufe", "Files as elements, a workflow and a report of page properties.",
		"Dateien als Elemente, ein Ablauf und ein Bericht der Seiteneigenschaften.", nil)
	var file struct {
		Hash string `json:"hash"`
		Name string `json:"name"`
		Mime string `json:"mime"`
		Size int64  `json:"size"`
	}
	d.optional("attachment", func() {
		d.call(http.MethodPost, "/api/v1/files", multipartFile(nil, "richtlinie.pdf", demoPDF([]string{"Richtlinie (Demo)", "", "Ein PDF als Element: Vorschau im Browser."})), &file)
		if d.err == nil {
			d.element(docs, "core.attachment", d.tr("Guideline (PDF)", "Richtlinie (PDF)"), d.same(map[string]any{"file": file, "description": "Demo"}), nil)
		}
	})
	d.page(docs, "Documents of the media library", "Dokumente der Medienbibliothek",
		`<p>Files of the media library with their own permissions:</p><ul><li><a href="/api/v1/media/`+strconv.FormatInt(guide.ID, 10)+`/view">Quick guide (PDF)</a></li><li><a href="/api/v1/media/`+strconv.FormatInt(confidential.ID, 10)+`/view">Confidential minutes – view only with your name as watermark</a></li><li><a href="/media?open=`+strconv.FormatInt(letter.ID, 10)+`">Letter template – opens in the office program if one is set up</a></li></ul>`,
		`<p>Dateien der Medienbibliothek mit eigenen Rechten:</p><ul><li><a href="/api/v1/media/`+strconv.FormatInt(guide.ID, 10)+`/view">Kurzanleitung (PDF)</a></li><li><a href="/api/v1/media/`+strconv.FormatInt(confidential.ID, 10)+`/view">Vertrauliches Protokoll – nur ansehen, mit deinem Namen als Wasserzeichen</a></li><li><a href="/media?open=`+strconv.FormatInt(letter.ID, 10)+`">Briefvorlage – öffnet sich im Office-Programm, wenn eines eingerichtet ist</a></li></ul>`, nil)
	d.optional("workflow", func() {
		d.element(docs, "workflow.process", d.tr("Vacation request", "Urlaubsantrag"), d.same(map[string]any{"description": "Demo", "fields": []any{}, "steps": []map[string]any{
			{"id": "check", "name": "Prüfen", "assignee": map[string]string{"type": "group", "value": "#" + strconv.FormatInt(editorsGroup, 10)}, "actions": []map[string]any{{"id": "ok", "label": "Genehmigen", "next": "done"}}},
			{"id": "done", "name": "Erledigt", "actions": []any{}, "outcome": "success"}}}), nil)
	})
	d.optional("workflow2", func() {
		wf := func(l string) map[string]any {
			tr := func(en, de string) string {
				if l == "de" {
					return de
				}
				return en
			}
			editors := "#" + strconv.FormatInt(editorsGroup, 10)
			return map[string]any{
				"description": tr("Workflow 2: computed values, a deadline of three working days with a warning and hand-over, messages in the wiki, and triggers (a monthly check – switched off – and an incoming webhook that needs a secret first).",
					"Workflow 2: berechnete Werte, eine Frist von drei Arbeitstagen mit Warnung und Übergabe, Nachrichten im Wiki und Auslöser (eine monatliche Prüfung – ausgeschaltet – und ein eingehender Webhook, der erst ein Geheimnis braucht)."),
				"fields": []map[string]any{
					{"id": "item", "label": tr("Device", "Gerät"), "type": "text", "required": true},
					{"id": "qty", "label": tr("Quantity", "Anzahl"), "type": "number", "required": true},
					{"id": "price", "label": tr("Price per piece (€)", "Preis pro Stück (€)"), "type": "number", "required": true}},
				"steps": []map[string]any{
					{"id": "sum", "name": tr("Compute the total", "Summe berechnen"), "type": "set", "next": "big",
						"set": []map[string]any{{"field": "total", "formula": "round(qty * price, 2)"}}},
					{"id": "big", "name": tr("More than 1000 €?", "Mehr als 1000 €?"), "type": "condition", "match": "all", "next": "approve", "next_else": "ok",
						"rules": []map[string]any{{"field": "total", "op": "gt", "value": "1000"}}},
					{"id": "approve", "name": tr("Approval", "Freigabe"), "type": "approval", "assignee": map[string]string{"type": "group", "value": editors}, "required": 1,
						"next": "ok", "next_else": "no", "actions": []any{},
						"deadline": map[string]any{"days": 3, "workdays": true, "warn": []int{24},
							"escalate": map[string]any{"action": "reassign", "assignee": map[string]string{"type": "role", "value": "Admin"},
								"notify": map[string]any{"to": "starter", "subject": tr("Your request {{case.title}} is waiting longer", "Ihr Antrag {{case.title}} wartet länger"), "channel": "inbox"}}},
						"notices": []map[string]any{{"on": "enter", "to": "starter", "channel": "inbox", "subject": tr("{{case.title}}: total {{field.total}} € – waiting for approval", "{{case.title}}: Summe {{field.total}} € – wartet auf Freigabe")}}},
					{"id": "ok", "name": tr("Tell the requester", "Antragsteller informieren"), "type": "notify", "next": "done",
						"notify": map[string]any{"to": "starter", "channel": "both", "subject": tr("Approved: {{case.title}}", "Genehmigt: {{case.title}}"), "body": tr("Total: {{field.total}} €", "Summe: {{field.total}} €")}},
					{"id": "done", "name": tr("Approved", "Genehmigt"), "type": "end", "outcome": "success", "actions": []any{}},
					{"id": "no", "name": tr("Rejected", "Abgelehnt"), "type": "end", "outcome": "failure", "actions": []any{}}},
				"triggers": []map[string]any{
					{"id": "monthly", "type": "schedule", "name": tr("Monthly check", "Monatliche Prüfung"), "cron": "0 8 1 * *", "off": true,
						"title": tr("Monthly check {{date}}", "Monatliche Prüfung {{date}}"), "data": map[string]string{"item": tr("Check of all devices", "Prüfung aller Geräte"), "qty": "1", "price": "0"}},
					{"id": "shop", "type": "webhook", "name": tr("Web shop", "Webshop"), "title": "{{trigger.data.item}}",
						"data": map[string]string{"item": "{{trigger.data.item}}", "qty": "{{trigger.data.qty}}", "price": "{{trigger.data.price}}"}}},
			}
		}
		device := d.element(docs, "workflow.process", d.tr("Device request (Workflow 2)", "Geräteantrag (Workflow 2)"), map[string]any{"en": wf("en"), "de": wf("de")}, nil)
		if d.err != nil || device <= 0 {
			return
		}
		editors := "#" + strconv.FormatInt(editorsGroup, 10)
		both := func(f func(tr func(en, de string) string) map[string]any) map[string]any {
			mk := func(l string) map[string]any {
				return f(func(en, de string) string {
					if l == "de" {
						return de
					}
					return en
				})
			}
			return map[string]any{"en": mk("en"), "de": mk("de")}
		}
		end := func(tr func(en, de string) string) map[string]any {
			return map[string]any{"id": "done", "name": tr("Done", "Erledigt"), "type": "end", "outcome": "success", "actions": []any{}}
		}
		// onboarding: a welcome page, the device request as sub-workflow, a deadline from the start date
		d.element(docs, "workflow.process", d.tr("Onboarding (Workflow 2)", "Einarbeitung (Workflow 2)"), both(func(tr func(en, de string) string) map[string]any {
			return map[string]any{
				"description": tr("Creates a welcome page, starts the device request as a sub-workflow and waits for it, then the team prepares the workplace – due two working days before the first day.",
					"Legt eine Willkommensseite an, startet den Geräteantrag als Unter-Workflow und wartet darauf, danach bereitet das Team den Arbeitsplatz vor – fällig zwei Arbeitstage vor dem ersten Tag."),
				"fields": []map[string]any{{"id": "name", "label": tr("Name", "Name"), "type": "text", "required": true},
					{"id": "start", "label": tr("First day", "Erster Arbeitstag"), "type": "date", "required": true},
					{"id": "device", "label": tr("Laptop model", "Laptop-Modell"), "type": "text"}},
				"steps": []map[string]any{
					{"id": "page", "name": tr("Welcome page", "Willkommensseite"), "type": "create", "next": "laptop",
						"create": map[string]any{"category": docs, "title": tr("Welcome, {{field.name}}!", "Willkommen, {{field.name}}!"), "store_id": "page", "store_url": "page_url",
							"body": tr("# Welcome {{field.name}}\n\nYour first day: **{{field.start}}**.", "# Willkommen {{field.name}}\n\nDein erster Tag: **{{field.start}}**.")}},
					{"id": "laptop", "name": tr("Device request", "Geräteantrag"), "type": "subflow", "next": "prepare",
						"subflow": map[string]any{"workflow": device, "wait": true, "title": tr("Laptop for {{field.name}}", "Laptop für {{field.name}}"),
							"data": map[string]string{"item": "{{field.device}}", "qty": "1", "price": "1200"}, "return": map[string]string{"device_total": "total"}}},
					{"id": "prepare", "name": tr("Prepare the workplace", "Arbeitsplatz vorbereiten"), "assignee": map[string]string{"type": "group", "value": editors},
						"actions": []map[string]any{{"id": "ok", "label": tr("Ready", "Bereit"), "next": "tell"}},
						"deadline": map[string]any{"field": "start", "days": 0, "workdays": true, "warn": []int{48},
							"escalate": map[string]any{"action": "notify", "notify": map[string]any{"to": "starter", "channel": "both",
								"subject": tr("The workplace of {{field.name}} is not ready yet", "Der Arbeitsplatz von {{field.name}} ist noch nicht bereit")}}}},
					{"id": "tell", "name": tr("Tell the requester", "Antragsteller informieren"), "type": "notify", "next": "done",
						"notify": map[string]any{"to": "starter", "channel": "inbox", "subject": tr("All set for {{field.name}}", "Alles bereit für {{field.name}}"),
							"body": tr("Welcome page: {{field.page_url}}", "Willkommensseite: {{field.page_url}}")}},
					end(tr)}}
		}), nil)
		// projects: when the status of a project becomes "Done", a closing report is due
		d.element(docs, "workflow.process", d.tr("Project closed (trigger)", "Projekt abgeschlossen (Auslöser)"), both(func(tr func(en, de string) string) map[string]any {
			return map[string]any{
				"description": tr("Starts by itself when the status of a project (Projects) becomes “Done”: creates the page of the closing report and asks the team for a review within five working days.",
					"Startet von selbst, wenn der Status eines Projekts (Projekte) „Erledigt“ wird: legt die Seite des Abschlussberichts an und bittet das Team um Prüfung innerhalb von fünf Arbeitstagen."),
				"no_manual": true,
				"fields":    []map[string]any{{"id": "project", "label": tr("Project", "Projekt"), "type": "text"}, {"id": "link", "label": tr("Link", "Link"), "type": "text"}},
				"triggers": []map[string]any{{"id": "done", "type": "content", "name": tr("Status becomes Done", "Status wird Erledigt"),
					"sources": fmt.Sprintf("category:%d+sub", projCat), "prop": map[string]string{"field": "status", "op": "equals", "value": "Done"},
					"title": tr("Closing report: {{trigger.object.title}}", "Abschlussbericht: {{trigger.object.title}}"),
					"data":  map[string]string{"project": "{{trigger.object.title}}", "link": "{{trigger.object.url}}"}}},
				"steps": []map[string]any{
					{"id": "report", "name": tr("Report page", "Berichtsseite"), "type": "create", "next": "review",
						"create": map[string]any{"category": projCat, "title": tr("Closing report: {{field.project}}", "Abschlussbericht: {{field.project}}"), "store_url": "report",
							"body": tr("Project: {{field.link}}\n\n## Results\n\n## Lessons learned", "Projekt: {{field.link}}\n\n## Ergebnisse\n\n## Erkenntnisse")}},
					{"id": "review", "name": tr("Review the report", "Bericht prüfen"), "assignee": map[string]string{"type": "group", "value": editors},
						"actions":  []map[string]any{{"id": "ok", "label": tr("Reviewed", "Geprüft"), "next": "done"}},
						"deadline": map[string]any{"days": 5, "workdays": true, "warn": []int{24}, "escalate": map[string]any{"action": "reassign", "assignee": map[string]string{"type": "role", "value": "Admin"}}},
						"notices":  []map[string]any{{"on": "enter", "to": "assignees", "channel": "inbox", "subject": tr("Please review: {{field.report}}", "Bitte prüfen: {{field.report}}")}}},
					end(tr)}}
		}), nil)
		// contracts: one review per department at the same time, a form in each step
		d.element(docs, "workflow.process", d.tr("Contract review (parallel)", "Vertragsprüfung (parallel)"), both(func(tr func(en, de string) string) map[string]any {
			return map[string]any{
				"description": tr("One review branch per department at the same time; each reviewer fills in a note (the price stays hidden from them). The final decision needs a comment. Tasks can be taken over, handed over or sent back to the requester with a question; deputies stand in during absences (My tasks). Task e-mails carry decision links; finished cases are anonymized after two years. Try it first with \"Start test run\".",
					"Ein Prüfzweig je Abteilung gleichzeitig; jede prüfende Person füllt eine Notiz aus (der Preis bleibt ihr verborgen). Die Schlussentscheidung braucht einen Kommentar. Aufgaben lassen sich übernehmen, weitergeben oder mit einer Rückfrage an den Antragsteller schicken; Vertretungen springen bei Abwesenheit ein (Meine Aufgaben). Aufgaben-E-Mails enthalten Entscheidungslinks; beendete Fälle werden nach zwei Jahren anonymisiert. Zuerst mit „Testlauf starten“ ausprobieren."),
				"links":     true,
				"retention": map[string]any{"days": 730, "mode": "anonymize"},
				"fields": []map[string]any{
					{"id": "contract", "label": tr("Contract", "Vertrag"), "type": "text", "required": true},
					{"id": "price", "label": tr("Price (€)", "Preis (€)"), "type": "number"},
					{"id": "departments", "label": tr("Departments (one per line)", "Abteilungen (eine pro Zeile)"), "type": "textarea", "required": true},
					{"id": "review_note", "label": tr("Note of the review", "Notiz der Prüfung"), "type": "textarea", "later": true},
					{"id": "comment", "label": tr("Comment of the decision", "Kommentar zur Entscheidung"), "type": "textarea", "later": true}},
				"steps": []map[string]any{
					{"id": "par", "name": tr("Reviews of the departments", "Prüfung der Abteilungen"), "type": "parallel", "next": "decide",
						"parallel": map[string]any{"branches": []string{"review"}, "for_each": "departments"}},
					{"id": "review", "name": tr("Review of the department", "Prüfung der Abteilung"), "assignee": map[string]string{"type": "group", "value": editors},
						"actions": []map[string]any{{"id": "ok", "label": tr("Reviewed", "Geprüft"), "next": "decide"}},
						"form":    []map[string]string{{"field": "review_note", "mode": "required"}, {"field": "price", "mode": "hidden"}}},
					{"id": "decide", "name": tr("Decision", "Entscheidung"), "assignee": map[string]string{"type": "starter"},
						"actions": []map[string]any{{"id": "sign", "label": tr("Sign", "Unterschreiben"), "next": "done"}, {"id": "drop", "label": tr("Do not sign", "Nicht unterschreiben"), "next": "no", "note": "required"}},
						"form":    []map[string]string{{"field": "comment", "mode": "required"}}},
					end(tr),
					{"id": "no", "name": tr("Not signed", "Nicht unterschrieben"), "type": "end", "outcome": "failure", "actions": []any{}}}}
		}), nil)
		// comments: the editors hear about every comment in this category
		d.element(docs, "workflow.process", d.tr("Feedback on comments (event)", "Rückmeldung zu Kommentaren (Ereignis)"), both(func(tr func(en, de string) string) map[string]any {
			return map[string]any{
				"description": tr("Starts for every comment in “Documents and processes” and tells the editors; also a weekly review (switched off) as a schedule.",
					"Startet bei jedem Kommentar in „Dokumente und Abläufe“ und informiert die Redaktion; dazu ein wöchentlicher Rückblick (ausgeschaltet) als Zeitplan."),
				"no_manual": true,
				"fields":    []map[string]any{{"id": "page", "label": tr("Page", "Seite"), "type": "text"}},
				"triggers": []map[string]any{
					{"id": "comment", "type": "event", "event": "comment.create", "category": docs, "run_as": "actor", "name": tr("New comment", "Neuer Kommentar"),
						"title": tr("Comment on {{trigger.object.title}}", "Kommentar zu {{trigger.object.title}}"), "data": map[string]string{"page": "{{trigger.object.title}}"}},
					{"id": "weekly", "type": "schedule", "cron": "0 8 * * 1", "off": true, "name": tr("Weekly review", "Wöchentlicher Rückblick"),
						"title": tr("Weekly review {{date}}", "Wochenrückblick {{date}}"), "data": map[string]string{"page": tr("all pages", "alle Seiten")}}},
				"steps": []map[string]any{
					{"id": "tell", "name": tr("Tell the editors", "Redaktion informieren"), "type": "notify", "next": "done",
						"notify": map[string]any{"to": "group:" + editors, "channel": "inbox", "subject": tr("{{case.title}} by {{trigger.actor}}", "{{case.title}} von {{trigger.actor}}")}},
					end(tr)}}
		}), nil)
		d.element(docs, "workflow.process", d.tr("New colleagues (accounts)", "Neue Kolleginnen und Kollegen (Konten)"), both(func(tr func(en, de string) string) map[string]any {
			return map[string]any{
				"description": tr("Starts for every new account (administration, SCIM or sign-in provider): the person may read “Documents and processes” for 90 days and the editors are told. Try it with a test run.",
					"Startet bei jedem neuen Konto (Administration, SCIM oder Anmeldeanbieter): Die Person darf „Dokumente und Abläufe“ 90 Tage lang lesen, die Redaktion wird informiert. Mit einem Testlauf ausprobieren."),
				"no_manual": true,
				"fields":    []map[string]any{{"id": "who", "label": tr("User name", "Benutzername"), "type": "text", "required": true}},
				"triggers": []map[string]any{
					{"id": "new", "type": "event", "event": "user.created", "name": tr("New account", "Neues Konto"),
						"title": tr("Welcome {{trigger.data.display_name}}", "Willkommen {{trigger.data.display_name}}"), "data": map[string]string{"who": "{{trigger.data.username}}"}}},
				"steps": accountSteps(tr, docs, mediaCat, calID, editors, end(tr))}
		}), nil)
	})
	d.optional("forms", func() {
		mk := func(intro, name, mail, topic, it, hr, other, which, msg, msgHelp, rate, file, ok, send, thanks string) map[string]any {
			return map[string]any{"intro": intro, "submit_label": send, "success_message": thanks, "layout": "grid",
				"fields": []map[string]any{
					{"id": "name", "type": "text", "label": name, "required": true, "autocomplete": "name", "prefill": "user.name", "width": 6},
					{"id": "mail", "type": "email", "label": mail, "required": true, "autocomplete": "email", "prefill": "user.email", "width": 6},
					{"id": "topic", "type": "radio", "label": topic, "required": true, "options": []map[string]string{{"value": "it", "label": it}, {"value": "hr", "label": hr}, {"value": "other", "label": other}}},
					{"id": "which", "type": "text", "label": which, "required": true, "show_if": map[string]string{"field": "topic", "op": "eq", "value": "other"}},
					{"id": "msg", "type": "textarea", "label": msg, "help": msgHelp, "rows": 4},
					{"id": "rate", "type": "rating", "label": rate},
					{"id": "doc", "type": "file", "label": file, "accept": []string{"pdf", "image"}, "max_files": 3, "max_size_mb": 5, "file_comment": true},
					{"id": "ok", "type": "consent", "label": ok, "required": true},
				}, "targets": map[string]any{"store": true}}
		}
		d.element(docs, "content.forms", d.tr("Contact form", "Kontaktformular"), map[string]any{
			"en": mk("Write to us – the fields adapt to your answers (“Other” asks for details), files can be dropped onto the upload area.", "Your name", "E-mail", "What is it about?", "IT", "HR", "Other", "What exactly?", "Message", "Please do not send passwords.", "How happy are you with the wiki?", "Attachment", "I agree that my details are processed.", "Send request", "Thank you! We will get back to you."),
			"de": mk("Schreiben Sie uns – die Felder passen sich Ihren Antworten an („Anderes“ fragt nach), Dateien lassen sich auf die Upload-Fläche ziehen.", "Ihr Name", "E-Mail", "Worum geht es?", "IT", "Personal", "Anderes", "Was genau?", "Nachricht", "Bitte keine Passwörter senden.", "Wie zufrieden sind Sie mit dem Wiki?", "Anhang", "Ich stimme der Verarbeitung meiner Angaben zu.", "Anfrage senden", "Danke! Wir melden uns."),
		}, nil)
	})
	d.optional("forms-order", func() {
		order := func(l string) map[string]any {
			tr := func(en, de string) string {
				if l == "de" {
					return de
				}
				return en
			}
			return map[string]any{"layout": "grid", "proof_of_work": true, "public_results": true, "submit_label": tr("Order", "Bestellen"),
				"intro": tr("A form in steps: computed total, rows for several people, signature drawn or typed. The answers about delivery are public (see “Results”).",
					"Ein Formular in Schritten: berechnete Summe, Zeilen für mehrere Personen, Unterschrift gezeichnet oder getippt. Die Antworten zur Zustellung sind öffentlich (siehe „Ergebnisse“)."),
				"fields": []map[string]any{
					{"id": "p1", "type": "page", "label": tr("Quantity", "Menge")},
					{"id": "qty", "type": "number", "label": tr("Number of tickets", "Anzahl Karten"), "required": true, "min": 1, "max": 20, "width": 6},
					{"id": "price", "type": "number", "label": tr("Price per ticket (€)", "Preis pro Karte (€)"), "default": "12.5", "width": 6},
					{"id": "total", "type": "calc", "label": tr("Total (€)", "Summe (€)"), "formula": "round(qty * price, 2)", "decimals": 2},
					{"id": "p2", "type": "page", "label": tr("Guests", "Gäste")},
					{"id": "guests", "type": "repeat", "label": tr("Guest", "Gast"), "max_rows": 20, "fields": []map[string]any{
						{"id": "name", "type": "text", "label": "Name", "required": true},
						{"id": "diet", "type": "select", "label": tr("Food", "Essen"), "options": []map[string]string{{"value": "veg", "label": tr("Vegetarian", "Vegetarisch")}, {"value": "all", "label": tr("Everything", "Alles")}}}}},
					{"id": "p3", "type": "page", "label": tr("Delivery", "Zustellung")},
					{"id": "way", "type": "radio", "label": tr("Delivery", "Zustellung"), "required": true, "options": []map[string]string{{"value": "post", "label": tr("By post", "Per Post")}, {"value": "pick", "label": tr("Pick up", "Abholung")}}},
					{"id": "sig", "type": "signature", "label": tr("Signature", "Unterschrift"), "required": true},
				}, "targets": map[string]any{"store": true}}
		}
		oid := d.element(docs, "content.forms", d.tr("Ticket order (steps)", "Kartenbestellung (Schritte)"), map[string]any{"en": order("en"), "de": order("de")}, nil)
		if d.err == nil && oid > 0 {
			src := fmt.Sprintf("<p>%s</p>", "Results of the ticket order / Ergebnisse der Kartenbestellung") +
				macro("form-results", map[string]string{"form": strconv.FormatInt(oid, 10), "fields": "way"}, "")
			d.page(docs, "Results", "Ergebnisse", src, src, nil)
		}
	})
	d.optional("notes", func() {
		bodies := map[string]any{}
		for l, src := range d.tr("## Agenda\n\n- [x] Welcome\n- [ ] Demo of the wiki\n- [ ] Next steps\n\nWrite here together: everybody who may edit sees the others live.\n",
			"## Tagesordnung\n\n- [x] Begrüßung\n- [ ] Vorführung des Wikis\n- [ ] Nächste Schritte\n\nHier gemeinsam schreiben: Alle mit Bearbeitungsrecht sehen die anderen live.\n") {
			bodies[l] = map[string]string{"format": "markdown", "source": src}
		}
		d.element(docs, "content.notes", d.tr("Meeting notes (shared)", "Besprechungsnotizen (gemeinsam)"), bodies, nil)
	})
	d.page(docs, "Report: page properties", "Bericht: Seiteneigenschaften",
		"<p>All pages of the demo with their properties:</p>"+macro("properties", map[string]string{"source": "category:" + strconv.FormatInt(root, 10) + "+sub", "filter": "responsible notempty", "columns": "responsible,state,review", "limit": "50"}, ""),
		"<p>Alle Seiten der Demo mit ihren Eigenschaften:</p>"+macro("properties", map[string]string{"source": "category:" + strconv.FormatInt(root, 10) + "+sub", "filter": "responsible notempty", "columns": "responsible,state,review", "limit": "50"}, ""), nil)

	// a page with rights of its own: only administrators and managers of the
	// category see it (own rights only), classified strictly confidential
	d.optional("element_rights", func() {
		id := d.page(docs, "Board decisions (own rights only)", "Vorstandsbeschlüsse (nur eigene Rechte)",
			"<p>This page has rights of its own: only the people named in its <strong>Rights</strong> see it – plus administrators and managers of the category. It appears nowhere else: not in lists, the search or feeds.</p>",
			"<p>Diese Seite hat eigene Rechte: Nur die in ihren <strong>Rechten</strong> genannten Personen sehen sie – dazu Administratoren und Verwalter der Kategorie. Sonst erscheint sie nirgends: nicht in Listen, der Suche oder Feeds.</p>", nil)
		if id != 0 {
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/elements/%d/acl", id), map[string]any{"own_rights_only": true, "classification": 3}, nil)
		}
	})

	// classification: the documents are confidential (marked on every page, not in feeds)
	d.optional("classification", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", docs), map[string]any{"classification": 2}, nil)
	})

	// ---- overview of the demo area (first element = default of the category)
	d.optional("overview", func() {
		d.element(root, "content.catlist", d.tr("Overview of the demo", "Übersicht der Demo"), d.same(map[string]any{"intro": htmlBodyOf(""), "category_id": root, "depth": 2,
			"layout": "cards", "descriptions": true, "elements": true, "max_elements": 5}), map[string]any{"position": 0})
	})

	// ---- display of categories: a handbook (navigation in the side column,
	// the first page in the content, a contact page featured beside it) and
	// pages as cards
	d.optional("category-display", func() {
		hb := d.category(root, "Handbook", "Handbuch",
			"The pages as navigation in the side column – like a manual.",
			"Die Seiten als Navigation in der Seitenspalte – wie ein Handbuch.", nil)
		first := d.page(hb, "Getting started", "Erste Schritte",
			"<p>Start here. The navigation on the side leads through all chapters of the handbook.</p>",
			"<p>Beginnen Sie hier. Die Navigation an der Seite führt durch alle Kapitel des Handbuchs.</p>", map[string]any{"position": 0})
		d.page(hb, "Daily work", "Tägliche Arbeit", "<p>What you do every day.</p>", "<p>Was Sie jeden Tag tun.</p>", map[string]any{"position": 1})
		d.page(hb, "Questions and help", "Fragen und Hilfe", "<p>Where to find help.</p>", "<p>Wo Sie Hilfe finden.</p>", map[string]any{"position": 2})
		contact := d.page(hb, "Contact", "Kontakt", "<p>The editorial team: demo@example.com</p>", "<p>Die Redaktion: demo@example.com</p>", map[string]any{"position": 3})
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", hb), map[string]any{"default_element_id": first}, nil)
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/settings", hb), map[string]any{"display": map[string]any{"view": "handbook"}}, nil)
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category_page?target=%d", hb), map[string]any{"placements": []map[string]any{
			{"block": fmt.Sprintf("core.category.element/%d", contact), "position": "category-aside", "sort": 10}}}, nil)
		where = append(where, whereNote{
			en: whereText("en", "Handbook", "the pages of the category as navigation (display <em>Handbook</em>) and the page <em>Contact</em> beside the first page",
				"Category: side column (<code>category-aside</code>), only on the page of the category <em>Handbook</em>",
				"Edit mode → <em>Handbook</em> → Category settings → <em>Display and content</em> and <em>Placement</em>"),
			de: whereText("de", "Handbuch", "die Seiten der Kategorie als Navigation (Darstellung <em>Handbuch</em>) und die Seite <em>Kontakt</em> neben der ersten Seite",
				"Kategorie: Seitenspalte (<code>category-aside</code>), nur auf der Seite der Kategorie <em>Handbuch</em>",
				"Bearbeitungsmodus → <em>Handbuch</em> → Kategorie-Einstellungen → <em>Darstellung und Inhalte</em> und <em>Platzierung</em>"),
		})
		cards := d.category(root, "Pages as cards", "Seiten als Karten",
			"The overview of this category shows its pages as cards with the beginning of the text.",
			"Die Übersicht dieser Kategorie zeigt ihre Seiten als Karten mit dem Anfang des Textes.", nil)
		for i, x := range [][4]string{
			{"Onboarding", "Einarbeitung", "<p>The first weeks in the team: people, tools and places.</p>", "<p>Die ersten Wochen im Team: Menschen, Werkzeuge und Orte.</p>"},
			{"Travel", "Dienstreisen", "<p>How to plan, book and settle a business trip.</p>", "<p>Wie Sie eine Dienstreise planen, buchen und abrechnen.</p>"},
			{"Workplace", "Arbeitsplatz", "<p>Desk, laptop, phone: what you get and how to ask for it.</p>", "<p>Schreibtisch, Laptop, Telefon: was Sie bekommen und wie Sie es anfordern.</p>"},
		} {
			d.page(cards, x[0], x[1], x[2], x[3], map[string]any{"position": i})
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/settings", cards), map[string]any{"display": map[string]any{"view": "cards", "excerpt": true}}, nil)
		// short pages all on one page, and pages with properties as a table
		one := d.category(root, "All on one page", "Alles auf einer Seite",
			"Short pages one below the other, no click needed.",
			"Kurze Seiten untereinander, ohne Klick.", nil)
		for i, x := range [][4]string{
			{"Opening hours", "Öffnungszeiten", "<p>Monday to Friday, 8:00 to 17:00.</p>", "<p>Montag bis Freitag, 8:00 bis 17:00 Uhr.</p>"},
			{"Address", "Adresse", "<p>Example Street 1, 1010 Vienna</p>", "<p>Beispielstraße 1, 1010 Wien</p>"},
			{"Parking", "Parken", "<p>Parking in the yard, entrance from the side street.</p>", "<p>Parkplätze im Hof, Einfahrt über die Seitengasse.</p>"},
		} {
			d.page(one, x[0], x[1], x[2], x[3], map[string]any{"position": i})
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/settings", one), map[string]any{"display": map[string]any{"view": "all"}}, nil)
		rooms := d.category(root, "Rooms as a table", "Räume als Tabelle",
			"The pages with their properties as a sortable table.",
			"Die Seiten mit ihren Eigenschaften als sortierbare Tabelle.", nil)
		for i, x := range [][5]string{
			{"Meeting room Danube", "Besprechungsraum Donau", "12", "<p>With projector and video conferencing.</p>", "<p>Mit Beamer und Videokonferenz.</p>"},
			{"Meeting room Inn", "Besprechungsraum Inn", "6", "<p>Small room for quick meetings.</p>", "<p>Kleiner Raum für kurze Besprechungen.</p>"},
			{"Hall", "Saal", "80", "<p>For events and training.</p>", "<p>Für Veranstaltungen und Schulungen.</p>"},
		} {
			id := d.page(rooms, x[0], x[1], x[3], x[4], map[string]any{"position": i})
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/x/core.properties/elements/%d", id), map[string]any{
				"values": map[string]any{"seats": x[2]}, "free": map[string]any{"seats": map[string]any{"type": "integer", "label": d.tr("Seats", "Plätze")[d.lang()]}}}, nil)
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/settings", rooms), map[string]any{"display": map[string]any{"view": "table", "columns": []string{"seats"}}}, nil)
	})

	// ---- outside the tree: a menu, a text block and a carousel
	d.optional("menu", func() {
		// shown as mega menu (wide panels with groups and descriptions) while
		// the plugin core.megamenu is active, otherwise as dropdowns
		entry := func(en, de string, key string, id int64, descEN, descDE string) map[string]any {
			return map[string]any{"label": d.tr(en, de), key: id, "description": d.tr(descEN, descDE)}
		}
		d.create("menu", "/api/v1/x/core.menu/menus", map[string]any{"name": "Demo", "location": "header-nav", "style": "mega", "items": []map[string]any{
			{"label": d.tr("Demo tour", "Demo-Rundgang"), "element_id": overview, "children": []map[string]any{
				{"label": d.tr("Read", "Lesen"), "type": "heading", "children": []map[string]any{
					entry("News", "Neuigkeiten", "category_id", news, "Blog with posts and a feed", "Blog mit Beiträgen und Feed"),
					entry("FAQ", "FAQ", "category_id", faq, "Questions in sections, instant search", "Fragen in Abschnitten, Sofortsuche"),
				}},
				{"label": d.tr("Take part", "Mitmachen"), "type": "heading", "children": []map[string]any{
					entry("Events", "Termine", "category_id", evCat, "Calendar with registration", "Kalender mit Anmeldung"),
					entry("Team", "Team", "category_id", team, "People and business cards", "Personen und Visitenkarten"),
				}},
			}},
			{"label": d.tr("News", "Neuigkeiten"), "category_id": news},
			{"label": d.tr("FAQ", "FAQ"), "category_id": faq},
			{"label": d.tr("Events", "Termine"), "category_id": evCat},
			{"label": d.tr("Team", "Team"), "category_id": team},
		}})
		where = append(where, whereNote{
			en: whereText("en", "Menu “Demo”", "the entries <em>Demo tour</em>, <em>News</em>, <em>FAQ</em>, <em>Events</em> and <em>Team</em>; <em>Demo tour</em> opens a mega menu with groups and descriptions",
				"Navigation in the header (<code>header-nav</code>), on every page, for everybody who may see the pages", "Administration → <em>Menus</em>"),
			de: whereText("de", "Menü „Demo“", "die Einträge <em>Demo-Rundgang</em>, <em>Neuigkeiten</em>, <em>FAQ</em>, <em>Termine</em> und <em>Team</em>; <em>Demo-Rundgang</em> öffnet ein Megamenü mit Gruppen und Beschreibungen",
				"Navigation im Kopfbereich (<code>header-nav</code>), auf jeder Seite, für alle, die die Seiten sehen dürfen", "Administration → <em>Menüs</em>"),
		})
	})
	bodies := map[string]any{}
	for l, t := range d.tr("<p><strong>Demo contact</strong><br>Anna Berger · +43 1 234 56 70</p>", "<p><strong>Demo-Kontakt</strong><br>Anna Berger · +43 1 234 56 70</p>") {
		bodies[l] = htmlBodyOf(t)
	}
	d.optional("block", func() {
		d.create("block", "/api/v1/x/core.blocks/blocks", map[string]any{"name": "Demo", "location": "sidebar", "position": 90, "audience": "users", "enabled": true,
			"titles": d.tr("Contact", "Kontakt"), "bodies": bodies})
		where = append(where, whereNote{
			en: whereText("en", "Block “Contact”", "a fixed text with the contact of the demo",
				"Side column (<code>sidebar</code>), below the tree of categories, on every page, only for signed-in people", "Administration → <em>Blocks</em>"),
			de: whereText("de", "Block „Kontakt“", "ein fester Text mit dem Kontakt der Demo",
				"Seitenleiste (<code>sidebar</code>), unter dem Kategoriebaum, auf jeder Seite, nur für Angemeldete", "Administration → <em>Blöcke</em>"),
		})
	})
	d.optional("carousel", func() {
		// no fixed position: placed for one category only, with a page that
		// says what it is and where it is
		car := d.create("carousel", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Demo", "position": "", "audience": "users", "autoplay": 0, "max_slides": 8, "enabled": true,
			"sources": fmt.Sprintf("category:%d", news), "feed_items": 3})
		for i, pc := range pics {
			if d.err != nil {
				break
			}
			// slides have one language: the default language of the wiki
			t := d.tr(pc.en, pc.de)[d.lang()]
			d.call(http.MethodPost, fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", car), map[string]any{"kind": "manual", "title": t,
				"text": d.tr("Demo slide – removed with the demo.", "Demo-Folie – verschwindet mit der Demo.")[d.lang()], "link": "/demo" + suffix,
				"image": "/api/v1/media/" + picIDs[i] + "/view", "image_alt": t}, nil)
		}
		carCat := d.category(root, "Carousel demo", "Karussell-Demo",
			"A carousel placed for this category only.", "Ein Karussell, das nur für diese Kategorie platziert ist.", nil)
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category?target=%d", carCat), map[string]any{"placements": []map[string]any{
			{"block": fmt.Sprintf("core.carousel/%d", car), "position": "content-top", "sort": 5}}}, nil)
		carPage := d.page(carCat, "Carousel demo", "Karussell-Demo", carouselPageEN, carouselPageDE, map[string]any{"position": 0})
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", carCat), map[string]any{"default_element_id": carPage}, nil)
		where = append(where, whereNote{
			en: whereText("en", "Carousel “Demo”", `three demo slides and the newest posts of <em>News</em> – see the page <a href="/e/`+strconv.FormatInt(carPage, 10)+`">Carousel demo</a>`,
				"Above the content of every page (<code>content-top</code>), placed only for the category <em>Carousel demo</em>, only for signed-in people",
				"Administration → <em>Carousels</em>; the placement: Edit mode → <em>Carousel demo</em> → Category settings → <em>Placement</em>"),
			de: whereText("de", "Karussell „Demo“", `drei Demo-Folien und die neuesten Beiträge aus <em>Neuigkeiten</em> – siehe die Seite <a href="/e/`+strconv.FormatInt(carPage, 10)+`">Karussell-Demo</a>`,
				"Über dem Inhalt jeder Seite (<code>content-top</code>), platziert nur für die Kategorie <em>Karussell-Demo</em>, nur für Angemeldete",
				"Administration → <em>Karussells</em>; die Platzierung: Bearbeitungsmodus → <em>Karussell-Demo</em> → Kategorie-Einstellungen → <em>Platzierung</em>"),
		})
	})

	// ---- how the demo is built: what is placed where outside the pages
	d.optional("where", func() {
		var en, de strings.Builder
		for _, w := range where {
			en.WriteString(w.en)
			de.WriteString(w.de)
		}
		d.page(start, "How the demo is built", "So ist die Demo aufgebaut", whereIntroEN+en.String()+whereEndEN, whereIntroDE+de.String()+whereEndDE, nil)
	})
}

// accountSteps are the steps of the demo "New colleagues (accounts)": read
// permission for 90 days, a welcome letter (Word and PDF) in the media
// library, a welcome meeting in the event calendar (if there is one) and a
// message to the editors.
func accountSteps(tr func(en, de string) string, docs, media, cal int64, editors string, end map[string]any) []map[string]any {
	steps := []map[string]any{
		{"id": "read", "name": tr("Read the documents", "Dokumente lesen"), "type": "access", "next": "letter",
			"access": map[string]any{"mode": "grant", "category": docs, "subject": "user", "user": "{{field.who}}", "level": "reader", "days": 90}},
		{"id": "letter", "name": tr("Welcome letter", "Willkommensbrief"), "type": "file", "next": "meet",
			"file": map[string]any{"category": media, "name": tr("Welcome {{field.who}}", "Willkommen {{field.who}}"), "formats": []string{"docx", "pdf"},
				"content": tr("# Welcome, {{trigger.data.display_name}}\n\nYou can read “Documents and processes” for the next 90 days. Your editors will contact you.",
					"# Willkommen, {{trigger.data.display_name}}\n\nDu kannst „Dokumente und Abläufe“ in den nächsten 90 Tagen lesen. Die Redaktion meldet sich bei dir."),
				"store_url": "letter"}},
		{"id": "meet", "name": tr("Welcome meeting", "Willkommensgespräch"), "type": "calendar", "next": "tell",
			"calendar": map[string]any{"calendar": cal, "title": tr("Welcome meeting {{trigger.data.display_name}}", "Willkommensgespräch {{trigger.data.display_name}}"),
				"start": "+3d", "minutes": 30, "store_url": "meeting"}},
		{"id": "tell", "name": tr("Tell the editors", "Redaktion informieren"), "type": "notify", "next": "done",
			"notify": map[string]any{"to": "group:" + editors, "channel": "inbox", "subject": tr("New colleague: {{field.who}}", "Neu im Team: {{field.who}}"),
				"body": tr("Welcome letter: {{field.letter}}\nMeeting: {{field.meeting}}", "Willkommensbrief: {{field.letter}}\nTermin: {{field.meeting}}")}},
		end}
	if cal == 0 { // no event calendar in this demo
		steps[1]["next"] = "tell"
		steps = append(steps[:2], steps[3:]...)
	}
	return steps
}

// whereNote describes one part of the demo outside the pages (a menu, a
// block, a carousel …): what it shows, where it is placed and where it is
// changed, in English and German.
type whereNote struct{ en, de string }

func whereText(lang, name, what, where, change string) string {
	words := map[string][3]string{"en": {"What it shows:", "Where it is:", "Where to change it:"}, "de": {"Was es zeigt:", "Wo es steht:", "Wo man es ändert:"}}[lang]
	return "<h3>" + name + "</h3><ul><li><strong>" + words[0] + "</strong> " + what + "</li><li><strong>" + words[1] + "</strong> " + where +
		"</li><li><strong>" + words[2] + "</strong> " + change + "</li></ul>"
}

// The page "How the demo is built": the positions in words, then one part
// after the other.
const whereIntroEN = `<p>Most of the demo is pages. Some parts are not part of a page: the administration <strong>places</strong> them at a <strong>position</strong> of the template, and they appear there on many pages. This page says for each of them what it shows, where it is and where you change it.</p>` +
	`<h2>The positions of the template</h2><ul>` +
	`<li><strong>Header</strong> (<code>header-nav</code>, <code>header-tools</code>): the bar at the very top with logo, menu, search, language, <em>Edit</em> and your name.</li>` +
	`<li><strong>Side column</strong> (<code>sidebar</code>): on the left, with the tree of the categories; on a phone behind the button ☰.</li>` +
	`<li><strong>Above the content of every page</strong> (<code>content-top</code>): below the header and above the content of the page – that is above the path, the title and the buttons <em>Watch</em>, <em>Favourite</em> and <em>Share</em>. It is not part of the header; it only comes before everything that belongs to the page.</li>` +
	`<li><strong>Below the content</strong> (<code>content-bottom</code>) and <strong>footer</strong> (<code>footer</code>).</li></ul>` +
	`<p>Pages and categories have positions of their own as well (for example <em>Category: side column</em>). A part can be placed for the whole website, for one category with its subcategories, or for the page of one category.</p>` +
	`<h2>The parts of this demo</h2>`
const whereEndEN = `<p>You find all positions with their blocks under <em>Administration → Placement</em>; a category has its own under <em>Category settings → Placement</em>.</p>`
const whereIntroDE = `<p>Der größte Teil der Demo sind Seiten. Manche Teile gehören zu keiner Seite: Die Administration <strong>platziert</strong> sie an einer <strong>Position</strong> des Templates, und sie erscheinen dort auf vielen Seiten. Diese Seite sagt für jeden, was er zeigt, wo er steht und wo man ihn ändert.</p>` +
	`<h2>Die Positionen des Templates</h2><ul>` +
	`<li><strong>Kopfbereich</strong> (<code>header-nav</code>, <code>header-tools</code>): die Leiste ganz oben mit Logo, Menü, Suche, Sprache, <em>Bearbeiten</em> und Ihrem Namen.</li>` +
	`<li><strong>Seitenleiste</strong> (<code>sidebar</code>): links, mit dem Baum der Kategorien; auf dem Handy hinter dem Knopf ☰.</li>` +
	`<li><strong>Über dem Inhalt jeder Seite</strong> (<code>content-top</code>): unter dem Kopfbereich und über dem Inhalt der Seite – also über dem Pfad, dem Titel und den Knöpfen <em>Beobachten</em>, <em>Favorit</em> und <em>Teilen</em>. Sie gehört nicht zum Kopfbereich; sie steht nur vor allem, was zur Seite gehört.</li>` +
	`<li><strong>Unter dem Inhalt</strong> (<code>content-bottom</code>) und <strong>Fußbereich</strong> (<code>footer</code>).</li></ul>` +
	`<p>Auch Seiten und Kategorien haben eigene Positionen (zum Beispiel <em>Kategorie: Seitenspalte</em>). Ein Teil lässt sich für die ganze Website platzieren, für eine Kategorie mit ihren Unterkategorien oder nur für die Seite einer Kategorie.</p>` +
	`<h2>Die Teile dieser Demo</h2>`
const whereEndDE = `<p>Alle Positionen mit ihren Bausteinen finden Sie unter <em>Administration → Platzierung</em>; eine Kategorie hat ihre eigene unter <em>Kategorie-Einstellungen → Platzierung</em>.</p>`

// The page of the carousel demo.
const carouselPageEN = `<p>At the top of this page, above the path and the title, there is a <strong>carousel</strong>: slides with a picture, a title, a short text and a link, one after the other. Arrows and dots change the slide; it does not change by itself here.</p>` +
	`<h2>What it shows</h2><ul><li>Three slides of its own (pictures of the media library of the demo).</li><li>The newest posts of the category <em>News</em>: the carousel takes them from a source and stays up to date by itself.</li></ul>` +
	`<h2>Where it is</h2><ul>` +
	`<li><strong>Position:</strong> <em>Above the content of every page</em> (<code>content-top</code>). This place of the template lies below the header (logo, menu, search) and above everything that belongs to the page: path, title, buttons and text. It is not part of the header.</li>` +
	`<li><strong>Placement:</strong> The carousel has no fixed position. It is placed only for this category, <em>Carousel demo</em>. That is why it appears here and on the pages of this category, and nowhere else.</li>` +
	`<li><strong>Who sees it:</strong> signed-in people.</li></ul>` +
	`<h2>Try it yourself</h2><ul>` +
	`<li><strong>Slides:</strong> Administration → <em>Carousels</em> → <em>Slides</em> of the carousel “Demo”.</li>` +
	`<li><strong>Placement of this category:</strong> switch on the edit mode, then <em>Category settings</em> → <em>Placement</em>.</li>` +
	`<li><strong>On every page:</strong> give the carousel a fixed position under Administration → <em>Carousels</em>.</li>` +
	`<li><strong>A page as a slide:</strong> when publishing a page, tick the carousel under <em>As news</em>.</li></ul>`
const carouselPageDE = `<p>Oben auf dieser Seite, über dem Pfad und dem Titel, steht ein <strong>Karussell</strong>: Folien mit Bild, Titel, kurzem Text und Link, eine nach der anderen. Pfeile und Punkte wechseln die Folie; von selbst wechselt sie hier nicht.</p>` +
	`<h2>Was es zeigt</h2><ul><li>Drei eigene Folien (Bilder aus der Mediathek der Demo).</li><li>Die neuesten Beiträge der Kategorie <em>Neuigkeiten</em>: Das Karussell holt sie aus einer Quelle und bleibt von selbst aktuell.</li></ul>` +
	`<h2>Wo es steht</h2><ul>` +
	`<li><strong>Position:</strong> <em>Über dem Inhalt jeder Seite</em> (<code>content-top</code>). Diese Stelle des Templates liegt unter dem Kopfbereich (Logo, Menü, Suche) und über allem, was zur Seite gehört: Pfad, Titel, Knöpfe und Text. Sie gehört nicht zum Kopfbereich.</li>` +
	`<li><strong>Platzierung:</strong> Das Karussell hat keine feste Position. Es ist nur für diese Kategorie platziert, <em>Karussell-Demo</em>. Darum erscheint es hier und auf den Seiten dieser Kategorie, sonst nirgends.</li>` +
	`<li><strong>Wer es sieht:</strong> angemeldete Personen.</li></ul>` +
	`<h2>Selbst ausprobieren</h2><ul>` +
	`<li><strong>Folien:</strong> Administration → <em>Karussells</em> → <em>Folien</em> des Karussells „Demo“.</li>` +
	`<li><strong>Platzierung dieser Kategorie:</strong> den Bearbeitungsmodus einschalten, dann <em>Kategorie-Einstellungen</em> → <em>Platzierung</em>.</li>` +
	`<li><strong>Auf jeder Seite:</strong> dem Karussell unter Administration → <em>Karussells</em> eine feste Position geben.</li>` +
	`<li><strong>Eine Seite als Folie:</strong> beim Veröffentlichen unter <em>Als Neuigkeit</em> das Karussell ankreuzen.</li></ul>`
