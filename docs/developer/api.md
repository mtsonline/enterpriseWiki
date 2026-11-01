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
- **Suche:** `GET /api/v1/search?q=wort&tags=a,b&lang=de` liefert `snippet` als HTML-escaped Text mit `<mark>`.

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
| `GET` | `/api/v1/auth/sso/{provider}/start` |
| `GET` | `/api/v1/auth/sso/{provider}/callback` |
| `POST` | `/api/v1/auth/sso/{provider}/callback` |

### Eigenes Konto (inkl. 2FA, API-Keys, SSH)

| Methode | Pfad |
|---|---|
| `PUT` | `/api/v1/me` |
| `PUT` | `/api/v1/me/password` |
| `GET` | `/api/v1/me/api-keys` |
| `POST` | `/api/v1/me/api-keys` |
| `DELETE` | `/api/v1/me/api-keys/{id}` |
| `GET` | `/api/v1/me/ssh-keys` |
| `POST` | `/api/v1/me/ssh-keys` |
| `DELETE` | `/api/v1/me/ssh-keys/{id}` |
| `GET` | `/api/v1/me/totp` |
| `POST` | `/api/v1/me/totp/setup` |
| `POST` | `/api/v1/me/totp/enable` |
| `POST` | `/api/v1/me/totp/disable` |

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

### Kategorien & Berechtigungen

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/tree` |
| `POST` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/{id}` |
| `PUT` | `/api/v1/categories/{id}` |
| `DELETE` | `/api/v1/categories/{id}` |
| `POST` | `/api/v1/categories/{id}/move` |
| `POST` | `/api/v1/categories/{id}/duplicate` |
| `POST` | `/api/v1/categories/{id}/reorder` |
| `GET` | `/api/v1/categories/{id}/acl` |
| `POST` | `/api/v1/categories/{id}/acl` |
| `DELETE` | `/api/v1/acl/{id}` |
| `GET` | `/api/v1/categories/{id}/history` |
| `POST` | `/api/v1/categories/{id}/rollback` |

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
| `GET` | `/api/v1/admin/settings` |
| `PUT` | `/api/v1/admin/settings` |
| `GET` | `/api/v1/admin/audit` |
| `POST` | `/api/v1/admin/retention/run` |
| `GET` | `/api/v1/admin/plugins` |
| `POST` | `/api/v1/admin/plugins/{id}/enable` |
| `POST` | `/api/v1/admin/plugins/{id}/disable` |
| `POST` | `/api/v1/admin/plugins/install` |
| `DELETE` | `/api/v1/admin/plugins/{id}` |
| `POST` | `/api/v1/admin/plugins/build` |
| `POST` | `/api/v1/admin/restart` |

### Plugins

| Methode | Pfad |
|---|---|
| `POST` | `/api/v1/x/auth.ldap/test` |
| `GET` | `/api/v1/x/auth.oidc/check` |
| `GET` | `/api/v1/x/auth.shib/metadata` |
| `GET` | `/api/v1/x/core.attachment/elements/{id}/file` |
| `GET` | `/api/v1/x/core.comments/elements/{id}/comments` |
| `POST` | `/api/v1/x/core.comments/elements/{id}/comments` |
| `PUT` | `/api/v1/x/core.comments/comments/{id}` |
| `DELETE` | `/api/v1/x/core.comments/comments/{id}` |
| `GET` | `/api/v1/x/core.menu/menus` |
| `POST` | `/api/v1/x/core.menu/menus` |
| `PUT` | `/api/v1/x/core.menu/menus/{id}` |
| `DELETE` | `/api/v1/x/core.menu/menus/{id}` |
| `GET` | `/api/v1/x/example.notice/tones` |
| `GET` | `/api/v1/x/macro.shell/commands` |
| `POST` | `/api/v1/x/macro.shell/commands` |
| `POST` | `/api/v1/x/macro.shell/commands/{hash}/` |
| `DELETE` | `/api/v1/x/macro.shell/commands/{hash}` |
| `POST` | `/api/v1/x/macro.shell/commands/{hash}/approve` |
| `POST` | `/api/v1/x/macro.shell/commands/{hash}/revoke` |
| `GET` | `/api/v1/x/workflow.process/elements/{id}/instances` |
| `POST` | `/api/v1/x/workflow.process/elements/{id}/instances` |
| `GET` | `/api/v1/x/workflow.process/instances/{id}` |
| `GET` | `/api/v1/x/workflow.process/instances/{id}/files/{field}` |
| `POST` | `/api/v1/x/workflow.process/instances/{id}/actions` |
| `POST` | `/api/v1/x/workflow.process/instances/{id}/cancel` |
| `GET` | `/api/v1/x/workflow.process/tasks` |
