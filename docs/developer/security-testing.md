# Sicherheitsprüfungen

Security first: Eine Version wird nur veröffentlicht, wenn alle Prüfungen grün sind. Sie laufen in der CI bei jedem Push.

| Prüfung | Was sie findet | Wo |
|---|---|---|
| `govulncheck` | bekannte Schwachstellen in Go und allen Modulen, die der Code tatsächlich aufruft | Job *Security* |
| `gosec` | unsichere Muster (Injection, Pfade, TLS, Rechte, Redirects …); jede Ausnahme steht als `#nosec Gxxx -- Begründung` im Code | Job *Security* |
| `npm audit` | bekannte Schwachstellen in allen npm-Paketen, auch Build-Werkzeugen | Job *Security* |
| Trivy (Image) | Schwachstellen (HIGH/CRITICAL mit verfügbarem Fix) und Geheimnisse im Docker-Image | Job *Docker images* |
| Trivy (Konfiguration) | Fehlkonfigurationen in Dockerfile und Compose-Dateien | Job *Docker images* |
| `TestSecurityBaseline` | Angriffe auf die HTTP-Schicht: Header/CSP, Cookie-Flags, Zugriffsrechte und Rechteausweitung, Injection, Path Traversal, gespeichertes XSS, Brute Force, X-Forwarded-For-Umgehung, Benutzer-Enumeration, Host-Header, CSRF, übergroße Anfragen | `internal/server/security_test.go` |
| Weitere Tests | Trennung der Sites, zweiter Faktor, erlaubte Netze, Open Redirects (`SafeNext`), vertrauenswürdige Proxys | `m18_test.go`, `proxy_test.go`, `internal/auth` |
| `scripts/test-docker-proxy.sh` | Eingaben von `docker-setup.sh` (auch bösartige), Proxy ohne Root-Rechte, schreibgeschütztes Dateisystem, Zertifikate nur für eigene Hostnamen | Job *Docker images* |

Bei neuen Funktionen gehört ein Test für die Rechteprüfung dazu (wer darf es, wer nicht – auch über API-Keys und auf anderen Sites). Neue `#nosec`-Ausnahmen brauchen eine Begründung, warum kein Risiko besteht.

Sicherheitslücken bitte nicht öffentlich melden, sondern wie in `SECURITY.md` beschrieben (GitHub: *Security → Report a vulnerability*).
