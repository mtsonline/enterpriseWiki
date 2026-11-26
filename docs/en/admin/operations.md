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

With a self-built image (own plugins): `git pull && docker compose -f docker-compose.build.yml up -d --build`.

### Database of an older development version

Until the first release, new tables and columns go straight into the first schema files; there are no migrations for older development states. A database from such an older state lacks them. The wiki then refuses to start, with a log line like "the database was created by an older development version of the wiki and lacks: table …, column …". This avoids running on an incomplete schema, where some actions would fail with an error 500 (for example creating the demo or resetting the site).

The wiki compares the schema only when the schema files have changed since the last start. The comparison runs in a temporary schema and leaves nothing behind.

What to do: create a new, empty database. Back up anything you still need first (*Backup and restore*). With Docker:

```bash
docker compose down
mv data/postgres data/postgres.old      # keep it until the new start works
docker compose up -d                    # creates the database and the administrator (EWIKI_ADMIN_PASSWORD in .env) again
```

With systemd: create the database anew (`dropdb`/`createdb` as in the installation) and run `ewiki setup`.

### Notice of new versions in the overview

The start page of the administration (*Administration → Overview*, `/admin`) shows whether a new version of the core has been released, with its changes. Security updates are highlighted: their sections `## Security` or `## Sicherheit` from the release notes of all versions since the installed one come first (rule for release notes: [CONTRIBUTING.md](../../../CONTRIBUTING.md)).

There is deliberately no button to install it: the wiki would need access to Docker, that is root on the server. Instead the overview shows the matching command:

| Installation | Command | Recognised by |
|---|---|---|
| Docker with the published image | `docker compose pull` and `docker compose up -d` | `EWIKI_IMAGE_ORIGIN=published`, set only in the published image |
| Docker with a self-built image | `git pull && docker compose -f docker-compose.build.yml up -d --build` | `EWIKI_IMAGE_ORIGIN=self-built`, default of the `Dockerfile` |
| systemd | `sudo <directory>/scripts/update.sh` | `EWIKI_INSTALL=systemd`, set by `scripts/install.sh` |

Without the marker (for example an older image or an older systemd installation) the overview shows all commands with their labels. The update timer: with systemd, if switched on, it installs on Sundays between 03:30 and 04:30; the wiki cannot see whether it is switched on. Under Docker the overview shows the next run of the update helper (below) when it has reported recently, otherwise the commands. `ewiki status` (`GET /api/v1/admin/status`) gives the same information.

### Docker: automatic updates with the update helper

The wiki never gets access to Docker (that would be root on the server). Instead a helper on the server installs new releases: `scripts/update-docker.sh`. `./docker-setup.sh` asks whether to set it up (default yes, daily at 03:30).

```bash
scripts/update-docker.sh --enable-timer 03:30   # daily; systemd with root/sudo, else crontab
scripts/update-docker.sh --enable-timer 03:30 --cron   # always the crontab of the user
scripts/update-docker.sh --disable-timer
scripts/update-docker.sh --status               # timer, last run, what the wiki reported
scripts/update-docker.sh --dry-run              # what a run would do, without changes
scripts/update-docker.sh                        # one run now
```

How it works:

1. Once a day the wiki checks for updates (job `updates.check`, switched off with `plugins.updates.check: false`). If a newer **published release** of the core exists, it writes `./data/wiki/update-available` (version, link, security update yes/no); otherwise it removes the file. Pre-releases are never reported.
2. The helper runs daily as the user who runs the wiki (`EWIKI_UID`). Without `update-available` it does nothing. It treats the file as untrusted: it only takes a version of the form `1.2.3` and executes nothing from it.
3. With a self-built image (`docker-compose.build.yml`) it changes nothing and only reports it: update it with `git pull && docker compose -f docker-compose.build.yml up -d --build`.
4. It backs up the database first (`pg_dump` into `./data/backups/before-update-*.dump`, the newest 10 are kept). If the backup fails, nothing is updated.
5. Then `docker compose pull wiki` and `docker compose up -d`; it waits until the wiki is ready and compares the version. A fixed version in `EWIKI_IMAGE_TAG` (for example `7.5`) can keep the new release out; the helper reports that.
6. It writes `./data/wiki/update-timer.json` (next run, last run and its result) and logs to `./data/update-docker.log`. The overview of the administration shows the next run from it; the wiki checks the file strictly and ignores it when it is old.

Runs never overlap, and a run installs at most one update.

**Why systemd or cron:** `./docker-setup.sh` usually runs as the normal user. A systemd timer (like `update.sh --enable-timer`) needs root to install; with root rights or `sudo` the helper sets one up (`enterprisewiki-docker-update.timer`, a random delay of up to one hour, runs missed while the server was off are made up). The service runs as the user of the wiki, not as root. Without root rights the helper uses the crontab of the user instead: it needs no rights beyond the access to Docker, which the user has anyway. Cron has no random delay, so a random minute within the hour after the chosen time is fixed once.

## Automatic updates of the components

Enterprise Wiki stays on the newest state of all components (Go, Go modules, Node.js,
npm packages, Docker base images, GitHub Actions) without manual intervention:

1. **Renovate** (`renovate.json`) checks daily before 6 a.m. and opens pull requests: smaller updates collected in one,
   big version jumps one by one. New versions are only taken after **3 days** (withdrawn or
   compromised packages do not reach the product this way), **security updates at once**. Versions and Docker images
   are pinned exactly (images and GitHub Actions with their digest); every update changes them by pull request.
   For the weekly refresh of the lock file Renovate cannot check the age; a CI step
   (`go run ./tools/releaseage`) does that and refuses every package version younger than 3 days.
2. The **CI** checks every pull request completely: build, Go tests with PostgreSQL, browser tests with
   accessibility checks, Docker build and license check of the dependencies. Only if everything is green does
   GitHub merge the pull request **automatically** (auto-merge, turned on by Renovate; the repository setting
   "Allow auto-merge" must be on) – also for big version jumps. Red pull requests stay
   open; the check *Renovate watch* (item 6) reports them. The overview of all open updates is in the issue "Dependency Dashboard".
3. After every change on `main`, **docker-publish** rebuilds the image `ghcr.io/mtsonline/enterprisewiki:latest` (amd64,
   arm64); version tags `v7.5.0` additionally give `:7.5.0`, `:7.5` and `:7`.
4. The nightly **canary** run builds and tests with the very newest versions (without waiting time, without taking
   anything over) as an early warning.
5. The daily check **Outdated components** (`.github/workflows/outdated.yml`, locally `go run ./tools/outdated`)
   compares the Go toolchain, direct Go modules, npm packages and the Node.js LTS with the newest stable
   version of each. If an update is open for longer than 7 days (e.g. a Renovate PR with red CI), it fails – so
   nothing becomes outdated unnoticed.
6. The daily check **Renovate watch** (`.github/workflows/renovate-watch.yml`) opens an issue
   "Renovate: stuck pull requests" (label `renovate-stuck`) as soon as a Renovate pull request is stuck: red CI for
   another reason than the 3-day rule for new npm versions, or green CI but open for more than 2 days. The issue is
   updated daily and closed when nothing is stuck any more. Unlike a red check in a bot's pull request, an issue
   reaches everybody who watches the repository.

**PostgreSQL major versions:** Renovate raises the *recommended* version (`docker-compose*.yml`, `.env.example`,
notice in the administration), the CI tests it and the upgrade from the older versions. Existing installations
only switch with `./docker-postgres-upgrade.sh` (next section). The `go` line in `go.mod` stays the
minimum version for building; the Go version from the `toolchain` line is always used.

## PostgreSQL upgrades (Docker)

Docker Compose uses the **official** image `postgres:<version>`. The version is in `.env`
(`POSTGRES_VERSION`), the data of every major version is in its own folder: `./data/postgres/19/docker`,
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

The CLI is the same binary. It runs with superadmin rights; whoever can run it as the service user has full control. With `--config` or `EWIKI_CONFIG` you choose the installation.

**All commands go through the API** (in the same process, through the normal server handler): the same checks, limits and audit entries apply as in the interface; the audit log shows them with "cli". With `--as USER` exactly that user's rights apply. Exceptions, approved by the project owner because they work with files instead of the wiki:

- `serve`, `setup`, `migrate` (start or set up the wiki);
- `config show|path|validate|get|set` (edits `config.yaml` directly and also works when the database cannot be reached). `config set` writes an entry to the audit log (`settings.config_set`, only the key) when the database can be reached. When a running wiki picks up a file changed from outside, it logs the changed keys (`settings.file_changed`), never the values;
- `plugin generate|build` (source tree and building the program);
- `signature verify FILE --offline` (checks an exported file without the wiki).

```text
ewiki serve | setup [--non-interactive] | migrate | version
ewiki api METHOD PATH [BODY|@file.json|-] [--as USER]
ewiki user list|create|passwd|disable|enable|unlock ...
ewiki directory sync|runs|adopt PROVIDER ...   # LDAP/AD directory sync, see authentication.md
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

### Commands from the action register

Every action of the action register (see [API: action register](../developer/api.md#action-register)) has its own command, also where no hand-written one exists. Like all commands it goes through the API, with the same permissions, checks and audit entries.

```bash
ewiki help                                  # all commands, those from the register at the end
ewiki smime show --help                     # usage, route and permission of a command
ewiki smime show 7 --as anna                # path parameter as an argument, with the permissions of anna
ewiki auth password-reset request --email anna@example.com
ewiki ldap test --json @test.json           # body as JSON (file, inline or - for stdin)
ewiki file upload --file file=@notes.txt    # upload (multipart): --form NAME=VALUE, --file FIELD=@FILE
```

- Path parameters are arguments or options (`--id 7`), fields of query and body are options (`--email …`) where the register describes them; otherwise `--json` or `--query NAME=VALUE`.
- Uploads: `--form NAME=VALUE` for text fields (in the given order) and `--file FIELD=@FILE` for files; not together with `--json`.
- Pass passwords and other secrets with `--json -` on standard input rather than as an option (they would end up in the shell history).
- Hand-written commands (`ewiki smime list` etc.) take precedence and stay as they are.

## Demo content and reset

Under *Administration → Demo* (administrators only) **Create demo** creates a space "Demo: Enterprise Wiki"
(key `DEMO`, short URL `/demo`) with two ways into the wiki:

- **Example company** ("Enterprise IT Services", a fictional IT service company): an intranet the way a company uses it – start
  page with quick links, news (blog with a comment, @mention and reaction), events with registration and a series,
  teams (team page, decision log, tasks in the text, calendar of the teams, shared notes), handbook (display
  *Handbook*), knowledge base (FAQ with instant search), forms and workflows (holiday request decided by the team
  lead and entered in the calendar, internal job application with a CV, pre-check by HR, decision of the team
  lead and the answer as a pop-up in the wiki, device request, onboarding, idea box with public results, contact form, further
  workflows with triggers), projects (properties, project database, reports), documents (classified confidential,
  PDF view-only with watermark, Word file for the office program, a page with rights of its own) and categories as
  cards, a table and "all on one page". A short hint "How this works" explains each feature where it is used and
  links to the matching station of the tour. The demo accounts of the team leads decide requests; so that you can
  play the holiday request and the job application through yourself, the administrator account that creates the
  demo belongs to the groups "Demo: Verwaltung" and "Demo: Teamleitung" as long as the demo exists.
- **Tour of the features**: one station per feature, each a page that is always built the same way – what it is,
  *How it is done* (the steps), *What it looks like* (the result live on the page), *What it is good for* and, if
  needed, *Good to know*. The category has the display *Handbook*: all stations are in the side column, and
  *Previous* and *Next* at the end of every page lead to the station before and after. The station *Menus,
  placement and carousel* says for every part outside the pages (menu, block "Reception", light and dark, carousel,
  handbook) what it shows, where it is placed and where to change it.

What a disabled plugin needs is left out (also its station); the message after creating names the parts left out
(in the API the field `skipped` of the answer to `POST /api/v1/admin/demo`). A test checks the minimum rules of the
demo: every page in German and English, no block errors, no dead links, only addresses of the demo company (`enterpriseit.local`, never valid on the internet), no English text in the German version,
every station with the same structure and no gap in the order; the browser tests check every page with axe, light
and dark.

- **Start page:** The demo makes its page *Enterprise Wiki at a glance* the start page of the site (short URL
  `/welcome`, in a wiki with German as default language `/willkommen`, template "CMS modern"): what makes visitors
  curious, uses (intranet, handbook, knowledge base, forms and workflows, university, website) as ways into the
  matching place of the demo, the highlights and the two ways into the demo – without path, page details and
  comments. **Remove demo** restores the previous start page, unless it was changed meanwhile under
  *Administration → Settings*.
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

## Maintenance mode

For work on the server – for example moving it – administrators set the wiki to **read-only** or **closed** under *Administration → Maintenance* (or `ewiki maintenance set`, see [configuration.md](configuration.md#maintenance)). Read-only: everybody reads, only administrators change, a bar on every page says so with your message and the expected end. Closed: others see only that message and the way to sign in. Background jobs wait by default, so a copy of the database taken now is complete and sends no e-mail twice after the move. The mode does not end by itself: switch it off when done.

Moving the server: switch on read-only, copy database and data directory, start the new server (still read-only, the setting moves with the configuration), check it, point the address to it, switch maintenance off.

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
| `data.rebuild` | 1 min | build the data for reports (for example decisions) of kinds that are new or have a new version, from the latest versions of the pages, 200 pages per step; *Administration → Data for reports* shows the state and starts it again |
| `workflow.process.timers` | 30 s | continue waiting times and webhooks of workflows, reminders for overdue tasks |
| `search.external.sync` | 10 s | synchronise the external search index (only when configured) |
| `updates.check` | daily (and 3 s after the start) | check for updates of the core and the plugins (`plugins.updates.check`); in Docker the notice for the update helper |

In cluster mode each job only runs on one instance at a time (advisory lock).

## Logs and monitoring

- Logs go to stderr, so with systemd into the journal (`journalctl -u enterprisewiki -f`). JSON format with `EWIKI_LOG_FORMAT=json`, more details with `EWIKI_LOG_LEVEL=debug`.
- `GET /healthz` reports that the process runs. `GET /readyz` also checks the database (for load balancers and Kubernetes).
- The audit log of all changes (users, permissions, content, settings, plugins) is under *Administration → Log* or `GET /api/v1/admin/audit/search` (`ewiki audit search`).

## Performance and scaling

- Permission checks need no database access (snapshot in memory, invalidated on changes).
- Sessions and API keys are kept in memory for 30 seconds. Sign-out and permission changes clear the cache at once, on all cluster instances too.
- Static files have content hashes in their names and are cached for one year. They can be delivered through a CDN.
- Scale horizontally: more instances in mode `cluster`, PostgreSQL with PgBouncer or read replicas.
- Public wikis/websites: switch on the guest cache (`cache.enabled`, see [configuration.md](configuration.md#cache)). Pages then come from memory for guests, without database access.
- E-mail in large amounts: several hundred notifications to thousands of recipients each per day are no problem. About 1,000 recipients per second were measured against a local SMTP server with 4 connections. In practice the mail server or provider limits this, so adjust `mail.concurrency` and possibly `mail.rate_per_minute` to its limits. Watch: `ewiki mail status` (field `oldest_pending`) or the warning under *Administration → E-mail*.
