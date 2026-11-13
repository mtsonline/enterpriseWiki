# REST API

The web interface only uses this API. Everything that works in the interface also works with scripts or through `ewiki api` (CLI).

- Base: `/api/v1`, JSON (UTF-8). Plugin routes are under `/api/v1/x/<plugin-id>/…`.
- Errors: HTTP status plus `{"error": {"code": "not_found", "message": "…"}}`. Codes: `bad_request`, `unauthorized`, `invalid_credentials`, `forbidden`, `csrf`, `not_found`, `conflict`, `rate_limited`, `internal`.
- Multilingual fields are objects `{"de": "…", "en": "…"}`.
- Permissions as a bit mask: 1 = view, 2 = edit, 4 = create, 8 = delete/move, 16 = manage permissions.

## Authentication

| Method | Usage |
|---|---|
| session cookie | `POST /api/v1/auth/login {"username","password"}` sets `ewiki_session`. Writing requests – also the sign-in itself and requests of guests – need the header `X-Requested-With: ewiki` |
| API key | `Authorization: Bearer ewk_…` (create it under *My profile*) |
| SSH key | get a challenge, sign it with `ssh-keygen -Y sign -n ewiki-login`, send the token as `Authorization: Bearer …` (see `scripts/ewiki-ssh-login.sh`) |
| LDAP accounts | like local accounts through `POST /api/v1/auth/login` (also for scripts); further factors afterwards through `/api/v1/auth/mfa/…` |
| SSO (OIDC/SAML) | only in the browser: `/api/v1/auth/sso/<id>/start?next=/path`. For scripts then create an API key or SSH key |

```bash
KEY=ewk_...
curl -H "Authorization: Bearer $KEY" https://wiki.example.com/api/v1/categories?parent=0
curl -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
     -d '{"category_id": 3, "type": "core.html", "lang": "en", "title": "New page",
          "body": {"format": "markdown", "source": "# Hello"}}' \
     https://wiki.example.com/api/v1/elements
```

### Sign-in from scripts

For scripts an API key is the simplest way. Whoever works with user name and password instead sends **the header `X-Requested-With: ewiki` with every writing request without an API key – also with the sign-in itself**. Without it the answer is `403` with `"code": "csrf"`. The header protects browsers from foreign pages making requests in the name of the visitors or signing them into a foreign account; scripts simply send it along.

```bash
WIKI=https://wiki.example.com
JAR=$(mktemp)            # file for the session cookie

# 1. sign in: the cookie ewiki_session ends up in $JAR
curl -sS -c "$JAR" -H 'X-Requested-With: ewiki' -H 'Content-Type: application/json' \
     -d '{"username": "anna", "password": "…"}' "$WIKI/api/v1/auth/login"

# 2. read: the cookie is enough
curl -sS -b "$JAR" "$WIKI/api/v1/auth/me"

# 3. write: cookie and header
curl -sS -b "$JAR" -H 'X-Requested-With: ewiki' -H 'Content-Type: application/json' \
     -d '{"category_id": 3, "type": "core.html", "title": "New page",
          "body": {"format": "markdown", "source": "# Hello"}}' "$WIKI/api/v1/elements"

# 4. sign out
curl -sS -b "$JAR" -H 'X-Requested-With: ewiki' -X POST "$WIKI/api/v1/auth/logout"; rm -f "$JAR"
```

The same in Python:

```python
import requests

s = requests.Session()
s.headers["X-Requested-With"] = "ewiki"      # applies to all requests of the session
r = s.post("https://wiki.example.com/api/v1/auth/login", json={"username": "anna", "password": "…"})
r.raise_for_status()
print(s.get("https://wiki.example.com/api/v1/auth/me").json())
```

If the account requires a second factor, the sign-in answers with `mfa_required` and a token for `/api/v1/auth/mfa/…`; for unattended scripts therefore use an API key or SSH key. Requests with `Authorization: Bearer …` do not need the header.

**Limits.** The wiki answers too many requests with `429` and `Retry-After` (seconds), too large ones with `413`, short-term overload with `503` and `Retry-After`. In all three cases nothing was stored; the request can be repeated unchanged after the time given. An API key can have its own limit (`rate_per_second` when creating it, see [configuration](../admin/configuration.md#limits-expert-settings)).

## Important flows

- **Saving a page:** `PUT /api/v1/elements/{id}` with `{"lang", "title", "body", "comment", "base_rev"}`. `base_rev` is the version the editor started with (0 for a new translation). If another version was saved in the meantime, `409 conflict` comes back. Every save creates a new version.
- **Translation:** saving in a language other than `original_lang`. The status per language is in `translations` (`original`, `current`, `outdated`, `missing`), the progress in `translated_percent`. If a language is missing, `GET /elements/{id}?lang=xx` delivers the original language with `fallback: true`.
- **Content of an element** (`body`) depends on the element type; for `core.html` it is `{"format": "html"|"markdown", "source": "…"}`. `GET /elements/{id}?edit=1` delivers `body` (only with edit permission), `html` is always the sanitised rendering.
- **Resetting:** `POST /api/v1/categories/{id}/rollback {"at": "2026-10-01T12:00:00Z", "recursive": true}`.
- **Files:** `POST /api/v1/files` (multipart, field `file`) delivers `{hash, name, mime, size}`. An element `core.attachment` refers to the file with `{"file": {"hash": "…"}, "description": "…"}`. Downloading with permission check through `GET /api/v1/x/core.attachment/elements/{id}/file[?hash=…][&inline=1]`. Images are also available under `GET /api/v1/files/{hash}` (for images in pages).
- **Search:** `GET /api/v1/search?q=word&tags=a,b&lang=en` delivers `snippet` as HTML-escaped text with `<mark>`. With `category=ID` only this category and its sub-categories are searched (`recursive=0`: only the category).
- **Listing elements:** `GET /api/v1/elements?q=&type=&category=&recursive=1&sort=title|updated|created|type&limit=&offset=` delivers `{items, total}` with `category_path` (only visible elements, all for administrators).
- **Short URLs:** `short_url` with `POST /elements`, `PATCH /elements/{id}`, `POST/PUT /categories` (empty = remove). `GET /api/v1/resolve?path=calendar` delivers `{type, id}`.
- **Watching:** `PUT /api/v1/me/watches {"target_type": "category"|"element", "target_id": 12, "mode": "link"|"content", "recursive": true}`; status for a target: `GET /api/v1/me/watches?target_type=element&target_id=5`.
- **Feeds:** `GET /api/v1/feeds/categories/{id}?format=atom|rss&lang=en[&content=1]` (when switched on in the category), blog/FAQ under `/api/v1/x/content.blog|content.faq/elements/{id}/feed`. Feed readers without sign-in use the personal key from `GET /api/v1/me/feed-token` as `&token=…`. It only counts for feed and calendar routes and can be renewed with `POST` or deleted with `DELETE`.
- **Sources:** fields such as `more_sources` (blog, FAQ, calendar), `sources` (carousel) and the macro parameter `source` take sources as text: `category:12+sub; category:7; tag:hr; type:core.html; elements:4,9; search:holiday; props:status = open && due <= today+7d`, depending on the place additionally `feed:https://…` (macro `news`), `calendar:ID` and `ical:https://…` (calendar, macro `events`). **Forms** (`content.forms`): `GET …/elements/{id}/start` delivers the token (signed, at least 2 s old, at most 24 h), the state (`""`, `not_open`, `closed`, `sign_in`, `full`, `already`) and the pre-filled values; `POST …/elements/{id}/submit` as `multipart/form-data` with the part `data` = `{"values": {ID: value}, "token": "…", "website": "", "nonce": "…"}` (`nonce` only with `pow_bits` from `start`: the SHA-256 of `token:nonce` starts with this many zero bits; repeating groups as a list of objects, errors there as `GROUP.ROW.FIELD`; signature as text or file `file.ID` (PNG)) and files `file.FIELD` (comments `comment.FIELD.N`) or as JSON without files; answer `201 {id, message}`, on errors `422 {"error": {"code": "invalid", "fields": [{field, code, param}]}}`. `search:` (full-text search, at most 100 hits) and `props:` (property filter as in the report, filters separated by `&&`, at most 200 hits; only with the property plugin active) are resolved with the permissions of the readers. A `-` before a source excludes it (`-tag:internal`), a `+` requires it additionally (`+tag:top`); in JSON as `"mode": "exclude" | "require"`. `POST /api/v1/sources/preview` with `{sources, category_id, lang}` delivers `{total, items}` (the first 5 titles) with the permissions of the caller (signed in only). **Pages of macro lists:** parameters `limit`, `paginate` (`yes`/`no`), `per_page`; `GET /api/v1/elements/{id}/macro/{index}?page=N&lang=` delivers only this macro (`{html}`), `GET /api/v1/elements/{id}?mp=INDEX-PAGE` the whole page with this page of the list. Upper limit: setting `lists.max_items` (`PUT /api/v1/admin/settings` with `{"lists": {"max_items": 2000}}`). Invalid input gives `400`. `GET /api/v1/x/content.events/calendars/{id}/events` also delivers events of further calendars (`source`, read-only) and external events (`external: true`, negative `id`).
- **Website template per category:** `PUT /api/v1/categories/{id}` with `{"template": "theme.cms"}` (only active template plugins, `""` = inherit; permission *Manage*). Categories deliver `template` (own) and `effective_template` (the one that applies, `""` = site template); `GET /api/v1/site` lists the active templates under `templates`.
- **Spaces:** `GET /api/v1/spaces` (directory, only visible ones), `GET /api/v1/spaces/{key}` (`404` if not visible), `PUT /api/v1/categories/{id}/space` with `{"key": "HR", "own_tree": true, "shortcuts": [{"label": {"en": "Forms"}, "url": "/wiki/hr/forms"}], "allowed_types": ["core.html"], "language": "en"}` (top level only, permission *Manage*; duplicate key `409`). `GET /api/v1/categories/{id}` also delivers `space` (the space of the category or `null`). New space from a template: `POST /api/v1/spaces` with `{"template_id": 5, "titles": {"en": "Project X"}, "key": "PX", "with_pages": false}` (category administrators; template = space with `is_template`). Archiving: `POST /api/v1/categories/{id}/archive` with `{"archived": true|false}` (*Manage*); categories deliver `archived` (also inherited) and `archived_at`. Export: `GET /api/v1/categories/{id}/export` (JSON Lines, *Manage*); import: `POST /api/v1/categories/import?parent=ID` with the file as body (`0` = new space), answer `{category_id, categories, elements, templates, skipped}`. Page templates of a space: `space_id` when saving, `GET /api/v1/templates?category=ID`. Personal space: `POST /api/v1/spaces/personal` (permission `core.spaces.personal`; delivers the existing one if there is one). Spaces deliver `member` (own permissions as person, group or role) and `personal`. Requesting access: `POST /api/v1/access-requests` with `{"type": "category"|"element", "id": 12, "message": "…"}` (always `{"sent": true}`, at most 10 per day, otherwise `429`); open requests `GET /api/v1/categories/{id}/access-requests` (*Manage*), answering `POST /api/v1/access-requests/{id}` with `{"grant": true, "preset": "reader"|"editor"}` or `{"grant": false}`. Classification: `PUT /api/v1/categories/{id}` with `{"classification": 0..3}` (`-1` = as above); categories deliver `classification` and `effective_classification`. Views: `POST /api/v1/elements/{id}/view` (only counting, per page and day); analytics: `GET /api/v1/categories/{id}/insights?days=30&lang=en` (*Manage*). Further space settings: `allowed_macros` (empty = all), `accent` (`#rrggbb`, contrast ≥ 4.5:1 to white, otherwise `400`), `logo` (`/api/v1/files/…` or `/api/v1/media/…`).
- **Property extensions:** `POST /api/v1/x/core.properties/query.csv|query.xlsx` with a query or the body of `/report` (Excel without formulas, never strictly confidential rows); `POST …/trend` with `{"report": {…}, "key": "status", "weeks": 12}` → `{weeks, series:[{value, values}]}`; saved views `GET|POST …/databases/{id}/views`, `DELETE …/views/{id}`; subscriptions `GET|POST …/subscriptions`, `DELETE …/subscriptions/{id}` (`frequency`: `daily`|`weekly`). With `"export_mode": "original"` (query) or the parameter `export_mode` (report, database) values with `=` become formulas and CSV cells are not protected. Categories: `"watermark": "auto"|"off"|""` and `"downloads": "auto"|"allow"|""` (`""` = as above).
- **Events:** `POST /api/v1/x/content.events/calendars/{id}/events {"title", "starts_at", "ends_at", "all_day", "tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;BYDAY=MO", "registration": true, "capacity": 20, "reminders": [1440]}`; events of a period with `GET …/calendars/{id}/events?from=…&to=…` (series are expanded, `occurrences[].date` is the key for registrations). Registering: `POST …/events/{id}/registrations {"occurrence": "2026-11-23"}` (`interested: true` only for reminders).
- **E-mail queue (admin):** `GET /api/v1/admin/mail` (status with counters per recipient), `GET /api/v1/admin/mail/outbox?status=failed&q=…`, `GET /api/v1/admin/mail/outbox/{id}` (with recipients and the last server answer).

## Endpoints

### System

| Method | Path |
|---|---|
| `GET` | `/readyz` |
| `GET` | `/api/v1/site` |
| `GET` | `/healthz` |

### Sign-in & single sign-on

| Method | Path |
|---|---|
| `GET` | `/api/v1/auth/providers` |
| `POST` | `/api/v1/auth/login` |
| `POST` | `/api/v1/auth/logout` |
| `GET` | `/api/v1/auth/me` |
| `POST` | `/api/v1/auth/ssh/challenge` |
| `POST` | `/api/v1/auth/ssh/verify` |
| `POST` | `/api/v1/auth/verify-email` |
| `POST` | `/api/v1/auth/link/info` |
| `GET` | `/api/v1/auth/sso/{provider}/start` |
| `GET` | `/api/v1/auth/sso/{provider}/callback` |
| `POST` | `/api/v1/auth/sso/{provider}/callback` |
| `POST` | `/api/v1/auth/mfa/info` |
| `POST` | `/api/v1/auth/mfa/challenge` |
| `POST` | `/api/v1/auth/mfa/verify` |
| `POST` | `/api/v1/auth/mfa/enroll/begin` |
| `POST` | `/api/v1/auth/mfa/enroll/finish` |
| `POST` | `/api/v1/auth/passwordless/{method}/begin` |
| `POST` | `/api/v1/auth/passwordless/{method}/finish` |
| `GET` | `/api/v1/auth/sso/{provider}/logout` |
| `POST` | `/api/v1/auth/sso/{provider}/logout` |

### Own account (incl. 2FA, API keys, SSH)

| Method | Path |
|---|---|
| `PUT` | `/api/v1/me` |
| `GET` | `/api/v1/me/prefs` |
| `PUT` | `/api/v1/me/password` |
| `GET` | `/api/v1/me/api-keys` |
| `POST` | `/api/v1/me/api-keys` |
| `DELETE` | `/api/v1/me/api-keys/{id}` |
| `PUT` | `/api/v1/me/api-keys/{id}/rate` |
| `GET` | `/api/v1/admin/api-keys` |
| `PUT` | `/api/v1/admin/api-keys/{id}/rate` |
| `GET` | `/api/v1/me/ssh-keys` |
| `POST` | `/api/v1/me/ssh-keys` |
| `DELETE` | `/api/v1/me/ssh-keys/{id}` |
| `GET` | `/api/v1/me/inbox` |
| `POST` | `/api/v1/me/inbox/read` |
| `DELETE` | `/api/v1/me/inbox/{id}` |
| `GET` | `/api/v1/me/watch-assignments` |
| `PUT` | `/api/v1/me/watch-assignments/{id}` |
| `GET` | `/api/v1/me/emails` |
| `POST` | `/api/v1/me/emails` |
| `POST` | `/api/v1/me/emails/{id}/resend` |
| `POST` | `/api/v1/me/emails/{id}/primary` |
| `DELETE` | `/api/v1/me/emails/{id}` |
| `GET` | `/api/v1/me/identities` |
| `DELETE` | `/api/v1/me/identities` |
| `POST` | `/api/v1/me/identities/link` |
| `PUT` | `/api/v1/me/alias` |
| `GET` | `/api/v1/me/notifications` |
| `PUT` | `/api/v1/me/notifications` |
| `GET` | `/api/v1/me/watches` |
| `PUT` | `/api/v1/me/watches` |
| `DELETE` | `/api/v1/me/watches/{id}` |
| `GET` | `/api/v1/me/feed-token` |
| `POST` | `/api/v1/me/feed-token` |
| `DELETE` | `/api/v1/me/feed-token` |
| `GET` | `/api/v1/me/profile` |
| `PUT` | `/api/v1/me/profile` |
| `GET` | `/api/v1/media` |
| `POST` | `/api/v1/media` |
| `GET` | `/api/v1/media/{id}` |
| `PATCH` | `/api/v1/media/{id}` |
| `DELETE` | `/api/v1/media/{id}` |
| `POST` | `/api/v1/media/{id}/file` |
| `POST` | `/api/v1/media/{id}/edit` |
| `GET` | `/api/v1/media/{id}/versions` |
| `POST` | `/api/v1/media/{id}/versions/{v}/restore` |
| `GET` | `/api/v1/media/{id}/usage` |
| `GET` | `/api/v1/media/{id}/acl` |
| `GET` | `/api/v1/media/{id}/view` |
| `GET` | `/api/v1/media/{id}/download` |
| `GET` | `/api/v1/me/mfa` |
| `POST` | `/api/v1/me/mfa/{method}/begin` |
| `POST` | `/api/v1/me/mfa/{method}/finish` |
| `GET` | `/api/v1/signature-methods` |
| `GET` | `/api/v1/signature-keys` |
| `GET` | `/api/v1/signatures/{id}` |
| `GET` | `/api/v1/signatures/{id}/verify` |
| `GET` | `/api/v1/signatures/{id}/export` |
| `POST` | `/api/v1/signatures/verify` |
| `GET` | `/api/v1/media/viewers` |
| `GET` | `/api/v1/media/{id}/open` |
| `POST` | `/api/v1/media/{id}/open` |
| `GET` | `/api/v1/me/sessions` |
| `DELETE` | `/api/v1/me/sessions` |
| `DELETE` | `/api/v1/me/sessions/{id}` |

### Users, groups, roles

| Method | Path |
|---|---|
| `GET` | `/api/v1/admin/users` |
| `POST` | `/api/v1/admin/users` |
| `GET` | `/api/v1/admin/users/{id}` |
| `PUT` | `/api/v1/admin/users/{id}` |
| `DELETE` | `/api/v1/admin/users/{id}` |
| `GET` | `/api/v1/groups` |
| `GET` | `/api/v1/roles` |
| `GET` | `/api/v1/users/lookup` |
| `POST` | `/api/v1/admin/groups` |
| `PUT` | `/api/v1/admin/groups/{id}` |
| `DELETE` | `/api/v1/admin/groups/{id}` |
| `GET` | `/api/v1/admin/groups/{id}/members` |
| `PUT` | `/api/v1/admin/groups/{id}/members/{user}` |
| `DELETE` | `/api/v1/admin/groups/{id}/members/{user}` |
| `POST` | `/api/v1/admin/roles` |
| `PUT` | `/api/v1/admin/roles/{id}` |
| `DELETE` | `/api/v1/admin/roles/{id}` |
| `GET` | `/api/v1/admin/users/{id}/emails` |
| `POST` | `/api/v1/admin/users/{id}/emails` |
| `POST` | `/api/v1/admin/users/{id}/emails/{eid}/primary` |
| `DELETE` | `/api/v1/admin/users/{id}/emails/{eid}` |
| `GET` | `/api/v1/admin/users/{id}/identities` |
| `DELETE` | `/api/v1/admin/users/{id}/identities` |
| `POST` | `/api/v1/admin/users/invite` |
| `PUT` | `/api/v1/admin/users/{id}/alias` |
| `GET` | `/api/v1/admin/users/{id}/profile` |
| `PUT` | `/api/v1/admin/users/{id}/profile` |
| `GET` | `/api/v1/admin/users/{id}/sessions` |
| `DELETE` | `/api/v1/admin/users/{id}/sessions` |
| `DELETE` | `/api/v1/admin/users/{id}/sessions/{sid}` |

### Categories & permissions

| Method | Path |
|---|---|
| `GET` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/tree` |
| `POST` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/{id}` |
| `PUT` | `/api/v1/categories/{id}` |
| `PUT` | `/api/v1/categories/{id}/translations/{lang}` |
| `DELETE` | `/api/v1/categories/{id}` |
| `POST` | `/api/v1/categories/{id}/move` |
| `POST` | `/api/v1/categories/{id}/duplicate` |
| `POST` | `/api/v1/categories/{id}/reorder` |
| `GET` | `/api/v1/categories/{id}/acl` |
| `POST` | `/api/v1/categories/{id}/acl` |
| `DELETE` | `/api/v1/acl/{id}` |
| `GET` | `/api/v1/categories/{id}/history` |
| `POST` | `/api/v1/categories/{id}/rollback` |
| `PUT` | `/api/v1/acl/{id}` |

### Elements & versions

| Method | Path |
|---|---|
| `GET` | `/api/v1/element-types` |
| `POST` | `/api/v1/elements` |
| `POST` | `/api/v1/elements/preview` |
| `POST` | `/api/v1/sources/preview` |
| `GET` | `/api/v1/elements/{id}` |
| `GET` | `/api/v1/elements/{id}/macro/{index}` |
| `PUT` | `/api/v1/elements/{id}` |
| `PATCH` | `/api/v1/elements/{id}` |
| `DELETE` | `/api/v1/elements/{id}` |
| `POST` | `/api/v1/elements/{id}/move` |
| `POST` | `/api/v1/elements/{id}/duplicate` |
| `GET` | `/api/v1/elements/{id}/revisions` |
| `GET` | `/api/v1/elements/{id}/revisions/{lang}/{rev}` |
| `POST` | `/api/v1/elements/{id}/restore` |
| `GET` | `/api/v1/elements` |

### Macros

| Method | Path |
|---|---|
| `GET` | `/api/v1/macros` |

### Files

| Method | Path |
|---|---|
| `POST` | `/api/v1/files` |
| `GET` | `/api/v1/files/{hash}` |

### Trash, search, tags

| Method | Path |
|---|---|
| `GET` | `/api/v1/trash` |
| `POST` | `/api/v1/trash/{type}/{id}/restore` |
| `DELETE` | `/api/v1/trash/{type}/{id}` |
| `GET` | `/api/v1/search` |
| `GET` | `/api/v1/tags` |

### Translations & languages

| Method | Path |
|---|---|
| `GET` | `/api/v1/i18n/status` |
| `GET` | `/api/v1/i18n/{lang}` |
| `GET` | `/api/v1/admin/i18n/{lang}` |
| `PUT` | `/api/v1/admin/i18n/{lang}` |
| `GET` | `/api/v1/admin/i18n/{lang}/export` |
| `POST` | `/api/v1/admin/i18n/{lang}/import` |
| `POST` | `/api/v1/admin/languages` |
| `DELETE` | `/api/v1/admin/languages/{code}` |

### Updates

| Method | Path |
|---|---|
| `GET` | `/api/v1/admin/updates` |
| `POST` | `/api/v1/admin/updates/{id}` |

### Administration

| Method | Path |
|---|---|
| `GET` | `/api/v1/admin/permissions` |
| `PUT` | `/api/v1/admin/permissions/authenticated` |
| `GET` | `/api/v1/admin/settings` |
| `PUT` | `/api/v1/admin/settings` |
| `GET` | `/api/v1/admin/audit` |
| `POST` | `/api/v1/admin/retention/run` |
| `GET` | `/api/v1/admin/plugins` |
| `PUT` | `/api/v1/admin/plugins/options` |
| `POST` | `/api/v1/admin/plugins/{id}/enable` |
| `POST` | `/api/v1/admin/plugins/{id}/disable` |
| `POST` | `/api/v1/admin/plugins/install` |
| `DELETE` | `/api/v1/admin/plugins/{id}` |
| `POST` | `/api/v1/admin/plugins/build` |
| `POST` | `/api/v1/admin/restart` |
| `GET` | `/api/v1/admin/audit/search` |
| `GET` | `/api/v1/admin/audit/export` |
| `GET` | `/api/v1/admin/audit/settings` |
| `PUT` | `/api/v1/admin/audit/settings` |
| `POST` | `/api/v1/admin/audit/forward/test` |
| `GET` | `/api/v1/admin/watch-assignments` |
| `POST` | `/api/v1/admin/watch-assignments` |
| `PUT` | `/api/v1/admin/watch-assignments/{id}` |
| `DELETE` | `/api/v1/admin/watch-assignments/{id}` |
| `GET` | `/api/v1/admin/mail` |
| `POST` | `/api/v1/admin/mail/test` |
| `GET` | `/api/v1/admin/mail/outbox` |
| `GET` | `/api/v1/admin/mail/outbox/{id}` |
| `POST` | `/api/v1/admin/mail/outbox/{id}/retry` |
| `DELETE` | `/api/v1/admin/mail/outbox/{id}` |
| `GET` | `/api/v1/admin/jobs` |
| `POST` | `/api/v1/admin/jobs/{name}/run` |
| `GET` | `/api/v1/admin/cache` |
| `DELETE` | `/api/v1/admin/cache` |
| `GET` | `/api/v1/admin/webhooks` |
| `POST` | `/api/v1/admin/webhooks` |
| `PUT` | `/api/v1/admin/webhooks/options` |
| `GET` | `/api/v1/admin/webhooks/{id}` |
| `PUT` | `/api/v1/admin/webhooks/{id}` |
| `DELETE` | `/api/v1/admin/webhooks/{id}` |
| `POST` | `/api/v1/admin/webhooks/{id}/test` |
| `POST` | `/api/v1/admin/webhooks/{id}/deliveries/{did}/retry` |
| `GET` | `/api/v1/admin/database` |
| `GET` | `/api/v1/admin/license` |
| `PUT` | `/api/v1/admin/license` |
| `DELETE` | `/api/v1/admin/license` |
| `GET` | `/api/v1/admin/limits` |
| `GET` | `/api/v1/admin/secrets` |
| `POST` | `/api/v1/admin/secrets/rewrap` |
| `PUT` | `/api/v1/admin/limits` |
| `GET` | `/api/v1/admin/mfa` |
| `PUT` | `/api/v1/admin/mfa` |
| `POST` | `/api/v1/admin/auth/providers/{id}/preview` |
| `GET` | `/api/v1/admin/scim/clients` |
| `POST` | `/api/v1/admin/scim/clients` |
| `POST` | `/api/v1/admin/scim/clients/{id}/token` |
| `DELETE` | `/api/v1/admin/scim/clients/{id}` |
| `GET` | `/api/v1/admin/demo` |
| `POST` | `/api/v1/admin/demo` |
| `DELETE` | `/api/v1/admin/demo` |
| `POST` | `/api/v1/admin/reset` |

### Plugins

| Method | Path |
|---|---|
| `POST` | `/api/v1/x/auth.ldap/test` |
| `GET` | `/api/v1/x/auth.oidc/check` |
| `GET` | `/api/v1/x/auth.shib/metadata` |
| `GET` | `/api/v1/x/content.blog/elements/{id}/posts` |
| `GET` | `/api/v1/x/content.blog/elements/{id}/feed` |
| `POST` | `/api/v1/x/content.blog/elements/{id}/posts` |
| `POST` | `/api/v1/x/content.blog/elements/{id}/categories` |
| `GET` | `/api/v1/x/content.events/calendars/{id}/events` |
| `POST` | `/api/v1/x/content.events/calendars/{id}/events` |
| `GET` | `/api/v1/x/content.events/events/{id}` |
| `PUT` | `/api/v1/x/content.events/events/{id}` |
| `DELETE` | `/api/v1/x/content.events/events/{id}` |
| `POST` | `/api/v1/x/content.events/events/{id}/registrations` |
| `DELETE` | `/api/v1/x/content.events/events/{id}/registrations` |
| `GET` | `/api/v1/x/content.events/events/{id}/registrations` |
| `DELETE` | `/api/v1/x/content.events/events/{id}/registrations/{rid}` |
| `GET` | `/api/v1/x/content.events/me/registrations` |
| `GET` | `/api/v1/x/content.events/upcoming` |
| `PUT` | `/api/v1/x/content.events/events/{id}/occurrences/{date}` |
| `DELETE` | `/api/v1/x/content.events/events/{id}/occurrences/{date}` |
| `GET` | `/api/v1/x/content.events/calendars/{id}/ical` |
| `GET` | `/api/v1/x/content.events/events/{id}/ical` |
| `GET` | `/api/v1/x/content.faq/elements/{id}/questions` |
| `GET` | `/api/v1/x/content.faq/elements/{id}/feed` |
| `POST` | `/api/v1/x/content.faq/elements/{id}/questions` |
| `POST` | `/api/v1/x/content.faq/elements/{id}/categories` |
| `GET` | `/api/v1/x/content.team/groups` |
| `GET` | `/api/v1/x/content.team/fields` |
| `GET` | `/api/v1/x/content.team/elements/{id}/members` |
| `GET` | `/api/v1/x/core.attachment/elements/{id}/file` |
| `GET` | `/api/v1/x/core.blocks/blocks` |
| `POST` | `/api/v1/x/core.blocks/blocks` |
| `PUT` | `/api/v1/x/core.blocks/blocks/{id}` |
| `DELETE` | `/api/v1/x/core.blocks/blocks/{id}` |
| `GET` | `/api/v1/x/core.carousel/position/{position}` |
| `GET` | `/api/v1/x/core.carousel/targets` |
| `GET` | `/api/v1/x/core.carousel/carousels` |
| `POST` | `/api/v1/x/core.carousel/carousels` |
| `PUT` | `/api/v1/x/core.carousel/carousels/{id}` |
| `DELETE` | `/api/v1/x/core.carousel/carousels/{id}` |
| `GET` | `/api/v1/x/core.carousel/carousels/{id}/slides` |
| `POST` | `/api/v1/x/core.carousel/carousels/{id}/slides` |
| `PUT` | `/api/v1/x/core.carousel/carousels/{id}/slides/{slide}` |
| `DELETE` | `/api/v1/x/core.carousel/carousels/{id}/slides/{slide}` |
| `PUT` | `/api/v1/x/core.carousel/carousels/{id}/order` |
| `POST` | `/api/v1/x/core.carousel/carousels/{id}/refresh` |
| `GET` | `/api/v1/x/core.comments/elements/{id}/comments` |
| `POST` | `/api/v1/x/core.comments/elements/{id}/comments` |
| `PUT` | `/api/v1/x/core.comments/comments/{id}` |
| `DELETE` | `/api/v1/x/core.comments/comments/{id}` |
| `DELETE` | `/api/v1/x/core.favorites/{type}/{id}` |
| `PUT` | `/api/v1/x/core.favorites/order` |
| `POST` | `/api/v1/x/core.html/edit-html` |
| `POST` | `/api/v1/x/core.html/to-markdown` |
| `GET` | `/api/v1/x/core.menu/menus` |
| `GET` | `/api/v1/x/core.menu/menus/{id}` |
| `POST` | `/api/v1/x/core.menu/menus` |
| `PUT` | `/api/v1/x/core.menu/menus/{id}` |
| `DELETE` | `/api/v1/x/core.menu/menus/{id}` |
| `GET` | `/api/v1/x/core.properties/types` |
| `GET` | `/api/v1/x/core.properties/defs` |
| `POST` | `/api/v1/x/core.properties/defs` |
| `PUT` | `/api/v1/x/core.properties/defs/{id}` |
| `DELETE` | `/api/v1/x/core.properties/defs/{id}` |
| `PUT` | `/api/v1/x/core.properties/settings/{id}` |
| `POST` | `/api/v1/x/core.properties/query` |
| `POST` | `/api/v1/x/core.properties/report` |
| `GET` | `/api/v1/x/core.properties/values` |
| `GET` | `/api/v1/x/core.properties/keys` |
| `GET` | `/api/v1/x/core.properties/elements/{id}` |
| `GET` | `/api/v1/x/core.properties/new` |
| `PUT` | `/api/v1/x/core.properties/elements/{id}` |
| `GET` | `/api/v1/x/core.properties/refs` |
| `GET` | `/api/v1/x/core.properties/history/{id}` |
| `GET` | `/api/v1/x/core.reactions/{type}/{id}` |
| `GET` | `/api/v1/x/core.reactions/batch` |
| `POST` | `/api/v1/x/core.reactions/{type}/{id}` |
| `DELETE` | `/api/v1/x/core.reactions/{type}/{id}/users/{uid}` |
| `GET` | `/api/v1/x/core.reactions/settings` |
| `PUT` | `/api/v1/x/core.reactions/settings` |
| `POST` | `/api/v1/x/core.share/check` |
| `POST` | `/api/v1/x/core.share/share` |
| `GET` | `/api/v1/x/example.notice/tones` |
| `GET` | `/api/v1/x/macro.shell/commands` |
| `POST` | `/api/v1/x/macro.shell/commands` |
| `POST` | `/api/v1/x/macro.shell/commands/{hash}/` |
| `DELETE` | `/api/v1/x/macro.shell/commands/{hash}` |
| `POST` | `/api/v1/x/macro.shell/commands/{hash}/approve` |
| `POST` | `/api/v1/x/macro.shell/commands/{hash}/revoke` |
| `GET` | `/api/v1/x/mfa.email/me` |
| `DELETE` | `/api/v1/x/mfa.email/me` |
| `GET` | `/api/v1/x/mfa.network/me` |
| `PUT` | `/api/v1/x/mfa.network/me` |
| `GET` | `/api/v1/x/mfa.network/users/{id}` |
| `PUT` | `/api/v1/x/mfa.network/users/{id}` |
| `GET` | `/api/v1/x/mfa.totp/me` |
| `DELETE` | `/api/v1/x/mfa.totp/me` |
| `GET` | `/api/v1/x/mfa.webauthn/credentials` |
| `DELETE` | `/api/v1/x/mfa.webauthn/credentials/{id}` |
| `GET` | `/api/v1/x/signature.smime/settings` |
| `PUT` | `/api/v1/x/signature.smime/settings` |
| `GET` | `/api/v1/x/signature.smime/certs` |
| `POST` | `/api/v1/x/signature.smime/certs` |
| `GET` | `/api/v1/x/signature.smime/certs/{id}` |
| `PATCH` | `/api/v1/x/signature.smime/certs/{id}` |
| `DELETE` | `/api/v1/x/signature.smime/certs/{id}` |
| `GET` | `/api/v1/x/signature.smime/me` |
| `POST` | `/api/v1/x/office.collabora/test` |
| `POST` | `/api/v1/x/office.m365/test` |
| `GET` | `/api/v1/x/office.onlyoffice/file/{id}` |
| `POST` | `/api/v1/x/office.onlyoffice/callback/{id}` |
| `POST` | `/api/v1/x/office.onlyoffice/test` |
| `GET` | `/api/v1/x/search.external/status` |
| `POST` | `/api/v1/x/search.external/test` |
| `POST` | `/api/v1/x/search.external/reindex` |
| `GET` | `/api/v1/x/theme.cms/config` |
| `GET` | `/api/v1/x/workflow.process/meta` |
| `GET` | `/api/v1/x/workflow.process/elements/{id}/instances` |
| `POST` | `/api/v1/x/workflow.process/elements/{id}/instances` |
| `GET` | `/api/v1/x/workflow.process/instances/{id}` |
| `GET` | `/api/v1/x/workflow.process/instances/{id}/files/{field}` |
| `POST` | `/api/v1/x/workflow.process/instances/{id}/actions` |
| `POST` | `/api/v1/x/workflow.process/instances/{id}/sign` |
| `POST` | `/api/v1/x/workflow.process/instances/{id}/cancel` |
| `GET` | `/api/v1/x/workflow.process/tasks` |
| `GET` | `/api/v1/x/workflow.process/elements/{id}/triggers` |
| `POST` | `/api/v1/x/workflow.process/elements/{id}/triggers/{trigger}/secret` |
| `POST` | `/api/v1/x/workflow.process/hooks/{id}/{trigger}` |
| `GET` | `/api/v1/x/workflow.process/me/deputy` |
| `PUT` | `/api/v1/x/workflow.process/me/deputy` |
| `DELETE` | `/api/v1/x/workflow.process/me/deputy` |
| `GET` | `/api/v1/x/workflow.process/links/info` |
| `POST` | `/api/v1/x/workflow.process/links/act` |
| `GET` | `/api/v1/x/workflow.process/elements/{id}/stats` |
| `GET` | `/api/v1/x/content.forms/meta` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/start` |
| `POST` | `/api/v1/x/content.forms/elements/{id}/submit` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/submissions` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/submissions.csv` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/submissions.xlsx` |
| `GET` | `/api/v1/x/content.forms/submissions/{sid}` |
| `DELETE` | `/api/v1/x/content.forms/submissions/{sid}` |
| `GET` | `/api/v1/x/content.forms/submissions/{sid}/files/{n}` |

### Other

| Method | Path |
|---|---|
| `GET` | `/tls-allowed` |
| `GET` | `/api/v1/drafts` |
| `POST` | `/api/v1/drafts` |
| `GET` | `/api/v1/drafts/{id}` |
| `PUT` | `/api/v1/drafts/{id}` |
| `DELETE` | `/api/v1/drafts/{id}` |
| `POST` | `/api/v1/drafts/{id}/publish` |
| `DELETE` | `/api/v1/drafts/{id}/schedule` |
| `GET` | `/api/v1/templates` |
| `GET` | `/api/v1/templates/{id}` |
| `POST` | `/api/v1/templates` |
| `PUT` | `/api/v1/templates/{id}` |
| `DELETE` | `/api/v1/templates/{id}` |
| `GET` | `/api/v1/permissions` |
| `GET` | `/api/v1/permissions/overview` |
| `GET` | `/api/v1/feeds/categories/{id}` |
| `GET` | `/api/v1/resolve` |
| `GET` | `/api/v1/not-found` |
| `GET` | `/api/v1/prerender` |
| `GET` | `/api/v1/wopi/files/{id}` |
| `POST` | `/api/v1/wopi/files/{id}` |
| `GET` | `/api/v1/wopi/files/{id}/contents` |
| `POST` | `/api/v1/wopi/files/{id}/contents` |
| `GET` | `/api/v1/instance/sites` |
| `POST` | `/api/v1/instance/sites` |
| `PATCH` | `/api/v1/instance/sites/{name}` |
| `DELETE` | `/api/v1/instance/sites/{name}` |
| `GET` | `/api/v1/instance/admins` |
| `POST` | `/api/v1/instance/admins` |
| `DELETE` | `/api/v1/instance/admins/{id}` |
