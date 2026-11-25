package server

// Area 1 of the demo: the intranet of a fictional company (Enterprise IT
// Services, an IT service company). Every feature is used the way a company would use it, and
// a short hint on the page says how it works, with a link to the station of
// the tour that explains it.

import (
	"fmt"
	"net/http"
	"time"
)

// hint is the short explanation on a page of the company: what happens here
// and where the tour explains it.
func (r *demoRefs) hint(tr trf, en, de, station string) string {
	body := `<p>` + tr(en, de)
	if id := r.station[station]; id != 0 {
		body += ` <a href="` + eURL(id) + `">` + tr("How it is done: the tour explains it", "Wie das geht, zeigt der Rundgang") + `</a>.`
	}
	return macro("panel", map[string]string{"type": "tip", "title": tr("How this works", "So funktioniert das")}, body+`</p>`)
}

// hintText is the hint as a paragraph, for the introductions of elements
// (blog, calendar, FAQ, team), which show text without blocks.
func (r *demoRefs) hintText(tr trf, en, de, station string) string {
	body := `<p><strong>` + tr("How this works:", "So funktioniert das:") + `</strong> ` + tr(en, de)
	if id := r.station[station]; id != 0 {
		body += ` <a href="` + eURL(id) + `">` + tr("How it is done: the tour explains it", "Wie das geht, zeigt der Rundgang") + `</a>.`
	}
	return body + `</p>`
}

// companyCategories creates the categories of the company first: the tour
// and the parts outside the tree link to them.
func companyCategories(d *demoRun, r *demoRefs) {
	cat := func(pos int, en, de, descEN, descDE string) int64 {
		return d.category(r.company, en, de, descEN, descDE, map[string]any{"position": pos})
	}
	r.news = cat(0, "News", "Neuigkeiten", "What is new at Enterprise IT Services: the blog of the company.", "Was es Neues gibt bei Enterprise IT Services: der Blog der Firma.")
	r.events = cat(1, "Events", "Termine", "Courses, celebrations and meetings – with registration.", "Kurse, Feiern und Besprechungen – mit Anmeldung.")
	r.teams = cat(2, "Teams", "Teams", "Who works here, what was decided and what is to do.", "Wer hier arbeitet, was entschieden ist und was zu tun ist.")
	r.handbook = cat(3, "Handbook", "Handbuch", "Everything for the daily work, chapter by chapter.", "Alles für die tägliche Arbeit, Kapitel für Kapitel.")
	r.faq = cat(4, "Knowledge base", "Wissensdatenbank", "Answers to frequent questions, with instant search.", "Antworten auf häufige Fragen, mit Sofortsuche.")
	r.forms = cat(5, "Forms and workflows", "Formulare und Abläufe", "Requests that run by themselves: holiday, applications, devices, ideas.", "Anträge, die von selbst laufen: Urlaub, Bewerbungen, Geräte, Ideen.")
	r.projects = cat(6, "Projects", "Projekte", "All projects for customers and in-house, with status, budget and dates – as table, board and charts.", "Alle Projekte für Kunden und im Haus, mit Status, Budget und Terminen – als Tabelle, Board und Diagramme.")
	r.docs = cat(7, "Documents", "Dokumente", "Guidelines, templates and confidential minutes.", "Richtlinien, Vorlagen und vertrauliche Protokolle.")
	r.onboarding = cat(8, "Onboarding", "Einarbeitung", "The first weeks at Enterprise IT Services, as cards.", "Die ersten Wochen bei Enterprise IT Services, als Karten.")
	r.rooms = cat(9, "Rooms", "Räume", "The meeting rooms with their seats, as a table.", "Die Besprechungsräume mit ihren Plätzen, als Tabelle.")
	r.travel = cat(10, "How to find us", "Anfahrt", "Opening hours, address and parking – all on one page.", "Öffnungszeiten, Adresse und Parken – alles auf einer Seite.")
}

// companyBasics: what the stations of the tour show or include, created
// before them (no hints with links to the tour).
func companyBasics(d *demoRun, r *demoRefs) {
	// a page with an excerpt (included by a station of the tour)
	r.houseRules = d.pageF(r.handbook, "House rules", "Hausordnung", func(tr trf) string {
		return `<p>` + tr("The rules for working together in the office and the server room.", "Die Regeln für das Miteinander in Büro und Serverraum.") + `</p>` +
			macro("excerpt", nil, `<p><strong>`+tr("In short:", "Kurz gesagt:")+`</strong> `+tr("the last one out switches off the lights and locks the door.", "Wer zuletzt geht, schaltet das Licht aus und schließt ab.")+`</p>`) +
			`<h2>` + tr("Details", "Einzelheiten") + `</h2><p>` + tr("Keys are at the reception. Guests sign in there too.", "Schlüssel gibt es am Empfang. Gäste melden sich dort ebenfalls an.") + `</p>`
	}, map[string]any{"position": 4})
	// a page template of the space (offered for new pages in the demo only)
	d.optional("space-template", func() {
		var t idResp
		d.call(http.MethodPost, "/api/v1/templates", map[string]any{"name": d.tr("Meeting minutes", "Besprechungsprotokoll")[d.lang()], "type": "core.html", "lang": d.lang(),
			"title": d.tr("Minutes", "Protokoll")[d.lang()], "space_id": r.root,
			"body": htmlBodyOf(d.tr("<h2>Participants</h2><p>…</p><h2>Decisions</h2><ul><li>…</li></ul><h2>To-dos</h2><ul><li>…</li></ul>",
				"<h2>Teilnehmende</h2><p>…</p><h2>Beschlüsse</h2><ul><li>…</li></ul><h2>Aufgaben</h2><ul><li>…</li></ul>")[d.lang()])}, &t)
		r.minutesTpl = t.ID
	})
	// the idea box: a form in steps with public results
	d.optional("forms-order", func() {
		order := func(tr trf) any {
			return map[string]any{"layout": "grid", "proof_of_work": true, "public_results": true, "submit_label": tr("Send idea", "Idee senden"),
				"intro": tr("Your idea for Enterprise IT Services, in three short steps. The answers about the topic are public (see “Results of the idea box”).",
					"Ihre Idee für Enterprise IT Services, in drei kurzen Schritten. Die Antworten zum Thema sind öffentlich (siehe „Ergebnisse der Ideenbox“)."),
				"fields": []map[string]any{
					{"id": "p1", "type": "page", "label": tr("Your idea", "Ihre Idee")},
					{"id": "topic", "type": "radio", "label": tr("What is it about?", "Worum geht es?"), "required": true, "options": []map[string]string{
						{"value": "service", "label": tr("Service desk", "Service Desk")}, {"value": "office", "label": tr("Office", "Büro")}, {"value": "canteen", "label": tr("Canteen", "Kantine")}}},
					{"id": "idea", "type": "textarea", "label": tr("Your idea", "Ihre Idee"), "required": true, "rows": 4},
					{"id": "p2", "type": "page", "label": tr("Benefit", "Nutzen")},
					{"id": "hours", "type": "number", "label": tr("Hours saved per month", "Gesparte Stunden im Monat"), "min": 0, "max": 500, "width": 6},
					{"id": "rate", "type": "number", "label": tr("Cost of an hour (€)", "Kosten einer Stunde (€)"), "default": "45", "width": 6},
					{"id": "saving", "type": "calc", "label": tr("Saving per year (€)", "Ersparnis im Jahr (€)"), "formula": "round(hours * rate * 12, 2)", "decimals": 2},
					{"id": "p3", "type": "page", "label": tr("Who joins", "Wer mitmacht")},
					{"id": "team", "type": "repeat", "label": tr("Colleague", "Kollegin oder Kollege"), "max_rows": 10, "fields": []map[string]any{
						{"id": "name", "type": "text", "label": "Name", "required": true}}},
					{"id": "sig", "type": "signature", "label": tr("Signature", "Unterschrift"), "required": true},
				}, "targets": map[string]any{"store": true}}
		}
		r.ideas = d.element(r.forms, "content.forms", d.tr("Idea box", "Ideenbox"), d.perLang(order), map[string]any{"position": 3})
		if d.err == nil && r.ideas > 0 {
			r.ideaResults = d.pageF(r.forms, "Results of the idea box", "Ergebnisse der Ideenbox", func(tr trf) string {
				return `<p>` + tr("What the ideas are about – counted live from the answers of the idea box.", "Worum es in den Ideen geht – live gezählt aus den Antworten der Ideenbox.") + `</p>` +
					macro("form-results", map[string]string{"form": itoa(r.ideas), "fields": "topic"}, "")
			}, map[string]any{"position": 4})
		}
	})
	companyProjects(d, r)
}

// companyProjects: properties of the projects, the projects and the database.
func companyProjects(d *demoRun, r *demoRefs) {
	d.optional("properties", func() {
		def := func(body map[string]any) {
			body["category_id"] = r.projects
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
			"options": map[string]any{"choices": ch([3]string{"Cloud", "Cloud", ""}, [3]string{"Support", "Support", ""}, [3]string{"HR", "Personal", ""}, [3]string{"Finance", "Finanzen", ""})}})
		day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
		lat := func(f float64) *float64 { return &f }
		projects := []struct {
			en, de, descEN, descDE, status, prio, owner string
			from, to, due                               int
			budget, progress, rating                    float64
			city                                        string
			la, lo                                      float64
			areas                                       []string
		}{
			{"Cloud move for a law firm", "Cloud-Umzug für eine Kanzlei", "A law firm moves its servers into the cloud.", "Eine Kanzlei zieht ihre Server in die Cloud.", "Active", "High", "ben", -60, 90, 12, 120000, 45, 4, "Wien", 48.2082, 16.3738, []string{"Cloud"}},
			{"Office move", "Büroumzug", "We move into the new office floor.", "Wir ziehen in die neue Büroetage.", "At risk", "High", "anna", -30, 45, -3, 80000, 70, 2, "Graz", 47.0707, 15.4395, []string{"Finance"}},
			{"New intranet", "Neues Intranet", "This wiki replaces the old file server.", "Dieses Wiki ersetzt den alten Dateiserver.", "Active", "Medium", "ben", -20, 60, 5, 15000, 30, 3, "Graz", 47.0707, 15.4395, []string{"Support"}},
			{"Onboarding programme", "Einarbeitungsprogramm", "Mentors and a plan for the first weeks.", "Mentoren und ein Plan für die ersten Wochen.", "Planned", "Medium", "clara", 20, 120, 25, 8000, 0, 0, "Linz", 48.3069, 14.2858, []string{"HR"}},
			{"Annual accounts", "Jahresabschluss", "The accounts of last year.", "Der Abschluss des letzten Jahres.", "Done", "Medium", "anna", -120, -10, -10, 25000, 100, 5, "Wien", 48.2082, 16.3738, []string{"Finance"}},
			{"Service desk for a school", "Service Desk für eine Schule", "Support for 40 teachers and 600 pupils.", "Betreuung für 40 Lehrkräfte und 600 Schülerinnen und Schüler.", "Planned", "Low", "david", 40, 200, 60, 60000, 0, 0, "Salzburg", 47.8095, 13.0550, []string{"Support", "Cloud"}},
		}
		for _, x := range projects {
			if d.err != nil {
				break
			}
			values := map[string]any{"status": x.status, "priority": x.prio, "period": map[string]string{"from": day(x.from), "to": day(x.to)}, "due": day(x.due),
				"budget": x.budget, "progress": x.progress, "site": map[string]any{"address": x.city, "lat": lat(x.la), "lng": lat(x.lo)}, "areas": x.areas}
			if uid := r.userIDs[x.owner]; uid != 0 {
				values["owner"] = []int64{uid}
			}
			if x.rating > 0 {
				values["rating"] = x.rating
			}
			d.page(r.projects, x.en, x.de, "<p>"+x.descEN+"</p>", "<p>"+x.descDE+"</p>",
				map[string]any{"fields": map[string]any{"core.properties": map[string]any{"values": values}}, "tags": []string{"demo", "project"}})
		}
		d.optional("propdb", func() {
			r.propdb = d.element(r.projects, "content.propdb", d.tr("Project database", "Projekt-Datenbank"), d.perLang(func(tr trf) any {
				return map[string]any{
					"intro": map[string]string{"format": "markdown", "source": tr("All projects with their properties: search, filter, group, show as board or calendar and change values right here.",
						"Alle Projekte mit ihren Eigenschaften: suchen, filtern, gruppieren, als Board oder Kalender ansehen und Werte direkt ändern.")},
					"source": "category:" + itoa(r.projects), "filter": "tags has project", "columns_text": "ref,status,priority,owner,due,budget,progress", "sort_text": "ref", "view": "table", "group": "status", "date": "period",
					"new_type": "core.html", "new_category": 0, "per_page": 50}
			}), map[string]any{"position": 0})
		})
	})
}

// buildCompany: the content of the company with hints (after the tour, so
// the hints can link to its stations).
func buildCompany(d *demoRun, r *demoRefs) {
	companyNews(d, r)
	companyEvents(d, r)
	companyTeams(d, r)
	companyFlows(d, r)
	companyHandbook(d, r)
	companyFAQ(d, r)
	companyDocs(d, r)
	companyDisplays(d, r)
	companyReports(d, r)
	companyIntranet(d, r) // last: it shows what the others created
}

// demoParty: the year-end party of the company, three weeks ahead, 17:00 in Vienna.
func demoParty() time.Time {
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		loc = time.UTC
	}
	n := time.Now().In(loc).AddDate(0, 0, 21)
	return time.Date(n.Year(), n.Month(), n.Day(), 17, 0, 0, 0, loc)
}

func companyIntranet(d *demoRun, r *demoRefs) {
	party := demoParty().Format("2006-01-02")
	request := cURL(r.forms)
	if r.holidayFlow != 0 {
		request = eURL(r.holidayFlow)
	}
	r.intranet = d.pageF(r.company, "Intranet", "Intranet", func(tr trf) string {
		box := func(icon, en, de, link, textEN, textDE string) string {
			return macro("iconbox", map[string]string{"name": icon, "title": tr(en, de), "link": link, "decorative": "true", "position": "left"}, `<p>`+tr(textEN, textDE)+`</p>`)
		}
		return `<p>` + tr("Good to have you here! The intranet of "+companyEN+" – news, dates and everything for your working day in one place.",
			"Schön, dass Sie da sind! Das Intranet der "+companyDE+" – Neuigkeiten, Termine und alles für Ihren Arbeitstag an einem Ort.") + `</p>` +
			macro("hero", map[string]string{"title": tr("Welcome to Enterprise IT Services", "Willkommen bei Enterprise IT Services"), "subtitle": tr("IT services from Graz since 2008 – 48 people, one team.", "IT-Dienstleistungen aus Graz seit 2008 – 48 Menschen, ein Team."),
				"button": tr("Request holiday", "Urlaub beantragen"), "link": request, "background": "light"}, "") +
			`<h2>` + tr("Quick access", "Schnell erreichbar") + `</h2>` +
			macro("grid", map[string]string{"desktop": "3", "tablet": "2", "mobile": "1"},
				box("beach", "Request holiday", "Urlaub beantragen", request, "Filled in in one minute, decided by your team lead.", "In einer Minute ausgefüllt, entschieden von Ihrer Teamleitung.")+`<hr>`+
					box("book-2", "Handbook", "Handbuch", cURL(r.handbook), "Working hours, IT, support.", "Arbeitszeit, IT, Support.")+`<hr>`+
					box("help-circle", "Knowledge base", "Wissensdatenbank", cURL(r.faq), "Answers to frequent questions.", "Antworten auf häufige Fragen.")+`<hr>`+
					box("calendar-event", "Events", "Termine", cURL(r.events), "Courses and celebrations, with registration.", "Kurse und Feiern, mit Anmeldung.")+`<hr>`+
					box("users", "Teams", "Teams", cURL(r.teams), "Who does what – and how to reach them.", "Wer was macht – und wie Sie ihn erreichen.")+`<hr>`+
					box("bulb", "Idea box", "Ideenbox", eURLor(r.ideas, cURL(r.forms)), "Your idea makes Enterprise IT Services better.", "Ihre Idee macht Enterprise IT Services besser.")) +
			r.hint(tr, "This start page is an ordinary page with building blocks: a hero banner, a grid of icon boxes, the newest posts and the next events. The lists update themselves.",
				"Diese Startseite ist eine normale Seite mit Bausteinen: ein Hero-Banner, ein Raster aus Symbolboxen, die neuesten Beiträge und die nächsten Termine. Die Listen aktualisieren sich selbst.", "website") +
			`<h2>` + tr("News", "Neuigkeiten") + `</h2>` +
			macro("news", map[string]string{"source": "category:" + itoa(r.news), "layout": "cards", "limit": "3"}, "") +
			`<h2>` + tr("Next events", "Nächste Termine") + `</h2>` +
			macro("events", map[string]string{"limit": "3", "days": "60"}, "") +
			macro("countdown", map[string]string{"date": party, "time": "17:00", "label": tr("Until the year-end party", "Bis zur Jahresfeier"), "reached": tr("The party has started!", "Die Feier hat begonnen!")}, "") +
			`<h2>` + tr("New in the team", "Neu im Team") + `</h2>` +
			macro("personcard", map[string]string{"name": "Clara Novak", "role": tr("HR officer", "Personalreferentin"), "email": "clara.novak@example.com", "phone": "+43 1 234 56 72"},
				`<p>`+tr("Clara looks after holidays, onboarding and everything about your contract.", "Clara kümmert sich um Urlaub, Einarbeitung und alles rund um Ihren Vertrag.")+`</p>`) +
			macro("counter", map[string]string{"value": "214", "label": tr("tickets solved this month", "gelöste Tickets in diesem Monat")}, "")
	}, map[string]any{"position": 0})
	d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.company), map[string]any{"default_element_id": r.intranet}, nil)
}

func eURLor(id int64, alt string) string {
	if id == 0 {
		return alt
	}
	return eURL(id)
}

func companyNews(d *demoRun, r *demoRefs) {
	posts := []struct{ en, de, textEN, textDE string }{
		{"Year-end party in the new office", "Jahresfeier im neuen Büro",
			"<p>This year we celebrate on our new office floor: food from the canteen, music and a short look back. Please register in the calendar so the kitchen can plan.</p>" + r.img(2, "Evening light"),
			"<p>Dieses Jahr feiern wir in unserer neuen Büroetage: Essen aus der Kantine, Musik und ein kurzer Rückblick. Bitte melden Sie sich im Kalender an, damit die Küche planen kann.</p>" + r.img(2, "Abendlicht")},
		{"New: managed backup for our customers", "Neu: verwaltete Datensicherung für unsere Kunden",
			"<p>From next month we back up the data of our customers every night – encrypted, in two data centres. The service desk answers questions.</p>" + r.img(1, "Noon over the valley"),
			"<p>Ab nächstem Monat sichern wir die Daten unserer Kunden jede Nacht – verschlüsselt, in zwei Rechenzentren. Fragen beantwortet der Service Desk.</p>" + r.img(1, "Mittag über dem Tal")},
		{"IT: sign in with a passkey", "IT: Anmelden mit Passkey",
			"<p>From now on you can sign in to the intranet with a passkey – your fingerprint or face instead of a password. Set it up under <em>My profile</em>; the knowledge base explains how.</p>",
			"<p>Ab sofort melden Sie sich im Intranet mit einem Passkey an – Fingerabdruck oder Gesicht statt Passwort. Richten Sie ihn unter <em>Mein Profil</em> ein; die Wissensdatenbank erklärt, wie es geht.</p>"},
		{"Welcome to our new colleagues", "Willkommen, neue Kolleginnen und Kollegen",
			"<p>Three new colleagues start in the service desk and in HR this month. Say hello – the team page shows who they are.</p>",
			"<p>Drei neue Kolleginnen und Kollegen starten diesen Monat im Service Desk und im Personal. Sagen Sie Hallo – die Teamseite zeigt, wer sie sind.</p>"},
	}
	first := int64(0)
	for _, p := range posts {
		id := d.page(r.news, p.en, p.de, p.textEN, p.textDE, nil)
		if first == 0 {
			first = id
		}
	}
	d.optional("blog", func() {
		r.blog = d.element(r.news, "content.blog", d.tr("News", "Neuigkeiten"), d.perLang(func(tr trf) any {
			return map[string]any{"category_id": r.news, "recursive": false, "per_page": 10, "layout": "cards", "show_author": true, "show_date": true, "summary_length": 200,
				"intro": htmlBodyOf(`<p>` + tr("What is new at Enterprise IT Services.", "Was es Neues gibt bei Enterprise IT Services.") + `</p>` +
					r.hintText(tr, "The pages of this category are the posts of the blog: a new page is a new post. Watch the category and you hear about every new post.",
						"Die Seiten dieser Kategorie sind die Beiträge des Blogs: Eine neue Seite ist ein neuer Beitrag. Wer die Kategorie beobachtet, erfährt von jedem neuen Beitrag.", "news"))}
		}), map[string]any{"position": 0})
		// labels of this blog (instead of the plugin's "New post")
		if r.blog != 0 {
			texts := map[string]string{"en": "Write news", "de": "Neuigkeit schreiben"}
			values := map[string]any{}
			for _, l := range d.s.app.Config.Get().I18n.Languages {
				if v, ok := texts[l]; ok {
					values[l] = map[string]string{"content.blog.new_post": v}
				}
			}
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/elements/%d/labels", r.blog), map[string]any{"values": values}, nil)
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.news), map[string]any{"default_element_id": r.blog}, nil)
		}
	})
	// comments with a mention, a reaction and a favourite of the administrator
	if first != 0 {
		d.optional("comments", func() {
			d.create("comment", fmt.Sprintf("/api/v1/x/core.comments/elements/%d/comments", first), map[string]any{"body": d.tr("Great! @"+r.handles["ben"]+" can we have music?", "Super! @"+r.handles["ben"]+" gibt es Musik?")[d.lang()]})
		})
		d.optional("reactions", func() {
			d.call(http.MethodPost, fmt.Sprintf("/api/v1/x/core.reactions/element/%d", first), map[string]any{"emoji": "👍"}, nil)
		})
		d.optional("favorites", func() {
			d.call(http.MethodPost, "/api/v1/x/core.favorites/", map[string]any{"type": "element", "id": first}, nil)
		})
	}
}

func companyEvents(d *demoRun, r *demoRefs) {
	d.optional("events", func() {
		cal := d.element(r.events, "content.events", d.tr("Events", "Termine"), d.perLang(func(tr trf) any {
			return map[string]any{"view": "month", "week_start": 1, "ical": true, "list_days": 90,
				"categories": []map[string]string{{"id": "training", "label": tr("Course", "Kurs"), "color": "#2e7d32"}, {"id": "meeting", "label": tr("Meeting", "Besprechung"), "color": "#1565c0"}, {"id": "party", "label": tr("Celebration", "Feier"), "color": "#ad1457"}},
				"intro": htmlBodyOf(r.hintText(tr, "A calendar with registration: courses have a number of places and a waiting list. Every event can be added to your own calendar (iCal).",
					"Ein Kalender mit Anmeldung: Kurse haben eine Zahl an Plätzen und eine Warteliste. Jeder Termin lässt sich in den eigenen Kalender übernehmen (iCal).", "events"))}
		}), map[string]any{"position": 0})
		r.calendar = cal
		if d.err != nil {
			return
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.events), map[string]any{"default_element_id": cal}, nil)
		day := time.Now().AddDate(0, 0, 7).Truncate(24 * time.Hour).Add(9 * time.Hour)
		ev := func(body map[string]any) {
			d.create("event", fmt.Sprintf("/api/v1/x/content.events/calendars/%d/events", cal), body)
		}
		de := d.lang() == "de"
		pick := func(en, dee string) string {
			if de {
				return dee
			}
			return en
		}
		ev(map[string]any{"title": pick("First aid course", "Erste-Hilfe-Kurs"), "description": pick("Refresh the basics in four hours.", "Die Grundlagen in vier Stunden auffrischen."),
			"location": pick("Training room 1", "Schulungsraum 1"), "category": "training", "starts_at": day, "ends_at": day.Add(4 * time.Hour), "tz": "Europe/Vienna",
			"registration": true, "capacity": 12, "waitlist": true})
		party := demoParty()
		ev(map[string]any{"title": pick("Year-end party", "Jahresfeier"), "description": pick("On the new office floor.", "In der neuen Büroetage."),
			"location": pick("New office, 5th floor", "Neues Büro, 5. Stock"), "category": "party", "starts_at": party, "ends_at": party.Add(5 * time.Hour), "tz": "Europe/Vienna",
			"registration": true, "capacity": 80})
		monday := day.AddDate(0, 0, -int(day.Weekday())+1).Add(time.Hour)
		ev(map[string]any{"title": pick("Team round", "Teamrunde"), "description": pick("Every Monday: what is coming this week.", "Jeden Montag: Was diese Woche ansteht."),
			"location": "Online", "category": "meeting", "starts_at": monday, "ends_at": monday.Add(30 * time.Minute), "tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;BYDAY=MO;COUNT=10"})
	})
}

func companyTeams(d *demoRun, r *demoRefs) {
	d.optional("team", func() {
		r.teamPage = d.element(r.teams, "content.team", d.tr("Our team", "Unser Team"), d.perLang(func(tr trf) any {
			return map[string]any{"groups": []int64{r.editors, r.staff}, "fields": []string{"photo", "job_title", "department", "phone", "office"}, "layout": "cards", "by_group": true, "sort": "name",
				"description": htmlBodyOf(`<p>` + tr("Demo accounts – nobody can sign in with them.", "Demo-Konten – mit ihnen kann sich niemand anmelden.") + `</p>` +
					r.hintText(tr, "The team page is built from groups and the profiles of the people. Change a profile, and the page shows it at once.",
						"Die Teamseite entsteht aus Gruppen und den Profilen der Personen. Ändert jemand sein Profil, zeigt die Seite es sofort.", "people"))}
		}), map[string]any{"position": 0})
		if r.teamPage != 0 {
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.teams), map[string]any{"default_element_id": r.teamPage}, nil)
		}
	})
	d.optional("decisions", func() {
		dec := func(title, status, day, who, why string) string {
			p := map[string]string{"title": title, "status": status, "day": day}
			if id := r.userIDs[who]; id != 0 {
				p["person"] = itoa(id)
			} else {
				p["owner"] = who
			}
			return macro("decision", p, "<p>"+why+"</p>")
		}
		r.decisions = d.pageF(r.teams, "Decision log", "Entscheidungsprotokoll", func(tr trf) string {
			return `<p>` + tr("What the management has decided, with status, day and who is responsible.", "Was die Leitung entschieden hat, mit Status, Datum und Verantwortlichen.") + `</p>` +
				dec(tr("Team round on Mondays", "Teamrunde am Montag"), "decided", "2026-10-06", "anna", tr("Most of the team is in the office then.", "Dann ist der größte Teil des Teams im Haus.")) +
				dec(tr("Move into the new office in March", "Umzug ins neue Büro im März"), "open", "2026-12-01", "anna", tr("We are waiting for the offer of the movers.", "Wir warten auf das Angebot der Umzugsfirma.")) +
				dec(tr("Own canteen on the new floor", "Eigene Kantine in der neuen Etage"), "rejected", "2026-09-15", tr("Management", "Geschäftsleitung"), tr("The canteen next door stays; it is enough.", "Die Kantine nebenan bleibt; sie reicht.")) +
				r.hint(tr, "Every decision is a block in the text. The report below collects the decisions of all pages of the teams – only those the reader may see.",
					"Jede Entscheidung ist ein Baustein im Text. Der Bericht unten sammelt die Entscheidungen aller Seiten der Teams – nur die, die die lesende Person sehen darf.", "decisions") +
				`<h2>` + tr("All decisions of the teams", "Alle Entscheidungen der Teams") + `</h2>` + macro("decisions", map[string]string{"category": itoa(r.teams)}, "")
		}, map[string]any{"position": 2})
	})
	d.optional("tasks", func() {
		team := itoa(r.teams)
		r.tasks = d.mdF(r.teams, "Office move", "Büroumzug", func(tr trf) string {
			return tr("We move into the new office floor. Who does what:", "Wir ziehen in die neue Büroetage. Wer was macht:") + "\n\n" +
				"- [ ] " + tr("Check the offer of the movers", "Angebot der Umzugsfirma prüfen") + " @" + r.handles["anna"] + " :date{value=2026-11-30}\n" +
				"- [ ] " + tr("Plan the new places of the servers", "Neue Plätze für die Server planen") + " @" + r.handles["david"] + " @" + r.handles["ben"] + "\n" +
				"- [x] " + tr("Order the network cables", "Netzwerkkabel bestellen") + " @" + r.handles["ben"] + "\n" +
				"- [ ] " + tr("Tell the customers", "Kundschaft informieren") + " :date{value=2026-12-10}\n\n" +
				tr("The move starts on", "Der Umzug beginnt am") + " :date{value=2026-12-12 label=\"" + tr("Office move", "Büroumzug") + "\" time=08:00 end=12:00}; " +
				tr("we meet before on", "vorher treffen wir uns am") + " :date{value=2026-12-08 label=\"" + tr("Kick-off of the move", "Auftakt zum Umzug") + "\" time=14:00}.\n\n" +
				":::panel{type=tip title=\"" + tr("How this works", "So funktioniert das") + "\"}\n" +
				tr("A task item with @name or a date is a task: it appears below, under *My tasks* and on the dashboard. A date with a title is an appointment in the calendars of the team.",
					"Ein Aufgabenpunkt mit @Name oder Datum ist eine Aufgabe: Er erscheint unten, unter *Meine Aufgaben* und im Dashboard. Ein Datum mit Titel ist ein Termin in den Kalendern des Teams.") + "\n:::\n\n" +
				"## " + tr("Open tasks of the teams", "Offene Aufgaben der Teams") + "\n\n::tasks{category=" + team + "}\n\n## " + tr("Dates of the teams", "Termine der Teams") + "\n\n::dates{category=" + team + "}\n"
		}, map[string]any{"position": 3})
		// a calendar with the dates (and tasks) of the pages of the teams
		r.teamCalendar = d.element(r.teams, "content.events", d.tr("Calendar of the teams", "Kalender der Teams"), d.perLang(func(tr trf) any {
			return map[string]any{"view": "list", "week_start": 1, "list_days": 90, "page_tasks": true, "more_sources": "category:" + team + "+sub",
				"intro": htmlBodyOf(`<p>` + tr("Dates and tasks from the pages of the teams, and approved holidays.", "Termine und Aufgaben aus den Seiten der Teams und genehmigter Urlaub.") + `</p>`)}
		}), map[string]any{"position": 4})
	})
	d.optional("notes", func() {
		r.notes = d.element(r.teams, "content.notes", d.tr("Notes of the team round", "Notizen der Teamrunde"), d.perLang(func(tr trf) any {
			return map[string]string{"format": "markdown", "source": "## " + tr("Agenda", "Tagesordnung") + "\n\n- [x] " + tr("Welcome", "Begrüßung") + "\n- [ ] " + tr("State of the move", "Stand des Umzugs") + "\n- [ ] " + tr("Year-end party", "Jahresfeier") + "\n\n" +
				tr("Write here together: everybody who may edit sees the others live.", "Hier gemeinsam schreiben: Alle mit Bearbeitungsrecht sehen die anderen live.") + "\n"}
		}), map[string]any{"position": 5})
	})
	d.optional("people", func() {
		d.pageF(r.teams, "Who does what", "Wer macht was", func(tr trf) string {
			return `<p>` + tr("The contacts for the most frequent questions.", "Die Ansprechpersonen für die häufigsten Fragen.") + `</p>` +
				macro("grid", map[string]string{"desktop": "2", "tablet": "2", "mobile": "1"},
					macro("personcard", map[string]string{"name": "Ben Huber", "role": tr("IT administrator", "IT-Administrator"), "email": "it@example.com", "phone": "+43 1 234 56 71"}, `<p>`+tr("Laptops, passwords, passkeys, VPN.", "Laptops, Passwörter, Passkeys, VPN.")+`</p>`)+`<hr>`+
						macro("personcard", map[string]string{"name": "Clara Novak", "role": tr("HR officer", "Personalreferentin"), "email": "personal@example.com", "phone": "+43 1 234 56 72"}, `<p>`+tr("Holidays, onboarding, contracts.", "Urlaub, Einarbeitung, Verträge.")+`</p>`)) +
				`<h2>` + tr("The administration", "Die Verwaltung") + `</h2>` +
				macro("people", map[string]string{"group": itoa(r.editors), "layout": "list", "details": "short"}, "") +
				r.hint(tr, "The block People of a group shows the members to everybody who may list the group – guests see nobody.",
					"Der Baustein Personen einer Gruppe zeigt die Mitglieder allen, die die Gruppe auflisten dürfen – Gäste sehen niemanden.", "people")
		}, map[string]any{"position": 1})
	})
}

func companyHandbook(d *demoRun, r *demoRefs) {
	first := d.pageF(r.handbook, "Your first week", "Ihre erste Woche", func(tr trf) string {
		return `<p>` + tr("Welcome to Enterprise IT Services! This handbook leads you through everything you need. The chapters are in the navigation on the side.",
			"Willkommen bei Enterprise IT Services! Dieses Handbuch führt Sie durch alles, was Sie brauchen. Die Kapitel stehen in der Navigation an der Seite.") + `</p>` +
			`<h2>` + tr("Your first day", "Ihr erster Tag") + `</h2><ol><li>` + tr("Collect your key at the reception.", "Holen Sie Ihren Schlüssel am Empfang ab.") + `</li><li>` +
			tr("Sign in to the intranet and set up a passkey.", "Melden Sie sich im Intranet an und richten Sie einen Passkey ein.") + `</li><li>` +
			tr("Have lunch with your mentor.", "Gehen Sie mit Ihrer Mentorin oder Ihrem Mentor essen.") + `</li></ol>` +
			r.hint(tr, "This category is shown as a handbook: its pages are the chapters in the side column, the contact stands beside the first page.",
				"Diese Kategorie wird als Handbuch angezeigt: Ihre Seiten sind die Kapitel in der Seitenspalte, der Kontakt steht neben der ersten Seite.", "display")
	}, map[string]any{"position": 0})
	d.pageF(r.handbook, "Working hours and holidays", "Arbeitszeit und Urlaub", func(tr trf) string {
		link := eURLor(r.holidayFlow, cURL(r.forms))
		return `<p>` + tr("We work flexibly between 7:00 and 19:00; the core time is 9:00 to 15:00.", "Wir arbeiten flexibel zwischen 7:00 und 19:00 Uhr; die Kernzeit ist 9:00 bis 15:00 Uhr.") + `</p>` +
			macro("panel", map[string]string{"type": "info", "title": tr("Holidays", "Urlaub")}, `<p>`+tr("You have 25 days of holiday a year. Request them with the ", "Sie haben 25 Urlaubstage im Jahr. Beantragen Sie sie mit dem ")+
				`<a href="`+link+`">`+tr("holiday request", "Urlaubsantrag")+`</a>`+tr(" – your team lead decides, and the approved holiday appears in the calendar of the teams.", " – Ihre Teamleitung entscheidet, und der genehmigte Urlaub erscheint im Kalender der Teams.")+`</p>`)
	}, map[string]any{"position": 1})
	d.pageF(r.handbook, "IT and security", "IT und Sicherheit", func(tr trf) string {
		return `<p>` + tr("Three rules keep our data safe:", "Drei Regeln schützen unsere Daten:") + `</p>` +
			macro("iconlist", map[string]string{"name": "check"}, `<ul><li>`+tr("Sign in with a passkey instead of a password.", "Melden Sie sich mit Passkey statt Passwort an.")+`</li><li>`+
				tr("Lock your screen when you leave your desk.", "Sperren Sie den Bildschirm, wenn Sie den Platz verlassen.")+`</li><li>`+
				tr("Never send passwords by e-mail.", "Senden Sie nie Passwörter per E-Mail.")+`</li></ul>`) +
			`<p>` + tr("From outside you reach the company network through the ", "Von außen erreichen Sie das Firmennetz über das ") +
			`<span data-macro="tooltip" data-params='{"text":"VPN","tip":"` + tr("Virtual private network: the secure way into the company network from outside.", "Virtuelles privates Netz: der sichere Weg ins Firmennetz von außen.") + `"}'></span>.</p>`
	}, map[string]any{"position": 2})
	d.pageF(r.handbook, "Support: from ticket to solution", "Support: vom Ticket zur Lösung", func(tr trf) string {
		return `<p>` + tr("How the service desk handles a request of a customer, in four steps.", "Wie der Service Desk eine Anfrage von Kunden bearbeitet, in vier Schritten.") + `</p>` +
			macro("timeline", map[string]string{"label": tr("A ticket", "Ein Ticket")},
				`<h3>`+tr("Receive", "Annehmen")+`</h3><p>`+tr("Confirm the ticket within one hour.", "Das Ticket innerhalb einer Stunde bestätigen.")+`</p><hr><h3>`+
					tr("Prioritise", "Einstufen")+`</h3><p>`+tr("A fault that stops work comes first.", "Eine Störung, die die Arbeit stoppt, kommt zuerst.")+`</p><hr><h3>`+
					tr("Solve", "Lösen")+`</h3><p>`+tr("Write down every step in the ticket.", "Jeden Schritt im Ticket festhalten.")+`</p><hr><h3>`+tr("Close", "Abschließen")+`</h3><p>`+
					tr("Ask the customer whether everything works.", "Nachfragen, ob alles funktioniert.")+`</p>`)
	}, map[string]any{"position": 3})
	contact := d.pageF(r.handbook, "Contact", "Kontakt", func(tr trf) string {
		return `<p>` + tr("Questions about the handbook? Write to ", "Fragen zum Handbuch? Schreiben Sie an ") + `<a href="mailto:handbuch@example.com">handbuch@example.com</a>.</p>`
	}, map[string]any{"position": 5})
	d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.handbook), map[string]any{"default_element_id": first}, nil)
	d.display(r.handbook, map[string]any{"view": "handbook"})
	d.optional("category-display", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category_page?target=%d", r.handbook), map[string]any{"placements": []map[string]any{
			{"block": fmt.Sprintf("core.category.element/%d", contact), "position": "category-aside", "sort": 10}}}, nil)
	})
}

func companyFAQ(d *demoRun, r *demoRefs) {
	link := eURLor(r.holidayFlow, cURL(r.forms))
	secs := []struct {
		en, de string
		qs     [][4]string
	}{
		{"IT", "IT", [][4]string{
			{"How do I sign in with a passkey?", "Wie melde ich mich mit einem Passkey an?",
				"<p>Open <em>My profile</em> → <em>Security</em> and add a passkey. Then sign in with your fingerprint, face or the PIN of your device.</p>",
				"<p>Öffnen Sie <em>Mein Profil</em> → <em>Sicherheit</em> und fügen Sie einen Passkey hinzu. Danach melden Sie sich mit Fingerabdruck, Gesicht oder der PIN Ihres Geräts an.</p>"},
			{"My laptop is broken – what now?", "Mein Laptop ist kaputt – was nun?",
				"<p>Start a <em>device request</em> under Forms and workflows. IT gets it at once.</p>",
				"<p>Starten Sie einen <em>Geräteantrag</em> unter Formulare und Abläufe. Die IT bekommt ihn sofort.</p>"},
		}},
		{"HR", "Personal", [][4]string{
			{"How do I request holiday?", "Wie beantrage ich Urlaub?",
				`<p>With the <a href="` + link + `">holiday request</a>. Your team lead decides; you get a message either way.</p>`,
				`<p>Mit dem <a href="` + link + `">Urlaubsantrag</a>. Ihre Teamleitung entscheidet; Sie bekommen in jedem Fall eine Nachricht.</p>`},
			{"Who stands in for me?", "Wer vertritt mich?",
				"<p>Name your deputy in the holiday request. Under <em>My tasks</em> you can also set a deputy for your tasks.</p>",
				"<p>Nennen Sie Ihre Vertretung im Urlaubsantrag. Unter <em>Meine Aufgaben</em> legen Sie auch eine Vertretung für Ihre Aufgaben fest.</p>"},
		}},
		{"Working with the wiki", "Arbeiten mit dem Wiki", [][4]string{
			{"How do I restore an old version?", "Wie stelle ich eine alte Version wieder her?",
				"<p>Open <em>History</em>, choose the version and click <em>Restore</em>. Nothing is lost: the restored text is a new version.</p>",
				"<p>Öffnen Sie den <em>Verlauf</em>, wählen Sie die Version und klicken Sie auf <em>Wiederherstellen</em>. Nichts geht verloren: Der alte Text wird eine neue Version.</p>"},
			{"Who sees a page?", "Wer sieht eine Seite?",
				"<p>Everybody with the right <em>view</em> on its category (inherited from above). A page can also have rights of its own.</p>",
				"<p>Alle mit dem Recht <em>Ansehen</em> auf ihre Kategorie (von oben vererbt). Eine Seite kann auch eigene Rechte haben.</p>"},
		}},
	}
	for i, sec := range secs {
		sc := d.category(r.faq, sec.en, sec.de, "", "", map[string]any{"position": i})
		for _, q := range sec.qs {
			d.page(sc, q[0], q[1], q[2], q[3], nil)
		}
	}
	d.optional("faq", func() {
		r.faqEl = d.element(r.faq, "content.faq", d.tr("Knowledge base", "Wissensdatenbank"), d.perLang(func(tr trf) any {
			return map[string]any{"category_id": r.faq, "recursive": true, "search": true, "numbering": true,
				"intro": htmlBodyOf(r.hintText(tr, "Each subcategory is a section, each page a question. Type into the search field: the questions filter while you type.",
					"Jede Unterkategorie ist ein Abschnitt, jede Seite eine Frage. Tippen Sie ins Suchfeld: Die Fragen filtern sich beim Tippen.", "faq"))}
		}), map[string]any{"position": 0})
		if r.faqEl != 0 {
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.faq), map[string]any{"default_element_id": r.faqEl}, nil)
		}
	})
}

// companyFlows: the holiday request and the other workflows and forms.
func companyFlows(d *demoRun, r *demoRefs) {
	leads := "#" + itoa(r.leads)
	editors := "#" + itoa(r.editors)
	end := func(tr trf) map[string]any {
		return map[string]any{"id": "done", "name": tr("Done", "Erledigt"), "type": "end", "outcome": "success", "actions": []any{}}
	}
	// the holiday request: a start form, the decision of the team lead,
	// the approved holiday in the calendar of the teams, a message either way
	d.optional("workflow", func() {
		r.holidayFlow = d.element(r.forms, "workflow.process", d.tr("Holiday request", "Urlaubsantrag"), d.perLang(func(tr trf) any {
			steps := []map[string]any{
				{"id": "decide", "name": tr("Decision of the team lead", "Entscheidung der Teamleitung"), "assignee": map[string]string{"type": "group", "value": leads},
					"actions": []map[string]any{{"id": "ok", "label": tr("Approve", "Genehmigen"), "next": "enter"}, {"id": "no", "label": tr("Reject", "Ablehnen"), "next": "tell_no", "note": "required"}},
					"form":    []map[string]string{{"field": "from", "mode": "read"}, {"field": "to", "mode": "read"}, {"field": "days", "mode": "read"}, {"field": "deputy", "mode": "read"}}},
				{"id": "enter", "name": tr("Enter in the calendar", "In den Kalender eintragen"), "type": "calendar", "next": "tell_ok",
					"calendar": map[string]any{"calendar": r.teamCalendar, "title": tr("Holiday: {{starter.name}} ({{field.days}} days)", "Urlaub: {{starter.name}} ({{field.days}} Tage)"),
						"start": "{{field.from}}", "all_day": true, "minutes": 1440}},
				{"id": "tell_ok", "name": tr("Tell the requester", "Antragsteller informieren"), "type": "notify", "next": "done",
					"notify": map[string]any{"to": "starter", "channel": "inbox", "subject": tr("Approved: your holiday from {{field.from}}", "Genehmigt: Ihr Urlaub ab {{field.from}}"),
						"body": tr("Have a good rest!", "Gute Erholung!")}},
				{"id": "tell_no", "name": tr("Tell the requester", "Antragsteller informieren"), "type": "notify", "next": "no",
					"notify": map[string]any{"to": "starter", "channel": "inbox", "subject": tr("Not approved: your holiday from {{field.from}}", "Nicht genehmigt: Ihr Urlaub ab {{field.from}}"),
						"body": "{{last.note}}"}},
				end(tr),
				{"id": "no", "name": tr("Rejected", "Abgelehnt"), "type": "end", "outcome": "failure", "actions": []any{}},
			}
			if r.teamCalendar == 0 { // no calendar of the teams in this demo
				steps[0]["actions"].([]map[string]any)[0]["next"] = "tell_ok"
				steps = append(steps[:1], steps[2:]...)
			}
			return map[string]any{
				"description": tr("Fill in the form and press Start. The team lead decides under My tasks; the approved holiday is entered in the calendar of the teams and you get a message. In this demo you belong to the team leads, so you can decide your own request.",
					"Formular ausfüllen und Starten drücken. Die Teamleitung entscheidet unter Meine Aufgaben; genehmigter Urlaub wird in den Kalender der Teams eingetragen, und Sie bekommen eine Nachricht. In dieser Demo gehören Sie zur Teamleitung und können Ihren Antrag selbst entscheiden."),
				"fields": []map[string]any{
					{"id": "from", "label": tr("First day", "Erster Tag"), "type": "date", "required": true},
					{"id": "to", "label": tr("Last day", "Letzter Tag"), "type": "date", "required": true},
					{"id": "days", "label": tr("Working days", "Arbeitstage"), "type": "number", "required": true},
					{"id": "deputy", "label": tr("Deputy", "Vertretung"), "type": "text"},
					{"id": "note", "label": tr("Note", "Bemerkung"), "type": "textarea"}},
				"steps": steps}
		}), map[string]any{"position": 0})
	})
	d.optional("workflow2", func() {
		wf := func(tr trf) any {
			return map[string]any{
				"description": tr("Computes the total, asks for approval above 1000 € (three working days, with a warning and hand-over) and tells the requester. Also a monthly check (switched off) and an incoming webhook (needs a secret first).",
					"Berechnet die Summe, verlangt über 1000 € eine Freigabe (drei Arbeitstage, mit Warnung und Übergabe) und informiert die antragstellende Person. Dazu eine monatliche Prüfung (ausgeschaltet) und ein eingehender Webhook (braucht erst ein Geheimnis)."),
				"fields": []map[string]any{
					{"id": "item", "label": tr("Device", "Gerät"), "type": "text", "required": true},
					{"id": "qty", "label": tr("Quantity", "Anzahl"), "type": "number", "required": true},
					{"id": "price", "label": tr("Price per piece (€)", "Preis pro Stück (€)"), "type": "number", "required": true}},
				"steps": []map[string]any{
					{"id": "sum", "name": tr("Compute the total", "Summe berechnen"), "type": "set", "next": "big",
						"set": []map[string]any{{"field": "total", "formula": "round(qty * price, 2)"}}},
					{"id": "big", "name": tr("More than 1000 €?", "Mehr als 1000 €?"), "type": "condition", "match": "all", "next": "approve", "next_else": "ok",
						"rules": []map[string]any{{"field": "total", "op": "gt", "value": "1000"}}},
					{"id": "approve", "name": tr("Approval", "Freigabe"), "type": "approval", "assignee": map[string]string{"type": "group", "value": leads}, "required": 1,
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
		device := d.element(r.forms, "workflow.process", d.tr("Device request", "Geräteantrag"), d.perLang(wf), map[string]any{"position": 1})
		r.deviceFlow = device
		if d.err != nil || device <= 0 {
			return
		}
		// onboarding: a welcome page, the device request as sub-workflow, a deadline from the start date
		d.element(r.forms, "workflow.process", d.tr("Onboarding", "Einarbeitung"), d.perLang(func(tr trf) any {
			return map[string]any{
				"description": tr("Creates a welcome page, starts the device request as a sub-workflow and waits for it, then the team prepares the workplace – due two working days before the first day.",
					"Legt eine Willkommensseite an, startet den Geräteantrag als Unter-Workflow und wartet darauf, danach bereitet das Team den Arbeitsplatz vor – fällig zwei Arbeitstage vor dem ersten Tag."),
				"fields": []map[string]any{{"id": "name", "label": tr("Name", "Name"), "type": "text", "required": true},
					{"id": "start", "label": tr("First day", "Erster Arbeitstag"), "type": "date", "required": true},
					{"id": "device", "label": tr("Laptop model", "Laptop-Modell"), "type": "text"}},
				"steps": []map[string]any{
					{"id": "page", "name": tr("Welcome page", "Willkommensseite"), "type": "create", "next": "laptop",
						"create": map[string]any{"category": r.onboarding, "title": tr("Welcome, {{field.name}}!", "Willkommen, {{field.name}}!"), "store_id": "page", "store_url": "page_url",
							"body": tr("# Welcome {{field.name}}\n\nYour first day: **{{field.start}}**.", "# Willkommen {{field.name}}\n\nIhr erster Tag: **{{field.start}}**.")}},
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
		}), map[string]any{"position": 2})
		// projects: when the status of a project becomes "Done", a closing report is due
		d.element(r.forms, "workflow.process", d.tr("Project closed (trigger)", "Projekt abgeschlossen (Auslöser)"), d.perLang(func(tr trf) any {
			return map[string]any{
				"description": tr("Starts by itself when the status of a project (Projects) becomes “Done”: creates the page of the closing report and asks the team for a review within five working days.",
					"Startet von selbst, wenn der Status eines Projekts (Projekte) „Erledigt“ wird: legt die Seite des Abschlussberichts an und bittet das Team um Prüfung innerhalb von fünf Arbeitstagen."),
				"no_manual": true,
				"fields":    []map[string]any{{"id": "project", "label": tr("Project", "Projekt"), "type": "text"}, {"id": "link", "label": tr("Link", "Link"), "type": "text"}},
				"triggers": []map[string]any{{"id": "done", "type": "content", "name": tr("Status becomes Done", "Status wird Erledigt"),
					"sources": fmt.Sprintf("category:%d+sub", r.projects), "prop": map[string]string{"field": "status", "op": "equals", "value": "Done"},
					"title": tr("Closing report: {{trigger.object.title}}", "Abschlussbericht: {{trigger.object.title}}"),
					"data":  map[string]string{"project": "{{trigger.object.title}}", "link": "{{trigger.object.url}}"}}},
				"steps": []map[string]any{
					{"id": "report", "name": tr("Report page", "Berichtsseite"), "type": "create", "next": "review",
						"create": map[string]any{"category": r.projects, "title": tr("Closing report: {{field.project}}", "Abschlussbericht: {{field.project}}"), "store_url": "report",
							"body": tr("Project: {{field.link}}\n\n## Results\n\n## Lessons learned", "Projekt: {{field.link}}\n\n## Ergebnisse\n\n## Erkenntnisse")}},
					{"id": "review", "name": tr("Review the report", "Bericht prüfen"), "assignee": map[string]string{"type": "group", "value": editors},
						"actions":  []map[string]any{{"id": "ok", "label": tr("Reviewed", "Geprüft"), "next": "done"}},
						"deadline": map[string]any{"days": 5, "workdays": true, "warn": []int{24}, "escalate": map[string]any{"action": "reassign", "assignee": map[string]string{"type": "role", "value": "Admin"}}},
						"notices":  []map[string]any{{"on": "enter", "to": "assignees", "channel": "inbox", "subject": tr("Please review: {{field.report}}", "Bitte prüfen: {{field.report}}")}}},
					end(tr)}}
		}), map[string]any{"position": 5})
		// contracts: one review per department at the same time, a form in each step
		d.element(r.forms, "workflow.process", d.tr("Contract review (parallel)", "Vertragsprüfung (parallel)"), d.perLang(func(tr trf) any {
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
		}), map[string]any{"position": 6})
		// comments: the administration hears about every comment on the documents
		d.element(r.forms, "workflow.process", d.tr("Feedback on comments (event)", "Rückmeldung zu Kommentaren (Ereignis)"), d.perLang(func(tr trf) any {
			return map[string]any{
				"description": tr("Starts for every comment in “Documents” and tells the administration; also a weekly review (switched off) as a schedule.",
					"Startet bei jedem Kommentar in „Dokumente“ und informiert die Verwaltung; dazu ein wöchentlicher Rückblick (ausgeschaltet) als Zeitplan."),
				"no_manual": true,
				"fields":    []map[string]any{{"id": "page", "label": tr("Page", "Seite"), "type": "text"}},
				"triggers": []map[string]any{
					{"id": "comment", "type": "event", "event": "comment.create", "category": r.docs, "run_as": "actor", "name": tr("New comment", "Neuer Kommentar"),
						"title": tr("Comment on {{trigger.object.title}}", "Kommentar zu {{trigger.object.title}}"), "data": map[string]string{"page": "{{trigger.object.title}}"}},
					{"id": "weekly", "type": "schedule", "cron": "0 8 * * 1", "off": true, "name": tr("Weekly review", "Wöchentlicher Rückblick"),
						"title": tr("Weekly review {{date}}", "Wochenrückblick {{date}}"), "data": map[string]string{"page": tr("all pages", "alle Seiten")}}},
				"steps": []map[string]any{
					{"id": "tell", "name": tr("Tell the administration", "Verwaltung informieren"), "type": "notify", "next": "done",
						"notify": map[string]any{"to": "group:" + editors, "channel": "inbox", "subject": tr("{{case.title}} by {{trigger.actor}}", "{{case.title}} von {{trigger.actor}}")}},
					end(tr)}}
		}), map[string]any{"position": 7})
		d.element(r.forms, "workflow.process", d.tr("New colleagues (accounts)", "Neue Kolleginnen und Kollegen (Konten)"), d.perLang(func(tr trf) any {
			return map[string]any{
				"description": tr("Starts for every new account (administration, SCIM or sign-in provider): the person may read “Documents” for 90 days and the administration is told. Try it with a test run.",
					"Startet bei jedem neuen Konto (Administration, SCIM oder Anmeldeanbieter): Die Person darf „Dokumente“ 90 Tage lang lesen, die Verwaltung wird informiert. Mit einem Testlauf ausprobieren."),
				"no_manual": true,
				"fields":    []map[string]any{{"id": "who", "label": tr("User name", "Benutzername"), "type": "text", "required": true}},
				"triggers": []map[string]any{
					{"id": "new", "type": "event", "event": "user.created", "name": tr("New account", "Neues Konto"),
						"title": tr("Welcome {{trigger.data.display_name}}", "Willkommen {{trigger.data.display_name}}"), "data": map[string]string{"who": "{{trigger.data.username}}"}}},
				"steps": accountSteps(tr, r.docs, r.media, r.calendar, editors, end(tr))}
		}), map[string]any{"position": 8})
	})
	d.optional("forms", func() {
		r.contactForm = d.element(r.forms, "content.forms", d.tr("Contact form", "Kontaktformular"), d.perLang(func(tr trf) any {
			return map[string]any{"intro": tr("Write to the administration – the fields adapt to your answers (“Other” asks for details), files can be dropped onto the upload area.",
				"Schreiben Sie der Verwaltung – die Felder passen sich Ihren Antworten an („Anderes“ fragt nach), Dateien lassen sich auf die Upload-Fläche ziehen."),
				"submit_label": tr("Send request", "Anfrage senden"), "success_message": tr("Thank you! We will get back to you.", "Danke! Wir melden uns."), "layout": "grid",
				"fields": []map[string]any{
					{"id": "name", "type": "text", "label": tr("Your name", "Ihr Name"), "required": true, "autocomplete": "name", "prefill": "user.name", "width": 6},
					{"id": "mail", "type": "email", "label": tr("E-mail", "E-Mail"), "required": true, "autocomplete": "email", "prefill": "user.email", "width": 6},
					{"id": "topic", "type": "radio", "label": tr("What is it about?", "Worum geht es?"), "required": true, "options": []map[string]string{
						{"value": "it", "label": "IT"}, {"value": "hr", "label": tr("HR", "Personal")}, {"value": "other", "label": tr("Other", "Anderes")}}},
					{"id": "which", "type": "text", "label": tr("What exactly?", "Was genau?"), "required": true, "show_if": map[string]string{"field": "topic", "op": "eq", "value": "other"}},
					{"id": "msg", "type": "textarea", "label": tr("Message", "Nachricht"), "help": tr("Please do not send passwords.", "Bitte keine Passwörter senden."), "rows": 4},
					{"id": "rate", "type": "rating", "label": tr("How happy are you with the intranet?", "Wie zufrieden sind Sie mit dem Intranet?")},
					{"id": "doc", "type": "file", "label": tr("Attachment", "Anhang"), "accept": []string{"pdf", "image"}, "max_files": 3, "max_size_mb": 5, "file_comment": true},
					{"id": "ok", "type": "consent", "label": tr("I agree that my details are processed.", "Ich stimme der Verarbeitung meiner Angaben zu."), "required": true},
				}, "targets": map[string]any{"store": true}}
		}), map[string]any{"position": 9})
	})
	// the overview of the category: what is here and how to start
	overview := d.pageF(r.forms, "Forms and workflows", "Formulare und Abläufe", func(tr trf) string {
		item := func(id int64, en, de, textEN, textDE string) string {
			if id == 0 {
				return ""
			}
			return `<li><a href="` + eURL(id) + `">` + tr(en, de) + `</a> – ` + tr(textEN, textDE) + `</li>`
		}
		return `<p>` + tr("Requests at Enterprise IT Services run by themselves: you fill in a form, the right people get a task, and you get a message.",
			"Anträge laufen bei Enterprise IT Services von selbst: Sie füllen ein Formular aus, die richtigen Personen bekommen eine Aufgabe, und Sie bekommen eine Nachricht.") + `</p><ul>` +
			item(r.holidayFlow, "Holiday request", "Urlaubsantrag", "decided by the team lead, then in the calendar.", "entscheidet die Teamleitung, danach im Kalender.") +
			item(r.deviceFlow, "Device request", "Geräteantrag", "above 1000 € with an approval.", "über 1000 € mit Freigabe.") +
			item(r.ideas, "Idea box", "Ideenbox", "a form in three steps.", "ein Formular in drei Schritten.") +
			item(r.contactForm, "Contact form", "Kontaktformular", "a message to the administration.", "eine Nachricht an die Verwaltung.") + `</ul>` +
			r.hint(tr, "A workflow has a start form and steps: tasks for people, approvals, messages, calendar entries. Who decides finds the task under My tasks.",
				"Ein Workflow hat ein Startformular und Schritte: Aufgaben für Personen, Freigaben, Nachrichten, Kalendereinträge. Wer entscheidet, findet die Aufgabe unter Meine Aufgaben.", "workflows")
	}, map[string]any{"position": -1})
	d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.forms), map[string]any{"default_element_id": overview}, nil)
}

func companyDocs(d *demoRun, r *demoRefs) {
	d.optional("attachment", func() {
		var file struct {
			Hash string `json:"hash"`
			Name string `json:"name"`
			Mime string `json:"mime"`
			Size int64  `json:"size"`
		}
		d.call(http.MethodPost, "/api/v1/files", multipartFile(nil, "richtlinie.pdf", demoPDF([]string{"Enterprise IT Services - Richtlinie Informationssicherheit", "", "Ein PDF als Element: Vorschau im Browser."})), &file)
		if d.err == nil {
			r.guideline = d.element(r.docs, "core.attachment", d.tr("Information security guideline (PDF)", "Richtlinie Informationssicherheit (PDF)"), d.same(map[string]any{"file": file, "description": "Demo"}), nil)
		}
	})
	d.pageF(r.docs, "Documents of the media library", "Dokumente der Medienbibliothek", func(tr trf) string {
		return `<p>` + tr("Files of the media library, each with its own permissions:", "Dateien der Medienbibliothek, jede mit eigenen Rechten:") + `</p><ul>` +
			`<li><a href="/api/v1/media/` + itoa(r.guide) + `/view">` + tr("Quick guide (PDF)", "Kurzanleitung (PDF)") + `</a></li>` +
			`<li><a href="/api/v1/media/` + itoa(r.confidential) + `/view">` + tr("Confidential minutes – view only, with your name as watermark", "Vertrauliches Protokoll – nur ansehen, mit Ihrem Namen als Wasserzeichen") + `</a></li>` +
			`<li><a href="/media?open=` + itoa(r.letter) + `">` + tr("Letter template – opens in the office program if one is set up", "Briefvorlage – öffnet sich im Office-Programm, wenn eines eingerichtet ist") + `</a></li></ul>` +
			r.hint(tr, "This category is marked confidential: every page shows it, and it stays out of feeds.",
				"Diese Kategorie ist als vertraulich eingestuft: Jede Seite zeigt das an, und sie erscheint in keinem Feed.", "rights")
	}, map[string]any{"position": 0})
	// a page with rights of its own: only administrators and managers of the
	// category see it (own rights only), classified strictly confidential
	d.optional("element_rights", func() {
		r.boardPage = d.pageF(r.docs, "Decisions of the management (own rights only)", "Beschlüsse der Geschäftsleitung (nur eigene Rechte)", func(tr trf) string {
			return `<p>` + tr("This page has rights of its own: only the people named in its <strong>Rights</strong> see it – plus administrators and managers of the category. It appears nowhere else: not in lists, the search or feeds.",
				"Diese Seite hat eigene Rechte: Nur die in ihren <strong>Rechten</strong> genannten Personen sehen sie – dazu Administratoren und Verwalter der Kategorie. Sonst erscheint sie nirgends: nicht in Listen, der Suche oder Feeds.") + `</p>`
		}, nil)
		if r.boardPage != 0 {
			d.call(http.MethodPut, fmt.Sprintf("/api/v1/elements/%d/acl", r.boardPage), map[string]any{"own_rights_only": true, "classification": 3}, nil)
		}
	})
	d.optional("classification", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.docs), map[string]any{"classification": 2}, nil)
	})
}

// companyDisplays: categories shown as cards, as a table and all on one page.
func companyDisplays(d *demoRun, r *demoRefs) {
	for i, x := range [][4]string{
		{"Your first day", "Ihr erster Tag", "<p>The people, tools and places of your first day.</p>", "<p>Die Menschen, Werkzeuge und Orte Ihres ersten Tages.</p>"},
		{"Your mentor", "Ihre Mentorin, Ihr Mentor", "<p>A colleague who answers your questions in the first weeks.</p>", "<p>Eine Kollegin oder ein Kollege, die oder der in den ersten Wochen Ihre Fragen beantwortet.</p>"},
		{"Your workplace", "Ihr Arbeitsplatz", "<p>Desk, laptop, phone: what you get and how to ask for it.</p>", "<p>Schreibtisch, Laptop, Telefon: was Sie bekommen und wie Sie es anfordern.</p>"},
	} {
		d.page(r.onboarding, x[0], x[1], x[2], x[3], map[string]any{"position": i})
	}
	d.display(r.onboarding, map[string]any{"view": "cards", "excerpt": true})
	for i, x := range [][4]string{
		{"Opening hours", "Öffnungszeiten", "<p>Monday to Friday, 7:30 to 17:00.</p>", "<p>Montag bis Freitag, 7:30 bis 17:00 Uhr.</p>"},
		{"Address", "Adresse", "<p>Enterprise IT Services, Example Street 1, 8010 Graz</p>", "<p>Enterprise IT Services, Beispielstraße 1, 8010 Graz</p>"},
		{"Parking", "Parken", "<p>Parking in the yard, entrance from the side street. Bicycle stands are at the door.</p>", "<p>Parkplätze im Hof, Einfahrt über die Seitengasse. Fahrradständer stehen an der Tür.</p>"},
	} {
		d.page(r.travel, x[0], x[1], x[2], x[3], map[string]any{"position": i})
	}
	d.display(r.travel, map[string]any{"view": "all"})
	for i, x := range [][5]string{
		{"Meeting room Mur", "Besprechungsraum Mur", "12", "<p>With projector and video conferencing.</p>", "<p>Mit Beamer und Videokonferenz.</p>"},
		{"Meeting room Schöckl", "Besprechungsraum Schöckl", "6", "<p>Small room for quick meetings.</p>", "<p>Kleiner Raum für kurze Besprechungen.</p>"},
		{"Training room", "Schulungsraum", "20", "<p>For training of customers and colleagues.</p>", "<p>Für Schulungen von Kunden und im Team.</p>"},
	} {
		id := d.page(r.rooms, x[0], x[1], x[3], x[4], map[string]any{"position": i})
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/x/core.properties/elements/%d", id), map[string]any{
			"values": map[string]any{"seats": x[2]}, "free": map[string]any{"seats": map[string]any{"type": "integer", "label": d.tr("Seats", "Plätze")[d.lang()]}}}, nil)
	}
	d.display(r.rooms, map[string]any{"view": "table", "columns": []string{"seats"}})
}

// companyReports: reports from the properties of the projects.
func companyReports(d *demoRun, r *demoRefs) {
	if r.propdb == 0 {
		return
	}
	src := "category:" + itoa(r.projects)
	rep := func(params map[string]string) string {
		params["source"] = src
		if params["conditions"] != "" {
			params["conditions"] += "; "
		}
		params["conditions"] += "tags has project"
		return macro("properties", params, "")
	}
	r.reports = d.pageF(r.projects, "Reports: projects", "Berichte: Projekte", func(tr trf) string {
		h := func(en, de string) string { return `<h2>` + tr(en, de) + `</h2>` }
		return `<p>` + tr("Reports from the properties of the projects – always with the permissions of the reader.", "Berichte aus den Eigenschaften der Projekte – jeweils mit den Rechten der Lesenden.") + `</p>` +
			r.hint(tr, "Every project page has properties such as status, budget and owner. Each report below is a block that reads them – change a status, and all reports follow.",
				"Jede Projektseite hat Eigenschaften wie Status, Budget und Verantwortliche. Jeder Bericht unten ist ein Baustein, der sie liest – ändert sich ein Status, folgen alle Berichte.", "properties") +
			h("Table by status with totals", "Tabelle nach Status mit Summen") + rep(map[string]string{"view": "table", "columns": "ref,priority,owner,budget,spent,progress", "group": "status", "totals": "true", "sorting": "-budget", "live_filters": "priority,owner"}) +
			h("Board", "Board") + rep(map[string]string{"view": "kanban", "group": "status", "columns": "priority,owner,due"}) +
			h("Budget by status", "Budget nach Status") + rep(map[string]string{"view": "chart", "chart": "bar", "group": "status", "agg": "sum", "value": "budget", "title": tr("Budget by status", "Budget nach Status")}) +
			h("Projects by area and priority", "Projekte nach Bereich und Priorität") + rep(map[string]string{"view": "chart", "chart": "stacked", "group": "areas", "group2": "priority", "title": tr("Projects by area and priority", "Projekte nach Bereich und Priorität")}) +
			h("Share by priority", "Anteil nach Priorität") + rep(map[string]string{"view": "chart", "chart": "donut", "group": "priority", "title": tr("Share by priority", "Anteil nach Priorität")}) +
			h("Key figures", "Kennzahlen") + rep(map[string]string{"view": "kpi", "group": "status", "agg": "sum", "value": "budget"}) +
			h("Pivot: status × priority", "Pivot: Status × Priorität") + rep(map[string]string{"view": "pivot", "group": "status", "group2": "priority"}) +
			h("Timeline", "Zeitleiste") + rep(map[string]string{"view": "timeline", "date": "period"}) +
			h("Calendar of milestones", "Kalender der Meilensteine") + rep(map[string]string{"view": "calendar", "date": "due"}) +
			h("Overdue and due soon", "Überfällig und bald fällig") + rep(map[string]string{"view": "overdue", "date": "due", "columns": "owner,status", "conditions": "status != Done"}) +
			h("Completeness", "Vollständigkeit") + rep(map[string]string{"view": "completeness", "columns": "owner,due,budget,rating"}) +
			h("Locations", "Standorte") + rep(map[string]string{"view": "map", "columns": "site"})
	}, map[string]any{"position": 1})
}
