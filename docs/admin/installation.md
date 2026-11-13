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
- Verwendet werden das fertige Image `ghcr.io/mtsonline/enterprisewiki:${EWIKI_IMAGE_TAG}` (amd64 und arm64, bei jedem
  `docker compose up -d` die neueste Version) und das offizielle Image
  `postgres:${POSTGRES_VERSION}`. Neue PostgreSQL-Hauptversionen: `./docker-postgres-upgrade.sh`
  (siehe [operations.md](operations.md#postgresql-upgrades-docker)).
- Beim Start führt der Container automatisch `ewiki setup --non-interactive` aus und startet danach den Server.
- Der Administrator heißt `EWIKI_ADMIN_USER` (Standard `admin`), das Passwort ist `EWIKI_ADMIN_PASSWORD`. Es wird nur verwendet, solange noch kein Administrator existiert.
- CLI im Container: `docker compose exec wiki ewiki plugin list`

**Image-Varianten** (`EWIKI_IMAGE_TAG` in `.env`, `./docker-setup.sh` fragt bei neuen Installationen danach):

| Tag | Größe | Plugins |
|---|---|---|
| `latest` bzw. `7.5` (**Lite**) | ca. 40 MB | mitgelieferte Plugins aktivieren und deaktivieren |
| `full` bzw. `7.5-full` (**Full**) | ca. 700 MB | zusätzlich Plugins installieren (Oberfläche, CLI) oder nach `./data/wiki/plugins` kopieren; das Wiki baut sie selbst und behält sie über Updates |

Wechsel jederzeit: `EWIKI_IMAGE_TAG` ändern und `docker compose up -d`. Details: [plugins.md](plugins.md#docker-full-image-im-detail).

Eigenes Image (z. B. mit eigenen Plugins in `plugins/`): `docker compose -f docker-compose.build.yml up -d --build` (gleicher Aufbau, baut die Full-Variante; mit `export COMPOSE_FILE=docker-compose.build.yml` entfällt `-f`).

## 2. Linux-Server mit systemd

Voraussetzungen: git, Go ≥ 1.27 (empfohlen: aktuelle Version), Node.js ≥ 22 mit npm (empfohlen: aktuelle LTS, derzeit 26), eine erreichbare PostgreSQL-Datenbank ab Version 16 (empfohlen: 18).

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

### Mitgelieferter Proxy (Docker Compose, Caddy)

Für Docker Compose ist ein Reverse Proxy mit automatischem HTTPS dabei ([Caddy](https://caddyserver.com), Apache-2.0). Er wird in `.env` eingeschaltet:

```dotenv
COMPOSE_PROFILES=proxy
EWIKI_BASE_URL=https://wiki.example.com
EWIKI_ACME=letsencrypt          # letsencrypt, letsencrypt-staging (zum Ausprobieren) oder zerossl
EWIKI_ACME_EMAIL=it@example.com # Pflicht: Konto bei der Zertifizierungsstelle
EWIKI_PORT=127.0.0.1:8080       # Wiki nur noch über den Proxy erreichbar
```

Danach `docker compose up -d`. Die Ports 80 und 443 (TCP, für HTTP/3 auch UDP 443) müssen aus dem Internet erreichbar sein und die DNS-Einträge auf den Server zeigen. Caddy holt das Zertifikat beim ersten Aufruf eines Hostnamens und erneuert es selbst. Zertifikate gibt es nur für Hostnamen, die das Wiki kennt: die Basis-URL und die Hostnamen aller Sites (Abfrage an `/tls-allowed`). Neue Sites brauchen also keine Änderung am Proxy. HTTP wird auf HTTPS umgeleitet. Zertifikate und ACME-Konten liegen in `./data/caddy`; die Konfiguration in `docker/caddy/Caddyfile`. Bei *ZeroSSL* wird das Konto über `EWIKI_ACME_EMAIL` angelegt (EAB automatisch). Beim Einrichten neuer Server empfiehlt sich zuerst `letsencrypt-staging`, um nicht in die Limits von Let's Encrypt zu laufen.

### Vertrauenswürdige Proxys (`EWIKI_TRUSTED_PROXIES`)

Hinter einem Reverse Proxy kommen alle Anfragen von dessen Adresse. Damit Anfragebegrenzung, Login-Sperren, Audit-Protokoll und die erlaubten Netze von Konten die **Adresse der Besucher** verwenden, liest das Wiki sie aus dem Header des Proxys – aber nur von vertrauenswürdigen Proxys: Jeder andere Absender könnte den Header fälschen und sich so als beliebige Adresse ausgeben. Die Umgebungsvariable `EWIKI_TRUSTED_PROXIES` (IP-Adressen und CIDR-Netze, durch Komma getrennt) überschreibt `server.trusted_proxies` aus `config.yaml`:

| Aufbau | `EWIKI_TRUSTED_PROXIES` |
|---|---|
| Docker Compose mit mitgeliefertem Proxy | `172.30.80.250` (Standard in `docker-compose.yml`) |
| Docker Compose, nginx/Apache auf dem Host | `172.30.80.1` (Gateway des Docker-Netzes) |
| ohne Docker, nginx auf demselben Server | `127.0.0.1,::1` |
| Proxy/Load-Balancer auf anderen Rechnern | deren Adressen, z. B. `10.0.5.10,10.0.5.11` |

**Welcher Header** gelesen wird, bestimmt `server.client_ip_header`:

| Wert | Header | passend für |
|---|---|---|
| `x-forwarded-for` (Vorgabe) | `X-Forwarded-For: client, proxy1` | Caddy, nginx, Apache, HAProxy, Traefik, die meisten Load-Balancer |
| `forwarded` | `Forwarded: for=192.0.2.60;proto=https` (RFC 7239) | Proxys, die den genormten Header senden |
| Name eines Headers | genau eine Adresse, z. B. `x-real-ip`, `cf-connecting-ip`, `true-client-ip` | nginx mit `X-Real-IP`, CDNs |

Bei einem Header mit genau einer Adresse muss der Proxy ihn selbst setzen und Werte der Besucher überschreiben (bei nginx `proxy_set_header X-Real-IP $remote_addr;`).

**Warnung bei fehlendem Eintrag.** Sendet eine Adresse den gewählten Header, ohne als Proxy eingetragen zu sein, wird der Header ignoriert. Das Wiki meldet das im Server-Protokoll, unter **Administration → Experteneinstellungen → Adresse der Besucher** (mit Adresse und Zahl der Anfragen) und einmal je Adresse und Tag im Audit-Protokoll (`server.proxy_untrusted`). Ist es Ihr eigener Proxy, fehlt er in `trusted_proxies` – bis dahin teilen sich alle Besucher dahinter eine Adresse und damit eine Anfragebegrenzung. Einzelne fremde Adressen in dieser Liste sind dagegen normal (Besucher hinter Firmen-Proxys) und brauchen keine Reaktion.

Listen (`X-Forwarded-For`, `Forwarded`) werden von rechts gelesen: Die erste Adresse, die kein vertrauenswürdiger Proxy ist, gilt als Client. Vom Client selbst gesetzte Werte weiter links werden ignoriert. Das Docker-Netz hat ein festes Subnetz (`EWIKI_SUBNET`, Standard `172.30.80.0/24`); wer es ändert, setzt auch `EWIKI_IP_RANGE` (Bereich für die Container), `EWIKI_PROXY_IP` (feste Adresse des Proxys außerhalb dieses Bereichs) und `EWIKI_TRUSTED_PROXIES`. Mehrere Installationen auf demselben Host brauchen je ein eigenes Subnetz.

### Eigener Proxy

Ohne den mitgelieferten Proxy betreiben Sie das Wiki hinter einem eigenen Reverse Proxy mit TLS und setzen in `config.yaml` (oder per `EWIKI_TRUSTED_PROXIES`):

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
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
  }
}
```

Bei mehreren Sites müssen alle Hostnamen der Sites auf das Wiki zeigen und der Proxy den `Host`-Header unverändert weitergeben.

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
- Plugins im Cluster: Full-Image mit gemeinsamem Datenordner verwenden. Gebaut wird auf einem Knoten, neu gestartet nacheinander (siehe [plugins.md](plugins.md#docker-full-image-im-detail)).

Health-Checks: `GET /healthz` (Prozess läuft) und `GET /readyz` (Datenbank erreichbar).
