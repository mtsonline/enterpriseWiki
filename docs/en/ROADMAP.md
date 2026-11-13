# Roadmap

The implementation happens in milestones. Every milestone runs and is tested by the CI.

## Milestone 1 – Core (done, v7.0)

- Plugin system (Go and frontend), enabling/disabling at runtime, installation from GitHub, Git or a folder, rebuild and restart from the interface, inheritance of plugins
- Categories (unlimited depth, stable IDs), elements, default page, moving, duplicating, sorting, trash
- Element type `core.html` (WYSIWYG/Markdown) and example `example.notice`
- Versioning of all content, restoring, resetting to a point in time (single or per level), audit log, retention rules
- Multilingual content (with translation status) and interface (translation editor with progress)
- Permissions for users, groups, roles, guests and signed-in people with inheritance
- Local sign-in (always first), sessions, API keys, SSH key login
- Full-text search, tags
- Menus (`core.menu`), template `theme.wiki` (+ example `theme.wiki.dark`)
- Admin area: users, groups, roles, plugins, settings (in `config.yaml`), languages, log, menus
- CLI with access to every API function, setup assistant, Docker, systemd installer, update script with backup and rollback
- Tests: unit, API integration (including a check that every route is used by the frontend), browser E2E with WCAG check (axe), GitHub Actions

## Milestone 2 – Enterprise connection and files (done, v7.1)

- `auth.ldap` (LDAP/Active Directory) with attribute and group lookup, test login in the admin area
- `auth.oidc` (OpenID Connect with PKCE: Keycloak, Entra ID, …) with claim and group mapping
- `auth.shib` (SAML 2.0 native service provider and header mode for Apache `mod_shib`)
- Uniform mapping of external groups to local groups and roles for all methods, synchronisation on every sign-in
- Element type `core.attachment` (PDF, office, images, video) with preview, versioning and full text from PDFs/text files. Content-addressed storage in the file system, clean-up of unused files
- Inserting images in the editor (with required alternative text)
- Two-factor sign-in (TOTP) for local accounts, reset by administrators
- Generic settings form for plugins (`pluginSettingsForm`) with masking of secrets

Open from this area: S3-compatible object storage as an alternative to the file system, preview of office documents (conversion), single logout.

## Milestone 3 – Processes, macros, CMS (done, v7.2)

- Element type `workflow.process`: start form (text, e-mail, date, number, choice, confirmation, file upload), steps with those responsible (starter, user, group, role), actions with optional or required note, end states with result. Every case stores a snapshot of the process, every step is logged. "My tasks" in the sidebar and under `/tasks`. Structured editor with templates (application, approval request)
- Macro system (plugin-based, `sdk.Host.RegisterMacro`): macros are rendered on every display with the permissions of the viewer, macro dialog in the editor (WYSIWYG and Markdown)
  - `core.macros`: table of contents, sub-pages, recently changed, notice boxes (info, note, tip, warning, success), expand section, status label, columns
  - `core.layout`: section (background, width), hero banner, card grid, button link, spacing
  - `macro.shell`: shell command; only exactly approved commands (SHA-256 of the command text), approval only by super administrators from `config.yaml` or the CLI, timeout, own process group, unprivileged user, limited output, off by default
  - `macro.http`: HTTP request as text, sanitised HTML or JSON (value, list, table); allow list, internal addresses blocked after DNS resolution, redirects checked, size and time limits, cache
- Security settings of the macros only in `config.yaml` (cannot be changed through the interface or API)
- `core.comments`: comments and answers below pages, editing and deleting with audit log, deleted comments stay as placeholders
- Template `theme.cms` (website layout with main navigation, off-canvas menu, footer columns), layout blocks run across the full width
- Update check through GitHub releases (core and plugins, also GitHub Enterprise), release notes, one-click update for plugins (interface, API, `ewiki plugin updates|update`)
- Frontend SDK: `registerWidget` (interactive parts in server-rendered content), `registerElementExtension` (areas below pages), plugin routes through `route`

Open from this area: deputies in workflows, automatic core update from the interface.

## Milestone 4 – E-mail, workflow steps, external search (done, v7.3)

- E-mail in the core: outbox in the database (transactional), background sending with retries, SMTP (STARTTLS, TLS, PLAIN/LOGIN), sendmail, log; without a mail server, messages are kept and sent later. Management under *Administration → E-mail* (settings, test message, outbox), setup through environment variables in the Docker setup
- Notifications: new workflow tasks, reminders, completion/cancellation of own cases, comments and answers; only to those entitled, can be switched off per user
- Workflow step kinds: task, approval (four eyes, number or all), condition (branching by form data), send e-mail (recipients, Cc, sender, reply address, subject, text with placeholders for form, applicant, editor etc.), webhook (signed, with retries and error path), waiting time (fixed or until a date from the form), end; reminders after X days; templates application, approval, onboarding
- Background jobs for core and plugins (`RegisterWorker`), coordinated in the cluster
- `search.external`: OpenSearch/Elasticsearch or Meilisearch with automatic synchronisation through a change log, rebuild, fallback to PostgreSQL, permission check in the wiki

Improvements after the first test (v7.3.1): *Edit category* and *Edit page* directly in the category view, menu dialog with positions of the active template, category selection and check of the entries, display of the template used on the plugin page, more positions in both templates, position preview, plugin `core.blocks` for text blocks at every position.

Open: notifications in the browser (without e-mail), summaries (digest), deputy rules, webhooks whose answer writes data back into the case.

## Milestone 5 – Reliable e-mail, watching, feeds, new element types, guest cache (done, v7.4)

- E-mail queue for large volumes: status per recipient, accepted only with `250`, temporary errors (4xx, network) retried without limit, permanent errors (5xx) and bounces per recipient with a report to the sender or the triggering person. Several instances send in parallel (lease, `SKIP LOCKED`), reused SMTP connections, `concurrency` and `rate_per_minute`. Bounce mailbox through IMAP with VERP and DSN evaluation. `mail.enabled` switches e-mail off completely. API and CLI: `ewiki mail status|list|show|retry|delete|bounces`, `ewiki jobs`.
- Watching categories (optionally with sub-categories) and pages: e-mail with link or new content, collected after 2 minutes of quiet, only content the person may see. API `me/watches`, CLI `ewiki watch`.
- Menus: targets category, element (any type), free link, other menu (multi-level) and heading. Selection of existing content or "Create new", title of the target as label, protection against loops. Shown as drop-down menus with flyouts, as a tree in the sidebar and on mobile, in both templates.
- Atom/RSS: feed per category (setting), for blog and FAQ; personal feed and calendar links with a renewable key. Search within a category (`/api/v1/search?category=`).
- Element types `content.blog`, `content.faq`, `content.events` (event calendar with series, registration, waiting list, reminders, iCal, macro "Upcoming events"), `content.team` with the new business card in the profile.
- Optional guest cache: API answers in memory, pages with embedded data (one request, no database). Every change clears the cache, through the database also in the cluster and from the command line. *Administration → Cache*, `ewiki cache`.

Open / suggestions:

- Server-side pre-rendering of the page content for guests: search engines and visitors without JavaScript then see the full text, not only the preloaded data.
- E-mail invitations (iTIP, `METHOD:REQUEST`) for events and VTIMEZONE blocks in the iCal export for older Outlook versions.
- Editing exceptions in series ("change only this event"), not only cancelling.
- Daily or weekly summary instead of immediate watch e-mails, selectable per watch.
- Webhooks for events (page changed, registration for an event) for Teams/Slack.

## Improvements v7.4.1

Administration → Elements (search, filter, edit, move, delete all elements); element selection with search across all categories (category only as a filter), filterable category selection; "New element" only asks for type and title, blog/FAQ create a category without a source; short URLs (`/calendar`) for elements and categories; selectable start page (overview, category, element) and element type `content.catlist` (category overview).

## Improvements v7.4.2

The menu editor only shows the list of entries, "Edit" opens a single entry (including the parent entry); the wiki template is now called `theme.wiki`/`theme.wiki.dark`, narrower sidebar that can be shown and hidden; website template `theme.cms` redesigned (transparent header above the hero image, title banner on sub-pages, footer columns, "Back to top" button, font Source Sans 3) with its own settings page *Administration → Website template*; settings forms know image and colour fields.

## Licensing (v7.5)

Business Source License 1.1 with free private use by natural persons, license keys with offline check (*Administration → License*, `ewiki license`), contributor license agreement (CLA) for contributors and catalogue plugins, license list of the contained components (`/licenses.txt`) and license check of new dependencies in the CI.

## Milestone 6 – Guests and search engines, calendar, summaries, webhooks (done, v7.5)

- **Pre-rendering for guests** (`ui.prerender`, on by default): pages, categories, start page and short URLs come with title, description, canonical address, Open Graph data, language and the content as HTML; the app replaces it on start. Always from the view of a guest, kept in the guest cache. Check under *Administration → Settings → Show guest view* or `GET /api/v1/prerender?path=`.
- **iCal**: VTIMEZONE definitions for all time zones of series (yearly rules, single dates for irregular zones), changed single events as `RECURRENCE-ID`.
- **Changing single events of a series** (title, location, description, time, moved up to 31 days) and resetting them; registrations stay, registered people receive an e-mail on a new time or location. API `PUT/DELETE …/events/{id}/occurrences/{date}`.
- **Watches as a daily or weekly summary** (`frequency`), time, weekday and time zone under *Administration → E-mail*; CLI `ewiki watch add … --frequency daily`.
- **Webhooks** for Microsoft Teams (Adaptive Card for workflows), Slack and JSON (HMAC-signed): page and category changes, registrations and deregistrations, changed events; filter by event and category, queue with retries, delivery log, test and "Send again". *Administration → Webhooks*, API `/api/v1/admin/webhooks`, plugins through `host.Webhooks.Emit`.
- Second website template `theme.cms.modern` (neutral, modern), the previous one is called "Website – classic (university, organisation)".

## Milestone 7 – Event invitations, more webhooks, preview images

- **Event invitations** (iTIP, RFC 5546): registration, move-up and change e-mails contain a calendar invitation (`METHOD:REQUEST`), deregistration and cancellation a cancellation (`METHOD:CANCEL`). Outlook, Google and Apple Calendar enter the event directly or update or remove it; every event of a series has its own UID. Can be switched off per calendar (*Do not attach calendar invitations*). If a series is moved or its location changed, people registered for future events receive an updated invitation.
- **Webhooks** for new comments (`comment.create`: author, start of text, answer yes/no) and processes (`workflow.started`, `workflow.completed` with result, `workflow.cancelled`); category filter as for pages. Process events are queued in the same transaction as the change.
- **Preview image per page** for link previews (`og:image`, `twitter:card`): upload under *Properties*, choose from the images of the page or enter an address; otherwise the first image of the page, otherwise the preview image of the website (`ui.preview_image`). API `PATCH /api/v1/elements/{id}` with `preview_image`.

## Milestone 8 – Plugins in the Docker images

- Two images: **Lite** (`latest`, `7.5`; only the program) and **Full** (`full`, `7.5-full`; additionally source code, Go, Node.js and all modules of the core for builds without internet). Both private for now.
- Full image: plugins from interface, CLI, API or the folder `./data/wiki/plugins` are built in the data folder (`bin/ewiki-<identifier>`) and survive updates and new containers; after an image update they are rebuilt automatically on start (maintenance page meanwhile, on errors start without the additional plugins).
- Installing, updating and removing build and restart automatically; plugins copied into the folder are built automatically and active after **Restart** (or automatically with `plugins.auto_restart_on_drop`).
- Cluster: one node builds (file lock), the others take over the result; restart one after another (database lease `cluster_lease`), the wiki stays reachable.
- Lite image: installing is disabled in the interface, with a notice about the Full image.

## Milestone 9 – Permissions, log, drafts, notifications

- **Granular permissions:** global permissions for roles (users, groups, roles, settings, plugins, e-mail, webhooks, license, system, log, translations, trash, element overview, assigning watches, global templates) and default permissions of all signed-in people; category permissions with templates **Reader**, **Category editor**, **Category manager**; global permission **Category administrator** (top level, structure and permissions of all categories, content only with view).
- **Plugins declare their permissions** in the manifest (`permissions`: key, scope, English label, presets, `implies`); undeclared permissions are rejected. Bundled: workflows (create/change own, workflow administrator with detailed logging and notification of the creator, manage cases), participant lists, comment moderation, menus, blocks, shell macros, external search.
- **Central permission management** under *Administration → Permissions* (direct and effective permissions per user, group, role with origin). Default: sub-categories inherit, new top-level categories only belong to their creator.
- **Workflow steps** are only carried out by the entered responsible people.
- **Drafts, preview, templates, publishing with options** (target category, point in time, notify watchers, extension point for plugins such as "as news" in M11).
- **Notifications in the wiki** (bell) and by e-mail, **sharing** (plugin `core.share`, does not change permissions), **@mentions** (plugin `core.mentions`), **assigned watches** (mandatory or optional).
- **Log:** IP, browser, channel; sign-ins, rejected access, optionally page views; selectable what is logged; forwarding to syslog, webhook, OpenTelemetry, file; search and filter page with export.
- **Translation progress** no longer counts the original language; plugins must ship `locales/en.json`.
- CLI: `ewiki perm`, `ewiki audit`, `ewiki draft`, `ewiki inbox`, `ewiki watch assign`.

**Changes in behaviour:** "Create" alone no longer allows sub-categories (*Manage* is needed for that). New top-level categories are no longer automatically visible to guests. Administrators only carry out workflow steps if they are responsible.

## Milestone 10 – Translations

- **Plural forms** according to CLDR (`key_one`, `key_other`, `key_few` …), chosen through `count`/`n` – in the frontend with `Intl.PluralRules`, in the backend with `golang.org/x/text`. Core and plugins are converted.
- **Permissions per language:** `core.i18n.translate.<code>` for every configured language; `core.i18n.manage` allows all languages and adding/removing.
- **Translation page:** progress per language and component, filter missing/outdated/translated here, search, pages, fields per plural form with example numbers, *Still correct*.
- **Outdated translations:** own translations remember the English text; if it changes (e.g. through a plugin update), they count as outdated.
- **Import/export:** JSON, XLIFF 2.0 and PO, per language and optionally per component (UI, API, `ewiki i18n`).
- **Category translations** as for pages: original language, status current/outdated/missing, progress, button *Translations* in the category, API `PUT /api/v1/categories/{id}/translations/{lang}`, CLI `ewiki category translations|translate`.
- Language names for new languages come from the browser (`Intl.DisplayNames`).

## Milestone 11 – Components

- **Reactions** (`core.reactions`): emojis on pages, categories and comments, free selection with a block list, who reacted, notification of the author (e-mail off by default), managing the reactions of others.
- **Readable addresses** `/wiki/<categories>/<page>` **per language** with history: renamed and moved pages and changed short URLs redirect permanently (301); the address bar shows the address in the language of the user. Can be switched off (`ui.readable_urls`), per page "Only this short URL".
- **"Page not found"** shows deleted pages (with trash link) and "Did you mean?" suggestions.
- **Favourites** (`core.favorites`): star, list at a position of the template and in the profile.
- **Page properties** (`core.properties`) with macro "Page properties report".
- **Carousel** (`core.carousel`): news through "As news" when publishing, own slides, external RSS/Atom feeds; accessible (pause, keyboard, reduced motion).
- **Mega menu** (`core.megamenu`, optional) as a display for menus; menu entries have descriptions.
- SDK: category and profile extensions, areas above the page content, reaction bars, menu displays; notification kinds with e-mail off by default.

## Planned

- **Milestone 12 – Multi-site (done, branch m18):** several sites in one instance with their own content and settings (host name, theme, storage, sign-in; editor defaults with the new editor in M15). Every site has its own PostgreSQL schema with all tables; users, groups, roles, sessions and license are in a shared schema. Permissions per site, sign-in separated per site (own providers and sessions, shared accounts), instance admins (sites, users, license) and site admins (only their site), plugins can be switched on and off per site. The license counts the users of the whole instance.
- **Milestone 13 – Media (done, branch m19):** media library in categories; categories can be invisible (freely manageable "folders", e.g. for media). Permissions of a medium: by default those of the category, additionally everyone who may see an element in which it is embedded sees it; when uploading, own permissions can be set that replace both (only permissions you have yourself). Inserting in the editor, "view only" (no download button, no direct link, context menu blocked) for users without download permission or if switched off when uploading. Watermark selectable when uploading: none; do not store the original, only with the watermark of the uploader; display with the watermark of the uploader, original for those entitled; burn in the name of the viewer when viewing; burn in the name of the viewer when viewing and downloading (user and date preselected, each deselectable, optional comment). Image editor (crop, scale, rotate, colours, blur) with version history, embeddings show the new version. Gallery macro. Registration of viewers (internal; office plugins from M14) with a default viewer in the settings.
- **Milestone 14 – Office (done, branches m14-office, m14b-office-compose; compose profiles collabora/onlyoffice with CI against real servers; plus demo content and resetting the site):** WOPI (Collabora, OnlyOffice, Microsoft 365) and OnlyOffice interface. Office plugins register as viewer/editor (edit, view read-only); the default viewer is chosen in the general settings (internal or one of the active office plugins), "Always open files in the external program" is only allowed for one. With download/edit permission the office program opens, otherwise read-only there (if enabled in the plugin) or in the browser.
- **Milestone 15 – New editor and live editing (done, branch m15-editor):** visual and Markdown mode always store Markdown (GitHub Markdown); what Markdown cannot do (e.g. font colours, merged cells), the visual editor stores as a macro in readable notation (`::: toc{levels=3}`); macros are editable in both modes. Existing HTML content is converted once. Yjs collaboration with cursors, notes plugin (live together). Demo content with all core plugins and macros. The previous simple editor stays as an alternative (default set by the admin, selectable per user). Plus: tag field in the editor, colours/fonts as directives from the template palette, macro plugins can register their own editor button, FAQ edit buttons, mega menu in the demo.
- **Milestone 15b – Identity and sign-in (brought forward, before real users):** users with several unique e-mail addresses (one user, one profile) and an optional unique user name; automatic linking of a new sign-in only with an e-mail confirmed by the provider and a provider marked as trustworthy by the admin, otherwise confirmation with the existing login; managing e-mails (profile, API, CLI, confirmation link); creating and mapping users in advance (admin, API, CLI, SCIM 2.0); JIT per provider instance; role/group mapping per provider instance to claims/attributes; sessions with idle timeout, "my sessions", OIDC back-channel/RP logout, SAML SLO; MFA as plugins (TOTP, e-mail, WebAuthn/FIDO2, passkeys) for all sign-in methods; required plugin kind in the manifest (element, macro, auth, mfa, storage, office, search, integration, template, general) and the admin area grouped by it; visibility of profile pictures (admin default or fixed, users within the limits).
- **Milestone 15c – Properties, sources, areas (in progress, branch m15c-properties):** properties for all element types (over 30 types, definitions global/per category, free properties, permissions per property, formulas, sequential numbers, history, tags as the standard property), property database and property report with 13 displays (done); flexible sources for all reading plugins and macros (carousel, calendar, news, blog, FAQ …) with collecting macros; top-level categories as areas (template per category, key and short address, sidebar, area management, features on/off, templates, archive, directory, members, request access, personal areas, classification, analytics, export/import).
- **Milestone 15h/15i – Source builder (done):** source kinds search and property filter; source builder with building blocks by click or drag & drop into the areas "Elements from", "Exclude" and "Must also match" (text form `-`/`+`), live preview of the number of hits with your own permissions; maximum and pages (from N entries) for all lists from sources, paging without reloading with its own link per page; upper limit `lists.max_items`.
- **Milestone 16a – Forms (done):** plugin for web forms (element type and macro): form editor with all usual fields, conditions, validation rules, computed fields; layout selectable as an accessible grid (recommended) or pixel-precise; uploads with restrictions, target category and checks (file type, office macros, PDF JavaScript, optionally ClamAV/ICAP); storage targets property database, storage, files, e-mail, workflow, webhook; display macro for submissions; protection of public forms; permissions as for the other plugins.
- **Secrets in the database (done):** selectable unencrypted (default), AES-256-GCM or hybrid post-quantum (ML-KEM-1024 + X25519) with a key file, optionally protected by a password from the environment (Argon2id); bound to their row; rewrapping through admin page/API; webhook secrets of workflows and forms outside the page.
- **Milestone 16b – Workflow 2 (part 1 done: triggers schedule, page changes with property change, events including chaining, signed incoming webhook; steps message, values/formulas, HTTP request, create page, properties, sub-workflow; deadlines with working days, warnings and escalation; messages per step; part 2a done: parallel branches (fixed or per value, k of n), take over/give back/pass on/question, deputies, step forms with field permissions; part 2b done: decision links in e-mails (optionally without sign-in), versions per case, test run, evaluation, retention (anonymise/delete); part 3a done: triggers for accounts and groups (from every source), steps permissions (also time-limited), group membership, move/archive page; part 3b done: create file (PDF, Word, Excel, CSV, JSON, text, Markdown), calendar entry; signatures: core with seal (Ed25519 + ML-DSA-65), passkey signature, signed decisions; S/MIME: signed e-mails with certificates of the organisation and personal certificates, expiry warnings, optionally holding after expiry, second seal for evidence with the certificate of the organisation; open: CSC/ID Austria, EU wallet, designer, BPMN):** any number of triggers (form, button, schedule, report/search, property, file, webhook, chaining …), automatic actions at every step and on completion (notifications, webhook, create element/entry/file, report, permissions, groups …), deadlines per case and step with warnings and escalation, notifications before/after every step with templates, parallel branches, deputies, step forms, versions, test run, evaluation.
- **Milestone 16 – Approval and lifecycle:** (plus new process steps: upload/check file with visibility for following steps, shell command, S/MIME signature, parallel approval k of n, conditions/branches, deadlines with reminder and escalation, webhook/HTTP, sub-processes, create document) permission "Suggest changes", reviewers with a freely selectable number of required approvals per category, scheduled publishing, annotations on text passages (can be marked as done); field "Responsible" (people/groups, default creator, inheritable from categories); lifecycle rules with a selectable trigger (not updated for X months or review date) and actions selectable per page/category (banner "outdated", notify those responsible, status "Review needed", move into the archive, remove from the search, into the trash after Y months).
- **Milestone 17 – Drawing, diagrams, maps:** Excalidraw plugin (element type "Drawing", macro for embedding, "Insert drawing" in the editor, editable later, SVG with alternative text, own permissions, only loaded when drawing), diagram macro, interactive maps.
- **Milestone 18 – Polls, feedback, calendar:** polls and scheduling with notifications and calendar entries, feedback plugin (ideas, comments, votes), personal calendar connection (Google and Microsoft 365 through sign-in, iCloud and Nextcloud through CalDAV) for immediate transfer respecting permissions.
- **Milestone 19 – Plugin management and sign-in:** category display: mark elements per category as "default", each with a position of the template (new position "content"); overview as its own building block with display types (navigation, list, cards, tiles, tree, table, accordion, tabs, A–Z, timeline, everything below each other). Settings per plugin with defaults of the developer; position of every plugin area selectable from all positions of the active template (default mostly "content"; if the position is missing after a template change, the default applies again); plugins with several areas (e.g. command bars of blog/FAQ) positionable individually. Sign-in plugins can be enabled several times (e.g. two OIDC providers). Sign-in with ID Austria (OIDC/SAML after registration as a service provider) and further eIDs through eIDAS; EU Digital Identity Wallet (OpenID4VP) as its own plugin. Principle for all plugins: templates for notifications, confirmations and downloads can be adapted as files in the source code and in the interface.
- **Milestone 20 – PDF export:** elements (pages, categories, blog …) as PDF, as its own plugin and as an option in "Share".
- **Milestone 21 – Newsletter:** plugin with command menu, folder list and main area (positionable individually); drafts, templates, scheduled sending with a warning without a checked version, visual or Markdown editor; recipients from users, groups, roles and external contacts (manageable, double opt-in, one-click unsubscribe); sending as your own account or as approved sender addresses; optional S/MIME signature; delivery status and bounces; open and click statistics with charts.
- **Milestone 22 – Elections:** election plugin with election period, election commission, nested ballots, eligible voters (users, groups, roles, import, certain sign-in methods such as ID Austria), secret voting (encrypted votes, no interim result, decryption only by the commission), exactly one vote per person, verifiable logs without breaking ballot secrecy, release and publication of results.
- **Later (order open, see planning document):** password protection for categories/elements/media; end-to-end encryption with a personal post-quantum key (ML-KEM hybrid, in the browser, recovery code to print); trust store (k of n or fixed chain, selectable per trust store); federation of instances (users of the other wiki selectable per federation as individually visible or only as a group, default: individually visible; content always queried live, not mirrored); search across plugin content (events, team); tamper-proof audit log with WORM export; integrations (Jira, video/telephony, cloud storage, AI with MCP server, CampusOnline, open data, social media); video/audio player; CSV/chart macro; whiteboard and voice channel; PWA; export/import of instances; import from other wikis (list of common systems with an overview of which features are covered and which are missing) and databases; template designer; university template.
- **Milestone 23 – Shop:** products with freely definable attributes and variants, cart and checkout, payment providers as plugins (invoice, SumUp, PayPal and others), invoices.
- Afterwards: **template "Wiki (intranet/university)"** and the tool for plugin developers (scaffold, lint, tests).
