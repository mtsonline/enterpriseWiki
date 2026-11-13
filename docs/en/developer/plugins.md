# Developing plugins

A plugin is a directory under `plugins/` with:

```
plugins/acme_kanban/
  plugin.json          manifest (required)
  plugin.go            Go package, registers itself in init() (required)
  migrations/0001_*.sql  own tables (optional)
  locales/en.json, de.json  UI texts (optional, en is the reference)
  web/index.ts         frontend part (optional), web/style.css
  *_test.go            tests (recommended)
```

Complete examples in the repository:

| Example | shows |
|---|---|
| `plugins/example_notice` | element type that inherits from `core.html`, own API route, translations, frontend editor that reuses the base editor |
| `plugins/theme_wiki_dark` | template that inherits from another template |
| `plugins/core_menu` | migrations, CRUD API with admin check, widgets at template positions, own admin page |
| `plugins/auth_local` | sign-in method |

## Manifest (`plugin.json`)

```json
{
  "id": "acme.kanban",
  "name": "Kanban board",
  "version": "1.0.0",
  "description": "Agile board as element type",
  "author": "ACME",
  "repo": "github.com/acme/ewiki-kanban",
  "kinds": ["element"],
  "extends": ["core.html"],
  "frontend": true
}
```

- `id`: unique, lower case, dots (`vendor.name`), at most 64 characters. The directory is usually named like the ID with `_` instead of `.`.
- `kinds` (**required**, one or more): `element` (element type), `macro`, `auth` (sign-in), `mfa` (sign-in factor), `signature` (signature method), `storage` (storage & cloud), `office`, `search`, `notification`, `workflow`, `menu`, `template`, `integration` (other systems), `general`. The administration groups the plugins by the first entry. A manifest without a kind or with an unknown one is neither installed nor loaded.
- `extends`: plugins this one builds on. A plugin is only active as long as all base plugins are active.
- `required`: cannot be disabled (only for core plugins).
- `frontend`: `true` if `web/index.ts` exists.

## Permissions (required)

Every plugin that allows or forbids something **declares its permissions in the manifest**. The core rejects undeclared keys (`HasPerm` returns `false` and writes an error to the log), so administrators can see and grant all permissions centrally (*Administration → Roles* and *→ Permissions*, category settings).

```json
"permissions": [
  { "key": "acme.kanban.create", "scope": "category", "label": "Create boards",
    "description": "Create boards and change or delete your own ones.", "presets": ["editor"] },
  { "key": "acme.kanban.admin", "scope": "category", "label": "Board administrator",
    "description": "Change and delete all boards of the category.", "presets": ["manager"],
    "implies": ["acme.kanban.create"] },
  { "key": "acme.kanban.export", "scope": "global", "label": "Export all boards", "presets": ["authenticated"] }
]
```

- `key`: starts with the plugin ID (`acme.kanban.…`), lower case, dots. A renamed key is a new permission – existing assignments are lost.
- `scope`: `category` – granted and inherited in category rules; `global` – assigned to roles (Administration → Roles).
- `label`, `description`: **English only** in the manifest. You deliver translations in the locale files as `perm.<key>` and `perm.<key>_help` (see *UI texts*). `en.json` must contain all keys other languages translate (the test `plugins/all` checks this).
- `presets`: which templates contain the permission. Category: `reader` ⊂ `editor` ⊂ `manager` (a permission in the editor preset is also in the manager preset). Global: `authenticated` = all signed-in users have it by default (changeable in the administration).
- `implies`: whoever has this permission also has the ones named (e.g. *admin* → *create*).
- `group` (optional): heading in the permission selection; the default is the plugin name.

The five basic permissions of a category stay in the core: `view`, `edit`, `create`, `delete` (for **elements**) and `manage` (the **category itself**: texts, settings, sub-categories, permissions). Category administrators (`core.categories.admin`) have `manage` everywhere, but only see content with `view`.

**Own or all:** a proven pattern is a permission to create, with which you only change and delete *your own* objects, and an admin permission for all objects. If an admin changes other people's objects, log the change in detail (`audit.Log(ctx, host.DB, action, entityType, id, before, after)`) and notify the creator (`host.Notify`). Model: `plugins/workflow_process` (`guard.go`).

Checking in the Go part:

```go
// global
if err := host.RequirePerm(r, "acme.kanban.export"); err != nil { return nil, err } // 403
if host.HasPerm(ctx, "acme.kanban.export") { … }
// in a category (named permission or basic permission "edit")
if !host.Can(ctx, categoryID, "acme.kanban.create") { return nil, apperr.Forbidden() }
// for another user (e.g. before a notification)
host.UserCan(ctx, uid, categoryID, acl.View)
// resolve users from users, groups and roles
ids, _ := host.Members(ctx, users, groups, roles)
```

Element types can additionally implement `content.Guard`: `CheckCreate(ctx, svc, categoryID)` is called before creating, `CheckChange(ctx, svc, el, oldBody, newBody)` before saving, moving, duplicating and deleting (`newBody == nil` for delete/move). `el.CreatedByID` is the creator.

In the frontend: `hasPerm('acme.kanban.export')` for global permissions; `el.permissions` / `category.permissions` contains the basic permissions and named permissions of the current category. The interface only hides – what counts is always the check in the backend.

Admin pages (`registerAdminPage`) state with `permission: 'acme.kanban.export'` who sees them; without it only administrators.

### Extension points in the frontend

| Function | What for |
|---|---|
| `registerElementExtension({ id, order, placement: 'top' \| 'bottom', render(el) })` | area below (or with `top` above) the content of a page, e.g. comments, properties |
| `registerCategoryExtension({ id, order, render(category) })` | area below the content of a category |
| `registerPageAction({ id, order, render(target) })` | button in the actions of a page or category (share, favourite) |
| `registerSlot({ id, position, render(position) })` | widget at a position of the template; `position: '*'` for all, `render` may return a promise – `null` leaves the position empty (important so empty positions stay hidden) |
| `registerProfileSection({ id, order, render() })` | section in "My profile" |
| `registerPublishOption({ id, render(ctx) })` | field in the dialog "Publish with options"; in the backend `host.Content.RegisterPublishOption(id, option)` with `Apply(ctx, svc, el, value)` |
| `registerReactions(provider)` / `reactionBar(type, id, title)` / `prefetchReactions(type, ids)` | show the reaction bar of a plugin (e.g. `core.reactions`) on own objects; without the plugin `reactionBar` returns `null` |
| `registerEditField({ id, order, render(ctx) })` | part in editing **every** element type (e.g. properties); the value goes along as `fields[id]` with saving, publishing and in drafts. In the backend `host.RegisterElementField(f)` with `content.ElementField` (`Check` with the permissions of the caller, `Store` in the transaction of the element); `content.ElementFieldAlways` is also checked without a value sent (defaults, computed values) |
| `registerCategorySettingsTab({ id, order, label, visible(c), render(panel, c) })` | own tab in the category settings |
| `sourcesEditor(label, value, { categoryId, kinds, extra, help })` → `{ el, get }` | list of any number of sources; without `kinds` all kinds resolved to elements (category ± sub-categories, tag, element type, single elements, search, property filter) plus `extra` (e.g. `['feed']`), with `kinds` exactly these (e.g. `['calendar','ical']`); value in text format `category:12+sub; tag:hr`. Macro parameters of type `custom:sources` or `custom:sources.feeds` use it automatically. `registerSourceKind({ kind, label, element, render })` adds own kinds. An own kind resolved to elements: in Go `init` `content.RegisterElementSourceKind(kind)`, in setup `host.Content.RegisterSourceResolver(kind, func(ctx, value, lang, limit) ([]int64, error))` (at most `limit` IDs = setting `lists.max_items`; the core checks the permissions), in the frontend `element: true`. Sources have a `Mode` (`""` = include, `content.ModeExclude` = `-`, `content.ModeRequire` = `+`); `content.WithDefault(list, catID)` adds the own category if nothing is included. In the backend `content.ParseSources` / `ParseSourcesKinds`, `svc.ResolveSources(ctx, list)` → `*SourceSet` and `ListOptions{Sources: set}` (permissions of the readers; own SQL through `content.SourceSQL(set, arg)`, only narrowing `svc.ResolveNarrowing` + `content.MatchSQL`). **Lists with pages:** append `content.ListParams(def)` to the macro parameters (`limit`, `paginate`, `per_page`), in render `pg := mc.Paging(def)`, load with `Limit: pg.Size(), Offset: pg.Offset()` and output `mc.Pager(pg, total)` – the core handles page changes without reloading; external feeds and calendars through `host.Feeds.Items(ctx, url, n)` or `host.Feeds.Calendar(ctx, url)` (cached, SSRF-protected) |
| `registerMacroParamControl(name, (value, ctx) => ({ el, get }))` | own input field for macro parameters of type `custom:<name>` (Go: `content.MacroParam{Type: "custom:<name>"}`); `ctx.params()` delivers the other parameters, `ctx.categoryId` the category. `getMacroParamControl(name)` uses a registered field in editors of element types too; `option_labels` labels choice values per language |
| `userPicker`, `groupPicker`, `subjectsEditor` | choose people/groups, list of those allowed (users, groups, roles, signed-in people, everyone) like `acl.Subject`; in the backend `acl.MatchesAny(subjects, principal)` |
| `registerMenuStyle({ id, label, itemFields, render(menu, helpers) })` | own view of horizontal menus (example `core.megamenu`); `itemFields: ['description']` shows description fields in the menu management |

In the backend `host.Content.Path(ctx, "element"|"category", id)` delivers the readable address (`/wiki/…`), `host.Content.Resolve(ctx, path)` resolves addresses, short URLs and former addresses.

### Notifications, publish options, page actions

- `notify.RegisterKind("acme.kanban.assigned", "acme.kanban.kind")` declares a kind of notification (users switch off its e-mail in the profile); `notify.RegisterKindOff(…)` for kinds whose e-mails must be switched on first (e.g. reactions). Sending: `host.Notify.Send(ctx, userIDs, notify.Message{Kind: …, EntityType: "element", EntityID: id, Link: "/e/12", Text: func(lang string) (string, string) { … }})`. The sender is never notified; check beforehand with `UserCan` whether the receiver may see the target.
- **Notifications must never get lost.** Two ways, depending on where the change is stored:
  - *The plugin stores itself* (comment, reaction, task …): notify in the same transaction – `host.Notify.SendQ(ctx, tx, userIDs, msg)` or `host.Mail.Enqueue(ctx, tx, …)`. Return errors, do not only log them: then either both are stored or nothing, and the caller learns about it. Load receivers in the transaction with `host.Auth.LoadPrincipalQ(ctx, tx, id)` (never take a second connection while holding one). Mentions in own text: `core_mentions.NotifyTx(ctx, tx, …)`.
  - *The plugin reacts to stored pages or categories:* `host.Content.OnChangeDurable("acme.kanban", func(ctx, event, entityType string, id int64) error { … })` instead of `OnChange`. The core writes every change into the queue `change_events` in the same transaction as the content, calls the hook after the commit and repeats it after an error or crash until it returns `nil` (after 1, 2, 4 … minutes, at most hourly; with the permissions of the account that made the change). The hook can therefore run **several times** for the same change and must cope with that – e.g. store "already notified" and the notification in one transaction. `OnChange` remains for everything that may get lost (caches, statistics). If a plugin stores data of an element outside the revisions (e.g. events of a calendar), it reports the change in its own transaction with `after, err := host.Content.TouchTx(ctx, tx, elementID)` and calls `after()` after the commit; `Touch` without a transaction is only meant for changes whose notification may get lost.
- `@mentions` in own texts: `core_mentions.Notify(ctx, "acme.card", id, categoryID, title, link, text)` (does nothing if the plugin is disabled).
- `registerPublishOption({ id, render })` adds a field to the dialog *Publish with options*; the value arrives in the backend through `content.RegisterPublishOption` (the example follows with the carousel in milestone 11).
- `registerPageAction({ id, order, render(target) })` adds a button to the actions of a page or category (example: `plugins/core_share`).

## Go part

```go
package acme_kanban

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

func (p *Plugin) Manifest() sdk.Manifest   { return sdk.ParseManifest(manifest) }
func (p *Plugin) Migrations() fs.FS        { s, _ := fs.Sub(migrations, "migrations"); return s }
func (p *Plugin) Locales() fs.FS           { s, _ := fs.Sub(locales, "locales"); return s }

func (p *Plugin) Setup(host *sdk.Host) error {
	host.RegisterElementType(&BoardType{})
	// -> GET /api/v1/x/acme.kanban/boards/{id}
	host.Handle("GET", "/boards/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		var cat int64
		if err := host.DB.QueryRow(r.Context(), `SELECT category_id FROM elements WHERE id=$1`, id).Scan(&cat); err != nil {
			return nil, apperr.NotFound("board")
		}
		if !host.ACL.Can(r.Context(), identity.From(r.Context()), cat, acl.View) {
			return nil, apperr.ErrForbidden
		}
		return map[string]any{"id": id}, nil
	})
	return nil
}
```

Important:

- `Setup` is called on start for **every** compiled plugin. Everything registered through the `Host` (routes, element types, sign-in methods) is automatically inactive as long as the plugin is disabled. Routes then answer with 404.
- **Always check permissions yourself**: `identity.From(ctx)` delivers the caller (nil = guest), `host.ACL.Can(...)` the category permissions, `sdk.RequireAdmin(r)` checks administrators. The services in `host.Content` already check permissions themselves.
- Handlers return `(value, error)`. The value is sent as JSON, `apperr` errors are sent with the matching status. With `h.NoContent{}` and `h.Created{V: x}` you control the status 204 and 201.
- Migrations are named `NNNN_name.sql` and run once per database in a transaction. Until the first release of the wiki, changes are written directly into the existing files (there are no old states); from then on a published migration is never changed, changes go into a new file (rules in `CLAUDE.md`). Prefix table names (`plugin_acme_…`).
- Settings: `host.Settings()` delivers the section `plugin_settings.<id>` from `config.yaml`.
- For changes to content write to the log with `audit.Log(...)`.

### Element types

```go
type ElementType interface {
	TypeID() string                                           // "acme.kanban"
	Labels() map[string]string                                // {"en": "Board", "de": "Board"}
	Normalize(body json.RawMessage) (json.RawMessage, error)  // check + sanitise before saving
	Render(ctx context.Context, rc content.RenderContext, body json.RawMessage) (string, error) // safe HTML
	Text(body json.RawMessage) string                         // plain text for the full-text search
	EmptyBody() json.RawMessage                               // content of a new element
}
```

The core takes care of versioning, translations, trash, permissions and search for every element type. `Render` must deliver safe HTML. Use `core_html.Policy()` or `core_html.ToHTML()` for user texts.

### Inheritance

A plugin builds on another one by **embedding** its type and only overriding what differs (`plugins/example_notice/plugin.go`):

```go
type Type struct{ core_html.Type }      // inherits Normalize, Text, ...

func (t *Type) TypeID() string { return "example.notice" }
func (t *Type) Render(ctx context.Context, rc content.RenderContext, raw json.RawMessage) (string, error) {
	// own rendering, uses core_html.ToHTML
}
```

Enter the base in `extends`.

### Macros

Macros are in the page content as `<div data-macro="name" data-params='{"key":"value"}'>content</div>` and are rendered on every display with the permissions of the viewer (inner macros first).

```go
type Macro interface {
	Name() string                      // "acme.weather"
	Labels() map[string]string         // name in the macro dialog
	Description() map[string]string
	Params() []content.MacroParam      // fields in the dialog: text, textarea, number, select, bool, url
	HasBody() bool                     // wraps content (boxes, sections, ...)
	Render(ctx context.Context, mc content.MacroContext) (string, error)
}
host.RegisterMacro(&Weather{})
```

Optional interfaces:

```go
func (Weather) Inline() bool    { return true } // in the text: :acme.weather[Vienna]{days=3}; the text is the parameter "text"
func (Weather) Toolbar() string { return "☀" }  // own button in the toolbar of the editor (symbol)
```

In Markdown a macro is written as a directive: `::name{a=b}` (without content), `:::name{a=b}` … `:::` (with content) or `:name[text]{a=b}` (in the text); it is rendered into the placeholder above.

`MacroContext` contains the `RenderContext` (element, category, language, viewer, `Service` for queries with their permissions), the parameters, the **already sanitised** content (`Body`) and the headings of the page (`Headings`). Everything else the macro outputs it must escape itself (`html.EscapeString`). Check links with `core_layout.SafeURL`. Errors appear as a notice on the page, not as an error page. The macro dialog in the editor appears automatically (`GET /api/v1/macros`).

### Files in own tables

If a plugin stores file hashes outside of element content, it registers the column so the clean-up does not delete the files: `host.Files.AddReference("plugin_acme_items", "data")`. Delivering with permission check: `host.Files.Get` and `host.Files.Serve` (see `plugins/workflow_process`).

### E-mail, background jobs, translations in the backend

- `host.Mail.Enqueue(ctx, tx, mail.Message{To, Cc, From, ReplyTo, Subject, Text, HTML, Source, Ref, Owner})` writes an e-mail into the outbox, best in the same transaction as the triggering change. Delivery per recipient, unlimited retries on temporary errors, bounces and the case "no mail server" are the business of the core. `Owner` decides who is informed on permanent errors: 0 = the signed-in person who triggers the message, -1 = nobody (e.g. for reminders). If e-mail is switched off (`host.Mail.Enabled()` = false), nothing is created. `host.Mail.Users(ctx, tx, ids)` delivers address, language and notification wishes (`u.Wants("tasks")`).
- `host.T(ctx, lang, "acme.mail.subject", map[string]string{"name": …})` translates texts from the locale files of the plugin (e.g. for e-mails in the language of the receiver).
- `host.RegisterWorker("sync", 30*time.Second, func(ctx) error)` registers a background job `<plugin-id>.sync`; it only runs as long as the plugin is active, in a cluster on one instance at a time. Still use `FOR UPDATE SKIP LOCKED`.
- `host.Webhooks.Emit(ctx, webhook.Event{Name: "acme.ticket_closed", CategoryID: cat, Object: webhook.Object{Type: "ticket", ID: id, Title: t, URL: "/e/12#ticket-5"}, Data: map[string]any{…}})` reports an event to the configured webhooks (the core handles queue, retries and the formats Teams/Slack/JSON). Own event names must be in `webhook.Events` so administrators can choose them; texts for Teams/Slack come from `webhook.text.<name>` (placeholders: `title`, `actor`, `site` and the keys from `Data`).
- `host.RegisterSearchBackend(b)` connects an external search engine (`content.SearchBackend`, see `plugins/search_external`). The table `search_changes` logs every change to the search index.

### Limits {#limits}

Technical limits of a plugin (sizes, counts, waiting times) are not fixed in the code but in the manifest. Administrators see and change them together with those of the core under **Administration → Expert settings**.

```go
func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "acme.forms", /* … */
		Limits: []limits.Def{{Key: "acme.forms.answer_kb", Group: "requests", Unit: "kb", Default: 256, Min: 16, Max: 16384,
			Label: "Size of a form answer", Help: "Largest answer a form accepts."}}}
}

func (p *Plugin) Setup(h *sdk.Host) error {
	// the route accepts bodies up to this limit (otherwise the general limit applies: 64 kB without sign-in, 1 MB with)
	h.BodyLimit("POST", "/forms/{id}/answers", "acme.forms.answer_kb")
	h.Handle("POST", "/forms/{id}/answers", p.answer)
	return nil
}

max := h.Limit("acme.forms.answer_kb") // current value, at any time
```

- The key starts with the plugin ID. Units: `kb`, `mb`, `bytes`, `count`, `seconds`, `per_second`, `days`.
- `Label` and `Help` are English; translations as UI texts `limit.<key>` and `limit.<key>.help`.
- For page content use the limit of the core: `h.BodyLimit("POST", "/…", limits.RequestContentKB)`.
- Stream uploads (`multipart`) and register them with `limits.RequestUpload`; then `storage.max_upload_mb` applies.

### Incoming messages from other systems and events

- `h.Signed("POST", "/hooks/{id}")` registers a route that other systems call (incoming webhooks): the caller there is **always anonymous** – sessions and API keys never count, a CSRF header is not needed. The handler checks the signature itself (`hmac.Equal`, timestamp against replays) and answers the same on every error (`403`). The size and rate limits for guests apply.
- `host.Webhooks.Listen(func(ctx, q, ev) error)` receives every event (`element.*`, `category.*`, `comment.create`, `form.submitted`, `workflow.*`, `user.*`, `group.*` …) **in the transaction of the change** (`q`), before webhooks are queued. Whoever writes into `q` should take a savepoint (`q.(pgx.Tx).Begin`), so an own error does not prevent the change. Expensive work belongs in a queue and a worker.

### Secrets

Store secrets in own tables with `host.Secrets.Seal(value, secrets.AAD("table", key))`, read them with `Open` (unchanged with `secrets.mode: plain`) and register the column with `secrets.Register(secrets.Column{…})`, so "rewrap" finds it. Never store secrets in the content of an element type in the body: `Prepare` stores them with `secrets.PutRef` and only writes the reference into the body, `SecretRefs(body)` names the references (they are bound to the element when saving), reading is done with `secrets.GetRef(ctx, db, host.Secrets, elementID, ref)`.

### Permissions in plugin routes

`host.Content.ElementCategory(ctx, elementID, acl.View)` delivers the category of an element and checks the permission (404 instead of 403 if the element is not visible). `host.Content.Require(ctx, categoryID, perm)` checks a permission on a category.

### Listing content, feeds, own data of an element

- `host.Content.ElementBody(ctx, id, lang)` delivers type, stored content and category of an element for everyone who may see it. This is how element types read their settings in own API routes.
- `host.Content.Scope(ctx, categoryID, recursive)` delivers the category and (with `recursive`) the visible sub-categories. `host.Content.ListIn(ctx, content.ListOptions{Categories, Lang, Order, Tag, Limit, Offset, WithContent, Exclude})` lists their elements with title, teaser, author and optionally rendered content. `host.Content.SearchIn(ctx, q, tags, lang, limit, scope)` only searches in these categories.
- The package `internal/feed` writes Atom and RSS (`feed.Write` with ETag and 304) and builds entries from lists (`feed.FromListing`). Personal feed links: `host.Auth.FeedContext(r)` evaluates `?token=` (only use it for feed and calendar routes).
- `pkg/catlist` bundles what category-based element types need (configuration with category and introduction, permissions in the area, routes for creating entries and sub-categories, feed). Examples are `plugins/content_blog` and `plugins/content_faq`.
- An element type can implement `content.Preparer` (`Prepare(ctx, svc, categoryID, lang, title, body)`) to complete content before the check with the permissions of the caller. Blog and FAQ create the source category this way if none was chosen (`catlist.PrepareSource`).
- If an element type stores data outside of its versions (e.g. events of a calendar), it reports changes with `host.Content.Touch(ctx, elementID)`. Then watchers are informed and the guest cache is cleared. Changes through the API (POST/PUT/DELETE) clear the cache anyway.

### Sign-in methods

```go
type PasswordProvider interface {
	ID() string    // e.g. "ldap" -> identities are called name@ldap
	Name() string
	Authenticate(ctx context.Context, username, password string) (*auth.ExternalIdentity, error)
}
host.RegisterAuthProvider(&MyProvider{})
```

For browser methods (OAuth, SAML, …) implement `auth.RedirectProvider` (`Start`, `Callback`) and register it with `host.RegisterRedirectProvider`. The core takes care of state, nonce and PKCE verifier (stored on the server), single use, the session and the return. With `Configured() bool` you hide an unconfigured method on the sign-in page. You map groups and roles with `auth.MappingFromSettings(settings).Apply(identity)` (see `plugins/auth_oidc`).

**Several instances:** if a method should be settable several times, build a set of providers with `auth.NewInstanceSet(base, host.Settings, mk)`, one per entry in `instances` plus the base provider, and register it with `sdk.RegisterAuthInstances(host, set)` or `sdk.RegisterRedirectInstances(host, set)`. `mk(id, settings)` gets the provider ID (`base` or `base-<id>`) and a function that always delivers the current settings of this instance. `ID()` must return this ID and set `ExternalIdentity.Provider` to it. With `Settings()` (interface `auth.SettingsProvider`) the instance is available in the preview. The admin page builds `renderAuthProviderPage(el, {pluginId, base, title, help, fields, extra})` from the frontend SDK: choosing, creating and removing instances, settings form and preview.

**Sign-in factors (MFA plugins, kind `mfa`):** implement `auth.MFAMethod` (`ID`, `DefaultFactor`, `Enrolled`, `Challenge`, `Verify`) and register it with `host.RegisterMFAMethod`. The administration sets the factor number (plugin setting `factor`, suggestion `DefaultFactor`); the core asks the chain and keeps the state from `Challenge` to `Verify` on the server. Optional interfaces: `auth.MFAEnroller` (`BeginEnroll`/`FinishEnroll`: setting up in the profile and during a sign-in that needs more factors), `auth.MFASilent` (`Passes`: without a prompt, e.g. network), `auth.MFAResetter` (reset by administrators), `auth.MFABulk` (`EnrolledAmong` for lists), `auth.PasswordlessMethod` (may be factor 1 and identifies the person itself) and `auth.AccessGuard` (`host.RegisterAccessGuard`: checks every sign-in and request). Own endpoints for changing factors call `auth.RequireRecentSignIn`. In the frontend `registerMFAMethodUI({id, label, render, enroll, profile})` adds the step of the sign-in, the setup and the part in *My profile → Sign-in factors*; `mfaCodeForm` and `enrollFactor` help with this, `registerUserAdminField` completes the dialog "Edit user" (examples: `plugins/mfa_totp`, `mfa_email`, `mfa_network`, `mfa_webauthn`). `webauthnCreate`/`webauthnGet` take care of converting the WebAuthn options.

**Signature methods (plugins, kind `signature`):** implement `signature.Method` (`ID` = plugin ID, `Level` `advanced`/`qualified`, `Label`, `Available`, `Begin` → challenge `webauthn` or `redirect`, `Finish` → evidence as JSON) and register it with `host.RegisterSignatureMethod`; `signature.RegisterVerifier` (from `init`) checks the evidence offline, also in `ewiki signature verify`. Whoever has something signed calls `host.Signatures.Begin` with purpose, reference, requirement and the record (the core makes canonical JSON and SHA-256 of it) and finishes in its own transaction with `Complete` – its check function builds the record again and compares the hash. `RegisterViewer(purpose, …)` decides who sees evidence. Factors with keys (`mfa.webauthn`) offer `auth.KeySigner` for this. In the frontend: `signatureMethods`, `signChallenge`, `openSignature` (example: `plugins/signature_passkey`, use: `workflow.process`).

**Signing e-mails and second seal:** `host.Mail.SetSigner(x)` sets a `mail.Signer` (`SignDetached(ctx, from, msgID, content)` → detached CMS in DER or `nil` for unsigned; an error keeps the e-mail in the outbox, `mail.ErrHold` for holding on purpose). The core builds the MIME frame (`multipart/signed`). `host.Signatures.SetCMSSealer(f)` additionally seals signature evidence; `signature.RegisterCMSVerifier` (in `init`) checks the seal offline too. Example: `plugins/signature_smime`.

**Single logout:** a browser method can implement `auth.LogoutProvider` (`LogoutURL`: sign out at the provider) and `auth.LogoutHandler` (`HandleLogout`: messages of the provider to `/api/v1/auth/sso/<id>/logout`). The session stores `ExternalIdentity.SessionID` and `LogoutHint` for this. Message IDs that may only count once are checked by `host.Auth.ReplayCache(prefix).Consume` across all instances of the server.

Return `auth.ErrUnknownUser` if the user is unknown (then the next method is checked), and `auth.ErrBadCredentials` for a wrong password. `ExternalIdentity.Groups` is synchronised as group membership on every sign-in.

## Frontend part (`web/index.ts`)

```ts
import { api, h, registerEditor, registerSlot, registerAdminPage, registerTemplate, getEditor, t } from '@ewiki/sdk';
import './style.css';

export function register(): void {
  // editor for the own element type
  registerEditor('acme.kanban', (container, body, ctx) => {
    const ta = h('textarea', { class: 'ew-input', 'aria-labelledby': ctx.labelId });
    ta.value = JSON.stringify(body);
    container.append(ta);
    return { getBody: () => JSON.parse(ta.value) };
  });

  // widget at a template position
  registerSlot({ id: 'acme.kanban.widget', position: 'sidebar', order: 30,
    render: async () => h('p', null, t('acme.kanban.hello')) });

  // page in the admin area (/admin/kanban)
  registerAdminPage({ id: 'kanban', label: () => t('acme.kanban.admin'),
    render: async (el) => { el.append(h('h1', null, t('acme.kanban.admin'))); } });
}
```

- Settings pages: `pluginSettingsForm('acme.kanban', [{ key: 'url', label: t('…') }, …])` creates an accessible form that stores into `config.yaml` (types `text`, `secret`, `bool`, `number`, `list`, `map`, `select`, `textarea`, `image` – upload, the SHA-256 hash is stored, delivered under `/api/v1/files/{hash}` –, `color` – `#rrggbb`). `MAPPING_FIELDS()` adds the group and role mapping.
- **Discard outdated answers:** if you start a request on every input (search, person search), protect the display with `newest()`: create `const fresh = newest();` once, call `const current = fresh();` in the load function and `if (!current()) return;` after the `await`. Otherwise an older, slower answer can overwrite the result of a newer one.
- Uploading files: `uploadFile(file)` delivers `{hash, name, mime, size}`.
- Choosing categories and elements with "Create new": `categorySelect({ selected, perm, noneLabel, allowCreate, onChange })` (`.field(label)` delivers the labelled field), `elementSelect({ selected, allowCreate, type, onChange })` (search across all categories), `newCategory(parent, done)`, `newElement(category, done)`.
- `watchButton(type, id, title)` (watch), `feedButton(apiPath, title)` (feed dialog with personal link) and `feedURL(path, format)` for own feeds or subscriptions.
- The SDK (`web/src/sdk.ts`) provides DOM helpers (`h`), API client (`api` with CSRF header), translations (`t`, `pick`), accessible building blocks (`field`, `textInput`, `select`, `checkbox`, `button`, `formDialog`, `confirmDialog`, `openDialog`), `toast`/`announce` and `navigate`.
- Create DOM with `h()`. Never use `innerHTML` for user data; use `trustedHTML()` only for HTML the server has rendered and sanitised.
- **Accessibility:** every input field needs a label (`field()`), icon buttons an `ariaLabel`, dialogs go through `formDialog`/`openDialog`. The CI checks all pages with axe-core for WCAG 2.2 AA.
- Editors of other types can be reused and extended with `getEditor('core.html')` (see `example_notice/web/index.ts`).
- **Widgets:** an element type can output a placeholder `<div data-ew-widget="acme.board" data-element="42"></div>` in the HTML rendered on the server. `registerWidget('acme.board', async (el) => { … })` makes it interactive (see `workflow_process/web`).
- **Areas below pages:** `registerElementExtension({ id, order, render: async (element) => node })`, e.g. comments.
- **Own pages:** `route('/tasks', ({ main }) => …)` registers a route in the frontend.
- The frontend does not depend on a framework and has no runtime dependencies. Keep plugins just as lean.

### Templates

```ts
registerTemplate({
  id: 'acme.theme',
  extends: 'theme.wiki',      // inherit the layout ...
  className: 'acme-theme',          // ... and only adapt it with CSS
});
```

An own layout delivers `layout: (ctx) => ({ root, main })` and places positions with `ctx.position('sidebar')`. If possible use the usual names (`top-bar`, `header-nav`, `header-tools`, `sidebar`, `sidebar-bottom`, `content-top`, `content-bottom`, `footer`, `footer-1` … `footer-4`, `hero`), so menus and blocks stay in a fitting place when the template changes; readable names come from the translations `position.<name>`, the template name from `template.<id>`.

Widgets can appear at a fixed position (`registerSlot({ position: 'sidebar', … })`) or at all positions (`position: '*'`, `render(position)` gets the name, see `core_menu` and `core_blocks`). For selection lists `positionOptions()` delivers the positions of the active template, `activeTemplate()` the template of the current page (categories can have their own; on a change the shell rebuilds the frame and moves the content, positions are filled again – so always scope template CSS with the own `className` on `body`). Required: skip link to the content, landmarks (`header`, `nav`/`aside` with `aria-label`, `main#main[tabindex=-1]`, `footer`). Set colours with the CSS variables `--ew-*` (contrast ≥ 4.5:1).

## UI texts

`locales/en.json` (reference) and further languages contain flat keys. Prefix them with the plugin ID:

```json
{ "acme.kanban.hello": "Hello {name}" }
```

In the code: `t('acme.kanban.hello', { name })`.

**Plural forms:** texts with a number get one key per CLDR plural form with the ending `_zero`, `_one`, `_two`, `_few`, `_many` or `_other` (`_other` is required). English and German need `_one` and `_other`, other languages add their forms (Polish `_few`, `_many`). The parameter `count` (or `n`) chooses the form – in the frontend through `Intl.PluralRules`, in the backend through the CLDR rules of `golang.org/x/text`:

```json
{ "acme.kanban.cards_one": "{n} card", "acme.kanban.cards_other": "{n} cards" }
```

```ts
t('acme.kanban.cards', { n: list.length })          // "1 card", "3 cards"
```

```go
host.T(ctx, lang, "acme.kanban.cards", map[string]string{"n": strconv.Itoa(n)})
```

Do not use these endings for other purposes (`…_other` would be read as a plural form). For ordinal numbers such as "step {n}" a simple key is enough.

**`en.json` is required** as soon as a plugin brings locale files (otherwise it does not start); further languages may only contain keys that exist in `en.json`. The core and the bundled plugins deliver English and German. Missing translations appear in the admin area with a progress display (also per plugin) and can be completed there or exported as JSON, XLIFF 2.0 or PO and imported again. If you change an English text in an update, the translations of this text made in the administration are marked as *outdated*.

## Building, testing, publishing

```bash
go run ./tools/genplugins     # add the plugin to plugins/all/zz_generated.go
./scripts/build.sh
go test ./plugins/acme_kanban/
EWIKI_TEST_DATABASE_URL=… go test ./internal/server/   # checks among other things that every API route is used in the frontend
```

Publishing: a Git repository whose root is the plugin directory (`plugin.json` in the root), with version tags (`v1.0.0`). Installation: `ewiki plugin install github.com/acme/ewiki-kanban --ref v1.0.0` or through the catalogue.

The Go package of the plugin belongs to the module `github.com/mtsonline/enterprisewiki`. Imports from `internal/…` and `pkg/sdk` are therefore allowed. Prefer `pkg/sdk` and the services referenced there, because that is the stable interface.

### Properties in own plugins

`core_properties` provides `Query`/`Run` (filter, sorting, groups with the permissions of the caller), `ParseSources`, `ParseFilters`, `QueryOf(params)` and `RenderTable(ctx, query)` (example: `plugins/content_propdb`). Through the API: `POST /api/v1/x/core.properties/query` (rows or groups), `…/report` (parameters of the macro), `…/query.csv`, `GET/PUT …/elements/{id}` (values: `{"values": {…}, "free": {key: {type, label}}}`), `…/defs` (definitions), `…/history/{id}`.
