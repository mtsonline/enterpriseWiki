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

## Origin of core plugins

The plugins on this page are **core plugins**: they are compiled with the wiki, run inside the wiki process and have the same rights as the wiki itself. That is why the wiki shows a warning before every installation and checks where the code comes from. The check looks only at the source, never at what the plugin says about itself.

| Origin | From | Installing |
|---|---|---|
| **Official** | shipped with the wiki (later also with a verified signature of the project) | with a warning |
| **Organisation** | from a source an instance administrator entered as trusted beforehand | with a warning |
| **External** | any other source, for example an unknown GitHub repository | with a warning **and** a checkbox that must be ticked; again on every update |
| **Unknown** | a folder copied into the plugin folder by hand | treated like *External* |

**SDK plugins are not affected.** They run as a separate, isolated process and reach the wiki only through the SDK.

### Trusted sources

*Administration → Plugins → Trusted sources* (instance administrators only):

- An entry is an address (`https://…` or `ssh://…`, also `git@host:path`) or a directory on the server. It covers the address and everything below it, only at path boundaries: `https://git.example.com/team` covers `https://git.example.com/team/kanban`, but not `https://git.example.com/team-other/kanban`.
- A whole server without a path is not accepted. Addresses with a password, a query (`?`), `..`, empty path segments (`//`), `%` encoding, `\`, control characters or `http://` are refused, so the wiki checks exactly the address git fetches. Upper and lower case of the server, `.git` at the end, the default port and a user name before the server do not matter.
- **Allow external sources** (`plugins.allow_external`, default: on): switched off, only official core plugins and those from trusted sources can be installed.
- Adding a source and allowing external sources again needs a **sign-in within the last 15 minutes** in the browser, or the command line on the server; API keys cannot do it. Removing and switching off is always possible.
- Core plugins already installed stay installed when their source is removed.

The plugin list shows the level, the source and the commit in the *Origin* column. For this the wiki writes the file `.ewiki-origin.json` into the folder of every plugin it installs. A file of that name in the sources of a plugin is removed when installing.

In `config.yaml`:

```yaml
plugins:
  allow_external: true
  trusted_sources:
    - prefix: https://git.example.com/wiki-plugins
      note: Plugins of our team
    - prefix: /srv/ewiki-plugins
```

### Access data for private sources

Plugins from private repositories (for example Pro plugins or the organisation's own plugins) need access data. *Administration → Plugins → Access data for plugin sources* (instance administrators only):

- An entry covers all repositories below an **https address**, e.g. `https://github.com/acme` or `https://git.example.com/team`, at path boundaries as for trusted sources. When several entries match, the longest wins. Nothing is sent without `https://`.
- **Token:** preferably a read-only token for the repositories needed only (on GitHub a "fine-grained token" with *Contents: Read-only*). The user name is `x-access-token` when not given, as GitHub expects for tokens.
- The wiki uses the access data when **installing**, **updating** and **checking for updates**. Git gets them through the environment only, never in the command line or the address; redirects are not followed, so the token does not go to another server. Error messages show it blanked out.
- The token is **never shown again** after saving, neither in the API nor in the audit log (which records the address only). To change it, save a new entry with the same address.
- Saving needs a **sign-in within the last 15 minutes** or the command line on the server. Removing is always possible.
- On the command line, without the token in the shell history: `printf '{"prefix":"https://github.com/acme","token":"%s"}' "$TOKEN" | ewiki plugin credentials set --json -`; `ewiki plugin credentials list`, `ewiki plugin credentials remove --prefix …`.

In `config.yaml` (stored like other secrets; a reference to an environment variable is possible here only, not through the interface or the API):

```yaml
plugins:
  credentials:
    - prefix: https://github.com/acme
      token: ${ACME_PLUGIN_TOKEN}
      note: Pro plugins
```

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

# origin
ewiki plugin install github.com/other/plugin --accept-external   # external source: confirm the warning
ewiki plugin sources list
ewiki plugin sources add https://git.example.com/wiki-plugins --note "our team"
ewiki plugin sources remove https://git.example.com/wiki-plugins
ewiki plugin sources check github.com/acme/ewiki-kanban
ewiki plugin sources external forbid   # or: allow
```

Without `--accept-external` an installation from an external source stops with the warning.

## Updates

*Administration → Updates* compares the installed versions with the newest GitHub release (or, without releases, the highest version tag) of the core and of all plugins that name a `repo` in `plugin.json` or are in the catalogue. The result is cached for one hour; *Check for updates* checks at once.

- **Plugins:** *Update* downloads the release, keeps the old version under `plugins/.backup` and rebuilds the wiki. Then restart under *Plugins*. With the CLI: `ewiki plugin updates`, `ewiki plugin update ID --build`.
- **Core:** is updated on the server with `scripts/update.sh` (or the update timer), with backup and automatic rollback; the interface only shows that an update is available.

Private repositories: see [Access data for private sources](#access-data-for-private-sources). For GitHub Enterprise see `plugins.updates` in [configuration.md](configuration.md).

## Templates and positions

All template plugins are "active", that is available. **Exactly one is used**: the one chosen under *Administration → Settings → Template* (`ui.template` in `config.yaml`). On the plugin page it is marked as *Template in use*; for the others *Use as template* switches directly.

Every template offers positions where menus (`core.menu`), blocks (`core.blocks`, elements of the type *Block*) and widgets of other plugins appear:

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
| `logo`, `logo_light`, `logo_height` | logo (uploaded image) and light variant for the transparent header above the start image; height in pixels; in the dark scheme the header always shows the light variant (without one, the normal logo) |
| `primary_color`, `accent_color` | main colour (links, buttons, active navigation) and accent colour, `#rrggbb`; in the dark scheme the wiki lightens the main colour as far as needed for 4.5 : 1 |
| `header` | `overlay`: the header lies transparently above the start image and turns white when scrolling; `solid`: always white |
| `hero_image`, `hero_title`, `hero_subtitle`, `hero_button_label`, `hero_button_link`, `hero_size` | start image only on the home page; size `large` or `medium` |
| `banner`, `banner_image` | page title on sub-pages in an image banner (without an own image a gradient is used) |
| `copyright` | text in the bottom line, `{year}` is replaced by the current year |
| `back_to_top` | "Back to top" button when scrolling |

Images are stored like attachments and not deleted by the storage clean-up as long as a setting refers to them. `GET /api/v1/x/theme.cms/config` delivers the public values. You create menus under *Administration → Menus*: main navigation at `header-nav` (it then replaces the automatic navigation), quick links at `top-bar`, footer columns with blocks or menus at `footer-1` … `footer-4`, legal links at `footer`.

With `?positions=1` on any address (or *Show positions on the page* under *Menus* or *Blocks*) administrators see all positions framed with their names. Menus and blocks at a position the active template does not have are marked in the management and not shown.

Multi-level menus: in horizontal positions (`header-nav`, `top-bar`, `cms-nav`, `header-tools`) sub-menus open as drop-down menus with side flyouts. In the sidebar they are shown as an expandable tree, in the footer completely. On narrow screens and in the off-canvas menu of the CMS template they expand below each other. Templates adapt the look with CSS variables: `--ewm-link`, `--ewm-link-hover-bg`, `--ewm-panel-bg`, `--ewm-panel-border`, `--ewm-accent`.

### Templates of data (template designer)

Besides templates of code (plugins, as references), the wiki has **templates of data**. Such a template is only a description: rows of areas with positions, design values and the views for tablet and phone. The wiki builds the page from it with the same code in the designer and on every page, so what is designed is what is shown.

- **Where:** *Administration → Templates* has tabs. The *Overview* shows each template as a card with a schematic: where its positions are on a desktop, a tablet and a phone held upright or sideways (choose the device above the cards). On a tablet or phone, the schematic shows which positions are behind the menu button or the tools button, and what is hidden there. The card says which template is active for the website and how many categories choose it (`GET /api/v1/admin/templates/usage`, CLI `ewiki templates usage`). The tab *Positions and placements* shows the schematic of a template with its positions as buttons and arranges the blocks for the whole website (see the user guide); it replaces the former page *Placement* (`/admin/placement` leads there). *Edit* leads to the tab *Designer*; *Own fonts* and *Trash* have tabs of their own. Templates of code cannot be changed or copied. The two shipped templates of data (*Wiki (data)*, *Website (data)*) are read only; copy one to change it. The designer has two views of the same design, and a change in one is in the other at once:
  - **visual:** the structure (rows from top to bottom, areas side by side; choose, move with the arrow buttons, add, delete), the properties of a row or area, the views of tablet and phone (automatic, or adjusted by hand and *Automatic again*), colours with the contrast of every pair and the nearest colour that passes (*Take*), font, size, corners, logo and behaviour. Everything works with the keyboard; undo and redo also with Ctrl+Z and Ctrl+Y;
  - **as text (JSON)**, checked while typing.

  Every change is checked by the server at once. At each row the designer says how much room the areas need and how much there is in each view.
- **Preview:** the wiki itself with the design, as a desktop (1280 px), tablet (768 px) or phone (375 px), on any page of the wiki, also in a window of its own. It is shown only to you, with your permissions; nothing is saved until you save. The preview measures what really has room, for example how many entries of a menu fit on one line and how high a sticky header is, and says it under *Hints*. A sticky header sticks only up to a quarter of the window height (`sticky_max_vh`); if it is higher, the hint says so, in the phone view also for a phone held sideways (about 360 px window height). That is why the shipped templates of data put the tools (search, language, edit, notifications, user menu) behind a button *Tools* on tablet and phone too. The engine writes CSS only from values of a safe form, so a draft that is not checked yet cannot change anything else.
- **Who:** the permission `core.template.design` (create, copy, import, change, trash). Which template applies is still chosen with the settings of the site (`core.settings.manage`) and of a category (*Manage*).
- **Checks on the server**, the same as in the designer (`plugins/core_template/limits.json`):
  - exactly one area is the content of the page, and at least one more area (for the user menu);
  - wherever parts stand side by side there must be room: the areas of every row (not only beside the content) and the positions inside an area (layout `row`). On the desktop this is always checked; on tablet and phone when a view keeps them side by side, otherwise they go one below the other. The text says how much room is needed and how much there is;
  - a sticky header takes at most a quarter of the height of the window (`sticky_max_vh`); when it is higher, for example on a phone held sideways or with an open drawer, it scrolls with the page, so it never hides the content or the focus;
  - every color pair has enough contrast (text 4.5:1, controls and focus 3:1), in the dark colors too;
  - header and footer are rows with the role `banner` or `contentinfo`; adjacent rows form one landmark;
  - several navigations or side columns need different labels; positions of the pages themselves (`element-…`, `category-…`) cannot be used;
  - only fonts of the wiki itself and pictures of the wiki (`/api/v1/files/…`); unknown fields are refused.
- **Light and dark:** under *Design → Dark colours*, *Offer dark colours* gives a template a second palette (in the tokens under `dark`, the same 13 colours). The first suggestion takes the dark colours of the wiki for surfaces and text and lightens the main colour, link and focus of the design as far as the contrast needs; every colour can be changed, with its own contrast list and *take*. With dark colours the template follows the choice of every person (profile, block *Light and dark*); without a choice *Colour scheme without a choice of the person* applies (`options.color_scheme`: `system`, `light`, `dark`; default `system`). Without dark colours the template stays light. The preview shows both schemes (*Colour scheme of the preview*).
- **Views:** tablet and phone follow from the desktop by rules: side columns beside the content and the menus of the header go into drawers (a button opens them, Escape closes them), areas that do not fit go to the next line or one below the other. A row can set its view by hand (`tablet`, `mobile`: `stack`, `hidden`, `drawer`, `widths`). **Own CSS:** when the settings allow it (`ui.custom_css`), the designer has a field *Own CSS* under *Design*; it checks while typing by the same rules as the server (`limits.json`, section `css`). **On the desktop too** an area can be in a drawer (`desktop`: only `drawer`), for example the menu as a "burger menu"; it is then in a drawer in every view and needs no room in the row.
- **Behaviour** (designer: *Behaviour* and the properties of a row or area):
  - the sticky header can get **lower** once the page is scrolled (`header_shrink`) and **go away while scrolling down** (`header_hide`); it comes back when scrolling up and whenever the keyboard focus is in it, so it never hides what has the focus. Both need `sticky_header`;
  - the header can **lie on a hero banner** (`header_overlay`): on pages that begin with a hero banner right below the header, the header has no background of its own and takes the color of the banner's text (whose contrast the banner checked); the banner reaches under it over the whole width. It gets its background back while a menu or drawer of it is open and, when sticky, once the banner is scrolled away. With a side column beside the content, or with something above the banner (a title, a path), the header stays as it is;
  - the **path** (breadcrumbs) on or off for a template: the path is the building block *Path of the page* (`core.element.path`) and, on category pages, *Category: path* (`core.category.path`); in *Administration → Placement* on the level *Template* both are set to no position for this template (or placed elsewhere), and categories, element types or single pages can still show it again;
  - a side column (role `complementary`) can **stay in view** beside the content while scrolling (`sticky`; when it is higher than the window it scrolls in itself) and can be **hidden with a button** (`collapsible`; the browser keeps the choice). Both work on the desktop only; on tablet and phone the column follows the view as designed. Not together with a drawer on the desktop;
  - a footer row (role `contentinfo`) can **open and close on the phone** (`collapse_mobile`): each area shows only its name, a tap opens it. Every area then needs a name (`label`);
  - **print only the content** (`print_plain`): header, footer, side columns and buttons are left out when printing.
  - **boxed layout** (`boxed`): the whole page – header, content and footer – is a box of the content width in the middle of the window, with the subtle background around it; without it the rows span the window. Every row then has at most the content width, and the check of the room counts with it.
- **Widths with the mouse:** the properties of a row show the widths on the desktop as a bar; drag the edge of an area or use the arrow keys on it (Page up/down: 4 rem). **Standard positions:** when a position where building blocks stand by default leaves the template, the designer names those blocks and asks first. **Empty positions** are marked with their name in the preview, not in the wiki.
- **Rights per template:** *Rights* in the list of templates names who may change a template and who may manage it (its rights, the trash, its own CSS): people, groups, roles or all signed-in people. Nobody named: every designer may. Only managers named: only they may change it too. These rights only narrow down: without `core.template.design` they give nothing; administrators always may; nobody can take away their own right to manage. Others can still see and copy the template (its design is public anyway). Changes of rights are in the audit.
- **Own fonts:** the tab *Own fonts* uploads a font as one or more files in the usual formats (WOFF2, WOFF, TTF, OTF), for example regular, bold and italic, or one variable font with all weights; weight and style are taken from the file names and can be changed. Name its license (SIL OFL 1.1, Apache 2.0, MIT or another one with a note on where the right comes from) and confirm that it may be used on this website; both are kept in the audit. The wiki serves the file unchanged from itself (`/api/v1/x/core.template/fonts/ID/files/N`), never from elsewhere; it is public only while a template outside the trash uses it. As the file is not changed (no conversion, no subset), the rule of the OFL on reserved font names for modified versions does not apply. A font used by a template (also in the trash) cannot be removed. A template exported with an own font needs the same font in the other wiki. Limits: `core.template.fonts`, `core.template.font_kb`, `core.template.font_upload_kb`.
- **Any font works:** the wiki ships Inter (text) and Poppins Bold (headings of the wiki template), served by the wiki itself, so the layout is the same on every computer. A template or design may choose any other font, wider or narrower: rows of buttons wrap, long words break inside narrow boxes, boxes with text grow with it instead of cutting it off. The browser tests check this on every change with a wide and a narrow font, text at 200 % and the text spacing of WCAG 1.4.12 (see the developer documentation, accessibility checks). A design with its own heading font keeps it.
- **Try a template first:** *Only for me* shows the wiki with a template only to you, in this browser; a bar at the top of every page says so and ends it, or with the permission for the settings uses it for everybody. Categories with a template of their own keep it. A category can also get a template first (category settings).
- **Versions and trash:** every save is a version (at most `core.template.versions`); a template in use cannot go to the trash. At most `core.template.count` templates.
- **Export and import** as a file (`format: ewiki-template`).

## External search engine

For very large amounts of data the full-text search can use **OpenSearch/Elasticsearch** (from 7.10 or OpenSearch 1.x) or **Meilisearch** (from 1.3): better relevance, tolerance of typos, relief for the database. Set it up under *Administration → Search engine*: choose the search engine, enter URL, index name and credentials, *Test connection*.

- PostgreSQL stays the reference. Every change (save, move, tags, trash, restore, delete) ends up in a change log through database triggers; a background job transfers it every 10 seconds. After a change of search engine, URL or index and with *Rebuild index* the index is created completely anew.
- The external engine is only used once the index is completely built. Until then, on errors of the search engine and with the plugin disabled, the built-in search answers automatically. The answer of the search API contains `engine` for this.
- For each document the search engine stores who may see it (users, groups, roles, signed-in people, guests; administrators see everything). Every search filters by it. So everyone finds their pages, even if far more pages they may not see match the search term. The wiki transfers a change of permissions to the index within seconds.
- Afterwards the wiki checks every hit once more itself with the current permissions, as well as trash and tags. An outdated or faulty index therefore never shows more than someone may see.
- The search engine holds page contents and the IDs of the people allowed. It does not need to be publicly reachable and should not be; protect it with credentials.
- The built-in search (PostgreSQL) also filters by the areas someone may see right in the query.
- The automatic tests run against the newest version of OpenSearch, Elasticsearch and Meilisearch (building the index, changes, permissions, tags, typos, engine failure). Older versions from the ones named above are not tested automatically; before production use please check with your version (*Test connection*, *Rebuild index*, compare the number of documents).
- For Elasticsearch choose the search engine *OpenSearch / Elasticsearch*.

## Manual installation

Copy the plugin directory (with `plugin.json`, Go files and optionally `web/`, `locales/`, `migrations/`) into the plugin folder – the running server builds it automatically (see above). Without a running server:

- Source installation: `go run ./tools/genplugins` and `./scripts/build.sh`, then restart the service.
- Full image: `docker compose exec wiki ewiki plugin build` (or simply `docker compose restart wiki`, then it is built on start).

## Security

Core plugins are full code with access to the database and the server. Only install core plugins from trusted sources and with a fixed version (`ref`), see [Origin of core plugins](#origin-of-core-plugins). Installing, updating and trusted sources are for instance administrators. Every installation (with origin, source, commit and confirmation), removal, enabling or disabling and every change of the trusted sources is logged.

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
| `core.blocks` | general | **Blocks as elements:** an element of the type *Block* lives in a category (its permissions, versions, drafts, translations), is written like a page (editor with all blocks) and is shown at positions of the template: in the category (optionally only on its page, with or without subcategories), on one page or on the whole site. It is never in the search or in lists of pages. On the page of the block, *Where this block is shown* lists all placements; *Place the block* adds one. **A block shown on the whole site** (level template, site or element type) can only be changed, moved or deleted with the permission *Edit blocks of the whole site* (`core.blocks.edit`), so nobody with permissions in one category silently changes content on every page. **A page as the footer:** a block with `::include{page=ID title=false}` at the position `footer`; columns with `:::grid`. Includes count towards the limit `core.content.includes_per_page` here too |
| `core.macros` | macros | table of contents, child pages, recently changed (with sources), news from several sources and RSS/Atom feeds, notice boxes, expandable section, status label, columns, gallery, include page, excerpt, anchor, page tree, search box, new page from a template, attachments. Code blocks are highlighted in the browser with highlight.js (BSD-3-Clause), loaded only for pages with code. **Formulas** (`:math{tex="…"}` or a code block ```` ```math ````) are set as MathML by the browser with Temml (MIT), loaded only for pages with formulas. The server writes only the TeX text; the page gets a newly built MathML tree from a fixed list of elements and attributes (no links, no styles, no HTML). No math font is shipped; the browser uses the math font of the system. *Include page* shows only what the reader may see and never content classified stricter than the including page; the limit `core.content.includes_per_page` counts includes over all levels |
| `core.layout` | macros | layout building blocks: section, hero, cards, button, spacing, grid, heading, divider, buttons, call to action, tabs, accordion, icon, icon box, icon list (inherits from `core.macros`). The icons are a free set (MIT) shipped with the program and served by the wiki itself, one file per icon |
| `macro.shell` | macro | output of a shell command, only after exact approval by a superadmin, off by default |
| `macro.http` | macro | result of an HTTP request, only addresses from the allow list |
| `core.comments` | general | comments and replies below pages |
| `core.share` | general | share pages and categories with people, groups and roles (does not change permissions) |
| `core.mentions` | general | @mentions in pages and comments with suggestions while typing |
| `core.reactions` | general | emoji reactions to pages, categories and comments; any emoji, block list under *Administration → Reactions*; notification of the author (e-mail off by default) |
| `core.favorites` | general | personal favourites (star on pages and categories), list as a block of the placement (default: sidebar; hidden there, only in the profile) and in the profile |
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
| `content.team` | element type | team page from groups with selectable details from the business card or the identity provider. Only someone who may list a group and its members adds it (the own groups with `core.groups.list_own` and `core.users.list_own`, every group with `core.groups.list_all` and `core.users.list_all`); groups that are there stay for everybody who edits the page. **Guests** see only name, job title, department and photo (by the photo rule of the person); contact details (e-mail, phone, mobile, office, about, groups, attributes) only if the page has *Show the contact details to guests too*. Only someone with the permission *Show contact details publicly* (`content.team.public`) sets it; without it a public page stays public as long as groups and fields stay the same. Attributes of the identity provider (`attr:…`) are chosen only by who manages users (`core.users.manage`). Also the blocks *Profile card* and *People of a group* for any page. They show only people the reader may list; guests see none. |
| `content.notes` | element type | shared notes: write together live directly in the page view, saved automatically (meetings, brainstorming, task lists) |
| `search.external` | general | OpenSearch/Elasticsearch or Meilisearch for the full-text search |
| `theme.wiki` | template | documentation layout with page tree; text in Inter, headings in Poppins Bold (the font of the logo) |
| `theme.cms` | template | website, classic (university, organisation): main navigation, transparent header above the start image, title banner, footer columns |
| `theme.cms.modern` | template | website, modern and neutral: inherits structure, positions and settings from `theme.cms`, own design (font Inter, glass header, left-aligned start image, rounded cards) |
| `theme.wiki.dark` | template (example) | inherits from `theme.wiki`, only changes the colours |
| `example.notice` | element type (example) | notice box, inherits from `core.html` |

Developing own plugins: [Developing plugins](../developer/plugins.md)
