package server

// The content of the demo: a space with two areas – an example company
// (demo_company.go) and a tour of the features (demo_tour.go) – and a start
// page for visitors (demo_start.go). German and English; other active
// languages get the English text.

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

// trf picks the text of the language a body is built for.
type trf = func(en, de string) string

// trFor returns the picker for a language (German for de, else English).
func trFor(lang string) trf {
	return func(en, de string) string {
		if lang == "de" {
			return de
		}
		return en
	}
}

// pageF creates an HTML page whose body is built once per language.
func (d *demoRun) pageF(cat int64, en, de string, build func(tr trf) string, extra map[string]any) int64 {
	bodies := map[string]any{}
	for _, l := range d.langs {
		bodies[l] = htmlBodyOf(build(trFor(l)))
	}
	return d.element(cat, "core.html", d.tr(en, de), bodies, extra)
}

// mdF creates a Markdown page whose body is built once per language.
func (d *demoRun) mdF(cat int64, en, de string, build func(tr trf) string, extra map[string]any) int64 {
	bodies := map[string]any{}
	for _, l := range d.langs {
		bodies[l] = map[string]string{"format": "markdown", "source": build(trFor(l))}
	}
	return d.element(cat, "core.html", d.tr(en, de), bodies, extra)
}

// perLang builds the body of an element once per language.
func (d *demoRun) perLang(build func(tr trf) any) map[string]any {
	out := map[string]any{}
	for _, l := range d.langs {
		out[l] = build(trFor(l))
	}
	return out
}

// setting puts the display settings of a category.
func (d *demoRun) display(cat int64, display map[string]any) {
	d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/settings", cat), map[string]any{"display": display}, nil)
}

// demoRefs: what the parts of the demo create and the others link to (0 =
// not created, e.g. a plugin is switched off).
type demoRefs struct {
	suffix                      string
	root                        int64 // the demo area (a space)
	home                        int64 // its first page: the way into the two areas
	media                       int64 // hidden category with the pictures and documents
	pics                        []string
	guide, confidential, letter int64 // media: PDF, view-only PDF, Word

	// people
	editors, leads int64 // groups: "Verwaltung" (edits the demo), "Teamleitung" (decides requests)
	staff          int64 // group "Technik"
	handles        map[string]string
	userIDs        map[string]int64

	// area 1: the company
	company, intranet                                                     int64 // category, its start page
	news, events, teams, handbook, faq, forms, projects                   int64 // categories
	docs, rooms, travel, onboarding                                       int64
	blog, calendar, teamPage, faqEl, propdb                               int64 // elements
	holidayForm, holidayFlow, deviceFlow, ideas, ideaResults, contactForm int64
	notes, decisions, tasks, houseRules, boardPage, reports, guideline    int64
	minutesTpl                                                            int64 // page template of the space
	carousel                                                              int64
	contactBlock                                                          int64

	// area 2: the tour
	tour     int64 // category
	stations []int64
	station  map[string]int64 // key → page

	teamCalendar int64

	// outside the tree: what is placed where (a station lists them)
	where []whereNote
}

// The fictional company of the demo (no real company or brand).
const (
	companyDE = "Enterprise IT Services"
	companyEN = "Enterprise IT Services"
)

func buildDemo(d *demoRun) {
	r := &demoRefs{handles: map[string]string{}, userIDs: map[string]int64{}, station: map[string]int64{}}
	if site := d.s.app.Site; site != "main" {
		r.suffix = "-" + site // accounts and groups may be shared by all sites
	}
	// ---- the demo area: a space with two areas and a start page for visitors
	r.root = d.category(0, "Demo: Enterprise Wiki", "Demo: Enterprise Wiki",
		"Two ways into the wiki: an example company and a tour of all features – created under Administration → Demo, removed there without a trace.",
		"Zwei Wege ins Wiki: eine Beispiel-Firma und ein Rundgang durch alle Funktionen – angelegt unter Administration → Demo und dort spurlos wieder entfernt.", nil)
	d.call(http.MethodPost, fmt.Sprintf("/api/v1/categories/%d/acl", r.root), map[string]any{"subject_type": "authenticated", "permissions": 1, "inherit": true}, nil)
	demoPeople(d, r)
	demoMedia(d, r)
	if d.err != nil {
		return
	}
	// the categories of both areas first: each links to the other
	r.company = d.category(r.root, companyEN, companyDE,
		"Area 1: the intranet of a fictional IT service company – news, teams, handbook, forms with workflows, events and a knowledge base.",
		"Bereich 1: das Intranet eines erfundenen IT-Dienstleisters – Neuigkeiten, Teams, Handbuch, Formulare mit Abläufen, Termine und Wissensdatenbank.", map[string]any{"position": 1})
	r.tour = d.category(r.root, "Tour of the features", "Rundgang durch die Funktionen",
		"Area 2: one station per feature – what it is, how it is done, what it looks like and what it is good for.",
		"Bereich 2: eine Station je Funktion – was es ist, wie es geht, wie es aussieht und wofür es gut ist.", map[string]any{"position": 2})
	companyCategories(d, r)
	demoOutside(d, r)
	companyBasics(d, r)
	buildTour(d, r)
	buildCompany(d, r)
	demoHome(d, r)
	landingPage(d, r)
	// the demo is a space: key DEMO (/s/DEMO), own navigation with shortcuts
	d.optional("space", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d/space", r.root), map[string]any{"key": "DEMO", "own_tree": true, "accent": "#5f2aa3",
			"shortcuts": []map[string]any{
				{"label": d.tr("Example company", "Beispiel-Firma"), "url": "/c/" + strconv.FormatInt(r.company, 10)},
				{"label": d.tr("Tour", "Rundgang"), "url": "/c/" + strconv.FormatInt(r.tour, 10)},
				{"label": d.tr("Search", "Suche"), "url": "/search"},
			}}, nil)
	})
}

// demoPeople: demo accounts (no sign-in possible) in groups; the
// administrator who creates the demo joins the administration and the team
// leads, so they can decide the requests of the demo themselves.
func demoPeople(d *demoRun, r *demoRefs) {
	r.editors = d.create("group", "/api/v1/admin/groups", map[string]any{"name": "Demo: Verwaltung" + r.suffix, "description": "Demo"})
	r.staff = d.create("group", "/api/v1/admin/groups", map[string]any{"name": "Demo: Technik" + r.suffix, "description": "Demo"})
	r.leads = d.create("group", "/api/v1/admin/groups", map[string]any{"name": "Demo: Teamleitung" + r.suffix, "description": "Demo"})
	people := []struct {
		user, name, job, dept string
		group                 int64
		lead                  bool
	}{
		{"anna", "Anna Berger", "Geschäftsführerin", "Geschäftsführung", r.editors, true},
		{"ben", "Ben Huber", "Systemadministrator", "IT-Betrieb", r.staff, false},
		{"clara", "Clara Novak", "Personalreferentin", "Personal", r.editors, false},
		{"david", "David Steiner", "Leiter Service Desk", "Service Desk", r.staff, true},
	}
	for i, pp := range people {
		if d.err != nil {
			return
		}
		uname := "demo-" + pp.user + r.suffix
		var uid int64
		// no e-mail address: the demo never sends mails
		if err := d.s.app.DB.QueryRow(d.ctx, `INSERT INTO users(username, provider, subject, display_name)
			VALUES ($1, 'demo', $1, $2) RETURNING id`, uname, pp.name).Scan(&uid); err != nil {
			d.err = fmt.Errorf("demo user: %w", err)
			return
		}
		d.track("user", uid)
		groups := []int64{pp.group}
		if pp.lead {
			groups = append(groups, r.leads)
		}
		if _, err := d.s.app.DB.Exec(d.ctx, `INSERT INTO user_groups(user_id, group_id) SELECT $1, unnest($2::bigint[])`, uid, groups); err != nil {
			d.err = err
			return
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d/profile", uid), map[string]string{
			"job_title": pp.job, "department": pp.dept, "phone": "+43 1 234 56 7" + strconv.Itoa(i), "office": "Raum " + strconv.Itoa(101+i),
			"about": "Demo-Konto der " + companyDE + " (keine Anmeldung möglich).",
		}, nil)
		r.handles[pp.user] = uname
		r.userIDs[pp.user] = uid
	}
	// (directly, like the demo accounts; it goes with the groups): the
	// administration checks applications first, the team leads decide
	if p := identityOf(d); p != 0 && d.err == nil {
		if _, err := d.s.app.DB.Exec(d.ctx, `INSERT INTO user_groups(user_id, group_id) SELECT $1, unnest($2::bigint[]) ON CONFLICT DO NOTHING`, p, []int64{r.editors, r.leads}); err != nil {
			d.err = err
		}
	}
	// the administration may edit the demo area
	d.call(http.MethodPost, fmt.Sprintf("/api/v1/categories/%d/acl", r.root), map[string]any{"subject_type": "group", "subject_id": r.editors, "permissions": 1 | 2 | 4 | 8, "inherit": true}, nil)
}

// demoMedia: a hidden category as folder with pictures and documents.
func demoMedia(d *demoRun, r *demoRefs) {
	r.media = d.category(r.root, "Media", "Medien", "Pictures and documents of the demo (hidden category = folder).",
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
	for _, pc := range pics {
		var m idResp
		d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": r.media, "title": d.tr(pc.en, pc.de)[d.lang()],
			"alt": d.tr(pc.en, pc.de)[d.lang()]}, pc.name, demoPicture(pc.sky, pc.hill, pc.sun)), &m)
		d.track("medium", m.ID)
		r.pics = append(r.pics, strconv.FormatInt(m.ID, 10))
	}
	var guide, confidential, letter idResp
	d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": r.media, "title": d.tr("Quick guide", "Kurzanleitung")[d.lang()]},
		"kurzanleitung.pdf", demoPDF([]string{"Enterprise IT Services - Kurzanleitung Intranet", "", "1. Kategorien ordnen Inhalte.", "2. Seiten haben Versionen.", "3. Rechte vererben sich."})), &guide)
	d.track("medium", guide.ID)
	d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": r.media, "title": d.tr("Confidential minutes (view only, watermark)", "Vertrauliches Protokoll (nur ansehen, Wasserzeichen)")[d.lang()],
		"download": false, "watermark": map[string]any{"mode": "viewer_view", "user": true, "date": true, "comment": "Demo"}},
		"protokoll.pdf", demoPDF([]string{"Protokoll der Geschaeftsleitung", "", "Nur zur Ansicht - mit dem Namen des Betrachters."})), &confidential)
	d.track("medium", confidential.ID)
	d.call(http.MethodPost, "/api/v1/media", multipartFile(map[string]any{"category_id": r.media, "title": d.tr("Letter template (office)", "Briefvorlage (Office)")[d.lang()]},
		"briefvorlage.docx", demoDocx([]string{"Enterprise IT Services - Briefvorlage", "Sehr geehrte Damen und Herren,", "dieses Dokument lässt sich im Office-Programm des Wikis bearbeiten."})), &letter)
	d.track("medium", letter.ID)
	r.guide, r.confidential, r.letter = guide.ID, confidential.ID, letter.ID
}

// img is a picture of the media library of the demo with a caption.
func (r *demoRefs) img(i int, alt string) string {
	return `<figure><img src="/api/v1/media/` + r.pics[i] + `/view" alt="` + alt + `" width="960" height="540"><figcaption>` + alt + `</figcaption></figure>`
}

func eURL(id int64) string { return "/e/" + strconv.FormatInt(id, 10) }
func cURL(id int64) string { return "/c/" + strconv.FormatInt(id, 10) }
func itoa(id int64) string { return strconv.FormatInt(id, 10) }

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
					"# Willkommen, {{trigger.data.display_name}}\n\nSie können „Dokumente und Abläufe“ in den nächsten 90 Tagen lesen. Die Redaktion meldet sich bei Ihnen."),
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
