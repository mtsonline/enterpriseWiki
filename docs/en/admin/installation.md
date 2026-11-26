# Installation

Enterprise Wiki consists of **one binary** (`ewiki`) and a **PostgreSQL database** (version 16 or newer, recommended 19; the extension `ltree` is created automatically). The web interface is embedded in the binary.

There are three ways. All use the same setup command `ewiki setup`, which can be repeated as often as you like. It writes `config.yaml` and `.env` (if they do not exist), applies the database migrations and creates default roles, the first administrator and a welcome space.

## 1. Docker Compose (recommended to get started)

```bash
./docker-setup.sh        # once, as the user the wiki is to run as
                         # (or as root: ./docker-setup.sh --user NAME)
docker compose up -d     # start – and for updates
```

- `./docker-setup.sh` creates `.env` with random passwords, enters the user and group ID of the calling
  user (`EWIKI_UID`, `EWIKI_GID`) and creates `./data/postgres` and `./data/wiki`. It can be run again
  at any time and only does what is missing (e.g. correcting owners, with `sudo` if needed).
- **Run by root:** the wiki does not run as root. `./docker-setup.sh` then asks for the user who runs the wiki (default `ewiki`; without a terminal `--user NAME`). It creates the user if missing and adds it to the group `docker`. All files of the installation folder then belong to this user, also files root creates later (run `./docker-setup.sh` again). The folder must not lie below `/root`, for example `/opt/enterprise-wiki`. Then start the wiki as that user: `sudo -u NAME docker compose up -d`.
- **All data is in the folder of the installation:** `./data/postgres` (database, one folder per major version),
  `./data/wiki` (`config.yaml`, uploaded files). Other places: change the paths in `docker-compose.yml`.
- **Access to Docker:** the user who installs and later starts the wiki must be able to use Docker: as a member of the group `docker` or with rootless Docker. `./docker-setup.sh` checks this with `docker info` and otherwise stops with an explanation. Fix: `sudo usermod -aG docker USER`, then log out and in again (the new group only applies to a new login) and run `./docker-setup.sh` again.
- **Wiki, database and proxy never run as root:** they always run as `EWIKI_UID:EWIKI_GID` from `.env` (`user:` in `docker-compose.yml`), also when someone starts `docker compose up -d` as root (tested in `scripts/test-docker-setup-root.sh`). Only the optional CrowdSec, Collabora and OnlyOffice need root inside their own container; they are reachable only through the proxy. `./data` belongs to this user. Existing installations: run `./docker-setup.sh` again; it enters missing IDs and corrects the owners in `./data` (with `sudo` if needed). Then `docker compose up -d`.
- **Automatic updates:** `./docker-setup.sh` asks whether new releases are installed automatically (default yes, daily at 03:30, with a database backup first). `scripts/update-docker.sh` on the server does this, not the wiki itself; details in [operations.md](operations.md#docker-automatic-updates-with-the-update-helper).
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
EWIKI_CROWDSEC_PASSWORD=<random>
```

Then `docker compose up -d --build`. The proxy is built on the first start: the official Caddy image plus the [CrowdSec bouncer for Caddy](https://github.com/hslatman/caddy-crowdsec-bouncer) (Apache-2.0), see `docker/caddy-crowdsec/Dockerfile`.

**Data protection.** The access log is in `./data/caddy/logs` (at most 3 files of 20 MB) and contains addresses, times and paths. It contains no query strings (they may carry tokens), no cookies and no authorization headers. CrowdSec keeps its decisions in `./data/crowdsec`.

**In the administration.** The page *CrowdSec* shows whether CrowdSec runs, its latest alerts and the addresses it blocks. With the permission *Manage CrowdSec* you lift a block and keep a list of allowed addresses that CrowdSec never blocks (for example your office or a monitoring service; networks at most /8 for IPv4 and /24 for IPv6). Allowing an address also lifts its current blocks. The page works on the main site only, because CrowdSec protects the whole installation. Command line: `ewiki crowdsec status`, `alerts`, `blocks`, `unblock ID`, `allowlist`, `allowlist-set --json '{"entries":[{"value":"192.0.2.10","note":"office"}]}'`.

How the wiki talks to CrowdSec:

- **No access to Docker.** The wiki signs in to the Local API of CrowdSec (`http://crowdsec:8080`) as the machine `ewiki` with `EWIKI_CROWDSEC_PASSWORD`. A machine may read alerts and allowlists and delete decisions; it never sees the keys of bouncers. The script `docker/crowdsec/ewiki-sync.sh` runs inside the CrowdSec container and registers the machine on every start.
- **Allowed addresses through a folder.** The Local API cannot write allowlists. The wiki keeps the list in its database and writes it to `./data/wiki/crowdsec-exchange/allowlist.txt`. The script in the CrowdSec container checks every line (only IP addresses and networks pass) and applies the list to the allowlist `ewiki` with `cscli`, usually within 10 seconds. The page shows whether CrowdSec applies the list. The folder is set with `EWIKI_CROWDSEC_EXCHANGE`, only in the environment, never in the administration.
- **Protected changes.** Lifting a block and changing the list need a browser session signed in during the last 15 minutes, or the command line on the server; API keys cannot. Every change is in the audit log (`security.crowdsec.unblock`, `security.crowdsec.allowlist`).
- **Without the bundled setup:** register a machine yourself (`cscli machines add ewiki --password …`) and enter the address of the Local API and the password on the page. Without an exchange folder the list of allowed addresses can only be read.

**Operation:**

- Show blocked addresses: on the page *CrowdSec*, or `docker compose exec crowdsec cscli decisions list`.
- Unblock an address: on the page *CrowdSec*, or `docker compose exec crowdsec cscli decisions delete --ip 203.0.113.7`.
- If CrowdSec is not running, the proxy lets requests through (`appsec_fail_open`) and logs it, so the wiki stays reachable; the wiki checks every input itself either way.

**Tested** in the CI (`scripts/test-docker-proxy.sh`): a blocked address gets `403` and is let in again after unblocking; a virtual patch blocks `/.env`; a page with code, SQL and HTML is not blocked; no query string is in the log; the wiki stays reachable without CrowdSec; the wiki signs in as the machine `ewiki`, and an allowed address reaches CrowdSec and loses its block. The page itself is tested against a real CrowdSec (`tests/crowdsec/start.sh`), also with a hostile exchange file.

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
# in the http block
map $http_upgrade $connection_upgrade {
  default upgrade;
  ''      close;
}
server {
  listen 443 ssl http2;
  server_name wiki.example.com;
  client_max_body_size 101m;   # storage.max_upload_mb (default 100 MB) plus 1 MB
  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    # live editing (WebSocket)
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
  }
}
```

Without the lines `Upgrade` and `Connection`, live editing does not work; everybody then edits alone. The wiki sends a ping every 25 seconds, so the default `proxy_read_timeout` (60 seconds) is enough.

With several sites all host names of the sites must point to the wiki and the proxy must pass on the `Host` header unchanged.

### Behind a web application firewall (ModSecurity, Coraza, CrowdSec)

A web application firewall (WAF) with the OWASP Core Rule Set (CRS) in front of the wiki blocks ordinary work with its default rules: saving then fails with `403 Forbidden`, and the wiki says that the answer did not come from the wiki.

**Why.** The CRS looks for attack patterns in every value of a request. Pages of a wiki contain such text all the time, for example:

- a line starting with "Sort the list …" or "set the value …" (looks like a shell command),
- a code sample with `<script>` or `onclick=`,
- SQL in a manual (`SELECT … WHERE … OR 1=1`),
- a path like `../etc/hosts`,
- an address like `http://localhost:8080` in a guide for developers (CRS 4 takes it for server-side request forgery).

In addition the CRS allows only `GET`, `HEAD`, `POST` and `OPTIONS`. The wiki also uses `PUT`, `PATCH` and `DELETE`, for example to save an existing page.

The building blocks of the wiki themselves are written so that the CRS does not take them for attacks (CRS 3.3 and 4, paranoia level 1; checked in the tests).

**What to do.** Use the exclusions in [`docker/waf/ewiki-crs-exclusions.conf`](../../../docker/waf/ewiki-crs-exclusions.conf). Include the file *before* the CRS rules, for example on Debian or Ubuntu as `/etc/modsecurity/crs/REQUEST-900-EXCLUSION-RULES-BEFORE-CRS.conf`, then reload the web server. The file:

- lets JSON request bodies of the API (`POST`, `PUT`, `PATCH`, `DELETE` under `/api/v1/`) through the attack rules (cross-site scripting, SQL, commands, files, PHP, Java, Node.js, templates, server-side request forgery);
- allows `PUT`, `PATCH` and `DELETE` for `/api/v1/` only;
- lets the search term (`q`) of the search contain code and local addresses;
- allows SCIM of an identity provider (`application/scim+json` with `POST`, `PUT`, `PATCH`, `DELETE`) for `/scim/v2/` only;
- accepts files as they are for three addresses only, without reading their body: an office server storing a document (`POST /api/v1/wopi/files/ID/contents`, `application/octet-stream`), importing a category (`application/x-ndjson`) and importing a translation (`application/octet-stream`); the command line sends both imports as `application/json`. The CRS allows neither of the first two types, and these bodies can be larger than the limit of the WAF; the wiki checks the office token or the permission of the administration and the whole file itself.

Everything else stays checked: the path and the query string of all other requests, headers, cookies, the names of JSON fields, uploads, protocol rules and scanners.

**Sizes of requests.** The recommended configuration of ModSecurity (file modsecurity.conf-recommended) is smaller than what the wiki allows. Without a change, saving a page with more than 128 KB fails, and so does uploading a file over 12.5 MB, with `413` from the WAF. Set at least:

```apache
# largest body without files: content of a page (Administration → Expert settings,
# core.request.content_kb, default 4096 KB)
SecRequestBodyNoFilesLimit 4194304
# largest upload with the form around it (storage.max_upload_mb, default 100 MB, plus 1 MB)
SecRequestBodyLimit 105906176
```

Raise both when you raise the limits of the wiki. For nginx also `client_max_body_size 101m;`.

**Why this is safe.** The wiki checks these inputs itself:

- HTML is cleaned on the server, event attributes are never let through;
- every response has a strict Content-Security-Policy;
- the database only gets parameterised queries;
- content never reaches a shell;
- addresses that people enter and the wiki fetches (feeds, webhooks) go through its own guard against internal addresses; connections to internal servers (directory, office server, search) only the administration sets up.

The XSS and SQL rules of the WAF therefore add little for these fields, and without the exclusions editors are blocked.

**Tested.** The exclusions are tested in `internal/server/waf_test.go` with CRS 3.3.8 and CRS 4 in Coraza: the requests of the wiki pass (also SCIM, office documents and imports), attacks in addresses still get blocked, and the other addresses keep the content types closed.

**What is not covered.** Coraza cannot load all rules of CRS 3.3; the SQL rules of CRS 3 (942) and the rules for methods and content types of CRS 3 (920) are only checked with CRS 4 (the same rule IDs and the same format of the settings).

**Web application firewall in the wiki (plugin `core.waf`).** For installations without a firewall in front, the wiki ships a firewall of its own: Coraza with the OWASP Core Rule Set 4 and the exclusions above. It is off by default; switch it on under *Administration → Plugins*, then set it under *Administration → Web application firewall*:

- **Mode:** *Block* (default) refuses an attack with `403` (`waf_blocked`) as soon as the plugin is switched on; *Log only* lets it pass and only writes it to the server log (`waf:` with the rules, path and address), for example to try new rules or a stricter level first. Both modes log what they find.
- **Strictness:** paranoia level 1 (default) or 2.
- **Not checked:** static files (`/assets/`) and uploaded files; of other bodies at most `core.waf.body_kb` (expert settings, default 512 KB), the rest passes unchecked.
- **Cost** (measured on 2 cores): about 1.5 ms per page request and 2–4 ms per saved page, about 20 MB of memory; the program is about 15 MB larger whether the plugin is on or not.
- If the rules cannot be loaded, the wiki keeps working without the firewall and logs it.
- Tested in `internal/server/waf_plugin_test.go`: off by default; switched on, it blocks at once; log only lets attacks pass; blocking refuses attacks in addresses of pages and the API; pages with code, SQL and HTML can still be saved (also with `PUT`) and searched for; static files are not checked.

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
