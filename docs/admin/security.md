# Sicherheit, Anmeldung und Berechtigungen

## Anmeldung

### Reihenfolge der Anmeldeverfahren
Anmeldeverfahren sind Plugins. Beim Login werden sie in der Reihenfolge `auth.order` geprüft. **Lokale Konten (`auth.local`) werden immer zuerst geprüft** und lassen sich nicht deaktivieren. Ein lokaler Administrator kommt also auch dann ins System, wenn LDAP, OIDC oder Shibboleth ausfallen.

LDAP, OpenID Connect und Shibboleth/SAML sowie die Zuordnung externer Gruppen zu lokalen Gruppen und Rollen sind in [authentication.md](authentication.md) beschrieben.

Jede Identität ist intern eindeutig als `name@anbieter` gespeichert (`admin@local`, `max@ldap`). Gleichnamige Konten verschiedener Anbieter kollidieren nicht. Mit `name@anbieter` im Login-Feld wählt man gezielt ein Verfahren.

### Lokale Konten
- Passwörter werden mit **Argon2id** gehasht (19 MiB, t=2, p=1, OWASP-Empfehlung).
- Passwortregel: Mindestlänge `auth.min_password_length`, mindestens drei von vier Zeichenklassen.
- Nach `auth.max_login_failures` Fehlversuchen ist das Konto für `auth.lockout_duration` gesperrt. Zusätzlich gibt es ein Rate-Limit je IP und je Benutzername.
- Antwortzeiten verraten nicht, ob ein Konto existiert.
- Mehrere Faktoren als Plugins (Authenticator-App, Code per E-Mail, Sicherheitsschlüssel/Passkeys, erlaubte Netze) in einer frei wählbaren Kette pro Anmeldeverfahren, siehe [authentication.md](authentication.md#mehrere-faktoren-die-faktorkette).
- Setzt ein Administrator ein Passwort neu oder deaktiviert ein Konto, werden alle Sitzungen dieses Kontos beendet.
- Das System verhindert, dass der letzte aktive Administrator gelöscht, deaktiviert oder herabgestuft wird.

### Browser-Sitzungen
- Zufälliges 256-Bit-Token im Cookie `ewiki_session` (`HttpOnly`, `SameSite=Lax`, mit `secure_cookies: true` auch `Secure`). In der Datenbank liegt nur der SHA-256-Hash.
- CSRF-Schutz: Zustandsändernde Anfragen mit Cookie-Anmeldung müssen den Header `X-Requested-With: ewiki` senden. Browser schicken eigene Header fremder Seiten nur nach einer CORS-Freigabe, und das Wiki erlaubt keine.

### API-Keys
Unter *Mein Profil → API-Keys*:
- Gültigkeit begrenzt (höchstens `auth.api_key_max_ttl`), jederzeit widerrufbar,
- Rechte-Maske (Ansehen, Bearbeiten, Erstellen, Löschen, Berechtigungen verwalten). Ein Key hat **nie mehr Rechte als sein Besitzer**, die Maske wird bei jeder Anfrage mit den aktuellen Rechten verknüpft,
- optional auf eine Kategorie samt Unterkategorien beschränkt,
- Administrationsfunktionen nur mit ausdrücklich gesetztem „Administration erlauben“.

Verwendung: `Authorization: Bearer ewk_…`

### SSH-Schlüssel (wie bei GitHub)
Öffentlichen Schlüssel unter *Mein Profil → SSH-Schlüssel* hinterlegen (ed25519, ECDSA, RSA). Danach:

```bash
TOKEN=$(scripts/ewiki-ssh-login.sh https://wiki.example.com anna ~/.ssh/id_ed25519)
curl -H "Authorization: Bearer $TOKEN" https://wiki.example.com/api/v1/auth/me
```

Ablauf: `POST /api/v1/auth/ssh/challenge` liefert eine Einmal-Challenge (2 Minuten gültig). Der Client signiert sie mit `ssh-keygen -Y sign -n ewiki-login` (funktioniert auch mit ssh-agent und Hardware-Keys). `POST /api/v1/auth/ssh/verify` gibt dann ein Token mit `auth.ssh_token_ttl` Gültigkeit zurück.

### Kommandozeile
Die lokale CLI hat ohne Anmeldung volle Rechte. Schützen Sie Binärdatei, `config.yaml` und `.env` über Dateirechte (der Installer setzt `.env` auf 0600).

## Berechtigungen

Es gibt zwei Arten von Rechten:

- **Globale Rechte** erhalten **Rollen** (*Administration → Rollen*), z. B. *Benutzer verwalten*, *Protokoll ansehen*, *Kategorie-Administrator*. Rechte, die alle angemeldeten Benutzer standardmäßig haben (z. B. *Seiten teilen*, *Personen erwähnen*), stellen Sie unter *Administration → Rollen → Standardrechte* ein. Rollen mit „Administratorrolle“ haben alle Rechte.
- **Kategorierechte** werden in Regeln je Kategorie vergeben (*Kategorie → Einstellungen → Berechtigungen* oder zentral unter *Administration → Berechtigungen*).

Plugins bringen eigene Rechte mit (z. B. *Workflows anlegen*, *Kommentare moderieren*); sie erscheinen automatisch in beiden Auswahllisten, gruppiert nach Plugin.

### Kategorierechte

| Recht | erlaubt |
|---|---|
| Ansehen | Kategorie und Seiten lesen |
| Bearbeiten | Seiten ändern, Versionen wiederherstellen |
| Erstellen | Seiten anlegen |
| Löschen / verschieben | Seiten in den Papierkorb legen, verschieben, zurücksetzen |
| Verwalten | die **Kategorie selbst**: Texte, Einstellungen, Unterkategorien anlegen/verschieben/löschen, Berechtigungen (nur Rechte, die man selbst hat) |
| Plugin-Rechte | z. B. *Workflows anlegen*, *Workflow-Administrator*, *Teilnehmerlisten sehen* |

Für die Vergabe gibt es Vorlagen:

- **Leser** – Ansehen.
- **Kategorie-Editor** – arbeitet *in* der Kategorie (Seiten anlegen, bearbeiten, löschen und die Plugin-Rechte für Editoren), ändert aber die Kategorie nicht.
- **Kategorie-Manager** – zusätzlich *Verwalten* und die Plugin-Rechte für Manager.
- **Kategorie-Administrator** ist ein globales Recht: Nur wer es hat, legt Kategorien der obersten Ebene an und verschiebt Kategorien in die oberste Ebene. Er verwaltet Struktur, Einstellungen und Berechtigungen aller Kategorien, **sieht Inhalte aber nur mit dem Recht Ansehen**.

Regeln gelten für einen **Benutzer**, eine **Gruppe**, eine **Rolle**, **alle angemeldeten Benutzer** oder **Gäste**. Mit „gilt auch für Unterkategorien“ wird eine Regel vererbt. Die wirksamen Rechte sind die Vereinigung aller passenden Regeln der Kategorie und ihrer Vorfahren.

Grundsätze:
- **Ohne Regel ist nichts erlaubt.** Eine neue Unterkategorie erbt die Rechte der übergeordneten Kategorie. Eine neue Kategorie der obersten Ebene erhält nur eine Regel für ihren Ersteller (Kategorie-Manager); Gäste sehen sie erst, wenn Sie es erlauben.
- *Administration → Berechtigungen* zeigt für einen Benutzer, eine Gruppe oder Rolle alle Kategorien mit direkten und wirksamen Rechten und ihrer Herkunft; Regeln lassen sich dort ändern.
- Rechte hängen an der stabilen Kategorie-ID. Verschieben oder Umbenennen ändert die Regeln nicht.
- Gelöschte Kategorien (Papierkorb) sind nur für Administratoren erreichbar.
- Jede API-Anfrage wird im Backend geprüft. Die Oberfläche blendet nur aus, was ohnehin verboten ist.
- API-Schlüssel tragen globale Rechte nur, wenn sie mit „Administratorrechte“ erstellt wurden.
- Gruppen können Rollen erhalten. Gruppen externer Anbieter (LDAP/OIDC/SAML) werden beim Login synchronisiert.

### Workflows

Workflows ändern und löschen nur ihr **Ersteller** (Recht *Workflows anlegen*) und **Workflow-Administratoren**. Ändert ein Administrator einen fremden Workflow, wird jede Änderung ausführlich protokolliert und der Ersteller benachrichtigt. Schritte eines Vorgangs führen **nur die beim Schritt eingetragenen Zuständigen** aus – auch Administratoren nicht, solange sie nicht zuständig sind. *Vorgänge verwalten* erlaubt, alle Vorgänge zu sehen, abzubrechen und über fehlgeschlagene Schritte zu entscheiden.

### Entwürfe

Entwürfe sehen ihr Autor und die Editoren der Kategorie (Recht *Bearbeiten*). Veröffentlicht werden kann nur dort, wo man *Erstellen* (neue Seite) bzw. *Bearbeiten* (bestehende Seite) hat. Geplante Veröffentlichungen laufen mit den Rechten des Autors zum Zeitpunkt der Veröffentlichung.

## Übersetzungen der Oberfläche

*Administration → Sprachen* zeigt je Sprache, wie viel übersetzt ist, wie viele Texte fehlen und wie viele **veraltet** sind (der englische Text hat sich seit der Übersetzung geändert, z. B. durch ein Plugin-Update).

- **Rechte:** *Sprachen verwalten und Oberfläche übersetzen* (`core.i18n.manage`) erlaubt alles: Sprachen hinzufügen/entfernen und alle Sprachen übersetzen. *Oberfläche übersetzen: <Sprache>* (`core.i18n.translate.<code>`, je konfigurierter Sprache) erlaubt nur das Übersetzen, Importieren und Exportieren dieser einen Sprache. Wer eines der Rechte hat, sieht die Seite *Sprachen*; die Übersetzungsseite anderer Sprachen bleibt gesperrt (auch in der API).
- **Übersetzungsseite:** Fortschritt je Komponente (Kern und jedes Plugin), Filter *Zu erledigen*, *Fehlend*, *Veraltet*, *Hier übersetzt*, *Alle*, Suche in Schlüsseln und Texten, 100 Einträge je Seite. Veraltete Übersetzungen bestätigen Sie mit *Weiterhin korrekt*. Ein geleertes Feld entfernt die eigene Übersetzung, dann gilt wieder der mitgelieferte Text.
- **Pluralformen:** Texte mit Anzahl haben je Pluralform ein Feld (nach CLDR: Deutsch und Englisch *eins*/*andere*, Polnisch *eins*/*wenige*/*viele*/*andere*, Arabisch sechs Formen). Neben jedem Feld stehen Beispielzahlen.
- **Import und Export** (unter *Import und Export*): **JSON** (wie die Locale-Dateien), **XLIFF 2.0** (Übersetzungswerkzeuge wie Trados, memoQ, Phrase; je Komponente eine `<file>`) und **PO** (gettext, z. B. Poedit, Weblate; `msgctxt` = Schlüssel, jede Pluralform als eigener Eintrag). Exportieren lassen sich alle, nur fehlende/veraltete oder nur hier übersetzte Texte, auf Wunsch nur einer Komponente. Beim Import wird das Format erkannt; unbekannte Schlüssel, leere Texte und als unfertig markierte Einträge (PO `fuzzy`, XLIFF `state="initial"`) werden übersprungen, der Rest gilt als hier übersetzt. Veraltete Texte werden als `fuzzy` bzw. `initial` exportiert.
- Jede Änderung und jeder Import wird protokolliert (`i18n.translate`, `i18n.import`).

API: `GET|PUT /api/v1/admin/i18n/{lang}`, `GET /api/v1/admin/i18n/{lang}/export?format=json|xliff|po&source=&filter=all|missing|custom`, `POST /api/v1/admin/i18n/{lang}/import?format=` (Datei als Body). CLI: `ewiki i18n status|strings|set|export|import|add|remove`.

## Reaktionen, Karussells, Seiteneigenschaften

- **Reaktionen** (`core.reactions.use`, standardmäßig alle Angemeldeten): nur auf sichtbare Inhalte. `core.reactions.manage` sperrt Emojis (*Administration → Reaktionen*, gilt für alle Hauttöne; vorhandene Reaktionen damit werden ausgeblendet) und entfernt Reaktionen anderer.
- **Karussells:** `core.carousel.manage` legt Karussells an und bearbeitet Folien und Feeds, `core.carousel.publish` erlaubt „Als Neuigkeit“ beim Veröffentlichen. Externe Feeds werden über den SSRF-Schutz abgerufen: interne Adressen nur mit der Plugin-Einstellung *Feeds auf internen Adressen erlauben*. Links und Bilder eigener Folien müssen Pfade der Website oder http(s)-Adressen sein, Bilder brauchen einen Alternativtext.
- **Bereiche:** Logos nur aus dem Wiki (keine fremden Adressen, kein Tracking), Akzentfarben mit geprüftem Kontrast, nicht erlaubte Makros werden serverseitig nicht ausgeführt. Einstellungen eines Bereichs ändert nur, wer *Verwalten* auf seiner obersten Kategorie hat (protokolliert als `space.update`). Verknüpfungen nur als Pfad des Wikis oder http(s) (keine `javascript:`- oder `//`-Adressen). Erlaubte Elementtypen gelten serverseitig beim Anlegen, Veröffentlichen, Kopieren und Verschieben. Schlüssel, Name und Verknüpfungen eines Bereichs bekommt nur, wer die oberste Kategorie sehen darf. **Archiv:** archivierte Kategorien sind auf Ebene der Rechteprüfung schreibgeschützt (nur *Ansehen* und *Verwalten* zum Wiederherstellen bleiben, auch für Administratoren; keine benannten Aktionen wie Kommentieren). **Einstufung:** wird in der Rechteprüfung selbst durchgesetzt (ab *Intern* nie für Gäste, unabhängig von Regeln); ab *Vertraulich* nicht in Feeds, Kalender-Abos und E-Mail-Inhalten; *Streng vertraulich* nicht in der allgemeinen und externen Suche (Änderungen der Einstufung oder Verschieben stoßen die Neuindizierung an), kein Export, keine Kopien; Medien: ab *Vertraulich* Wasserzeichen mit Name und Zeit bei Bildern und PDFs (auch bei Bildern, die nur in vertraulichen Seiten eingebettet sind; nie zwischengespeichert), *Streng vertraulich* kein Herunterladen von Medien und Datei-Elementen ohne Verwaltungsrecht. Es gilt immer die strengste Einstufung darüber; bei einem Fehler wird die strengste angenommen. **Auswertung:** Aufrufe nur als Zahl pro Seite und Tag, keine personenbezogenen Daten, Löschung nach 400 Tagen. **Zugriffsanfragen:** Die Antwort ist immer gleich, egal ob das Ziel existiert (keine Auskunft über verborgene Inhalte); höchstens 10 Versuche pro Person und Tag; nur Verwaltende der Kategorie sehen und beantworten Anfragen; gewährt wird eine normale, protokollierte Regel. **Persönliche Bereiche:** eigene Berechtigung, einer pro Person, der Besitz ist nicht änderbar. **Export/Import:** Export nur mit *Verwalten* und nur Sichtbares; Import läuft vollständig über die normalen Rechte- und Inhaltsprüfungen (Elementtypen, Bereinigung, erlaubte Typen), höchstens 50 MB, ohne Rechte und Dateien.
- **Quellen (Makros, Blog, FAQ, Kalender, Karussell, Berichte):** Quellen werden immer mit den Rechten der lesenden Person aufgelöst – Elemente, Kalender und Kategorien, die sie nicht sehen darf, erscheinen nicht (auch nicht als Abschnittsname oder Anzahl). Höchstens 50 Quellen je Element/Makro, 1000 einzelne Elemente, 10 Feeds je Makro. Externe Feeds und iCal-Abonnements werden nur im Hintergrund über den SSRF-Schutz abgerufen (interne Adressen nur mit `feeds.allow_private`), höchstens 2 MB, ohne Zugangsdaten. Inhalte daraus werden maskiert: Titel und Zusammenfassungen als Text, Links nur `http(s)` mit `rel="noopener noreferrer nofollow"`, Bilder mit `referrerpolicy="no-referrer"`, Beschreibungen fremder Kalender als reiner Text (keine fremden Bilder oder Links, die nachladen).
- **Seiteneigenschaften:** lesen mit *Ansehen*, ändern mit *Bearbeiten* der Kategorie; Änderungen werden protokolliert (`element.properties`).

## Protokoll (Audit-Log)

*Administration → Protokoll* (Recht *Protokoll ansehen*) listet alle Änderungen mit Zeitpunkt, Person, IP-Adresse, Browser und Kanal (Oberfläche, API, CLI, Hintergrundjob). Filter: Freitext, Bereich, Aktion, Person, IP, Zeitraum, Objekt; Details zeigen den Zustand vorher und nachher. Export als CSV oder JSON Lines.

Mit *Protokoll konfigurieren* legen Sie fest, was protokolliert wird:

- Bereiche lassen sich abschalten – außer sicherheitsrelevanten (Rechte, Rollen, Benutzer, Anmeldung, Protokoll-Einstellungen), die immer protokolliert werden.
- Anmeldungen (erfolgreich und fehlgeschlagen), abgewiesene Zugriffe (403) und auf Wunsch Seitenaufrufe.
- **Weiterleitung** an andere Systeme: Syslog (UDP/TCP/TLS, RFC 5424), Webhook (JSON, HMAC-signiert, eigene Header), OpenTelemetry-Logs (OTLP/HTTP), Datei (JSON Lines) oder Standardausgabe. Je Ziel wählen Sie die Bereiche; jedes Ziel merkt sich die letzte gesendete Zeile und holt bei Ausfällen nach. *Test* sendet einen Probe-Eintrag, die Übersicht zeigt Rückstand und letzten Fehler.

API: `GET /api/v1/admin/audit/search`, `/admin/audit/export`, `GET|PUT /admin/audit/settings`, `POST /admin/audit/forward/{id}/test`. CLI: `ewiki audit search …`.

## Benachrichtigungen, Teilen, Erwähnungen, zugewiesene Beobachtungen

- Die **Glocke** neben dem Benutzermenü zeigt Benachrichtigungen im Wiki (Erwähnungen, geteilte Seiten, Änderungen an Workflows). Zusätzlich gehen E-Mails, soweit der Benutzer sie im Profil nicht abgeschaltet hat (Standard: an).
- **Teilen** (Plugin `core.share`, Recht *Seiten teilen*) weist Personen, Gruppen oder Rollen auf eine Seite hin. Teilen **ändert keine Rechte**: Wer die Seite nicht sehen darf, wird nicht benachrichtigt; der Dialog zeigt das vorher an.
- **@Erwähnungen** (Plugin `core.mentions`, Recht *Personen erwähnen*) in Seiten und Kommentaren benachrichtigen die genannten Personen – ebenfalls nur, wenn sie die Seite sehen dürfen.
- **Zugewiesene Beobachtungen** (*Administration → Zugewiesene Beobachtungen*, Recht *Beobachtungen zuweisen*): Benutzer, Gruppen oder Rollen beobachten eine Kategorie (optional mit Unterkategorien) oder Seite. Beim Zuweisen wählen Sie **verpflichtend** (nicht abbestellbar) oder **optional** (im Profil abbestellbar), dazu Inhalt der E-Mail, Häufigkeit und einen Hinweis für die Benutzer.

## Schutz der Weboberfläche

- Seiteninhalte werden beim Speichern **und** beim Anzeigen serverseitig bereinigt (bluemonday-Richtlinie für Benutzerinhalte: keine Skripte, Event-Handler, `javascript:`-Links, iframes oder Inline-Styles).
- Die Oberfläche erzeugt alle Elemente über DOM-APIs, nicht über `innerHTML` mit Benutzerdaten. Ausnahme ist der bereits bereinigte Seiteninhalt.
- Strikte Content Security Policy (`script-src 'self'`, kein Inline-JavaScript, `frame-ancestors 'self'`, `object-src 'none'`), dazu `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, `Permissions-Policy` und HSTS (mit `secure_cookies`).
- Menülinks erlauben nur `http(s)`, `mailto` und relative Pfade.
- Hochgeladene Dateien: Der Typ wird aus dem Inhalt bestimmt, nicht aus Angaben des Browsers. HTML, SVG und Skripte werden nie im Browser angezeigt, sondern nur heruntergeladen (`Content-Disposition: attachment`, `nosniff`, eigene CSP mit `sandbox`). Dateinamen werden bereinigt. Dateien eines Elements gibt es nur mit Leserecht auf das Element. Bilder sind zusätzlich über ihren nicht erratbaren SHA-256-Hash abrufbar, damit Bilder in öffentlichen Seiten auch für Gäste funktionieren.
- **Live-Bearbeitung** (WebSocket `/api/v1/collab/elements/{id}/{lang}`): nur angemeldete Benutzer mit Browser-Sitzung (keine API-Schlüssel) und Bearbeitungsrecht; die Herkunft (`Origin`) muss der eigene Host sein (kein Cross-Site-WebSocket). Sitzung und Recht werden alle 30 s neu geprüft; endet eines, wird die Verbindung getrennt. Der Server speichert nichts und leitet nur Yjs-Nachrichten weiter (Typ geprüft, höchstens 4 MiB je Nachricht, 100 Nachrichten/s je Person, 50 Personen je Seite, langsame Empfänger werden getrennt, keine Kompression). Name und Kennung im Cursor setzt der Server aus der Anmeldung ein – niemand kann unter fremdem Namen erscheinen oder Cursor anderer entfernen. Gespeichert wird weiterhin über die normale API mit allen Prüfungen und Bereinigungen.
- Konfigurationsgeheimnisse stehen nur in der Umgebung bzw. in `.env`. Die Datenbank-URL wird in der Oberfläche maskiert.
- Alle sicherheitsrelevanten Änderungen landen im Audit-Protokoll.

## Makros mit Systemzugriff

Das Shell- und das HTTP-Makro sind die einzigen Stellen, an denen Seiteninhalte etwas auf dem Server auslösen. Deshalb gilt:

- Die Schalter stehen nur in `config.yaml` (`plugins.macros`). Über Oberfläche und API lassen sie sich nicht ändern, auch nicht von Administratoren.
- **Shell:** standardmäßig aus. Redakteure können den Befehl zwar in eine Seite schreiben, er läuft aber erst nach der Freigabe genau dieses Befehlstexts (SHA-256) durch einen Superadmin aus `shell_approvers` oder die CLI. API-Keys können nie freigeben. Befehle laufen mit `/bin/sh -c` in einem leeren temporären Verzeichnis, mit minimaler Umgebung (keine Secrets aus `.env`), eigener Prozessgruppe, Zeitlimit und auf 64 KB begrenzter, maskierter Ausgabe. Läuft das Wiki als root, werden Befehle als `shell_user` (Standard `nobody`) ausgeführt. Freigaben, Sperren und Löschungen stehen im Audit-Protokoll. Freigegebene Befehle laufen mit den Rechten des Dienstbenutzers; geben Sie nur lesende, ungefährliche Befehle frei.
- **HTTP:** nur URLs aus der Allow-Liste; Verbindungen zu Loopback-, privaten, Link-Local- (u. a. Cloud-Metadaten 169.254.169.254) und CGNAT-Adressen werden nach der DNS-Auflösung abgelehnt (Schutz gegen SSRF und DNS-Rebinding), Weiterleitungen werden gegen dieselben Regeln geprüft. Antworten sind auf 1 MB und 15 Sekunden begrenzt; HTML wird bereinigt, Text und JSON werden maskiert.
- Makros werden bei jeder Anzeige mit den Rechten des Betrachters gerendert, z. B. listet *Unterseiten* nur sichtbare Kategorien.

## E-Mail und Webhooks

- Benachrichtigungen (Aufgaben, Erinnerungen, Kommentare) gehen nur an Benutzer, die die betroffene Seite sehen dürfen, und lassen sich im Profil abschalten.
- Workflow-E-Mails dürfen nur die konfigurierte Absenderadresse oder Adressen aus `mail.allowed_from` als Absender verwenden; alles andere wird zur Antwortadresse (Schutz vor gefälschten Absendern). Kopfzeilen werden von Zeilenumbrüchen bereinigt (keine Header-Injection), Empfängeradressen geprüft, höchstens 50 Empfänger je Nachricht.
- Webhooks von Workflows unterliegen denselben Regeln wie das HTTP-Makro (Allow-Liste `plugins.macros.http_allow`, interne Adressen gesperrt). Mit einem Geheimnis wird jede Anfrage per HMAC-SHA256 signiert (`X-Ewiki-Signature: sha256=…`), damit der Empfänger die Herkunft prüfen kann. Das Geheimnis steht im Workflow und ist für Bearbeiter des Workflows sichtbar.
- Das SMTP-Passwort wird in der Oberfläche maskiert; empfohlen ist `${EWIKI_SMTP_PASSWORD}` mit dem Wert in `.env`.

## Workflows und Kommentare

- Einen Vorgang sehen nur die Person, die ihn gestartet hat, alle, die für einen seiner bisherigen oder den aktuellen Schritt zuständig sind oder waren, Benutzer mit dem Recht *Berechtigungen verwalten* auf der Kategorie und Administratoren. Dateien aus Formularen werden nur mit dieser Prüfung ausgeliefert.
- Aktionen darf nur ausführen, wer für den aktuellen Schritt zuständig ist (Administratoren als Vertretung). Jede Aktion steht im Verlauf des Vorgangs und im Audit-Protokoll.
- Kommentare: Lesen mit Leserecht, Schreiben angemeldet mit Lese- (oder, per Einstellung, Bearbeitungs-)recht; löschen dürfen Verfasser, Benutzer mit *Berechtigungen verwalten* und Administratoren. Der Text gelöschter Kommentare bleibt im Audit-Protokoll erhalten.
