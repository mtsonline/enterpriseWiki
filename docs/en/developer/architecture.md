# Architecture

```
cmd/ewiki/            entry point (server + CLI in one binary)
internal/
  app/                wiring: configuration, DB, services, plugins
  config/             config.yaml (config-first), .env, ${VAR} resolution, atomic writes
  db/                 pgx pool, migration runner, migrations/ (core schema)
  identity/           principal (caller) in the request context
  auth/               chain of sign-in methods, sessions, API keys, SSH signatures, users/groups/roles
  acl/                permission checks (snapshot in memory)
  content/            categories, elements, versions, translations, trash, rollback, search, tags
  i18n/               UI texts of core + plugins, translation status, translations from the UI
  storage/            content-addressed file storage, safe delivery, PDF/text extraction
  events/             cache invalidation (in-process or PostgreSQL LISTEN/NOTIFY)
  audit/              immutable change log
  kernel/             activation state of plugins, dependencies
  pluginmgr/          install/remove plugins, rebuild, restart
  server/             REST API, security middleware, delivery of the frontend
  cli/                commands (serve, setup, api, user, plugin, config, …)
pkg/sdk/              plugin API (Go)
plugins/              bundled plugins (one Go package per plugin + optional web/, locales/, migrations/)
  all/zz_generated.go imports all plugins (generated)
web/                  frontend (TypeScript, no framework, esbuild), dist/ is embedded
  src/sdk.ts          plugin API (frontend)
scripts/              build, install, update, SSH login
```

## Request flow

1. `server.wrap` sets security headers (CSP etc.) and limits the request size.
2. Authentication with a bearer token (API key `ewk_…` or SSH token) or a session cookie yields an `identity.Principal`. With cookie sign-in it checks the CSRF header for writing methods.
3. The handler calls a service (`content`, `auth`, …). **Every service checks the permission itself.** So the API, the CLI (`ewiki api`) and plugins cannot bypass it.
4. Errors are typed (`apperr`) and always returned as `{"error": {"code", "message"}}`.

## Data model (core)

| Table | Content |
|---|---|
| `categories` | stable `id`, `parent_id`, `path` (ltree of ancestor IDs, e.g. `1.7.42`), `position`, multilingual `titles`/`descriptions` (JSONB), `tags`, `default_element_id`, trash fields |
| `elements` | `category_id`, `type` (plugin element type), `original_lang`, title cache per language, `tags`, trash fields |
| `element_revisions` | immutable versions per element **and language** (`rev`). Translations store `source_rev` (which original version they are based on); this gives the status *current/outdated* |
| `search_docs` | full-text index (tsvector: title A, tags B, content C) per element and language |
| `acl_rules` | category, subject (user/group/role/guest/authenticated), permission bit mask, `inherit` |
| `users`, `groups`, `roles`, `user_groups`, `user_roles`, `group_roles` | identities (unique per `provider` + `subject`). Memberships carry their source (`local` or provider), so synchronisation does not touch local assignments |
| `sessions`, `api_keys`, `ssh_keys`, `auth_challenges` | sign-in (tokens only as SHA-256) |
| `audit_log` | every change with a before/after snapshot. Categories are also reset from it |
| `ui_translations` | UI translations maintained in the interface |
| `files` | uploaded files (SHA-256, size, type determined from the content, name) |
| `sso_states` | running SSO sign-ins (state, nonce, PKCE verifier, return target), usable once, valid for 10 minutes |
| `schema_migrations` | applied migrations per source (core or `plugin:<id>`) |
| `change_events` | changes whose follow-up work (notifications to watchers and mentioned people) is not finished yet; written with the change, removed after success, otherwise retried by the worker `changes` |

The outline number (1.2.3) is computed from the positions. The IDs, in contrast, stay stable when moving, so permissions, links and API keys never break.

## Versioning and "nothing gets lost"

- Saving, restoring and resetting **always add new versions**.
- Deleting means trash (`deleted_at`). Final deletion only happens by an admin action or the retention rule. The current version is never removed.
- Optimistic locking (`base_rev`) prevents overwriting parallel changes.

## Permission cache

`acl.Service` holds an immutable snapshot of all rules and category paths (`atomic.Pointer`). On changes it is discarded and reloaded at the next check. In cluster mode this happens through `events.Bus` (PostgreSQL `NOTIFY`) on all instances. Checks therefore cost microseconds and no database query.

## Frontend

- TypeScript without a framework. `core/dom.ts` creates elements through DOM APIs (XSS-safe), `core/router.ts` is a history router with focus management, `components/ui.ts` contains accessible building blocks (fields with label and error text, native `<dialog>`).
- The template determines the page layout and offers **positions** (`header-nav`, `header-tools`, `sidebar`, `footer`). Core and plugins register widgets there (search, language choice, page tree, menus).
- `web/build.mjs` finds all `plugins/*/web/index.ts` and bundles them. Only the plugins the server reports as active (`GET /api/v1/site`) are activated.
- All texts come from the API (`/api/v1/i18n/{lang}`), so every text can be translated.

## Configuration

`config.Store` holds two versions: the raw version (with `${VAR}` references, written back) and the resolved version (used). Updates validate, write atomically and reload. Listeners (e.g. the plugin kernel) react at once.
