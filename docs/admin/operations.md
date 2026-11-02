# Betrieb: Updates, Backup, CLI, Wartung

## Updates

### systemd-Installation

```bash
sudo /opt/enterprisewiki/scripts/update.sh            # auf das neueste Release-Tag (v*)
sudo /opt/enterprisewiki/scripts/update.sh --ref v7.1.0
sudo /opt/enterprisewiki/scripts/update.sh --enable-timer   # wöchentlich automatisch (So 03:30 ± 1h)
```

Ablauf und Sicherheitsnetz:

1. neuen Stand holen (`git fetch --tags`), Ziel auschecken,
2. Frontend und Backend nach `bin/ewiki.new` bauen. Bei einem Fehler bleibt alles unverändert,
3. Selbsttest der neuen Binärdatei,
4. Datenbank-Backup mit `pg_dump` nach `backups/`,
5. Binärdatei tauschen (die alte bleibt als `bin/ewiki.prev`), Dienst neu starten,
6. Health-Check `/readyz`. Schlägt er fehl, werden alte Binärdatei und alter Quellstand **automatisch wiederhergestellt** und der Dienst neu gestartet.

Datenbank-Migrationen laufen beim Start automatisch, einmalig und in Transaktionen. Bei mehreren Instanzen verhindert eine Datenbank-Sperre parallele Migrationen. Migrationen fügen nur Strukturen hinzu, daher kann die vorige Version nach einem Rollback weiterlaufen. Für einen vollständigen Rückweg gibt es das Backup.

### Docker

```bash
git pull && docker compose up -d --build
```

## Backup und Wiederherstellung

Zu sichern sind:

| Was | Wo |
|---|---|
| Datenbank | `pg_dump --format=custom -f ewiki.dump "$EWIKI_DATABASE_URL"` |
| Konfiguration | `config.yaml` und `.env` (enthält Geheimnisse, verschlüsselt ablegen) |
| hochgeladene Dateien | Verzeichnis aus `storage.path` (inhaltsadressiert, Dateien ändern sich nie: inkrementelle Sicherung genügt) |
| eigene Plugins | `plugins/` im Quellbaum (bzw. das Git-Repository des Plugins) |

Wiederherstellen:

```bash
systemctl stop enterprisewiki
pg_restore --clean --if-exists -d "$EWIKI_DATABASE_URL" ewiki.dump
systemctl start enterprisewiki
```

## Kommandozeile (`ewiki`)

Die CLI ist dieselbe Binärdatei. Sie arbeitet direkt mit der Konfiguration und der Datenbank und läuft mit Superadmin-Rechten. Wer sie als Dienstbenutzer ausführen kann, hat volle Kontrolle. Mit `--config` bzw. `EWIKI_CONFIG` wählen Sie die Installation.

```text
ewiki serve | setup [--non-interactive] | migrate | version
ewiki api METHOD PATH [BODY|@datei.json|-] [--as BENUTZER]
ewiki user list|create|passwd|disable|enable|unlock ...
ewiki plugin list|enable|disable|install|remove|generate|build ...
ewiki config show|path|validate|get|set ...
ewiki retention run
ewiki mail status|list|show|retry|delete|test|send|bounces ...
ewiki jobs list | ewiki jobs run NAME
ewiki cache status|clear
ewiki watch list|add|remove ... --as BENUTZER
```

**Jede API-Funktion** steht über `ewiki api` zur Verfügung, auch die von Plugins:

```bash
ewiki api GET /api/v1/categories
ewiki api POST /api/v1/categories '{"titles":{"de":"Personal"},"parent_id":1}'
ewiki api GET /api/v1/x/core.menu/menus
ewiki api GET /api/v1/categories/1 --as anna        # mit den Rechten von anna@local
```

Typische Aufgaben:

```bash
ewiki user create anna --display-name "Anna Muster" --email anna@example.com   # fragt das Passwort ab
echo 'Neues-Passwort-42' | ewiki user passwd admin --password-stdin
ewiki user create notfall --admin --password-stdin < pw.txt   # lokaler Notfall-Admin
ewiki user unlock anna
ewiki plugin disable example.notice
ewiki mail status                       # Warteschlange: wartend, versendet, fehlgeschlagen, älteste wartende Nachricht
ewiki mail list --status failed         # fehlgeschlagene Nachrichten (mit Empfängern)
ewiki mail retry 123                    # fehlgeschlagene Empfänger erneut versuchen
ewiki mail bounces                      # Bounce-Postfach jetzt abrufen
ewiki cache clear                       # Gäste-Cache auf allen Servern leeren
ewiki watch add category 12 --mode content --as anna   # anna beobachtet Kategorie 12 (mit Inhalt)
ewiki api GET /api/v1/x/content.events/upcoming        # nächste Termine aller Kalender
ewiki api GET '/api/v1/elements?q=kalender&type=core.html'   # Elemente suchen (wie Administration → Elemente)
ewiki api PATCH /api/v1/elements/12 '{"short_url":"kalender"}' # Kurz-URL setzen
ewiki config set ui.home_element 12                    # Element 12 als Startseite
```

## Wartungsaufgaben

Stündlich läuft automatisch (im Cluster nur auf einer Instanz):

- abgelaufene Sessions, SSH-Challenges und SSO-Anfragen löschen,
- nicht mehr referenzierte Dateien entfernen (älter als ein Tag),
- Aufbewahrungsregeln anwenden (alte Versionen, Papierkorb, Protokoll, siehe [configuration.md](configuration.md#retention)).

Außerdem laufen Hintergrundjobs (Liste und „jetzt ausführen“ unter *Administration → E-Mail*, per API `GET /api/v1/admin/jobs`, `POST /api/v1/admin/jobs/{name}/run`):

| Job | Intervall | Aufgabe |
|---|---|---|
| `mail` | 10 s (und sofort bei neuen Nachrichten) | E-Mails aus dem Postausgang senden: pro Empfänger, vorübergehende Fehler ohne Limit wiederholen |
| `mail.bounces` | 5 min | Bounce-Postfach per IMAP lesen und Rückläufer zuordnen (nur wenn `mail.bounce` eingerichtet) |
| `watch` | 30 s | gesammelte Änderungen an beobachteten Kategorien und Seiten als eine E-Mail pro Person versenden (nach 2 Minuten Ruhe) |
| `content.events.reminders` | 1 min | Termin-Erinnerungen an angemeldete und interessierte Personen |
| `mail.cleanup` | 6 h | gesendete E-Mails nach `retention.mail_days` löschen |
| `search.changes.cleanup` | täglich | Änderungsprotokoll für externe Suchmaschinen kürzen (30 Tage) |
| `workflow.process.timers` | 30 s | Wartezeiten und Webhooks von Workflows fortsetzen, Erinnerungen für überfällige Aufgaben |
| `search.external.sync` | 10 s | externen Suchindex abgleichen (nur wenn konfiguriert) |

Im Cluster-Modus läuft jeder Job zur selben Zeit nur auf einer Instanz (Advisory Lock).

## Logs und Überwachung

- Logs gehen nach stderr, also in systemd ins Journal (`journalctl -u enterprisewiki -f`). JSON-Format mit `EWIKI_LOG_FORMAT=json`, mehr Details mit `EWIKI_LOG_LEVEL=debug`.
- `GET /healthz` meldet, dass der Prozess läuft. `GET /readyz` prüft zusätzlich die Datenbank (für Load Balancer und Kubernetes).
- Das Audit-Protokoll aller Änderungen (Benutzer, Rechte, Inhalte, Einstellungen, Plugins) steht unter *Administration → Protokoll* bzw. `GET /api/v1/admin/audit`.

## Leistung und Skalierung

- Berechtigungsprüfungen brauchen keinen Datenbankzugriff (Snapshot im Speicher, invalidiert bei Änderungen).
- Sessions und API-Keys werden 30 Sekunden im Speicher gehalten. Logout und Rechteänderungen leeren den Cache sofort, auch auf allen Cluster-Instanzen.
- Statische Dateien haben Content-Hashes im Namen und werden ein Jahr lang gecacht. Sie können über ein CDN ausgeliefert werden.
- Horizontal skalieren: weitere Instanzen im Modus `cluster`, PostgreSQL mit PgBouncer bzw. Read-Replicas.
- Öffentliche Wikis/Websites: den Gäste-Cache einschalten (`cache.enabled`, siehe [configuration.md](configuration.md#cache)). Seiten kommen dann für Gäste aus dem Speicher, ohne Datenbankzugriff.
- E-Mail in großen Mengen: Mehrere hundert Benachrichtigungen an jeweils tausende Empfänger pro Tag sind kein Problem. Gemessen wurden rund 1.000 Empfänger pro Sekunde gegen einen lokalen SMTP-Server bei 4 Verbindungen. In der Praxis begrenzt der Mailserver bzw. Provider, daher `mail.concurrency` und ggf. `mail.rate_per_minute` an dessen Limits anpassen. Beobachten: `ewiki mail status` (Feld `oldest_pending`) bzw. die Warnung unter *Administration → E-Mail*.
