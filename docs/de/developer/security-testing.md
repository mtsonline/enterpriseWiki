# Sicherheitsprüfungen

Security first: Eine Version wird nur veröffentlicht, wenn alle Prüfungen grün sind. Sie laufen in der CI bei jedem Push.

| Prüfung | Was sie findet | Wo |
|---|---|---|
| `govulncheck` | bekannte Schwachstellen in Go und allen Modulen, die der Code tatsächlich aufruft. Gibt es noch keine korrigierte Version, wird die Lücke im eigenen Code abgefangen und mit Ablaufdatum in `.govulncheck-allow` eingetragen (danach schlägt die Prüfung wieder an) | Job *Security* (`scripts/govulncheck.sh`) |
| `gosec` | unsichere Muster (Injection, Pfade, TLS, Rechte, Redirects …) und nicht ausgewertete Fehler (alle Regeln); jede Ausnahme steht als `#nosec Gxxx -- Begründung` im Code | Job *Security* |
| `npm audit` | bekannte Schwachstellen in allen npm-Paketen, auch Build-Werkzeugen | Job *Security* |
| Trivy (Image) | Schwachstellen (HIGH/CRITICAL mit verfügbarem Fix) und Geheimnisse im Docker-Image | Job *Docker images* |
| Trivy (Konfiguration) | Fehlkonfigurationen in Dockerfile und Compose-Dateien | Job *Docker images* |
| `TestSecurityBaseline` | Angriffe auf die HTTP-Schicht: Header/CSP, Cookie-Flags, Zugriffsrechte und Rechteausweitung, Injection, Path Traversal, gespeichertes XSS, Brute Force, X-Forwarded-For-Umgehung, Benutzer-Enumeration, Host-Header, CSRF, übergroße Anfragen | `internal/server/security_test.go` |
| `TestContentHostileParams` | jeder Parameter jedes Bausteins und jede Bausteinoption mit Werten, die ausbrechen wollen (Markup, Attribute, Adressen, CSS, JSON); Antworten des HTTP-Bausteins und externe Feeds in jedem Format und jeder Darstellung; die fertige Seite darf nichts enthalten, was Skript ausführt, Markup einschleust oder fremde Stile lädt. Vorher muss jeder Baustein und jede Option mit gültigen Werten wirken, damit die feindlichen Werte nicht nur eine Fehlermeldung prüfen. Im Browser dasselbe für das CSS der Optionen (`blockoptions.test.ts`) | `internal/server/content_hostile_test.go` |
| Weitere Tests | Trennung der Sites, zweiter Faktor, erlaubte Netze, Open Redirects (`SafeNext`), vertrauenswürdige Proxys | `m18_test.go`, `proxy_test.go`, `internal/auth` |
| `scripts/test-docker-proxy.sh` | Eingaben von `docker-setup.sh` (auch bösartige), Proxy ohne Root-Rechte, schreibgeschütztes Dateisystem, Zertifikate nur für eigene Hostnamen | Job *Docker images* |

Bei neuen Funktionen gehört ein Test für die Rechteprüfung dazu (wer darf es, wer nicht – auch über API-Keys und auf anderen Sites). Neue `#nosec`-Ausnahmen brauchen eine Begründung, warum kein Risiko besteht.

Sicherheitslücken bitte nicht öffentlich melden, sondern wie in `SECURITY.md` beschrieben (GitHub: *Security → Report a vulnerability*).

## Prüfung vor einem Pull Request (`scripts/precheck.sh`)

Das Skript führt die Prüfungen der CI auf dem eigenen Rechner aus, in der Reihenfolge, die Fehler am schnellsten findet:

```bash
scripts/precheck.sh               # fast + go + e2e (was die meisten Änderungen brauchen)
scripts/precheck.sh fast          # nur die schnellen Prüfungen (Sekunden bis wenige Minuten)
scripts/precheck.sh all           # zusätzlich security und docker
scripts/precheck.sh a11y          # nur die schnelle Prüfung der Barrierefreiheit (wenige Minuten)
```

| Teil | Was läuft |
|---|---|
| `fast` | gofmt, erzeugte Plugin-Importe, `go vet`, Plugin-Vertrag, Lizenzen, Dokumentation, `tools/traps -fit`, Alter neuer npm-Versionen, Bauen des Frontends, Typprüfung, Unit-Tests. Hält beim ersten Fehler an. |
| `go` | alle Go-Tests mit Race-Detector; die Server-Tests in den drei Teilen der CI, jeder mit eigener Datenbank |
| `e2e` | Bauen, ein frisches Wiki mit eigener Datenbank und eigenem Port, alle Browser-Tests mit axe, `tools/uicoverage` |
| `a11y` | Bauen, ein frisches Wiki auf dem Port nach `EWIKI_PRECHECK_PORT`, die schnelle Prüfung der Barrierefreiheit (siehe unten); nicht in `all` enthalten, `e2e` prüft dasselbe und mehr |
| `security` | `govulncheck`, `gosec`, `npm audit` |
| `docker` | Bauen des Images und die Docker-Tests (`test-docker-plugins.sh`, `test-docker-proxy.sh`); braucht ein laufendes Docker |

Die langen Teile laufen nebeneinander (`EWIKI_PRECHECK_JOBS`, Vorgabe: Zahl der CPUs, höchstens 4), jeder mit eigenem Protokoll; am Ende nennt das Skript die fehlgeschlagenen Tests. Es braucht einen PostgreSQL-Benutzer, der Datenbanken anlegen darf (`EWIKI_PRECHECK_PG`, Vorgabe `postgres://wiki:wiki@localhost:5432`), und entfernt seine Datenbanken wieder. Die Tests gegen echte Anmelde-, Office- und Suchsysteme bleiben in der CI (Jobs *Sign-in*, *Office*, *External search*); ihre Startskripte in `tests/auth`, `tests/office` und `tests/search` laufen mit Docker auch lokal.

Ein neuer Push in einen Pull Request beendet den CI-Lauf des Pushs davor.

## Prüfungen der Barrierefreiheit

Jede Ansicht wird in den Browser-Tests nach WCAG 2.2 AA geprüft. Die Hilfsfunktion `a11y()` in `web/tests/e2e.test.mjs` führt zwei Prüfungen aus `web/tests/a11y-lib.mjs` aus:

- **axe-core** mit den Regeln für WCAG 2.0 bis 2.2, Stufen A und AA.
- **Überdeckungsprüfung** (eigene Prüfung): Kein Bedienelement darf von einem anderen Element verdeckt sein. Jeder sichtbare Link, jede Schaltfläche, jedes Formularfeld und jedes fokussierbare Element im Fenster wird in der Mitte und an den vier Ecken (2 px nach innen) abgetastet. Liegt dort ein anderes Element obenauf, schlägt die Prüfung fehl, egal wie groß es ist. Nicht gezählt werden: Eltern und Kinder des Elements, seine eigenen Beschriftungen (ein gestaltetes Kontrollkästchen unter seinem Label), Elemente, die Klicks durchlassen (`pointer-events: none`), Elemente außerhalb eines offenen modalen Dialogs und versteckte Elemente (`.sr-only`). Der Bericht nennt das Element, das verdeckende Element und wie viel noch sichtbar ist (`< 28` markiert weniger als 28 px).

Warum die eigene Prüfung: axe misst, wie viel von einem verdeckten Element noch sichtbar ist, und lässt es ab 24 px durch. Das hängt von den Textbreiten des Rechners ab. In PR #225 verdeckte der fokussierte Sprunglink den Link zur Startseite: lokal blieben 31 px sichtbar (bestanden), in der CI 23,9 px (fehlgeschlagen). Die Überdeckungsprüfung schlägt auf beiden Rechnern fehl.

Die Browser-Tests fokussieren außerdem den Sprunglink (Tab vom Seitenanfang) und prüfen, dass er nichts verdeckt (`checkSkipLink`).

**Schneller Lauf** (wenige Minuten statt der ganzen Browser-Tests): Er meldet sich an einem mit `ewiki setup` eingerichteten Wiki an und prüft die wichtigsten Seiten (Startseite als Gast und angemeldet, Anmeldung, Kategorie, Seite, Verlauf, Suche, Profil, Dashboard, Bereiche, Medien, Aufgaben, die Verwaltung mit Lizenz) bei 390, 800 und 1280 px, im hellen und im dunklen Farbschema und mit fokussiertem Sprunglink auf Startseite und Seite. Die übrigen Seiten der Verwaltung prüft er einmal (1280 px, hell); mit `EWIKI_A11Y_ALL_ADMIN=1` alle wie die wichtigsten. Jede Seite wird zusätzlich mit einer breiten Schrift geprüft (DejaVu Sans, hell, 390 und 1280 px; `EWIKI_A11Y_WIDE_FONT` nennt eine andere, leer schaltet das ab): Das Wiki nutzt die Systemschrift, die sich von Rechner zu Rechner unterscheidet; Text, der auf einem Rechner gerade passt, kann auf einem anderen überlaufen. Er startet selbst nichts und ändert nichts außer dem Farbschema des Benutzers, das er am Ende zurücksetzt.

```bash
cd web && EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD=… npm run test:a11y
scripts/precheck.sh a11y          # dasselbe mit einem eigenen, frischen Wiki
```

Nach jeder Änderung an Layout, Kopfzeile, CSS oder einem Theme vor dem Pull Request ausführen.

**Bilder** dürfen die Seite beim Laden nicht verschieben (ein Klick, der unter ein Bild zielt, landet sonst woanders, sobald es seine Größe hat). `web/tests/unit/imgsize.test.ts` prüft jedes Bild, das das Frontend erzeugt: Attribute `width` und `height`, eine Klasse mit fester Höhe oder `aspect-ratio`, ein Rahmen mit fester Höhe (Liste `BOXED`) oder eine Ausnahme mit Begründung (Liste `EXCEPTIONS`).

## Abgefangene Lücken in Abhängigkeiten

| Lücke | Betroffen | Abfangen im Code | geprüft bis |
|---|---|---|---|
| GO-2026-6909 | `crewjam/saml` v0.5.1 (SAML-Anmeldung): Absturz bei Assertions ohne Subject/Conditions; `NameID` und Attribute werden ohne Namespace gelesen, ein fremdes `<NameID>` in einer signierten Assertion könnte den Benutzer festlegen | `plugins/auth_shib/guard.go`: Vorprüfung jeder (auch verschlüsselten) Assertion vor der Bibliothek – genau ein Issuer, Subject, Conditions, SubjectConfirmationData; alle SAML-Elemente im SAML-Namespace; Absturz der Bibliothek wird zum Fehler | 2026-12-31 |

## Office-Programme (M14)

`TestOfficeWOPI` und `TestOfficeOnlyOfficeAPI` (internal/server) prüfen mit einem nachgebauten Office-Server:
Proof-Key-Pflicht (unsigniert, fremder Schlüssel, alter Zeitstempel), Schlüssel nur für eine Datei, Sitzungs-Cookies
zählen am WOPI-Endpunkt nicht, Ansehen-Schlüssel können weder speichern noch sperren, Sperrkonflikte, Rechteentzug
und deaktiviertes Plugin machen Schlüssel sofort ungültig, ohne Download nur geschützt, Wasserzeichen-Originale nur
für Verwalter, API-Schlüssel und CLI ohne `--as` erzeugen keine Office-Schlüssel, CSP enthält nur aktive Programme,
OnlyOffice nur mit gültigem JWT und Downloads nur vom eingestellten Server (SSRF). `internal/wopi` testet Discovery und
Proof-Keys (inkl. Schlüsselwechsel, Überlauf, zu kurze Schlüssel), `internal/pagecache` dass Token-URLs nie im
Gast-Cache landen.

## Anmeldung gegen echte Systeme

Zusätzlich zu den Tests mit nachgebauten Anmeldeanbietern (`sso_test.go`) laufen in der CI (Job „auth“) Tests gegen
echte Systeme: Keycloak als OpenID-Connect- und SAML-Anbieter und OpenLDAP mit `memberOf`. Beide laufen als normale
Prozesse ohne Container, lokal genauso wie in der CI.

```bash
eval "$(EWIKI_AUTH_AD=1 EWIKI_AUTH_SHIB=1 tests/auth/start.sh)"   # Keycloak, OpenLDAP, AD, Apache mit mod_shib
EWIKI_TEST_DATABASE_URL=… go test -race -run TestReal ./internal/server/
(cd web && EWIKI_E2E_URL=http://localhost:8080 npm run test:auth)   # Browser-Test gegen ein frisch eingerichtetes Wiki
tests/auth/start.sh stop

eval "$(tests/search/start.sh)"   # OpenSearch, Elasticsearch, Meilisearch (externe Suche)
EWIKI_TEST_DATABASE_URL=… go test -race -run TestRealExternalSearch ./internal/server/
tests/search/start.sh stop   # Suchmaschinen beenden
```

Geprüft wird unter anderem: Anmeldung über das echte Anmeldeformular, falsches Passwort, Zuordnung von Gruppen und
Rollen, Back-Channel-Logout (Ende der Sitzung beim Anbieter beendet sie im Wiki), Abmelden im Wiki beendet auch die
Sitzung beim Anbieter, eine SAML-Antwort gilt nur einmal, SAML-Abmeldung in beide Richtungen, LDAP-Filter-Einschleusung,
Gruppen über `memberOf` und über eine Gruppensuche.

Mit `EWIKI_AUTH_AD=1` startet zusätzlich Samba als Active Directory (Paket `samba-ad-dc`, braucht `sudo`): LDAPS mit
einer eigenen Test-CA, gesperrte Konten, `objectGUID` als feste Kennung, verschachtelte Gruppen, StartTLS, kein
Anmelden ohne vertrauenswürdiges Zertifikat oder ohne Verschlüsselung. Mit `EWIKI_AUTH_SHIB=1` startet Apache mit
mod_shib vor dem Wiki (Pakete `apache2`, `libapache2-mod-shib`), eingerichtet wie in der Admin-Dokumentation: Der Test
meldet sich über Keycloak an und versucht, die Kopfzeilen mit der Identität zu fälschen – direkt am Wiki vorbei, über
ungeschützte Pfad-Varianten und mit einer echten Sitzung. Jeder Test legt in Keycloak einen eigenen Realm an; Passwörter werden bei jedem Start neu
erzeugt. Ohne die Umgebung werden die Tests übersprungen, mit `EWIKI_TEST_REQUIRE_REAL_AUTH=1` schlagen sie fehl.

## Testroutine gegen eine laufende Instanz (`tests/qa`)

Zusätzlich zu den Prüfungen oben gibt es eine Routine, die eine laufende Instanz von außen prüft: Funktionstest mit 100 gleichzeitigen Benutzern, Lasttest in Stufen bis 10.000 Benutzer und ein Penetrationstest mit rund 100 Prüfungen nach OWASP WSTG, ASVS und API Security Top 10 (jede mit Schweregrad und CVSS-Vektor), darunter ein Durchlauf über alle Routen aus `api.md` mit den IDs privater Objekte.

```bash
tests/qa/local.sh        # frische Wegwerf-Instanz, alle Tests, Bericht in /tmp/ewiki-qa/results/report.md
```

Vor jeder Version ausführen; neue Funktionen bekommen dort einen Schritt im Funktionstest. Der Bericht beschreibt offene Schwachstellen und gehört nicht ins Repository. Einzelheiten: [tests/qa/README.md](../../../tests/qa/README.md).

## Fallen-Tests: prüfen die Tests, was sie behaupten? (`tools/traps`)

Ein grüner Test sagt nur etwas, wenn er rot würde, sobald das Geprüfte kaputt ist. `tools/traps` baut deshalb nacheinander absichtlich Fehler in sicherheitskritische Stellen ein und lässt die zuständigen Tests laufen. Die Liste steht in `tools/traps/traps.go`. Beispiele:
- eine Rechteprüfung fehlt;
- „nur eigene Rechte“ wird übergangen;
- `--as` bekommt die Rechte der Kommandozeile;
- eine Kopie verliert ihre Einstufung;
- ein deaktiviertes Konto arbeitet weiter.

```bash
EWIKI_TEST_DATABASE_URL=postgres://USER:PASS@localhost:5432/DB?sslmode=disable go run ./tools/traps
go run ./tools/traps -list                       # alle Fallen
go run ./tools/traps -only cli.as,auth.csrf      # einzelne Fallen
go run ./tools/traps -fit                        # nur: passt jede Falle noch zum Code (schnell, läuft in der CI)
```

- Zuerst laufen die Tests ohne Fallen und müssen grün sein. Ohne Datenbank bricht das Werkzeug ab, weil die Integrationstests sonst übersprungen würden und jede Falle als „gefangen“ gälte.
- **caught:** Die Tests werden rot, wie erwartet. **MISSED:** Die Tests bleiben grün, also prüfen sie nicht, was sie behaupten; das ist ein Befund. **BROKEN:** Die Falle lässt sich nicht übersetzen.
- **OUTDATED:** Die bewachte Stelle hat sich geändert. Dann wird die Falle angepasst, sie wird nie entfernt. Die CI prüft das bei jeder Änderung (`-fit`), so fällt eine veraltete Falle sofort auf.
- Jede geänderte Datei wird nach ihrer Falle wiederhergestellt, auch bei Strg+C. Dateien mit nicht eingecheckten Änderungen werden abgelehnt.
- Läuft vor jedem Release. Jeder behobene Sicherheitsfehler bekommt eine Falle.
