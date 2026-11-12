# Architektur

```
cmd/ewiki/            Einstiegspunkt (Server + CLI in einer Binärdatei)
internal/
  app/                Verdrahtung: Konfiguration, DB, Dienste, Plugins
  config/             config.yaml (config-first), .env, ${VAR}-Auflösung, atomisches Schreiben
  db/                 pgx-Pool, Migrations-Runner, migrations/ (Kern-Schema)
  identity/           Principal (Aufrufer) im Request-Kontext
  auth/               Anmeldeverfahren-Kette, Sessions, API-Keys, SSH-Signaturen, Benutzer/Gruppen/Rollen
  acl/                Berechtigungsprüfung (Snapshot im Speicher)
  content/            Kategorien, Elemente, Versionen, Übersetzungen, Papierkorb, Rollback, Suche, Tags
  i18n/               UI-Texte aus Kern + Plugins, Übersetzungsstand, Übersetzungen aus der Oberfläche
  storage/            inhaltsadressierte Dateiablage, sichere Auslieferung, PDF-/Text-Extraktion
  events/             Cache-Invalidierung (in-process bzw. PostgreSQL LISTEN/NOTIFY)
  audit/              unveränderliches Änderungsprotokoll
  kernel/             Aktivierungszustand der Plugins, Abhängigkeiten
  pluginmgr/          Plugins installieren/entfernen, Neubau, Neustart
  server/             REST-API, Sicherheits-Middleware, Auslieferung des Frontends
  cli/                Befehle (serve, setup, api, user, plugin, config, …)
pkg/sdk/              Plugin-API (Go)
plugins/              mitgelieferte Plugins (je Plugin ein Go-Paket + optional web/, locales/, migrations/)
  all/zz_generated.go importiert alle Plugins (generiert)
web/                  Frontend (TypeScript, ohne Framework, esbuild), dist/ wird eingebettet
  src/sdk.ts          Plugin-API (Frontend)
scripts/              build, install, update, SSH-Login
```

## Request-Ablauf

1. `server.wrap` setzt Sicherheits-Header (CSP usw.) und begrenzt die Request-Größe.
2. Die Authentifizierung über Bearer-Token (API-Key `ewk_…` oder SSH-Token) oder Session-Cookie liefert einen `identity.Principal`. Bei Cookie-Anmeldung prüft sie für schreibende Methoden den CSRF-Header.
3. Der Handler ruft einen Dienst auf (`content`, `auth`, …). **Jeder Dienst prüft die Berechtigung selbst.** API, CLI (`ewiki api`) und Plugins können sie also nicht umgehen.
4. Fehler sind typisiert (`apperr`) und werden einheitlich als `{"error": {"code", "message"}}` ausgegeben.

## Datenmodell (Kern)

| Tabelle | Inhalt |
|---|---|
| `categories` | stabile `id`, `parent_id`, `path` (ltree aus Vorfahren-IDs, z. B. `1.7.42`), `position`, mehrsprachige `titles`/`descriptions` (JSONB), `tags`, `default_element_id`, Papierkorb-Felder |
| `elements` | `category_id`, `type` (Plugin-Elementtyp), `original_lang`, Titel-Cache je Sprache, `tags`, Papierkorb-Felder |
| `element_revisions` | unveränderliche Versionen je Element **und Sprache** (`rev`). Übersetzungen speichern `source_rev` (auf welcher Original-Version sie beruhen) und daraus ergibt sich der Status *aktuell/veraltet* |
| `search_docs` | Volltextindex (tsvector: Titel A, Tags B, Inhalt C) je Element und Sprache |
| `acl_rules` | Kategorie, Subjekt (user/group/role/guest/authenticated), Rechte-Bitmaske, `inherit` |
| `users`, `groups`, `roles`, `user_groups`, `user_roles`, `group_roles` | Identitäten (eindeutig je `provider` + `subject`). Mitgliedschaften tragen ihre Quelle (`local` oder Anbieter), damit die Synchronisation lokale Zuweisungen nicht berührt |
| `sessions`, `api_keys`, `ssh_keys`, `auth_challenges` | Anmeldung (Tokens nur als SHA-256) |
| `audit_log` | jede Änderung mit Vorher/Nachher-Snapshot. Daraus werden auch Kategorien zurückgesetzt |
| `ui_translations` | in der Oberfläche gepflegte UI-Übersetzungen |
| `files` | hochgeladene Dateien (SHA-256, Größe, aus dem Inhalt bestimmter Typ, Name) |
| `sso_states` | laufende SSO-Anmeldungen (State, Nonce, PKCE-Verifier, Rücksprungziel), einmal verwendbar, 10 Minuten gültig |
| `schema_migrations` | angewendete Migrationen je Quelle (Kern bzw. `plugin:<id>`) |
| `change_events` | Änderungen, deren Folgearbeit (Benachrichtigungen an Beobachter und Erwähnte) noch nicht abgeschlossen ist; mit der Änderung geschrieben, nach Erfolg entfernt, sonst vom Worker `changes` wiederholt |

Die Gliederungsnummer (1.2.3) wird aus den Positionen berechnet. Die IDs bleiben dagegen beim Verschieben stabil, daher brechen Rechte, Links und API-Keys nie.

## Versionierung und „nichts geht verloren“

- Speichern, Wiederherstellen und Zurücksetzen **fügen immer neue Versionen hinzu**.
- Löschen bedeutet Papierkorb (`deleted_at`). Endgültig gelöscht wird nur durch Admin-Aktion oder die Aufbewahrungsregel. Die aktuelle Version wird nie entfernt.
- Optimistisches Sperren (`base_rev`) verhindert das Überschreiben paralleler Änderungen.

## Berechtigungs-Cache

`acl.Service` hält einen unveränderlichen Snapshot aller Regeln und Kategorie-Pfade (`atomic.Pointer`). Bei Änderungen wird er verworfen und bei der nächsten Prüfung neu geladen. Im Cluster-Modus geschieht das über `events.Bus` (PostgreSQL `NOTIFY`) auf allen Instanzen. Prüfungen kosten damit Mikrosekunden und keine Datenbankabfrage.

## Frontend

- TypeScript ohne Framework. `core/dom.ts` erzeugt Elemente über DOM-APIs (XSS-sicher), `core/router.ts` ist ein History-Router mit Fokus-Management, `components/ui.ts` enthält barrierefreie Bausteine (Felder mit Label und Fehlertext, native `<dialog>`).
- Das Template bestimmt das Seitenlayout und bietet **Positionen** (`header-nav`, `header-tools`, `sidebar`, `footer`) an. Kern und Plugins registrieren dort Widgets (Suche, Sprachwahl, Seitenbaum, Menüs).
- `web/build.mjs` findet alle `plugins/*/web/index.ts` und bündelt sie mit. Aktiviert werden nur die Plugins, die der Server als aktiv meldet (`GET /api/v1/site`).
- Alle Texte kommen von der API (`/api/v1/i18n/{lang}`), damit jeder Text übersetzbar ist.

## Konfiguration

`config.Store` hält zwei Fassungen: die Rohfassung (mit `${VAR}`-Referenzen, wird zurückgeschrieben) und die aufgelöste Fassung (wird verwendet). Updates prüfen, schreiben atomar und laden neu. Listener (z. B. der Plugin-Kernel) reagieren sofort.
