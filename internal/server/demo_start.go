package server

// The ways into the demo: the start page of the site for visitors (what
// the wiki is good for, in words, no technical details), the first page of
// the demo area (the two areas) and the parts outside the pages (menu,
// contact block, carousel).

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// identityOf: the account of the administrator who creates the demo.
func identityOf(d *demoRun) int64 {
	if p := identity.From(d.ctx); p != nil {
		return p.UserID
	}
	return 0
}

// landingUses: what the wiki is used for, each with the place in the demo
// that shows it (the link is filled in by landingPage).
var landingUses = []struct{ icon, en, enText, de, deText string }{
	{"building-community", "Intranet", "News, events, teams and requests – the daily work of a company in one place.",
		"Intranet", "Neuigkeiten, Termine, Teams und Anträge – der Arbeitsalltag einer Firma an einem Ort."},
	{"book-2", "Handbook", "Chapters with a navigation of their own, always up to date, with versions.",
		"Handbuch", "Kapitel mit eigener Navigation, immer aktuell, mit Versionen."},
	{"help-circle", "Knowledge base", "Questions and answers that filter while you type.",
		"Wissensdatenbank", "Fragen und Antworten, die sich beim Tippen filtern."},
	{"checklist", "Forms and workflows", "Holiday, orders, approvals: requests that run by themselves.",
		"Abläufe und Formulare", "Urlaub, Bestellungen, Freigaben: Anträge, die von selbst laufen."},
	{"school", "University", "Courses with registration, sign-in with the university account, sites for faculties.",
		"Hochschule", "Kurse mit Anmeldung, Anmeldung mit dem Hochschulkonto, eigene Sites für Fakultäten."},
	{"world-www", "Website", "Banners, cards and buttons – a public website from the same building blocks.",
		"Website", "Banner, Karten und Buttons – eine öffentliche Website aus denselben Bausteinen."},
}

// landingHighlights: what makes the wiki special, in words for visitors;
// every point is a feature of the wiki.
var landingHighlights = []struct{ icon, en, enText, de, deText string }{
	{"shield-lock", "Security first", "Permissions down to single pages, sign-in with passkey, a complete log.",
		"Sicherheit zuerst", "Rechte bis auf einzelne Seiten, Anmeldung mit Passkey, ein lückenloses Protokoll."},
	{"accessible", "Accessible", "Usable with keyboard and screen reader, checked automatically.",
		"Barrierefrei", "Bedienbar mit Tastatur und Screenreader, laufend automatisch geprüft."},
	{"pencil", "Easy to write", "Visual editor, writing together live, every version kept.",
		"Einfach schreiben", "Visueller Editor, gemeinsam live schreiben, jede Version bleibt."},
	{"layout-grid", "More than 50 building blocks", "From tabs to charts – each with options for its look.",
		"Über 50 Bausteine", "Von Reitern bis Diagrammen – jeder mit Optionen für sein Aussehen."},
	{"language", "Many languages", "Interface in German and English, every page in several languages.",
		"Mehrsprachig", "Oberfläche auf Deutsch und Englisch, jede Seite in mehreren Sprachen."},
	{"server", "Runs with you", "On your own servers or in your cloud, without third-party services.",
		"Bei Ihnen betrieben", "Auf eigenen Servern oder in Ihrer Cloud, ohne fremde Dienste."},
}

// landingPage creates the start page of the site: curious-making, the
// highlights, use cases as ways into the demo, with a short address. It
// lives in a hidden category of its own with the website template; removing
// the demo restores the previous start page.
func landingPage(d *demoRun, r *demoRefs) {
	links := []string{cURL(r.company), cURL(r.handbook), cURL(r.faq), cURL(r.forms), stationOr(r, "events", cURL(r.tour)), stationOr(r, "website", cURL(r.tour))}
	build := func(tr trf) string {
		var b strings.Builder
		// the opening: the promise and the two ways in, the logo beside it
		b.WriteString(macro("section", map[string]string{"background": "light", "width": "wide"},
			macro("grid", map[string]string{"desktop": "2", "tablet": "2", "mobile": "1", "widths": "3 2", "align": "center", "gap": "large"},
				macro("heading", map[string]string{"text": tr("Knowledge that gets through.", "Wissen, das ankommt."), "size": "xl",
					"subtitle": tr("Intranet, handbook and website in one – secure, accessible and run by you.", "Intranet, Handbuch und Website in einem – sicher, barrierefrei und bei Ihnen betrieben.")}, "")+
					macro("buttons", map[string]string{"text1": tr("See the example company", "Beispiel-Firma ansehen"), "link1": cURL(r.company), "icon1": "building-community",
						"text2": tr("Start the tour", "Rundgang starten"), "link2": cURL(r.tour), "icon2": "route-2", "look2": "secondary"}, "")+
					`<hr><p><img src="/brand/logo.webp" alt="Enterprise Wiki" width="315" height="128"></p>`)))
		b.WriteString(macro("heading", map[string]string{"text": tr("What you use it for", "Wofür Sie es nutzen"), "subtitle": tr("Choose a use – the demo shows it right away.", "Wählen Sie einen Einsatz – die Demo zeigt ihn sofort."), "align": "center"}, ""))
		var cards []string
		for i, u := range landingUses {
			cards = append(cards, macro("iconbox", map[string]string{"name": u.icon, "size": "l", "decorative": "true", "title": tr(u.en, u.de), "link": links[i]}, `<p>`+tr(u.enText, u.deText)+`</p>`))
		}
		b.WriteString(macro("grid", map[string]string{"desktop": "3", "tablet": "2", "mobile": "1"}, strings.Join(cards, "<hr>")))
		b.WriteString(macro("heading", map[string]string{"text": tr("Highlights", "Das Wichtigste auf einen Blick"), "align": "center"}, ""))
		var hl []string
		for _, h := range landingHighlights {
			hl = append(hl, macro("iconbox", map[string]string{"name": h.icon, "decorative": "true", "title": tr(h.en, h.de), "position": "left"}, `<p>`+tr(h.enText, h.deText)+`</p>`))
		}
		b.WriteString(macro("grid", map[string]string{"desktop": "3", "tablet": "2", "mobile": "1"}, strings.Join(hl, "<hr>")))
		b.WriteString(macro("section", map[string]string{"background": "light", "width": "normal", "align": "center"},
			`<h2>`+tr("Two ways into the demo", "Zwei Wege in die Demo")+`</h2><p>`+
				tr("The example company shows the wiki at work. The tour explains every feature, station by station.", "Die Beispiel-Firma zeigt das Wiki im Einsatz. Der Rundgang erklärt jede Funktion, Station für Station.")+`</p>`+
				macro("buttons", map[string]string{"text1": tr("Example company", "Beispiel-Firma"), "link1": cURL(r.company), "text2": tr("Tour", "Rundgang"), "link2": cURL(r.tour), "align": "center"}, "")))
		return b.String()
	}
	start := d.category(r.root, "Start page", "Startseite", "The start page of the site for visitors.", "Die Startseite der Site für Besucherinnen und Besucher.",
		map[string]any{"hidden": true, "position": 0})
	short := "welcome"
	if d.lang() == "de" {
		short = "willkommen"
	}
	id := d.pageF(start, "Enterprise Wiki at a glance", "Enterprise Wiki auf einen Blick", build, map[string]any{"short_url": short + r.suffix, "position": 0})
	if d.err != nil || id == 0 {
		return
	}
	d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", start), map[string]any{"default_element_id": id}, nil)
	// the look of a website (template of the category)
	d.optional("category-template", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", start), map[string]any{"template": "theme.cms.modern"}, nil)
	})
	// no personal lists (tasks, favourites) on the page for visitors
	d.optional("start page blocks", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category?target=%d", start), map[string]any{"placements": []map[string]any{
			{"block": "core.tree", "position": ""}, {"block": "core.favorites", "position": ""}, {"block": "workflow.process.tasks", "position": ""}}}, nil)
	})
	// a page for visitors: no path, title (it stays for screen readers),
	// page details, buttons, reactions or comments
	// (blocks of plugins that are off are refused: then the page keeps them)
	d.optional("start page layout", func() {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/instance?target=%d", id), map[string]any{"placements": []map[string]any{
			{"block": "core.element.path", "position": ""}, {"block": "core.element.title", "position": ""}, {"block": "core.element.info", "position": ""},
			{"block": "core.element.watch", "position": ""}, {"block": "core.favorites.star", "position": ""}, {"block": "core.share", "position": ""},
			{"block": "core.reactions", "position": ""}, {"block": "core.comments", "position": ""}}}, nil)
	})
	// the start page of the site; removing the demo restores the old one
	cfg := d.s.app.Config.Get().UI
	d.track("home", fmt.Sprintf("%d:%d:%d", id, cfg.HomeElement, cfg.HomeCategory))
	// (directly, like the removal: the demo leaves no trace in the log)
	if d.err == nil {
		if err := d.s.app.Config.Update(func(c *config.Config) error { c.UI.HomeElement = id; return nil }); err != nil {
			d.err = err
		}
	}
}

func stationOr(r *demoRefs, key, alt string) string {
	if id := r.station[key]; id != 0 {
		return eURL(id)
	}
	return alt
}

// demoHome is the first page of the demo area (/demo): the two areas.
func demoHome(d *demoRun, r *demoRefs) {
	r.home = d.pageF(r.root, "Two ways into the demo", "Zwei Wege in die Demo", func(tr trf) string {
		box := func(icon, en, de, link, textEN, textDE string) string {
			return macro("iconbox", map[string]string{"name": icon, "size": "l", "decorative": "true", "title": tr(en, de), "link": link}, `<p>`+tr(textEN, textDE)+`</p>`)
		}
		return `<p>` + tr("This demo shows Enterprise Wiki with example content. Choose how you want to look around:", "Diese Demo zeigt das Enterprise Wiki mit Beispielinhalten. Wählen Sie, wie Sie sich umsehen möchten:") + `</p>` +
			macro("grid", map[string]string{"desktop": "2", "tablet": "2", "mobile": "1"},
				box("building-community", "Example company: "+companyEN, "Beispiel-Firma: "+companyDE, cURL(r.company),
					"The intranet of a bicycle maker – news, teams, handbook, holiday request and more. The features explain themselves where they are used.",
					"Das Intranet eines Fahrradherstellers – Neuigkeiten, Teams, Handbuch, Urlaubsantrag und mehr. Die Funktionen erklären sich dort, wo sie genutzt werden.")+`<hr>`+
					box("route-2", "Tour of the features", "Rundgang durch die Funktionen", cURL(r.tour),
						"Station by station: what a feature is, how it is done, what it looks like and what it is good for.",
						"Station für Station: was eine Funktion ist, wie sie geht, wie sie aussieht und wofür sie gut ist.")) +
			`<h2>` + tr("Try everything", "Alles ausprobieren") + `</h2><p>` +
			tr("Everything here may be changed – switch on Edit at the top. The ", "Alles hier darf verändert werden – schalten Sie oben Bearbeiten ein. Die ") +
			`<a href="/">` + tr("start page of the site", "Startseite der Site") + `</a>` + tr(" shows what visitors see first.", " zeigt, was Besucherinnen und Besucher zuerst sehen.") + `</p>` +
			macro("panel", map[string]string{"type": "info", "title": tr("Removing the demo", "Die Demo entfernen")},
				`<p>`+tr("Under Administration → Demo the whole demo disappears again with one click – also pages you added inside it.", "Unter Administration → Demo verschwindet die ganze Demo mit einem Klick wieder – auch Seiten, die Sie darin ergänzt haben.")+`</p>`)
	}, map[string]any{"short_url": "demo" + r.suffix, "position": 0})
	if r.home != 0 {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.root), map[string]any{"default_element_id": r.home}, nil)
	}
}

// demoOutside: the parts outside the pages – a menu, a contact block, the
// light and dark buttons and a carousel – each with a note for the station
// "Menus, placement and carousel".
func demoOutside(d *demoRun, r *demoRefs) {
	d.optional("menu", func() {
		// shown as mega menu (wide panels with groups and descriptions) while
		// the plugin core.megamenu is active, otherwise as dropdowns
		entry := func(en, de string, id int64, descEN, descDE string) map[string]any {
			return map[string]any{"label": d.tr(en, de), "category_id": id, "description": d.tr(descEN, descDE)}
		}
		d.create("menu", "/api/v1/x/core.menu/menus", map[string]any{"name": "Demo", "location": "header-nav", "style": "mega", "items": []map[string]any{
			{"label": d.tr("Example company", "Beispiel-Firma"), "category_id": r.company, "children": []map[string]any{
				{"label": d.tr("Stay informed", "Informiert bleiben"), "type": "heading", "children": []map[string]any{
					entry("News", "Neuigkeiten", r.news, "The blog of the company", "Der Blog der Firma"),
					entry("Events", "Termine", r.events, "Calendar with registration", "Kalender mit Anmeldung"),
					entry("Knowledge base", "Wissensdatenbank", r.faq, "Questions with instant search", "Fragen mit Sofortsuche"),
				}},
				{"label": d.tr("Get things done", "Erledigen"), "type": "heading", "children": []map[string]any{
					entry("Forms and workflows", "Formulare und Abläufe", r.forms, "Holiday request, devices, ideas", "Urlaubsantrag, Geräte, Ideen"),
					entry("Handbook", "Handbuch", r.handbook, "Everything for the daily work", "Alles für die tägliche Arbeit"),
					entry("Teams", "Teams", r.teams, "People, decisions and tasks", "Personen, Entscheidungen und Aufgaben"),
					entry("Projects", "Projekte", r.projects, "Table, board and charts", "Tabelle, Board und Diagramme"),
				}},
			}},
			{"label": d.tr("Tour", "Rundgang"), "category_id": r.tour},
			{"label": d.tr("News", "Neuigkeiten"), "category_id": r.news},
			{"label": d.tr("Events", "Termine"), "category_id": r.events},
		}})
		r.where = append(r.where, whereNote{
			en: whereText("en", "Menu “Demo”", "the entries <em>Example company</em>, <em>Tour</em>, <em>News</em> and <em>Events</em>; <em>Example company</em> opens a mega menu with groups and descriptions",
				"Navigation in the header (<code>header-nav</code>), on every page, for everybody who may see the pages", "Administration → <em>Menus</em>"),
			de: whereText("de", "Menü „Demo“", "die Einträge <em>Beispiel-Firma</em>, <em>Rundgang</em>, <em>Neuigkeiten</em> und <em>Termine</em>; <em>Beispiel-Firma</em> öffnet ein Megamenü mit Gruppen und Beschreibungen",
				"Navigation im Kopfbereich (<code>header-nav</code>), auf jeder Seite, für alle, die die Seiten sehen dürfen", "Administration → <em>Menüs</em>"),
		})
	})
	d.optional("block", func() {
		// a block is an element of the type core.block, placed for the
		// company (the category and its subcategories): it goes with the demo
		r.contactBlock = d.element(r.company, "core.block", d.tr("Reception", "Empfang"), d.perLang(func(tr trf) any {
			return htmlBodyOf(`<p><strong>` + tr("Reception", "Empfang") + `</strong><br>` + tr("Monday to Friday, 7:30 to 17:00", "Montag bis Freitag, 7:30 bis 17:00 Uhr") +
				`<br>+43 1 234 56 70<br><a href="mailto:empfang@example.com">empfang@example.com</a></p>`)
		}), map[string]any{"position": 99})
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category?target=%d", r.company), map[string]any{"placements": []map[string]any{
			{"block": fmt.Sprintf("core.blocks/%d", r.contactBlock), "position": "sidebar", "sort": 90}}}, nil)
		// light and dark: not placed by default; in the demo in the footer
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category?target=%d", r.root), map[string]any{"placements": []map[string]any{
			{"block": "core.scheme", "position": "footer", "sort": 95}}}, nil)
		r.where = append(r.where, whereNote{
			en: whereText("en", "Block “Reception”", "an element of the type <em>Block</em> with the opening hours and the contact of the reception",
				"Side column (<code>sidebar</code>), below the tree of categories, on every page of the example company",
				"the page of the block (<em>Where this block is shown</em>, <em>Place the block</em>)"),
			de: whereText("de", "Baustein „Empfang“", "ein Element vom Typ <em>Baustein</em> mit den Öffnungszeiten und dem Kontakt des Empfangs",
				"Seitenleiste (<code>sidebar</code>), unter dem Kategoriebaum, auf jeder Seite der Beispiel-Firma",
				"die Seite des Bausteins (<em>Wo dieser Baustein erscheint</em>, <em>Baustein platzieren</em>)"),
		})
		r.where = append(r.where, whereNote{
			en: whereText("en", "Block “Light and dark”", "three buttons: as the device is set (<em>Auto</em>), <em>Light</em> or <em>Dark</em>; the choice is kept in your profile (as a guest in a cookie)",
				"Footer (<code>footer</code>), on every page of the demo; elsewhere it is not placed by default",
				"<em>Category settings</em> → <em>Placement</em> of the demo area; the same choice: <em>My profile</em> → <em>Colour scheme</em>"),
			de: whereText("de", "Baustein „Hell und dunkel“", "drei Knöpfe: wie das Gerät eingestellt ist (<em>Auto</em>), <em>Hell</em> oder <em>Dunkel</em>; die Wahl bleibt im Profil (als Gast in einem Cookie)",
				"Fußzeile (<code>footer</code>), auf jeder Seite der Demo; sonst ist er anfangs nicht platziert",
				"<em>Kategorie-Einstellungen</em> → <em>Platzierung</em> des Demo-Bereichs; dieselbe Wahl: <em>Mein Profil</em> → <em>Farbschema</em>"),
		})
	})
	d.optional("carousel", func() {
		// no fixed position: placed for the start page of the company only
		r.carousel = d.create("carousel", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Demo", "position": "", "audience": "users", "autoplay": 0, "max_slides": 8, "enabled": true,
			"sources": fmt.Sprintf("category:%d", r.news), "feed_items": 3})
		slides := []struct{ en, de, textEN, textDE string }{
			{"Year-end party", "Jahresfeier", "Register now in the calendar.", "Jetzt im Kalender anmelden."},
			{"The cargo bike Kombi", "Das Lastenrad Kombi", "Our first cargo bike.", "Unser erstes Lastenrad."},
			{"Sign in with a passkey", "Anmelden mit Passkey", "Safer and faster than a password.", "Sicherer und schneller als ein Passwort."},
		}
		for i, s := range slides {
			if d.err != nil {
				break
			}
			// slides have one language: the default language of the wiki
			t := d.tr(s.en, s.de)[d.lang()]
			d.call(http.MethodPost, fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", r.carousel), map[string]any{"kind": "manual", "title": t,
				"text": d.tr(s.textEN, s.textDE)[d.lang()], "link": cURL(r.news),
				"image": "/api/v1/media/" + r.pics[(i+2)%3] + "/view", "image_alt": d.tr([]string{"Evening light", "Noon over the valley", "Morning in the hills"}[i], []string{"Abendlicht", "Mittag über dem Tal", "Morgen in den Hügeln"}[i])[d.lang()]}, nil)
		}
		placed := []map[string]any{{"block": fmt.Sprintf("core.carousel/%d", r.carousel), "position": "content-top", "sort": 5, "no_subcategories": true}}
		if r.contactBlock != 0 { // (the placements of a level are put as a whole)
			placed = append(placed, map[string]any{"block": fmt.Sprintf("core.blocks/%d", r.contactBlock), "position": "sidebar", "sort": 90})
		}
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/placements/category?target=%d", r.company), map[string]any{"placements": placed}, nil)
		r.where = append(r.where, whereNote{
			en: whereText("en", "Carousel “Demo”", "three slides of its own and the newest posts of <em>News</em>: the carousel takes them from a source and stays up to date by itself",
				"Above the content of every page (<code>content-top</code>) – below the header and above the path and the title –, placed only for the category <em>"+companyEN+"</em> without its subcategories, that is on the intranet start page; only for signed-in people",
				"Administration → <em>Carousels</em> (slides); the placement: Edit mode → <em>"+companyEN+"</em> → Category settings → <em>Placement</em>"),
			de: whereText("de", "Karussell „Demo“", "drei eigene Folien und die neuesten Beiträge aus <em>Neuigkeiten</em>: Das Karussell holt sie aus einer Quelle und bleibt von selbst aktuell",
				"Über dem Inhalt jeder Seite (<code>content-top</code>) – unter dem Kopfbereich und über Pfad und Titel –, nur für die Kategorie <em>"+companyDE+"</em> ohne ihre Unterkategorien platziert, also auf der Intranet-Startseite; nur für Angemeldete",
				"Administration → <em>Karussells</em> (Folien); die Platzierung: Bearbeitungsmodus → <em>"+companyDE+"</em> → Kategorie-Einstellungen → <em>Platzierung</em>"),
		})
	})
	r.where = append(r.where, whereNote{
		en: whereText("en", "Handbook", "the pages of the category as navigation (display <em>Handbook</em>) and the page <em>Contact</em> beside the first page",
			"Category: side column (<code>category-aside</code>), only on the page of the category <em>Handbook</em>",
			"Edit mode → <em>Handbook</em> → Category settings → <em>Display and content</em> and <em>Placement</em>"),
		de: whereText("de", "Handbuch", "die Seiten der Kategorie als Navigation (Darstellung <em>Handbuch</em>) und die Seite <em>Kontakt</em> neben der ersten Seite",
			"Kategorie: Seitenspalte (<code>category-aside</code>), nur auf der Seite der Kategorie <em>Handbuch</em>",
			"Bearbeitungsmodus → <em>Handbuch</em> → Kategorie-Einstellungen → <em>Darstellung und Inhalte</em> und <em>Platzierung</em>"),
	})
}
