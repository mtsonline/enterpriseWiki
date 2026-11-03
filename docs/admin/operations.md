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
docker compose up -d          # lädt das neueste Image und startet neu (Migrationen laufen automatisch)
```

Mit selbst gebautem Image (eigene Plugins): `git pull && docker compose up -d --build`.

## Automatische Updates der Komponenten

Enterprise Wiki bleibt ohne manuelles Eingreifen auf dem neuesten Stand aller Komponenten (Go, Go-Module, Node.js,
npm-Pakete, Docker-Basis-Images, GitHub Actions):

1. **Renovate** (`renovate.json`) prüft täglich vor 6 Uhr und öffnet Pull Requests: kleinere Updates gesammelt in einem,
   große Versionssprünge einzeln. Neue Versionen werden erst nach **3 Tagen** übernommen (zurückgezogene oder
   kompromittierte Pakete erreichen das Produkt so nicht), **Sicherheitsupdates sofort**.
2. Die **CI** prüft jeden Pull Request vollständig: Build, Go-Tests mit PostgreSQL, Browser-Tests mit
   Barrierefreiheitsprüfung, Docker-Build und Lizenzprüfung der Abhängigkeiten. Nur wenn alles grün ist, führt
   Renovate den Pull Request **automatisch** zusammen – auch bei großen Versionssprüngen. Rote Pull Requests bleiben
   offen; GitHub meldet das per E-Mail. Die Übersicht aller offenen Updates steht im Issue „Dependency Dashboard“.
3. Nach jeder Änderung auf `main` baut **docker-publish** das Image `ghcr.io/mtsonline/enterprisewiki:latest` (amd64,
   arm64) neu; Versions-Tags `v7.5.0` ergeben zusätzlich `:7.5.0`, `:7.5` und `:7`.
4. Der nächtliche **Canary**-Lauf baut und testet mit den allerneuesten Versionen (ohne Wartezeit, ohne etwas zu
   übernehmen) als Frühwarnung.

**PostgreSQL-Hauptversionen** (z. B. 16 → 18) werden ebenfalls automatisch übernommen, siehe nächster Abschnitt. Die `go`-Zeile in `go.mod` bleibt die
Mindestversion zum Bauen; verwendet wird immer die Go-Version aus der `toolchain`-Zeile.

## PostgreSQL-Upgrades (Docker)

Docker Compose verwendet das Image `ghcr.io/mtsonline/enterprisewiki-postgres` (offizielles PostgreSQL-Image plus
automatisches Upgrade). Erscheint eine neue Hauptversion, läuft beim nächsten `docker compose up -d` ohne Zutun:

1. Der Container findet die Daten der älteren Version im Volume `pgdata` und startet sie lokal (ohne Netzwerk) mit den
   mitgelieferten Programmen dieser Version.
2. Er schreibt eine **vollständige Sicherung** (`pg_dumpall`) ins Volume `pgbackup`:
   `/backups/pg16-before-pg18-<Zeitpunkt>.sql.gz`.
3. Er legt einen neuen Datenbestand der neuen Version an, spielt die Sicherung ein und **vergleicht jede Tabelle aller
   Datenbanken** (Zeilenanzahl) mit dem Stand vorher.
4. Nur wenn alles übereinstimmt, wird umgeschaltet. Bei einer Abweichung bleibt die alte Version unverändert in
   Betrieb-fähigem Zustand, der Container meldet den Fehler im Log (`docker compose logs postgres`) und versucht es beim
   nächsten Start erneut.

Die Daten der alten Version bleiben unverändert im Volume erhalten (Rückweg: altes Image angeben). Die Daten jeder
Version liegen in einem eigenen Verzeichnis (`/var/lib/postgresql/ewiki/pg18`). Platz: Während des Upgrades wird etwa
das Doppelte der Datenbankgröße plus die (komprimierte) Sicherung benötigt. Alte Daten und Sicherungen nach einiger Zeit
löschen:

```bash
docker compose exec postgres sh -c 'ls -la /var/lib/postgresql/ewiki /backups'
```

Das Wiki wartet währenddessen (Healthcheck mit langer Startphase) und startet danach automatisch. Vor jeder Übernahme
eines Updates prüft die CI das Upgrade von **jeder** im Image enthaltenen älteren Version auf die neue (Testdaten,
Erweiterung `ltree`, mehrere Datenbanken, Rollen und Passwörter, Neustart ohne erneutes Upgrade).

**Umstieg bestehender Installationen** (bisher `postgres:16-alpine` mit `pgdata:/var/lib/postgresql/data`): die neue
`docker-compose.yml` übernehmen – sie hängt dasselbe Volume unter `/var/lib/postgresql/ewiki` ein – und
`docker compose up -d` ausführen; das Upgrade auf die aktuelle Version läuft dabei wie oben beschrieben.

Bei Installationen ohne Docker (systemd) betreibt die Organisation PostgreSQL selbst; dort gilt das Upgrade-Verfahren der
jeweiligen Distribution (z. B. `pg_upgradecluster`).

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
ewiki license install @lizenz.txt       # Lizenzschlüssel einspielen (offline geprüft)
ewiki license show                      # Lizenznehmer, Benutzer, Laufzeit
ewiki license notices                   # Lizenzen der enthaltenen Komponenten
ewiki watch add category 12 --mode content --as anna   # anna beobachtet Kategorie 12 (mit Inhalt)
ewiki watch add category 12 --frequency daily --as anna    # tägliche Zusammenfassung statt sofortiger E-Mails
ewiki api GET /api/v1/admin/webhooks                      # Webhooks und Ereignisse
ewiki api POST /api/v1/admin/webhooks/3/test              # Testnachricht senden
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
