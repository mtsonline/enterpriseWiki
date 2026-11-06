# Medien (M13) – Entwurf

Umgesetzt in `internal/media` (Dienst, Rechte, Auslieferung, Bildbearbeitung, Wasserzeichen), `internal/server/api_media.go`, `internal/cli/media.go`, `web/src/components/media.ts` (Bibliothek, Hochladen, Details, Bildeditor, Anzeige, Auswahl) und dem Makro `gallery` (`plugins/core_macros`). Plugins erreichen den Dienst über `sdk.Host.Media`, im Frontend über `pickMedium`, `uploadDialog`, `mediaURL`, `mediaViewer` aus `@ewiki/sdk`. Offen: Registrierung von Anzeigeprogrammen (mit M14).

## Datenmodell (Site-Schema)

- `categories.hidden boolean` – unsichtbare Kategorien erscheinen nicht in Navigation, Menüs, Listen und Suche, sind aber normale Kategorien mit Rechten (frei verwaltbare „Ordner“, z. B. für Medien).
- `media` – ein Medium: `id`, `category_id`, `name`, `title`, `alt`, `description`, `current` (aktuelle Version), `download` (Herunterladen erlaubt), `own_acl` (eigene Rechte statt Kategorie/Einbindung), `watermark jsonb` (`mode`, `user`, `date`, `comment`), `created_by`, `created_at`, `updated_at`, `deleted_at`.
- `media_versions` – `media_id`, `version`, `hash` (Datei im Speicher), `mime`, `size`, `width`, `height`, `ops jsonb` (Bearbeitungsschritte), `note`, `created_by`, `created_at`. Jede Bearbeitung im Bildeditor ist eine neue Version; Einbindungen zeigen immer die aktuelle.
- `media_acl` – eigene Rechte eines Mediums: `subject_type` (user, group, role, guest, authenticated), `subject_id`, `perms` (Bits: ansehen 1, herunterladen 2, bearbeiten 4, verwalten 8).
- `media_usage` – (`media_id`, `element_id`): wird beim Speichern eines Elements aus seinem Inhalt ermittelt (Verweise `/api/v1/media/{id}/…` und Makros).

## Rechte (Reihenfolge)

1. `own_acl`: nur `media_acl` zählt (enger oder weiter als die Kategorie; vergeben kann man nur Rechte, die man selbst hat).
2. Sonst: wer die Kategorie ansehen darf **oder** ein Element, in dem das Medium eingebunden ist, darf es ansehen; Herunterladen zusätzlich nur, wenn `download` gesetzt ist. Bearbeiten/Verwalten: Bearbeitungsrecht in der Kategorie.
3. Administratoren dürfen alles.

`GET /api/v1/files/{hash}` liefert danach nur noch: Profilfotos, Bilder aus den Einstellungen (Logo usw.), Dateien von Medien, die der Aufrufer sehen darf, und Anhänge, deren Element er sehen darf. Alles andere: 404.

## Auslieferung

- `GET /api/v1/media/{id}` (Metadaten), `/api/v1/media/{id}/view` (Anzeige), `/api/v1/media/{id}/download`, `?v=` für ältere Versionen (nur mit Bearbeitungsrecht), `?w=` für verkleinerte Bilder.
- Nur anzeigen (kein Download-Recht oder `download=false`): kein Download-Knopf, kein direkter Link, `Content-Disposition: inline`, `Cache-Control: no-store`, Kontextmenü und Ziehen im Frontend gesperrt. PDFs werden seitenweise als Bilder angezeigt (poppler `pdftoppm`), damit keine Originaldatei im Browser landet.
- Wasserzeichen (`watermark.mode`):
  - `none`
  - `uploader_only` – Original wird nicht gespeichert, nur die Fassung mit Wasserzeichen des Hochladers
  - `uploader_view` – Anzeige mit Wasserzeichen des Hochladers, Original zum Herunterladen für Berechtigte
  - `viewer_view` – Name des Betrachters (und Datum) wird beim Anzeigen eingebrannt
  - `viewer_all` – beim Anzeigen und Herunterladen
  Bilder: eingebrannt (`media.Watermark`); PDFs: Seiten als Bilder mit Wasserzeichen, Download als PDF aus diesen Seiten. Fassungen pro Betrachter werden zwischengespeichert (Schlüssel: Version, Benutzer, Tag).

## Anzeigeprogramme (Office)

Office-Plugins registrieren in `Setup` ein `media.Opener` (`host.Media.RegisterOpener`): `ID`, `Label`, `Caps`
(`Restrict`: kann Download/Druck/Kopieren sperren, `Watermark`: blendet Text ein, `Script`: Frontend lädt ein Skript),
`Supports(name, mime)` → ansehen/bearbeiten, `Launch` → `media.Action` (wie der Browser startet) und `Origins` (für die
CSP). Optional: `OptionsProvider` (z. B. schreibgeschützt ohne Download), `ProofVerifier` (WOPI-Proof-Keys),
`FileInfoExtender` (eigene CheckFileInfo-Felder).

Für WOPI-Programme gibt es `pkg/wopi`: Discovery (Cache, Schlüsselwechsel), Proof-Key-Prüfung und `wopi.Opener`,
der ein vollständiges `media.Opener` ist – ein WOPI-Plugin braucht nur Einstellungen und Namen (siehe
`plugins/office_collabora`). Den WOPI-Host (`/api/v1/wopi/files/{id}`: CheckFileInfo, GetFile, PutFile, Lock,
GetLock, RefreshLock, Unlock) stellt der Kern.

Eigene Endpunkte für Office-Server (z. B. OnlyOffice-API) meldet das Plugin mit `host.Media.RegisterTokenPath`
an: Dort wertet der Kern keine Sitzungen aus; der Handler ruft `host.Media.TokenContext(ctx, id, token)` (prüft
Schlüssel und Rechte) und dann `Content` bzw. `SaveFromOffice`.

Der Kern entscheidet Programm und Betriebsart (`modesFor`): nie mehr als die Rechte der Datei, Wasserzeichen nur
geschützt, ohne Download nur mit `Restrict` und Option. Zugangsschlüssel: `media_access_tokens` (SHA-256),
Sperren: `media_locks`. Im Browser startet `registerOfficeRenderer(kind, fn)` das Programm (`wopi` im Kern,
`onlyoffice` im Plugin), die Seite `/office/{id}?viewer=…&mode=…` zeigt es.

## Oberfläche, CLI, API

- Medienbibliothek (Kategorien-Baum inkl. unsichtbarer, Raster/Liste, Suche, Filter nach Typ), Hochladen mit Rechten, Download-Schalter und Wasserzeichen-Optionen, Detailansicht mit Versionen und Verwendungen, Bildeditor.
- Einfügen im Editor über die Medienbibliothek (auch Hochladen direkt dorthin, Standard: Kategorie der Seite).
- Makro `gallery` (Medien einer Kategorie oder ausgewählte, Lightbox, Tastatur, barrierefrei).
- CLI: `ewiki media list|upload|info|versions|edit|delete|acl|viewers|programs|open`.
