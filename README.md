# Enterprise Wiki

**English** · [Deutsch](README.de.md)

A secure and flexible alternative to traditional enterprise wikis and CMS systems, self-hosted and modular: knowledge base with categories nested to any depth, versioned and multilingual pages, fine-grained permissions and a plugin system through which practically everything can be extended (element types, sign-in methods, menus, templates, admin pages, API endpoints).

- **Backend:** Go, a single statically linked binary (server + CLI), PostgreSQL
- **Frontend:** TypeScript without a framework and **without runtime dependencies**, embedded in the binary
- **Accessible:** target WCAG 2.2 AA, checked automatically with axe-core in the CI
- **Config-first:** without a cluster, `config.yaml` is the authoritative source; the admin interface and the CLI write into this file. In cluster mode, the settings that can be changed in the interface are stored in the database.

> Status: **development version 7.5.0-dev.** What is already included and what follows is in the [roadmap](docs/en/ROADMAP.md).

## Features

| Area | Included |
|---|---|
| Content | Categories with unlimited depth (stable IDs, outline numbers 1.2.3), default page per category, elements as plugin types, move, duplicate, sort, trash |
| Pages | Element type `core.html`: WYSIWYG editor or Markdown, images with required alternative text, server-side HTML sanitisation (XSS protection), preview |
| Files | Element type `core.attachment`: PDF, office, images, video. Preview in the browser, every new file is a version, PDF and text content in the full-text search, content-addressed storage (SHA-256) |
| Versioning | Every change is an immutable version, restoring single versions, resetting whole categories (with/without sub-categories) to a point in time, audit log, configurable retention |
| Multilingual | Content versioned per language, translation status (current / outdated / missing) with percentage, fallback to the original language; UI texts of core and plugins translatable in the admin area with progress display |
| Permissions | Rules per category for users, groups, roles, guests and signed-in users, optionally inherited. Without a rule nothing is allowed, guests only see the first level. Checked in the backend on every request, cached in memory |
| Sign-in | Local accounts (Argon2id, lockout after failed attempts, multi-factor as plugins: app, e-mail code, security keys/passkeys, allowed networks, freely selectable chain per sign-in method) are always checked first. **LDAP/Active Directory**, **OpenID Connect** (Keycloak, Entra ID, …) and **Shibboleth/SAML 2.0** (native or through mod_shib) as plugins, with uniform mapping of external groups to local groups and roles. API keys (expiry date, permission mask, category restriction), SSH key login as on GitHub |
| Processes | Element type `workflow.process` for processes such as applications or approvals: start form with file upload, tasks, approvals (four-eyes principle), conditions, automatic e-mails with placeholders, webhooks, waiting times, reminders, complete history, "My tasks" |
| E-mail | Outbox with retries, SMTP (STARTTLS/TLS), sendmail, notifications about tasks, cases and comments (can be switched off in the profile), watching pages and categories – immediately or as a daily/weekly summary |
| Webhooks | Microsoft Teams, Slack and signed JSON on page changes, registrations and changed events, with queue, retries and delivery log |
| Guests and search engines | Pre-rendered pages with title, description, canonical address and Open Graph data, optional guest cache, short URLs |
| Properties | Structured properties for elements of every type (`core.properties`): over 30 types from text, number, amount of money and date to status, person, location, sequential number and formula; definitions global or per category (inherited), free properties, permissions per property, history; tags as the standard property. Property database (`content.propdb`) and macro "Property report": table with totals, board, calendar, timeline, charts, key figures, pivot, completeness, due dates – always with the permissions of the readers |
| Macros | Table of contents, sub-pages, recently changed, notice boxes, expand section, status label, columns, layout blocks (section, hero, cards, button), **shell command** (only exactly approved by the super administrator) and **HTTP request** (allow list, internal addresses blocked), own macros through plugins |
| Collaboration | Comments and answers below pages (`core.comments`), changes in the audit log |
| Search | Full-text search across all languages with highlighting, tags on categories and pages, tag cloud; optionally OpenSearch/Elasticsearch or Meilisearch (`search.external`) for very large wikis |
| Plugins | Compiled with the core, can be enabled and disabled immediately, installation from GitHub, Git or a local folder through interface, API or CLI, rebuild and restart from the interface, inheritance (`extends`), update check through GitHub releases with one-click update |
| Bundled | `auth.local`, `auth.ldap`, `auth.oidc`, `auth.shib`, `core.html`, `core.attachment`, `core.menu` (menus at template positions), `core.macros`, `core.layout`, `macro.shell`, `macro.http`, `core.comments`, `workflow.process`, `search.external`, the templates `theme.wiki` (documentation), `theme.cms` (website, classic) and `theme.cms.modern` (website, modern), examples: `theme.wiki.dark` (inherits from a template), `example.notice` (inherits from an element type) |
| Operations | Setup assistant (interactive or headless), Docker/Compose, systemd installation, safe update script with backup and automatic rollback, single and cluster mode |
| CLI | Every API function through `ewiki api`, plus commands for users, plugins and configuration |
| Tests | Unit tests, API integration test against PostgreSQL (also checks that every API route is used by the frontend), browser E2E tests with WCAG check, all in GitHub Actions |

## Quick start with Docker

```bash
./docker-setup.sh           # .env with passwords, folder ./data, own user instead of root
docker compose up -d        # loads the current image (ghcr.io/mtsonline/enterprisewiki)
# http://localhost:8080 – sign in with admin / EWIKI_ADMIN_PASSWORD
```

## Installation on a server (systemd)

```bash
git clone https://github.com/mtsonline/enterpriseWiki.git && cd enterpriseWiki
sudo ./scripts/install.sh            # builds, asks for database and admin password, sets up the service
sudo ./scripts/update.sh --enable-timer   # optional: automatic updates
```

Details: [docs/en/admin/installation.md](docs/en/admin/installation.md)

## Development

Requirements: Go ≥ 1.27, Node.js ≥ 22 (recommended: current LTS, currently 26), PostgreSQL ≥ 16 (recommended and tested in the CI: 18).

```bash
./scripts/build.sh                                   # frontend + backend -> bin/ewiki
EWIKI_DATABASE_URL=postgres://user:pass@localhost/wiki ./bin/ewiki setup
./bin/ewiki serve
(cd web && npm run watch)                            # rebuild the frontend on changes
EWIKI_TEST_DATABASE_URL=postgres://…/wiki_test go test ./...   # deletes the test database!
```

## Documentation

| For | Document |
|---|---|
| Users | [User guide](docs/en/user/guide.md) |
| Administration | [Installation](docs/en/admin/installation.md) · [Configuration](docs/en/admin/configuration.md) · [Sign-in: LDAP, OIDC, SAML, 2FA](docs/en/admin/authentication.md) · [Operations, updates, backup, CLI](docs/en/admin/operations.md) · [Security & permissions](docs/en/admin/security.md) · [Managing plugins](docs/en/admin/plugins.md) · [Office programs](docs/en/admin/office.md) |
| Development | [Architecture](docs/en/developer/architecture.md) · [Developing plugins](docs/en/developer/plugins.md) · [REST API](docs/en/developer/api.md) |
| Planning | [Roadmap](docs/en/ROADMAP.md) |

## License

Enterprise Wiki is licensed under the **Business Source License 1.1** ([LICENSE](LICENSE)): free of charge for private use
by natural persons, for companies, public bodies, educational institutions and associations with a
(discounted) license. Every version additionally becomes free under the AGPL-3.0 four years after its release.
Details, license keys and contained components: [LICENSING.md](LICENSING.md). Contributing: [CONTRIBUTING.md](CONTRIBUTING.md), [CLA.md](CLA.md).
