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

## Herkunft von Core-Plugins

Die Plugins auf dieser Seite sind **Core-Plugins**: Sie werden mit dem Wiki kompiliert, laufen im Wiki-Prozess und haben dieselben Rechte wie das Wiki selbst. Deshalb zeigt das Wiki vor jeder Installation eine Warnung und prüft, woher der Code kommt. Die Prüfung richtet sich nur nach der Quelle, nie nach Angaben im Plugin selbst.

| Herkunft | Woher | Installieren |
|---|---|---|
| **Offiziell** | mit dem Wiki ausgeliefert (später zusätzlich mit geprüfter Signatur des Projekts) | mit Warnung |
| **Organisation** | aus einer Quelle, die ein Instanz-Administrator vorher als vertrauenswürdig eingetragen hat | mit Warnung |
| **Extern** | jede andere Quelle, zum Beispiel ein unbekanntes GitHub-Repository | mit Warnung **und** einem Kontrollkästchen, das angehakt werden muss; bei jedem Update erneut |
| **Unbekannt** | ein Ordner, der von Hand in den Plugin-Ordner kopiert wurde | wird wie *Extern* behandelt |

**SDK-Plugins sind davon nicht betroffen.** Sie laufen als eigener, abgeschotteter Prozess und greifen nur über das SDK auf das Wiki zu.

### Vertrauenswürdige Quellen

*Administration → Plugins → Vertrauenswürdige Quellen* (nur Instanz-Administratoren):

- Ein Eintrag ist eine Adresse (`https://…` oder `ssh://…`, auch `git@host:pfad`) oder ein Verzeichnis auf dem Server. Er gilt für die Adresse und alles darunter, nur an Pfadgrenzen: `https://git.example.com/team` umfasst `https://git.example.com/team/kanban`, aber nicht `https://git.example.com/team-fremd/kanban`.
- Ein ganzer Server ohne Pfad wird nicht angenommen. Adressen mit Passwort, Abfrage (`?`), `..`, leeren Pfadteilen (`//`), `%`-Kodierung, `\`, Steuerzeichen oder `http://` werden abgelehnt, damit das Wiki genau die Adresse prüft, die Git abruft. Groß- und Kleinschreibung des Servers, `.git` am Ende, der Standard-Port und ein Benutzername vor dem Server spielen keine Rolle.
- **Externe Quellen erlauben** (`plugins.allow_external`, Standard: an): Ausgeschaltet lassen sich nur offizielle Core-Plugins und solche aus vertrauenswürdigen Quellen installieren.
- Eine Quelle eintragen und externe Quellen wieder erlauben geht nur mit einer **Anmeldung in den letzten 15 Minuten** im Browser oder mit der Kommandozeile auf dem Server, nicht mit API-Schlüsseln. Entfernen und Ausschalten geht jederzeit.
- Bereits installierte Core-Plugins bleiben installiert, wenn ihre Quelle entfernt wird.

Die Liste der Plugins zeigt in der Spalte *Herkunft* die Stufe, die Quelle und den Stand (Commit). Das Wiki legt dazu im Ordner jedes installierten Plugins die Datei `.ewiki-origin.json` an. Eine Datei dieses Namens in den Quellen eines Plugins wird beim Installieren entfernt.

In `config.yaml`:

```yaml
plugins:
  allow_external: true
  trusted_sources:
    - prefix: https://git.example.com/wiki-plugins
      note: Plugins unseres Teams
    - prefix: /srv/ewiki-plugins
```

### Zugangsdaten für private Quellen

Plugins aus privaten Repositorys (zum Beispiel Pro-Plugins oder eigene Plugins der Organisation) brauchen Zugangsdaten. *Administration → Plugins → Zugangsdaten für Plugin-Quellen* (nur Instanz-Administratoren):

- Ein Eintrag gilt für alle Repositorys unter einer **https-Adresse**, z. B. `https://github.com/acme` oder `https://git.example.com/team`, an Pfadgrenzen wie bei den vertrauenswürdigen Quellen. Passen mehrere Einträge, gilt der längste. Ohne `https://` wird nichts gesendet.
- **Token:** am besten ein Token nur zum Lesen, nur für die nötigen Repositorys (bei GitHub ein „Fine-grained token“ mit *Contents: Read-only*). Der Benutzername ist ohne Angabe `x-access-token`, wie GitHub es für Tokens erwartet.
- Das Wiki nutzt die Zugangsdaten beim **Installieren**, beim **Aktualisieren** und bei der **Suche nach Updates**. Git bekommt sie nur über die Umgebung, nie in der Befehlszeile oder in der Adresse; Weiterleitungen werden dabei nicht verfolgt, damit das Token nicht an einen anderen Server geht. In Fehlermeldungen wird es unkenntlich gemacht.
- Das Token wird nach dem Speichern **nie wieder angezeigt**, auch nicht in der API oder im Audit-Protokoll (dort steht nur die Adresse). Zum Ändern einen neuen Eintrag mit derselben Adresse speichern.
- Speichern braucht eine **Anmeldung in den letzten 15 Minuten** oder die Kommandozeile auf dem Server. Entfernen geht jederzeit.
- Per CLI, ohne das Token in der Shell-Historie: `printf '{"prefix":"https://github.com/acme","token":"%s"}' "$TOKEN" | ewiki plugin credentials set --json -`; `ewiki plugin credentials list`, `ewiki plugin credentials remove --prefix …`.

In `config.yaml` (gespeichert wie andere Geheimnisse; ein Verweis auf eine Umgebungsvariable ist nur hier möglich, nicht über Oberfläche oder API):

```yaml
plugins:
  credentials:
    - prefix: https://github.com/acme
      token: ${ACME_PLUGIN_TOKEN}
      note: Pro-Plugins
```

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

# Herkunft
ewiki plugin install github.com/fremd/plugin --accept-external   # externe Quelle: Warnung bestätigen
ewiki plugin sources list
ewiki plugin sources add https://git.example.com/wiki-plugins --note "unser Team"
ewiki plugin sources remove https://git.example.com/wiki-plugins
ewiki plugin sources check github.com/acme/ewiki-kanban
ewiki plugin sources external forbid   # oder: allow
```

Ohne `--accept-external` bricht die Installation aus einer externen Quelle mit der Warnung ab.

## Updates

*Administration → Updates* vergleicht die installierten Versionen mit dem neuesten GitHub-Release (oder, ohne Releases, dem höchsten Versions-Tag) des Kerns und aller Plugins, die in `plugin.json` ein `repo` angeben oder im Katalog stehen. Das Ergebnis wird eine Stunde zwischengespeichert; *Nach Updates suchen* prüft sofort.

- **Plugins:** *Aktualisieren* lädt das Release herunter, legt die alte Version unter `plugins/.backup` ab und baut das Wiki neu. Danach unter *Plugins* neu starten. Per CLI: `ewiki plugin updates`, `ewiki plugin update ID --build`.
- **Kern:** wird auf dem Server mit `scripts/update.sh` (oder dem Update-Timer) aktualisiert, mit Backup und automatischem Rollback; die Oberfläche zeigt nur an, dass ein Update verfügbar ist.

Private Repositorys: siehe [Zugangsdaten für private Quellen](#zugangsdaten-für-private-quellen). Für GitHub Enterprise siehe `plugins.updates` in [configuration.md](configuration.md).

## Vorlagen (Templates) und Positionen

Alle Vorlagen-Plugins sind „aktiv“, das heißt verfügbar. **Verwendet wird genau eine**: die unter *Administration → Einstellungen → Vorlage* gewählte (`ui.template` in `config.yaml`). Auf der Plugin-Seite ist sie als *Verwendete Vorlage* markiert; bei den anderen wechselt *Als Vorlage verwenden* direkt.

Jede Vorlage bietet Positionen, an denen Menüs (`core.menu`), Bausteine (`core.blocks`, Elemente vom Typ *Baustein*) und Widgets anderer Plugins erscheinen:

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

Bei der Wiki-Vorlage (`theme.wiki`, `theme.wiki.dark`) lässt sich die Seitenleiste auf allen Bildschirmgrößen über den Knopf im Kopfbereich ausblenden; der Zustand wird im Browser gemerkt.

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

### Templates aus Daten (Template-Designer)

Neben Templates aus Code (Plugins, als Referenz) kennt das Wiki **Templates aus Daten**. Ein solches Template ist nur eine Beschreibung: Zeilen mit Bereichen und Positionen, Gestaltungswerte und die Ansichten für Tablet und Handy. Das Wiki baut daraus die Seite, mit demselben Code im Designer und auf jeder Seite. Was gestaltet wird, wird also genau so gezeigt.

- **Wo:** *Administration → Templates* listet alle Templates. Templates aus Code lassen sich dort nicht ändern oder kopieren. Die beiden mitgelieferten Templates aus Daten (*Wiki (Daten)*, *Website (Daten)*) sind schreibgeschützt; zum Ändern kopieren Sie eines. Der Designer hat zwei Ansichten desselben Designs; eine Änderung in der einen steht sofort in der anderen:
  - **visuell:** der Aufbau (Zeilen von oben nach unten, Bereiche nebeneinander; auswählen, mit den Pfeil-Knöpfen verschieben, hinzufügen, löschen), die Eigenschaften einer Zeile oder eines Bereichs, die Ansichten für Tablet und Handy (automatisch oder von Hand angepasst und *Wieder automatisch*), Farben mit dem Kontrast jedes Paares und der nächsten Farbe, die reicht (*übernehmen*), Schrift, Größe, Ecken, Logo und Verhalten. Alles geht mit der Tastatur; Rückgängig und Wiederholen auch mit Strg+Z und Strg+Y;
  - **als Text (JSON)**, beim Tippen geprüft.

  Jede Änderung prüft der Server sofort. An jeder Zeile sagt der Designer, wie viel Platz die Bereiche in jeder Ansicht brauchen und wie viel es gibt.
- **Vorschau:** das Wiki selbst mit dem Design, als Desktop (1280 px), Tablet (768 px) oder Handy (375 px), auf jeder Seite des Wikis, auch in einem eigenen Fenster. Sie sehen sie nur selbst, mit Ihren Rechten; gespeichert wird erst beim Speichern. Die Vorschau misst, was wirklich Platz hat, zum Beispiel wie viele Einträge eines Menüs in eine Zeile passen, und nennt es unter *Hinweise*. Der Baukasten schreibt CSS nur aus Werten mit sicherer Form; ein noch nicht geprüfter Entwurf kann also nichts anderes verändern.
- **Wer:** das Recht `core.template.design` (anlegen, kopieren, importieren, ändern, Papierkorb). Welches Template gilt, wählen weiter die Einstellungen der Website (`core.settings.manage`) und einer Kategorie (*Verwalten*).
- **Prüfungen im Server**, dieselben wie im Designer (`plugins/core_template/limits.json`):
  - genau ein Bereich ist der Inhalt der Seite, dazu mindestens ein weiterer Bereich (für das Benutzermenü);
  - überall, wo Teile nebeneinander stehen, muss Platz sein: bei den Bereichen jeder Zeile (nicht nur neben dem Inhalt) und bei den Positionen in einem Bereich (Anordnung `row`). Auf dem Desktop wird das immer geprüft, auf Tablet und Handy, wenn eine Ansicht sie nebeneinander lässt, sonst stehen sie untereinander. Die Meldung nennt, wie viel Platz nötig ist und wie viel es gibt;
  - ein klebender Kopf belegt höchstens ein Viertel der Fensterhöhe (`sticky_max_vh`); ist er höher, etwa auf dem Handy quer oder mit offener Schublade, scrollt er mit der Seite und verdeckt so nie Inhalt oder Fokus;
  - jedes Farbpaar hat genug Kontrast (Text 4,5:1, Bedienelemente und Fokus 3:1);
  - Kopf und Fuß sind Zeilen mit der Rolle `banner` oder `contentinfo`; nebeneinanderliegende Zeilen bilden eine Landmarke;
  - mehrere Navigationen oder Seitenspalten brauchen verschiedene Beschriftungen; Positionen der Seiten selbst (`element-…`, `category-…`) sind nicht erlaubt;
  - nur Schriften des Wikis selbst und Bilder des Wikis (`/api/v1/files/…`); unbekannte Felder werden abgelehnt.
- **Ansichten:** Tablet und Handy ergeben sich nach Regeln aus dem Desktop: Seitenspalten neben dem Inhalt und die Menüs des Kopfs kommen in Schubladen (ein Knopf öffnet sie, Escape schließt sie), Bereiche, die nicht passen, rutschen in die nächste Zeile oder untereinander. Eine Zeile kann ihre Ansicht auch von Hand festlegen (`tablet`, `mobile`: `stack`, `hidden`, `drawer`, `widths`). **Eigenes CSS:** Ist es in den Einstellungen erlaubt (`ui.custom_css`), hat der Designer unter *Gestaltung* ein Feld *Eigenes CSS*; es prüft beim Tippen nach denselben Regeln wie der Server (`limits.json`, Abschnitt `css`). **Auch auf dem Desktop** kann ein Bereich in einer Schublade liegen (`desktop`: nur `drawer`), zum Beispiel das Menü als „Burger-Menü“; er liegt dann in jeder Ansicht in einer Schublade und braucht in der Zeile keinen Platz.
- **Verhalten** (Designer: *Verhalten* und die Eigenschaften einer Zeile oder eines Bereichs):
  - der klebende Kopf kann **niedriger werden**, sobald die Seite gescrollt ist (`header_shrink`), und **beim Hinunterscrollen verschwinden** (`header_hide`); er kommt beim Hinaufscrollen zurück und immer, wenn der Tastatur-Fokus in ihm ist, so verdeckt er nie, was den Fokus hat. Beides setzt `sticky_header` voraus;
  - eine Seitenspalte (Rolle `complementary`) kann beim Scrollen **neben dem Inhalt sichtbar bleiben** (`sticky`; ist sie höher als das Fenster, scrollt sie in sich) und sich **mit einem Knopf ausblenden** lassen (`collapsible`; der Browser merkt sich die Wahl). Beides wirkt nur auf dem Desktop; auf Tablet und Handy folgt die Spalte der gestalteten Ansicht. Nicht zusammen mit einer Schublade auf dem Desktop;
  - eine Fußzeile (Rolle `contentinfo`) kann sich **auf dem Handy auf- und zuklappen** (`collapse_mobile`): Jeder Bereich zeigt nur seinen Namen, ein Tippen öffnet ihn. Dann braucht jeder Baustein einen Namen (`label`);
  - **nur den Inhalt drucken** (`print_plain`): Kopf, Fuß, Seitenspalten und Knöpfe werden beim Drucken weggelassen.
- **Breiten mit der Maus:** Die Eigenschaften einer Zeile zeigen die Breiten auf dem Desktop als Balken; ziehen Sie am Rand eines Bereichs oder nutzen Sie darauf die Pfeiltasten (Bild auf/ab: 4 rem). **Standard-Positionen:** Verlässt eine Position das Template, an der Bausteine als Vorgabe stehen, nennt der Designer diese Bausteine und fragt vorher. **Leere Positionen** sind in der Vorschau mit ihrem Namen markiert, im Wiki nicht.
- **Rechte je Template:** *Rechte* in der Liste der Templates legt fest, wer ein Template ändern und wer es verwalten darf (seine Rechte, den Papierkorb, sein eigenes CSS): Personen, Gruppen, Rollen oder alle Angemeldeten. Niemand eingetragen: alle Gestaltenden dürfen. Nur Verwaltende eingetragen: nur sie dürfen es auch ändern. Diese Rechte schränken nur ein: Ohne `core.template.design` geben sie nichts; Administratoren dürfen immer; niemand kann sich selbst das Verwalten nehmen. Andere können das Template weiter ansehen und kopieren (sein Design ist ohnehin öffentlich). Änderungen der Rechte stehen im Audit.
- **Eigene Schriften:** *Eigene Schriften* unter der Liste der Templates lädt eine Schrift als eine oder mehrere Dateien in den üblichen Formaten hoch (WOFF2, WOFF, TTF, OTF), zum Beispiel normal, fett und kursiv, oder eine variable Schrift mit allen Stärken; Stärke und Stil werden aus den Dateinamen übernommen und lassen sich ändern. Nennen Sie ihre Lizenz (SIL OFL 1.1, Apache 2.0, MIT oder eine andere mit einem Hinweis, woher das Recht kommt) und bestätigen Sie, dass sie auf dieser Website verwendet werden darf; beides steht im Audit. Das Wiki liefert die Datei unverändert selbst aus (`/api/v1/x/core.template/fonts/ID/files/N`), nie von fremden Servern; öffentlich ist sie nur, solange ein Template außerhalb des Papierkorbs sie verwendet. Weil die Datei nicht verändert wird (keine Umwandlung, keine Teilmenge), gilt die Regel der OFL zu reservierten Schriftnamen für veränderte Fassungen nicht. Eine Schrift, die ein Template verwendet (auch im Papierkorb), lässt sich nicht entfernen. Ein exportiertes Template mit eigener Schrift braucht im anderen Wiki dieselbe Schrift. Grenzen: `core.template.fonts`, `core.template.font_kb`, `core.template.font_upload_kb`.
- **Ein Template zuerst ausprobieren:** *Nur für mich* zeigt das Wiki mit einem Template nur Ihnen, in diesem Browser; ein Balken oben auf jeder Seite sagt das und beendet es, oder verwendet es mit dem Recht für die Einstellungen für alle. Kategorien mit eigenem Template behalten es. Auch eine Kategorie kann ein Template zuerst bekommen (Einstellungen der Kategorie).
- **Versionen und Papierkorb:** Jedes Speichern ist eine Version (höchstens `core.template.versions`); ein verwendetes Template kann nicht in den Papierkorb. Höchstens `core.template.count` Templates.
- **Export und Import** als Datei (`format: ewiki-template`).

## Externe Suchmaschine

Für sehr große Datenbestände kann die Volltextsuche **OpenSearch/Elasticsearch** (ab 7.10 bzw. OpenSearch 1.x) oder **Meilisearch** (ab 1.3) verwenden: bessere Relevanz, Tippfehler-Toleranz, Entlastung der Datenbank. Einrichtung unter *Administration → Suchmaschine*: Suchmaschine wählen, URL, Indexname und Zugangsdaten eintragen, *Verbindung testen*.

- PostgreSQL bleibt die Referenz. Jede Änderung (Speichern, Verschieben, Tags, Papierkorb, Wiederherstellen, Löschen) landet über Datenbank-Trigger in einem Änderungsprotokoll; ein Hintergrundjob überträgt sie alle 10 Sekunden. Nach einer Änderung von Suchmaschine, URL oder Index sowie mit *Index neu aufbauen* wird der Index vollständig neu erstellt.
- Gesucht wird extern erst, wenn der Index vollständig aufgebaut ist. Bis dahin, bei Fehlern der Suchmaschine und bei deaktiviertem Plugin antwortet automatisch die eingebaute Suche. Die Antwort der Such-API enthält dazu `engine`.
- Zu jedem Dokument speichert die Suchmaschine, wer es sehen darf (Benutzer, Gruppen, Rollen, angemeldete Personen, Gäste; Administratoren sehen alles). Jede Suche filtert danach. So findet jede Person ihre Seiten, auch wenn es zum Suchbegriff viel mehr Seiten gibt, die sie nicht sehen darf. Eine Änderung der Rechte überträgt das Wiki innerhalb von Sekunden in den Index.
- Danach prüft das Wiki jeden Treffer noch einmal selbst mit den aktuellen Rechten, ebenso Papierkorb und Tags. Ein veralteter oder fehlerhafter Index zeigt deshalb nie mehr, als jemand sehen darf.
- Die Suchmaschine enthält Seiteninhalte und die Kennungen der Berechtigten. Sie muss nicht öffentlich erreichbar sein und sollte es auch nicht sein; schützen Sie sie mit Zugangsdaten.
- Die eingebaute Suche (PostgreSQL) filtert genauso schon in der Abfrage nach den Bereichen, die jemand sehen darf.
- Die automatischen Tests laufen gegen die jeweils neueste Version von OpenSearch, Elasticsearch und Meilisearch (Indexaufbau, Änderungen, Rechte, Tags, Tippfehler, Ausfall der Suchmaschine). Ältere Versionen ab den oben genannten werden nicht automatisch getestet; bitte vor dem Produktiveinsatz mit Ihrer Version prüfen (*Verbindung testen*, *Index neu aufbauen*, Anzahl der Dokumente vergleichen).
- Für Elasticsearch wählen Sie die Suchmaschine *OpenSearch / Elasticsearch*.

## Manuelle Installation

Plugin-Verzeichnis (mit `plugin.json`, Go-Dateien und optional `web/`, `locales/`, `migrations/`) in den Plugin-Ordner kopieren – der laufende Server baut es automatisch (siehe oben). Ohne laufenden Server:

- Quellinstallation: `go run ./tools/genplugins` und `./scripts/build.sh`, dann den Dienst neu starten.
- Full-Image: `docker compose exec wiki ewiki plugin build` (oder einfach `docker compose restart wiki`, dann wird beim Start gebaut).

## Sicherheit

Core-Plugins sind vollwertiger Code mit Zugriff auf Datenbank und Server. Installieren Sie nur Core-Plugins aus vertrauenswürdigen Quellen und mit fester Version (`ref`), siehe [Herkunft von Core-Plugins](#herkunft-von-core-plugins). Installieren, Aktualisieren und vertrauenswürdige Quellen sind Sache der Instanz-Administratoren. Jede Installation (mit Herkunft, Quelle, Commit und Bestätigung), Entfernung, (De-)Aktivierung und jede Änderung der vertrauenswürdigen Quellen wird protokolliert.

## Mitgelieferte Plugins

| ID | Art | Beschreibung |
|---|---|---|
| `auth.local` | Authentifizierung | lokale Konten, immer zuerst, erforderlich |
| `auth.ldap` | Authentifizierung | LDAP / Active Directory |
| `auth.oidc` | Authentifizierung | OpenID Connect (Keycloak, Entra ID, …) |
| `auth.shib` | Authentifizierung | Shibboleth / SAML 2.0 |
| `mfa.totp` | Anmeldefaktor | Code einer Authenticator-App (RFC 6238) |
| `mfa.email` | Anmeldefaktor | Einmal-Code per E-Mail an die Haupt-Adresse |
| `mfa.network` | Anmeldefaktor | erlaubte Netze je Person (stiller Faktor, wahlweise für alle Sitzungen, API-Keys und Feed-Links durchgesetzt) |
| `mfa.webauthn` | Anmeldefaktor | Passkeys und Sicherheitsschlüssel (FIDO2), auch Anmeldung ohne Passwort |
| `signature.passkey` | Signatur | Entscheidungen (z. B. Freigaben in Workflows) mit Passkey oder Sicherheitsschlüssel signieren, fortgeschrittene elektronische Signatur |
| `signature.smime` | Signatur | ausgehende E-Mails mit S/MIME signieren (Zertifikate der Organisation und persönliche), Ablaufwarnungen, zweites Siegel für Nachweise |
| `office.collabora` | Office | Office-Dateien in Collabora Online öffnen und bearbeiten (WOPI) |
| `office.onlyoffice` | Office | Office-Dateien in OnlyOffice Docs öffnen und bearbeiten (OnlyOffice-API oder WOPI) |
| `office.m365` | Office | Word-, Excel- und PowerPoint-Dateien in Microsoft 365 für das Web (WOPI, nur mit CSPP) |
| `core.attachment` | Elementtyp | Dateien (PDF, Office, Bilder, Video) mit Vorschau und Volltext |
| `core.html` | Elementtyp | Seiten mit WYSIWYG oder Markdown |
| `core.menu` | Menü | beliebig viele, auch mehrstufige Menüs an jeder Position der Vorlage. Ziele sind Kategorien, Elemente, Links oder andere Menüs (*Administration → Menüs*). |
| `core.blocks` | Allgemein | **Bausteine als Elemente:** Ein Element vom Typ *Baustein* liegt in einer Kategorie (deren Rechte, Versionen, Entwürfe, Übersetzungen), wird wie eine Seite geschrieben (Editor mit allen Bausteinen) und erscheint an Positionen des Templates: in der Kategorie (wahlweise nur auf ihrer Seite, mit oder ohne Unterkategorien), auf einer Seite oder auf der ganzen Website. Es steht nie in der Suche oder in Seitenlisten. Auf der Seite des Bausteins zeigt *Wo dieser Baustein erscheint* alle Platzierungen; *Baustein platzieren* fügt eine hinzu. **Ein Baustein, der auf der ganzen Website erscheint** (Ebene Template, Website oder Elementtyp), lässt sich nur mit dem Recht *Bausteine der ganzen Website bearbeiten* (`core.blocks.edit`) ändern, verschieben oder löschen, damit niemand mit Rechten in einer Kategorie still Inhalt auf allen Seiten ändert. **Eine Seite als Fußzeile:** ein Baustein mit `::include{page=ID title=false}` an der Position `footer`; Spalten mit `:::grid`. Einbindungen zählen auch hier zur Grenze `core.content.includes_per_page` |
| `core.macros` | Makros | Inhaltsverzeichnis, Unterseiten, Zuletzt geändert (mit Quellen), Neuigkeiten aus mehreren Quellen und RSS/Atom-Feeds, Hinweis-Boxen, Aufklappbereich, Status-Label, Spalten, Bildergalerie, Seite einbinden, Auszug, Anker, Seitenbaum, Suchfeld, Seite aus Vorlage anlegen, Anhänge. Codeblöcke färbt der Browser mit highlight.js (BSD-3-Clause), geladen nur für Seiten mit Code. **Formeln** (`:math{tex="…"}` oder ein Codeblock ```` ```math ````) setzt der Browser mit Temml (MIT) als MathML, geladen nur für Seiten mit Formeln. Der Server schreibt nur den TeX-Text; in die Seite kommt ein neu gebauter MathML-Baum aus einer festen Liste von Elementen und Attributen (keine Links, keine Stile, kein HTML). Es gibt keine mitgelieferte Mathe-Schrift; der Browser nimmt die Mathe-Schrift des Systems. *Seite einbinden* zeigt nur, was die lesende Person sehen darf, und nie Inhalt, der strenger eingestuft ist als die einbindende Seite; die Grenze `core.content.includes_per_page` zählt Einbindungen über alle Ebenen |
| `core.layout` | Makros | Layout-Bausteine: Abschnitt, Hero, Karten, Button, Abstand, Raster, Überschrift, Trenner, Knopfgruppe, Handlungsaufruf, Reiter, Akkordeon, Symbol, Symbolbox, Symbolliste (erbt von `core.macros`). Die Symbole sind eine freie Sammlung (MIT), die mit dem Programm ausgeliefert und vom Wiki selbst bereitgestellt wird, eine Datei je Symbol |
| `macro.shell` | Makro | Ausgabe eines Shell-Befehls, nur nach exakter Freigabe durch einen Superadmin, standardmäßig aus |
| `macro.http` | Makro | Ergebnis eines HTTP-Abrufs, nur Adressen aus der Allow-Liste |
| `core.comments` | Allgemein | Kommentare und Antworten unter Seiten |
| `core.share` | Allgemein | Seiten und Kategorien mit Personen, Gruppen und Rollen teilen (ändert keine Rechte) |
| `core.mentions` | Allgemein | @Erwähnungen in Seiten und Kommentaren mit Vorschlägen beim Tippen |
| `core.reactions` | Allgemein | Emoji-Reaktionen auf Seiten, Kategorien und Kommentare; jedes Emoji, Sperrliste unter *Administration → Reaktionen*; Benachrichtigung des Autors (E-Mail standardmäßig aus) |
| `core.favorites` | Allgemein | persönliche Favoriten (Stern bei Seiten und Kategorien), Liste als Baustein der Platzierung (Vorgabe: Seitenleiste; dort ausgeblendet nur im Profil) und im Profil |
| `core.properties` | Allgemein, Makro | Eigenschaften für Elemente jedes Typs: Definitionen global und pro Kategorie (vererbt) mit über 30 Typen, Pflicht, Vorgabe, Elementtypen, Rechten (wer sieht, wer ändert); freie Eigenschaften (pro Kategorie erlaub- oder verbietbar, Plugin-Einstellung `free`); laufende Nummern und Formeln; Verlauf; Makro „Eigenschaften-Bericht“ mit 13 Darstellungen. Rechte: `core.properties.admin` (global), `core.properties.define` und `core.properties.free` (Kategorie) |
| `content.propdb` | Element | Eigenschaften-Datenbank: Elemente aus Quellen interaktiv als Tabelle, Karten, Board, Kalender, Zeitleiste oder Diagramm, mit Filtern, Bearbeiten und neuen Einträgen (braucht `core.properties`) |
| `core.carousel` | Allgemein | Karussells an Positionen der Vorlage: Neuigkeiten (Option „Als Neuigkeit“ beim Veröffentlichen), eigene Folien, externe RSS/Atom-Feeds, Elemente beliebiger Quellen (*Administration → Karussells*) |
| `core.megamenu` | Menü (optional) | Darstellung „Mega-Menü“ für waagrechte Menüs: breite Panels mit Spalten, Überschriften und Beschreibungen (Auswahl im Menü unter *Darstellung*) |
| `workflow.process` | Elementtyp | Abläufe wie Bewerbungen oder Genehmigungen: Formular, Aufgaben, Freigaben (Vier-Augen), Bedingungen, E-Mails, Webhooks, Wartezeiten, Erinnerungen; Workflow 2: Auslöser (Zeitplan, Seitenänderungen, Ereignisse, Konten und Gruppen, signierter eingehender Webhook), Schritte Nachricht, Werte/Formeln, HTTP-Abfrage, Seite anlegen, Eigenschaften, Berechtigungen (auch befristet), Gruppenmitgliedschaft, Seite verschieben, Datei erzeugen, Kalendereintrag, weiterer Workflow, parallele Zweige; Entscheidungslinks in E-Mails, Testlauf, Auswertung, Aufbewahrung; Fristen mit Arbeitstagen (Feiertage unter *Administration → Workflow*), Warnungen und Eskalation |
| `content.blog` | Elementtyp, Makro | Blog aus den Seiten einer Kategorie und weiteren Quellen: Anrisstexte, Layouts, Suche im Blog, Tags, Atom/RSS; Makro „Blogbeiträge aus mehreren Quellen“ |
| `content.forms` | Elementtyp | Webformulare: alle üblichen Felder, Bedingungen, Prüfregeln, Uploads mit Prüfung (Dateityp nach Inhalt, Office-Makros, aktive PDF-Inhalte, optional ClamAV und/oder ICAP), Raster oder frei platziert, Schritte, berechnete Werte, Wiederholgruppen, Unterschrift, Rechenaufgabe gegen Bots; Einsendungen speichern, exportieren, per E-Mail, Webhook, in eine Eigenschaften-Datenbank oder an einen Workflow; Einstellungen `clamd` und `icap` unter *Administration → Formulare*; Makro „Formular-Ergebnisse“ |
| `content.faq` | Elementtyp, Makro | FAQ aus Kategorien und Seiten (plus weitere Quellen): aufklappbare Abschnitte und Fragen, Sofort-Filter, Atom/RSS; Makro „FAQ aus mehreren Quellen“ |
| `content.events` | Elementtyp | Veranstaltungskalender: Monat/Woche/Liste, Serien, Anmeldung mit Warteliste, E-Mail-Erinnerungen, iCal-Abo, weitere Kalender und iCal-Abonnements als Quellen, Makro „Nächste Termine“ |
| `content.catlist` | Elementtyp | Kategorieübersicht: Unterkategorien einer Kategorie oder der obersten Ebene als Karten oder Liste, optional mit Seiten (z. B. als Startseite) |
| `content.team` | Elementtyp | Team-Seite aus Gruppen mit wählbaren Angaben aus der Visitenkarte bzw. dem Identity Provider. Eine Gruppe fügt nur hinzu, wer sie und ihre Mitglieder auflisten darf (eigene Gruppen mit `core.groups.list_own` und `core.users.list_own`, alle mit `core.groups.list_all` und `core.users.list_all`); vorhandene Gruppen bleiben für alle, die die Seite bearbeiten. **Gäste** sehen nur Name, Funktion, Abteilung und Foto (nach der Foto-Regel der Person); Kontaktdaten (E-Mail, Telefon, Mobilnummer, Büro, „Über mich“, Gruppen, Attribute) nur, wenn die Seite *Kontaktdaten auch Gästen zeigen* hat. Das setzt nur, wer das Recht *Kontaktdaten öffentlich zeigen* (`content.team.public`) hat; ohne das Recht bleibt eine öffentliche Seite öffentlich, solange Gruppen und Felder gleich bleiben. Attribute des Identity Providers (`attr:…`) wählt nur, wer Benutzer verwaltet (`core.users.manage`). Dazu die Bausteine *Profilkarte* und *Personen einer Gruppe* für beliebige Seiten. Sie zeigen nur Personen, die die lesende Person auflisten darf; Gäste sehen keine. |
| `content.notes` | Elementtyp | Gemeinsame Notizen: direkt in der Seitenansicht live gemeinsam schreiben, automatisch gespeichert (Besprechungen, Brainstorming, Aufgabenlisten) |
| `search.external` | Allgemein | OpenSearch/Elasticsearch oder Meilisearch für die Volltextsuche |
| `theme.wiki` | Template | Dokumentations-Layout mit Seitenbaum |
| `theme.cms` | Template | Website, klassisch (Hochschule, Organisation): Hauptnavigation, transparenter Kopfbereich über dem Startbild, Titel-Banner, Footer-Spalten |
| `theme.cms.modern` | Template | Website, modern und neutral: erbt Aufbau, Positionen und Einstellungen von `theme.cms`, eigene Gestaltung (Schrift Inter, Glas-Kopfbereich, linksbündiges Startbild, abgerundete Karten) |
| `theme.wiki.dark` | Template (Beispiel) | erbt von `theme.wiki`, ändert nur die Farben |
| `example.notice` | Elementtyp (Beispiel) | Hinweisbox, erbt von `core.html` |

Eigene Plugins entwickeln: [Plugins entwickeln](../developer/plugins.md)
