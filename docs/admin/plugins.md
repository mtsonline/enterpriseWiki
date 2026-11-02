# Plugins verwalten

Fast alles im Wiki ist ein Plugin: Elementtypen (z. B. Seiten), Anmeldeverfahren, Menüs, Templates, Admin-Seiten. Plugins werden **zusammen mit dem Kern kompiliert**. Das macht sie schnell und typsicher, und es gibt keine Laufzeit-Abhängigkeiten.

| Aktion | Wirkung | Neubau nötig? |
|---|---|---|
| aktivieren / deaktivieren | sofort (API-Routen, Elementtypen, Oberflächenteile) | nein |
| installieren / aktualisieren | Quellcode wird nach `plugins/<name>` kopiert | ja |
| entfernen | Quellcode wird nach `plugins/.backup/` verschoben, Daten bleiben in der Datenbank | ja |

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
- **Jetzt neu bauen** führt `scripts/build.sh` im Hintergrund aus. Ein fehlgeschlagener Build lässt die laufende Version unverändert. Nach erfolgreichem Build schaltet **Neu starten** ohne Abbruch laufender Anfragen auf die neue Version um.

Das Bauen aus der Oberfläche setzt die Quellinstallation (`scripts/install.sh`) mit Go und Node.js voraus. Bei Docker legen Sie Plugins in `plugins/` und bauen das Image neu.

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

*Administration → Website-Vorlage* (oder `plugin_settings.theme.cms` in `config.yaml`, API `PUT /api/v1/admin/settings`):

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

1. Plugin-Verzeichnis nach `plugins/<name>/` kopieren (es enthält `plugin.json`, Go-Dateien und optional `web/`, `locales/`, `migrations/`).
2. `go run ./tools/genplugins` (aktualisiert `plugins/all/zz_generated.go`).
3. `./scripts/build.sh` und den Dienst neu starten.

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
| `core.macros` | Makros | Inhaltsverzeichnis, Unterseiten, Zuletzt geändert, Hinweis-Boxen, Aufklappbereich, Status-Label, Spalten |
| `core.layout` | Makros | Layout-Bausteine: Abschnitt, Hero, Karten, Button, Abstand (erbt von `core.macros`) |
| `macro.shell` | Makro | Ausgabe eines Shell-Befehls, nur nach exakter Freigabe durch einen Superadmin, standardmäßig aus |
| `macro.http` | Makro | Ergebnis eines HTTP-Abrufs, nur Adressen aus der Allow-Liste |
| `core.comments` | Allgemein | Kommentare und Antworten unter Seiten |
| `workflow.process` | Elementtyp | Abläufe wie Bewerbungen oder Genehmigungen: Formular, Aufgaben, Freigaben (Vier-Augen), Bedingungen, E-Mails, Webhooks, Wartezeiten, Erinnerungen |
| `content.blog` | Elementtyp | Blog aus den Seiten einer Kategorie: Anrisstexte, Layouts, Suche im Blog, Tags, Atom/RSS |
| `content.faq` | Elementtyp | FAQ aus Kategorien und Seiten: aufklappbare Abschnitte und Fragen, Sofort-Filter, Atom/RSS |
| `content.events` | Elementtyp | Veranstaltungskalender: Monat/Woche/Liste, Serien, Anmeldung mit Warteliste, E-Mail-Erinnerungen, iCal-Abo, Makro „Nächste Termine“ |
| `content.catlist` | Elementtyp | Kategorieübersicht: Unterkategorien einer Kategorie oder der obersten Ebene als Karten oder Liste, optional mit Seiten (z. B. als Startseite) |
| `content.team` | Elementtyp | Team-Seite aus Gruppen mit wählbaren Angaben aus der Visitenkarte bzw. dem Identity Provider |
| `search.external` | Allgemein | OpenSearch/Elasticsearch oder Meilisearch für die Volltextsuche |
| `theme.wiki` | Template | Dokumentations-Layout mit Seitenbaum |
| `theme.cms` | Template | Website-Layout mit Hauptnavigation, Off-Canvas-Menü und Footer-Spalten |
| `theme.wiki.dark` | Template (Beispiel) | erbt von `theme.wiki`, ändert nur die Farben |
| `example.notice` | Elementtyp (Beispiel) | Hinweisbox, erbt von `core.html` |

Eigene Plugins entwickeln: [docs/developer/plugins.md](../developer/plugins.md)
