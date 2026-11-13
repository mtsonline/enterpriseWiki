# Regeln für die Arbeit am Enterprise Wiki

Diese Datei gilt für alle, die am Repository arbeiten: Menschen und KI-Agenten. `AGENTS.md` verweist hierher, es gibt nur diese eine Quelle. Änderungen an dieser Datei brauchen die Freigabe des Projektinhabers.

Enterprise Wiki ist eine sichere und flexible Alternative für herkömmliche Enterprise-Wikis und CMS-Systeme: ein einzelnes Go-Programm (`ewiki`), PostgreSQL, Frontend in TypeScript. Überblick: `README.de.md`, Aufbau: `docs/de/developer/architecture.md`. Englische Fassung dieser Datei: `CLAUDE.en.md`; maßgeblich ist diese deutsche Fassung.

## 1. Grundsätze

Bei jedem Konflikt gilt diese Reihenfolge: Sicherheit, kein Datenverlust, Stabilität, Barrierefreiheit, Geschwindigkeit, Komfort.

- **Sicherheit zuerst. Lücken sind tabu.** Jede Eingabe ist feindlich, bis sie geprüft ist. Rechte werden im Server geprüft, nie nur in der Oberfläche. Ansicht als andere Person (`--as`) zeigt nie mehr, als die Rechte des angemeldeten Benutzers erlauben.
- **Kein Datenverlust.** Nichts, was jemand eingegeben hat, geht verloren, auch nicht bei Überlast, Abbruch oder Absturz.
- **Folgen in derselben Transaktion.** Was aus einer Änderung folgt (Benachrichtigung, E-Mail, Warteschlangen-Eintrag), wird mit ihr in derselben Transaktion gespeichert (`Notify.SendQ`, `Mail.Enqueue`, `Content.OnChangeDurable`, `Content.TouchTx`). Folgearbeit muss Wiederholungen vertragen.
- **Jede Funktion auf allen Wegen:** API, CLI und Oberfläche. **Alle** CLI-Befehle gehen über den API-Weg (`serveInProcess`), damit Rechte, Prüfungen und Audit dieselben sind; bestehende direkte Befehle werden umgestellt. Ausnahmen nur mit dokumentierter Genehmigung des Projektinhabers.
- **Barrierefreiheit** nach der jeweils aktuellen WCAG-Version, Stufe AA (Stand 13.11.2026: WCAG 2.2), in jeder Ansicht; geprüft mit axe in den Browser-Tests.
- **Schnell:** keine Abfragen in Schleifen, Grenzwerte für alles, was wachsen kann. Grenzwerte gehören in die Experteneinstellungen (`limits`), Plugins melden ihre im Manifest an.
- **Modular:** Funktionen außerhalb des Kerns sind Plugins. Mitgelieferte Plugins dürfen interne Pakete nutzen; die stabile Schnittstelle für Plugins ist `pkg/sdk`. Das Modell für externe Plugins wird noch festgelegt.
- **Ehrlich:** Was die Oberfläche zeigt oder die Dokumentation beschreibt, existiert und funktioniert so, wie man es erwartet. Keine Simulationen, keine Attrappen, keine Ankündigungen als Funktion.
- **Fremde Produktnamen** stehen nur in Import-Funktionen für das jeweilige System und deren Tests, sonst nirgends.
- **Namensräume:** Alles, was ein Plugin anmeldet (Routen, Rechte, Einstellungen, Ereignisse, Aktionen), trägt seine Plugin-ID; Instanzen heißen `plugin-id/instanz-id`; `core.` ist dem Kern vorbehalten.
- **Geheimnisse:** Vorgabe `plain`. Verschlüsselung ist immer wählbar, nie Pflicht.
- **Konfiguration:** ohne Cluster in `config.yaml`. Nur im Cluster-Modus liegen die in der Oberfläche änderbaren Einstellungen in der Datenbank.

## 2. Abhängigkeiten und Lizenzen

- Immer die neueste stabile Version. Renovate aktualisiert; die CI scheitert, wenn ein Update länger als 7 Tage offen ist.
- Alles, was ausgeliefert wird, steht unter einer freizügigen Lizenz: MIT, BSD, Apache-2.0, ISC, Zlib, Unlicense, CC0. Das gilt auch für Schriften, Bilder und Icons; Schriften stehen unter OFL-1.1 oder einer der genannten Lizenzen. Schriften werden selbst ausgeliefert, nie von fremden Servern geladen. Bei OFL-Schriften mit reserviertem Namen vor dem Verkleinern oder Umwandeln prüfen, ob sie umbenannt werden müssen. Werkzeuge, die nur beim Bauen oder Testen laufen und nicht ausgeliefert werden, sind davon ausgenommen. `go run ./tools/notices -check` prüft die Go-Module.
- Neue Bibliotheken nur mit Begründung; Pflegezustand vorher prüfen. Lieber die Go-Standardbibliothek.

## 3. Arbeitsweise im Repository

- `main` ist geschützt und immer lauffähig. Gearbeitet wird auf einem Branch; jede Änderung kommt per Pull Request.
- **Gemerged wird nur mit grüner CI und nach Freigabe durch den Projektinhaber.** Einzige Ausnahme: Abhängigkeits-Updates von Renovate werden automatisch gemergt, wenn alle Pflicht-Checks grün sind.
- Je Aufgabe genau ein Branch und eine Sitzung. Vor Beginn `main` holen. Nichts aus späteren, noch nicht freigegebenen Schritten vorziehen.
- Widersprüche zwischen Code, Dokumentation und Plänen sofort melden statt still entscheiden. Verbesserungsideen ebenso.
- Beiträge von KI-Agenten werden offen gekennzeichnet (Co-Author-Zeile im Commit, Hinweis im Pull Request).
- Commit-Nachrichten auf Englisch, kurz, im Imperativ oder als Zusammenfassung der Funktion.
- Pull Requests nutzen die Vorlage (`.github/pull_request_template.md`) einschließlich CLA.

## 4. Jede Funktion ist erst fertig mit

1. API, CLI und Oberfläche.
2. Tests: Go-Tests (Einheit und API-Integration), Browser-Test für den Ablauf in der Oberfläche.
3. Übersetzungen Deutsch und Englisch (`TestLocaleParity` prüft, dass keine fehlt).
4. Dokumentation auf Deutsch und Englisch: API-Referenz für Routen, Benutzer-Anleitung, Admin-Doku für Betrieb und Sicherheit, Roadmap.
5. Demo-Inhalt, wo die Funktion etwas zeigt.
6. Jede Route trägt eine Aktions-ID nach der Namensraum-Regel als Kommentar (Vorbereitung für das Aktionsregister); bestehende Routen werden nachgezogen.

### Datenbank

- **Bis zum ersten Release:** neue Spalten und Tabellen direkt in die `0001`-Dateien (`internal/db/migrations/…/0001_*.sql`); keine Migrationen für alte Stände.
- **Tabellen so bauen, dass später möglichst wenige Änderungen nötig sind:** feste Spalten für das, was Indizes oder Prüfregeln braucht; Zusatzdaten (z. B. von Plugins) als JSON. Plugins haben eigene Tabellen und eigene Migrationen.
- **Ab dem ersten Release:** nötige Migrationen laufen automatisch beim Start, nur vorwärts, mit Sicherung vorher. Die CI testet das Upgrade von jeder veröffentlichten Version. Was ersetzt wird, wird frühestens in der folgenden Version gelöscht, weil im Cluster alte und neue Knoten kurz gleichzeitig laufen. Kein Zurück auf ältere Versionen; dafür gibt es die Sicherung.

## 5. Tests

- **Jede Funktion wird in echter Funktion getestet.** Nachgebaute Gegenstellen für Fremdsysteme gibt es nur in Tests, und zusätzlich zu Läufen gegen echte Testsysteme. Tests prüfen ganze Abläufe, zum Beispiel: ein Workflow wird gestartet, entschieden und abgeschlossen; die Anmeldung läuft auch mit Passkey. Das sind Beispiele, keine abschließende Liste.
- Tests laufen mit den neuesten stabilen Versionen von Go, Node.js und PostgreSQL, lokal wie in der CI.
- Tests sind unabhängig und deterministisch: keine festen Wartezeiten, kein Verlass auf Reihenfolge oder Uhrzeit, wo es sich vermeiden lässt. Die CI läuft mit Race-Detector.
- Jede API-Route wird von der Oberfläche oder einem dokumentierten Skript genutzt (`checkRouteCoverage` in `internal/server/integration_test.go`). Ausnahmen dort nur für Aufrufe von außen (Anmeldeanbieter, SCIM, Office-Server).
- Ein behobener Fehler bekommt einen Test, der ihn wieder finden würde.

## 6. Befehle

```bash
./scripts/build.sh                      # Frontend und Programm bauen (bin/ewiki)
gofmt -l .                              # muss leer sein
go vet ./...
go run ./tools/genplugins               # nach neuen oder entfernten Plugins; Ergebnis committen
go run ./tools/notices -check           # Lizenzen der Go-Module
go run ./tools/docscheck                # Doku Deutsch und Englisch vollständig, Links gültig

# Go-Tests; mit Datenbank auch die API-Integrationstests
EWIKI_TEST_DATABASE_URL=postgres://USER:PASS@localhost:5432/DB?sslmode=disable go test -race ./...

# Sicherheit
scripts/govulncheck.sh
go run github.com/securego/gosec/v2/cmd/gosec@latest -quiet -exclude-generated -exclude-dir=web -conf .gosec.json ./...
(cd web && npm audit --audit-level=low)

# Frontend
cd web && npm run typecheck && npm run test:unit
EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_REQUIRE_AXE=1 npm run test:e2e   # gegen ein frisch eingerichtetes Wiki

# Gesamtprüfung von außen (Funktion, Last, Pentest), immer beide Varianten
tests/qa/local.sh
EWIKI_QA_PROXY=caddy EWIKI_QA_WORK=/tmp/ewiki-qa-caddy tests/qa/local.sh
```

Die Browser-Tests bauen aufeinander auf und laufen nur vollständig. Vor einem Release laufen zusätzlich die Testroutine `tests/qa` und alle Sicherheitsprüfungen.

## 7. Sauber, ohne Warnungen

Build, `go vet`, gosec, Typprüfung und Tests laufen ohne Warnungen. Eine Ausnahme steht nur mit Begründung direkt an der Stelle (`// #nosec G… -- Grund`). Eine Übersicht aller Ausnahmen wird automatisch erzeugt; die CI prüft, dass sie aktuell ist (wird mit Phase 1 eingeführt).

## 8. Was nie ins Repository gehört

- Berichte oder `pentest.json` mit offenen Schwachstellen (siehe `SECURITY.md`).
- Das Werkzeug zum Erzeugen von Lizenzschlüsseln und dessen Schlüssel.
- Persönliche Daten: echte E-Mail-Adressen, Passwörter, Tokens, Schlüssel. Beispiele nutzen `example.com`.
- Umgebungsspezifisches: lokale Pfade, private Startskripte.

Docker-Images werden vorerst nur privat veröffentlicht.

## 9. Sprache

- Oberfläche, Dokumentation und Anleitungen gibt es **immer auf Deutsch und Englisch**, vollständig und gleichwertig. Deutsch mit „Sie“. Kurze, klare Sätze, einfache Sprache. Die Vollständigkeit wird automatisch geprüft (`TestLocaleParity`, `go run ./tools/docscheck`).
- Ablage: `docs/de/…` und `docs/en/…` mit gleichen Dateinamen; im Hauptverzeichnis ist `X.md` Englisch und `X.de.md` Deutsch. Ausnahme: `CLAUDE.md` ist Deutsch, die Übersetzung heißt `CLAUDE.en.md`. Bei Rechtstexten (Lizenz, CLA) ist die deutsche Fassung verbindlich.
- Code, Bezeichner, Kommentare, Commit-Nachrichten: Englisch.

## 10. Planung

Entscheidungen und Pläne des Projekts führt der Projektinhaber außerhalb des Repositorys. Wer im Auftrag des Projektinhabers arbeitet, liest vor Beginn die aktuellen Entscheidungen und den Plan der jeweiligen Aufgabe und trägt Fortschritt und neue Entscheidungen dort nach. Diese Datei fasst die dauerhaft gültigen Regeln daraus zusammen.
