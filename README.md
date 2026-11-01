# Enterprise Wiki

Ein selbst gehosteter, modularer Ersatz für Confluence: Wissensdatenbank mit beliebig tief verschachtelten Kategorien, versionierten und mehrsprachigen Seiten, fein granularen Berechtigungen und einem Plugin-System, über das praktisch alles erweitert werden kann (Elementtypen, Anmeldeverfahren, Menüs, Templates, Admin-Seiten, API-Endpunkte).

- **Backend:** Go, eine einzige statisch gelinkte Binärdatei (Server + CLI), PostgreSQL
- **Frontend:** TypeScript ohne Framework und **ohne Laufzeit-Abhängigkeiten**, in die Binärdatei eingebettet
- **Barrierefrei:** Ziel WCAG 2.2 AA, automatisch mit axe-core in der CI geprüft
- **Config-first:** `config.yaml` ist die einzige maßgebliche Quelle. Admin-Oberfläche und CLI schreiben in diese Datei.

> Status: **Version 7, Meilensteine 1 bis 4.** Was bereits enthalten ist und was noch folgt, steht in [docs/ROADMAP.md](docs/ROADMAP.md).

## Funktionen

| Bereich | Enthalten |
|---|---|
| Inhalte | Kategorien mit unbegrenzter Tiefe (stabile IDs, Gliederungsnummern 1.2.3), Standardseite je Kategorie, Elemente als Plugin-Typen, verschieben, duplizieren, sortieren, Papierkorb |
| Seiten | Elementtyp `core.html`: WYSIWYG-Editor oder Markdown, Bilder mit Pflicht-Alternativtext, serverseitige HTML-Bereinigung (XSS-Schutz), Vorschau |
| Dateien | Elementtyp `core.attachment`: PDF, Office, Bilder, Video. Vorschau im Browser, jede neue Datei ist eine Version, PDF- und Textinhalt in der Volltextsuche, inhaltsadressierte Ablage (SHA-256) |
| Versionierung | Jede Änderung ist eine unveränderliche Version, Wiederherstellen einzelner Versionen, Zurücksetzen ganzer Kategorien (mit/ohne Unterkategorien) auf einen Zeitpunkt, Audit-Protokoll, konfigurierbare Aufbewahrung |
| Mehrsprachigkeit | Inhalte je Sprache versioniert, Übersetzungsstatus (aktuell / veraltet / fehlt) mit Prozentanzeige, Fallback auf die Originalsprache; UI-Texte von Kern und Plugins im Admin-Bereich übersetzbar mit Fortschrittsanzeige |
| Berechtigungen | Regeln je Kategorie für Benutzer, Gruppen, Rollen, Gäste und angemeldete Benutzer, optional vererbt. Ohne Regel ist nichts erlaubt, Gäste sehen nur die erste Ebene. Prüfung im Backend bei jeder Anfrage, im Speicher gecacht |
| Anmeldung | Lokale Konten (Argon2id, Sperre nach Fehlversuchen, optional 2FA per TOTP) werden immer zuerst geprüft. **LDAP/Active Directory**, **OpenID Connect** (Keycloak, Entra ID, …) und **Shibboleth/SAML 2.0** (nativ oder über mod_shib) als Plugins, mit einheitlicher Zuordnung externer Gruppen zu lokalen Gruppen und Rollen. API-Keys (Ablaufdatum, Rechte-Maske, Kategorie-Beschränkung), SSH-Schlüssel-Login wie bei GitHub |
| Abläufe | Elementtyp `workflow.process` für Prozesse wie Bewerbungen oder Genehmigungen: Startformular mit Datei-Upload, Aufgaben, Freigaben (Vier-Augen-Prinzip), Bedingungen, automatische E-Mails mit Platzhaltern, Webhooks, Wartezeiten, Erinnerungen, lückenloser Verlauf, „Meine Aufgaben“ |
| E-Mail | Postausgang mit Wiederholungen, SMTP (STARTTLS/TLS), sendmail, Benachrichtigungen zu Aufgaben, Vorgängen und Kommentaren (im Profil abschaltbar) |
| Makros | Inhaltsverzeichnis, Unterseiten, Zuletzt geändert, Hinweis-Boxen, Aufklappbereich, Status-Label, Spalten, Layout-Bausteine (Abschnitt, Hero, Karten, Button), **Shell-Befehl** (nur exakt vom Superadmin freigegeben) und **HTTP-Abruf** (Allow-Liste, interne Adressen gesperrt), eigene Makros per Plugin |
| Zusammenarbeit | Kommentare und Antworten unter Seiten (`core.comments`), Änderungen im Audit-Protokoll |
| Suche | Volltextsuche über alle Sprachen mit Hervorhebung, Tags auf Kategorien und Seiten, Tag-Wolke; optional OpenSearch/Elasticsearch oder Meilisearch (`search.external`) für sehr große Wikis |
| Plugins | Werden mit dem Kern kompiliert, sofort aktivier- und deaktivierbar, Installation aus GitHub, Git oder lokalem Ordner per Oberfläche, API oder CLI, Neubau und Neustart aus der Oberfläche, Vererbung (`extends`), Update-Prüfung über GitHub-Releases mit 1-Klick-Update |
| Mitgeliefert | `auth.local`, `auth.ldap`, `auth.oidc`, `auth.shib`, `core.html`, `core.attachment`, `core.menu` (Menüs an Template-Positionen), `core.macros`, `core.layout`, `macro.shell`, `macro.http`, `core.comments`, `workflow.process`, `search.external`, die Templates `theme.confluence` (Dokumentation) und `theme.cms` (Website), Beispiele: `theme.confluence.dark` (erbt von einem Template), `example.notice` (erbt von einem Elementtyp) |
| Betrieb | Setup-Assistent (interaktiv oder headless), Docker/Compose, systemd-Installation, sicheres Update-Skript mit Backup und automatischem Rollback, Single- und Cluster-Modus |
| CLI | Jede API-Funktion über `ewiki api`, dazu Befehle für Benutzer, Plugins und Konfiguration |
| Tests | Unit-Tests, API-Integrationstest gegen PostgreSQL (prüft auch, dass jede API-Route eine Frontend-Anbindung hat), Browser-E2E-Tests mit WCAG-Prüfung, alles in GitHub Actions |

## Schnellstart mit Docker

```bash
cp .env.example .env        # Passwörter setzen
docker compose up -d --build
# http://localhost:8080 – Anmeldung mit admin / EWIKI_ADMIN_PASSWORD
```

## Installation auf einem Server (systemd)

```bash
git clone https://github.com/mtsonline/enterpriseWiki.git && cd enterpriseWiki
sudo ./scripts/install.sh            # baut, fragt Datenbank und Admin-Passwort ab, richtet den Dienst ein
sudo ./scripts/update.sh --enable-timer   # optional: automatische Updates
```

Details: [docs/admin/installation.md](docs/admin/installation.md)

## Entwicklung

Voraussetzungen: Go ≥ 1.24, Node.js ≥ 20, PostgreSQL ≥ 14.

```bash
./scripts/build.sh                                   # Frontend + Backend -> bin/ewiki
EWIKI_DATABASE_URL=postgres://user:pass@localhost/wiki ./bin/ewiki setup
./bin/ewiki serve
(cd web && npm run watch)                            # Frontend neu bauen bei Änderungen
EWIKI_TEST_DATABASE_URL=postgres://…/wiki_test go test ./...   # löscht die Testdatenbank!
```

## Dokumentation

| Für | Dokument |
|---|---|
| Benutzer:innen | [Benutzerhandbuch](docs/user/guide.md) |
| Administration | [Installation](docs/admin/installation.md) · [Konfiguration](docs/admin/configuration.md) · [Anmeldung: LDAP, OIDC, SAML, 2FA](docs/admin/authentication.md) · [Betrieb, Updates, Backup, CLI](docs/admin/operations.md) · [Sicherheit & Berechtigungen](docs/admin/security.md) · [Plugins verwalten](docs/admin/plugins.md) |
| Entwicklung | [Architektur](docs/developer/architecture.md) · [Plugins entwickeln](docs/developer/plugins.md) · [REST-API](docs/developer/api.md) |
| Planung | [Roadmap](docs/ROADMAP.md) |
