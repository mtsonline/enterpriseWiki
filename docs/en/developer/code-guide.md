# Developer guide: where is what

This guide says for every area of the wiki in which folder and file it is and what happens there. It is meant for new developers and for everybody who looks for the cause of a bug. Whoever adds, moves or renames code updates this guide in the same pull request; `go run ./tools/docscheck` checks that every linked file exists and that every package and every plugin is listed. How the parts work together is described in [architecture.md](architecture.md), plugins in [plugins.md](plugins.md), the API in [api.md](api.md) and the security tests in [security-testing.md](security-testing.md).

## Conventions

- **One area, one file.** A file is named after what it does: the routes of the API for webhooks are in `internal/server/api_webhooks.go`, the service behind them in `internal/webhook/`. Tests are named after their topic too (`webhooks_test.go`).
- **Handlers are thin, services check.** A route in `internal/server` reads the request and calls a service. The service (`content`, `auth`, `media` …) checks the permission itself, so API, command line and plugins take the same way.
- **Every route has an action.** Above each route stands `// action: <id>`; the entry of the action register (`internal/actions/core/*.yaml`, `plugins/*/actions.yaml`) describes it: permission, command line, interface, examples. Search for the action id to find route, command and tests at once.
- **Comments say why.** Every package starts with a package comment; read it first. Comments name the rule that applies (security, limit, transaction), not what the next line does.

## Finding the cause of a bug

Start where the symptom points:

| Symptom | Where to look first |
|---|---|
| `403` with code `forbidden` | the check in the service ([`internal/acl/`](../../../internal/acl/), [`internal/perm/`](../../../internal/perm/)); the route in `internal/server/api_*.go`; with `--as`: [`internal/identity/`](../../../internal/identity/) |
| `403` with code `waf_blocked` | the firewall of the wiki: the server log (`waf:` names the rules), [`plugins/core_waf/exclusions.conf`](../../../plugins/core_waf/exclusions.conf) |
| `500` or `request failed` in the log | the log line names method and path; search the path in `internal/server/` (or `plugins/*/` for `/api/v1/x/…`); errors are typed in [`internal/apperr/`](../../../internal/apperr/) |
| The wiki does not start | configuration [`internal/config/`](../../../internal/config/), database and migrations [`internal/db/db.go`](../../../internal/db/db.go) (an outdated development database is refused there), wiring [`internal/app/app.go`](../../../internal/app/app.go) |
| A text is missing or in English | `internal/i18n/locales/` and `plugins/*/locales/` (interface), `internal/apperr/locales/` (error messages); collected by [`internal/i18n/i18n.go`](../../../internal/i18n/i18n.go) |
| A page looks wrong | the view in `web/src/views/`, styles in `web/src/styles/`, the template in [`plugins/core_template/`](../../../plugins/core_template/); the content of a page is cleaned and rendered in [`internal/content/macros.go`](../../../internal/content/macros.go) |
| A block (macro) shows nothing or the wrong thing | the block's plugin ([`plugins/core_macros/`](../../../plugins/core_macros/), [`plugins/core_layout/`](../../../plugins/core_layout/) or the plugin named in the block list), rendering in [`internal/content/macros.go`](../../../internal/content/macros.go) |
| An e-mail or notification does not arrive | *Administration → E-mail* (outbox), [`internal/mail/`](../../../internal/mail/), [`internal/notify/`](../../../internal/notify/); follow-up work of changes: [`internal/content/changes.go`](../../../internal/content/changes.go) and the worker in [`internal/worker/`](../../../internal/worker/) |
| Sign-in fails | [`internal/auth/service.go`](../../../internal/auth/service.go) (chain of methods), the plugin of the method (`plugins/auth_*`, `plugins/mfa_*`), routes in [`internal/server/api_auth.go`](../../../internal/server/api_auth.go) and [`internal/server/api_sso.go`](../../../internal/server/api_sso.go) |
| Search finds too much or too little | [`internal/content/search.go`](../../../internal/content/search.go) (PostgreSQL), [`plugins/search_external/`](../../../plugins/search_external/) (external engines); permissions are applied in the search itself |
| A workflow step does not run | [`plugins/workflow_process/engine.go`](../../../plugins/workflow_process/engine.go) and the step's file in [`plugins/workflow_process/`](../../../plugins/workflow_process/); timers in `timers.go`; the server log (`workflow step failed`) |
| A command of `ewiki` behaves differently from the interface | commands go through the API in process ([`internal/cli/commands.go`](../../../internal/cli/commands.go), `serveInProcess`); generated commands come from the action register ([`internal/cli/register.go`](../../../internal/cli/register.go)) |

## The way of a request

1. [`cmd/ewiki/main.go`](../../../cmd/ewiki/main.go) starts the command line ([`internal/cli/cli.go`](../../../internal/cli/cli.go)); `ewiki serve` builds the app.
2. [`internal/app/app.go`](../../../internal/app/app.go) wires configuration, database, services and plugins; [`internal/app/instance.go`](../../../internal/app/instance.go) holds what all sites of an instance share.
3. [`internal/server/instance.go`](../../../internal/server/instance.go) picks the site by host name; [`internal/server/middleware.go`](../../../internal/server/middleware.go) (`wrap`, `authenticate`) sets the security headers, limits the request, finds the client address and the signed-in person.
4. [`internal/server/api.go`](../../../internal/server/api.go) registers all routes (`routes`) and wraps each handler (`handle`): permission of the route, JSON answer, error format. The routes themselves are in the `api_*.go` files below.
5. The handler calls a service ([`internal/content/`](../../../internal/content/), [`internal/auth/`](../../../internal/auth/), [`internal/media/`](../../../internal/media/) …), which checks the permission and writes to PostgreSQL; changes are recorded by [`internal/audit/`](../../../internal/audit/).
6. Plugin routes (`/api/v1/x/<plugin>/…`) are registered by the plugin in its `Setup` through [`internal/coreplugin/coreplugin.go`](../../../internal/coreplugin/coreplugin.go).

## Core packages (`internal/`)

Each package begins with a package comment that explains it in more detail.

| Package | What it does |
|---|---|
| [`acl`](../../../internal/acl/) | permissions of categories and elements, checked from a snapshot in memory |
| [`actions`](../../../internal/actions/) | the action register: one entry per action of the API (core in `core/*.yaml`) |
| [`app`](../../../internal/app/) | wiring of configuration, database, services and plugins; sites of an instance |
| [`apperr`](../../../internal/apperr/) | typed errors and their messages in German and English |
| [`audit`](../../../internal/audit/) | the unchangeable audit log (before and after of every change) |
| [`auth`](../../../internal/auth/) | sign-in methods, sessions, API keys, SSH keys, users, groups, roles, SCIM, multi-factor sign-in |
| [`catlist`](../../../internal/catlist/) | shared parts of types that present a category (blog, FAQ) |
| [`cli`](../../../internal/cli/) | the command line `ewiki`; every command goes through the API in process |
| [`collab`](../../../internal/collab/) | relay of live editing between editors |
| [`config`](../../../internal/config/) | `config.yaml` as the single source of settings, `.env`, atomic writes |
| [`content`](../../../internal/content/) | categories, elements, versions, translations, trash, search, tags, blocks, placements, paths, spaces |
| [`coreplugin`](../../../internal/coreplugin/) | the interface of compiled-in plugins (`Host`, `Register`) |
| [`csvsafe`](../../../internal/csvsafe/) | CSV values that spreadsheets never take as formulas |
| [`db`](../../../internal/db/) | connection pool, migrations, check against outdated development databases |
| [`events`](../../../internal/events/) | events for clearing caches (in process or PostgreSQL NOTIFY) |
| [`feed`](../../../internal/feed/) | writing Atom and RSS feeds |
| [`feedfetch`](../../../internal/feedfetch/) | reading external feeds with protection against internal addresses |
| [`httpx`](../../../internal/httpx/) | small HTTP helpers: JSON answers, query parameters |
| [`i18n`](../../../internal/i18n/) | texts of core and plugins, completeness, import and export of translations |
| [`identity`](../../../internal/identity/) | the person behind a request (`Principal`) |
| [`kernel`](../../../internal/kernel/) | which plugins are active, their order and dependencies |
| [`license`](../../../internal/license/) | offline check of license keys |
| [`limits`](../../../internal/limits/) | register of technical limits (expert settings) |
| [`mail`](../../../internal/mail/) | outbox in the database, worker with retries, SMTP, sendmail |
| [`markdown`](../../../internal/markdown/) | Markdown of pages with block directives, HTML to Markdown |
| [`media`](../../../internal/media/) | media library: files in categories, versions, rights, watermarks, image editor |
| [`netguard`](../../../internal/netguard/) | HTTP clients that refuse internal addresses (SSRF protection) |
| [`netlist`](../../../internal/netlist/) | lists of allowed addresses, networks and host names |
| [`notify`](../../../internal/notify/) | notifications in the wiki (bell) and by e-mail |
| [`ooxml`](../../../internal/ooxml/) | simple Word and Excel files without dependencies |
| [`pagecache`](../../../internal/pagecache/) | cache of pages for guests |
| [`pdfdoc`](../../../internal/pdfdoc/) | simple PDF documents (records) |
| [`perm`](../../../internal/perm/) | register of permissions (global and per category) |
| [`pgversion`](../../../internal/pgversion/) | checks of the PostgreSQL version |
| [`pluginmgr`](../../../internal/pluginmgr/) | installing, updating and removing plugins, rebuilding the program |
| [`readability`](../../../internal/readability/) | long sentences and words (plain language) |
| [`secrets`](../../../internal/secrets/) | protection of secrets stored in the database |
| [`server`](../../../internal/server/) | the REST API (`/api/v1`) and delivery of the frontend; routes by area, see below |
| [`shellcmd`](../../../internal/shellcmd/) | shell commands of the macro and of workflow steps: approval by exact text, safe running |
| [`signature`](../../../internal/signature/) | signing records with methods of plugins, verifiable evidence |
| [`storage`](../../../internal/storage/) | files stored by their SHA-256, safe delivery |
| [`tlsutil`](../../../internal/tlsutil/) | TLS for mail and directory servers, with an own certificate authority |
| [`version`](../../../internal/version/) | build information |
| [`watch`](../../../internal/watch/) | watching categories and elements, e-mails about changes |
| [`webhook`](../../../internal/webhook/) | HTTP notifications to Teams, Slack or any JSON endpoint |
| [`wopi`](../../../internal/wopi/) | helpers for office servers (discovery, proof keys) |
| [`worker`](../../../internal/worker/) | periodic background jobs, one node at a time in a cluster |
| [`xlsx`](../../../internal/xlsx/) | simple spreadsheets |

### Content and sign-in in detail

- [`content`](../../../internal/content/): The largest package. Its files by topic: [`categories.go`](../../../internal/content/categories.go) (categories), [`elements.go`](../../../internal/content/elements.go) (elements), [`drafts.go`](../../../internal/content/drafts.go) (drafts and templates), [`trash.go`](../../../internal/content/trash.go) (trash), [`search.go`](../../../internal/content/search.go) (search), [`macros.go`](../../../internal/content/macros.go) (rendering of blocks in pages), [`placements.go`](../../../internal/content/placements.go) (placement of blocks), [`paths.go`](../../../internal/content/paths.go) (readable paths), [`spaces.go`](../../../internal/content/spaces.go) (spaces), [`changes.go`](../../../internal/content/changes.go) (durable follow-up work), [`people.go`](../../../internal/content/people.go) (names of people for readers), [`sources.go`](../../../internal/content/sources.go) (sources of lists), [`elementrights.go`](../../../internal/content/elementrights.go) (rights of single elements).
- [`auth`](../../../internal/auth/): Its files: [`service.go`](../../../internal/auth/service.go) (chain of sign-in methods), [`manage.go`](../../../internal/auth/manage.go) (users, groups, roles), [`sessions.go`](../../../internal/auth/sessions.go) (sessions), [`mfaflow.go`](../../../internal/auth/mfaflow.go) (factors of a sign-in), [`sso.go`](../../../internal/auth/sso.go) (single sign-on), [`scim.go`](../../../internal/auth/scim.go) (SCIM), [`directory.go`](../../../internal/auth/directory.go) (directory sync), [`emails.go`](../../../internal/auth/emails.go) (e-mail addresses), [`people.go`](../../../internal/auth/people.go) (who may open whom).

## The API by area (`internal/server/`)

Each file registers the routes of one area in a function `routes…`, which `routes` in `api.go` calls.

| File | Area |
|---|---|
| [`api.go`](../../../internal/server/api.go) | registration of all routes, the wrapper `handle`, common checks |
| [`api_system.go`](../../../internal/server/api_system.go) | health, readiness, TLS check of the proxy, site information (`GET /api/v1/site`) |
| [`api_auth.go`](../../../internal/server/api_auth.go) | sign-in, sign-out, sign-in methods |
| [`api_sso.go`](../../../internal/server/api_sso.go) | single sign-on: start and answer of the identity provider |
| [`api_login.go`](../../../internal/server/api_login.go) | settings of the sign-in page |
| [`api_mfa.go`](../../../internal/server/api_mfa.go) | further factors of a sign-in, sign-in without password |
| [`api_sessions.go`](../../../internal/server/api_sessions.go) | own sessions, single logout |
| [`api_identity.go`](../../../internal/server/api_identity.go) | e-mail addresses, linked sign-ins, invitations |
| [`api_me.go`](../../../internal/server/api_me.go) | the signed-in person: account, API keys, SSH keys, preferences |
| [`api_profile.go`](../../../internal/server/api_profile.go) | profile (business card), person page |
| [`api_users.go`](../../../internal/server/api_users.go) | administration of users, groups and roles |
| [`api_categories.go`](../../../internal/server/api_categories.go) | categories: tree, settings, translations, export, import |
| [`api_permissions.go`](../../../internal/server/api_permissions.go) | permission rules of categories, central overview |
| [`api_elementrights.go`](../../../internal/server/api_elementrights.go) | rules of a single element |
| [`api_access.go`](../../../internal/server/api_access.go) | access requests |
| [`api_elements.go`](../../../internal/server/api_elements.go) | elements (pages and other types): content, versions, translations; the list of blocks |
| [`api_lookup.go`](../../../internal/server/api_lookup.go) | finding elements, readable paths, the page for unknown addresses |
| [`api_drafts.go`](../../../internal/server/api_drafts.go) | drafts, scheduled publication, templates |
| [`api_trash_search.go`](../../../internal/server/api_trash_search.go) | trash, full text search, tags |
| [`api_files.go`](../../../internal/server/api_files.go) | uploading and delivering files |
| [`api_media.go`](../../../internal/server/api_media.go) | media library |
| [`api_office.go`](../../../internal/server/api_office.go) | office programs, WOPI host |
| [`api_notifications.go`](../../../internal/server/api_notifications.go) | notifications (bell) and the own choice of e-mails |
| [`api_watches.go`](../../../internal/server/api_watches.go) | own watches |
| [`api_feeds.go`](../../../internal/server/api_feeds.go) | feeds of categories, personal feed key |
| [`api_placements.go`](../../../internal/server/api_placements.go) | placement of blocks of the interface |
| [`api_instances.go`](../../../internal/server/api_instances.go) | instances of plugins (a blog, a form …) |
| [`api_data.go`](../../../internal/server/api_data.go) | data of blocks and plugins for reports |
| [`api_signatures.go`](../../../internal/server/api_signatures.go) | signatures and their verification |
| [`api_i18n.go`](../../../internal/server/api_i18n.go) | texts of the interface and their translations |
| [`api_settings.go`](../../../internal/server/api_settings.go) | settings of the site |
| [`settings_secrets.go`](../../../internal/server/settings_secrets.go) | masking secrets in settings before they leave the server |
| [`api_secrets.go`](../../../internal/server/api_secrets.go) | mode of stored secrets (plain or sealed) |
| [`api_limits.go`](../../../internal/server/api_limits.go) | technical limits (expert settings) |
| [`api_plugins.go`](../../../internal/server/api_plugins.go) | plugins: list, switch on and off, install, update, remove |
| [`api_overview.go`](../../../internal/server/api_overview.go) | overview of the administration, update check, cache, state of the database |
| [`api_mail.go`](../../../internal/server/api_mail.go) | administration of e-mail and the outbox |
| [`api_jobs.go`](../../../internal/server/api_jobs.go) | background jobs |
| [`api_webhooks.go`](../../../internal/server/api_webhooks.go) | webhooks |
| [`api_audit.go`](../../../internal/server/api_audit.go) | audit log and its export |
| [`api_license.go`](../../../internal/server/api_license.go) | license key |
| [`api_sites.go`](../../../internal/server/api_sites.go) | several sites of one instance |
| [`scim.go`](../../../internal/server/scim.go) | SCIM service for directories that create accounts |
| [`consent.go`](../../../internal/server/consent.go) | consent to cookies and the privacy policy |
| [`maintenance.go`](../../../internal/server/maintenance.go) | maintenance mode |
| [`scheme.go`](../../../internal/server/scheme.go) | light and dark display |
| [`ratelimit.go`](../../../internal/server/ratelimit.go) | limits of requests per client |
| [`bodylimit.go`](../../../internal/server/bodylimit.go) | limits of request sizes and parallel uploads |
| [`prerender.go`](../../../internal/server/prerender.go) | pages for guests and search engines without JavaScript |
| [`collab.go`](../../../internal/server/collab.go) | live editing (WebSocket) |
| [`demo.go`](../../../internal/server/demo.go) | demo content: create, remove; the content in `demo_*.go` |
| [`server.go`](../../../internal/server/server.go) | the server, delivery of the frontend |

## Plugins (`plugins/`)

Everything outside the core is a plugin. The description of each plugin is in its `plugin.json`.

| Folder | Id | Name |
|---|---|---|
| [`auth_ldap`](../../../plugins/auth_ldap/) | `auth.ldap` | LDAP / Active Directory |
| [`auth_local`](../../../plugins/auth_local/) | `auth.local` | Local authentication |
| [`auth_oidc`](../../../plugins/auth_oidc/) | `auth.oidc` | OpenID Connect (Keycloak, Entra ID, ...) |
| [`auth_shib`](../../../plugins/auth_shib/) | `auth.shib` | Shibboleth / SAML 2.0 |
| [`content_blog`](../../../plugins/content_blog/) | `content.blog` | Blog |
| [`content_catlist`](../../../plugins/content_catlist/) | `content.catlist` | Category overview |
| [`content_events`](../../../plugins/content_events/) | `content.events` | Event calendar |
| [`content_faq`](../../../plugins/content_faq/) | `content.faq` | FAQ |
| [`content_forms`](../../../plugins/content_forms/) | `content.forms` | Forms |
| [`content_notes`](../../../plugins/content_notes/) | `content.notes` | Shared notes |
| [`content_propdb`](../../../plugins/content_propdb/) | `content.propdb` | Property database |
| [`content_team`](../../../plugins/content_team/) | `content.team` | Team |
| [`core_attachment`](../../../plugins/core_attachment/) | `core.attachment` | File / attachment |
| [`core_blocks`](../../../plugins/core_blocks/) | `core.blocks` | Blocks at positions |
| [`core_carousel`](../../../plugins/core_carousel/) | `core.carousel` | Carousel |
| [`core_comments`](../../../plugins/core_comments/) | `core.comments` | Comments |
| [`core_favorites`](../../../plugins/core_favorites/) | `core.favorites` | Favorites |
| [`core_html`](../../../plugins/core_html/) | `core.html` | Rich text page (HTML / Markdown) |
| [`core_layout`](../../../plugins/core_layout/) | `core.layout` | Layout builder macros |
| [`core_macros`](../../../plugins/core_macros/) | `core.macros` | Standard macros |
| [`core_megamenu`](../../../plugins/core_megamenu/) | `core.megamenu` | Mega menu |
| [`core_mentions`](../../../plugins/core_mentions/) | `core.mentions` | Mentions |
| [`core_menu`](../../../plugins/core_menu/) | `core.menu` | Menus |
| [`core_properties`](../../../plugins/core_properties/) | `core.properties` | Properties |
| [`core_reactions`](../../../plugins/core_reactions/) | `core.reactions` | Reactions |
| [`core_share`](../../../plugins/core_share/) | `core.share` | Share |
| [`core_template`](../../../plugins/core_template/) | `core.template` | Template designer |
| [`core_waf`](../../../plugins/core_waf/) | `core.waf` | Web application firewall |
| [`example_notice`](../../../plugins/example_notice/) | `example.notice` | Notice box (example plugin) |
| [`macro_http`](../../../plugins/macro_http/) | `macro.http` | HTTP macro |
| [`macro_shell`](../../../plugins/macro_shell/) | `macro.shell` | Shell command macro |
| [`mfa_email`](../../../plugins/mfa_email/) | `mfa.email` | Code by e-mail |
| [`mfa_network`](../../../plugins/mfa_network/) | `mfa.network` | Allowed networks |
| [`mfa_totp`](../../../plugins/mfa_totp/) | `mfa.totp` | Authenticator app (TOTP) |
| [`mfa_webauthn`](../../../plugins/mfa_webauthn/) | `mfa.webauthn` | Security keys and passkeys (WebAuthn) |
| [`office_collabora`](../../../plugins/office_collabora/) | `office.collabora` | Collabora Online |
| [`office_m365`](../../../plugins/office_m365/) | `office.m365` | Microsoft 365 for the web |
| [`office_onlyoffice`](../../../plugins/office_onlyoffice/) | `office.onlyoffice` | OnlyOffice Docs |
| [`search_external`](../../../plugins/search_external/) | `search.external` | External search engine |
| [`security_crowdsec`](../../../plugins/security_crowdsec/) | `security.crowdsec` | CrowdSec |
| [`signature_passkey`](../../../plugins/signature_passkey/) | `signature.passkey` | Signature with passkey |
| [`signature_smime`](../../../plugins/signature_smime/) | `signature.smime` | S/MIME |
| [`theme_cms`](../../../plugins/theme_cms/) | `theme.cms` | CMS / website template |
| [`theme_cms_modern`](../../../plugins/theme_cms_modern/) | `theme.cms.modern` | Website template – modern |
| [`theme_wiki`](../../../plugins/theme_wiki/) | `theme.wiki` | Wiki template |
| [`theme_wiki_dark`](../../../plugins/theme_wiki_dark/) | `theme.wiki.dark` | Wiki template (dark) – example |
| [`workflow_process`](../../../plugins/workflow_process/) | `workflow.process` | Workflow |

### Parts of a plugin

- `plugin.go`: the Go side; `Setup` registers routes, element types, blocks, jobs and hooks with the `Host`.
- `plugin.json`: the manifest (id, name, permissions, settings, limits, dependencies).
- `actions.yaml` (with `actions.go`, which embeds it): the plugin's entries of the action register.
- `locales/`: texts in German and English; `migrations/`: the plugin's own tables; `web/index.ts`: its part of the interface.
- [`plugins/all/zz_generated.go`](../../../plugins/all/zz_generated.go) imports all plugins; it is generated by `go run ./tools/genplugins`.

## Frontend (`web/`)

- [`web/src/main.ts`](../../../web/src/main.ts): start of the interface and the list of all pages (`route('/…', view)`). Search here for an address to find its view.
- `web/src/views/`: one file per page or group of pages (for example [`web/src/views/category.ts`](../../../web/src/views/category.ts), [`web/src/views/admin.ts`](../../../web/src/views/admin.ts)).
- `web/src/core/`: the basis: [`web/src/core/api.ts`](../../../web/src/core/api.ts) (all calls of the API), [`web/src/core/dom.ts`](../../../web/src/core/dom.ts) (building elements safely), [`web/src/core/router.ts`](../../../web/src/core/router.ts), [`web/src/core/i18n.ts`](../../../web/src/core/i18n.ts), [`web/src/core/state.ts`](../../../web/src/core/state.ts).
- `web/src/components/`: reusable parts, for example [`web/src/components/ui.ts`](../../../web/src/components/ui.ts) (fields, buttons, dialogs), [`web/src/components/person.ts`](../../../web/src/components/person.ts) (the menu on names), [`web/src/components/bell.ts`](../../../web/src/components/bell.ts) (notifications).
- `web/src/editor/`: the text editor ([`web/src/editor/editor.ts`](../../../web/src/editor/editor.ts)), blocks in the editor, live editing; `web/src/styles/`: CSS.
- [`web/src/coreplugin.ts`](../../../web/src/coreplugin.ts): what plugins may use (`@ewiki/coreplugin`); [`web/build.mjs`](../../../web/build.mjs) bundles the core and every `plugins/*/web/index.ts`.

## Database

- The schema of the core: [`internal/db/migrations/0001_schema.sql`](../../../internal/db/migrations/0001_schema.sql); accounts shared by several sites in `internal/db/migrations/identity/` and `shared/`. Tables of a plugin: `plugins/<dir>/migrations/`.
- [`internal/db/db.go`](../../../internal/db/db.go) applies migrations at the start and refuses to start on a database of an older development version (CLAUDE.md, section 4).

## Texts, translations and the action register

- Texts of the interface: `internal/i18n/locales/de.json` and `en.json`, of each plugin in `plugins/<dir>/locales/`; error messages of the server in `internal/apperr/locales/`. `TestLocaleParity` finds missing ones.
- The action register: [`internal/actions/actions.go`](../../../internal/actions/actions.go) reads `internal/actions/core/*.yaml` (one file per area) and the `actions.yaml` of every plugin. The examples there run as tests ([`internal/server/examples_test.go`](../../../internal/server/examples_test.go)).

## Tests

- Go tests stand next to the code. The tests of the API run against a real PostgreSQL: [`internal/server/integration_test.go`](../../../internal/server/integration_test.go) (`setup`, the client), one file per topic (`internal/server/<topic>_test.go`, for example `workflow_test.go`, `mail_test.go`). Shared helpers: [`internal/server/helpers_test.go`](../../../internal/server/helpers_test.go).
- Browser tests: [`web/tests/e2e.test.mjs`](../../../web/tests/e2e.test.mjs) (whole flows with axe), [`web/tests/a11y-quick.mjs`](../../../web/tests/a11y-quick.mjs) (quick accessibility check); unit tests of the frontend in `web/tests/unit/`.
- Traps: [`tools/traps/traps.go`](../../../tools/traps/traps.go) lists faults built in on purpose; each one must make the named test fail. Tests against real systems (sign-in, search, office, CrowdSec) are in `tests/`; the overall check from outside in `tests/qa/`.

## Tools, scripts and installation

- `tools/`: [`tools/docscheck/`](../../../tools/docscheck/) (documentation), [`tools/traps/`](../../../tools/traps/) (traps), [`tools/genplugins/`](../../../tools/genplugins/) (plugin list), [`tools/notices/`](../../../tools/notices/) (licenses), [`tools/uicoverage/`](../../../tools/uicoverage/) (every action used by the interface), [`tools/testshard/`](../../../tools/testshard/) (splitting the tests in the CI).
- `scripts/`: [`scripts/build.sh`](../../../scripts/build.sh) (build), [`scripts/precheck.sh`](../../../scripts/precheck.sh) (all checks before a pull request), [`scripts/update-docker.sh`](../../../scripts/update-docker.sh) (updates of a Docker installation) and the Docker tests `scripts/test-docker-*.sh`.
- Installation with Docker: [`docker-setup.sh`](../../../docker-setup.sh), [`docker-compose.yml`](../../../docker-compose.yml), files for the containers in `docker/`; the stable SDK for plugins as their own process: [`sdk/`](../../../sdk/).
