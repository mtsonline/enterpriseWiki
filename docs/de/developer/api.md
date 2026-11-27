# REST-API

Die Weboberfläche nutzt ausschließlich diese API. Alles, was in der Oberfläche geht, geht damit auch per Skript oder über `ewiki api` (CLI).

- Basis: `/api/v1`, JSON (UTF-8). Plugin-Routen liegen unter `/api/v1/x/<plugin-id>/…`.
- Fehler: HTTP-Status plus `{"error": {"code": "not_found", "message": "…"}}`. Codes: `bad_request`, `unauthorized`, `invalid_credentials`, `forbidden`, `csrf`, `not_found`, `conflict`, `rate_limited`, `internal`. Die Meldung steht in der Sprache des Headers `Accept-Language` (erster Eintrag; Deutsch mit `de`), sonst auf Englisch; der Code ist in jeder Sprache gleich. Die Texte stehen in `internal/apperr/locales/<sprache>.json` (Schlüssel: das englische Format); `go test ./internal/apperr` prüft, dass jede Meldung des Codes einen Text hat.
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
- **Zurücksetzen:** `POST /api/v1/categories/{id}/rollback {"at": "2026-10-01T12:00:00Z", "recursive": true, "structure": true}`; `structure` (nur mit `recursive`) macht seitdem erfolgte Verschiebungen und Umsortierungen rückgängig (neueste zuerst, mit den Prüfungen des normalen Verschiebens) und verschiebt seitdem angelegte Unterkategorien in den Papierkorb; Antwort `{categories, revisions, trashed, moved, categories_trashed}`, ein nicht mehr vorhandener früherer Platz gibt `409` und ändert nichts. Höchstzahl der Änderungen: Grenzwert `core.rollback.structure_changes`.
- **Dateien:** `POST /api/v1/files` (multipart, Feld `file`) liefert `{hash, name, mime, size}`. Ein Element `core.attachment` referenziert die Datei mit `{"file": {"hash": "…"}, "description": "…"}`. Heruntergeladen wird mit Rechteprüfung über `GET /api/v1/x/core.attachment/elements/{id}/file[?hash=…][&inline=1]`. Bilder sind zusätzlich unter `GET /api/v1/files/{hash}` abrufbar (für Bilder in Seiten).
- **Medien auflisten:** `GET /api/v1/media?category=ID&recursive=1&q=&type=&sort=&used_by=&limit=&offset=` liefert nur Dateien, die Sie sehen dürfen. `type`: `image`, `video`, `audio`, `pdf`, `document`; `sort`: `updated` (Vorgabe, neueste zuerst), `name`, `size` (größte zuerst), `type`; `used_by=ID`: nur die Dateien, die diese Seite zeigt (nur für eine Seite, die Sie sehen dürfen). Der Baustein *Anhänge* nutzt dieselbe Liste.
- **Suche:** `GET /api/v1/search?q=wort&tags=a,b&lang=de` liefert `snippet` als HTML-escaped Text mit `<mark>`. Mit `category=ID` wird nur in dieser Kategorie und ihren Unterkategorien gesucht (`recursive=0`: nur in der Kategorie).
- **Optionen platzierter Bausteine:** Jede Platzierung kann `options` tragen (Aussehen und Anzeige an ihrer Position). `GET /api/v1/block-options` (öffentlich) liefert das Schema: Gruppen, Optionen mit Art, Werten oder Bereich und ob ein Wert je Ansicht (`desktop`, `tablet`, `mobile`) möglich ist. Der Server prüft die Optionen bei `PUT /api/v1/placements/{level}`: unbekannte Optionen, Form und Bereich jedes Werts, Kontrast der Textfarbe auf ihrem Hintergrund (auch auf Verläufen und Bildern unter ihrer Überlagerung), Platz für Spalten in jeder Ansicht, eindeutige Anker; ein ausgeblendeter Baustein hat keine Optionen. CLI: `ewiki placement options`.
- **Templates aus Daten:** `GET /api/v1/x/core.template/designs` (öffentlich) liefert die verwendbaren Templates mit ihrem Design; das Wiki baut daraus seine Seiten. `GET …/limits` liefert die Grenzen (dieselbe Datei, mit der der Server prüft). Mit `core.template.design`: `GET …/templates` (`?deleted=1`: der Papierkorb), `GET …/templates/{key}` (mit `used_by`), `POST …/check` (Probleme eines Designs, nichts wird gespeichert), `POST …/templates` (`names` und `design` oder `copy_of`), `PUT …/templates/{key}` (`version` muss die geladene sein, sonst 409), `DELETE …/templates/{key}` (in den Papierkorb, nicht solange verwendet; `?purge=1` endgültig aus dem Papierkorb), `POST …/templates/{key}/restore`, `GET …/templates/{key}/versions`, `GET …/templates/{key}/export`, `POST …/templates/import`, `PUT …/templates/{key}/rights` (`edit_subjects`, `manage_subjects`: Personen, Gruppen, Rollen oder alle Angemeldeten, die dieses Template ändern oder verwalten dürfen; leere Listen: alle Gestaltenden; ein leeres `edit_subjects` mit Personen in `manage_subjects`: nur diese; Verwalten umfasst die Rechte, den Papierkorb und das eigene CSS; Administratoren dürfen immer; niemand kann sich selbst das Verwalten nehmen; die Antwort von `GET …/templates/{key}` hat `may` und `subject_names`). Schriften: `GET …/fonts`, `POST …/fonts` (`name`, `license` eine von `OFL-1.1`, `Apache-2.0`, `MIT`, `other` mit `license_note`, `confirm: true`, `files`: 1 bis 8 Dateien, je `data` (WOFF2, WOFF, TTF oder OTF als base64; eine Schrift, keine Sammlung), `weight` (`400` oder ein Bereich `100 900`) und `style` (`normal`, `italic`)), `DELETE …/fonts/{id}` (409, solange ein Template sie verwendet, auch im Papierkorb), `GET …/fonts/{id}/css` (ihre `@font-face`-Regeln) und `GET …/fonts/{id}/files/{n}` (öffentlich, solange ein Template außerhalb des Papierkorbs sie hat, wie die Designs; sonst nur für Gestaltende). Ein Design verwendet eine hochgeladene Schrift als `own-ID` in `tokens.font` oder `tokens.heading_font`. IDs sind `core.template/<key>`; sie werden wie andere Template-IDs verwendet (`ui.template`, das Template einer Kategorie, die Platzierungsebene `template`). CLI: `ewiki site-template …`.
- **Kategorieseite:** `GET /api/v1/categories/{id}/overview?lang=&page=&view=` liefert eine Seite der Übersicht einer Kategorie in ihrer Darstellung: `display` (`view`: `list`, `cards`, `tiles`, `tree`, `handbook`, `table`, `accordion`, `tabs`, `az`, `timeline`, `all`; `sort`: `manual`, `title`, `updated`, `created`; `page_size`, `subcategories`, `excerpt`; `columns`: bis zu 12 weitere Schlüssel der Tabelle, zum Beispiel Eigenschaften; `date`: `updated` oder `created` für die Zeitleiste), `items` (mit `excerpt` aus dem Textindex), `total`, `page`, `pages` und `category_titles`. `az` geht immer nach Titel, `timeline` nach ihrem Datum (neueste zuerst); `accordion`, `tabs` und `all` zeigen ganze Seiten, höchstens `core.categories.full` je Seite (Experteneinstellung). Nur Seiten, die Sie sehen dürfen; unsichtbare Seiten nur für Bearbeitende der Kategorie; mit `subcategories` auch die Seiten der Unterkategorien, die weder unsichtbar noch archiviert sind. Die Darstellung wird mit `PUT /api/v1/categories/{id}/settings` gesetzt (`display`; ohne: wie darüber) und gilt für die Unterkategorien weiter. CLI: `ewiki category overview ID`.
- **Vorlagen:** `GET /api/v1/templates?kind=page|block&type=&category=` (Vorgabe `page`); `POST /api/v1/templates` mit `kind: "block"` speichert eine Baustein-Vorlage (immer Markdown einer Seite, `type` wird `core.html`). Die Art einer Vorlage lässt sich nicht ändern; der Baustein *Seite aus Vorlage anlegen* nimmt nur Seitenvorlagen.
- **Geltungsbereich einer Platzierung für eine Kategorie:** Eine Platzierung der Ebene `category` kann `only_category_page: true` (nur auf der Seite der Kategorie, nicht auf den Seiten darin) und `no_subcategories: true` (nicht in den Unterkategorien; dort gelten die Ebenen darüber) tragen. Andere Ebenen lehnen beides ab (`400`). `GET /api/v1/placements?category=&element=` lässt weg, was für diese Seite nicht gilt.
- **Layout je Elementtyp:** Die Ebene `type` (Ziel: die ID des Elementtyps, zum Beispiel `content.blog`; braucht `core.settings.manage`) ordnet die Positionen der Seiten dieses Typs (`element-…`). Sie gilt nach der Website und vor den Kategorien; `GET /api/v1/placements?element=ID` liefert sie nur für eine Seite, die Sie sehen dürfen. CLI: `ewiki placement set type --query target=content.blog`.
- **Hervorgehobene Seiten:** Die Seite einer einzelnen Kategorie ist eine eigene Ebene der Platzierung, `category_page` (Ziel: die Kategorie-ID, nicht vererbt; braucht *Verwalten*), mit den Positionen `category-header`, `category-toolbar`, `category-content`, `category-aside` und `category-footer`. Eine hervorgehobene Seite ist der Baustein `core.category.element/<Element-ID>` auf dieser Ebene; sie muss eine Seite sein, die Sie sehen dürfen, höchstens `core.categories.featured` (Experteneinstellung). `GET /api/v1/elements/batch?ids=1,2,3&lang=` (höchstens der größere Wert von `core.categories.featured` und `core.categories.full`) lädt mehrere Seiten auf einmal (wie `GET /elements/{id}`); Seiten, die Sie nicht sehen dürfen, fehlen. CLI: `ewiki element batch --query ids=1,2`.
- **Zustand des Wikis:** `GET /api/v1/admin/status` (nur für Administratoren) liefert `version`, `updates_available` und `updates_checked_at` aus der letzten Prüfung (ohne Anfrage an GitHub), `mail_failed` und `mail_queued`, `queue_pending` und `queue_oldest` der Warteschlange für Änderungen, `login_failed_24h`, `audit_forward_errors`, `plugins_inactive` (eingeschaltet, aber nicht aktiv), `restart_pending`, `build_failed`, `license_level` und `database_level` (`ok`, `info`, `warning`, `error`). Dazu die Teile der Übersicht der Administration, jeweils nur mit dem Recht der passenden Seite: `updates` (nur mit `core.plugins.manage` und für Administratoren der ganzen Instanz: `core` aus der letzten Prüfung mit `security` und `security_notes`, `install` mit `kind` `docker`/`systemd`/leer, `image`, `origin` `published`/`self-built`, `dir` und `commands`, je `kind` und `lines`; unter Docker `timer` mit dem geprüften Zustand des Update-Helfers `scripts/update-docker.sh` – `mode`, `next_run`, `window_minutes`, `last_run`, `last_result`, `last_version`, `updated_at`, `fresh` – oder `null`, und `timer_error` `unreadable`, wenn seine Datei die Prüfung nicht besteht), `plugins` (`total`, `enabled`, `active`, `in_process`; `core.plugins.manage`), `users` (`active`, `disabled`, `invited`, `directory` mit dem letzten Lauf je Verzeichnis; `core.users.manage`), `security` (`providers` mit der Zahl nötiger Faktoren, `factors`; `core.settings.manage`) und `license` (wie `GET /api/v1/admin/license`; `core.license.manage`). Die Befehle führt das Wiki nie selbst aus. CLI: `ewiki status`.
- **Neue Einsendungen:** `GET /api/v1/x/content.forms/submissions/recent?limit=8` listet die Formulare mit Einsendungen der letzten 90 Tage, deren Einsendungen Sie sehen dürfen (`element_id`, `titles`, `last`, `unseen`), neueste zuerst. CLI: `ewiki form recent`.
- **Benachrichtigungen:** `GET /api/v1/me/inbox` listet Ihre eigenen Benachrichtigungen, die neueste zuerst: `items`, `unread` (Zahl aller ungelesenen) und `more` (es gibt ältere). Abfrageparameter: `limit` (1–200, Vorgabe 30), `before` (ID einer Benachrichtigung: nur ältere; eine ID, die nicht Ihnen gehört, ergibt eine leere Liste) und `unread=1` (nur ungelesene). Jeder Eintrag hat `id`, `kind`, `title`, `body`, `link`, `actor` (Anzeigename), `actor_id` (nur, wenn Sie diese Person öffnen dürfen), `created_at`, `read_at` und `popup`. CLI: `ewiki inbox list [--limit N] [--before ID] [--unread] --as USER`.
- **Pop-up-Benachrichtigungen:** `GET /api/v1/me/inbox/popups` listet Ihre eigenen ungelesenen Benachrichtigungen, die als Pop-up markiert sind, die älteste zuerst, höchstens `core.notify.popups` (Experteneinstellung); jede mit `id`, `title`, `body` (reiner Text, höchstens 200 und 2.000 Zeichen), `link` und `popup`. Die Oberfläche zeigt jede einmal als Dialog und markiert sie mit `POST /api/v1/me/inbox/read` als gelesen. Pop-ups entstehen im Kern und in Plugins (`notify.Message` mit `Popup`, zum Beispiel der Workflow-Schritt *Nachricht* mit `"popup": true`), nie über die API für andere Personen. CLI: `ewiki me inbox popups`.
- **Zuletzt geändert:** `GET /api/v1/recent?limit=10&category=ID` listet zuletzt geänderte Seiten, `?by=me` die zuletzt von Ihnen bearbeiteten (neueste eigene Version zuerst); immer nur, was Sie sehen dürfen.
- **Daten für Berichte:** `GET /api/v1/data` listet die Datenarten, die Bausteine und Plugins ablegen (Schema, `complete`). `GET /api/v1/data/{kind}?category=ID&state=open,decided&from=2026-01-01&to=…&person=ID&ref=ID&element=ID&lang=de&q=Wort&sort=-day&limit=50&offset=0` liefert die Einträge mit `total` (jeder mit `element_id`, `category_id`, `lang`, `rev`: die Fassung, aus der er stammt, `data`); `person=me`: die eigenen Einträge (nur angemeldet); `no_day=true`: nur Einträge ohne Tag; `elements=4,9`: nur von diesen Elementen (höchstens 1000); `prefer=de`: von jeder Seite nur die Einträge einer Sprachfassung (dieser Sprache, sonst der Originalsprache der Seite; die Berichte über Aufgaben, Termine und Entscheidungen und die Kalender tun das mit der Sprache der lesenden Person, damit eine Seite in zwei Sprachen einmal erscheint); nur von Elementen, die Sie sehen dürfen, nie aus dem Papierkorb, unsichtbare mit `hidden=true`; `direct=true` ohne Unterkategorien; `sort`: `day`, `-day`, `at`, `-at`, `when`, `-when` (nach Tag, dann Uhrzeit), `num`, `-num`, `label`, `state`, `page`; höchstens 500 je Antwort. `people` liefert die Namen der Personen in Feldern vom Typ `person` (ID → Name), nur von Personen, die Sie finden dürfen (Sie selbst, alle mit `core.users.list_all`, die Mitglieder Ihrer Gruppen mit `core.users.list_own`; nie für Gäste, nie deaktivierte Benutzer). `POST /api/v1/admin/data/rebuild` mit `{"kind": "…"}` (oder `{}` für alle) baut die Daten aus den neuesten Fassungen neu auf (`core.settings.manage`). **Aufgaben in Seiten:** Aufgabenpunkte (`- [ ] …`) mit `@Name` oder Datum (`:date{value=2026-11-30}`) werden als Datenart `core.html.task` abgelegt (Felder `title`, `day`, `state` `open`/`done`, `person`, `item` = Nummer des Aufgabenpunkts auf der Seite, ab 1 in der Reihenfolge der Seite, verschachtelte mitgezählt; ein Eintrag je Person; nicht in Bausteinen, die nur manchen zeigen). `POST /api/v1/x/core.html/tasks/{id}/{n}` mit `{"rev": 4, "done": true, "lang": "de"}` hakt Aufgabenpunkt `n` ab (mit `false` wieder offen): mit dem Recht, die Seite zu bearbeiten, jede Aufgabe, als Person der Aufgabe mit `core.html.task_done_own`; `rev` muss die neueste Fassung sein (sonst `409`); ohne `lang` die Originalsprache. Gespeichert wird eine neue Fassung, die nur dieses Kästchen ändert; die Antwort ist `{rev, state}`. CLI: `ewiki tasks done ID N --json '{"rev": 4, "done": true}'`, eigene Aufgaben mit `ewiki data query core.html.task --query person=me --query state=open`.
- **Wartung:** `GET /api/v1/admin/maintenance` und `PUT /api/v1/admin/maintenance` mit `{"mode": "read_only", "message": {"de": "…"}, "until": "2026-11-23T18:00:00+01:00", "run_jobs": false}` (`mode` leer: aus; nur Administratoren). Während der Wartung beantwortet der Server Änderungen anderer mit `503` und dem Code `maintenance` (und `Retry-After`); `GET /api/v1/site` enthält `maintenance` (Modus, Text, Ende), `null`, wenn aus.
- **Zustimmung:** `GET /api/v1/me/consent` (eigene Zustimmung zur Datenschutzerklärung), `POST /api/v1/me/consent` mit `{"kind": "privacy", "version": 1}` (nur die geltende Version, sonst `409`); `GET/PUT /api/v1/admin/consent` (Einstellungen, siehe Konfiguration `consent`); `GET /api/v1/admin/consents?version=&limit=&offset=` (Nachweis; `format=csv`). `GET /api/v1/site` enthält `consent` (`cookies`, `privacy`), `null`, wenn aus. Solange die Datenschutzerklärung nicht angenommen ist, bekommen Sitzungen `403` mit dem Code `consent_required`.
- **Hinweis auf lange Sätze:** `GET /api/v1/site` enthält `readability` (`mode` `off`/`offered`/`on`, `sentence_words`, `word_letters`, `max_kb`); die Einstellung der Website ist `ui.readability` in `PUT /api/v1/admin/settings` (nur Administratoren, geprüft: `400` außerhalb der Bereiche). Die eigene Wahl ist die Einstellung `readability` (`on`, `off`, `""` = wie die Website es vorgibt) in `PUT /api/v1/me`; `GET /api/v1/me/prefs` liefert außerdem `readability` mit `site` (dem Modus) und `active` (ob der Hinweis für die Person an ist). Gezählt wird im Browser; es gibt keine Route, die dafür Text bekommt.
- **Elemente auflisten:** `GET /api/v1/elements?q=&type=&category=&recursive=1&sort=title|updated|created|type&limit=&offset=` liefert `{items, total}` mit `category_path` (nur sichtbare Elemente, für Administratoren alle).
- **Kurz-URLs:** `short_url` bei `POST /elements`, `PATCH /elements/{id}`, `POST/PUT /categories` (leer = entfernen). `GET /api/v1/resolve?path=kalender` liefert `{type, id}`.
- **Beobachten:** `PUT /api/v1/me/watches {"target_type": "category"|"element", "target_id": 12, "mode": "link"|"content", "recursive": true}`; Status für ein Ziel: `GET /api/v1/me/watches?target_type=element&target_id=5`.
- **Feeds:** `GET /api/v1/feeds/categories/{id}?format=atom|rss&lang=de[&content=1]` (wenn in der Kategorie eingeschaltet), Blog/FAQ unter `/api/v1/x/content.blog|content.faq/elements/{id}/feed`. Feed-Reader ohne Anmeldung verwenden den persönlichen Schlüssel aus `GET /api/v1/me/feed-token` als `&token=…`. Er gilt nur für Feed- und Kalender-Routen und lässt sich mit `POST` erneuern bzw. mit `DELETE` löschen.
- **Quellen:** Felder wie `more_sources` (Blog, FAQ, Kalender), `sources` (Karussell) und der Makro-Parameter `source` nehmen Quellen als Text: `category:12+sub; category:7; tag:hr; type:core.html; elements:4,9; search:Urlaub; props:status = open && due <= today+7d`, je nach Stelle zusätzlich `feed:https://…` (Makro `news`), `calendar:ID` und `ical:https://…` (Kalender, Makro `events`). **Formulare** (`content.forms`): `GET …/elements/{id}/start` liefert das Token (signiert, mindestens 2 s alt, höchstens 24 h), den Zustand (`""`, `not_open`, `closed`, `sign_in`, `full`, `already`) und die vorausgefüllten Werte; `POST …/elements/{id}/submit` als `multipart/form-data` mit dem Teil `data` = `{"values": {ID: Wert}, "token": "…", "website": "", "nonce": "…"}` (`nonce` nur bei `pow_bits` aus `start`: SHA-256 von `token:nonce` beginnt mit so vielen Null-Bits; Wiederholgruppen als Liste von Objekten, Fehler dort als `GRUPPE.ZEILE.FELD`; Unterschrift als Text oder Datei `file.ID` (PNG)) und Dateien `file.FELD` (Kommentare `comment.FELD.N`) oder als JSON ohne Dateien; Antwort `201 {id, message}`, bei Fehlern `422 {"error": {"code": "invalid", "fields": [{field, code, param}]}}`. `search:` (Volltextsuche, max. 100 Treffer) und `props:` (Eigenschaften-Filter wie im Bericht, Filter durch `&&` getrennt, max. 200 Treffer; nur bei aktivem Eigenschaften-Plugin) werden mit den Rechten der Lesenden aufgelöst. Ein `-` vor einer Quelle schließt aus (`-tag:intern`), ein `+` verlangt sie zusätzlich (`+tag:top`); in JSON als `"mode": "exclude" | "require"`. `POST /api/v1/sources/preview` mit `{sources, category_id, lang}` liefert `{total, items}` (die ersten 5 Titel) mit den Rechten des Aufrufers (nur angemeldet). **Seiten von Makro-Listen:** Parameter `limit`, `paginate` (`yes`/`no`), `per_page`; `GET /api/v1/elements/{id}/macro/{index}?page=N&lang=` liefert nur dieses Makro (`{html}`), `GET /api/v1/elements/{id}?mp=INDEX-PAGE` die ganze Seite mit dieser Seite der Liste. Obergrenze: Einstellung `lists.max_items` (`PUT /api/v1/admin/settings` mit `{"lists": {"max_items": 2000}}`). Ungültige Angaben ergeben `400`. `GET /api/v1/x/content.events/calendars/{id}/events` liefert Termine weiterer Kalender (`source`, schreibgeschützt) und externe Termine (`external: true`, negative `id`) mit.
- **Website-Vorlage je Kategorie:** `PUT /api/v1/categories/{id}` mit `{"template": "theme.cms"}` (nur aktive Vorlagen-Plugins, `""` = übernehmen; Recht *Verwalten*). Kategorien liefern `template` (eigene) und `effective_template` (die geltende, `""` = Site-Vorlage); `GET /api/v1/site` listet die aktiven Vorlagen unter `templates`.
- **Bereiche:** `GET /api/v1/spaces` (Verzeichnis, nur sichtbare), `GET /api/v1/spaces/{key}` (`404`, wenn nicht sichtbar), `PUT /api/v1/categories/{id}/space` mit `{"key": "HR", "own_tree": true, "shortcuts": [{"label": {"de": "Formulare"}, "url": "/wiki/hr/formulare"}], "allowed_types": ["core.html"], "language": "de"}` (nur oberste Ebene, Recht *Verwalten*; doppelter Schlüssel `409`). `GET /api/v1/categories/{id}` liefert dazu `space` (den Bereich der Kategorie oder `null`). Neuer Bereich aus Vorlage: `POST /api/v1/spaces` mit `{"template_id": 5, "titles": {"de": "Projekt X"}, "key": "PX", "with_pages": false}` (Kategorie-Administratoren; Vorlage = Bereich mit `is_template`). Archivieren: `POST /api/v1/categories/{id}/archive` mit `{"archived": true|false}` (*Verwalten*); Kategorien liefern `archived` (auch geerbt) und `archived_at`. Export: `GET /api/v1/categories/{id}/export` (JSON Lines, *Verwalten*); Import: `POST /api/v1/categories/import?parent=ID` mit der Datei als Body (`0` = neuer Bereich), Antwort `{category_id, categories, elements, templates, skipped}`. Seitenvorlagen eines Bereichs: `space_id` beim Speichern, `GET /api/v1/templates?category=ID`. Persönlicher Bereich: `POST /api/v1/spaces/personal` (Berechtigung `core.spaces.personal`; `201`, wenn er angelegt wird, `200` mit dem vorhandenen). Bereiche liefern `member` (eigene Rechte als Person, Gruppe oder Rolle) und `personal`. Zugriff anfordern: `POST /api/v1/access-requests` mit `{"type": "category"|"element", "id": 12, "message": "…"}` (immer `202` mit `{"sent": true}`, höchstens 10 pro Tag, sonst `429`); offene Anfragen `GET /api/v1/categories/{id}/access-requests` (*Verwalten*), beantworten `POST /api/v1/access-requests/{id}` mit `{"grant": true, "preset": "reader"|"editor"}` oder `{"grant": false}`. Einstufung: `PUT /api/v1/categories/{id}` mit `{"classification": 0..3}` (`-1` = wie darüber); Kategorien liefern `classification` und `effective_classification`. Aufrufe: `POST /api/v1/elements/{id}/view` (nur zählen, pro Seite und Tag); Auswertung: `GET /api/v1/categories/{id}/insights?days=30&lang=de` (*Verwalten*). Weitere Bereichs-Einstellungen: `allowed_macros` (leer = alle), `accent` (`#rrggbb`, Kontrast ≥ 4,5:1 zu Weiß, sonst `400`), `logo` (`/api/v1/files/…` oder `/api/v1/media/…`).
- **Eigenschaften-Erweiterungen:** `POST /api/v1/x/core.properties/query.csv|query.xlsx` mit einer Abfrage oder dem Body von `/report` (Excel ohne Formeln, streng vertrauliche Zeilen nie); `POST …/trend` mit `{"report": {…}, "key": "status", "weeks": 12}` → `{weeks, series:[{value, values}]}`; gespeicherte Ansichten `GET|POST …/databases/{id}/views`, `DELETE …/views/{id}`; Abos `GET|POST …/subscriptions`, `DELETE …/subscriptions/{id}` (`frequency`: `daily`|`weekly`). Mit `"export_mode": "original"` (Abfrage) bzw. dem Parameter `export_mode` (Bericht, Datenbank) werden Werte mit `=` zu Formeln und CSV-Zellen nicht geschützt. Kategorien: `"watermark": "auto"|"off"|""` und `"downloads": "auto"|"allow"|""` (`""` = wie darüber).
- **Termine:** `POST /api/v1/x/content.events/calendars/{id}/events {"title", "starts_at", "ends_at", "all_day", "tz": "Europe/Vienna", "rrule": "FREQ=WEEKLY;BYDAY=MO", "registration": true, "capacity": 20, "reminders": [1440]}`; Termine eines Zeitraums mit `GET …/calendars/{id}/events?from=…&to=…` (Serien sind aufgelöst, `occurrences[].date` ist der Schlüssel für Anmeldungen). Anmelden: `POST …/events/{id}/registrations {"occurrence": "2026-11-23"}` (`interested: true` nur für Erinnerungen).
- **E-Mail-Warteschlange (Admin):** `GET /api/v1/admin/mail` (Status mit Zählern pro Empfänger), `GET /api/v1/admin/mail/outbox?status=failed&q=…`, `GET /api/v1/admin/mail/outbox/{id}` (mit Empfängern und letzter Serverantwort).

## Aktionsregister

Jede Route ist eine **Aktion** mit einem Eintrag im Aktionsregister, im Format `ewiki.plugin.v1.Action` (`sdk/proto/ewiki/plugin/v1/action.proto`). Daraus prüft das Wiki, dass jede Aktion über API, CLI und Oberfläche erreichbar ist, und baut die CLI-Befehle für Aktionen ohne handgeschriebenen Befehl.

- **Ablage:** Kern in `internal/actions/core/<bereich>.yaml`, Core-Plugins in `plugins/<plugin>/actions.yaml` (Pfade relativ zu `/api/v1/x/<plugin-id>/`, eingebunden mit `actions.Add` in `actions.go`).
- **Pflicht je Eintrag:** `id` nach der Namensraum-Regel, `category` (`CATEGORY_ACTION` oder `CATEGORY_PROTOCOL` für Aufrufe anderer Systeme), `method`, `path`, `summary` auf Deutsch **und** Englisch, genau eines von `permission`, `signed_in`, `public`, bei Aktionen `cli` und `ui` oder eine vom Projektinhaber genehmigte Ausnahme (`exceptions`).
- **Erfolgsstatus:** Jede Aktion (`CATEGORY_ACTION`) nennt `success_status`, und die Beispiele prüfen ihn exakt: `201`, wenn etwas Neues mit eigener Adresse entsteht, `202`, wenn Arbeit im Hintergrund beginnt (z. B. Plugins bauen, Neustart, Zugriffsanfrage mit Benachrichtigung), `204` ohne Inhalt, sonst `200`. `DELETE` antwortet mit `204`; ein anderer Status braucht eine beschriebene `response`.
- **Kommentar an der Route:** `// action: <id>` direkt über `s.handle(…)` bzw. `host.Handle(…)`.
- **Prüfungen:**
  - `TestActionRegister` (Stufe 1): Register und Routen stimmen überein, die Kommentare passen. Jede Route hat einen Eintrag; eine neue Route ohne Eintrag lässt die CI scheitern. Die Tabellen unter „Endpunkte“ müssen genau die Routen des Registers enthalten. Auch eine Route, die nur am Mux hängt und nicht erfasst ist (`h.RecordRoute`), fällt auf. Routen, die eine gemeinsame Hilfsfunktion anlegt, nennen ihre Aktion beim Aufruf: `// action: <id> for <METHODE> <Pfad>`.
  - `TestNoServerErrors` (Stufe 2, „kein Absturz“): Jede Route des Registers bekommt falsche Eingaben (ungültige oder zu große IDs, kaputtes JSON, falsche Typen, unsinnige Parameter), als Administrator, als angemeldete Person ohne Rechte und als Gast. Keine Antwort darf ein 5xx sein. Ausnahmen stehen mit Begründung in `notProbed`.
  - `TestActionExamples` (Stufe 2): Jedes Beispiel (`examples`) läuft über die API und über die Kommandozeile mit demselben Ergebnis (Status, `expect_json` als Teilmenge der Antwort). `given` legt vorher für jeden Weg frisch als Administrator an, worauf das Beispiel aufbaut; `${given.<Aktion>#<Beispiel>.<Feld>}` setzt Werte daraus ein, `${unique}` macht Namen eindeutig, `${unique_lang}` liefert einen freien Sprachcode für den lokalen Gebrauch (`qaa`–`qtz`). `as: self` führt das Beispiel mit einem frischen eigenen Konto aus (Aktionen am eigenen Konto); seine Vorbedingungen mit `as: self` laufen mit demselben Konto, die mit `as: other` mit einem zweiten Konto (dem von `${other.…}`). Listen und Objekte aus `${given…}` werden als JSON eingesetzt. `${self.id|name|password}` und `${other.id|name|password}` (eine weitere angemeldete Person) setzen solche Konten ein, `${ssh_key}` einen frischen öffentlichen SSH-Schlüssel, `${totp:GEHEIMNIS}` den aktuellen Code einer Authenticator-App (z. B. mit dem Geheimnis aus einer Vorbedingung). `${webauthn.create:REF}` und `${webauthn.get:REF.PFAD}` beantworten die Optionen einer Vorbedingung mit dem Software-Passkey des eigenen Kontos (Einrichten, Signieren), `${pkcs12:ADRESSE}` ist ein Zertifikat mit Schlüssel einer Test-Zertifizierungsstelle (Passwort `s`). `${mail_link:ADRESSE:PARAM}` ist PARAM des Links in der neuesten E-Mail an ADRESSE (die Beispiele versenden über einen Mailserver der Tests), `${smtp_port}` dessen Port. `requires` nennt echte Testsysteme, die ein Beispiel braucht (`actions.TestSystems`: `opensearch`, `elasticsearch`, `meilisearch`, `ldap`, `keycloak`, `collabora`, `onlyoffice`, `microsoft365`); `${system.<Name>.<Feld>}` setzt deren Adresse oder Schlüssel ein. Solche Beispiele laufen nur, wo die Systeme gestartet sind (Umgebungsvariablen `EWIKI_TEST_<NAME>_<FELD>` aus `tests/search/start.sh`, `tests/auth/start.sh` und `tests/office/start.sh`; `microsoft365` ist die öffentliche Discovery von Microsoft und läuft nur in der CI), sonst werden sie übersprungen; mit `EWIKI_TEST_EXAMPLE_SYSTEMS=opensearch,…` laufen nur sie, und die Systeme müssen laufen (CI-Jobs `search`, `auth` und `office`). `${local_plugin:ID}` legt ein frisches lokales Plugin mit dieser ID an und setzt seinen Ordner ein, `${plugin_root}` ist der Ordner dieser Plugins (für vertrauenswürdige Quellen). Bei kritischen Aktionen (`safety.recent_sign_in`) läuft der Kommandozeilen-Weg als Kommandozeile auf dem Server; vorher muss `--as` (keine Anmeldung) abgewiesen werden. `body_text` sendet einen Inhalt, der kein JSON ist (z. B. einen Export); `${given.REF.$raw}` ist die ganze Antwort. Aktionen mit genehmigter Ausnahme für die Kommandozeile laufen nur über die API. `<Feld>` kann ein Pfad sein, etwa `deliveries.0.id`; bei einer Antwort ohne Inhalt (204) gelten die Felder der Anfrage. Hochladen: `form` (Textfelder) und `files` (`field`, `name`, `content_type`, `text` oder `base64`) statt `body_json`. `as: member` bekommt genau das Recht aus dem Register (global über eine Rolle, für Kategorien über eine Regel auf der Kategorie aus dem Parameter); zusätzlich muss eine Person ohne dieses Recht auf beiden Wegen abgewiesen werden. Aktionen ohne Beispiel stehen in `internal/actions/examples-missing.txt`; die Liste darf nur kürzer werden.
  - Abdeckung der Oberfläche (`tools/uicoverage`, CI-Job der Browser-Tests): Der Browser-Test schneidet jede API-Anfrage mit, die die Seiten senden (`EWIKI_E2E_UI_REQUESTS=DATEI`). Das Werkzeug ordnet sie mit denselben Mustern wie der Server den Aktionen zu. Jede Aktion mit Weg in der Oberfläche (`ui`) muss im Browser-Test mindestens einmal eine erfolgreiche Antwort (Status unter 400) bekommen; eine Ablehnung zählt nicht. Eine Anfrage, die keine Aktion beschreibt, lässt die Prüfung ebenfalls scheitern. Anfragen, die der Test selbst aus einer Seite sendet, laufen über `__e2eFetch` und zählen nicht; ein eigener Test verbietet ein ungekennzeichnetes `fetch` im Browser-Test. Noch nicht abgedeckte Aktionen stehen in `internal/actions/ui-missing.txt`; die Liste darf nur kürzer werden (`go run ./tools/uicoverage -requests DATEI -write` verkürzt sie).
  - `go run ./tools/protocheck` im Ordner `sdk`: übersetzt den Vertrag und liest alle Register-Dateien und Beispiel-Manifeste mit den echten Nachrichten (unbekannte Felder, falsche Werte und ungültiges JSON in Schemas sind Fehler).
- **Texte mit Komma** in Block-Form schreiben (`text:` mit `en:` und `de:` darunter); in der Kurzform `{en: a, b}` würde das Komma den Text trennen. Die Prüfung erkennt das.

```yaml
actions:
  - id: core.auth.password_reset
    category: CATEGORY_ACTION
    method: POST
    path: /api/v1/auth/password-reset
    summary:
      text:
        en: Ask for a link to set a new password (local accounts)
        de: Link für ein neues Passwort anfordern (lokale Konten)
    public: true
    body:
      json: '{"type": "object", "required": ["email"], "properties": {"email": {"type": "string"}}}'
    success_status: 202
    safety: {changes: true}
    cli: {command: [auth, password-reset, request]}
    ui: {entry: /password-reset}
```

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
| `POST` | `/api/v1/auth/register` |
| `POST` | `/api/v1/auth/register/confirm` |
| `POST` | `/api/v1/auth/password-reset` |
| `POST` | `/api/v1/auth/password-reset/confirm` |
| `GET` | `/api/v1/auth/sso/{provider}/logout` |
| `POST` | `/api/v1/auth/sso/{provider}/logout` |

### Eigenes Konto (inkl. 2FA, API-Keys, SSH)

| Methode | Pfad |
|---|---|
| `PUT` | `/api/v1/me` |
| `GET` | `/api/v1/me/prefs` |
| `PUT` | `/api/v1/scheme` |
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
| `GET` | `/api/v1/me/inbox/popups` |
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
| `DELETE` | `/api/v1/media/{id}/versions` |
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

`PUT /api/v1/scheme` mit `{"scheme": "system"|"light"|"dark"|""}` wählt das Farbschema (`""`: Vorgabe des Templates), auch für Gäste. Es setzt das Cookie `ewiki_scheme` und speichert angemeldet zusätzlich die persönliche Einstellung `scheme` (in der CLI: `ewiki pref set scheme dark --as BENUTZER`).

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

#### Personen (Personenseite, Menü an Namen)

`GET /api/v1/people/{id}` liefert, was der Aufrufer von einer Person sehen darf: `id`, `display_name`, `handle`, `card` (Visitenkarte: `job_title`, `department`, `phone`, `mobile`, `office`, `about`, `photo` – das Foto nur nach der Foto-Regel) und `self`. `email` (die primäre bestätigte Adresse) ist nur dabei, wenn der Aufrufer sie sehen darf (Einstellung `people.email_visibility`, Recht `core.people.email`, das eigene `email_hidden` der Person). Der Aufrufer braucht `core.people.view` und öffnet nur Personen, die er finden darf (sich selbst, mit `core.users.list_all` alle, mit `core.users.list_own` die Mitglieder gemeinsamer Gruppen). Gäste bekommen `401`, Aufrufer ohne das Recht `403`, eine Person, die der Aufrufer nicht finden darf, `404` – dasselbe wie eine Person, die es nicht gibt. Jeder Aufruf zählt für die Grenze `core.people.views_per_hour` (darüber `429`; Administration ohne Grenze). CLI: `ewiki person show ID`.

Neben Namen tragen Antworten die ID der Person nur für Aufrufer, die die Person öffnen dürfen (nur dann zeigt die Oberfläche das Menü am Namen): `updated_by_id` (Element), `author_id` (Versionen, Kommentare), `deleted_by_id` (Papierkorb), `started_by_id` und `actor_id` (Vorgänge von `workflow.process`), `id` der Mitglieder von Team-Seiten und Personen-Bausteinen, `updated_by_id` (Entwürfe), `author_id` (Listen von Seiten und Beiträgen, Termine, Formular-Einsendungen), `user_id` (Zugangsanfragen), `subject_person` und `created_by_id` (zugewiesene Beobachtungen), `open` (je `user_ids` von Reaktionen für die Verwaltung, je Zeile im Nachweis der Zustimmungen). Gäste bekommen diese IDs nie. Termine senden `created_by` nicht mehr, Formular-Einsendungen `user_id` nicht mehr. Namen neben Inhalten sind nur der Anzeigename, nie die Kennung (`benutzer@anbieter`); die Kennung steht auf der Personenseite und in der Benutzerverwaltung.

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/people/{id}` |

### SCIM 2.0 (Verzeichnisse)

Aufgerufen von Verzeichnissen wie Entra ID oder Okta mit dem Token eines SCIM-Zugangs (siehe Admin-Dokumentation).

| Methode | Pfad |
|---|---|
| `GET` | `/scim/v2/ServiceProviderConfig` |
| `GET` | `/scim/v2/ResourceTypes` |
| `GET` | `/scim/v2/Schemas` |
| `GET` | `/scim/v2/Users` |
| `POST` | `/scim/v2/Users` |
| `GET` | `/scim/v2/Users/{id}` |
| `PUT` | `/scim/v2/Users/{id}` |
| `PATCH` | `/scim/v2/Users/{id}` |
| `DELETE` | `/scim/v2/Users/{id}` |
| `GET` | `/scim/v2/Groups` |
| `POST` | `/scim/v2/Groups` |
| `GET` | `/scim/v2/Groups/{id}` |
| `PUT` | `/scim/v2/Groups/{id}` |
| `PATCH` | `/scim/v2/Groups/{id}` |
| `DELETE` | `/scim/v2/Groups/{id}` |

### Kategorien & Berechtigungen

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/tree` |
| `POST` | `/api/v1/categories` |
| `GET` | `/api/v1/categories/{id}` |
| `GET` | `/api/v1/categories/{id}/overview` |
| `PUT` | `/api/v1/categories/{id}` |
| `PUT` | `/api/v1/categories/{id}/translations/{lang}` |
| `DELETE` | `/api/v1/categories/{id}` |
| `POST` | `/api/v1/categories/{id}/move` |
| `POST` | `/api/v1/categories/{id}/duplicate` |
| `POST` | `/api/v1/categories/{id}/reorder` |
| `GET` | `/api/v1/categories/{id}/acl` |
| `POST` | `/api/v1/categories/{id}/acl` |
| `GET` | `/api/v1/categories/{id}/access-requests` |
| `POST` | `/api/v1/categories/{id}/archive` |
| `DELETE` | `/api/v1/acl/{id}` |
| `GET` | `/api/v1/categories/{id}/history` |
| `POST` | `/api/v1/categories/{id}/rollback` |
| `GET` | `/api/v1/categories/{id}/export` |
| `GET` | `/api/v1/categories/{id}/insights` |
| `PUT` | `/api/v1/categories/{id}/space` |
| `POST` | `/api/v1/categories/import` |
| `PUT` | `/api/v1/acl/{id}` |
| `GET` | `/api/v1/categories/{id}/settings` |
| `PUT` | `/api/v1/categories/{id}/settings` |
| `GET` | `/api/v1/elements/{id}/rights` |
| `PUT` | `/api/v1/elements/{id}/rights` |
| `GET` | `/api/v1/elements/{id}/labels` |
| `PUT` | `/api/v1/elements/{id}/labels` |
| `GET` | `/api/v1/elements/{id}/acl` |
| `GET` | `/api/v1/elements/{id}/access-requests` |
| `PUT` | `/api/v1/elements/{id}/acl` |
| `POST` | `/api/v1/elements/{id}/acl` |
| `PUT` | `/api/v1/element-acl/{id}` |
| `DELETE` | `/api/v1/element-acl/{id}` |
| `GET` | `/api/v1/placements` |
| `GET` | `/api/v1/placements/{level}` |
| `PUT` | `/api/v1/placements/{level}` |
| `POST` | `/api/v1/access-requests` |
| `POST` | `/api/v1/access-requests/{id}` |
| `GET` | `/api/v1/spaces` |
| `POST` | `/api/v1/spaces` |
| `POST` | `/api/v1/spaces/personal` |
| `GET` | `/api/v1/spaces/{key}` |

### Elemente & Versionen

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/element-types` |
| `POST` | `/api/v1/elements` |
| `POST` | `/api/v1/elements/preview` |
| `POST` | `/api/v1/sources/preview` |
| `GET` | `/api/v1/elements/batch` |
| `GET` | `/api/v1/block-options` |
| `GET` | `/api/v1/x/core.template/designs` |
| `GET` | `/api/v1/x/core.template/limits` |
| `GET` | `/api/v1/x/core.template/templates` |
| `POST` | `/api/v1/x/core.template/templates` |
| `POST` | `/api/v1/x/core.template/check` |
| `POST` | `/api/v1/x/core.template/templates/import` |
| `GET` | `/api/v1/x/core.template/templates/{key}` |
| `PUT` | `/api/v1/x/core.template/templates/{key}` |
| `DELETE` | `/api/v1/x/core.template/templates/{key}` |
| `POST` | `/api/v1/x/core.template/templates/{key}/restore` |
| `PUT` | `/api/v1/x/core.template/templates/{key}/rights` |
| `GET` | `/api/v1/x/core.template/templates/{key}/versions` |
| `GET` | `/api/v1/x/core.template/fonts` |
| `POST` | `/api/v1/x/core.template/fonts` |
| `DELETE` | `/api/v1/x/core.template/fonts/{id}` |
| `GET` | `/api/v1/x/core.template/fonts/{id}/css` |
| `GET` | `/api/v1/x/core.template/fonts/{id}/files/{n}` |
| `GET` | `/api/v1/x/core.template/templates/{key}/export` |
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
| `POST` | `/api/v1/elements/{id}/view` |
| `GET` | `/api/v1/elements` |
| `GET` | `/api/v1/collab/elements/{id}/{lang}` |

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
| `GET` | `/api/v1/recent` |
| `GET` | `/api/v1/search` |
| `GET` | `/api/v1/tags` |
| `GET` | `/api/v1/data` |
| `GET` | `/api/v1/data/{kind}` |
| `POST` | `/api/v1/admin/data/rebuild` |
| `GET` | `/api/v1/admin/maintenance` |
| `PUT` | `/api/v1/admin/maintenance` |
| `GET` | `/api/v1/me/consent` |
| `POST` | `/api/v1/me/consent` |
| `GET` | `/api/v1/admin/consent` |
| `PUT` | `/api/v1/admin/consent` |
| `GET` | `/api/v1/admin/consents` |

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
| `GET` | `/api/v1/admin/status` |

### Administration

| Methode | Pfad |
|---|---|
| `GET` | `/api/v1/admin/permissions` |
| `PUT` | `/api/v1/admin/permissions/authenticated` |
| `GET` | `/api/v1/admin/settings` |
| `PUT` | `/api/v1/admin/settings` |
| `POST` | `/api/v1/admin/retention/run` |
| `GET` | `/api/v1/admin/plugins` |
| `PUT` | `/api/v1/admin/plugins/options` |
| `POST` | `/api/v1/admin/plugins/{id}/enable` |
| `POST` | `/api/v1/admin/plugins/{id}/disable` |
| `POST` | `/api/v1/admin/plugins/install` |
| `DELETE` | `/api/v1/admin/plugins/{id}` |
| `POST` | `/api/v1/admin/plugins/build` |
| `POST` | `/api/v1/admin/plugins/origin` |
| `GET` | `/api/v1/admin/plugins/sources` |
| `POST` | `/api/v1/admin/plugins/sources` |
| `DELETE` | `/api/v1/admin/plugins/sources` |
| `PUT` | `/api/v1/admin/plugins/sources/external` |
| `GET` | `/api/v1/admin/plugins/credentials` |
| `PUT` | `/api/v1/admin/plugins/credentials` |
| `DELETE` | `/api/v1/admin/plugins/credentials` |
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
| `GET` | `/api/v1/admin/templates/usage` |
| `GET` | `/api/v1/admin/mfa` |
| `PUT` | `/api/v1/admin/mfa` |
| `POST` | `/api/v1/admin/auth/providers/{id}/preview` |
| `GET` | `/api/v1/admin/auth/login` |
| `PUT` | `/api/v1/admin/auth/login` |
| `GET` | `/api/v1/admin/scim/clients` |
| `POST` | `/api/v1/admin/scim/clients` |
| `POST` | `/api/v1/admin/scim/clients/{id}/token` |
| `DELETE` | `/api/v1/admin/scim/clients/{id}` |
| `GET` | `/api/v1/admin/demo` |
| `POST` | `/api/v1/admin/demo` |
| `DELETE` | `/api/v1/admin/demo` |
| `POST` | `/api/v1/admin/reset` |

**Herkunft von Core-Plugins** (nur Instanz-Administratoren, siehe [Plugins verwalten](../admin/plugins.md#herkunft-von-core-plugins)):

- `POST /api/v1/admin/plugins/origin` mit `{"source": "…"}` prüft eine Quelle vor dem Installieren und antwortet `{"level": "official"|"organisation"|"external", "source": "…", "trusted": "…", "allowed": true}`.
- `POST /api/v1/admin/plugins/install` und `POST /api/v1/admin/updates/{id}` nehmen `"accept_external": true` an. Fehlt die Bestätigung bei einer externen Quelle, antwortet das Wiki mit 400 und dem Code `external_not_accepted`; sind externe Quellen ausgeschaltet, mit 403. Die Antwort der Installation enthält `origin` (Stufe, Quelle, Commit, wer installiert hat). `GET /api/v1/admin/plugins` liefert `origins` je Plugin-ID.
- `GET /api/v1/admin/plugins/sources` liefert `{"sources": [{"prefix", "note"}], "allow_external": true}`. `POST …/sources` mit `{"prefix", "note"}` trägt eine Quelle ein, `DELETE …/sources?prefix=…` entfernt sie, `PUT …/sources/external` mit `{"allow": false}` schaltet externe Quellen aus. Eintragen und Erlauben brauchen eine Browser-Sitzung mit Anmeldung in den letzten 15 Minuten (sonst 403 mit `reauth_required`) oder die Kommandozeile; API-Schlüssel werden abgelehnt.
- `GET /api/v1/admin/plugins/credentials` liefert die Zugangsdaten für Plugin-Quellen ohne Token: `{"credentials": [{"prefix", "username", "note", "token_set", "from_env"}]}`. `PUT …/credentials` mit `{"prefix", "username", "token", "note"}` setzt sie für alle Quellen unter einer https-Adresse (ersetzt einen Eintrag mit demselben Präfix; Benutzername ohne Angabe `x-access-token`), `DELETE …/credentials?prefix=…` entfernt sie. Das Token wird nie wieder ausgegeben, auch nicht im Audit-Protokoll. Setzen braucht eine frische Anmeldung wie oben. Verweise auf Umgebungsvariablen (`${…}`) werden abgelehnt (400); die gibt es nur in `config.yaml`.

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
| `GET` | `/api/v1/x/content.team/groups` |
| `GET` | `/api/v1/x/content.team/fields` |
| `GET` | `/api/v1/x/content.team/elements/{id}/members` |
| `GET` | `/api/v1/x/core.attachment/elements/{id}/file` |
| `GET` | `/api/v1/x/core.blocks/blocks` |
| `GET` | `/api/v1/x/core.blocks/blocks/{id}/placements` |
| `GET` | `/api/v1/x/core.carousel/shown` |
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
| `GET` | `/api/v1/x/core.favorites/` |
| `POST` | `/api/v1/x/core.favorites/` |
| `POST` | `/api/v1/x/core.html/edit-html` |
| `POST` | `/api/v1/x/core.html/tasks/{id}/{n}` |
| `POST` | `/api/v1/x/core.html/to-markdown` |
| `GET` | `/api/v1/x/core.menu/menus` |
| `GET` | `/api/v1/x/core.menu/menus/{id}` |
| `POST` | `/api/v1/x/core.menu/menus` |
| `PUT` | `/api/v1/x/core.menu/menus/{id}` |
| `DELETE` | `/api/v1/x/core.menu/menus/{id}` |
| `GET` | `/api/v1/x/core.properties/types` |
| `POST` | `/api/v1/x/core.properties/trend` |
| `GET` | `/api/v1/x/core.properties/defs` |
| `POST` | `/api/v1/x/core.properties/defs` |
| `PUT` | `/api/v1/x/core.properties/defs/{id}` |
| `DELETE` | `/api/v1/x/core.properties/defs/{id}` |
| `GET` | `/api/v1/x/core.properties/databases/{id}/views` |
| `POST` | `/api/v1/x/core.properties/databases/{id}/views` |
| `PUT` | `/api/v1/x/core.properties/settings/{id}` |
| `GET` | `/api/v1/x/core.properties/subscriptions` |
| `POST` | `/api/v1/x/core.properties/subscriptions` |
| `DELETE` | `/api/v1/x/core.properties/subscriptions/{id}` |
| `POST` | `/api/v1/x/core.properties/query` |
| `POST` | `/api/v1/x/core.properties/query.csv` |
| `POST` | `/api/v1/x/core.properties/query.xlsx` |
| `POST` | `/api/v1/x/core.properties/report` |
| `GET` | `/api/v1/x/core.properties/values` |
| `DELETE` | `/api/v1/x/core.properties/views/{id}` |
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
| `GET` | `/api/v1/x/core.waf/status` |
| `GET` | `/api/v1/x/security.crowdsec/status` |
| `GET` | `/api/v1/x/security.crowdsec/alerts` |
| `GET` | `/api/v1/x/security.crowdsec/decisions` |
| `DELETE` | `/api/v1/x/security.crowdsec/decisions/{id}` |
| `GET` | `/api/v1/x/security.crowdsec/allowlist` |
| `PUT` | `/api/v1/x/security.crowdsec/allowlist` |
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
| `GET` | `/api/v1/x/content.forms/submissions/recent` |
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
