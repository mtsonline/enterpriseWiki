# REST API

The web interface only uses this API. Everything that works in the interface also works with scripts or through `ewiki api` (CLI).

- Base: `/api/v1`, JSON (UTF-8). Plugin routes are under `/api/v1/x/<plugin-id>/…`.
- Errors: HTTP status plus `{"error": {"code": "not_found", "message": "…"}}`. Codes: `bad_request`, `unauthorized`, `invalid_credentials`, `forbidden`, `csrf`, `not_found`, `conflict`, `rate_limited`, `internal`. The message is in the language of the header `Accept-Language` (first entry; German with `de`), otherwise English; the code is the same in every language. The texts are in `internal/apperr/locales/<lang>.json` (key: the English format); `go test ./internal/apperr` checks that every message of the code has a text.
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
- **Resetting:** `POST /api/v1/categories/{id}/rollback {"at": "2026-10-01T12:00:00Z", "recursive": true, "structure": true}`; `structure` (only with `recursive`) undoes moves and orders since then (newest first, with the checks of a normal move) and moves subcategories created since then to the trash; answer `{categories, revisions, trashed, moved, categories_trashed}`, a former place that no longer exists gives `409` and changes nothing. Most changes: limit `core.rollback.structure_changes`.
- **Files:** `POST /api/v1/files` (multipart, field `file`) delivers `{hash, name, mime, size}`. An element `core.attachment` refers to the file with `{"file": {"hash": "…"}, "description": "…"}`. Downloading with permission check through `GET /api/v1/x/core.attachment/elements/{id}/file[?hash=…][&inline=1]`. Images are also available under `GET /api/v1/files/{hash}` (for images in pages).
- **List media:** `GET /api/v1/media?category=ID&recursive=1&q=&type=&sort=&used_by=&limit=&offset=` returns only files you may see. `type`: `image`, `video`, `audio`, `pdf`, `document`; `sort`: `updated` (default, newest first), `name`, `size` (largest first), `type`; `used_by=ID`: only the files this page shows (only for a page you may see). The block *Attachments* uses the same list.
- **Search:** `GET /api/v1/search?q=word&tags=a,b&lang=en` delivers `snippet` as HTML-escaped text with `<mark>`. With `category=ID` only this category and its sub-categories are searched (`recursive=0`: only the category).
- **Options of placed blocks:** every placement may carry `options` (look and showing at its position). `GET /api/v1/block-options` (public) returns the schema: groups, options with type, values or range, and whether a value per view (`desktop`, `tablet`, `mobile`) is possible. The server checks the options in `PUT /api/v1/placements/{level}`: unknown options, form and range of each value, contrast of the text color on its background (also on gradients and pictures under their overlay), room for columns in each view, unique anchors; a hidden block has no options. CLI: `ewiki placement options`.
- **Templates of data:** `GET /api/v1/x/core.template/designs` (public) returns the templates that can be used with their designs; the wiki builds its pages from them. `GET …/limits` returns the limits (the same file the server checks with). With `core.template.design`: `GET …/templates` (`?deleted=1`: the trash), `GET …/templates/{key}` (with `used_by`), `POST …/check` (problems of a design, nothing stored), `POST …/templates` (`names` and `design` or `copy_of`), `PUT …/templates/{key}` (`version` must be the one loaded, else 409), `DELETE …/templates/{key}` (to the trash, not while in use; `?purge=1` from the trash for good), `POST …/templates/{key}/restore`, `GET …/templates/{key}/versions`, `GET …/templates/{key}/export`, `POST …/templates/import`, `PUT …/templates/{key}/rights` (`edit_subjects`, `manage_subjects`: people, groups, roles or all signed-in people who may change or manage this template; empty lists: all designers; an empty `edit_subjects` with people in `manage_subjects`: only those; managing covers the rights, the trash and own CSS; administrators always may; nobody can remove their own right to manage; the answer of `GET …/templates/{key}` has `may` and `subject_names`). Fonts: `GET …/fonts`, `POST …/fonts` (`name`, `license` one of `OFL-1.1`, `Apache-2.0`, `MIT`, `other` with `license_note`, `confirm: true`, `files`: 1 to 8 files, each `data` (WOFF2, WOFF, TTF or OTF as base64; one font, no collection), `weight` (`400` or a range `100 900`) and `style` (`normal`, `italic`)), `DELETE …/fonts/{id}` (409 while a template uses it, also in the trash), `GET …/fonts/{id}/css` (its `@font-face` rules) and `GET …/fonts/{id}/files/{n}` (public while a template outside the trash has it, like the designs; else only for designers). A design uses an uploaded font as `own-ID` in `tokens.font` or `tokens.heading_font`. Ids are `core.template/<key>`; they are used like other template ids (`ui.template`, the template of a category, the placement level `template`). CLI: `ewiki site-template …`.
- **Category page:** `GET /api/v1/categories/{id}/overview?lang=&page=&view=` returns one page of the overview of a category in its display: `display` (`view`: `list`, `cards`, `tiles`, `tree`, `handbook`, `table`, `accordion`, `tabs`, `az`, `timeline`, `all`; `sort`: `manual`, `title`, `updated`, `created`; `page_size`, `subcategories`, `excerpt`; `columns`: up to 12 further keys of the table, e.g. properties; `date`: `updated` or `created` for the timeline), `items` (with `excerpt` from the text index), `total`, `page`, `pages` and `category_titles`. `az` always goes by title, `timeline` by its date (newest first); `accordion`, `tabs` and `all` show whole pages, at most `core.categories.full` per page (expert setting). Only pages you may view; hidden pages only for editors of the category; with `subcategories` also the pages of the subcategories that are neither hidden nor archived. The display is set with `PUT /api/v1/categories/{id}/settings` (`display`; without it: as above) and is inherited by the subcategories. CLI: `ewiki category overview ID`.
- **Templates:** `GET /api/v1/templates?kind=page|block&type=&category=` (default `page`); `POST /api/v1/templates` with `kind: "block"` saves a block template (always Markdown of a page, `type` becomes `core.html`). The kind of a template cannot be changed; the block *New page from a template* takes page templates only.
- **Scope of a placement for a category:** a placement of the level `category` may carry `only_category_page: true` (only on the page of the category, not on the pages in it) and `no_subcategories: true` (not in the subcategories; there the levels above apply). Other levels refuse both (`400`). `GET /api/v1/placements?category=&element=` leaves out what does not apply to that page.
- **Layout per element type:** the level `type` (target: the id of the element type, for example `content.blog`; needs `core.settings.manage`) arranges the positions of the pages of this type (`element-…`). It applies after the site and before the categories; `GET /api/v1/placements?element=ID` returns it only for a page you may see. CLI: `ewiki placement set type --query target=content.blog`.
- **Featured pages:** the page of one category is a placement level of its own, `category_page` (target: the category id, not inherited; needs *Manage*), with the positions `category-header`, `category-toolbar`, `category-content`, `category-aside` and `category-footer`. A featured page is the block `core.category.element/<element id>` on this level; it must be a page you may view, at most `core.categories.featured` (expert setting). `GET /api/v1/elements/batch?ids=1,2,3&lang=` (at most the larger of `core.categories.featured` and `core.categories.full`) loads several pages at once (as `GET /elements/{id}`); pages you may not view are left out. CLI: `ewiki element batch --query ids=1,2`.
- **State of the wiki:** `GET /api/v1/admin/status` (administrators only) returns `version`, `updates_available` and `updates_checked_at` from the last check (it does not ask GitHub), `mail_failed` and `mail_queued`, `queue_pending` and `queue_oldest` of the change queue, `login_failed_24h`, `audit_forward_errors`, `plugins_inactive` (switched on but not active), `restart_pending`, `build_failed`, `license_level` and `database_level` (`ok`, `info`, `warning`, `error`). CLI: `ewiki system status`.
- **New submissions:** `GET /api/v1/x/content.forms/submissions/recent?limit=8` lists the forms with submissions of the last 90 days whose submissions you see (`element_id`, `titles`, `last`, `unseen`), newest first. CLI: `ewiki form recent`.
- **Recently changed:** `GET /api/v1/recent?limit=10&category=ID` lists recently changed pages, `?by=me` the pages you edited last (newest own revision first); always only what you may see.
- **Data for reports:** `GET /api/v1/data` lists the kinds of data that blocks and plugins keep (schema, `complete`). `GET /api/v1/data/{kind}?category=ID&state=open,decided&from=2026-01-01&to=…&person=ID&ref=ID&element=ID&lang=en&q=word&sort=-day&limit=50&offset=0` gives the entries with `total`; only of elements you may see, never from the trash, hidden ones with `hidden=true`; `direct=true` without sub-categories; `sort`: `day`, `-day`, `at`, `-at`, `num`, `-num`, `label`, `state`, `page`; at most 500 per answer. `people` gives the names of the people in fields of the type `person` (id → name), only of people you may find (yourself, all with `core.users.list_all`, the members of your groups with `core.users.list_own`; never for guests, never disabled users). `POST /api/v1/admin/data/rebuild` with `{"kind": "…"}` (or `{}` for all) builds the data again from the latest versions (`core.settings.manage`).
- **Maintenance:** `GET /api/v1/admin/maintenance` and `PUT /api/v1/admin/maintenance` with `{"mode": "read_only", "message": {"en": "…"}, "until": "2026-11-23T18:00:00+01:00", "run_jobs": false}` (`mode` empty: off; administrators only). During maintenance the server answers changes of others with `503` and the code `maintenance` (and `Retry-After`); `GET /api/v1/site` carries `maintenance` (mode, message, until), `null` when off.
- **Consent:** `GET /api/v1/me/consent` (own acceptance of the privacy policy), `POST /api/v1/me/consent` with `{"kind": "privacy", "version": 1}` (only the version in force, else `409`); `GET/PUT /api/v1/admin/consent` (settings, see configuration `consent`); `GET /api/v1/admin/consents?version=&limit=&offset=` (proof; `format=csv`). `GET /api/v1/site` carries `consent` (`cookies`, `privacy`), `null` when off. While the privacy policy is not accepted, sessions get `403` with code `consent_required`.
- **Listing elements:** `GET /api/v1/elements?q=&type=&category=&recursive=1&sort=title|updated|created|type&limit=&offset=` delivers `{items, total}` with `category_path` (only visible elements, all for administrators).
- **Short URLs:** `short_url` with `POST /elements`, `PATCH /elements/{id}`, `POST/PUT /categories` (empty = remove). `GET /api/v1/resolve?path=calendar` delivers `{type, id}`.
- **Watching:** `PUT /api/v1/me/watches {"target_type": "category"|"element", "target_id": 12, "mode": "link"|"content", "recursive": true}`; status for a target: `GET /api/v1/me/watches?target_type=element&target_id=5`.
- **Feeds:** `GET /api/v1/feeds/categories/{id}?format=atom|rss&lang=en[&content=1]` (when switched on in the category), blog/FAQ under `/api/v1/x/content.blog|content.faq/elements/{id}/feed`. Feed readers without sign-in use the personal key from `GET /api/v1/me/feed-token` as `&token=…`. It only counts for feed and calendar routes and can be renewed with `POST` or deleted with `DELETE`.
- **Sources:** fields such as `more_sources` (blog, FAQ, calendar), `sources` (carousel) and the macro parameter `source` take sources as text: `category:12+sub; category:7; tag:hr; type:core.html; elements:4,9; search:holiday; props:status = open && due <= today+7d`, depending on the place additionally `feed:https://…` (macro `news`), `calendar:ID` and `ical:https://…` (calendar, macro `events`). **Forms** (`content.forms`): `GET …/elements/{id}/start` delivers the token (signed, at least 2 s old, at most 24 h), the state (`""`, `not_open`, `closed`, `sign_in`, `full`, `already`) and the pre-filled values; `POST …/elements/{id}/submit` as `multipart/form-data` with the part `data` = `{"values": {ID: value}, "token": "…", "website": "", "nonce": "…"}` (`nonce` only with `pow_bits` from `start`: the SHA-256 of `token:nonce` starts with this many zero bits; repeating groups as a list of objects, errors there as `GROUP.ROW.FIELD`; signature as text or file `file.ID` (PNG)) and files `file.FIELD` (comments `comment.FIELD.N`) or as JSON without files; answer `201 {id, message}`, on errors `422 {"error": {"code": "invalid", "fields": [{field, code, param}]}}`. `search:` (full-text search, at most 100 hits) and `props:` (property filter as in the report, filters separated by `&&`, at most 200 hits; only with the property plugin active) are resolved with the permissions of the readers. A `-` before a source excludes it (`-tag:internal`), a `+` requires it additionally (`+tag:top`); in JSON as `"mode": "exclude" | "require"`. `POST /api/v1/sources/preview` with `{sources, category_id, lang}` delivers `{total, items}` (the first 5 titles) with the permissions of the caller (signed in only). **Pages of macro lists:** parameters `limit`, `paginate` (`yes`/`no`), `per_page`; `GET /api/v1/elements/{id}/macro/{index}?page=N&lang=` delivers only this macro (`{html}`), `GET /api/v1/elements/{id}?mp=INDEX-PAGE` the whole page with this page of the list. Upper limit: setting `lists.max_items` (`PUT /api/v1/admin/settings` with `{"lists": {"max_items": 2000}}`). Invalid input gives `400`. `GET /api/v1/x/content.events/calendars/{id}/events` also delivers events of further calendars (`source`, read-only) and external events (`external: true`, negative `id`).
- **Website template per category:** `PUT /api/v1/categories/{id}` with `{"template": "theme.cms"}` (only active template plugins, `""` = inherit; permission *Manage*). Categories deliver `template` (own) and `effective_template` (the one that applies, `""` = site template); `GET /api/v1/site` lists the active templates under `templates`.
- **Spaces:** `GET /api/v1/spaces` (directory, only visible ones), `GET /api/v1/spaces/{key}` (`404` if not visible), `PUT /api/v1/categories/{id}/space` with `{"key": "HR", "own_tree": true, "shortcuts": [{"label": {"en": "Forms"}, "url": "/wiki/hr/forms"}], "allowed_types": ["core.html"], "language": "en"}` (top level only, permission *Manage*; duplicate key `409`). `GET /api/v1/categories/{id}` also delivers `space` (the space of the category or `null`). New space from a template: `POST /api/v1/spaces` with `{"template_id": 5, "titles": {"en": "Project X"}, "key": "PX", "with_pages": false}` (category administrators; template = space with `is_template`). Archiving: `POST /api/v1/categories/{id}/archive` with `{"archived": true|false}` (*Manage*); categories deliver `archived` (also inherited) and `archived_at`. Export: `GET /api/v1/categories/{id}/export` (JSON Lines, *Manage*); import: `POST /api/v1/categories/import?parent=ID` with the file as body (`0` = new space), answer `{category_id, categories, elements, templates, skipped}`. Page templates of a space: `space_id` when saving, `GET /api/v1/templates?category=ID`. Personal space: `POST /api/v1/spaces/personal` (permission `core.spaces.personal`; `201` when it is created, `200` with the existing one). Spaces deliver `member` (own permissions as person, group or role) and `personal`. Requesting access: `POST /api/v1/access-requests` with `{"type": "category"|"element", "id": 12, "message": "…"}` (always `202` with `{"sent": true}`, at most 10 per day, otherwise `429`); open requests `GET /api/v1/categories/{id}/access-requests` (*Manage*), answering `POST /api/v1/access-requests/{id}` with `{"grant": true, "preset": "reader"|"editor"}` or `{"grant": false}`. Classification: `PUT /api/v1/categories/{id}` with `{"classification": 0..3}` (`-1` = as above); categories deliver `classification` and `effective_classification`. Views: `POST /api/v1/elements/{id}/view` (only counting, per page and day); analytics: `GET /api/v1/categories/{id}/insights?days=30&lang=en` (*Manage*). Further space settings: `allowed_macros` (empty = all), `accent` (`#rrggbb`, contrast ≥ 4.5:1 to white, otherwise `400`), `logo` (`/api/v1/files/…` or `/api/v1/media/…`).
- **Property extensions:** `POST /api/v1/x/core.properties/query.csv|query.xlsx` with a query or the body of `/report` (Excel without formulas, never strictly confidential rows); `POST …/trend` with `{"report": {…}, "key": "status", "weeks": 12}` → `{weeks, series:[{value, values}]}`; saved views `GET|POST …/databases/{id}/views`, `DELETE …/views/{id}`; subscriptions `GET|POST …/subscriptions`, `DELETE …/subscriptions/{id}` (`frequency`: `daily`|`weekly`). With `"export_mode": "original"` (query) or the parameter `export_mode` (report, database) values with `=` become formulas and CSV cells are not protected. Categories: `"watermark": "auto"|"off"|""` and `"downloads": "auto"|"allow"|""` (`""` = as above).
- **Events:** `POST /api/v1/x/content.events/calendars/{id}/events {"title", "starts_at", "ends_at", "all_day", "tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;BYDAY=MO", "registration": true, "capacity": 20, "reminders": [1440]}`; events of a period with `GET …/calendars/{id}/events?from=…&to=…` (series are expanded, `occurrences[].date` is the key for registrations). Registering: `POST …/events/{id}/registrations {"occurrence": "2026-11-23"}` (`interested: true` only for reminders).
- **E-mail queue (admin):** `GET /api/v1/admin/mail` (status with counters per recipient), `GET /api/v1/admin/mail/outbox?status=failed&q=…`, `GET /api/v1/admin/mail/outbox/{id}` (with recipients and the last server answer).

## Action register

Every route is an **action** with an entry in the action register, in the format `ewiki.plugin.v1.Action` (`sdk/proto/ewiki/plugin/v1/action.proto`). From it the wiki checks that every action is reachable by API, CLI and interface, and builds the CLI commands for actions without a hand-written command.

- **Where:** the core in `internal/actions/core/<area>.yaml`, core plugins in `plugins/<plugin>/actions.yaml` (paths relative to `/api/v1/x/<plugin-id>/`, added with `actions.Add` in `actions.go`).
- **Required per entry:** `id` after the namespace rule, `category` (`CATEGORY_ACTION`, or `CATEGORY_PROTOCOL` for calls of other systems), `method`, `path`, `summary` in German **and** English, exactly one of `permission`, `signed_in`, `public`, and for actions `cli` and `ui` or an exception approved by the project owner (`exceptions`).
- **Success status:** every action (`CATEGORY_ACTION`) names `success_status`, and the examples check it exactly: `201` when something new with its own address is created, `202` when work starts in the background (e.g. building plugins, a restart, an access request that notifies), `204` without content, otherwise `200`. `DELETE` answers `204`; another status needs a documented `response`.
- **Comment at the route:** `// action: <id>` directly above `s.handle(…)` or `host.Handle(…)`.
- **Checks:**
  - `TestActionRegister` (stage 1): register and routes agree, the comments match. Every route has an entry; a new route without one fails the CI. The tables under "Endpoints" must list exactly the routes of the register. A route only on the mux and not recorded (`h.RecordRoute`) is found as well. Routes a shared helper registers name their action where the helper is called: `// action: <id> for <METHOD> <path>`.
  - `TestNoServerErrors` (stage 2, "no crash"): every route of the register gets wrong input (invalid or too large ids, broken JSON, wrong types, nonsense parameters), as administrator, as signed-in person without permissions and as guest. No answer may be a 5xx. Exceptions are listed with a reason in `notProbed`.
  - `TestActionExamples` (stage 2): every example (`examples`) runs on the API and on the command line with the same result (status, `expect_json` as a subset of the answer). `given` first creates, freshly for each way and as administrator, what the example builds on; `${given.<action>#<example>.<field>}` fills in values from it, `${unique}` makes names unique, `${unique_lang}` gives an unused language code for local use (`qaa`–`qtz`). `as: self` runs the example as a fresh account of its own (actions on your own account); its given examples with `as: self` run as the same account, those with `as: other` as a second account (the one of `${other.…}`). Lists and objects from `${given…}` are filled in as JSON. `${self.id|name|password}` and `${other.id|name|password}` (another signed-in person) fill in such accounts, `${ssh_key}` a fresh public SSH key, `${totp:SECRET}` the current code of an authenticator app (e.g. with the secret of a given example). `${webauthn.create:REF}` and `${webauthn.get:REF.PATH}` answer the options of a given example with the software passkey of the own account (setting up, signing), `${pkcs12:ADDRESS}` is a certificate with its key from a test authority (password `s`). `${mail_link:ADDRESS:PARAM}` is PARAM of the link in the newest e-mail to ADDRESS (the examples send through a mail server of the tests), `${smtp_port}` its port. `requires` names real test systems an example needs (`actions.TestSystems`: `opensearch`, `elasticsearch`, `meilisearch`, `ldap`, `keycloak`, `collabora`, `onlyoffice`, `microsoft365`); `${system.<name>.<field>}` fills in their address or key. Such examples run only where the systems are started (environment variables `EWIKI_TEST_<NAME>_<FIELD>` from `tests/search/start.sh`, `tests/auth/start.sh` and `tests/office/start.sh`; `microsoft365` is the public discovery of Microsoft and runs in the CI only) and are skipped elsewhere; with `EWIKI_TEST_EXAMPLE_SYSTEMS=opensearch,…` only they run, and the systems must be running (CI jobs `search`, `auth` and `office`). `${local_plugin:ID}` creates a fresh local plugin with this id and fills in its folder, `${plugin_root}` is the folder of these plugins (for trusted sources). For critical actions (`safety.recent_sign_in`) the command line way runs as the command line on the server; before, `--as` (no sign-in) must be refused. `body_text` sends a body that is not JSON (e.g. an export); `${given.REF.$raw}` is the whole answer. Actions with an approved exception for the command line run on the API only. `<field>` can be a path such as `deliveries.0.id`; for an answer without content (204) the fields of the request are used. Uploads: `form` (text fields) and `files` (`field`, `name`, `content_type`, `text` or `base64`) instead of `body_json`. `as: member` gets exactly the permission of the register (global through a role, for categories through a rule on the category of the parameter); in addition a person without that permission must be refused on both ways. Actions without an example are listed in `internal/actions/examples-missing.txt`; the list may only get shorter.
  - Interface coverage (`tools/uicoverage`, CI job of the browser tests): the browser test records every API request the pages send (`EWIKI_E2E_UI_REQUESTS=FILE`). The tool maps them to actions with the same patterns as the server. Every action with a way in the interface (`ui`) must get a successful answer (status below 400) in the browser test at least once; a refusal does not count. A request that no action describes also fails the check. Requests the test sends itself from a page go through `__e2eFetch` and do not count; a test of its own forbids an unmarked `fetch` in the browser test. Actions not covered yet are listed in `internal/actions/ui-missing.txt`; the list may only get shorter (`go run ./tools/uicoverage -requests FILE -write` shortens it).
  - `go run ./tools/protocheck` in the folder `sdk`: compiles the contract and reads all register files and example manifests with the real messages (unknown fields, wrong values and invalid JSON in schemas are errors).
- **Texts with a comma** go in block form (`text:` with `en:` and `de:` below); in the short form `{en: a, b}` the comma would split the text. The check detects this.

```yaml
actions:
  - id: core.auth.password_reset
    category: CATEGORY_ACTION
    method: POST
    path: /api/v1/auth/password-reset
    summary:
      text:
        en: Ask for a link to set a new password (local accounts)
        de: Link für ein neues Passwort anfordern (lokale Konten)
    public: true
    body:
      json: '{"type": "object", "required": ["email"], "properties": {"email": {"type": "string"}}}'
    success_status: 202
    safety: {changes: true}
    cli: {command: [auth, password-reset, request]}
    ui: {entry: /password-reset}
```

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
| `POST` | `/api/v1/auth/register` |
| `POST` | `/api/v1/auth/register/confirm` |
| `POST` | `/api/v1/auth/password-reset` |
| `POST` | `/api/v1/auth/password-reset/confirm` |
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

### SCIM 2.0 (directories)

Called by directories such as Entra ID or Okta with the token of a SCIM client (see the admin documentation).

| Method | Path |
|---|---|
| `GET` | `/scim/v2/ServiceProviderConfig` |
| `GET` | `/scim/v2/ResourceTypes` |
| `GET` | `/scim/v2/Schemas` |
| `GET` | `/scim/v2/Users` |
| `POST` | `/scim/v2/Users` |
| `GET` | `/scim/v2/Users/{id}` |
| `PUT` | `/scim/v2/Users/{id}` |
| `PATCH` | `/scim/v2/Users/{id}` |
| `DELETE` | `/scim/v2/Users/{id}` |
| `GET` | `/scim/v2/Groups` |
| `POST` | `/scim/v2/Groups` |
| `GET` | `/scim/v2/Groups/{id}` |
| `PUT` | `/scim/v2/Groups/{id}` |
| `PATCH` | `/scim/v2/Groups/{id}` |
| `DELETE` | `/scim/v2/Groups/{id}` |

### Categories & permissions

| Method | Path |
|---|---|
| `GET` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/tree` |
| `POST` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/{id}` |
| `GET` | `/api/v1/categories/{id}/overview` |
| `PUT` | `/api/v1/categories/{id}` |
| `PUT` | `/api/v1/categories/{id}/translations/{lang}` |
| `DELETE` | `/api/v1/categories/{id}` |
| `POST` | `/api/v1/categories/{id}/move` |
| `POST` | `/api/v1/categories/{id}/duplicate` |
| `POST` | `/api/v1/categories/{id}/reorder` |
| `GET` | `/api/v1/categories/{id}/acl` |
| `POST` | `/api/v1/categories/{id}/acl` |
| `GET` | `/api/v1/categories/{id}/access-requests` |
| `POST` | `/api/v1/categories/{id}/archive` |
| `DELETE` | `/api/v1/acl/{id}` |
| `GET` | `/api/v1/categories/{id}/history` |
| `POST` | `/api/v1/categories/{id}/rollback` |
| `GET` | `/api/v1/categories/{id}/export` |
| `GET` | `/api/v1/categories/{id}/insights` |
| `PUT` | `/api/v1/categories/{id}/space` |
| `POST` | `/api/v1/categories/import` |
| `PUT` | `/api/v1/acl/{id}` |
| `GET` | `/api/v1/categories/{id}/settings` |
| `PUT` | `/api/v1/categories/{id}/settings` |
| `GET` | `/api/v1/elements/{id}/rights` |
| `PUT` | `/api/v1/elements/{id}/rights` |
| `GET` | `/api/v1/elements/{id}/labels` |
| `PUT` | `/api/v1/elements/{id}/labels` |
| `GET` | `/api/v1/elements/{id}/acl` |
| `GET` | `/api/v1/elements/{id}/access-requests` |
| `PUT` | `/api/v1/elements/{id}/acl` |
| `POST` | `/api/v1/elements/{id}/acl` |
| `PUT` | `/api/v1/element-acl/{id}` |
| `DELETE` | `/api/v1/element-acl/{id}` |
| `GET` | `/api/v1/placements` |
| `GET` | `/api/v1/placements/{level}` |
| `PUT` | `/api/v1/placements/{level}` |
| `POST` | `/api/v1/access-requests` |
| `POST` | `/api/v1/access-requests/{id}` |
| `GET` | `/api/v1/spaces` |
| `POST` | `/api/v1/spaces` |
| `POST` | `/api/v1/spaces/personal` |
| `GET` | `/api/v1/spaces/{key}` |

### Elements & versions

| Method | Path |
|---|---|
| `GET` | `/api/v1/element-types` |
| `POST` | `/api/v1/elements` |
| `POST` | `/api/v1/elements/preview` |
| `POST` | `/api/v1/sources/preview` |
| `GET` | `/api/v1/elements/batch` |
| `GET` | `/api/v1/block-options` |
| `GET` | `/api/v1/x/core.template/designs` |
| `GET` | `/api/v1/x/core.template/limits` |
| `GET` | `/api/v1/x/core.template/templates` |
| `POST` | `/api/v1/x/core.template/templates` |
| `POST` | `/api/v1/x/core.template/check` |
| `POST` | `/api/v1/x/core.template/templates/import` |
| `GET` | `/api/v1/x/core.template/templates/{key}` |
| `PUT` | `/api/v1/x/core.template/templates/{key}` |
| `DELETE` | `/api/v1/x/core.template/templates/{key}` |
| `POST` | `/api/v1/x/core.template/templates/{key}/restore` |
| `PUT` | `/api/v1/x/core.template/templates/{key}/rights` |
| `GET` | `/api/v1/x/core.template/templates/{key}/versions` |
| `GET` | `/api/v1/x/core.template/fonts` |
| `POST` | `/api/v1/x/core.template/fonts` |
| `DELETE` | `/api/v1/x/core.template/fonts/{id}` |
| `GET` | `/api/v1/x/core.template/fonts/{id}/css` |
| `GET` | `/api/v1/x/core.template/fonts/{id}/files/{n}` |
| `GET` | `/api/v1/x/core.template/templates/{key}/export` |
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
| `POST` | `/api/v1/elements/{id}/view` |
| `GET` | `/api/v1/elements` |
| `GET` | `/api/v1/collab/elements/{id}/{lang}` |

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
| `GET` | `/api/v1/recent` |
| `GET` | `/api/v1/search` |
| `GET` | `/api/v1/tags` |
| `GET` | `/api/v1/data` |
| `GET` | `/api/v1/data/{kind}` |
| `POST` | `/api/v1/admin/data/rebuild` |
| `GET` | `/api/v1/admin/maintenance` |
| `PUT` | `/api/v1/admin/maintenance` |
| `GET` | `/api/v1/me/consent` |
| `POST` | `/api/v1/me/consent` |
| `GET` | `/api/v1/admin/consent` |
| `PUT` | `/api/v1/admin/consent` |
| `GET` | `/api/v1/admin/consents` |

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
| `GET` | `/api/v1/admin/status` |

### Administration

| Method | Path |
|---|---|
| `GET` | `/api/v1/admin/permissions` |
| `PUT` | `/api/v1/admin/permissions/authenticated` |
| `GET` | `/api/v1/admin/settings` |
| `PUT` | `/api/v1/admin/settings` |
| `POST` | `/api/v1/admin/retention/run` |
| `GET` | `/api/v1/admin/plugins` |
| `PUT` | `/api/v1/admin/plugins/options` |
| `POST` | `/api/v1/admin/plugins/{id}/enable` |
| `POST` | `/api/v1/admin/plugins/{id}/disable` |
| `POST` | `/api/v1/admin/plugins/install` |
| `DELETE` | `/api/v1/admin/plugins/{id}` |
| `POST` | `/api/v1/admin/plugins/build` |
| `POST` | `/api/v1/admin/plugins/origin` |
| `GET` | `/api/v1/admin/plugins/sources` |
| `POST` | `/api/v1/admin/plugins/sources` |
| `DELETE` | `/api/v1/admin/plugins/sources` |
| `PUT` | `/api/v1/admin/plugins/sources/external` |
| `GET` | `/api/v1/admin/plugins/credentials` |
| `PUT` | `/api/v1/admin/plugins/credentials` |
| `DELETE` | `/api/v1/admin/plugins/credentials` |
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
| `GET` | `/api/v1/admin/auth/login` |
| `PUT` | `/api/v1/admin/auth/login` |
| `GET` | `/api/v1/admin/scim/clients` |
| `POST` | `/api/v1/admin/scim/clients` |
| `POST` | `/api/v1/admin/scim/clients/{id}/token` |
| `DELETE` | `/api/v1/admin/scim/clients/{id}` |
| `GET` | `/api/v1/admin/demo` |
| `POST` | `/api/v1/admin/demo` |
| `DELETE` | `/api/v1/admin/demo` |
| `POST` | `/api/v1/admin/reset` |

**Origin of core plugins** (instance administrators only, see [Managing plugins](../admin/plugins.md#origin-of-core-plugins)):

- `POST /api/v1/admin/plugins/origin` with `{"source": "…"}` checks a source before installing and answers `{"level": "official"|"organisation"|"external", "source": "…", "trusted": "…", "allowed": true}`.
- `POST /api/v1/admin/plugins/install` and `POST /api/v1/admin/updates/{id}` accept `"accept_external": true`. Without that confirmation for an external source the wiki answers 400 with the code `external_not_accepted`; when external sources are switched off, 403. The answer of an installation contains `origin` (level, source, commit, who installed). `GET /api/v1/admin/plugins` returns `origins` per plugin id.
- `GET /api/v1/admin/plugins/sources` returns `{"sources": [{"prefix", "note"}], "allow_external": true}`. `POST …/sources` with `{"prefix", "note"}` adds a source, `DELETE …/sources?prefix=…` removes it, `PUT …/sources/external` with `{"allow": false}` switches external sources off. Adding and allowing need a browser session signed in within the last 15 minutes (otherwise 403 with `reauth_required`) or the command line; API keys are refused.
- `GET /api/v1/admin/plugins/credentials` returns the access data of plugin sources without tokens: `{"credentials": [{"prefix", "username", "note", "token_set", "from_env"}]}`. `PUT …/credentials` with `{"prefix", "username", "token", "note"}` sets them for all sources below an https address (replaces an entry with the same prefix; user name `x-access-token` when not given), `DELETE …/credentials?prefix=…` removes them. The token is never shown again, not in the audit log either. Setting needs a fresh sign-in as above. References to environment variables (`${…}`) are refused (400); they exist in `config.yaml` only.

### Plugins

| Method | Path |
|---|---|
| `POST` | `/api/v1/x/auth.ldap/test` |
| `GET` | `/api/v1/x/auth.ldap/sync` |
| `POST` | `/api/v1/x/auth.ldap/sync` |
| `POST` | `/api/v1/x/auth.ldap/sync/adopt` |
| `GET` | `/api/v1/x/auth.oidc/check` |
| `GET` | `/api/v1/x/auth.shib/metadata` |
| `GET` | `/api/v1/x/content.blog/elements/{id}/posts` |
| `GET` | `/api/v1/x/content.blog/elements/{id}/feed` |
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
| `GET` | `/api/v1/x/content.team/groups` |
| `GET` | `/api/v1/x/content.team/fields` |
| `GET` | `/api/v1/x/content.team/elements/{id}/members` |
| `GET` | `/api/v1/x/core.attachment/elements/{id}/file` |
| `GET` | `/api/v1/x/core.blocks/blocks` |
| `GET` | `/api/v1/x/core.blocks/blocks/{id}/placements` |
| `GET` | `/api/v1/x/core.carousel/shown` |
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
| `GET` | `/api/v1/x/core.favorites/` |
| `POST` | `/api/v1/x/core.favorites/` |
| `POST` | `/api/v1/x/core.html/edit-html` |
| `POST` | `/api/v1/x/core.html/to-markdown` |
| `GET` | `/api/v1/x/core.menu/menus` |
| `GET` | `/api/v1/x/core.menu/menus/{id}` |
| `POST` | `/api/v1/x/core.menu/menus` |
| `PUT` | `/api/v1/x/core.menu/menus/{id}` |
| `DELETE` | `/api/v1/x/core.menu/menus/{id}` |
| `GET` | `/api/v1/x/core.properties/types` |
| `POST` | `/api/v1/x/core.properties/trend` |
| `GET` | `/api/v1/x/core.properties/defs` |
| `POST` | `/api/v1/x/core.properties/defs` |
| `PUT` | `/api/v1/x/core.properties/defs/{id}` |
| `DELETE` | `/api/v1/x/core.properties/defs/{id}` |
| `GET` | `/api/v1/x/core.properties/databases/{id}/views` |
| `POST` | `/api/v1/x/core.properties/databases/{id}/views` |
| `PUT` | `/api/v1/x/core.properties/settings/{id}` |
| `GET` | `/api/v1/x/core.properties/subscriptions` |
| `POST` | `/api/v1/x/core.properties/subscriptions` |
| `DELETE` | `/api/v1/x/core.properties/subscriptions/{id}` |
| `POST` | `/api/v1/x/core.properties/query` |
| `POST` | `/api/v1/x/core.properties/query.csv` |
| `POST` | `/api/v1/x/core.properties/query.xlsx` |
| `POST` | `/api/v1/x/core.properties/report` |
| `GET` | `/api/v1/x/core.properties/values` |
| `DELETE` | `/api/v1/x/core.properties/views/{id}` |
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
| `GET` | `/api/v1/x/core.waf/status` |
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
| `GET` | `/api/v1/x/content.forms/submissions/recent` |
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
