# Plugins verwalten

Fast alles im Wiki ist ein Plugin: Elementtypen (z. B. Seiten), Anmeldeverfahren, Menüs, Templates, Admin-Seiten. Plugins werden **zusammen mit dem Kern kompiliert**. Das macht sie schnell und typsicher, und es gibt keine Laufzeit-Abhängigkeiten.

| Aktion | Wirkung | Neubau nötig? |
|---|---|---|
| aktivieren / deaktivieren | sofort (API-Routen, Elementtypen, Oberflächenteile) | nein |
| installieren / aktualisieren | Quellcode wird in den Plugin-Ordner kopiert, das Wiki baut sich neu und startet danach automatisch neu | ja (automatisch) |
| entfernen | Quellcode wird nach `.backup/` im Plugin-Ordner verschoben, Daten bleiben in der Datenbank; Neubau und Neustart automatisch | ja (automatisch) |
| in den Plugin-Ordner kopieren | wird nach etwa 20 Sekunden ohne weitere Änderungen erkannt und gebaut; aktiv nach **Neu starten** (oder automatisch mit `plugins.auto_restart_on_drop: true`) | ja (automatisch) |

Plugin-Ordner: bei der Quellinstallation `plugins/`, im Docker-Full-Image `./data/wiki/plugins` (im Container `/data/plugins`).

## In der Oberfläche

*Administration → Plugins*:

- Liste aller Plugins mit Version, Art, Abhängigkeiten (*Baut auf*) und Status (aktiv / deaktiviert / inaktiv, weil ein Basis-Plugin fehlt / erforderlich),
- **Aktivieren/Deaktivieren**: Ein Plugin, auf dem andere aktive Plugins aufbauen, muss zuletzt deaktiviert werden. Pflicht-Plugins (`auth.local`) lassen sich nicht deaktivieren,
- **Installieren** aus
  - GitHub: `github.com/owner/repo` (optional Tag/Branch),
  - einer beliebigen Git-URL (`https://…`, `git@…`),
  - einer Katalog-ID (siehe unten) oder
  - einem Verzeichnis auf dem Server,
- **Ausstehende Änderungen** zeigt, was nach dem nächsten Neubau aktiv wird,
- nach dem Installieren, Aktualisieren oder Entfernen baut das Wiki im Hintergrund neu und startet danach automatisch neu; die Seite lädt sich selbst neu, sobald das Wiki wieder antwortet. Ein fehlgeschlagener Build lässt die laufende Version unverändert (Protokoll unter *Build-Protokoll*),
- **Jetzt neu bauen** startet einen Build von Hand, **Neu starten** schaltet ohne Abbruch laufender Anfragen auf den neuen Build um (im Cluster ein Knoten nach dem anderen),
- **Nach abgelegten Plugins automatisch neu starten** (`plugins.auto_restart_on_drop`): sonst wartet ein Build aus dem Plugin-Ordner auf **Neu starten**, damit nicht mitten im Kopieren neu gestartet wird.

Bauen setzt eine der folgenden Varianten voraus:

| Variante | Plugins installieren | Gebaut wird … |
|---|---|---|
| Quellinstallation (`scripts/install.sh`) | ja | im Quellbaum, das Programm wird ersetzt |
| Docker **Full-Image** (`ghcr.io/mtsonline/enterprisewiki:full`, `EWIKI_IMAGE_TAG=full`) | ja | in `./data/wiki/build`, Ergebnis in `./data/wiki/bin/ewiki-<Kennung>` |
| eigenes Image (`docker-compose.build.yml`, Ziel `full`) | ja | wie beim Full-Image; Plugins in `plugins/` sind bereits eingebaut |
| Docker **Lite-Image** (`:latest`, Standard) | nein (Hinweis in der Oberfläche) | – nur aktivieren und deaktivieren |

### Docker-Full-Image im Detail

- Das Image enthält zusätzlich den Quellcode des Kerns, Go, Node.js sowie alle Go-Module und npm-Pakete des Kerns. Plugins ohne weitere Abhängigkeiten werden daher auch **ohne Internet** gebaut; braucht ein Plugin weitere Go-Module, werden sie beim Bau geladen.
- Alles Veränderliche liegt im Datenordner und übersteht Updates und neu angelegte Container: `plugins/` (zusätzliche Plugins), `build/cache/` (Build-Cache), `bin/` (gebaute Programme, die letzten drei bleiben erhalten).
- **Beim Start** sucht das Image-Programm ein gebautes Programm passend zu Kernversion, Architektur und Plugin-Quellen und übergibt an dieses. Gibt es keines (erster Start, neues Image), baut es zuerst; das dauert beim ersten Mal einige Minuten, danach dank Cache deutlich kürzer. Währenddessen zeigt das Wiki eine Seite „Das Wiki wird aktualisiert“ (HTTP 503). Schlägt der Bau fehl, startet das Wiki ohne die zusätzlichen Plugins, und *Administration → Plugins* zeigt das Protokoll.
- Ein Plugin mit derselben ID wie ein mitgeliefertes ersetzt dieses (neuere Version). Mitgelieferte Plugins lassen sich nur deaktivieren, nicht entfernen.
- Gebaut wird als der Benutzer aus `.env`, nicht als root.
- **Cluster**: Alle Knoten teilen den Datenordner. Es baut immer nur ein Knoten (Dateisperre in `bin/`), die anderen warten und übernehmen das Ergebnis. Neu gestartet wird ein Knoten nach dem anderen; der nächste erst, wenn der vorige wieder Anfragen annimmt (Abstimmung über die Datenbank), so bleibt das Wiki erreichbar.

## Katalog (1-Klick-Installation)

Plugins, die in `config.yaml` gelistet sind, erscheinen unter *Katalog* mit einem Installations-Knopf:

```yaml
plugins:
  catalog:
    - id: acme.kanban
      name: Kanban board
      repo: github.com/acme/ewiki-kanban
      ref: v1.2.0
      description: Agiles Board als Elementtyp
```

## Kommandozeile

```bash
ewiki plugin list
ewiki plugin install github.com/acme/ewiki-kanban --ref v1.2.0 --build
ewiki plugin install ./mein-plugin
ewiki plugin enable acme.kanban
ewiki plugin disable acme.kanban
ewiki plugin remove acme.kanban --build
ewiki plugin build              # Neubau, danach: systemctl restart enterprisewiki
```

## Updates

*Administration → Updates* vergleicht die installierten Versionen mit dem neuesten GitHub-Release (oder, ohne Releases, dem höchsten Versions-Tag) des Kerns und aller Plugins, die in `plugin.json` ein `repo` angeben oder im Katalog stehen. Das Ergebnis wird eine Stunde zwischengespeichert; *Nach Updates suchen* prüft sofort.

- **Plugins:** *Aktualisieren* lädt das Release herunter, legt die alte Version unter `plugins/.backup` ab und baut das Wiki neu. Danach unter *Plugins* neu starten. Per CLI: `ewiki plugin updates`, `ewiki plugin update ID --build`.
- **Kern:** wird auf dem Server mit `scripts/update.sh` (oder dem Update-Timer) aktualisiert, mit Backup und automatischem Rollback; die Oberfläche zeigt nur an, dass ein Update verfügbar ist.

Für GitHub Enterprise oder private Repositories siehe `plugins.updates` in [configuration.md](configuration.md).

## Vorlagen (Templates) und Positionen

Alle Vorlagen-Plugins sind „aktiv“, das heißt verfügbar. **Verwendet wird genau eine**: die unter *Administration → Einstellungen → Vorlage* gewählte (`ui.template` in `config.yaml`). Auf der Plugin-Seite ist sie als *Verwendete Vorlage* markiert; bei den anderen wechselt *Als Vorlage verwenden* direkt.

Jede Vorlage bietet Positionen, an denen Menüs (`core.menu`), Blöcke (`core.blocks`) und Widgets anderer Plugins erscheinen:

| Position | Wiki (`theme.wiki`) | Website (`theme.cms`) |
|---|---|---|
| `top-bar` | Leiste über dem Kopfbereich (z. B. Hinweise) | links in der dunklen Leiste ganz oben |
| `header-tools` | Suche, Sprache, Bearbeiten, Benutzer | rechts in der Leiste ganz oben |
| `header-nav` | Navigation im Kopfbereich | neben der Hauptnavigation |
| `cms-nav` | – | Hauptnavigation (automatisch: oberste Kategorien) |
| `hero` | – | volle Breite unter dem Kopfbereich (Banner) |
| `sidebar` | Seitenleiste mit Seitenbaum | Bereich unter dem Inhalt |
| `sidebar-bottom` | Seitenleiste unter dem Seitenbaum | – |
| `content-top`, `content-bottom` | über bzw. unter dem Inhalt jeder Seite | über bzw. unter dem Inhalt jeder Seite |
| `footer-1` … `footer-4` | – | Spalten im Fußbereich |
| `footer` | Fußbereich | unterste Zeile des Fußbereichs |

Die Wiki-Vorlage hieß bis v7.4.1 `theme.confluence` bzw. `theme.confluence.dark`. Beim Start werden `ui.template`, `plugins.disabled` und `plugin_settings` automatisch auf `theme.wiki` bzw. `theme.wiki.dark` umgestellt. Ihre Seitenleiste lässt sich auf allen Bildschirmgrößen über den Knopf im Kopfbereich ausblenden; der Zustand wird im Browser gemerkt.

### Website-Vorlage einrichten

*Administration → Website-Vorlage* (gilt für beide Website-Vorlagen; oder `plugin_settings.theme.cms` in `config.yaml`, API `PUT /api/v1/admin/settings`):

| Einstellung | Bedeutung |
|---|---|
| `logo`, `logo_light`, `logo_height` | Logo (hochgeladenes Bild) und helle Variante für den transparenten Kopfbereich über dem Startbild; Höhe in Pixel |
| `primary_color`, `accent_color` | Hauptfarbe (Links, Schaltflächen, aktive Navigation) und Akzentfarbe, `#rrggbb` |
| `header` | `overlay`: Kopfbereich liegt transparent über dem Startbild und wird beim Scrollen weiß; `solid`: immer weiß |
| `hero_image`, `hero_title`, `hero_subtitle`, `hero_button_label`, `hero_button_link`, `hero_size` | Startbild nur auf der Startseite; Größe `large` oder `medium` |
| `banner`, `banner_image` | Seitentitel auf Unterseiten in einem Bild-Banner (ohne eigenes Bild wird ein Farbverlauf verwendet) |
| `copyright` | Text in der untersten Zeile, `{year}` wird durch das aktuelle Jahr ersetzt |
| `back_to_top` | „Nach oben“-Knopf beim Scrollen |

Bilder werden wie Anhänge gespeichert und bei der Speicherbereinigung nicht gelöscht, solange eine Einstellung auf sie verweist. Die öffentlichen Werte liefert `GET /api/v1/x/theme.cms/config`. Menüs legen Sie unter *Administration → Menüs* an: Hauptnavigation an `header-nav` (ersetzt dann die automatische Navigation), Quicklinks an `top-bar`, Footer-Spalten mit Blöcken oder Menüs an `footer-1` … `footer-4`, rechtliche Links an `footer`.

Mit `?positions=1` an einer beliebigen Adresse (oder *Positionen auf der Seite anzeigen* unter *Menüs* bzw. *Blöcke*) sehen Administratoren alle Positionen mit Namen umrahmt. Menüs und Blöcke an einer Position, die die aktive Vorlage nicht hat, werden in der Verwaltung markiert und nicht angezeigt.

Mehrstufige Menüs: In horizontalen Positionen (`header-nav`, `top-bar`, `cms-nav`, `header-tools`) öffnen sich Untermenüs als Ausklapp-Menüs mit seitlichen Flyouts. In der Seitenleiste werden sie als aufklappbarer Baum gezeigt, im Fußbereich vollständig. Auf schmalen Bildschirmen und im Off-Canvas-Menü des CMS-Templates klappen sie untereinander auf. Templates passen das Aussehen über CSS-Variablen an: `--ewm-link`, `--ewm-link-hover-bg`, `--ewm-panel-bg`, `--ewm-panel-border`, `--ewm-accent`.

## Externe Suchmaschine

Für sehr große Datenbestände kann die Volltextsuche **OpenSearch/Elasticsearch** (ab 7.10 bzw. OpenSearch 1.x) oder **Meilisearch** (ab 1.3) verwenden: bessere Relevanz, Tippfehler-Toleranz, Entlastung der Datenbank. Einrichtung unter *Administration → Suchmaschine*: Suchmaschine wählen, URL, Indexname und Zugangsdaten eintragen, *Verbindung testen*.

- PostgreSQL bleibt die Referenz. Jede Änderung (Speichern, Verschieben, Tags, Papierkorb, Wiederherstellen, Löschen) landet über Datenbank-Trigger in einem Änderungsprotokoll; ein Hintergrundjob überträgt sie alle 10 Sekunden. Nach einer Änderung von Suchmaschine, URL oder Index sowie mit *Index neu aufbauen* wird der Index vollständig neu erstellt.
- Gesucht wird extern erst, wenn der Index vollständig aufgebaut ist. Bis dahin, bei Fehlern der Suchmaschine und bei deaktiviertem Plugin antwortet automatisch die eingebaute Suche. Die Antwort der Such-API enthält dazu `engine`.
- Berechtigungen, Papierkorb und Tags prüft immer das Wiki selbst; die Suchmaschine bekommt keine Rechteinformationen und muss nicht öffentlich erreichbar sein (sie sollte es auch nicht sein).
- Getestet ist die Anbindung gegen die dokumentierten APIs mit Testservern; vor dem Produktiveinsatz bitte mit Ihrer Version prüfen (*Verbindung testen*, *Index neu aufbauen*, Anzahl der Dokumente vergleichen).

## Manuelle Installation

Plugin-Verzeichnis (mit `plugin.json`, Go-Dateien und optional `web/`, `locales/`, `migrations/`) in den Plugin-Ordner kopieren – der laufende Server baut es automatisch (siehe oben). Ohne laufenden Server:

- Quellinstallation: `go run ./tools/genplugins` und `./scripts/build.sh`, dann den Dienst neu starten.
- Full-Image: `docker compose exec wiki ewiki plugin build` (oder einfach `docker compose restart wiki`, dann wird beim Start gebaut).

## Sicherheit

Plugins sind vollwertiger Code mit Zugriff auf Datenbank und Server. Installieren Sie nur Plugins aus vertrauenswürdigen Quellen und mit fester Version (`ref`). Jede Installation, Entfernung und (De-)Aktivierung wird protokolliert.

## Mitgelieferte Plugins

| ID | Art | Beschreibung |
|---|---|---|
| `auth.local` | Authentifizierung | lokale Konten, immer zuerst, erforderlich |
| `auth.ldap` | Authentifizierung | LDAP / Active Directory |
| `auth.oidc` | Authentifizierung | OpenID Connect (Keycloak, Entra ID, …) |
| `auth.shib` | Authentifizierung | Shibboleth / SAML 2.0 |
| `core.attachment` | Elementtyp | Dateien (PDF, Office, Bilder, Video) mit Vorschau und Volltext |
| `core.html` | Elementtyp | Seiten mit WYSIWYG oder Markdown |
| `core.menu` | Menü | beliebig viele, auch mehrstufige Menüs an jeder Position der Vorlage. Ziele sind Kategorien, Elemente, Links oder andere Menüs (*Administration → Menüs*). |
| `core.blocks` | Allgemein | Textblöcke (Markdown/HTML, Makros) an jeder Position, je Sprache, für alle/Gäste/Angemeldete (*Administration → Blöcke*) |
| `core.macros` | Makros | Inhaltsverzeichnis, Unterseiten, Zuletzt geändert (mit Quellen), Neuigkeiten aus mehreren Quellen und RSS/Atom-Feeds, Hinweis-Boxen, Aufklappbereich, Status-Label, Spalten |
| `core.layout` | Makros | Layout-Bausteine: Abschnitt, Hero, Karten, Button, Abstand (erbt von `core.macros`) |
| `macro.shell` | Makro | Ausgabe eines Shell-Befehls, nur nach exakter Freigabe durch einen Superadmin, standardmäßig aus |
| `macro.http` | Makro | Ergebnis eines HTTP-Abrufs, nur Adressen aus der Allow-Liste |
| `core.comments` | Allgemein | Kommentare und Antworten unter Seiten |
| `core.share` | Allgemein | Seiten und Kategorien mit Personen, Gruppen und Rollen teilen (ändert keine Rechte) |
| `core.mentions` | Allgemein | @Erwähnungen in Seiten und Kommentaren mit Vorschlägen beim Tippen |
| `core.reactions` | Allgemein | Emoji-Reaktionen auf Seiten, Kategorien und Kommentare; jedes Emoji, Sperrliste unter *Administration → Reaktionen*; Benachrichtigung des Autors (E-Mail standardmäßig aus) |
| `core.favorites` | Allgemein | persönliche Favoriten (Stern bei Seiten und Kategorien), Liste an einer Position der Vorlage (*Administration → Favoriten*) und im Profil |
| `core.properties` | Allgemein, Makro | Eigenschaften für Elemente jedes Typs: Definitionen global und pro Kategorie (vererbt) mit über 30 Typen, Pflicht, Vorgabe, Elementtypen, Rechten (wer sieht, wer ändert); freie Eigenschaften (pro Kategorie erlaub- oder verbietbar, Plugin-Einstellung `free`); laufende Nummern und Formeln; Verlauf; Makro „Eigenschaften-Bericht“ mit 13 Darstellungen. Rechte: `core.properties.admin` (global), `core.properties.define` und `core.properties.free` (Kategorie) |
| `content.propdb` | Element | Eigenschaften-Datenbank: Elemente aus Quellen interaktiv als Tabelle, Karten, Board, Kalender, Zeitleiste oder Diagramm, mit Filtern, Bearbeiten und neuen Einträgen (braucht `core.properties`) |
| `core.carousel` | Allgemein | Karussells an Positionen der Vorlage: Neuigkeiten (Option „Als Neuigkeit“ beim Veröffentlichen), eigene Folien, externe RSS/Atom-Feeds, Elemente beliebiger Quellen (*Administration → Karussells*) |
| `core.megamenu` | Menü (optional) | Darstellung „Mega-Menü“ für waagrechte Menüs: breite Panels mit Spalten, Überschriften und Beschreibungen (Auswahl im Menü unter *Darstellung*) |
| `workflow.process` | Elementtyp | Abläufe wie Bewerbungen oder Genehmigungen: Formular, Aufgaben, Freigaben (Vier-Augen), Bedingungen, E-Mails, Webhooks, Wartezeiten, Erinnerungen |
| `content.blog` | Elementtyp, Makro | Blog aus den Seiten einer Kategorie und weiteren Quellen: Anrisstexte, Layouts, Suche im Blog, Tags, Atom/RSS; Makro „Blogbeiträge aus mehreren Quellen“ |
| `content.forms` | Elementtyp | Webformulare: alle üblichen Felder, Bedingungen, Prüfregeln, Uploads mit Prüfung (Dateityp nach Inhalt, Office-Makros, aktive PDF-Inhalte, optional ClamAV und/oder ICAP), Raster oder frei platziert, Schritte, berechnete Werte, Wiederholgruppen, Unterschrift, Rechenaufgabe gegen Bots; Einsendungen speichern, exportieren, per E-Mail, Webhook, in eine Eigenschaften-Datenbank oder an einen Workflow; Einstellungen `clamd` und `icap` unter *Administration → Formulare*; Makro „Formular-Ergebnisse“ |
| `content.faq` | Elementtyp, Makro | FAQ aus Kategorien und Seiten (plus weitere Quellen): aufklappbare Abschnitte und Fragen, Sofort-Filter, Atom/RSS; Makro „FAQ aus mehreren Quellen“ |
| `content.events` | Elementtyp | Veranstaltungskalender: Monat/Woche/Liste, Serien, Anmeldung mit Warteliste, E-Mail-Erinnerungen, iCal-Abo, weitere Kalender und iCal-Abonnements als Quellen, Makro „Nächste Termine“ |
| `content.catlist` | Elementtyp | Kategorieübersicht: Unterkategorien einer Kategorie oder der obersten Ebene als Karten oder Liste, optional mit Seiten (z. B. als Startseite) |
| `content.team` | Elementtyp | Team-Seite aus Gruppen mit wählbaren Angaben aus der Visitenkarte bzw. dem Identity Provider |
| `content.notes` | Elementtyp | Gemeinsame Notizen: direkt in der Seitenansicht live gemeinsam schreiben, automatisch gespeichert (Besprechungen, Brainstorming, Aufgabenlisten) |
| `search.external` | Allgemein | OpenSearch/Elasticsearch oder Meilisearch für die Volltextsuche |
| `theme.wiki` | Template | Dokumentations-Layout mit Seitenbaum |
| `theme.cms` | Template | Website, klassisch (Hochschule, Organisation): Hauptnavigation, transparenter Kopfbereich über dem Startbild, Titel-Banner, Footer-Spalten |
| `theme.cms.modern` | Template | Website, modern und neutral: erbt Aufbau, Positionen und Einstellungen von `theme.cms`, eigene Gestaltung (Schrift Inter, Glas-Kopfbereich, linksbündiges Startbild, abgerundete Karten) |
| `theme.wiki.dark` | Template (Beispiel) | erbt von `theme.wiki`, ändert nur die Farben |
| `example.notice` | Elementtyp (Beispiel) | Hinweisbox, erbt von `core.html` |

Eigene Plugins entwickeln: [docs/developer/plugins.md](../developer/plugins.md)
