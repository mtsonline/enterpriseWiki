# Office programs: Collabora, OnlyOffice, Microsoft 365

Office files of the media library (documents, spreadsheets, presentations) can be opened and edited directly in the
browser in an office program. Each program is its own plugin; the settings are under
*Administration → Collabora Online / OnlyOffice / Microsoft 365* (or in `config.yaml` under `plugin_settings`).

| Plugin | Interface | Read-only without download | Watermark of the viewer |
|---|---|---|---|
| `office.collabora` – Collabora Online / CODE | WOPI | yes | yes |
| `office.onlyoffice` – OnlyOffice Docs | OnlyOffice API (JWT) or WOPI | yes | no |
| `office.m365` – Microsoft 365 for the web | WOPI (only with CSPP) | no | no |

## Who gets what?

Every time a file is opened, the wiki decides by the **permissions of the file** – never more than you have:

- **Edit permission** (and download allowed): the file opens for editing. Every save is a
  new version (note: name of the program); earlier versions can be restored.
- **View only with download permission:** read-only in the office program.
- **Without download permission** (or "download allowed" switched off): read-only in the office program with
  download, printing, exporting and copying switched off – only if the plugin can enforce that and
  the option *read-only without download* is active (default). Otherwise only in the browser.
- **Watermark for viewers:** view only protected and only in programs that show the name
  (Collabora). Only managers of the file edit the original.

Under *Administration → Settings → Media* you choose the **default viewer** (browser or an active
office program). *Always open files directly in the default program* applies only to this one program: a click
on a fitting file opens it there at once. The detail view offers the other programs ("View in …",
"Edit in …").

## Security

- For every opening the office program gets an **access key for exactly one file, one user and one
  mode** (edit or view). Only its SHA-256 hash is stored. Validity: `media.token_ttl`
  (default 10 hours, at most 24).
- **On every access** the wiki checks the permissions again: if a permission is revoked, the file deleted, the user
  locked, "download allowed" switched off or the plugin disabled, the key stops working at once.
- The key is passed into the frame by POST (never in a browser address) and never stored in the guest cache.
- **API keys** and the command line without `--as` cannot create access keys (they would
  otherwise have the full permissions of the user instead of the limits of the API key).
- The WOPI endpoint (`/api/v1/wopi/files/…`) evaluates no sessions or API keys, only the access key.
- **Proof keys:** with `verify_proof: true` (default) the wiki only accepts WOPI requests that the office server
  signed with its key from the discovery (timestamp at most 20 minutes old; key changes are
  detected automatically). For this `server.base_url` must be exactly the public address under which the
  office server calls the wiki (scheme, host, port).
- **OnlyOffice API:** configuration, file download and callbacks are signed with the JWT secret; without a secret
  the API interface is off. The wiki downloads saved files only from the configured server address
  (or `internal_url`) – no other targets (SSRF protection).
- The pages of the wiki allow the addresses of active office programs as frames (`frame-src`, `form-action`) and for
  the OnlyOffice API as a script source (`script-src`) – nothing else.
- Every save is in the audit log (`media.office_save`, `via = office`).

## With Docker Compose

With HTTPS through the bundled proxy, `./docker-setup.sh` asks for an office server (none, Collabora or
OnlyOffice) and its **own host name** (e.g. `office.example.com`, DNS to this server). An own name
means an own origin: the office program shares no cookies with the wiki. The script sets the profile
(`COMPOSE_PROFILES=proxy,collabora` or `proxy,onlyoffice`), the addresses and for OnlyOffice a random
JWT secret; the wiki takes them as the default of the plugin settings and chooses the program as the
default viewer (both can be changed in the administration). Caddy also gets the certificates for the office host.

- Collabora (`collabora/code`, `COLLABORA_VERSION`) runs as its own unprivileged user with only the
  capabilities the document jails need; the admin console is off.
- OnlyOffice (`onlyoffice/documentserver`, `ONLYOFFICE_VERSION`) needs about 4 GB RAM and starts its services in the
  container as root; it is only reachable through the proxy and requires JWT on every request.
- Office server and wiki reach each other through the proxy in the Docker network (without a detour through the internet).
- The CI starts both real servers and opens, edits and saves one document each in a real browser
  (`scripts/test-docker-office.sh`).

## Collabora Online

```yaml
plugin_settings:
  office.collabora:
    url: https://collabora.example.com     # discovery: /hosting/discovery
    verify_proof: true
    readonly_without_download: true
```

The Collabora server must reach the wiki under `server.base_url` and allow it as a WOPI host
(`coolwsd.xml`: `storage.wopi.alias_groups`, or `aliasgroup1=https://wiki.example.com:443` in the Docker image).

## OnlyOffice Docs

```yaml
plugin_settings:
  office.onlyoffice:
    url: https://docs.example.com
    mode: api                     # or wopi (Document Server: wopi.enable = true)
    jwt_secret: ${ONLYOFFICE_JWT} # = services.CoAuthoring.secret.*.string
    internal_url: http://onlyoffice   # optional, when saved files are delivered internally
```

## Microsoft 365 for the web

Microsoft only serves WOPI hosts from the **Cloud Storage Partner Program (CSPP)**; Microsoft enables the domain of the
wiki. The plugin only calls Microsoft servers once it is switched on:

```yaml
plugin_settings:
  office.m365:
    enabled: true
    url: https://onenote.officeapps.live.com/hosting/discovery   # default (production)
    verify_proof: true
```

Microsoft 365 cannot reliably block downloading and printing: users without download permission therefore see files
only in the browser.

## Command line and API

```bash
ewiki media viewers                                   # browser and active office programs
ewiki media programs 42 --as anna                     # how anna may open the file
ewiki media open 42 --as anna --viewer office.collabora --mode view
ewiki api POST /api/v1/x/office.collabora/test        # test the connection
```

API: `GET /api/v1/media/viewers`, `GET|POST /api/v1/media/{id}/open`, WOPI host under `/api/v1/wopi/files/{id}`.
