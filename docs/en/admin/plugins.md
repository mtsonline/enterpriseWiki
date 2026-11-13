# Managing plugins

Almost everything in the wiki is a plugin: element types (e.g. pages), sign-in methods, menus, templates, admin pages. Plugins are **compiled together with the core**. That makes them fast and type-safe, and there are no runtime dependencies.

| Action | Effect | Rebuild needed? |
|---|---|---|
| enable / disable | at once (API routes, element types, parts of the interface) | no |
| install / update | the source code is copied to the plugin folder, the wiki rebuilds itself and then restarts automatically | yes (automatic) |
| remove | the source code is moved to `.backup/` in the plugin folder, the data stays in the database; rebuild and restart automatic | yes (automatic) |
| copy into the plugin folder | detected and built after about 20 seconds without further changes; active after **Restart** (or automatically with `plugins.auto_restart_on_drop: true`) | yes (automatic) |

Plugin folder: with the source installation `plugins/`, in the Docker Full image `./data/wiki/plugins` (in the container `/data/plugins`).

## In the interface

*Administration → Plugins*:

- list of all plugins with version, kind, dependencies (*Builds on*) and status (active / disabled / inactive because a base plugin is missing / required),
- **Enable/Disable**: a plugin that other active plugins build on must be disabled last. Required plugins (`auth.local`) cannot be disabled,
- **Install** from
  - GitHub: `github.com/owner/repo` (optional tag/branch),
  - any Git URL (`https://…`, `git@…`),
  - a catalogue ID (see below) or
  - a directory on the server,
- **Pending changes** shows what becomes active after the next rebuild,
- after installing, updating or removing, the wiki rebuilds in the background and then restarts automatically; the page reloads itself as soon as the wiki answers again. A failed build leaves the running version unchanged (log under *Build log*),
- **Rebuild now** starts a build by hand, **Restart** switches to the new build without cancelling running requests (in a cluster one node after another),
- **Restart automatically after dropped plugins** (`plugins.auto_restart_on_drop`): otherwise a build from the plugin folder waits for **Restart**, so the wiki does not restart in the middle of copying.

Building requires one of the following variants:

| Variant | Install plugins | Built … |
|---|---|---|
| source installation (`scripts/install.sh`) | yes | in the source tree, the program is replaced |
| Docker **Full image** (`ghcr.io/mtsonline/enterprisewiki:full`, `EWIKI_IMAGE_TAG=full`) | yes | in `./data/wiki/build`, result in `./data/wiki/bin/ewiki-<id>` |
| own image (`docker-compose.build.yml`, target `full`) | yes | as with the Full image; plugins in `plugins/` are already built in |
| Docker **Lite image** (`:latest`, default) | no (note in the interface) | – only enable and disable |

### Docker Full image in detail

- The image additionally contains the source code of the core, Go, Node.js and all Go modules and npm packages of the core. Plugins without further dependencies are therefore built **without internet** too; if a plugin needs further Go modules, they are downloaded during the build.
- Everything that changes is in the data folder and survives updates and newly created containers: `plugins/` (additional plugins), `build/cache/` (build cache), `bin/` (built programs, the last three are kept).
- **On start** the program of the image looks for a built program that fits the core version, architecture and plugin sources and hands over to it. If there is none (first start, new image), it builds first; the first time this takes a few minutes, afterwards much less thanks to the cache. Meanwhile the wiki shows a page "The wiki is being updated" (HTTP 503). If the build fails, the wiki starts without the additional plugins, and *Administration → Plugins* shows the log.
- A plugin with the same ID as a bundled one replaces it (newer version). Bundled plugins can only be disabled, not removed.
- Builds run as the user from `.env`, not as root.
- **Cluster**: all nodes share the data folder. Only one node builds at a time (file lock in `bin/`), the others wait and take over the result. Nodes restart one after another; the next one only when the previous one accepts requests again (coordinated through the database), so the wiki stays reachable.

## Catalogue (1-click installation)

Plugins listed in `config.yaml` appear under *Catalogue* with an install button:

```yaml
plugins:
  catalog:
    - id: acme.kanban
      name: Kanban board
      repo: github.com/acme/ewiki-kanban
      ref: v1.2.0
      description: Agile board as an element type
```

## Command line

```bash
ewiki plugin list
ewiki plugin install github.com/acme/ewiki-kanban --ref v1.2.0 --build
ewiki plugin install ./my-plugin
ewiki plugin enable acme.kanban
ewiki plugin disable acme.kanban
ewiki plugin remove acme.kanban --build
ewiki plugin build              # rebuild, then: systemctl restart enterprisewiki
```

## Updates

*Administration → Updates* compares the installed versions with the newest GitHub release (or, without releases, the highest version tag) of the core and of all plugins that name a `repo` in `plugin.json` or are in the catalogue. The result is cached for one hour; *Check for updates* checks at once.

- **Plugins:** *Update* downloads the release, keeps the old version under `plugins/.backup` and rebuilds the wiki. Then restart under *Plugins*. With the CLI: `ewiki plugin updates`, `ewiki plugin update ID --build`.
- **Core:** is updated on the server with `scripts/update.sh` (or the update timer), with backup and automatic rollback; the interface only shows that an update is available.

For GitHub Enterprise or private repositories see `plugins.updates` in [configuration.md](configuration.md).

## Templates and positions

All template plugins are "active", that is available. **Exactly one is used**: the one chosen under *Administration → Settings → Template* (`ui.template` in `config.yaml`). On the plugin page it is marked as *Template in use*; for the others *Use as template* switches directly.

Every template offers positions where menus (`core.menu`), blocks (`core.blocks`) and widgets of other plugins appear:

| Position | Wiki (`theme.wiki`) | Website (`theme.cms`) |
|---|---|---|
| `top-bar` | bar above the header (e.g. notices) | left in the dark bar at the very top |
| `header-tools` | search, language, edit, user | right in the bar at the very top |
| `header-nav` | navigation in the header | next to the main navigation |
| `cms-nav` | – | main navigation (automatic: top categories) |
| `hero` | – | full width below the header (banner) |
| `sidebar` | sidebar with page tree | area below the content |
| `sidebar-bottom` | sidebar below the page tree | – |
| `content-top`, `content-bottom` | above or below the content of every page | above or below the content of every page |
| `footer-1` … `footer-4` | – | columns in the footer |
| `footer` | footer | bottom line of the footer |

With the wiki template (`theme.wiki`, `theme.wiki.dark`) the sidebar can be hidden on all screen sizes with the button in the header; the state is remembered in the browser.

### Setting up the website template

*Administration → Website template* (applies to both website templates; or `plugin_settings.theme.cms` in `config.yaml`, API `PUT /api/v1/admin/settings`):

| Setting | Meaning |
|---|---|
| `logo`, `logo_light`, `logo_height` | logo (uploaded image) and light variant for the transparent header above the start image; height in pixels |
| `primary_color`, `accent_color` | main colour (links, buttons, active navigation) and accent colour, `#rrggbb` |
| `header` | `overlay`: the header lies transparently above the start image and turns white when scrolling; `solid`: always white |
| `hero_image`, `hero_title`, `hero_subtitle`, `hero_button_label`, `hero_button_link`, `hero_size` | start image only on the home page; size `large` or `medium` |
| `banner`, `banner_image` | page title on sub-pages in an image banner (without an own image a gradient is used) |
| `copyright` | text in the bottom line, `{year}` is replaced by the current year |
| `back_to_top` | "Back to top" button when scrolling |

Images are stored like attachments and not deleted by the storage clean-up as long as a setting refers to them. `GET /api/v1/x/theme.cms/config` delivers the public values. You create menus under *Administration → Menus*: main navigation at `header-nav` (it then replaces the automatic navigation), quick links at `top-bar`, footer columns with blocks or menus at `footer-1` … `footer-4`, legal links at `footer`.

With `?positions=1` on any address (or *Show positions on the page* under *Menus* or *Blocks*) administrators see all positions framed with their names. Menus and blocks at a position the active template does not have are marked in the management and not shown.

Multi-level menus: in horizontal positions (`header-nav`, `top-bar`, `cms-nav`, `header-tools`) sub-menus open as drop-down menus with side flyouts. In the sidebar they are shown as an expandable tree, in the footer completely. On narrow screens and in the off-canvas menu of the CMS template they expand below each other. Templates adapt the look with CSS variables: `--ewm-link`, `--ewm-link-hover-bg`, `--ewm-panel-bg`, `--ewm-panel-border`, `--ewm-accent`.

## External search engine

For very large amounts of data the full-text search can use **OpenSearch/Elasticsearch** (from 7.10 or OpenSearch 1.x) or **Meilisearch** (from 1.3): better relevance, tolerance of typos, relief for the database. Set it up under *Administration → Search engine*: choose the search engine, enter URL, index name and credentials, *Test connection*.

- PostgreSQL stays the reference. Every change (save, move, tags, trash, restore, delete) ends up in a change log through database triggers; a background job transfers it every 10 seconds. After a change of search engine, URL or index and with *Rebuild index* the index is created completely anew.
- The external engine is only used once the index is completely built. Until then, on errors of the search engine and with the plugin disabled, the built-in search answers automatically. The answer of the search API contains `engine` for this.
- The wiki itself always checks permissions, trash and tags; the search engine gets no permission information and does not need to be publicly reachable (it should not be).
- The connection is tested against the documented APIs with test servers; before production use please check with your version (*Test connection*, *Rebuild index*, compare the number of documents).

## Manual installation

Copy the plugin directory (with `plugin.json`, Go files and optionally `web/`, `locales/`, `migrations/`) into the plugin folder – the running server builds it automatically (see above). Without a running server:

- Source installation: `go run ./tools/genplugins` and `./scripts/build.sh`, then restart the service.
- Full image: `docker compose exec wiki ewiki plugin build` (or simply `docker compose restart wiki`, then it is built on start).

## Security

Plugins are full code with access to the database and the server. Only install plugins from trusted sources and with a fixed version (`ref`). Every installation, removal and enabling or disabling is logged.

## Bundled plugins

| ID | Kind | Description |
|---|---|---|
| `auth.local` | authentication | local accounts, always first, required |
| `auth.ldap` | authentication | LDAP / Active Directory |
| `auth.oidc` | authentication | OpenID Connect (Keycloak, Entra ID, …) |
| `auth.shib` | authentication | Shibboleth / SAML 2.0 |
| `mfa.totp` | sign-in factor | code of an authenticator app (RFC 6238) |
| `mfa.email` | sign-in factor | one-time code by e-mail to the primary address |
| `mfa.network` | sign-in factor | allowed networks per person (silent factor, optionally enforced for all sessions, API keys and feed links) |
| `mfa.webauthn` | sign-in factor | passkeys and security keys (FIDO2), sign-in without password too |
| `signature.passkey` | signature | sign decisions (e.g. approvals in workflows) with a passkey or security key, advanced electronic signature |
| `signature.smime` | signature | sign outgoing e-mails with S/MIME (certificates of the organisation and personal ones), expiry warnings, second seal for evidence |
| `office.collabora` | office | open and edit office files in Collabora Online (WOPI) |
| `office.onlyoffice` | office | open and edit office files in OnlyOffice Docs (OnlyOffice API or WOPI) |
| `office.m365` | office | Word, Excel and PowerPoint files in Microsoft 365 for the web (WOPI, only with CSPP) |
| `core.attachment` | element type | files (PDF, office, images, video) with preview and full text |
| `core.html` | element type | pages with WYSIWYG or Markdown |
| `core.menu` | menu | any number of menus, also multi-level, at every position of the template. Targets are categories, elements, links or other menus (*Administration → Menus*). |
| `core.blocks` | general | text blocks (Markdown/HTML, macros) at every position, per language, for everyone/guests/signed-in people (*Administration → Blocks*) |
| `core.macros` | macros | table of contents, child pages, recently changed (with sources), news from several sources and RSS/Atom feeds, notice boxes, expandable section, status label, columns |
| `core.layout` | macros | layout building blocks: section, hero, cards, button, spacing (inherits from `core.macros`) |
| `macro.shell` | macro | output of a shell command, only after exact approval by a superadmin, off by default |
| `macro.http` | macro | result of an HTTP request, only addresses from the allow list |
| `core.comments` | general | comments and replies below pages |
| `core.share` | general | share pages and categories with people, groups and roles (does not change permissions) |
| `core.mentions` | general | @mentions in pages and comments with suggestions while typing |
| `core.reactions` | general | emoji reactions to pages, categories and comments; any emoji, block list under *Administration → Reactions*; notification of the author (e-mail off by default) |
| `core.favorites` | general | personal favourites (star on pages and categories), list at a position of the template (*Administration → Favourites*) and in the profile |
| `core.properties` | general, macro | properties for elements of any type: definitions global and per category (inherited) with more than 30 types, required, default, element types, permissions (who sees, who changes); free properties (allowed or forbidden per category, plugin setting `free`); running numbers and formulas; history; macro "property report" with 13 views. Permissions: `core.properties.admin` (global), `core.properties.define` and `core.properties.free` (category) |
| `content.propdb` | element | property database: elements from sources interactively as table, cards, board, calendar, timeline or chart, with filters, editing and new entries (needs `core.properties`) |
| `core.carousel` | general | carousels at positions of the template: news (option "As news" when publishing), own slides, external RSS/Atom feeds, elements of any sources (*Administration → Carousels*) |
| `core.megamenu` | menu (optional) | "mega menu" view for horizontal menus: wide panels with columns, headings and descriptions (chosen in the menu under *View*) |
| `workflow.process` | element type | processes such as applications or approvals: form, tasks, approvals (four eyes), conditions, e-mails, webhooks, waiting times, reminders; workflow 2: triggers (schedule, page changes, events, accounts and groups, signed incoming webhook), steps message, values/formulas, HTTP request, create page, properties, permissions (also time-limited), group membership, move page, create file, calendar entry, further workflow, parallel branches; decision links in e-mails, test run, analysis, retention; deadlines with working days (holidays under *Administration → Workflow*), warnings and escalation |
| `content.blog` | element type, macro | blog from the pages of a category and further sources: teasers, layouts, search in the blog, tags, Atom/RSS; macro "blog posts from several sources" |
| `content.forms` | element type | web forms: all usual fields, conditions, validation rules, uploads with checks (file type by content, office macros, active PDF content, optionally ClamAV and/or ICAP), grid or freely placed, steps, computed values, repeating groups, signature, arithmetic task against bots; store submissions, export them, send them by e-mail, webhook, into a property database or to a workflow; settings `clamd` and `icap` under *Administration → Forms*; macro "form results" |
| `content.faq` | element type, macro | FAQ from categories and pages (plus further sources): expandable sections and questions, instant filter, Atom/RSS; macro "FAQ from several sources" |
| `content.events` | element type | event calendar: month/week/list, series, registration with waiting list, e-mail reminders, iCal subscription, further calendars and iCal subscriptions as sources, macro "next events" |
| `content.catlist` | element type | category overview: sub-categories of a category or of the top level as cards or list, optionally with pages (e.g. as home page) |
| `content.team` | element type | team page from groups with selectable details from the business card or the identity provider |
| `content.notes` | element type | shared notes: write together live directly in the page view, saved automatically (meetings, brainstorming, task lists) |
| `search.external` | general | OpenSearch/Elasticsearch or Meilisearch for the full-text search |
| `theme.wiki` | template | documentation layout with page tree |
| `theme.cms` | template | website, classic (university, organisation): main navigation, transparent header above the start image, title banner, footer columns |
| `theme.cms.modern` | template | website, modern and neutral: inherits structure, positions and settings from `theme.cms`, own design (font Inter, glass header, left-aligned start image, rounded cards) |
| `theme.wiki.dark` | template (example) | inherits from `theme.wiki`, only changes the colours |
| `example.notice` | element type (example) | notice box, inherits from `core.html` |

Developing own plugins: [Developing plugins](../developer/plugins.md)
