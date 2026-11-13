# Configuration (`config.yaml`)

`config.yaml` is the **only authoritative source** of all settings:

- The admin interface (*Administration → Settings*, *Languages*, *Plugins*) and `ewiki config set` change only this file. It is written atomically (temporary file, then rename).
- Running servers reload the file automatically when it changes (also when edited by hand), or at once with `systemctl reload enterprisewiki` (SIGHUP). An invalid file is rejected, the previous configuration then stays active. The error message is in the log.
- Values can refer to environment variables: `${NAME}` or `${NAME:-default}`. The reference is kept when writing back, so **secrets never end up in the file**. Variables from `.env` in the same directory are loaded automatically. Environment variables that are already set take precedence.
- Changes to `server`, `database` and `mode` need a restart.

Complete example: [config.example.yaml](../../../config.example.yaml)

## Reference

### `server`
| Key | Meaning | Default |
|---|---|---|
| `listen` | address/port | `:8080` |
| `base_url` | public URL | `http://localhost:8080` |
| `secure_cookies` | send cookies only over HTTPS, plus the HSTS header | `false` |
| `trusted_proxies` | IPs/CIDRs of the reverse proxies: only from them is the passed-on address of the visitors accepted (for rate limits, sign-in protection, audit log, allowed networks); `EWIKI_TRUSTED_PROXIES` overrides the value | `[]` |
| `client_ip_header` | header in which the proxies give the address of the visitors: `x-forwarded-for`, `forwarded` (RFC 7239) or the name of a header with exactly one address (`x-real-ip`, `cf-connecting-ip` …). See [address of the visitors](installation.md#trusted-proxies-ewiki_trusted_proxies) | `x-forwarded-for` |
| `max_body_mb` | older setting for the size of imports; replaced by the limit `core.request.import_mb` (see [expert settings](#limits-expert-settings)) | `64` |
| `rate_limit.disabled` | `true` switches the request limits off (only for load tests or with limits in the reverse proxy) | `false` |

Behind a reverse proxy always set `trusted_proxies`, otherwise all users share the limits of the proxy address.

### `mode`
`single` (one instance) or `cluster` (several instances, see [installation.md](installation.md#several-instances-cluster-mode)).

### `database`
| Key | Meaning | Default |
|---|---|---|
| `url` | PostgreSQL URL, normally `${EWIKI_DATABASE_URL}` | `${EWIKI_DATABASE_URL}` |
| `max_conns` | maximum connections of this instance | `50` |

PostgreSQL ends a transaction that waits for the application for longer than 60 seconds (`idle_in_transaction_session_timeout`); so the connection pool can never block permanently. An own value in the database URL (`?idle_in_transaction_session_timeout=…`) takes precedence.

### `storage`
| Key | Meaning | Default |
|---|---|---|
| `path` | directory for uploaded files (`<path>/files/ab/cd/<sha256>`). In cluster mode a shared volume | `./data` |
| `max_upload_mb` | maximum size of an uploaded file | `100` |

For the best PDF text extraction install `pdftotext` (package `poppler-utils`, included in the Docker image). Without the tool a built-in extractor for simple PDFs is used. Files no version refers to any more are deleted automatically after one day.

### `media`
| Key | Meaning | Default |
|---|---|---|
| `viewer` | default viewer for files: `internal` (browser) or an active office plugin (`office.collabora`, `office.onlyoffice`, `office.m365`) | `internal` |
| `open_external` | open files the default program can handle there at once (only with an office plugin as `viewer`) | `false` |
| `token_ttl` | validity of the access key of an office program for a file (1m–24h) | `10h` |

Details: [office programs](office.md).

### `i18n`
| Key | Meaning |
|---|---|
| `languages` | active languages (codes such as `de`, `en`, `pt-BR`). Applies to interface and content |
| `default_language` | default language, must be in `languages` |

Languages are added or removed in the interface under *Administration → Languages* (permission *Manage languages and translate the interface*) or with `ewiki i18n add|remove CODE`. For every configured language there is automatically the permission *Translate the interface: <language>* (`core.i18n.translate.<code>`, e.g. `core.i18n.translate.pt_br`), which you assign to roles, see [translations of the interface](security.md#translations-of-the-interface).

### `ui`
| Key | Meaning | Default |
|---|---|---|
| `site_name` | name of the website | `Enterprise Wiki` |
| `template` | active template (plugin ID): `theme.wiki` (documentation) or `theme.cms` (website), also under *Administration → Settings* | `theme.wiki` |
| `home_element` | element ID of the home page (e.g. a page or a category overview), takes precedence over `home_category`. New installations: the welcome page. | `0` |
| `home_category` | category ID of the home page (`0` = list of spaces or the only space) | `0` |
| `prerender` | deliver pages for guests (search engines, link previews, visitors without JavaScript) with title, description, canonical address, Open Graph data and the content as HTML. Always from the view of a guest; with `cache` switched on without database access. Check: *Administration → Settings → Show guest view* or `ewiki api GET "/api/v1/prerender?path=/calendar"`. | `true` |
| `readable_urls` | readable addresses `/wiki/<categories>/<page>`, one per language; former addresses redirect (301). `false`: only `/e/12`, `/c/7` and short URLs | `true` |
| `editor` | default editor for pages: `visual` (ProseMirror, with live editing) or `simple` (simple input field). Users can choose differently in their profile (`ewiki pref set editor simple --as NAME`). Both store Markdown. | `visual` |
| `collaboration` | live editing: several people edit a page at the same time in the visual editor (WebSocket `/api/v1/collab/…`, only with edit permission, session and permission are checked every 30 s during the connection). In cluster operation only people connected to the same node see each other – use a reverse proxy with session affinity for `/api/v1/collab/`. | `true` |
| `preview_image` | preview image for link previews (`og:image`) of pages without an own image: path like `/api/v1/files/<hash>` or an http(s) address, can also be uploaded under *Administration → Settings*. Pages first use their own preview image (*Properties*), then their first image. | empty |

### `auth`
| Key | Meaning | Default |
|---|---|---|
| `order` | order of the sign-in methods (provider IDs). `local` is **always checked first** | `[local]` |
| `session_ttl` | validity of a browser session (absolute) | `168h` |
| `session_idle` | a browser session ends after this much inactivity. Automatic requests (e.g. notifications) do not count as activity. The last activity is stored at most once per minute (no write per request): a session can therefore end up to one minute earlier, never later. `0` = only `session_ttl` | `12h` |
| `photo_visibility` | who sees profile photos as long as the person chooses nothing: `nobody` (only the person), `users` (signed-in people), `public` (everyone, guests too). Whoever has the global permission *See all profile photos* (`core.photos.view`) sees every photo (administrators always) | `users` |
| `photo_visibility_max` | the furthest a person may share their photo. Narrower is always possible | `public` |
| `photo_visibility_fixed` | `true`: no choice, `photo_visibility` always applies | `false` |
| `api_key_max_ttl` | maximum validity of an API key | `8760h` |
| `ssh_token_ttl` | validity of a token after SSH sign-in | `1h` |
| `max_login_failures` | failed attempts until a local account is locked | `10` |
| `lockout_duration` | duration of the lock | `15m` |
| `min_password_length` | minimum length of local passwords (≥ 8) | `12` |

### `retention`
| Key | Meaning | Default |
|---|---|---|
| `revisions_keep_days` | delete older versions after this many days (`0` = never) | `0` |
| `revisions_keep_min` | always keep at least this many versions per page and language (≥ 1) | `50` |
| `trash_days` | delete trash entries finally after this many days (`0` = never) | `30` |
| `audit_days` | delete log entries after this many days (`0` = never) | `0` |
| `mail_days` | delete sent e-mails from the outbox after this many days (`0` = never) | `90` |

The current version is never deleted. Only items in the trash are deleted. The rules run every hour or at once with *Settings → Apply retention now* or `ewiki retention run`.

### `plugins`
| Key | Meaning |
|---|---|
| `disabled` | disabled plugin IDs (required plugins such as `auth.local` cannot be disabled) |
| `dir` | plugin directory in the source tree |
| `catalog` | plugins that can be installed with one click under *Plugins* (`id`, `name`, `repo`, optional `ref`, `description`) |
| `macros.*` | security switches for the shell and HTTP macro, see below. **Can only be changed in this file**, not through the interface or the API |
| `auto_restart_on_drop` | restart at once after the automatic build of plugins copied into the plugin folder (default `false`: *Administration → Plugins* offers **Restart**); also settable under *Administration → Plugins* |
| `updates.check` | automatic update check when opening *Administration → Updates* (default `true`) |
| `updates.core_repo` | repository of the core (default `github.com/mtsonline/enterpriseWiki`) |
| `updates.api_url` | GitHub API (default `https://api.github.com`, for GitHub Enterprise e.g. `https://github.example.com/api/v3`) |
| `updates.token` | optional GitHub token for private repositories and higher request limits, e.g. `${EWIKI_GITHUB_TOKEN}` |

#### `plugins.macros`

```yaml
plugins:
  macros:
    shell_enabled: false          # switch the shell macro on
    shell_approvers:              # superadmins who may approve commands
      - admin@local
    shell_timeout: 10s            # at most 5m
    shell_user: nobody            # only relevant when the wiki runs as root
    http_allow:                   # URL prefixes for the HTTP macro
      - https://status.example.com/api/
    http_allow_private: false     # allow internal addresses (10.x, 192.168.x, localhost, …)
```

- **Shell macro:** a command only runs once a superadmin has approved exactly this command text (bound to its SHA-256 hash). Every change needs a new approval. Only signed-in administrators listed in `shell_approvers` (not with an API key) and the local command line may approve: `ewiki api POST /api/v1/x/macro.shell/commands '{"command":"uptime","approve":true}'`. Open and approved commands are under *Administration → Shell commands*.
- **HTTP macro and workflow webhooks:** only URLs under a prefix from `http_allow` (same scheme, same host, path prefix). Connections to internal addresses are blocked after DNS resolution, as long as `http_allow_private` is not set. With `http_allow_private: true` only the allow list applies. A proxy from `HTTPS_PROXY` is only used when internal addresses are allowed (otherwise the block could be bypassed through the proxy).

### `mail`
Outgoing e-mails (notifications, workflow e-mails). Also settable under *Administration → E-mail*, where there is a test message, the outbox and "send now".

```yaml
mail:
  enabled: true              # false: e-mail switched off completely (tests, installations without mail delivery)
  transport: auto            # auto | smtp | sendmail | log | none
  from: wiki@example.com     # sender address (must be allowed to send through the mail server: SPF/DKIM)
  from_name: Enterprise Wiki
  reply_to: ""               # optional reply address
  subject_prefix: "[Wiki]"   # optional
  allowed_from:              # further senders for workflow e-mails
    - "*@hr.example.com"
  concurrency: 4             # parallel SMTP connections (1–64)
  rate_per_minute: 0         # upper limit of recipients per minute (0 = unlimited), if the provider throttles
  smtp:
    host: smtp.example.com
    port: 587                # empty: 587 (starttls), 465 (tls), 25 (none)
    security: starttls       # starttls | tls | none
    username: wiki@example.com
    password: ${EWIKI_SMTP_PASSWORD}   # value in .env
    skip_verify: false       # only for internal relays with a self-signed certificate
  sendmail: /usr/sbin/sendmail
  bounce:                    # optional: bounce mailbox (see below)
    address: bounces@example.com
    verp: true               # bounces+m12r34@example.com: assigns bounces unambiguously
    imap_host: imap.example.com
    imap_port: 993
    security: tls            # tls | starttls | none
    username: bounces@example.com
    password: ${EWIKI_BOUNCE_PASSWORD}
    folder: INBOX
    delete_processed: false  # delete processed bounces instead of only marking them as read
```

**Reliable delivery (queue).** Every message first goes to the outbox, in the same transaction as the change that triggers it. So nothing gets lost, not even in a crash. Every recipient has its own row (`mail_recipients`) with status, attempts and the last answer of the mail server:

- A recipient only counts as *sent* when the mail server has accepted it with `250`.
- **Temporary errors** (4xx, connection lost, timeout, server not reachable) are retried **without limit**: after 1, 2, 5, 15 and 30 minutes, then every hour. The message stays in the outbox that long.
- **Permanent errors** (5xx, e.g. "mailbox does not exist") and bounces from the bounce mailbox only mark this recipient as *failed*. The sender or the triggering person (e.g. whoever changed a page or carried out a workflow step) gets a message with the addresses concerned and the reason.
- Several server instances can send at the same time: messages are reserved with `FOR UPDATE SKIP LOCKED` and a lease. If a process aborts, another one takes over after the lease expires.
- Delivery uses one SMTP connection for many messages and `concurrency` parallel connections. New messages are triggered at once; a background job also checks every 10 seconds.

With `transport: auto`:

1. If `smtp.host` is set, mail is sent through this SMTP server (recommended: the mail server or relay of your organisation, Microsoft 365, Google Workspace, Mailjet …).
2. Otherwise the `sendmail` program of the server is used, if present (e.g. Postfix or msmtp, which forwards the mails itself).
3. Otherwise **nothing is sent**: the messages stay in the outbox as "held". As soon as a delivery route is set up, held messages of the last 3 days are sent automatically, older ones only after confirmation under *Administration → E-mail*. Direct delivery to the mail servers of the recipients (without a relay) is deliberately not built in: it almost always ends up as spam or is blocked (port 25, missing SPF/DKIM).

`log` only writes messages to the server log (test), `none` keeps everything in the outbox. With `enabled: false` **no** messages are created (not even in the outbox). Notifications, watches and workflow e-mails are then left out, and the interface points this out.

**Bounces.** Many mail servers first accept a message and only report later by e-mail that it could not be delivered. With `bounce` the bounce address is used as the envelope sender (Return-Path), with `verp: true` unambiguously per recipient (`bounces+m<message>r<recipient>@…`). The wiki reads the mailbox every 5 minutes through IMAP, recognises delivery reports (DSN, RFC 3464) and marks the recipient as failed; the sender is informed as with 5xx. The bounce address must be allowed as a sender at the mail server (with VERP: plus addresses or a catch-all for `bounces+*`).

**S/MIME (plugin `signature.smime`).** Outgoing e-mails are signed when a valid certificate fits the sender address: first a personal certificate of the person the address belongs to (confirmed address), otherwise one of the organisation. Certificates are uploaded under *Administration → S/MIME* or *My profile → S/MIME certificate* (PKCS#12 with a password or PEM; the password is not stored, the private key is stored in the database like all secrets with `secrets.mode`). Several certificates per address are possible: the newest valid one always applies, so a renewed certificate takes over from the start of its validity. 60, 30, 14, 7 and 1 day before it expires and once afterwards, administrators (or the person) get a notification in the wiki and by e-mail. Settings:

```yaml
plugin_settings:
  signature.smime:
    mail_signing: true      # sign e-mails (default)
    on_expired: unsigned    # or hold: hold e-mails of a sender without a valid certificate in the outbox (retried every 15 minutes)
    personal: true          # allow personal certificates
    seal_evidence: false    # also seal signature evidence with the certificate of the organisation
```

Signing follows RFC 8551 (`multipart/signed`, CMS with SHA-256, certificate chain without the root). Certificates of the organisation must contain an address the wiki uses as a sender (`from` or `allowed_from`); personal certificates a confirmed address of the person. Accepted are RSA from 2048 bits and ECDSA P-256/P-384/P-521 with the key usage "e-mail protection".

Senders: workflow e-mails may only use `from` or addresses from `allowed_from` as the sender; other requested senders are set as the reply address (Reply-To), so no foreign senders are forged and SPF/DMARC do not fail.

**Summaries of watches.** Watches with `frequency: daily` or `weekly` are sent once a day at `digest_hour` o'clock or weekly on `digest_weekday` (0 = Sunday, 1 = Monday …), in the time zone `digest_timezone` (IANA name, empty = time zone of the server). Default: 7 o'clock, Monday.

```yaml
mail:
  digest_hour: 7
  digest_weekday: 1
  digest_timezone: Europe/Vienna
```

In the Docker container `EWIKI_MAIL_FROM`, `EWIKI_SMTP_HOST`, `EWIKI_SMTP_PORT`, `EWIKI_SMTP_SECURITY`, `EWIKI_SMTP_USER` and `EWIKI_SMTP_PASSWORD` are taken over on the first start (see `.env.example`).

### `cache`
Optional cache for guests (visitors who are not signed in). Also settable under *Administration → Cache*.

```yaml
cache:
  enabled: false     # switch on for public wikis/websites with many guests
  ttl_seconds: 300   # maximum age of an entry (0 = until the next change)
  max_entries: 5000  # 0 = unlimited
  max_mb: 128        # memory limit (0 = unlimited)
```

For guests the API answers are kept in memory. When a page is requested, the server embeds the data this page needed last time. The page then appears with **one** request and without database access. Signed-in people always see the current data.

**Every change clears the cache**: pages, categories, menus, blocks, permissions, users and groups, settings, plugins, translations, events. The reason is macros: a page can show lists of other pages, events or team members, so clearing only part of it would be unsafe. In a cluster and for changes through the command line a counter in the database (`cache_state`) makes sure that all servers clear their cache within 3 seconds. This counter is queried independently of the number of visitors. `ttl_seconds` limits time-dependent content such as "next events".

Command line: `ewiki cache status`, `ewiki cache clear`. API: `GET/DELETE /api/v1/admin/cache`.

### `secrets`

How secrets are stored in the database (details: [security](security.md#secrets-in-the-database)).

```yaml
secrets:
  mode: pq                     # plain (default) | aes | pq
  key_file: secrets.key        # relative to config.yaml or absolute
  password_env: EWIKI_SECRETS_PASSWORD   # optional: password of the key file
```

### `license`
License key of the organisation (see [LICENSING.md](../../../LICENSING.md)). Easiest to install under *Administration → License*
or with `ewiki license install KEY`; the check is done offline.

```yaml
license:
  key: EWL1.eyJpZCI6…   # or ${EWIKI_LICENSE} with the value in .env
```

### `feeds`
External RSS/Atom feeds (macro "news from several sources") and iCal subscriptions (further sources of a calendar, macro "next events") are fetched in the background every 10 minutes and cached in the database; pages never wait for a foreign address. At most 2 MB per fetch, only `http(s)`, redirects only to public addresses; fetches nobody has needed for 30 days are forgotten.

```yaml
feeds:
  allow_private: false  # true allows feeds and calendars in the internal network (10.x, 192.168.x, localhost …)
```

The feeds of the carousel have their own setting in the plugin (*Administration → Plugins → Carousel*).

### `lists`
Upper limit for lists from sources (macros news, blog, FAQ, property report …) and for search and filter sources. Below it, editors choose the maximum number and whether to page from a certain number in the macro itself. Also under *Administration → Settings → Lists*.

```yaml
lists:
  max_items: 0  # 0 = 1000; 50 to 10000
```

### `webhooks`
Webhooks themselves are created under *Administration → Webhooks* (API `/api/v1/admin/webhooks`). `config.yaml` only contains:

```yaml
webhooks:
  allow_private: false  # true allows targets in the internal network (10.x, 192.168.x, localhost …)
  keep_days: 30         # keep delivered and failed deliveries this long
```

Formats: **Teams** (Adaptive Card; in Teams create a workflow "Post to a channel when a Teams webhook request is received"), **Slack** (incoming webhook) and **JSON** for own systems:

```json
{"event": "element.update", "occurred_at": "2026-11-02T16:30:00Z", "site": "Intranet", "actor": "Anna Example",
 "object": {"type": "element", "id": 12, "title": "Travel expenses", "url": "https://wiki.example.com/travel-expenses"}, "data": {}}
```

Headers: `X-Ewiki-Event`, `X-Ewiki-Delivery` and with a signing key `X-Ewiki-Signature: sha256=<HMAC-SHA256 of the content>`. Events: `element.create|update|delete|move|restore|rollback`, `category.create|update|delete|move|restore`, `event.registration`, `event.registration_cancelled`, `event.occurrence_changed` (data: `occurrence`, `start`, `when`, `calendar`, `location`, `status`, `participant`), `comment.create` (data: `author`, `excerpt`, `reply`, `element_id`), `workflow.started`, `workflow.completed`, `workflow.cancelled` (data: `workflow`, `status`, `outcome`, `step`). Deliveries run through a queue; on errors they are retried after 1, 5, 30 minutes, 2 and 12 hours. Targets in the internal network are blocked without `allow_private` (protection against SSRF), redirects only within the webhook address.

### `plugin_settings`
Freely structured settings per plugin, e.g.

```yaml
plugin_settings:
  acme.kanban:
    columns: [todo, doing, done]
```

Plugins read them through `Host.Settings()`. Set them with the CLI: `ewiki config set plugin_settings.acme.kanban.columns "[todo, done]"`.

Further plugin settings:

| Plugin | Keys | Meaning |
|---|---|---|
| `search.external` | `engine`, `url`, `index`, `api_key`, `username`, `password` | external search engine, see [plugins.md](plugins.md#external-search-engine) |
| `core.comments` | `write_permission` | `view` (default): every signed-in user with read permission may comment; `edit`: only with edit permission |

The settings of the sign-in methods (`auth.ldap`, `auth.oidc`, `auth.shib`) are described in [authentication.md](authentication.md).

## Command line

```bash
ewiki config show                         # show the file
ewiki config get auth.session_ttl
ewiki config set retention.trash_days 60
ewiki config set i18n.languages "[en, de, fr]"
ewiki config validate
```

Further sites of an instance have their own settings files (`sites/<name>.yaml`), see [sites.md](sites.md).

### `limits` (expert settings)

Technical limits of the core and all plugins. Each has a default and an allowed range; `config.yaml` only contains what differs from the default. Change them under **Administration → Expert settings** (all values in one place, with explanation, default and range; changes are in the audit log as `settings.limits`), with `PUT /api/v1/admin/limits` or directly:

```yaml
limits:
  core.request.guest_kb: 128
  core.rate.user_per_second: 100
```

The limits apply to the whole instance; in further sites only administrators of the instance change them. Plugins register their own limits (the key starts with the plugin ID); they appear in the same list.

| Key | Meaning | Default |
|---|---|---|
| `core.request.guest_kb` | largest request without a session and without an API key (sign-in, requests of guests; form submissions have their own limit) | 64 kB |
| `core.request.body_kb` | largest request of signed-in users; routes that need more register their own limit | 1024 kB |
| `core.request.content_kb` | saving pages, drafts, templates and previews | 4096 kB |
| `core.request.sso_kb` | messages from identity providers (SAML, OIDC) | 1024 kB |
| `core.request.import_mb` | import of categories and translations | 64 MB |
| `core.request.large_parallel` | requests above 1 MB processed at the same time (uploads are streamed through and do not count) | 4 |
| `core.request.large_wait_s` | a large request waits this long for a free slot, then `503` (nothing stored, can be repeated) | 30 s |
| `core.rate.user_per_second`, `core.rate.user_burst` | requests per account: sustained rate and reserve for short peaks | 50 / 500 |
| `core.rate.guest_per_second`, `core.rate.guest_burst` | requests per address without sign-in | 20 / 200 |
| `core.rate.expensive_per_second`, `core.rate.expensive_burst` | additionally for search, preview, export and upload | 5 / 50 |
| `core.live.persons` | people who edit a page together at the same time | 50 |
| `core.live.connections_per_person` | windows or devices of one person on the same page | 5 |
| `core.audit.search_days` | how far back the text search in the before/after values of the audit log reaches when no start date is given (with a start date: any period) | 90 days |
| `core.reactions.per_person` | plugin *Reactions*: different emojis of one person per target | 10 |
| `content.forms.submit_mb` | plugin *Forms*: largest submission with all files, guests included. Each form only accepts the files its fields allow (number × size); this limit caps all forms | 200 MB |
| `workflow.process.trigger_per_hour` | plugin *Workflow*: starts of a trigger per hour (schedule, change, event, incoming webhook); further starts are left out and shown in the editor | 60 |
| `core.audit.value_guest_bytes` | longest value in the audit log for events without a signed-in user | 1024 bytes |
| `core.audit.value_bytes` | longest before/after value for events of signed-in users | 65536 bytes |
| `core.ui.wait_notice_s` | after this time without an answer to a change, the interface asks "keep waiting or cancel"; the input is kept in both cases | 5 s |

Too large requests get `413`, too many `429` with `Retry-After`.

**Limit per API key.** Every API key can have its own limit in requests per second:

- Whoever owns a key can limit it **below** the limit of the account when creating it (*My profile*) or with `PUT /api/v1/me/api-keys/{id}/rate {"rate_per_second": 5}`. The key still counts for the account too: several keys do not multiply the quota.
- Administrators grant an **own quota** under **Administration → Expert settings → API keys** (or `PUT /api/v1/admin/api-keys/{id}/rate`), which may also be above the limit of the account, e.g. for integrations. Such a key no longer counts for the account; the person cannot change the value.
- `{"rate_per_second": null}` removes the own limit. The reserve for short peaks follows the ratio of the accounts (default: ten times the rate). Changes are in the audit log (`apikey.rate`).
