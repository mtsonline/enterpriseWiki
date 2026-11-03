# Installation

Enterprise Wiki besteht aus **einer Binärdatei** (`ewiki`) und einer **PostgreSQL-Datenbank** (Version 14 oder neuer, Erweiterung `ltree` wird automatisch angelegt). Die Weboberfläche ist in die Binärdatei eingebettet.

Es gibt drei Wege. Alle verwenden denselben, beliebig oft wiederholbaren Setup-Befehl `ewiki setup`. Er schreibt `config.yaml` und `.env` (falls nicht vorhanden), spielt die Datenbank-Migrationen ein und legt Standardrollen, den ersten Administrator und einen Willkommensbereich an.

## 1. Docker Compose (empfohlen für den Einstieg)

```bash
./docker-setup.sh        # einmalig, als der Benutzer, unter dem das Wiki laufen soll (nicht root)
docker compose up -d     # starten – und für Updates
```

- `./docker-setup.sh` legt `.env` mit zufälligen Passwörtern an, trägt die Benutzer- und Gruppen-ID des aufrufenden
  Benutzers ein (`EWIKI_UID`, `EWIKI_GID`) und legt `./data/postgres` und `./data/wiki` an. Es kann jederzeit erneut
  ausgeführt werden und erledigt nur, was fehlt (z. B. Besitzer korrigieren, falls nötig mit `sudo`).
- **Alle Daten liegen im Ordner der Installation:** `./data/postgres` (Datenbank, je Hauptversion ein Ordner),
  `./data/wiki` (`config.yaml`, hochgeladene Dateien). Andere Orte: Pfade in `docker-compose.yml` ändern.
- **Kein Container läuft als root:** Wiki und PostgreSQL laufen als `EWIKI_UID:EWIKI_GID`.
- Verwendet werden das fertige Image `ghcr.io/mtsonline/enterprisewiki:latest` (amd64 und arm64, bei jedem
  `docker compose up -d` die neueste Version; festhalten z. B. mit `:7.5`) und das offizielle Image
  `postgres:${POSTGRES_VERSION}`. Neue PostgreSQL-Hauptversionen: `./docker-postgres-upgrade.sh`
  (siehe [operations.md](operations.md#postgresql-upgrades-docker)).
- Beim Start führt der Container automatisch `ewiki setup --non-interactive` aus und startet danach den Server.
- Der Administrator heißt `EWIKI_ADMIN_USER` (Standard `admin`), das Passwort ist `EWIKI_ADMIN_PASSWORD`. Es wird nur verwendet, solange noch kein Administrator existiert.
- CLI im Container: `docker compose exec wiki ewiki plugin list`

Plugins werden bei Docker installiert, indem man sie in das Verzeichnis `plugins/` legt und das Image selbst baut: `docker compose -f docker-compose.build.yml up -d --build` (gleicher Aufbau, `build: .` statt des fertigen Images; mit `export COMPOSE_FILE=docker-compose.build.yml` entfällt `-f`). Das Bauen aus der Oberfläche heraus braucht die Quellinstallation (Weg 2).

## 2. Linux-Server mit systemd

Voraussetzungen: git, Go ≥ 1.24 (empfohlen: aktuelle Version), Node.js ≥ 22 mit npm (empfohlen: aktuelle LTS), eine erreichbare PostgreSQL-Datenbank.

```bash
sudo -u postgres createuser -P ewiki
sudo -u postgres createdb -O ewiki ewiki
git clone https://github.com/mtsonline/enterpriseWiki.git
cd enterpriseWiki
sudo ./scripts/install.sh            # Standard-Ziel: /opt/enterprisewiki
```

Das Skript

1. kopiert den Quellbaum nach `/opt/enterprisewiki` (er bleibt dort, damit Plugins aus der Oberfläche installiert und neu gebaut werden können),
2. legt den Systembenutzer `ewiki` an,
3. baut `bin/ewiki`,
4. startet den interaktiven Setup-Assistenten (Datenbank-URL, Seitenname, Sprachen, Admin-Passwort),
5. richtet den Dienst `enterprisewiki.service` ein und startet ihn.

Automatische Updates (wöchentlich, mit Backup und Rollback): `sudo /opt/enterprisewiki/scripts/update.sh --enable-timer`. Siehe [operations.md](operations.md).

## 3. Manuell

```bash
./scripts/build.sh                                  # -> bin/ewiki
./bin/ewiki setup                                   # interaktiv
./bin/ewiki serve
```

Headless (z. B. für Ansible oder Kubernetes-Jobs):

```bash
EWIKI_DATABASE_URL='postgres://ewiki:secret@db:5432/ewiki' \
EWIKI_ADMIN_PASSWORD='Very-Secret-42' \
EWIKI_SITE_NAME='Firmenwiki' EWIKI_LANGUAGES='de,en' \
EWIKI_BASE_URL='https://wiki.example.com' \
./bin/ewiki setup --non-interactive
```

| Variable | Bedeutung | Standard |
|---|---|---|
| `EWIKI_CONFIG` | Pfad der Konfigurationsdatei | `./config.yaml` |
| `EWIKI_DATABASE_URL` | PostgreSQL-URL. Wird beim interaktiven Setup in `.env` gespeichert, nicht in der Konfiguration | – |
| `EWIKI_ADMIN_USER` / `EWIKI_ADMIN_PASSWORD` | erster Administrator | `admin` / – |
| `EWIKI_SITE_NAME`, `EWIKI_LISTEN`, `EWIKI_BASE_URL` | Grundeinstellungen | `Enterprise Wiki`, `:8080`, `http://localhost:8080` |
| `EWIKI_LANGUAGES`, `EWIKI_DEFAULT_LANGUAGE` | Sprachen (erste = Standard) | `en,de` |
| `EWIKI_LOG_LEVEL`, `EWIKI_LOG_FORMAT` | `debug`/`info`/`warn`/`error`, `text`/`json` | `info`, `text` |
| `EWIKI_SOURCE` | Quellbaum für Plugin-Installationen, falls nicht neben `config.yaml` | Verzeichnis der Konfiguration |

## Reverse Proxy und HTTPS

Betreiben Sie das Wiki hinter einem Reverse Proxy mit TLS und setzen Sie in `config.yaml`:

```yaml
server:
  base_url: https://wiki.example.com
  secure_cookies: true          # Session-Cookie nur über HTTPS, zusätzlich HSTS
  trusted_proxies: [127.0.0.1]  # nur diese Adressen dürfen X-Forwarded-For setzen
```

Beispiel nginx:

```nginx
server {
  listen 443 ssl http2;
  server_name wiki.example.com;
  client_max_body_size 64m;
  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto https;
  }
}
```

## Mehrere Instanzen (Cluster-Modus)

Für sehr viele gleichzeitige Benutzer starten Sie mehrere Instanzen hinter einem Load Balancer:

```yaml
mode: cluster
```

- Alle Instanzen nutzen dieselbe Datenbank und **dieselbe `config.yaml`** (gemeinsames Volume, z. B. NFS oder ein ReadWriteMany-Volume). Änderungen aus der Oberfläche werden atomar geschrieben, und jede Instanz lädt die Datei automatisch neu (Prüfung alle 3 Sekunden oder sofort per `SIGHUP`).
- Caches (Berechtigungen, Sessions, Übersetzungen) werden über PostgreSQL `LISTEN/NOTIFY` auf allen Instanzen invalidiert. Redis oder NATS sind nicht nötig.
- Wartungsaufgaben (Aufbewahrung, Aufräumen abgelaufener Sessions) laufen nur auf der Instanz, die die Datenbank-Sperre hält.
- Verwenden Sie PgBouncer (Transaktionsmodus) oder passen Sie `database.max_conns` je Instanz an.
- Im Modus `single` entfallen Benachrichtigungen und Sperren, ohne Performance-Overhead.

Health-Checks: `GET /healthz` (Prozess läuft) und `GET /readyz` (Datenbank erreichbar).
