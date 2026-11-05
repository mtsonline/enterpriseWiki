# Anmeldung einrichten: LDAP, OpenID Connect, Shibboleth/SAML, 2FA

Alle Anmeldeverfahren sind Plugins. Konfiguriert werden sie unter *Administration → LDAP / Active Directory*, *OpenID Connect* bzw. *Shibboleth / SAML* oder direkt in `config.yaml` unter `plugin_settings`. Ein Verfahren erscheint erst auf der Login-Seite, wenn es konfiguriert ist.

Grundregeln für alle Verfahren:

- **Lokale Konten werden immer zuerst geprüft.** Ein lokaler Administrator kommt auch dann hinein, wenn LDAP oder der Identity Provider ausfällt.
- Identitäten sind eindeutig als `name@anbieter` gespeichert (`anna@ldap`, `anna@oidc`, `anna@local`). Intern zählt die stabile ID des Anbieters (LDAP `entryUUID`/`objectGUID`, OIDC `sub`, SAML-Benutzerattribut). Ändert sich der Name im Verzeichnis, bleibt es dasselbe Konto.
- Bei jeder Anmeldung werden Name, E-Mail, Gruppen und Rollen aus dem Verfahren **synchronisiert**. Lokal im Admin-Bereich vergebene Gruppen und Rollen bleiben davon unberührt.
- Geheimnisse (Passwörter, Client-Secrets) sollten als `${VARIABLE}` eingetragen und in `.env` hinterlegt werden. Die Oberfläche zeigt gespeicherte Klartext-Geheimnisse nur maskiert an.

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

Externe Gruppen werden ohne Beachtung der Groß-/Kleinschreibung verglichen, entweder mit dem vollen Wert (z. B. DN) oder mit dem Kurznamen (`cn=HR,ou=…` passt auch auf den Schlüssel `hr`). Berechtigungen vergeben Sie dann wie gewohnt an die lokalen Gruppen und Rollen.

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
- Nur vom Wiki angestoßene Logins werden akzeptiert (kein IdP-initiated SSO), jede Antwort nur einmal.

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

## Zwei-Faktor-Authentifizierung (App oder E-Mail) und erlaubte Netze

Lokale Konten können unter *Mein Profil → Zwei-Faktor-Authentifizierung* eine Authenticator-App einrichten (RFC 6238, 6 Ziffern, 30 Sekunden). Danach verlangt das Login zusätzlich den Code. Jeder Code ist nur einmal gültig. Bei verlorenem Telefon setzt ein Administrator die 2FA unter *Benutzer → Bearbeiten → Zwei-Faktor-Authentifizierung zurücksetzen* zurück. Für LDAP-, OIDC- und SAML-Konten regelt der jeweilige Anbieter die Mehrfaktor-Anmeldung.

### Code per E-Mail

Statt oder zusätzlich zur App können lokale Konten unter *Mein Profil → Code per E-Mail* einen Code per E-Mail als zweiten Faktor wählen (bestätigt mit einem Code an die hinterlegte Adresse). Beim Login wird dann ein sechsstelliger Code geschickt; er gilt 10 Minuten, nur einmal und für höchstens 5 Versuche. Sind App und E-Mail eingerichtet, fragt das Login nach dem App-Code und bietet „Code per E-Mail senden“ an. Voraussetzung ist ein funktionierender Mailversand (`mail`). „Zwei-Faktor-Authentifizierung zurücksetzen“ entfernt beide Faktoren; per CLI: `ewiki user mfa-reset NAME`.

### Einschränkung auf Netze

Unter *Mein Profil → Erlaubte Netze* oder von Administratoren unter *Benutzer → Bearbeiten → Erlaubte Netze* lässt sich ein Konto auf IP-Adressen und Netze (CIDR, IPv4 und IPv6, z. B. `10.0.0.0/8`, `192.0.2.7`, `2001:db8::/32`) beschränken. Leer bedeutet: überall. Die Einschränkung gilt für alle Anmeldearten des Kontos (lokal, LDAP, OIDC, SAML, SSH) und für bestehende Sitzungen, API-Keys und Feed-Links; Anfragen von außerhalb erhalten `403 network_denied` und werden protokolliert (`auth.network_denied`). Wer die eigene Einschränkung setzt, muss die aktuelle Adresse einschließen. Hinter einem Reverse Proxy muss `server.trusted_proxies` gesetzt sein, damit die echte Client-Adresse zählt. Per CLI: `ewiki user networks NAME [NETZ ...]` (ohne Netze wird die Einschränkung aufgehoben).

## Skripte und API

- Lokale und LDAP-Konten: `POST /api/v1/auth/login` (Session-Cookie) oder besser ein **API-Key** bzw. **SSH-Schlüssel** (siehe [security.md](security.md)).
- SSO-Konten (OIDC/SAML) melden sich einmal im Browser an und legen dann im Profil einen API-Key oder SSH-Schlüssel an.
