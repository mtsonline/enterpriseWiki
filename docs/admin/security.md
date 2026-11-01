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

Inhalte sind in **Kategorien** organisiert. Rechte werden je Kategorie vergeben (*Kategorie → Einstellungen → Berechtigungen*):

| Recht | erlaubt |
|---|---|
| Ansehen | Kategorie und Seiten lesen |
| Bearbeiten | Seiten und Kategorietexte ändern, Versionen wiederherstellen |
| Erstellen | Seiten und Unterkategorien anlegen |
| Löschen / verschieben | in den Papierkorb legen, verschieben, zurücksetzen |
| Berechtigungen verwalten | Regeln dieser Kategorie ändern (nur Rechte, die man selbst hat) |

Regeln gelten für einen **Benutzer**, eine **Gruppe**, eine **Rolle**, **alle angemeldeten Benutzer** oder **Gäste**. Mit „gilt auch für Unterkategorien“ wird eine Regel vererbt. Die wirksamen Rechte sind die Vereinigung aller passenden Regeln der Kategorie und ihrer Vorfahren. Es gibt beliebig viele Regeln.

Grundsätze:
- **Ohne Regel ist nichts erlaubt.** Nicht angemeldete Besucher haben keine Rechte. Ausnahme: Neue Kategorien der obersten Ebene erhalten automatisch „Gäste dürfen ansehen“ (ohne Vererbung). Diese Regel lässt sich entfernen.
- Rollen mit „Administratorrolle“ haben überall vollen Zugriff.
- Rechte hängen an der stabilen Kategorie-ID. Verschieben oder Umbenennen ändert die Regeln nicht, die angezeigte Gliederungsnummer (1.2.3) passt sich an.
- Gelöschte Kategorien (Papierkorb) sind für niemanden außer Administratoren erreichbar.
- Jede API-Anfrage wird im Backend geprüft. Die Oberfläche blendet nur aus, was ohnehin verboten ist.
- Gruppen können Rollen erhalten. Gruppen externer Anbieter (LDAP/OIDC, ab Meilenstein 2) werden beim Login synchronisiert.

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
