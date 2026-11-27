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

Mit selbst gebautem Image (eigene Plugins): `git pull && docker compose -f docker-compose.build.yml up -d --build`.

### Datenbank einer älteren Entwicklungsfassung

Bis zum ersten Release kommen neue Tabellen und Spalten direkt in die ersten Schema-Dateien; Migrationen für ältere Entwicklungsstände gibt es nicht. Einer Datenbank aus so einem älteren Stand fehlen sie. Das Wiki startet dann nicht und schreibt ins Protokoll etwa „the database was created by an older development version of the wiki and lacks: table …, column …“. So läuft es nicht auf einem unvollständigen Schema, auf dem einzelne Aktionen mit einem Fehler 500 scheitern würden (zum Beispiel die Demo anlegen oder die Website zurücksetzen).

Das Wiki vergleicht das Schema nur, wenn sich die Schema-Dateien seit dem letzten Start geändert haben. Der Vergleich läuft in einem vorübergehenden Schema und hinterlässt nichts.

Was zu tun ist: eine neue, leere Datenbank anlegen. Sichern Sie vorher, was Sie noch brauchen (*Backup und Wiederherstellung*). Mit Docker:

```bash
docker compose down
mv data/postgres data/postgres.old      # aufheben, bis der neue Start klappt
docker compose up -d                    # legt Datenbank und Administrator (EWIKI_ADMIN_PASSWORD in .env) neu an
```

Mit systemd: die Datenbank neu anlegen (`dropdb`/`createdb` wie bei der Installation) und `ewiki setup` ausführen.

### Das Menü der Verwaltung

Die Verwaltung hat oben eine Leiste. Zuerst kommt *Übersicht*; sie ist die Startseite (`/admin`). Danach hat jeder Bereich eine Schaltfläche: Anmeldung, Personen und Rechte, Darstellung, Inhalte, Anbindungen, Sicherheit und Datenschutz, Betrieb und Erweiterungen für Plugins ohne Bereich. Eine Schaltfläche öffnet die Seiten ihres Bereichs als Menü über der Seite; die Seite selbst zeigt nur, was Sie gewählt haben. Escape schließt das Menü, die Pfeiltasten wandern durch die Leiste. Auf dem Handy ist die Leiste eine Schaltfläche, die die aktuelle Seite nennt; sie öffnet die Bereiche untereinander.

### Hinweis auf neue Versionen in der Übersicht

Die Startseite der Administration (*Administration → Übersicht*, `/admin`) zeigt, ob eine neue Version des Kerns erschienen ist, mit ihren Änderungen. Sicherheitsupdates sind hervorgehoben: Ihre Abschnitte `## Security` bzw. `## Sicherheit` aus den Release-Notes aller Versionen seit der installierten stehen zuerst (Regel für Release-Notes: [CONTRIBUTING.de.md](../../../CONTRIBUTING.de.md)).

Einen Knopf zum Einspielen gibt es bewusst nicht: Dafür bräuchte das Wiki Zugriff auf Docker, also Root-Rechte auf dem Server. Die Übersicht zeigt stattdessen den passenden Befehl:

| Installation | Befehl | Erkannt an |
|---|---|---|
| Docker mit dem fertigen Image | `docker compose pull` und `docker compose up -d` | `EWIKI_IMAGE_ORIGIN=published`, nur im veröffentlichten Image gesetzt |
| Docker mit selbst gebautem Image | `git pull && docker compose -f docker-compose.build.yml up -d --build` | `EWIKI_IMAGE_ORIGIN=self-built`, Vorgabe des `Dockerfile` |
| systemd | `sudo <Verzeichnis>/scripts/update.sh` | `EWIKI_INSTALL=systemd`, gesetzt von `scripts/install.sh` |

Fehlt das Merkmal (etwa bei einem älteren Image oder einer älteren systemd-Installation), zeigt die Übersicht alle Befehle mit ihrer Bezeichnung. Zum Update-Timer: Bei systemd spielt er, wenn eingeschaltet, sonntags zwischen 03:30 und 04:30 Uhr ein; ob er eingeschaltet ist, kann das Wiki nicht sehen. Unter Docker zeigt die Übersicht den nächsten Lauf des Update-Helfers (unten), wenn er sich kürzlich gemeldet hat, sonst die Befehle. Dieselben Angaben liefert `ewiki status` (`GET /api/v1/admin/status`).

### Docker: automatische Updates mit dem Update-Helfer

Das Wiki bekommt nie Zugriff auf Docker (das wäre Root auf dem Server). Neue Versionen spielt stattdessen ein Helfer auf dem Server ein: `scripts/update-docker.sh`. `./docker-setup.sh` fragt, ob er eingerichtet werden soll (Vorgabe ja, täglich um 03:30 Uhr).

```bash
scripts/update-docker.sh --enable-timer 03:30   # täglich; systemd mit Root/sudo, sonst crontab
scripts/update-docker.sh --enable-timer 03:30 --cron   # immer die crontab des Benutzers
scripts/update-docker.sh --disable-timer
scripts/update-docker.sh --status               # Timer, letzter Lauf, Meldung des Wikis
scripts/update-docker.sh --dry-run              # was ein Lauf täte, ohne Änderungen
scripts/update-docker.sh                        # jetzt einmal ausführen
```

So funktioniert es:

1. Einmal am Tag prüft das Wiki auf Updates (Job `updates.check`, abschaltbar mit `plugins.updates.check: false`). Gibt es ein neueres **veröffentlichtes Release** des Kerns, schreibt es `./data/wiki/update-available` (Version, Link, Sicherheitsupdate ja/nein); sonst entfernt es die Datei. Vorabversionen meldet es nie.
2. Der Helfer läuft täglich als der Benutzer, unter dem das Wiki läuft (`EWIKI_UID`). Ohne `update-available` tut er nichts. Er behandelt die Datei als nicht vertrauenswürdig: Er übernimmt nur eine Version der Form `1.2.3` und führt nichts daraus aus.
3. Bei einem selbst gebauten Image (`docker-compose.build.yml`) ändert er nichts und meldet das nur: Aktualisieren Sie es mit `git pull && docker compose -f docker-compose.build.yml up -d --build`.
4. Zuerst sichert er die Datenbank (`pg_dump` nach `./data/backups/before-update-*.dump`, die neuesten 10 bleiben). Schlägt die Sicherung fehl, wird nichts aktualisiert.
5. Dann `docker compose pull wiki` und `docker compose up -d`; er wartet, bis das Wiki bereit ist, und vergleicht die Version. Eine feste Version in `EWIKI_IMAGE_TAG` (zum Beispiel `7.5`) kann das neue Release verhindern; das meldet der Helfer.
6. Er schreibt `./data/wiki/update-timer.json` (nächster Lauf, letzter Lauf und sein Ergebnis) und protokolliert nach `./data/update-docker.log`. Die Übersicht der Administration zeigt daraus den nächsten Lauf; das Wiki prüft die Datei streng und übergeht sie, wenn sie alt ist.

Läufe überschneiden sich nie, und ein Lauf spielt höchstens ein Update ein.

**Warum systemd oder cron:** `./docker-setup.sh` läuft meist als normaler Benutzer. Einen systemd-Timer (wie `update.sh --enable-timer`) kann nur Root einrichten; mit Root-Rechten oder `sudo` richtet der Helfer einen ein (`enterprisewiki-docker-update.timer`, zufällige Verzögerung bis zu einer Stunde, verpasste Läufe bei ausgeschaltetem Server werden nachgeholt). Der Dienst läuft als Benutzer des Wikis, nicht als Root. Ohne Root-Rechte nutzt der Helfer stattdessen die crontab des Benutzers: Sie braucht keine Rechte außer dem Zugriff auf Docker, den der Benutzer ohnehin hat. Cron kennt keine zufällige Verzögerung, daher wird einmalig eine zufällige Minute in der Stunde nach der gewählten Zeit festgelegt.

## Automatische Updates der Komponenten

Enterprise Wiki bleibt ohne manuelles Eingreifen auf dem neuesten Stand aller Komponenten (Go, Go-Module, Node.js,
npm-Pakete, Docker-Basis-Images, GitHub Actions):

1. **Renovate** (`renovate.json`) prüft täglich vor 6 Uhr und öffnet Pull Requests: kleinere Updates gesammelt in einem,
   große Versionssprünge einzeln. Neue Versionen werden erst nach **3 Tagen** übernommen (zurückgezogene oder
   kompromittierte Pakete erreichen das Produkt so nicht), **Sicherheitsupdates sofort**. Versionen und Docker-Images
   sind exakt festgeschrieben (bei Images und GitHub Actions mit Prüfwert); jedes Update ändert sie per Pull Request.
   Bei der wöchentlichen Auffrischung der Lock-Datei kann Renovate das Alter nicht prüfen; das übernimmt ein
   CI-Schritt (`go run ./tools/releaseage`), der jede Paketversion ablehnt, die jünger als 3 Tage ist.
2. Die **CI** prüft jeden Pull Request vollständig: Build, Go-Tests mit PostgreSQL, Browser-Tests mit
   Barrierefreiheitsprüfung, Docker-Build und Lizenzprüfung der Abhängigkeiten. Nur wenn alles grün ist, führt
   GitHub den Pull Request **automatisch** zusammen (Auto-Merge, von Renovate eingeschaltet; die Repository-Einstellung
   „Allow auto-merge“ muss an sein) – auch bei großen Versionssprüngen. Rote Pull Requests bleiben
   offen; die Prüfung *Renovate watch* (Punkt 6) meldet sie. Die Übersicht aller offenen Updates steht im Issue „Dependency Dashboard“.
3. Nach jeder Änderung auf `main` baut **docker-publish** das Image `ghcr.io/mtsonline/enterprisewiki:latest` (amd64,
   arm64) neu; Versions-Tags `v7.5.0` ergeben zusätzlich `:7.5.0`, `:7.5` und `:7`.
4. Der nächtliche **Canary**-Lauf baut und testet mit den allerneuesten Versionen (ohne Wartezeit, ohne etwas zu
   übernehmen) als Frühwarnung.
5. Die tägliche Prüfung **Outdated components** (`.github/workflows/outdated.yml`, lokal `go run ./tools/outdated`)
   vergleicht Go-Toolchain, direkte Go-Module, npm-Pakete und die Node.js-LTS mit der jeweils neuesten stabilen
   Version. Ist ein Update länger als 7 Tage offen (z. B. Renovate-PR mit roter CI), schlägt sie fehl – so bleibt
   nichts unbemerkt veraltet.
6. Die tägliche Prüfung **Renovate watch** (`.github/workflows/renovate-watch.yml`) öffnet ein Issue
   „Renovate: stuck pull requests“ (Label `renovate-stuck`), sobald ein Renovate-Pull-Request hängt: rote CI aus
   einem anderen Grund als der 3-Tage-Regel für neue npm-Versionen, oder grüne CI, aber seit mehr als 2 Tagen offen.
   Das Issue wird täglich aktualisiert und geschlossen, wenn nichts mehr hängt. Anders als ein roter Check im
   Pull Request eines Bots erreicht ein Issue alle, die das Repository beobachten.

**PostgreSQL-Hauptversionen:** Renovate hebt die *empfohlene* Version an (`docker-compose*.yml`, `.env.example`,
Hinweis in der Administration), die CI testet sie und das Upgrade von den älteren Versionen. Bestehende Installationen
wechseln erst mit `./docker-postgres-upgrade.sh` (nächster Abschnitt). Die `go`-Zeile in `go.mod` bleibt die
Mindestversion zum Bauen; verwendet wird immer die Go-Version aus der `toolchain`-Zeile.

## PostgreSQL-Upgrades (Docker)

Docker Compose verwendet das **offizielle** Image `postgres:<Version>`. Die Version steht in `.env`
(`POSTGRES_VERSION`), die Daten jeder Hauptversion liegen in einem eigenen Ordner: `./data/postgres/19/docker`,
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

Die CLI ist dieselbe Binärdatei. Sie läuft mit Superadmin-Rechten; wer sie als Dienstbenutzer ausführen kann, hat volle Kontrolle. Mit `--config` bzw. `EWIKI_CONFIG` wählen Sie die Installation.

**Alle Befehle gehen über die API** (im selben Prozess, durch den normalen Server-Handler): Es gelten dieselben Prüfungen, Grenzwerte und Audit-Einträge wie in der Oberfläche; im Audit-Protokoll stehen sie mit „cli“. Mit `--as BENUTZER` gelten genau dessen Rechte. Ausnahmen, vom Projektinhaber genehmigt, weil sie nicht mit dem Wiki, sondern mit Dateien arbeiten:

- `serve`, `setup`, `migrate` (starten oder richten das Wiki ein);
- `config show|path|validate|get|set` (bearbeitet `config.yaml` direkt und funktioniert auch, wenn die Datenbank nicht erreichbar ist). `config set` schreibt einen Eintrag ins Audit-Protokoll (`settings.config_set`, nur der Schlüssel), wenn die Datenbank erreichbar ist. Übernimmt ein laufendes Wiki eine von außen geänderte Datei, protokolliert es die geänderten Schlüssel (`settings.file_changed`), nie die Werte;
- `plugin generate|build` (Quellbaum und Bauen des Programms);
- `signature verify DATEI --offline` (prüft eine exportierte Datei ohne Wiki).

```text
ewiki serve | setup [--non-interactive] | migrate | version
ewiki api METHOD PATH [BODY|@datei.json|-] [--as BENUTZER]
ewiki user list|create|passwd|disable|enable|unlock ...
ewiki directory sync|runs|adopt PROVIDER ...   # Verzeichnis-Abgleich LDAP/AD, siehe authentication.md
ewiki plugin list|enable|disable|install|remove|generate|build ...
ewiki config show|path|validate|get|set ...
ewiki retention run
ewiki signature verify DATEI [--offline] | ewiki signature keys
ewiki smime list [--all | --mine --as BENUTZER]
ewiki smime add DATEI [--label TEXT] [--password-file PFAD | --password-stdin] [--seal] [--no-sign] [--mine --as BENUTZER]
ewiki smime set ID [--label TEXT] [--sign | --no-sign] [--seal | --no-seal] | ewiki smime remove ID
ewiki smime settings [mail_signing=true|false] [on_expired=unsigned|hold] [personal=true|false] [seal_evidence=true|false]
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

### Befehle aus dem Aktionsregister

Für jede Aktion des Aktionsregisters (siehe [API: Aktionsregister](../developer/api.md#aktionsregister)) gibt es einen eigenen Befehl, auch wenn kein handgeschriebener existiert. Er geht wie alle Befehle über die API, mit denselben Rechten, Prüfungen und Audit-Einträgen.

```bash
ewiki help                                  # alle Befehle, unten die aus dem Register
ewiki smime show --help                     # Aufruf, Route und Recht eines Befehls
ewiki smime show 7 --as anna                # Pfad-Parameter als Argument, mit den Rechten von anna
ewiki auth password-reset request --email anna@example.com
ewiki ldap test --json @test.json           # Inhalt als JSON (Datei, inline oder - für stdin)
ewiki file upload --file file=@notizen.txt  # Hochladen (multipart): --form NAME=WERT, --file FELD=@DATEI
```

- Pfad-Parameter stehen als Argument oder als Option (`--id 7`), Felder von Abfrage und Inhalt als Optionen (`--email …`), soweit das Register sie beschreibt; sonst `--json` bzw. `--query NAME=WERT`.
- Hochladen: `--form NAME=WERT` für Textfelder (in der angegebenen Reihenfolge) und `--file FELD=@DATEI` für Dateien; nicht zusammen mit `--json`.
- Passwörter und andere Geheimnisse besser mit `--json -` über die Standardeingabe übergeben, nicht als Option (sie stünden sonst im Verlauf der Shell).
- Handgeschriebene Befehle (`ewiki smime list` usw.) haben Vorrang und bleiben, wie sie sind.

## Demo-Inhalte und Zurücksetzen

Unter *Administration → Demo* (nur Administratoren) legt **Demo anlegen** einen Bereich „Demo: Enterprise Wiki“
(Schlüssel `DEMO`, Kurz-URL `/demo`) mit zwei Wegen ins Wiki an:

- **Beispiel-Firma** („Enterprise IT Services“, ein erfundener IT-Dienstleister): ein Intranet, wie es eine Firma nutzt –
  Startseite mit Schnellzugriffen, Neuigkeiten (Blog mit Kommentar, @Erwähnung und Reaktion), Termine mit Anmeldung
  und Serie, Teams (Teamseite, Entscheidungsprotokoll, Aufgaben im Text, Kalender der Teams, gemeinsame Notizen),
  Handbuch (Darstellung *Handbuch*), Wissensdatenbank (FAQ mit Sofortsuche), Formulare und Abläufe (Urlaubsantrag
  mit Entscheidung der Teamleitung und Eintrag im Kalender, interne Bewerbung mit Lebenslauf, Vorprüfung durch das
  Personal, Entscheidung der Teamleitung und der Antwort als Pop-up im Wiki, Geräteantrag, Einarbeitung, Ideenbox mit öffentlichen
  Ergebnissen, Kontaktformular, weitere Workflows mit Auslösern), Projekte (Eigenschaften, Projekt-Datenbank,
  Berichte), Dokumente (vertraulich eingestuft, PDF nur zur Ansicht mit Wasserzeichen, Word-Datei für das
  Office-Programm, eine Seite mit eigenen Rechten) und Kategorien als Karten, Tabelle und „alles auf einer Seite“.
  Ein kurzer Hinweis „So funktioniert das“ erklärt die Funktion dort, wo sie genutzt wird, und verweist auf die
  passende Station des Rundgangs. Die Demo-Konten der Teamleitung entscheiden Anträge; damit Sie Urlaubsantrag und
  Bewerbung selbst ganz durchspielen können, gehört das Administratorkonto, das die Demo anlegt, für die Dauer der
  Demo zu den Gruppen „Demo: Verwaltung“ und „Demo: Teamleitung“.
- **Rundgang durch die Funktionen**: eine Station je Funktion, jede eine Seite mit immer demselben Aufbau – was es
  ist, *So geht's* (die Schritte), *So sieht es aus* (das Ergebnis live auf der Seite), *Wofür Sie es nutzen* und
  bei Bedarf *Gut zu wissen*. Die Kategorie hat die Darstellung *Handbuch*: alle Stationen stehen in der
  Seitenspalte, am Ende jeder Seite führen *Zurück* und *Weiter* zur vorigen und nächsten Station. Die Station
  *Menüs, Platzierung und Karussell* sagt für jeden Teil außerhalb der Seiten (Menü, Baustein „Empfang“, Hell und
  dunkel, Karussell, Handbuch), was er zeigt, wo er platziert ist und wo man ihn ändert.

Was ein deaktiviertes Plugin braucht, wird ausgelassen (auch die Station dazu); die Meldung nach dem Anlegen nennt
die ausgelassenen Teile (in der API das Feld `skipped` der Antwort auf `POST /api/v1/admin/demo`). Ein Test prüft
die Mindestregeln der Demo: jede Seite auf Deutsch und Englisch, ohne Fehler in Bausteinen, ohne tote Links, nur
Adressen der Demo-Firma (`enterpriseit.local`, im Internet nie gültig), kein englischer Text in der deutschen Fassung, jede Station mit demselben Aufbau und ohne Lücke in der Reihenfolge; die
Browser-Tests prüfen jede Seite mit axe, hell und dunkel.

- **Startseite:** Die Demo setzt ihre Seite *Enterprise Wiki auf einen Blick* als Startseite der Website (Kurz-URL
  `/willkommen`, in einem Wiki mit Englisch als Grundsprache `/welcome`, Template „CMS modern“): was neugierig
  macht, Einsatzbereiche (Intranet, Handbuch, Wissensdatenbank, Abläufe und Formulare, Hochschule, Website) als
  Einstieg in die passende Stelle der Demo, die Highlights und die beiden Wege in die Demo – ohne Pfad, Seitendetails
  und Kommentare. **Demo entfernen** stellt die vorige Startseite wieder her, außer sie wurde inzwischen unter
  *Administration → Einstellungen* geändert.
- Die **Demo-Konten** (`demo-anna`, `demo-ben`, …) haben keine Anmeldung (Anbieter `demo`), keine E-Mail-Adresse
  und zählen nicht zur Lizenz.
- Alles, was die Demo anlegt, wird in `demo_objects` vermerkt. **Demo entfernen** löscht genau das restlos:
  Inhalte samt Versionen, Übersetzungen, Adressen, Kommentaren, Reaktionen, Favoriten, Benachrichtigungen,
  Medien und gespeicherten Dateien, Konten und Gruppen der Demo sowie ihre Einträge im Audit-Log. Eigene Inhalte
  bleiben unberührt – auch eigene Seiten, die Sie in Demo-Kategorien angelegt haben, verschwinden mit ihnen.

**Site auf den ersten Start zurücksetzen** (auf derselben Seite) löscht alle Inhalte, Medien, Dateien, Versionen, den
Papierkorb, das Audit-Log und die Einstellungen der Site. Übrig bleiben Ihr Administratorkonto, die Rollen und die
Willkommensseite – wie nach `ewiki setup`. Instanz-Einstellungen, Adresse, Sprachen, E-Mail-Versand, Name der Site und
Lizenz bleiben. Bei mehreren Sites mit gemeinsamen Konten bleiben die Konten erhalten (sie gehören allen Sites).
Bestätigung: Name der Site eintippen und (bei lokalem Konto) das eigene Passwort; mit API-Schlüsseln ist das nicht
möglich. Danach melden Sie sich neu an. Vorher eine Sicherung machen!

```bash
ewiki demo status
ewiki demo create --as admin
ewiki demo remove
ewiki demo reset-site --as admin --confirm "Enterprise Wiki"
```

## Wartungsmodus

Für Arbeiten am Server – zum Beispiel einen Umzug – stellt die Administration das Wiki unter *Administration → Wartung* auf **nur lesen** oder **geschlossen** (oder mit `ewiki maintenance set`, siehe [configuration.md](configuration.md#maintenance)). Nur lesen: Alle lesen, nur die Administration ändert, ein Balken auf jeder Seite sagt das mit Ihrem Hinweis und dem erwarteten Ende. Geschlossen: Andere sehen nur diesen Hinweis und den Weg zur Anmeldung. Hintergrundaufgaben warten in der Vorgabe, damit eine jetzt gezogene Kopie der Datenbank vollständig ist und nach dem Umzug keine E-Mail doppelt sendet. Der Modus endet nicht von selbst: Schalten Sie ihn aus, wenn Sie fertig sind.

Umzug des Servers: „nur lesen“ einschalten, Datenbank und Datenverzeichnis kopieren, den neuen Server starten (weiter „nur lesen“, die Einstellung zieht mit der Konfiguration um), prüfen, die Adresse auf ihn zeigen lassen, die Wartung ausschalten.

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
| `data.rebuild` | 1 min | Daten für Berichte (zum Beispiel Entscheidungen) von neuen Arten oder Arten mit neuer Version aus den neuesten Fassungen der Seiten aufbauen, 200 Seiten je Schritt; *Administration → Daten für Berichte* zeigt den Stand und startet den Aufbau neu |
| `workflow.process.timers` | 30 s | Wartezeiten und Webhooks von Workflows fortsetzen, Erinnerungen für überfällige Aufgaben |
| `search.external.sync` | 10 s | externen Suchindex abgleichen (nur wenn konfiguriert) |
| `updates.check` | täglich (und 3 s nach dem Start) | auf Updates des Kerns und der Plugins prüfen (`plugins.updates.check`); unter Docker der Hinweis für den Update-Helfer |

Im Cluster-Modus läuft jeder Job zur selben Zeit nur auf einer Instanz (Advisory Lock).

## Logs und Überwachung

- Logs gehen nach stderr, also in systemd ins Journal (`journalctl -u enterprisewiki -f`). JSON-Format mit `EWIKI_LOG_FORMAT=json`, mehr Details mit `EWIKI_LOG_LEVEL=debug`.
- `GET /healthz` meldet, dass der Prozess läuft. `GET /readyz` prüft zusätzlich die Datenbank (für Load Balancer und Kubernetes).
- Das Audit-Protokoll aller Änderungen (Benutzer, Rechte, Inhalte, Einstellungen, Plugins) steht unter *Administration → Protokoll* bzw. `GET /api/v1/admin/audit/search` (`ewiki audit search`).

## Leistung und Skalierung

- Berechtigungsprüfungen brauchen keinen Datenbankzugriff (Snapshot im Speicher, invalidiert bei Änderungen).
- Sessions und API-Keys werden 30 Sekunden im Speicher gehalten. Logout und Rechteänderungen leeren den Cache sofort, auch auf allen Cluster-Instanzen.
- Statische Dateien haben Content-Hashes im Namen und werden ein Jahr lang gecacht. Sie können über ein CDN ausgeliefert werden.
- Horizontal skalieren: weitere Instanzen im Modus `cluster`, PostgreSQL mit PgBouncer bzw. Read-Replicas.
- Öffentliche Wikis/Websites: den Gäste-Cache einschalten (`cache.enabled`, siehe [configuration.md](configuration.md#cache)). Seiten kommen dann für Gäste aus dem Speicher, ohne Datenbankzugriff.
- E-Mail in großen Mengen: Mehrere hundert Benachrichtigungen an jeweils tausende Empfänger pro Tag sind kein Problem. Gemessen wurden rund 1.000 Empfänger pro Sekunde gegen einen lokalen SMTP-Server bei 4 Verbindungen. In der Praxis begrenzt der Mailserver bzw. Provider, daher `mail.concurrency` und ggf. `mail.rate_per_minute` an dessen Limits anpassen. Beobachten: `ewiki mail status` (Feld `oldest_pending`) bzw. die Warnung unter *Administration → E-Mail*.
