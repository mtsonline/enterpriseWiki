#!/usr/bin/env bash
# CI test of the access setup and the bundled reverse proxy:
#   1. docker-setup.sh: HTTPS questions (domain, certificate authority,
#      e-mail) write .env; invalid and malicious input is rejected
#   2. docker-setup.sh: plain HTTP asks for the IP (default 127.0.0.1)
#   3. the proxy runs unprivileged with a read-only file system, redirects
#      HTTP to HTTPS and forwards to the wiki; certificates are only allowed
#      for the wiki's own host names
#   scripts/test-docker-proxy.sh [IMAGE]   (default: builds the lite image)
set -euo pipefail
src="$(cd "$(dirname "$0")/.." && pwd)"
image="${1:-}"
fail() {
	echo "FAIL: $*" >&2
	grep -v -i 'password\|secret\|token' .env >&2 2>/dev/null || true # never print secrets into (public) CI logs
	docker compose logs --no-color --tail 100 >&2 2>/dev/null || true
	exit 1
}
prepare() { # name
	work="$(mktemp -d)/$1"; mkdir -p "$work"
	cp -r "$src"/{docker-compose.yml,.env.example,docker-setup.sh,docker} "$work/"
	cd "$work"
	export COMPOSE_PROJECT_NAME="ewproxy$1"
}
# runs docker-setup.sh in a pseudo terminal with the given answers (one per line)
interactive() { printf '%s\n' "$@" | script -qec ./docker-setup.sh /dev/null >setup.log 2>&1 || { cat setup.log >&2; fail "setup failed"; }; }
val() { sed -n "s/^$1=//p" .env | tail -n 1; }
expect() { [ "$(val "$1")" = "$2" ] || fail "$1 is '$(val "$1")', want '$2'"; }

echo "== setup: HTTPS via the bundled proxy (ZeroSSL), invalid input rejected"
prepare https
# image: lite; HTTPS: yes; domains: two invalid (injection attempts), then valid;
# CA: invalid, then 2 (ZeroSSL); e-mail: invalid, then valid
# office: invalid choice, then 3 (OnlyOffice); host: the wiki's own domain and an
# injection attempt are rejected, then the default (docs.example.com)
interactive l y 'evil.com|x' 'a.com;rm -rf /' Wiki.Example.COM 9 2 'x@y' 'it@example.com' 7 3 wiki.example.com 'x.com$(id)' ''
expect EWIKI_HTTPS proxy
expect COMPOSE_PROFILES proxy,onlyoffice
expect EWIKI_DOMAIN wiki.example.com
expect EWIKI_ONLYOFFICE_HOST docs.example.com
expect EWIKI_ONLYOFFICE_URL https://docs.example.com
expect EWIKI_MEDIA_VIEWER office.onlyoffice
[ "$(val EWIKI_ONLYOFFICE_JWT_SECRET | wc -c)" -ge 40 ] || fail "no JWT secret generated"
grep -q 'id)' .env && fail "malicious office host reached .env"
expect EWIKI_BASE_URL https://wiki.example.com
expect EWIKI_ACME zerossl
expect EWIKI_ACME_EMAIL it@example.com
expect EWIKI_PORT 127.0.0.1:8080
expect EWIKI_TRUSTED_PROXIES 172.30.80.250
grep -q 'rm -rf' .env && fail "malicious input reached .env"
[ "$(stat -c %a .env)" = 600 ] || fail ".env is not private"

echo "== setup: HTTPS with Collabora"
prepare collabora
interactive l y wiki.example.org 1 it@example.org 2 ''
expect COMPOSE_PROFILES proxy,collabora
expect EWIKI_COLLABORA_HOST office.example.org
expect EWIKI_COLLABORA_URL http://collabora:9980
expect EWIKI_MEDIA_VIEWER office.collabora

echo "== setup: plain HTTP, default 127.0.0.1, then an explicit IP"
prepare http
interactive l n '' n
expect EWIKI_HTTPS off
expect EWIKI_PORT 127.0.0.1:8080
expect EWIKI_BASE_URL http://localhost:8080
prepare http2
interactive l n '300.1.1.1' '10.0.0.5' 'http://wiki.intra:8080' y
expect EWIKI_PORT 10.0.0.5:8080
expect EWIKI_BASE_URL http://wiki.intra:8080
expect EWIKI_TRUSTED_PROXIES 172.30.80.1
echo "== setup: non-interactive defaults to HTTP on 127.0.0.1"
prepare batch
./docker-setup.sh </dev/null >/dev/null
expect EWIKI_HTTPS off
expect EWIKI_PORT 127.0.0.1:8080

[ -z "${SETUP_ONLY:-}" ] || { echo "OK (setup only)"; exit 0; }
echo "== proxy: unprivileged, read-only, HTTP -> HTTPS, forwards to the wiki"
if [ -z "$image" ]; then
	docker build -q --target lite -t ewiki-test:lite "$src" >/dev/null
	image=ewiki-test:lite
fi
prepare run
./docker-setup.sh </dev/null >/dev/null
sed -i -E "s#^(\s+image:) ghcr.io/mtsonline/enterprisewiki:.*#\1 $image#; s#pull_policy: always#pull_policy: never#" docker-compose.yml
cat >> .env <<'EOF'
COMPOSE_PROFILES=proxy
EWIKI_ACME=letsencrypt-staging
EWIKI_ACME_EMAIL=ci@example.com
EWIKI_HTTP_PORT=18081
EWIKI_HTTPS_PORT=18443
EWIKI_SUBNET=172.30.90.0/24
EWIKI_IP_RANGE=172.30.90.0/25
EWIKI_PROXY_IP=172.30.90.250
EWIKI_TRUSTED_PROXIES=172.30.90.250
EOF
sed -i 's#^EWIKI_BASE_URL=.*#EWIKI_BASE_URL=https://wiki.example.com#; s#^EWIKI_PORT=.*#EWIKI_PORT=127.0.0.1:18080#' .env
docker compose up -d >up.log 2>&1 || { cat up.log >&2; fail "compose up"; }
trap 'docker compose down -v >/dev/null 2>&1 || true' EXIT
for _ in $(seq 1 120); do curl -fs http://127.0.0.1:18080/readyz >/dev/null && break; sleep 1; done
curl -fs http://127.0.0.1:18080/readyz >/dev/null || fail "wiki not ready"
pid="$(docker compose ps -q proxy)"
[ -n "$pid" ] && [ "$(docker inspect -f '{{.State.Running}}' "$pid")" = true ] || fail "proxy not running"
[ "$(docker inspect -f '{{.Config.User}}' "$pid")" = "$(id -u):$(id -g)" ] || fail "proxy runs as $(docker inspect -f '{{.Config.User}}' "$pid")"
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' "$pid")" = true ] || fail "proxy root file system writable"
grep -q ALL <<<"$(docker inspect -f '{{.HostConfig.CapDrop}}' "$pid")" || fail "capabilities not dropped"
loc="$(curl -s -o /dev/null -w '%{redirect_url}' -H 'Host: wiki.example.com' http://127.0.0.1:18081/some/page)"
[ "$loc" = https://wiki.example.com/some/page ] || fail "HTTP redirect: '$loc'"
# certificates only for the wiki's own host names (asked by Caddy)
code() { docker compose exec -T proxy wget -q -S -O /dev/null "http://wiki:8080/tls-allowed?domain=$1" 2>&1 | grep -o 'HTTP/[0-9.]* [0-9][0-9][0-9]' | head -n 1 | awk '{print $2}'; }
[ "$(code wiki.example.com)" = 200 ] || fail "own host not allowed"
[ "$(code evil.example.org)" = 404 ] || fail "foreign host allowed"
# the wiki is not reachable from outside the host (127.0.0.1 only)
grep -q '^127.0.0.1:' <<<"$(docker compose port wiki 8080)" || fail "wiki port not bound to 127.0.0.1"
echo "OK"
