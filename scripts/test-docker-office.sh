#!/usr/bin/env bash
# CI test against real office servers (compose profiles collabora and
# onlyoffice): a browser opens a document in Collabora Online and in
# OnlyOffice Docs, the office servers fetch it from the wiki (WOPI with proof
# keys / OnlyOffice API with JWT) and save a new version back.
#
#   scripts/test-docker-office.sh [IMAGE]   (default: builds the lite image)
#
# Host names: wiki.test, office.test (Collabora) and docs.test (OnlyOffice)
# all point to this computer; the containers reach each other through the
# published ports (host-gateway), like through the proxy in production.
set -euo pipefail
src="$(cd "$(dirname "$0")/.." && pwd)"
image="${1:-}"
export COMPOSE_PROJECT_NAME=ewoffice
work="$(mktemp -d)/office"
mkdir -p "$work"
cp -r "$src"/{docker-compose.yml,.env.example,docker-setup.sh,docker} "$work/"
cd "$work"
fail() {
	echo "FAIL: $*" >&2
	for s in wiki collabora onlyoffice; do
		echo "---- $s" >&2
		docker compose logs --no-color --tail 80 "$s" 2>&1 | grep -v -i 'password\|secret' >&2 || true
	done
	exit 1
}
trap 'docker compose down -v >/dev/null 2>&1 || true' EXIT

if [ -z "$image" ]; then
	docker build -q --target lite -t ewiki-test:lite "$src" >/dev/null
	image=ewiki-test:lite
fi
./docker-setup.sh </dev/null >/dev/null
sed -i -E "s#^(\s+image:) ghcr.io/mtsonline/enterprisewiki:.*#\1 $image#; s#pull_policy: always#pull_policy: never#" docker-compose.yml
secret="$(LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 40)"
cat >>.env <<EOF
COMPOSE_PROFILES=collabora,onlyoffice
EWIKI_PORT=0.0.0.0:8080
EWIKI_BASE_URL=http://wiki.test:8080
EWIKI_DOMAIN=wiki.test
EWIKI_COLLABORA_HOST=office.test:9980
EWIKI_COLLABORA_URL=http://office.test:9980
EWIKI_COLLABORA_TLS_TERMINATION=false
EWIKI_ONLYOFFICE_HOST=docs.test
EWIKI_ONLYOFFICE_URL=http://docs.test:8090
EWIKI_ONLYOFFICE_JWT_SECRET=$secret
EWIKI_SUBNET=172.30.91.0/24
EWIKI_IP_RANGE=172.30.91.0/25
EWIKI_PROXY_IP=172.30.91.250
EOF
# CI only: everything through the host's published ports
cat >docker-compose.override.yml <<'EOF'
services:
  wiki:
    extra_hosts: ["office.test:host-gateway", "docs.test:host-gateway"]
  collabora:
    ports: ["0.0.0.0:9980:9980"]
    extra_hosts: ["wiki.test:host-gateway"]
  onlyoffice:
    ports: ["0.0.0.0:8090:80"]
    extra_hosts: ["wiki.test:host-gateway"]
EOF
docker compose up -d --quiet-pull >/dev/null 2>&1 || fail "compose up"

wait_for() { # NAME URL PATTERN SECONDS
	local i
	for ((i = 0; i < $4; i += 5)); do
		if curl -fsS --max-time 5 "$2" 2>/dev/null | grep -q "$3"; then return 0; fi
		sleep 5
	done
	fail "$1 did not start ($2)"
}
echo "== waiting for the wiki, Collabora and OnlyOffice"
wait_for wiki http://127.0.0.1:8080/healthz ok 180
wait_for collabora http://127.0.0.1:9980/hosting/discovery wopi-discovery 300
wait_for onlyoffice http://127.0.0.1:8090/healthcheck true 420

echo "== browser: open, edit and save in both programs"
pw="$(sed -n 's/^EWIKI_ADMIN_PASSWORD=//p' .env | tail -n 1)"
cd "$src/web"
EWIKI_OFFICE_URL=http://wiki.test:8080 EWIKI_OFFICE_USER=admin EWIKI_OFFICE_PASSWORD="$pw" \
	node --test tests/office.real.mjs || { cd "$work"; fail "browser test"; }
cd "$work"
echo "OK"
