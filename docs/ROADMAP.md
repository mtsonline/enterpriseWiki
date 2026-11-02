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

## Meilenstein 6 – Gäste und Suchmaschinen, Kalender, Zusammenfassungen, Webhooks (fertig, v7.5)

- **Vorrendern für Gäste** (`ui.prerender`, Standard an): Seiten, Kategorien, Startseite und Kurz-URLs kommen mit Titel, Beschreibung, kanonischer Adresse, Open-Graph-Angaben, Sprache und dem Inhalt als HTML; die App ersetzt ihn beim Start. Immer aus Sicht eines Gastes, im Gäste-Cache gehalten. Prüfen unter *Administration → Einstellungen → Gast-Ansicht anzeigen* bzw. `GET /api/v1/prerender?path=`.
- **iCal**: VTIMEZONE-Definitionen für alle Zeitzonen von Serien (jährliche Regeln, bei unregelmäßigen Zonen einzelne Daten), geänderte Einzeltermine als `RECURRENCE-ID`.
- **Einzelne Termine einer Serie ändern** (Titel, Ort, Beschreibung, Zeit, bis 31 Tage verschoben) und zurücksetzen; Anmeldungen bleiben, Angemeldete erhalten bei neuer Zeit oder neuem Ort eine E-Mail. API `PUT/DELETE …/events/{id}/occurrences/{datum}`.
- **Beobachtungen als tägliche oder wöchentliche Zusammenfassung** (`frequency`), Uhrzeit, Wochentag und Zeitzone unter *Administration → E-Mail*; CLI `ewiki watch add … --frequency daily`.
- **Webhooks** für Microsoft Teams (Adaptive Card für Workflows), Slack und JSON (HMAC-signiert): Seiten- und Kategorieänderungen, Anmeldungen und Abmeldungen, geänderte Termine; Filter nach Ereignis und Kategorie, Warteschlange mit Wiederholungen, Zustellprotokoll, Test und „Erneut senden“. *Administration → Webhooks*, API `/api/v1/admin/webhooks`, Plugins über `host.Webhooks.Emit`.
- Zweite Website-Vorlage `theme.cms.modern` (neutral, modern), die bisherige heißt „Website – klassisch (Hochschule, Organisation)“.

Offen / Vorschläge:

- E-Mail-Einladungen (iTIP, `METHOD:REQUEST`) für Termine, damit sie direkt im Outlook-Kalender landen.
- Webhooks auch für Kommentare und abgeschlossene Workflows.
- Open-Graph-Bild je Seite (erstes Bild oder Startbild der Website-Vorlage) für schönere Link-Vorschauen.
