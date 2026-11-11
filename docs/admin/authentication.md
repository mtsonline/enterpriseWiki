# Anmeldung einrichten: LDAP, OpenID Connect, Shibboleth/SAML, Mehrfaktor

Alle Anmeldeverfahren sind Plugins. Konfiguriert werden sie unter *Administration → LDAP / Active Directory*, *OpenID Connect* bzw. *Shibboleth / SAML* oder direkt in `config.yaml` unter `plugin_settings`. Ein Verfahren erscheint erst auf der Login-Seite, wenn es konfiguriert ist.

Grundregeln für alle Verfahren:

- **Lokale Konten werden immer zuerst geprüft.** Ein lokaler Administrator kommt auch dann hinein, wenn LDAP oder der Identity Provider ausfällt.
- Identitäten sind eindeutig als `name@anbieter` gespeichert (`anna@ldap`, `anna@oidc`, `anna@local`). Intern zählt die stabile ID des Anbieters (LDAP `entryUUID`/`objectGUID`, OIDC `sub`, SAML-Benutzerattribut). Ändert sich der Name im Verzeichnis, bleibt es dasselbe Konto.
- Bei jeder Anmeldung werden Name, E-Mail, Gruppen und Rollen aus dem Verfahren **synchronisiert**. Lokal im Admin-Bereich vergebene Gruppen und Rollen bleiben davon unberührt.
- Geheimnisse (Passwörter, Client-Secrets) sollten als `${VARIABLE}` eingetragen und in `.env` hinterlegt werden. Die Oberfläche zeigt gespeicherte Klartext-Geheimnisse nur maskiert an.

## Mehrere Anbieter derselben Art (Instanzen)

Jedes Verfahren kann mehrfach eingerichtet werden, z. B. ein OpenID-Connect-Anbieter für das Personal und einer für Partner, oder zwei LDAP-Verzeichnisse. Die Einstellungen auf oberster Ebene sind der erste Anbieter (`oidc`, `ldap`, `shib`). Jeder Eintrag unter `instances` ist ein weiterer mit vollständig eigenen Einstellungen: Verbindung, Gruppen- und Rollen-Zuordnung, JIT und Vertrauen. Seine Anbieter-ID ist `<verfahren>-<id>`:

```yaml
plugin_settings:
  auth.oidc:
    name: Personal
    issuer: https://keycloak.example.com/realms/staff
    client_id: wiki
    client_secret: ${OIDC_STAFF_SECRET}
    instances:
      - id: partner                      # Anbieter-ID oidc-partner
        name: Partner-Login
        issuer: https://login.microsoftonline.com/<tenant>/v2.0
        client_id: 6f1e…
        client_secret: ${OIDC_PARTNER_SECRET}
        group_mapping: { sales: Partners }
        keep_unmapped_groups: false
        jit: false                       # nur eingeladene Partner
```

- Die Kennung (`id`): 1–24 Kleinbuchstaben, Ziffern und Bindestriche, eindeutig im Verfahren. Sie steckt in den Adressen (`/api/v1/auth/sso/oidc-partner/callback`) und in der Benutzerkennung (`anna@oidc-partner`). Ändern Sie sie nicht mehr, sobald sich jemand damit angemeldet hat.
- In der Administration wählen Sie den Anbieter oben auf der Seite des Verfahrens aus, legen weitere an oder entfernen sie. Geheimnisse sind auch bei Instanzen nur maskiert sichtbar.
- SAML: Jede Instanz ist ein eigener Service Provider mit eigenen Metadaten (`/api/v1/x/auth.shib/metadata?instance=shib-uni`), eigener Entity-ID, eigenem ACS und SLO-Endpunkt. Das Schlüsselpaar ist für alle Instanzen gleich.
- LDAP: Die Verzeichnisse werden in der Reihenfolge von `auth.order` geprüft (z. B. `[ldap, ldap-extern]`). Mit `name@ldap-extern` wird gezielt ein Verzeichnis gewählt.

**Vorschau einer Anmeldung:** Auf der Seite jedes Anbieters zeigt *Vorschau einer Anmeldung*, was eine Anmeldung mit Benutzername, E-Mail-Adresse und externen Gruppen bewirken würde: bestehendes Konto, automatische Verknüpfung, Bestätigung nötig, neues Konto (JIT) oder Ablehnung. Außerdem sieht man die zugeordneten Gruppen (neu anzulegende eigens markiert) und Rollen. Unbekannte Rollen werden gemeldet, weil sie bei der Anmeldung ignoriert würden. Es wird nichts gespeichert. Dasselbe per CLI und API:

```bash
ewiki user signin-preview oidc-partner dora --email dora@partner.example --verified --groups sales,emea
curl -X POST …/api/v1/admin/auth/providers/oidc-partner/preview -d '{"username":"dora","groups":["sales"]}'
```

## Gruppen- und Rollen-Zuordnung (für alle Verfahren gleich)

```yaml
plugin_settings:
  auth.ldap:                        # genauso bei auth.oidc und auth.shib
    group_mapping:                  # externe Gruppe -> lokale Gruppe
      "cn=hr,ou=groups,dc=example,dc=com": Personal
      sales: Vertrieb
      ignore-me: ""                 # leer = ignorieren
    role_mapping:                   # externe Gruppe -> lokale Rolle (muss existieren)
      wiki-admins: Administrators
      wiki-editors: Editors
    default_groups: [Mitarbeitende] # bekommt jede:r dieses Verfahrens
    default_roles: []
    keep_unmapped_groups: true      # nicht zugeordnete Gruppen als gleichnamige lokale Gruppen übernehmen
```

**Benutzername aus dem Verzeichnis.** Mit `username_from` übernimmt das Wiki bei jeder Anmeldung ein Attribut bzw. einen Claim als Benutzernamen (Alias), z. B. `username_from: uid` oder `cn` bei LDAP, `preferred_username` bei OpenID Connect. Ohne `username_from` können Personen einen Benutzernamen optional im Profil eintragen (Administratoren unter *Benutzer → Bearbeiten*; CLI `ewiki user alias NAME [ALIAS]`). Ein Benutzername ist eindeutig (ohne Beachtung der Groß-/Kleinschreibung), 2–64 Zeichen aus Buchstaben, Ziffern, `.`, `_` und `-`. Mit ihm kann man sich auch anmelden, außer ein lokales Konto heißt genauso; ein neues lokales Konto darf nicht wie ein bestehender Benutzername heißen.

Externe Gruppen werden ohne Beachtung der Groß-/Kleinschreibung verglichen, entweder mit dem vollen Wert (z. B. DN) oder mit dem Kurznamen (`cn=HR,ou=…` passt auch auf den Schlüssel `hr`). Berechtigungen vergeben Sie dann wie gewohnt an die lokalen Gruppen und Rollen.

## E-Mail-Adressen, verknüpfte Anmeldungen und Einladungen

Ein Benutzer ist **ein Profil mit einer oder mehreren E-Mail-Adressen**. Eine **bestätigte** Adresse gehört genau einem Benutzer. Sie kennzeichnet ihn eindeutig: Mit ihr kann man sich bei lokalen Konten anmelden, und neue Anmeldungen anderer Verfahren mit dieser Adresse werden mit dem Konto **verknüpft**.

- **Adressen verwalten:**
  - Benutzer: im Profil unter *E-Mail-Adressen* (`/api/v1/me/emails`).
  - Administratoren: *Benutzer → E-Mail & Anmeldungen* (`/api/v1/admin/users/{id}/emails`), CLI `ewiki user emails|email-add|email-primary|email-remove`.
  - Selbst hinzugefügte Adressen bestätigt ein Link per E-Mail (48 Stunden gültig, nur einmal verwendbar). Ohne E-Mail-Versand fügen Administratoren Adressen direkt bestätigt hinzu.
  - Benachrichtigungen gehen an die **primäre** Adresse; primär kann nur eine bestätigte Adresse sein.
- **Verknüpfen statt doppelter Konten** (Schutz vor Konto-Übernahme): Meldet sich jemand mit einem anderen Verfahren an und dessen Adresse gehört schon zu einem Konto, dann
  - wird **automatisch verknüpft**, wenn das Verfahren die Adresse bestätigt (`email_verified`, bei OIDC der Claim) **und** Sie ihm vertrauen (`trust_email: true`);
  - sonst meldet sich die Person **einmal mit dem bestehenden Konto an** und bestätigt so die Verknüpfung. Die Login-Seite erklärt das; der Vorgang ist 30 Minuten gültig.
  - Weitere Anmeldungen eines Kontos stehen im Profil unter *Anmeldungen* und können dort entfernt werden (`ewiki user identities|unlink`).
- **Vorab anlegen (Einladung):** *Benutzer → Einladen*, `POST /api/v1/admin/users/invite` oder `ewiki user invite ADRESSE --groups A,B`.
  - Das Konto bekommt Gruppen und Rollen sofort. Es zählt erst ab der ersten Anmeldung für die Lizenz.
  - Die erste Anmeldung über ein **vertrauenswürdiges** Verfahren mit dieser bestätigten Adresse übernimmt das Konto.
- **Konten bei der ersten Anmeldung (JIT):** Standard ist an. Mit `jit: false` bekommen unbekannte Personen kein Konto, nur eingeladene, angelegte oder per Adresse verknüpfte.

```yaml
plugin_settings:
  auth.oidc:              # genauso bei auth.ldap und auth.shib
    jit: true             # Konto bei der ersten Anmeldung anlegen
    trust_email: false    # bestätigte Adressen dieses Verfahrens automatisch verknüpfen
    email_verified: false # nur LDAP/SAML: das Verzeichnis bestätigt alle Adressen (OIDC: Claim email_verified)
```

Setzen Sie `trust_email` nur für Verfahren, bei denen niemand eine fremde Adresse eintragen kann, etwa den eigenen Identity Provider oder das eigene Active Directory.

## LDAP / Active Directory (`auth.ldap`)

Benutzer:innen melden sich im normalen Login-Formular mit Verzeichnis-Benutzername und Passwort an, auch per API (`POST /api/v1/auth/login`).

**OpenLDAP:**

```yaml
plugin_settings:
  auth.ldap:
    url: ldaps://ldap.example.com:636
    bind_dn: cn=wiki,ou=services,dc=example,dc=com
    bind_password: ${LDAP_BIND_PASSWORD}
    base_dn: ou=people,dc=example,dc=com
    user_filter: (&(objectClass=inetOrgPerson)(uid={username}))
    attr_username: uid
    attr_email: mail
    attr_display_name: cn
    attr_subject: entryUUID
    group_base_dn: ou=groups,dc=example,dc=com
    group_filter: (|(member={dn})(uniqueMember={dn})(memberUid={username}))
    group_name_attribute: cn
```

**Active Directory:**

```yaml
plugin_settings:
  auth.ldap:
    url: ldaps://dc01.corp.example.com:636
    bind_dn: CN=svc-wiki,OU=Service,DC=corp,DC=example,DC=com
    bind_password: ${LDAP_BIND_PASSWORD}
    base_dn: OU=Users,DC=corp,DC=example,DC=com
    user_filter: (&(objectCategory=person)(sAMAccountName={username}))
    attr_username: sAMAccountName
    attr_display_name: displayName
    attr_subject: objectGUID
    group_attribute: memberOf
```

- `ldap://` mit `start_tls: true` ist ebenso möglich. `insecure_skip_verify` nur für Tests verwenden.
- Ohne `bind_dn` wird anonym gesucht.
- Leere Passwörter werden immer abgelehnt (Schutz vor „unauthenticated bind“).
- Mit **Anmeldung testen** im Admin-Bereich sehen Sie, welche Werte und Gruppen ankommen, ohne dass eine Sitzung entsteht.

## OpenID Connect (`auth.oidc`)

Authorization Code Flow mit PKCE, Nonce, Signatur- und Audience-Prüfung des ID-Tokens. Registrieren Sie beim Anbieter als Redirect-URI:

```
<server.base_url>/api/v1/auth/sso/oidc/callback
```

```yaml
plugin_settings:
  auth.oidc:
    name: Firmen-Login            # Beschriftung des Knopfs
    issuer: https://keycloak.example.com/realms/acme
    client_id: enterprise-wiki
    client_secret: ${OIDC_CLIENT_SECRET}
    scopes: [openid, profile, email]
    claim_username: preferred_username
    claim_email: email
    claim_display_name: name
    claim_groups: groups
```

| Anbieter | Hinweise |
|---|---|
| Keycloak | `issuer` = Realm-URL. Für Gruppen einen Mapper „Group Membership“ (Token Claim Name `groups`, „Full group path“ aus) im Client-Scope anlegen |
| Microsoft Entra ID | `issuer: https://login.microsoftonline.com/<tenant-id>/v2.0`, `claim_username: preferred_username`. Für Gruppen „groupMembershipClaims“ im App-Manifest aktivieren. Es kommen Objekt-IDs, die Sie per `group_mapping` zuordnen |
| Authentik, Zitadel, Google | Standard-Claims genügen. Google liefert keine Gruppen |

**Verbindung prüfen** im Admin-Bereich lädt das Discovery-Dokument des Anbieters.

**Abmelden (Single Logout):**

- *RP-initiated Logout:* Beim Abmelden im Wiki wird der Browser zum `end_session_endpoint` des Anbieters geschickt (mit `id_token_hint` und `client_id`), sofern der Anbieter einen hat. Registrieren Sie als Post-Logout-Redirect-URI `<server.base_url>/login?logged_out=1` oder setzen Sie `post_logout_redirect_uri`. Mit `single_logout: false` endet nur die Sitzung im Wiki.
- *Back-Channel Logout:* Tragen Sie beim Anbieter `<server.base_url>/api/v1/auth/sso/oidc/logout` als Back-Channel-Logout-URL ein (Keycloak: „Backchannel logout URL“, „Backchannel logout session required“ an). Das Wiki prüft das Logout-Token vollständig (Signatur, Aussteller, Audience, Ablauf, Logout-Event, kein `nonce`, aktuelles `iat`, `jti` nur einmal) und beendet genau die genannte Sitzung (`sid`) bzw. alle Sitzungen des Benutzers (`sub`).

## Shibboleth / SAML 2.0 (`auth.shib`)

### Nativer Modus (empfohlen)

Das Wiki ist selbst SAML Service Provider. Ein Schlüsselpaar wird beim ersten Aufruf erzeugt und in der Datenbank gespeichert.

```yaml
plugin_settings:
  auth.shib:
    mode: saml
    name: Uni-Login
    idp_metadata_url: https://idp.uni.example/idp/shibboleth   # oder idp_metadata: <XML>
    attr_username: urn:oid:1.3.6.1.4.1.5923.1.1.1.6            # eduPersonPrincipalName
    attr_email: urn:oid:0.9.2342.19200300.100.1.3              # mail
    attr_display_name: urn:oid:2.16.840.1.113730.3.1.241       # displayName
    attr_groups: urn:oid:1.3.6.1.4.1.5923.1.5.1.1              # isMemberOf
```

- Metadaten des Wikis für den IdP bzw. die Föderation: `<server.base_url>/api/v1/x/auth.shib/metadata`
- Assertion Consumer Service: `<server.base_url>/api/v1/auth/sso/shib/callback` (HTTP-POST)
- Attribute können mit dem Namen (OID) oder dem FriendlyName angegeben werden.
- Föderations-Metadaten (EntitiesDescriptor) werden unterstützt. Verwendet wird der erste IdP darin. Für eine bestimmte Organisation tragen Sie die Metadaten des einzelnen IdP ein.
- Single Logout Service: `<server.base_url>/api/v1/auth/sso/shib/logout` (HTTP-Redirect und HTTP-POST; steht in den Metadaten).
- Nur vom Wiki angestoßene Logins werden akzeptiert (kein IdP-initiated SSO), jede Antwort und jede LogoutRequest nur einmal (auch über mehrere Instanzen).
- **Single Logout (SLO):** Beim Abmelden im Wiki geht eine signierte LogoutRequest (NameID und SessionIndex) an den IdP, sofern er einen SLO-Endpunkt anbietet. Eine LogoutRequest des IdP wird nur mit gültiger Signatur, passendem Aussteller und Ziel und frischem Zeitstempel angenommen. Sie beendet die genannten Sitzungen und wird mit einer signierten LogoutResponse beantwortet. Mit `single_logout: false` schickt das Wiki keine LogoutRequest an den IdP.

### Header-Modus (Apache mit mod_shib)

Wenn bereits eine Shibboleth-SP-Installation existiert:

```yaml
server:
  trusted_proxies: [127.0.0.1]     # nur von hier werden Header akzeptiert!
plugin_settings:
  auth.shib:
    mode: header
    header_username: eppn          # oder REMOTE_USER
    header_email: mail
    header_display_name: displayName
    header_groups: isMemberOf
    header_separator: ";"
```

```apache
<Location /api/v1/auth/sso/shib/start>
  AuthType shibboleth
  ShibRequestSetting requireSession true
  ShibUseHeaders On
  Require shib-session
</Location>
ProxyPass / http://127.0.0.1:8080/
RequestHeader unset eppn early      # Header von außen verwerfen
```

## SCIM 2.0: Bereitstellung aus dem Verzeichnis

Microsoft Entra ID, Okta und andere Verzeichnisse legen über SCIM 2.0 (RFC 7643/7644) Personen und Gruppen an, ändern sie, sperren sie und entfernen sie, bevor sich jemand anmeldet. Einrichten unter *Administration → SCIM-Bereitstellung* oder per CLI:

```bash
ewiki scim create "Entra ID" --provider oidc-entra --subject-from externalId   # zeigt Tenant-URL und Token (einmalig)
ewiki scim list | ewiki scim token 3 | ewiki scim delete 3
```

Im Verzeichnis tragen Sie als **Tenant-URL** `<server.base_url>/scim/v2` und als **Secret Token** das Token ein.

- **Wiedererkennen bei der Anmeldung:** Jeder Zugang gehört zu einem Anmeldeverfahren (`--provider`). Bereitgestellte Konten bekommen dessen Anbieter-ID und als stabile ID das SCIM-Attribut `userName` oder `externalId`. Meldet sich die Person an, findet das Wiki das Konto über diese ID. Für **Entra ID mit OpenID Connect**: `--subject-from externalId` (Entra sendet die objectId) und beim OIDC-Anbieter `claim_subject: oid`. Bei Okta und Keycloak passt meist `userName` zum Benutzernamen beim Anbieter.
- **Was SCIM darf:** Personen des eigenen Anmeldeverfahrens, die kein anderer Zugang verwaltet, und von ihm angelegte Gruppen. **Nie Administratoren und nie Rollen.** Welche Rollen eine bereitgestellte Gruppe bekommt, legen Administratoren unter *Gruppen* fest. Lokale Konten sind für SCIM unsichtbar.
- **Name und Adressen** kommen aus dem Verzeichnis: Die Anmeldung überschreibt sie bei bereitgestellten Konten nicht. Adressen aus SCIM gelten als bestätigt. Sendet das Verzeichnis eine Adresse nicht mehr, wird sie entfernt. Eine Adresse, die ein anderes Konto bestätigt hat, wird abgelehnt (409).
- **Sperren** (`active: false`) beendet alle Sitzungen sofort. **Löschen** sperrt das Konto (Standard) oder löscht es (`--on-delete delete`).
- Unterstützt: Users und Groups mit GET (Filter `attribut eq "wert"` auf `userName`, `externalId`, `emails.value`, `displayName`, `id`; Paging), POST, PUT, PATCH (wie von Entra ID und Okta gesendet), DELETE. Nicht unterstützt: Bulk, Sortierung, ETags. Attribute, die das Wiki nicht kennt (z. B. die Enterprise-Erweiterung), werden ignoriert.
- Sicherheit: Nur das Token des Zugangs authentifiziert, keine Sitzung und kein API-Key. Gespeichert wird nur sein Hash. Nach 30 falschen Tokens in 5 Minuten wird die IP-Adresse vorübergehend abgewiesen. Jede Änderung steht im Audit-Log als `scim:<Name>`. Bei mehreren Sites mit gemeinsamen Konten gibt es SCIM nur auf der Haupt-Site.

## Sitzungen

- Eine Browser-Sitzung endet nach `auth.session_ttl` (Standard 7 Tage) und nach `auth.session_idle` ohne Aktivität (Standard 12 Stunden). Automatische Abfragen der Oberfläche (Benachrichtigungen) halten eine Sitzung nicht am Leben.
- Unter *Profil → Sitzungen* sieht jede Person, wo sie angemeldet ist (Gerät, IP-Adresse, Anmeldeverfahren, Beginn, letzte Aktivität), und kann einzelne Sitzungen oder alle anderen beenden („Überall sonst abmelden“). Das Sitzungstoken selbst wird nie angezeigt.
- Wer das eigene Passwort ändert, bleibt nur in der aktuellen Sitzung angemeldet. Ändert ein Administrator das Passwort oder sperrt das Konto, enden alle Sitzungen.
- Administratoren sehen und beenden die Sitzungen eines Benutzers unter *Administration → Benutzer → E-Mail & Anmeldungen*.
- Mit einem API-Key lassen sich Sitzungen weder auflisten noch beenden.
- Bei Anmeldung über OIDC oder SAML speichert das Wiki zur Sitzung, was der Anbieter für den Single Logout braucht (OIDC: `sid` und ID-Token als `id_token_hint`; SAML: SessionIndex und NameID).

```bash
ewiki user sessions anna            # Sitzungen auflisten
ewiki user logout anna 42           # eine Sitzung beenden
ewiki user logout anna              # alle Sitzungen beenden
ewiki api GET /api/v1/me/sessions --as anna
```

## Mehrere Faktoren: die Faktorkette

Jeder weitere Faktor der Anmeldung ist ein **MFA-Plugin**. Die Kette gilt für **alle Anmeldearten**: lokale Konten, LDAP, OpenID Connect und SAML.

| Plugin | Faktor | Standard-Nummer | Hinweise |
|---|---|---|---|
| `mfa.webauthn` | Sicherheitsschlüssel oder Passkey | 1 | FIDO2/WebAuthn: YubiKey, Windows Hello, Touch ID, Android. Als Faktor 1 ist jeder Schlüssel ein **Passkey** (Benutzerprüfung mit PIN oder Biometrie ist Pflicht) und ersetzt das Passwort („Mit Passkey anmelden“) |
| `mfa.totp` | Authenticator-App | 2 | RFC 6238, 6 Ziffern, 30 Sekunden, jeder Code nur einmal |
| `mfa.email` | Code per E-Mail | 2 | 6 Ziffern, 10 Minuten, höchstens 5 Versuche, höchstens 5 Codes in 15 Minuten. Braucht funktionierenden Mailversand |
| `mfa.network` | Erlaubte Netze | 2 | Ohne Abfrage: erfüllt, wenn die Anmeldung aus einem erlaubten Netz der Person kommt (siehe unten) |

**Faktornummern.** Beim Einschalten eines MFA-Plugins fragt die Administration, als wievielter Faktor es abgefragt wird (1–9). Unter *Administration → Anmeldefaktoren* lässt sich die Nummer jederzeit ändern.

- Faktoren mit **derselben Nummer** sind Alternativen: Die Person wählt einen davon.
- **Höhere Nummern** werden danach abgefragt, aufsteigend. So entstehen beliebig lange Ketten, z. B. Passwort → App oder E-Mail (2) → Sicherheitsschlüssel (3).
- **Faktor 1** steht neben dem Passwort bzw. dem Anmeldeverfahren und ist nur für Plugins erlaubt, die selbst die Person feststellen (Passkeys). Ein Passkey gilt nur, wenn Faktor 1 in der Kette des Anmeldeverfahrens seines Kontos enthalten ist.

**Pro Anmeldeverfahren (Instanz).** Unter *Administration → Anmeldefaktoren* wählen Sie für jede Instanz (z. B. `local`, `oidc-1`, `ldap-2`):

- **Welche Faktoren gelten:** alle aktiven (auch später eingeschaltete) oder genau die gewählten. Ohne Faktoren fragt das Wiki nach dieser Anmeldung nichts weiter, z. B. wenn der Identity Provider selbst MFA verlangt (Entra ID mit bedingtem Zugriff).
- **Wie viele Faktoren eine Anmeldung braucht** (`mfa_required`, Standard 1). Bei 1 werden weitere Faktoren nur abgefragt, wenn die Person sie eingerichtet hat. Bei 2 oder mehr richtet jemand mit zu wenigen Faktoren während der Anmeldung einen ein, bevor eine Sitzung entsteht.

In der Konfiguration stehen die Werte in den Plugin-Einstellungen: `factor` beim MFA-Plugin, `mfa_methods` (Liste; fehlt = alle) und `mfa_required` beim Anmelde-Plugin bzw. in dessen Instanz.

```yaml
plugin_settings:
  mfa.totp:   { factor: 2 }
  mfa.email:  { factor: 2 }
  mfa.webauthn: { factor: 1 }
  auth.local: { mfa_methods: [totp, webauthn], mfa_required: 2 }
  auth.oidc:  { mfa_methods: [] }   # der Identity Provider macht MFA
```

**Ablauf.** Zuerst prüft das Anmeldeverfahren die Person (oder ein Passkey als Faktor 1). Braucht die Anmeldung weitere Faktoren, entsteht noch keine Sitzung, sondern eine wartende Anmeldung: 10 Minuten gültig, höchstens 5 Versuche, nur einmal verwendbar. Erst der letzte Faktor erzeugt die Sitzung. Bei Single Sign-on schickt das Wiki den Browser dafür auf `/login?mfa=1`; die wartende Anmeldung steckt in einem kurzlebigen HttpOnly-Cookie, nicht in der Adresse. Hat eine Person für eine Stufe keinen Faktor eingerichtet, entfällt diese Stufe – außer `mfa_required` verlangt mehr.

- **Einrichten:** *Mein Profil → Anmeldefaktoren* zeigt die eigene Kette und die Faktoren zum Einrichten. Einrichten und Entfernen geht nur im Browser (nicht mit einem API-Key) und nur bis 15 Minuten nach der Anmeldung, und nur für Faktoren der eigenen Kette.
- **Sicherheitsschlüssel:** Signatur, Herkunft (Origin) und Relying-Party-ID werden geprüft. Ein Schlüssel, dessen Zähler zurückspringt (Hinweis auf eine Kopie), wird abgewiesen und protokolliert. WebAuthn braucht einen Domainnamen (keine IP-Adresse); Standard ist der Host von `server.base_url`, abweichend `plugin_settings."mfa.webauthn".rp_id` und `origins`.
- **Zurücksetzen:** Bei verlorenem Gerät entfernt ein Administrator unter *Benutzer → Bearbeiten → Alle Faktoren zurücksetzen* App, E-Mail-Codes und Sicherheitsschlüssel. Per CLI: `ewiki user mfa-reset NAME`.
- **API:** `POST /api/v1/auth/login` antwortet bei weiteren Faktoren mit `401 mfa_required`; `detail` enthält das Token der wartenden Anmeldung. Danach `POST /api/v1/auth/mfa/info` (aktuelle Stufe: `verify` oder `enroll`, Faktornummer, Alternativen), `…/mfa/challenge` (E-Mail-Code senden, WebAuthn-Optionen), `…/mfa/verify` bzw. `…/mfa/enroll/begin` und `…/enroll/finish`. Jede Antwort ist entweder die nächste Stufe (`done: false`) oder die Sitzung (`done: true`). Die Kette verwalten `GET`/`PUT /api/v1/admin/mfa`, die eigenen Faktoren `GET /api/v1/me/mfa` und `POST /api/v1/me/mfa/{faktor}/begin|finish`.

### Erlaubte Netze (`mfa.network`)

Unter *Mein Profil → Anmeldefaktoren → Erlaubte Netze* oder von Administratoren unter *Benutzer → Bearbeiten → Erlaubte Netze* lässt sich ein Konto auf IP-Adressen und Netze (CIDR, IPv4 und IPv6, z. B. `10.0.0.0/8`, `192.0.2.7`, `2001:db8::/32`) beschränken. Leer bedeutet: überall.

- **Als Faktor** gilt die Stufe als erfüllt, wenn die Anmeldung aus einem erlaubten Netz kommt. Steht ein anderer Faktor mit derselben Nummer daneben (z. B. die App), ist er von außerhalb die Alternative; sonst wird die Anmeldung von außerhalb abgewiesen (`403 network_denied`, protokolliert als `auth.network_denied`).
- **Immer durchsetzen** (`plugin_settings."mfa.network".enforce_always`, Standard `true`): Die Netze gelten zusätzlich für alle Anmeldearten (auch SSH) und für bestehende Sitzungen, API-Keys und Feed-Links.
- Wer die eigene Einschränkung setzt, muss die aktuelle Adresse einschließen. Hinter einem Reverse Proxy muss `server.trusted_proxies` gesetzt sein, damit die echte Client-Adresse zählt. Per CLI: `ewiki user networks NAME [NETZ ...]` (ohne Netze wird die Einschränkung aufgehoben); per API `GET`/`PUT /api/v1/x/mfa.network/me` bzw. `…/users/{id}`.

## Skripte und API

- Lokale und LDAP-Konten: `POST /api/v1/auth/login` (Session-Cookie) oder besser ein **API-Key** bzw. **SSH-Schlüssel** (siehe [security.md](security.md)).
- SSO-Konten (OIDC/SAML) melden sich einmal im Browser an und legen dann im Profil einen API-Key oder SSH-Schlüssel an.
