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

**PostgreSQL-Hauptversionen:** Renovate hebt die *empfohlene* Version an (`docker-compose*.yml`, `.env.example`,
Hinweis in der Administration), die CI testet sie und das Upgrade von den älteren Versionen. Bestehende Installationen
wechseln erst mit `./docker-postgres-upgrade.sh` (nächster Abschnitt). Die `go`-Zeile in `go.mod` bleibt die
Mindestversion zum Bauen; verwendet wird immer die Go-Version aus der `toolchain`-Zeile.

## PostgreSQL-Upgrades (Docker)

Docker Compose verwendet das **offizielle** Image `postgres:<Version>`. Die Version steht in `.env`
(`POSTGRES_VERSION`), die Daten jeder Hauptversion liegen in einem eigenen Ordner: `./data/postgres/18/docker`,
später `./data/postgres/19/docker` usw. Ein `docker compose up -d` wechselt daher **nie** von selbst die Hauptversion.

**Wann?** Ist eine neuere Version empfohlen oder endet die Unterstützung der verwendeten Version durch das
PostgreSQL-Projekt (fünf Jahre nach Erscheinen), zeigt das die Administration (Hinweis auf allen Admin-Seiten bzw.
unter *Einstellungen → Datenbank*), das Server-Log beim Start und eine E-Mail an alle Administratoren – einmal, wenn
eine neue Version empfohlen wird, und 180 bzw. 30 Tage vor sowie nach dem Ende der Unterstützung. API:
`GET /api/v1/admin/database`.

**Wie?** Im Ordner der Installation:

```bash
./docker-postgres-upgrade.sh             # auf die empfohlene Version (fragt vorher nach)
./docker-postgres-upgrade.sh --check     # nur prüfen (Exit 0: aktuell, 10: Upgrade empfohlen)
./docker-postgres-upgrade.sh --yes       # ohne Rückfrage, z. B. per Cron/systemd-Timer
./docker-postgres-upgrade.sh --to 19     # auf eine bestimmte Version
./docker-postgres-upgrade.sh --link      # pg_upgrade --link: schnell, fast kein Zusatzplatz, alte Daten danach nicht mehr nutzbar
```

Ablauf:

1. Prüft den freien Speicherplatz (Kopie: etwa die Größe der Datenbank) und stoppt die Container.
2. Startet einmalig einen Container aus dem **offiziellen Image der neuen Version** und installiert darin die Programme
   der alten Version aus dem offiziellen PostgreSQL-Paketarchiv (im Image bereits eingerichtet; braucht Internetzugang).
3. Liest Zeichensatz, Sortierung und Prüfsummen-Einstellung der alten Daten, legt den neuen Datenbestand passend an,
   führt `pg_upgrade --check` und dann `pg_upgrade` aus.
4. Vergleicht die Zeilenanzahl **jeder Tabelle aller Datenbanken** vorher und nachher, aktualisiert die Statistiken.
5. Trägt `POSTGRES_VERSION` in `.env` ein und startet alles wieder.

Schlägt ein Schritt fehl, wird nichts umgeschaltet: Der unvollständige neue Ordner wird entfernt und die bisherige
Version startet wieder. Die alten Daten bleiben in `./data/postgres/<alt>` erhalten (Rückweg: `POSTGRES_VERSION` in
`.env` zurücksetzen); nach der Kontrolle löschen mit `rm -rf data/postgres/<alt>`. Die CI testet das Upgrade von 16 und
17 auf die empfohlene Version (Testdaten, `ltree`, mehrere Datenbanken, Rollen und Passwörter, Kopie und `--link`,
das Wiki selbst, eigener Benutzer statt root).

Bei Installationen ohne Docker (systemd) betreibt die Organisation PostgreSQL selbst; dort gilt das Upgrade-Verfahren der
jeweiligen Distribution (z. B. `pg_upgradecluster`). Die Hinweise in der Administration erscheinen dort ebenso.

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
ewiki watch assign list|add|remove ...
ewiki perm list | overview TYP [ID] | defaults set RECHT...
ewiki audit search|export [Filter] | settings [set @datei] | forward-test @ziel.json
ewiki draft list|show|publish|delete ... --as BENUTZER
ewiki inbox [list] | read [ID...] --as BENUTZER
```

**Jede API-Funktion** steht über `ewiki api` zur Verfügung, auch die von Plugins:

```bash
ewiki api GET /api/v1/categories
ewiki api POST /api/v1/categories '{"titles":{"de":"Personal"},"parent_id":1}'
ewiki api GET /api/v1/x/core.menu/menus
ewiki api GET /api/v1/categories/1 --as anna        # mit den Rechten von anna@local
```

`--as BENUTZER` arbeitet **genau** mit den Rechten dieses Benutzers, nie mit
mehr: Die Sonderrechte der Kommandozeile (z. B. Shell-Befehle freigeben)
gelten dabei nicht, auch nicht mit `--as admin`. Die Option gibt es nur in
der lokalen Kommandozeile, die ohnehin Zugriff auf Konfiguration und
Datenbank hat; über HTTP, API-Schlüssel oder SSH-Token kann man sich nicht
als anderer Benutzer ausgeben. Im Audit-Log erscheinen solche Aufrufe mit
`via = cli-as`.

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
ewiki watch assign add group 4 category 12 --mandatory --frequency weekly --note "Pflicht für das Team"
ewiki perm overview group 4             # direkte und wirksame Rechte der Gruppe 4 je Kategorie
ewiki perm defaults set core.share.use core.mentions.use   # Standardrechte aller Angemeldeten
ewiki audit search --actor anna --from 2026-11-01 --area acl   # Rechteänderungen von anna seit 1.11.
ewiki audit export --format csv --area auth > anmeldungen.csv
ewiki draft publish 7 --at 2026-12-01T08:00:00+01:00 --as anna   # Entwurf geplant veröffentlichen
ewiki api GET /api/v1/admin/webhooks                      # Webhooks und Ereignisse
ewiki api POST /api/v1/admin/webhooks/3/test              # Testnachricht senden
ewiki api GET /api/v1/x/content.events/upcoming        # nächste Termine aller Kalender
ewiki api GET '/api/v1/elements?q=kalender&type=core.html'   # Elemente suchen (wie Administration → Elemente)
ewiki api PATCH /api/v1/elements/12 '{"short_url":"kalender"}' # Kurz-URL setzen
ewiki config set ui.home_element 12                    # Element 12 als Startseite
```

## Demo-Inhalte und Zurücksetzen

Unter *Administration → Demo* (nur Administratoren) legt **Demo anlegen** einen Bereich „Demo: Enterprise Wiki“ an,
der möglichst alle Funktionen zeigt: Rundgang-Seite mit Makros (Inhaltsverzeichnis, Hinweis-Boxen, Status-Labels,
Spalten, Aufklappbereiche, Galerie, Unterseiten, zuletzt geändert, nächste Termine), Seiteneigenschaften, Kommentare
mit @Erwähnungen, Reaktionen und Favorit, Markdown-Seite, Website-Bausteine, Blog, FAQ, Veranstaltungskalender mit
Serie und Anmeldung, Teamseite, Medien (unsichtbare Kategorie als Ordner, Bilder, PDF nur zur Ansicht mit
Wasserzeichen, Word-Datei für das Office-Programm), Datei-Element, Ablauf, Eigenschaften-Bericht,
Kategorieübersicht, Kurz-URL `/demo`, ein Menü, ein Textblock und ein Karussell (nur für angemeldete Benutzer). Was
ein deaktiviertes Plugin braucht, wird ausgelassen.

- Die **Demo-Konten** (`demo-anna`, `demo-ben`, …) haben keine Anmeldung (Anbieter `demo`), keine E-Mail-Adresse
  und zählen nicht zur Lizenz.
- Alles, was die Demo anlegt, wird in `demo_objects` vermerkt. **Demo entfernen** löscht genau das restlos:
  Inhalte samt Versionen, Übersetzungen, Adressen, Kommentaren, Reaktionen, Favoriten, Benachrichtigungen,
  Medien und gespeicherten Dateien, Konten und Gruppen der Demo sowie ihre Einträge im Audit-Log. Eigene Inhalte
  bleiben unberührt – auch eigene Seiten, die du in Demo-Kategorien angelegt hast, verschwinden mit ihnen.

**Site auf den ersten Start zurücksetzen** (auf derselben Seite) löscht alle Inhalte, Medien, Dateien, Versionen, den
Papierkorb, das Audit-Log und die Einstellungen der Site. Übrig bleiben dein Administratorkonto, die Rollen und die
Willkommensseite – wie nach `ewiki setup`. Instanz-Einstellungen, Adresse, Sprachen, E-Mail-Versand, Name der Site und
Lizenz bleiben. Bei mehreren Sites mit gemeinsamen Konten bleiben die Konten erhalten (sie gehören allen Sites).
Bestätigung: Name der Site eintippen und (bei lokalem Konto) das eigene Passwort; mit API-Schlüsseln ist das nicht
möglich. Danach meldest du dich neu an. Vorher eine Sicherung machen!

```bash
ewiki demo status
ewiki demo create --as admin
ewiki demo remove
ewiki demo reset-site --as admin --confirm "Enterprise Wiki"
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
