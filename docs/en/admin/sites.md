# Several sites (multi-site)

One instance can run several sites, e.g. an intranet, a public website and a project wiki. Each site is an independent wiki with its own content, settings, plugins and permissions. Users, groups and role definitions are shared.

## What is shared and what is not

| Shared (schema `public`) | Per site (schema `site_<name>`) |
|---|---|
| user accounts (password, 2FA, allowed networks, profile) | pages, categories, files, revisions, search |
| groups and memberships | role **assignments** and permissions (ACLs), default permissions |
| role **definitions** | sessions, sign-in processes, API keys |
| SSH keys | e-mail queue, notifications, watches, webhooks |
| list of sites, instance admins | audit log, translations of the interface |
| license (counts the users of the whole instance) | all plugin tables |

Sign-in is separate per site: a session of site A does not count on site B. Each site decides itself which sign-in methods it offers (local, LDAP, OIDC, SAML). If the same provider is set up on two sites, it is the same account. Whoever is already signed in at the identity provider gets through without entering the password again.

## Shared or own accounts

When a site is created, you decide where its accounts come from; this cannot be changed later:

- **Shared with the instance** (default): users, groups and role definitions of the instance, permissions per site (see the table above).
- **Own accounts of the site**: the site has its own users, groups, roles and SSH keys in its schema. It does not see the accounts of the instance and vice versa – for strictly separated groups of people (e.g. a customer or partner site). Its first administrator (local account) is given when the site is created; after that its administrators manage the accounts themselves. Such sites have no instance admins: account IDs belong to the site there, not to the instance.

The default for new sites is in `config.yaml`:

```yaml
sites:
  accounts: shared   # or separate
```

The license counts the active accounts of all sites together (shared and own). When a site starts, the wiki checks that the account tables are where the setting of the site expects them, and refuses to start otherwise – so queries can never use accounts of another site by mistake.

## Admin levels

- **Instance admins** manage sites, users, groups, role definitions, the license and the plugin installation. An instance admin is whoever is administrator on the site `main` or was added under *Administration → Sites → Instance administrators*.
- **Site admins** (administrator role on their site) manage content, settings, plugins, permissions and role assignments of their site. They cannot change shared data (accounts, groups).

## Creating a site

In the interface under *Administration → Sites → New site* or with the CLI:

```bash
ewiki site create docs --host docs.example.org --title "Documentation"
ewiki site create partner --host partner.example.org --accounts separate --admin chief   # asks for the password
ewiki site list
ewiki site update docs --host docs.example.org,doku.example.org
ewiki site update docs --disable        # stop the site, the data stays
ewiki site delete docs --yes            # delete the site with all its data
ewiki site admins add 42                # add an instance admin
```

API (instance admins only): `GET|POST /api/v1/instance/sites`, `PATCH|DELETE /api/v1/instance/sites/{name}`, `GET|POST /api/v1/instance/admins`, `DELETE /api/v1/instance/admins/{id}`.

The name (`[a-z][a-z0-9_]*`, at most 31 characters) determines the schema `site_<name>`. Every further site needs at least one host name; the host names must point to the server in DNS. Requests to unknown host names are answered by the site `main`. With the [bundled proxy](installation.md#bundled-proxy-docker-compose-caddy) every new host name gets a certificate automatically.

## Settings of a site

The settings in `config.yaml` are those of the site `main` and at the same time the default for all other sites. What a site sets differently (in its administration or with `ewiki --site docs config set …`) is stored in `sites/<name>.yaml` next to `config.yaml`, and only the differences. Changes to `config.yaml` therefore affect all sites that have not set the value themselves.

Only for the whole instance: `mode`, `database`, `license`, `server.listen`, `limits`, `server.trusted_proxies`, `server.client_ip_header`, `server.max_body_mb`. These values are ignored in site files.

Files of a site are in `<storage.path>/sites/<name>`, as long as the site does not set its own `storage.path`.

## CLI for a particular site

Local commands work on the site `main`. For another site give `--site NAME` or `EWIKI_SITE=NAME`:

```bash
ewiki --site docs user list
EWIKI_SITE=docs ewiki plugin disable content.blog
```

## Notes

- Plugins are compiled in for the whole instance; each site switches them on and off individually. Plugin tables are created in every site, even if the plugin is off there.
- On sites with shared accounts, the user search shows all accounts of the instance. For separated groups of people there are sites with own accounts.
- Backups: a `pg_dump` of the database contains all sites; a single site can be backed up with `pg_dump --schema=site_<name>`.
