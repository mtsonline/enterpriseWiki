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
```

- Zuerst laufen die Tests ohne Fallen und müssen grün sein. Ohne Datenbank bricht das Werkzeug ab, weil die Integrationstests sonst übersprungen würden und jede Falle als „gefangen“ gälte.
- **caught:** Die Tests werden rot, wie erwartet. **MISSED:** Die Tests bleiben grün, also prüfen sie nicht, was sie behaupten; das ist ein Befund. **BROKEN:** Die Falle lässt sich nicht übersetzen.
- **OUTDATED:** Die bewachte Stelle hat sich geändert. Dann wird die Falle angepasst, sie wird nie entfernt.
- Jede geänderte Datei wird nach ihrer Falle wiederhergestellt, auch bei Strg+C. Dateien mit nicht eingecheckten Änderungen werden abgelehnt.
- Läuft vor jedem Release. Jeder behobene Sicherheitsfehler bekommt eine Falle.
