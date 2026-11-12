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
| `trusted_proxies` | IPs/CIDRs der Reverse-Proxys: nur von ihnen wird die weitergereichte Adresse der Besucher angenommen (für Anfragebegrenzung, Anmeldeschutz, Audit-Protokoll, erlaubte Netze); `EWIKI_TRUSTED_PROXIES` überschreibt den Wert | `[]` |
| `client_ip_header` | Header, in dem die Proxys die Adresse der Besucher nennen: `x-forwarded-for`, `forwarded` (RFC 7239) oder der Name eines Headers mit genau einer Adresse (`x-real-ip`, `cf-connecting-ip` …). Siehe [Adresse der Besucher](installation.md#vertrauenswürdige-proxys-ewiki_trusted_proxies) | `x-forwarded-for` |
| `max_body_mb` | ältere Einstellung für die Größe von Importen; ersetzt durch den Grenzwert `core.request.import_mb` (siehe [Experteneinstellungen](#limits-experteneinstellungen)) | `64` |
| `rate_limit.disabled` | `true` schaltet die Anfragegrenzen ab (nur für Lasttests oder mit Grenzen im Reverse Proxy) | `false` |

Hinter einem Reverse Proxy unbedingt `trusted_proxies` setzen, sonst teilen sich alle Benutzer die Grenzen der Proxy-Adresse.

### `mode`
`single` (eine Instanz) oder `cluster` (mehrere Instanzen, siehe [installation.md](installation.md#mehrere-instanzen-cluster-modus)).

### `database`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `url` | PostgreSQL-URL, normalerweise `${EWIKI_DATABASE_URL}` | `${EWIKI_DATABASE_URL}` |
| `max_conns` | maximale Verbindungen dieser Instanz | `50` |

Eine Transaktion, die länger als 60 Sekunden auf die Anwendung wartet, beendet PostgreSQL (`idle_in_transaction_session_timeout`); so kann der Verbindungspool nie dauerhaft blockieren. Ein eigener Wert in der Datenbank-URL (`?idle_in_transaction_session_timeout=…`) hat Vorrang.

### `storage`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `path` | Verzeichnis für hochgeladene Dateien (`<path>/files/ab/cd/<sha256>`). Im Cluster-Modus ein gemeinsames Volume | `./data` |
| `max_upload_mb` | maximale Größe einer hochgeladenen Datei | `100` |

Für die beste PDF-Texterkennung installieren Sie `pdftotext` (Paket `poppler-utils`, im Docker-Image enthalten). Ohne das Tool greift ein eingebauter Extraktor für einfache PDFs. Dateien, die keine Version mehr referenziert, werden nach einem Tag automatisch gelöscht.

### `media`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `viewer` | Standard-Anzeige für Dateien: `internal` (Browser) oder ein aktives Office-Plugin (`office.collabora`, `office.onlyoffice`, `office.m365`) | `internal` |
| `open_external` | Dateien, die das Standard-Programm kann, sofort dort öffnen (nur mit einem Office-Plugin als `viewer`) | `false` |
| `token_ttl` | Gültigkeit des Zugangsschlüssels eines Office-Programms für eine Datei (1m–24h) | `10h` |

Details: [Office-Programme](office.md).

### `i18n`
| Schlüssel | Bedeutung |
|---|---|
| `languages` | aktive Sprachen (Codes wie `de`, `en`, `pt-BR`). Gilt für Oberfläche und Inhalte |
| `default_language` | Standardsprache, muss in `languages` enthalten sein |

Sprachen werden in der Oberfläche unter *Administration → Sprachen* hinzugefügt oder entfernt (Recht *Sprachen verwalten und Oberfläche übersetzen*) oder mit `ewiki i18n add|remove CODE`. Für jede konfigurierte Sprache gibt es automatisch das Recht *Oberfläche übersetzen: <Sprache>* (`core.i18n.translate.<code>`, z. B. `core.i18n.translate.pt_br`), das Sie Rollen zuweisen, siehe [Übersetzungen der Oberfläche](security.md#übersetzungen-der-oberfläche).

### `ui`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `site_name` | Name der Website | `Enterprise Wiki` |
| `template` | aktives Template (Plugin-ID): `theme.wiki` (Dokumentation) oder `theme.cms` (Website), auch unter *Administration → Einstellungen* | `theme.wiki` |
| `home_element` | Element-ID der Startseite (z. B. eine Seite oder eine Kategorieübersicht), hat Vorrang vor `home_category`. Neue Installationen: die Willkommensseite. | `0` |
| `home_category` | Kategorie-ID der Startseite (`0` = Liste der Bereiche bzw. der einzige Bereich) | `0` |
| `prerender` | Seiten für Gäste (Suchmaschinen, Link-Vorschauen, Besucher ohne JavaScript) mit Titel, Beschreibung, kanonischer Adresse, Open-Graph-Angaben und dem Inhalt als HTML ausliefern. Immer aus Sicht eines Gastes; mit eingeschaltetem `cache` ohne Datenbankzugriff. Prüfen: *Administration → Einstellungen → Gast-Ansicht anzeigen* oder `ewiki api GET "/api/v1/prerender?path=/kalender"`. | `true` |
| `readable_urls` | sprechende Adressen `/wiki/<Kategorien>/<Seite>`, je Sprache eine; frühere Adressen leiten weiter (301). `false`: nur `/e/12`, `/c/7` und Kurz-URLs | `true` |
| `editor` | Standard-Editor für Seiten: `visual` (ProseMirror, mit Live-Bearbeitung) oder `simple` (einfaches Eingabefeld). Benutzer können im Profil abweichen (`ewiki pref set editor simple --as NAME`). Beide speichern Markdown. | `visual` |
| `collaboration` | Live-Bearbeitung: mehrere Personen bearbeiten eine Seite gleichzeitig im visuellen Editor (WebSocket `/api/v1/collab/…`, nur mit Bearbeitungsrecht, Sitzung und Recht werden während der Verbindung alle 30 s geprüft). Im Cluster-Betrieb sehen sich nur Personen, die mit demselben Knoten verbunden sind – Reverse Proxy mit Session-Affinität für `/api/v1/collab/` verwenden. | `true` |
| `preview_image` | Vorschaubild für Link-Vorschauen (`og:image`) von Seiten ohne eigenes Bild: Pfad wie `/api/v1/files/<hash>` oder http(s)-Adresse, auch unter *Administration → Einstellungen* hochladbar. Seiten verwenden zuerst ihr eigenes Vorschaubild (*Eigenschaften*), dann ihr erstes Bild. | leer |

### `auth`
| Schlüssel | Bedeutung | Standard |
|---|---|---|
| `order` | Reihenfolge der Anmeldeverfahren (Provider-IDs). `local` wird **immer zuerst** geprüft | `[local]` |
| `session_ttl` | Gültigkeit einer Browser-Sitzung (absolut) | `168h` |
| `session_idle` | Eine Browser-Sitzung endet nach so langer Inaktivität. Automatische Abfragen (z. B. Benachrichtigungen) zählen nicht als Aktivität. Die letzte Aktivität wird höchstens einmal pro Minute gespeichert (kein Schreibzugriff je Anfrage): Eine Sitzung kann deshalb bis zu eine Minute früher enden, nie später. `0` = nur `session_ttl` | `12h` |
| `photo_visibility` | Wer Profilfotos sieht, solange die Person nichts wählt: `nobody` (nur die Person), `users` (angemeldete Personen), `public` (alle, auch Gäste). Wer das globale Recht *Alle Profilfotos sehen* (`core.photos.view`) hat, sieht jedes Foto (Administratoren immer) | `users` |
| `photo_visibility_max` | Am weitesten darf eine Person ihr Foto so freigeben. Enger geht immer | `public` |
| `photo_visibility_fixed` | `true`: keine Wahl, es gilt immer `photo_visibility` | `false` |
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
| `mail_days` | gesendete E-Mails nach so vielen Tagen aus dem Postausgang löschen (`0` = nie) | `90` |

Die jeweils aktuelle Version wird nie gelöscht. Gelöscht wird nur aus dem Papierkorb. Die Regeln laufen stündlich oder sofort über *Einstellungen → Aufbewahrung jetzt anwenden* bzw. `ewiki retention run`.

### `plugins`
| Schlüssel | Bedeutung |
|---|---|
| `disabled` | deaktivierte Plugin-IDs (Pflicht-Plugins wie `auth.local` lassen sich nicht deaktivieren) |
| `dir` | Plugin-Verzeichnis im Quellbaum |
| `catalog` | Plugins, die unter *Plugins* mit einem Klick installiert werden können (`id`, `name`, `repo`, optional `ref`, `description`) |
| `macros.*` | Sicherheitsschalter für Shell- und HTTP-Makro, siehe unten. **Nur in dieser Datei änderbar**, nicht über Oberfläche oder API |
| `auto_restart_on_drop` | nach dem automatischen Bau von Plugins, die in den Plugin-Ordner kopiert wurden, sofort neu starten (Standard `false`: *Administration → Plugins* bietet **Neu starten** an); auch unter *Administration → Plugins* einstellbar |
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
- **HTTP-Makro und Workflow-Webhooks:** Nur URLs unter einem Präfix aus `http_allow` (gleiches Schema, gleicher Host, Pfad-Präfix). Verbindungen zu internen Adressen werden nach der DNS-Auflösung blockiert, solange `http_allow_private` nicht gesetzt ist. Mit `http_allow_private: true` gilt nur noch die Allow-Liste. Ein Proxy aus `HTTPS_PROXY` wird nur dann verwendet, wenn interne Adressen erlaubt sind (sonst ließe sich die Sperre über den Proxy umgehen).

### `mail`
Ausgehende E-Mails (Benachrichtigungen, Workflow-E-Mails). Einstellbar auch unter *Administration → E-Mail*, dort gibt es eine Testnachricht, den Postausgang und „jetzt senden“.

```yaml
mail:
  enabled: true              # false: E-Mail komplett abgeschaltet (Tests, Installationen ohne Mailversand)
  transport: auto            # auto | smtp | sendmail | log | none
  from: wiki@example.com     # Absenderadresse (muss über den Mailserver senden dürfen: SPF/DKIM)
  from_name: Enterprise Wiki
  reply_to: ""               # optionale Antwortadresse
  subject_prefix: "[Wiki]"   # optional
  allowed_from:              # weitere Absender für Workflow-E-Mails
    - "*@hr.example.com"
  concurrency: 4             # parallele SMTP-Verbindungen (1–64)
  rate_per_minute: 0         # Obergrenze Empfänger pro Minute (0 = unbegrenzt), falls der Provider drosselt
  smtp:
    host: smtp.example.com
    port: 587                # leer: 587 (starttls), 465 (tls), 25 (none)
    security: starttls       # starttls | tls | none
    username: wiki@example.com
    password: ${EWIKI_SMTP_PASSWORD}   # Wert in .env
    skip_verify: false       # nur für interne Relays mit selbst signiertem Zertifikat
  sendmail: /usr/sbin/sendmail
  bounce:                    # optional: Rückläufer-Postfach (siehe unten)
    address: bounces@example.com
    verp: true               # bounces+m12r34@example.com: ordnet Rückläufer eindeutig zu
    imap_host: imap.example.com
    imap_port: 993
    security: tls            # tls | starttls | none
    username: bounces@example.com
    password: ${EWIKI_BOUNCE_PASSWORD}
    folder: INBOX
    delete_processed: false  # verarbeitete Rückläufer löschen statt nur als gelesen markieren
```

**Zuverlässiger Versand (Warteschlange).** Jede Nachricht landet zuerst im Postausgang, und zwar in derselben Transaktion wie die Änderung, die sie auslöst. Es geht also nichts verloren, auch nicht bei einem Absturz. Pro Empfänger gibt es eine eigene Zeile (`mail_recipients`) mit Status, Versuchen und letzter Antwort des Mailservers:

- Ein Empfänger gilt erst als *versendet*, wenn der Mailserver ihn mit `250` angenommen hat.
- **Vorübergehende Fehler** (4xx, Verbindungsabbruch, Zeitüberschreitung, Server nicht erreichbar) werden **ohne Limit** wiederholt: nach 1, 2, 5, 15 und 30 Minuten, danach stündlich. Die Nachricht bleibt so lange im Postausgang.
- **Endgültige Fehler** (5xx, z. B. „Postfach existiert nicht“) und Rückläufer aus dem Bounce-Postfach markieren nur diesen Empfänger als *fehlgeschlagen*. Der Absender bzw. die auslösende Person (z. B. wer eine Seite geändert oder einen Workflow-Schritt ausgeführt hat) bekommt eine Meldung mit den betroffenen Adressen und dem Grund.
- Mehrere Server-Instanzen können gleichzeitig senden: Nachrichten werden mit `FOR UPDATE SKIP LOCKED` und einer Lease reserviert. Bricht ein Prozess ab, übernimmt nach Ablauf der Lease ein anderer.
- Der Versand nutzt eine SMTP-Verbindung für viele Nachrichten und `concurrency` parallele Verbindungen. Neue Nachrichten werden sofort angestoßen, ein Hintergrundjob prüft zusätzlich alle 10 Sekunden.

Mit `transport: auto` gilt:

1. Ist `smtp.host` gesetzt, wird über diesen SMTP-Server versendet (empfohlen: der Mailserver bzw. Relay Ihrer Organisation, Microsoft 365, Google Workspace, Mailjet …).
2. Sonst wird das `sendmail`-Programm des Servers verwendet, falls vorhanden (z. B. Postfix oder msmtp, das die Mails selbst weiterleitet).
3. Sonst wird **nichts versendet**: Die Nachrichten bleiben als „zurückgehalten“ im Postausgang. Sobald ein Versandweg eingerichtet ist, werden zurückgehaltene Nachrichten der letzten 3 Tage automatisch versendet, ältere erst nach Bestätigung unter *Administration → E-Mail*. Ein direkter Versand an die Mailserver der Empfänger (ohne Relay) ist bewusst nicht eingebaut: Er landet fast immer im Spam oder wird blockiert (Port 25, fehlendes SPF/DKIM).

`log` schreibt Nachrichten nur ins Server-Log (Test), `none` behält alles im Postausgang. Mit `enabled: false` werden **keine** Nachrichten erzeugt (auch nicht im Postausgang). Benachrichtigungen, Beobachtungen und Workflow-E-Mails entfallen dann, und die Oberfläche weist darauf hin.

**Rückläufer (Bounces).** Viele Mailserver nehmen eine Nachricht zuerst an und melden erst später per E-Mail, dass sie nicht zustellbar war. Mit `bounce` wird als Absender im Umschlag (Return-Path) die Bounce-Adresse verwendet, mit `verp: true` je Empfänger eindeutig (`bounces+m<Nachricht>r<Empfänger>@…`). Das Wiki liest das Postfach alle 5 Minuten per IMAP, erkennt Zustellberichte (DSN, RFC 3464) und markiert den Empfänger als fehlgeschlagen; der Absender wird wie bei 5xx informiert. Die Bounce-Adresse muss beim Mailserver als Absender erlaubt sein (bei VERP: Plus-Adressen bzw. Catch-all für `bounces+*`).

Absender: Workflow-E-Mails dürfen nur `from` oder Adressen aus `allowed_from` als Absender verwenden; andere gewünschte Absender werden als Antwortadresse (Reply-To) gesetzt, damit keine fremden Absender gefälscht werden und SPF/DMARC nicht scheitern.

**Zusammenfassungen von Beobachtungen.** Beobachtungen mit `frequency: daily` bzw. `weekly` werden einmal täglich um `digest_hour` Uhr bzw. wöchentlich am `digest_weekday` (0 = Sonntag, 1 = Montag …) verschickt, in der Zeitzone `digest_timezone` (IANA-Name, leer = Zeitzone des Servers). Standard: 7 Uhr, Montag.

```yaml
mail:
  digest_hour: 7
  digest_weekday: 1
  digest_timezone: Europe/Vienna
```

Im Docker-Container werden beim ersten Start `EWIKI_MAIL_FROM`, `EWIKI_SMTP_HOST`, `EWIKI_SMTP_PORT`, `EWIKI_SMTP_SECURITY`, `EWIKI_SMTP_USER` und `EWIKI_SMTP_PASSWORD` übernommen (siehe `.env.example`).

### `cache`
Optionaler Cache für Gäste (nicht angemeldete Besucher). Einstellbar auch unter *Administration → Cache*.

```yaml
cache:
  enabled: false     # einschalten für öffentliche Wikis/Websites mit vielen Gästen
  ttl_seconds: 300   # Höchstalter eines Eintrags (0 = bis zur nächsten Änderung)
  max_entries: 5000  # 0 = unbegrenzt
  max_mb: 128        # Speicherobergrenze (0 = unbegrenzt)
```

Für Gäste werden die API-Antworten im Arbeitsspeicher gehalten. Beim Aufruf einer Seite bettet der Server die Daten ein, die diese Seite beim letzten Mal gebraucht hat. Die Seite erscheint dann mit **einer** Anfrage und ohne Datenbankzugriff. Angemeldete Personen sehen immer die aktuellen Daten.

**Jede Änderung leert den Cache**: Seiten, Kategorien, Menüs, Blöcke, Rechte, Benutzer und Gruppen, Einstellungen, Plugins, Übersetzungen, Termine. Grund sind Makros: Eine Seite kann Listen anderer Seiten, Termine oder Teammitglieder zeigen, darum wäre ein teilweises Leeren unsicher. Im Cluster und bei Änderungen über die Kommandozeile sorgt ein Zähler in der Datenbank (`cache_state`) dafür, dass alle Server ihren Cache innerhalb von 3 Sekunden leeren. Dieser Zähler wird unabhängig vom Besucheraufkommen abgefragt. `ttl_seconds` begrenzt zeitabhängige Inhalte wie „Nächste Termine“.

Kommandozeile: `ewiki cache status`, `ewiki cache clear`. API: `GET/DELETE /api/v1/admin/cache`.

### `secrets`

Wie Geheimnisse in der Datenbank gespeichert werden (Details: [Sicherheit](security.md#geheimnisse-in-der-datenbank)).

```yaml
secrets:
  mode: pq                     # plain (Vorgabe) | aes | pq
  key_file: secrets.key        # relativ zu config.yaml oder absolut
  password_env: EWIKI_SECRETS_PASSWORD   # optional: Passwort der Schlüsseldatei
```

### `license`
Lizenzschlüssel der Organisation (siehe [LICENSING.md](../../LICENSING.md)). Am einfachsten unter *Administration → Lizenz*
oder mit `ewiki license install KEY` einspielen; die Prüfung erfolgt offline.

```yaml
license:
  key: EWL1.eyJpZCI6…   # oder ${EWIKI_LICENSE} mit dem Wert in .env
```

### `feeds`
Externe RSS/Atom-Feeds (Makro „Neuigkeiten aus mehreren Quellen“) und iCal-Abonnements (weitere Quellen eines Kalenders, Makro „Nächste Termine“) werden im Hintergrund alle 10 Minuten abgerufen und in der Datenbank zwischengespeichert; Seiten warten nie auf eine fremde Adresse. Höchstens 2 MB je Abruf, nur `http(s)`, Umleitungen nur auf öffentliche Adressen, Abrufe, die 30 Tage niemand mehr gebraucht hat, werden vergessen.

```yaml
feeds:
  allow_private: false  # true erlaubt Feeds und Kalender im internen Netz (10.x, 192.168.x, localhost …)
```

Die Feeds des Karussells haben eine eigene Einstellung im Plugin (*Administration → Plugins → Karussell*).

### `lists`
Obergrenze für Listen aus Quellen (Makros Neuigkeiten, Blog, FAQ, Eigenschaften-Bericht …) und für Such- und Filterquellen. Bearbeitende wählen darunter beim Makro selbst die Höchstzahl und ob ab einer Anzahl seitenweise geblättert wird. Auch unter *Administration → Einstellungen → Listen*.

```yaml
lists:
  max_items: 0  # 0 = 1000; 50 bis 10000
```

### `webhooks`
Webhooks selbst werden unter *Administration → Webhooks* (API `/api/v1/admin/webhooks`) angelegt. In `config.yaml` stehen nur:

```yaml
webhooks:
  allow_private: false  # true erlaubt Ziele im internen Netz (10.x, 192.168.x, localhost …)
  keep_days: 30         # zugestellte und fehlgeschlagene Zustellungen so lange aufbewahren
```

Formate: **Teams** (Adaptive Card; in Teams einen Workflow „Bei Empfang einer Teams-Webhookanforderung in einem Kanal posten“ anlegen), **Slack** (Incoming Webhook) und **JSON** für eigene Systeme:

```json
{"event": "element.update", "occurred_at": "2026-11-02T16:30:00Z", "site": "Intranet", "actor": "Anna Muster",
 "object": {"type": "element", "id": 12, "title": "Reisekosten", "url": "https://wiki.example.com/reisekosten"}, "data": {}}
```

Kopfzeilen: `X-Ewiki-Event`, `X-Ewiki-Delivery` und mit Signaturschlüssel `X-Ewiki-Signature: sha256=<HMAC-SHA256 des Inhalts>`. Ereignisse: `element.create|update|delete|move|restore|rollback`, `category.create|update|delete|move|restore`, `event.registration`, `event.registration_cancelled`, `event.occurrence_changed` (Daten: `occurrence`, `start`, `when`, `calendar`, `location`, `status`, `participant`), `comment.create` (Daten: `author`, `excerpt`, `reply`, `element_id`), `workflow.started`, `workflow.completed`, `workflow.cancelled` (Daten: `workflow`, `status`, `outcome`, `step`). Zustellungen laufen über eine Warteschlange; bei Fehlern wird nach 1, 5, 30 Minuten, 2 und 12 Stunden erneut versucht. Ziele im internen Netz sind ohne `allow_private` gesperrt (Schutz vor SSRF), Umleitungen nur innerhalb der Webhook-Adresse.

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
| `search.external` | `engine`, `url`, `index`, `api_key`, `username`, `password` | externe Suchmaschine, siehe [plugins.md](plugins.md#externe-suchmaschine) |
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

Weitere Sites einer Instanz haben eigene Einstellungsdateien (`sites/<name>.yaml`), siehe [sites.md](sites.md).

### `limits` (Experteneinstellungen)

Technische Grenzwerte des Kerns und aller Plugins. Jeder hat eine Vorgabe und einen erlaubten Bereich; in `config.yaml` steht nur, was von der Vorgabe abweicht. Ändern unter **Administration → Experteneinstellungen** (alle Werte an einer Stelle, mit Erklärung, Vorgabe und Bereich; Änderungen stehen im Audit-Protokoll als `settings.limits`), mit `PUT /api/v1/admin/limits` oder direkt:

```yaml
limits:
  core.request.guest_kb: 128
  core.rate.user_per_second: 100
```

Die Grenzwerte gelten für die ganze Instanz; in weiteren Sites ändern sie nur Administratoren der Instanz. Plugins melden ihre eigenen Grenzwerte an (Schlüssel beginnt mit der Plugin-ID); sie erscheinen in derselben Liste.

| Schlüssel | Bedeutung | Vorgabe |
|---|---|---|
| `core.request.guest_kb` | größte Anfrage ohne Sitzung und ohne API-Key (Anmeldung, Anfragen von Gästen; Einsendungen von Formularen haben eine eigene Grenze) | 64 kB |
| `core.request.body_kb` | größte Anfrage angemeldeter Benutzer; Routen, die mehr brauchen, melden eine eigene Grenze an | 1024 kB |
| `core.request.content_kb` | Speichern von Seiten, Entwürfen, Vorlagen und Vorschauen | 4096 kB |
| `core.request.sso_kb` | Nachrichten von Identity Providern (SAML, OIDC) | 1024 kB |
| `core.request.import_mb` | Import von Kategorien und Übersetzungen | 64 MB |
| `core.request.large_parallel` | Anfragen über 1 MB, die zugleich verarbeitet werden (Uploads werden durchgereicht und zählen nicht) | 4 |
| `core.request.large_wait_s` | so lange wartet eine große Anfrage auf einen freien Platz, dann `503` (nichts gespeichert, wiederholbar) | 30 s |
| `core.rate.user_per_second`, `core.rate.user_burst` | Anfragen je Konto: Dauerrate und Vorrat für kurze Spitzen | 50 / 500 |
| `core.rate.guest_per_second`, `core.rate.guest_burst` | Anfragen je Adresse ohne Anmeldung | 20 / 200 |
| `core.rate.expensive_per_second`, `core.rate.expensive_burst` | zusätzlich für Suche, Vorschau, Export und Upload | 5 / 50 |
| `core.live.persons` | Personen, die eine Seite gleichzeitig gemeinsam bearbeiten | 50 |
| `core.live.connections_per_person` | Fenster oder Geräte einer Person an derselben Seite | 5 |
| `core.audit.search_days` | so weit reicht die Textsuche in den Vorher-/Nachher-Werten des Audit-Protokolls zurück, wenn kein Startdatum angegeben ist (mit Startdatum: jeder Zeitraum) | 90 Tage |
| `core.reactions.per_person` | Plugin *Reaktionen*: verschiedene Emojis einer Person je Ziel | 10 |
| `content.forms.submit_mb` | Plugin *Formulare*: größte Einsendung mit allen Dateien, auch von Gästen. Jedes Formular nimmt höchstens die Dateien an, die seine Felder erlauben (Anzahl × Größe), diese Grenze deckelt alle Formulare | 200 MB |
| `workflow.process.trigger_per_hour` | Plugin *Workflow*: Starts eines Auslösers pro Stunde (Zeitplan, Änderung, Ereignis, eingehender Webhook); weitere Starts entfallen und werden im Editor angezeigt | 60 |
| `core.audit.value_guest_bytes` | längster Wert im Audit-Protokoll bei Ereignissen ohne angemeldeten Benutzer | 1024 Bytes |
| `core.audit.value_bytes` | längster Vorher-/Nachher-Wert bei Ereignissen angemeldeter Benutzer | 65536 Bytes |
| `core.ui.wait_notice_s` | nach dieser Zeit ohne Antwort auf eine Änderung fragt die Oberfläche „weiter warten oder abbrechen“; die Eingabe bleibt in beiden Fällen erhalten | 5 s |

Zu große Anfragen erhalten `413`, zu viele `429` mit `Retry-After`.

**Grenze je API-Schlüssel.** Jeder API-Schlüssel kann eine eigene Grenze in Anfragen pro Sekunde haben:

- Wer einen Schlüssel besitzt, kann ihn beim Anlegen (*Mein Profil*) oder mit `PUT /api/v1/me/api-keys/{id}/rate {"rate_per_second": 5}` **unterhalb** der Grenze des Kontos begrenzen. Der Schlüssel zählt weiter auch für das Konto: Mehrere Schlüssel vervielfachen das Kontingent nicht.
- Administratoren vergeben unter **Administration → Experteneinstellungen → API-Schlüssel** (oder `PUT /api/v1/admin/api-keys/{id}/rate`) ein **eigenes Kontingent**, das auch über der Grenze des Kontos liegen darf, etwa für Integrationen. Ein solcher Schlüssel zählt nicht mehr für das Konto; die Person selbst kann den Wert nicht ändern.
- `{"rate_per_second": null}` entfernt die eigene Grenze. Der Vorrat für kurze Spitzen folgt dem Verhältnis der Konten (Vorgabe: das Zehnfache der Rate). Änderungen stehen im Audit-Protokoll (`apikey.rate`).
