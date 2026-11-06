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
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/x/core.properties/elements/%d", overview), map[string]any{"items": []map[string]string{
			{"key": "Verantwortlich", "value": "Anna Berger", "type": "text"},
			{"key": "Status", "value": "Aktiv", "type": "status"},
			{"key": "Prüfdatum", "value": time.Now().AddDate(0, 6, 0).Format("2006-01-02"), "type": "date"},
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

	d.element(start, "core.html", d.tr("Markdown page", "Markdown-Seite"), d.same(map[string]string{"format": "markdown", "source": "## Markdown\n\nPages can also be written in **Markdown**.\n\n| Feature | Status |\n|---|---|\n| Tables | ✓ |\n| Code | ✓ |\n\n```go\nfmt.Println(\"Hello, wiki!\")\n```\n\n- [x] Task done\n- [ ] Task open\n\n> A quote.\n"}), nil)

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
		d.element(news, "content.blog", d.tr("Wiki news", "Wiki-Neuigkeiten"), d.same(map[string]any{"category_id": news, "recursive": false,
			"intro": htmlBodyOf("<p>Demo-Blog</p>"), "per_page": 10, "layout": "cards", "show_author": true, "show_date": true, "summary_length": 200}), nil)
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
	d.optional("events", func() {
		cal := d.element(evCat, "content.events", d.tr("Event calendar", "Veranstaltungskalender"), d.same(map[string]any{"intro": htmlBodyOf(""), "view": "month", "week_start": 1,
			"categories": []map[string]string{{"id": "training", "label": "Schulung", "color": "#2e7d32"}, {"id": "meeting", "label": "Besprechung", "color": "#1565c0"}},
			"ical":       true, "list_days": 90}), nil)
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

	// ---- 5. team
	team := d.category(root, "Team", "Team", "A team page from groups and business cards.", "Eine Teamseite aus Gruppen und Visitenkarten.", nil)
	d.optional("team", func() {
		d.element(team, "content.team", d.tr("Our team", "Unser Team"), d.same(map[string]any{"description": htmlBodyOf("<p>Demo-Konten – eine Anmeldung ist nicht möglich.</p>"),
			"groups": []int64{editorsGroup, readersGroup}, "fields": []string{"photo", "job_title", "department", "phone", "office"}, "layout": "cards", "by_group": true, "sort": "name"}), nil)
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
			{"id": "check", "name": "Prüfen", "assignee": map[string]string{"type": "group", "value": strconv.FormatInt(editorsGroup, 10)}, "actions": []map[string]any{{"id": "ok", "label": "Genehmigen", "next": "done"}}},
			{"id": "done", "name": "Erledigt", "actions": []any{}, "outcome": "success"}}}), nil)
	})
	d.page(docs, "Report: page properties", "Bericht: Seiteneigenschaften",
		"<p>All pages of the demo with their properties:</p>"+macro("properties", map[string]string{"category": strconv.FormatInt(root, 10), "recursive": "true", "limit": "50"}, ""),
		"<p>Alle Seiten der Demo mit ihren Eigenschaften:</p>"+macro("properties", map[string]string{"category": strconv.FormatInt(root, 10), "recursive": "true", "limit": "50"}, ""), nil)

	// ---- overview of the demo area (first element = default of the category)
	d.optional("overview", func() {
		d.element(root, "content.catlist", d.tr("Overview of the demo", "Übersicht der Demo"), d.same(map[string]any{"intro": htmlBodyOf(""), "category_id": root, "depth": 2,
			"layout": "cards", "descriptions": true, "elements": true, "max_elements": 5}), map[string]any{"position": 0})
	})

	// ---- outside the tree: a menu, a text block and a carousel
	d.optional("menu", func() {
		d.create("menu", "/api/v1/x/core.menu/menus", map[string]any{"name": "Demo", "location": "header-nav", "items": []map[string]any{
			{"label": d.tr("Demo tour", "Demo-Rundgang"), "element_id": overview},
			{"label": d.tr("News", "Neuigkeiten"), "category_id": news},
			{"label": d.tr("FAQ", "FAQ"), "category_id": faq},
			{"label": d.tr("Events", "Termine"), "category_id": evCat},
			{"label": d.tr("Team", "Team"), "category_id": team},
		}})
	})
	bodies := map[string]any{}
	for l, t := range d.tr("<p><strong>Demo contact</strong><br>Anna Berger · +43 1 234 56 70</p>", "<p><strong>Demo-Kontakt</strong><br>Anna Berger · +43 1 234 56 70</p>") {
		bodies[l] = htmlBodyOf(t)
	}
	d.optional("block", func() {
		d.create("block", "/api/v1/x/core.blocks/blocks", map[string]any{"name": "Demo", "location": "sidebar", "position": 90, "audience": "users", "enabled": true,
			"titles": d.tr("Contact", "Kontakt"), "bodies": bodies})
	})
	d.optional("carousel", func() {
		car := d.create("carousel", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Demo", "position": "content-top", "audience": "users", "autoplay": 0, "max_slides": 5, "enabled": true})
		for i, pc := range pics {
			if d.err != nil {
				break
			}
			t := d.tr(pc.en, pc.de)[d.lang()]
			d.call(http.MethodPost, fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", car), map[string]any{"kind": "manual", "title": t,
				"text": d.tr("Demo slide – removed with the demo.", "Demo-Folie – verschwindet mit der Demo.")[d.lang()], "link": "/demo" + suffix,
				"image": "/api/v1/media/" + picIDs[i] + "/view", "image_alt": t}, nil)
		}
	})
}
