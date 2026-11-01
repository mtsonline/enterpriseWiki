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
| `template` | aktives Template (Plugin-ID) | `theme.confluence` |
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
| `macros.shell_enabled`, `macros.http_allow` | Sicherheitsschalter für Makros (ab Meilenstein 3) |

### `plugin_settings`
Frei strukturierte Einstellungen je Plugin, z. B.

```yaml
plugin_settings:
  acme.kanban:
    columns: [todo, doing, done]
```

Plugins lesen sie über `Host.Settings()`. Setzen per CLI: `ewiki config set plugin_settings.acme.kanban.columns "[todo, done]"`.

Die Einstellungen der Anmeldeverfahren (`auth.ldap`, `auth.oidc`, `auth.shib`) sind in [authentication.md](authentication.md) beschrieben.

## Kommandozeile

```bash
ewiki config show                         # Datei anzeigen
ewiki config get auth.session_ttl
ewiki config set retention.trash_days 60
ewiki config set i18n.languages "[de, en, fr]"
ewiki config validate
```
