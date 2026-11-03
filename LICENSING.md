# Lizenzierung von Enterprise Wiki

> Kurzfassung: Enterprise Wiki steht unter der **Business Source License 1.1** (BSL, siehe [LICENSE](LICENSE)).
> **Privatpersonen** nutzen es für private Zwecke **kostenlos**. **Organisationen** – Unternehmen,
> öffentliche Einrichtungen, Schulen und Hochschulen, Vereine – benötigen eine **Lizenz**.
> Jede Version wird vier Jahre nach ihrer Veröffentlichung zusätzlich Open Source (AGPL-3.0).

Diese Seite erklärt die Lizenz in verständlichen Worten. Rechtlich verbindlich ist der Text in [LICENSE](LICENSE)
bzw. der jeweilige Lizenzvertrag.

## Wer darf was?

| Nutzung | Ohne Lizenz | Mit Lizenz |
|---|---|---|
| Quellcode lesen, ändern, weitergeben | ✔ (unter derselben Lizenz) | ✔ |
| Testen, Entwickeln, Evaluieren (keine produktive Nutzung) | ✔ für alle | ✔ |
| Produktiv als **Privatperson** für private Zwecke (Familien-Wiki, private Website) | ✔ kostenlos | – |
| Produktiv durch oder für **Unternehmen**, Selbständige, beruflich | ✘ | ✔ Lizenz „Unternehmen“ |
| **Öffentliche Einrichtungen** (Behörden, Gemeinden, Ämter) | ✘ | ✔ vergünstigt |
| **Bildung und Forschung** (Schulen, Hochschulen, Forschungseinrichtungen) | ✘ | ✔ vergünstigt (EDU) |
| **Vereine**, Stiftungen, gemeinnützige Organisationen | ✘ | ✔ vergünstigt |
| Als gehosteten Dienst für Dritte anbieten | ✘ | nur mit gesonderter Vereinbarung |

Vier Jahre nach Erscheinen einer Version gilt für **diese** Version zusätzlich die AGPL-3.0-or-later: Ab dann darf
sie jeder nach den Regeln der AGPL nutzen. Neuere Versionen bleiben bis zu ihrem eigenen Stichtag unter der BSL.

## Lizenzschlüssel

Organisationen erhalten einen **Lizenzschlüssel** (beginnt mit `EWL1.`). Er enthält Lizenznehmer, Lizenzart,
Anzahl der Benutzer und Laufzeit und ist digital signiert (Ed25519). Die Prüfung erfolgt **offline** – das Wiki
nimmt dafür keine Verbindung nach außen auf.

- Einspielen: *Administration → Lizenz* oder `ewiki license install KEY` (gespeichert in `config.yaml` unter `license.key`).
- Anzeige: `ewiki license show`, API `GET /api/v1/admin/license`.
- Gezählt werden alle **aktiven** (nicht deaktivierten) Benutzerkonten.

Der Schlüssel **sperrt keine Funktionen**. Ist er abgelaufen oder werden mehr Benutzer aktiv als lizenziert, weist die
Administration deutlich darauf hin. Die Pflicht zur Lizenz ergibt sich aus der Lizenz selbst, nicht aus dem Schlüssel.

## Enthaltene Komponenten Dritter

Alle Komponenten, die in das ausgelieferte Programm eingebaut werden, stehen unter **freizügigen** Lizenzen
(MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC) bzw. bei Schriften unter der **SIL Open Font License 1.1**.
Diese Lizenzen erlauben die Verwendung in einem Produkt unter der BSL, in kommerziellen Lizenzen und später unter der
AGPL. Es gibt damit **keinen Lizenzwiderspruch**. Zu erfüllen sind:

- **Urheberrechtshinweise und Lizenztexte mitliefern**: Beim Bauen erzeugt `tools/notices` die Datei `licenses.txt`
  mit allen Lizenztexten (inkl. der NOTICE-Dateien von Apache-2.0-Komponenten). Sie ist im Programm enthalten,
  abrufbar unter `/licenses.txt`, unter *Administration → Lizenz* und mit `ewiki license notices`.
- **Schriften** (Inter, Source Sans 3; OFL 1.1): dürfen mit Software gebündelt, aber nicht einzeln verkauft werden; der
  Lizenztext ist in `licenses.txt` enthalten.

Nur zum **Bauen und Testen** (nicht im ausgelieferten Programm): TypeScript (Apache-2.0), esbuild (MIT), Playwright
(Apache-2.0), axe-core (MPL-2.0), gldap (MIT, nur Tests). Diese Lizenzen stellen keine Anforderungen an das Produkt.

**Docker-Image**: Es enthält zusätzlich Programme aus Alpine Linux (u. a. BusyBox, musl) und `poppler-utils`
(`pdftotext`, GPL). Diese laufen als **eigenständige Programme** neben Enterprise Wiki (nicht hineinkompiliert), das ist
eine zulässige Zusammenstellung („Aggregat“) und berührt die Lizenz von Enterprise Wiki nicht. Die Quellen dieser Pakete
stellt Alpine Linux bereit (<https://pkgs.alpinelinux.org>).

### Regeln für neue Abhängigkeiten

Die CI prüft bei jedem Build mit `go run ./tools/notices -check`, dass alle eingebauten Go-Module eine erlaubte Lizenz
haben:

- **Erlaubt**: MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, ISC, Zlib, Unlicense/CC0; Schriften unter OFL-1.1.
- **Nur nach Prüfung**: MPL-2.0 (Änderungen an diesen Dateien müssen offengelegt werden).
- **Nicht erlaubt** im Programm: GPL, LGPL, AGPL (würden die BSL-Lizenzierung unmöglich machen), SSPL, BSL/„Source
  available“-Lizenzen Dritter, Lizenzen mit Nutzungsbeschränkungen (z. B. „non-commercial“), Code ohne Lizenz.

## Mitwirken und Plugins

Damit Enterprise Wiki weiterhin unter der BSL **und** unter kommerziellen Lizenzen vertrieben werden kann, braucht der
Lizenzgeber an jedem Beitrag die nötigen Rechte. Deshalb gilt:

- **Beiträge zum Repository** (Pull Requests, Patches, Übersetzungen, Dokumentation) werden nur angenommen, wenn die
  beitragende Person der [Beitragsvereinbarung (CLA)](CLA.md) zugestimmt hat. Sie behält ihr Urheberrecht, räumt dem
  Lizenzgeber aber ein umfassendes, unbefristetes Nutzungsrecht inkl. Unterlizenzierung und Neulizenzierung ein.
- **Plugins im offiziellen Plugin-Katalog** bzw. Plugins, die mit Enterprise Wiki ausgeliefert werden, setzen ebenfalls
  die Zustimmung zur CLA voraus (Abschnitt „Plugins“ der CLA).
- **Eigene Plugins für den Eigengebrauch** (z. B. ein internes Plugin einer Organisation) brauchen keine CLA. Sie
  dürfen unter einer beliebigen, mit der BSL vereinbaren Lizenz stehen. Ein Programm, das Enterprise Wiki zusammen mit
  dem Plugin enthält, unterliegt als Ganzes weiterhin der BSL bzw. dem Lizenzvertrag.

## Lizenzarten und Preise

| Lizenzart | Für | Schlüssel-Edition |
|---|---|---|
| Unternehmen | Firmen, Selbständige, berufliche Nutzung | `business` |
| Öffentliche Einrichtung | Behörden, Gemeinden, öffentliche Unternehmen | `public` |
| Bildung und Forschung | Schulen, Hochschulen, Forschungseinrichtungen | `edu` |
| Verein / gemeinnützig | Vereine, Stiftungen, NGOs | `nonprofit` |
| Testlizenz | befristete Evaluierung mit Produktivdaten | `trial` |

Lizenzgeber, Preise und Anfragen:

**thinking systems**, Inh. Martin Thomas Schrott
Höhenweg 114, 9073 Klagenfurt am Wörthersee, Österreich

## Häufige Fragen

**Ich nutze das Wiki privat und möchte meinen Verein damit organisieren.** Die Nutzung für den Verein ist keine private
Nutzung mehr; dafür gibt es die vergünstigte Vereinslizenz.

**Wir möchten Enterprise Wiki testen.** Testen und Evaluieren ist ohne Lizenz erlaubt, solange keine produktive Nutzung
stattfindet. Für einen Pilotbetrieb mit echten Nutzern gibt es Testlizenzen.

**Dürfen wir den Code anpassen?** Ja. Angepasste Versionen unterliegen ebenfalls der BSL; für die produktive Nutzung in
einer Organisation braucht es weiterhin eine Lizenz.

**Was passiert nach vier Jahren?** Die betreffende Version darf dann zusätzlich unter der AGPL-3.0 genutzt werden.
Updates, Sicherheitskorrekturen und Support für aktuelle Versionen gibt es weiterhin mit Lizenz.
