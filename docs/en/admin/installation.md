# Installation

Enterprise Wiki consists of **one binary** (`ewiki`) and a **PostgreSQL database** (version 16 or newer, recommended 18; the extension `ltree` is created automatically). The web interface is embedded in the binary.

There are three ways. All use the same setup command `ewiki setup`, which can be repeated as often as you like. It writes `config.yaml` and `.env` (if they do not exist), applies the database migrations and creates default roles, the first administrator and a welcome space.

## 1. Docker Compose (recommended to get started)

```bash
./docker-setup.sh        # once, as the user the wiki is to run as (not root)
docker compose up -d     # start – and for updates
```

- `./docker-setup.sh` creates `.env` with random passwords, enters the user and group ID of the calling
  user (`EWIKI_UID`, `EWIKI_GID`) and creates `./data/postgres` and `./data/wiki`. It can be run again
  at any time and only does what is missing (e.g. correcting owners, with `sudo` if needed).
- **All data is in the folder of the installation:** `./data/postgres` (database, one folder per major version),
  `./data/wiki` (`config.yaml`, uploaded files). Other places: change the paths in `docker-compose.yml`.
- **No container runs as root:** wiki and PostgreSQL run as `EWIKI_UID:EWIKI_GID`.
- The ready-made image `ghcr.io/mtsonline/enterprisewiki:${EWIKI_IMAGE_TAG}` (amd64 and arm64, the newest version on every
  `docker compose up -d`) and the official image
  `postgres:${POSTGRES_VERSION}` are used. New PostgreSQL major versions: `./docker-postgres-upgrade.sh`
  (see [operations.md](operations.md#postgresql-upgrades-docker)).
- On start the container runs `ewiki setup --non-interactive` automatically and then starts the server.
- The administrator is called `EWIKI_ADMIN_USER` (default `admin`), the password is `EWIKI_ADMIN_PASSWORD`. It is only used as long as no administrator exists.
- CLI in the container: `docker compose exec wiki ewiki plugin list`

**Image variants** (`EWIKI_IMAGE_TAG` in `.env`, `./docker-setup.sh` asks for it on new installations):

| Tag | Size | Plugins |
|---|---|---|
| `latest` or `7.5` (**Lite**) | about 40 MB | enable and disable the bundled plugins |
| `full` or `7.5-full` (**Full**) | about 700 MB | additionally install plugins (interface, CLI) or copy them to `./data/wiki/plugins`; the wiki builds them itself and keeps them across updates |

Switch at any time: change `EWIKI_IMAGE_TAG` and run `docker compose up -d`. Details: [plugins.md](plugins.md#docker-full-image-in-detail).

Own image (e.g. with own plugins in `plugins/`): `docker compose -f docker-compose.build.yml up -d --build` (same structure, builds the Full variant; with `export COMPOSE_FILE=docker-compose.build.yml` you can leave out `-f`).

## 2. Linux server with systemd

Requirements: git, Go ≥ 1.27 (recommended: current version), Node.js ≥ 22 with npm (recommended: current LTS, at present 26), a reachable PostgreSQL database from version 16 (recommended: 18).

```bash
sudo -u postgres createuser -P ewiki
sudo -u postgres createdb -O ewiki ewiki
git clone https://github.com/mtsonline/enterpriseWiki.git
cd enterpriseWiki
sudo ./scripts/install.sh            # default target: /opt/enterprisewiki
```

The script

1. copies the source tree to `/opt/enterprisewiki` (it stays there so plugins can be installed from the interface and rebuilt),
2. creates the system user `ewiki`,
3. builds `bin/ewiki`,
4. starts the interactive setup assistant (database URL, site name, languages, admin password),
5. sets up the service `enterprisewiki.service` and starts it.

Automatic updates (weekly, with backup and rollback): `sudo /opt/enterprisewiki/scripts/update.sh --enable-timer`. See [operations.md](operations.md).

## 3. Manually

```bash
./scripts/build.sh                                  # -> bin/ewiki
./bin/ewiki setup                                   # interactive
./bin/ewiki serve
```

Headless (e.g. for Ansible or Kubernetes jobs):

```bash
EWIKI_DATABASE_URL='postgres://ewiki:secret@db:5432/ewiki' \
EWIKI_ADMIN_PASSWORD='Very-Secret-42' \
EWIKI_SITE_NAME='Company wiki' EWIKI_LANGUAGES='en,de' \
EWIKI_BASE_URL='https://wiki.example.com' \
./bin/ewiki setup --non-interactive
```

| Variable | Meaning | Default |
|---|---|---|
| `EWIKI_CONFIG` | path of the configuration file | `./config.yaml` |
| `EWIKI_DATABASE_URL` | PostgreSQL URL. With the interactive setup it is stored in `.env`, not in the configuration | – |
| `EWIKI_ADMIN_USER` / `EWIKI_ADMIN_PASSWORD` | first administrator | `admin` / – |
| `EWIKI_SITE_NAME`, `EWIKI_LISTEN`, `EWIKI_BASE_URL` | basic settings | `Enterprise Wiki`, `:8080`, `http://localhost:8080` |
| `EWIKI_LANGUAGES`, `EWIKI_DEFAULT_LANGUAGE` | languages (first = default) | `en,de` |
| `EWIKI_LOG_LEVEL`, `EWIKI_LOG_FORMAT` | `debug`/`info`/`warn`/`error`, `text`/`json` | `info`, `text` |
| `EWIKI_SOURCE` | source tree for plugin installations, if not next to `config.yaml` | directory of the configuration |

## Reverse proxy and HTTPS

### Bundled proxy (Docker Compose, Caddy)

Docker Compose comes with a reverse proxy with automatic HTTPS ([Caddy](https://caddyserver.com), Apache-2.0). It is switched on in `.env`:

```dotenv
COMPOSE_PROFILES=proxy
EWIKI_BASE_URL=https://wiki.example.com
EWIKI_ACME=letsencrypt          # letsencrypt, letsencrypt-staging (to try it out) or zerossl
EWIKI_ACME_EMAIL=it@example.com # required: account at the certification authority
EWIKI_PORT=127.0.0.1:8080       # wiki only reachable through the proxy
```

Then `docker compose up -d`. Ports 80 and 443 (TCP, for HTTP/3 also UDP 443) must be reachable from the internet and the DNS entries must point to the server. Caddy gets the certificate on the first request for a host name and renews it itself. There are certificates only for host names the wiki knows: the base URL and the host names of all sites (query to `/tls-allowed`). So new sites need no change to the proxy. HTTP is redirected to HTTPS. Certificates and ACME accounts are in `./data/caddy`; the configuration in `docker/caddy/Caddyfile`. With *ZeroSSL* the account is created from `EWIKI_ACME_EMAIL` (EAB automatically). When setting up new servers, start with `letsencrypt-staging` so you do not run into the limits of Let's Encrypt.

### Trusted proxies (`EWIKI_TRUSTED_PROXIES`)

Behind a reverse proxy all requests come from its address. So that rate limits, sign-in locks, the audit log and the allowed networks of accounts use the **address of the visitors**, the wiki reads it from the header of the proxy – but only from trusted proxies: any other sender could forge the header and pretend to be any address. The environment variable `EWIKI_TRUSTED_PROXIES` (IP addresses and CIDR networks, separated by commas) overrides `server.trusted_proxies` from `config.yaml`:

| Setup | `EWIKI_TRUSTED_PROXIES` |
|---|---|
| Docker Compose with the bundled proxy | `172.30.80.250` (default in `docker-compose.yml`) |
| Docker Compose, nginx/Apache on the host | `172.30.80.1` (gateway of the Docker network) |
| without Docker, nginx on the same server | `127.0.0.1,::1` |
| proxy/load balancer on other machines | their addresses, e.g. `10.0.5.10,10.0.5.11` |

**Which header** is read is set by `server.client_ip_header`:

| Value | Header | suitable for |
|---|---|---|
| `x-forwarded-for` (default) | `X-Forwarded-For: client, proxy1` | Caddy, nginx, Apache, HAProxy, Traefik, most load balancers |
| `forwarded` | `Forwarded: for=192.0.2.60;proto=https` (RFC 7239) | proxies that send the standard header |
| name of a header | exactly one address, e.g. `x-real-ip`, `cf-connecting-ip`, `true-client-ip` | nginx with `X-Real-IP`, CDNs |

With a header with exactly one address the proxy must set it itself and overwrite values from visitors (with nginx `proxy_set_header X-Real-IP $remote_addr;`).

**Warning when an entry is missing.** If an address sends the chosen header without being entered as a proxy, the header is ignored. The wiki reports this in the server log, under **Administration → Expert settings → Address of the visitors** (with address and number of requests) and once per address and day in the audit log (`server.proxy_untrusted`). If it is your own proxy, it is missing in `trusted_proxies` – until then all visitors behind it share one address and therefore one rate limit. Single foreign addresses in this list, on the other hand, are normal (visitors behind company proxies) and need no reaction.

Lists (`X-Forwarded-For`, `Forwarded`) are read from the right: the first address that is not a trusted proxy counts as the client. Values further left set by the client itself are ignored. The Docker network has a fixed subnet (`EWIKI_SUBNET`, default `172.30.80.0/24`); whoever changes it also sets `EWIKI_IP_RANGE` (range for the containers), `EWIKI_PROXY_IP` (fixed address of the proxy outside this range) and `EWIKI_TRUSTED_PROXIES`. Several installations on the same host each need their own subnet.

### Own proxy

Without the bundled proxy you run the wiki behind your own reverse proxy with TLS and set in `config.yaml` (or with `EWIKI_TRUSTED_PROXIES`):

```yaml
server:
  base_url: https://wiki.example.com
  secure_cookies: true          # session cookie only over HTTPS, plus HSTS
  trusted_proxies: [127.0.0.1]  # only these addresses may set X-Forwarded-For
```

Example nginx:

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

With several sites all host names of the sites must point to the wiki and the proxy must pass on the `Host` header unchanged.

## Several instances (cluster mode)

For very many simultaneous users, start several instances behind a load balancer:

```yaml
mode: cluster
```

- All instances use the same database and **the same `config.yaml`** (shared volume, e.g. NFS or a ReadWriteMany volume). Changes from the interface are written atomically, and every instance reloads the file automatically (check every 3 seconds or at once with `SIGHUP`).
- Caches (permissions, sessions, translations) are invalidated on all instances through PostgreSQL `LISTEN/NOTIFY`. Redis or NATS are not needed.
- Maintenance tasks (retention, cleaning up expired sessions) only run on the instance that holds the database lock.
- Use PgBouncer (transaction mode) or adjust `database.max_conns` per instance.
- In mode `single` notifications and locks are left out, without performance overhead.
- Plugins in a cluster: use the Full image with a shared data folder. The build happens on one node, restarts one after another (see [plugins.md](plugins.md#docker-full-image-in-detail)).

Health checks: `GET /healthz` (process runs) and `GET /readyz` (database reachable).
