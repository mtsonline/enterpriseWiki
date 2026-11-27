# Entwickler-Leitfaden: Was steht wo

Dieser Leitfaden sagt für jeden Bereich des Wikis, in welchem Ordner und in welcher Datei er liegt und was dort geschieht. Er ist für neue Entwicklerinnen und Entwickler und für alle, die die Ursache eines Fehlers suchen. Wer Code anlegt, verschiebt oder umbenennt, zieht diesen Leitfaden im selben Pull Request nach; `go run ./tools/docscheck` prüft, dass jede verlinkte Datei existiert und jedes Paket und jedes Plugin genannt ist. Wie die Teile zusammenspielen, beschreibt [architecture.md](architecture.md), Plugins [plugins.md](plugins.md), die API [api.md](api.md) und die Sicherheitstests [security-testing.md](security-testing.md).

## Konventionen

- **Ein Bereich, eine Datei.** Eine Datei heißt nach dem, was sie tut: Die Routen der API für Webhooks stehen in `internal/server/api_webhooks.go`, der Dienst dahinter in `internal/webhook/`. Auch Tests heißen nach ihrem Thema (`webhooks_test.go`).
- **Handler sind dünn, Dienste prüfen.** Eine Route in `internal/server` liest die Anfrage und ruft einen Dienst. Der Dienst (`content`, `auth`, `media` …) prüft das Recht selbst, so nehmen API, Kommandozeile und Plugins denselben Weg.
- **Jede Route hat eine Aktion.** Über jeder Route steht `// action: <id>`; der Eintrag im Aktionsregister (`internal/actions/core/*.yaml`, `plugins/*/actions.yaml`) beschreibt sie: Recht, Kommandozeile, Oberfläche, Beispiele. Wer die Aktions-ID sucht, findet Route, Befehl und Tests auf einmal.
- **Kommentare sagen, warum.** Jedes Paket beginnt mit einem Paketkommentar; lesen Sie ihn zuerst. Kommentare nennen die Regel, die gilt (Sicherheit, Grenzwert, Transaktion), nicht, was die nächste Zeile tut.

## Die Ursache eines Fehlers finden

Beginnen Sie dort, wohin das Anzeichen zeigt:

| Anzeichen | Wo zuerst suchen |
|---|---|
| `403` mit Code `forbidden` | die Prüfung im Dienst ([`internal/acl/`](../../../internal/acl/), [`internal/perm/`](../../../internal/perm/)); die Route in `internal/server/api_*.go`; mit `--as`: [`internal/identity/`](../../../internal/identity/) |
| `403` mit Code `waf_blocked` | die Firewall des Wikis: das Protokoll des Servers (`waf:` nennt die Regeln), [`plugins/core_waf/exclusions.conf`](../../../plugins/core_waf/exclusions.conf) |
| `500` oder `request failed` im Protokoll | die Protokollzeile nennt Methode und Pfad; den Pfad in `internal/server/` suchen (oder `plugins/*/` für `/api/v1/x/…`); Fehler sind in [`internal/apperr/`](../../../internal/apperr/) typisiert |
| Das Wiki startet nicht | Konfiguration [`internal/config/`](../../../internal/config/), Datenbank und Migrationen [`internal/db/db.go`](../../../internal/db/db.go) (eine veraltete Entwicklungsdatenbank wird dort abgelehnt), Verdrahtung [`internal/app/app.go`](../../../internal/app/app.go) |
| Ein Text fehlt oder ist englisch | `internal/i18n/locales/` und `plugins/*/locales/` (Oberfläche), `internal/apperr/locales/` (Fehlermeldungen); gesammelt von [`internal/i18n/i18n.go`](../../../internal/i18n/i18n.go) |
| Eine Seite sieht falsch aus | die Ansicht in `web/src/views/`, Stile in `web/src/styles/`, das Template in [`plugins/core_template/`](../../../plugins/core_template/); der Inhalt einer Seite wird in [`internal/content/macros.go`](../../../internal/content/macros.go) bereinigt und dargestellt |
| Ein Baustein (Makro) zeigt nichts oder Falsches | das Plugin des Bausteins ([`plugins/core_macros/`](../../../plugins/core_macros/), [`plugins/core_layout/`](../../../plugins/core_layout/) oder das in der Bausteinliste genannte), Darstellung in [`internal/content/macros.go`](../../../internal/content/macros.go) |
| Eine E-Mail oder Benachrichtigung kommt nicht an | *Administration → E-Mail* (Postausgang), [`internal/mail/`](../../../internal/mail/), [`internal/notify/`](../../../internal/notify/); Folgearbeit von Änderungen: [`internal/content/changes.go`](../../../internal/content/changes.go) und der Worker in [`internal/worker/`](../../../internal/worker/) |
| Die Anmeldung scheitert | [`internal/auth/service.go`](../../../internal/auth/service.go) (Kette der Verfahren), das Plugin des Verfahrens (`plugins/auth_*`, `plugins/mfa_*`), Routen in [`internal/server/api_auth.go`](../../../internal/server/api_auth.go) und [`internal/server/api_sso.go`](../../../internal/server/api_sso.go) |
| Die Suche findet zu viel oder zu wenig | [`internal/content/search.go`](../../../internal/content/search.go) (PostgreSQL), [`plugins/search_external/`](../../../plugins/search_external/) (externe Suchmaschinen); die Rechte wirken in der Suche selbst |
| Ein Workflow-Schritt läuft nicht | [`plugins/workflow_process/engine.go`](../../../plugins/workflow_process/engine.go) und die Datei des Schritts in [`plugins/workflow_process/`](../../../plugins/workflow_process/); Fristen in `timers.go`; das Protokoll des Servers (`workflow step failed`) |
| Die falschen Personen dürfen einen Workflow-Schritt bearbeiten oder die richtigen bekommen keine Aufgabe | `assignees` und `assigned` in [`plugins/workflow_process/plugin.go`](../../../plugins/workflow_process/plugin.go) (wer handeln darf), `membersOf` in [`plugins/workflow_process/engine.go`](../../../plugins/workflow_process/engine.go) (wer die Aufgabe bekommt und wie viele Stimmen eine Freigabe braucht), das Feld im Editor [`plugins/workflow_process/web/assignees.ts`](../../../plugins/workflow_process/web/assignees.ts) |
| Ein Befehl von `ewiki` verhält sich anders als die Oberfläche | Befehle gehen im Prozess über die API ([`internal/cli/commands.go`](../../../internal/cli/commands.go), `serveInProcess`); erzeugte Befehle kommen aus dem Aktionsregister ([`internal/cli/register.go`](../../../internal/cli/register.go)) |

## Der Weg einer Anfrage

1. [`cmd/ewiki/main.go`](../../../cmd/ewiki/main.go) startet die Kommandozeile ([`internal/cli/cli.go`](../../../internal/cli/cli.go)); `ewiki serve` baut die App.
2. [`internal/app/app.go`](../../../internal/app/app.go) verdrahtet Konfiguration, Datenbank, Dienste und Plugins; [`internal/app/instance.go`](../../../internal/app/instance.go) hält, was alle Websites einer Instanz teilen.
3. [`internal/server/instance.go`](../../../internal/server/instance.go) wählt die Website nach dem Hostnamen; [`internal/server/middleware.go`](../../../internal/server/middleware.go) (`wrap`, `authenticate`) setzt die Sicherheits-Header, begrenzt die Anfrage, ermittelt die Adresse des Clients und die angemeldete Person.
4. [`internal/server/api.go`](../../../internal/server/api.go) meldet alle Routen an (`routes`) und umhüllt jeden Handler (`handle`): Recht der Route, JSON-Antwort, Fehlerformat. Die Routen selbst stehen in den `api_*.go`-Dateien unten.
5. Der Handler ruft einen Dienst ([`internal/content/`](../../../internal/content/), [`internal/auth/`](../../../internal/auth/), [`internal/media/`](../../../internal/media/) …), der das Recht prüft und in PostgreSQL schreibt; Änderungen hält [`internal/audit/`](../../../internal/audit/) fest.
6. Routen von Plugins (`/api/v1/x/<plugin>/…`) meldet das Plugin in seinem `Setup` über [`internal/coreplugin/coreplugin.go`](../../../internal/coreplugin/coreplugin.go) an.

## Pakete des Kerns (`internal/`)

Jedes Paket beginnt mit einem Paketkommentar, der es genauer erklärt.

| Paket | Was es tut |
|---|---|
| [`acl`](../../../internal/acl/) | Rechte von Kategorien und Elementen, geprüft aus einem Abbild im Speicher |
| [`actions`](../../../internal/actions/) | das Aktionsregister: ein Eintrag je Aktion der API (Kern in `core/*.yaml`) |
| [`app`](../../../internal/app/) | Verdrahtung von Konfiguration, Datenbank, Diensten und Plugins; Websites einer Instanz |
| [`apperr`](../../../internal/apperr/) | typisierte Fehler und ihre Meldungen auf Deutsch und Englisch |
| [`audit`](../../../internal/audit/) | das unveränderliche Audit-Log (vorher und nachher jeder Änderung) |
| [`auth`](../../../internal/auth/) | Anmeldeverfahren, Sitzungen, API-Schlüssel, SSH-Schlüssel, Benutzer, Gruppen, Rollen, SCIM, Mehr-Faktor-Anmeldung |
| [`catlist`](../../../internal/catlist/) | gemeinsame Teile von Typen, die eine Kategorie darstellen (Blog, FAQ) |
| [`cli`](../../../internal/cli/) | die Kommandozeile `ewiki`; jeder Befehl geht im Prozess über die API |
| [`collab`](../../../internal/collab/) | Weiterleitung des gemeinsamen Bearbeitens zwischen Editoren |
| [`config`](../../../internal/config/) | `config.yaml` als einzige Quelle der Einstellungen, `.env`, atomares Schreiben |
| [`content`](../../../internal/content/) | Kategorien, Elemente, Versionen, Übersetzungen, Papierkorb, Suche, Schlagwörter, Bausteine, Platzierungen, Pfade, Bereiche |
| [`coreplugin`](../../../internal/coreplugin/) | die Schnittstelle mitkompilierter Plugins (`Host`, `Register`) |
| [`csvsafe`](../../../internal/csvsafe/) | CSV-Werte, die Tabellenprogramme nie als Formel lesen |
| [`db`](../../../internal/db/) | Verbindungspool, Migrationen, Prüfung gegen veraltete Entwicklungsdatenbanken |
| [`events`](../../../internal/events/) | Ereignisse zum Leeren von Caches (im Prozess oder PostgreSQL NOTIFY) |
| [`feed`](../../../internal/feed/) | Atom- und RSS-Feeds schreiben |
| [`feedfetch`](../../../internal/feedfetch/) | externe Feeds lesen, mit Schutz vor internen Adressen |
| [`httpx`](../../../internal/httpx/) | kleine HTTP-Helfer: JSON-Antworten, Abfrageparameter |
| [`i18n`](../../../internal/i18n/) | Texte von Kern und Plugins, Vollständigkeit, Import und Export von Übersetzungen |
| [`identity`](../../../internal/identity/) | die Person hinter einer Anfrage (`Principal`) |
| [`kernel`](../../../internal/kernel/) | welche Plugins aktiv sind, ihre Reihenfolge und Abhängigkeiten |
| [`license`](../../../internal/license/) | Offline-Prüfung von Lizenzschlüsseln |
| [`limits`](../../../internal/limits/) | Register der technischen Grenzwerte (Experteneinstellungen) |
| [`tplsettings`](../../../internal/tplsettings/) | die Einstellungen, die ein Template anmeldet (Farben, Schriften, Logos …): Beschreibung, Prüfung der Werte, wirksame Werte |
| [`mail`](../../../internal/mail/) | Postausgang in der Datenbank, Worker mit Wiederholung, SMTP, sendmail |
| [`markdown`](../../../internal/markdown/) | Markdown von Seiten mit Baustein-Direktiven, HTML zu Markdown |
| [`media`](../../../internal/media/) | Medienbibliothek: Dateien in Kategorien, Versionen, Rechte, Wasserzeichen, Bildeditor |
| [`netguard`](../../../internal/netguard/) | HTTP-Clients, die interne Adressen ablehnen (Schutz vor SSRF) |
| [`netlist`](../../../internal/netlist/) | Listen erlaubter Adressen, Netze und Hostnamen |
| [`notify`](../../../internal/notify/) | Benachrichtigungen im Wiki (Glocke) und per E-Mail |
| [`ooxml`](../../../internal/ooxml/) | einfache Word- und Excel-Dateien ohne Abhängigkeiten |
| [`pagecache`](../../../internal/pagecache/) | Cache der Seiten für Gäste |
| [`pdfdoc`](../../../internal/pdfdoc/) | einfache PDF-Dokumente (Nachweise) |
| [`perm`](../../../internal/perm/) | Register der Rechte (global und je Kategorie) |
| [`pgversion`](../../../internal/pgversion/) | Prüfungen der PostgreSQL-Version |
| [`pluginmgr`](../../../internal/pluginmgr/) | Plugins installieren, aktualisieren und entfernen, das Programm neu bauen |
| [`readability`](../../../internal/readability/) | lange Sätze und Wörter (einfache Sprache) |
| [`secrets`](../../../internal/secrets/) | Schutz der in der Datenbank gespeicherten Geheimnisse |
| [`server`](../../../internal/server/) | die REST-API (`/api/v1`) und die Auslieferung des Frontends; Routen nach Bereich, siehe unten |
| [`shellcmd`](../../../internal/shellcmd/) | Shell-Befehle von Makro und Workflow-Schritten: Freigabe nach genauem Text, sicheres Ausführen |
| [`signature`](../../../internal/signature/) | Nachweise mit Verfahren von Plugins signieren, prüfbare Belege |
| [`storage`](../../../internal/storage/) | Dateien nach ihrem SHA-256 gespeichert, sichere Auslieferung |
| [`tlsutil`](../../../internal/tlsutil/) | TLS für Mail- und Verzeichnisserver, mit eigener Zertifizierungsstelle |
| [`version`](../../../internal/version/) | Build-Informationen |
| [`watch`](../../../internal/watch/) | Kategorien und Elemente beobachten, E-Mails über Änderungen |
| [`webhook`](../../../internal/webhook/) | HTTP-Benachrichtigungen an Teams, Slack oder JSON-Endpunkte |
| [`wopi`](../../../internal/wopi/) | Helfer für Office-Server (Discovery, Proof Keys) |
| [`worker`](../../../internal/worker/) | regelmäßige Hintergrundaufgaben, im Cluster auf einem Knoten zugleich |
| [`xlsx`](../../../internal/xlsx/) | einfache Tabellen |

### Inhalte und Anmeldung im Einzelnen

- [`content`](../../../internal/content/): Das größte Paket. Seine Dateien nach Thema: [`categories.go`](../../../internal/content/categories.go) (Kategorien), [`elements.go`](../../../internal/content/elements.go) (Elemente), [`drafts.go`](../../../internal/content/drafts.go) (Entwürfe und Vorlagen), [`trash.go`](../../../internal/content/trash.go) (Papierkorb), [`search.go`](../../../internal/content/search.go) (Suche), [`macros.go`](../../../internal/content/macros.go) (Darstellung von Bausteinen in Seiten), [`placements.go`](../../../internal/content/placements.go) (Platzierung von Bausteinen), [`paths.go`](../../../internal/content/paths.go) (lesbare Pfade), [`spaces.go`](../../../internal/content/spaces.go) (Bereiche), [`changes.go`](../../../internal/content/changes.go) (dauerhafte Folgearbeit), [`people.go`](../../../internal/content/people.go) (Namen von Personen für Lesende), [`sources.go`](../../../internal/content/sources.go) (Quellen von Listen), [`elementrights.go`](../../../internal/content/elementrights.go) (Rechte einzelner Elemente).
- [`auth`](../../../internal/auth/): Seine Dateien: [`service.go`](../../../internal/auth/service.go) (Kette der Anmeldeverfahren), [`manage.go`](../../../internal/auth/manage.go) (Benutzer, Gruppen, Rollen), [`sessions.go`](../../../internal/auth/sessions.go) (Sitzungen), [`mfaflow.go`](../../../internal/auth/mfaflow.go) (Faktoren einer Anmeldung), [`sso.go`](../../../internal/auth/sso.go) (Single Sign-on), [`scim.go`](../../../internal/auth/scim.go) (SCIM), [`directory.go`](../../../internal/auth/directory.go) (Abgleich mit Verzeichnissen), [`emails.go`](../../../internal/auth/emails.go) (E-Mail-Adressen), [`people.go`](../../../internal/auth/people.go) (wer wen öffnen darf).

## Die API nach Bereichen (`internal/server/`)

Jede Datei meldet die Routen eines Bereichs in einer Funktion `routes…` an, die `routes` in `api.go` aufruft.

| Datei | Bereich |
|---|---|
| [`api.go`](../../../internal/server/api.go) | Anmeldung aller Routen, der Wrapper `handle`, gemeinsame Prüfungen |
| [`api_system.go`](../../../internal/server/api_system.go) | Zustand, Bereitschaft, TLS-Prüfung des Proxys, Website-Informationen (`GET /api/v1/site`) |
| [`api_auth.go`](../../../internal/server/api_auth.go) | Anmeldung, Abmeldung, Anmeldeverfahren |
| [`api_sso.go`](../../../internal/server/api_sso.go) | Single Sign-on: Start und Antwort des Identity Providers |
| [`api_login.go`](../../../internal/server/api_login.go) | Einstellungen der Anmeldeseite |
| [`api_mfa.go`](../../../internal/server/api_mfa.go) | weitere Faktoren der Anmeldung, Anmeldung ohne Passwort |
| [`api_sessions.go`](../../../internal/server/api_sessions.go) | eigene Sitzungen, Single Logout |
| [`api_identity.go`](../../../internal/server/api_identity.go) | E-Mail-Adressen, verknüpfte Anmeldungen, Einladungen |
| [`api_me.go`](../../../internal/server/api_me.go) | die angemeldete Person: Konto, API-Schlüssel, SSH-Schlüssel, Vorlieben |
| [`api_profile.go`](../../../internal/server/api_profile.go) | Profil (Visitenkarte), Personenseite |
| [`api_users.go`](../../../internal/server/api_users.go) | Verwaltung von Benutzern, Gruppen und Rollen |
| [`api_categories.go`](../../../internal/server/api_categories.go) | Kategorien: Baum, Einstellungen, Übersetzungen, Export, Import |
| [`api_permissions.go`](../../../internal/server/api_permissions.go) | Rechte-Regeln von Kategorien, zentrale Übersicht |
| [`api_elementrights.go`](../../../internal/server/api_elementrights.go) | Regeln eines einzelnen Elements |
| [`api_access.go`](../../../internal/server/api_access.go) | Zugriffsanfragen |
| [`api_elements.go`](../../../internal/server/api_elements.go) | Elemente (Seiten und andere Typen): Inhalt, Versionen, Übersetzungen; die Liste der Bausteine |
| [`api_lookup.go`](../../../internal/server/api_lookup.go) | Elemente finden, lesbare Pfade, die Seite für unbekannte Adressen |
| [`api_drafts.go`](../../../internal/server/api_drafts.go) | Entwürfe, geplante Veröffentlichung, Vorlagen |
| [`api_trash_search.go`](../../../internal/server/api_trash_search.go) | Papierkorb, Volltextsuche, Schlagwörter |
| [`api_files.go`](../../../internal/server/api_files.go) | Dateien hochladen und ausliefern |
| [`api_media.go`](../../../internal/server/api_media.go) | Medienbibliothek |
| [`api_office.go`](../../../internal/server/api_office.go) | Office-Programme, WOPI-Host |
| [`api_notifications.go`](../../../internal/server/api_notifications.go) | Benachrichtigungen (Glocke) und die eigene Wahl der E-Mails |
| [`api_watches.go`](../../../internal/server/api_watches.go) | eigene Beobachtungen |
| [`api_feeds.go`](../../../internal/server/api_feeds.go) | Feeds von Kategorien, persönlicher Feed-Schlüssel |
| [`api_placements.go`](../../../internal/server/api_placements.go) | Platzierung von Bausteinen der Oberfläche |
| [`api_instances.go`](../../../internal/server/api_instances.go) | Instanzen von Plugins (ein Blog, ein Formular …) |
| [`api_data.go`](../../../internal/server/api_data.go) | Daten von Bausteinen und Plugins für Berichte |
| [`api_signatures.go`](../../../internal/server/api_signatures.go) | Signaturen und ihre Prüfung |
| [`api_i18n.go`](../../../internal/server/api_i18n.go) | Texte der Oberfläche und ihre Übersetzungen |
| [`api_settings.go`](../../../internal/server/api_settings.go) | Einstellungen der Website |
| [`settings_secrets.go`](../../../internal/server/settings_secrets.go) | Geheimnisse in Einstellungen maskieren, bevor sie den Server verlassen |
| [`api_secrets.go`](../../../internal/server/api_secrets.go) | Modus gespeicherter Geheimnisse (plain oder versiegelt) |
| [`api_limits.go`](../../../internal/server/api_limits.go) | technische Grenzwerte (Experteneinstellungen) |
| [`api_templates.go`](../../../internal/server/api_templates.go) | welches Template die Website nutzt und wie viele Kategorien welches wählen; die Einstellungen jedes Templates (geprüft, gespeichert, an Besucher geliefert) |
| [`api_plugins.go`](../../../internal/server/api_plugins.go) | Plugins: Liste, ein- und ausschalten, installieren, aktualisieren, entfernen |
| [`api_overview.go`](../../../internal/server/api_overview.go) | Übersicht der Administration, Update-Prüfung, Cache, Zustand der Datenbank |
| [`api_mail.go`](../../../internal/server/api_mail.go) | Verwaltung der E-Mail und des Postausgangs |
| [`api_jobs.go`](../../../internal/server/api_jobs.go) | Hintergrundaufgaben |
| [`api_webhooks.go`](../../../internal/server/api_webhooks.go) | Webhooks |
| [`api_audit.go`](../../../internal/server/api_audit.go) | Audit-Log und sein Export |
| [`api_license.go`](../../../internal/server/api_license.go) | Lizenzschlüssel |
| [`api_sites.go`](../../../internal/server/api_sites.go) | mehrere Websites einer Instanz |
| [`scim.go`](../../../internal/server/scim.go) | SCIM-Dienst für Verzeichnisse, die Konten anlegen |
| [`consent.go`](../../../internal/server/consent.go) | Zustimmung zu Cookies und Datenschutzerklärung |
| [`maintenance.go`](../../../internal/server/maintenance.go) | Wartungsmodus |
| [`scheme.go`](../../../internal/server/scheme.go) | helle und dunkle Darstellung |
| [`ratelimit.go`](../../../internal/server/ratelimit.go) | Grenzen der Anfragen je Client |
| [`bodylimit.go`](../../../internal/server/bodylimit.go) | Grenzen der Anfragegröße und paralleler Uploads |
| [`prerender.go`](../../../internal/server/prerender.go) | Seiten für Gäste und Suchmaschinen ohne JavaScript |
| [`collab.go`](../../../internal/server/collab.go) | gemeinsames Bearbeiten (WebSocket) |
| [`demo.go`](../../../internal/server/demo.go) | Demo-Inhalt: anlegen, entfernen; der Inhalt in `demo_*.go` |
| [`server.go`](../../../internal/server/server.go) | der Server, Auslieferung des Frontends |

## Plugins (`plugins/`)

Alles außerhalb des Kerns ist ein Plugin. Die Beschreibung jedes Plugins steht in seiner `plugin.json`.

| Ordner | ID | Name |
|---|---|---|
| [`auth_ldap`](../../../plugins/auth_ldap/) | `auth.ldap` | LDAP / Active Directory |
| [`auth_local`](../../../plugins/auth_local/) | `auth.local` | Local authentication |
| [`auth_oidc`](../../../plugins/auth_oidc/) | `auth.oidc` | OpenID Connect (Keycloak, Entra ID, ...) |
| [`auth_shib`](../../../plugins/auth_shib/) | `auth.shib` | Shibboleth / SAML 2.0 |
| [`content_blog`](../../../plugins/content_blog/) | `content.blog` | Blog |
| [`content_catlist`](../../../plugins/content_catlist/) | `content.catlist` | Category overview |
| [`content_events`](../../../plugins/content_events/) | `content.events` | Event calendar |
| [`content_faq`](../../../plugins/content_faq/) | `content.faq` | FAQ |
| [`content_forms`](../../../plugins/content_forms/) | `content.forms` | Forms |
| [`content_notes`](../../../plugins/content_notes/) | `content.notes` | Shared notes |
| [`content_propdb`](../../../plugins/content_propdb/) | `content.propdb` | Property database |
| [`content_team`](../../../plugins/content_team/) | `content.team` | Team |
| [`core_attachment`](../../../plugins/core_attachment/) | `core.attachment` | File / attachment |
| [`core_blocks`](../../../plugins/core_blocks/) | `core.blocks` | Blocks at positions |
| [`core_carousel`](../../../plugins/core_carousel/) | `core.carousel` | Carousel |
| [`core_comments`](../../../plugins/core_comments/) | `core.comments` | Comments |
| [`core_favorites`](../../../plugins/core_favorites/) | `core.favorites` | Favorites |
| [`core_html`](../../../plugins/core_html/) | `core.html` | Rich text page (HTML / Markdown) |
| [`core_layout`](../../../plugins/core_layout/) | `core.layout` | Layout builder macros |
| [`core_macros`](../../../plugins/core_macros/) | `core.macros` | Standard macros |
| [`core_megamenu`](../../../plugins/core_megamenu/) | `core.megamenu` | Mega menu |
| [`core_mentions`](../../../plugins/core_mentions/) | `core.mentions` | Mentions |
| [`core_menu`](../../../plugins/core_menu/) | `core.menu` | Menus |
| [`core_properties`](../../../plugins/core_properties/) | `core.properties` | Properties |
| [`core_reactions`](../../../plugins/core_reactions/) | `core.reactions` | Reactions |
| [`core_share`](../../../plugins/core_share/) | `core.share` | Share |
| [`core_template`](../../../plugins/core_template/) | `core.template` | Template designer |
| [`core_waf`](../../../plugins/core_waf/) | `core.waf` | Web application firewall |
| [`example_notice`](../../../plugins/example_notice/) | `example.notice` | Notice box (example plugin) |
| [`macro_http`](../../../plugins/macro_http/) | `macro.http` | HTTP macro |
| [`macro_shell`](../../../plugins/macro_shell/) | `macro.shell` | Shell command macro |
| [`mfa_email`](../../../plugins/mfa_email/) | `mfa.email` | Code by e-mail |
| [`mfa_network`](../../../plugins/mfa_network/) | `mfa.network` | Allowed networks |
| [`mfa_totp`](../../../plugins/mfa_totp/) | `mfa.totp` | Authenticator app (TOTP) |
| [`mfa_webauthn`](../../../plugins/mfa_webauthn/) | `mfa.webauthn` | Security keys and passkeys (WebAuthn) |
| [`office_collabora`](../../../plugins/office_collabora/) | `office.collabora` | Collabora Online |
| [`office_m365`](../../../plugins/office_m365/) | `office.m365` | Microsoft 365 for the web |
| [`office_onlyoffice`](../../../plugins/office_onlyoffice/) | `office.onlyoffice` | OnlyOffice Docs |
| [`search_external`](../../../plugins/search_external/) | `search.external` | External search engine |
| [`security_crowdsec`](../../../plugins/security_crowdsec/) | `security.crowdsec` | CrowdSec |
| [`signature_passkey`](../../../plugins/signature_passkey/) | `signature.passkey` | Signature with passkey |
| [`signature_smime`](../../../plugins/signature_smime/) | `signature.smime` | S/MIME |
| [`theme_cms`](../../../plugins/theme_cms/) | `theme.cms` | CMS / website template |
| [`theme_cms_modern`](../../../plugins/theme_cms_modern/) | `theme.cms.modern` | Website template – modern |
| [`theme_wiki`](../../../plugins/theme_wiki/) | `theme.wiki` | Wiki template |
| [`theme_wiki_dark`](../../../plugins/theme_wiki_dark/) | `theme.wiki.dark` | Wiki template (dark) – example |
| [`workflow_process`](../../../plugins/workflow_process/) | `workflow.process` | Workflow |

### Teile eines Plugins

- `plugin.go`: die Go-Seite; `Setup` meldet Routen, Elementtypen, Bausteine, Aufgaben und Hooks beim `Host` an.
- `plugin.json`: das Manifest (ID, Name, Rechte, Einstellungen, Grenzwerte, Abhängigkeiten).
- `actions.yaml` (mit `actions.go`, das es einbettet): die Einträge des Plugins im Aktionsregister.
- `locales/`: Texte auf Deutsch und Englisch; `migrations/`: die eigenen Tabellen des Plugins; `web/index.ts`: sein Teil der Oberfläche.
- [`plugins/all/zz_generated.go`](../../../plugins/all/zz_generated.go) importiert alle Plugins; es wird von `go run ./tools/genplugins` erzeugt.

## Frontend (`web/`)

- [`web/src/main.ts`](../../../web/src/main.ts): Start der Oberfläche und die Liste aller Seiten (`route('/…', view)`). Hier nach einer Adresse suchen, um ihre Ansicht zu finden.
- `web/src/views/`: eine Datei je Seite oder Gruppe von Seiten (zum Beispiel [`web/src/views/category.ts`](../../../web/src/views/category.ts), [`web/src/views/admin.ts`](../../../web/src/views/admin.ts)).
- `web/src/core/`: die Grundlage: [`web/src/core/api.ts`](../../../web/src/core/api.ts) (alle Aufrufe der API), [`web/src/core/dom.ts`](../../../web/src/core/dom.ts) (Elemente sicher bauen), [`web/src/core/router.ts`](../../../web/src/core/router.ts), [`web/src/core/i18n.ts`](../../../web/src/core/i18n.ts), [`web/src/core/state.ts`](../../../web/src/core/state.ts).
- `web/src/components/`: wiederverwendbare Teile, zum Beispiel [`web/src/components/ui.ts`](../../../web/src/components/ui.ts) (Felder, Knöpfe, Dialoge), [`web/src/components/person.ts`](../../../web/src/components/person.ts) (das Menü an Namen), [`web/src/components/bell.ts`](../../../web/src/components/bell.ts) (Benachrichtigungen), [`web/src/components/datepicker.ts`](../../../web/src/components/datepicker.ts) (die Datumsauswahl neben jedem Datumsfeld; ihre Rechnung in [`web/src/core/calendarmath.ts`](../../../web/src/core/calendarmath.ts)), [`web/src/components/choicelist.ts`](../../../web/src/components/choicelist.ts) (Auswahl mehrerer Einträge: Kästchen oder Liste mit Filter und Tasten; Personen nach 2 Buchstaben gesucht), [`web/src/components/subjects.ts`](../../../web/src/components/subjects.ts) (die Auswahl von Personen, Gruppen und Rollen darauf aufgebaut), [`web/src/components/templateSchema.ts`](../../../web/src/components/templateSchema.ts) (das Schema eines Templates für ein Gerät; die Geräte, die Beschreibung der Zeilen, die Zeilen der Seiten im Inhalt und das Lesen von Media Queries in [`web/src/core/templateSchema.ts`](../../../web/src/core/templateSchema.ts)); der Editor der Platzierung zeigt die Positionen darin als Schaltflächen ([`web/src/components/placement.ts`](../../../web/src/components/placement.ts)). Die Seite *Administration → Templates* mit ihren Reitern ist [`web/src/views/adminTemplates.ts`](../../../web/src/views/adminTemplates.ts), ihr Reiter Einstellungen mit der Live-Vorschau [`web/src/views/templateSettings.ts`](../../../web/src/views/templateSettings.ts); der Template-Designer ergänzt seine Reiter und das Schema der Templates aus Daten ([`plugins/core_template/web/schema.ts`](../../../plugins/core_template/web/schema.ts)).
- `web/src/editor/`: der Texteditor ([`web/src/editor/editor.ts`](../../../web/src/editor/editor.ts)), Bausteine im Editor, gemeinsames Bearbeiten; `web/src/styles/`: CSS.
- [`web/src/coreplugin.ts`](../../../web/src/coreplugin.ts): was Plugins nutzen dürfen (`@ewiki/coreplugin`); [`web/build.mjs`](../../../web/build.mjs) bündelt den Kern und jedes `plugins/*/web/index.ts`.

## Datenbank

- Das Schema des Kerns: [`internal/db/migrations/0001_schema.sql`](../../../internal/db/migrations/0001_schema.sql); von mehreren Websites geteilte Konten in `internal/db/migrations/identity/` und `shared/`. Tabellen eines Plugins: `plugins/<ordner>/migrations/`.
- [`internal/db/db.go`](../../../internal/db/db.go) spielt Migrationen beim Start ein und startet nicht mit einer Datenbank einer älteren Entwicklungsfassung (CLAUDE.md, Abschnitt 4).

## Texte, Übersetzungen und Aktionsregister

- Texte der Oberfläche: `internal/i18n/locales/de.json` und `en.json`, die jedes Plugins in `plugins/<ordner>/locales/`; Fehlermeldungen des Servers in `internal/apperr/locales/`. `TestLocaleParity` findet fehlende.
- Das Aktionsregister: [`internal/actions/actions.go`](../../../internal/actions/actions.go) liest `internal/actions/core/*.yaml` (eine Datei je Bereich) und die `actions.yaml` jedes Plugins. Die Beispiele dort laufen als Tests ([`internal/server/examples_test.go`](../../../internal/server/examples_test.go)).

## Tests

- Go-Tests stehen neben dem Code. Die Tests der API laufen gegen ein echtes PostgreSQL: [`internal/server/integration_test.go`](../../../internal/server/integration_test.go) (`setup`, der Client), eine Datei je Thema (`internal/server/<thema>_test.go`, zum Beispiel `workflow_test.go`, `mail_test.go`). Gemeinsame Helfer: [`internal/server/helpers_test.go`](../../../internal/server/helpers_test.go).
- Browser-Tests: [`web/tests/e2e.test.mjs`](../../../web/tests/e2e.test.mjs) (ganze Abläufe mit axe), [`web/tests/a11y-quick.mjs`](../../../web/tests/a11y-quick.mjs) (schnelle Prüfung der Barrierefreiheit); Unit-Tests des Frontends in `web/tests/unit/`.
- Fallen: [`tools/traps/traps.go`](../../../tools/traps/traps.go) nennt absichtlich eingebaute Fehler; jeder muss den genannten Test rot machen. Tests gegen echte Systeme (Anmeldung, Suche, Office, CrowdSec) liegen in `tests/`; die Gesamtprüfung von außen in `tests/qa/`.

## Werkzeuge, Skripte und Installation

- `tools/`: [`tools/docscheck/`](../../../tools/docscheck/) (Dokumentation), [`tools/traps/`](../../../tools/traps/) (Fallen), [`tools/genplugins/`](../../../tools/genplugins/) (Plugin-Liste), [`tools/notices/`](../../../tools/notices/) (Lizenzen), [`tools/uicoverage/`](../../../tools/uicoverage/) (jede Aktion von der Oberfläche genutzt), [`tools/testshard/`](../../../tools/testshard/) (Aufteilung der Tests in der CI).
- `scripts/`: [`scripts/build.sh`](../../../scripts/build.sh) (bauen), [`scripts/precheck.sh`](../../../scripts/precheck.sh) (alle Prüfungen vor einem Pull Request), [`scripts/update-docker.sh`](../../../scripts/update-docker.sh) (Updates einer Docker-Installation) und die Docker-Tests `scripts/test-docker-*.sh`.
- Installation mit Docker: [`docker-setup.sh`](../../../docker-setup.sh), [`docker-compose.yml`](../../../docker-compose.yml), Dateien für die Container in `docker/`; das stabile SDK für Plugins als eigener Prozess: [`sdk/`](../../../sdk/).
