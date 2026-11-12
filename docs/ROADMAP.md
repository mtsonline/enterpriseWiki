# Roadmap

Die Umsetzung erfolgt in Meilensteinen. Jeder Meilenstein ist lauffähig und durch die CI getestet.

## Meilenstein 1 – Kern (fertig, v7.0)

- Plugin-System (Go und Frontend), Aktivieren/Deaktivieren zur Laufzeit, Installation aus GitHub, Git oder Ordner, Neubau und Neustart aus der Oberfläche, Vererbung von Plugins
- Kategorien (unbegrenzte Tiefe, stabile IDs), Elemente, Standardseite, Verschieben, Duplizieren, Sortieren, Papierkorb
- Elementtyp `core.html` (WYSIWYG/Markdown) und Beispiel `example.notice`
- Versionierung aller Inhalte, Wiederherstellen, Zurücksetzen auf einen Zeitpunkt (einzeln oder je Ebene), Audit-Protokoll, Aufbewahrungsregeln
- Mehrsprachigkeit von Inhalten (mit Übersetzungsstatus) und Oberfläche (Übersetzungseditor mit Fortschritt)
- Berechtigungen für Benutzer, Gruppen, Rollen, Gäste und Angemeldete mit Vererbung
- Lokale Anmeldung (immer zuerst), Sessions, API-Keys, SSH-Schlüssel-Login
- Volltextsuche, Tags
- Menüs (`core.menu`), Template `theme.wiki` (+ Beispiel `theme.wiki.dark`)
- Admin-Bereich: Benutzer, Gruppen, Rollen, Plugins, Einstellungen (in `config.yaml`), Sprachen, Protokoll, Menüs
- CLI mit Zugriff auf jede API-Funktion, Setup-Assistent, Docker, systemd-Installer, Update-Skript mit Backup und Rollback
- Tests: Unit, API-Integration (inkl. Prüfung der Frontend-Anbindung jeder Route), Browser-E2E mit WCAG-Prüfung (axe), GitHub Actions

## Meilenstein 2 – Unternehmens-Anbindung und Dateien (fertig, v7.1)

- `auth.ldap` (LDAP/Active Directory) mit Attribut- und Gruppenabfrage, Test-Login im Admin-Bereich
- `auth.oidc` (OpenID Connect mit PKCE: Keycloak, Entra ID, …) mit Claim- und Gruppen-Mapping
- `auth.shib` (SAML 2.0 nativer Service Provider und Header-Modus für Apache `mod_shib`)
- Einheitliche Zuordnung externer Gruppen auf lokale Gruppen und Rollen für alle Verfahren, Synchronisation bei jeder Anmeldung
- Elementtyp `core.attachment` (PDF, Office, Bilder, Video) mit Vorschau, Versionierung und Volltext aus PDFs/Textdateien. Inhaltsadressierte Ablage im Dateisystem, Aufräumen ungenutzter Dateien
- Bilder im Editor einfügen (mit Pflicht-Alternativtext)
- 2-Faktor-Anmeldung (TOTP) für lokale Konten, Rücksetzung durch Administratoren
- generisches Einstellungsformular für Plugins (`pluginSettingsForm`) mit Maskierung von Geheimnissen

Offen aus diesem Bereich: S3-kompatibler Objektspeicher als Alternative zum Dateisystem, Vorschau von Office-Dokumenten (Konvertierung), Single Logout.

## Meilenstein 3 – Abläufe, Makros, CMS (fertig, v7.2)

- Elementtyp `workflow.process`: Startformular (Text, E-Mail, Datum, Zahl, Auswahl, Bestätigung, Datei-Upload), Schritte mit Zuständigen (Starter, Benutzer, Gruppe, Rolle), Aktionen mit optionaler oder Pflicht-Notiz, Endzustände mit Ergebnis. Jeder Vorgang speichert eine Momentaufnahme des Ablaufs, jeder Schritt wird protokolliert. „Meine Aufgaben“ in der Seitenleiste und unter `/tasks`. Strukturierter Editor mit Vorlagen (Bewerbung, Genehmigungsantrag)
- Makro-System (Plugin-basiert, `sdk.Host.RegisterMacro`): Makros werden bei jeder Anzeige mit den Rechten des Betrachters gerendert, Makro-Dialog im Editor (WYSIWYG und Markdown)
  - `core.macros`: Inhaltsverzeichnis, Unterseiten, Zuletzt geändert, Hinweis-Boxen (Info, Notiz, Tipp, Warnung, Erfolg), Aufklappbereich, Status-Label, Spalten
  - `core.layout`: Abschnitt (Hintergrund, Breite), Hero-Banner, Karten-Raster, Button-Link, Abstand
  - `macro.shell`: Shell-Befehl; nur exakt freigegebene Befehle (SHA-256 des Befehlstexts), Freigabe nur durch Superadmins aus `config.yaml` oder die CLI, Timeout, eigene Prozessgruppe, unprivilegierter Benutzer, begrenzte Ausgabe, standardmäßig aus
  - `macro.http`: HTTP-Abruf als Text, bereinigtes HTML oder JSON (Wert, Liste, Tabelle); Allow-Liste, interne Adressen nach der DNS-Auflösung gesperrt, Weiterleitungen geprüft, Größen- und Zeitlimits, Zwischenspeicher
- Sicherheitseinstellungen der Makros nur in `config.yaml` (nicht über Oberfläche oder API änderbar)
- `core.comments`: Kommentare und Antworten unter Seiten, Bearbeiten und Löschen mit Audit-Protokoll, gelöschte Kommentare bleiben als Platzhalter
- Template `theme.cms` (Website-Layout mit Hauptnavigation, Off-Canvas-Menü, Footer-Spalten), Layout-Bausteine laufen über die volle Breite
- Update-Prüfung über GitHub-Releases (Kern und Plugins, auch GitHub Enterprise), Versionshinweise, 1-Klick-Update für Plugins (Oberfläche, API, `ewiki plugin updates|update`)
- Frontend-SDK: `registerWidget` (interaktive Teile in serverseitig gerenderten Inhalten), `registerElementExtension` (Bereiche unter Seiten), Plugin-Routen über `route`

Offen aus diesem Bereich: Vertretungen in Workflows, automatisches Kern-Update aus der Oberfläche.

## Meilenstein 4 – E-Mail, Workflow-Schritte, externe Suche (fertig, v7.3)

- E-Mail im Kern: Postausgang in der Datenbank (transaktional), Hintergrundversand mit Wiederholungen, SMTP (STARTTLS, TLS, PLAIN/LOGIN), sendmail, Log; ohne Mailserver bleiben Nachrichten erhalten und werden nachgesendet. Verwaltung unter *Administration → E-Mail* (Einstellungen, Testnachricht, Postausgang), Einrichtung per Umgebungsvariablen im Docker-Setup
- Benachrichtigungen: neue Workflow-Aufgaben, Erinnerungen, Abschluss/Abbruch eigener Vorgänge, Kommentare und Antworten; nur an Berechtigte, je Benutzer abschaltbar
- Workflow-Schrittarten: Aufgabe, Freigabe (Vier-Augen, Anzahl oder alle), Bedingung (Verzweigung nach Formularangaben), E-Mail senden (Empfänger, Cc, Absender, Antwortadresse, Betreff, Text mit Platzhaltern für Formular, Antragsteller, Bearbeiter usw.), Webhook (signiert, mit Wiederholungen und Fehlerpfad), Wartezeit (fest oder bis zu einem Datum aus dem Formular), Ende; Erinnerungen nach X Tagen; Vorlagen Bewerbung, Genehmigung, Einarbeitung
- Hintergrundjobs für Kern und Plugins (`RegisterWorker`), im Cluster koordiniert
- `search.external`: OpenSearch/Elasticsearch oder Meilisearch mit automatischer Synchronisation über ein Änderungsprotokoll, Neuaufbau, Rückfall auf PostgreSQL, Rechteprüfung im Wiki

Nachbesserungen nach dem ersten Test (v7.3.1): *Kategorie bearbeiten* und *Seite bearbeiten* direkt in der Kategorieansicht, Menü-Dialog mit Positionen der aktiven Vorlage, Kategorie-Auswahl und Prüfung der Einträge, Anzeige der verwendeten Vorlage auf der Plugin-Seite, mehr Positionen in beiden Vorlagen, Positionsvorschau, Plugin `core.blocks` für Textblöcke an jeder Position.

Offen: Benachrichtigungen im Browser (ohne E-Mail), Zusammenfassungen (Digest), Vertretungsregeln, Webhooks, deren Antwort Daten in den Vorgang zurückschreibt.

## Meilenstein 5 – Zuverlässige E-Mail, Beobachten, Feeds, neue Elementtypen, Gäste-Cache (fertig, v7.4)

- E-Mail-Warteschlange für große Mengen: Status pro Empfänger, Annahme erst mit `250`, vorübergehende Fehler (4xx, Netzwerk) ohne Limit wiederholt, endgültige Fehler (5xx) und Rückläufer pro Empfänger mit Meldung an den Absender bzw. die auslösende Person. Mehrere Instanzen senden parallel (Lease, `SKIP LOCKED`), wiederverwendete SMTP-Verbindungen, `concurrency` und `rate_per_minute`. Bounce-Postfach per IMAP mit VERP und DSN-Auswertung. `mail.enabled` schaltet E-Mail ganz ab. API und CLI: `ewiki mail status|list|show|retry|delete|bounces`, `ewiki jobs`.
- Beobachten von Kategorien (optional mit Unterkategorien) und Seiten: E-Mail mit Link oder neuem Inhalt, gesammelt nach 2 Minuten Ruhe, nur Inhalte, die die Person sehen darf. API `me/watches`, CLI `ewiki watch`.
- Menüs: Ziele Kategorie, Element (jeder Typ), freier Link, anderes Menü (mehrstufig) und Überschrift. Auswahl vorhandener Inhalte oder „Neu anlegen“, Titel des Ziels als Beschriftung, Schutz vor Schleifen. Darstellung als Ausklapp-Menüs mit Flyouts, als Baum in der Seitenleiste und mobil, in beiden Vorlagen.
- Atom/RSS: Feed je Kategorie (Einstellung), für Blog und FAQ; persönliche Feed- und Kalender-Links mit erneuerbarem Schlüssel. Suche innerhalb einer Kategorie (`/api/v1/search?category=`).
- Elementtypen `content.blog`, `content.faq`, `content.events` (Veranstaltungskalender mit Serien, Anmeldung, Warteliste, Erinnerungen, iCal, Makro „Nächste Termine“), `content.team` mit der neuen Visitenkarte im Profil.
- Optionaler Gäste-Cache: API-Antworten im Speicher, Seiten mit eingebetteten Daten (eine Anfrage, keine Datenbank). Jede Änderung leert den Cache, über die Datenbank auch im Cluster und von der Kommandozeile aus. *Administration → Cache*, `ewiki cache`.

Offen / Vorschläge:

- Serverseitiges Vorrendern des Seiteninhalts für Gäste: Suchmaschinen und Besucher ohne JavaScript sehen dann den vollen Text, nicht nur die Vorab-Daten.
- E-Mail-Einladungen (iTIP, `METHOD:REQUEST`) für Termine und VTIMEZONE-Blöcke im iCal-Export für ältere Outlook-Versionen.
- Ausnahmen in Serien bearbeiten („nur diesen Termin ändern“), nicht nur absagen.
- Tägliche oder wöchentliche Zusammenfassung statt sofortiger Beobachtungs-E-Mails, wählbar je Beobachtung.
- Webhooks für Ereignisse (Seite geändert, Anmeldung zu einem Termin) für Teams/Slack.

## Nachbesserungen v7.4.1

Administration → Elemente (alle Elemente suchen, filtern, bearbeiten, verschieben, löschen); Element-Auswahl mit Suche über alle Kategorien (Kategorie nur als Filter), filterbare Kategorie-Auswahl; „Neues Element“ fragt nur Typ und Titel, Blog/FAQ legen ohne Quelle eine Kategorie an; Kurz-URLs (`/kalender`) für Elemente und Kategorien; Startseite wählbar (Übersicht, Kategorie, Element) und Elementtyp `content.catlist` (Kategorieübersicht).

## Nachbesserungen v7.4.2

Menü-Editor zeigt nur die Liste der Einträge, „Bearbeiten“ öffnet einen einzelnen Eintrag (inkl. übergeordnetem Eintrag); Wiki-Vorlage heißt jetzt `theme.wiki`/`theme.wiki.dark` (alte IDs werden beim Start automatisch übernommen), schmalere und ein-/ausblendbare Seitenleiste; Website-Vorlage `theme.cms` neu gestaltet (transparenter Kopfbereich über dem Startbild, Titel-Banner auf Unterseiten, Footer-Spalten, „Nach oben“-Knopf, Schrift Source Sans 3) mit eigener Einstellungsseite *Administration → Website-Vorlage*; Einstellungsformulare kennen Bild- und Farbfelder.

## Lizenzierung (v7.5)

Business Source License 1.1 mit kostenloser privater Nutzung durch natürliche Personen, Lizenzschlüssel mit Offline-Prüfung (*Administration → Lizenz*, `ewiki license`), Beitragsvereinbarung (CLA) für Mitwirkende und Katalog-Plugins, Lizenzliste der enthaltenen Komponenten (`/licenses.txt`) und Lizenzprüfung neuer Abhängigkeiten in der CI.

## Meilenstein 6 – Gäste und Suchmaschinen, Kalender, Zusammenfassungen, Webhooks (fertig, v7.5)

- **Vorrendern für Gäste** (`ui.prerender`, Standard an): Seiten, Kategorien, Startseite und Kurz-URLs kommen mit Titel, Beschreibung, kanonischer Adresse, Open-Graph-Angaben, Sprache und dem Inhalt als HTML; die App ersetzt ihn beim Start. Immer aus Sicht eines Gastes, im Gäste-Cache gehalten. Prüfen unter *Administration → Einstellungen → Gast-Ansicht anzeigen* bzw. `GET /api/v1/prerender?path=`.
- **iCal**: VTIMEZONE-Definitionen für alle Zeitzonen von Serien (jährliche Regeln, bei unregelmäßigen Zonen einzelne Daten), geänderte Einzeltermine als `RECURRENCE-ID`.
- **Einzelne Termine einer Serie ändern** (Titel, Ort, Beschreibung, Zeit, bis 31 Tage verschoben) und zurücksetzen; Anmeldungen bleiben, Angemeldete erhalten bei neuer Zeit oder neuem Ort eine E-Mail. API `PUT/DELETE …/events/{id}/occurrences/{datum}`.
- **Beobachtungen als tägliche oder wöchentliche Zusammenfassung** (`frequency`), Uhrzeit, Wochentag und Zeitzone unter *Administration → E-Mail*; CLI `ewiki watch add … --frequency daily`.
- **Webhooks** für Microsoft Teams (Adaptive Card für Workflows), Slack und JSON (HMAC-signiert): Seiten- und Kategorieänderungen, Anmeldungen und Abmeldungen, geänderte Termine; Filter nach Ereignis und Kategorie, Warteschlange mit Wiederholungen, Zustellprotokoll, Test und „Erneut senden“. *Administration → Webhooks*, API `/api/v1/admin/webhooks`, Plugins über `host.Webhooks.Emit`.
- Zweite Website-Vorlage `theme.cms.modern` (neutral, modern), die bisherige heißt „Website – klassisch (Hochschule, Organisation)“.

## Meilenstein 7 – Termineinladungen, mehr Webhooks, Vorschaubilder

- **Termineinladungen** (iTIP, RFC 5546): Anmelde-, Nachrücker- und Änderungs-Mails enthalten eine Kalendereinladung (`METHOD:REQUEST`), Abmeldung und Absage eine Stornierung (`METHOD:CANCEL`). Outlook, Google und Apple Kalender tragen den Termin direkt ein bzw. aktualisieren oder entfernen ihn; jeder Termin einer Serie hat eine eigene UID. Abschaltbar je Kalender (*Keine Kalendereinladungen anhängen*). Wird eine Serie verschoben oder ihr Ort geändert, erhalten Angemeldete künftiger Termine eine aktualisierte Einladung.
- **Webhooks** für neue Kommentare (`comment.create`: Autor, Textanfang, Antwort ja/nein) und Abläufe (`workflow.started`, `workflow.completed` mit Ergebnis, `workflow.cancelled`); Kategoriefilter wie bei Seiten. Ablauf-Ereignisse werden in derselben Transaktion wie die Änderung eingereiht.
- **Vorschaubild je Seite** für Link-Vorschauen (`og:image`, `twitter:card`): unter *Eigenschaften* hochladen, aus den Bildern der Seite wählen oder eine Adresse angeben; sonst das erste Bild der Seite, sonst das Vorschaubild der Website (`ui.preview_image`). API `PATCH /api/v1/elements/{id}` mit `preview_image`.

## Meilenstein 8 – Plugins in den Docker-Images

- Zwei Images: **Lite** (`latest`, `7.5`; nur das Programm) und **Full** (`full`, `7.5-full`; zusätzlich Quellcode, Go, Node.js und alle Module des Kerns für Builds ohne Internet). Beide vorerst privat.
- Full-Image: Plugins aus Oberfläche, CLI, API oder dem Ordner `./data/wiki/plugins` werden im Datenordner gebaut (`bin/ewiki-<Kennung>`) und überstehen Updates und neue Container; nach einem Image-Update wird beim Start automatisch neu gebaut (Wartungsseite währenddessen, bei Fehlern Start ohne die Zusatz-Plugins).
- Installieren, Aktualisieren und Entfernen bauen und starten automatisch neu; in den Ordner kopierte Plugins werden automatisch gebaut und nach **Neu starten** aktiv (oder automatisch mit `plugins.auto_restart_on_drop`).
- Cluster: ein Knoten baut (Dateisperre), die anderen übernehmen das Ergebnis; Neustart nacheinander (Datenbank-Lease `cluster_lease`), das Wiki bleibt erreichbar.
- Lite-Image: Installieren ist in der Oberfläche deaktiviert, mit Hinweis auf das Full-Image.

## Meilenstein 9 – Rechte, Protokoll, Entwürfe, Benachrichtigungen

- **Granulare Rechte:** globale Rechte für Rollen (Benutzer, Gruppen, Rollen, Einstellungen, Plugins, E-Mail, Webhooks, Lizenz, System, Protokoll, Übersetzungen, Papierkorb, Elementübersicht, Beobachtungen zuweisen, globale Vorlagen) und Standardrechte aller Angemeldeten; Kategorierechte mit Vorlagen **Leser**, **Kategorie-Editor**, **Kategorie-Manager**; globales Recht **Kategorie-Administrator** (oberste Ebene, Struktur und Rechte aller Kategorien, Inhalte nur mit Ansehen).
- **Plugins deklarieren ihre Rechte** im Manifest (`permissions`: Schlüssel, Bereich, englische Bezeichnung, Vorlagen, `implies`); undeklarierte Rechte werden abgelehnt. Mitgeliefert: Workflows (anlegen/eigene ändern, Workflow-Administrator mit detaillierter Protokollierung und Benachrichtigung des Erstellers, Vorgänge verwalten), Teilnehmerlisten, Kommentar-Moderation, Menüs, Blöcke, Shell-Makros, externe Suche.
- **Zentrale Rechtevergabe** unter *Administration → Berechtigungen* (direkte und wirksame Rechte je Benutzer, Gruppe, Rolle mit Herkunft). Standard: Unterkategorien erben, neue Kategorien der obersten Ebene gehören nur ihrem Ersteller.
- **Workflow-Schritte** führen nur die eingetragenen Zuständigen aus.
- **Entwürfe, Vorschau, Vorlagen, Veröffentlichen mit Optionen** (Zielkategorie, Zeitpunkt, Beobachter benachrichtigen, Erweiterungspunkt für Plugins wie „als Neuigkeit“ in M11).
- **Benachrichtigungen im Wiki** (Glocke) und per E-Mail, **Teilen** (Plugin `core.share`, ändert keine Rechte), **@Erwähnungen** (Plugin `core.mentions`), **zugewiesene Beobachtungen** (verpflichtend oder optional).
- **Protokoll:** IP, Browser, Kanal; Anmeldungen, abgewiesene Zugriffe, optional Seitenaufrufe; einstellbar, was protokolliert wird; Weiterleitung an Syslog, Webhook, OpenTelemetry, Datei; Such- und Filterseite mit Export.
- **Übersetzungsfortschritt** zählt die Originalsprache nicht mehr mit; Plugins müssen `locales/en.json` mitliefern.
- CLI: `ewiki perm`, `ewiki audit`, `ewiki draft`, `ewiki inbox`, `ewiki watch assign`.

**Verhaltensänderungen:** Nur-„Erstellen“ erlaubt keine Unterkategorien mehr (dafür *Verwalten*). Neue Kategorien der obersten Ebene sind nicht mehr automatisch für Gäste sichtbar. Administratoren führen Workflow-Schritte nur aus, wenn sie zuständig sind.

## Meilenstein 10 – Übersetzungen

- **Pluralformen** nach CLDR (`key_one`, `key_other`, `key_few` …), gewählt über `count`/`n` – im Frontend mit `Intl.PluralRules`, im Backend mit `golang.org/x/text`. Kern und Plugins sind umgestellt.
- **Rechte je Sprache:** für jede konfigurierte Sprache `core.i18n.translate.<code>`; `core.i18n.manage` erlaubt alle Sprachen und das Hinzufügen/Entfernen.
- **Übersetzungsseite:** Fortschritt je Sprache und Komponente, Filter fehlend/veraltet/hier übersetzt, Suche, Seiten, Felder je Pluralform mit Beispielzahlen, *Weiterhin korrekt*.
- **Veraltete Übersetzungen:** eigene Übersetzungen merken sich den englischen Text; ändert er sich (z. B. durch ein Plugin-Update), gelten sie als veraltet.
- **Import/Export:** JSON, XLIFF 2.0 und PO, je Sprache und optional je Komponente (UI, API, `ewiki i18n`).
- **Kategorie-Übersetzungen** wie bei Seiten: Originalsprache, Status aktuell/veraltet/fehlt, Fortschritt, Knopf *Übersetzungen* in der Kategorie, API `PUT /api/v1/categories/{id}/translations/{lang}`, CLI `ewiki category translations|translate`.
- Sprachnamen für neue Sprachen kommen aus dem Browser (`Intl.DisplayNames`).

## Meilenstein 11 – Komponenten

- **Reaktionen** (`core.reactions`): Emojis auf Seiten, Kategorien und Kommentaren, freie Auswahl mit Sperrliste, wer reagiert hat, Benachrichtigung des Autors (E-Mail standardmäßig aus), Verwaltung der Reaktionen anderer.
- **Lesbare Adressen** `/wiki/<Kategorien>/<Seite>` **je Sprache** mit Verlauf: umbenannte und verschobene Seiten und geänderte Kurz-URLs leiten dauerhaft (301) weiter; die Adresszeile zeigt die Adresse in der Sprache des Benutzers. Abschaltbar (`ui.readable_urls`), je Seite „Nur diese Kurz-URL“.
- **„Seite nicht gefunden“** zeigt gelöschte Seiten (mit Papierkorb-Link) und „Meinten Sie?“-Vorschläge.
- **Favoriten** (`core.favorites`): Stern, Liste an einer Position der Vorlage und im Profil.
- **Seiteneigenschaften** (`core.properties`) mit Makro „Seiteneigenschaften-Bericht“.
- **Karussell** (`core.carousel`): Neuigkeiten über „Als Neuigkeit“ beim Veröffentlichen, eigene Folien, externe RSS/Atom-Feeds; barrierefrei (Pause, Tastatur, reduzierte Bewegung).
- **Mega-Menü** (`core.megamenu`, optional) als Darstellung für Menüs; Menüeinträge haben Beschreibungen.
- SDK: Kategorie- und Profil-Erweiterungen, Bereiche über dem Seiteninhalt, Reaktionsleisten, Menü-Darstellungen; Benachrichtigungsarten mit E-Mail standardmäßig aus.

## Geplant

- **Meilenstein 12 – Multi-Site (fertig, Branch m18):** mehrere Sites in einer Instanz mit eigenen Inhalten und Einstellungen (Hostname, Theme, Speicher, Anmeldung; Editor-Standards mit dem neuen Editor in M15). Jede Site hat ein eigenes PostgreSQL-Schema mit allen Tabellen; Benutzer, Gruppen, Rollen, Sitzungen und Lizenz liegen in einem gemeinsamen Schema. Rechte pro Site, Anmeldung pro Site getrennt (eigene Anbieter und Sitzungen, gemeinsame Konten), Instanz-Admins (Sites, Benutzer, Lizenz) und Site-Admins (nur ihre Site), Plugins pro Site ein- und ausschaltbar. Die Lizenz zählt die Benutzer der ganzen Instanz.
- **Meilenstein 13 – Medien (fertig, Branch m19):** Medienbibliothek in Kategorien; Kategorien können unsichtbar sein (frei verwaltbare „Ordner“, z. B. für Medien). Rechte eines Mediums: standardmäßig die der Kategorie, zusätzlich sehen es alle, die ein Element sehen dürfen, in dem es eingebunden ist; beim Hochladen lassen sich eigene Rechte festlegen, die beides ersetzen (nur Rechte, die man selbst hat). Einfügen im Editor, „nur anzeigen“ (kein Download-Knopf, kein direkter Link, Kontextmenü gesperrt) für Benutzer ohne Download-Recht oder wenn beim Hochladen abgeschaltet. Wasserzeichen beim Hochladen wählbar: keins; Original nicht speichern, nur mit Wasserzeichen des Hochladers; Anzeige mit Wasserzeichen des Hochladers, Original für Berechtigte; Name des Betrachters beim Anzeigen einbrennen; Name des Betrachters beim Anzeigen und Herunterladen einbrennen (Benutzer und Datum vorausgewählt, einzeln abwählbar, optionaler Kommentar). Bildeditor (Zuschneiden, Skalieren, Drehen, Farben, Weichzeichnen) mit Versionsverlauf, Einbindungen zeigen die neue Version. Galerie-Makro. Registrierung von Anzeigeprogrammen (intern; Office-Plugins ab M14) mit Standard-Anzeige in den Einstellungen.
- **Meilenstein 14 – Office (fertig, Branches m14-office, m14b-office-compose; Compose-Profile collabora/onlyoffice mit CI gegen echte Server; dazu Demo-Inhalte und Zurücksetzen der Site):** WOPI (Collabora, OnlyOffice, Microsoft 365) und OnlyOffice-Schnittstelle. Office-Plugins registrieren sich als Anzeige-/Bearbeitungsprogramm (bearbeiten, schreibgeschützt anzeigen); in den allgemeinen Einstellungen wird die Standard-Anzeige gewählt (intern oder eines der aktiven Office-Plugins), „Dateien immer im externen Programm öffnen“ ist nur für eines erlaubt. Mit Download-/Bearbeitungsrecht öffnet sich das Office-Programm, sonst schreibgeschützt dort (wenn im Plugin aktiviert) oder im Browser.
- **Meilenstein 15 – Neuer Editor und Live-Bearbeitung (fertig, Branch m15-editor):** visueller und Markdown-Modus speichern immer Markdown (GitHub-Markdown); was Markdown nicht kann (z. B. Schriftfarben, verbundene Zellen), speichert der visuelle Editor als Makro in lesbarer Schreibweise (`::: toc{levels=3}`); Makros sind in beiden Modi editierbar. Bestehende HTML-Inhalte werden einmalig umgewandelt. Yjs-Zusammenarbeit mit Cursorn, Notizen-Plugin (live gemeinsam). Demo-Inhalte mit allen Core-Plugins und Makros. Der bisherige einfache Editor bleibt als Alternative (Vorgabe durch Admin, pro Benutzer wählbar). Dazu: Tag-Feld im Editor, Farben/Schriften als Direktiven aus der Template-Palette, Makro-Plugins können einen eigenen Editor-Button anmelden, FAQ-Bearbeitungsknöpfe, Megamenü in der Demo.
- **Meilenstein 15b – Identität und Anmeldung (vorgezogen, vor echten Benutzern):** Benutzer mit mehreren eindeutigen E-Mail-Adressen (ein Benutzer, ein Profil) und optionalem eindeutigem Benutzernamen; automatische Verknüpfung einer neuen Anmeldung nur bei vom Anbieter bestätigter E-Mail und vom Admin als vertrauenswürdig markiertem Anbieter, sonst Bestätigung mit dem bestehenden Login; E-Mails verwalten (Profil, API, CLI, Bestätigungslink); Benutzer vorab anlegen und zuordnen (Admin, API, CLI, SCIM 2.0); JIT pro Anbieter-Instanz; Rollen-/Gruppen-Mapping pro Anbieter-Instanz auf Claims/Attribute; Sitzungen mit Leerlauf-Timeout, „meine Sitzungen“, OIDC Back-Channel-/RP-Logout, SAML-SLO; MFA als Plugins (TOTP, E-Mail, WebAuthn/FIDO2, Passkeys) für alle Anmeldearten; Pflicht-Plugintyp im Manifest (element, macro, auth, mfa, storage, office, search, integration, template, general) und Admin-Bereich danach gruppiert; Sichtbarkeit der Profilbilder (Admin-Vorgabe oder fest, Benutzer innerhalb der Grenzen).
- **Meilenstein 15c – Eigenschaften, Quellen, Bereiche (in Arbeit, Branch m15c-properties):** Eigenschaften für alle Elementtypen (über 30 Typen, Definitionen global/pro Kategorie, freie Eigenschaften, Rechte pro Eigenschaft, Formeln, laufende Nummern, Verlauf, Schlagwörter als Standard-Eigenschaft), Eigenschaften-Datenbank und Eigenschaften-Bericht mit 13 Darstellungen (fertig); flexible Quellen für alle lesenden Plugins und Makros (Karussell, Kalender, Neuigkeiten, Blog, FAQ …) mit Sammel-Makros; Top-Level-Kategorien als Bereiche wie Confluence-Spaces (Template pro Kategorie, Schlüssel und Kurzadresse, Seitenleiste, Bereichsverwaltung, Funktionen ein/aus, Vorlagen, Archiv, Verzeichnis, Mitglieder, Zugriff anfordern, persönliche Bereiche, Einstufung, Analysen, Export/Import).
- **Meilenstein 15h/15i – Quellen-Baukasten (fertig):** Quellarten Suche und Eigenschaften-Filter; Quellen-Baukasten mit Bausteinen per Klick oder Drag & Drop in die Bereiche „Elemente aus“, „Ausschließen“ und „Muss auch passen“ (Textform `-`/`+`), Live-Vorschau der Trefferzahl mit den eigenen Rechten; Höchstzahl und Seiten (ab N Einträgen) für alle Listen aus Quellen, Blättern ohne Neuladen mit eigenem Link je Seite; Obergrenze `lists.max_items`.
- **Meilenstein 16a – Formulare:** Plugin für Webformulare (Elementtyp und Makro): Formular-Editor mit allen üblichen Feldern, Bedingungen, Prüfregeln, berechneten Feldern; Layout wählbar als barrierefreies Raster (empfohlen) oder pixelgenau; Uploads mit Einschränkungen, Zielkategorie und Prüfung (Dateityp, Office-Makros, PDF-JavaScript, optional ClamAV/ICAP); Speicherziele Eigenschaften-Datenbank, Ablage, Dateien, E-Mail, Workflow, Webhook; Anzeige-Makro für Einsendungen; Schutz öffentlicher Formulare; Rechte wie bei den anderen Plugins.
- **Meilenstein 16b – Workflow 2:** beliebig viele Auslöser (Formular, Knopf, Zeitplan, Bericht/Suche, Eigenschaft, Datei, Webhook, Verkettung …), automatische Aktionen an jedem Schritt und zum Abschluss (Benachrichtigungen, Webhook, Element/Eintrag/Datei erzeugen, Bericht, Rechte, Gruppen …), Fristen pro Vorgang und Schritt mit Warnungen und Eskalation, Benachrichtigungen vor/nach jedem Schritt mit Vorlagen, parallele Zweige, Vertretungen, Schrittformulare, Versionen, Testlauf, Auswertung.
- **Meilenstein 16 – Freigabe und Lebenszyklus:** (dazu neue Ablauf-Schritte: Datei hochladen/prüfen mit Sichtbarkeit für Folgeschritte, Shell-Befehl, S/MIME-Signatur, Parallel-Freigabe k von n, Bedingungen/Verzweigungen, Fristen mit Erinnerung und Eskalation, Webhook/HTTP, Unterabläufe, Dokument erzeugen) Recht „Änderungen vorschlagen“, Prüfer mit frei wählbarer Zahl nötiger Freigaben pro Kategorie, geplante Veröffentlichung, Anmerkungen zu Textstellen (erledigt markierbar); Feld „Verantwortliche“ (Personen/Gruppen, Standard Ersteller, von Kategorien vererbbar); Lebenszyklus-Regeln mit wählbarem Auslöser (nicht aktualisiert seit X Monaten oder Prüfdatum) und pro Seite/Kategorie wählbaren Aktionen (Banner „veraltet“, Verantwortliche benachrichtigen, Status „Prüfung nötig“, ins Archiv verschieben, aus der Suche nehmen, nach Y Monaten in den Papierkorb).
- **Meilenstein 17 – Zeichnen, Diagramme, Karten:** Excalidraw-Plugin (Elementtyp „Zeichnung“, Makro zum Einbetten, „Zeichnung einfügen“ im Editor, nachträglich bearbeitbar, SVG mit Alternativtext, eigene Rechte, wird nur beim Zeichnen geladen), Diagramm-Makro, interaktive Karten.
- **Meilenstein 18 – Umfragen, Feedback, Kalender:** Umfragen und Terminfindung mit Benachrichtigungen und Kalendereinträgen, Feedback-Plugin (Ideen, Kommentare, Abstimmungen), persönliche Kalenderverbindung (Google und Microsoft 365 per Anmeldung, iCloud und Nextcloud per CalDAV) für sofortige Übertragung unter Beachtung der Rechte.
- **Meilenstein 19 – Plugin-Verwaltung und Anmeldung:** Kategorie-Darstellung: Elemente pro Kategorie als „Standard“ markieren, je mit Position des Templates (neue Position „content“); Übersicht als eigener Baustein mit Darstellungstypen (Navigation, Liste, Karten, Kacheln, Baum, Tabelle, Akkordeon, Tabs, A–Z, Zeitleiste, alles untereinander). Einstellungen pro Plugin mit Vorgaben des Entwicklers; Position jedes Plugin-Bereichs wählbar aus allen Positionen des aktiven Templates (Vorgabe meist „Inhalt“; fehlt die Position nach einem Template-Wechsel, gilt wieder die Vorgabe); Plugins mit mehreren Bereichen (z. B. Befehlsleisten von Blog/FAQ) einzeln positionierbar. Anmelde-Plugins mehrfach aktivierbar (z. B. zwei OIDC-Anbieter). Anmeldung mit ID Austria (OIDC/SAML nach Registrierung als Service-Provider) und weiteren eIDs über eIDAS; EU-Digital-Identity-Wallet (OpenID4VP) als eigenes Plugin. Grundsatz für alle Plugins: Vorlagen für Benachrichtigungen, Bestätigungen und Downloads sind als Dateien im Quellcode und in der Oberfläche anpassbar.
- **Meilenstein 20 – PDF-Export:** Elemente (Seiten, Kategorien, Blog …) als PDF, als eigenes Plugin und als Option in „Teilen“.
- **Meilenstein 21 – Newsletter:** Plugin mit Befehlsmenü, Ordnerliste und Hauptbereich (einzeln positionierbar); Entwürfe, Vorlagen, geplanter Versand mit Warnung ohne geprüfte Version, visueller oder Markdown-Editor; Empfänger aus Benutzern, Gruppen, Rollen und externen Kontakten (verwaltbar, Double-Opt-in, Abmeldung mit einem Klick); Versand als eigenes Konto oder als freigegebene Absenderadressen; optional S/MIME-Signatur; Zustellstatus und Rückläufer; Öffnungs- und Klick-Statistiken mit Diagrammen.
- **Meilenstein 22 – Wahlen:** Wahl-Plugin mit Wahlzeitraum, Wahlkommission, verschachtelten Stimmzetteln, Wahlberechtigten (Benutzer, Gruppen, Rollen, Import, bestimmte Anmeldearten wie ID Austria), geheimer Stimmabgabe (verschlüsselte Stimmen, kein Zwischenstand, Entschlüsselung nur durch die Kommission), genau einer Stimme pro Person, prüfbaren Protokollen ohne Wahlgeheimnis, Ergebnisfreigabe und -veröffentlichung.
- **Später (Reihenfolge offen, siehe Planungsdokument):** Passwortschutz für Kategorien/Elemente/Medien; Ende-zu-Ende-Verschlüsselung mit persönlichem Post-Quanten-Schlüssel (ML-KEM hybrid, im Browser, Wiederherstellungscode zum Ausdrucken); Trust Store (k von n oder feste Kette, pro Trust Store wählbar); Föderation von Instanzen (Benutzer des fremden Wikis pro Föderation wählbar einzeln sichtbar oder nur als Gruppe, Vorgabe: einzeln sichtbar; Inhalte immer live abgefragt, nicht gespiegelt); Suche über Plugin-Inhalte (Termine, Team); manipulationssicheres Audit-Log mit WORM-Export; Integrationen (Jira, Video/Telefonie, Cloud-Speicher, KI mit MCP-Server, CampusOnline, Open Data, Social Media); Video-/Audio-Player; CSV-/Diagramm-Makro; Whiteboard und Sprachkanal; PWA; Export/Import von Instanzen; Confluence-Importer und Datenbanken; Template-Designer; Hochschul-Template.
- **Meilenstein 23 – Shop:** Produkte mit frei definierbaren Attributen und Varianten, Warenkorb und Checkout, Zahlungsanbieter als Plugins (Rechnung, SumUp, PayPal und weitere), Rechnungen.
- Danach: **Vorlage „Wiki (Intranet/Hochschule)“** und das Werkzeug für Plugin-Entwickler (Gerüst, Lint, Tests).
