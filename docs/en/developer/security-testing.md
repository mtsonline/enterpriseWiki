# Security checks

Security first: a version is released only when all checks are green. They run in the CI on every push.

| Check | What it finds | Where |
|---|---|---|
| `govulncheck` | known vulnerabilities in Go and in all modules the code actually calls. If there is no fixed version yet, the vulnerability is caught in our own code and listed with an expiry date in `.govulncheck-allow` (after that the check fails again) | job *Security* (`scripts/govulncheck.sh`) |
| `gosec` | unsafe patterns (injection, paths, TLS, permissions, redirects …) and unchecked errors (all rules); every exception is in the code as `#nosec Gxxx -- reason` | job *Security* |
| `npm audit` | known vulnerabilities in all npm packages, build tools included | job *Security* |
| Trivy (image) | vulnerabilities (HIGH/CRITICAL with an available fix) and secrets in the Docker image | job *Docker images* |
| Trivy (configuration) | misconfigurations in the Dockerfile and Compose files | job *Docker images* |
| `TestSecurityBaseline` | attacks on the HTTP layer: headers/CSP, cookie flags, access rights and privilege escalation, injection, path traversal, stored XSS, brute force, X-Forwarded-For bypass, user enumeration, host header, CSRF, oversized requests | `internal/server/security_test.go` |
| `TestContentHostileParams` | every parameter of every block and every block option, with values that try to break out (markup, attributes, URLs, CSS, JSON); answers of the HTTP block and external feeds in every format and layout; the rendered page must not contain anything that runs, injects markup or loads foreign styles. First every block and option must take effect with valid values, so the hostile values do not only test an error message. In the browser the same for the CSS of the options (`blockoptions.test.ts`) | `internal/server/content_hostile_test.go` |
| Further tests | separation of sites, second factor, allowed networks, open redirects (`SafeNext`), trusted proxies | `m18_test.go`, `proxy_test.go`, `internal/auth` |
| `scripts/test-docker-proxy.sh` | input to `docker-setup.sh` (malicious input too), proxy without root rights, read-only file system, certificates only for own host names | job *Docker images* |

New features come with a test of the permission check (who may, who may not – also with API keys and on other sites). New `#nosec` exceptions need a reason why there is no risk.

Please do not report vulnerabilities publicly but as described in `SECURITY.md` (GitHub: *Security → Report a vulnerability*).

## Local check before a pull request (`scripts/precheck.sh`)

The script runs the checks of the CI on your own machine, in the order that finds mistakes fastest:

```bash
scripts/precheck.sh               # fast + go + e2e (what most changes need)
scripts/precheck.sh fast          # only the quick checks (seconds to a few minutes)
scripts/precheck.sh all           # also security and docker
scripts/precheck.sh a11y          # only the quick accessibility run (a few minutes)
```

| Part | What runs |
|---|---|
| `fast` | gofmt, generated plugin imports, `go vet`, plugin contract, licences, documentation, `tools/traps -fit`, npm release age, frontend build, typecheck, unit tests. Stops at the first mistake. |
| `go` | all Go tests with the race detector; the server tests in the three shards of the CI, each with its own database |
| `e2e` | build, a fresh wiki on its own database and port, all browser tests with axe, `tools/uicoverage` |
| `a11y` | build, a fresh wiki on the port after `EWIKI_PRECHECK_PORT`, the quick accessibility run (see below); not part of `all`, `e2e` checks the same and more |
| `security` | `govulncheck`, `gosec`, `npm audit` |
| `docker` | image build and the Docker tests (`test-docker-plugins.sh`, `test-docker-proxy.sh`); needs a running Docker |

The long parts run side by side (`EWIKI_PRECHECK_JOBS`, default: number of CPUs, at most 4), each with a log of its own; at the end the script shows the failed tests. It needs a PostgreSQL user that may create databases (`EWIKI_PRECHECK_PG`, default `postgres://wiki:wiki@localhost:5432`) and removes its databases again. The tests against real sign-in, office and search systems stay in the CI (jobs *Sign-in*, *Office*, *External search*); their start scripts in `tests/auth`, `tests/office` and `tests/search` also run locally with Docker.

A new push to a pull request stops the CI run of the push before it.

## Accessibility checks

Every view is checked for WCAG 2.2 AA in the browser tests. The helper `a11y()` in `web/tests/e2e.test.mjs` runs two checks from `web/tests/a11y-lib.mjs`:

- **axe-core** with the rules for WCAG 2.0 to 2.2, levels A and AA.
- **Overlap check** (our own): no control may be covered by another element. Every visible link, button, form field and focusable element in the window is hit-tested at its centre and at its four corners (2 px inside). Another element on top fails, whatever its size. Not counted: the parents and children of the control, its own labels (a styled checkbox under its label), elements that let clicks through (`pointer-events: none`), controls outside an open modal dialog, hidden controls (`.sr-only`). The report names the control, the covering element and how much is still visible (`< 28` marks less than 28 px).

Why the overlap check: axe measures how much of a covered control is still visible and passes from 24 px on. That depends on the text widths of the machine. In PR #225 the focused skip link covered the link to the start page: locally 31 px were visible (passed), in the CI 23.9 px (failed). The overlap check fails on both machines.

The browser tests also focus the skip link (Tab from the start of the page) and check that it covers nothing (`checkSkipLink`).

**Quick run** (a few minutes instead of the full browser tests): against a wiki set up with `ewiki setup`, it logs in and checks the key pages (start page as guest and signed in, sign-in, category, page, history, search, profile, dashboard, spaces, media, tasks, the administration with licence) at 390, 800 and 1280 px, in the light and the dark scheme, and with the skip link focused on start page and page. The other administration pages are checked once (1280 px, light); `EWIKI_A11Y_ALL_ADMIN=1` checks all of them like the key pages. It starts nothing itself and changes nothing except the colour scheme of the user, which it sets back.

```bash
cd web && EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_USER=admin EWIKI_E2E_PASSWORD=… npm run test:a11y
scripts/precheck.sh a11y          # the same with a fresh wiki of its own
```

Run it after every change to the layout, the header, the CSS or a theme, before the pull request.

**Images** must not move the page when they load (a click aimed below an image lands elsewhere once it has its size). `web/tests/unit/imgsize.test.ts` checks every image the frontend creates: width and height attributes, a class with a fixed height or `aspect-ratio`, a box with a fixed height (list `BOXED`), or an exception with a reason (list `EXCEPTIONS`).

## Caught vulnerabilities in dependencies

| Vulnerability | Affected | Caught in the code | checked until |
|---|---|---|---|
| GO-2026-6909 | `crewjam/saml` v0.5.1 (SAML sign-in): crash on assertions without Subject/Conditions; `NameID` and attributes are read without namespace, a foreign `<NameID>` in a signed assertion could determine the user | `plugins/auth_shib/guard.go`: pre-check of every (also encrypted) assertion before the library – exactly one Issuer, Subject, Conditions, SubjectConfirmationData; all SAML elements in the SAML namespace; a crash of the library becomes an error | 2026-12-31 |

## Office programs (M14)

`TestOfficeWOPI` and `TestOfficeOnlyOfficeAPI` (internal/server) check with a simulated office server:
proof keys are required (unsigned, foreign key, old timestamp), keys only for one file, session cookies
do not count at the WOPI endpoint, view keys can neither save nor lock, lock conflicts, revoked rights
and a disabled plugin invalidate keys at once, without download only protected, watermark originals only
for managers, API keys and the CLI without `--as` create no office keys, the CSP contains only active programs,
OnlyOffice only with a valid JWT and downloads only from the configured server (SSRF). `internal/wopi` tests discovery and
proof keys (key rotation, overflow, too short keys included), `internal/pagecache` that token URLs never end up in the
guest cache.

## Sign-in against real systems

In addition to the tests with replicated sign-in providers (`sso_test.go`), the CI (job "auth") runs tests against
real systems: Keycloak as OpenID Connect and SAML provider and OpenLDAP with `memberOf`. Both run as normal
processes without containers, locally just as in the CI.

```bash
eval "$(EWIKI_AUTH_AD=1 EWIKI_AUTH_SHIB=1 tests/auth/start.sh)"   # Keycloak, OpenLDAP, AD, Apache with mod_shib
EWIKI_TEST_DATABASE_URL=… go test -race -run TestReal ./internal/server/
(cd web && EWIKI_E2E_URL=http://localhost:8080 npm run test:auth)   # browser test against a freshly set-up wiki
tests/auth/start.sh stop

eval "$(tests/search/start.sh)"   # OpenSearch, Elasticsearch, Meilisearch (external search)
EWIKI_TEST_DATABASE_URL=… go test -race -run TestRealExternalSearch ./internal/server/
tests/search/start.sh stop   # stop the search engines
```

Checked among other things: sign-in through the real sign-in form, wrong password, mapping of groups and
roles, back-channel logout (ending the session at the provider ends it in the wiki), signing out in the wiki also ends
the session at the provider, a SAML response counts only once, SAML logout in both directions, LDAP filter injection,
groups through `memberOf` and through a group search.

With `EWIKI_AUTH_AD=1` Samba starts in addition as Active Directory (package `samba-ad-dc`, needs `sudo`): LDAPS with
an own test CA, disabled accounts, `objectGUID` as stable id, nested groups, StartTLS, no sign-in without a trusted
certificate or without encryption. With `EWIKI_AUTH_SHIB=1` Apache with mod_shib starts in front of the wiki (packages
`apache2`, `libapache2-mod-shib`), set up as in the admin documentation: the test signs in through Keycloak and tries
to forge the identity headers – directly past the proxy, through unprotected path variants and with a real
session. Every test creates its own realm in Keycloak; passwords are generated anew on every
start. Without the environment the tests are skipped, with `EWIKI_TEST_REQUIRE_REAL_AUTH=1` they fail.

## Test routine against a running instance (`tests/qa`)

In addition to the checks above, a routine checks a running instance from outside: a functional test with 100 simultaneous users, a load test in stages up to 10,000 users and a penetration test with about 100 checks following OWASP WSTG, ASVS and API Security Top 10 (each with severity and CVSS vector), among them a sweep over all routes from `api.md` with the IDs of private objects.

```bash
tests/qa/local.sh        # fresh throwaway instance, all tests, report in /tmp/ewiki-qa/results/report.md
```

Run it before every version; new features get a step in the functional test there. The report describes open vulnerabilities and does not belong in the repository. Details: [tests/qa/README.md](../../../tests/qa/README.md).

## Trap tests: do the tests check what they claim? (`tools/traps`)

A green test only means something if it would turn red as soon as what it checks is broken. `tools/traps` therefore builds deliberate bugs into security-critical places, one after the other, and runs the tests responsible. The list is in `tools/traps/traps.go`. Examples:
- a permission check is missing;
- "own permissions only" is bypassed;
- `--as` gets the rights of the command line;
- a copy loses its classification;
- a disabled account keeps working.

```bash
EWIKI_TEST_DATABASE_URL=postgres://USER:PASS@localhost:5432/DB?sslmode=disable go run ./tools/traps
go run ./tools/traps -list                       # all traps
go run ./tools/traps -only cli.as,auth.csrf      # single traps
go run ./tools/traps -fit                        # only: does every trap still fit the code (fast, runs in the CI)
```

- First the tests run without traps and must be green. Without a database the tool stops, because the integration tests would be skipped and every trap would count as "caught".
- **caught:** the tests turn red, as expected. **MISSED:** the tests stay green, so they do not check what they claim; that is a finding. **BROKEN:** the trap does not compile.
- **OUTDATED:** the guarded place has changed. Then the trap is adjusted, never removed. The CI checks this on every change (`-fit`), so an outdated trap shows at once.
- Every changed file is restored after its trap, also on Ctrl+C. Files with uncommitted changes are refused.
- Runs before every release. Every fixed security bug gets a trap.
