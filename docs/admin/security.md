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
- Optional zweiter Faktor (TOTP) je Konto, siehe [authentication.md](authentication.md#zwei-faktor-authentifizierung-totp).
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
