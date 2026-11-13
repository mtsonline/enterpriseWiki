# Rules for working on Enterprise Wiki

> Translation of [CLAUDE.md](CLAUDE.md). If the two differ, the German version applies.

This file applies to everyone working on the repository: humans and AI agents. `AGENTS.md` points here; there is only this one source. Changes to this file need the approval of the project owner.

Enterprise Wiki is a secure and flexible alternative to traditional enterprise wikis and CMS systems: a single Go program (`ewiki`), PostgreSQL, frontend in TypeScript. Overview: `README.md`, structure: `docs/en/developer/architecture.md`.

## 1. Principles

In every conflict this order applies: security, no data loss, stability, accessibility, speed, comfort.

- **Security first. Vulnerabilities are taboo.** Every input is hostile until it has been checked. Permissions are checked in the server, never only in the interface. Viewing as another person (`--as`) never shows more than the permissions of the signed-in user allow.
- **No data loss.** Nothing someone has entered gets lost, not even on overload, abort or crash.
- **Consequences in the same transaction.** What follows from a change (notification, e-mail, queue entry) is stored with it in the same transaction (`Notify.SendQ`, `Mail.Enqueue`, `Content.OnChangeDurable`, `Content.TouchTx`). Follow-up work must tolerate repetition.
- **Every feature on all paths:** API, CLI and interface. **All** CLI commands go through the API path (`serveInProcess`), so permissions, checks and audit are the same; existing direct commands are converted. Exceptions only with documented approval of the project owner.
- **Accessibility** according to the current WCAG version, level AA (as of 13 Nov 2026: WCAG 2.2), in every view; checked with axe in the browser tests.
- **Fast:** no queries in loops, limits for everything that can grow. Limits belong in the expert settings (`limits`); plugins register theirs in the manifest.
- **Modular:** features outside the core are plugins. Bundled plugins may use internal packages; the stable interface for plugins is `pkg/sdk`. The model for external plugins is still to be decided.
- **Honest:** what the interface shows or the documentation describes exists and works as one expects. No simulations, no dummies, no announcements presented as features.
- **Foreign product names** only appear in import functions for the respective system and their tests, nowhere else.
- **Namespaces:** everything a plugin registers (routes, permissions, settings, events, actions) carries its plugin ID; instances are called `plugin-id/instance-id`; `core.` is reserved for the core.
- **Secrets:** default `plain`. Encryption is always selectable, never mandatory.
- **Configuration:** without a cluster in `config.yaml`. Only in cluster mode are the settings that can be changed in the interface stored in the database.

## 2. Dependencies and licenses

- Always the newest stable version. Renovate updates; the CI fails if an update is open for longer than 7 days.
- Everything delivered is under a permissive license: MIT, BSD, Apache-2.0, ISC, Zlib, Unlicense, CC0. This also applies to fonts, images and icons; fonts are under OFL-1.1 or one of the licenses named. Fonts are delivered by the wiki itself, never loaded from other servers. For OFL fonts with a reserved name, check before subsetting or converting whether they must be renamed. Tools that only run when building or testing and are not delivered are exempt. `go run ./tools/notices -check` checks the Go modules.
- New libraries only with a reason; check the state of maintenance beforehand. Prefer the Go standard library.

## 3. Working in the repository

- `main` is protected and always runs. Work happens on a branch; every change comes through a pull request.
- **Merging only with a green CI and after approval by the project owner.** Only exception: dependency updates by Renovate are merged automatically when all required checks are green.
- Exactly one branch and one session per task. Fetch `main` before starting. Do not bring forward anything from later steps that have not been approved yet.
- Report contradictions between code, documentation and plans immediately instead of deciding silently. Ideas for improvement likewise.
- Contributions by AI agents are marked openly (co-author line in the commit, note in the pull request).
- Commit messages in English, short, in the imperative or as a summary of the feature.
- Pull requests use the template (`.github/pull_request_template.md`) including the CLA.

## 4. A feature is only done with

1. API, CLI and interface.
2. Tests: Go tests (unit and API integration), browser test for the flow in the interface.
3. Translations German and English (`TestLocaleParity` checks that none is missing).
4. Documentation in German and English: API reference for routes, user guide, admin documentation for operations and security, roadmap.
5. Demo content where the feature shows something.
6. Every route carries an action ID following the namespace rule as a comment (preparation for the action registry); existing routes are brought up to date.

### Database

- **Until the first release:** new columns and tables directly in the `0001` files (`internal/db/migrations/…/0001_*.sql`); no migrations for old states.
- **Build tables so that as few changes as possible are needed later:** fixed columns for what needs indexes or check constraints; additional data (e.g. from plugins) as JSON. Plugins have their own tables and their own migrations.
- **From the first release:** necessary migrations run automatically on start, forward only, with a backup beforehand. The CI tests the upgrade from every released version. What is replaced is deleted at the earliest in the following version, because in a cluster old and new nodes run at the same time for a short while. No going back to older versions; that is what the backup is for.

## 5. Tests

- **Every feature is tested in real operation.** Replicated counterparts for other systems only exist in tests, and in addition to runs against real test systems. Tests check whole flows, for example: a workflow is started, decided and completed; sign-in also works with a passkey. These are examples, not an exhaustive list.
- Tests run with the newest stable versions of Go, Node.js and PostgreSQL, locally as in the CI.
- Tests are independent and deterministic: no fixed waiting times, no reliance on order or time of day where it can be avoided. The CI runs with the race detector.
- Every API route is used by the interface or a documented script (`checkRouteCoverage` in `internal/server/integration_test.go`). Exceptions there only for calls from outside (sign-in providers, SCIM, office servers).
- A fixed bug gets a test that would find it again.

## 6. Commands

```bash
./scripts/build.sh                      # build frontend and program (bin/ewiki)
gofmt -l .                              # must be empty
go vet ./...
go run ./tools/genplugins               # after new or removed plugins; commit the result
go run ./tools/notices -check           # licenses of the Go modules
go run ./tools/docscheck                # docs complete in German and English, links valid

# Go tests; with a database also the API integration tests
EWIKI_TEST_DATABASE_URL=postgres://USER:PASS@localhost:5432/DB?sslmode=disable go test -race ./...

# Security
scripts/govulncheck.sh
go run github.com/securego/gosec/v2/cmd/gosec@latest -quiet -exclude-generated -exclude-dir=web -conf .gosec.json ./...
(cd web && npm audit --audit-level=low)

# Frontend
cd web && npm run typecheck && npm run test:unit
EWIKI_E2E_URL=http://localhost:8080 EWIKI_E2E_REQUIRE_AXE=1 npm run test:e2e   # against a freshly set-up wiki

# Complete check from outside (function, load, pentest), always both variants
tests/qa/local.sh
EWIKI_QA_PROXY=caddy EWIKI_QA_WORK=/tmp/ewiki-qa-caddy tests/qa/local.sh
```

The browser tests build on each other and only run completely. Before a release, the test routine `tests/qa` and all security checks run in addition.

## 7. Clean, without warnings

Build, `go vet`, gosec, type check and tests run without warnings. An exception only stands with a reason directly at the place (`// #nosec G… -- reason`). An overview of all exceptions is generated automatically; the CI checks that it is up to date (introduced with phase 1).

## 8. What never belongs in the repository

- Reports or `pentest.json` with open vulnerabilities (see `SECURITY.md`).
- The tool for generating license keys and its key.
- Personal data: real e-mail addresses, passwords, tokens, keys. Examples use `example.com`.
- Environment-specific things: local paths, private start scripts.

Docker images are only published privately for now.

## 9. Language

- Interface, documentation and guides always exist **in German and English**, complete and equivalent. German with "Sie". Short, clear sentences, plain language. Completeness is checked automatically (`TestLocaleParity`, `go run ./tools/docscheck`).
- Storage: `docs/de/…` and `docs/en/…` with the same file names; in the root directory `X.md` is English and `X.de.md` German. Exception: `CLAUDE.md` is German, the translation is called `CLAUDE.en.md`. For legal texts (license, CLA) the German version is binding.
- Code, identifiers, comments, commit messages: English.

## 10. Planning

The project owner keeps the decisions and plans of the project outside the repository. Whoever works on behalf of the project owner reads the current decisions and the plan of the respective task before starting and records progress and new decisions there. This file summarises the permanently valid rules from them.
