#!/usr/bin/env bash
# CI test of the plugin builds in the Docker images:
#   1. full image: a plugin in ./data/wiki/plugins is built at the first
#      start (placeholder page meanwhile), the wiki runs it, as the user of .env
#   2. a restart reuses the built binary (no second build)
#   3. a plugin copied into the folder while running is built automatically
#      (trigger "drop", no automatic restart)
#   4. lite image: starts, ignores additional plugins, cannot install
#   scripts/test-docker-plugins.sh
set -euo pipefail
src="$(cd "$(dirname "$0")/.." && pwd)"
fail() { echo "FAIL: $*" >&2; docker compose logs --no-color --tail 120 >&2 || true; exit 1; }

docker build -q --target full -t ewiki-test:full "$src" >/dev/null
docker build -q --target lite -t ewiki-test:lite "$src" >/dev/null

plugin() { # dir id
	mkdir -p "$1"
	cat > "$1/plugin.json" <<JSON
{"id": "$2", "name": "Test $2", "version": "1.0.0", "description": "CI test plugin"}
JSON
	pkg="$(basename "$1")"
	cat > "$1/plugin.go" <<GO
package $pkg

import "github.com/mtsonline/enterprisewiki/pkg/sdk"

type Plugin struct{}

func init() { sdk.Register(&Plugin{}) }

func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "$2", Name: "Test $2", Version: "1.0.0"}
}
GO
}

prepare() { # name image
	work="$(mktemp -d)/$1"; mkdir -p "$work"
	cp "$src"/{docker-compose.yml,.env.example,docker-setup.sh} "$work/"
	cd "$work"
	export COMPOSE_PROJECT_NAME="ewplug$1"
	./docker-setup.sh >/dev/null
	sed -i -E "s#^(\s+image:) ghcr.io/mtsonline/enterprisewiki:.*#\1 $2#; s#pull_policy: always#pull_policy: never#" docker-compose.yml
	grep -q "image: $2" docker-compose.yml || fail "image not replaced"
}
ready() { # seconds
	for _ in $(seq 1 "$1"); do curl -fs http://localhost:8080/readyz >/dev/null && return 0; sleep 1; done
	return 1
}

echo "== full image: plugin built at the first start"
prepare full ewiki-test:full
plugin data/wiki/plugins/test_hello test.hello
docker compose up -d
placeholder=no
for _ in $(seq 1 900); do
	body="$(curl -s http://localhost:8080/ || true)"
	case "$body" in *"wird aktualisiert"*) placeholder=yes ;; esac
	curl -fs http://localhost:8080/readyz >/dev/null && break
	sleep 1
done
ready 5 || fail "wiki not ready after the build"
[ "$placeholder" = yes ] || fail "no placeholder page during the build"
docker compose exec -T wiki ewiki plugin list | grep -q '^test.hello' || fail "test.hello not compiled in"
ls data/wiki/bin/ewiki-* >/dev/null || fail "no built binary in ./data/wiki/bin"
[ "$(docker compose exec -T wiki id -u)" = "$(id -u)" ] || fail "wiki does not run as the user of .env"
other="$(find data/wiki \( ! -uid "$(id -u)" \) -print | head -3)"; [ -z "$other" ] || fail "files of another user: $other"
echo "ok: built at the first start"

echo "== restart reuses the binary"
docker compose restart wiki >/dev/null
ready 60 || fail "not ready after the restart"
[ "$(docker compose logs wiki | grep -c 'building a binary with the additional plugins')" = 1 ] || fail "built again after a restart"
echo "ok: no second build"

echo "== plugin copied into the folder while running"
plugin data/wiki/plugins/test_second test.second
for _ in $(seq 1 600); do
	docker compose exec -T wiki ewiki plugin list 2>/dev/null | grep -q '^test.second' && break
	sleep 2
done
docker compose exec -T wiki ewiki plugin list | grep -q '^test.second' || fail "dropped plugin not built"
grep -q '"trigger":"drop"' data/wiki/bin/ewiki-*.json || fail "trigger drop not recorded"
curl -fs http://localhost:8080/readyz >/dev/null || fail "restarted automatically after a dropped plugin"
echo "ok: dropped plugin built, no automatic restart"
docker compose down >/dev/null 2>&1

echo "== lite image"
prepare lite ewiki-test:lite
plugin data/wiki/plugins/test_hello test.hello
docker compose up -d
ready 120 || fail "lite image not ready"
docker compose logs wiki | grep -q 'ignored in the lite image' || fail "no hint about ignored plugins"
if docker compose exec -T wiki ewiki plugin list | grep -q '^test.hello'; then fail "lite image runs the additional plugin"; fi
if docker compose exec -T wiki ewiki plugin install /data/plugins/test_hello >/dev/null 2>&1; then fail "lite image installed a plugin"; fi
[ ! -e data/wiki/bin ] || [ -z "$(ls data/wiki/bin)" ] || fail "lite image built something"
docker compose down >/dev/null 2>&1
echo "all plugin image tests passed"
