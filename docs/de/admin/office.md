# Office-Programme: Collabora, OnlyOffice, Microsoft 365

Office-Dateien der Medienbibliothek (Texte, Tabellen, Präsentationen) lassen sich direkt im Browser in einem
Office-Programm öffnen und bearbeiten. Jedes Programm ist ein eigenes Plugin; die Einstellungen liegen unter
*Administration → Collabora Online / OnlyOffice / Microsoft 365* (oder in `config.yaml` unter `plugin_settings`).

| Plugin | Schnittstelle | Schreibgeschützt ohne Download | Wasserzeichen des Betrachters |
|---|---|---|---|
| `office.collabora` – Collabora Online / CODE | WOPI | ja | ja |
| `office.onlyoffice` – OnlyOffice Docs | OnlyOffice-API (JWT) oder WOPI | ja | nein |
| `office.m365` – Microsoft 365 für das Web | WOPI (nur mit CSPP) | nein | nein |

## Wer bekommt was?

Das Wiki entscheidet bei jedem Öffnen anhand der **Rechte der Datei** – nie mehr, als man hat:

- **Bearbeiten-Recht** (und Herunterladen erlaubt): Die Datei öffnet sich zum Bearbeiten. Jedes Speichern ist eine
  neue Version (Notiz: Name des Programms), frühere Versionen lassen sich wiederherstellen.
- **Nur Ansehen mit Download-Recht:** schreibgeschützt im Office-Programm.
- **Ohne Download-Recht** (oder „Herunterladen erlaubt“ ausgeschaltet): schreibgeschützt im Office-Programm mit
  abgeschaltetem Herunterladen, Drucken, Exportieren und Kopieren – nur wenn das Plugin das durchsetzen kann und
  die Option *schreibgeschützt ohne Download* aktiv ist (Standard). Sonst nur im Browser.
- **Wasserzeichen für Betrachter:** nur geschützt ansehen und nur in Programmen, die den Namen einblenden
  (Collabora). Das Original bearbeiten nur Verwalter der Datei.

Unter *Administration → Einstellungen → Medien* wählen Sie die **Standard-Anzeige** (Browser oder ein aktives
Office-Programm). *Dateien immer direkt im Standard-Programm öffnen* gilt nur für dieses eine Programm: Ein Klick
auf eine passende Datei öffnet sie sofort dort. Die anderen Programme bietet die Detailansicht an („In … ansehen“,
„In … bearbeiten“).

## Sicherheit

- Das Office-Programm bekommt pro Öffnen einen **Zugangsschlüssel für genau eine Datei, einen Benutzer und eine
  Betriebsart** (Bearbeiten oder Ansehen). Gespeichert wird nur sein SHA-256-Hash. Gültigkeit: `media.token_ttl`
  (Standard 10 Stunden, höchstens 24).
- **Bei jedem Zugriff** prüft das Wiki die Rechte erneut: Wird ein Recht entzogen, die Datei gelöscht, der Benutzer
  gesperrt, „Herunterladen erlaubt“ abgeschaltet oder das Plugin deaktiviert, funktioniert der Schlüssel sofort nicht mehr.
- Der Schlüssel wird per POST in den Rahmen übergeben (nie in einer Browser-Adresse) und nie im Gast-Cache gespeichert.
- Mit **API-Schlüsseln** und der Kommandozeile ohne `--as` lassen sich keine Zugangsschlüssel erzeugen (sie hätten
  sonst die vollen Rechte des Benutzers statt der Grenzen des API-Schlüssels).
- Der WOPI-Endpunkt (`/api/v1/wopi/files/…`) wertet keine Sitzungen oder API-Schlüssel aus, nur den Zugangsschlüssel.
- **Proof-Keys:** Mit `verify_proof: true` (Standard) nimmt das Wiki nur WOPI-Anfragen an, die der Office-Server
  mit seinem Schlüssel aus der Discovery signiert hat (Zeitstempel höchstens 20 Minuten alt; Schlüsselwechsel werden
  automatisch erkannt). Dafür muss `server.base_url` genau die öffentliche Adresse sein, unter der der
  Office-Server das Wiki aufruft (Schema, Host, Port).
- **OnlyOffice-API:** Konfiguration, Dateiabruf und Rückmeldungen sind mit dem JWT-Geheimnis signiert; ohne Geheimnis
  ist die API-Schnittstelle aus. Gespeicherte Dateien lädt das Wiki nur von der eingestellten Server-Adresse
  (oder `internal_url`) – keine anderen Ziele (SSRF-Schutz).
- Die Seiten des Wikis erlauben die Adressen aktiver Office-Programme als Rahmen (`frame-src`, `form-action`) und bei
  der OnlyOffice-API als Skriptquelle (`script-src`) – sonst nichts.
- Jede Speicherung steht im Audit-Log (`media.office_save`, `via = office`).

## Mit Docker Compose

`./docker-setup.sh` fragt bei HTTPS über den mitgelieferten Proxy nach einem Office-Server (keiner, Collabora oder
OnlyOffice) und seinem **eigenen Host-Namen** (z. B. `office.example.com`, DNS auf diesen Server). Ein eigener Name
heißt eigene Herkunft (Origin): Das Office-Programm teilt keine Cookies mit dem Wiki. Das Skript setzt das Profil
(`COMPOSE_PROFILES=proxy,collabora` bzw. `proxy,onlyoffice`), die Adressen und bei OnlyOffice ein zufälliges
JWT-Geheimnis; das Wiki übernimmt sie als Vorgabe der Plugin-Einstellungen und wählt das Programm als
Standard-Anzeige (beides in der Administration änderbar). Caddy holt die Zertifikate auch für den Office-Host.

- Collabora (`collabora/code`, `COLLABORA_VERSION`) läuft als eigener unprivilegierter Benutzer mit nur den
  Fähigkeiten, die die Dokument-Käfige brauchen; die Admin-Konsole ist aus.
- OnlyOffice (`onlyoffice/documentserver`, `ONLYOFFICE_VERSION`) braucht etwa 4 GB RAM und startet seine Dienste im
  Container als root; es ist nur über den Proxy erreichbar und verlangt JWT bei jeder Anfrage.
- Office-Server und Wiki erreichen einander über den Proxy im Docker-Netz (ohne Umweg über das Internet).
- Die CI startet beide echten Server und öffnet, bearbeitet und speichert in einem echten Browser je ein Dokument
  (`scripts/test-docker-office.sh`).

## Collabora Online

```yaml
plugin_settings:
  office.collabora:
    url: https://collabora.example.com     # Discovery: /hosting/discovery
    verify_proof: true
    readonly_without_download: true
```

Der Collabora-Server muss das Wiki unter `server.base_url` erreichen und es als WOPI-Host erlauben
(`coolwsd.xml`: `storage.wopi.alias_groups` bzw. `aliasgroup1=https://wiki.example.com:443` im Docker-Image).

## OnlyOffice Docs

```yaml
plugin_settings:
  office.onlyoffice:
    url: https://docs.example.com
    mode: api                     # oder wopi (Document Server: wopi.enable = true)
    jwt_secret: ${ONLYOFFICE_JWT} # = services.CoAuthoring.secret.*.string
    internal_url: http://onlyoffice   # optional, wenn gespeicherte Dateien intern ausgeliefert werden
```

## Microsoft 365 für das Web

Microsoft bedient nur WOPI-Hosts aus dem **Cloud Storage Partner Program (CSPP)**; Microsoft schaltet die Domain des
Wikis frei. Das Plugin ruft Microsoft-Server erst auf, wenn es eingeschaltet ist:

```yaml
plugin_settings:
  office.m365:
    enabled: true
    url: https://onenote.officeapps.live.com/hosting/discovery   # Standard (Produktion)
    verify_proof: true
```

Microsoft 365 kann Herunterladen und Drucken nicht verlässlich sperren: Benutzer ohne Download-Recht sehen Dateien
deshalb nur im Browser.

## Kommandozeile und API

```bash
ewiki media viewers                                   # Browser und aktive Office-Programme
ewiki media programs 42 --as anna                     # wie anna die Datei öffnen darf
ewiki media open 42 --as anna --viewer office.collabora --mode view
ewiki api POST /api/v1/x/office.collabora/test        # Verbindung testen
```

API: `GET /api/v1/media/viewers`, `GET|POST /api/v1/media/{id}/open`, WOPI-Host unter `/api/v1/wopi/files/{id}`.
