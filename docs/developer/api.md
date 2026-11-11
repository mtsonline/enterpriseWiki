# REST-API

Die Weboberfläche nutzt ausschließlich diese API. Alles, was in der Oberfläche geht, geht damit auch per Skript oder über `ewiki api` (CLI).

- Basis: `/api/v1`, JSON (UTF-8). Plugin-Routen liegen unter `/api/v1/x/<plugin-id>/…`.
- Fehler: HTTP-Status plus `{"error": {"code": "not_found", "message": "…"}}`. Codes: `bad_request`, `unauthorized`, `invalid_credentials`, `forbidden`, `csrf`, `not_found`, `conflict`, `rate_limited`, `internal`.
- Mehrsprachige Felder sind Objekte `{"de": "…", "en": "…"}`.
- Berechtigungen als Bitmaske: 1 = Ansehen, 2 = Bearbeiten, 4 = Erstellen, 8 = Löschen/Verschieben, 16 = Berechtigungen verwalten.

## Authentifizierung

| Verfahren | Verwendung |
|---|---|
| Session-Cookie | `POST /api/v1/auth/login {"username","password"}` setzt `ewiki_session`. Schreibende Anfragen brauchen den Header `X-Requested-With: ewiki` |
| API-Key | `Authorization: Bearer ewk_…` (unter *Mein Profil* erstellen) |
| SSH-Schlüssel | Challenge holen, mit `ssh-keygen -Y sign -n ewiki-login` signieren, das Token als `Authorization: Bearer …` senden (siehe `scripts/ewiki-ssh-login.sh`) |
| LDAP-Konten | wie lokale Konten über `POST /api/v1/auth/login` (auch für Skripte), optional `"totp"` bei lokalen Konten mit 2FA |
| SSO (OIDC/SAML) | nur im Browser: `/api/v1/auth/sso/<id>/start?next=/pfad`. Für Skripte danach einen API-Key oder SSH-Schlüssel anlegen |

```bash
KEY=ewk_...
curl -H "Authorization: Bearer $KEY" https://wiki.example.com/api/v1/categories?parent=0
curl -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
     -d '{"category_id": 3, "type": "core.html", "lang": "de", "title": "Neue Seite",
          "body": {"format": "markdown", "source": "# Hallo"}}' \
     https://wiki.example.com/api/v1/elements
```

## Wichtige Abläufe

- **Seite speichern:** `PUT /api/v1/elements/{id}` mit `{"lang", "title", "body", "comment", "base_rev"}`. `base_rev` ist die Version, mit der der Editor begonnen hat (0 für eine neue Übersetzung). Wurde inzwischen eine andere Version gespeichert, kommt `409 conflict` zurück. Jede Speicherung erzeugt eine neue Version.
- **Übersetzung:** Speichern in einer anderen Sprache als `original_lang`. Der Status je Sprache steht in `translations` (`original`, `current`, `outdated`, `missing`), der Fortschritt in `translated_percent`. Fehlt eine Sprache, liefert `GET /elements/{id}?lang=xx` die Originalsprache mit `fallback: true`.
- **Inhalt eines Elements** (`body`) hängt vom Elementtyp ab, für `core.html` ist es `{"format": "html"|"markdown", "source": "…"}`. `GET /elements/{id}?edit=1` liefert `body` (nur mit Bearbeitungsrecht), `html` ist immer die bereinigte Darstellung.
- **Zurücksetzen:** `POST /api/v1/categories/{id}/rollback {"at": "2026-10-01T12:00:00Z", "recursive": true}`.
- **Dateien:** `POST /api/v1/files` (multipart, Feld `file`) liefert `{hash, name, mime, size}`. Ein Element `core.attachment` referenziert die Datei mit `{"file": {"hash": "…"}, "description": "…"}`. Heruntergeladen wird mit Rechteprüfung über `GET /api/v1/x/core.attachment/elements/{id}/file[?hash=…][&inline=1]`. Bilder sind zusätzlich unter `GET /api/v1/files/{hash}` abrufbar (für Bilder in Seiten).
- **Suche:** `GET /api/v1/search?q=wort&tags=a,b&lang=de` liefert `snippet` als HTML-escaped Text mit `<mark>`. Mit `category=ID` wird nur in dieser Kategorie und ihren Unterkategorien gesucht (`recursive=0`: nur in der Kategorie).
- **Elemente auflisten:** `GET /api/v1/elements?q=&type=&category=&recursive=1&sort=title|updated|created|type&limit=&offset=` liefert `{items, total}` mit `category_path` (nur sichtbare Elemente, für Administratoren alle).
- **Kurz-URLs:** `short_url` bei `POST /elements`, `PATCH /elements/{id}`, `POST/PUT /categories` (leer = entfernen). `GET /api/v1/resolve?path=kalender` liefert `{type, id}`.
- **Beobachten:** `PUT /api/v1/me/watches {"target_type": "category"|"element", "target_id": 12, "mode": "link"|"content", "recursive": true}`; Status für ein Ziel: `GET /api/v1/me/watches?target_type=element&target_id=5`.
- **Feeds:** `GET /api/v1/feeds/categories/{id}?format=atom|rss&lang=de[&content=1]` (wenn in der Kategorie eingeschaltet), Blog/FAQ unter `/api/v1/x/content.blog|content.faq/elements/{id}/feed`. Feed-Reader ohne Anmeldung verwenden den persönlichen Schlüssel aus `GET /api/v1/me/feed-token` als `&token=…`. Er gilt nur für Feed- und Kalender-Routen und lässt sich mit `POST` erneuern bzw. mit `DELETE` löschen.
- **Termine:** `POST /api/v1/x/content.events/calendars/{id}/events {"title", "starts_at", "ends_at", "all_day", "tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;BYDAY=MO", "registration": true, "capacity": 20, "reminders": [1440]}`; Termine eines Zeitraums mit `GET …/calendars/{id}/events?from=…&to=…` (Serien sind aufgelöst, `occurrences[].date` ist der Schlüssel für Anmeldungen). Anmelden: `POST …/events/{id}/registrations {"occurrence": "2026-11-23"}` (`interested: true` nur für Erinnerungen).
- **E-Mail-Warteschlange (Admin):** `GET /api/v1/admin/mail` (Status mit Zählern pro Empfänger), `GET /api/v1/admin/mail/outbox?status=failed&q=…`, `GET /api/v1/admin/mail/outbox/{id}` (mit Empfängern und letzter Serverantwort).

## Endpunkte

### System

| Methode | Pfad |
|---|---|
| `GET` | `/readyz` |
| `GET` | `/api/v1/site` |
| `GET` | `/healthz` |

### Anmeldung & Single Sign-on

| Methode | Pfad |
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
| `POST` | `/api/v1/auth/passwordless/{method}/begin` |
| `POST` | `/api/v1/auth/passwordless/{method}/finish` |
| `GET` | `/api/v1/auth/sso/{provider}/logout` |
| `POST` | `/api/v1/auth/sso/{provider}/logout` |

### Eigenes Konto (inkl. 2FA, API-Keys, SSH)

| Methode | Pfad |
|---|---|
| `PUT` | `/api/v1/me` |
| `GET` | `/api/v1/me/prefs` |
| `PUT` | `/api/v1/me/password` |
| `GET` | `/api/v1/me/api-keys` |
| `POST` | `/api/v1/me/api-keys` |
| `DELETE` | `/api/v1/me/api-keys/{id}` |
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
| `GET` | `/api/v1/me/totp` |
| `POST` | `/api/v1/me/totp/setup` |
| `POST` | `/api/v1/me/totp/enable` |
| `POST` | `/api/v1/me/totp/disable` |
| `GET` | `/api/v1/me/mfa` |
| `POST` | `/api/v1/me/mfa/email/code` |
| `POST` | `/api/v1/me/mfa/email/enable` |
| `POST` | `/api/v1/me/mfa/email/disable` |
| `PUT` | `/api/v1/me/mfa/networks` |
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
| `GET` | `/api/v1/media/viewers` |
| `GET` | `/api/v1/media/{id}/open` |
| `POST` | `/api/v1/media/{id}/open` |
| `GET` | `/api/v1/me/sessions` |
| `DELETE` | `/api/v1/me/sessions` |
| `DELETE` | `/api/v1/me/sessions/{id}` |

### Benutzer, Gruppen, Rollen

| Methode | Pfad |
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
| `GET` | `/api/v1/admin/users/{id}/profile` |
| `PUT` | `/api/v1/admin/users/{id}/profile` |
| `GET` | `/api/v1/admin/users/{id}/sessions` |
| `DELETE` | `/api/v1/admin/users/{id}/sessions` |
| `DELETE` | `/api/v1/admin/users/{id}/sessions/{sid}` |

### Kategorien & Berechtigungen

| Methode | Pfad |
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

### Elemente & Versionen

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/element-types` |
| `POST` | `/api/v1/elements` |
| `POST` | `/api/v1/elements/preview` |
| `GET` | `/api/v1/elements/{id}` |
| `PUT` | `/api/v1/elements/{id}` |
| `PATCH` | `/api/v1/elements/{id}` |
| `DELETE` | `/api/v1/elements/{id}` |
| `POST` | `/api/v1/elements/{id}/move` |
| `POST` | `/api/v1/elements/{id}/duplicate` |
| `GET` | `/api/v1/elements/{id}/revisions` |
| `GET` | `/api/v1/elements/{id}/revisions/{lang}/{rev}` |
| `POST` | `/api/v1/elements/{id}/restore` |
| `GET` | `/api/v1/elements` |

### Makros

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/macros` |

### Dateien

| Methode | Pfad |
|---|---|
| `POST` | `/api/v1/files` |
| `GET` | `/api/v1/files/{hash}` |

### Papierkorb, Suche, Tags

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/trash` |
| `POST` | `/api/v1/trash/{type}/{id}/restore` |
| `DELETE` | `/api/v1/trash/{type}/{id}` |
| `GET` | `/api/v1/search` |
| `GET` | `/api/v1/tags` |

### Übersetzungen & Sprachen

| Methode | Pfad |
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

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/admin/updates` |
| `POST` | `/api/v1/admin/updates/{id}` |

### Administration

| Methode | Pfad |
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
| `GET` | `/api/v1/admin/webhooks/{id}` |
| `PUT` | `/api/v1/admin/webhooks/{id}` |
| `DELETE` | `/api/v1/admin/webhooks/{id}` |
| `POST` | `/api/v1/admin/webhooks/{id}/test` |
| `POST` | `/api/v1/admin/webhooks/{id}/deliveries/{did}/retry` |
| `GET` | `/api/v1/admin/database` |
| `GET` | `/api/v1/admin/license` |
| `PUT` | `/api/v1/admin/license` |
| `DELETE` | `/api/v1/admin/license` |
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

| Methode | Pfad |
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
| `GET` | `/api/v1/x/core.properties/elements/{id}` |
| `PUT` | `/api/v1/x/core.properties/elements/{id}` |
| `GET` | `/api/v1/x/core.properties/keys` |
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
| `GET` | `/api/v1/x/mfa.webauthn/credentials` |
| `POST` | `/api/v1/x/mfa.webauthn/register/begin` |
| `POST` | `/api/v1/x/mfa.webauthn/register/finish` |
| `DELETE` | `/api/v1/x/mfa.webauthn/credentials/{id}` |
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
| `POST` | `/api/v1/x/workflow.process/instances/{id}/cancel` |
| `GET` | `/api/v1/x/workflow.process/tasks` |

### Sonstige

| Methode | Pfad |
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
