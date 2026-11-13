# Security, sign-in and permissions

## Sign-in

### Order of the sign-in methods
Sign-in methods are plugins. At sign-in they are checked in the order of `auth.order`. **Local accounts (`auth.local`) are always checked first** and cannot be disabled. So a local administrator gets into the system even when LDAP, OIDC or Shibboleth fail.

LDAP, OpenID Connect and Shibboleth/SAML and the mapping of external groups to local groups and roles are described in [authentication.md](authentication.md).

Every identity is stored internally and uniquely as `name@provider` (`admin@local`, `max@ldap`). Accounts of the same name from different providers do not collide. With `name@provider` in the sign-in field you choose a method specifically.

### Local accounts
- Passwords are hashed with **Argon2id** (19 MiB, t=2, p=1, OWASP recommendation).
- Password rule: minimum length `auth.min_password_length`, at least three of four character classes.
- After `auth.max_login_failures` failed attempts the account is locked for `auth.lockout_duration`. There is also a rate limit for **failed** attempts per IP (30 in 5 minutes) and per user name (20) – successful sign-ins do not count, so offices behind one address are not locked out. User names over 255 characters and passwords over 1024 characters are not processed at all.
- Response times do not reveal whether an account exists.
- Several factors as plugins (authenticator app, code by e-mail, security keys/passkeys, allowed networks) in a freely selectable chain per sign-in method, see [authentication.md](authentication.md#several-factors-the-factor-chain).
- If an administrator resets a password or disables an account, all sessions of this account end.
- The system prevents the last active administrator from being deleted, disabled or demoted.

### Browser sessions
- Random 256-bit token in the cookie `ewiki_session` (`HttpOnly`, `SameSite=Lax`, with `secure_cookies: true` also `Secure`). Only the SHA-256 hash is stored in the database.
- CSRF protection: state-changing API requests without an API key – with cookie sign-in, but also the sign-in itself and requests of guests – must send the header `X-Requested-With: ewiki` (this also prevents "login CSRF", where a foreign page signs visitors into an account of the attacker). Browsers only send own headers of foreign pages after a CORS approval, and the wiki allows none.

### API keys
Under *My profile → API keys*:
- limited validity (at most `auth.api_key_max_ttl`), revocable at any time,
- permission mask (view, edit, create, delete, manage permissions). A key **never has more permissions than its owner**; the mask is combined with the current permissions on every request,
- optionally restricted to a category with its sub-categories,
- administration functions only with "Allow administration" set explicitly.

Usage: `Authorization: Bearer ewk_…`

### SSH keys (as with GitHub)
Store your public key under *My profile → SSH keys* (ed25519, ECDSA, RSA). Then:

```bash
TOKEN=$(scripts/ewiki-ssh-login.sh https://wiki.example.com anna ~/.ssh/id_ed25519)
curl -H "Authorization: Bearer $TOKEN" https://wiki.example.com/api/v1/auth/me
```

Procedure: `POST /api/v1/auth/ssh/challenge` delivers a one-time challenge (valid for 2 minutes). The client signs it with `ssh-keygen -Y sign -n ewiki-login` (also works with ssh-agent and hardware keys). `POST /api/v1/auth/ssh/verify` then returns a token valid for `auth.ssh_token_ttl`.

### Command line
The local CLI has full rights without signing in. Protect the binary, `config.yaml` and `.env` with file permissions (the installer sets `.env` to 0600).

## Permissions

There are two kinds of permissions:

- **Global permissions** are given to **roles** (*Administration → Roles*), e.g. *Manage users*, *View log*, *Category administrator*. Permissions all signed-in users have by default (e.g. *Share pages*, *Mention people*) are set under *Administration → Roles → Default permissions*. Roles with "administrator role" have all permissions.
- **Category permissions** are given in rules per category (*Category → Settings → Permissions* or centrally under *Administration → Permissions*).

Plugins bring their own permissions (e.g. *Create workflows*, *Moderate comments*); they appear automatically in both selection lists, grouped by plugin.

### Category permissions

| Permission | allows |
|---|---|
| View | read the category and pages |
| Edit | change pages, restore versions |
| Create | create pages |
| Delete / move | put pages in the trash, move them, reset them |
| Manage | the **category itself**: texts, settings, create/move/delete sub-categories, permissions (only permissions you have yourself) |
| Plugin permissions | e.g. *Create workflows*, *Workflow administrator*, *See participant lists* |

There are templates for granting them:

- **Reader** – view.
- **Category editor** – works *in* the category (create, edit, delete pages and the plugin permissions for editors), but does not change the category.
- **Category manager** – additionally *Manage* and the plugin permissions for managers.
- **Category administrator** is a global permission: only whoever has it creates top-level categories and moves categories to the top level. They manage structure, settings and permissions of all categories, **but only see content with the permission View**.

Rules apply to a **user**, a **group**, a **role**, **all signed-in users** or **guests**. With "also applies to sub-categories" a rule is inherited. The effective permissions are the union of all matching rules of the category and its ancestors.

Principles:
- **Without a rule nothing is allowed.** A new sub-category inherits the permissions of the parent category. A new top-level category only gets a rule for its creator (category manager); guests only see it once you allow it.
- *Administration → Permissions* shows for a user, a group or a role all categories with direct and effective permissions and their origin; rules can be changed there.
- Permissions are tied to the stable category ID. Moving or renaming does not change the rules.
- Deleted categories (trash) are only reachable for administrators.
- Every API request is checked in the backend. The interface only hides what is forbidden anyway.
- API keys carry global permissions only if they were created with "administrator rights".
- Groups can get roles. Groups of external providers (LDAP/OIDC/SAML) are synchronised at sign-in.

### Workflows

Only their **creator** (permission *Create workflows*) and **workflow administrators** change and delete workflows. If an administrator changes someone else's workflow, every change is logged in detail and the creator is notified. Responsible groups and roles are stored as `#ID` (they stay valid when renamed; names still work). Steps of a case are carried out **only by the people responsible entered at the step** – not even by administrators as long as they are not responsible. *Manage cases* allows seeing all cases, cancelling them and deciding on failed steps.

### Drafts

Drafts are seen by their author and the editors of the category (permission *Edit*). Publishing is only possible where you have *Create* (new page) or *Edit* (existing page). Planned publications run with the permissions of the author at the time of publishing.

## Translations of the interface

*Administration → Languages* shows per language how much is translated, how many texts are missing and how many are **outdated** (the English text has changed since the translation, e.g. through a plugin update).

- **Permissions:** *Manage languages and translate the interface* (`core.i18n.manage`) allows everything: add/remove languages and translate all languages. *Translate the interface: <language>* (`core.i18n.translate.<code>`, per configured language) only allows translating, importing and exporting this one language. Whoever has one of the permissions sees the page *Languages*; the translation page of other languages stays locked (also in the API).
- **Translation page:** progress per component (core and every plugin), filters *To do*, *Missing*, *Outdated*, *Translated here*, *All*, search in keys and texts, 100 entries per page. You confirm outdated translations with *Still correct*. An emptied field removes your own translation, then the bundled text applies again.
- **Plural forms:** texts with a number have one field per plural form (according to CLDR: German and English *one*/*other*, Polish *one*/*few*/*many*/*other*, Arabic six forms). Example numbers are shown next to every field.
- **Import and export** (under *Import and export*): **JSON** (like the locale files), **XLIFF 2.0** (translation tools such as Trados, memoQ, Phrase; one `<file>` per component) and **PO** (gettext, e.g. Poedit, Weblate; `msgctxt` = key, every plural form as its own entry). You can export all texts, only missing/outdated ones or only those translated here, optionally only of one component. On import the format is detected; unknown keys, empty texts and entries marked as unfinished (PO `fuzzy`, XLIFF `state="initial"`) are skipped, the rest counts as translated here. Outdated texts are exported as `fuzzy` or `initial`.
- Every change and every import is logged (`i18n.translate`, `i18n.import`).

API: `GET|PUT /api/v1/admin/i18n/{lang}`, `GET /api/v1/admin/i18n/{lang}/export?format=json|xliff|po&source=&filter=all|missing|custom`, `POST /api/v1/admin/i18n/{lang}/import?format=` (file as body). CLI: `ewiki i18n status|strings|set|export|import|add|remove`.

## Reactions, carousels, page properties

- **Reactions** (`core.reactions.use`, all signed-in people by default): only on visible content. `core.reactions.manage` blocks emojis (*Administration → Reactions*, applies to all skin tones; existing reactions with them are hidden) and removes reactions of others.
- **Carousels:** `core.carousel.manage` creates carousels and edits slides and feeds, `core.carousel.publish` allows "As news" when publishing. External feeds are fetched through the SSRF protection: internal addresses only with the plugin setting *Allow feeds on internal addresses*. Links and images of own slides must be paths of the website or http(s) addresses; images need an alternative text.
- **Spaces:** logos only from the wiki (no foreign addresses, no tracking), accent colours with checked contrast, macros that are not allowed are not executed on the server. Only whoever has *Manage* on its top category changes the settings of a space (logged as `space.update`). Links only as a path of the wiki or http(s) (no `javascript:` or `//` addresses). Allowed element types apply on the server when creating, publishing, copying and moving. Only whoever may see the top category gets key, name and links of a space. **Archive:** archived categories are read-only at the level of the permission check (only *View* and *Manage* for restoring remain, administrators included; no named actions such as commenting). **Classification:** enforced in the permission check itself (from *Internal* never for guests, regardless of rules); from *Confidential* not in feeds, calendar subscriptions and e-mail content; *Strictly confidential* not in the general and external search (changes of the classification or moving trigger re-indexing), no export, no copies; media: from *Confidential* a watermark with name and time on images and PDFs (also on images that are only embedded in confidential pages; never cached), *Strictly confidential* no downloading of media and file elements without manage permission. Restrictions of a file itself (own permissions, *View only*, watermark) always take precedence; category and space settings can only restrict further, and restricted files are never delivered raw through embedding pages. Office programs (Collabora, OnlyOffice, Microsoft 365) also show the watermark of confidential categories and open such files only protected for viewing. Watermark and download block can be switched off deliberately per category (*Original*; logged, the next setting above counts). The strictest classification above always applies; on an error the strictest one is assumed. **Analytics:** views only as a number per page and day, no personal data, deleted after 400 days. **Access requests:** the answer is always the same, whether the target exists or not (no information about hidden content); at most 10 attempts per person and day; only managers of the category see and answer requests; a normal, logged rule is granted. **Personal spaces:** own permission, one per person, the ownership cannot be changed. **Export/import:** export only with *Manage* and only what is visible; import runs completely through the normal permission and content checks (element types, sanitising, allowed types), at most 50 MB, without permissions and files.
- **Sources (macros, blog, FAQ, calendar, carousel, reports):** sources are always resolved with the permissions of the reading person – elements, calendars and categories they may not see do not appear (not even as a section name or count). At most 50 sources per element/macro, 1000 single elements, 10 feeds per macro. External feeds and iCal subscriptions are only fetched in the background through the SSRF protection (internal addresses only with `feeds.allow_private`), at most 2 MB, without credentials. Their content is escaped: titles and summaries as text, links only `http(s)` with `rel="noopener noreferrer nofollow"`, images with `referrerpolicy="no-referrer"`, descriptions of foreign calendars as plain text (no foreign images or links that load more).
- **Page properties:** read with *View*, change with *Edit* of the category; changes are logged (`element.properties`).

## Secrets in the database

Signing keys of webhooks, secrets of authenticator apps, secrets of incoming and outgoing webhooks of workflows and forms, the key of the form tokens, the key of the decision links, the seal keys for signatures and the private keys of S/MIME certificates are stored in the database. How they are stored is set by `secrets.mode` in `config.yaml` (for the whole instance):

| Mode | Protection |
|---|---|
| `plain` (default) | as before: protected by the access to the database |
| `aes` | AES-256-GCM with a key file on the file system of the wiki. AES-256 is considered secure against quantum computers too (Grover leaves 128 bits) – the recommended choice for most installations. |
| `pq` | hybrid post-quantum encryption: per value its own key from **ML-KEM-1024** (NIST FIPS 203) **and** X25519, derived with HKDF-SHA256, then AES-256-GCM. Secure as long as one of the two methods holds. For requirements that ask for post-quantum methods. |

- The **key file** (`secrets.key_file`, default `secrets.key` next to `config.yaml`, permissions `600`) is created on the first start of a mode `aes` or `pq` and contains the keys of both modes. It does **not** belong in the same backup as the database – keep a copy separately: without it encrypted values are lost.
- Optionally a **password** protects the key file (Argon2id): if the environment variable `EWIKI_SECRETS_PASSWORD` (name changeable with `secrets.password_env`, e.g. from `.env`) contains a password on the first start, the file is encrypted with it; afterwards every start needs this password.
- Every encrypted value is bound to its place (table and row): copied into another row, it cannot be opened.
- **Changing the mode:** change `secrets.mode`, restart, then under *Administration → Expert settings → Secrets in the database* "Store all values in the current mode" (or `ewiki api POST /api/v1/admin/secrets/rewrap`, per site). Previous values stay readable until then; going back to `plain` only works with the key file present.
- **Database on another host** (not part of the standard installation, but possible: `database.url`): then key file and database are separate – whoever only gets the database (or a backup of it) reads no secrets; both hosts and possibly the password are needed.
- Secrets of workflows and forms (webhooks) are **never part of the page**: only a reference is stored; editors see "A secret is stored". A copy of the page (duplicate, template) does not take over the secret – it must be entered again.
- Passwords and keys in `config.yaml` or `.env` (SMTP, SSO, LDAP …) are not affected; they belong in the environment (`${NAME}`) and on a protected file system.

## Protection against overload

- **Request limits** per account or per address for guests, stricter for expensive requests (search, preview, export, upload); answer `429` with `Retry-After`. Settable under Administration → Expert settings (`core.rate.*`, see [configuration.md](configuration.md#limits-expert-settings)).
- **Size of requests:** without sign-in 64 kB, signed in 1 MB, content (pages, drafts, templates) 4 MB, imports 64 MB, uploads `storage.max_upload_mb`; at most 4 requests above 1 MB at the same time. Routes and plugins that need more register their limit; all values are under Administration → Expert settings (`core.request.*`). Images with more than 80 million pixels are rejected already at upload (`422`).
- **Database:** no function takes a second connection while it holds a transaction open; additionally PostgreSQL ends transactions that wait for longer than 60 seconds. So many simultaneous comments or case starts cannot block the pool.
- **Log:** values are limited (without a signed-in user 1 kB, otherwise 64 kB per before/after value, settable as `core.audit.*`; shortened texts note their original length); the free-text search in the before/after states covers the last 90 days without a start date.
- **Live editing:** at most 5 connections of one person and 50 people per page.

## Log (audit log)

*Administration → Log* (permission *View log*) lists all changes with time, person, IP address, browser and channel (interface, API, CLI, background job). Filters: free text, area, action, person, IP, period, object; details show the state before and after. Export as CSV or JSON Lines.

With *Configure log* you decide what is logged:

- Areas can be switched off – except security-relevant ones (permissions, roles, users, sign-in, log settings), which are always logged.
- Sign-ins (successful and failed), rejected accesses (403) and, if wanted, page views.
- **Forwarding** to other systems: syslog (UDP/TCP/TLS, RFC 5424), webhook (JSON, HMAC-signed, own headers), OpenTelemetry logs (OTLP/HTTP), file (JSON Lines) or standard output. Per target you choose the areas; every target remembers the last line sent and catches up after outages. *Test* sends a test entry, the overview shows backlog and last error.

API: `GET /api/v1/admin/audit/search`, `/admin/audit/export`, `GET|PUT /admin/audit/settings`, `POST /admin/audit/forward/{id}/test`. CLI: `ewiki audit search …`.

## Notifications, sharing, mentions, assigned watches

- **Notifications do not get lost.** What follows from a change is stored with it: comments, reactions, mentions in comments, answers to access requests and steps in processes write their notifications in the same transaction – either both are stored or nothing, and in the second case the person learns about it at once. Every change to pages and categories (create, save, move, delete, restore, archive, translate, events of a calendar) is written into a queue in the same transaction as the content; watchers, mentioned people and webhooks are served directly afterwards, and after an error or restart the background service `changes` catches up (every 30 seconds, with persistent errors at growing intervals up to one hour). A notification can arrive twice in exceptional cases, but never be missing. E-mails themselves are in the outbox and are retried until the mail server accepts them.
- The **bell** next to the user menu shows notifications in the wiki (mentions, shared pages, changes to workflows). E-mails are sent as well, unless the user has switched them off in the profile (default: on).
- **Sharing** (plugin `core.share`, permission *Share pages*) points people, groups or roles to a page. Sharing **does not change permissions**: whoever may not see the page is not notified; the dialog shows this beforehand.
- **@Mentions** (plugin `core.mentions`, permission *Mention people*) in pages and comments notify the people named – also only if they may see the page.
- **Assigned watches** (*Administration → Assigned watches*, permission *Assign watches*): users, groups or roles watch a category (optionally with sub-categories) or page. When assigning, you choose **mandatory** (cannot be unsubscribed) or **optional** (can be unsubscribed in the profile), plus the content of the e-mail, frequency and a note for the users.

## Protection of the web interface

- Page content is sanitised on the server when saving **and** when displaying (bluemonday policy for user content: no scripts, event handlers, `javascript:` links, iframes or inline styles).
- The interface creates all elements through DOM APIs, not through `innerHTML` with user data. The exception is the already sanitised page content.
- Strict content security policy (`script-src 'self'`, no inline JavaScript, `frame-ancestors 'self'`, `object-src 'none'`), plus `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, `Permissions-Policy` and HSTS (with `secure_cookies`).
- Menu links only allow `http(s)`, `mailto` and relative paths.
- Uploaded files: the type is determined from the content, not from what the browser says. HTML, SVG and scripts are never shown in the browser, only downloaded (`Content-Disposition: attachment`, `nosniff`, own CSP with `sandbox`). File names are sanitised. Files of an element are only available with read permission on the element. Images are also reachable through their SHA-256 hash, which cannot be guessed, so images in public pages work for guests too.
- **Live editing** (WebSocket `/api/v1/collab/elements/{id}/{lang}`): only signed-in users with a browser session (no API keys) and edit permission; the origin (`Origin`) must be the own host (no cross-site WebSocket). Session and permission are checked again every 30 s; if one ends, the connection is closed. The server stores nothing and only forwards Yjs messages (type checked, at most 4 MiB per message, 100 messages/s per person, 50 people and 5 connections per person per page (settable: `core.live.persons`, `core.live.connections_per_person`), slow receivers are disconnected, no compression). The server sets name and identifier in the cursor from the sign-in – nobody can appear under a foreign name or remove the cursors of others. Saving still happens through the normal API with all checks and sanitising.
- Configuration secrets are only in the environment or in `.env`. The database URL is masked in the interface.
- All security-relevant changes end up in the audit log.

## Macros with system access

The shell and the HTTP macro are the only places where page content triggers something on the server. That is why:

- The switches are only in `config.yaml` (`plugins.macros`). They cannot be changed through the interface or the API, not even by administrators.
- **Shell:** off by default. Editors can write the command into a page, but it only runs after a superadmin from `shell_approvers` or the CLI has approved exactly this command text (SHA-256). API keys can never approve. Commands run with `/bin/sh -c` in an empty temporary directory, with a minimal environment (no secrets from `.env`), their own process group, a time limit and output limited to 64 KB and escaped. If the wiki runs as root, commands are run as `shell_user` (default `nobody`). Approvals, blocks and deletions are in the audit log. Approved commands run with the permissions of the service user; only approve reading, harmless commands.
- **HTTP:** only URLs from the allow list; connections to loopback, private, link-local (among them cloud metadata 169.254.169.254) and CGNAT addresses are refused after DNS resolution (protection against SSRF and DNS rebinding), redirects are checked against the same rules. Answers are limited to 1 MB and 15 seconds; HTML is sanitised, text and JSON are escaped.
- Macros are rendered on every display with the permissions of the viewer, e.g. *Child pages* only lists visible categories.

## E-mail and webhooks

- Notifications (tasks, reminders, comments) only go to users who may see the page concerned, and can be switched off in the profile.
- Workflow e-mails may only use the configured sender address or addresses from `mail.allowed_from` as the sender; everything else becomes the reply address (protection against forged senders). Headers are cleaned of line breaks (no header injection), recipient addresses are checked, at most 50 recipients per message.
- Webhooks of workflows are subject to the same rules as the HTTP macro (allow list `plugins.macros.http_allow`, internal addresses blocked). With a secret every request is signed with HMAC-SHA256 (`X-Ewiki-Signature: sha256=…`), so the receiver can check its origin. The secret is not part of the workflow (see [secrets in the database](#secrets-in-the-database)); editors only see that one is stored.
- The SMTP password is masked in the interface; `${EWIKI_SMTP_PASSWORD}` with the value in `.env` is recommended.

## Workflows and comments

- A case is only seen by the person who started it, everyone who is or was responsible for one of its previous steps or the current step, users with the permission *Manage permissions* on the category and administrators. Files from forms are only delivered with this check.
- Only whoever is responsible for the current step may carry out actions (administrators as a substitute). Every action is in the history of the case and in the audit log.
- Decision links in task e-mails are signed with HMAC-SHA256 (key random per installation, stored encrypted like other secrets) and bind person, case, step entry, action and expiry (selectable per workflow, default 14 days, at most 90 days, never beyond the deadline of the step). A link only counts as long as the case is in exactly this step, and only once (only its hash is stored). Signed in, it only leads to the decision of your own person. Without sign-in only if *Decisions from e-mails without sign-in* is switched on, and never in confidential or secret categories; the audit log notes `workflow.link_action`. The link disappears from the address bar at once (no history, no referrer). Only the receiving person gets the token: outbox, API and CLI show it to administrators too only as `[hidden]`, and after delivery it is removed from the stored message (plugins register such secrets with `mail.RegisterRedaction`). If a message already delivered is sent again, it contains no valid links; the next reminder brings new ones.
- Only whoever manages accounts (`core.users.manage`) may save triggers for accounts (person created/disabled/deleted, group membership); this is checked when saving and again at every start. The events are created by database triggers from every source (administration, SCIM, sign-in providers). The change log `identity_changes` only contains IDs, for deleted people additionally user and display names (no e-mail), and is deleted after one day. Webhooks only get account events if they are chosen explicitly; through "all events" only if the administration allows it under *Administration → Webhooks* (`webhooks.accounts_in_all`, default off).
- The steps *Permissions*, *Group membership* and *Move page* run through the API with the permissions of the person who saved the workflow (or of the starting person): permissions only up to your own level, groups only with `core.groups.manage`, no changes to administrators by non-administrators, the last administrator stays. Values from the start form are decided by whoever starts a case by hand. That is why *Permissions* and *Group membership* reject a person from a start form field, as long as the administration does not allow it under *Administration → Workflow* (`form_values_for_accounts`, default off). Triggers, parent workflows, fixed values and `{{starter.username}}` always work. The system also revokes time-limited permissions when the person has lost their permissions in the meantime; every revocation is in the audit log (`workflow.grant_expired`).
- *Create file* uploads with the permissions of the person who saved the workflow (or of the starting person) into the chosen category of the media library; file names are sanitised, Excel cells are always text (never formulas), CSV values that start with `= + - @` get a `'` in front. *Calendar entry* goes through the interface of the calendar with the same permissions.
- **Signatures** (plugin kind `signature`, core `internal/signature`): what is signed is a record the server builds from the case (never the browser) – workflow and version, case, step with entry time, decision, note, values of the step form and all data of the case. On completion the server builds it again and compares the SHA-256 value; any difference makes the decision fail. A started signing process is valid for 10 minutes, for one person and exactly one attempt.
- **Passkey signature** (`signature.passkey`, requires `mfa.webauthn`): the WebAuthn challenge is the hash of the record plus 16 random bytes; user verification (PIN/biometrics) is required, copied keys (counter) are rejected. The evidence contains public key, authenticator and client data and the signature – checkable without the wiki. Level: advanced electronic signature.
- **S/MIME** (`signature.smime`): private keys are checked at upload (fit the certificate, RSA ≥ 2048 bits or ECDSA P-256/384/521, key usage e-mail, no CA certificate, not expired) and only stored encrypted with `secrets.mode`, bound to their row; they never leave the server and appear in no answer. The password of the PKCS#12 file is not stored and not logged. Only whoever has `signature.smime.manage` (administrators) manages the certificates of the organisation. Personal certificates only count for confirmed addresses of their person and only as long as the account is active; every use is in the audit log (`signature.smime.mail_signed`). Legally this proves the sender; it is not a signature under the sole control of the person – the interface says so. If an e-mail cannot be signed because something fails unexpectedly, it is never sent unsigned but stays in the outbox.
- **Second seal with the certificate of the organisation** (optional, `seal_evidence`): in addition to the seal of the wiki a CMS signature over the same bytes; the export contains it (`seal.cms`) and the sealed bytes (`sealed`), checkable with `openssl cms -verify -binary -inform DER -in seal.p7s -content sealed.json -CAfile ca.pem`.
- **Seal of the wiki:** every piece of evidence is sealed hybridly with Ed25519 and ML-DSA-65 (FIPS 204, post-quantum); the private key is in the secret store (`signature_keys`, encrypted with `secrets.mode`). Check: `/signatures/{id}/verify`, page *Verify signature*, `ewiki signature verify`. Evidence is only seen by whoever may see the case (and administrators); it is deleted with the case or removed when the case is anonymised.
- Test runs may only be started by whoever may change the workflow; they are only visible to this person and managers, send nothing outside and are deleted after 7 days. Only editors of the workflow and managers see the analysis.
- Retention: a background job anonymises or deletes finished cases after the set period and logs every run in the audit log.
- Comments: reading with read permission, writing signed in with read (or, by setting, edit) permission; authors, users with *Manage permissions* and administrators may delete. The text of deleted comments stays in the audit log.
