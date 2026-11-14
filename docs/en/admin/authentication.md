# Setting up sign-in: LDAP, OpenID Connect, Shibboleth/SAML, multi-factor

All sign-in methods are plugins. They are configured under *Administration → LDAP / Active Directory*, *OpenID Connect* or *Shibboleth / SAML*, or directly in `config.yaml` under `plugin_settings`. A method only appears on the sign-in page once it is configured.

Basic rules for all methods:

- **Local accounts are always checked first.** A local administrator gets in even when LDAP or the identity provider fails.
- Identities are stored uniquely as `name@provider` (`anna@ldap`, `anna@oidc`, `anna@local`). Internally the stable ID of the provider counts (LDAP `entryUUID`/`objectGUID`, OIDC `sub`, SAML user attribute). If the name changes in the directory, it stays the same account.
- On every sign-in, name, e-mail, groups and roles are **synchronised** from the method. Groups and roles assigned locally in the admin area stay untouched.
- Secrets (passwords, client secrets) should be entered as `${VARIABLE}` and stored in `.env`. The interface shows stored plain-text secrets only masked.

## Several providers of the same kind (instances)

Every method can be set up several times, e.g. one OpenID Connect provider for staff and one for partners, or two LDAP directories. The settings at the top level are the first provider (`oidc`, `ldap`, `shib`). Every entry under `instances` is a further one with completely own settings: connection, group and role mapping, JIT and trust. Its provider ID is `<method>-<id>`:

```yaml
plugin_settings:
  auth.oidc:
    name: Staff
    issuer: https://keycloak.example.com/realms/staff
    client_id: wiki
    client_secret: ${OIDC_STAFF_SECRET}
    instances:
      - id: partner                      # provider ID oidc-partner
        name: Partner sign-in
        issuer: https://login.microsoftonline.com/<tenant>/v2.0
        client_id: 6f1e…
        client_secret: ${OIDC_PARTNER_SECRET}
        group_mapping: { sales: Partners }
        keep_unmapped_groups: false
        jit: false                       # only invited partners
```

- The identifier (`id`): 1–24 lower-case letters, digits and hyphens, unique within the method. It is part of the addresses (`/api/v1/auth/sso/oidc-partner/callback`) and of the user identifier (`anna@oidc-partner`). Do not change it any more once someone has signed in with it.
- In the administration you choose the provider at the top of the page of the method, create further ones or remove them. Secrets are visible only masked for instances too.
- SAML: every instance is its own service provider with own metadata (`/api/v1/x/auth.shib/metadata?instance=shib-uni`), own entity ID, own ACS and SLO endpoint. The key pair is the same for all instances.
- LDAP: the directories are checked in the order of `auth.order` (e.g. `[ldap, ldap-extern]`). With `name@ldap-extern` a directory is chosen specifically.

**Preview of a sign-in:** on the page of every provider, *Preview of a sign-in* shows what a sign-in with user name, e-mail address and external groups would do: existing account, automatic link, confirmation needed, new account (JIT) or rejection. You also see the mapped groups (those to be created are marked) and roles. Unknown roles are reported because they would be ignored at sign-in. Nothing is stored. The same with the CLI and the API:

```bash
ewiki user signin-preview oidc-partner dora --email dora@partner.example --verified --groups sales,emea
curl -X POST …/api/v1/admin/auth/providers/oidc-partner/preview -d '{"username":"dora","groups":["sales"]}'
```

## Group and role mapping (the same for all methods)

```yaml
plugin_settings:
  auth.ldap:                        # the same with auth.oidc and auth.shib
    group_mapping:                  # external group -> local group
      "cn=hr,ou=groups,dc=example,dc=com": Staff
      sales: Sales
      ignore-me: ""                 # empty = ignore
    role_mapping:                   # external group -> local role (must exist)
      wiki-admins: Administrators
      wiki-editors: Editors
    default_groups: [Employees]     # everyone of this method gets these
    default_roles: []
    keep_unmapped_groups: true      # take over unmapped groups as local groups of the same name
```

**User name from the directory.** With `username_from` the wiki takes an attribute or claim as the user name (alias) on every sign-in, e.g. `username_from: uid` or `cn` with LDAP, `preferred_username` with OpenID Connect. Without `username_from` people can optionally enter a user name in their profile (administrators under *Users → Edit*; CLI `ewiki user alias NAME [ALIAS]`). A user name is unique (case does not matter), 2–64 characters of letters, digits, `.`, `_` and `-`. You can also sign in with it, unless a local account has the same name; a new local account may not have the name of an existing user name.

External groups are compared ignoring case, either with the full value (e.g. DN) or with the short name (`cn=HR,ou=…` also matches the key `hr`). You then assign permissions to the local groups and roles as usual.

## E-mail addresses, linked sign-ins and invitations

A user is **a profile with one or more e-mail addresses**. A **confirmed** address belongs to exactly one user. It identifies the user uniquely: local accounts can sign in with it, and new sign-ins of other methods with this address are **linked** to the account.

- **Managing addresses:**
  - Users: in the profile under *E-mail addresses* (`/api/v1/me/emails`).
  - Administrators: *Users → E-mail & sign-ins* (`/api/v1/admin/users/{id}/emails`), CLI `ewiki user emails|email-add|email-primary|email-remove`.
  - Addresses added by the person themselves are confirmed with a link by e-mail (valid for 48 hours, usable once). Without e-mail delivery administrators add addresses directly as confirmed.
  - Notifications go to the **primary** address; only a confirmed address can be primary.
- **Linking instead of duplicate accounts** (protection against account takeover): if someone signs in with another method and their address already belongs to an account, then
  - it is **linked automatically** if the method confirms the address (`email_verified`, with OIDC the claim) **and** you trust it (`trust_email: true`);
  - otherwise the person **signs in once with the existing account** and so confirms the link. The sign-in page explains this; the process is valid for 30 minutes.
  - Further sign-ins of an account are in the profile under *Sign-ins* and can be removed there (`ewiki user identities|unlink`).
- **Creating in advance (invitation):** *Users → Invite*, `POST /api/v1/admin/users/invite` or `ewiki user invite ADDRESS --groups A,B`.
  - The account gets groups and roles at once. It only counts for the license from the first sign-in.
  - The first sign-in through a **trusted** method with this confirmed address takes over the account.
- **Accounts on the first sign-in (JIT):** on by default. With `jit: false` unknown people get no account, only invited, created or address-linked ones.

```yaml
plugin_settings:
  auth.oidc:              # the same with auth.ldap and auth.shib
    jit: true             # create the account on the first sign-in
    trust_email: false    # link confirmed addresses of this method automatically
    email_verified: false # only LDAP/SAML: the directory confirms all addresses (OIDC: claim email_verified)
```

Only set `trust_email` for methods where nobody can enter a foreign address, such as your own identity provider or your own Active Directory.

## LDAP / Active Directory (`auth.ldap`)

Users sign in in the normal sign-in form with their directory user name and password, also through the API (`POST /api/v1/auth/login`).

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

- `ldap://` with `start_tls: true` is possible as well. Only use `insecure_skip_verify` for tests.
- Enter an own certificate authority (for example the company CA) as `ca_cert` (PEM or path to the file). The server certificate needs the server name as subject alternative name (SAN); certificates with a CN only are refused.
- `memberOf` names the **direct** groups only. AD finds **nested groups** (a group in a group) with its own search rule; for this leave `group_attribute` empty and set `group_base_dn: DC=corp,DC=example,DC=com` and `group_filter: (member:1.2.840.113556.1.4.1941:={dn})`.
- Without `bind_dn` the search is anonymous.
- Empty passwords are always rejected (protection against "unauthenticated bind").
- With **Test sign-in** in the admin area you see which values and groups arrive, without creating a session.

### Directory sync (without a sign-in)

The sync takes people and groups from the directory, also for people who never signed in. So you can give rights, watches and workflows to groups at once, and people who leave the company lose access at the next sync, not only when their password no longer works.

Set it up under *Administration → LDAP / Active Directory*. There you also find **Preview** (shows all changes, changes nothing), **Sync now** and the recent syncs. With the CLI:

```bash
ewiki directory sync ldap --dry-run      # preview
ewiki directory sync ldap                # sync; ldap-<id> for further instances
ewiki directory runs ldap                # recent syncs
ewiki directory adopt ldap anna          # same person with a new directory id (see below)
```

```yaml
plugin_settings:
  auth.ldap:
    # … connection as above …
    sync_enabled: true               # automatic; otherwise only with the button or the CLI
    sync_interval_minutes: 60        # at least 5
    sync_filter: (objectClass=inetOrgPerson)   # empty: user_filter with * instead of {username}
    disabled_filter: (userAccountControl:1.2.840.113556.1.4.803:=2)   # AD only: locked accounts
    sync_create_users: true          # create accounts for new people
    sync_disable_missing: true       # people who are missing are disabled
    sync_max_disable_percent: 10     # protection, see below
```

- **Same values as at a sign-in:** attributes, the group search (also nested AD groups) and the group and role mapping are the same. An account from the sync is the same account as after a sign-in.
- **What the sync changes:** name and e-mail, the groups and roles this directory gives, and whether the account is active. **Groups given by hand in the wiki stay.** Accounts that SCIM manages are left out.
- **Disabling:** people who are no longer in the directory (or whom the `disabled_filter` finds) are disabled and signed out at once. Pages and contributions stay. If the person comes back, the sync enables the account again. An account an administrator disabled is never enabled by the sync. The last active administrator account is never disabled.
- **Protection against mistakes:** if a sync would disable more than `sync_max_disable_percent` percent of the active accounts (for example because of a wrong filter or an empty search result), it stops and changes nothing. Only if that many people really left, start it with **Sync anyway** (CLI: `--force`). If the directory has more entries than the limit *People per directory sync* (expert settings, default 100,000), the sync stops as well instead of syncing only a part.
- **Conflicts:** if the user name already belongs to another account of this directory (typically: a person deleted and created again in the directory, so with a new id), the wiki does not merge the accounts itself. It could be another person, who would otherwise get someone else's pages and rights. The sync reports a conflict, and the sign-in is refused with a note. If it is the same person, choose **Same person: keep the account** (CLI: `ewiki directory adopt`). The new id is read from the directory, never typed in. If it is another person, delete the old account; the next sync creates a new one.
- **Rights and log:** only people with the rights *Manage users* and *Change settings* may sync and keep accounts. Every run is in the audit log with a summary (`auth.directory.sync`, `auth.directory.adopt`); the newest 100 runs per directory are kept with all changes.
- The whole sync is one transaction: if it stops, everything stays as before. Rights apply at once, also in running sessions.

## OpenID Connect (`auth.oidc`)

Authorization code flow with PKCE, nonce, signature and audience checks of the ID token. Register as redirect URI at the provider:

```
<server.base_url>/api/v1/auth/sso/oidc/callback
```

```yaml
plugin_settings:
  auth.oidc:
    name: Company sign-in         # label of the button
    issuer: https://keycloak.example.com/realms/acme
    client_id: enterprise-wiki
    client_secret: ${OIDC_CLIENT_SECRET}
    scopes: [openid, profile, email]
    claim_username: preferred_username
    claim_email: email
    claim_display_name: name
    claim_groups: groups
```

| Provider | Notes |
|---|---|
| Keycloak | `issuer` = realm URL. For groups create a mapper "Group Membership" (token claim name `groups`, "Full group path" off) in the client scope |
| Microsoft Entra ID | `issuer: https://login.microsoftonline.com/<tenant-id>/v2.0`, `claim_username: preferred_username`. For groups enable "groupMembershipClaims" in the app manifest. Object IDs arrive, which you map with `group_mapping` |
| Authentik, Zitadel, Google | the standard claims are enough. Google delivers no groups |

**Check connection** in the admin area loads the discovery document of the provider.

**Sign-out (single logout):**

- *RP-initiated logout:* when signing out in the wiki, the browser is sent to the `end_session_endpoint` of the provider (with `id_token_hint` and `client_id`), if the provider has one. Register `<server.base_url>/login?logged_out=1` as post-logout redirect URI or set `post_logout_redirect_uri`. With `single_logout: false` only the session in the wiki ends.
- *Back-channel logout:* enter `<server.base_url>/api/v1/auth/sso/oidc/logout` as back-channel logout URL at the provider (Keycloak: "Backchannel logout URL", "Backchannel logout session required" on). The wiki checks the logout token completely (signature, issuer, audience, expiry, logout event, no `nonce`, current `iat`, `jti` only once) and ends exactly the session named (`sid`) or all sessions of the user (`sub`).

## Shibboleth / SAML 2.0 (`auth.shib`)

### Native mode (recommended)

The wiki itself is the SAML service provider. A key pair is created on the first call and stored in the database.

```yaml
plugin_settings:
  auth.shib:
    mode: saml
    name: University sign-in
    idp_metadata_url: https://idp.uni.example/idp/shibboleth   # or idp_metadata: <XML>
    attr_username: urn:oid:1.3.6.1.4.1.5923.1.1.1.6            # eduPersonPrincipalName
    attr_email: urn:oid:0.9.2342.19200300.100.1.3              # mail
    attr_display_name: urn:oid:2.16.840.1.113730.3.1.241       # displayName
    attr_groups: urn:oid:1.3.6.1.4.1.5923.1.5.1.1              # isMemberOf
```

- Metadata of the wiki for the IdP or the federation: `<server.base_url>/api/v1/x/auth.shib/metadata`
- Assertion consumer service: `<server.base_url>/api/v1/auth/sso/shib/callback` (HTTP POST)
- Attributes can be given with the name (OID) or the FriendlyName.
- Federation metadata (EntitiesDescriptor) is supported. The first IdP in it is used. For a particular organisation enter the metadata of the single IdP.
- Single logout service: `<server.base_url>/api/v1/auth/sso/shib/logout` (HTTP redirect and HTTP POST; listed in the metadata).
- Only sign-ins started by the wiki are accepted (no IdP-initiated SSO), every response and every LogoutRequest only once (also across several instances).
- **Single logout (SLO):** when signing out in the wiki, a signed LogoutRequest (NameID and SessionIndex) goes to the IdP, if it offers an SLO endpoint. A LogoutRequest of the IdP is only accepted with a valid signature, matching issuer and destination and a fresh timestamp. It ends the sessions named and is answered with a signed LogoutResponse. With `single_logout: false` the wiki sends no LogoutRequest to the IdP.

### Header mode (Apache with mod_shib)

When a Shibboleth SP installation already exists:

```yaml
server:
  trusted_proxies: [127.0.0.1]     # headers are only accepted from here!
plugin_settings:
  auth.shib:
    mode: header
    header_username: eppn          # or REMOTE_USER
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
RequestHeader unset eppn early      # drop headers from outside
```

## SCIM 2.0: provisioning from the directory

Microsoft Entra ID, Okta and other directories create, change, lock and remove people and groups through SCIM 2.0 (RFC 7643/7644) before anyone signs in. Set it up under *Administration → SCIM provisioning* or with the CLI:

```bash
ewiki scim create "Entra ID" --provider oidc-entra --subject-from externalId   # shows tenant URL and token (once)
ewiki scim list | ewiki scim token 3 | ewiki scim delete 3
```

In the directory enter `<server.base_url>/scim/v2` as **tenant URL** and the token as **secret token**.

- **Recognition at sign-in:** every access belongs to a sign-in method (`--provider`). Provisioned accounts get its provider ID and as stable ID the SCIM attribute `userName` or `externalId`. When the person signs in, the wiki finds the account by this ID. For **Entra ID with OpenID Connect**: `--subject-from externalId` (Entra sends the objectId) and `claim_subject: oid` at the OIDC provider. With Okta and Keycloak `userName` usually matches the user name at the provider.
- **What SCIM may do:** people of its own sign-in method that no other access manages, and groups it created. **Never administrators and never roles.** Administrators decide under *Groups* which roles a provisioned group gets. Local accounts are invisible to SCIM.
- **Name and addresses** come from the directory: sign-in does not overwrite them for provisioned accounts. Addresses from SCIM count as confirmed. If the directory no longer sends an address, it is removed. An address another account has confirmed is rejected (409).
- **Locking** (`active: false`) ends all sessions at once. **Deleting** locks the account (default) or deletes it (`--on-delete delete`).
- Supported: users and groups with GET (filter `attribute eq "value"` on `userName`, `externalId`, `emails.value`, `displayName`, `id`; paging), POST, PUT, PATCH (as sent by Entra ID and Okta), DELETE. Not supported: bulk, sorting, ETags. Attributes the wiki does not know (e.g. the enterprise extension) are ignored.
- Security: only the token of the access authenticates, no session and no API key. Only its hash is stored. After 30 wrong tokens in 5 minutes the IP address is rejected temporarily. Every change is in the audit log as `scim:<name>`. With several sites with shared accounts, SCIM only exists on the main site.

## Sessions

- A browser session ends after `auth.session_ttl` (default 7 days) and after `auth.session_idle` without activity (default 12 hours). Automatic requests of the interface (notifications) do not keep a session alive.
- Under *Profile → Sessions* every person sees where they are signed in (device, IP address, sign-in method, start, last activity) and can end single sessions or all others ("Sign out everywhere else"). The session token itself is never shown.
- Whoever changes their own password stays signed in only in the current session. If an administrator changes the password or locks the account, all sessions end.
- Administrators see and end the sessions of a user under *Administration → Users → E-mail & sign-ins*.
- Sessions can neither be listed nor ended with an API key.
- With sign-in through OIDC or SAML, the wiki stores with the session what the provider needs for single logout (OIDC: `sid` and the ID token as `id_token_hint`; SAML: SessionIndex and NameID).

```bash
ewiki user sessions anna            # list sessions
ewiki user logout anna 42           # end one session
ewiki user logout anna              # end all sessions
ewiki api GET /api/v1/me/sessions --as anna
```

## Several factors: the factor chain

Every further factor of the sign-in is an **MFA plugin**. The chain applies to **all kinds of sign-in**: local accounts, LDAP, OpenID Connect and SAML.

| Plugin | Factor | Default number | Notes |
|---|---|---|---|
| `mfa.webauthn` | security key or passkey | 1 | FIDO2/WebAuthn: YubiKey, Windows Hello, Touch ID, Android. As factor 1 every key is a **passkey** (user verification with PIN or biometrics is required) and replaces the password ("Sign in with passkey") |
| `mfa.totp` | authenticator app | 2 | RFC 6238, 6 digits, 30 seconds, every code only once |
| `mfa.email` | code by e-mail | 2 | 6 digits, 10 minutes, at most 5 attempts, at most 5 codes in 15 minutes. Needs working mail delivery |
| `mfa.network` | allowed networks | 2 | without a prompt: fulfilled when the sign-in comes from an allowed network of the person (see below) |

**Factor numbers.** When an MFA plugin is switched on, the administration asks which factor it is (1–9). Under *Administration → Sign-in factors* the number can be changed at any time.

- Factors with **the same number** are alternatives: the person chooses one of them.
- **Higher numbers** are asked afterwards, in ascending order. This gives chains of any length, e.g. password → app or e-mail (2) → security key (3).
- **Counts as two factors** (only passkeys, setting `counts_two`, selectable when switching on and under *Sign-in factors*): the device checks the person itself (PIN, biometrics). A passkey then counts twice and also covers the next level of the chain – e.g. passkey instead of password **and** app. Only allowed networks for which there is no alternative at this level still decide.
- **Factor 1** stands next to the password or the sign-in method and is only allowed for plugins that identify the person themselves (passkeys). A passkey only counts if factor 1 is part of the chain of the sign-in method of its account.

**Per sign-in method (instance).** Under *Administration → Sign-in factors* you choose for every instance (e.g. `local`, `oidc-1`, `ldap-2`):

- **Which factors apply:** all active ones (also those switched on later) or exactly the chosen ones. Without factors the wiki asks nothing further after this sign-in, e.g. when the identity provider itself requires MFA (Entra ID with conditional access).
- **How many factors a sign-in needs** (`mfa_required`, default 1). With 1, further factors are only asked if the person has set them up. With 2 or more, someone with too few factors sets one up during the sign-in, before a session is created.

In the configuration the values are in the plugin settings: `factor` for the MFA plugin, `mfa_methods` (list; missing = all) and `mfa_required` for the sign-in plugin or its instance.

```yaml
plugin_settings:
  mfa.totp:   { factor: 2 }
  mfa.email:  { factor: 2 }
  mfa.webauthn: { factor: 1, counts_two: true }
  auth.local: { mfa_methods: [totp, webauthn], mfa_required: 2 }
  auth.oidc:  { mfa_methods: [] }   # the identity provider does MFA
```

**Procedure.** First the sign-in method checks the person (or a passkey as factor 1). If the sign-in needs further factors, no session is created yet but a waiting sign-in: valid for 10 minutes, at most 5 attempts, usable only once. Only the last factor creates the session. With single sign-on the wiki sends the browser to `/login?mfa=1` for this; the waiting sign-in is in a short-lived HttpOnly cookie, not in the address. If a person has set up no factor for a level, this level is left out – unless `mfa_required` requires more.

- **Setting up:** *My profile → Sign-in factors* shows your own chain and the factors to set up. Setting up and removing only works in the browser (not with an API key), only up to 15 minutes after signing in, and only for factors of your own chain.
- **Security keys:** signature, origin and relying party ID are checked. A key whose counter jumps back (a hint of a copy) is rejected and logged. WebAuthn needs a domain name (no IP address); the default is the host of `server.base_url`, otherwise `plugin_settings."mfa.webauthn".rp_id` and `origins`.
- **Resetting:** with a lost device an administrator removes app, e-mail codes and security keys under *Users → Edit → Reset all factors*. With the CLI: `ewiki user mfa-reset NAME`.
- **API:** `POST /api/v1/auth/login` answers with `401 mfa_required` when further factors are needed; `detail` contains the token of the waiting sign-in. Then `POST /api/v1/auth/mfa/info` (current level: `verify` or `enroll`, factor number, alternatives), `…/mfa/challenge` (send the e-mail code, WebAuthn options), `…/mfa/verify` or `…/mfa/enroll/begin` and `…/enroll/finish`. Every answer is either the next level (`done: false`) or the session (`done: true`). `GET`/`PUT /api/v1/admin/mfa` manage the chain, `GET /api/v1/me/mfa` and `POST /api/v1/me/mfa/{factor}/begin|finish` your own factors.

### Allowed networks (`mfa.network`)

Under *My profile → Sign-in factors → Allowed networks*, or by administrators under *Users → Edit → Allowed networks*, an account can be restricted to IP addresses and networks (CIDR, IPv4 and IPv6, e.g. `10.0.0.0/8`, `192.0.2.7`, `2001:db8::/32`). Empty means: everywhere.

- **As a factor** the level counts as fulfilled when the sign-in comes from an allowed network. If another factor with the same number stands next to it (e.g. the app), it is the alternative from outside; otherwise the sign-in from outside is rejected (`403 network_denied`, logged as `auth.network_denied`).
- **Always enforce** (`plugin_settings."mfa.network".enforce_always`, default `true`): the networks additionally apply to all kinds of sign-in (SSH too) and to existing sessions, API keys and feed links.
- Whoever sets their own restriction must include the current address. Behind a reverse proxy `server.trusted_proxies` must be set so the real client address counts. With the CLI: `ewiki user networks NAME [NETWORK ...]` (without networks the restriction is lifted); with the API `GET`/`PUT /api/v1/x/mfa.network/me` or `…/users/{id}`.

## Scripts and API

- Local and LDAP accounts: `POST /api/v1/auth/login` (session cookie) or better an **API key** or **SSH key** (see [security.md](security.md)).
- SSO accounts (OIDC/SAML) sign in once in the browser and then create an API key or SSH key in the profile.
