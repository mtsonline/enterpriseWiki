# Konfiguration (`config.yaml`)

`config.yaml` ist die **einzige maßgebliche Quelle** aller Einstellungen:

- Die Admin-Oberfläche (*Administration → Einstellungen*, *Sprachen*, *Plugins*) und `ewiki config set` ändern ausschließlich diese Datei. Geschrieben wird atomar (temporäre Datei, dann umbenennen).
- Laufende Server laden die Datei automatisch neu, wenn sie sich ändert (auch bei manueller Bearbeitung), oder sofort per `systemctl reload enterprisewiki` (SIGHUP). Eine ungültige Datei wird abgelehnt, die vorherige Konfiguration bleibt dann aktiv. Die Fehlermeldung steht im Log.
- Werte können Umgebungsvariablen referenzieren: `${NAME}` oder `${NAME:-standard}`. Die Referenz bleibt beim Zurückschreiben erhalten, so landen **Geheimnisse nie in der Datei**. Variablen aus `.env` im selben Verzeichnis werden automatisch geladen. Bereits gesetzte Umgebungsvariablen haben Vorrang.
- Änderungen an `server`, `database` und `mode` erfordern einen Neustart.

Vollständiges Beispiel: [config.example.yaml](../../config.example.yaml)

## Referenz

### `server`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `listen` | Adresse/Port | `:8080` |
| `base_url` | öffentliche URL | `http://localhost:8080` |
| `secure_cookies` | Cookies nur über HTTPS und HSTS-Header senden | `false` |
| `trusted_proxies` | IPs/CIDRs, deren `X-Forwarded-For` vertraut wird (für Rate-Limits und Logs) | `[]` |
| `max_body_mb` | maximale Größe einer API-Anfrage | `64` |

### `mode`
`single` (eine Instanz) oder `cluster` (mehrere Instanzen, siehe [installation.md](installation.md#mehrere-instanzen-cluster-modus)).

### `database`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `url` | PostgreSQL-URL, normalerweise `${EWIKI_DATABASE_URL}` | `${EWIKI_DATABASE_URL}` |
| `max_conns` | maximale Verbindungen dieser Instanz | `50` |

### `storage`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `path` | Verzeichnis für hochgeladene Dateien (`<path>/files/ab/cd/<sha256>`). Im Cluster-Modus ein gemeinsames Volume | `./data` |
| `max_upload_mb` | maximale Größe einer hochgeladenen Datei | `100` |

Für die beste PDF-Texterkennung installieren Sie `pdftotext` (Paket `poppler-utils`, im Docker-Image enthalten). Ohne das Tool greift ein eingebauter Extraktor für einfache PDFs. Dateien, die keine Version mehr referenziert, werden nach einem Tag automatisch gelöscht.

### `i18n`
| Schlüssel | Bedeutung |
|---|---|
| `languages` | aktive Sprachen (Codes wie `de`, `en`, `pt-BR`). Gilt für Oberfläche und Inhalte |
| `default_language` | Standardsprache, muss in `languages` enthalten sein |

Sprachen werden in der Oberfläche unter *Administration → Sprachen & Übersetzungen* hinzugefügt oder entfernt.

### `ui`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `site_name` | Name der Website | `Enterprise Wiki` |
| `template` | aktives Template (Plugin-ID): `theme.confluence` (Dokumentation) oder `theme.cms` (Website), auch unter *Administration → Einstellungen* | `theme.confluence` |
| `home_category` | Kategorie-ID der Startseite (`0` = Liste der Bereiche bzw. der einzige Bereich) | `0` |

### `auth`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `order` | Reihenfolge der Anmeldeverfahren (Provider-IDs). `local` wird **immer zuerst** geprüft | `[local]` |
| `session_ttl` | Gültigkeit einer Browser-Sitzung | `168h` |
| `api_key_max_ttl` | maximale Gültigkeit eines API-Keys | `8760h` |
| `ssh_token_ttl` | Gültigkeit eines Tokens nach SSH-Login | `1h` |
| `max_login_failures` | Fehlversuche bis zur Sperre eines lokalen Kontos | `10` |
| `lockout_duration` | Sperrdauer | `15m` |
| `min_password_length` | Mindestlänge lokaler Passwörter (≥ 8) | `12` |

### `retention`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `revisions_keep_days` | ältere Versionen nach so vielen Tagen löschen (`0` = nie) | `0` |
| `revisions_keep_min` | je Seite und Sprache immer mindestens so viele Versionen behalten (≥ 1) | `50` |
| `trash_days` | Papierkorb-Einträge nach so vielen Tagen endgültig löschen (`0` = nie) | `30` |
| `audit_days` | Protokolleinträge nach so vielen Tagen löschen (`0` = nie) | `0` |

Die jeweils aktuelle Version wird nie gelöscht. Gelöscht wird nur aus dem Papierkorb. Die Regeln laufen stündlich oder sofort über *Einstellungen → Aufbewahrung jetzt anwenden* bzw. `ewiki retention run`.

### `plugins`
| Schlüssel | Bedeutung |
|---|---|
| `disabled` | deaktivierte Plugin-IDs (Pflicht-Plugins wie `auth.local` lassen sich nicht deaktivieren) |
| `dir` | Plugin-Verzeichnis im Quellbaum |
| `catalog` | Plugins, die unter *Plugins* mit einem Klick installiert werden können (`id`, `name`, `repo`, optional `ref`, `description`) |
| `macros.*` | Sicherheitsschalter für Shell- und HTTP-Makro, siehe unten. **Nur in dieser Datei änderbar**, nicht über Oberfläche oder API |
| `updates.check` | automatische Update-Prüfung beim Öffnen von *Administration → Updates* (Standard `true`) |
| `updates.core_repo` | Repository des Kerns (Standard `github.com/mtsonline/enterpriseWiki`) |
| `updates.api_url` | GitHub-API (Standard `https://api.github.com`, für GitHub Enterprise z. B. `https://github.example.com/api/v3`) |
| `updates.token` | optionales GitHub-Token für private Repositories und höhere Abfragegrenzen, z. B. `${EWIKI_GITHUB_TOKEN}` |

#### `plugins.macros`

```yaml
plugins:
  macros:
    shell_enabled: false          # Shell-Makro einschalten
    shell_approvers:              # Superadmins, die Befehle freigeben dürfen
      - admin@local
    shell_timeout: 10s            # höchstens 5m
    shell_user: nobody            # nur relevant, wenn das Wiki als root läuft
    http_allow:                   # URL-Präfixe für das HTTP-Makro
      - https://status.example.com/api/
    http_allow_private: false     # interne Adressen (10.x, 192.168.x, localhost, …) erlauben
```

- **Shell-Makro:** Ein Befehl läuft erst, wenn ein Superadmin genau diesen Befehlstext freigegeben hat (gebunden an den SHA-256-Hash). Jede Änderung braucht eine neue Freigabe. Freigeben dürfen nur angemeldete Administratoren, die in `shell_approvers` stehen (nicht per API-Key), sowie die lokale Kommandozeile: `ewiki api POST /api/v1/x/macro.shell/commands '{"command":"uptime","approve":true}'`. Offene und freigegebene Befehle stehen unter *Administration → Shell-Befehle*.
- **HTTP-Makro:** Nur URLs unter einem Präfix aus `http_allow` (gleiches Schema, gleicher Host, Pfad-Präfix). Verbindungen zu internen Adressen werden nach der DNS-Auflösung blockiert, solange `http_allow_private` nicht gesetzt ist. Mit `http_allow_private: true` gilt nur noch die Allow-Liste. Ein Proxy aus `HTTPS_PROXY` wird nur dann verwendet, wenn interne Adressen erlaubt sind (sonst ließe sich die Sperre über den Proxy umgehen).

### `plugin_settings`
Frei strukturierte Einstellungen je Plugin, z. B.

```yaml
plugin_settings:
  acme.kanban:
    columns: [todo, doing, done]
```

Plugins lesen sie über `Host.Settings()`. Setzen per CLI: `ewiki config set plugin_settings.acme.kanban.columns "[todo, done]"`.

Weitere Plugin-Einstellungen:

| Plugin | Schlüssel | Bedeutung |
|---|---|---|
| `core.comments` | `write_permission` | `view` (Standard): jeder angemeldete Benutzer mit Leserecht darf kommentieren; `edit`: nur mit Bearbeitungsrecht |

Die Einstellungen der Anmeldeverfahren (`auth.ldap`, `auth.oidc`, `auth.shib`) sind in [authentication.md](authentication.md) beschrieben.

## Kommandozeile

```bash
ewiki config show                         # Datei anzeigen
ewiki config get auth.session_ttl
ewiki config set retention.trash_days 60
ewiki config set i18n.languages "[de, en, fr]"
ewiki config validate
```
