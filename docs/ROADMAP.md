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

## Meilenstein 2 – Unternehmens-Anbindung und Dateien

- `auth.ldap` (LDAP/Active Directory) mit Attribut- und Gruppen-Mapping
- `auth.oidc` (OpenID Connect: Keycloak, Entra ID, …) mit Claim- und Gruppen-Mapping
- `auth.shib` (SAML 2.0 nativ und Header-Modus für Apache `mod_shib`)
- Zentrales Mapping externer Gruppen/Attribute auf lokale Gruppen und Rollen
- Elementtyp `core.attachment` (PDF, Office, Bilder) mit Vorschau, Versionierung und Volltext aus PDFs. Speicherung im Dateisystem (hash-basiert), S3-fähige Schnittstelle
- Bilder und Anhänge direkt im Editor einfügen
- optional 2-Faktor-Anmeldung (TOTP) für lokale Konten

## Meilenstein 3 – Abläufe, Makros, CMS

- Elementtyp `workflow.process`: konfigurierbare Schritte (z. B. Bewerbungsverfahren) mit Rechten je Schritt, Freigaben per Klick, Notizen, Verlauf
- Makro-System für Seiten (Plugin-basiert): Inhaltsverzeichnis, Unterseiten-Liste, Info/Warnung/Tipp, Aufklappbereich, Code-Block, Spalten/Layout, Status-Label, Ankündigung. Dazu **Shell-Befehl** (nur vom Superadmin exakt freigegebene Befehle, Freigabe über einen Hash, Timeout, unprivilegierter Benutzer, standardmäßig aus) und **HTTP-Abruf** (Allow-Liste, interne Adressen gesperrt)
- Kommentar-Plugin für Elemente
- CMS-/Website-Template (Sektionen, Hero, Layout-Baukasten)
- Plugin-Marktplatz: Versionsprüfung über GitHub-Releases, Update-Hinweise und 1-Klick-Update
- Optionale Such-Anbindung (OpenSearch/Meilisearch) für sehr große Datenbestände
