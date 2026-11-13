# Operation: updates, backup, CLI, maintenance

## Updates

### systemd installation

```bash
sudo /opt/enterprisewiki/scripts/update.sh            # to the newest release tag (v*)
sudo /opt/enterprisewiki/scripts/update.sh --ref v7.1.0
sudo /opt/enterprisewiki/scripts/update.sh --enable-timer   # weekly, automatically (Sun 03:30 ± 1h)
```

Procedure and safety net:

1. get the new state (`git fetch --tags`), check out the target,
2. build frontend and backend into `bin/ewiki.new`. On an error everything stays unchanged,
3. self-test of the new binary,
4. database backup with `pg_dump` into `backups/`,
5. swap the binary (the old one stays as `bin/ewiki.prev`), restart the service,
6. health check `/readyz`. If it fails, the old binary and the old source state are **restored automatically** and the service is restarted.

Database migrations run automatically on start, once and in transactions. With several instances a database lock prevents parallel migrations. Migrations only add structures, so the previous version can keep running after a rollback. For a complete way back there is the backup.

### Docker

```bash
docker compose up -d          # pulls the newest image and restarts (migrations run automatically)
```

With a self-built image (own plugins): `git pull && docker compose up -d --build`.

## Automatic updates of the components

Enterprise Wiki stays on the newest state of all components (Go, Go modules, Node.js,
npm packages, Docker base images, GitHub Actions) without manual intervention:

1. **Renovate** (`renovate.json`) checks daily before 6 a.m. and opens pull requests: smaller updates collected in one,
   big version jumps one by one. New versions are only taken after **3 days** (withdrawn or
   compromised packages do not reach the product this way), **security updates at once**.
2. The **CI** checks every pull request completely: build, Go tests with PostgreSQL, browser tests with
   accessibility checks, Docker build and license check of the dependencies. Only if everything is green does
   Renovate merge the pull request **automatically** – also for big version jumps. Red pull requests stay
   open; GitHub reports this by e-mail. The overview of all open updates is in the issue "Dependency Dashboard".
3. After every change on `main`, **docker-publish** rebuilds the image `ghcr.io/mtsonline/enterprisewiki:latest` (amd64,
   arm64); version tags `v7.5.0` additionally give `:7.5.0`, `:7.5` and `:7`.
4. The nightly **canary** run builds and tests with the very newest versions (without waiting time, without taking
   anything over) as an early warning.
5. The daily check **Outdated components** (`.github/workflows/outdated.yml`, locally `go run ./tools/outdated`)
   compares the Go toolchain, direct Go modules, npm packages and the Node.js LTS with the newest stable
   version of each. If an update is open for longer than 7 days (e.g. a Renovate PR with red CI), it fails – so
   nothing becomes outdated unnoticed.

**PostgreSQL major versions:** Renovate raises the *recommended* version (`docker-compose*.yml`, `.env.example`,
notice in the administration), the CI tests it and the upgrade from the older versions. Existing installations
only switch with `./docker-postgres-upgrade.sh` (next section). The `go` line in `go.mod` stays the
minimum version for building; the Go version from the `toolchain` line is always used.

## PostgreSQL upgrades (Docker)

Docker Compose uses the **official** image `postgres:<version>`. The version is in `.env`
(`POSTGRES_VERSION`), the data of every major version is in its own folder: `./data/postgres/18/docker`,
later `./data/postgres/19/docker` and so on. So a `docker compose up -d` **never** changes the major version by itself.

**When?** If a newer version is recommended or the support of the version in use by the
PostgreSQL project ends (five years after its release), the administration shows it (notice on all admin pages and
under *Settings → Database*), the server log on start and an e-mail to all administrators – once when
a new version is recommended, and 180 and 30 days before and after the end of support. API:
`GET /api/v1/admin/database`.

**How?** In the folder of the installation:

```bash
./docker-postgres-upgrade.sh             # to the recommended version (asks first)
./docker-postgres-upgrade.sh --check     # only check (exit 0: current, 10: upgrade recommended)
./docker-postgres-upgrade.sh --yes       # without asking, e.g. from cron/systemd timer
./docker-postgres-upgrade.sh --to 19     # to a particular version
./docker-postgres-upgrade.sh --link      # pg_upgrade --link: fast, hardly any extra space, the old data cannot be used afterwards
```

Procedure:

1. Checks the free space (copy: about the size of the database) and stops the containers.
2. Starts a container from the **official image of the new version** once and installs the programs
   of the old version in it from the official PostgreSQL package archive (already set up in the image; needs internet access).
3. Reads encoding, collation and checksum setting of the old data, creates the new data set to match,
   runs `pg_upgrade --check` and then `pg_upgrade`.
4. Compares the row count of **every table of all databases** before and after, updates the statistics.
5. Enters `POSTGRES_VERSION` in `.env` and starts everything again.

If a step fails, nothing is switched: the incomplete new folder is removed and the previous
version starts again. The old data stays in `./data/postgres/<old>` (way back: reset `POSTGRES_VERSION` in
`.env`); delete it after checking with `rm -rf data/postgres/<old>`. The CI tests the upgrade from 16 and
17 to the recommended version (test data, `ltree`, several databases, roles and passwords, copy and `--link`,
the wiki itself, own user instead of root).

With installations without Docker (systemd) the organisation runs PostgreSQL itself; there the upgrade procedure of the
distribution applies (e.g. `pg_upgradecluster`). The notices in the administration appear there as well.

## Backup and restore

To back up:

| What | Where |
|---|---|
| database | `pg_dump --format=custom -f ewiki.dump "$EWIKI_DATABASE_URL"` |
| configuration | `config.yaml` and `.env` (contains secrets, store it encrypted) |
| uploaded files | directory from `storage.path` (content-addressed, files never change: an incremental backup is enough) |
| own plugins | `plugins/` in the source tree (or the Git repository of the plugin) |

Restore:

```bash
systemctl stop enterprisewiki
pg_restore --clean --if-exists -d "$EWIKI_DATABASE_URL" ewiki.dump
systemctl start enterprisewiki
```

## Command line (`ewiki`)

The CLI is the same binary. It works directly with the configuration and the database and runs with superadmin rights. Whoever can run it as the service user has full control. With `--config` or `EWIKI_CONFIG` you choose the installation.

```text
ewiki serve | setup [--non-interactive] | migrate | version
ewiki api METHOD PATH [BODY|@file.json|-] [--as USER]
ewiki user list|create|passwd|disable|enable|unlock ...
ewiki plugin list|enable|disable|install|remove|generate|build ...
ewiki config show|path|validate|get|set ...
ewiki retention run
ewiki signature verify FILE [--offline] | ewiki signature keys
ewiki smime list [--all | --mine --as USER]
ewiki smime add FILE [--label TEXT] [--password-file PATH | --password-stdin] [--seal] [--no-sign] [--mine --as USER]
ewiki smime set ID [--label TEXT] [--sign | --no-sign] [--seal | --no-seal] | ewiki smime remove ID
ewiki smime settings [mail_signing=true|false] [on_expired=unsigned|hold] [personal=true|false] [seal_evidence=true|false]
ewiki mail status|list|show|retry|delete|test|send|bounces ...
ewiki jobs list | ewiki jobs run NAME
ewiki cache status|clear
ewiki watch list|add|remove ... --as USER
ewiki watch assign list|add|remove ...
ewiki perm list | overview TYPE [ID] | defaults set PERMISSION...
ewiki audit search|export [filter] | settings [set @file] | forward-test @target.json
ewiki draft list|show|publish|delete ... --as USER
ewiki inbox [list] | read [ID...] --as USER
```

**Every API function** is available through `ewiki api`, those of plugins too:

```bash
ewiki api GET /api/v1/categories
ewiki api POST /api/v1/categories '{"titles":{"en":"Staff"},"parent_id":1}'
ewiki api GET /api/v1/x/core.menu/menus
ewiki api GET /api/v1/categories/1 --as anna        # with the permissions of anna@local
```

`--as USER` works with **exactly** the permissions of this user, never with
more: the special rights of the command line (e.g. approving shell commands)
do not apply then, not even with `--as admin`. The option only exists in
the local command line, which has access to configuration and
database anyway; through HTTP, API keys or SSH tokens nobody can pretend to be
another user. In the audit log such calls appear with
`via = cli-as`.

Typical tasks:

```bash
ewiki user create anna --display-name "Anna Example" --email anna@example.com   # asks for the password
echo 'New-Password-42' | ewiki user passwd admin --password-stdin
ewiki user create emergency --admin --password-stdin < pw.txt   # local emergency admin
ewiki user unlock anna
ewiki plugin disable example.notice
ewiki mail status                       # queue: waiting, sent, failed, oldest waiting message
ewiki mail list --status failed         # failed messages (with recipients)
ewiki mail retry 123                    # try failed recipients again
ewiki mail bounces                      # fetch the bounce mailbox now
ewiki cache clear                       # clear the guest cache on all servers
ewiki license install @license.txt      # install a license key (checked offline)
ewiki license show                      # licensee, users, term
ewiki license notices                   # licenses of the included components
ewiki watch add category 12 --mode content --as anna   # anna watches category 12 (with content)
ewiki watch add category 12 --frequency daily --as anna    # daily summary instead of immediate e-mails
ewiki watch assign add group 4 category 12 --mandatory --frequency weekly --note "Required for the team"
ewiki perm overview group 4             # direct and effective permissions of group 4 per category
ewiki perm defaults set core.share.use core.mentions.use   # default permissions of all signed-in people
ewiki audit search --actor anna --from 2026-11-01 --area acl   # permission changes by anna since 1 Nov
ewiki audit export --format csv --area auth > signins.csv
ewiki draft publish 7 --at 2026-12-01T08:00:00+01:00 --as anna   # publish a draft at a planned time
ewiki api GET /api/v1/admin/webhooks                      # webhooks and events
ewiki api POST /api/v1/admin/webhooks/3/test              # send a test message
ewiki api GET /api/v1/x/content.events/upcoming        # next events of all calendars
ewiki api GET '/api/v1/elements?q=calendar&type=core.html'   # search elements (like Administration → Elements)
ewiki api PATCH /api/v1/elements/12 '{"short_url":"calendar"}' # set a short URL
ewiki config set ui.home_element 12                    # element 12 as home page
```

## Demo content and reset

Under *Administration → Demo* (administrators only) **Create demo** creates a space "Demo: Enterprise Wiki"
that shows as many features as possible: tour page with macros (table of contents, notice boxes, status labels,
columns, expandable sections, gallery, child pages, recently changed, next events), page properties, comments
with @mentions, reactions and favourite, Markdown page, website building blocks, blog, FAQ, event calendar with
series and registration, team page, media (hidden category as folder, images, PDF for viewing only with
watermark, Word file for the office program), file element, process, property report,
category overview, short URL `/demo`, a menu, a text block and a carousel (only for signed-in users). What
a disabled plugin needs is left out.

- The **demo accounts** (`demo-anna`, `demo-ben`, …) have no sign-in (provider `demo`), no e-mail address
  and do not count for the license.
- Everything the demo creates is noted in `demo_objects`. **Remove demo** deletes exactly that, completely:
  content with versions, translations, addresses, comments, reactions, favourites, notifications,
  media and stored files, accounts and groups of the demo and their entries in the audit log. Own content
  stays untouched – but own pages you created in demo categories disappear with them.

**Reset the site to the first start** (on the same page) deletes all content, media, files, versions, the
trash, the audit log and the settings of the site. What remains is your administrator account, the roles and the
welcome page – as after `ewiki setup`. Instance settings, address, languages, e-mail delivery, name of the site and
license remain. With several sites with shared accounts, the accounts remain (they belong to all sites).
Confirmation: type the name of the site and (with a local account) your own password; with API keys this is not
possible. Afterwards you sign in again. Make a backup first!

```bash
ewiki demo status
ewiki demo create --as admin
ewiki demo remove
ewiki demo reset-site --as admin --confirm "Enterprise Wiki"
```

## Maintenance tasks

Every hour the following runs automatically (in a cluster only on one instance):

- delete expired sessions, SSH challenges and SSO requests,
- remove files that are no longer referenced (older than one day),
- apply retention rules (old versions, trash, log, see [configuration.md](configuration.md#retention)).

Background jobs also run (list and "run now" under *Administration → E-mail*, through the API `GET /api/v1/admin/jobs`, `POST /api/v1/admin/jobs/{name}/run`):

| Job | Interval | Task |
|---|---|---|
| `mail` | 10 s (and at once for new messages) | send e-mails from the outbox: per recipient, temporary errors retried without limit |
| `mail.bounces` | 5 min | read the bounce mailbox through IMAP and assign bounces (only when `mail.bounce` is set up) |
| `watch` | 30 s | send collected changes to watched categories and pages as one e-mail per person (after 2 minutes of quiet) |
| `content.events.reminders` | 1 min | event reminders to registered and interested people |
| `mail.cleanup` | 6 h | delete sent e-mails after `retention.mail_days` |
| `search.changes.cleanup` | daily | shorten the change log for external search engines (30 days) |
| `workflow.process.timers` | 30 s | continue waiting times and webhooks of workflows, reminders for overdue tasks |
| `search.external.sync` | 10 s | synchronise the external search index (only when configured) |

In cluster mode each job only runs on one instance at a time (advisory lock).

## Logs and monitoring

- Logs go to stderr, so with systemd into the journal (`journalctl -u enterprisewiki -f`). JSON format with `EWIKI_LOG_FORMAT=json`, more details with `EWIKI_LOG_LEVEL=debug`.
- `GET /healthz` reports that the process runs. `GET /readyz` also checks the database (for load balancers and Kubernetes).
- The audit log of all changes (users, permissions, content, settings, plugins) is under *Administration → Log* or `GET /api/v1/admin/audit`.

## Performance and scaling

- Permission checks need no database access (snapshot in memory, invalidated on changes).
- Sessions and API keys are kept in memory for 30 seconds. Sign-out and permission changes clear the cache at once, on all cluster instances too.
- Static files have content hashes in their names and are cached for one year. They can be delivered through a CDN.
- Scale horizontally: more instances in mode `cluster`, PostgreSQL with PgBouncer or read replicas.
- Public wikis/websites: switch on the guest cache (`cache.enabled`, see [configuration.md](configuration.md#cache)). Pages then come from memory for guests, without database access.
- E-mail in large amounts: several hundred notifications to thousands of recipients each per day are no problem. About 1,000 recipients per second were measured against a local SMTP server with 4 connections. In practice the mail server or provider limits this, so adjust `mail.concurrency` and possibly `mail.rate_per_minute` to its limits. Watch: `ewiki mail status` (field `oldest_pending`) or the warning under *Administration → E-mail*.
