# REST-API

Die Weboberfläche nutzt ausschließlich diese API. Alles, was in der Oberfläche geht, geht damit auch per Skript oder über `ewiki api` (CLI).

- Basis: `/api/v1`, JSON (UTF-8). Plugin-Routen liegen unter `/api/v1/x/<plugin-id>/…`.
- Fehler: HTTP-Status plus `{"error": {"code": "not_found", "message": "…"}}`. Codes: `bad_request`, `unauthorized`, `invalid_credentials`, `forbidden`, `csrf`, `not_found`, `conflict`, `rate_limited`, `internal`.
- Mehrsprachige Felder sind Objekte `{"de": "…", "en": "…"}`.
- Berechtigungen als Bitmaske: 1 = Ansehen, 2 = Bearbeiten, 4 = Erstellen, 8 = Löschen/Verschieben, 16 = Berechtigungen verwalten.

## Authentifizierung

| Verfahren | Verwendung |
|---|---|
| Session-Cookie | `POST /api/v1/auth/login {"username","password"}` setzt `ewiki_session`. Schreibende Anfragen – auch die Anmeldung selbst und Anfragen von Gästen – brauchen den Header `X-Requested-With: ewiki` |
| API-Key | `Authorization: Bearer ewk_…` (unter *Mein Profil* erstellen) |
| SSH-Schlüssel | Challenge holen, mit `ssh-keygen -Y sign -n ewiki-login` signieren, das Token als `Authorization: Bearer …` senden (siehe `scripts/ewiki-ssh-login.sh`) |
| LDAP-Konten | wie lokale Konten über `POST /api/v1/auth/login` (auch für Skripte); weitere Faktoren danach über `/api/v1/auth/mfa/…` |
| SSO (OIDC/SAML) | nur im Browser: `/api/v1/auth/sso/<id>/start?next=/pfad`. Für Skripte danach einen API-Key oder SSH-Schlüssel anlegen |

```bash
KEY=ewk_...
curl -H "Authorization: Bearer $KEY" https://wiki.example.com/api/v1/categories?parent=0
curl -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
     -d '{"category_id": 3, "type": "core.html", "lang": "de", "title": "Neue Seite",
          "body": {"format": "markdown", "source": "# Hallo"}}' \
     https://wiki.example.com/api/v1/elements
```

### Anmeldung aus Skripten

Für Skripte ist ein API-Key der einfachste Weg. Wer stattdessen mit Benutzername und Passwort arbeitet, sendet bei **jeder schreibenden Anfrage ohne API-Key – auch bei der Anmeldung selbst – den Header `X-Requested-With: ewiki`**. Ohne ihn lautet die Antwort `403` mit `"code": "csrf"`. Der Header schützt Browser davor, dass fremde Seiten im Namen der Besucher Anfragen stellen oder sie in ein fremdes Konto einloggen; Skripte setzen ihn einfach mit.

```bash
WIKI=https://wiki.example.com
JAR=$(mktemp)            # Datei für das Sitzungs-Cookie

# 1. anmelden: das Cookie ewiki_session landet in $JAR
curl -sS -c "$JAR" -H 'X-Requested-With: ewiki' -H 'Content-Type: application/json' \
     -d '{"username": "anna", "password": "…"}' "$WIKI/api/v1/auth/login"

# 2. lesen: Cookie genügt
curl -sS -b "$JAR" "$WIKI/api/v1/auth/me"

# 3. schreiben: Cookie und Header
curl -sS -b "$JAR" -H 'X-Requested-With: ewiki' -H 'Content-Type: application/json' \
     -d '{"category_id": 3, "type": "core.html", "title": "Neue Seite",
          "body": {"format": "markdown", "source": "# Hallo"}}' "$WIKI/api/v1/elements"

# 4. abmelden
curl -sS -b "$JAR" -H 'X-Requested-With: ewiki' -X POST "$WIKI/api/v1/auth/logout"; rm -f "$JAR"
```

Dasselbe in Python:

```python
import requests

s = requests.Session()
s.headers["X-Requested-With"] = "ewiki"      # gilt für alle Anfragen der Sitzung
r = s.post("https://wiki.example.com/api/v1/auth/login", json={"username": "anna", "password": "…"})
r.raise_for_status()
print(s.get("https://wiki.example.com/api/v1/auth/me").json())
```

Verlangt das Konto einen zweiten Faktor, antwortet die Anmeldung mit `mfa_required` und einem Token für `/api/v1/auth/mfa/…`; für unbeaufsichtigte Skripte deshalb API-Key oder SSH-Schlüssel verwenden. Anfragen mit `Authorization: Bearer …` brauchen den Header nicht.

**Grenzen.** Zu viele Anfragen beantwortet das Wiki mit `429` und `Retry-After` (Sekunden), zu große mit `413`, bei kurzzeitiger Überlast mit `503` und `Retry-After`. In allen drei Fällen wurde nichts gespeichert; die Anfrage kann nach der genannten Zeit unverändert wiederholt werden. Ein API-Key kann eine eigene Grenze haben (`rate_per_second` beim Anlegen, siehe [Konfiguration](../admin/configuration.md#limits-experteneinstellungen)).

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
- **Quellen:** Felder wie `more_sources` (Blog, FAQ, Kalender), `sources` (Karussell) und der Makro-Parameter `source` nehmen Quellen als Text: `category:12+sub; category:7; tag:hr; type:core.html; elements:4,9; search:Urlaub; props:status = open && due <= today+7d`, je nach Stelle zusätzlich `feed:https://…` (Makro `news`), `calendar:ID` und `ical:https://…` (Kalender, Makro `events`). **Formulare** (`content.forms`): `GET …/elements/{id}/start` liefert das Token (signiert, mindestens 2 s alt, höchstens 24 h), den Zustand (`""`, `not_open`, `closed`, `sign_in`, `full`, `already`) und die vorausgefüllten Werte; `POST …/elements/{id}/submit` als `multipart/form-data` mit dem Teil `data` = `{"values": {ID: Wert}, "token": "…", "website": "", "nonce": "…"}` (`nonce` nur bei `pow_bits` aus `start`: SHA-256 von `token:nonce` beginnt mit so vielen Null-Bits; Wiederholgruppen als Liste von Objekten, Fehler dort als `GRUPPE.ZEILE.FELD`; Unterschrift als Text oder Datei `file.ID` (PNG)) und Dateien `file.FELD` (Kommentare `comment.FELD.N`) oder als JSON ohne Dateien; Antwort `201 {id, message}`, bei Fehlern `422 {"error": {"code": "invalid", "fields": [{field, code, param}]}}`. `search:` (Volltextsuche, max. 100 Treffer) und `props:` (Eigenschaften-Filter wie im Bericht, Filter durch `&&` getrennt, max. 200 Treffer; nur bei aktivem Eigenschaften-Plugin) werden mit den Rechten der Lesenden aufgelöst. Ein `-` vor einer Quelle schließt aus (`-tag:intern`), ein `+` verlangt sie zusätzlich (`+tag:top`); in JSON als `"mode": "exclude" | "require"`. `POST /api/v1/sources/preview` mit `{sources, category_id, lang}` liefert `{total, items}` (die ersten 5 Titel) mit den Rechten des Aufrufers (nur angemeldet). **Seiten von Makro-Listen:** Parameter `limit`, `paginate` (`yes`/`no`), `per_page`; `GET /api/v1/elements/{id}/macro/{index}?page=N&lang=` liefert nur dieses Makro (`{html}`), `GET /api/v1/elements/{id}?mp=INDEX-PAGE` die ganze Seite mit dieser Seite der Liste. Obergrenze: Einstellung `lists.max_items` (`PUT /api/v1/admin/settings` mit `{"lists": {"max_items": 2000}}`). Ungültige Angaben ergeben `400`. `GET /api/v1/x/content.events/calendars/{id}/events` liefert Termine weiterer Kalender (`source`, schreibgeschützt) und externe Termine (`external: true`, negative `id`) mit.
- **Website-Vorlage je Kategorie:** `PUT /api/v1/categories/{id}` mit `{"template": "theme.cms"}` (nur aktive Vorlagen-Plugins, `""` = übernehmen; Recht *Verwalten*). Kategorien liefern `template` (eigene) und `effective_template` (die geltende, `""` = Site-Vorlage); `GET /api/v1/site` listet die aktiven Vorlagen unter `templates`.
- **Bereiche:** `GET /api/v1/spaces` (Verzeichnis, nur sichtbare), `GET /api/v1/spaces/{key}` (`404`, wenn nicht sichtbar), `PUT /api/v1/categories/{id}/space` mit `{"key": "HR", "own_tree": true, "shortcuts": [{"label": {"de": "Formulare"}, "url": "/wiki/hr/formulare"}], "allowed_types": ["core.html"], "language": "de"}` (nur oberste Ebene, Recht *Verwalten*; doppelter Schlüssel `409`). `GET /api/v1/categories/{id}` liefert dazu `space` (den Bereich der Kategorie oder `null`). Neuer Bereich aus Vorlage: `POST /api/v1/spaces` mit `{"template_id": 5, "titles": {"de": "Projekt X"}, "key": "PX", "with_pages": false}` (Kategorie-Administratoren; Vorlage = Bereich mit `is_template`). Archivieren: `POST /api/v1/categories/{id}/archive` mit `{"archived": true|false}` (*Verwalten*); Kategorien liefern `archived` (auch geerbt) und `archived_at`. Export: `GET /api/v1/categories/{id}/export` (JSON Lines, *Verwalten*); Import: `POST /api/v1/categories/import?parent=ID` mit der Datei als Body (`0` = neuer Bereich), Antwort `{category_id, categories, elements, templates, skipped}`. Seitenvorlagen eines Bereichs: `space_id` beim Speichern, `GET /api/v1/templates?category=ID`. Persönlicher Bereich: `POST /api/v1/spaces/personal` (Berechtigung `core.spaces.personal`; liefert den vorhandenen, falls es ihn gibt). Bereiche liefern `member` (eigene Rechte als Person, Gruppe oder Rolle) und `personal`. Zugriff anfordern: `POST /api/v1/access-requests` mit `{"type": "category"|"element", "id": 12, "message": "…"}` (immer `{"sent": true}`, höchstens 10 pro Tag, sonst `429`); offene Anfragen `GET /api/v1/categories/{id}/access-requests` (*Verwalten*), beantworten `POST /api/v1/access-requests/{id}` mit `{"grant": true, "preset": "reader"|"editor"}` oder `{"grant": false}`. Einstufung: `PUT /api/v1/categories/{id}` mit `{"classification": 0..3}` (`-1` = wie darüber); Kategorien liefern `classification` und `effective_classification`. Aufrufe: `POST /api/v1/elements/{id}/view` (nur zählen, pro Seite und Tag); Auswertung: `GET /api/v1/categories/{id}/insights?days=30&lang=de` (*Verwalten*). Weitere Bereichs-Einstellungen: `allowed_macros` (leer = alle), `accent` (`#rrggbb`, Kontrast ≥ 4,5:1 zu Weiß, sonst `400`), `logo` (`/api/v1/files/…` oder `/api/v1/media/…`).
- **Eigenschaften-Erweiterungen:** `POST /api/v1/x/core.properties/query.csv|query.xlsx` mit einer Abfrage oder dem Body von `/report` (Excel ohne Formeln, streng vertrauliche Zeilen nie); `POST …/trend` mit `{"report": {…}, "key": "status", "weeks": 12}` → `{weeks, series:[{value, values}]}`; gespeicherte Ansichten `GET|POST …/databases/{id}/views`, `DELETE …/views/{id}`; Abos `GET|POST …/subscriptions`, `DELETE …/subscriptions/{id}` (`frequency`: `daily`|`weekly`). Mit `"export_mode": "original"` (Abfrage) bzw. dem Parameter `export_mode` (Bericht, Datenbank) werden Werte mit `=` zu Formeln und CSV-Zellen nicht geschützt. Kategorien: `"watermark": "auto"|"off"|""` und `"downloads": "auto"|"allow"|""` (`""` = wie darüber).
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
| `POST` | `/api/v1/auth/mfa/enroll/begin` |
| `POST` | `/api/v1/auth/mfa/enroll/finish` |
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
| `PUT` | `/api/v1/me/api-keys/{id}/rate` |
| `GET` | `/api/v1/admin/api-keys` |
| `PUT` | `/api/v1/admin/api-keys/{id}/rate` |
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
| `PUT` | `/api/v1/me/alias` |
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
| `GET` | `/api/v1/me/mfa` |
| `POST` | `/api/v1/me/mfa/{method}/begin` |
| `POST` | `/api/v1/me/mfa/{method}/finish` |
| `GET` | `/api/v1/signature-methods` |
| `GET` | `/api/v1/signature-keys` |
| `GET` | `/api/v1/signatures/{id}` |
| `GET` | `/api/v1/signatures/{id}/verify` |
| `GET` | `/api/v1/signatures/{id}/export` |
| `POST` | `/api/v1/signatures/verify` |
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
| `PUT` | `/api/v1/admin/groups/{id}/members/{user}` |
| `DELETE` | `/api/v1/admin/groups/{id}/members/{user}` |
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
| `PUT` | `/api/v1/admin/users/{id}/alias` |
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
| `POST` | `/api/v1/sources/preview` |
| `GET` | `/api/v1/elements/{id}` |
| `GET` | `/api/v1/elements/{id}/macro/{index}` |
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
| `PUT` | `/api/v1/admin/webhooks/options` |
| `GET` | `/api/v1/admin/webhooks/{id}` |
| `PUT` | `/api/v1/admin/webhooks/{id}` |
| `DELETE` | `/api/v1/admin/webhooks/{id}` |
| `POST` | `/api/v1/admin/webhooks/{id}/test` |
| `POST` | `/api/v1/admin/webhooks/{id}/deliveries/{did}/retry` |
| `GET` | `/api/v1/admin/database` |
| `GET` | `/api/v1/admin/license` |
| `PUT` | `/api/v1/admin/license` |
| `DELETE` | `/api/v1/admin/license` |
| `GET` | `/api/v1/admin/limits` |
| `GET` | `/api/v1/admin/secrets` |
| `POST` | `/api/v1/admin/secrets/rewrap` |
| `PUT` | `/api/v1/admin/limits` |
| `GET` | `/api/v1/admin/mfa` |
| `PUT` | `/api/v1/admin/mfa` |
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
| `GET` | `/api/v1/x/auth.ldap/sync` |
| `POST` | `/api/v1/x/auth.ldap/sync` |
| `POST` | `/api/v1/x/auth.ldap/sync/adopt` |
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
| `GET` | `/api/v1/x/core.properties/types` |
| `GET` | `/api/v1/x/core.properties/defs` |
| `POST` | `/api/v1/x/core.properties/defs` |
| `PUT` | `/api/v1/x/core.properties/defs/{id}` |
| `DELETE` | `/api/v1/x/core.properties/defs/{id}` |
| `PUT` | `/api/v1/x/core.properties/settings/{id}` |
| `POST` | `/api/v1/x/core.properties/query` |
| `POST` | `/api/v1/x/core.properties/report` |
| `GET` | `/api/v1/x/core.properties/values` |
| `GET` | `/api/v1/x/core.properties/keys` |
| `GET` | `/api/v1/x/core.properties/elements/{id}` |
| `GET` | `/api/v1/x/core.properties/new` |
| `PUT` | `/api/v1/x/core.properties/elements/{id}` |
| `GET` | `/api/v1/x/core.properties/refs` |
| `GET` | `/api/v1/x/core.properties/history/{id}` |
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
| `GET` | `/api/v1/x/mfa.email/me` |
| `DELETE` | `/api/v1/x/mfa.email/me` |
| `GET` | `/api/v1/x/mfa.network/me` |
| `PUT` | `/api/v1/x/mfa.network/me` |
| `GET` | `/api/v1/x/mfa.network/users/{id}` |
| `PUT` | `/api/v1/x/mfa.network/users/{id}` |
| `GET` | `/api/v1/x/mfa.totp/me` |
| `DELETE` | `/api/v1/x/mfa.totp/me` |
| `GET` | `/api/v1/x/mfa.webauthn/credentials` |
| `DELETE` | `/api/v1/x/mfa.webauthn/credentials/{id}` |
| `GET` | `/api/v1/x/signature.smime/settings` |
| `PUT` | `/api/v1/x/signature.smime/settings` |
| `GET` | `/api/v1/x/signature.smime/certs` |
| `POST` | `/api/v1/x/signature.smime/certs` |
| `GET` | `/api/v1/x/signature.smime/certs/{id}` |
| `PATCH` | `/api/v1/x/signature.smime/certs/{id}` |
| `DELETE` | `/api/v1/x/signature.smime/certs/{id}` |
| `GET` | `/api/v1/x/signature.smime/me` |
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
| `POST` | `/api/v1/x/workflow.process/instances/{id}/sign` |
| `POST` | `/api/v1/x/workflow.process/instances/{id}/cancel` |
| `GET` | `/api/v1/x/workflow.process/tasks` |
| `GET` | `/api/v1/x/workflow.process/elements/{id}/triggers` |
| `POST` | `/api/v1/x/workflow.process/elements/{id}/triggers/{trigger}/secret` |
| `POST` | `/api/v1/x/workflow.process/hooks/{id}/{trigger}` |
| `GET` | `/api/v1/x/workflow.process/me/deputy` |
| `PUT` | `/api/v1/x/workflow.process/me/deputy` |
| `DELETE` | `/api/v1/x/workflow.process/me/deputy` |
| `GET` | `/api/v1/x/workflow.process/links/info` |
| `POST` | `/api/v1/x/workflow.process/links/act` |
| `GET` | `/api/v1/x/workflow.process/elements/{id}/stats` |
| `GET` | `/api/v1/x/content.forms/meta` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/start` |
| `POST` | `/api/v1/x/content.forms/elements/{id}/submit` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/submissions` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/submissions.csv` |
| `GET` | `/api/v1/x/content.forms/elements/{id}/submissions.xlsx` |
| `GET` | `/api/v1/x/content.forms/submissions/{sid}` |
| `DELETE` | `/api/v1/x/content.forms/submissions/{sid}` |
| `GET` | `/api/v1/x/content.forms/submissions/{sid}/files/{n}` |

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
