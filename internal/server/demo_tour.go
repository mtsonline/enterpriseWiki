package server

// Area 2 of the demo: the tour. Like a guided tour through a museum: one
// station per page, always built the same way – what it is, how it is done,
// what it looks like (the live result right on the page), what it is good
// for – and the stations in a row (category displayed as a handbook: all
// stations in the side column, previous and next at the end of the page).

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// stationText is the content of a station in one language.
type stationText struct {
	intro  string   // what it is (one or two sentences)
	how    []string // the steps
	result string   // the live result (HTML with blocks)
	uses   []string // what it is good for
	tip    string   // optional: good to know
}

// station is one stop of the tour.
type station struct {
	key, en, de string
	off         bool // the feature is not there (a plugin switched off): no station
	build       func(tr trf) stationText
	after       func(id int64) // optional: more for the page (e.g. its properties)
}

// The headings of every station (the rules test checks them).
var stationHeadings = map[string][4]string{
	"en": {"How it is done", "What it looks like", "What it is good for", "Good to know"},
	"de": {"So geht's", "So sieht es aus", "Wofür Sie es nutzen", "Gut zu wissen"},
}

func renderStation(lang string, s stationText) string {
	hd := stationHeadings["en"]
	if lang == "de" {
		hd = stationHeadings["de"]
	}
	var b strings.Builder
	b.WriteString(`<p>` + s.intro + `</p>`)
	b.WriteString(`<h2>` + hd[0] + `</h2><ol>`)
	for _, x := range s.how {
		b.WriteString(`<li>` + x + `</li>`)
	}
	b.WriteString(`</ol><h2>` + hd[1] + `</h2>` + s.result)
	b.WriteString(`<h2>` + hd[2] + `</h2><ul>`)
	for _, x := range s.uses {
		b.WriteString(`<li>` + x + `</li>`)
	}
	b.WriteString(`</ul>`)
	if s.tip != "" {
		b.WriteString(`<h2>` + hd[3] + `</h2>` + macro("panel", map[string]string{"type": "tip", "title": map[bool]string{true: "Tipp", false: "Tip"}[lang == "de"]}, `<p>`+s.tip+`</p>`))
	}
	return b.String()
}

// buildTour creates the stations in their order; the first one is the
// page of the category (the overview of all stations).
func buildTour(d *demoRun, r *demoRefs) {
	r.station = map[string]int64{}
	list := tourStations(d, r)
	n := 0
	for _, s := range list {
		if d.err != nil {
			return
		}
		if s.off {
			d.skipped = append(d.skipped, "station "+s.key)
			continue
		}
		num := strconv.Itoa(n) + ". "
		if n == 0 {
			num = ""
		}
		bodies := map[string]any{}
		for _, l := range d.langs {
			bodies[l] = htmlBodyOf(renderStation(l, s.build(trFor(l))))
		}
		id := d.element(r.tour, "core.html", d.tr(num+s.en, num+s.de), bodies, map[string]any{"position": n})
		r.station[s.key] = id
		r.stations = append(r.stations, id)
		if s.after != nil && id != 0 {
			s.after(id)
		}
		n++
	}
	if len(r.stations) > 0 {
		d.call(http.MethodPut, fmt.Sprintf("/api/v1/categories/%d", r.tour), map[string]any{"default_element_id": r.stations[0]}, nil)
	}
	d.display(r.tour, map[string]any{"view": "handbook"})
}

// hasType reports whether an element type is active (its plugin is on).
func (d *demoRun) hasType(id string) bool {
	for _, t := range d.s.app.Content.Types() {
		if t.ID == id {
			return true
		}
	}
	return false
}

func tourStations(d *demoRun, r *demoRefs) []station {
	link := func(href, text string) string { return `<a href="` + href + `">` + text + `</a>` }
	company := func(tr trf, cat int64, en, de string) string {
		return link(cURL(cat), tr(en, de))
	}
	opening := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	return []station{
		{key: "welcome", en: "Welcome to the tour", de: "Willkommen zum Rundgang", build: func(tr trf) stationText {
			return stationText{
				intro: tr("This tour shows the features of Enterprise Wiki one by one. Every station is one page. It says what a feature is and how it is done. You see the result right here, live, and what it is good for.",
					"Dieser Rundgang zeigt die Funktionen des Enterprise Wiki eine nach der anderen. Jede Station ist eine Seite. Sie sagt, was eine Funktion ist und wie sie geht. Sie sehen das Ergebnis direkt hier, live, und wofür es gut ist."),
				how: []string{
					tr("Read a station from top to bottom.", "Lesen Sie eine Station von oben nach unten."),
					tr("Go on with <em>Next</em> at the end of the page, or choose a station in the list on the side.", "Weiter geht es mit <em>Weiter</em> am Ende der Seite, oder Sie wählen eine Station in der Liste an der Seite."),
					tr("Try it yourself: switch on <em>Edit</em> at the top and change the page. Everything here may be changed.", "Probieren Sie selbst: Schalten Sie oben <em>Bearbeiten</em> ein und ändern Sie die Seite. Alles hier darf verändert werden."),
				},
				result: `<p>` + tr("All stations of the tour:", "Alle Stationen des Rundgangs:") + `</p>` + macro("children", map[string]string{"category": itoa(r.tour), "show": "pages"}, "") +
					`<p>` + tr("You see the features in use in ", "Im Einsatz sehen Sie die Funktionen in der ") + company(tr, r.company, "the example company "+companyEN, "Beispiel-Firma "+companyDE) + `.</p>`,
				uses: []string{
					tr("Get to know the wiki before you decide.", "Das Wiki kennenlernen, bevor Sie sich entscheiden."),
					tr("Show colleagues what is possible.", "Kolleginnen und Kollegen zeigen, was möglich ist."),
					tr("Copy a block you like into your own pages.", "Einen Baustein, der Ihnen gefällt, in die eigenen Seiten übernehmen."),
				},
				tip: tr("Under <em>Administration → Demo</em> the whole demo disappears again with one click – also what you added inside it.",
					"Unter <em>Administration → Demo</em> verschwindet die ganze Demo mit einem Klick wieder – auch das, was Sie darin ergänzt haben."),
			}
		}},
		{key: "write", en: "Writing pages", de: "Seiten schreiben", build: func(tr trf) stationText {
			return stationText{
				intro: tr("A page is written in a visual editor or in Markdown – both store the same text. Every save is a version you can compare and restore.",
					"Eine Seite schreiben Sie im visuellen Editor oder in Markdown – beides speichert denselben Text. Jedes Speichern ist eine Version, die Sie vergleichen und wiederherstellen können."),
				how: []string{
					tr("Switch on <em>Edit</em> at the top and choose <em>New page</em>.", "Schalten Sie oben <em>Bearbeiten</em> ein und wählen Sie <em>Neue Seite</em>."),
					tr("Write. The button above the text switches between the visual editor and Markdown.", "Schreiben Sie. Der Knopf über dem Text wechselt zwischen visuellem Editor und Markdown."),
					tr("Press <em>Publish</em>. Later changes appear under <em>History</em>.", "Drücken Sie <em>Veröffentlichen</em>. Spätere Änderungen stehen unter <em>Verlauf</em>."),
				},
				result: `<table><thead><tr><th>` + tr("Feature", "Funktion") + `</th><th>Status</th></tr></thead><tbody><tr><td>` + tr("Tables", "Tabellen") + `</td><td>✓</td></tr><tr><td>` + tr("Code", "Code") + `</td><td>✓</td></tr></tbody></table>` +
					`<pre><code class="language-go">package main

import "fmt"

func main() {
	fmt.Println("Hello, wiki!")
}</code></pre>` +
					`<ul data-type="taskList"><li data-checked="true">` + tr("Task done", "Aufgabe erledigt") + `</li><li data-checked="false">` + tr("Task open", "Aufgabe offen") + `</li></ul>` +
					`<blockquote><p>` + tr("A quote.", "Ein Zitat.") + `</p></blockquote>`,
				uses: []string{
					tr("Minutes, guides and notes that many people keep up to date.", "Protokolle, Anleitungen und Notizen, die viele aktuell halten."),
					tr("Texts with a history: who changed what and when.", "Texte mit Geschichte: wer hat wann was geändert."),
				},
				tip: tr("Several people can edit the same page at the same time; you see their cursors.", "Mehrere Personen können dieselbe Seite gleichzeitig bearbeiten; Sie sehen ihre Cursor."),
			}
		}},
		{key: "structure", en: "Structuring text", de: "Text gliedern", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Long pages stay clear with a table of contents, boxes, labels, columns, tabs and sections that open on a click.",
					"Lange Seiten bleiben übersichtlich mit Inhaltsverzeichnis, Hinweiskästen, Status-Labels, Spalten, Reitern und Bereichen, die sich auf Klick öffnen."),
				how: []string{
					tr("In the editor choose <em>Insert block</em>.", "Wählen Sie im Editor <em>Baustein einfügen</em>."),
					tr("Search for the block, for example “Tabs”, and insert it.", "Suchen Sie den Baustein, zum Beispiel „Reiter“, und fügen Sie ihn ein."),
					tr("Fill in the parts; <em>+ Part</em> adds another one.", "Füllen Sie die Teile; <em>+ Teil</em> fügt einen weiteren hinzu."),
				},
				result: macro("toc", map[string]string{"levels": "3"}, "") +
					macro("panel", map[string]string{"type": "info", "title": tr("Note", "Hinweis")}, `<p>`+tr("A box draws attention to something important.", "Ein Kasten lenkt den Blick auf Wichtiges.")+`</p>`) +
					`<p>` + macro("status", map[string]string{"text": tr("DONE", "ERLEDIGT"), "color": "green"}, "") + ` ` + macro("status", map[string]string{"text": tr("IN PROGRESS", "IN ARBEIT"), "color": "yellow"}, "") + ` ` + macro("status", map[string]string{"text": tr("BLOCKED", "BLOCKIERT"), "color": "red"}, "") + `</p>` +
					macro("columns", nil, `<p><strong>`+tr("Left:", "Links:")+`</strong> `+tr("categories organise the content in a tree.", "Kategorien ordnen die Inhalte in einem Baum.")+`</p><hr><p><strong>`+tr("Right:", "Rechts:")+`</strong> `+tr("pages, blogs, calendars and forms live in them.", "Darin liegen Seiten, Blogs, Kalender und Formulare.")+`</p>`) +
					macro("tabs", map[string]string{"label": tr("Opening hours", "Öffnungszeiten")}, `<h3>`+tr("Office", "Büro")+`</h3><p>`+tr("Monday to Friday, 8:00 to 17:00.", "Montag bis Freitag, 8:00 bis 17:00 Uhr.")+`</p><hr><h3>`+tr("Service desk", "Service Desk")+`</h3><p>`+tr("Monday to Saturday, 7:00 to 19:00.", "Montag bis Samstag, 7:00 bis 19:00 Uhr.")+`</p>`) +
					macro("accordion", nil, `<h3>`+tr("What does it cost?", "Was kostet das?")+`</h3><p>`+tr("Ask the reception.", "Fragen Sie am Empfang.")+`</p><hr><h3>`+tr("Where do I park?", "Wo parke ich?")+`</h3><p>`+tr("In the yard.", "Im Hof.")+`</p>`) +
					macro("expand", map[string]string{"title": tr("Show more details", "Mehr Details anzeigen")}, `<p>`+tr("This text appears after a click. Screen readers announce the state.", "Dieser Text erscheint nach einem Klick. Screenreader sagen den Zustand an.")+`</p>`),
				uses: []string{
					tr("Guides with many steps.", "Anleitungen mit vielen Schritten."),
					tr("FAQ-like pages where readers open only what they need.", "Seiten wie eine FAQ, auf denen Lesende nur öffnen, was sie brauchen."),
					tr("Status pages of projects.", "Statusseiten von Projekten."),
				},
			}
		}},
		{key: "pictures", en: "Pictures and galleries", de: "Bilder und Galerien", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Pictures come from the media library. A gallery shows a whole folder; pictures open large on a click.", "Bilder kommen aus der Medienbibliothek. Eine Galerie zeigt einen ganzen Ordner; Bilder öffnen sich groß auf Klick."),
				how: []string{
					tr("Drag a picture into the editor, or choose <em>Insert block → Gallery</em>.", "Ziehen Sie ein Bild in den Editor oder wählen Sie <em>Baustein einfügen → Galerie</em>."),
					tr("Give every picture a short description (alternative text) – screen readers read it.", "Geben Sie jedem Bild eine kurze Beschreibung (Alternativtext) – Screenreader lesen sie vor."),
					tr("For a gallery choose the folder of the media library.", "Für eine Galerie wählen Sie den Ordner der Medienbibliothek."),
				},
				result: r.img(0, tr("Morning in the hills", "Morgen in den Hügeln")) + `<h3>` + tr("Gallery of a folder", "Galerie eines Ordners") + `</h3>` +
					macro("gallery", map[string]string{"category": itoa(r.media)}, "") +
					`<h3>` + tr("Picture with hotspots", "Bild mit Hotspots") + `</h3>` +
					macro("hotspots", map[string]string{"image": "/api/v1/media/" + r.pics[1] + "/view", "alt": tr("Noon over the valley", "Mittag über dem Tal")},
						`<ul><li>`+tr("50/20: The sun at noon.", "50/20: Die Sonne am Mittag.")+`</li><li>`+tr("25/80: The hills of the valley.", "25/80: Die Hügel des Tals.")+`</li></ul>`) +
					`<h3>` + tr("Before and after", "Vorher und nachher") + `</h3>` +
					macro("compare", map[string]string{"before": "/api/v1/media/" + r.pics[0] + "/view", "before_alt": tr("Morning in the hills", "Morgen in den Hügeln"),
						"after": "/api/v1/media/" + r.pics[2] + "/view", "after_alt": tr("Evening light", "Abendlicht"), "before_label": tr("Morning", "Morgen"), "after_label": tr("Evening", "Abend")}, ""),
				uses: []string{
					tr("Photos of an event, a building site or a product.", "Fotos einer Feier, einer Baustelle oder eines Produkts."),
					tr("A picture that explains its parts (hotspots).", "Ein Bild, das seine Teile erklärt (Hotspots)."),
					tr("Before and after a renovation.", "Vorher und nachher eines Umbaus."),
				},
			}
		}},
		{key: "files", en: "Files and the media library", de: "Dateien und Medienbibliothek", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Files live in the media library, each with its own permissions. A PDF can be view-only with the name of the viewer as a watermark; office files open in a connected office program.",
					"Dateien liegen in der Medienbibliothek, jede mit eigenen Rechten. Ein PDF kann nur zur Ansicht freigegeben sein, mit dem Namen der betrachtenden Person als Wasserzeichen; Office-Dateien öffnen sich in einem angebundenen Office-Programm."),
				how: []string{
					tr("Open the media library (<em>Media</em> in the menu of your name or under Administration).", "Öffnen Sie die Medienbibliothek (<em>Medien</em> im Menü Ihres Namens oder unter Administration)."),
					tr("Upload a file and set who may see or download it, and whether it shows a watermark.", "Laden Sie eine Datei hoch. Legen Sie fest, wer sie sehen oder herunterladen darf und ob sie ein Wasserzeichen zeigt."),
					tr("On a page, <em>Insert block → Attachments</em> lists the files of a folder.", "Auf einer Seite listet <em>Baustein einfügen → Anhänge</em> die Dateien eines Ordners."),
				},
				result: macro("attachments", map[string]string{"source": "category", "category": itoa(r.media)}, "") +
					`<ul><li>` + link("/api/v1/media/"+itoa(r.confidential)+"/view", tr("Confidential minutes – view only, with your name as watermark", "Vertrauliches Protokoll – nur ansehen, mit Ihrem Namen als Wasserzeichen")) + `</li><li>` +
					link("/media?open="+itoa(r.letter), tr("Letter template – opens in the office program if one is set up", "Briefvorlage – öffnet sich im Office-Programm, wenn eines eingerichtet ist")) + `</li></ul>` +
					`<p>` + tr("In use: ", "Im Einsatz: ") + company(tr, r.docs, "the documents of "+companyEN, "die Dokumente der "+companyDE) + `.</p>`,
				uses: []string{
					tr("Guidelines and forms everybody needs.", "Richtlinien und Vorlagen, die alle brauchen."),
					tr("Confidential minutes that may be read but not passed on.", "Vertrauliche Protokolle, die gelesen, aber nicht weitergegeben werden sollen."),
					tr("Letters and spreadsheets edited together in the office program.", "Briefe und Tabellen, gemeinsam im Office-Programm bearbeitet."),
				},
				tip: tr("The office program (for example Collabora, OnlyOffice or Microsoft 365) is set up by the administration; without one the file opens in the browser.",
					"Das Office-Programm (zum Beispiel Collabora, OnlyOffice oder Microsoft 365) richtet die Administration ein; ohne eines öffnet sich die Datei im Browser."),
			}
		}},
		{key: "website", en: "Website building blocks", de: "Website-Bausteine", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Hero banners, sections with a background, cards and buttons turn a page into a website – without any programming.",
					"Hero-Banner, Abschnitte mit Hintergrund, Karten und Buttons machen aus einer Seite eine Website – ganz ohne Programmierung."),
				how: []string{
					tr("<em>Insert block → Hero banner</em>: title, subtitle, button and its link.", "<em>Baustein einfügen → Hero-Banner</em>: Titel, Untertitel, Schaltfläche und ihr Link."),
					tr("Add <em>Cards</em>; a horizontal line separates the cards.", "Fügen Sie <em>Karten</em> hinzu; eine Trennlinie trennt die Karten."),
					tr("Every block has <em>options</em>: background, border, spacing, and on which devices it appears.", "Jeder Baustein hat <em>Optionen</em>: Hintergrund, Rahmen, Abstand und auf welchen Geräten er erscheint."),
				},
				result: macro("hero", map[string]string{"title": tr("A hero banner", "Ein Hero-Banner"), "subtitle": tr("For the start of a website", "Für den Anfang einer Website"), "button": tr("To the start page", "Zur Startseite"), "link": "/", "background": "accent"}, "") +
					macro("section", map[string]string{"background": "light", "width": "normal", "align": "center"}, `<p>`+tr("A section with a background and centred text.", "Ein Abschnitt mit Hintergrund und zentriertem Text.")+`</p>`) +
					macro("cards", nil, `<h3>`+tr("Fast", "Schnell")+`</h3><p>`+tr("One single program.", "Ein einziges Programm.")+`</p><hr><h3>`+tr("Secure", "Sicher")+`</h3><p>`+tr("Permissions on every level.", "Rechte auf jeder Ebene.")+`</p><hr><h3>`+tr("Accessible", "Barrierefrei")+`</h3><p>WCAG 2.2 AA.</p>`) +
					macro("cta", map[string]string{"title": tr("A call to action", "Ein Handlungsaufruf"), "button": tr("Overview of the tour", "Übersicht des Rundgangs"), "link": cURL(r.tour)}, `<p>`+tr("A box with a title, text and a button.", "Ein Kasten mit Titel, Text und Schaltfläche.")+`</p>`),
				uses: []string{
					tr("The public website of a company, a school or a club.", "Die öffentliche Website einer Firma, einer Schule oder eines Vereins."),
					tr("Landing pages for a campaign or an event.", "Startseiten für eine Aktion oder eine Veranstaltung."),
					tr("The start page of an intranet – like the one of ", "Die Startseite eines Intranets – wie die der ") + company(tr, r.company, companyEN, companyDE) + `.`,
				},
				tip: tr("The start page of this demo is built from these blocks too, with the template “CMS modern”.", "Auch die Startseite dieser Demo ist aus diesen Bausteinen gebaut, mit dem Template „CMS modern“."),
			}
		}},
		{key: "grid", en: "Grid and icons", de: "Raster und Symbole", build: func(tr trf) stationText {
			box := func(icon, en, de, textEN, textDE string) string {
				return macro("iconbox", map[string]string{"name": icon, "title": tr(en, de), "decorative": "true"}, `<p>`+tr(textEN, textDE)+`</p>`)
			}
			return stationText{
				intro: tr("A grid puts blocks side by side. Desktop, tablet and phone each get their own number of columns. More than 6,000 free icons are ready for it.",
					"Ein Raster stellt Bausteine nebeneinander. Desktop, Tablet und Handy haben je eine eigene Zahl an Spalten. Über 6.000 freie Symbole stehen dafür bereit."),
				how: []string{
					tr("<em>Insert block → Grid</em> and choose the columns per device.", "<em>Baustein einfügen → Raster</em> und die Spalten je Gerät wählen."),
					tr("Put an <em>Icon box</em> into each column; the icon picker has a search.", "In jede Spalte eine <em>Symbolbox</em> setzen; die Symbolauswahl hat eine Suche."),
					tr("For a list with check marks use the <em>Icon list</em>.", "Für eine Liste mit Häkchen nehmen Sie die <em>Symbolliste</em>."),
				},
				result: macro("grid", map[string]string{"desktop": "3", "tablet": "2", "mobile": "1"},
					box("cloud", "Cloud", "Cloud", "Servers that run by themselves.", "Server, die von selbst laufen.")+`<hr>`+box("headset", "Support", "Support", "Help within an hour.", "Hilfe innerhalb einer Stunde.")+`<hr>`+box("heart-handshake", "Consulting", "Beratung", "Advice in person.", "Beratung vor Ort.")) +
					macro("iconlist", map[string]string{"name": "check"}, `<ul><li>`+tr("Three columns on the desktop", "Drei Spalten auf dem Desktop")+`</li><li>`+tr("Two on the tablet", "Zwei auf dem Tablet")+`</li><li>`+tr("One on the phone", "Eine auf dem Handy")+`</li></ul>`),
				uses: []string{
					tr("Quick links on an intranet start page.", "Schnellzugriffe auf einer Intranet-Startseite."),
					tr("The services of a company at a glance.", "Die Leistungen einer Firma auf einen Blick."),
				},
				tip: tr("Make this window narrower: the grid changes from three columns to one.", "Machen Sie dieses Fenster schmaler: Das Raster wechselt von drei Spalten zu einer."),
			}
		}},
		{key: "figures", en: "Figures and charts", de: "Zahlen und Diagramme", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Progress bars, counters, charts from a table and formulas show figures so that everybody understands them.", "Fortschrittsbalken, Zähler, Diagramme aus einer Tabelle und Formeln zeigen Zahlen so, dass alle sie verstehen."),
				how: []string{
					tr("<em>Insert block → Chart</em>, then write the figures into the table of the block.", "<em>Baustein einfügen → Diagramm</em>, dann die Zahlen in die Tabelle des Bausteins schreiben."),
					tr("Choose bars, lines or pie.", "Balken, Linien oder Torte wählen."),
					tr("The wiki draws the chart; the table stays for everybody who cannot see it.", "Das Wiki zeichnet das Diagramm; die Tabelle bleibt für alle, die es nicht sehen können."),
				},
				result: macro("progress", map[string]string{"label": tr("Office move", "Büroumzug"), "value": "65", "color": "success"}, "") +
					macro("counter", map[string]string{"value": "320", "label": tr("Customers", "Kunden"), "suffix": "+"}, "") +
					macro("chart", map[string]string{"title": tr("Tickets per quarter", "Tickets je Quartal"), "type": "bar"},
						`<table><thead><tr><th>`+tr("Quarter", "Quartal")+`</th><th>`+tr("Questions", "Fragen")+`</th><th>`+tr("Faults", "Störungen")+`</th></tr></thead><tbody>`+
							`<tr><td>Q1</td><td>120</td><td>80</td></tr><tr><td>Q2</td><td>150</td><td>95</td></tr><tr><td>Q3</td><td>140</td><td>110</td></tr><tr><td>Q4</td><td>180</td><td>130</td></tr></tbody></table>`) +
					`<p>` + tr("The sum of the numbers from 1 to n:", "Die Summe der Zahlen von 1 bis n:") + `</p><p><span data-macro="math" data-params='{"tex":"\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}","display":"block"}'></span></p>`,
				uses: []string{
					tr("Key figures of a department.", "Kennzahlen einer Abteilung."),
					tr("The progress of a project.", "Der Fortschritt eines Projekts."),
					tr("Teaching material with formulas.", "Lehrmaterial mit Formeln."),
				},
			}
		}},
		{key: "more", en: "Timeline, slider, dialog and more", de: "Zeitleiste, Slider, Dialog und mehr", build: func(tr trf) stationText {
			plan := func(name, price, a, b string) string {
				return `<h3>` + name + `</h3><p><strong>` + price + `</strong></p><ul><li>` + a + `</li><li>` + b + `</li></ul>`
			}
			return stationText{
				intro: tr("Small blocks with a big effect: a timeline, a slider, a dialog behind a button, an explanation on a word, a countdown and a price table.",
					"Kleine Bausteine mit großer Wirkung: eine Zeitleiste, ein Slider, ein Dialog hinter einem Knopf, eine Erklärung zu einem Wort, ein Countdown und eine Preistabelle."),
				how: []string{
					tr("<em>Insert block</em> and search for the name, for example “Timeline”.", "<em>Baustein einfügen</em> und nach dem Namen suchen, zum Beispiel „Zeitleiste“."),
					tr("Parts are separated by a horizontal line.", "Teile werden durch eine Trennlinie getrennt."),
				},
				result: macro("timeline", map[string]string{"label": tr("History", "Geschichte"), "direction": "horizontal"},
					`<h3>2008</h3><p>`+tr("The first office opens.", "Das erste Büro öffnet.")+`</p><hr><h3>2015</h3><p>`+tr("The own data centre.", "Das eigene Rechenzentrum.")+`</p><hr><h3>2026</h3><p>`+tr("The wiki replaces the file server.", "Das Wiki ersetzt den Dateiserver.")+`</p>`) +
					macro("slider", map[string]string{"label": tr("First steps", "Erste Schritte")},
						`<p>`+tr("1. Sign in.", "1. Anmelden.")+`</p><hr><p>`+tr("2. Open your area.", "2. Den eigenen Bereich öffnen.")+`</p><hr><p>`+tr("3. Write your first page.", "3. Die erste Seite schreiben.")+`</p>`) +
					macro("dialog", map[string]string{"button": tr("Opening hours", "Öffnungszeiten"), "title": tr("When the reception is open", "Wann der Empfang besetzt ist")},
						`<p>`+tr("Monday to Friday, 8:00 to 17:00.", "Montag bis Freitag, 8:00 bis 17:00 Uhr.")+`</p>`) +
					`<p>` + tr("Questions about the ", "Fragen zum ") + `<span data-macro="tooltip" data-params='{"text":"VPN","tip":"` + tr("Virtual private network: the secure way into the company network from outside.", "Virtuelles privates Netz: der sichere Weg ins Firmennetz von außen.") + `"}'></span>` + tr(" go to the IT desk.", " beantwortet die IT.") + `</p>` +
					macro("countdown", map[string]string{"date": opening, "time": "10:00", "label": tr("Until the opening of the new office", "Bis zur Eröffnung des neuen Büros"), "reached": tr("The new office is open.", "Das neue Büro ist offen.")}, "") +
					macro("pricing", map[string]string{"featured": "2"}, plan("Basic", tr("from 290 € a month", "ab 290 € im Monat"), tr("Help on working days", "Hilfe an Werktagen"), tr("Remote support", "Fernwartung"))+`<hr>`+
						plan("Business", tr("from 890 € a month", "ab 890 € im Monat"), tr("Hotline day and night", "Hotline Tag und Nacht"), tr("On site within four hours", "Vor Ort in vier Stunden"))+`<hr>`+
						plan(tr("Custom", "Individuell"), tr("on request", "auf Anfrage"), tr("Built for you", "Für Sie zusammengestellt"), tr("Personal advice", "Persönliche Beratung"))),
				uses: []string{
					tr("The history of a company or a project.", "Die Geschichte einer Firma oder eines Projekts."),
					tr("Prices on a website.", "Preise auf einer Website."),
					tr("Explaining a technical term without leaving the page.", "Einen Fachbegriff erklären, ohne die Seite zu verlassen."),
				},
			}
		}},
		{key: "people", en: "People", de: "Personen", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Pages show people: a business card, a quote, the members of a group or a whole team page. With @name you mention somebody, and they get a notification.",
					"Seiten zeigen Menschen: eine Visitenkarte, ein Zitat, die Mitglieder einer Gruppe oder eine ganze Teamseite. Mit @Name erwähnen Sie jemanden, und die Person wird benachrichtigt."),
				how: []string{
					tr("<em>Insert block → Person card</em> for a contact.", "<em>Baustein einfügen → Personenkarte</em> für einen Kontakt."),
					tr("<em>People of a group</em> lists the members of a group from their profiles.", "<em>Personen einer Gruppe</em> listet die Mitglieder einer Gruppe aus ihren Profilen."),
					tr("Type @ and the first letters of a name to mention somebody.", "Tippen Sie @ und die ersten Buchstaben eines Namens, um jemanden zu erwähnen."),
				},
				result: macro("personcard", map[string]string{"name": "Ben Huber", "role": tr("IT administrator", "IT-Administrator"), "email": "it@example.com"}, `<p>`+tr("Answers questions about the wiki.", "Beantwortet Fragen zum Wiki.")+`</p>`) +
					macro("testimonial", map[string]string{"name": "Anna Berger", "role": tr("Managing director", "Geschäftsführerin")},
						`<p>`+tr("Since our answers are in the wiki, new colleagues find their way in a few days.", "Seit unsere Antworten im Wiki stehen, finden sich neue Kolleginnen und Kollegen in wenigen Tagen zurecht.")+`</p>`) +
					macro("people", map[string]string{"group": itoa(r.editors), "layout": "list", "details": "short"}, "") +
					`<p>` + tr("A mention: ", "Eine Erwähnung: ") + `@` + r.handles["anna"] + `</p>` +
					`<p>` + tr("The team page of the example company: ", "Die Teamseite der Beispiel-Firma: ") + company(tr, r.teams, "Teams", "Teams") + `.</p>`,
				uses: []string{
					tr("Contacts on every page of a department.", "Ansprechpersonen auf jeder Seite einer Abteilung."),
					tr("A team page that keeps itself up to date.", "Eine Teamseite, die sich selbst aktuell hält."),
					tr("Asking a colleague something right in the text.", "Eine Kollegin direkt im Text etwas fragen."),
				},
				tip: tr("Names are buttons for people who may open the person. The menu offers <em>Open profile</em> and <em>Send e-mail</em>, if the address may be shown. It is on every name, for example under a page, in its history and in comments. The block People of a group shows members only to those who may list the group. Guests see nobody.",
					"Namen sind Knöpfe für alle, die die Person öffnen dürfen. Das Menü bietet <em>Profil öffnen</em> und <em>E-Mail senden</em>, wenn die Adresse gezeigt werden darf. Es steht an jedem Namen, zum Beispiel unter einer Seite, im Verlauf und in Kommentaren. Der Baustein Personen einer Gruppe zeigt Mitglieder nur denen, die die Gruppe auflisten dürfen. Gäste sehen niemanden."),
			}
		}},
		{key: "display", en: "Showing a category", de: "Kategorien darstellen", build: func(tr trf) stationText {
			ex := func(cat int64, en, de, textEN, textDE string) string {
				return `<li>` + link(cURL(cat), tr(en, de)) + ` – ` + tr(textEN, textDE) + `</li>`
			}
			return stationText{
				intro: tr("A category shows its pages the way that suits them: as a list, cards, tiles, a tree, a table, a handbook or all on one page.",
					"Eine Kategorie zeigt ihre Seiten so, wie es zu ihnen passt: als Liste, Karten, Kacheln, Baum, Tabelle, Handbuch oder alles auf einer Seite."),
				how: []string{
					tr("Switch on <em>Edit</em> and open the category.", "Schalten Sie <em>Bearbeiten</em> ein und öffnen Sie die Kategorie."),
					tr("<em>Category settings → Display and content</em>: choose the display.", "<em>Kategorie-Einstellungen → Darstellung und Inhalte</em>: die Darstellung wählen."),
					tr("Under <em>Placement</em> a page of the category can stand beside the others.", "Unter <em>Platzierung</em> kann eine Seite der Kategorie neben den anderen stehen."),
				},
				result: `<p>` + tr("Examples in the company:", "Beispiele in der Firma:") + `</p><ul>` +
					ex(r.handbook, "Handbook", "Handbuch", "chapters in the side column, the contact beside them.", "Kapitel in der Seitenspalte, der Kontakt daneben.") +
					ex(r.onboarding, "Onboarding", "Einarbeitung", "pages as cards with the beginning of the text.", "Seiten als Karten mit dem Anfang des Textes.") +
					ex(r.rooms, "Rooms", "Räume", "a sortable table with the seats as a column.", "eine sortierbare Tabelle mit den Plätzen als Spalte.") +
					ex(r.travel, "How to find us", "Anfahrt", "short pages one below the other.", "kurze Seiten untereinander.") + `</ul>` +
					`<p>` + tr("This tour itself is a handbook: all stations are in the side column.", "Dieser Rundgang selbst ist ein Handbuch: Alle Stationen stehen in der Seitenspalte.") + `</p>`,
				uses: []string{
					tr("A manual read chapter by chapter.", "Ein Handbuch, Kapitel für Kapitel gelesen."),
					tr("Rooms, devices or contracts with their data as a table.", "Räume, Geräte oder Verträge mit ihren Daten als Tabelle."),
					tr("Short information without extra clicks.", "Kurze Informationen ohne zusätzliche Klicks."),
				},
			}
		}},
		{key: "wikiblocks", en: "Wiki building blocks", de: "Wiki-Bausteine", build: func(tr trf) stationText {
			nt := ""
			if r.minutesTpl != 0 {
				nt = macro("newpage", map[string]string{"template": itoa(r.minutesTpl), "text": tr("New meeting minutes", "Neues Besprechungsprotokoll")}, "")
			}
			return stationText{
				intro: tr("Blocks that connect pages. Include a part of another page, jump to a place or show the page tree. Search in one area, create a page from a template or draw a diagram from text.",
					"Bausteine, die Seiten verbinden. Einen Teil einer anderen Seite einbinden, zu einer Stelle springen oder den Seitenbaum zeigen. In einem Bereich suchen, eine Seite aus einer Vorlage anlegen oder ein Diagramm aus Text zeichnen."),
				how: []string{
					tr("Mark a part of a page as <em>Excerpt</em>.", "Markieren Sie einen Teil einer Seite als <em>Auszug</em>."),
					tr("On another page: <em>Insert block → Include page</em>, choose the page and “only the excerpt”.", "Auf einer anderen Seite: <em>Baustein einfügen → Seite einbinden</em>, die Seite wählen und „nur den Auszug“."),
					tr("Change the original – every page that includes it shows the new text.", "Ändern Sie das Original – jede Seite, die es einbindet, zeigt den neuen Text."),
				},
				result: `<p>` + tr("Jump to the ", "Zum ") + `<a href="#tree">` + tr("page tree", "Seitenbaum") + `</a>` + tr(". The next review is on ", " springen. Die nächste Durchsicht ist am ") +
					`<span data-macro="date" data-params='{"value":"2026-12-15","format":"weekday"}'></span>.</p>` +
					`<h3>` + tr("Included from the house rules", "Aus der Hausordnung eingebunden") + `</h3>` + macro("include", map[string]string{"page": itoa(r.houseRules), "excerpt": "true"}, "") +
					`<h3>` + tr("Search in the handbook", "Im Handbuch suchen") + `</h3>` + macro("searchbox", map[string]string{"category": itoa(r.handbook), "label": tr("Search in the handbook", "Im Handbuch suchen")}, "") +
					nt +
					`<h3>` + tr("Pages of the company", "Seiten der Firma") + ` <span data-macro="anchor" data-params='{"name":"tree"}'></span></h3>` + macro("pagetree", map[string]string{"category": itoa(r.company), "depth": "1"}, "") +
					`<h3>` + tr("A diagram from text", "Ein Diagramm aus Text") + `</h3><pre><code class="language-mermaid">flowchart LR
  accTitle: ` + tr("How a holiday request is decided", "Wie ein Urlaubsantrag entschieden wird") + `
  A[` + tr("Request", "Antrag") + `] --&gt; B{` + tr("Approved?", "Genehmigt?") + `}
  B --&gt;|` + tr("yes", "ja") + `| C[` + tr("Calendar", "Kalender") + `]
  B --&gt;|` + tr("no", "nein") + `| D[` + tr("Reply with the reason", "Antwort mit Begründung") + `]</code></pre>`,
				uses: []string{
					tr("One text in many places – changed once.", "Ein Text an vielen Stellen – einmal geändert."),
					tr("Minutes that always look the same (page template).", "Protokolle, die immer gleich aussehen (Seitenvorlage)."),
					tr("Processes drawn as a diagram that stays editable.", "Abläufe als Diagramm, das bearbeitbar bleibt."),
				},
			}
		}},
		{key: "news", en: "Blog and news", de: "Blog und Neuigkeiten", off: !d.hasType("content.blog"), build: func(tr trf) stationText {
			src := "category:" + itoa(r.news)
			return stationText{
				intro: tr("A blog is a category whose pages are the posts. The block News shows the newest posts anywhere – from one or several categories.",
					"Ein Blog ist eine Kategorie, deren Seiten die Beiträge sind. Der Baustein Neuigkeiten zeigt die neuesten Beiträge überall – aus einer oder mehreren Kategorien."),
				how: []string{
					tr("Create an element <em>Blog</em> in a category.", "Legen Sie in einer Kategorie ein Element <em>Blog</em> an."),
					tr("Every new page of the category is a post.", "Jede neue Seite der Kategorie ist ein Beitrag."),
					tr("On another page: <em>Insert block → News</em> and choose the sources.", "Auf einer anderen Seite: <em>Baustein einfügen → Neuigkeiten</em> und die Quellen wählen."),
				},
				result: macro("news", map[string]string{"source": src, "layout": "cards", "limit": "3"}, "") +
					`<h3>` + tr("As a list, page by page", "Als Liste, seitenweise") + `</h3>` +
					macro("news", map[string]string{"source": src + "; category:" + itoa(r.faq) + "+sub; -type:content.faq; -type:content.blog", "layout": "list", "limit": "30", "paginate": "yes", "per_page": "3", "title": tr("News and questions", "Neuigkeiten und Fragen")}, "") +
					`<h3>` + tr("Only what is about holidays", "Nur was mit Urlaub zu tun hat") + `</h3>` +
					macro("blog", map[string]string{"source": tr("search:holiday", "search:Urlaub") + "; -category:" + itoa(r.tour), "layout": "compact", "limit": "5"}, "") +
					`<p>` + tr("The whole blog: ", "Der ganze Blog: ") + company(tr, r.news, "News of "+companyEN, "Neuigkeiten der "+companyDE) + `.</p>`,
				uses: []string{
					tr("News of the management on the intranet.", "Nachrichten der Geschäftsleitung im Intranet."),
					tr("The news of a department, collected on the start page.", "Die Neuigkeiten einer Abteilung, gesammelt auf der Startseite."),
					tr("A public blog on the website.", "Ein öffentlicher Blog auf der Website."),
				},
				tip: tr("Readers who watch the category hear about every new post; the blog also has a feed.", "Wer die Kategorie beobachtet, erfährt von jedem neuen Beitrag; der Blog hat außerdem einen Feed."),
			}
		}},
		{key: "faq", en: "Questions and answers", de: "Fragen und Antworten", off: !d.hasType("content.faq"), build: func(tr trf) stationText {
			return stationText{
				intro: tr("A FAQ collects questions and answers: subcategories are the sections, pages are the questions. A search field filters while you type.",
					"Eine FAQ sammelt Fragen und Antworten: Unterkategorien sind die Abschnitte, Seiten sind die Fragen. Ein Suchfeld filtert beim Tippen."),
				how: []string{
					tr("Create a category with one subcategory per topic.", "Legen Sie eine Kategorie mit einer Unterkategorie je Thema an."),
					tr("Write every question as a page; the title is the question.", "Schreiben Sie jede Frage als Seite; der Titel ist die Frage."),
					tr("Add an element <em>FAQ</em> – or the block <em>FAQ</em> on any page.", "Fügen Sie ein Element <em>FAQ</em> hinzu – oder den Baustein <em>FAQ</em> auf einer beliebigen Seite."),
				},
				result: macro("faq", map[string]string{"source": "category:" + itoa(r.faq) + "+sub", "open": "no", "numbering": "yes"}, "") +
					`<p>` + tr("The knowledge base with search: ", "Die Wissensdatenbank mit Suche: ") + company(tr, r.faq, "Knowledge base of "+companyEN, "Wissensdatenbank der "+companyDE) + `.</p>`,
				uses: []string{
					tr("The IT helpdesk answers once, for everybody.", "Die IT beantwortet eine Frage einmal, für alle."),
					tr("Questions of customers on the website.", "Fragen der Kundschaft auf der Website."),
					tr("Rules for students at a university.", "Regeln für Studierende an einer Hochschule."),
				},
			}
		}},
		{key: "events", en: "Events and calendars", de: "Termine und Kalender", off: !d.hasType("content.events"), build: func(tr trf) stationText {
			return stationText{
				intro: tr("A calendar shows events by month, week or as a list. Events can repeat, have a number of places, a registration and a waiting list.",
					"Ein Kalender zeigt Termine als Monat, Woche oder Liste. Termine können sich wiederholen, eine Zahl an Plätzen, eine Anmeldung und eine Warteliste haben."),
				how: []string{
					tr("Create an element <em>Calendar</em> in a category.", "Legen Sie in einer Kategorie ein Element <em>Kalender</em> an."),
					tr("<em>New event</em>: title, time, place; switch on the registration if you want.", "<em>Neuer Termin</em>: Titel, Zeit, Ort; bei Bedarf die Anmeldung einschalten."),
					tr("A calendar can show further calendars and the dates in pages too.", "Ein Kalender kann weitere Kalender und die Termine in Seiten mit anzeigen."),
				},
				result: macro("events", map[string]string{"limit": "5", "days": "60"}, "") +
					`<p>` + tr("The calendar with registration: ", "Der Kalender mit Anmeldung: ") + company(tr, r.events, "Events of "+companyEN, "Termine der "+companyDE) + `.</p>`,
				uses: []string{
					tr("Courses with a limited number of places.", "Kurse mit begrenzter Zahl an Plätzen."),
					tr("Lectures and exams at a university.", "Vorlesungen und Prüfungen an einer Hochschule."),
					tr("The celebrations of a company.", "Die Feiern einer Firma."),
				},
				tip: tr("Every calendar can be subscribed to in your own calendar program (iCal).", "Jeden Kalender können Sie in Ihrem eigenen Kalenderprogramm abonnieren (iCal)."),
			}
		}},
		{key: "tasks", en: "Tasks and dates in the text", de: "Aufgaben und Termine im Text", build: func(tr trf) stationText {
			return stationText{
				intro: tr("A task item that names somebody with @name or has a date is a task. It appears under My tasks, on the dashboard and in reports – the page stays where it is.",
					"Ein Aufgabenpunkt, der jemanden mit @Name nennt oder ein Datum hat, ist eine Aufgabe. Er erscheint unter Meine Aufgaben, im Dashboard und in Berichten – die Seite bleibt, wo sie ist."),
				how: []string{
					tr("Write a task list in a page (in Markdown: <code>- [ ] </code>).", "Schreiben Sie eine Aufgabenliste in eine Seite (in Markdown: <code>- [ ] </code>)."),
					tr("Add @name and a date to an item.", "Ergänzen Sie einen Punkt um @Name und ein Datum."),
					tr("The people tick their tasks under <em>My tasks</em>.", "Die Personen haken ihre Aufgaben unter <em>Meine Aufgaben</em> ab."),
				},
				result: `<p>` + tr("The open tasks of the teams of the company:", "Die offenen Aufgaben der Teams der Firma:") + `</p>` + macro("tasks", map[string]string{"category": itoa(r.teams)}, "") +
					macro("dates", map[string]string{"category": itoa(r.teams)}, "") +
					`<p>` + tr("The page with the tasks is “Office move” in ", "Die Seite mit den Aufgaben ist „Büroumzug“ unter ") + company(tr, r.teams, "Teams", "Teams") + `.</p>`,
				uses: []string{
					tr("To-dos from minutes that nobody forgets.", "Aufgaben aus Protokollen, die niemand vergisst."),
					tr("Plans of a move or an event.", "Pläne für einen Umzug oder eine Veranstaltung."),
				},
			}
		}},
		{key: "decisions", en: "Decisions", de: "Entscheidungen", build: func(tr trf) stationText {
			return stationText{
				intro: tr("A decision is a block in the text with status, day and who is responsible. A report collects the decisions of a whole area.",
					"Eine Entscheidung ist ein Baustein im Text mit Status, Datum und Verantwortlichen. Ein Bericht sammelt die Entscheidungen eines ganzen Bereichs."),
				how: []string{
					tr("<em>Insert block → Decision</em>: what was decided, status and who.", "<em>Baustein einfügen → Entscheidung</em>: was entschieden wurde, Status und wer."),
					tr("Anywhere: <em>Insert block → Decisions</em> lists the decisions of a category.", "Irgendwo: <em>Baustein einfügen → Entscheidungen</em> listet die Entscheidungen einer Kategorie."),
				},
				result: macro("decisions", map[string]string{"category": itoa(r.teams)}, "") +
					`<p>` + tr("The log of the company is the page “Decision log” in ", "Das Protokoll der Firma ist die Seite „Entscheidungsprotokoll“ unter ") + company(tr, r.teams, "Teams", "Teams") + `.</p>`,
				uses: []string{
					tr("Decisions of the management that are easy to find later.", "Beschlüsse der Leitung, die später leicht zu finden sind."),
					tr("Architecture decisions of an IT team.", "Entscheidungen über die Technik eines IT-Teams."),
				},
				tip: tr("The report shows only pages the reader may see.", "Der Bericht zeigt nur Seiten, die die lesende Person sehen darf."),
			}
		}},
		{key: "properties", en: "Properties and reports", de: "Eigenschaften und Berichte", off: r.propdb == 0 || !d.hasType("content.propdb"), build: func(tr trf) stationText {
			return stationText{
				intro: tr("Pages carry properties – status, owner, date, budget and many more. Reports show them as a table, board, chart, calendar, timeline or map.",
					"Seiten tragen Eigenschaften – Status, Verantwortliche, Datum, Budget und viele mehr. Berichte zeigen sie als Tabelle, Board, Diagramm, Kalender, Zeitleiste oder Karte."),
				how: []string{
					tr("Define the properties of a category (for example Status and Budget).", "Legen Sie die Eigenschaften einer Kategorie fest (zum Beispiel Status und Budget)."),
					tr("Fill them in when you edit a page.", "Füllen Sie sie beim Bearbeiten einer Seite aus."),
					tr("<em>Insert block → Properties</em>: choose source, columns and view.", "<em>Baustein einfügen → Eigenschaften</em>: Quelle, Spalten und Ansicht wählen."),
				},
				result: `<p>` + tr("This page has properties of its own (the table above the content). The projects of the company as a board:", "Diese Seite hat eigene Eigenschaften (die Tabelle über dem Inhalt). Die Projekte der Firma als Board:") + `</p>` +
					macro("properties", map[string]string{"source": "category:" + itoa(r.projects), "conditions": "tags has project", "view": "kanban", "group": "status", "columns": "priority,owner,due"}, "") +
					`<p>` + link(eURL(r.propdb), tr("Project database", "Projekt-Datenbank")) + ` · ` + link(cURL(r.projects), tr("All projects and reports", "Alle Projekte und Berichte")) + `</p>`,
				uses: []string{
					tr("A list of projects with status and budget.", "Eine Projektliste mit Status und Budget."),
					tr("Contracts with the day they end.", "Verträge mit ihrem Enddatum."),
					tr("Pages that need a review at a certain date.", "Seiten, die zu einem Datum geprüft werden müssen."),
				},
			}
		}, after: func(id int64) {
			d.optional("properties", func() {
				d.call(http.MethodPut, fmt.Sprintf("/api/v1/x/core.properties/elements/%d", id), map[string]any{
					"values": map[string]any{"responsible": "Anna Berger", "state": "Aktiv", "review": time.Now().AddDate(0, 6, 0).Format("2006-01-02")},
					"free": map[string]any{
						"responsible": map[string]string{"type": "text", "label": d.tr("Responsible", "Verantwortlich")[d.lang()]},
						"state":       map[string]string{"type": "status", "label": "Status"},
						"review":      map[string]string{"type": "date", "label": d.tr("Review date", "Prüfdatum")[d.lang()]},
					}}, nil)
			})
		}},
		{key: "forms", en: "Forms", de: "Formulare", off: r.ideas == 0 || !d.hasType("content.forms"), build: func(tr trf) stationText {
			return stationText{
				intro: tr("Forms collect answers. They can have steps, computed fields and fields that appear after an answer. Files and a signature are possible, and the results can be public.",
					"Formulare sammeln Antworten. Sie können Schritte, berechnete Felder und Felder haben, die nach einer Antwort erscheinen. Dateien und eine Unterschrift sind möglich, und die Ergebnisse können öffentlich sein."),
				how: []string{
					tr("Create an element <em>Form</em> and add fields.", "Legen Sie ein Element <em>Formular</em> an und fügen Sie Felder hinzu."),
					tr("Choose where the answers go: stored, by e-mail, into a workflow or a database.", "Wählen Sie, wohin die Antworten gehen: gespeichert, per E-Mail, in einen Workflow oder eine Datenbank."),
					tr("<em>Insert block → Form results</em> shows how people answered.", "<em>Baustein einfügen → Formularergebnisse</em> zeigt, wie geantwortet wurde."),
				},
				result: macro("form-results", map[string]string{"form": itoa(r.ideas), "fields": "topic"}, "") +
					`<p>` + link(eURL(r.ideas), tr("Fill in the idea box of the company", "Die Ideenbox der Firma ausfüllen")) + tr(" – the results above count your answer at once.", " – die Ergebnisse oben zählen Ihre Antwort sofort mit.") + `</p>`,
				uses: []string{
					tr("Requests and orders inside a company.", "Anträge und Bestellungen in einer Firma."),
					tr("Registrations for a course or an event.", "Anmeldungen zu einem Kurs oder einer Veranstaltung."),
					tr("Surveys with public results.", "Umfragen mit öffentlichen Ergebnissen."),
				},
				tip: tr("Forms for guests are protected against spam without a puzzle for the reader.", "Formulare für Gäste sind gegen Spam geschützt, ohne Rätsel für die Lesenden."),
			}
		}},
		{key: "workflows", en: "Workflows", de: "Abläufe (Workflows)", off: !d.hasType("workflow.process"), build: func(tr trf) stationText {
			return stationText{
				intro: tr("A workflow leads a request through its steps: who decides, what happens then, who is told. Without programming.",
					"Ein Workflow führt einen Antrag durch seine Schritte: wer entscheidet, was dann passiert, wer Bescheid bekommt. Ohne Programmierung."),
				how: []string{
					tr("Create an element <em>Workflow</em>: the fields of the start form and the steps.", "Legen Sie ein Element <em>Workflow</em> an: die Felder des Startformulars und die Schritte."),
					tr("Steps are tasks for people or groups, approvals, conditions, messages, calendar entries, new pages and more.", "Schritte sind Aufgaben für Personen oder Gruppen, Freigaben, Bedingungen, Nachrichten, Kalendereinträge, neue Seiten und mehr."),
					tr("Try it with <em>Start test run</em>; who decides finds the task under <em>My tasks</em>.", "Probieren Sie es mit <em>Testlauf starten</em>; wer entscheidet, findet die Aufgabe unter <em>Meine Aufgaben</em>."),
				},
				result: `<pre><code class="language-mermaid">flowchart LR
  accTitle: ` + tr("The holiday request of the example company", "Der Urlaubsantrag der Beispiel-Firma") + `
  A[` + tr("Start form", "Startformular") + `] --&gt; B{` + tr("Team lead", "Teamleitung") + `}
  B --&gt;|` + tr("approve", "genehmigen") + `| C[` + tr("Calendar", "Kalender") + `] --&gt; D[` + tr("Message", "Nachricht") + `]
  B --&gt;|` + tr("reject", "ablehnen") + `| E[` + tr("Message with the reason", "Nachricht mit Begründung") + `]</code></pre>` +
					`<p>` + tr("Try it: start a ", "Probieren Sie es: Starten Sie einen ") + link(cURL(r.forms), tr("holiday request", "Urlaubsantrag")) +
					tr(". In this demo you belong to the team leads. So you decide it yourself under My tasks.", ". In dieser Demo gehören Sie zur Teamleitung. Sie entscheiden ihn selbst unter Meine Aufgaben.") + `</p>` +
					`<p>` + tr("Or apply for a job internally: HR and the team lead decide one after the other, and the answer comes as a pop-up in the wiki.", "Oder bewerben Sie sich intern auf eine Stelle: Personal und Teamleitung entscheiden nacheinander, und die Antwort kommt als Pop-up im Wiki.") + `</p>` +
					`<p>` + tr("More workflows: ", "Weitere Abläufe: ") + company(tr, r.forms, "Forms and workflows of "+companyEN, "Formulare und Abläufe der "+companyDE) + `.</p>`,
				uses: []string{
					tr("Holiday, job applications, devices.", "Urlaub, Bewerbungen, Geräte."),
					tr("Contracts that several departments check at the same time.", "Verträge, die mehrere Abteilungen gleichzeitig prüfen."),
					tr("Onboarding of new colleagues, started by itself for every new account.", "Einarbeitung neuer Kolleginnen und Kollegen, von selbst gestartet für jedes neue Konto."),
				},
				tip: tr("Deadlines count working days; when a task waits too long, it is handed over or somebody is told.", "Fristen zählen Arbeitstage; wartet eine Aufgabe zu lange, wird sie übergeben oder jemand informiert."),
			}
		}},
		{key: "notes", en: "Writing together live", de: "Live gemeinsam schreiben", off: !d.hasType("content.notes"), build: func(tr trf) stationText {
			return stationText{
				intro: tr("Several people write shared notes at the same time. Everybody sees the cursors and changes of the others at once.",
					"Gemeinsame Notizen schreiben mehrere Personen gleichzeitig – alle sehen die Cursor und Änderungen der anderen sofort."),
				how: []string{
					tr("Create an element <em>Notes</em>.", "Legen Sie ein Element <em>Notizen</em> an."),
					tr("Send the link to your colleagues.", "Schicken Sie den Link an Ihre Kolleginnen und Kollegen."),
					tr("Write together; there is nothing to save.", "Schreiben Sie gemeinsam; Speichern ist nicht nötig."),
				},
				result: `<p>` + tr("The “Notes of the team round” in ", "Die „Notizen der Teamrunde“ unter ") + company(tr, r.teams, "Teams", "Teams") + tr(": open them in two windows and type in one.", ": Öffnen Sie sie in zwei Fenstern und tippen Sie in einem.") + `</p>`,
				uses: []string{
					tr("Minutes written during the meeting.", "Protokolle, während der Besprechung geschrieben."),
					tr("Collecting ideas in a workshop.", "Ideen in einem Workshop sammeln."),
				},
			}
		}},
		{key: "rights", en: "Permissions and confidentiality", de: "Rechte und Vertraulichkeit", build: func(tr trf) stationText {
			return stationText{
				intro: tr("Permissions are set on categories and inherited downwards. A page can have rights of its own. Classified content is marked on every page and stays out of feeds.",
					"Rechte werden an Kategorien vergeben und nach unten vererbt. Eine Seite kann eigene Rechte haben. Eingestufte Inhalte sind auf jeder Seite gekennzeichnet und erscheinen in keinem Feed."),
				how: []string{
					tr("Switch on <em>Edit</em>, open a category and choose <em>Rights</em>.", "Schalten Sie <em>Bearbeiten</em> ein, öffnen Sie eine Kategorie und wählen Sie <em>Rechte</em>."),
					tr("Give people, groups or roles the right to view, edit or manage.", "Geben Sie Personen, Gruppen oder Rollen das Recht zum Ansehen, Bearbeiten oder Verwalten."),
					tr("Under <em>Category settings</em> choose a classification such as “confidential”.", "Unter <em>Kategorie-Einstellungen</em> wählen Sie eine Einstufung wie „vertraulich“."),
				},
				result: `<ul><li>` + company(tr, r.docs, "Documents", "Dokumente") + ` – ` + tr("classified confidential: see the mark on the page.", "als vertraulich eingestuft: siehe die Kennzeichnung auf der Seite.") + `</li>` +
					`<li>` + tr("In it, “Decisions of the management” has rights of its own: only named people, administrators and managers see it.", "Darin hat „Beschlüsse der Geschäftsleitung“ eigene Rechte: Nur genannte Personen, Administratoren und Verwalter sehen sie.") + `</li>` + `</ul>`,
				uses: []string{
					tr("HR documents only HR may read.", "Personalunterlagen, die nur das Personalwesen lesen darf."),
					tr("A public website and an internal area in one wiki.", "Eine öffentliche Website und ein interner Bereich in einem Wiki."),
					tr("Minutes of the management for the management only.", "Protokolle der Geschäftsleitung nur für die Geschäftsleitung."),
				},
				tip: tr("Every check happens on the server: a page you may not see never reaches your browser – not in the search, lists or feeds either.",
					"Jede Prüfung passiert auf dem Server: Eine Seite, die Sie nicht sehen dürfen, erreicht Ihren Browser nie – auch nicht über die Suche, Listen oder Feeds."),
			}
		}},
		{key: "placement", en: "Menus, placement and carousel", de: "Menüs, Platzierung und Karussell", build: func(tr trf) stationText {
			var notes strings.Builder
			for _, w := range r.where {
				notes.WriteString(map[bool]string{true: w.de, false: w.en}[tr("en", "de") == "de"])
			}
			return stationText{
				intro: tr("Some parts belong to no page: a menu, a contact box, a carousel. The administration places them at a position of the template, and they appear there on many pages.",
					"Manche Teile gehören zu keiner Seite: ein Menü, ein Kontaktkasten, ein Karussell. Die Administration platziert sie an einer Position des Templates, und sie erscheinen dort auf vielen Seiten."),
				how: []string{
					tr("<em>Administration → Placement</em> shows all positions of the template with their blocks.", "<em>Administration → Platzierung</em> zeigt alle Positionen des Templates mit ihren Bausteinen."),
					tr("Drag a block to a position, or choose it with the keyboard.", "Ziehen Sie einen Baustein an eine Position oder wählen Sie ihn mit der Tastatur."),
					tr("A category can place its own blocks: <em>Category settings → Placement</em>.", "Eine Kategorie kann eigene Bausteine platzieren: <em>Kategorie-Einstellungen → Platzierung</em>."),
				},
				result: tr(whereIntroEN, whereIntroDE) + notes.String() + tr(whereEndEN, whereEndDE),
				uses: []string{
					tr("A menu of the most important pages at the top.", "Ein Menü der wichtigsten Seiten oben."),
					tr("A contact box on every page of a department.", "Ein Kontaktkasten auf jeder Seite einer Abteilung."),
					tr("A carousel of news on a start page.", "Ein Karussell mit Neuigkeiten auf einer Startseite."),
				},
			}
		}},
		{key: "design", en: "Look: templates, light and dark", de: "Aussehen: Templates, hell und dunkel", build: func(tr trf) stationText {
			return stationText{
				intro: tr("A template decides the look of the whole wiki or of one category: colours, fonts, header and columns. Every person chooses light, dark or the setting of the device.",
					"Ein Template bestimmt das Aussehen des ganzen Wikis oder einer Kategorie: Farben, Schriften, Kopfbereich und Spalten. Jede Person wählt hell, dunkel oder die Einstellung des Geräts."),
				how: []string{
					tr("<em>Administration → Templates</em>: choose a template or design one in the visual designer.", "<em>Administration → Templates</em>: ein Template wählen oder im visuellen Designer gestalten."),
					tr("A category can have a template of its own (Category settings).", "Eine Kategorie kann ein eigenes Template haben (Kategorie-Einstellungen)."),
					tr("Light or dark: the buttons at the bottom of the page or <em>My profile → Colour scheme</em>.", "Hell oder dunkel: die Knöpfe unten auf der Seite oder <em>Mein Profil → Farbschema</em>."),
				},
				result: macro("columns", nil, `<p><strong>`+tr("Wiki", "Wiki")+`</strong>: `+tr("this tour – navigation on the side, made for reading.", "dieser Rundgang – Navigation an der Seite, gemacht zum Lesen.")+`</p><hr><p><strong>`+tr("CMS modern", "CMS modern")+`</strong>: `+
					tr("the start page of the demo – a website look.", "die Startseite der Demo – im Look einer Website.")+` `+link("/", tr("Open the start page", "Startseite öffnen"))+`</p>`) +
					`<p>` + tr("Switch to dark with the buttons at the bottom of this page – every colour follows.", "Wechseln Sie mit den Knöpfen unten auf dieser Seite zu dunkel – jede Farbe folgt.") + `</p>`,
				uses: []string{
					tr("The colours and logo of your company.", "Die Farben und das Logo Ihrer Firma."),
					tr("An intranet and a public website with different looks.", "Ein Intranet und eine öffentliche Website mit verschiedenem Aussehen."),
				},
				tip: tr("Both schemes are checked for contrast; a too light colour is darkened until text stays readable.", "Beide Schemata werden auf Kontrast geprüft; eine zu helle Farbe wird abgedunkelt, bis Text lesbar bleibt."),
			}
		}},
		{key: "find", en: "Finding and following", de: "Finden und folgen", build: func(tr trf) stationText {
			return stationText{
				intro: tr("The search finds pages, files and people – only what you may see. Tags, favourites, watching, comments and reactions keep you up to date.",
					"Die Suche findet Seiten, Dateien und Personen – nur, was Sie sehen dürfen. Schlagwörter, Favoriten, Beobachten, Kommentare und Reaktionen halten Sie auf dem Laufenden."),
				how: []string{
					tr("Type into the search field at the top; filters narrow the result.", "Tippen Sie oben ins Suchfeld; Filter grenzen das Ergebnis ein."),
					tr("<em>Watch</em> a page or category to hear about changes.", "<em>Beobachten</em> Sie eine Seite oder Kategorie, um von Änderungen zu erfahren."),
					tr("<em>Favourite</em> puts a page into your list in the side column.", "<em>Favorit</em> legt eine Seite in Ihre Liste in der Seitenspalte."),
				},
				result: `<h3>` + tr("Tags of the wiki", "Schlagwörter des Wikis") + `</h3>` + macro("tagcloud", map[string]string{"max": "30"}, "") +
					`<h3>` + tr("Who wrote this page", "Wer an dieser Seite geschrieben hat") + `</h3>` + macro("contributors", nil, "") +
					`<h3>` + tr("Recently changed in the company", "Zuletzt geändert in der Firma") + `</h3>` + macro("recent", map[string]string{"count": "5", "scope": "all"}, ""),
				uses: []string{
					tr("Finding the right form in seconds.", "Das richtige Formular in Sekunden finden."),
					tr("Hearing about a change of a guideline.", "Von der Änderung einer Richtlinie erfahren."),
					tr("Feedback on a page right below it.", "Rückmeldung zu einer Seite direkt darunter."),
				},
			}
		}},
		{key: "languages", en: "Languages and accessibility", de: "Sprachen und Barrierefreiheit", build: func(tr trf) stationText {
			return stationText{
				intro: tr("The interface speaks German and English; every page can exist in several languages. Everything can be used with the keyboard and a screen reader (WCAG 2.2 AA).",
					"Die Oberfläche spricht Deutsch und Englisch; jede Seite kann es in mehreren Sprachen geben. Alles lässt sich mit Tastatur und Screenreader bedienen (WCAG 2.2 AA)."),
				how: []string{
					tr("Choose the language at the top: this page changes its language too.", "Wählen Sie oben die Sprache: Auch diese Seite wechselt ihre Sprache."),
					tr("To translate a page: <em>Translate now</em>; outdated translations are marked.", "Eine Seite übersetzen: <em>Jetzt übersetzen</em>; veraltete Übersetzungen werden markiert."),
					tr("Press Tab: the first stop is the link to the content.", "Drücken Sie die Tabulatortaste: Der erste Halt ist der Link zum Inhalt."),
				},
				result: macro("iconlist", map[string]string{"name": "check"}, `<ul><li>`+tr("Every picture has an alternative text.", "Jedes Bild hat einen Alternativtext.")+`</li><li>`+
					tr("Every control can be reached with the keyboard.", "Jedes Bedienelement ist mit der Tastatur erreichbar.")+`</li><li>`+
					tr("Contrasts are checked in light and dark.", "Kontraste sind hell und dunkel geprüft.")+`</li><li>`+
					tr("This page exists in German and English.", "Diese Seite gibt es auf Deutsch und Englisch.")+`</li></ul>`),
				uses: []string{
					tr("Companies with colleagues in several countries.", "Firmen mit Kolleginnen und Kollegen in mehreren Ländern."),
					tr("Public bodies and universities that must be accessible.", "Behörden und Hochschulen, die barrierefrei sein müssen."),
				},
				tip: tr("Every page of this demo is checked automatically for accessibility, in light and in dark.", "Jede Seite dieser Demo wird automatisch auf Barrierefreiheit geprüft, hell und dunkel."),
			}
		}},
		{key: "done", en: "Done – what next?", de: "Geschafft – wie weiter?", build: func(tr trf) stationText {
			return stationText{
				intro: tr("You have seen every station. Now look at it in use – or start your own space.", "Sie haben alle Stationen gesehen. Jetzt sehen Sie es im Einsatz – oder starten Ihren eigenen Bereich."),
				how: []string{
					tr("Look around the example company.", "Sehen Sie sich in der Beispiel-Firma um."),
					tr("Create a space of your own: <em>All spaces → New space</em>.", "Legen Sie einen eigenen Bereich an: <em>Alle Bereiche → Neuer Bereich</em>."),
					tr("When you are done: <em>Administration → Demo → Remove demo</em>.", "Wenn Sie fertig sind: <em>Administration → Demo → Demo entfernen</em>."),
				},
				result: macro("buttons", map[string]string{"text1": tr("To the example company", "Zur Beispiel-Firma"), "link1": cURL(r.company), "icon1": "building-community",
					"text2": tr("Back to the start", "Zurück zum Anfang"), "link2": cURL(r.tour), "icon2": "arrow-back-up"}, ""),
				uses: []string{
					tr("An intranet like the one of the example company.", "Ein Intranet wie das der Beispiel-Firma."),
					tr("A handbook, a knowledge base or a website.", "Ein Handbuch, eine Wissensdatenbank oder eine Website."),
				},
			}
		}},
	}
}
