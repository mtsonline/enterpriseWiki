# Installation

Enterprise Wiki consists of **one binary** (`ewiki`) and a **PostgreSQL database** (version 16 or newer, recommended 19; the extension `ltree` is created automatically). The web interface is embedded in the binary.

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

Requirements: git, Go ≥ 1.27 (recommended: current version), Node.js ≥ 22 with npm (recommended: current LTS, at present 26), a reachable PostgreSQL database from version 16 (recommended: 19).

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

### Bundled proxy with CrowdSec (optional)

[CrowdSec](https://www.crowdsec.net) (MIT) protects the bundled proxy. `./docker-setup.sh` asks for it when you choose HTTPS (default: yes). Who does not want it keeps `docker-compose.yml` alone, with the official Caddy image.

**What it does:**

- It reads the access log of the proxy and blocks addresses that scan the server or attack it (scenarios of the collection `crowdsecurity/caddy`, known vulnerabilities in requests `crowdsecurity/http-cve`).
- Its AppSec component checks every request against known vulnerabilities of other products (virtual patches, `crowdsecurity/appsec-virtual-patching`). Wiki content, code samples included, is not blocked by these rules.
- The scenario `crowdsecurity/http-crawl-non_statics` is off: the interface of the wiki makes many requests, and the scenario would block busy readers.

**Local only.** There is no connection to the CrowdSec console, and no addresses are shared with CrowdSec (`DISABLE_ONLINE_API`). The community blocklist needs that connection and is therefore off. On start, CrowdSec downloads its rules from its hub (`cdn-hub.crowdsec.net`, `hub-data.crowdsec.net`; the version from `version.crowdsec.net`).

**Setup.** `./docker-setup.sh` writes to `.env`:

```dotenv
EWIKI_SECURITY=crowdsec
COMPOSE_FILE=docker-compose.yml:docker-compose.crowdsec.yml
CROWDSEC_BOUNCER_KEY=<random>
```

Then `docker compose up -d --build`. The proxy is built on the first start: the official Caddy image plus the [CrowdSec bouncer for Caddy](https://github.com/hslatman/caddy-crowdsec-bouncer) (Apache-2.0), see `docker/caddy-crowdsec/Dockerfile`.

**Data protection.** The access log is in `./data/caddy/logs` (at most 3 files of 20 MB) and contains addresses, times and paths. It contains no query strings (they may carry tokens), no cookies and no authorization headers. CrowdSec keeps its decisions in `./data/crowdsec`.

**Operation:**

- Show blocked addresses: `docker compose exec crowdsec cscli decisions list`.
- Unblock an address: `docker compose exec crowdsec cscli decisions delete --ip 203.0.113.7`.
- If CrowdSec is not running, the proxy lets requests through (`appsec_fail_open`) and logs it, so the wiki stays reachable; the wiki checks every input itself either way.

**Tested** in the CI (`scripts/test-docker-proxy.sh`): a blocked address gets `403` and is let in again after unblocking; a virtual patch blocks `/.env`; a page with code, SQL and HTML is not blocked; no query string is in the log; the wiki stays reachable without CrowdSec.

**Turning it off:** set `EWIKI_SECURITY=none` and `COMPOSE_FILE=docker-compose.yml`, then run `docker compose up -d --remove-orphans`.

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

### Behind a web application firewall (ModSecurity, Coraza, CrowdSec)

A web application firewall (WAF) with the OWASP Core Rule Set (CRS) in front of the wiki blocks ordinary work with its default rules: saving then fails with `403 Forbidden`, and the wiki says that the answer did not come from the wiki.

**Why.** The CRS looks for attack patterns in every value of a request. Pages of a wiki contain such text all the time, for example:

- a line starting with "Sort the list …" or "set the value …" (looks like a shell command),
- a code sample with `<script>` or `onclick=`,
- SQL in a manual (`SELECT … WHERE … OR 1=1`),
- a path like `../etc/hosts`.

In addition the CRS allows only `GET`, `HEAD`, `POST` and `OPTIONS`. The wiki also uses `PUT`, `PATCH` and `DELETE`, for example to save an existing page.

The building blocks of the wiki themselves are written so that the CRS does not take them for attacks (CRS 3.3 and 4, paranoia level 1; checked in the tests).

**What to do.** Use the exclusions in [`docker/waf/ewiki-crs-exclusions.conf`](../../../docker/waf/ewiki-crs-exclusions.conf). Include the file *before* the CRS rules, for example on Debian or Ubuntu as `/etc/modsecurity/crs/REQUEST-900-EXCLUSION-RULES-BEFORE-CRS.conf`, then reload the web server. The file:

- lets JSON request bodies of the API (`POST`, `PUT`, `PATCH`, `DELETE` under `/api/v1/`) through the attack rules (cross-site scripting, SQL, commands, files, PHP, Java, Node.js, templates);
- allows `PUT`, `PATCH` and `DELETE` for `/api/v1/` only;
- lets the search term (`q`) of the search contain code.

Everything else stays checked: the path and the query string of all other requests, headers, cookies, the names of JSON fields, uploads, protocol rules and scanners.

**Why this is safe.** The wiki checks these inputs itself:

- HTML is cleaned on the server, event attributes are never let through;
- every response has a strict Content-Security-Policy;
- the database only gets parameterised queries;
- content never reaches a shell.

The XSS and SQL rules of the WAF therefore add little for these fields, and without the exclusions editors are blocked.

**Tested.** The exclusions are tested in `internal/server/waf_test.go` with CRS 3.3.8 and CRS 4 in Coraza: the requests of the wiki pass, and attacks in addresses still get blocked.

**What is not covered.** Coraza cannot load all rules of CRS 3.3; the SQL rules of CRS 3 (942) are only checked with CRS 4.

**Web application firewall in the wiki (plugin `core.waf`).** For installations without a firewall in front, the wiki ships a firewall of its own: Coraza with the OWASP Core Rule Set 4 and the exclusions above. It is off by default; switch it on under *Administration → Plugins*, then set it under *Administration → Web application firewall*:

- **Mode:** *Log only* (default) writes what would be blocked to the server log (`waf:` with the rules, path and address); *Block* refuses it with `403` (`waf_blocked`). Start with *Log only* and watch the log for a few days.
- **Strictness:** paranoia level 1 (default) or 2.
- **Not checked:** static files (`/assets/`) and uploaded files; of other bodies at most `core.waf.body_kb` (expert settings, default 512 KB), the rest passes unchecked.
- **Cost** (measured on 2 cores): about 1.5 ms per page request and 2–4 ms per saved page, about 20 MB of memory; the program is about 15 MB larger whether the plugin is on or not.
- If the rules cannot be loaded, the wiki keeps working without the firewall and logs it.
- Tested in `internal/server/waf_plugin_test.go`: off by default; log only lets attacks pass; blocking refuses attacks in addresses of pages and the API; pages with code, SQL and HTML can still be saved (also with `PUT`) and searched for; static files are not checked.

**If the wiki still gets blocked.** The log of the WAF names the rule (`[id "…"]`), for example `/var/log/apache2/modsec_audit.log`. Please report it.

**CrowdSec:** the default rules of its AppSec component aim at known vulnerabilities of certain products (virtual patching); with its CRS collection the same applies as above. The bundled proxy with CrowdSec (above) uses only the virtual patches and is tested with wiki content.

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
