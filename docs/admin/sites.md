# Mehrere Sites (Multi-Site)

Eine Instanz kann mehrere Sites betreiben, z. B. ein Intranet, eine öffentliche Website und ein Projekt-Wiki. Jede Site ist ein eigenständiges Wiki mit eigenen Inhalten, Einstellungen, Plugins und Rechten. Benutzer, Gruppen und Rollen-Definitionen sind gemeinsam.

## Was gemeinsam ist und was nicht

| Gemeinsam (Schema `public`) | Pro Site (Schema `site_<name>`) |
|---|---|
| Benutzerkonten (Passwort, 2FA, erlaubte Netze, Profil) | Seiten, Kategorien, Dateien, Revisionen, Suche |
| Gruppen und Mitgliedschaften | Rollen-**Zuweisungen** und Rechte (ACLs), Standardrechte |
| Rollen-**Definitionen** | Sitzungen, Anmeldevorgänge, API-Keys |
| SSH-Schlüssel | E-Mail-Warteschlange, Benachrichtigungen, Beobachtungen, Webhooks |
| Liste der Sites, Instanz-Admins | Audit-Protokoll, Übersetzungen der Oberfläche |
| Lizenz (zählt die Benutzer der ganzen Instanz) | alle Plugin-Tabellen |

Die Anmeldung ist pro Site getrennt: Eine Sitzung von Site A gilt nicht auf Site B. Jede Site legt selbst fest, welche Anmeldearten sie anbietet (lokal, LDAP, OIDC, SAML). Ist derselbe Anbieter auf zwei Sites eingerichtet, ist es dasselbe Konto. Wer beim Identity Provider schon angemeldet ist, kommt ohne erneute Passworteingabe durch.

## Gemeinsame oder eigene Konten

Beim Anlegen einer Site wird festgelegt, woher ihre Konten kommen; später lässt sich das nicht mehr ändern:

- **Gemeinsam mit der Instanz** (Standard): Benutzer, Gruppen und Rollen-Definitionen der Instanz, Rechte pro Site (siehe Tabelle oben).
- **Eigene Konten der Site**: Die Site hat eigene Benutzer, Gruppen, Rollen und SSH-Schlüssel in ihrem Schema. Sie sieht die Konten der Instanz nicht und umgekehrt – für strikt getrennte Personenkreise (z. B. eine Kunden- oder Partner-Site). Beim Anlegen wird ihr erster Administrator (lokales Konto) angegeben; danach verwalten ihre Administratoren die Konten selbst. Auf solchen Sites gibt es keine Instanz-Admins: Konten-IDs gehören dort zur Site, nicht zur Instanz.

Die Vorgabe für neue Sites steht in `config.yaml`:

```yaml
sites:
  accounts: shared   # oder separate
```

Die Lizenz zählt die aktiven Konten aller Sites zusammen (gemeinsame und eigene). Startet eine Site, prüft das Wiki, dass die Konten-Tabellen dort liegen, wo die Einstellung der Site sie erwartet, und verweigert sonst den Start – so können Abfragen nie versehentlich Konten einer anderen Site verwenden.

## Admin-Ebenen

- **Instanz-Admins** verwalten Sites, Benutzer, Gruppen, Rollen-Definitionen, die Lizenz und die Plugin-Installation. Instanz-Admin ist, wer auf der Site `main` Administrator ist oder unter *Administration → Sites → Instanz-Administratoren* eingetragen wurde.
- **Site-Admins** (Administrator-Rolle auf ihrer Site) verwalten Inhalte, Einstellungen, Plugins, Rechte und Rollen-Zuweisungen ihrer Site. Gemeinsame Daten (Konten, Gruppen) können sie nicht ändern.

## Site anlegen

In der Oberfläche unter *Administration → Sites → Neue Site* oder per CLI:

```bash
ewiki site create docs --host docs.example.org --title "Dokumentation"
ewiki site create partner --host partner.example.org --accounts separate --admin chef   # fragt nach dem Passwort
ewiki site list
ewiki site update docs --host docs.example.org,doku.example.org
ewiki site update docs --disable        # Site anhalten, Daten bleiben
ewiki site delete docs --yes            # Site mit allen Daten löschen
ewiki site admins add 42                # Instanz-Admin eintragen
```

API (nur Instanz-Admins): `GET|POST /api/v1/instance/sites`, `PATCH|DELETE /api/v1/instance/sites/{name}`, `GET|POST /api/v1/instance/admins`, `DELETE /api/v1/instance/admins/{id}`.

Der Name (`[a-z][a-z0-9_]*`, höchstens 31 Zeichen) bestimmt das Schema `site_<name>`. Jede weitere Site braucht mindestens einen Hostnamen; die Hostnamen müssen per DNS auf den Server zeigen. Anfragen an unbekannte Hostnamen beantwortet die Site `main`. Mit dem [mitgelieferten Proxy](installation.md#mitgelieferter-proxy-docker-compose-caddy) bekommt jeder neue Hostname automatisch ein Zertifikat.

## Einstellungen einer Site

Die Einstellungen in `config.yaml` sind die der Site `main` und zugleich die Vorgabe für alle anderen Sites. Was eine Site abweichend einstellt (in ihrer Administration oder per `ewiki --site docs config set …`), steht in `sites/<name>.yaml` neben `config.yaml`, und zwar nur die Abweichungen. Änderungen an `config.yaml` wirken also auf alle Sites, die den Wert nicht selbst gesetzt haben.

Nur für die ganze Instanz gelten: `mode`, `database`, `license`, `server.listen`, `limits`, `server.trusted_proxies`, `server.client_ip_header`, `server.max_body_mb`. Diese Werte werden in Site-Dateien ignoriert.

Dateien einer Site liegen in `<storage.path>/sites/<name>`, solange die Site keinen eigenen `storage.path` setzt.

## CLI für eine bestimmte Site

Lokale Befehle arbeiten auf der Site `main`. Für eine andere Site `--site NAME` oder `EWIKI_SITE=NAME` angeben:

```bash
ewiki --site docs user list
EWIKI_SITE=docs ewiki plugin disable content.blog
```

## Hinweise

- Plugins sind für die ganze Instanz einkompiliert; jede Site schaltet sie einzeln ein und aus. Plugin-Tabellen werden in jeder Site angelegt, auch wenn das Plugin dort aus ist.
- Auf Sites mit gemeinsamen Konten zeigt die Benutzersuche alle Konten der Instanz. Für getrennte Personenkreise gibt es Sites mit eigenen Konten.
- Backups: Ein `pg_dump` der Datenbank enthält alle Sites; eine einzelne Site lässt sich mit `pg_dump --schema=site_<name>` sichern.
