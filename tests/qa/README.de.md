# Testroutine: Funktion, Last, Sicherheit

[English](README.md) · **Deutsch**

`tests/qa` prüft eine **laufende** Instanz von außen, über dieselbe HTTP-API und denselben WebSocket wie der Browser. Sie ergänzt die Go-Tests (`go test ./...`) und die Browser-Tests (`web/tests`): Dort wird jede Funktion einzeln geprüft, hier alles gleichzeitig, unter Last und aus Sicht eines Angreifers.

> **Nie gegen ein produktives Wiki starten.** Die Tests legen Inhalte und Konten an und löschen sie, sperren Konten, senden sehr große Anfragen und belasten den Server, bis er nicht mehr rechtzeitig antwortet. Ziele außerhalb des eigenen Rechners oder privater Netze verlangen `-force`.

> **Ergebnisse vertraulich behandeln.** `report.md` und `pentest.json` beschreiben offene Schwachstellen. Sie gehören nicht ins Repository und nicht in öffentliche Tickets (siehe `SECURITY.de.md`).

## Schnellstart

```bash
tests/qa/local.sh              # alles, auf einer frischen Wegwerf-Instanz (etwa 25 Minuten)
tests/qa/local.sh functional   # nur ein Teil: functional | load | pentest
EWIKI_QA_PROXY=caddy tests/qa/local.sh   # dasselbe hinter dem Reverse Proxy des Docker-Setups
```

**Immer beide Varianten fahren** – ohne Proxy (das Wiki allein) und mit `EWIKI_QA_PROXY=caddy` (TLS, HTTP/2, Kompression, weitergereichte Adressen wie im Betrieb) – und für die zweite ein eigenes Arbeitsverzeichnis wählen (`EWIKI_QA_WORK`). Der Proxy läuft mit demselben Handler wie `docker/caddy/Caddyfile`; anders sind nur zwei Dinge, die ein Test auf einem Rechner braucht: Zertifikate einer eigenen Zertifizierungsstelle statt ACME, und der Proxy glaubt dem Testrechner die Adressen der virtuellen Benutzer. `caddy` muss im `PATH` liegen (oder `EWIKI_QA_CADDY=/pfad/caddy`; ohne Paket: `go install github.com/caddyserver/caddy/v2/cmd/caddy@latest`).

`local.sh` legt eine eigene PostgreSQL-Instanz und Datenbank an, baut das Wiki aus dem Checkout, startet es, lässt die Tests laufen und räumt wieder auf. Ergebnisse: `$EWIKI_QA_WORK/results` (Vorgabe `/tmp/ewiki-qa/results`): `report.md` und die Rohdaten als JSON. Voraussetzungen: Go, Node.js (für den Build), PostgreSQL (`initdb`), für die statischen Prüfungen `govulncheck` und `gosec`.

Gegen eine vorhandene Testinstanz:

```bash
cd tests/qa && go build -o qa .
export EWIKI_QA_ADMIN_PASSWORD=…          # Passwort des Administrators
./qa all -base http://127.0.0.1:8080 -spoof-ips -server-pid "$(pgrep -f 'ewiki serve')"
```

## Was geprüft wird

| Befehl | Inhalt | Ergebnis |
|---|---|---|
| `seed` | QA-Bereich mit einer Kopie der Demo (alle Elementtypen), privatem und öffentlichem Bereich, Gruppen `qa-editors`/`qa-readers`, 100 Konten (4 von 5 dürfen bearbeiten) | `world.json` |
| `functional` | Alle Konten melden sich im selben Moment an und gehen jede Funktion durch: Anmeldung, Navigation, alle Elementtypen, Suche, Seiten (Versionen, Übersetzung, Verschieben, Papierkorb), Entwürfe, Vorlagen, Dateien, Medien, Blog, FAQ, Termine, Abläufe, Eigenschaften, Kommentare, Reaktionen, Favoriten, Beobachten, API-Keys, Live-Bearbeitung, Rechte. Schritte mit Wettlauf starten für alle gleichzeitig: dieselbe Seite speichern (genau einer gewinnt), Termin mit begrenzten Plätzen, ein Vorgang mit vielen Freigebenden | `functional.json` |
| `load` | Stufen mit steigender Zahl gleichzeitiger Benutzer (Gäste, Lesende, Bearbeitende, Live-Bearbeitende), Antwortzeiten je Funktion (p50/p95/p99), Fehlerquote, CPU und Speicher des Wikis; danach ein Stoßtest (N Anfragen im selben Moment) | `load.json` |
| `pentest` | Rund 100 Prüfungen nach OWASP WSTG v4.2, ASVS 4.0.3 und API Security Top 10 2023, jede mit Schweregrad und CVSS-3.1-Vektor: Konfiguration, Sitzungen, Anmeldung, Zugriffsschutz (alle Routen aus `docs/en/developer/api.md` als Gast, Lesender und Bearbeitender mit den IDs privater Objekte), XSS, Injection, Uploads, SSRF, CSRF/CORS, WebSocket, Geschäftslogik, Ressourcenverbrauch | `pentest.json` |
| `static` | `govulncheck`, `gosec`, `npm audit`, `go vet` (wie in der CI) | `static.json` |
| `report` | Bericht aus allen Ergebnisdateien | `report.md` |
| `cleanup` | entfernt QA-Bereich, Konten und Gruppen | |

`all` führt `functional`, `load`, `pentest`, `static` und `report` aus, jeweils nach einem frischen `seed`. Der Penetrationstest kommt zuletzt, weil er gesperrte Konten und große Protokolleinträge hinterlässt.

## Wichtige Schalter

| Schalter | Bedeutung |
|---|---|
| `-users N` | Zahl der QA-Konten (Vorgabe 100) |
| `-spoof-ips` | Jeder virtuelle Benutzer bekommt eine eigene Adresse (`X-Forwarded-For`). Die Instanz muss dem Testrechner vertrauen (`server.trusted_proxies`), sonst greift die Grenze von 30 Anmeldungen pro Adresse in 5 Minuten |
| `-ca DATEI` | Zertifizierungsstelle der Testinstanz hinter einem Proxy mit eigenen Zertifikaten (auch `EWIKI_QA_CA`); die Verbindungen nutzen dann HTTP/2, die Live-Bearbeitung WebSocket über HTTP/1.1 |
| `-server-pid PID` | Prozess des Wikis auf demselben Rechner: CPU und Speicher im Bericht, Speicherprüfungen im Penetrationstest |
| `-stages 100,500,…` | gleichzeitige Benutzer je Laststufe |
| `-hold 40s`, `-think 5s` | Messdauer je Stufe, mittlere Denkzeit zwischen zwei Anfragen |
| `-collab 0.1`, `-guests 0.3` | Anteil Live-Bearbeitende und Gäste |
| `-spike 10000` | Anfragen im selben Moment (0: aus) |
| `-notify-ops` | Kommentare und Vorgangsstarts in den Lastmix aufnehmen (benachrichtigen viele Personen; Vorgabe aus) |
| `-routes DATEI` | Routenliste für den Zugriffsschutz-Durchlauf (Vorgabe `docs/en/developer/api.md`) |

## Anfragebegrenzung und die Tests

Das Wiki begrenzt Anfragen je Konto und je Adresse (Experteneinstellungen, `core.rate.*`). Die Routine arbeitet **mit** diesen Grenzen, nicht an ihnen vorbei:

- **Funktions- und Penetrationstest** laufen mit den Vorgaben des Wikis. Antwortet es mit `429`, warten die Prüfungen die genannte Zeit und wiederholen die Anfrage – wie ein ordentlicher Client. Die Prüfungen der Grenzen selbst (`API4-RATELIMIT`, `ATHN-RATE`, `BUSL-ACCESSREQ`) warten nicht.
- **Lasttest:** Die virtuellen Benutzer teilen sich die QA-Konten (10 000 Benutzer auf 100 Konten: jedes Konto steht für 100 Personen). Für die Dauer des Lasttests hebt die Routine deshalb die Grenzen **je Konto** um diesen Faktor an und stellt sie danach wieder her (`PUT /api/v1/admin/limits`). Die Grenzen je Adresse bleiben unverändert: Jeder virtuelle Gast hat seine eigene Adresse. Abgelehnte Anfragen (`429`) weist der Bericht getrennt von Fehlern aus.
- Wer die Grenzen ganz ausschalten will (reiner Durchsatztest), setzt in der Testinstanz `server.rate_limit.disabled: true`.

## Zielwerte des Lasttests

Eine Stufe gilt als bestanden, wenn Lesen p95 ≤ 500 ms, Schreiben p95 ≤ 1000 ms, Live-Bearbeitung p95 ≤ 250 ms und die Fehlerquote ≤ 0,1 % ist (`targets` in `load.go`). Das sind Annahmen dieser Routine; für ein Projekt mit eigenen Vorgaben dort anpassen.

Für belastbare Kapazitätszahlen den Lastgenerator auf einem **zweiten Rechner** starten (`-base http://testserver:8080 -force`): Auf einem Rechner teilen sich Wiki, Datenbank und Generator die CPU.

## Neue Funktionen aufnehmen

- **Funktionstest:** einen Schritt in `journey()` oder `editor()` (`functional.go`) ergänzen, mit `v.want(Bereich, Name, Antwort, erwartete Status…)`.
- **Zugriffsschutz:** Neue Routen stehen automatisch in `docs/en/developer/api.md` und laufen damit im Durchlauf mit. Für neue Arten privater Objekte einen Eintrag in `seedPrivate()` (`pentest.go`) mit Markertext anlegen und die ID in `priv.IDs` aufnehmen.
- **Sicherheitsprüfung:** eine Prüfung mit `check{id, area, title, refs, sev, vector, fix}` und `p.verdict(…)` schreiben. Schweregrad und CVSS-Vektor gelten für den Fall, dass sie fehlschlägt.
- **Last:** die Funktion in den Mix von `user()` (`load.go`) aufnehmen; Namen mit `read:` oder `write:` beginnen.

## Grenzen

- Die Proxy-Variante prüft das Zusammenspiel mit dem Reverse Proxy (Adressen, Kompression, WebSocket, HTTP/2), nicht dessen Zertifikatsbezug (ACME) und nicht HTTP/3. Der Lastgenerator bündelt mit HTTP/2 viele Benutzer auf wenige Verbindungen; echte Browser öffnen je Benutzer eine eigene.
- Skript im Browser (DOM-basiertes XSS) sieht ein HTTP-Test nicht: Browser-Tests und ein Browser-Scanner (z. B. OWASP ZAP) ergänzen das.
- Die Anmeldung prüfen die Go-Tests mit nachgebauten Anbietern und zusätzlich gegen echtes Keycloak (OIDC, SAML) und OpenLDAP (`tests/auth/start.sh`, CI-Job „auth“); Office-Server laufen echt im CI-Job „office“. SCIM-Clients sind bisher nur nachgebaut.
- Ein automatischer Test ersetzt keinen Penetrationstest durch eine unabhängige Stelle, wie ihn Zertifizierungen verlangen.
