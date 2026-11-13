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
| Further tests | separation of sites, second factor, allowed networks, open redirects (`SafeNext`), trusted proxies | `m18_test.go`, `proxy_test.go`, `internal/auth` |
| `scripts/test-docker-proxy.sh` | input to `docker-setup.sh` (malicious input too), proxy without root rights, read-only file system, certificates only for own host names | job *Docker images* |

New features come with a test of the permission check (who may, who may not – also with API keys and on other sites). New `#nosec` exceptions need a reason why there is no risk.

Please do not report vulnerabilities publicly but as described in `SECURITY.md` (GitHub: *Security → Report a vulnerability*).

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
OnlyOffice only with a valid JWT and downloads only from the configured server (SSRF). `pkg/wopi` tests discovery and
proof keys (key rotation, overflow, too short keys included), `internal/pagecache` that token URLs never end up in the
guest cache.

## Test routine against a running instance (`tests/qa`)

In addition to the checks above, a routine checks a running instance from outside: a functional test with 100 simultaneous users, a load test in stages up to 10,000 users and a penetration test with about 100 checks following OWASP WSTG, ASVS and API Security Top 10 (each with severity and CVSS vector), among them a sweep over all routes from `api.md` with the IDs of private objects.

```bash
tests/qa/local.sh        # fresh throwaway instance, all tests, report in /tmp/ewiki-qa/results/report.md
```

Run it before every version; new features get a step in the functional test there. The report describes open vulnerabilities and does not belong in the repository. Details: [tests/qa/README.md](../../../tests/qa/README.md).
