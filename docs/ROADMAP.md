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
- Menüs (`core.menu`), Template `theme.confluence` (+ Beispiel `theme.confluence.dark`)
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

Offen: Benachrichtigungen im Browser (ohne E-Mail), Zusammenfassungen (Digest), Vertretungsregeln, Webhooks, deren Antwort Daten in den Vorgang zurückschreibt.
