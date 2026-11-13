# Test routine: function, load, security

**English** · [Deutsch](README.de.md)

`tests/qa` checks a **running** instance from the outside, through the same HTTP API and the same WebSocket as the browser. It complements the Go tests (`go test ./...`) and the browser tests (`web/tests`): there every feature is checked on its own, here everything at the same time, under load and from the view of an attacker.

> **Never start it against a productive wiki.** The tests create and delete content and accounts, lock accounts, send very large requests and load the server until it no longer answers in time. Targets outside your own computer or private networks require `-force`.

> **Treat results as confidential.** `report.md` and `pentest.json` describe open vulnerabilities. They do not belong in the repository or in public tickets (see `SECURITY.md`).

## Quick start

```bash
tests/qa/local.sh              # everything, on a fresh throw-away instance (about 25 minutes)
tests/qa/local.sh functional   # only one part: functional | load | pentest
EWIKI_QA_PROXY=caddy tests/qa/local.sh   # the same behind the reverse proxy of the Docker setup
```

**Always run both variants** – without proxy (the wiki alone) and with `EWIKI_QA_PROXY=caddy` (TLS, HTTP/2, compression, forwarded addresses as in operation) – and choose a separate working directory for the second (`EWIKI_QA_WORK`). The proxy runs with the same handler as `docker/caddy/Caddyfile`; only two things differ that a test on one computer needs: certificates of its own certificate authority instead of ACME, and the proxy trusts the addresses of the virtual users from the test computer. `caddy` must be in the `PATH` (or `EWIKI_QA_CADDY=/path/caddy`; without a package: `go install github.com/caddyserver/caddy/v2/cmd/caddy@latest`).

`local.sh` creates its own PostgreSQL instance and database, builds the wiki from the checkout, starts it, runs the tests and cleans up again. Results: `$EWIKI_QA_WORK/results` (default `/tmp/ewiki-qa/results`): `report.md` and the raw data as JSON. Requirements: Go, Node.js (for the build), PostgreSQL (`initdb`), for the static checks `govulncheck` and `gosec`.

Against an existing test instance:

```bash
cd tests/qa && go build -o qa .
export EWIKI_QA_ADMIN_PASSWORD=…          # password of the administrator
./qa all -base http://127.0.0.1:8080 -spoof-ips -server-pid "$(pgrep -f 'ewiki serve')"
```

## What is checked

| Command | Content | Result |
|---|---|---|
| `seed` | QA area with a copy of the demo (all element types), private and public area, groups `qa-editors`/`qa-readers`, 100 accounts (4 of 5 may edit) | `world.json` |
| `functional` | All accounts sign in at the same moment and go through every feature: sign-in, navigation, all element types, search, pages (versions, translation, moving, trash), drafts, templates, files, media, blog, FAQ, events, processes, properties, comments, reactions, favourites, watching, API keys, live editing, permissions. Steps with a race start for everyone at the same time: saving the same page (exactly one wins), event with limited places, a case with many approvers | `functional.json` |
| `load` | Stages with an increasing number of simultaneous users (guests, readers, editors, live editors), response times per feature (p50/p95/p99), error rate, CPU and memory of the wiki; afterwards a spike test (N requests at the same moment) | `load.json` |
| `pentest` | About 100 checks according to OWASP WSTG v4.2, ASVS 4.0.3 and API Security Top 10 2023, each with severity and CVSS 3.1 vector: configuration, sessions, sign-in, access control (all routes from `docs/en/developer/api.md` as guest, reader and editor with the IDs of private objects), XSS, injection, uploads, SSRF, CSRF/CORS, WebSocket, business logic, resource consumption | `pentest.json` |
| `static` | `govulncheck`, `gosec`, `npm audit`, `go vet` (as in the CI) | `static.json` |
| `report` | report from all result files | `report.md` |
| `cleanup` | removes QA area, accounts and groups | |

`all` runs `functional`, `load`, `pentest`, `static` and `report`, each after a fresh `seed`. The penetration test comes last because it leaves locked accounts and large log entries behind.

## Important switches

| Switch | Meaning |
|---|---|
| `-users N` | number of QA accounts (default 100) |
| `-spoof-ips` | every virtual user gets its own address (`X-Forwarded-For`). The instance must trust the test computer (`server.trusted_proxies`), otherwise the limit of 30 sign-ins per address in 5 minutes applies |
| `-ca FILE` | certificate authority of the test instance behind a proxy with its own certificates (also `EWIKI_QA_CA`); the connections then use HTTP/2, live editing WebSocket over HTTP/1.1 |
| `-server-pid PID` | process of the wiki on the same computer: CPU and memory in the report, memory checks in the penetration test |
| `-stages 100,500,…` | simultaneous users per load stage |
| `-hold 40s`, `-think 5s` | measuring time per stage, average think time between two requests |
| `-collab 0.1`, `-guests 0.3` | share of live editors and guests |
| `-spike 10000` | requests at the same moment (0: off) |
| `-notify-ops` | include comments and case starts in the load mix (they notify many people; default off) |
| `-routes FILE` | route list for the access-control run (default `docs/en/developer/api.md`) |

## Rate limiting and the tests

The wiki limits requests per account and per address (expert settings, `core.rate.*`). The routine works **with** these limits, not around them:

- **Functional and penetration test** run with the defaults of the wiki. If it answers with `429`, the checks wait the stated time and repeat the request – like a well-behaved client. The checks of the limits themselves (`API4-RATELIMIT`, `ATHN-RATE`, `BUSL-ACCESSREQ`) do not wait.
- **Load test:** the virtual users share the QA accounts (10 000 users on 100 accounts: every account stands for 100 people). For the duration of the load test the routine therefore raises the limits **per account** by this factor and restores them afterwards (`PUT /api/v1/admin/limits`). The limits per address stay unchanged: every virtual guest has its own address. The report shows rejected requests (`429`) separately from errors.
- Whoever wants to switch the limits off completely (pure throughput test) sets `server.rate_limit.disabled: true` in the test instance.

## Target values of the load test

A stage passes if reading p95 ≤ 500 ms, writing p95 ≤ 1000 ms, live editing p95 ≤ 250 ms and the error rate ≤ 0.1 % (`targets` in `load.go`). These are assumptions of this routine; adapt them there for a project with its own requirements.

For reliable capacity figures start the load generator on a **second computer** (`-base http://testserver:8080 -force`): on one computer, wiki, database and generator share the CPU.

## Adding new features

- **Functional test:** add a step in `journey()` or `editor()` (`functional.go`), with `v.want(area, name, response, expected status…)`.
- **Access control:** new routes are automatically in `docs/en/developer/api.md` and therefore run in the pass. For new kinds of private objects add an entry in `seedPrivate()` (`pentest.go`) with a marker text and add the ID to `priv.IDs`.
- **Security check:** write a check with `check{id, area, title, refs, sev, vector, fix}` and `p.verdict(…)`. Severity and CVSS vector apply to the case that it fails.
- **Load:** add the feature to the mix of `user()` (`load.go`); start names with `read:` or `write:`.

## Limits

- The proxy variant checks the interaction with the reverse proxy (addresses, compression, WebSocket, HTTP/2), not its certificate retrieval (ACME) and not HTTP/3. With HTTP/2 the load generator bundles many users onto few connections; real browsers open one per user.
- An HTTP test does not see script in the browser (DOM-based XSS): browser tests and a browser scanner (e.g. OWASP ZAP) complement this.
- The Go tests check sign-in through real identity providers, SCIM clients and office servers with replicated servers.
- An automatic test does not replace a penetration test by an independent body, as certifications require.
